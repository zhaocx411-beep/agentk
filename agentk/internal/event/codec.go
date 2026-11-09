package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"agentk/internal/types"
)

var ErrUnknownKind = errors.New("event: unknown kind")

// MarshalPayload 编码 payload 本体；kind 由 Payload.Kind 单独提供。
func MarshalPayload(p Payload) (json.RawMessage, error) {
	if p == nil {
		return nil, fmt.Errorf("event: nil payload")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("event: marshal %s: %w", p.Kind(), err)
	}
	return raw, nil
}

// UnmarshalPayload 按 kind 解码 payload。存储读取和对外 envelope 共用此唯一判别分支，
// 避免两套协议映射漂移。调用方可以单独识别未知 kind；已知 kind 的损坏数据始终报错。
func UnmarshalPayload(kind Kind, data []byte) (Payload, error) {
	switch kind {
	case KindUserInput:
		return decodeInto[UserInput](data, kind)
	case KindRoundStarted:
		return decodeInto[RoundStarted](data, kind)
	case KindRoundCompleted:
		return decodeInto[RoundCompleted](data, kind)
	case KindAssistantText:
		return decodeInto[AssistantText](data, kind)
	case KindAssistantReasoning:
		return decodeInto[AssistantReasoning](data, kind)
	case KindAssistantToolCall:
		return decodeInto[AssistantToolCall](data, kind)
	case KindToolExecStarted:
		return decodeInto[ToolExecStarted](data, kind)
	case KindToolExecSucceeded:
		return decodeInto[ToolExecSucceeded](data, kind)
	case KindToolExecFailed:
		return decodeInto[ToolExecFailed](data, kind)
	case KindAskUserRequested:
		return decodeInto[AskUserRequested](data, kind)
	case KindAskUserResponded:
		return decodeInto[AskUserResponded](data, kind)
	case KindExecutionStarted:
		return decodeInto[ExecutionStarted](data, kind)
	case KindInvocationProgress:
		return decodeInto[InvocationProgress](data, kind)
	case KindExecutionFinished:
		return decodeInto[ExecutionFinished](data, kind)
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownKind, kind)
}

// decodeInto 按值返回 payload，以匹配 Payload 联合的值类型成员。
func decodeInto[T Payload](data []byte, kind Kind) (Payload, error) {
	var p T
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("event: decode %s: %w", kind, err)
	}
	return p, nil
}

// wire 是事件的 JSON envelope，不包含仅用于存储幂等的内部 BatchID。
type wire struct {
	ID          EventID           `json:"eventId"`
	ExecutionID types.ExecutionID `json:"executionId"`
	RoundID     RoundID           `json:"roundId,omitempty"`
	Kind        Kind              `json:"kind"`
	Data        json.RawMessage   `json:"data"`
	CreatedAt   time.Time         `json:"createdAt"`
}

func (e Event) MarshalJSON() ([]byte, error) {
	data, err := MarshalPayload(e.Payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire{
		ID:          e.ID,
		ExecutionID: e.ExecutionID,
		RoundID:     e.RoundID,
		Kind:        e.Payload.Kind(),
		Data:        data,
		CreatedAt:   e.CreatedAt,
	})
}

//goland:noinspection GoMixedReceiverTypes
func (e *Event) UnmarshalJSON(b []byte) error {
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	payload, err := UnmarshalPayload(w.Kind, w.Data)
	if err != nil {
		return err
	}
	e.ID, e.ExecutionID, e.RoundID, e.Payload, e.CreatedAt =
		w.ID, w.ExecutionID, w.RoundID, payload, w.CreatedAt
	return nil
}
