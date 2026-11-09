// Package temporalx 是与业务无关的 Temporal 公共设施：Activity 错误分类与重试
// 策略、长任务轮询退避、对 Client/Worker 的最小契约。
//
// agentloop（会话推进）与 pipeline（低代码编排）是两个平级的编排域，它们同向
// 依赖本包、彼此不相互依赖——编排域之间共享的只能是这类底座，不能是对方。
// 分类清单只此一份：两个域对"什么错误值得重试"的答案不允许漂移。
package temporalx

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/workflow"
)

// Registry 是业务 workflow/activity 注册所需的最小 worker 契约。
// temporal worker.Worker 天然满足；保持窄接口便于装配测试。
type Registry interface {
	RegisterWorkflowWithOptions(w any, options workflow.RegisterOptions)
	RegisterActivityWithOptions(a any, options activity.RegisterOptions)
}
