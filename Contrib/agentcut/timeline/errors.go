package timeline

import "fmt"

// ErrorCode 是稳定的机器可读错误码，前端与 Agent Tool 层据此分支。
type ErrorCode string

const (
	ErrBadRequest          ErrorCode = "BAD_REQUEST"         // 形状/字段非法
	ErrNotFound            ErrorCode = "NOT_FOUND"           // 引用的 item/track/media 不存在
	ErrConflictingID       ErrorCode = "CONFLICTING_ID"      // ID 已存在
	ErrOverlap             ErrorCode = "OVERLAP"             // 同轨重叠
	ErrLockedTrack         ErrorCode = "LOCKED_TRACK"        // 轨道被锁定
	ErrOutOfRange          ErrorCode = "OUT_OF_RANGE"        // 帧坐标越界
	ErrKindMismatch        ErrorCode = "KIND_MISMATCH"       // item/track/media 类别不匹配
	ErrLimitExceeded       ErrorCode = "LIMIT_EXCEEDED"      // 超出规模上限
	ErrStillReferenced     ErrorCode = "STILL_REFERENCED"    // media 仍被 item 引用
	ErrUnsupported         ErrorCode = "UNSUPPORTED"         // schemaVersion / op 不支持
	ErrFPSLocked           ErrorCode = "FPS_LOCKED"          // 非空 Timeline 禁改 fps
	ErrInvariant           ErrorCode = "INVARIANT_VIOLATION" // 批量应用后全文档校验失败
	ErrInvalidTransition   ErrorCode = "INVALID_TRANSITION"
	ErrInsufficientHandle  ErrorCode = "INSUFFICIENT_HANDLE"
	ErrInvalidDucking      ErrorCode = "INVALID_DUCKING"
	ErrInvalidCaptionStyle ErrorCode = "INVALID_CAPTION_STYLE"
)

// OpError 描述一批 Ops 中第 Index 个操作失败。整批不生效。
type OpError struct {
	Index   int       `json:"index"` // 失败 op 在批次中的下标；批级错误为 -1
	Op      string    `json:"op"`    // 失败 op 名；批级错误为空
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *OpError) Error() string {
	if e.Index < 0 {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("op[%d] %s: %s: %s", e.Index, e.Op, e.Code, e.Message)
}

func opErrf(index int, op string, code ErrorCode, format string, args ...any) *OpError {
	return &OpError{Index: index, Op: op, Code: code, Message: fmt.Sprintf(format, args...)}
}

func batchErrf(code ErrorCode, format string, args ...any) *OpError {
	return &OpError{Index: -1, Code: code, Message: fmt.Sprintf(format, args...)}
}
