package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type AgentStatus struct {
	Version        string    `json:"version"`
	State          string    `json:"state"`
	SessionSource  string    `json:"session_source"`
	LastTranscript string    `json:"last_transcript"`
	LastResponse   string    `json:"last_response"`
	LastError      string    `json:"last_error"`
	UpdatedAt      time.Time `json:"updated_at"`
	Configured     struct {
		ASR       bool `json:"asr"`
		LLM       bool `json:"llm"`
		Music     bool `json:"music"`
		RemoteTTS bool `json:"remote_tts"`
	} `json:"configured"`
}

type Agent struct {
	store               *ConfigStore
	music               *MusicClient
	mu                  sync.Mutex
	status              AgentStatus
	history             []ChatMessage
	cancel              context.CancelFunc
	sessionGen          uint64
	logs                []string
	native              *NativeSpeechServer
	nativeFollowups     chan *NativeSpeechTurn
	pendingNativeSource string
	pendingMusicResume  bool
	pendingMusicToken   uint64
	led                 *LEDController
}

func NewAgent(store *ConfigStore) *Agent {
	a := &Agent{store: store, music: NewMusicClient()}
	a.led = NewLEDController(a.logf)
	a.status.Version = buildVersion
	a.status.State = "IDLE"
	a.status.UpdatedAt = time.Now()
	a.refreshConfigured()
	return a
}

func (a *Agent) refreshConfigured() {
	cfg, secrets := a.store.Snapshot()
	a.mu.Lock()
	a.status.Configured.ASR = secrets.ASRAPIKey != ""
	a.status.Configured.LLM = secrets.LLMAPIKey != "" && cfg.LLMBaseURL != "" && cfg.LLMModel != ""
	a.status.Configured.Music = secrets.MusicAPIKey != ""
	a.status.Configured.RemoteTTS = secrets.TTSAPIKey != "" && cfg.TTSBaseURL != ""
	a.status.UpdatedAt = time.Now()
	a.mu.Unlock()
}

func (a *Agent) Status() AgentStatus {
	a.refreshConfigured()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *Agent) Logs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.logs...)
}

func (a *Agent) logf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	log.Print(line)
	a.mu.Lock()
	a.logs = append(a.logs, time.Now().Format(time.RFC3339)+" "+line)
	if len(a.logs) > 250 {
		a.logs = append([]string(nil), a.logs[len(a.logs)-250:]...)
	}
	a.mu.Unlock()
}

func (a *Agent) setState(state string) {
	if err := a.setStateChecked(state); err != nil {
		a.logf("set state %s LED failed: %v", state, err)
	}
}

func (a *Agent) setStateChecked(state string) error {
	a.mu.Lock()
	a.status.State = state
	a.status.UpdatedAt = time.Now()
	a.mu.Unlock()
	if a.led != nil {
		return a.led.Set(state)
	}
	return nil
}

func (a *Agent) setError(err error) {
	a.mu.Lock()
	if err == nil {
		a.status.LastError = ""
	} else {
		a.status.LastError = err.Error()
	}
	a.status.UpdatedAt = time.Now()
	a.mu.Unlock()
}

func (a *Agent) SetNativeSpeech(server *NativeSpeechServer) {
	a.mu.Lock()
	a.native = server
	a.mu.Unlock()
}

func (a *Agent) PrepareNativeSpeechTurn(wakeup bool) error {
	resumeMusic := false
	resumeToken := uint64(0)
	if wakeup {
		resumeToken, resumeMusic = a.music.BeginInterruption()
	}
	a.mu.Lock()
	previousState := a.status.State
	cancel := a.cancel
	if resumeMusic {
		a.pendingMusicResume = true
		a.pendingMusicToken = resumeToken
	}
	a.mu.Unlock()
	if wakeup && (previousState == "SPEAKING" || previousState == "RESPONDING") {
		if cancel != nil {
			cancel()
		}
		a.interruptCurrentAnswer()
	}
	if err := a.setStateChecked("LISTENING"); err != nil {
		if resumeMusic {
			cfg, secrets := a.store.Snapshot()
			resumeCtx, cancelResume := context.WithTimeout(context.Background(), 30*time.Second)
			_, _ = a.music.ResumeInterrupted(resumeCtx, cfg, secrets, resumeToken)
			cancelResume()
			a.mu.Lock()
			a.pendingMusicResume = false
			a.pendingMusicToken = 0
			a.mu.Unlock()
		}
		return fmt.Errorf("privacy guard refused capture because listening LED failed: %w", err)
	}
	return nil
}

