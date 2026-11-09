package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

const reasoningField = "reasoning_content"

type ChatClient struct{ sdk sdk.Client }

func NewChat(r gateway.Route, opts ...upstream.Option) (*ChatClient, error) {
	base, client, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &ChatClient{sdk: sdk.NewClient(
		option.WithBaseURL(strings.TrimRight(base.String(), "/")+"/"),
		option.WithHTTPClient(client),
	)}, nil
}

func (c *ChatClient) Stream(ctx context.Context, req chat.Request) (chat.Response, error) {
	if req.Model == "" {
		return chat.Response{}, fmt.Errorf("model is required")
	}
	params, err := buildParams(req.Model, req)
	if err != nil {
		return chat.Response{}, fmt.Errorf("openai: build request: %w", err)
	}
	opts := make([]option.RequestOption, 0, 1)
	if req.IdempotencyKey != "" {
		opts = append(opts, option.WithHeader("Idempotency-Key", req.IdempotencyKey))
	}

	st := &streamState{stream: c.sdk.Chat.Completions.NewStreaming(ctx, params, opts...)}
	return chat.Response{Stream: st.frames, Result: st.result}, nil
}

type streamState struct {
	stream *ssestream.Stream[sdk.ChatCompletionChunk]
	acc    sdk.ChatCompletionAccumulator

	blocks    blockIndexer
	reasoning strings.Builder
	sawFinish bool
	drained   bool
	failed    bool
}

func (s *streamState) frames(yield func(chat.Delta, error) bool) {
	defer func() { _ = s.stream.Close() }()
	for s.stream.Next() {
		chunk := s.stream.Current()
		if !s.acc.AddChunk(chunk) {
			s.failed = true
			yield(chat.Delta{}, errors.New("openai: chunk out of sequence"))
			return
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				s.sawFinish = true
			}
			if text := reasoningDelta(choice.Delta); text != "" {
				s.reasoning.WriteString(text)
				if !yield(s.delta(chat.BlockReasoning, text), nil) {
					return
				}
			}
			if choice.Delta.Content != "" {
				if !yield(s.delta(chat.BlockText, choice.Delta.Content), nil) {
					return
				}
			}
		}
	}
	if err := s.stream.Err(); err != nil {
		s.failed = true
		yield(chat.Delta{}, wrapErr(err))
		return
	}
	// 正常 EOF 但缺少 finish_reason 仍视为截断，避免使用不完整 tool call 参数。
	if !s.sawFinish {
		s.failed = true
		yield(chat.Delta{}, errors.New("openai: stream ended without a finish reason"))
		return
	}
	s.drained = true
}

func (s *streamState) delta(kind chat.BlockKind, text string) chat.Delta {
	return chat.Delta{Block: s.blocks.index(kind), Kind: kind, Text: text}
}

func (s *streamState) result() (chat.Result, error) {
	if s.failed {
		return chat.Result{}, errors.New("openai: stream failed, no result")
	}
	if !s.drained {
		return chat.Result{}, errors.New("openai: result requested before stream drained")
	}
	var out chat.Result
	if len(s.acc.Choices) > 0 {
		choice := s.acc.Choices[0]
		out.FinishReason = choice.FinishReason
		for _, kind := range s.blocks.kinds(s.reasoning.Len() > 0, choice.Message.Content != "") {
			switch kind {
			case chat.BlockReasoning:
				out.Blocks = append(out.Blocks, chat.Block{Kind: kind, Text: s.reasoning.String()})
			case chat.BlockText:
				out.Blocks = append(out.Blocks, chat.Block{Kind: kind, Text: choice.Message.Content})
			}
		}
		for _, call := range choice.Message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, chat.ToolCall{
				ID: call.ID, ProviderID: call.ID,
				Name: call.Function.Name, Args: toolArgs(call.Function.Arguments),
			})
		}
	}
	out.Usage = chat.Usage{
		InputTokens:     int(s.acc.Usage.PromptTokens),
		OutputTokens:    int(s.acc.Usage.CompletionTokens),
		CacheReadTokens: int(s.acc.Usage.PromptTokensDetails.CachedTokens),
	}
	return out, nil
}

// SDK 将 reasoning_content 保存在未校验的 ExtraFields 中，需通过 Raw() 解码。
func reasoningDelta(delta sdk.ChatCompletionChunkChoiceDelta) string {
	raw := delta.JSON.ExtraFields[reasoningField].Raw()
	if raw == "" {
		return ""
	}
	var text string
	if err := json.Unmarshal([]byte(raw), &text); err != nil {
		return ""
	}
	return text
}

type blockIndexer struct {
	order []chat.BlockKind
}

func (b *blockIndexer) index(kind chat.BlockKind) int {
	for i, k := range b.order {
		if k == kind {
			return i
		}
	}
	b.order = append(b.order, kind)
	return len(b.order) - 1
}

func (b *blockIndexer) kinds(hasReasoning, hasText bool) []chat.BlockKind {
	if hasReasoning {
		b.index(chat.BlockReasoning)
	}
	if hasText {
		b.index(chat.BlockText)
	}
	return b.order
}

func wrapErr(err error) error {
	var apiErr *sdk.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("openai: stream: %w", err)
	}
	msg := strings.TrimSpace(apiErr.Message)
	if msg == "" {
		msg = strings.TrimSpace(apiErr.RawJSON())
	}
	if rejected(apiErr) {
		return fmt.Errorf("openai: status %d: %s: %w", apiErr.StatusCode, msg, chat.ErrRejected)
	}
	return fmt.Errorf("openai: status %d: %s", apiErr.StatusCode, msg)
}

// 429 的 insufficient_quota 表示配额耗尽，而非临时限流。
func rejected(apiErr *sdk.Error) bool {
	switch apiErr.StatusCode {
	case http.StatusTooManyRequests:
		return apiErr.Code == "insufficient_quota"
	case http.StatusRequestTimeout, http.StatusConflict:
		return false
	default:
		return apiErr.StatusCode >= 400 && apiErr.StatusCode < 500
	}
}
