package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AetherCore-Works/aegys-bot/internal/format"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

// Update models the Telegram Bot API update payload (messages + inline keyboard callbacks).
type Update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Text string `json:"text"`
	} `json:"message"`
	CallbackQuery *struct {
		ID   string `json:"id"`
		From struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Data    string `json:"data"`
		Message *struct {
			Chat struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// Adapter connects Aegys to the Telegram Bot API via long-polling or webhooks.
type Adapter struct {
	Token   string
	BaseURL string
	Router  *router.Router
	Client  *http.Client
}

func NewAdapter(token string, r *router.Router) *Adapter {
	return &Adapter{
		Token:   token,
		BaseURL: "https://api.telegram.org",
		Router:  r,
		Client:  &http.Client{Timeout: 35 * time.Second},
	}
}

func (a *Adapter) Platform() string { return "telegram" }

// Send delivers a formatted outbound notification to a Telegram chat ID.
func (a *Adapter) Send(ctx context.Context, recipient, text string) error {
	return a.sendWithButtons(ctx, recipient, text, nil)
}

func (a *Adapter) sendWithButtons(ctx context.Context, chatID, text string, buttons []router.ActionButton) error {
	if a.Token == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN not configured")
	}
	formatted := format.ForChannel("telegram", text)
	chunks := format.SplitChunks(formatted, 4000)
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(a.BaseURL, "/"), a.Token)

	for i, chunk := range chunks {
		payload := map[string]any{
			"chat_id":    chatID,
			"text":       chunk,
			"parse_mode": "Markdown",
		}
		if i == len(chunks)-1 && len(buttons) > 0 {
			rows := make([][]map[string]string, 0, len(buttons))
			for _, btn := range buttons {
				rows = append(rows, []map[string]string{{
					"text":          btn.Label,
					"callback_data": btn.Command,
				}})
			}
			payload["reply_markup"] = map[string]any{"inline_keyboard": rows}
		}

		status, errBody, err := a.postSendMessage(ctx, endpoint, payload)
		if err != nil {
			return err
		}
		if status == http.StatusBadRequest && payload["parse_mode"] != nil {
			// Fallback to plain text if Telegram rejects unescaped Markdown entities (common in LLM code/identifiers).
			delete(payload, "parse_mode")
			status, errBody, err = a.postSendMessage(ctx, endpoint, payload)
			if err != nil {
				return err
			}
		}
		if status >= 300 {
			return fmt.Errorf("telegram sendMessage failed (%d): %s", status, errBody)
		}
	}
	return nil
}

func (a *Adapter) postSendMessage(ctx context.Context, endpoint string, payload map[string]any) (int, string, error) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.Client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, strings.TrimSpace(string(errBody)), nil
	}
	return resp.StatusCode, "", nil
}

func (a *Adapter) answerCallback(ctx context.Context, callbackID string) {
	if callbackID == "" || a.Token == "" {
		return
	}
	endpoint := fmt.Sprintf("%s/bot%s/answerCallbackQuery", strings.TrimRight(a.BaseURL, "/"), a.Token)
	raw, _ := json.Marshal(map[string]string{"callback_query_id": callbackID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if resp, err := a.Client.Do(req); err == nil {
		resp.Body.Close()
	}
}

// HandleUpdate processes a single Telegram Update (from webhook or long-polling).
func (a *Adapter) HandleUpdate(ctx context.Context, u Update) error {
	var msg router.InboundMessage
	switch {
	case u.Message != nil && strings.TrimSpace(u.Message.Text) != "":
		msg = router.InboundMessage{
			Platform: "telegram",
			UserID:   strconv.FormatInt(u.Message.From.ID, 10),
			ChatID:   strconv.FormatInt(u.Message.Chat.ID, 10),
			Text:     strings.TrimSpace(u.Message.Text),
			IsDM:     u.Message.Chat.Type == "private",
		}
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil && strings.TrimSpace(u.CallbackQuery.Data) != "":
		a.answerCallback(ctx, u.CallbackQuery.ID)
		msg = router.InboundMessage{
			Platform: "telegram",
			UserID:   strconv.FormatInt(u.CallbackQuery.From.ID, 10),
			ChatID:   strconv.FormatInt(u.CallbackQuery.Message.Chat.ID, 10),
			Text:     strings.TrimSpace(u.CallbackQuery.Data),
			IsDM:     u.CallbackQuery.Message.Chat.Type == "private",
		}
	default:
		return nil
	}

	res := a.Router.Handle(ctx, msg)
	if !res.Allowed {
		return nil
	}
	return a.sendWithButtons(ctx, msg.ChatID, res.Text, res.Buttons)
}

// StartPolling runs a resilient Telegram getUpdates loop until ctx is cancelled.
func (a *Adapter) StartPolling(ctx context.Context) {
	var offset int64
	log.Println("[telegram] starting long-polling loop")
	for {
		if ctx.Err() != nil {
			return
		}
		endpoint := fmt.Sprintf("%s/bot%s/getUpdates?timeout=25&offset=%d", strings.TrimRight(a.BaseURL, "/"), a.Token, offset)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return
		}
		resp, err := a.Client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[telegram] polling error: %v", err)
			time.Sleep(3 * time.Second)
			continue
		}

		var body struct {
			OK     bool     `json:"ok"`
			Result []Update `json:"result"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()

		for _, u := range body.Result {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			go func(upd Update) {
				if err := a.HandleUpdate(ctx, upd); err != nil {
					log.Printf("[telegram] handle update error: %v", err)
				}
			}(u)
		}
	}
}
