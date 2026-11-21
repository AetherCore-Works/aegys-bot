package router

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AetherCore-Works/aegys-bot/internal/backend"
	"github.com/AetherCore-Works/aegys-bot/internal/rbac"
)

// InboundMessage is the channel-agnostic message envelope passed to the Hybrid Router.
type InboundMessage struct {
	Platform string
	UserID   string
	ChatID   string
	Text     string
	IsDM     bool
}

// ActionButton describes an optional interactive button rendered by Telegram/Discord adapters.
type ActionButton struct {
	Label   string `json:"label"`
	Command string `json:"command"`
}

// Response is returned by the Router and formatted by each channel adapter.
type Response struct {
	Allowed     bool           `json:"allowed"`
	Path        string         `json:"path"` // "fast" | "ai" | "denied"
	Role        string         `json:"role"`
	Text        string         `json:"text"`
	Buttons     []ActionButton `json:"buttons,omitempty"`
	OutboxFiles []string       `json:"outbox_files,omitempty"`
}

// Router implements Aegys's Hybrid Routing architecture (Fast-Path vs AI-Path) with RBAC.
type Router struct {
	botName   string
	startedAt time.Time
	guard     *rbac.Guard
	ai        backend.AIBackend
	custom    map[string]CustomCommand

	mu       sync.RWMutex
	sessions map[string]string // "platform:chatID" -> conversationID
}

// New creates a Hybrid Router backed by an RBAC Guard, an AI backend, and optional custom commands.
func New(botName string, guard *rbac.Guard, ai backend.AIBackend, custom []CustomCommand) *Router {
	if strings.TrimSpace(botName) == "" {
		botName = "Aegys"
	}
	cm := make(map[string]CustomCommand, len(custom))
	for _, c := range custom {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		if name != "" {
			if !strings.HasPrefix(name, "/") {
				name = "/" + name
			}
			c.Name = name
			cm[name] = c
		}
	}
	return &Router{
		botName:   botName,
		startedAt: time.Now(),
		guard:     guard,
		ai:        ai,
		custom:    cm,
		sessions:  make(map[string]string),
	}
}

// Handle evaluates RBAC, dispatches Fast-Path deterministic commands, or routes AI-Path prompts.
func (r *Router) Handle(ctx context.Context, msg InboundMessage) Response {
	role := r.guard.RoleFor(msg.Platform, msg.UserID, msg.IsDM)
	if role == rbac.RoleDenied {
		return Response{
			Allowed: false,
			Path:    "denied",
			Role:    role.String(),
			Text:    "Access denied. Your ID is not authorized for inbound commands on this channel.",
		}
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: "Send a message or `/help` to list available commands."}
	}

	// Parse command prefix if present
	if strings.HasPrefix(text, "/") {
		fields := strings.Fields(text)
		cmd := strings.ToLower(fields[0])
		// Strip Telegram bot mention suffix (e.g. /status@aegys_core_bot)
		if before, _, ok := strings.Cut(cmd, "@"); ok {
			cmd = before
		}
		arg := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))

		switch cmd {
		case "/start", "/help":
			return Response{Allowed: true, Path: "fast", Role: role.String(), Text: r.helpText(role)}
		case "/ping":
			uptime := time.Since(r.startedAt).Round(time.Second)
			return Response{
				Allowed: true,
				Path:    "fast",
				Role:    role.String(),
				Text:    fmt.Sprintf("**%s** is online (uptime: `%s`, backend: `%s`).", r.botName, uptime, r.ai.Name()),
			}
		case "/whoami":
			return Response{
				Allowed: true,
				Path:    "fast",
				Role:    role.String(),
				Text:    fmt.Sprintf("Platform: `%s`\nUser ID: `%s`\nRole: `%s`\nDM: `%v`", msg.Platform, msg.UserID, role.String(), msg.IsDM),
			}
		case "/reset", "/new":
			r.resetSession(msg.Platform, msg.ChatID)
			return Response{Allowed: true, Path: "fast", Role: role.String(), Text: "Conversation context reset. Your next message starts a fresh session."}
		case "/status":
			return r.handleStatus(ctx, role)
		case "/tasks":
			return r.handleTasks(ctx, role)
		case "/run":
			return r.handleRunTask(ctx, role, arg)
		case "/wol":
			return r.handleWakeHost(ctx, role)
		case "/ask":
			if arg == "" {
				return Response{Allowed: true, Path: "fast", Role: role.String(), Text: "Usage: `/ask <your prompt>`"}
			}
			return r.handleAI(ctx, msg, role, arg)
		default:
			if customCmd, ok := r.custom[cmd]; ok {
				if customCmd.AdminOnly && !rbac.CanExecute(role, rbac.RoleAdmin) {
					return r.adminRequired(role, cmd)
				}
				prompt := strings.ReplaceAll(customCmd.PromptTemplate, "{{input}}", arg)
				return r.handleAI(ctx, msg, role, strings.TrimSpace(prompt))
			}
			return Response{
				Allowed: true,
				Path:    "fast",
				Role:    role.String(),
				Text:    fmt.Sprintf("Unknown command `%s`. Type `/help` to view available commands.", cmd),
			}
		}
	}

	// Free text routes directly to AI-Path
	return r.handleAI(ctx, msg, role, text)
}

