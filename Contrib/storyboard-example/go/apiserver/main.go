package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	profile, err := loadProfile(cfg.ProfileFile)
	if err != nil {
		return err
	}
	agentk, err := NewAgentKClient()
	if err != nil {
		return err
	}
	handler := NewServer(
		agentk,
		NewMemoryJobStore(),
		profile,
		cfg.Models,
	).Handler()

	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	httpServer := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	signalCtx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	serverError := make(chan error, 1)
	go func() {
		log.Printf("storyboard example API listening on %s", address)
		serverError <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-signalCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		if err := <-serverError; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	}
}

func loadProfile(path string) (*apiv1.Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", path, err)
	}
	profile := &apiv1.Profile{}
	if err := protojson.Unmarshal(raw, profile); err != nil {
		return nil, fmt.Errorf("decode profile %s: %w", path, err)
	}
	return profile, nil
}
