package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type ASRClient struct{}

type asrRequest struct {
	User struct {
		UID string `json:"uid"`
	} `json:"user"`
	Audio struct {
		Format  string `json:"format"`
		Codec   string `json:"codec"`
		Rate    int    `json:"rate"`
		Bits    int    `json:"bits"`
		Channel int    `json:"channel"`
	} `json:"audio"`
	Request struct {
		ModelName       string         `json:"model_name"`
		EnableNonstream bool           `json:"enable_nonstream"`
		EnableITN       bool           `json:"enable_itn"`
		EnablePUNC      bool           `json:"enable_punc"`
		EnableDDC       bool           `json:"enable_ddc"`
		EnableLID       bool           `json:"enable_lid"`
		ShowUtterances  bool           `json:"show_utterances"`
		ResultType      string         `json:"result_type"`
		EndWindowMS     int            `json:"end_window_size"`
		ForceSpeechMS   int            `json:"force_to_speech_time"`
		EnableMusicFC   bool           `json:"enable_music_fc"`
		Corpus          map[string]any `json:"corpus,omitempty"`
	} `json:"request"`
}

type asrAudioInput struct {
	reader io.Reader
	stop   func()
	detail func() string
}

type asrAudioStarter func(context.Context) (*asrAudioInput, error)

func (ASRClient) Transcribe(ctx context.Context, cfg Config, secrets Secrets, contextText string, waitForSpeechSec int) (string, error) {
	return (ASRClient{}).transcribe(ctx, cfg, secrets, contextText, waitForSpeechSec, func(captureCtx context.Context) (*asrAudioInput, error) {
		cmd := exec.CommandContext(captureCtx, cfg.CaptureProgram, cfg.CaptureArgs...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		var captureErr bytes.Buffer
		cmd.Stderr = &captureErr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start capture: %w", err)
		}
		var stopOnce sync.Once
		return &asrAudioInput{
			reader: stdout,
			stop: func() {
				stopOnce.Do(func() {
					if cmd.Process != nil {
						_ = cmd.Process.Kill()
					}
					_ = cmd.Wait()
				})
			},
			detail: func() string { return strings.TrimSpace(captureErr.String()) },
		}, nil
	})
}

func (ASRClient) TranscribePCM(ctx context.Context, cfg Config, secrets Secrets, contextText string, waitForSpeechSec int, pcm <-chan []byte, stopCapture func() error) (string, error) {
	return (ASRClient{}).transcribe(ctx, cfg, secrets, contextText, waitForSpeechSec, func(captureCtx context.Context) (*asrAudioInput, error) {
		reader, writer := io.Pipe()
		pumpCtx, cancelPump := context.WithCancel(captureCtx)
		go func() {
			defer writer.Close()
			for {
				select {
				case <-pumpCtx.Done():
					return
				case chunk, ok := <-pcm:
					if !ok {
						return
					}
					if len(chunk) == 0 {
						continue
					}
					if _, err := writer.Write(chunk); err != nil {
						return
					}
				}
			}
		}()
		var stopOnce sync.Once
		var stopErr error
		return &asrAudioInput{
			reader: reader,
			stop: func() {
				stopOnce.Do(func() {
					cancelPump()
					if stopCapture != nil {
						stopErr = stopCapture()
					}
					_ = reader.Close()
					_ = writer.Close()
				})
			},
			detail: func() string {
				if stopErr != nil {
					return "speech.usock stop: " + stopErr.Error()
				}
				return "speech.usock PCM"
			},
		}, nil
	})
}

