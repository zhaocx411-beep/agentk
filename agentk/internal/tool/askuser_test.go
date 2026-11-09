package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestParseAskQuestions 验证 ID 补全、空白处理和选项规范化。
func TestParseAskQuestions(t *testing.T) {
	got, err := ParseAskQuestions(json.RawMessage(`{"questions":[
		{"label":" 画风 ","question":" 想要什么画风？ ","options":["写实（推荐）"," 二次元 "]},
		{"id":"scene","question":"要哪几个场景？","options":["清晨","黄昏","夜景"],"multiSelect":true}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("questions = %d, want 2", len(got))
	}
	first := got[0]
	if first.ID != "q1" || first.Label != "画风" || first.Question != "想要什么画风？" {
		t.Fatalf("question[0] = %+v", first)
	}
	// 选项仅去除首尾空白。
	if len(first.Options) != 2 || first.Options[1] != "二次元" {
		t.Fatalf("options = %v", first.Options)
	}
	if first.MultiSelect {
		t.Fatal("question[0] must be single-select by default")
	}
	// 显式 ID 保持不变。
	if got[1].ID != "scene" || !got[1].MultiSelect {
		t.Fatalf("question[1] = %+v", got[1])
	}
}

// TestParseAskQuestionsRejects 覆盖无效题目数量、ID 和选项。
func TestParseAskQuestionsRejects(t *testing.T) {
	cases := map[string]string{
		"无问题":   `{"questions":[]}`,
		"缺字段":   `{}`,
		"坏JSON": `{"questions":`,
		"空问题":   `{"questions":[{"question":"   ","options":["A","B"]}]}`,
		"单选项":   `{"questions":[{"question":"要不要做？","options":["要"]}]}`,
		"无选项":   `{"questions":[{"question":"要不要做？"}]}`,
		"选项过多":  `{"questions":[{"question":"选一个","options":["1","2","3","4","5","6","7"]}]}`,
		"选项重复":  `{"questions":[{"question":"选一个","options":["A","A"]}]}`,
		"空选项":   `{"questions":[{"question":"选一个","options":["A",""]}]}`,
		"id 重复": `{"questions":[
			{"id":"x","question":"q1","options":["A","B"]},{"id":"x","question":"q2","options":["A","B"]}
		]}`,
		"问题过多": `{"questions":[
			{"question":"q1","options":["A","B"]},{"question":"q2","options":["A","B"]},
			{"question":"q3","options":["A","B"]},{"question":"q4","options":["A","B"]},
			{"question":"q5","options":["A","B"]},{"question":"q6","options":["A","B"]}
		]}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAskQuestions(json.RawMessage(input)); err == nil {
				t.Fatalf("input %s must be rejected", input)
			}
		})
	}
}

// TestAskUserRunIsGuarded 验证 UserBinding 不能通过 Spec.Run 执行。
func TestAskUserRunIsGuarded(t *testing.T) {
	_, err := NewAskUserTool().Run(context.Background(),
		json.RawMessage(`{"questions":[{"question":"选哪个？","options":["A","B"]}]}`), Context{})
	if err == nil || !strings.Contains(err.Error(), "workflow") {
		t.Fatalf("err = %v, want a routing-guard error", err)
	}
}

// TestAskUserSpec 验证 ask_user 使用 UserBinding。
func TestAskUserSpec(t *testing.T) {
	spec := NewAskUserTool()
	if spec.Name != AskUserToolName {
		t.Fatalf("spec = %+v", spec)
	}
	if _, ok := spec.Binding.(UserBinding); !ok {
		t.Fatalf("Binding = %T, want UserBinding", spec.Binding)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties struct {
			Questions struct {
				Items struct {
					Required   []string                   `json:"required"`
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"items"`
			} `json:"questions"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "questions" {
		t.Fatalf("required = %v", schema.Required)
	}
	item := schema.Properties.Questions.Items
	if len(item.Required) != 2 {
		t.Fatalf("question required = %v, want question + options", item.Required)
	}
	for _, name := range []string{"id", "label", "question", "options", "multiSelect"} {
		if _, ok := item.Properties[name]; !ok {
			t.Fatalf("question schema missing %q", name)
		}
	}
}