func (r *Router) handleStatus(ctx context.Context, role rbac.Role) Response {
	if !rbac.CanExecute(role, rbac.RoleAdmin) {
		return r.adminRequired(role, "/status")
	}
	health, err := r.ai.Health(ctx)
	if err != nil {
		return Response{
			Allowed: true,
			Path:    "fast",
			Role:    role.String(),
			Text:    fmt.Sprintf("## %s Status\n- **Bot:** online (`%s`)\n- **Backend (`%s`):** degraded — `%v`", r.botName, time.Since(r.startedAt).Round(time.Second), r.ai.Name(), err),
		}
	}
	return Response{
		Allowed: true,
		Path:    "fast",
		Role:    role.String(),
		Text:    fmt.Sprintf("## %s Status\n- **Bot:** online (`%s`)\n- **Backend:** %s", r.botName, time.Since(r.startedAt).Round(time.Second), health),
	}
}

func (r *Router) handleTasks(ctx context.Context, role rbac.Role) Response {
	if !rbac.CanExecute(role, rbac.RoleAdmin) {
		return r.adminRequired(role, "/tasks")
	}
	ops, ok := r.ai.(backend.OpsBackend)
	if !ok {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: fmt.Sprintf("Backend `%s` does not expose scheduled tasks.", r.ai.Name())}
	}
	tasks, err := ops.ListTasks(ctx)
	if err != nil {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: fmt.Sprintf("Failed to list tasks: %v", err)}
	}
	if len(tasks) == 0 {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: "No scheduled tasks configured on the backend."}
	}

	var b strings.Builder
	b.WriteString("## Scheduled Tasks\n")
	buttons := make([]ActionButton, 0, len(tasks))
	for _, t := range tasks {
		state := "paused"
		if t.Enabled {
			state = "active"
		}
		b.WriteString(fmt.Sprintf("- `%s` — **%s** (`%s`, %s)\n", t.ID, t.Name, t.Schedule, state))
		buttons = append(buttons, ActionButton{
			Label:   "Run " + t.ID,
			Command: "/run " + t.ID,
		})
	}
	return Response{Allowed: true, Path: "fast", Role: role.String(), Text: strings.TrimSpace(b.String()), Buttons: buttons}
}

func (r *Router) handleRunTask(ctx context.Context, role rbac.Role, taskID string) Response {
	if !rbac.CanExecute(role, rbac.RoleAdmin) {
		return r.adminRequired(role, "/run")
	}
	if taskID == "" {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: "Usage: `/run <task-id>` (see `/tasks`)."}
	}
	ops, ok := r.ai.(backend.OpsBackend)
	if !ok {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: fmt.Sprintf("Backend `%s` does not support remote task execution.", r.ai.Name())}
	}
	msg, err := ops.RunTask(ctx, taskID)
	if err != nil {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: fmt.Sprintf("Task trigger failed: %v", err)}
	}
	return Response{Allowed: true, Path: "fast", Role: role.String(), Text: msg}
}