func (a *Agent) AcceptNativeTurn(turn *NativeSpeechTurn) {
	if turn == nil {
		return
	}
	a.mu.Lock()
	followups := a.nativeFollowups
	active := a.cancel != nil && followups != nil
	pendingSource := a.pendingNativeSource
	if pendingSource != "" {
		a.pendingNativeSource = ""
	}
	a.mu.Unlock()
	if turn.Wakeup {
		a.startNativeSession(turn, "hotword")
		return
	}
	if active {
		select {
		case followups <- turn:
			a.logf("native follow-up received turn=%d", turn.ID)
		default:
			a.logf("native follow-up queue full turn=%d", turn.ID)
			_ = turn.StopCapture()
			_ = turn.Finish(false)
		}
		return
	}
	if pendingSource == "" {
		pendingSource = "native"
	}
	a.startNativeSession(turn, pendingSource)
}

func (a *Agent) TriggerWake(source string) {
	a.mu.Lock()
	native := a.native
	if native != nil {
		a.pendingNativeSource = source
	}
	a.mu.Unlock()
	if native != nil {
		a.logf("native capture requested source=%s", source)
		go func() {
			if err := native.RequestCapture(); err != nil {
				a.logf("native capture request failed: %v", err)
				a.setError(err)
			}
		}()
		return
	}
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.sessionGen++
	gen := a.sessionGen
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.status.SessionSource = source
	a.status.LastError = ""
	a.status.UpdatedAt = time.Now()
	a.mu.Unlock()
	a.logf("wake source=%s", source)
	go func() {
		err := a.runVoiceSession(ctx)
		a.mu.Lock()
		current := a.sessionGen == gen
		a.mu.Unlock()
		if current {
			if _, finishErr := runCommand("/bin/wakeup.sh", "ready", "0"); finishErr != nil {
				a.logf("finish wake signal failed: %v", finishErr)
			}
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			a.logf("session error: %v", err)
			a.setError(err)
		}
		a.mu.Lock()
		becameIdle := false
		if a.sessionGen == gen {
			a.status.State = "IDLE"
			a.status.UpdatedAt = time.Now()
			a.cancel = nil
			becameIdle = true
		}
		a.mu.Unlock()
		if becameIdle && a.led != nil {
			a.led.Set("IDLE")
		}
	}()
}

func (a *Agent) startNativeSession(first *NativeSpeechTurn, source string) {
	a.mu.Lock()
	previousState := a.status.State
	if a.cancel != nil {
		a.cancel()
	}
	a.sessionGen++
	gen := a.sessionGen
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	followups := make(chan *NativeSpeechTurn, 2)
	a.nativeFollowups = followups
	resumeMusic := a.pendingMusicResume
	resumeToken := a.pendingMusicToken
	a.pendingMusicResume = false
	a.pendingMusicToken = 0
	a.status.SessionSource = source
	a.status.LastError = ""
	a.status.UpdatedAt = time.Now()
	a.mu.Unlock()
	if first.Wakeup && (previousState == "SPEAKING" || previousState == "RESPONDING") {
		a.interruptCurrentAnswer()
	}
	a.logf("wake source=%s native_turn=%d", source, first.ID)
	go func() {
		err := a.runNativeVoiceSession(ctx, first, followups, resumeMusic)
		if err != nil && !errors.Is(err, context.Canceled) {
			a.logf("session error: %v", err)
			a.setError(err)
		}
		a.mu.Lock()
		current := a.sessionGen == gen
		if current {
			a.cancel = nil
			a.nativeFollowups = nil
		}
		a.mu.Unlock()
		if current {
			finalState := "IDLE"
			if resumeMusic {
				cfg, secrets := a.store.Snapshot()
				resumeCtx, cancelResume := context.WithTimeout(context.Background(), 30*time.Second)
				resumed, resumeErr := a.music.ResumeInterrupted(resumeCtx, cfg, secrets, resumeToken)
				cancelResume()
				if resumeErr != nil {
					a.logf("resume interrupted music failed: %v", resumeErr)
					a.setError(resumeErr)
				} else if resumed {
					a.logf("native music retained after conversation; no URL reload or seek")
					finalState = "MUSIC"
				}
			}
			a.setState(finalState)
		}
	}()
}

func (a *Agent) interruptCurrentAnswer() {
	cfg, _ := a.store.Snapshot()
	_, _ = runCommand("killall", "miplayer")
	if !a.music.HasResumablePlayback() {
		if _, err := runCommand(cfg.PlayerCommand, "stop"); err != nil {
			a.logf("barge-in player stop failed: %v", err)
		}
	}
	a.logf("barge-in stopped current answer")
}

