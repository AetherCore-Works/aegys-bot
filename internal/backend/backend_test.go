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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/api/webhook/prompt":
			_ = json.NewEncoder(w).Encode(map[string]any{"reply": "Gateway OK", "conv_id": "c-1", "model": "qwen3:8b"})
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
	if err != nil || res.Reply != "Gateway OK" {
		t.Fatalf("unexpected Ask result: res=%+v err=%v", res, err)
	}
	tasks, err := gw.ListTasks(context.Background())
	if err != nil || len(tasks) != 1 || tasks[0].ID != "t1" {
		t.Fatalf("unexpected ListTasks: tasks=%+v err=%v", tasks, err)
	}
	if _, err := gw.WakeHost(context.Background()); err != nil {
		t.Fatalf("WakeHost failed: %v", err)
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
