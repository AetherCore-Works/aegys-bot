package whatsapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/AetherCore-Works/aegys-bot/internal/config"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
)

// Provider abstracts the underlying WhatsApp transport (Evolution API v2, Cloud API proxy, or generic webhook).
type Provider interface {
	DriverName() string
	ParseWebhook(body []byte) (router.InboundMessage, bool, error)
	SendText(ctx context.Context, recipient, text string) error
}

// Adapter wraps a pluggable WhatsApp Provider as a standard Aegys channel.
type Adapter struct {
	Provider Provider
	Router   *router.Router
}

func NewAdapter(p Provider, r *router.Router) *Adapter {
	return &Adapter{Provider: p, Router: r}
}

func (a *Adapter) Platform() string { return "whatsapp" }

func (a *Adapter) Send(ctx context.Context, recipient, text string) error {
	return a.Provider.SendText(ctx, recipient, text)
}

// NewProviderFromConfig selects the configured WhatsApp transport driver.
func NewProviderFromConfig(cfg config.Settings) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.WhatsAppProvider)) {
	case "", "evolution":
		return NewEvolutionProvider(cfg.EvolutionAPIURL, cfg.EvolutionInstance, cfg.EvolutionAPIKey), nil
	case "webhook", "generic":
		return NewGenericWebhookProvider(cfg.WhatsAppGenericSendURL, cfg.WhatsAppGenericAuthTok), nil
	default:
		return nil, fmt.Errorf("unsupported WHATSAPP_PROVIDER %q (supported: evolution, webhook)", cfg.WhatsAppProvider)
	}
}
