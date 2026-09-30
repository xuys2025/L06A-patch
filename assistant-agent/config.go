package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultConfigPath = "/etc/assistant/config.default.json"
	dataConfigPath    = "/data/assistant/config.json"
	secretsPath       = "/data/assistant/secrets.env"
	adminTokenPath    = "/data/assistant/admin.token"
)

type Config struct {
	ListenAddr              string   `json:"listen_addr"`
	ASRURL                  string   `json:"asr_url"`
	ASRResourceID           string   `json:"asr_resource_id"`
	CaptureProgram          string   `json:"capture_program"`
	CaptureArgs             []string `json:"capture_args"`
	AudioRate               int      `json:"audio_rate"`
	AudioBits               int      `json:"audio_bits"`
	AudioChannels           int      `json:"audio_channels"`
	AudioChunkMS            int      `json:"audio_chunk_ms"`
	InitialSpeechTimeoutSec int      `json:"initial_speech_timeout_sec"`
	MaxUtteranceSec         int      `json:"max_utterance_sec"`
	EndWindowMS             int      `json:"end_window_ms"`
	ForceSpeechMS           int      `json:"force_speech_ms"`
	LLMBaseURL              string   `json:"llm_base_url"`
	LLMModel                string   `json:"llm_model"`
	LLMSystemPrompt         string   `json:"llm_system_prompt"`
	LLMTimeoutSec           int      `json:"llm_timeout_sec"`
	LLMMaxHistory           int      `json:"llm_max_history"`
	TTSEngine               string   `json:"tts_engine"`
	TTSBaseURL              string   `json:"tts_base_url"`
	TTSModel                string   `json:"tts_model"`
	TTSVoice                string   `json:"tts_voice"`
	MusicSearchURL          string   `json:"music_search_url"`
	MusicAPIBase            string   `json:"music_api_base"`
	MusicQuality            string   `json:"music_quality"`
	MusicQualityFallback    []string `json:"music_quality_fallback"`
	FollowupEnabled         bool     `json:"followup_enabled"`
	FollowupTimeoutSec      int      `json:"followup_timeout_sec"`
	FollowupMaxTurns        int      `json:"followup_max_turns"`
	SessionMaxSec           int      `json:"session_max_sec"`
	ExitPhrases             []string `json:"exit_phrases"`
	NativeTTSCommand        string   `json:"native_tts_command"`
	PlayerCommand           string   `json:"player_command"`
	VolumeCommand           string   `json:"volume_command"`
}

type Secrets struct {
	ASRAPIKey   string
	LLMAPIKey   string
	MusicAPIKey string
	TTSAPIKey   string
}

type ConfigStore struct {
	mu      sync.RWMutex
	config  Config
	secrets Secrets
}

func defaultConfig() Config {
	return Config{
		ListenAddr:              ":8090",
		ASRURL:                  "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async",
		ASRResourceID:           "volc.seedasr.sauc.duration",
		CaptureProgram:          "/usr/bin/arecord",
		CaptureArgs:             []string{"-q", "-D", "mico_record", "-t", "raw", "-f", "S16_LE", "-r", "16000", "-c", "1"},
		AudioRate:               16000,
		AudioBits:               16,
		AudioChannels:           1,
		AudioChunkMS:            200,
		InitialSpeechTimeoutSec: 10,
		MaxUtteranceSec:         18,
		EndWindowMS:             900,
		ForceSpeechMS:           1000,
		LLMSystemPrompt:         "你是运行在小米小爱音箱 L06A 上的中文语音助手。回答简洁、口语化，避免 Markdown；需要播放音乐时让本地音乐路由处理。",
		LLMTimeoutSec:           90,
		LLMMaxHistory:           12,
		TTSEngine:               "native",
		TTSModel:                "tts-1",
		TTSVoice:                "alloy",
		MusicSearchURL:          "https://music.163.com/api/search/get/web",
		MusicAPIBase:            "https://source.shiqianjiang.cn/api",
		MusicQuality:            "320k",
		MusicQualityFallback:    []string{"128k"},
		FollowupEnabled:         true,
		FollowupTimeoutSec:      10,
		FollowupMaxTurns:        8,
		SessionMaxSec:           120,
		ExitPhrases:             []string{"退出", "结束对话", "不用了", "再见", "退下"},
		NativeTTSCommand:        "/usr/libexec/assistant/native-tts.sh",
		PlayerCommand:           "/usr/libexec/assistant/playerctl.sh",
		VolumeCommand:           "/usr/libexec/assistant/volume.sh",
	}
}

