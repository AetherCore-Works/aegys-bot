package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/AetherCore-Works/aegys-bot/internal/format"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

// Adapter connects Aegys to Discord via REST API v10 and Gateway WebSocket v10.
type Adapter struct {
	Token      string
	AppID      string
	DevGuildID string
	APIBase    string
	GatewayURL string
	Router     *router.Router
	Client     *http.Client
}

func NewAdapter(token, appID, devGuildID string, r *router.Router) *Adapter {
	return &Adapter{
		Token:      token,
		AppID:      appID,
		DevGuildID: devGuildID,
		APIBase:    "https://discord.com/api/v10",
		GatewayURL: "wss://gateway.discord.gg/?v=10&encoding=json",
		Router:     r,
		Client:     &http.Client{Timeout: 20 * time.Second},
	}
}

func (a *Adapter) Platform() string { return "discord" }

// Send opens (or reuses) a Discord DM channel with recipient (user ID) and delivers formatted chunks.
func (a *Adapter) Send(ctx context.Context, recipient, text string) error {
	if a.Token == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN not configured")
	}
	chBody, _ := json.Marshal(map[string]string{"recipient_id": recipient})
	chReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.APIBase+"/users/@me/channels", bytes.NewReader(chBody))
	if err != nil {
		return err
	}
	chReq.Header.Set("Content-Type", "application/json")
	chReq.Header.Set("Authorization", "Bot "+a.Token)

	chResp, err := a.Client.Do(chReq)
	if err != nil {
		return err
	}
	var ch struct {
		ID string `json:"id"`
	}
	if chResp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(chResp.Body, 512))
		chResp.Body.Close()
		return fmt.Errorf("discord open DM failed (%d): %s", chResp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	_ = json.NewDecoder(chResp.Body).Decode(&ch)
	chResp.Body.Close()
	if ch.ID == "" {
		return fmt.Errorf("discord returned empty DM channel id")
	}
	return a.SendChannelMessage(ctx, ch.ID, text)
}

// SendChannelMessage posts formatted text chunks to a Discord channel ID.
func (a *Adapter) SendChannelMessage(ctx context.Context, channelID, text string) error {
	for _, chunk := range format.SplitChunks(text, 1900) {
		raw, _ := json.Marshal(map[string]string{"content": chunk})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.APIBase+"/channels/"+channelID+"/messages", bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bot "+a.Token)
		resp, err := a.Client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return fmt.Errorf("discord sendMessage failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}
		resp.Body.Close()
	}
	return nil
}

// RegisterSlashCommands registers core Aegys slash commands globally or in a dev guild.
func (a *Adapter) RegisterSlashCommands(ctx context.Context) error {
	if a.Token == "" || a.AppID == "" {
		return nil
	}
	endpoint := fmt.Sprintf("%s/applications/%s/commands", a.APIBase, a.AppID)
	if a.DevGuildID != "" {
		endpoint = fmt.Sprintf("%s/applications/%s/guilds/%s/commands", a.APIBase, a.AppID, a.DevGuildID)
	}

	commands := []map[string]any{
		{"name": "ping", "description": "Check Aegys uptime and latency"},
		{"name": "status", "description": "Check backend and host health (admin)"},
		{"name": "tasks", "description": "List scheduled backend tasks (admin)"},
		{"name": "wol", "description": "Send Wake-on-LAN packet (admin)"},
		{"name": "reset", "description": "Reset your AI conversation session"},
		{
			"name":        "ask",
			"description": "Ask Aegys AI assistant",
			"options": []map[string]any{{
				"type":        3,
				"name":        "prompt",
				"description": "Prompt to send to the AI backend",
				"required":    true,
			}},
		},
	}

	raw, _ := json.Marshal(commands)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+a.Token)
	resp, err := a.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("discord slash command registration failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	return nil
}

// StartGateway maintains a reconnecting Discord Gateway v10 WebSocket session.
func (a *Adapter) StartGateway(ctx context.Context) {
	if err := a.RegisterSlashCommands(ctx); err != nil {
		log.Printf("[discord] slash command sync warning: %v", err)
	}
	for {
		if ctx.Err() != nil {
			return
		}
		if err := a.runGatewaySession(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[discord] gateway disconnected: %v (reconnecting in 5s)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

type gatewayPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

func (a *Adapter) runGatewaySession(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, a.GatewayURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "shutdown")
	conn.SetReadLimit(4 << 20)

	var lastSeq atomic.Int64
	hbCtx, cancelHB := context.WithCancel(ctx)
	defer cancelHB()

	// Read initial Hello (op 10)
	_, raw, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	var hello gatewayPayload
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Op != 10 {
		return fmt.Errorf("expected gateway Hello op=10, got op=%d err=%v", hello.Op, err)
	}
	var helloData struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	_ = json.Unmarshal(hello.D, &helloData)
	interval := time.Duration(helloData.HeartbeatInterval) * time.Millisecond
	if interval <= 0 {
		interval = 41250 * time.Millisecond
	}

	// Send Identify (op 2): Guilds(1) | GuildMessages(512) | DirectMessages(4096) | MessageContent(32768)
	identify, _ := json.Marshal(map[string]any{
		"op": 2,
		"d": map[string]any{
			"token":   a.Token,
			"intents": 1 | 512 | 4096 | 32768,
			"properties": map[string]string{
				"os":      "linux",
				"browser": "aegys-bot",
				"device":  "aegys-bot",
			},
		},
	})
	if err := conn.Write(ctx, websocket.MessageText, identify); err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				seq := lastSeq.Load()
				var d any
				if seq > 0 {
					d = seq
				}
				hb, _ := json.Marshal(map[string]any{"op": 1, "d": d})
				_ = conn.Write(hbCtx, websocket.MessageText, hb)
			}
		}
	}()

	log.Println("[discord] gateway connected")
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var p gatewayPayload
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		if p.S != nil {
			lastSeq.Store(*p.S)
		}
		if p.Op == 7 { // Reconnect requested
			return fmt.Errorf("gateway requested reconnect")
		}
		if p.Op != 0 {
			continue
		}
		switch p.T {
		case "MESSAGE_CREATE":
			go a.handleMessageCreate(ctx, p.D)
		case "INTERACTION_CREATE":
			go a.handleInteractionCreate(ctx, p.D)
		}
	}
}

