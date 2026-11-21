package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "AEGYS_BOT_NAME=AegysTest\nAEGYS_PORT=9091\nEVOLUTION_INSTANCE=aegys\nADMIN_USER_IDS=discord:111,telegram:222\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("AEGYS_BOT_NAME", "")
	t.Setenv("AEGYS_PORT", "")
	t.Setenv("EVOLUTION_INSTANCE", "")
	t.Setenv("ADMIN_USER_IDS", "")
	_ = os.Unsetenv("AEGYS_BOT_NAME")
	_ = os.Unsetenv("AEGYS_PORT")
	_ = os.Unsetenv("EVOLUTION_INSTANCE")
	_ = os.Unsetenv("ADMIN_USER_IDS")

	cfg := Load(envFile)
	if cfg.BotName != "AegysTest" || cfg.Port != 9091 {
		t.Fatalf("unexpected dotenv load: %+v", cfg)
	}
	if cfg.EvolutionInstance != "aegys" {
		t.Fatalf("expected default/dotenv instance aegys, got %q", cfg.EvolutionInstance)
	}
	if len(cfg.AdminUserIDs) != 2 {
		t.Fatalf("expected 2 admins, got %v", cfg.AdminUserIDs)
	}
	if !cfg.DMOnly {
		t.Fatal("DMOnly should default to true")
	}
}
