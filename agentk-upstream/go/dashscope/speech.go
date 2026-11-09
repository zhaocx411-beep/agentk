package dashscope

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type SpeechClient struct {
	base string
	http *http.Client
}

func NewSpeech(r gateway.Route, opts ...upstream.Option) (*SpeechClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &SpeechClient{strings.TrimRight(base.String(), "/"), c}, nil
}

type SpeechRequest struct {
	Model, Text, Voice, LanguageType, Instructions, Format string
	SampleRate                                             int
	OptimizeInstructions                                   bool
}
type SpeechResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Output    struct {
		Audio struct {
			Data      string `json:"data"`
			URL       string `json:"url"`
			ID        string `json:"id"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"audio"`
	} `json:"output"`
	Usage struct {
		Characters   int64 `json:"characters"`
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
	Audio []byte `json:"-"`
}

func (c *SpeechClient) Synthesize(ctx context.Context, req SpeechRequest) (SpeechResponse, error) {
	input := map[string]any{"text": req.Text, "voice": req.Voice, "language_type": req.LanguageType}
	if req.Instructions != "" {
		input["instructions"] = req.Instructions
		input["optimize_instructions"] = req.OptimizeInstructions
	}
	return c.call(ctx, "/services/aigc/multimodal-generation/generation", req.Model, input)
}
func (c *SpeechClient) SynthesizeCosyVoice(ctx context.Context, req SpeechRequest) (SpeechResponse, error) {
	input := map[string]any{"text": req.Text, "voice": req.Voice, "format": req.Format, "sample_rate": req.SampleRate}
	if req.Instructions != "" {
		input["instruction"] = req.Instructions
	}
	return c.call(ctx, "/services/audio/tts/SpeechSynthesizer", req.Model, input)
}
func (c *SpeechClient) call(ctx context.Context, path, model string, input map[string]any) (SpeechResponse, error) {
	base := c.base
	if !strings.HasSuffix(base, "/api/v1") {
		base += "/api/v1"
	}
	raw, resp, err := httpcall.JSON(ctx, c.http, http.MethodPost, base+path, struct {
		Model string         `json:"model"`
		Input map[string]any `json:"input"`
	}{model, input})
	if err != nil {
		return SpeechResponse{}, err
	}
	var out SpeechResponse
	if resp.StatusCode != 200 {
		_ = json.Unmarshal(raw, &out)
		message := out.Message
		if message == "" {
			message = httpcall.Snippet(raw)
		}
		e := httpcall.Error(resp, out.Code, message)
		if out.RequestID != "" {
			e.RequestID = out.RequestID
		}
		return out, e
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Code != "" {
		e := httpcall.Error(resp, out.Code, out.Message)
		if out.RequestID != "" {
			e.RequestID = out.RequestID
		}
		return out, e
	}
	if out.Output.Audio.Data != "" {
		out.Audio, err = base64.StdEncoding.DecodeString(out.Output.Audio.Data)
	}
	return out, err
}
