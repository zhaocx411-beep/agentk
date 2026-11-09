package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentk/internal/agentloop"
	"agentk/internal/api"
	"agentk/internal/artifact"
	"agentk/internal/authz"
	"agentk/internal/deltabuf"
	"agentk/internal/discovery"
	"agentk/internal/execution"
	"agentk/internal/invocation"
	"agentk/internal/pipeline"
	"agentk/internal/service"
)

type embeddedWorker interface {
	Start() error
	Stop()
}

// startEmbeddedWorker 仅在 standalone 模式构造并启动 Worker。
func startEmbeddedWorker(
	mode Mode,
	taskQueue string,
	build func() embeddedWorker,
) (func(), error) {
	if mode != ModeStandalone {
		return func() {}, nil
	}
	w := build()
	if err := w.Start(); err != nil {
		return nil, fmt.Errorf("start embedded temporal worker: %w", err)
	}
	log.Printf("embedded Temporal worker started (task_queue=%s)", taskQueue)
	return w.Stop, nil
}

// RunAPI 启动 HTTP API，并在 standalone 模式同时启动 Worker。
func RunAPI() error {
	cfg, err := APIConfigFromEnv()
	if err != nil {
		return err
	}
	temporalCfg := temporalConfig{
		TaskQueue: temporalTaskQueue,
	}

	stores, closeStores, err := buildStores(
		context.Background(), cfg.Store, cfg.Store == storeMySQL)
	if err != nil {
		return err
	}
	defer closeStores()

	artifactStorage, err := buildArtifactStorage(cfg.Listen)
	if err != nil {
		return err
	}
	if err := validateArtifactStorage(cfg.Mode, artifactStorage); err != nil {
		return err
	}

	deltaBackend := "kedis"
	capabilityCatalogBackend := "kedis"
	// 两个组件当前随 Mode 选择相同 backend，但保持独立变量，避免以后误认为它们
	// 必须共享实现或生命周期。
	if cfg.Mode == ModeStandalone {
		deltaBackend = "mem"
		capabilityCatalogBackend = "mem"
	}
	deltas, err := deltabuf.New(deltaBackend)
	if err != nil {
		return err
	}
	capabilityDirectory, err := buildCapabilityCatalog(capabilityCatalogBackend)
	if err != nil {
		return err
	}
	profileRuntime, models, err := buildProfileRuntime(artifactStorage, capabilityDirectory)
	if err != nil {
		return err
	}

	appCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	temporalAPI, err := dialTemporal(context.Background())
	if err != nil {
		return err
	}
	defer temporalAPI.Close()

	agentLoop := agentloop.NewStarter(temporalAPI, temporalCfg.TaskQueue)
	stopEmbedded, err := startEmbeddedWorker(cfg.Mode, temporalCfg.TaskQueue, func() embeddedWorker {
		loopCfg := agentloop.Config{
			Store: stores.loop, Runtime: profileRuntime, WorkerID: workerID(),
			Deltas: deltas,
		}
		return newTemporalWorker(
			temporalAPI, temporalCfg, loopCfg, models, artifactStorage,
			pipeline.NewActivities(stores.pipeline, deltas),
			invocation.NewActivities(stores.invocation),
			discovery.NewActivities(capabilityDirectory))
	})
	if err != nil {
		return err
	}
	defer stopEmbedded()

	pipelines := pipeline.NewStarter(pipeline.StarterConfig{
		Temporal:  temporalAPI,
		TaskQueue: temporalCfg.TaskQueue,
		Catalog:   capabilityDirectory,
	})
	agentkService := service.New(service.Config{
		Store:          stores.service,
		AgentLoop:      agentLoop,
		Executions:     execution.NewCanceller(temporalAPI),
		ProfileRuntime: profileRuntime,
		Pipelines:      pipelines,
		Capabilities:   capabilityDirectory,
	})

	// AuthOff 使用固定开发身份；AuthAPIKey 从持久化 Store 验证请求。
	auth := authz.WithDevPrincipal
	if cfg.Auth == AuthAPIKey {
		verifier := authz.NewAPIKeyVerifier(stores.apiKeys)
		auth = func(next http.Handler) http.Handler {
			return authz.Middleware(next, verifier, []string{"/healthz"})
		}
	}
	handler := newRootHandler(api.ServerConfig{
		Service: agentkService,
		Deltas:  deltas,
	}, artifactStorage, auth)

	log.Printf("agentk-api listening on %s (mode=%s, store=%s, auth=%s, models catalog=%s)",
		cfg.Listen, cfg.Mode, cfg.Store, cfg.Auth, models.Current().Version)
	return serve(appCtx, cfg.Listen, handler)
}

// newRootHandler 组装 API、资源读取和健康检查路由，再统一应用鉴权中间件。
// healthz 由鉴权 allowlist 放行；生产和 E2E 共用相同路由及中间件顺序。
func newRootHandler(
	cfg api.ServerConfig, storage artifact.Storage, auth func(http.Handler) http.Handler,
) http.Handler {
	root := http.NewServeMux()
	root.Handle("/", api.NewServer(cfg).Handler())
	mountArtifactFiles(root, storage)
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return auth(root)
}

// serve 阻塞到 ctx 取消或监听失败；取消后为在途请求保留最多 10 秒关闭时间。
func serve(ctx context.Context, listen string, handler http.Handler) error {
	httpServer := &http.Server{Addr: listen, Handler: handler}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		err := <-serveErr
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// mountArtifactFiles 仅为 LocalStorage 挂载读取端点。
func mountArtifactFiles(mux *http.ServeMux, storage artifact.Storage) {
	local, ok := storage.(*artifact.LocalStorage)
	if !ok {
		return
	}
	files := http.StripPrefix("/files/", http.FileServer(http.Dir(local.Dir())))
	mux.Handle("GET /files/", files)
}

func buildArtifactStorage(listen string) (artifact.Storage, error) {
	return artifact.NewStorageFromEnv(listenBaseURL(listen))
}

// listenBaseURL 为本地文件 URL 生成回环地址；显式配置的外部地址由 artifact 包处理。
func listenBaseURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		return "http://127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
