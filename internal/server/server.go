package server

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/AetherCore-Works/aegys-bot/internal/channels"
	"github.com/AetherCore-Works/aegys-bot/internal/channels/telegram"
	"github.com/AetherCore-Works/aegys-bot/internal/channels/whatsapp"
	"github.com/AetherCore-Works/aegys-bot/internal/config"
	"github.com/AetherCore-Works/aegys-bot/internal/format"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

// Server exposes health, inbound webhooks, outbound notification API, and dev simulator endpoints.
type Server struct {
	Cfg      config.Settings
	Router   *router.Router
	Registry *channels.Registry
	WhatsApp *whatsapp.Adapter
	Telegram *telegram.Adapter
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"bot":        s.Cfg.BotName,
			"ai_backend": s.Cfg.AIBackend,
			"channels":   s.Registry.EnabledPlatforms(),
		})
	})

	// Outbound Notification API (Tier: NotifyOnly — sends alerts to any recipient without granting inbound access)
	mux.HandleFunc("POST /api/v1/notify", func(w http.ResponseWriter, r *http.Request) {
		if !verifyBearer(r.Header.Get("Authorization"), s.Cfg.APISecret) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or missing bearer token"})
			return
		}
		var body struct {
			Platform  string `json:"platform"`
			Recipient string `json:"recipient"`
			Text      string `json:"text"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if strings.TrimSpace(body.Platform) == "" || strings.TrimSpace(body.Recipient) == "" || strings.TrimSpace(body.Text) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "platform, recipient, and text are required"})
			return
		}
		if err := s.Registry.Send(r.Context(), body.Platform, body.Recipient, body.Text); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// Inbound WhatsApp webhook (Evolution API v2 or generic provider)
	mux.HandleFunc("POST /webhooks/whatsapp", func(w http.ResponseWriter, r *http.Request) {
		if !s.Cfg.EnableWhatsApp || s.WhatsApp == nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "whatsapp channel is disabled"})
			return
		}
		if s.Cfg.WhatsAppWebhookSecret != "" {
			got := r.Header.Get("X-Webhook-Secret")
			if got == "" {
				got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			}
			if !secureEquals(got, s.Cfg.WhatsAppWebhookSecret) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid webhook secret"})
				return
			}
		}

		raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
			return
		}
		msg, ok, err := s.WhatsApp.Provider.ParseWebhook(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid webhook payload"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": true})
			return
		}

		res := s.Router.Handle(r.Context(), msg)
		if !res.Allowed {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ignored": true})
			return
		}
		if err := s.WhatsApp.Send(r.Context(), msg.ChatID, res.Text); err != nil {
			log.Printf("[whatsapp] send reply failed: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": res.Path})
	})

	// Inbound Telegram webhook (when TELEGRAM_MODE=webhook)
	mux.HandleFunc("POST /webhooks/telegram", func(w http.ResponseWriter, r *http.Request) {
		if !s.Cfg.EnableTelegram || s.Telegram == nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "telegram channel is disabled"})
			return
		}
		if s.Cfg.TelegramWebhookSecret != "" {
			if !secureEquals(r.Header.Get("X-Telegram-Bot-Api-Secret-Token"), s.Cfg.TelegramWebhookSecret) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid telegram secret token"})
				return
			}
		}
		var upd telegram.Update
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&upd); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid telegram update"})
			return
		}
		if err := s.Telegram.HandleUpdate(r.Context(), upd); err != nil {
			log.Printf("[telegram] webhook error: %v", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// Local Developer Simulator (active only when ENABLE_MOCK=true)
	mux.HandleFunc("POST /api/v1/dev/simulate", func(w http.ResponseWriter, r *http.Request) {
		if !s.Cfg.EnableMock {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "dev simulator disabled (set ENABLE_MOCK=true)"})
			return
		}
		var req struct {
			Platform string `json:"platform"`
			UserID   string `json:"user_id"`
			ChatID   string `json:"chat_id"`
			Text     string `json:"text"`
			IsDM     *bool  `json:"is_dm"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if req.Platform == "" {
			req.Platform = "mock"
		}
		if req.ChatID == "" {
			req.ChatID = "dev-chat"
		}
		isDM := true
		if req.IsDM != nil {
			isDM = *req.IsDM
		}

		res := s.Router.Handle(r.Context(), router.InboundMessage{
			Platform: req.Platform,
			UserID:   req.UserID,
			ChatID:   req.ChatID,
			Text:     req.Text,
			IsDM:     isDM,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed":   res.Allowed,
			"path":      res.Path,
			"role":      res.Role,
			"raw_text":  res.Text,
			"formatted": format.ForChannel(req.Platform, res.Text),
			"buttons":   res.Buttons,
		})
	})

	return mux
}

func verifyBearer(authHeader, secret string) bool {
	if strings.TrimSpace(secret) == "" {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return false
	}
	return secureEquals(strings.TrimSpace(strings.TrimPrefix(authHeader, prefix)), strings.TrimSpace(secret))
}

func secureEquals(a, b string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
