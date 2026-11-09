// AgentCut Temporal Worker：
//
//   - baseline 实例承载实体及媒体 Workflow/Activity；
//   - 所有实例在独立 capability 队列注册并发布 contrib.agentcut.* 工具；
//   - named-lane 实例只消费 lane capability 队列，不争抢内部状态任务。
//
// 生产环境经 KESS 自动发现 agent-k 所连的 Temporal 集群；本地开发设
// TEMPORAL_ADDRESS=localhost:7233 直连 dev server。
//
// AGENTCUT_RENDER_EXECUTOR=local|agentk chooses the executor recorded into new
// render inputs by cut-apiserver. The worker supports deterministic replay of
// both branches; unknown values fail startup.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	agentkworker "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"agentcut/debug/pingcap"
	"agentcut/entity"
	"agentcut/platform/agentkclient"
	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/platform/temporalx"
	"agentcut/preparemedia"
	"agentcut/render"
	"agentcut/separateaudio"
	"agentcut/tools"
)

func main() {
	ctx := context.Background()
	renderSelection, err := render.SelectionFromEnv()
	if err != nil {
		log.Fatalf("render executor: %v", err)
	}

	temporalClient, err := temporalx.Dial(ctx)
	if err != nil {
		log.Fatalf("dial temporal: %v", err)
	}
	defer temporalClient.Close()

	blobStore, err := blob.NewStoreFromEnv()
	if err != nil {
		log.Fatalf("blob store: %v", err)
	}
	if blobStore == nil {
		if renderSelection.Executor == render.ExecutorLocal {
			log.Fatal("blob store: local render executor requires AGENTCUT_BLOB_ENDPOINT/AGENTCUT_BLOB_BUCKET or AGENTCUT_BLOB_BACKEND=local")
		}
		log.Printf("WARN: blob storage not configured (AGENTCUT_BLOB_ENDPOINT/BUCKET); timeline snapshots will be skipped")
	}
	agentKClient, err := agentkclient.NewFromEnv()
	if err != nil {
		log.Fatalf("agent-k client: %v", err)
	}
	if agentKClient == nil {
		log.Printf("WARN: AGENTCUT_AGENTK_DISABLED=true; external Agent-K artifacts cannot be archived")
	} else {
		authCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		identity, authErr := validateAgentKAuth(authCtx, agentKClient)
		cancel()
		if authErr != nil {
			log.Fatalf("agent-k WhoAmI preflight: %v", authErr)
		}
		log.Printf("agent-k connection verified: appId=%q displayName=%q",
			identity.AppID, identity.DisplayName)
	}

	instanceLane, err := validateWorkerLaneEnvironment()
	if err != nil {
		log.Fatalf("worker lane: %v", err)
	}
	runInternal, internalTaskQueue, err := internalWorkerConfig(instanceLane, os.Getenv)
	if err != nil {
		log.Fatalf("internal worker: %v", err)
	}
	platformmedia.ConfigureFFmpegConcurrency(platformmedia.FFmpegConcurrencyFromEnv())
	workerOptions := temporalworker.Options{
		MaxConcurrentActivityExecutionSize: envPositiveInt("AGENTCUT_WORKER_MAX_ACTIVITIES", 32),
	}
	// This is a per-process cap. Deployment replica counts and task-queue
	// routing must also be bounded by the deployment configuration.
	if runInternal {
		internalWorker := temporalworker.New(temporalClient, internalTaskQueue, workerOptions)
		registerInternal(
			internalWorker, temporalClient, blobStore, agentKClient,
		)
		if err := internalWorker.Start(); err != nil {
			log.Fatalf("start internal worker: %v", err)
		}
		defer internalWorker.Stop()
		log.Printf("agentcut internal worker started: task queue=%q", internalTaskQueue)
	}

	capabilityWorker := agentkworker.New(
		temporalClient, tools.WorkerTaskQueue, workerOptions,
	)
	acts := &tools.Activities{Temporal: temporalClient, TaskQueue: internalTaskQueue}
	registerTools(capabilityWorker, acts)

	log.Printf(
		"agentcut capability worker started: baseTaskQueue=%q internalTaskQueue=%q renderExecutor=%s renderProfile=%s",
		tools.WorkerTaskQueue, internalTaskQueue, renderSelection.Executor, renderSelection.Profile,
	)
	if err := capabilityWorker.Run(temporalworker.InterruptCh()); err != nil {
		log.Fatalf("worker exited: %v", err)
	}
}

func internalWorkerConfig(
	instanceLane string,
	getenv func(string) string,
) (bool, string, error) {
	taskQueue := strings.TrimSpace(getenv("AGENTCUT_TASK_QUEUE"))
	if taskQueue == "" {
		taskQueue = tools.InternalTaskQueue
	}

	runInternal := instanceLane == ""
	switch raw := strings.ToLower(strings.TrimSpace(getenv("AGENTCUT_RUN_INTERNAL_WORKER"))); raw {
	case "":
	case "true":
		runInternal = true
	case "false":
		runInternal = false
	default:
		return false, "", fmt.Errorf(
			"AGENTCUT_RUN_INTERNAL_WORKER must be true or false",
		)
	}

	if runInternal && instanceLane != "" {
		if strings.TrimSpace(getenv("AGENTCUT_TASK_QUEUE")) == "" {
			return false, "", fmt.Errorf(
				"named-lane internal worker requires an explicit private AGENTCUT_TASK_QUEUE",
			)
		}
		if taskQueue == tools.InternalTaskQueue || taskQueue == tools.WorkerTaskQueue {
			return false, "", fmt.Errorf(
				"named-lane internal worker must not use shared AGENTCUT_TASK_QUEUE %q",
				taskQueue,
			)
		}
	}
	return runInternal, taskQueue, nil
}

