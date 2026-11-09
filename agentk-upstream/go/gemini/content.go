package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type Media struct {
	MIME, URL string
	Data      []byte
}
type VideoSegment struct{ StartSeconds, EndSeconds, FPS float64 }
type mediaPart struct {
	Text          string         `json:"text,omitempty"`
	FileData      *fileData      `json:"fileData,omitempty"`
	InlineData    *inlineData    `json:"inlineData,omitempty"`
	VideoMetadata *videoMetadata `json:"videoMetadata,omitempty"`
}

type videoMetadata struct {
	StartOffset string  `json:"startOffset"`
	EndOffset   string  `json:"endOffset"`
	FPS         float64 `json:"fps,omitempty"`
}
type contentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text       string      `json:"text"`
				InlineData *inlineData `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error struct {
		Code    int    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}
type ContentError struct {
	*upstream.APIError
	NumericCode int
}

func (e *ContentError) Unwrap() error { return e.APIError }
func IsInputDownloadFailure(err error) bool {
	var e *ContentError
	if !errors.As(err, &e) || e.StatusCode != 400 || e.NumericCode != 400 || e.Code != "INVALID_ARGUMENT" {
		return false
	}
	return strings.HasPrefix(e.Message, "Cannot fetch content from the provided URL") &&
		!strings.Contains(strings.ToUpper(e.Message), "URL_ERROR-PAYLOAD_TOO_LARGE")
}

type contentClient struct {
	base string
	http *http.Client
}

func newContent(r gateway.Route, opts ...upstream.Option) (contentClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return contentClient{}, err
	}
	return contentClient{strings.TrimRight(base.String(), "/"), c}, nil
}
func (c contentClient) generate(
	ctx context.Context,
	model, prompt string,
	media []Media,
	segment *VideoSegment,
	imageConfig *ImageConfig,
) (contentResponse, error) {
	parts := make([]mediaPart, 0, len(media)+1)
	for _, m := range media {
		p := mediaPart{}
		if m.URL != "" {
			p.FileData = &fileData{m.MIME, m.URL}
		} else {
			p.InlineData = &inlineData{MIMEType: m.MIME, Data: base64.StdEncoding.EncodeToString(m.Data)}
		}
		if segment != nil {
			p.VideoMetadata = &videoMetadata{
				strconv.FormatFloat(segment.StartSeconds, 'f', -1, 64) + "s",
				strconv.FormatFloat(segment.EndSeconds, 'f', -1, 64) + "s",
				segment.FPS,
			}
		}
		parts = append(parts, p)
	}
	parts = append(parts, mediaPart{Text: prompt})
	body := struct {
		Contents []struct {
			Role  string      `json:"role"`
			Parts []mediaPart `json:"parts"`
		} `json:"contents"`
		GenerationConfig *struct {
			ResponseModalities []string     `json:"responseModalities"`
			ImageConfig        *ImageConfig `json:"imageConfig"`
		} `json:"generationConfig,omitempty"`
	}{}
	body.Contents = append(body.Contents, struct {
		Role  string      `json:"role"`
		Parts []mediaPart `json:"parts"`
	}{"user", parts})
	if imageConfig != nil {
		body.GenerationConfig = &struct {
			ResponseModalities []string     `json:"responseModalities"`
			ImageConfig        *ImageConfig `json:"imageConfig"`
		}{[]string{"TEXT", "IMAGE"}, imageConfig}
	}
	raw, resp, err := httpcall.JSON(
		ctx,
		c.http,
		http.MethodPost,
		c.base+"/models/"+url.PathEscape(model)+":generateContent",
		body,
	)
	if err != nil {
		return contentResponse{}, err
	}
	var out contentResponse
	if resp.StatusCode != 200 {
		_ = json.Unmarshal(raw, &out)
		message := out.Error.Message
		if message == "" {
			message = httpcall.Snippet(raw)
		}
		return out, &ContentError{httpcall.Error(resp, out.Error.Status, message), out.Error.Code}
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.Error.Message != "" {
		return out, &ContentError{httpcall.Error(resp, out.Error.Status, out.Error.Message), out.Error.Code}
	}
	return out, nil
}

type ImageClient struct{ contentClient }
type ImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	ImageSize   string `json:"imageSize"`
}
type ImageRequest struct {
	Model, Prompt string
	Media         []Media
	Config        ImageConfig
}
type Image struct {
	Data []byte
	MIME string
}

func NewImage(r gateway.Route, opts ...upstream.Option) (*ImageClient, error) {
	c, err := newContent(r, opts...)
	if err != nil {
		return nil, err
	}
	return &ImageClient{c}, nil
}
func (c *ImageClient) Generate(ctx context.Context, req ImageRequest) (Image, error) {
	out, err := c.generate(ctx, req.Model, req.Prompt, req.Media, nil, &req.Config)
	if err != nil {
		return Image{}, err
	}
	if len(out.Candidates) > 0 {
		for _, p := range out.Candidates[0].Content.Parts {
			if p.InlineData != nil && p.InlineData.Data != "" {
				data, err := base64.StdEncoding.DecodeString(p.InlineData.Data)
				mime := p.InlineData.MIMEType
				if mime == "" {
					mime = "image/png"
				}
				return Image{data, mime}, err
			}
		}
	}
	var finishReason string
	if len(out.Candidates) > 0 {
		finishReason = out.Candidates[0].FinishReason
	}
	return Image{}, fmt.Errorf(
		"gemini returned no image (finishReason=%q blockReason=%q)",
		finishReason, out.PromptFeedback.BlockReason,
	)
}

type MediaClient struct{ contentClient }
type MediaRequest struct {
	Model, Prompt string
	Media         []Media
	Segment       *VideoSegment
}

func NewMedia(r gateway.Route, opts ...upstream.Option) (*MediaClient, error) {
	c, err := newContent(r, opts...)
	if err != nil {
		return nil, err
	}
	return &MediaClient{c}, nil
}
func (c *MediaClient) Understand(ctx context.Context, req MediaRequest) (string, error) {
	out, err := c.generate(ctx, req.Model, req.Prompt, req.Media, req.Segment, nil)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	if len(out.Candidates) > 0 {
		for _, p := range out.Candidates[0].Content.Parts {
			text.WriteString(p.Text)
		}
	}
	result := strings.TrimSpace(text.String())
	if result == "" {
		var finishReason string
		if len(out.Candidates) > 0 {
			finishReason = out.Candidates[0].FinishReason
		}
		return "", fmt.Errorf(
			"gemini returned no text (finishReason=%q blockReason=%q)",
			finishReason, out.PromptFeedback.BlockReason,
		)
	}
	return result, nil
}
