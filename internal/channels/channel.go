package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Sender is implemented by every outbound channel adapter (Discord, Telegram, WhatsApp, Mock).
type Sender interface {
	Platform() string
	Send(ctx context.Context, recipient, text string) error
}

// Registry maps platform names to their active outbound Sender adapters.
type Registry struct {
	mu      sync.RWMutex
	senders map[string]Sender
}

func NewRegistry() *Registry {
	return &Registry{senders: make(map[string]Sender)}
}

func (r *Registry) Register(s Sender) {
	if s == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.senders[strings.ToLower(strings.TrimSpace(s.Platform()))] = s
}

func (r *Registry) Send(ctx context.Context, platform, recipient, text string) error {
	key := strings.ToLower(strings.TrimSpace(platform))
	r.mu.RLock()
	s, ok := r.senders[key]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("channel %q is not enabled", platform)
	}
	return s.Send(ctx, recipient, text)
}

func (r *Registry) EnabledPlatforms() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.senders))
	for k := range r.senders {
		out = append(out, k)
	}
	return out
}
