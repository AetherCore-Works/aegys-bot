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

// EvolutionProvider implements WhatsApp messaging via Evolution API v2.
type EvolutionProvider struct {
	BaseURL  string
	Instance string
	APIKey   string
	Client   *http.Client
}

func NewEvolutionProvider(baseURL, instance, apiKey string) *EvolutionProvider {
	if strings.TrimSpace(instance) == "" {
		instance = "aegys"
	}
	return &EvolutionProvider{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Instance: instance,
		APIKey:   apiKey,
		Client:   &http.Client{Timeout: 20 * time.Second},
	}
}

func (e *EvolutionProvider) DriverName() string { return "evolution-v2" }

func (e *EvolutionProvider) ParseWebhook(body []byte) (router.InboundMessage, bool, error) {
	var payload struct {
		Data struct {
			Key struct {
				RemoteJID string `json:"remoteJid"`
				FromMe    bool   `json:"fromMe"`
			} `json:"key"`
			Message struct {
				Conversation        string `json:"conversation"`
				ExtendedTextMessage struct {
					Text string `json:"text"`
				} `json:"extendedTextMessage"`
			} `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return router.InboundMessage{}, false, err
	}
	if payload.Data.Key.FromMe || payload.Data.Key.RemoteJID == "" {
		return router.InboundMessage{}, false, nil
	}

	text := strings.TrimSpace(payload.Data.Message.Conversation)
	if text == "" {
		text = strings.TrimSpace(payload.Data.Message.ExtendedTextMessage.Text)
	}
	if text == "" {
		return router.InboundMessage{}, false, nil
	}

	jid := strings.TrimSpace(payload.Data.Key.RemoteJID)
	isDM := strings.HasSuffix(jid, "@s.whatsapp.net")
	number := jid
	if before, _, ok := strings.Cut(jid, "@"); ok {
		number = before
	}

	return router.InboundMessage{
		Platform: "whatsapp",
		UserID:   number,
		ChatID:   number,
		Text:     text,
		IsDM:     isDM,
	}, true, nil
}

func (e *EvolutionProvider) SendText(ctx context.Context, recipient, text string) error {
	if e.BaseURL == "" || e.Instance == "" {
		return fmt.Errorf("evolution api url or instance not configured")
	}
	endpoint := fmt.Sprintf("%s/message/sendText/%s", e.BaseURL, e.Instance)
	formatted := format.ForChannel("whatsapp", text)
	for _, chunk := range format.SplitChunks(formatted, 3500) {
		raw, _ := json.Marshal(map[string]any{
			"number": recipient,
			"text":   chunk,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if e.APIKey != "" {
			req.Header.Set("apikey", e.APIKey)
		}
		resp, err := e.Client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return fmt.Errorf("evolution sendText failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}
		resp.Body.Close()
	}
	return nil
}
