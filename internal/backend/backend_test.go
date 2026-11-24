package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgyGatewayBackend(t *testing.T) {
	var seenPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/api/webhook/prompt":
			_ = json.NewDecoder(r.Body).Decode(&seenPayload)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":              true,
				"reply":           "Gateway OK",
				"conversation_id": "c-1",
				"model":           "qwen3:8b",
				"outbox_files":    []string{"report.pdf"},
			})
		case "/api/automations":
			_ = json.NewEncoder(w).Encode(map[string]any{"automations": []map[string]any{{"id": "t1", "name": "Backup", "cron_expr": "0 3 * * *", "enabled": true}}})
		case "/api/wol":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gw := NewAgyGateway(srv.URL, "tok-1")
	res, err := gw.Ask(context.Background(), AskRequest{Prompt: "hello"})
	if err != nil || res.Reply != "Gateway OK" || res.ConversationID != "c-1" || len(res.OutboxFiles) != 1 || res.OutboxFiles[0] != "report.pdf" {
		t.Fatalf("unexpected Ask result: res=%+v err=%v", res, err)
	}
	if _, hasConv := seenPayload["conversation_id"]; hasConv {
		t.Fatalf("expected empty conversation_id to be omitted, got %+v", seenPayload)
	}
	if _, hasLegacy := seenPayload["conv_id"]; hasLegacy {
		t.Fatalf("expected legacy conv_id not to be sent, got %+v", seenPayload)
	}

	// Subsequent turn must forward conversation_id
	_, err = gw.Ask(context.Background(), AskRequest{Prompt: "follow-up", ConversationID: "c-1"})
	if err != nil {
		t.Fatalf("second Ask failed: %v", err)
	}
	if seenPayload["conversation_id"] != "c-1" {
		t.Fatalf("expected conversation_id=c-1, got %+v", seenPayload)
	}

	tasks, err := gw.ListTasks(context.Background())
	if err != nil || len(tasks) != 1 || tasks[0].ID != "t1" {
		t.Fatalf("unexpected ListTasks: tasks=%+v err=%v", tasks, err)
	}
	if _, err := gw.WakeHost(context.Background()); err != nil {
		t.Fatalf("WakeHost failed: %v", err)
	}
}

func TestAgyGatewayAsync202EventStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/webhook/prompt":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"job_id":  "job-42",
				"status":  "running",
				"pending": true,
			})
		case "/api/jobs/job-42/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(": ping\n\n"))
			_, _ = w.Write([]byte("id: 1\ndata: {\"type\":\"delta\",\"text\":\"Hello \"}\n\n"))
			_, _ = w.Write([]byte("id: 2\ndata: {\"type\":\"done\",\"response\":\"Hello async world\",\"conversation_id\":\"conv-42\",\"outbox_files\":[\"chart.png\"]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gw := NewAgyGateway(srv.URL, "tok-2")
	res, err := gw.Ask(context.Background(), AskRequest{Prompt: "long task"})
	if err != nil {
		t.Fatalf("async Ask failed: %v", err)
	}
	if res.Reply != "Hello async world" || res.ConversationID != "conv-42" || len(res.OutboxFiles) != 1 || res.OutboxFiles[0] != "chart.png" {
		t.Fatalf("unexpected async Ask result: %+v", res)
	}
}

func TestAgyGatewayJobError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     false,
			"status": "error",
			"error":  "upstream model timeout",
		})
	}))
	defer srv.Close()

	gw := NewAgyGateway(srv.URL, "")
	if _, err := gw.Ask(context.Background(), AskRequest{Prompt: "fail"}); err == nil || !strings.Contains(err.Error(), "upstream model timeout") {
		t.Fatalf("expected error containing upstream model timeout, got %v", err)
	}
}

func TestOllamaAndOpenAIBackends(t *testing.T) {
	ollamaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": "Ollama reply"}})
	}))
	defer ollamaSrv.Close()

	ol := NewOllama(ollamaSrv.URL, "qwen3:8b", "sys")
	res, err := ol.Ask(context.Background(), AskRequest{Prompt: "hi"})
	if err != nil || !strings.Contains(res.Reply, "Ollama reply") {
		t.Fatalf("ollama failed: res=%+v err=%v", res, err)
	}

	oaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": "OpenAI reply"}}}})
	}))
	defer oaSrv.Close()

	oa := NewOpenAI(oaSrv.URL, "key", "gpt-4o-mini", "sys")
	res, err = oa.Ask(context.Background(), AskRequest{Prompt: "hi"})
	if err != nil || res.Reply != "OpenAI reply" {
		t.Fatalf("openai failed: res=%+v err=%v", res, err)
	}
}

func TestAgyGatewayEvolutionEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/feedback":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["rating"] != "up" || body["conversation_id"] != "conv-1" {
				t.Fatalf("unexpected feedback payload: %+v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "kind": "feedback_up"})
		case "/api/evolution/report":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"enabled": true,
				"proposals": []map[string]any{
					{"id": "prop-1", "kind": "prompt_rule", "title": "Prefer concise diffs", "status": "pending"},
					{"id": "prop-2", "kind": "slash_command", "title": "Old", "status": "approved"},
				},
			})
		case "/api/evolution/proposals/prop-1/approve":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/api/evolution/proposals/prop-1/reject":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	gw := NewAgyGateway(srv.URL, "tok")
	kind, err := gw.SubmitFeedback(context.Background(), "conv-1", "up", "")
	if err != nil || kind != "feedback_up" {
		t.Fatalf("SubmitFeedback failed: kind=%q err=%v", kind, err)
	}
	rep, err := gw.EvolutionReport(context.Background())
	if err != nil || !rep.Enabled || rep.PendingCount != 1 || len(rep.Proposals) != 2 {
		t.Fatalf("EvolutionReport failed: rep=%+v err=%v", rep, err)
	}
	msg, err := gw.DecideProposal(context.Background(), "prop-1", true)
	if err != nil || !strings.Contains(msg, "approved") {
		t.Fatalf("DecideProposal approve failed: msg=%q err=%v", msg, err)
	}
	msg, err = gw.DecideProposal(context.Background(), "prop-1", false)
	if err != nil || !strings.Contains(msg, "rejected") {
		t.Fatalf("DecideProposal reject failed: msg=%q err=%v", msg, err)
	}
}
