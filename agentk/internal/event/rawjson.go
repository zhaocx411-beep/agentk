package event

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RawJSON 保存不由 event 包解释的 JSON，例如工具参数和结果；其具体类型由工具契约定义。
// 自定义编码避免 []byte 被写成 base64，并让零值稳定表示 JSON null。
type RawJSON []byte

// MarshalJSON 将零值编码为 null。
func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

func (r *RawJSON) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*r = nil
		return nil
	}
	*r = append((*r)[:0], b...)
	return nil
}

// Into 将非空 payload 解码到 v；零值或 null 表示没有结果，不能静默解成零值对象。
func (r RawJSON) Into(v any) error {
	if len(r) == 0 {
		return errors.New("event: raw json is empty")
	}
	if err := json.Unmarshal(r, v); err != nil {
		return fmt.Errorf("event: decode raw json into %T: %w", v, err)
	}
	return nil
}

// String 返回原始 JSON；零值返回 null。
func (r RawJSON) String() string {
	if len(r) == 0 {
		return "null"
	}
	return string(r)
}
