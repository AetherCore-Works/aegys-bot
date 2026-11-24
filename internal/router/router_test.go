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

type fakeEvoBackend struct {
	backend.AIBackend
	lastRating     string
	lastCorrection string
	lastApproved   *bool
}

func (f *fakeEvoBackend) SubmitFeedback(_ context.Context, _ string, rating, correction string) (string, error) {
	f.lastRating = rating
	f.lastCorrection = correction
	if correction != "" {
		return "user_correction", nil
	}
	return "feedback_" + rating, nil
}

func (f *fakeEvoBackend) EvolutionReport(_ context.Context) (backend.EvolutionReportSummary, error) {
	return backend.EvolutionReportSummary{
		Enabled:      true,
		PendingCount: 1,
		Proposals: []backend.EvolutionProposalSummary{
			{ID: "prop-7", Kind: "prompt_rule", Title: "Always verify builds", Status: "pending"},
		},
	}, nil
}

func (f *fakeEvoBackend) DecideProposal(_ context.Context, id string, approve bool) (string, error) {
	f.lastApproved = &approve
	if approve {
		return "Proposal `" + id + "` approved.", nil
	}
	return "Proposal `" + id + "` rejected.", nil
}

func TestRouterEvolutionAndFeedback(t *testing.T) {
	guard := rbac.New([]string{"mock:admin"}, []string{"mock:user"}, true)
	evo := &fakeEvoBackend{AIBackend: backend.NewMock()}
	r := New("Aegys", guard, evo, nil)
	ctx := context.Background()

	fbUp := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "user", ChatID: "c1", Text: "/feedback 👍", IsDM: true})
	if fbUp.Path != "fast" || !strings.Contains(fbUp.Text, "feedback_up") || evo.lastRating != "up" {
		t.Fatalf("unexpected /feedback up: %+v", fbUp)
	}

	fbNote := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "user", ChatID: "c1", Text: "/feedback use go test -race", IsDM: true})
	if fbNote.Path != "fast" || !strings.Contains(fbNote.Text, "user_correction") || evo.lastCorrection != "use go test -race" {
		t.Fatalf("unexpected /feedback note: %+v", fbNote)
	}

	deniedEvo := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "user", ChatID: "c1", Text: "/evolution", IsDM: true})
	if deniedEvo.Path != "denied" {
		t.Fatalf("expected /evolution denied for standard user, got %+v", deniedEvo)
	}

	listEvo := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "admin", ChatID: "c2", Text: "/evolution", IsDM: true})
	if listEvo.Path != "fast" || !strings.Contains(listEvo.Text, "prop-7") {
		t.Fatalf("unexpected /evolution list: %+v", listEvo)
	}

	appEvo := r.Handle(ctx, InboundMessage{Platform: "mock", UserID: "admin", ChatID: "c2", Text: "/evolution approve prop-7", IsDM: true})
	if appEvo.Path != "fast" || !strings.Contains(appEvo.Text, "approved") || evo.lastApproved == nil || !*evo.lastApproved {
		t.Fatalf("unexpected /evolution approve: %+v", appEvo)
	}
}
