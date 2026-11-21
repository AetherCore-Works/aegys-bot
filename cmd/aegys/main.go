package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AetherCore-Works/aegys-bot/internal/backend"
	"github.com/AetherCore-Works/aegys-bot/internal/channels"
	"github.com/AetherCore-Works/aegys-bot/internal/channels/discord"
	"github.com/AetherCore-Works/aegys-bot/internal/channels/mock"
	"github.com/AetherCore-Works/aegys-bot/internal/channels/telegram"
	"github.com/AetherCore-Works/aegys-bot/internal/channels/whatsapp"
	"github.com/AetherCore-Works/aegys-bot/internal/config"
	"github.com/AetherCore-Works/aegys-bot/internal/rbac"
	"github.com/AetherCore-Works/aegys-bot/internal/router"
	"github.com/AetherCore-Works/aegys-bot/internal/server"
)

func main() {
	devCLI := flag.Bool("dev-cli", false, "Run interactive local CLI sandbox (zero tokens required)")
	envPath := flag.String("env", ".env", "Path to optional .env file")
	flag.Parse()

	cfg := config.Load(*envPath)

	if *devCLI {
		cfg.EnableMock = true
		if len(cfg.AdminUserIDs) == 0 {
			cfg.AdminUserIDs = []string{"mock:admin", "discord:admin", "telegram:admin", "whatsapp:admin"}
		}
		if len(cfg.AllowedUserIDs) == 0 {
			cfg.AllowedUserIDs = []string{"mock:user", "discord:user", "telegram:user", "whatsapp:user"}
		}
	}

	aiBackend, err := backend.NewFromConfig(cfg)
	if err != nil {
		log.Fatalf("[aegys] backend init failed: %v", err)
	}
	customCmds, err := router.LoadCustomCommands(cfg.CommandsFile)
	if err != nil {
		log.Fatalf("[aegys] failed to load commands file %q: %v", cfg.CommandsFile, err)
	}

	guard := rbac.New(cfg.AdminUserIDs, cfg.AllowedUserIDs, cfg.DMOnly)
	rt := router.New(cfg.BotName, guard, aiBackend, customCmds)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *devCLI {
		mock.RunCLI(ctx, rt, os.Stdin, os.Stdout)
		return
	}

	registry := channels.NewRegistry()
	if cfg.EnableMock {
		registry.Register(mock.NewAdapter())
		log.Println("[aegys] mock channel enabled (/api/v1/dev/simulate active)")
	}

	var waAdapter *whatsapp.Adapter
	if cfg.EnableWhatsApp {
		provider, err := whatsapp.NewProviderFromConfig(cfg)
		if err != nil {
			log.Fatalf("[aegys] whatsapp provider init failed: %v", err)
		}
		waAdapter = whatsapp.NewAdapter(provider, rt)
		registry.Register(waAdapter)
		log.Printf("[aegys] whatsapp channel enabled (provider=%s)", provider.DriverName())
	}

	var tgAdapter *telegram.Adapter
	if cfg.EnableTelegram {
		tgAdapter = telegram.NewAdapter(cfg.TelegramBotToken, rt)
		registry.Register(tgAdapter)
		if cfg.TelegramMode == "polling" && cfg.TelegramBotToken != "" {
			go tgAdapter.StartPolling(ctx)
		}
		log.Printf("[aegys] telegram channel enabled (mode=%s)", cfg.TelegramMode)
	}

	if cfg.EnableDiscord {
		dcAdapter := discord.NewAdapter(cfg.DiscordBotToken, cfg.DiscordAppID, cfg.DiscordDevGuildID, rt)
		registry.Register(dcAdapter)
		if cfg.DiscordBotToken != "" {
			go dcAdapter.StartGateway(ctx)
		}
		log.Println("[aegys] discord channel enabled (gateway v10)")
	}

	srv := &server.Server{
		Cfg:      cfg,
		Router:   rt,
		Registry: registry,
		WhatsApp: waAdapter,
		Telegram: tgAdapter,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("[aegys] listening on http://%s (backend=%s, custom_commands=%d)", addr, aiBackend.Name(), len(customCmds))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[aegys] HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[aegys] shutting down gracefully...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
