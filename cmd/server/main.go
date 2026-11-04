// Command server is the composition root of the Wallet Track API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/SebastianP07/wallet-track-core-app/internal/shared/platform"
)

func main() {
	if err := run(); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("server stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfg, err := platform.LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := platform.NewLogger(os.Stdout, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}

	server := platform.NewHTTPServer(platform.NewRouter())
	logger.Info("server started", "addr", listener.Addr().String())
	if err := platform.Serve(ctx, server, listener, cfg.ShutdownTimeout); err != nil {
		return err
	}
	logger.Info("server stopped")
	return nil
}
