package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type ChatClient struct {
	messages sdk.MessageService
	config   Config
}

func NewChat(r gateway.Route, config Config, opts ...upstream.Option) (*ChatClient, error) {
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	if config.Temperature != nil {
		value := *config.Temperature
		config.Temperature = &value
	}
	base, client, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &ChatClient{messages: sdk.NewMessageService(option.WithBaseURL(base.String()), option.WithHTTPClient(client), option.WithMaxRetries(0)), config: config}, nil
}

func (c *ChatClient) Stream(ctx context.Context, req chat.Request) (chat.Response, error) {
	p, err := requestParams(req, c.config)
	if err != nil {
		return chat.Response{}, fmt.Errorf("anthropic: %v: %w", err, chat.ErrRejected)
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
			stream := c.messages.NewStreaming(ctx, p, opts...)
			defer stream.Close()
			fail := func(err error) { streamErr = wrapErr(err); yield(chat.Delta{}, streamErr) }
			var message sdk.Message
			started := false
			open := map[int64]bool{}
			blocks := map[int64]int{}
			for stream.Next() {
				e := stream.Current()
				if e.Type == "message_start" {
					if started {
						fail(fmt.Errorf("duplicate message_start"))
						return
					}
					started = true
				} else if !started {
					fail(fmt.Errorf("missing message_start"))
					return
				}
				switch e.Type {
				case "content_block_delta", "content_block_stop":
					if !open[e.Index] {
						fail(fmt.Errorf("event for closed content block"))
						return
					}
					if e.Type == "content_block_stop" {
						// The SDK repairs truncated tool JSON to {}; reject it before accumulation.
						if err := validateBlock(message.Content[e.Index]); err != nil {
							fail(err)
							return
						}
						delete(open, e.Index)
					}
				case "message_stop":
					if len(open) > 0 || message.StopReason == "" {
						fail(fmt.Errorf("incomplete message"))
						return
					}
				}
				if err := message.Accumulate(e); err != nil {
					fail(err)
					return
				}
				var text string
				switch e.Type {
				case "content_block_start":
					open[e.Index] = true
					kind := chat.BlockText
					switch e.ContentBlock.Type {
					case "text":
						text = e.ContentBlock.Text
					case "thinking":
						kind = chat.BlockReasoning
						text = e.ContentBlock.Thinking
					case "redacted_thinking":
						kind = chat.BlockReasoning
					case "tool_use":
						continue
					default:
						fail(fmt.Errorf("unsupported content block %q", e.ContentBlock.Type))
						return
					}
					blocks[e.Index] = len(out.Blocks)
					out.Blocks = append(out.Blocks, chat.Block{Kind: kind})
				case "content_block_delta":
					if e.Delta.Type == "text_delta" {
						text = e.Delta.Text
					}
					if e.Delta.Type == "thinking_delta" {
						text = e.Delta.Thinking
					}
				case "message_stop":
					for index, block := range message.Content {
						if err := validateBlock(block); err != nil {
							fail(err)
							return
						}
						state, err := json.Marshal(blockState{Index: index, Block: json.RawMessage(block.RawJSON())})
						if err != nil {
							fail(err)
							return
						}
						if block.Type == "tool_use" {
							out.ToolCalls = append(out.ToolCalls, chat.ToolCall{ID: block.ID, ProviderID: block.ID, Name: block.Name, Args: block.Input, ProtocolState: string(state)})
						} else {
							i := blocks[int64(index)]
							out.Blocks[i].Text = block.Text + block.Thinking
							out.Blocks[i].ProtocolState = string(state)
						}
					}
					out.FinishReason = string(message.StopReason)
					u := message.Usage
					out.Usage = chat.Usage{InputTokens: int(u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens), OutputTokens: int(u.OutputTokens), CacheReadTokens: int(u.CacheReadInputTokens), CacheWriteTokens: int(u.CacheCreationInputTokens)}
					completed = true
					return
				}
				if text != "" {
					i, ok := blocks[e.Index]
					if !ok {
						fail(fmt.Errorf("text delta for non-text block"))
						return
					}
					if !yield(chat.Delta{Block: i, Kind: out.Blocks[i].Kind, Text: text}, nil) {
						return
					}
				}
			}
			err := stream.Err()
			if err == nil {
				err = fmt.Errorf("stream ended without message_stop")
			}
			fail(err)
		},
		Result: func() (chat.Result, error) {
			if streamErr != nil {
				return chat.Result{}, streamErr
			}
			if !completed {
				return chat.Result{}, fmt.Errorf("anthropic: result requested before stream completed")
			}
			return out, nil
		},
	}, nil
}

func wrapErr(err error) error {
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		s := apiErr.StatusCode
		rejected := s >= 400 && s < 500 && s != 408 && s != 409 && s != 429
		switch apiErr.Type() {
		case "invalid_request_error", "authentication_error", "permission_error", "not_found_error":
			rejected = true
		}
		if rejected {
			return fmt.Errorf("anthropic: %w: %w", err, chat.ErrRejected)
		}
	}
	return fmt.Errorf("anthropic: %w", err)
}