func (r *Router) handleWakeHost(ctx context.Context, role rbac.Role) Response {
	if !rbac.CanExecute(role, rbac.RoleAdmin) {
		return r.adminRequired(role, "/wol")
	}
	ops, ok := r.ai.(backend.OpsBackend)
	if !ok {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: fmt.Sprintf("Backend `%s` does not support Wake-on-LAN.", r.ai.Name())}
	}
	msg, err := ops.WakeHost(ctx)
	if err != nil {
		return Response{Allowed: true, Path: "fast", Role: role.String(), Text: fmt.Sprintf("Wake-on-LAN failed: %v", err)}
	}
	return Response{Allowed: true, Path: "fast", Role: role.String(), Text: msg}
}

func (r *Router) handleAI(ctx context.Context, msg InboundMessage, role rbac.Role, prompt string) Response {
	convID := r.getSession(msg.Platform, msg.ChatID)
	res, err := r.ai.Ask(ctx, backend.AskRequest{
		Platform:       msg.Platform,
		UserID:         msg.UserID,
		ChatID:         msg.ChatID,
		ConversationID: convID,
		Prompt:         prompt,
	})
	if err != nil {
		return Response{Allowed: true, Path: "ai", Role: role.String(), Text: fmt.Sprintf("AI backend error: %v", err)}
	}
	if res.ConversationID != "" {
		r.setSession(msg.Platform, msg.ChatID, res.ConversationID)
	}
	reply := strings.TrimSpace(res.Reply)
	if reply == "" {
		reply = "(empty response from AI backend)"
	}
	return Response{
		Allowed:     true,
		Path:        "ai",
		Role:        role.String(),
		Text:        reply,
		OutboxFiles: res.OutboxFiles,
	}
}

// IsAllowed checks RBAC authorization without invoking any backend or mutating sessions.
func (r *Router) IsAllowed(platform, userID string, isDM bool) bool {
	return r.guard.RoleFor(platform, userID, isDM) != rbac.RoleDenied
}

func (r *Router) adminRequired(role rbac.Role, cmd string) Response {
	return Response{
		Allowed: true,
		Path:    "denied",
		Role:    role.String(),
		Text:    fmt.Sprintf("Command `%s` requires the `admin` role.", cmd),
	}
}

func (r *Router) helpText(role rbac.Role) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## %s Commands\n", r.botName))
	b.WriteString("**Fast-Path (Instant):**\n")
	b.WriteString("- `/ping` — Check bot latency and uptime\n")
	b.WriteString("- `/whoami` — Show your platform ID and RBAC role\n")
	b.WriteString("- `/reset` — Start a new AI conversation session\n")
	if rbac.CanExecute(role, rbac.RoleAdmin) {
		b.WriteString("- `/status` — Backend & host health check *(admin)*\n")
		b.WriteString("- `/tasks` — List scheduled backend automations *(admin)*\n")
		b.WriteString("- `/run <id>` — Trigger an automation immediately *(admin)*\n")
		b.WriteString("- `/wol` — Send Wake-on-LAN packet *(admin)*\n")
	}
	b.WriteString("\n**AI-Path:**\n")
	b.WriteString("- `/ask <prompt>` or any plain text message — Route to `" + r.ai.Name() + "`\n")

	if len(r.custom) > 0 {
		names := make([]string, 0, len(r.custom))
		for k := range r.custom {
			names = append(names, k)
		}
		sort.Strings(names)
		b.WriteString("\n**Custom Commands:**\n")
		for _, k := range names {
			c := r.custom[k]
			if c.AdminOnly && !rbac.CanExecute(role, rbac.RoleAdmin) {
				continue
			}
			b.WriteString(fmt.Sprintf("- `%s` — %s\n", c.Name, c.Description))
		}
	}
	return strings.TrimSpace(b.String())
}

func sessionKey(platform, chatID string) string {
	return strings.ToLower(strings.TrimSpace(platform)) + ":" + strings.TrimSpace(chatID)
}

func (r *Router) getSession(platform, chatID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[sessionKey(platform, chatID)]
}

func (r *Router) setSession(platform, chatID, convID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[sessionKey(platform, chatID)] = convID
}

func (r *Router) resetSession(platform, chatID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, sessionKey(platform, chatID))
}
