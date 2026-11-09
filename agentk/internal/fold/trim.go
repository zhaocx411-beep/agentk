package fold

import (
	"encoding/json"
	"fmt"
	"log"

	"agentk/internal/tool"
)

// trimField 截断结果对象中 trim.Field 指向的字符串。
// 历史瘦身不是正确性要求；无法识别的结构、非字符串字段或无需截断时必须原样返回，
// 不能为了节省上下文改坏工具结果。
func trimField(raw json.RawMessage, trim tool.HistoryTrim, toolName string) (json.RawMessage, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, false
	}
	field, ok := obj[trim.Field]
	if !ok {
		return raw, false
	}
	var text string
	if err := json.Unmarshal(field, &text); err != nil {
		return raw, false
	}
	runes := []rune(text)
	if len(runes) <= trim.MaxChars {
		return raw, false
	}
	// 在字段内标记截断，避免模型将截断位置解释为原始文本结尾。
	kept := string(runes[:trim.MaxChars]) + fmt.Sprintf(
		"\n\n…（后续 %d 字符已在历史回放中省略。需要完整内容请重新调用 %s。）",
		len(runes)-trim.MaxChars, toolName)
	packed, err := json.Marshal(kept)
	if err != nil {
		return raw, false
	}
	obj[trim.Field] = packed
	out, err := json.Marshal(obj)
	if err != nil {
		log.Printf("fold: trim %s history result: %v", toolName, err)
		return raw, false
	}
	return out, true
}
