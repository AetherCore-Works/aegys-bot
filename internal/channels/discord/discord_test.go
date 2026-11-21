package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscordSendDM(t *testing.T) {
	var sentContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot test-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/users/@me/channels":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "dm-chan-1"})
		case "/channels/dm-chan-1/messages":
			var body struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			sentContent = body.Content
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	adapter := NewAdapter("test-tok", "app-1", "", nil)
	adapter.APIBase = srv.URL

	if err := adapter.Send(context.Background(), "111111111111111111", "Aegys alert online"); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if sentContent != "Aegys alert online" {
		t.Fatalf("expected delivered content, got %q", sentContent)
	}
}
