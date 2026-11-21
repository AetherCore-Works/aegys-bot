package discord

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

type longReplyBackend struct{ text string }

func (l longReplyBackend) Name() string { return "stub" }
func (l longReplyBackend) Health(context.Context) (string, error) {
	return "ok", nil
}
func (l longReplyBackend) Ask(context.Context, backend.AskRequest) (backend.AskResponse, error) {
	return backend.AskResponse{Reply: l.text}, nil
}

func TestDiscordInteractionDeferredAndChunkedFollowup(t *testing.T) {
	var sequence []string
	var deferredType float64
	var editedContent string
	var followupContents []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sequence = append(sequence, r.Method+" "+r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/interactions/int-1/tok-1/callback":
			deferredType, _ = body["type"].(float64)
		case r.Method == http.MethodPatch && r.URL.Path == "/webhooks/app-1/tok-1/messages/@original":
			editedContent, _ = body["content"].(string)
		case r.Method == http.MethodPost && r.URL.Path == "/webhooks/app-1/tok-1":
			c, _ := body["content"].(string)
			followupContents = append(followupContents, c)
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	longText := strings.Repeat("A", 1800) + "\n\n" + strings.Repeat("B", 500)
	guard := rbac.New([]string{"discord:u-admin"}, nil, true)
	rt := router.New("Aegys", guard, longReplyBackend{text: longText}, nil)
	adapter := NewAdapter("test-tok", "app-1", "", rt)
	adapter.APIBase = srv.URL

	raw := json.RawMessage(`{"id":"int-1","application_id":"app-1","token":"tok-1","channel":{"id":"c-1"},"user":{"id":"u-admin"},"data":{"name":"ask","options":[{"name":"prompt","value":"hello"}]}}`)
	adapter.handleInteractionCreate(context.Background(), raw)

	if deferredType != 5 {
		t.Fatalf("expected deferred ACK type=5, got %v (seq=%v)", deferredType, sequence)
	}
	if len(sequence) < 3 || sequence[0] != "POST /interactions/int-1/tok-1/callback" || sequence[1] != "PATCH /webhooks/app-1/tok-1/messages/@original" {
		t.Fatalf("unexpected interaction call sequence: %v", sequence)
	}
	if !strings.Contains(editedContent, "AAA") || len(followupContents) != 1 || !strings.Contains(followupContents[0], "BBB") {
		t.Fatalf("expected chunked original+followup, got editLen=%d followups=%d", len(editedContent), len(followupContents))
	}
}

func TestDiscordInteractionUnauthorizedEphemeral(t *testing.T) {
	var gotType float64
	var gotFlags float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotType, _ = body["type"].(float64)
		if data, ok := body["data"].(map[string]any); ok {
			gotFlags, _ = data["flags"].(float64)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	guard := rbac.New([]string{"discord:u-admin"}, nil, true)
	rt := router.New("Aegys", guard, backend.NewMock(), nil)
	adapter := NewAdapter("test-tok", "app-1", "", rt)
	adapter.APIBase = srv.URL

	raw := json.RawMessage(`{"id":"int-2","application_id":"app-1","token":"tok-2","channel":{"id":"c-1"},"user":{"id":"intruder"},"data":{"name":"ping"}}`)
	adapter.handleInteractionCreate(context.Background(), raw)

	if gotType != 4 || gotFlags != 64 {
		t.Fatalf("expected ephemeral rejection (type=4, flags=64), got type=%v flags=%v", gotType, gotFlags)
	}
}
