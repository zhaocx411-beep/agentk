package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/mediatype"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
)

type blockState struct {
	Index int             `json:"index"`
	Block json.RawMessage `json:"block"`
}

func object(raw json.RawMessage) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, fmt.Errorf("expected a JSON object")
	}
	return value, nil
}

func requestParams(req chat.Request, config Config) (sdk.MessageNewParams, error) {
	p := sdk.MessageNewParams{Model: sdk.Model(req.Model), MaxTokens: config.MaxTokens}
	if err := config.apply(req.Model, &p); err != nil {
		return p, err
	}
	if req.Model == "" || len(req.Messages) == 0 {
		return p, fmt.Errorf("model and messages are required")
	}
	if req.System != "" {
		p.System = []sdk.TextBlockParam{{Text: req.System}}
	}
	if len(req.OutputSchema) > 0 {
		schema, err := object(req.OutputSchema)
		if err != nil {
			return p, fmt.Errorf("output schema: %w", err)
		}
		p.OutputConfig.Format = sdk.JSONOutputFormatParam{Schema: schema}
	}
	for _, tool := range req.Tools {
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		if _, err := object(schema); err != nil || tool.Name == "" {
			return p, fmt.Errorf("invalid tool definition %q", tool.Name)
		}
		p.Tools = append(p.Tools, sdk.ToolUnionParam{OfTool: &sdk.ToolParam{Name: tool.Name, Description: sdk.String(tool.Description), InputSchema: param.Override[sdk.ToolInputSchemaParam](schema)}})
	}
	ids := map[string]string{}
	for _, msg := range req.Messages {
		var content []sdk.ContentBlockParamUnion
		var err error
		switch msg.Role {
		case chat.RoleAssistant:
			content, err = assistantContent(msg)
			for _, call := range msg.ToolCalls {
				ids[call.ID] = callID(call)
			}
		case chat.RoleUser, chat.RoleTool:
			content, err = userContent(msg)
			if err == nil && msg.Role == chat.RoleTool {
				id := msg.ToolCallID
				if native := ids[id]; native != "" {
					id = native
				}
				if id == "" {
					return p, fmt.Errorf("tool result has no call ID")
				}
				content = []sdk.ContentBlockParamUnion{param.Override[sdk.ContentBlockParamUnion](map[string]any{"type": "tool_result", "tool_use_id": id, "content": content})}
			}
		default:
			return p, fmt.Errorf("unsupported role %q", msg.Role)
		}
		if err != nil {
			return p, err
		}
		if len(content) == 0 {
			return p, fmt.Errorf("empty message")
		}
		if msg.Role == chat.RoleAssistant {
			p.Messages = append(p.Messages, sdk.NewAssistantMessage(content...))
		} else if n := len(p.Messages); n > 0 && p.Messages[n-1].Role == "user" {
			p.Messages[n-1].Content = append(p.Messages[n-1].Content, content...)
		} else {
			p.Messages = append(p.Messages, sdk.NewUserMessage(content...))
		}
	}
	return p, nil
}

func userContent(msg chat.Message) ([]sdk.ContentBlockParamUnion, error) {
	content := []sdk.ContentBlockParamUnion{}
	if msg.Text != "" {
		content = append(content, sdk.NewTextBlock(msg.Text))
	}
	for _, media := range msg.Media {
		mime := mediatype.Canonical(media.MIMEType)
		if len(media.Data) == 0 || !slices.Contains([]string{"image/png", "image/jpeg", "image/webp", "image/gif"}, mime) {
			return nil, fmt.Errorf("image is unresolved or unsupported")
		}
		content = append(content, sdk.NewImageBlockBase64(mime, base64.StdEncoding.EncodeToString(media.Data)))
	}
	return content, nil
}

func callID(call chat.ToolCall) string {
	if call.ProviderID != "" {
		return call.ProviderID
	}
	return call.ID
}

func assistantContent(msg chat.Message) ([]sdk.ContentBlockParamUnion, error) {
	if len(msg.Media) > 0 {
		return nil, fmt.Errorf("assistant images are unsupported")
	}
	var states []blockState
	var content []sdk.ContentBlockParamUnion
	appendState := func(raw string, kinds ...string) error {
		var state blockState
		var block sdk.ContentBlockUnion
		if json.Unmarshal([]byte(raw), &state) != nil || json.Unmarshal(state.Block, &block) != nil || !slices.Contains(kinds, block.Type) {
			return fmt.Errorf("invalid Anthropic history state")
		}
		if err := validateBlock(block); err != nil {
			return err
		}
		states = append(states, state)
		return nil
	}
	for _, block := range msg.Blocks {
		if block.ProtocolState != "" {
			kinds := []string{"text"}
			if block.Kind == chat.BlockReasoning {
				kinds = []string{"thinking", "redacted_thinking"}
			}
			if err := appendState(block.ProtocolState, kinds...); err != nil {
				return nil, err
			}
		} else if block.Kind == chat.BlockText && block.Text != "" {
			content = append(content, sdk.NewTextBlock(block.Text))
		}
	}
	if len(msg.Blocks) == 0 && msg.Text != "" {
		content = append(content, sdk.NewTextBlock(msg.Text))
	}
	for _, call := range msg.ToolCalls {
		if call.ProtocolState != "" {
			if err := appendState(call.ProtocolState, "tool_use"); err != nil {
				return nil, err
			}
		} else {
			if _, err := object(call.Args); err != nil || callID(call) == "" || call.Name == "" {
				return nil, fmt.Errorf("invalid tool call")
			}
			content = append(content, sdk.NewToolUseBlock(callID(call), call.Args, call.Name))
		}
	}
	if len(states) > 0 {
		if len(content) > 0 {
			return nil, fmt.Errorf("incomplete Anthropic history state")
		}
		slices.SortFunc(states, func(a, b blockState) int { return a.Index - b.Index })
		for i, state := range states {
			if state.Index != i {
				return nil, fmt.Errorf("invalid Anthropic history order")
			}
			var block sdk.ContentBlockUnion
			if err := json.Unmarshal(state.Block, &block); err != nil {
				return nil, err
			}
			content = append(content, block.ToParam())
		}
	}
	return content, nil
}

func validateBlock(block sdk.ContentBlockUnion) error {
	switch block.Type {
	case "text":
	case "thinking":
		if block.Signature == "" {
			return fmt.Errorf("thinking block has no signature")
		}
	case "redacted_thinking":
		if block.Data == "" {
			return fmt.Errorf("redacted thinking block has no data")
		}
	case "tool_use":
		if _, err := object(block.Input); err != nil || block.ID == "" || block.Name == "" {
			return fmt.Errorf("invalid tool use block")
		}
	default:
		return fmt.Errorf("unsupported content block %q", block.Type)
	}
	return nil
}
