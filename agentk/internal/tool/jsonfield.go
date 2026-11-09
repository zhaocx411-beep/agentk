package tool

import (
	"bytes"
	"encoding/json"
	"strings"
)

// JSONField 按点分路径从 JSON 对象取字段（如 "data.task_id"）；路径为空返回原文。
func JSONField(raw []byte, path string) (json.RawMessage, bool) {
	if path == "" {
		return raw, true
	}
	cur := json.RawMessage(raw)
	for {
		key, rest, more := strings.Cut(path, ".")
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(cur, &obj); err != nil {
			return nil, false
		}
		v, ok := obj[key]
		if !ok {
			return nil, false
		}
		cur = v
		if !more {
			return cur, true
		}
		path = rest
	}
}

// JSONFieldString 取字段并转为字符串：string 原样、数字/布尔转字面量。
func JSONFieldString(raw []byte, path string) (string, bool) {
	v, ok := JSONField(raw, path)
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s, true
	}
	return string(bytes.TrimSpace(v)), true
}
