package seedance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type VideoClient struct {
	base string
	http *http.Client
}

func NewVideo(r gateway.Route, opts ...upstream.Option) (*VideoClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &VideoClient{strings.TrimRight(base.String(), "/"), c}, nil
}

type Media struct{ Role, Kind, URL string }
type Request struct {
	Model, Prompt, Ratio, Resolution string
	Media                            []Media
	Duration                         int
	GenerateAudio, Watermark         bool
	IdempotencyKey                   string
}
type Task struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Status  string `json:"status"`
	Content struct {
		VideoURL string `json:"video_url"`
	} `json:"content"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type SubmissionError struct {
	Err  error
	Sent bool
}

func (e *SubmissionError) Error() string { return e.Err.Error() }
func (e *SubmissionError) Unwrap() error { return e.Err }

func (c *VideoClient) CreateTask(ctx context.Context, req Request) (Task, error) {
	var sent atomic.Bool
	if req.IdempotencyKey != "" {
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) {
			sent.Store(true)
		}})
	}
	content := []map[string]any{{"type": "text", "text": req.Prompt}}
	for _, m := range req.Media {
		kind := m.Kind + "_url"
		content = append(content, map[string]any{"type": kind, kind: map[string]string{"url": m.URL}, "role": m.Role})
	}
	body := struct {
		Model         string           `json:"model"`
		Content       []map[string]any `json:"content"`
		GenerateAudio bool             `json:"generate_audio"`
		Ratio         string           `json:"ratio"`
		Resolution    string           `json:"resolution"`
		Duration      int              `json:"duration"`
		Watermark     bool             `json:"watermark"`
	}{req.Model, content, req.GenerateAudio, req.Ratio, req.Resolution, req.Duration, req.Watermark}
	payload, err := json.Marshal(body)
	if err != nil {
		return Task{}, err
	}
	if len(payload) > 64<<20 {
		return Task{}, fmt.Errorf("seedance request exceeds %d bytes", 64<<20)
	}
	out, err := c.call(ctx, http.MethodPost, c.base+"/contents/generations/tasks", json.RawMessage(payload), req.IdempotencyKey)
	if err == nil && out.ID == "" {
		err = fmt.Errorf("create seedance task returned empty task id")
	}
	if err != nil && req.IdempotencyKey != "" {
		var apiErr *upstream.APIError
		if !errors.As(err, &apiErr) {
			err = &SubmissionError{Err: err, Sent: sent.Load()}
		}
	}
	return out, err
}
func (c *VideoClient) GetTask(ctx context.Context, id string) (Task, error) {
	return c.call(ctx, http.MethodGet, c.base+"/contents/generations/tasks/"+url.PathEscape(id), nil, "")
}
func (c *VideoClient) call(ctx context.Context, method, target string, body any, idempotencyKey string) (Task, error) {
	var raw []byte
	var resp *httpcall.Response
	var err error
	if idempotencyKey == "" {
		raw, resp, err = httpcall.JSON(ctx, c.http, method, target, body)
	} else {
		payload, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return Task{}, marshalErr
		}
		req, requestErr := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(payload))
		if requestErr != nil {
			return Task{}, requestErr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)
		raw, resp, err = httpcall.Do(c.http, req)
	}
	if err != nil {
		return Task{}, err
	}
	var out Task
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = json.Unmarshal(raw, &out)
		message := out.Error.Message
		if message == "" {
			message = httpcall.Snippet(raw)
		}
		return out, httpcall.Error(resp, out.Error.Code, message)
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}
