package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/config"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/gateway"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/kessclient"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("MaaS gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadGateway()
	if err != nil {
		return fmt.Errorf("load gateway configuration: %w", err)
	}
	clients, err := kessclient.New(cfg.Routes)
	if err != nil {
		return fmt.Errorf("initialize KESS clients: %w", err)
	}
	defer clients.Close()

	handler := gateway.New(clients, gateway.Options{
		Routes: cfg.Routes, MaxRequestSize: cfg.MaxRequestSize,
		BearerToken: cfg.BearerToken, Logger: logger,
	})
	server := &http.Server{
		Addr: cfg.HTTPAddress, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 1 << 20,
	}
	listener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen for HTTP gateway: %w", err)
	}
	defer listener.Close()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	logger.Info("PRT MaaS HTTP gateway started", "address", listener.Addr().String(), "routes", len(cfg.Routes))

	select {
	case err := <-serveError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP gateway: %w", err)
		}
		return nil
	case <-shutdownSignal.Done():
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Shutdown)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shut down HTTP gateway: %w", err)
		}
		if err := <-serveError; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP gateway during shutdown: %w", err)
		}
		return nil
	}
}
