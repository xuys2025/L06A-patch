package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	qqMusicSearchURL = "https://u.y.qq.com/cgi-bin/musicu.fcg"
	kuwoSearchURL    = "http://search.kuwo.cn/r.s"
)

type MusicClient struct {
	mu       sync.Mutex
	queue    []MusicTrack
	index    int
	current  *MusicPlayback
	revision uint64
}

func NewMusicClient() *MusicClient { return &MusicClient{index: -1} }

type MusicIntent struct {
	Action string
	Query  string
	Volume int
}

type MusicVariant struct {
	Source string `json:"source"`
	ID     string `json:"id"`
}

type MusicTrack struct {
	Name       string         `json:"name"`
	Artist     string         `json:"artist"`
	Album      string         `json:"album,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	Variants   []MusicVariant `json:"variants"`
}

type MusicPlayback struct {
	Track         MusicTrack
	Variant       MusicVariant
	Quality       string
	State         string
	MediaURL      string `json:"-"`
	PositionMS    int64
	StartedAt     time.Time
	InterruptedAt time.Time
}

var volumePattern = regexp.MustCompile(`(?:音量(?:调到|设置为)?|声音(?:调到|设置为)?)[：: ]*([0-9]{1,3})`)

func ParseMusicIntent(input string) (MusicIntent, bool) {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(input, "。"), "！"))
	if t == "" {
		return MusicIntent{}, false
	}
	if isCurrentMusicQuestion(t) {
		return MusicIntent{Action: "current"}, true
	}
	if match := volumePattern.FindStringSubmatch(t); len(match) == 2 {
		v, _ := strconv.Atoi(match[1])
		if v > 100 {
			v = 100
		}
		return MusicIntent{Action: "volume", Volume: v}, true
	}
	switch {
	case strings.Contains(t, "暂停"):
		return MusicIntent{Action: "pause"}, true
	case strings.Contains(t, "继续播放") || t == "继续" || strings.Contains(t, "恢复播放"):
		return MusicIntent{Action: "resume"}, true
	case strings.Contains(t, "下一首") || strings.Contains(t, "换一首"):
		return MusicIntent{Action: "next"}, true
	case strings.Contains(t, "上一首"):
		return MusicIntent{Action: "prev"}, true
	case strings.Contains(t, "停止播放") || t == "停止" || strings.Contains(t, "关掉音乐"):
		return MusicIntent{Action: "stop"}, true
	}
	prefixes := []string{"请播放", "给我播放", "给我放", "帮我播放", "帮我放", "播放一下", "播放", "放一首", "放点", "放一下", "我要听", "我想听", "想听", "来一首", "来点", "听一下"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(t, prefix) {
			q := strings.TrimSpace(strings.TrimPrefix(t, prefix))
			q = strings.TrimSpace(strings.TrimSuffix(q, "这首歌"))
			if q != "" {
				return MusicIntent{Action: "play", Query: q}, true
			}
		}
	}
	return MusicIntent{}, false
}

func isCurrentMusicQuestion(text string) bool {
	for _, phrase := range []string{"这是什么歌", "这首歌叫什么", "现在放的什么", "现在播放什么", "正在播放什么", "当前播放什么", "这是谁唱的", "谁唱的歌", "当前歌曲"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func (c *MusicClient) Handle(ctx context.Context, cfg Config, secrets Secrets, intent MusicIntent) (string, error) {
	switch intent.Action {
	case "current":
		return c.CurrentDescription(), nil
	case "pause":
		if _, err := runCommand(cfg.PlayerCommand, "pause"); err != nil {
			return "", err
		}
		c.mu.Lock()
		if c.current != nil {
			c.freezePositionLocked(time.Now())
			c.current.State = "paused"
		}
		c.revision++
		c.mu.Unlock()
		return "已暂停", nil
	case "resume":
		resumed, err := c.resumeCurrent(ctx, cfg, secrets)
		if err != nil {
			return "", err
		}
		if resumed {
			return "继续播放", nil
		}
		return "没有可继续播放的歌曲", nil
	case "stop":
		if _, err := runCommand(cfg.PlayerCommand, "stop"); err != nil {
			return "", err
		}
		c.mu.Lock()
		if c.current != nil {
			c.freezePositionLocked(time.Now())
			c.current.State = "stopped"
		}
		c.revision++
		c.mu.Unlock()
		return "已停止播放", nil
	case "next", "prev":
		if secrets.MusicAPIKey != "" {
			delta := 1
			if intent.Action == "prev" {
				delta = -1
			}
			if playback, err := c.playAdjacent(ctx, cfg, secrets, delta); err == nil {
				return playbackDescription(playback), nil
			}
		}
		if _, err := runCommand(cfg.PlayerCommand, intent.Action); err != nil {
			return "", err
		}
		if intent.Action == "next" {
			return "下一首", nil
		}
		return "上一首", nil
	case "volume":
		if _, err := runCommand(cfg.VolumeCommand, strconv.Itoa(intent.Volume)); err != nil {
			return "", err
		}
		return fmt.Sprintf("音量已调到百分之%d", intent.Volume), nil
	case "play":
		if secrets.MusicAPIKey == "" {
			return "", errors.New("MUSIC_API_KEY is not configured")
		}
		tracks, err := searchMusic(ctx, cfg, intent.Query)
		if err != nil {
			return "", err
		}
		var lastErr error
		for index, track := range tracks {
			playback, playErr := c.playTrack(ctx, cfg, secrets, track)
			if playErr != nil {
				lastErr = errors.Join(lastErr, playErr)
				continue
			}
			c.mu.Lock()
			c.queue = cloneMusicTracks(tracks)
			c.index = index
			c.current = &playback
			c.revision++
			c.mu.Unlock()
			return playbackDescription(playback), nil
		}
		if lastErr == nil {
			lastErr = errors.New("搜索结果均不可播放")
		}
		return "", lastErr
	default:
		return "", errors.New("unsupported music action")
	}
}

func (c *MusicClient) CurrentDescription() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return "现在没有由我播放的歌曲"
	}
	prefix := "正在播放"
	switch c.current.State {
	case "paused":
		prefix = "现在暂停的是"
	case "interrupted":
		prefix = "刚才正在播放的是"
	case "stopped":
		prefix = "刚才播放的是"
	}
	return fmt.Sprintf("%s%s的%s，来源%s", prefix, displayArtist(c.current.Track.Artist), c.current.Track.Name, musicSourceLabel(c.current.Variant.Source))
}

func (c *MusicClient) ContextDescription() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return "当前没有由本助手播放的歌曲。"
	}
	return fmt.Sprintf("当前音乐状态：%s；歌曲：%s；歌手：%s；平台：%s；音质：%s。", c.current.State, c.current.Track.Name, displayArtist(c.current.Track.Artist), musicSourceLabel(c.current.Variant.Source), c.current.Quality)
}

func (c *MusicClient) BeginInterruption() (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return 0, false
	}
	if c.current.State == "interrupted" {
		return c.revision, true
	}
	if c.current.State != "playing" {
		return 0, false
	}
	now := time.Now()
	c.freezePositionLocked(now)
	c.current.InterruptedAt = now
	c.current.State = "interrupted"
	return c.revision, true
}

func (c *MusicClient) HasResumablePlayback() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return false
	}
	return c.current.State == "playing" || c.current.State == "interrupted" || c.current.State == "paused"
}

func (c *MusicClient) ResumeInterrupted(ctx context.Context, cfg Config, secrets Secrets, token uint64) (bool, error) {
	c.mu.Lock()
	if c.current == nil || c.revision != token || c.current.State != "interrupted" {
		c.mu.Unlock()
		return false, nil
	}
	snapshot := *c.current
	c.mu.Unlock()

	// Native wake/TTS normally ducks the original music without pausing it.
	// Never reopen its URL or seek to the wake checkpoint on automatic cleanup.
	result, err := runCommandContext(ctx, cfg.PlayerCommand, "finish_interruption", snapshot.Variant.ID)
	if err != nil {
		return false, err
	}
	result = strings.TrimSpace(result)
	if result != "continued" && result != "resumed" && result != "inactive" && result != "replaced" {
		return false, fmt.Errorf("invalid music interruption result: %q", result)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil || c.revision != token || c.current.State != "interrupted" {
		return false, nil
	}
	if result == "inactive" || result == "replaced" {
		c.current.State = "stopped"
		c.current.InterruptedAt = time.Time{}
		return false, nil
	}
	now := time.Now()
	if result == "continued" && !snapshot.InterruptedAt.IsZero() {
		// It kept advancing while ducked. Preserve that time for later pauses.
		c.current.PositionMS += max(now.Sub(snapshot.InterruptedAt).Milliseconds(), 0)
	}
	c.current.StartedAt = now
	c.current.InterruptedAt = time.Time{}
	c.current.State = "playing"
	return true, nil
}

func (c *MusicClient) freezePositionLocked(now time.Time) {
	if c.current == nil {
		return
	}
	started := c.current.StartedAt
	if started.IsZero() && c.current.State == "interrupted" {
		started = c.current.InterruptedAt
	}
	if started.IsZero() {
		return
	}
	elapsed := now.Sub(started).Milliseconds()
	if elapsed > 0 {
		c.current.PositionMS += elapsed
	}
	c.current.StartedAt = time.Time{}
	c.current.InterruptedAt = time.Time{}
}

func (c *MusicClient) resumeCurrent(ctx context.Context, cfg Config, secrets Secrets) (bool, error) {
	c.mu.Lock()
	if c.current == nil {
		c.mu.Unlock()
		return false, nil
	}
	if c.current.State == "interrupted" {
		token := c.revision
		c.mu.Unlock()
		return c.ResumeInterrupted(ctx, cfg, secrets, token)
	}
	if c.current.State != "paused" {
		c.mu.Unlock()
		return false, nil
	}
	snapshot := *c.current
	revision := c.revision
	c.mu.Unlock()

	mediaURL := snapshot.MediaURL
	variant := snapshot.Variant
	quality := snapshot.Quality
	position := snapshot.PositionMS
	if snapshot.Track.DurationMS > 0 && position >= snapshot.Track.DurationMS {
		position = max(snapshot.Track.DurationMS-1000, 0)
	}
	playAt := func(candidate string) error {
		if strings.TrimSpace(candidate) == "" {
			return errors.New("cached music URL is empty")
		}
		_, err := runCommand(cfg.PlayerCommand, "play_at", candidate,
			strconv.FormatInt(position, 10), variant.ID, strconv.FormatInt(snapshot.Track.DurationMS, 10))
		return err
	}

	playErr := playAt(mediaURL)
	if playErr != nil {
		freshURL, resolveErr := resolveMusicVariant(ctx, cfg, secrets, snapshot.Variant, snapshot.Quality)
		if resolveErr == nil {
			mediaURL = freshURL
			playErr = playAt(mediaURL)
		}
		if resolveErr != nil || playErr != nil {
			fallbackURL, fallbackVariant, fallbackQuality, fallbackErr := resolveMusic(ctx, cfg, secrets, snapshot.Track)
			if fallbackErr != nil {
				return false, errors.Join(playErr, resolveErr, fallbackErr)
			}
			mediaURL = fallbackURL
			variant = fallbackVariant
			quality = fallbackQuality
			if err := playAt(mediaURL); err != nil {
				return false, errors.Join(playErr, resolveErr, err)
			}
		}
	}

	c.mu.Lock()
	unchanged := c.current != nil && c.revision == revision && c.current.State == snapshot.State
	if unchanged {
		c.current.MediaURL = mediaURL
		c.current.Variant = variant
		c.current.Quality = quality
		c.current.PositionMS = position
		c.current.StartedAt = time.Now()
		c.current.InterruptedAt = time.Time{}
		c.current.State = "playing"
		c.revision++
	}
	c.mu.Unlock()
	if !unchanged {
		_, _ = runCommand(cfg.PlayerCommand, "stop")
		return false, nil
	}
	return true, nil
}

func (c *MusicClient) playAdjacent(ctx context.Context, cfg Config, secrets Secrets, delta int) (MusicPlayback, error) {
	c.mu.Lock()
	queue := cloneMusicTracks(c.queue)
	start := c.index
	c.mu.Unlock()
	if len(queue) == 0 || start < 0 {
		return MusicPlayback{}, errors.New("播放队列为空")
	}
	var lastErr error
	for step := 1; step <= len(queue); step++ {
		index := (start + delta*step) % len(queue)
		if index < 0 {
			index += len(queue)
		}
		playback, err := c.playTrack(ctx, cfg, secrets, queue[index])
		if err != nil {
			lastErr = errors.Join(lastErr, err)
			continue
		}
		c.mu.Lock()
		c.index = index
		c.current = &playback
		c.revision++
		c.mu.Unlock()
		return playback, nil
	}
	if lastErr == nil {
		lastErr = errors.New("播放队列没有可用歌曲")
	}
	return MusicPlayback{}, lastErr
}

func (c *MusicClient) playTrack(ctx context.Context, cfg Config, secrets Secrets, track MusicTrack) (MusicPlayback, error) {
	qualities := append([]string{cfg.MusicQuality}, cfg.MusicQualityFallback...)
	var lastErr error
	for _, variant := range track.Variants {
		seen := map[string]bool{}
		for _, quality := range qualities {
			quality = strings.TrimSpace(quality)
			if quality == "" || seen[quality] {
				continue
			}
			seen[quality] = true
			mediaURL, err := resolveMusicVariant(ctx, cfg, secrets, variant, quality)
			if err != nil {
				lastErr = errors.Join(lastErr, err)
				continue
			}
			if _, err := runCommand(cfg.PlayerCommand, "play", mediaURL, variant.ID, strconv.FormatInt(track.DurationMS, 10)); err != nil {
				lastErr = errors.Join(lastErr, fmt.Errorf("start %s from %s at %s: %w", track.Name, musicSourceLabel(variant.Source), quality, err))
				continue
			}
			return MusicPlayback{Track: track, Variant: variant, Quality: quality, State: "playing", MediaURL: mediaURL, StartedAt: time.Now()}, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的音乐来源或音质")
	}
	return MusicPlayback{}, fmt.Errorf("play %s - %s: %w", track.Name, track.Artist, lastErr)
}

type musicSearchResult struct {
	tracks []MusicTrack
	err    error
}

func searchMusic(ctx context.Context, cfg Config, query string) ([]MusicTrack, error) {
	spec := parseMusicSearchSpec(query)
	client := &http.Client{Timeout: 15 * time.Second}
	providers := []func(context.Context, *http.Client, Config, string) ([]MusicTrack, error){searchNetEase, searchQQMusic, searchKuwo}
	results := make([]musicSearchResult, len(providers))
	var wg sync.WaitGroup
	for index, provider := range providers {
		wg.Add(1)
		go func(index int, provider func(context.Context, *http.Client, Config, string) ([]MusicTrack, error)) {
			defer wg.Done()
			results[index].tracks, results[index].err = provider(ctx, client, cfg, spec.SearchText)
		}(index, provider)
	}
	wg.Wait()
	var merged []MusicTrack
	var searchErr error
	for _, result := range results {
		if result.err != nil {
			searchErr = errors.Join(searchErr, result.err)
			continue
		}
		merged = mergeMusicTracks(merged, result.tracks)
	}
	if len(merged) == 0 {
		if searchErr == nil {
			searchErr = fmt.Errorf("没有找到歌曲“%s”", query)
		}
		return nil, searchErr
	}
	rankMusicTracks(merged, spec)
	return merged, nil
}

func searchNetEase(ctx context.Context, client *http.Client, cfg Config, query string) ([]MusicTrack, error) {
	form := url.Values{"s": {query}, "type": {"1"}, "offset": {"0"}, "limit": {"10"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.MusicSearchURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://music.163.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 L06A-Assistant/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网易云搜索: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("网易云搜索 HTTP %d", resp.StatusCode)
	}
	var data struct {
		Code   int `json:"code"`
		Result struct {
			Songs []struct {
				ID      json.Number `json:"id"`
				Name    string      `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name string `json:"name"`
				} `json:"album"`
				Duration int64 `json:"duration"`
			} `json:"songs"`
		} `json:"result"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("网易云搜索响应: %w", err)
	}
	if data.Code != 200 || len(data.Result.Songs) == 0 {
		return nil, fmt.Errorf("网易云没有找到歌曲“%s”", query)
	}
	tracks := make([]MusicTrack, 0, len(data.Result.Songs))
	for _, song := range data.Result.Songs {
		artists := make([]string, 0, len(song.Artists))
		for _, artist := range song.Artists {
			if artist.Name != "" {
				artists = append(artists, artist.Name)
			}
		}
		tracks = append(tracks, newMusicTrack(string(song.ID), "wy", song.Name, strings.Join(artists, "、"), song.Album.Name, song.Duration))
	}
	return tracks, nil
}

func searchQQMusic(ctx context.Context, client *http.Client, _ Config, query string) ([]MusicTrack, error) {
	payload := map[string]any{"req_1": map[string]any{
		"method": "DoSearchForQQMusicDesktop",
		"module": "music.search.SearchCgiService",
		"param":  map[string]any{"num_per_page": 10, "page_num": 1, "query": query, "search_type": 0},
	}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qqMusicSearchURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://y.qq.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 L06A-Assistant/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("QQ音乐搜索: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("QQ音乐搜索 HTTP %d", resp.StatusCode)
	}
	var data struct {
		Code int `json:"code"`
		Req  struct {
			Code int `json:"code"`
			Data struct {
				Body struct {
					Song struct {
						List []struct {
							ID       int64  `json:"id"`
							Mid      string `json:"mid"`
							Title    string `json:"title"`
							Interval int64  `json:"interval"`
							Singer   []struct {
								Name string `json:"name"`
							} `json:"singer"`
							Album struct {
								Name string `json:"name"`
							} `json:"album"`
						} `json:"list"`
					} `json:"song"`
				} `json:"body"`
			} `json:"data"`
		} `json:"req_1"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 3<<20)).Decode(&data); err != nil {
		return nil, fmt.Errorf("QQ音乐搜索响应: %w", err)
	}
	if data.Code != 0 || data.Req.Code != 0 || len(data.Req.Data.Body.Song.List) == 0 {
		return nil, fmt.Errorf("QQ音乐没有找到歌曲“%s”", query)
	}
	tracks := make([]MusicTrack, 0, len(data.Req.Data.Body.Song.List))
	for _, song := range data.Req.Data.Body.Song.List {
		id := song.Mid
		if id == "" {
			id = strconv.FormatInt(song.ID, 10)
		}
		artists := make([]string, 0, len(song.Singer))
		for _, singer := range song.Singer {
			if singer.Name != "" {
				artists = append(artists, singer.Name)
			}
		}
		tracks = append(tracks, newMusicTrack(id, "tx", song.Title, strings.Join(artists, "、"), song.Album.Name, song.Interval*1000))
	}
	return tracks, nil
}

