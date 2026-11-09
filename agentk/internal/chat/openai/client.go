// Package openai 使用官方 SDK 实现 chat 的 /chat/completions 协议适配。
// SDK 负责传输、SSE 和标准字段累积；本包负责 chat IR 转换，以及读取标准 schema
// 未声明的 reasoning_content 扩展字段。
package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"

	"agentk/internal/chat"
)

// reasoningField 是兼容模型返回 reasoning 增量的扩展字段。
const reasoningField = "reasoning_content"

type Config struct {
	// BaseURL 是 SDK 拼接 chat/completions 的端点前缀。
	BaseURL string
	APIKey  string
	// Model 是请求体中的模型名。
	Model string
	// Headers 是创建客户端时固定的网关请求头。
	Headers map[string]string
	// HTTPClient 为 nil 时使用 http.DefaultClient。
	HTTPClient *http.Client
}

type Client struct {
	cfg Config
	sdk sdk.Client
}

func New(cfg Config) *Client {
	opts := []option.RequestOption{}
	if cfg.BaseURL != "" {
		// SDK 使用相对路径解析；尾斜杠用于保留 BaseURL 的最后一个 path segment。
		opts = append(opts, option.WithBaseURL(strings.TrimRight(cfg.BaseURL, "/")+"/"))
	}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	for key, value := range cfg.Headers {
		opts = append(opts, option.WithHeader(key, value))
	}
	return &Client{cfg: cfg, sdk: sdk.NewClient(opts...)}
}

func (c *Client) Stream(ctx context.Context, req chat.Request) (chat.Response, error) {
	params, err := buildParams(c.cfg.Model, req)
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

// streamState 将 SDK chunk 转换为增量，并在流结束后生成完整结果。
// SDK accumulator 处理正文、tool call 和 usage，本地只维护 reasoning 与 block 坐标。
type streamState struct {
	stream *ssestream.Stream[sdk.ChatCompletionChunk]
	acc    sdk.ChatCompletionAccumulator

	blocks    blockIndexer
	reasoning strings.Builder
	// sawFinish 表示收到非空 finish_reason。
	sawFinish bool
	drained   bool
	failed    bool
}

// frames 是一次性迭代器；消费完成或调用方提前退出时都会关闭底层连接。
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
			// 按到达顺序为 reasoning 和正文分配 block index。
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
		// 为只出现在 accumulator 中的完整内容补充 block index。
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
				ID:   call.ID,
				Name: call.Function.Name,
				Args: toolArgs(call.Function.Arguments),
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

// reasoningDelta 从 ExtraFields 读取标准 schema 未定义的 reasoning_content。
// SDK 不校验未知字段，因此使用 Raw() 解码 JSON 字符串。
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

// blockIndexer 按内容类型首次出现的顺序分配稳定 block index。
// 同一坐标同时写入 Delta 和持久化事件，客户端据此归并流式与最终内容。
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

// kinds 返回流中出现的 block 顺序，并补充仅存在于最终结果的内容。
func (b *blockIndexer) kinds(hasReasoning, hasText bool) []chat.BlockKind {
	if hasReasoning {
		b.index(chat.BlockReasoning)
	}
	if hasText {
		b.index(chat.BlockText)
	}
	return b.order
}

// wrapErr 将 SDK API 错误转换为用于重试分类的 chat.StatusError。
func wrapErr(err error) error {
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		msg := strings.TrimSpace(apiErr.Message)
		if msg == "" {
			msg = strings.TrimSpace(apiErr.RawJSON())
		}
		return &chat.StatusError{Code: apiErr.StatusCode, Message: msg}
	}
	return fmt.Errorf("openai: stream: %w", err)
}