func validateWorkerLaneEnvironment() (string, error) {
	if os.Getenv("KWS_LANE_ID") == "" &&
		strings.TrimSpace(os.Getenv("KWS_SERVICE_STAGE")) == "" {
		return "", fmt.Errorf(
			"KWS_SERVICE_STAGE is required for a baseline worker (use STAGING for staging), or set KWS_LANE_ID explicitly",
		)
	}
	lane, err := agentkworker.InstanceLaneFromEnv()
	if err != nil {
		return "", err
	}
	return string(lane), nil
}

func registerInternal(
	w temporalworker.Worker,
	temporalClient client.Client,
	blobStore blob.Storage,
	agentKClient *agentkclient.Client,
) {
	// 实体 Workflow：权威状态载体，不发布为 capability。
	w.RegisterWorkflowWithOptions(entity.TimelineEntityWorkflow,
		workflow.RegisterOptions{Name: entity.TimelineWorkflowName})
	w.RegisterWorkflowWithOptions(entity.UserProjectsWorkflow,
		workflow.RegisterOptions{Name: entity.UserProjectsWorkflowName})

	// 由 cut-apiserver 或工具 Activity 直接启动的内部 Workflow。
	w.RegisterWorkflowWithOptions(render.Workflow,
		workflow.RegisterOptions{Name: render.WorkflowName})
	w.RegisterWorkflowWithOptions(preparemedia.Workflow,
		workflow.RegisterOptions{Name: preparemedia.WorkflowName})
	w.RegisterWorkflowWithOptions(separateaudio.Workflow,
		workflow.RegisterOptions{Name: separateaudio.WorkflowName})
	w.RegisterWorkflowWithOptions(pingcap.Workflow,
		workflow.RegisterOptions{Name: pingcap.WorkflowName})

	snapshots := &blob.SnapshotActivities{Store: blobStore}
	w.RegisterActivityWithOptions(snapshots.SnapshotTimeline,
		activity.RegisterOptions{Name: entity.SnapshotActivityName})
	cleanup := &blob.CleanupActivities{Store: blobStore}
	w.RegisterActivityWithOptions(cleanup.DeleteMediaBlob,
		activity.RegisterOptions{Name: entity.DeleteMediaBlobActivityName})
	mediaPreparation := &preparemedia.Activities{
		Temporal: temporalClient, Store: blobStore, ArtifactVerifier: agentKClient,
	}
	w.RegisterActivityWithOptions(mediaPreparation.LoadMedia,
		activity.RegisterOptions{Name: preparemedia.LoadMediaActivityName})
	w.RegisterActivityWithOptions(mediaPreparation.ArchiveArtifact,
		activity.RegisterOptions{Name: preparemedia.ArchiveArtifactActivityName})
	w.RegisterActivityWithOptions(mediaPreparation.LocalArtifacts,
		activity.RegisterOptions{Name: preparemedia.LocalArtifactsActivityName})
	w.RegisterActivityWithOptions(mediaPreparation.UpdateMedia,
		activity.RegisterOptions{Name: preparemedia.UpdateMediaActivityName})
	audioSeparation := &separateaudio.Activities{Temporal: temporalClient, Store: blobStore}
	w.RegisterActivityWithOptions(audioSeparation.Extract,
		activity.RegisterOptions{Name: separateaudio.ExtractActivityName})
	w.RegisterActivityWithOptions(audioSeparation.Commit,
		activity.RegisterOptions{Name: separateaudio.CommitActivityName})
	w.RegisterActivityWithOptions(audioSeparation.Cleanup,
		activity.RegisterOptions{Name: separateaudio.CleanupActivityName})
	localRender := &render.LocalActivities{Store: blobStore}
	w.RegisterActivityWithOptions(localRender.Execute,
		activity.RegisterOptions{Name: render.LocalActivityName})
	w.RegisterActivityWithOptions(localRender.Cleanup,
		activity.RegisterOptions{Name: render.LocalCleanupActivityName})
}

type agentKAuthenticator interface {
	ValidateAuth(context.Context) (agentkclient.Identity, error)
}

func validateAgentKAuth(ctx context.Context, client agentKAuthenticator) (agentkclient.Identity, error) {
	identity, err := client.ValidateAuth(ctx)
	if err != nil {
		return agentkclient.Identity{}, err
	}
	if strings.TrimSpace(identity.AppID) == "" {
		return agentkclient.Identity{}, fmt.Errorf("Agent-K returned an empty app id")
	}
	return identity, nil
}

func envPositiveInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func registerTools(w *agentkworker.Worker, acts *tools.Activities) {
	for _, tool := range tools.Definitions() {
		w.RegisterActivityWithOptions(tool.Handler(acts), activity.RegisterOptions{Name: tool.Capability.TemporalType})
		w.PublishActivity(tool.Capability)
	}
}
