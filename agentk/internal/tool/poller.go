package tool

import (
	"context"
	"encoding/json"
)

// PollResult 是一次远端任务状态查询的结论。
type PollResult struct {
	// Done 表示任务成功，Result 为最终结果。
	Done   bool
	Result json.RawMessage
	// Failed 表示业务终态失败；瞬时查询故障通过 error 返回。
	Failed        bool
	FailureReason string
	// StatusDetail 是可选的远端状态原值，供未完成任务展示进度。
	StatusDetail string
}

// Poller 查询远端任务状态；error 表示网络等瞬时查询故障，由调用方退避重试。
type Poller interface {
	Poll(ctx context.Context, remoteTaskID string) (PollResult, error)
}
