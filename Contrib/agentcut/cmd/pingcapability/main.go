// pingcapability 是联调用命令行工具：启动一次 debug/pingcap.Workflow，
// 按 capability ID 转发调用并打印原始 JSON 结果。
//
// 用于在 contrib.ffmpeg_tool 等目标 capability 尚未确认上线时，先验证
// agentcut worker 自身到 Temporal 集群 / agent-k 路由这条链路是否打通——
// 选一个保证在线的内置 capability（如 agentk.web_search）做目标即可，
// 与目标 capability 是否上线无关。
//
// 前提：cmd/worker 已经以同一个 Temporal 集群配置在运行（同一份
// TEMPORAL_ADDRESS / TEMPORAL_NAMESPACE，或同样走 KESS 发现），因为本工具
// 只负责启动 Workflow，实际执行在 worker 进程里。
//
// 用法：
//
//	go run ./cmd/pingcapability -capability agentk.web_search \
//	  -input '{"model":"<staging 模型目录里的检索条目名>","query":"hello"}'
//
//	go run ./cmd/pingcapability -capability contrib.ffmpeg_tool -input '{}'
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"go.temporal.io/sdk/client"

	"agentcut/debug/pingcap"
	"agentcut/platform/temporalx"
	"agentcut/tools"
)

func main() {
	capabilityID := flag.String("capability", "", "要调用的 capability ID，例如 agentk.web_search")
	inputJSON := flag.String("input", "{}", "capability 入参，JSON 对象")
	timeout := flag.Duration("timeout", 60*time.Second, "等待结果的最长时间")
	flag.Parse()

	if *capabilityID == "" {
		log.Fatal("必须指定 -capability")
	}
	if !json.Valid([]byte(*inputJSON)) {
		log.Fatalf("-input 不是合法 JSON: %s", *inputJSON)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	temporalClient, err := temporalx.Dial(ctx)
	if err != nil {
		log.Fatalf("dial temporal: %v", err)
	}
	defer temporalClient.Close()

	workflowID := fmt.Sprintf("pingcap-%d", time.Now().UnixNano())
	run, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: tools.InternalTaskQueue,
	}, pingcap.WorkflowName, pingcap.Input{
		CapabilityID: *capabilityID,
		Input:        json.RawMessage(*inputJSON),
	})
	if err != nil {
		log.Fatalf("start workflow: %v (worker 进程是否已启动、是否连的同一个 Temporal 集群？)", err)
	}
	log.Printf("workflow started: id=%s runId=%s", run.GetID(), run.GetRunID())

	var out pingcap.Output
	if err := run.Get(ctx, &out); err != nil {
		log.Fatalf("workflow failed: %v", err)
	}

	var pretty map[string]any
	if err := json.Unmarshal(out.Output, &pretty); err == nil {
		if formatted, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			fmt.Println(string(formatted))
			return
		}
	}
	fmt.Println(string(out.Output))
}
