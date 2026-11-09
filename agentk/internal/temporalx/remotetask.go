package temporalx

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

// 本文件是"提交-轮询式远端任务在单个 Activity 内收敛"的公共机制（官方 frequent
// polling 模式）：轮询循环在 Activity 内，等待期不写 History，heartbeat 保活。
//
// 关键纪律是跨 attempt 的续任务：提交成功即把任务身份记进 heartbeat details，
// 重试先取回——有任务就跳过提交直接续轮询。重执行一次生成花真金白银，这条纪律
// 不允许在各能力之间漂移，所以与错误分类同置本包。
//
// 不引入业务类型（能力包各自的 Poller 用闭包适配）：本包对业务无感知。

// RemoteTaskProgress 是 heartbeat details 载荷：已提交任务的身份与提交时刻。
// 提交时刻供轮询间隔按任务年龄自适应，且不因 Activity 重试而重置。
type RemoteTaskProgress struct {
	TaskID            string `json:"taskId"`
	SubmittedAtMillis int64  `json:"submittedAtMillis"`
}

// NewRemoteTaskProgress 记下一个刚提交的任务（提交时刻取当下）。
func NewRemoteTaskProgress(taskID string) RemoteTaskProgress {
	return RemoteTaskProgress{TaskID: taskID, SubmittedAtMillis: time.Now().UnixMilli()}
}

// LoadRemoteTaskProgress 取回上一 attempt 记下的任务进度；没有（首次执行）或
// 载荷不可解时返回零值，调用方据此决定是否提交。
func LoadRemoteTaskProgress(ctx context.Context) RemoteTaskProgress {
	if !activity.HasHeartbeatDetails(ctx) {
		return RemoteTaskProgress{}
	}
	var progress RemoteTaskProgress
	if err := activity.GetHeartbeatDetails(ctx, &progress); err != nil {
		return RemoteTaskProgress{}
	}
	return progress
}

// RecordRemoteTaskProgress 把任务进度写进 heartbeat（既保活又供重试续任务）。
func RecordRemoteTaskProgress(ctx context.Context, progress RemoteTaskProgress) {
	activity.RecordHeartbeat(ctx, progress)
}

// RemoteTaskStatus 是一次远端状态查询的结论（各能力的 Poller 结果归一到这里）。
type RemoteTaskStatus struct {
	Done          bool
	Failed        bool
	FailureReason string
	Result        []byte
}

// remoteTaskFailedErrorType 是远端任务终态失败的错误类型：同一个任务再怎么查
// 还是失败，重试没有意义。
const remoteTaskFailedErrorType = "RemoteTaskFailed"

// AwaitRemoteTask 轮询已提交的远端任务到终态并返回结果载荷。
//
// 每轮 heartbeat 携带 progress，间隔按任务年龄自适应（pollDelay 为 nil 时用
// DefaultJobPollBackoff）。瞬时轮询故障以可重试错误返回，交给 Activity 重试吸收
// （heartbeat details 保证续轮而非重新提交）；远端终态失败以不可重试错误返回。
func AwaitRemoteTask(
	ctx context.Context,
	progress RemoteTaskProgress,
	op string,
	poll func(ctx context.Context, taskID string) (RemoteTaskStatus, error),
	pollDelay func(age time.Duration) time.Duration,
) ([]byte, error) {
	if pollDelay == nil {
		pollDelay = func(age time.Duration) time.Duration {
			return JobPollDelay(age, DefaultJobPollBackoff)
		}
	}
	submittedAt := time.UnixMilli(progress.SubmittedAtMillis)
	if progress.SubmittedAtMillis <= 0 {
		submittedAt = time.Now()
	}
	for {
		status, err := poll(ctx, progress.TaskID)
		if err != nil {
			return nil, ClassifyActivityError(op+": poll remote task", err)
		}
		switch {
		case status.Failed:
			return nil, temporal.NewNonRetryableApplicationError(
				op+": remote task failed: "+status.FailureReason, remoteTaskFailedErrorType, nil)
		case status.Done:
			return status.Result, nil
		}
		RecordRemoteTaskProgress(ctx, progress)
		if err := sleepContext(ctx, pollDelay(time.Since(submittedAt))); err != nil {
			return nil, err
		}
	}
}

// RemoteTaskFailure 构造"远端任务失败"的不可重试错误（能力层在轮询之外确认失败时使用，
// 如任务完成但结果缺了必需字段）。
func RemoteTaskFailure(op, reason string) error {
	return temporal.NewNonRetryableApplicationError(
		op+": "+reason, remoteTaskFailedErrorType, nil)
}

// sleepContext 可被 ctx 取消的 sleep（Activity 取消经 heartbeat 传达后 ctx.Done
// 触发，轮询循环随之退出）。
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
