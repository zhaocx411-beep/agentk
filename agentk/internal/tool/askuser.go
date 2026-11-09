package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"agentk/internal/event"
)

// AskUserToolName 内置澄清提问工具名。
const AskUserToolName = "ask_user"

const (
	// maxAskQuestions 保证一组澄清问题能在一次交互中完成。
	maxAskQuestions = 5
	// minAskOptions 要求选择题至少有两个有效选项。
	minAskOptions = 2
	// maxAskOptions 限制单题选项数量。
	maxAskOptions = 6
)

// NewAskUserTool 构造由 AskUserActivity 处理的 UserBinding 工具。
// Spec 只提供模型契约和路由标签，不包含执行函数；Activity 登记问题和事件后结束
// 当前 AgentWorkflow，后续回答再启动新的推进。
func NewAskUserTool() Spec {
	return Spec{
		Name: AskUserToolName,
		Description: "向用户提问并给出可选项，等用户拍板后再继续。调用后本轮立即结束——" +
			"你不会在这一轮看到答案，用户选完会作为这次调用的结果回来，届时你被重新唤起。" +
			"用在需求有多种合理解法、且不同选择会导向不同产出时：风格路线、范围边界、素材取舍等。" +
			"每题给 2-6 个具体选项（不要「其他」「都行」这类占位，用户本来就能自己输入），" +
			"推荐项在文案末尾加「（推荐）」。多个疑问一次问完，不要连着问好几轮。" +
			"必须单独调用，不能和其他工具在同一轮里一起调。" +
			"能从上下文推断、或猜错了代价很小的事情不要问——直接做，做完让用户改。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["questions"],
			"properties": {
				"questions": {
					"type": "array",
					"minItems": 1,
					"maxItems": 5,
					"description": "一次要问的问题（把相关疑问一次问完）",
					"items": {
						"type": "object",
						"required": ["question", "options"],
						"properties": {
							"id": {"type": "string", "description": "该题的稳定标识，可选；不给按 q1..qn 自动补"},
							"label": {"type": "string", "description": "给界面用的短标题（如「画风」），可选"},
							"question": {"type": "string", "description": "问题正文，说清楚在选什么"},
							"options": {
								"type": "array", "minItems": 2, "maxItems": 6,
								"items": {"type": "string"},
								"description": "候选项文案；推荐项末尾加「（推荐）」。用户也可以不选、自己写"
							},
							"multiSelect": {"type": "boolean", "description": "true 时允许多选；缺省单选"}
						}
					}
				}
			}
		}`),
		Binding: UserBinding{},
	}
}

// ParseAskQuestions 解析并规范化 ask_user 参数。
func ParseAskQuestions(input json.RawMessage) ([]event.Question, error) {
	var in struct {
		Questions []event.Question `json:"questions"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("ask_user: 入参不是合法 JSON：%w", err)
	}
	return normalizeAskQuestions(in.Questions)
}

// normalizeAskQuestions 校验提问、补充 ID 并去除首尾空白。
// 校验错误会作为工具错误返回给模型修正；无效问题不得登记成等待用户的 interaction。
func normalizeAskQuestions(questions []event.Question) ([]event.Question, error) {
	if len(questions) == 0 {
		return nil, fmt.Errorf("ask_user: questions 不能为空；没有要澄清的事就别调用本工具，直接做")
	}
	if len(questions) > maxAskQuestions {
		return nil, fmt.Errorf("ask_user: 一次最多问 %d 个问题，收到 %d 个；只留真正影响产出的那几个",
			maxAskQuestions, len(questions))
	}
	out := make([]event.Question, 0, len(questions))
	seenID := make(map[string]bool, len(questions))
	for i, q := range questions {
		text := strings.TrimSpace(q.Question)
		if text == "" {
			return nil, fmt.Errorf("ask_user: questions[%d].question 不能为空", i)
		}
		if len(q.Options) < minAskOptions {
			return nil, fmt.Errorf("ask_user: questions[%d] 只有 %d 个选项，至少要 %d 个；"+
				"没有可选项说明这不是选择题，直接把问题写进正文",
				i, len(q.Options), minAskOptions)
		}
		if len(q.Options) > maxAskOptions {
			return nil, fmt.Errorf("ask_user: questions[%d] 有 %d 个选项，最多 %d 个；选项过多就该把问题拆开问",
				i, len(q.Options), maxAskOptions)
		}
		options := make([]string, 0, len(q.Options))
		seenOpt := make(map[string]bool, len(q.Options))
		for j, opt := range q.Options {
			opt = strings.TrimSpace(opt)
			if opt == "" {
				return nil, fmt.Errorf("ask_user: questions[%d].options[%d] 不能为空", i, j)
			}
			if seenOpt[opt] {
				return nil, fmt.Errorf("ask_user: questions[%d] 选项 %q 重复", i, opt)
			}
			seenOpt[opt] = true
			options = append(options, opt)
		}
		// ID 用于关联回答；缺失时按位置生成且必须唯一。
		id := strings.TrimSpace(q.ID)
		if id == "" {
			id = fmt.Sprintf("q%d", i+1)
		}
		if seenID[id] {
			return nil, fmt.Errorf("ask_user: questions[%d] 的 id %q 与前面重复", i, id)
		}
		seenID[id] = true
		out = append(out, event.Question{
			ID:          id,
			Label:       strings.TrimSpace(q.Label),
			Question:    text,
			Options:     options,
			MultiSelect: q.MultiSelect,
		})
	}
	return out, nil
}
