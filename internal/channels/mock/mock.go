package mock

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/AetherCore-Works/aegys-bot/internal/format"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

// DeliveredMessage records outbound notifications sent through the Mock channel.
type DeliveredMessage struct {
	Recipient string `json:"recipient"`
	Text      string `json:"text"`
}

// Adapter provides an in-memory outbound sink and interactive CLI dev sandbox.
type Adapter struct {
	mu      sync.Mutex
	History []DeliveredMessage
}

func NewAdapter() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Platform() string { return "mock" }

func (a *Adapter) Send(_ context.Context, recipient, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.History = append(a.History, DeliveredMessage{Recipient: recipient, Text: text})
	return nil
}

// RunCLI launches an interactive terminal sandbox so contributors can test commands,
// RBAC tiers, and channel formatting without Discord/Telegram/WhatsApp tokens.
func RunCLI(ctx context.Context, rt *router.Router, in io.Reader, out io.Writer) {
	platform := "mock"
	userID := "admin"
	isDM := true

	fmt.Fprintln(out, "=== Aegys Bot — Interactive Dev Sandbox (--dev-cli) ===")
	fmt.Fprintln(out, "Meta commands: :as <admin|user|guest> | :platform <mock|discord|telegram|whatsapp> | :dm <true|false> | :quit")
	fmt.Fprintln(out, "Try: /help, /ping, /status, /tasks, /whoami, or any natural-language prompt.")
	fmt.Fprintln(out)

	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprintf(out, "[%s:%s dm=%v]> ", platform, userID, isDM)
		if !scanner.Scan() {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == ":quit" || line == ":exit" {
			fmt.Fprintln(out, "Bye!")
			return
		}
		if strings.HasPrefix(line, ":as ") {
			userID = strings.TrimSpace(strings.TrimPrefix(line, ":as "))
			fmt.Fprintf(out, "Switched active user to %q\n", userID)
			continue
		}
		if strings.HasPrefix(line, ":platform ") {
			platform = strings.TrimSpace(strings.TrimPrefix(line, ":platform "))
			fmt.Fprintf(out, "Switched simulated platform to %q\n", platform)
			continue
		}
		if strings.HasPrefix(line, ":dm ") {
			isDM = strings.TrimSpace(strings.TrimPrefix(line, ":dm ")) == "true"
			fmt.Fprintf(out, "Switched isDM to %v\n", isDM)
			continue
		}

		res := rt.Handle(ctx, router.InboundMessage{
			Platform: platform,
			UserID:   userID,
			ChatID:   "dev-chat",
			Text:     line,
			IsDM:     isDM,
		})
		formatted := format.ForChannel(platform, res.Text)
		fmt.Fprintf(out, "[path=%s role=%s allowed=%v]\n%s\n", res.Path, res.Role, res.Allowed, formatted)
		if len(res.Buttons) > 0 {
			labels := make([]string, 0, len(res.Buttons))
			for _, b := range res.Buttons {
				labels = append(labels, fmt.Sprintf("[%s -> %s]", b.Label, b.Command))
			}
			fmt.Fprintf(out, "Buttons: %s\n", strings.Join(labels, " "))
		}
		fmt.Fprintln(out)
	}
}
