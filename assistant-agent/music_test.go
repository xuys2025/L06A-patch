package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMergeMusicTracksKeepsProviderFallbacks(t *testing.T) {
	tracks := []MusicTrack{newMusicTrack("1357375695", "wy", "海阔天空", "Beyond", "乐与怒")}
	tracks = mergeMusicTracks(tracks, []MusicTrack{
		newMusicTrack("001yS0N33yPm1B", "tx", "海阔天空", "BEYOND", "乐与怒", 310000),
		newMusicTrack("5886682", "kw", "海阔天空", "BEYOND", "乐与怒"),
	})
	if len(tracks) != 1 {
		t.Fatalf("expected one merged track, got %#v", tracks)
	}
	if len(tracks[0].Variants) != 3 {
		t.Fatalf("expected three provider variants, got %#v", tracks[0].Variants)
	}
	if tracks[0].DurationMS != 310000 {
		t.Fatalf("expected merged duration, got %#v", tracks[0])
	}
}

func TestParseMusicDurationMS(t *testing.T) {
	for input, expected := range map[string]int64{"03:45": 225000, "1:02:03": 3723000, "180": 180000, "bad": 0} {
		if got := parseMusicDurationMS(input); got != expected {
			t.Fatalf("parseMusicDurationMS(%q) = %d, want %d", input, got, expected)
		}
	}
}

func TestResolveMusicFallsBackAcrossProvidersAndQuality(t *testing.T) {
	type request struct{ Source, Quality, Key string }
	var requests []request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, request{r.URL.Query().Get("source"), r.URL.Query().Get("quality"), r.Header.Get("X-API-Key")})
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("source") == "tx" && r.URL.Query().Get("quality") == "320k" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "url": serverURL(r) + "/audio.mp3"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "message": "unavailable"})
	}))
	defer server.Close()

	cfg := defaultConfig()
	cfg.MusicAPIBase = server.URL
	cfg.MusicQuality = "320k"
	cfg.MusicQualityFallback = []string{"128k"}
	track := MusicTrack{Name: "海阔天空", Artist: "Beyond", Variants: []MusicVariant{{Source: "wy", ID: "1"}, {Source: "tx", ID: "mid"}}}
	mediaURL, variant, quality, err := resolveMusic(context.Background(), cfg, Secrets{MusicAPIKey: "test-key"}, track)
	if err != nil {
		t.Fatal(err)
	}
	if mediaURL == "" || variant.Source != "tx" || quality != "320k" {
		t.Fatalf("unexpected fallback result: %q %#v %q", mediaURL, variant, quality)
	}
	if len(requests) != 3 || requests[0].Source != "wy" || requests[0].Quality != "320k" || requests[1].Quality != "128k" || requests[2].Source != "tx" {
		t.Fatalf("unexpected request order: %#v", requests)
	}
	for _, got := range requests {
		if got.Key != "test-key" {
			t.Fatalf("API key missing from request: %#v", got)
		}
	}
}

func TestMusicToolArguments(t *testing.T) {
	intent, err := musicIntentFromTool(LLMToolRequest{Name: "play_music", Arguments: `{"query":"适合睡前的周杰伦"}`})
	if err != nil || intent.Action != "play" || intent.Query != "适合睡前的周杰伦" {
		t.Fatalf("play tool -> %#v, %v", intent, err)
	}
	intent, err = musicIntentFromTool(LLMToolRequest{Name: "music_control", Arguments: `{"action":"volume","volume":35}`})
	if err != nil || intent.Action != "volume" || intent.Volume != 35 {
		t.Fatalf("control tool -> %#v, %v", intent, err)
	}
	intent, err = musicIntentFromTool(LLMToolRequest{Name: "get_current_music", Arguments: `{}`})
	if err != nil || intent.Action != "current" {
		t.Fatalf("current tool -> %#v, %v", intent, err)
	}
	intent, err = musicIntentFromTool(LLMToolRequest{Name: "play_music", Arguments: `{"query":"放首歌","artist":"周杰伦","title":"稻香","version":"原版"}`})
	if err != nil || intent.Query != "周杰伦的稻香" {
		t.Fatalf("structured play tool -> %#v, %v", intent, err)
	}
}

func TestCurrentMusicQuestion(t *testing.T) {
	for _, input := range []string{"这是什么歌", "现在播放什么", "这是谁唱的"} {
		intent, ok := ParseMusicIntent(input)
		if !ok || intent.Action != "current" {
			t.Fatalf("%q -> %#v, %v", input, intent, ok)
		}
	}
}

func TestObservedMusicRequestPhrase(t *testing.T) {
	intent, ok := ParseMusicIntent("我要听五月天的知足。")
	if !ok || intent.Action != "play" || intent.Query != "五月天的知足" {
		t.Fatalf("observed device phrase -> %#v, %v", intent, ok)
	}
}

func TestSpecifiedArtistRankingPrefersOriginalAndQQ(t *testing.T) {
	tracks := []MusicTrack{
		newMusicTrack("cover", "wy", "稻香(深情版)", "Lucky小爱", ""),
		newMusicTrack("original-wy", "wy", "稻香", "周杰伦", "魔杰座"),
		newMusicTrack("noise", "wy", "稻香", "粉白噪音", ""),
	}
	tracks = mergeMusicTracks(tracks, []MusicTrack{newMusicTrack("original-qq", "tx", "稻香", "周杰伦", "魔杰座")})
	rankMusicTracks(tracks, parseMusicSearchSpec("周杰伦的稻香"))
	if tracks[0].Name != "稻香" || tracks[0].Artist != "周杰伦" {
		t.Fatalf("wrong first result: %#v", tracks[0])
	}
	if tracks[0].Variants[0].Source != "tx" {
		t.Fatalf("QQ should be tried first for merged original: %#v", tracks[0].Variants)
	}
}

