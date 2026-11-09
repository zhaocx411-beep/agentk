// Package textgen 提供单次、不调用工具的文本生成能力。
package textgen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/heartbeat"
	"github.com/google/jsonschema-go/jsonschema"

	"agentk/internal/chat"
	"agentk/internal/temporalx"
)

const ActivityName = "agentk.capability.generate_text"

type Input struct {
	// Model 是必填的模型条目名。
	Model        string          `json:"model"`
	Prompt       string          `json:"prompt"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type Output struct {
	Result json.RawMessage `json:"result"`
}

type ModelLookup interface {
	ChatClient(name string) (chat.Client, bool)
}

type Activity struct{ models ModelLookup }

func NewActivity(models ModelLookup) *Activity {
	return &Activity{models: models}
}

func (a *Activity) Run(ctx context.Context, input Input) (Output, error) {
	const op = "capability generate_text"
	if a.models == nil {
		return Output{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("textgen model lookup is required")))
	}
	if input.Model == "" || input.Prompt == "" {
		return Output{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model and prompt are required"))
	}
	model, ok := a.models.ChatClient(input.Model)
	if !ok {
		return Output{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("text capability %q is not available", input.Model)))
	}

	var schema *jsonschema.Resolved
	prompt := input.Prompt
	if len(input.OutputSchema) > 0 {
		resolved, err := resolveSchema(input.OutputSchema)
		if err != nil {
			return Output{}, temporalx.ClassifyActivityError(op,
				temporalx.InvalidActivityInput("resolve output schema: %v", err))
		}
		schema = resolved
		prompt += "\n\n输出必须是符合以下 JSON Schema 的单个 JSON 值（注意根节点类型必须一致），只输出该 JSON，不要输出 schema 本身或其他文字：\n" + string(input.OutputSchema)
	}

	stop := heartbeat.Start(ctx)
	defer stop()
	resp, err := model.Stream(ctx, chat.Request{
		Messages:       []chat.Message{{Role: chat.RoleUser, Text: prompt}},
		IdempotencyKey: temporalx.ActivityIdempotencyKey(ctx),
	})
	if err != nil {
		return Output{}, temporalx.ClassifyActivityError(op+": model call", err)
	}
	for _, err := range resp.Stream {
		if err != nil {
			return Output{}, temporalx.ClassifyActivityError(op+": model stream", err)
		}
	}
	result, err := resp.Result()
	if err != nil {
		return Output{}, temporalx.ClassifyActivityError(op+": model response", err)
	}
	var b strings.Builder
	for _, block := range result.Blocks {
		if block.Kind == chat.BlockText {
			b.WriteString(block.Text)
		}
	}
	text := b.String()
	if schema == nil {
		raw, err := json.Marshal(text)
		if err != nil {
			return Output{}, temporalx.ClassifyActivityError(op, err)
		}
		return Output{Result: raw}, nil
	}
	raw := extractJSON(text)
	if err := validateJSON(schema, raw); err != nil {
		return Output{}, temporalx.ClassifyActivityError(op,
			fmt.Errorf("model output does not match outputSchema: %w", err))
	}
	return Output{Result: raw}, nil
}

func resolveSchema(raw json.RawMessage) (*jsonschema.Resolved, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	return schema.Resolve(nil)
}

func validateJSON(schema *jsonschema.Resolved, raw json.RawMessage) error {
	var value any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("invalid json: %w", err)
		}
	}
	return schema.Validate(value)
}

func extractJSON(text string) json.RawMessage {
	value := strings.TrimSpace(text)
	if strings.HasPrefix(value, "```") {
		if i := strings.Index(value, "\n"); i >= 0 {
			value = value[i+1:]
		}
		if i := strings.LastIndex(value, "```"); i >= 0 {
			value = value[:i]
		}
		value = strings.TrimSpace(value)
	}
	if i := strings.IndexAny(value, "{["); i > 0 {
		value = value[i:]
	}
	return json.RawMessage(value)
}
