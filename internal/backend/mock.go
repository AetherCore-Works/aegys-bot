package backend

import (
	"context"
	"fmt"
	"strings"
)

// Mock is a zero-dependency local backend for contributors, CLI sandbox mode, and automated tests.
type Mock struct {
	Tasks []TaskInfo
}

func NewMock() *Mock {
	return &Mock{
		Tasks: []TaskInfo{
			{ID: "daily-briefing", Name: "Daily Operations Briefing", Schedule: "0 8 * * *", Enabled: true},
			{ID: "health-watch", Name: "Container Health Watcher", Schedule: "*/15 * * * *", Enabled: true},
		},
	}
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Ask(_ context.Context, req AskRequest) (AskResponse, error) {
	conv := req.ConversationID
	if conv == "" {
		conv = fmt.Sprintf("mock-%s-%s", req.Platform, req.ChatID)
	}
	return AskResponse{
		Reply:          fmt.Sprintf("[Mock AI] Received (%s): %s", req.Platform, strings.TrimSpace(req.Prompt)),
		ConversationID: conv,
		Model:          "mock-v1",
	}, nil
}

func (m *Mock) Health(_ context.Context) (string, error) {
	return "mock backend healthy (local dev mode)", nil
}

func (m *Mock) ListTasks(_ context.Context) ([]TaskInfo, error) {
	return m.Tasks, nil
}

func (m *Mock) RunTask(_ context.Context, taskID string) (string, error) {
	for _, t := range m.Tasks {
		if t.ID == taskID {
			return fmt.Sprintf("[Mock Ops] Executed task `%s` (%s).", t.ID, t.Name), nil
		}
	}
	return "", fmt.Errorf("task %q not found", taskID)
}

func (m *Mock) WakeHost(_ context.Context) (string, error) {
	return "[Mock Ops] Wake-on-LAN packet simulated.", nil
}
