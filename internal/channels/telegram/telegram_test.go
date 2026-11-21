package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AetherCore-Works/aegys-bot/internal/backend"
	"github.com/AetherCore-Works/aegys-bot/internal/rbac"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

func TestTelegramHandleUpdateWithInlineButtons(t *testing.T) {
	var gotText string
	var hasKeyboard bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotText, _ = body["text"].(string)
		_, hasKeyboard = body["reply_markup"]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	guard := rbac.New([]string{"telegram:222222222"}, nil, true)
	rt := router.New("Aegys", guard, backend.NewMock(), nil)
	adapter := NewAdapter("fake-token", rt)
	adapter.BaseURL = srv.URL

	var u Update
	_ = json.Unmarshal([]byte(`{"update_id":1,"message":{"chat":{"id":222222222,"type":"private"},"from":{"id":222222222},"text":"/tasks"}}`), &u)

	if err := adapter.HandleUpdate(context.Background(), u); err != nil {
		t.Fatalf("HandleUpdate failed: %v", err)
	}
	if !strings.Contains(gotText, "daily-briefing") || !hasKeyboard {
		t.Fatalf("expected inline keyboard with tasks, got text=%q hasKeyboard=%v", gotText, hasKeyboard)
	}
}

func TestTelegramMarkdownFallbackOn400(t *testing.T) {
	attempts := 0
	var lastParseMode any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		lastParseMode = body["parse_mode"]
		if body["parse_mode"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	adapter := NewAdapter("fake-token", nil)
	adapter.BaseURL = srv.URL

	if err := adapter.Send(context.Background(), "222222222", "Broken _markdown_ [unclosed"); err != nil {
		t.Fatalf("expected plain-text fallback to succeed, got %v", err)
	}
	if attempts != 2 || lastParseMode != nil {
		t.Fatalf("expected 2 attempts with plain-text fallback, got attempts=%d lastParseMode=%v", attempts, lastParseMode)
	}
}
