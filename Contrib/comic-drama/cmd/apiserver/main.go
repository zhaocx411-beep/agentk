// API Server：单个进程承载全部 Agent 产品——漫剧工坊（comic-drama）、
// 六个固定 Profile 的通用 Chat Agent、Agent 集市前端与共享上传。
//
// 漫剧双模式：pipeline（确定性流水线，需部署 cmd/drama-worker）或
// agent（自由 Agent 会话，默认，只依赖 AgentK Connect API）。
// 本进程不落库：任务与会话索引在内存里，重启即丢；核心数据（账本、
// 产物）都在 AgentK 侧。
package main

import (
	"context"
	"encoding/json"
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

	comicdrama "studio/agents/comic-drama"
	"studio/platform/app"
	"studio/platform/chat"
	"studio/platform/core"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := core.LoadConfig()
	if err != nil {
		return err
	}
	profile, err := dramaProfile(cfg)
	if err != nil {
		return err
	}
	agentk, err := core.NewHTTPAgentKClient(cfg.AgentKBaseURL, cfg.AgentKAPIKey)
	if err != nil {
		return err
	}
	uploads, err := chat.NewBlobUploadStorageFromEnv()
	if err != nil {
		return err
	}

	agents := chat.DefaultRegistry()
	dramaStore := comicdrama.NewMemoryDramaStore()
	if cfg.Mode == core.ModeAgent {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		sessions, recoveryErr := agentk.ListSessions(recoveryCtx)
		cancel()
		if recoveryErr != nil {
			log.Printf("comic drama session recovery skipped: %v", recoveryErr)
		} else if restored := comicdrama.RestoreAgentSessions(dramaStore, sessions); restored > 0 {
			log.Printf("restored %d comic drama session(s) from AgentK", restored)
		}
	}
	drama := comicdrama.NewServer(
		cfg.Mode, agentk, dramaStore, profile, cfg.Models,
	)
	var uploadStorage chat.UploadStorage
	if uploads != nil {
		uploadStorage = uploads
		log.Printf("shared upload storage enabled")
	}
	chatServer := chat.NewServer(agentk, agents, uploadStorage)
	handler := app.Handler(drama, chatServer, agents)
	log.Printf("comic drama mode: %s", cfg.Mode)

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
		log.Printf("agent studio API listening on %s", address)
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

// dramaProfile 决定漫剧会话使用的 Profile：PROFILE_FILE 指定磁盘路径时
// 以磁盘为准（线上热修 prompt 用），否则用编译期内嵌的缺省版本。
func dramaProfile(cfg core.Config) (json.RawMessage, error) {
	if cfg.ProfileFile == "" {
		log.Printf("comic drama profile: embedded default (%s)", cfg.Mode)
		raw := json.RawMessage(comicdrama.DefaultProfile(cfg.Mode))
		if _, err := core.DecodeAgentKProfile(raw); err != nil {
			return nil, fmt.Errorf("decode embedded comic drama profile: %w", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(cfg.ProfileFile)
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", cfg.ProfileFile, err)
	}
	if _, err := core.DecodeAgentKProfile(raw); err != nil {
		return nil, fmt.Errorf("decode profile %s: %w", cfg.ProfileFile, err)
	}
	log.Printf("comic drama profile: %s", cfg.ProfileFile)
	return json.RawMessage(raw), nil
}
