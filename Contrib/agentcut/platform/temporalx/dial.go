// Package temporalx 封装 Temporal 客户端连接：生产环境经 agentk-sdk 的 KESS
// 服务发现（依赖容器内 kess-agent），本地开发用 TEMPORAL_ADDRESS 环境变量
// 直连（如 localhost:7233）。
package temporalx

import (
	"context"
	"os"
	"strings"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/requestlane"
	sdktemporal "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/temporal"
	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/invocation"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

// Dial 建立 Temporal 连接。
//
//   - TEMPORAL_ADDRESS 已设置：直连该地址，namespace 取 TEMPORAL_NAMESPACE
//     （缺省 default），用于本地 Temporal dev server；
//   - 否则：走 agentk-sdk 的 KESS 发现，连 agent-k 所在集群（namespace agentk）。
//
// 两条路径都挂 invocation 与 request lane propagator，保证 agent-k 透传的
// 调用元数据（sessionId/tenantId）和请求泳道在我们自己的 Workflow 链路里不丢。
func Dial(ctx context.Context) (client.Client, error) {
	address := strings.TrimSpace(os.Getenv("TEMPORAL_ADDRESS"))
	if address == "" {
		return sdktemporal.Dial(ctx)
	}
	namespace := strings.TrimSpace(os.Getenv("TEMPORAL_NAMESPACE"))
	if namespace == "" {
		namespace = "default"
	}
	return client.DialContext(ctx, client.Options{
		HostPort:  address,
		Namespace: namespace,
		ContextPropagators: []workflow.ContextPropagator{
			invocation.MetaPropagator{},
			requestlane.TemporalPropagator{},
		},
	})
}
