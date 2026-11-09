package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/mediatype"
)

const vertexHTTPFileLimit = 15 << 20

type ChatClient struct {
	base   string
	client *http.Client
	warn   func(context.Context, string, ...any)
}

func WithWarn(warn func(context.Context, string, ...any)) upstream.Option {
	return func(o *clientconfig.Options) { o.Warn = warn }
}
func NewChat(r gateway.Route, opts ...upstream.Option) (*ChatClient, error) {
	cfg := clientconfig.Resolve(opts...)
	base, client, err := cfg.Client(r)
	if err != nil {
		return nil, err
	}
	return &ChatClient{base: strings.TrimRight(base.String(), "/"), client: client, warn: cfg.Warn}, nil
}

func (c *ChatClient) Stream(ctx context.Context, req chat.Request) (chat.Response, error) {
	if req.Model == "" {
		return chat.Response{}, fmt.Errorf("model is required")
	}
	body, err := buildRequest(req)
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: build request: %w", err)
	}
	c.warnOversizedFileURIs(ctx, req)
	raw, err := json.Marshal(body)
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: marshal request: %w", err)
	}
	endpoint := c.base + "/models/" + url.PathEscape(req.Model) + ":generateContent"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.IdempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return chat.Response{}, fmt.Errorf("gemini: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chat.Response{}, statusError(resp.StatusCode, data)
	}
	var wire responseBody
	if err := json.Unmarshal(data, &wire); err != nil {
		return chat.Response{}, fmt.Errorf("gemini: decode response: %w", err)
	}
	result, err := wire.result()
	if err != nil {
		return chat.Response{}, err
	}
	state := &bufferedState{value: result}
	return chat.Response{Stream: state.frames, Result: state.result}, nil
}

func statusError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	switch {
	case status < 400 || status >= 500,
		status == http.StatusRequestTimeout,
		status == http.StatusConflict,
		status == http.StatusTooManyRequests:
		return fmt.Errorf("gemini: status %d: %s", status, msg)
	default:
		return fmt.Errorf("gemini: status %d: %s: %w", status, msg, chat.ErrRejected)
	}
}

type fileURIWarning struct {
	MessageIndex int
	MediaIndex   int
	MIMEType     string
	Bytes        int64
}

func (c *ChatClient) warnOversizedFileURIs(ctx context.Context, req chat.Request) {
	if c.warn == nil {
		return
	}
	for _, warning := range oversizedFileURIs(c.base, req) {
		c.warn(ctx, "Gemini fileUri exceeds the documented Vertex HTTP size limit; request will still be sent",
			"model", req.Model,
			"messageIndex", warning.MessageIndex,
			"mediaIndex", warning.MediaIndex,
			"mimeType", warning.MIMEType,
			"bytes", warning.Bytes,
			"limitBytes", vertexHTTPFileLimit)
	}
}

func oversizedFileURIs(baseURL string, req chat.Request) []fileURIWarning {
	parsed, err := url.Parse(baseURL)
	if err != nil || !strings.Contains(strings.ToLower(parsed.Path), "/projects/") ||
		!strings.Contains(strings.ToLower(parsed.Path), "/publishers/google") {
		return nil
	}
	warnings := make([]fileURIWarning, 0)
	for messageIndex, message := range req.Messages {
		for mediaIndex, media := range message.Media {
			kind := mediatype.Kind(media.MIMEType)
			if (kind != mediatype.KindVideo && kind != mediatype.KindAudio) || media.URI == "" ||
				len(media.Data) != 0 || media.Bytes <= vertexHTTPFileLimit {
				continue
			}
			warnings = append(warnings, fileURIWarning{
				MessageIndex: messageIndex, MediaIndex: mediaIndex,
				MIMEType: media.MIMEType, Bytes: media.Bytes,
			})
		}
	}
	return warnings
}

type responseBody struct {
	Candidates []struct {
		Content      content `json:"content"`
		FinishReason string  `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
}

func (r responseBody) result() (chat.Result, error) {
	if len(r.Candidates) == 0 {
		return chat.Result{}, errors.New("gemini: response has no candidates")
	}
	candidate := r.Candidates[0]
	out := chat.Result{
		FinishReason: candidate.FinishReason,
		Usage: chat.Usage{
			InputTokens:     r.UsageMetadata.PromptTokenCount,
			OutputTokens:    r.UsageMetadata.CandidatesTokenCount,
			CacheReadTokens: r.UsageMetadata.CachedContentTokenCount,
		},
	}
	seenFunctionCall := false
	for _, item := range candidate.Content.Parts {
		if item.Text != nil {
			if seenFunctionCall {
				return chat.Result{}, errors.New("gemini: text or thought after functionCall cannot be replayed safely")
			}
			kind := chat.BlockText
			if item.Thought {
				kind = chat.BlockReasoning
			}
			out.Blocks = append(
				out.Blocks,
				chat.Block{Kind: kind, Text: *item.Text, ProtocolState: item.ThoughtSignature},
			)
		}
		if item.FunctionCall != nil {
			seenFunctionCall = true
			args := json.RawMessage(`{}`)
			if item.FunctionCall.Args != nil {
				args, _ = json.Marshal(item.FunctionCall.Args)
			}
			out.ToolCalls = append(
				out.ToolCalls,
				chat.ToolCall{
					ID:            item.FunctionCall.ID,
					ProviderID:    item.FunctionCall.ID,
					Name:          item.FunctionCall.Name,
					Args:          args,
					ProtocolState: item.ThoughtSignature,
				},
			)
		}
	}
	if len(out.Blocks) == 0 && len(out.ToolCalls) == 0 {
		return chat.Result{}, fmt.Errorf("gemini: candidate has no content (finishReason %q)", candidate.FinishReason)
	}
	return out, nil
}

type bufferedState struct {
	value   chat.Result
	drained bool
}

func (s *bufferedState) frames(yield func(chat.Delta, error) bool) {
	for i, block := range s.value.Blocks {
		if block.Text != "" && !yield(chat.Delta{Block: i, Kind: block.Kind, Text: block.Text}, nil) {
			return
		}
	}
	s.drained = true
}
func (s *bufferedState) result() (chat.Result, error) {
	if !s.drained {
		return chat.Result{}, errors.New("gemini: result requested before response consumed")
	}
	return s.value, nil
}

var _ chat.Client = (*ChatClient)(nil)