func (a *Agent) runNativeVoiceSession(ctx context.Context, first *NativeSpeechTurn, followups <-chan *NativeSpeechTurn, resumeMusic bool) error {
	cfg, _ := a.store.Snapshot()
	maxDuration := time.Duration(cfg.SessionMaxSec) * time.Second
	if maxDuration <= 0 {
		maxDuration = 120 * time.Second
	}
	sessionCtx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()
	turnLimit := cfg.FollowupMaxTurns
	if turnLimit < 1 {
		turnLimit = 1
	}
	turn := first
	emptyRetryUsed := false
	for turnIndex := 0; turnIndex < turnLimit; turnIndex++ {
		cfg, secrets := a.store.Snapshot()
		var text string
		for {
			state := "LISTENING"
			waitSec := cfg.InitialSpeechTimeoutSec
			if turnIndex > 0 {
				state = "FOLLOWUP_WAIT"
				waitSec = cfg.FollowupTimeoutSec
			}
			if err := a.setStateChecked(state); err != nil {
				_ = turn.StopCapture()
				_ = turn.Finish(false)
				return fmt.Errorf("listening LED privacy guard: %w", err)
			}
			a.mu.Lock()
			previousReply := a.status.LastResponse
			a.mu.Unlock()
			var err error
			text, err = (ASRClient{}).TranscribePCM(sessionCtx, cfg, secrets, previousReply, waitSec, turn.Audio, turn.StopCapture)
			if err == nil {
				break
			}
			if !isNativeEmptyAudioError(err) {
				_ = turn.Finish(false)
				return err
			}
			if turnIndex > 0 || emptyRetryUsed {
				a.logf("native capture ended without speech; closing session turn=%d", turn.ID)
				_ = turn.Finish(false)
				return nil
			}

			emptyRetryUsed = true
			a.logf("native initial capture was empty; prompting and retrying once turn=%d", turn.ID)
			if finishErr := turn.Finish(true); finishErr != nil {
				a.logf("finish empty native turn failed: %v", finishErr)
			}
			a.setState("RESPONDING")
			if speakErr := (TTSClient{}).Speak(sessionCtx, cfg, secrets, "我没听清，请再说一遍"); speakErr != nil {
				a.logf("empty capture prompt failed: %v", speakErr)
			}
			time.Sleep(100 * time.Millisecond)
			next, waitErr := a.requestNextNativeTurn(sessionCtx, followups, cfg.FollowupTimeoutSec)
			if waitErr != nil {
				return waitErr
			}
			if next == nil {
				return nil
			}
			turn = next
		}
		text = strings.TrimSpace(text)
		if text == "" {
			_ = turn.Finish(false)
			return nil
		}
		a.mu.Lock()
		a.status.LastTranscript = text
		a.status.UpdatedAt = time.Now()
		a.mu.Unlock()
		a.logf("recognized: %s", text)
		if isExitPhrase(text, cfg.ExitPhrases) {
			_ = turn.Finish(false)
			return nil
		}
		answer, local, err := a.routeText(sessionCtx, text, true)
		if err != nil {
			_ = turn.Finish(false)
			return err
		}
		if answer != "" {
			a.mu.Lock()
			a.status.LastResponse = answer
			a.status.UpdatedAt = time.Now()
			a.mu.Unlock()
		}
		if local || resumeMusic || !cfg.FollowupEnabled || turnIndex+1 >= turnLimit {
			if err := turn.Finish(false); err != nil {
				return err
			}
			return nil
		}
		if err := turn.Finish(true); err != nil {
			return err
		}
		time.Sleep(100 * time.Millisecond)
		next, err := a.requestNextNativeTurn(sessionCtx, followups, cfg.FollowupTimeoutSec)
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		turn = next
	}
	return nil
}

func isNativeEmptyAudioError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "audio source ended before producing PCM") ||
		strings.Contains(message, "ASR audio ended without transcript") ||
		strings.Contains(message, "ASR ended without transcript")
}

func (a *Agent) requestNextNativeTurn(ctx context.Context, followups <-chan *NativeSpeechTurn, timeoutSec int) (*NativeSpeechTurn, error) {
	a.mu.Lock()
	native := a.native
	a.mu.Unlock()
	if native == nil {
		return nil, errors.New("native speech server stopped")
	}
	if err := native.RequestCapture(); err != nil {
		return nil, fmt.Errorf("request follow-up capture: %w", err)
	}
	if timeoutSec < 1 {
		timeoutSec = 10
	}
	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case next := <-followups:
		if next == nil {
			return nil, errors.New("native speech follow-up channel closed")
		}
		return next, nil
	case <-timer.C:
		return nil, nil
	}
}

