package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type TTSClient struct{}

func (TTSClient) Speak(ctx context.Context, cfg Config, secrets Secrets, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if cfg.TTSEngine == "native" || cfg.TTSEngine == "" {
		if _, err := runCommandContext(ctx, cfg.NativeTTSCommand, text); err == nil {
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		} else if cfg.TTSBaseURL == "" {
			return fmt.Errorf("native TTS failed: %w", err)
		}
	}
	if cfg.TTSBaseURL == "" || secrets.TTSAPIKey == "" {
		return errors.New("remote TTS is not configured")
	}
	payload := map[string]any{"model": cfg.TTSModel, "voice": cfg.TTSVoice, "input": text, "response_format": "mp3"}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinEndpoint(cfg.TTSBaseURL, "/audio/speech"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secrets.TTSAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("TTS HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(limited)))
	}
	path := "/tmp/assistant-tts.mp3"
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	_, err = runCommand(cfg.PlayerCommand, "file", path)
	return err
}
