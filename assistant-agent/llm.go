package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMClient struct{}

type LLMResult struct {
	Text            string
	EndConversation bool
	ToolCalls       []LLMToolRequest
}

type LLMToolRequest struct {
	Name      string
	Arguments string
}

type llmToolCall struct {
	Index    int `json:"index"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (LLMClient) Chat(ctx context.Context, cfg Config, secrets Secrets, history []ChatMessage, input string) (LLMResult, error) {
	if strings.TrimSpace(cfg.LLMBaseURL) == "" || strings.TrimSpace(cfg.LLMModel) == "" {
		return LLMResult{}, errors.New("LLM base URL and model are not configured")
	}
	if strings.TrimSpace(secrets.LLMAPIKey) == "" {
		return LLMResult{}, errors.New("LLM_API_KEY is not configured")
	}
	messages := make([]ChatMessage, 0, len(history)+2)
	if cfg.LLMSystemPrompt != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: cfg.LLMSystemPrompt})
	}
	if cfg.LLMMaxHistory > 0 && len(history) > cfg.LLMMaxHistory {
		history = history[len(history)-cfg.LLMMaxHistory:]
	}
	messages = append(messages, history...)
	messages = append(messages, ChatMessage{Role: "user", Content: input})
	tools := []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":        "end_conversation",
			"description": "当用户明确表示结束、退出、让助手退下、休息、停止继续聆听或今天聊到这里时调用。调用后立即结束当前连续对话，不要再生成普通回答。",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
	}, {
		"type": "function",
		"function": map[string]any{
			"name":        "play_music",
			"description": "当用户要求搜索、推荐或播放歌曲、歌手、风格、场景音乐时调用。尽量把明确提到的歌手和歌名分别填入 artist、title；工具会验证真实候选并聚合尝试 QQ音乐、网易云和酷我。不要只口头答应播放。",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":   map[string]any{"type": "string", "description": "完整搜索意图；仍需填写"},
					"artist":  map[string]any{"type": "string", "description": "用户明确指定的原唱或歌手；没有则留空"},
					"title":   map[string]any{"type": "string", "description": "用户明确指定的歌曲名；没有则留空"},
					"version": map[string]any{"type": "string", "description": "例如原版、现场版、伴奏；没有则留空"},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
	}, {
		"type": "function",
		"function": map[string]any{
			"name":        "music_control",
			"description": "控制当前音乐。适用于暂停、继续、上一首、下一首、停止和调节音量。",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{"type": "string", "enum": []string{"pause", "resume", "next", "prev", "stop", "volume"}},
					"volume": map[string]any{"type": "integer", "minimum": 0, "maximum": 100, "description": "action 为 volume 时的目标音量"},
				},
				"required":             []string{"action"},
				"additionalProperties": false,
			},
		},
	}, {
		"type": "function",
		"function": map[string]any{
			"name":        "get_current_music",
			"description": "查询当前或刚才由本助手播放的歌曲、歌手、平台和播放状态。",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
		},
	}}
	payload := map[string]any{"model": cfg.LLMModel, "messages": messages, "stream": true, "temperature": 0.6, "tools": tools, "tool_choice": "auto"}
	body, _ := json.Marshal(payload)
	timeout := time.Duration(cfg.LLMTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, joinEndpoint(cfg.LLMBaseURL, "/chat/completions"), bytes.NewReader(body))
	if err != nil {
		return LLMResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+secrets.LLMAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	resp, err := (&http.Client{Timeout: timeout + 5*time.Second}).Do(req)
	if err != nil {
		return LLMResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return LLMResult{}, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(limited)))
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		var decoded struct {
			Choices []struct {
				Message struct {
					Content   string        `json:"content"`
					ToolCalls []llmToolCall `json:"tool_calls"`
				} `json:"message"`
				Text string `json:"text"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			return LLMResult{}, err
		}
		if len(decoded.Choices) == 0 {
			return LLMResult{}, errors.New("LLM returned no choices")
		}
		toolRequests, endConversation := decodeToolRequests(decoded.Choices[0].Message.ToolCalls)
		if endConversation {
			return LLMResult{EndConversation: true}, nil
		}
		if len(toolRequests) > 0 {
			return LLMResult{ToolCalls: toolRequests}, nil
		}
		if decoded.Choices[0].Message.Content != "" {
			return LLMResult{Text: strings.TrimSpace(decoded.Choices[0].Message.Content)}, nil
		}
		return LLMResult{Text: strings.TrimSpace(decoded.Choices[0].Text)}, nil
	}
	var answer strings.Builder
	toolRequests := map[int]LLMToolRequest{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var event struct {
			Choices []struct {
				Delta struct {
					Content   string        `json:"content"`
					ToolCalls []llmToolCall `json:"tool_calls"`
				} `json:"delta"`
				Message struct {
					Content   string        `json:"content"`
					ToolCalls []llmToolCall `json:"tool_calls"`
				} `json:"message"`
				Text string `json:"text"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &event) != nil || len(event.Choices) == 0 {
			continue
		}
		part := event.Choices[0].Delta.Content
		for _, call := range event.Choices[0].Delta.ToolCalls {
			request := toolRequests[call.Index]
			request.Name += call.Function.Name
			request.Arguments += call.Function.Arguments
			toolRequests[call.Index] = request
		}
		for _, call := range event.Choices[0].Message.ToolCalls {
			request := toolRequests[call.Index]
			if request.Name == "" {
				request.Name = call.Function.Name
			}
			if request.Arguments == "" {
				request.Arguments = call.Function.Arguments
			}
			toolRequests[call.Index] = request
		}
		if part == "" {
			part = event.Choices[0].Message.Content
		}
		if part == "" {
			part = event.Choices[0].Text
		}
		answer.WriteString(part)
	}
	if err := scanner.Err(); err != nil {
		return LLMResult{}, err
	}
	orderedTools := make([]LLMToolRequest, 0, len(toolRequests))
	for index := 0; index < len(toolRequests); index++ {
		request, ok := toolRequests[index]
		if !ok {
			continue
		}
		if request.Name == "end_conversation" {
			return LLMResult{EndConversation: true}, nil
		}
		if request.Name != "" {
			orderedTools = append(orderedTools, request)
		}
	}
	if len(orderedTools) > 0 {
		return LLMResult{ToolCalls: orderedTools}, nil
	}
	result := strings.TrimSpace(answer.String())
	if result == "" {
		return LLMResult{}, errors.New("LLM returned an empty response")
	}
	return LLMResult{Text: result}, nil
}

func hasEndConversationTool(calls []llmToolCall) bool {
	for _, call := range calls {
		if call.Function.Name == "end_conversation" {
			return true
		}
	}
	return false
}

func decodeToolRequests(calls []llmToolCall) ([]LLMToolRequest, bool) {
	requests := make([]LLMToolRequest, 0, len(calls))
	for _, call := range calls {
		if call.Function.Name == "end_conversation" {
			return nil, true
		}
		if call.Function.Name != "" {
			requests = append(requests, LLMToolRequest{Name: call.Function.Name, Arguments: call.Function.Arguments})
		}
	}
	return requests, false
}
