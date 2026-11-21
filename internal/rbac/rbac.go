package rbac

import (
	"strings"
)

// Role represents a sender's authorization tier in Aegys.
type Role int

const (
	// RoleDenied blocks all inbound commands and prompts.
	RoleDenied Role = iota
	// RoleUser grants access to safe Fast-Path commands and AI prompts.
	RoleUser
	// RoleAdmin grants full system control (/tasks, /run, /wol, admin commands).
	RoleAdmin
)

func (r Role) String() string {
	switch r {
	case RoleAdmin:
		return "admin"
	case RoleUser:
		return "user"
	default:
		return "denied"
	}
}

// Guard evaluates inbound sender permissions across channels.
type Guard struct {
	admins  map[string]struct{}
	users   map[string]struct{}
	dmOnly  bool
}

// New builds an RBAC Guard from platform-scoped ("discord:123") or plain ("123") IDs.
func New(adminIDs, allowedIDs []string, dmOnly bool) *Guard {
	g := &Guard{
		admins: make(map[string]struct{}, len(adminIDs)),
		users:  make(map[string]struct{}, len(allowedIDs)),
		dmOnly: dmOnly,
	}
	for _, id := range adminIDs {
		if norm := normalizeEntry(id); norm != "" {
			g.admins[norm] = struct{}{}
		}
	}
	for _, id := range allowedIDs {
		if norm := normalizeEntry(id); norm != "" {
			g.users[norm] = struct{}{}
		}
	}
	return g
}

// RoleFor resolves the effective Role for a sender on a given platform.
// If DMOnly is enabled and isDM is false, RoleDenied is always returned.
func (g *Guard) RoleFor(platform, userID string, isDM bool) Role {
	if g == nil {
		return RoleDenied
	}
	if g.dmOnly && !isDM {
		return RoleDenied
	}

	p := strings.ToLower(strings.TrimSpace(platform))
	u := normalizeID(p, userID)
	if u == "" {
		return RoleDenied
	}

	scoped := p + ":" + u
	if _, ok := g.admins[scoped]; ok {
		return RoleAdmin
	}
	if _, ok := g.admins[u]; ok {
		return RoleAdmin
	}
	if _, ok := g.users[scoped]; ok {
		return RoleUser
	}
	if _, ok := g.users[u]; ok {
		return RoleUser
	}
	return RoleDenied
}

// CanExecute reports whether role meets the minimum required tier.
func CanExecute(role, required Role) bool {
	return role >= required && role != RoleDenied
}

func normalizeEntry(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if p, id, ok := strings.Cut(trimmed, ":"); ok {
		p = strings.ToLower(strings.TrimSpace(p))
		id = normalizeID(p, id)
		if p == "" || id == "" {
			return ""
		}
		return p + ":" + id
	}
	return strings.TrimSpace(trimmed)
}

func normalizeID(platform, raw string) string {
	s := strings.TrimSpace(raw)
	if platform == "whatsapp" {
		if before, _, ok := strings.Cut(s, "@"); ok {
			s = before
		}
		var digits strings.Builder
		for _, r := range s {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		return digits.String()
	}
	return s
}
