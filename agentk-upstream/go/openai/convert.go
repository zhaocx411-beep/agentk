package openai

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/mediatype"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func buildParams(model string, req chat.Request) (sdk.ChatCompletionNewParams, error) {
	if len(req.OutputSchema) > 0 {
		return sdk.ChatCompletionNewParams{}, fmt.Errorf("output schema is not implemented for Chat Completions")
	}
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
		Model:         shared.ChatModel(model),
		Messages:      messages,
		Tools:         tools,
		StreamOptions: sdk.ChatCompletionStreamOptionsParam{IncludeUsage: sdk.Bool(true)},
	}, nil
}

func buildMessage(m chat.Message) (sdk.ChatCompletionMessageParamUnion, error) {
	switch m.Role {
	case chat.RoleUser:
		if len(m.Media) > 0 {
			parts := make([]sdk.ChatCompletionContentPartUnionParam, 0, len(m.Media)+1)
			if m.Text != "" {
				parts = append(parts, sdk.TextContentPart(m.Text))
			}
			for i, image := range m.Media {
				if len(image.Data) == 0 || !mediatype.SupportedImage(image.MIMEType) {
					return sdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("image %d is not resolved", i)
				}
				parts = append(parts, sdk.ImageContentPart(
					sdk.ChatCompletionContentPartImageImageURLParam{
						URL: "data:" + image.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(image.Data),
					},
				))
			}
			return sdk.UserMessage(parts), nil
		}
		return sdk.UserMessage(m.Text), nil
	case chat.RoleTool:
		if m.ToolCallID == "" {
			return sdk.ChatCompletionMessageParamUnion{},
				fmt.Errorf("tool message without tool call id")
		}
		if len(m.Media) > 0 {
			return sdk.ChatCompletionMessageParamUnion{},
				fmt.Errorf("OpenAI-compatible chat does not support images in tool results")
		}
		return sdk.ToolMessage(m.Text, m.ToolCallID), nil
	case chat.RoleAssistant:
		// 该协议不接受 assistant 输入中的 reasoning_content。
		msg := sdk.ChatCompletionAssistantMessageParam{}
		if m.Text != "" {
			msg.Content.OfString = sdk.String(m.Text)
		}
		for _, call := range m.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, sdk.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &sdk.ChatCompletionMessageFunctionToolCallParam{
					ID: providerToolCallID(call),
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

func providerToolCallID(call chat.ToolCall) string {
	if call.ProviderID != "" {
		return call.ProviderID
	}
	return call.ID
}

func argsText(args json.RawMessage) string {
	if s := strings.TrimSpace(string(args)); s != "" {
		return s
	}
	return "{}"
}

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
