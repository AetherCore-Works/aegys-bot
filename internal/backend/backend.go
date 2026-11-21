package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/AetherCore-Works/aegys-bot/internal/config"
)

// AskRequest represents an AI-Path prompt dispatched from a channel.
type AskRequest struct {
	Platform       string
	UserID         string
	ChatID         string
	ConversationID string
	Prompt         string
}

// AskResponse is returned by an AIBackend after completing a prompt.
type AskResponse struct {
	Reply          string
	ConversationID string
	Model          string
}

// TaskInfo describes an automation/scheduled job exposed by an ops backend.
type TaskInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
}

// AIBackend abstracts the execution engine behind Aegys (AGY Gateway, Ollama, OpenAI, or Mock).
type AIBackend interface {
	Name() string
	Ask(ctx context.Context, req AskRequest) (AskResponse, error)
	Health(ctx context.Context) (string, error)
}

// OpsBackend is an optional extension implemented by backends supporting remote host operations.
type OpsBackend interface {
	ListTasks(ctx context.Context) ([]TaskInfo, error)
	RunTask(ctx context.Context, taskID string) (string, error)
	WakeHost(ctx context.Context) (string, error)
}

// NewFromConfig instantiates the configured AI backend.
func NewFromConfig(cfg config.Settings) (AIBackend, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.AIBackend)) {
	case "", "mock":
		return NewMock(), nil
	case "agy-gateway", "agy":
		return NewAgyGateway(cfg.AgyGatewayURL, cfg.AgyGatewaySecret), nil
	case "ollama":
		return NewOllama(cfg.OllamaURL, cfg.OllamaModel, cfg.AISystemPrompt), nil
	case "openai":
		return NewOpenAI(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.AISystemPrompt), nil
	default:
		return nil, fmt.Errorf("unsupported AI_BACKEND %q (supported: mock, agy-gateway, ollama, openai)", cfg.AIBackend)
	}
}
