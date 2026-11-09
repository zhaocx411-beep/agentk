// studio Worker：注册 ComicDramaFilm workflow 与 call_agent 能力，监听独立
// Task Queue。经 KESS 服务发现连接 agent-k 所在的 Temporal 集群（服务名与
// namespace 由 SDK 固定），无需配置地址；KESS 依赖容器环境的 kess-agent。
//
// call_agent 的 activity 同样经 SDK KESS gRPC 自动发现 AgentK，为子 Agent
// 创建、驱动会话。AGENTK_LOCAL_BASE_URL 仅保留给本地 Fake AgentK 测试。
package main

import (
	"context"
	"log"
	"os"
	"strings"

	sdktemporal "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/temporal"
	agentkworker "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker"
	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
	"go.temporal.io/sdk/activity"
	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	routerworker "studio/agents/assistant/worker"
	dramaworker "studio/agents/comic-drama/worker"
	"studio/platform/chat"
	"studio/platform/core"
)

const dramaPipelineDescription = "把题材、梗概或多集剧本制作成漫剧成片（最多 10 集）：" +
	"分集剧本 → 人物定妆三视图 + 全剧固定音色 → 切集 → 逐集（资产描述 → 台词精简 → " +
	"分镜 → 资产图 → 逐镜首帧与视频 → 合成）→ 全剧成片。"

func main() {
	temporalClient, err := sdktemporal.Dial(context.Background())
	if err != nil {
		log.Fatalf("dial temporal: %v", err)
	}
	defer temporalClient.Close()

	agentkLocalBaseURL := strings.TrimSpace(os.Getenv("AGENTK_LOCAL_BASE_URL"))
	agentkTarget := "KESS gRPC"
	if agentkLocalBaseURL != "" {
		agentkTarget = agentkLocalBaseURL
	}
	agentkClient, err := core.NewAgentKClient(agentkLocalBaseURL)
	if err != nil {
		log.Fatalf("agentk client: %v", err)
	}
	callAgent := &routerworker.CallAgentActivities{
		Client:   agentkClient,
		Registry: chat.DefaultRegistry(),
		Heartbeat: func(ctx context.Context) {
			activity.RecordHeartbeat(ctx)
		},
	}

	temporalWorker := agentkworker.New(
		temporalClient, dramaworker.WorkerTaskQueue, temporalworker.Options{},
	)
	temporalWorker.RegisterWorkflowWithOptions(
		dramaworker.ComicDramaWorkflow,
		workflow.RegisterOptions{Name: dramaworker.WorkflowType},
	)
	// 单集子流程：只被父流程以 child workflow 启动（同一 Task Queue），
	// 不发布到 Catalog。
	temporalWorker.RegisterWorkflowWithOptions(
		dramaworker.ComicDramaEpisodeWorkflow,
		workflow.RegisterOptions{Name: dramaworker.EpisodeWorkflowType},
	)
	// 发布到 Catalog：是否作为 pipeline 暴露由 Session Profile 决定，Worker
	// 只发布 capability 的 Temporal 实现契约。
	temporalWorker.PublishWorkflow(sdkcapability.Capability{
		ID:           dramaworker.CapabilityID,
		TemporalType: dramaworker.WorkflowType,
		Description:  dramaPipelineDescription,
		InputSchema:  sdkcapability.SchemaOf[dramaworker.DramaInput](),
		OutputSchema: sdkcapability.SchemaOf[dramaworker.DramaOutput](),
		// 全片兜底超时 4 小时：常规一两集几十分钟完成；集数多且单集顶满
		// 2 小时兜底的极端场景会被截断，后续集数记超时错误。
		TimeoutSeconds: 4 * 60 * 60,
	})
	temporalWorker.RegisterWorkflowWithOptions(
		routerworker.CallAgentWorkflow,
		workflow.RegisterOptions{Name: routerworker.WorkflowType},
	)
	temporalWorker.RegisterActivityWithOptions(
		callAgent.Run,
		activity.RegisterOptions{Name: routerworker.ActivityName},
	)
	temporalWorker.PublishWorkflow(sdkcapability.Capability{
		ID:             routerworker.CapabilityID,
		TemporalType:   routerworker.WorkflowType,
		Description:    routerworker.ToolDescription,
		InputSchema:    sdkcapability.SchemaOf[routerworker.CallAgentInput](),
		OutputSchema:   sdkcapability.SchemaOf[routerworker.CallAgentOutput](),
		TimeoutSeconds: 60 * 60,
		Tool: &sdkcapability.Tool{
			Description: routerworker.ToolDescription,
		},
	})

	log.Printf(
		"worker started: task queue %q, workflows %q + %q, agentk %s",
		dramaworker.WorkerTaskQueue,
		dramaworker.WorkflowType,
		routerworker.WorkflowType,
		agentkTarget,
	)
	if err := temporalWorker.Run(temporalworker.InterruptCh()); err != nil {
		log.Fatalf("worker exited: %v", err)
	}
}
