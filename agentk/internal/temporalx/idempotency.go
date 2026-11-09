package temporalx

import (
	"context"

	"go.temporal.io/sdk/activity"
)

// ActivityIdempotencyKey 返回一次逻辑 activity 调用的稳定标识。RunID 隔离同一
// WorkflowID 的不同 Run（包括 workflow 结束后重新启动和 continue-as-new）；
// ActivityID 在单个 Run 的 History 中确定，attempt 重试不变。
func ActivityIdempotencyKey(ctx context.Context) (key string) {
	defer func() {
		if recover() != nil {
			key = "non-temporal/activity"
		}
	}()
	info := activity.GetInfo(ctx)
	if info.WorkflowExecution.ID == "" ||
		info.WorkflowExecution.RunID == "" ||
		info.ActivityID == "" {
		return "non-temporal/activity"
	}
	return info.WorkflowExecution.ID + "/" +
		info.WorkflowExecution.RunID + "/" +
		info.ActivityID
}
