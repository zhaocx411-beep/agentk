package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"agentk/internal/chat"
)

// buildParams 将 chat Request 转换为 /chat/completions 参数。
func buildParams(model string, req chat.Request) (sdk.ChatCompletionNewParams, error) {
	messages := make([]sdk.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, sdk.SystemMessage(req.System))
	}
	for i, m := range req.Messages {
		converted, err := buildMessage(m)
		if err != nil {
			return sdk.ChatCompletionNewParams{}, fmt.Errorf("message %d: %w", i, err)
		}
		messages = append(messages, converted)
	}
	tools, err := buildTools(req.Tools)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, err
	}
	return sdk.ChatCompletionNewParams{
		Model:    shared.ChatModel(model),
		Messages: messages,
		Tools:    tools,
		// IncludeUsage 使最后一个 chunk 包含 TokenUsage。
		StreamOptions: sdk.ChatCompletionStreamOptionsParam{IncludeUsage: sdk.Bool(true)},
	}, nil
}

func buildMessage(m chat.Message) (sdk.ChatCompletionMessageParamUnion, error) {
	switch m.Role {
	case chat.RoleUser:
		return sdk.UserMessage(m.Text), nil
	case chat.RoleTool:
		if m.ToolCallID == "" {
			return sdk.ChatCompletionMessageParamUnion{},
				fmt.Errorf("tool message without tool call id")
		}
		return sdk.ToolMessage(m.Text, m.ToolCallID), nil
	case chat.RoleAssistant:
		// reasoning_content 是响应侧 provider 扩展，/chat/completions 不接受它作为
		// assistant 输入。Reasoning 仍保留在中立 IR，供支持回传的其他 adapter 使用。
		msg := sdk.ChatCompletionAssistantMessageParam{}
		if m.Text != "" {
			msg.Content.OfString = sdk.String(m.Text)
		}
		for _, call := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, sdk.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &sdk.ChatCompletionMessageFunctionToolCallParam{
					ID: call.ID,
					Function: sdk.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      call.Name,
						Arguments: argsText(call.Args),
					},
				},
			})
		}
		return sdk.ChatCompletionMessageParamUnion{OfAssistant: &msg}, nil
	default:
		return sdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("unsupported role %q", m.Role)
	}
}

// argsText 将空 tool call 参数规范化为 {}。
func argsText(args json.RawMessage) string {
	if s := strings.TrimSpace(string(args)); s != "" {
		return s
	}
	return "{}"
}

// toolArgs 将模型返回的空参数规范化为 {}。
func toolArgs(raw string) json.RawMessage {
	if s := strings.TrimSpace(raw); s != "" {
		return json.RawMessage(s)
	}
	return json.RawMessage(`{}`)
}

func buildTools(defs []chat.ToolDef) ([]sdk.ChatCompletionToolUnionParam, error) {
	if len(defs) == 0 {
		return nil, nil
	}
	out := make([]sdk.ChatCompletionToolUnionParam, 0, len(defs))
	for _, def := range defs {
		fn := shared.FunctionDefinitionParam{Name: def.Name}
		if def.Description != "" {
			fn.Description = sdk.String(def.Description)
		}
		if len(def.InputSchema) > 0 {
			// SDK 参数要求 map，因此在此解码 JSON Schema。
			var params map[string]any
			if err := json.Unmarshal(def.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("tool %s: decode input schema: %w", def.Name, err)
			}
			fn.Parameters = params
		}
		out = append(out, sdk.ChatCompletionFunctionTool(fn))
	}
	return out, nil
}
