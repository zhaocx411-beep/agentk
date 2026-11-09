package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"agentk/internal/artifact"
)

const (
	DefaultAzureAPIVersion = "2024-06-01"
	maxResponseBytes       = 8 << 20
)

type AzureConfig struct {
	BaseURL        string
	APIKey         string
	Deployment     string
	Model          string
	APIVersion     string
	ResponseFormat string
	ProxyURL       string
	Client         *http.Client
	Artifacts      artifact.Storage
}

type Azure struct {
	cfg               AzureConfig
	http              *http.Client
	allowPrivateFetch bool
}

func NewAzure(cfg AzureConfig) (*Azure, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("asr: azure baseUrl is required")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("asr: azure baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("asr: azure api key is required")
	}
	if cfg.Deployment == "" {
		return nil, fmt.Errorf("asr: azure deployment is required")
	}
	if cfg.Model == "" {
		cfg.Model = cfg.Deployment
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = DefaultAzureAPIVersion
	}
	if cfg.ResponseFormat == "" {
		cfg.ResponseFormat = ResponseFormatVerboseJSON
	}
	if cfg.ResponseFormat != ResponseFormatJSON && cfg.ResponseFormat != ResponseFormatVerboseJSON {
		return nil, fmt.Errorf("asr: azure responseFormat must be %q or %q",
			ResponseFormatJSON, ResponseFormatVerboseJSON)
	}
	client := cfg.Client
	if client == nil {
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &Azure{cfg: cfg, http: client}, nil
}

func (a *Azure) endpoint(task string) string {
	operation := "transcriptions"
	if task == TaskTranslate {
		operation = "translations"
	}
	return fmt.Sprintf("%s/openai/deployments/%s/audio/%s?api-version=%s",
		strings.TrimSuffix(a.cfg.BaseURL, "/"), url.PathEscape(a.cfg.Deployment),
		operation, url.QueryEscape(a.cfg.APIVersion))
}

func (a *Azure) Transcribe(ctx context.Context, req Request) (Result, error) {
	req, err := normalizeRequest(req)
	if err != nil {
		return Result{}, err
	}
	audio, err := fetchAudio(ctx, a.http, a.cfg.Artifacts, req.AudioURL, a.allowPrivateFetch)
	if err != nil {
		return Result{}, err
	}
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`,
		strings.ReplaceAll(audio.Filename, `"`, "")))
	header.Set("Content-Type", audio.MIME)
	part, err := writer.CreatePart(header)
	if err != nil {
		return Result{}, err
	}
	if _, err := part.Write(audio.Data); err != nil {
		return Result{}, err
	}
	if err := writer.WriteField("model", a.cfg.Model); err != nil {
		return Result{}, err
	}
	if req.Language != "" {
		if err := writer.WriteField("language", req.Language); err != nil {
			return Result{}, err
		}
	}
	if req.Prompt != "" {
		if err := writer.WriteField("prompt", req.Prompt); err != nil {
			return Result{}, err
		}
	}
	if err := writer.WriteField("response_format", a.cfg.ResponseFormat); err != nil {
		return Result{}, err
	}
	if a.cfg.ResponseFormat == ResponseFormatVerboseJSON && req.Task == TaskTranscribe {
		if err := writer.WriteField("timestamp_granularities[]", "segment"); err != nil {
			return Result{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return Result{}, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(req.Task), &payload)
	if err != nil {
		return Result{}, err
	}
	hreq.Header.Set("Content-Type", writer.FormDataContentType())
	hreq.Header.Set("api-key", a.cfg.APIKey)
	resp, err := a.http.Do(hreq)
	if err != nil {
		return Result{}, fmt.Errorf("asr: azure %s: %w", req.Task, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("asr: azure read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return Result{}, fmt.Errorf("asr: azure response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, &httpStatusError{status: resp.StatusCode, body: responseSnippet(raw)}
	}
	var body struct {
		Text     string    `json:"text"`
		Language string    `json:"language"`
		Duration float64   `json:"duration"`
		Segments []Segment `json:"segments"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Result{}, fmt.Errorf("asr: azure parse response: %w (%s)", err, responseSnippet(raw))
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" {
		return Result{}, fmt.Errorf("asr: azure returned empty text")
	}
	params := map[string]any{
		"task": req.Task, "audioUrl": req.AudioURL, "mime": audio.MIME, "bytes": len(audio.Data),
		"responseFormat": a.cfg.ResponseFormat,
	}
	if req.Language != "" {
		params["language"] = req.Language
	}
	if req.Prompt != "" {
		params["prompt"] = req.Prompt
	}
	requestID := resp.Header.Get("x-request-id")
	if requestID == "" {
		requestID = resp.Header.Get("apim-request-id")
	}
	return Result{
		Text: body.Text, Language: body.Language, Duration: body.Duration, Segments: body.Segments,
		Params: params, Metadata: Metadata{RequestID: requestID},
	}, nil
}

func responseSnippet(raw []byte) string {
	value := strings.TrimSpace(string(raw))
	if len(value) > 300 {
		return value[:300] + "..."
	}
	return value
}