func (a *Adapter) handleMessageCreate(ctx context.Context, raw json.RawMessage) {
	var ev struct {
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Content   string `json:"content"`
		Author    struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Author.Bot || strings.TrimSpace(ev.Content) == "" {
		return
	}
	res := a.Router.Handle(ctx, router.InboundMessage{
		Platform: "discord",
		UserID:   ev.Author.ID,
		ChatID:   ev.ChannelID,
		Text:     ev.Content,
		IsDM:     ev.GuildID == "",
	})
	if !res.Allowed {
		return
	}
	if err := a.SendChannelMessage(ctx, ev.ChannelID, res.Text); err != nil {
		log.Printf("[discord] reply error: %v", err)
	}
}

func (a *Adapter) handleInteractionCreate(ctx context.Context, raw json.RawMessage) {
	var ev struct {
		ID            string `json:"id"`
		ApplicationID string `json:"application_id"`
		Token         string `json:"token"`
		GuildID       string `json:"guild_id"`
		Channel       struct {
			ID string `json:"id"`
		} `json:"channel"`
		Member *struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"member"`
		User *struct {
			ID string `json:"id"`
		} `json:"user"`
		Data struct {
			Name    string `json:"name"`
			Options []struct {
				Name  string `json:"name"`
				Value any    `json:"value"`
			} `json:"options"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Data.Name == "" {
		return
	}
	userID := ""
	if ev.User != nil {
		userID = ev.User.ID
	} else if ev.Member != nil {
		userID = ev.Member.User.ID
	}
	isDM := ev.GuildID == ""
	callbackURL := fmt.Sprintf("%s/interactions/%s/%s/callback", a.APIBase, ev.ID, ev.Token)

	// Reject unauthorized interactions immediately with an ephemeral response (within Discord's 3s window).
	if !a.Router.IsAllowed("discord", userID, isDM) {
		_ = a.postJSON(ctx, http.MethodPost, callbackURL, map[string]any{
			"type": 4,
			"data": map[string]any{
				"content": "Access denied.",
				"flags":   64, // Ephemeral
			},
		})
		return
	}

	// Acknowledge immediately with DEFERRED_CHANNEL_MESSAGE_WITH_SOURCE (type 5) to avoid Discord's 3s timeout.
	if err := a.postJSON(ctx, http.MethodPost, callbackURL, map[string]any{"type": 5}); err != nil {
		log.Printf("[discord] defer interaction failed: %v", err)
		return
	}

	cmd := "/" + ev.Data.Name
	if len(ev.Data.Options) > 0 {
		cmd += " " + fmt.Sprint(ev.Data.Options[0].Value)
	}

	res := a.Router.Handle(ctx, router.InboundMessage{
		Platform: "discord",
		UserID:   userID,
		ChatID:   ev.Channel.ID,
		Text:     cmd,
		IsDM:     isDM,
	})

	reply := strings.TrimSpace(res.Text)
	if reply == "" {
		reply = "(empty response)"
	}

	appID := ev.ApplicationID
	if appID == "" {
		appID = a.AppID
	}
	if appID == "" {
		if ev.Channel.ID != "" {
			_ = a.SendChannelMessage(ctx, ev.Channel.ID, reply)
		}
		return
	}

	chunks := format.SplitChunks(reply, 1900)
	editURL := fmt.Sprintf("%s/webhooks/%s/%s/messages/@original", a.APIBase, appID, ev.Token)
	if err := a.postJSON(ctx, http.MethodPatch, editURL, map[string]any{"content": chunks[0]}); err != nil {
		log.Printf("[discord] edit interaction response failed: %v", err)
		return
	}
	if len(chunks) > 1 {
		followupURL := fmt.Sprintf("%s/webhooks/%s/%s", a.APIBase, appID, ev.Token)
		for _, chunk := range chunks[1:] {
			if err := a.postJSON(ctx, http.MethodPost, followupURL, map[string]any{"content": chunk}); err != nil {
				log.Printf("[discord] followup message failed: %v", err)
				return
			}
		}
	}
}

func (a *Adapter) postJSON(ctx context.Context, method, endpoint string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("discord %s %s failed (%d): %s", method, endpoint, resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	return nil
}
