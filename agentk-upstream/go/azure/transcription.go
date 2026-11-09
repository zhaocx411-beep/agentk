package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type TranscriptionClient struct {
	base string
	http *http.Client
}

func NewTranscription(r gateway.Route, opts ...upstream.Option) (*TranscriptionClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &TranscriptionClient{strings.TrimRight(base.String(), "/"), c}, nil
}

type Audio struct {
	Data           []byte
	Filename, MIME string
}
type TranscriptionRequest struct {
	Audio                                                                 Audio
	Model, Deployment, APIVersion, ResponseFormat, Task, Language, Prompt string
}
type Transcription struct {
	Text, Language, RequestID string
	Duration                  float64
	Segments                  []Segment
}
type Segment struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func (a *TranscriptionClient) Transcribe(ctx context.Context, req TranscriptionRequest) (Transcription, error) {
	operation := "transcriptions"
	if req.Task == "translate" {
		operation = "translations"
	}
	endpoint := fmt.Sprintf(
		"%s/openai/deployments/%s/audio/%s?api-version=%s",
		a.base,
		url.PathEscape(req.Deployment),
		operation,
		url.QueryEscape(req.APIVersion),
	)
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`,
		strings.ReplaceAll(req.Audio.Filename, `"`, "")))
	header.Set("Content-Type", req.Audio.MIME)
	part, err := writer.CreatePart(header)
	if err != nil {
		return Transcription{}, err
	}
	if _, err := part.Write(req.Audio.Data); err != nil {
		return Transcription{}, err
	}
	if err := writer.WriteField("model", req.Model); err != nil {
		return Transcription{}, err
	}
	if req.Language != "" {
		if err := writer.WriteField("language", req.Language); err != nil {
			return Transcription{}, err
		}
	}
	if req.Prompt != "" {
		if err := writer.WriteField("prompt", req.Prompt); err != nil {
			return Transcription{}, err
		}
	}
	if err := writer.WriteField("response_format", req.ResponseFormat); err != nil {
		return Transcription{}, err
	}
	if req.ResponseFormat == "verbose_json" && req.Task == "transcribe" {
		if err := writer.WriteField("timestamp_granularities[]", "segment"); err != nil {
			return Transcription{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return Transcription{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &payload)
	if err != nil {
		return Transcription{}, err
	}
	hreq.Header.Set("Content-Type", writer.FormDataContentType())
	raw, resp, err := httpcall.Do(a.http, hreq)
	if err != nil {
		return Transcription{}, err
	}
	if resp.StatusCode != 200 {
		return Transcription{}, apiError(raw, resp)
	}
	var body struct {
		Text     string    `json:"text"`
		Language string    `json:"language"`
		Duration float64   `json:"duration"`
		Segments []Segment `json:"segments"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Transcription{}, fmt.Errorf("asr: azure parse response: %w (%s)", err, httpcall.Snippet(raw))
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		return Transcription{}, fmt.Errorf("asr: azure returned empty text")
	}
	return Transcription{
		Text:      body.Text,
		Language:  body.Language,
		Duration:  body.Duration,
		Segments:  body.Segments,
		RequestID: httpcall.RequestID(resp),
	}, nil
}