func searchKuwo(ctx context.Context, client *http.Client, _ Config, query string) ([]MusicTrack, error) {
	u, _ := url.Parse(kuwoSearchURL)
	q := u.Query()
	for key, value := range map[string]string{
		"client": "kt", "all": query, "pn": "0", "rn": "10", "uid": "2574109560",
		"ver": "kwplayer_ar_8.5.4.2", "vipver": "1", "ft": "music", "cluster": "0",
		"strategy": "2012", "encoding": "utf8", "rformat": "json", "vermerge": "1", "mobi": "1",
	} {
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 L06A-Assistant/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("酷我搜索: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("酷我搜索 HTTP %d", resp.StatusCode)
	}
	var data struct {
		Songs []struct {
			ID       string `json:"MUSICRID"`
			Name     string `json:"NAME"`
			Artist   string `json:"ARTIST"`
			Album    string `json:"ALBUM"`
			Duration string `json:"DURATION"`
		} `json:"abslist"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 3<<20)).Decode(&data); err != nil {
		return nil, fmt.Errorf("酷我搜索响应: %w", err)
	}
	if len(data.Songs) == 0 {
		return nil, fmt.Errorf("酷我没有找到歌曲“%s”", query)
	}
	tracks := make([]MusicTrack, 0, len(data.Songs))
	for _, song := range data.Songs {
		id := strings.TrimPrefix(song.ID, "MUSIC_")
		tracks = append(tracks, newMusicTrack(id, "kw", song.Name, song.Artist, song.Album, parseMusicDurationMS(song.Duration)))
	}
	return tracks, nil
}

func resolveMusic(ctx context.Context, cfg Config, secrets Secrets, track MusicTrack) (string, MusicVariant, string, error) {
	qualities := append([]string{cfg.MusicQuality}, cfg.MusicQualityFallback...)
	var lastErr error
	for _, variant := range track.Variants {
		seen := map[string]bool{}
		for _, quality := range qualities {
			quality = strings.TrimSpace(quality)
			if quality == "" || seen[quality] {
				continue
			}
			seen[quality] = true
			mediaURL, err := resolveMusicVariant(ctx, cfg, secrets, variant, quality)
			if err == nil {
				return mediaURL, variant, quality, nil
			}
			lastErr = errors.Join(lastErr, err)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的音乐来源或音质")
	}
	return "", MusicVariant{}, "", lastErr
}

func resolveMusicVariant(ctx context.Context, cfg Config, secrets Secrets, variant MusicVariant, quality string) (string, error) {
	u, err := url.Parse(strings.TrimRight(cfg.MusicAPIBase, "/") + "/music/url")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("source", variant.Source)
	q.Set("songId", variant.ID)
	q.Set("quality", quality)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", secrets.MusicAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 L06A-Assistant/1.0")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", variant.Source, quality, err)
	}
	defer resp.Body.Close()
	var data struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		URL     string `json:"url"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data)
	if decodeErr != nil {
		return "", fmt.Errorf("%s %s HTTP %d: %w", variant.Source, quality, resp.StatusCode, decodeErr)
	}
	if resp.StatusCode != http.StatusOK || data.Code != 200 || data.URL == "" {
		return "", fmt.Errorf("%s %s HTTP %d code %d: %s", variant.Source, quality, resp.StatusCode, data.Code, data.Message)
	}
	mediaURL, err := url.Parse(data.URL)
	if err != nil || (mediaURL.Scheme != "http" && mediaURL.Scheme != "https") || mediaURL.Host == "" {
		return "", fmt.Errorf("%s %s returned an invalid media URL", variant.Source, quality)
	}
	return data.URL, nil
}

func newMusicTrack(id, source, name, artist, album string, durationMS ...int64) MusicTrack {
	track := MusicTrack{Name: cleanMusicText(name), Artist: cleanMusicText(artist), Album: cleanMusicText(album), Variants: []MusicVariant{{Source: source, ID: strings.TrimSpace(id)}}}
	if len(durationMS) > 0 && durationMS[0] > 0 {
		track.DurationMS = durationMS[0]
	}
	return track
}

func parseMusicDurationMS(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parts := strings.Split(value, ":")
	if len(parts) == 1 {
		seconds, _ := strconv.ParseInt(parts[0], 10, 64)
		return max(seconds, 0) * 1000
	}
	var seconds int64
	for _, part := range parts {
		component, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || component < 0 {
			return 0
		}
		seconds = seconds*60 + component
	}
	return seconds * 1000
}

func cleanMusicText(value string) string {
	return strings.TrimSpace(html.UnescapeString(value))
}

func mergeMusicTracks(existing, incoming []MusicTrack) []MusicTrack {
	indexes := make(map[string]int, len(existing))
	for index, track := range existing {
		indexes[musicTrackKey(track)] = index
	}
	for _, track := range incoming {
		key := musicTrackKey(track)
		if index, ok := indexes[key]; ok && key != "|" {
			if existing[index].DurationMS == 0 && track.DurationMS > 0 {
				existing[index].DurationMS = track.DurationMS
			}
			for _, variant := range track.Variants {
				if !hasMusicVariant(existing[index].Variants, variant) {
					existing[index].Variants = append(existing[index].Variants, variant)
				}
			}
			continue
		}
		indexes[key] = len(existing)
		existing = append(existing, track)
	}
	return existing
}

type musicSearchSpec struct {
	Raw        string
	SearchText string
	Artist     string
	Title      string
}

func parseMusicSearchSpec(query string) musicSearchSpec {
	raw := strings.TrimSpace(strings.Trim(query, "，。！？!?、 "))
	spec := musicSearchSpec{Raw: raw, SearchText: raw}
	for _, suffix := range []string{"的歌曲", "的作品", "的音乐", "的歌"} {
		if strings.HasSuffix(raw, suffix) {
			artist := strings.TrimSpace(strings.TrimSuffix(raw, suffix))
			if artist != "" {
				spec.Artist = artist
				spec.SearchText = artist
				return spec
			}
		}
	}
	if separator := strings.Index(raw, "的"); separator > 0 && separator < len(raw)-len("的") {
		artist := strings.TrimSpace(raw[:separator])
		title := strings.TrimSpace(raw[separator+len("的"):])
		if artist != "" && title != "" {
			spec.Artist = artist
			spec.Title = title
			spec.SearchText = artist + " " + title
		}
	}
	return spec
}

func rankMusicTracks(tracks []MusicTrack, spec musicSearchSpec) {
	for index := range tracks {
		sort.SliceStable(tracks[index].Variants, func(i, j int) bool {
			return musicSourcePriority(tracks[index].Variants[i].Source) > musicSourcePriority(tracks[index].Variants[j].Source)
		})
	}
	sort.SliceStable(tracks, func(i, j int) bool {
		return musicTrackScore(tracks[i], spec) > musicTrackScore(tracks[j], spec)
	})
}

func musicTrackScore(track MusicTrack, spec musicSearchSpec) int {
	score := bestMusicSourcePriority(track.Variants)
	title := normalizeMusicText(track.Name)
	artistParts := splitMusicArtists(track.Artist)
	if spec.Artist != "" {
		desiredArtist := normalizeMusicText(spec.Artist)
		switch {
		case containsExactMusicText(artistParts, desiredArtist):
			score += 1200
		case containsPartialMusicText(artistParts, desiredArtist):
			score += 500
		default:
			score -= 1200
		}
	}
	if spec.Title != "" {
		desiredTitle := normalizeMusicText(spec.Title)
		switch {
		case title == desiredTitle:
			score += 1400
		case strings.Contains(title, desiredTitle):
			score += 600
		default:
			score -= 800
		}
	} else if spec.Artist == "" {
		query := normalizeMusicText(spec.Raw)
		switch {
		case title == query:
			score += 500
		case query != "" && strings.Contains(title, query):
			score += 250
		case containsExactMusicText(artistParts, query):
			score += 200
		}
	}
	for _, marker := range []string{"翻唱", "深情版", "dj", "伴奏", "纯音乐", "白噪音", "片段", "cover"} {
		if strings.Contains(strings.ToLower(track.Name), marker) && !strings.Contains(strings.ToLower(spec.Title), marker) {
			score -= 300
		}
	}
	return score
}

func splitMusicArtists(artists string) []string {
	parts := strings.FieldsFunc(artists, func(r rune) bool {
		switch r {
		case '、', ',', '，', '/', '&', '＆':
			return true
		default:
			return false
		}
	})
	for index := range parts {
		parts[index] = normalizeMusicText(parts[index])
	}
	return parts
}

func containsExactMusicText(values []string, desired string) bool {
	for _, value := range values {
		if value == desired {
			return true
		}
	}
	return false
}

func containsPartialMusicText(values []string, desired string) bool {
	for _, value := range values {
		if desired != "" && (strings.Contains(value, desired) || strings.Contains(desired, value)) {
			return true
		}
	}
	return false
}

func bestMusicSourcePriority(variants []MusicVariant) int {
	best := 0
	for _, variant := range variants {
		if priority := musicSourcePriority(variant.Source); priority > best {
			best = priority
		}
	}
	return best
}

func musicSourcePriority(source string) int {
	switch source {
	case "tx":
		return 100
	case "wy":
		return 60
	case "kw":
		return 40
	default:
		return 0
	}
}

func musicTrackKey(track MusicTrack) string {
	return normalizeMusicText(track.Name) + "|" + normalizeMusicText(track.Artist)
}

func normalizeMusicText(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func hasMusicVariant(variants []MusicVariant, candidate MusicVariant) bool {
	for _, variant := range variants {
		if variant.Source == candidate.Source && variant.ID == candidate.ID {
			return true
		}
	}
	return false
}

func cloneMusicTracks(tracks []MusicTrack) []MusicTrack {
	cloned := make([]MusicTrack, len(tracks))
	for index, track := range tracks {
		cloned[index] = track
		cloned[index].Variants = append([]MusicVariant(nil), track.Variants...)
	}
	return cloned
}

func playbackDescription(playback MusicPlayback) string {
	return fmt.Sprintf("正在播放%s的%s，来源%s，音质%s", displayArtist(playback.Track.Artist), playback.Track.Name, musicSourceLabel(playback.Variant.Source), playback.Quality)
}

func displayArtist(artist string) string {
	if strings.TrimSpace(artist) == "" {
		return "未知歌手"
	}
	return artist
}

func musicSourceLabel(source string) string {
	switch source {
	case "wy":
		return "网易云音乐"
	case "tx":
		return "QQ音乐"
	case "kw":
		return "酷我音乐"
	default:
		return source
	}
}
