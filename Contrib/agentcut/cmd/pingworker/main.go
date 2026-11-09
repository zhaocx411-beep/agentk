// pingworker 是联调专用的最小 Worker：只注册 debug/pingcap.Workflow，
// 不注册实体 Workflow、不注册/发布任何 contrib.agentcut.* 工具能力。
//
// 与 cmd/worker 的区别、以及为什么需要它：cmd/worker 用 agentk-sdk 的
// agentkworker.New + PublishActivity，进程一 Run() 起来就会立刻向 agent-k
// 所在集群"公告"一次 contrib.agentcut.* 的能力清单，并此后每隔约 15 分钟
// 续约一次——这正是 agentk-docs 能力目录页面看到的"上线"机制。在只是想验证
// capability.Invoke 出向调用链路（本机 -> agent-k 路由 -> 目标 capability）
// 是否打通时，我们不想顺带把本机变成 contrib.agentcut.* 在共享环境里的一个
// 真实实例（万一笔记本睡眠/断网，团队其他人在目录里看到的就是一个忽隐忽现
// 的能力）。本 Worker 直接用 go.temporal.io/sdk/worker，跳过 agentk-sdk 的
// 公告逻辑，只是单纯地"接活"，干净得多。
//
// 用法（需已可通过 KESS 或 TEMPORAL_ADDRESS 连到 agent-k 所在集群）：
//
//	go run ./cmd/pingworker
//
// 另开一个终端：
//
//	go run ./cmd/pingcapability -capability agentk.web_search -input '{...}'
package main

import (
	"context"
	"log"

	temporalworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"agentcut/debug/pingcap"
	"agentcut/platform/temporalx"
	"agentcut/tools"
)

func main() {
	ctx := context.Background()

	temporalClient, err := temporalx.Dial(ctx)
	if err != nil {
		log.Fatalf("dial temporal: %v", err)
	}
	defer temporalClient.Close()

	w := temporalworker.New(temporalClient, tools.InternalTaskQueue, temporalworker.Options{})
	w.RegisterWorkflowWithOptions(pingcap.Workflow,
		workflow.RegisterOptions{Name: pingcap.WorkflowName})

	log.Printf("pingworker started: task queue %q (no capability announce)", tools.InternalTaskQueue)
	if err := w.Run(temporalworker.InterruptCh()); err != nil {
		log.Fatalf("worker exited: %v", err)
	}
}