func NewConfigStore() (*ConfigStore, error) {
	s := &ConfigStore{}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ConfigStore) Reload() error {
	cfg := defaultConfig()
	path := dataConfigPath
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		path = defaultConfigPath
	}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	applyConfigDefaults(&cfg)
	if err := validateConfig(cfg); err != nil {
		return err
	}
	secrets, err := loadSecrets(secretsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.mu.Lock()
	s.config = cfg
	s.secrets = secrets
	s.mu.Unlock()
	return nil
}

func (s *ConfigStore) Snapshot() (Config, Secrets) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.config
	cfg.CaptureArgs = append([]string(nil), cfg.CaptureArgs...)
	cfg.MusicQualityFallback = append([]string(nil), cfg.MusicQualityFallback...)
	cfg.ExitPhrases = append([]string(nil), cfg.ExitPhrases...)
	return cfg, s.secrets
}

func (s *ConfigStore) Save(cfg Config, updates map[string]string) error {
	applyConfigDefaults(&cfg)
	if err := validateConfig(cfg); err != nil {
		return err
	}
	_, current := s.Snapshot()
	if v := strings.TrimSpace(updates["ASR_API_KEY"]); v != "" {
		current.ASRAPIKey = v
	}
	if v := strings.TrimSpace(updates["LLM_API_KEY"]); v != "" {
		current.LLMAPIKey = v
	}
	if v := strings.TrimSpace(updates["MUSIC_API_KEY"]); v != "" {
		current.MusicAPIKey = v
	}
	if v := strings.TrimSpace(updates["TTS_API_KEY"]); v != "" {
		current.TTSAPIKey = v
	}
	if err := os.MkdirAll(filepath.Dir(dataConfigPath), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(dataConfigPath, append(raw, '\n'), 0600); err != nil {
		return err
	}
	if err := saveSecrets(secretsPath, current); err != nil {
		return err
	}
	return s.Reload()
}

func applyConfigDefaults(c *Config) {
	d := defaultConfig()
	if c.ListenAddr == "" {
		c.ListenAddr = d.ListenAddr
	}
	if c.ASRURL == "" {
		c.ASRURL = d.ASRURL
	}
	if c.ASRResourceID == "" {
		c.ASRResourceID = d.ASRResourceID
	}
	if c.CaptureProgram == "" {
		c.CaptureProgram = d.CaptureProgram
	}
	if len(c.CaptureArgs) == 0 {
		c.CaptureArgs = d.CaptureArgs
	}
	if c.AudioRate == 0 {
		c.AudioRate = d.AudioRate
	}
	if c.AudioBits == 0 {
		c.AudioBits = d.AudioBits
	}
	if c.AudioChannels == 0 {
		c.AudioChannels = d.AudioChannels
	}
	if c.AudioChunkMS == 0 {
		c.AudioChunkMS = d.AudioChunkMS
	}
	if c.InitialSpeechTimeoutSec == 0 {
		c.InitialSpeechTimeoutSec = d.InitialSpeechTimeoutSec
	}
	if c.MaxUtteranceSec == 0 {
		c.MaxUtteranceSec = d.MaxUtteranceSec
	}
	if c.EndWindowMS == 0 {
		c.EndWindowMS = d.EndWindowMS
	}
	if c.ForceSpeechMS == 0 {
		c.ForceSpeechMS = d.ForceSpeechMS
	}
	if c.LLMTimeoutSec == 0 {
		c.LLMTimeoutSec = d.LLMTimeoutSec
	}
	if c.LLMMaxHistory == 0 {
		c.LLMMaxHistory = d.LLMMaxHistory
	}
	if c.TTSEngine == "" {
		c.TTSEngine = d.TTSEngine
	}
	if c.TTSModel == "" {
		c.TTSModel = d.TTSModel
	}
	if c.TTSVoice == "" {
		c.TTSVoice = d.TTSVoice
	}
	if c.MusicSearchURL == "" {
		c.MusicSearchURL = d.MusicSearchURL
	}
	if c.MusicAPIBase == "" {
		c.MusicAPIBase = d.MusicAPIBase
	}
	if c.MusicQuality == "" {
		c.MusicQuality = d.MusicQuality
	}
	if len(c.MusicQualityFallback) == 0 {
		c.MusicQualityFallback = d.MusicQualityFallback
	}
	if c.FollowupTimeoutSec == 0 {
		c.FollowupTimeoutSec = d.FollowupTimeoutSec
	}
	if c.FollowupMaxTurns == 0 {
		c.FollowupMaxTurns = d.FollowupMaxTurns
	}
	if c.SessionMaxSec == 0 {
		c.SessionMaxSec = d.SessionMaxSec
	}
	if len(c.ExitPhrases) == 0 {
		c.ExitPhrases = append([]string(nil), d.ExitPhrases...)
	} else {
		seen := make(map[string]bool, len(c.ExitPhrases))
		for _, phrase := range c.ExitPhrases {
			seen[strings.TrimSpace(phrase)] = true
		}
		for _, phrase := range d.ExitPhrases {
			if !seen[phrase] {
				c.ExitPhrases = append(c.ExitPhrases, phrase)
			}
		}
	}
	if c.NativeTTSCommand == "" {
		c.NativeTTSCommand = d.NativeTTSCommand
	}
	if c.PlayerCommand == "" {
		c.PlayerCommand = d.PlayerCommand
	}
	if c.VolumeCommand == "" {
		c.VolumeCommand = d.VolumeCommand
	}
}

func validateConfig(c Config) error {
	if c.AudioRate < 8000 || c.AudioRate > 48000 {
		return errors.New("audio_rate out of range")
	}
	if c.AudioBits != 16 {
		return errors.New("audio_bits must be 16")
	}
	if c.AudioChannels < 1 || c.AudioChannels > 2 {
		return errors.New("audio_channels out of range")
	}
	if c.AudioChunkMS < 40 || c.AudioChunkMS > 1000 {
		return errors.New("audio_chunk_ms out of range")
	}
	if c.InitialSpeechTimeoutSec < 1 || c.InitialSpeechTimeoutSec > 60 {
		return errors.New("initial_speech_timeout_sec out of range")
	}
	if c.EndWindowMS < 300 || c.EndWindowMS > 5000 {
		return errors.New("end_window_ms out of range")
	}
	if c.MaxUtteranceSec < 3 || c.MaxUtteranceSec > 60 {
		return errors.New("max_utterance_sec out of range")
	}
	if c.FollowupTimeoutSec < 1 || c.FollowupTimeoutSec > 60 {
		return errors.New("followup_timeout_sec out of range")
	}
	if c.FollowupMaxTurns < 1 || c.FollowupMaxTurns > 30 {
		return errors.New("followup_max_turns out of range")
	}
	if c.SessionMaxSec < 10 || c.SessionMaxSec > 600 {
		return errors.New("session_max_sec out of range")
	}
	if c.TTSEngine != "native" && c.TTSEngine != "remote" {
		return errors.New("tts_engine must be native or remote")
	}
	return nil
}

func loadSecrets(path string) (Secrets, error) {
	var s Secrets
	f, err := os.Open(path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if decoded, err := strconv.Unquote(value); err == nil {
			value = decoded
		} else {
			value = strings.Trim(value, "\"'")
		}
		switch strings.TrimSpace(key) {
		case "ASR_API_KEY":
			s.ASRAPIKey = value
		case "LLM_API_KEY":
			s.LLMAPIKey = value
		case "MUSIC_API_KEY":
			s.MusicAPIKey = value
		case "TTS_API_KEY":
			s.TTSAPIKey = value
		}
	}
	return s, scanner.Err()
}

func saveSecrets(path string, s Secrets) error {
	quote := func(v string) string { return strconv.Quote(v) }
	body := "ASR_API_KEY=" + quote(s.ASRAPIKey) + "\n" +
		"LLM_API_KEY=" + quote(s.LLMAPIKey) + "\n" +
		"MUSIC_API_KEY=" + quote(s.MusicAPIKey) + "\n" +
		"TTS_API_KEY=" + quote(s.TTSAPIKey) + "\n"
	return atomicWrite(path, []byte(body), 0600)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
