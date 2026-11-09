package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/mediatype"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

type ResponsesClient struct{ sdk sdk.Client }

func NewResponses(r gateway.Route, opts ...upstream.Option) (*ResponsesClient, error) {
	base, client, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &ResponsesClient{sdk: sdk.NewClient(
		option.WithBaseURL(strings.TrimRight(base.String(), "/")+"/"),
		option.WithHTTPClient(client), option.WithMaxRetries(0),
	)}, nil
}

func (c *ResponsesClient) Stream(ctx context.Context, req chat.Request) (chat.Response, error) {
	p, err := responsesParams(req)
	if err != nil {
		return chat.Response{}, fmt.Errorf("responses: %v: %w", err, chat.ErrRejected)
	}
	var opts []option.RequestOption
	if req.IdempotencyKey != "" {
		opts = append(opts, option.WithHeader("Idempotency-Key", req.IdempotencyKey))
	}
	var out chat.Result
	var completed bool
	var streamErr error
	return chat.Response{
		Stream: func(yield func(chat.Delta, error) bool) {
			stream := c.sdk.Responses.NewStreaming(ctx, p, opts...)
			defer stream.Close()
			blocks := map[int64]int{}
			block := func(index int64, kind chat.BlockKind) int {
				if i, ok := blocks[index]; ok {
					return i
				}
				i := len(out.Blocks)
				blocks[index] = i
				out.Blocks = append(out.Blocks, chat.Block{Kind: kind})
				return i
			}
			for stream.Next() {
				e := stream.Current()
				switch e.Type {
				case "response.output_item.added":
					if e.Item.Type == "message" {
						block(e.OutputIndex, chat.BlockText)
					} else if e.Item.Type == "reasoning" {
						block(e.OutputIndex, chat.BlockReasoning)
					}
				case "response.output_text.delta", "response.refusal.delta", "response.reasoning_summary_text.delta":
					kind := chat.BlockText
					if e.Type == "response.reasoning_summary_text.delta" {
						kind = chat.BlockReasoning
					}
					if !yield(chat.Delta{Block: block(e.OutputIndex, kind), Kind: kind, Text: e.Delta}, nil) {
						return
					}
				case "response.completed":
					for index, item := range e.Response.Output {
						state, err := json.Marshal(responseItemState{Index: index, Item: json.RawMessage(item.RawJSON())})
						if err != nil {
							streamErr = err
							break
						}
						switch item.Type {
						case "message":
							var text strings.Builder
							for _, part := range item.Content {
								text.WriteString(part.Text)
								text.WriteString(part.Refusal)
							}
							i := block(int64(index), chat.BlockText)
							out.Blocks[i] = chat.Block{Kind: chat.BlockText, Text: text.String(), ProtocolState: string(state)}
						case "reasoning":
							var summary strings.Builder
							for _, part := range item.Summary {
								summary.WriteString(part.Text)
							}
							i := block(int64(index), chat.BlockReasoning)
							out.Blocks[i] = chat.Block{Kind: chat.BlockReasoning, Text: summary.String(), ProtocolState: string(state)}
						case "function_call":
							call := item.AsFunctionCall()
							if call.CallID == "" || call.Name == "" || !json.Valid([]byte(call.Arguments)) {
								streamErr = fmt.Errorf("responses: invalid function call")
								break
							}
							out.ToolCalls = append(out.ToolCalls, chat.ToolCall{ID: call.CallID, ProviderID: call.CallID,
								Name: call.Name, Args: json.RawMessage(call.Arguments), ProtocolState: string(state)})
						default:
							streamErr = fmt.Errorf("responses: unsupported output item %q", item.Type)
						}
					}
					if streamErr != nil {
						yield(chat.Delta{}, streamErr)
						return
					}
					out.FinishReason = string(e.Response.Status)
					out.Usage = chat.Usage{InputTokens: int(e.Response.Usage.InputTokens), OutputTokens: int(e.Response.Usage.OutputTokens), CacheReadTokens: int(e.Response.Usage.InputTokensDetails.CachedTokens)}
					completed = true
					return
				case "error", "response.failed", "response.incomplete":
					streamErr = fmt.Errorf("responses: %s: %s %s %s", e.Type, e.Message, e.Response.Error.Message, e.Response.IncompleteDetails.Reason)
					yield(chat.Delta{}, streamErr)
					return
				}
			}
			streamErr = stream.Err()
			if streamErr == nil {
				streamErr = errors.New("responses: stream ended without response.completed")
			}
			streamErr = wrapErr(streamErr)
			yield(chat.Delta{}, streamErr)
		},
		Result: func() (chat.Result, error) {
			if streamErr != nil {
				return chat.Result{}, streamErr
			}
			if !completed {
				return chat.Result{}, errors.New("responses: result requested before stream completed")
			}
			return out, nil
		},
	}, nil
}

