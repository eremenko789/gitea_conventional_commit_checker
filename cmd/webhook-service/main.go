package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/eremenko789/gitea_conventional_commit_checker/internal/config"
	"github.com/eremenko789/gitea_conventional_commit_checker/internal/gitea"
	"github.com/eremenko789/gitea_conventional_commit_checker/internal/processor"
	"github.com/eremenko789/gitea_conventional_commit_checker/internal/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration")
	debug := flag.Bool("debug", false, "log incoming webhook payloads and outgoing Gitea HTTP (debug level)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config", "path", *configPath, "err", err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if cfg.Server.WebhookSecret == "" {
		log.Warn("server.webhook_secret is empty: webhook signatures are not verified")
	}

	client, err := gitea.NewClient(
		cfg.Gitea.BaseURL,
		cfg.Gitea.Token,
		cfg.Server.GiteaTimeout,
		cfg.Check.HTTPRetries,
		cfg.Check.HTTPRetryBaseDelay,
		log,
	)
	if err != nil {
		log.Error("gitea client", "err", err)
		os.Exit(1)
	}

	proc := processor.New(cfg, client, log, cfg.Server.QueueSize)
	proc.Start(cfg.Server.Workers)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := server.New(cfg, proc, log)
	if err := srv.Run(ctx); err != nil {
		log.Error("http server", "err", err)
	}

	proc.Shutdown()
}