func TestArtistOnlySearchSpec(t *testing.T) {
	spec := parseMusicSearchSpec("周杰伦的歌")
	if spec.Artist != "周杰伦" || spec.Title != "" || spec.SearchText != "周杰伦" {
		t.Fatalf("unexpected artist search spec: %#v", spec)
	}
}

func TestInterruptedMusicResumesOnlyWhenUnchanged(t *testing.T) {
	client := NewMusicClient()
	client.current = &MusicPlayback{
		Track:     newMusicTrack("id", "tx", "稻香", "周杰伦", ""),
		Variant:   MusicVariant{Source: "tx", ID: "id"},
		Quality:   "320k",
		State:     "playing",
		MediaURL:  "https://example.invalid/song.mp3",
		StartedAt: time.Now().Add(-5 * time.Second),
	}
	client.revision = 7
	token, ok := client.BeginInterruption()
	if !ok || token != 7 || client.current.State != "interrupted" {
		t.Fatalf("begin interruption: token=%d ok=%v current=%#v", token, ok, client.current)
	}
	if client.current.PositionMS < 4900 || client.current.PositionMS > 5500 || !client.current.StartedAt.IsZero() {
		t.Fatalf("interruption did not freeze playback position: %#v", client.current)
	}
	cfg := defaultConfig()
	cfg.PlayerCommand = interruptionTestPlayer(t, "continued")
	client.current.InterruptedAt = time.Now().Add(-12 * time.Second)
	resumed, err := client.ResumeInterrupted(context.Background(), cfg, Secrets{}, token)
	if err != nil || !resumed || client.current.State != "playing" {
		t.Fatalf("resume interruption: resumed=%v err=%v current=%#v", resumed, err, client.current)
	}
	if client.current.StartedAt.IsZero() {
		t.Fatalf("resume did not restart playback clock: %#v", client.current)
	}
	if client.current.PositionMS < 16900 || client.current.PositionMS > 18000 {
		t.Fatalf("ducked playback time was lost: %#v", client.current)
	}
	commands, _ := os.ReadFile(cfg.PlayerCommand + ".calls")
	if string(commands) != "finish_interruption id\n" {
		t.Fatalf("automatic cleanup reloaded or sought the track: %q", commands)
	}

	client.current.State = "interrupted"
	client.revision++
	resumed, err = client.ResumeInterrupted(context.Background(), cfg, Secrets{}, token)
	if err != nil || resumed {
		t.Fatalf("stale interruption resumed: resumed=%v err=%v", resumed, err)
	}
	after, _ := os.ReadFile(cfg.PlayerCommand + ".calls")
	if string(after) != string(commands) {
		t.Fatal("stale session touched the player")
	}
}

func interruptionTestPlayer(t *testing.T, result string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "player")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$0.calls"
[ "$1" = finish_interruption ] || exit 99
cat "$0.result"
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".result", []byte(result), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExplicitContinueWhileDuckedDoesNotSeek(t *testing.T) {
	client := NewMusicClient()
	client.current = &MusicPlayback{State: "interrupted", PositionMS: 30000, InterruptedAt: time.Now().Add(-4 * time.Second), Variant: MusicVariant{ID: "id"}}
	cfg := defaultConfig()
	cfg.PlayerCommand = interruptionTestPlayer(t, "continued")
	if _, err := client.Handle(context.Background(), cfg, Secrets{}, MusicIntent{Action: "resume"}); err != nil {
		t.Fatal(err)
	}
	commands, _ := os.ReadFile(cfg.PlayerCommand + ".calls")
	if string(commands) != "finish_interruption id\n" {
		t.Fatalf("continue sought backward: %q", commands)
	}
}

func TestPauseCheckpointIncludesDuckedPlaybackTime(t *testing.T) {
	now := time.Now()
	client := NewMusicClient()
	client.current = &MusicPlayback{State: "interrupted", PositionMS: 30000, InterruptedAt: now.Add(-5 * time.Second)}
	client.freezePositionLocked(now)
	if client.current.PositionMS != 35000 || !client.current.InterruptedAt.IsZero() {
		t.Fatal("pause froze the old wake position")
	}
	client.freezePositionLocked(now.Add(time.Second))
	if client.current.PositionMS != 35000 {
		t.Fatal("pause counted the same time twice")
	}
}

func TestNativeInterruptionOutcomesNeverReopenURL(t *testing.T) {
	for _, result := range []string{"resumed", "inactive", "replaced", "bad-context"} {
		t.Run(result, func(t *testing.T) {
			client := NewMusicClient()
			client.current = &MusicPlayback{State: "interrupted", PositionMS: 30000, InterruptedAt: time.Now().Add(-10 * time.Second), Variant: MusicVariant{ID: "same-track"}}
			cfg := defaultConfig()
			cfg.PlayerCommand = interruptionTestPlayer(t, result)
			resumed, err := client.ResumeInterrupted(context.Background(), cfg, Secrets{}, 0)
			if result == "bad-context" {
				if err == nil || resumed {
					t.Fatal("invalid player result must fail without fallback")
				}
			} else if err != nil || resumed != (result == "resumed") {
				t.Fatalf("result=%s resumed=%v error=%v", result, resumed, err)
			}
			if client.current.PositionMS != 30000 {
				t.Fatal("paused/inactive position should not advance")
			}
			commands, _ := os.ReadFile(cfg.PlayerCommand + ".calls")
			if strings.TrimSpace(string(commands)) != "finish_interruption same-track" {
				t.Fatalf("unexpected commands %q", commands)
			}
		})
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