func (ASRClient) transcribe(ctx context.Context, cfg Config, secrets Secrets, contextText string, waitForSpeechSec int, startAudio asrAudioStarter) (string, error) {
	if strings.TrimSpace(secrets.ASRAPIKey) == "" {
		return "", errors.New("ASR_API_KEY is not configured")
	}
	if waitForSpeechSec <= 0 {
		waitForSpeechSec = cfg.InitialSpeechTimeoutSec
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.MaxUtteranceSec)*time.Second)
	defer cancel()

	header := http.Header{}
	header.Set("X-Api-Key", secrets.ASRAPIKey)
	header.Set("X-Api-Resource-Id", cfg.ASRResourceID)
	header.Set("X-Api-Request-Id", requestID())
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second, Proxy: http.ProxyFromEnvironment}
	conn, resp, err := dialer.DialContext(requestCtx, cfg.ASRURL, header)
	if err != nil {
		if resp != nil {
			return "", fmt.Errorf("ASR websocket HTTP %d: %w", resp.StatusCode, err)
		}
		return "", fmt.Errorf("ASR websocket: %w", err)
	}
	defer conn.Close()

	payload := asrRequest{}
	payload.User.UID = "l06a"
	payload.Audio.Format = "pcm"
	payload.Audio.Codec = "raw"
	payload.Audio.Rate = cfg.AudioRate
	payload.Audio.Bits = cfg.AudioBits
	payload.Audio.Channel = cfg.AudioChannels
	payload.Request.ModelName = "bigmodel"
	payload.Request.EnableNonstream = true
	payload.Request.EnableITN = true
	payload.Request.EnablePUNC = true
	payload.Request.EnableDDC = true
	payload.Request.EnableLID = true
	payload.Request.ShowUtterances = true
	payload.Request.ResultType = "full"
	payload.Request.EndWindowMS = cfg.EndWindowMS
	payload.Request.ForceSpeechMS = cfg.ForceSpeechMS
	payload.Request.EnableMusicFC = true
	if strings.TrimSpace(contextText) != "" {
		payload.Request.Corpus = map[string]any{"context": jsonString(map[string]any{
			"context_type": "dialog_ctx",
			"context_data": []map[string]string{{"speaker": "bot", "text": contextText}},
		})}
	}
	first, err := buildFullRequest(1, payload)
	if err != nil {
		return "", err
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, first); err != nil {
		return "", err
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, ack, err := conn.ReadMessage()
	if err != nil {
		return "", fmt.Errorf("ASR init response: %w", err)
	}
	parsedAck, err := parseASRResponse(ack)
	if err != nil {
		return "", fmt.Errorf("ASR init parse: %w", err)
	}
	if parsedAck.Code != 0 {
		return "", fmt.Errorf("ASR init code %d: %s", parsedAck.Code, parsedAck.Payload.Error)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Duration(cfg.MaxUtteranceSec+10) * time.Second))

	audio, err := startAudio(requestCtx)
	if err != nil {
		return "", err
	}
	defer audio.stop()

	stopAudio := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(stopAudio) }) }
	sendErr := make(chan error, 1)
	chunkBytes := cfg.AudioRate * (cfg.AudioBits / 8) * cfg.AudioChannels * cfg.AudioChunkMS / 1000
	if chunkBytes <= 0 {
		chunkBytes = 6400
	}
	go func() {
		seq := int32(2)
		buf := make([]byte, chunkBytes)
		totalBytes := 0
		for {
			select {
			case <-stopAudio:
				last, e := buildAudioRequest(seq, nil, true)
				if e == nil {
					e = conn.WriteMessage(websocket.BinaryMessage, last)
				}
				sendErr <- e
				return
			default:
			}
			n, readErr := io.ReadFull(audio.reader, buf)
			if n > 0 {
				totalBytes += n
				msg, e := buildAudioRequest(seq, buf[:n], false)
				if e == nil {
					e = conn.WriteMessage(websocket.BinaryMessage, msg)
				}
				if e != nil {
					sendErr <- e
					return
				}
				seq++
			}
			if readErr != nil {
				last, e := buildAudioRequest(seq, nil, true)
				if e == nil {
					e = conn.WriteMessage(websocket.BinaryMessage, last)
				}
				if readErr != io.EOF && readErr != io.ErrUnexpectedEOF && e == nil {
					e = readErr
				}
				if totalBytes == 0 && e == nil {
					e = errors.New("audio source ended before producing PCM")
				}
				sendErr <- e
				return
			}
		}
	}()

	initialTimer := time.NewTimer(time.Duration(waitForSpeechSec) * time.Second)
	defer initialTimer.Stop()
	initialC := initialTimer.C
	type readResult struct {
		msg []byte
		err error
	}
	readCh := make(chan readResult, 1)
	go func() {
		for {
			_, msg, e := conn.ReadMessage()
			readCh <- readResult{msg, e}
			if e != nil {
				return
			}
		}
	}()
	var latest string
	var final string
	for {
		select {
		case <-requestCtx.Done():
			stop()
			audio.stop()
			if latest != "" {
				return strings.TrimSpace(latest), nil
			}
			return "", requestCtx.Err()
		case <-initialC:
			if latest == "" {
				stop()
				audio.stop()
				return "", nil
			}
			initialC = nil
		case rr := <-readCh:
			if rr.err != nil {
				stop()
				audio.stop()
				if final != "" || latest != "" {
					if final != "" {
						return strings.TrimSpace(final), nil
					}
					return strings.TrimSpace(latest), nil
				}
				return "", fmt.Errorf("ASR receive: %w", rr.err)
			}
			res, e := parseASRResponse(rr.msg)
			if e != nil {
				stop()
				audio.stop()
				return "", e
			}
			if res.Code != 0 {
				stop()
				audio.stop()
				return "", fmt.Errorf("ASR code %d: %s", res.Code, res.Payload.Error)
			}
			if text := strings.TrimSpace(res.Payload.Result.Text); text != "" {
				latest = text
			}
			for _, utterance := range res.Payload.Result.Utterances {
				if utterance.Definite && strings.TrimSpace(utterance.Text) != "" {
					final = strings.TrimSpace(utterance.Text)
					stop()
				}
			}
			if res.Last {
				audio.stop()
				if final != "" {
					return final, nil
				}
				if latest != "" {
					return strings.TrimSpace(latest), nil
				}
				return "", fmt.Errorf("ASR ended without transcript; audio=%s", audio.detail())
			}
		case e := <-sendErr:
			audio.stop()
			if e != nil && final == "" && latest == "" {
				return "", fmt.Errorf("ASR audio: %w; source: %s", e, audio.detail())
			}
			if final != "" {
				return final, nil
			}
			if latest != "" {
				return strings.TrimSpace(latest), nil
			}
			return "", fmt.Errorf("ASR audio ended without transcript; source: %s", audio.detail())
		}
	}
}