type responseItemState struct {
	Index int             `json:"index"`
	Item  json.RawMessage `json:"item"`
}

func responsesParams(req chat.Request) (responses.ResponseNewParams, error) {
	p := responses.ResponseNewParams{Model: req.Model, Store: sdk.Bool(false), Include: []responses.ResponseIncludable{"reasoning.encrypted_content"}}
	if len(req.OutputSchema) > 0 {
		return p, errors.New("output schema is not implemented for Responses")
	}
	if req.Model == "" {
		return p, errors.New("model is required")
	}
	if req.System != "" {
		p.Instructions = sdk.String(req.System)
	}
	var input []responses.ResponseInputItemUnionParam
	add := func(v any) { input = append(input, param.Override[responses.ResponseInputItemUnionParam](v)) }
	for _, msg := range req.Messages {
		switch msg.Role {
		case chat.RoleUser:
			content := []any{map[string]any{"type": "input_text", "text": msg.Text}}
			for _, media := range msg.Media {
				if len(media.Data) == 0 || !mediatype.SupportedImage(media.MIMEType) {
					return p, errors.New("image is not resolved")
				}
				content = append(content, map[string]any{"type": "input_image", "image_url": "data:" + media.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(media.Data)})
			}
			add(map[string]any{"role": "user", "content": content})
		case chat.RoleTool:
			if msg.ToolCallID == "" || len(msg.Media) > 0 {
				return p, errors.New("tool output requires a call ID and text-only content")
			}
			add(map[string]any{"type": "function_call_output", "call_id": msg.ToolCallID, "output": msg.Text})
		case chat.RoleAssistant:
			items, err := responsesAssistant(msg)
			if err != nil {
				return p, err
			}
			for _, item := range items {
				add(item)
			}
		default:
			return p, fmt.Errorf("unsupported role %q", msg.Role)
		}
	}
	p.Input.OfInputItemList = input
	for _, tool := range req.Tools {
		schema := map[string]any{"type": "object", "properties": map[string]any{}}
		if len(tool.InputSchema) > 0 {
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema == nil {
				return p, fmt.Errorf("tool %s: invalid input schema", tool.Name)
			}
		}
		// Existing tool schemas allow optional parameters and are not strict schemas.
		p.Tools = append(p.Tools, responses.ToolUnionParam{OfFunction: &responses.FunctionToolParam{
			Name: tool.Name, Description: sdk.String(tool.Description), Parameters: schema, Strict: sdk.Bool(false),
		}})
	}
	return p, nil
}

func responsesAssistant(msg chat.Message) ([]any, error) {
	var states []responseItemState
	var items []any
	appendState := func(raw, kind string) error {
		var state responseItemState
		var item struct{ Type string }
		if json.Unmarshal([]byte(raw), &state) != nil || json.Unmarshal(state.Item, &item) != nil || item.Type != kind {
			return errors.New("invalid Responses history state")
		}
		states = append(states, state)
		return nil
	}
	for _, b := range msg.Blocks {
		kind := "message"
		if b.Kind == chat.BlockReasoning {
			kind = "reasoning"
		}
		if b.ProtocolState != "" {
			if err := appendState(b.ProtocolState, kind); err != nil {
				return nil, err
			}
		} else if b.Kind == chat.BlockText && b.Text != "" {
			items = append(items, map[string]any{"role": "assistant", "content": b.Text})
		}
	}
	if len(msg.Blocks) == 0 && msg.Text != "" {
		items = append(items, map[string]any{"role": "assistant", "content": msg.Text})
	}
	for _, call := range msg.ToolCalls {
		if call.ProtocolState != "" {
			if err := appendState(call.ProtocolState, "function_call"); err != nil {
				return nil, err
			}
			continue
		}
		id := providerToolCallID(call)
		if id == "" {
			return nil, errors.New("function call without call ID")
		}
		args := argsText(call.Args)
		items = append(items, map[string]any{"type": "function_call", "call_id": id, "name": call.Name, "arguments": args})
	}
	slices.SortStableFunc(states, func(a, b responseItemState) int { return a.Index - b.Index })
	for _, state := range states {
		items = append(items, state.Item)
	}
	return items, nil
}
