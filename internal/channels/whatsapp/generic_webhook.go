package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AetherCore-Works/aegys-bot/internal/format"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

// GenericWebhookProvider lets contributors plug in WhatsApp Cloud API proxies, WPPConnect, or custom bridges.
type GenericWebhookProvider struct {
	SendURL   string
	AuthToken string
	Client    *http.Client
}

func NewGenericWebhookProvider(sendURL, authToken string) *GenericWebhookProvider {
	return &GenericWebhookProvider{
		SendURL:   strings.TrimRight(sendURL, "/"),
		AuthToken: authToken,
		Client:    &http.Client{Timeout: 20 * time.Second},
	}
}

func (g *GenericWebhookProvider) DriverName() string { return "generic-webhook" }

func (g *GenericWebhookProvider) ParseWebhook(body []byte) (router.InboundMessage, bool, error) {
	var payload struct {
		From string `json:"from"`
		Chat string `json:"chat_id"`
		Text string `json:"text"`
		IsDM *bool  `json:"is_dm"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return router.InboundMessage{}, false, err
	}
	if strings.TrimSpace(payload.From) == "" || strings.TrimSpace(payload.Text) == "" {
		return router.InboundMessage{}, false, nil
	}
	chat := strings.TrimSpace(payload.Chat)
	if chat == "" {
		chat = strings.TrimSpace(payload.From)
	}
	isDM := true
	if payload.IsDM != nil {
		isDM = *payload.IsDM
	}
	return router.InboundMessage{
		Platform: "whatsapp",
		UserID:   strings.TrimSpace(payload.From),
		ChatID:   chat,
		Text:     strings.TrimSpace(payload.Text),
		IsDM:     isDM,
	}, true, nil
}

func (g *GenericWebhookProvider) SendText(ctx context.Context, recipient, text string) error {
	if g.SendURL == "" {
		return fmt.Errorf("WHATSAPP_GENERIC_SEND_URL not configured")
	}
	formatted := format.ForChannel("whatsapp", text)
	for _, chunk := range format.SplitChunks(formatted, 3500) {
		raw, _ := json.Marshal(map[string]string{
			"to":   recipient,
			"text": chunk,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.SendURL, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if g.AuthToken != "" {
			req.Header.Set("Authorization", "Bearer "+g.AuthToken)
		}
		resp, err := g.Client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return fmt.Errorf("generic whatsapp send failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}
		resp.Body.Close()
	}
	return nil
}
