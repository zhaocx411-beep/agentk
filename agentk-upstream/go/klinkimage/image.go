package klinkimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strings"
	"sync/atomic"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type Client struct {
	openAIBase string
	geminiBase string
	openAI     *http.Client
	gemini     *http.Client
}

type ImageFile struct {
	Data []byte
	MIME string
}

type Task struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	PollAfterMS int    `json:"poll_after_ms"`
	Output      []struct {
		URL  string `json:"url"`
		MIME string `json:"mime"`
	} `json:"output"`
	Error struct {
		Source  string `json:"source"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type SubmissionError struct {
	Err       error
	RequestID string
	Sent      bool
}

func (e *SubmissionError) Error() string {
	return fmt.Sprintf("KLink image submission request %s: %v", e.RequestID, e.Err)
}
func (e *SubmissionError) Unwrap() error { return e.Err }

func New(openAIRoute, geminiRoute gateway.Route, opts ...upstream.Option) (*Client, error) {
	openAIBase, openAI, err := clientconfig.New(openAIRoute, opts...)
	if err != nil {
		return nil, err
	}
	geminiBase, gemini, err := clientconfig.New(geminiRoute, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{strings.TrimRight(openAIBase.String(), "/"),
		strings.TrimRight(geminiBase.String(), "/"), openAI, gemini}, nil
}

func (c *Client) Generate(ctx context.Context, model, prompt, size, quality, requestID string) (Task, error) {
	body := map[string]any{"model": model, "prompt": prompt, "n": 1}
	if size != "" {
		body["size"] = size
	}
	if quality != "" {
		body["quality"] = quality
	}
	return c.postJSON(ctx, c.openAI, c.openAIBase+"/async/images/generations", body, requestID)
}

func (c *Client) Edit(ctx context.Context, model, prompt, size, quality string, images []ImageFile, requestID string) (Task, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range map[string]string{"model": model, "prompt": prompt, "n": "1", "size": size, "quality": quality} {
		if value != "" {
			if err := writer.WriteField(key, value); err != nil {
				return Task{}, err
			}
		}
	}
	var total int
	for i, image := range images {
		total += len(image.Data)
		if total > 50<<20 {
			return Task{}, fmt.Errorf("KLink image references exceed 50 MiB")
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="image_%d"`, i))
		header.Set("Content-Type", image.MIME)
		part, err := writer.CreatePart(header)
		if err != nil {
			return Task{}, err
		}
		if _, err := part.Write(image.Data); err != nil {
			return Task{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return Task{}, err
	}
	return c.post(ctx, c.openAI, c.openAIBase+"/async/images/edits",
		buf.Bytes(), writer.FormDataContentType(), requestID)
}

func (c *Client) Gemini(ctx context.Context, model, prompt, aspectRatio, imageSize string, images []ImageFile, requestID string) (Task, error) {
	parts := []map[string]any{{"text": prompt}}
	for _, image := range images {
		parts = append(parts, map[string]any{"inlineData": map[string]string{
			"mimeType": image.MIME, "data": base64.StdEncoding.EncodeToString(image.Data),
		}})
	}
	config := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	imageConfig := map[string]string{}
	if aspectRatio != "" {
		imageConfig["aspectRatio"] = aspectRatio
	}
	if imageSize != "" {
		imageConfig["imageSize"] = imageSize
	}
	if len(imageConfig) != 0 {
		config["imageConfig"] = imageConfig
	}
	body := map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": config}
	return c.postJSON(ctx, c.gemini,
		c.geminiBase+"/async/models/"+url.PathEscape(model)+":generateContent", body, requestID)
}

func (c *Client) GetTask(ctx context.Context, id string) (Task, error) {
	raw, resp, err := httpcall.JSON(ctx, c.openAI, http.MethodGet,
		c.openAIBase+"/async/tasks/"+url.PathEscape(id), nil)
	if err != nil {
		return Task{}, err
	}
	return decodeTask(raw, resp)
}

func (c *Client) postJSON(ctx context.Context, client *http.Client, target string, body any, requestID string) (Task, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return Task{}, err
	}
	return c.post(ctx, client, target, data, "application/json", requestID)
}

func (c *Client) post(ctx context.Context, client *http.Client, target string, body []byte, contentType, requestID string) (Task, error) {
	var sent atomic.Bool
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) {
		sent.Store(true)
	}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return Task{}, err
	}
	req.Header.Set("Content-Type", contentType)
	if requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
	raw, resp, err := httpcall.Do(client, req)
	if err != nil {
		return Task{}, &SubmissionError{Err: err, RequestID: requestID, Sent: sent.Load()}
	}
	task, err := decodeTask(raw, resp)
	if err != nil {
		return Task{}, &SubmissionError{Err: err, RequestID: requestID, Sent: true}
	}
	if task.ID == "" {
		return Task{}, &SubmissionError{Err: fmt.Errorf("empty task ID"), RequestID: requestID, Sent: true}
	}
	return task, nil
}

func decodeTask(raw []byte, resp *httpcall.Response) (Task, error) {
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return Task{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := task.Error.Message
		if message == "" {
			message = httpcall.Snippet(raw)
		}
		return Task{}, httpcall.Error(resp, task.Error.Code, message)
	}
	if task.PollAfterMS < 0 || task.PollAfterMS > 120000 {
		task.PollAfterMS = 0
	}
	return task, nil
}

func (t Task) PollDelay() int {
	if t.PollAfterMS > 0 {
		return t.PollAfterMS
	}
	return 2000
}

func (t Task) ErrorString() string {
	return strings.TrimSpace(t.Error.Source + ": " + t.Error.Code + ": " + t.Error.Message)
}

func (t Task) OutputURL() (string, error) {
	if len(t.Output) == 0 || t.Output[0].URL == "" {
		return "", fmt.Errorf("KLink image task %s has no output URL", t.ID)
	}
	return t.Output[0].URL, nil
}

func SupportsReferenceMIME(model, mime string) bool {
	return mime == "image/png" || mime == "image/jpeg" ||
		model == "gemini-3.1-flash-image" && mime == "image/webp"
}

// ResultURL returns an empty URL while the task is pending and an error on terminal failure.
func (t Task) ResultURL() (string, error) {
	switch t.Status {
	case "succeeded":
		return t.OutputURL()
	case "failed", "cancelled", "canceled":
		return "", fmt.Errorf("%s", t.ErrorString())
	case "uncertain":
		return "", fmt.Errorf("KLink image task %s status uncertain", t.ID)
	default:
		return "", nil
	}
}
