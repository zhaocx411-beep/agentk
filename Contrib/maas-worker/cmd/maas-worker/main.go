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

	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/capworker"
	"git.corp.kuaishou.com/klingai-agentk/contrib/maas-worker.git/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("AgentK MaaS worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("load worker configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	runtime, err := capworker.Start(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer runtime.Close()

	listener, err := net.Listen("tcp", cfg.HealthAddress)
	if err != nil {
		return fmt.Errorf("listen for worker health checks: %w", err)
	}
	defer listener.Close()
	healthServer := &http.Server{
		Addr: cfg.HealthAddress, Handler: workerHealthHandler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	serveError := make(chan error, 1)
	go func() { serveError <- healthServer.Serve(listener) }()
	logger.Info("AgentK MaaS worker health endpoint started", "address", listener.Addr().String())

	select {
	case err := <-serveError:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve worker health checks: %w", err)
		}
		return nil
	case <-ctx.Done():
		runtime.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Shutdown)
		defer cancel()
		if err := healthServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down worker health endpoint: %w", err)
		}
		if err := <-serveError; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve worker health checks during shutdown: %w", err)
		}
		return nil
	}
}

func workerHealthHandler() http.Handler {
	mux := http.NewServeMux()
	health := func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("{\"status\":\"ok\",\"role\":\"worker\"}\n"))
	}
	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /readyz", health)
	return mux
}
