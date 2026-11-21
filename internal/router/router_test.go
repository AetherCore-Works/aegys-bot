package router

import (
	"context"
	"strings"
	"testing"

	"github.com/AetherCore-Works/aegys-bot/internal/backend"
	"github.com/AetherCore-Works/aegys-bot/internal/rbac"
)

func TestHybridRouterRBACAndPaths(t *testing.T) {
	guard := rbac.New([]string{"mock:admin"}, []string{"mock:user"}, true)
	custom := []CustomCommand{
		{Name: "/briefing", Description: "Daily briefing", PromptTemplate: "Brief: {{input}}", AdminOnly: true},
		{Name: "/summarize", Description: "Summarize", PromptTemplate: "Sum: {{input}}", AdminOnly: false},
	}
	r := New("Aegys", guard, backend.NewMock(), custom)
	ctx := context.Background()

	// Denied unknown user
	denied := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "intruder", ChatID: "c1", Text: "/ping", IsDM: true})
	if denied.Allowed || denied.Path != "denied" {
		t.Fatalf("expected denied response, got %+v", denied)
	}

	// Standard user can run /ping and /summarize, but NOT /tasks or /briefing
	ping := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "user", ChatID: "c1", Text: "/ping", IsDM: true})
	if !ping.Allowed || ping.Path != "fast" || !strings.Contains(ping.Text, "online") {
		t.Fatalf("unexpected /ping response: %+v", ping)
	}

	tasksUser := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "user", ChatID: "c1", Text: "/tasks", IsDM: true})
	if tasksUser.Path != "denied" || !strings.Contains(tasksUser.Text, "admin") {
		t.Fatalf("expected admin-only block for /tasks, got %+v", tasksUser)
	}

	sumUser := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "user", ChatID: "c1", Text: "/summarize hello world", IsDM: true})
	if sumUser.Path != "ai" || !strings.Contains(sumUser.Text, "Sum: hello world") {
		t.Fatalf("unexpected custom command response: %+v", sumUser)
	}

	// Admin can run /tasks (with action buttons) and /run
	tasksAdmin := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "admin", ChatID: "c2", Text: "/tasks", IsDM: true})
	if tasksAdmin.Path != "fast" || len(tasksAdmin.Buttons) == 0 {
		t.Fatalf("expected admin /tasks with buttons, got %+v", tasksAdmin)
	}

	runAdmin := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "admin", ChatID: "c2", Text: "/run daily-briefing", IsDM: true})
	if runAdmin.Path != "fast" || !strings.Contains(runAdmin.Text, "daily-briefing") {
		t.Fatalf("unexpected /run response: %+v", runAdmin)
	}
}
