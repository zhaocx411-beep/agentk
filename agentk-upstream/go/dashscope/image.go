package dashscope

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type ImageClient struct {
	base string
	http *http.Client
}

func NewImage(r gateway.Route, opts ...upstream.Option) (*ImageClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &ImageClient{strings.TrimRight(base.String(), "/"), c}, nil
}

type ImageContent struct {
	Data  []byte `json:"-"`
	MIME  string `json:"-"`
	Image string `json:"image,omitempty"`
	Text  string `json:"text,omitempty"`
}
type ImageParameters struct {
	Size           string `json:"size,omitempty"`
	N              int    `json:"n,omitempty"`
	NegativePrompt string `json:"negative_prompt,omitempty"`
	PromptExtend   bool   `json:"prompt_extend"`
	Watermark      *bool  `json:"watermark,omitempty"`
}
type ImageRequest struct {
	Model      string
	Content    []ImageContent
	Parameters ImageParameters
}

func (c *ImageClient) Generate(ctx context.Context, req ImageRequest) (string, error) {
	base := c.base
	if !strings.HasSuffix(base, "/api/v1") {
		base += "/api/v1"
	}
	body := struct {
		Model string `json:"model"`
		Input struct {
			Messages []struct {
				Role    string         `json:"role"`
				Content []ImageContent `json:"content"`
			} `json:"messages"`
		} `json:"input"`
		Parameters ImageParameters `json:"parameters"`
	}{Model: req.Model, Parameters: req.Parameters}
	content := append([]ImageContent(nil), req.Content...)
	for i := range content {
		if content[i].Data != nil {
			content[i].Image = "data:" + content[i].MIME + ";base64," +
				base64.StdEncoding.EncodeToString(content[i].Data)
		}
	}
	body.Input.Messages = append(body.Input.Messages, struct {
		Role    string         `json:"role"`
		Content []ImageContent `json:"content"`
	}{"user", content})
	raw, resp, err := httpcall.JSON(
		ctx,
		c.http,
		http.MethodPost,
		base+"/services/aigc/multimodal-generation/generation",
		body,
	)
	if err != nil {
		return "", err
	}
	var out struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Output    struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}
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
		return "", e
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Code != "" {
		e := httpcall.Error(resp, out.Code, out.Message)
		if out.RequestID != "" {
			e.RequestID = out.RequestID
		}
		return "", e
	}
	for _, choice := range out.Output.Choices {
		for _, part := range choice.Message.Content {
			if part.Image != "" {
				return part.Image, nil
			}
		}
	}
	return "", fmt.Errorf("dashscope response contained no image")
}
func IsInputDownloadFailure(err error) bool {
	var e *upstream.APIError
	if !errors.As(err, &e) || e.StatusCode != 400 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(e.Code)) {
	case "invalidurl.connectionrefused", "invalidurl.timeout":
		return true
	}
	return false
}
