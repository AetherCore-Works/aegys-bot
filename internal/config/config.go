package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Settings holds all runtime configuration loaded from environment variables (and optional .env).
type Settings struct {
	Host         string
	Port         int
	BotName      string
	CommandsFile string
	APISecret    string

	EnableMock bool

	// AI Backend selection: mock | agy-gateway | ollama | openai
	AIBackend       string
	AISystemPrompt  string
	AgyGatewayURL   string
	AgyGatewaySecret string
	OllamaURL       string
	OllamaModel     string
	OpenAIBaseURL   string
	OpenAIAPIKey    string
	OpenAIModel     string

	// RBAC
	AdminUserIDs   []string
	AllowedUserIDs []string
	DMOnly         bool

	// Discord
	EnableDiscord     bool
	DiscordBotToken   string
	DiscordAppID      string
	DiscordDevGuildID string

	// Telegram
	EnableTelegram        bool
	TelegramBotToken      string
	TelegramMode          string // polling | webhook
	TelegramWebhookSecret string

	// WhatsApp (Pluggable Provider)
	EnableWhatsApp          bool
	WhatsAppProvider        string // evolution | webhook
	WhatsAppWebhookSecret   string
	EvolutionAPIURL         string
	EvolutionInstance       string
	EvolutionAPIKey         string
	WhatsAppGenericSendURL  string
	WhatsAppGenericAuthTok  string
}

// Load reads an optional .env file (without overriding existing OS env vars) and returns Settings.
func Load(dotenvPath string) Settings {
	if dotenvPath != "" {
		loadDotEnv(dotenvPath)
	}

	port, err := strconv.Atoi(envOr("AEGYS_PORT", "8090"))
	if err != nil || port <= 0 {
		port = 8090
	}

	return Settings{
		Host:         envOr("AEGYS_HOST", "127.0.0.1"),
		Port:         port,
		BotName:      envOr("AEGYS_BOT_NAME", "Aegys"),
		CommandsFile: envOr("AEGYS_COMMANDS_FILE", "config/commands.example.json"),
		APISecret:    strings.TrimSpace(os.Getenv("AEGYS_API_SECRET")),

		EnableMock: boolEnv("ENABLE_MOCK", false),

		AIBackend:        strings.ToLower(envOr("AI_BACKEND", "mock")),
		AISystemPrompt:   envOr("AI_SYSTEM_PROMPT", "You are Aegys, a concise and reliable personal operations assistant."),
		AgyGatewayURL:    strings.TrimRight(envOr("AGY_GATEWAY_URL", "http://127.0.0.1:8088"), "/"),
		AgyGatewaySecret: strings.TrimSpace(os.Getenv("AGY_GATEWAY_SECRET")),
		OllamaURL:        strings.TrimRight(envOr("OLLAMA_URL", "http://127.0.0.1:11434"), "/"),
		OllamaModel:      envOr("OLLAMA_MODEL", "qwen3:8b"),
		OpenAIBaseURL:    strings.TrimRight(envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"), "/"),
		OpenAIAPIKey:     strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		OpenAIModel:      envOr("OPENAI_MODEL", "gpt-4o-mini"),

		AdminUserIDs:   splitCSV(os.Getenv("ADMIN_USER_IDS")),
		AllowedUserIDs: splitCSV(os.Getenv("ALLOWED_USER_IDS")),
		DMOnly:         boolEnv("DM_ONLY", true),

		EnableDiscord:     boolEnv("ENABLE_DISCORD", false),
		DiscordBotToken:   strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")),
		DiscordAppID:      strings.TrimSpace(os.Getenv("DISCORD_APP_ID")),
		DiscordDevGuildID: strings.TrimSpace(os.Getenv("DISCORD_DEV_GUILD_ID")),

		EnableTelegram:        boolEnv("ENABLE_TELEGRAM", false),
		TelegramBotToken:      strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		TelegramMode:          strings.ToLower(envOr("TELEGRAM_MODE", "polling")),
		TelegramWebhookSecret: strings.TrimSpace(os.Getenv("TELEGRAM_WEBHOOK_SECRET")),

		EnableWhatsApp:         boolEnv("ENABLE_WHATSAPP", false),
		WhatsAppProvider:       strings.ToLower(envOr("WHATSAPP_PROVIDER", "evolution")),
		WhatsAppWebhookSecret:  strings.TrimSpace(os.Getenv("WHATSAPP_WEBHOOK_SECRET")),
		EvolutionAPIURL:        strings.TrimRight(envOr("EVOLUTION_API_URL", "http://127.0.0.1:8085"), "/"),
		EvolutionInstance:      envOr("EVOLUTION_INSTANCE", "aegys"),
		EvolutionAPIKey:        strings.TrimSpace(os.Getenv("EVOLUTION_API_KEY")),
		WhatsAppGenericSendURL: strings.TrimRight(strings.TrimSpace(os.Getenv("WHATSAPP_GENERIC_SEND_URL")), "/"),
		WhatsAppGenericAuthTok: strings.TrimSpace(os.Getenv("WHATSAPP_GENERIC_AUTH_TOKEN")),
	}
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k == "" {
			continue
		}
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
