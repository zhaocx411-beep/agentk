package app

import (
	"context"
	"fmt"
	"log"

	temporalworker "go.temporal.io/sdk/worker"

	"agentk/internal/agentloop"
	"agentk/internal/artifact"
	"agentk/internal/deltabuf"
	"agentk/internal/discovery"
	"agentk/internal/invocation"
	"agentk/internal/pipeline"
)

// RunWorker 启动独立 Temporal Worker。该入口使用跨进程共享状态，不读取 API 的
// Mode、Auth 或 Listen；全部依赖和 Worker 构造完成后才调用 ready。
func RunWorker(ready func() error) error {
	cfg, err := WorkerConfigFromEnv()
	if err != nil {
		return err
	}
	temporalCfg := temporalConfig{TaskQueue: temporalTaskQueue}
	stores, closeStores, err := buildStores(context.Background(), cfg.Store, false)
	if err != nil {
		return err
	}
	defer closeStores()

	// 独立 Worker 没有文件 HTTP 端点，因此不能使用 LocalStorage。
	artifactStorage, err := artifact.NewStorageFromEnv("")
	if err != nil {
		return err
	}
	if err := validateArtifactStorage(ModeDeployed, artifactStorage); err != nil {
		return err
	}
	deltas, err := deltabuf.New("kedis")
	if err != nil {
		return err
	}
	capabilityDirectory, err := buildCapabilityCatalog("kedis")
	if err != nil {
		return err
	}
	profileRuntime, models, err := buildProfileRuntime(artifactStorage, capabilityDirectory)
	if err != nil {
		return err
	}
	temporalAPI, err := dialTemporal(context.Background())
	if err != nil {
		return err
	}
	defer temporalAPI.Close()

	id := workerID()
	w := newTemporalWorker(
		temporalAPI,
		temporalCfg,
		agentloop.Config{
			Store: stores.loop, Runtime: profileRuntime, WorkerID: id,
			Deltas: deltas,
		},
		models,
		artifactStorage,
		pipeline.NewActivities(stores.pipeline, deltas),
		invocation.NewActivities(stores.invocation),
		discovery.NewActivities(capabilityDirectory),
	)
	log.Printf("agentk-worker starting (task_queue=%s, worker_id=%s, store=%s, models catalog=%s)",
		temporalCfg.TaskQueue, id, cfg.Store, models.Current().Version)
	if err := ready(); err != nil {
		return fmt.Errorf("report worker ready: %w", err)
	}
	if err := w.Run(temporalworker.InterruptCh()); err != nil {
		return fmt.Errorf("run temporal worker: %w", err)
	}
	return nil
}