func (a *Agent) ProcessText(ctx context.Context, text string) (string, error) {
	answer, _, err := a.routeText(ctx, strings.TrimSpace(text), false)
	return answer, err
}

func (a *Agent) runVoiceSession(ctx context.Context) error {
	cfg, _ := a.store.Snapshot()
	maxDuration := time.Duration(cfg.SessionMaxSec) * time.Second
	if maxDuration <= 0 {
		maxDuration = 120 * time.Second
	}
	sessionCtx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()
	turns := cfg.FollowupMaxTurns
	if turns < 1 {
		turns = 1
	}
	for turn := 0; turn < turns; turn++ {
		cfg, secrets := a.store.Snapshot()
		state := "LISTENING"
		waitSec := cfg.InitialSpeechTimeoutSec
		if turn > 0 {
			state = "FOLLOWUP_WAIT"
			waitSec = cfg.FollowupTimeoutSec
		}
		if err := a.setStateChecked(state); err != nil {
			return fmt.Errorf("listening LED privacy guard: %w", err)
		}
		a.mu.Lock()
		previousReply := a.status.LastResponse
		a.mu.Unlock()
		text, err := (ASRClient{}).Transcribe(sessionCtx, cfg, secrets, previousReply, waitSec)
		if err != nil {
			return err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		a.mu.Lock()
		a.status.LastTranscript = text
		a.status.UpdatedAt = time.Now()
		a.mu.Unlock()
		a.logf("recognized: %s", text)
		if isExitPhrase(text, cfg.ExitPhrases) {
			return nil
		}
		answer, local, err := a.routeText(sessionCtx, text, true)
		if err != nil {
			return err
		}
		if answer != "" {
			a.mu.Lock()
			a.status.LastResponse = answer
			a.status.UpdatedAt = time.Now()
			a.mu.Unlock()
		}
		if local {
			return nil
		}
		if !cfg.FollowupEnabled {
			return nil
		}
	}
	return nil
}

func (a *Agent) routeText(ctx context.Context, text string, fromVoice bool) (string, bool, error) {
	if text == "" {
		return "", false, errors.New("empty text")
	}
	cfg, secrets := a.store.Snapshot()
	if intent, ok := ParseMusicIntent(text); ok {
		return a.handleMusicIntent(ctx, cfg, secrets, intent, fromVoice)
	}
	a.setState("THINKING")
	a.mu.Lock()
	history := append([]ChatMessage(nil), a.history...)
	a.mu.Unlock()
	llmCfg := cfg
	llmCfg.LLMSystemPrompt = strings.TrimSpace(cfg.LLMSystemPrompt + "\n" + a.music.ContextDescription() + "\n需要搜索、推荐或播放音乐时必须调用 play_music；不要只口头表示会播放。")
	result, err := (LLMClient{}).Chat(ctx, llmCfg, secrets, history, text)
	if err != nil {
		return "", false, err
	}
	if result.EndConversation {
		a.logf("LLM tool=end_conversation")
		a.setState("IDLE")
		return "", true, nil
	}
	if len(result.ToolCalls) > 0 {
		intent, toolErr := musicIntentFromTool(result.ToolCalls[0])
		if toolErr != nil {
			return "", false, toolErr
		}
		a.logf("LLM tool=%s", result.ToolCalls[0].Name)
		return a.handleMusicIntent(ctx, cfg, secrets, intent, fromVoice)
	}
	answer := result.Text
	a.mu.Lock()
	a.history = append(a.history, ChatMessage{Role: "user", Content: text}, ChatMessage{Role: "assistant", Content: answer})
	if cfg.LLMMaxHistory > 0 && len(a.history) > cfg.LLMMaxHistory {
		a.history = append([]ChatMessage(nil), a.history[len(a.history)-cfg.LLMMaxHistory:]...)
	}
	a.status.LastResponse = answer
	a.status.UpdatedAt = time.Now()
	a.mu.Unlock()
	a.logf("LLM response chars=%d", len([]rune(answer)))
	if fromVoice {
		a.setState("SPEAKING")
		if err := (TTSClient{}).Speak(ctx, cfg, secrets, answer); err != nil {
			return answer, false, err
		}
	}
	return answer, false, nil
}

func (a *Agent) handleMusicIntent(ctx context.Context, cfg Config, secrets Secrets, intent MusicIntent, fromVoice bool) (string, bool, error) {
	a.setState("MUSIC")
	answer, err := a.music.Handle(ctx, cfg, secrets, intent)
	if err != nil {
		a.logf("music action=%s failed: %v", intent.Action, err)
		a.setError(err)
		if !fromVoice {
			return "", true, err
		}
		friendly := "音乐播放没有成功，我已经尝试了网易云、QQ音乐和酷我，请稍后再试"
		if intent.Action != "play" && intent.Action != "next" && intent.Action != "prev" {
			friendly = "音乐控制没有成功，请稍后再试"
		}
		a.setState("SPEAKING")
		if speakErr := (TTSClient{}).Speak(ctx, cfg, secrets, friendly); speakErr != nil {
			return "", true, errors.Join(err, speakErr)
		}
		return friendly, true, nil
	}
	a.setError(nil)
	a.mu.Lock()
	a.status.LastResponse = answer
	a.status.UpdatedAt = time.Now()
	a.mu.Unlock()
	a.logf("music action=%s result=%s", intent.Action, answer)
	if fromVoice && intent.Action == "current" {
		a.setState("SPEAKING")
		if err := (TTSClient{}).Speak(ctx, cfg, secrets, answer); err != nil {
			return answer, true, err
		}
	}
	return answer, true, nil
}

func musicIntentFromTool(call LLMToolRequest) (MusicIntent, error) {
	switch call.Name {
	case "play_music":
		var args struct {
			Query   string `json:"query"`
			Artist  string `json:"artist"`
			Title   string `json:"title"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return MusicIntent{}, fmt.Errorf("decode play_music arguments: %w", err)
		}
		args.Query = strings.TrimSpace(args.Query)
		args.Artist = strings.TrimSpace(args.Artist)
		args.Title = strings.TrimSpace(args.Title)
		args.Version = strings.TrimSpace(args.Version)
		if args.Artist != "" && args.Title != "" {
			title := args.Title
			if args.Version != "" && args.Version != "原版" && args.Version != "原唱" {
				title += " " + args.Version
			}
			args.Query = args.Artist + "的" + title
		} else if args.Artist != "" {
			args.Query = args.Artist + "的歌"
		} else if args.Title != "" {
			args.Query = args.Title
		}
		if args.Query == "" {
			return MusicIntent{}, errors.New("play_music query is empty")
		}
		return MusicIntent{Action: "play", Query: args.Query}, nil
	case "music_control":
		var args struct {
			Action string `json:"action"`
			Volume int    `json:"volume"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return MusicIntent{}, fmt.Errorf("decode music_control arguments: %w", err)
		}
		switch args.Action {
		case "pause", "resume", "next", "prev", "stop":
			return MusicIntent{Action: args.Action}, nil
		case "volume":
			if args.Volume < 0 || args.Volume > 100 {
				return MusicIntent{}, errors.New("music_control volume must be between 0 and 100")
			}
			return MusicIntent{Action: "volume", Volume: args.Volume}, nil
		default:
			return MusicIntent{}, fmt.Errorf("unsupported music_control action %q", args.Action)
		}
	case "get_current_music":
		return MusicIntent{Action: "current"}, nil
	default:
		return MusicIntent{}, fmt.Errorf("unsupported LLM tool %q", call.Name)
	}
}

func isExitPhrase(text string, phrases []string) bool {
	t := normalizeExitPhrase(text)
	for _, p := range phrases {
		if t == normalizeExitPhrase(p) {
			return true
		}
	}
	return false
}

func normalizeExitPhrase(text string) string {
	replacer := strings.NewReplacer(
		"，", "", "。", "", "！", "", "？", "",
		",", "", ".", "", "!", "", "?", "", " ", "",
	)
	text = replacer.Replace(strings.TrimSpace(text))
	text = strings.TrimPrefix(text, "小爱同学")
	for {
		before := text
		for _, prefix := range []string{"好了", "好啦", "行了", "请", "你", "可以"} {
			text = strings.TrimPrefix(text, prefix)
		}
		if text == before {
			break
		}
	}
	for {
		before := text
		for _, suffix := range []string{"谢谢你", "谢谢", "可以了", "吧", "了", "啦", "啊", "呀", "哦"} {
			text = strings.TrimSuffix(text, suffix)
		}
		if text == before {
			return text
		}
	}
}
