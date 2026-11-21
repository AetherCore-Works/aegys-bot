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

// Ollama provides a standalone local-LLM backend using Ollama's /api/chat endpoint.
type Ollama struct {
	BaseURL      string
	Model        string
	SystemPrompt string
	Client       *http.Client
}

func NewOllama(baseURL, model, systemPrompt string) *Ollama {
	return &Ollama{
		BaseURL:      strings.TrimRight(baseURL, "/"),
		Model:        model,
		SystemPrompt: systemPrompt,
		Client:       &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *Ollama) Name() string { return "ollama:" + o.Model }

func (o *Ollama) Ask(ctx context.Context, req AskRequest) (AskResponse, error) {
	messages := []map[string]string{}
	if o.SystemPrompt != "" {
		messages = append(messages, map[string]string{"role": "system", "content": o.SystemPrompt})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})

	raw, _ := json.Marshal(map[string]any{
		"model":    o.Model,
		"messages": messages,
		"stream":   false,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.BaseURL+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return AskResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.Client.Do(httpReq)
	if err != nil {
		return AskResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return AskResponse{}, fmt.Errorf("ollama error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return AskResponse{}, err
	}
	return AskResponse{
		Reply: strings.TrimSpace(parsed.Message.Content),
		Model: o.Model,
	}, nil
}

func (o *Ollama) Health(ctx context.Context) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.BaseURL+"/api/tags", nil)
	if err != nil {
		return "", err
	}
	resp, err := o.Client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ollama tags returned %d", resp.StatusCode)
	}
	return fmt.Sprintf("ollama reachable (%s, model=%s)", o.BaseURL, o.Model), nil
}
