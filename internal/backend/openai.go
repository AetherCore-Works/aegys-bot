package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI connects to any OpenAI-compatible Chat Completions endpoint (OpenAI, OpenRouter, Groq, vLLM, LM Studio).
type OpenAI struct {
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	Client       *http.Client
}

func NewOpenAI(baseURL, apiKey, model, systemPrompt string) *OpenAI {
	return &OpenAI{
		BaseURL:      strings.TrimRight(baseURL, "/"),
		APIKey:       apiKey,
		Model:        model,
		SystemPrompt: systemPrompt,
		Client:       &http.Client{Timeout: 90 * time.Second},
	}
}

func (o *OpenAI) Name() string { return "openai:" + o.Model }

func (o *OpenAI) Ask(ctx context.Context, req AskRequest) (AskResponse, error) {
	messages := []map[string]string{}
	if o.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": o.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})

	raw, _ := json.Marshal(map[string]any{
		"model":    o.Model,
		"messages": messages,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return AskResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.APIKey)
	}

	resp, err := o.Client.Do(httpReq)
	if err != nil {
		return AskResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return AskResponse{}, fmt.Errorf("openai-compatible error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return AskResponse{}, err
	}
	if len(parsed.Choices) == 0 {
		return AskResponse{}, fmt.Errorf("empty choices returned from model %s", o.Model)
	}
	return AskResponse{
		Reply: strings.TrimSpace(parsed.Choices[0].Message.Content),
		Model: o.Model,
	}, nil
}

func (o *OpenAI) Health(ctx context.Context) (string, error) {
	return fmt.Sprintf("openai-compatible configured (%s, model=%s)", o.BaseURL, o.Model), nil
}
