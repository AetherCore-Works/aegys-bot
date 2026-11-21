package whatsapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvolutionProviderParseAndSend(t *testing.T) {
	var gotPath, gotNumber, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			Number string `json:"number"`
			Text   string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotNumber = body.Number
		gotText = body.Text
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	p := NewEvolutionProvider(srv.URL, "aegys", "test-key")
	msg, ok, err := p.ParseWebhook([]byte(`{"data":{"key":{"remoteJid":"5500999990000@s.whatsapp.net","fromMe":false},"message":{"conversation":"/ping"}}}`))
	if err != nil || !ok || !msg.IsDM || msg.UserID != "5500999990000" {
		t.Fatalf("unexpected parse result: msg=%+v ok=%v err=%v", msg, ok, err)
	}

	if err := p.SendText(context.Background(), "5500999990000", "## OK\n**Aegys** ready"); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}
	if gotPath != "/message/sendText/aegys" || gotNumber != "5500999990000" || gotText != "*OK*\n*Aegys* ready" {
		t.Fatalf("unexpected send payload: path=%q number=%q text=%q", gotPath, gotNumber, gotText)
	}
}
