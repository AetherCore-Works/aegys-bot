package rbac

import "testing"

func TestGuardRolesAndDMOnly(t *testing.T) {
	g := New(
		[]string{"discord:111111111111111111", "whatsapp:5500999990000"},
		[]string{"telegram:222222222", "mock:user"},
		true,
	)

	if got := g.RoleFor("discord", "111111111111111111", false); got != RoleDenied {
		t.Fatalf("expected RoleDenied in non-DM when dmOnly=true, got %v", got)
	}
	if got := g.RoleFor("discord", "111111111111111111", true); got != RoleAdmin {
		t.Fatalf("expected RoleAdmin for scoped discord admin, got %v", got)
	}
	if got := g.RoleFor("whatsapp", "+55 (00) 99999-0000@s.whatsapp.net", true); got != RoleAdmin {
		t.Fatalf("expected RoleAdmin for normalized whatsapp JID, got %v", got)
	}
	if got := g.RoleFor("telegram", "222222222", true); got != RoleUser {
		t.Fatalf("expected RoleUser for scoped telegram user, got %v", got)
	}
	if got := g.RoleFor("telegram", "999999999", true); got != RoleDenied {
		t.Fatalf("expected RoleDenied for unknown user, got %v", got)
	}
	if CanExecute(RoleUser, RoleAdmin) {
		t.Fatal("RoleUser must not satisfy RoleAdmin requirement")
	}
	if !CanExecute(RoleAdmin, RoleUser) {
		t.Fatal("RoleAdmin must satisfy RoleUser requirement")
	}
}
