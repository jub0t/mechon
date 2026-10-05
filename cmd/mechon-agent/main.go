// Command mechon-agent runs on each node: it dials the panel, runs bots in locked-down Docker
// containers and streams their state, stats and logs back. Linux only; runs as root.
//
// Configuration (environment):
//
//	MECHON_PANEL_URL      panel base URL, e.g. https://panel.example.com (required)
//	MECHON_NODE_TOKEN     node token from the panel's "add node" screen (required)
//	MECHON_DATA_DIR       bot volumes and agent state (default /var/lib/mechon)
//	MECHON_DOCKER_SOCKET  Docker Engine socket (default /var/run/docker.sock)
//	MECHON_DNS            comma-separated resolvers for bots (default 1.1.1.1,8.8.8.8)
//	MECHON_LOG_LEVEL      debug | info | warn | error (default info)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jub0t/mechon/internal/agent"
	"github.com/jub0t/mechon/internal/runtime"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("mechon-agent", agent.Version)
		return
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(env("MECHON_LOG_LEVEL", "info")))
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	if err := run(log); err != nil && err != context.Canceled {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	panelURL := strings.TrimRight(os.Getenv("MECHON_PANEL_URL"), "/")
	token := os.Getenv("MECHON_NODE_TOKEN")
	if panelURL == "" || token == "" {
		return fmt.Errorf("MECHON_PANEL_URL and MECHON_NODE_TOKEN are required")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("mechon-agent must run as root (it mounts volumes and manages iptables)")
	}
	dataDir := env("MECHON_DATA_DIR", "/var/lib/mechon")
	var dns []string
	if v := os.Getenv("MECHON_DNS"); v != "" {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				dns = append(dns, s)
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := agent.New(agent.Config{PanelURL: panelURL, Token: token, DataDir: dataDir, Logger: log})
	rt, err := runtime.NewDocker(ctx, runtime.Config{
		DataDir:   dataDir,
		Socket:    env("MECHON_DOCKER_SOCKET", "/var/run/docker.sock"),
		PanelURL:  panelURL,
		NodeToken: token,
		DNS:       dns,
		Hooks:     a.Hooks(),
		Logger:    log,
	})
	if err != nil {
		return err
	}
	log.Info("mechon-agent starting", "version", agent.Version, "panel", panelURL, "data_dir", dataDir)
	// Bots keep running when the agent stops; the next agent adopts them.
	return a.Run(ctx, rt)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
