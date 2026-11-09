// Package asr 提供音频转文字能力及各厂商协议的适配层。
package asr

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"agentk/internal/artifact"
	"agentk/internal/netguard"
)

const maxAudioBytes = 25 << 20

const (
	TaskTranscribe = "transcribe"
	TaskTranslate  = "translate"

	ResponseFormatJSON        = "json"
	ResponseFormatVerboseJSON = "verbose_json"
)

type Request struct {
	AudioURL string
	Task     string
	Language string
	Prompt   string
}

type Result struct {
	Text     string
	Language string
	Duration float64
	Segments []Segment
	Params   map[string]any
	Metadata Metadata
}

type Segment struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type Metadata struct {
	RequestID string `json:"requestId,omitempty"`
}

type Client interface {
	Transcribe(context.Context, Request) (Result, error)
}

type AudioInput struct {
	Data     []byte
	Filename string
	MIME     string
}

var audioExtensions = map[string]string{
	".flac": "audio/flac", ".mp3": "audio/mpeg", ".mp4": "video/mp4",
	".mpeg": "audio/mpeg", ".mpga": "audio/mpeg", ".m4a": "audio/mp4",
	".ogg": "audio/ogg", ".wav": "audio/wav", ".webm": "video/webm",
}

func normalizeRequest(req Request) (Request, error) {
	req.AudioURL = strings.TrimSpace(req.AudioURL)
	if req.AudioURL == "" {
		return Request{}, fmt.Errorf("asr: audioUrl is required")
	}
	if req.Task == "" {
		req.Task = TaskTranscribe
	}
	if req.Task != TaskTranscribe && req.Task != TaskTranslate {
		return Request{}, fmt.Errorf("asr: task must be %q or %q", TaskTranscribe, TaskTranslate)
	}
	if req.Task == TaskTranslate && req.Language != "" {
		return Request{}, fmt.Errorf("asr: language is not valid for translate; translations always return English")
	}
	return req, nil
}

func fetchAudio(
	ctx context.Context, client *http.Client, storage artifact.Storage, rawURL string, allowPrivate bool,
) (AudioInput, error) {
	if storage != nil {
		if object, ok := storage.Resolve(rawURL); ok {
			reader, err := storage.Open(ctx, object)
			if err != nil {
				return AudioInput{}, fmt.Errorf("asr: open %s: %w", rawURL, err)
			}
			defer func() { _ = reader.Close() }()
			return readAudio(reader, object.Key, "")
		}
	}
	target, err := url.Parse(rawURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return AudioInput{}, fmt.Errorf("asr: audioUrl %q must be an absolute HTTP(S) or managed artifact URL", rawURL)
	}
	fetchClient := client
	if !allowPrivate {
		if _, err := netguard.ValidatePublicHTTPURL(ctx, target.String()); err != nil {
			return AudioInput{}, fmt.Errorf("asr: unsafe audioUrl: %w", err)
		}
		clone := *client
		clone.CheckRedirect = netguard.PublicRedirectPolicy(client.CheckRedirect)
		fetchClient = &clone
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return AudioInput{}, err
	}
	resp, err := fetchClient.Do(hreq)
	if err != nil {
		return AudioInput{}, fmt.Errorf("asr: download %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return AudioInput{}, fmt.Errorf("asr: download %s: HTTP %d", rawURL, resp.StatusCode)
	}
	return readAudio(resp.Body, target.Path, resp.Header.Get("Content-Type"))
}

func readAudio(reader io.Reader, name, contentType string) (AudioInput, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxAudioBytes+1))
	if err != nil {
		return AudioInput{}, fmt.Errorf("asr: read audio: %w", err)
	}
	if len(data) == 0 {
		return AudioInput{}, fmt.Errorf("asr: audio is empty")
	}
	if len(data) > maxAudioBytes {
		return AudioInput{}, fmt.Errorf("asr: audio exceeds %d bytes", maxAudioBytes)
	}
	ext := strings.ToLower(path.Ext(name))
	mimeType := audioExtensions[ext]
	if mimeType == "" {
		if declared, _, err := mime.ParseMediaType(contentType); err == nil {
			mimeType = declared
		}
	}
	if mimeType == "" {
		if detected, _, err := mime.ParseMediaType(http.DetectContentType(data)); err == nil {
			mimeType = detected
		}
	}
	if !supportedAudioMIME(mimeType) {
		return AudioInput{}, fmt.Errorf("asr: unsupported audio type %q; supported: flac, mp3, mp4, mpeg, mpga, m4a, ogg, wav, webm", mimeType)
	}
	filename := path.Base(name)
	if filename == "." || filename == "/" || filename == "" {
		filename = "audio" + extensionForMIME(mimeType)
	}
	if path.Ext(filename) == "" {
		filename += extensionForMIME(mimeType)
	}
	return AudioInput{Data: data, Filename: filename, MIME: mimeType}, nil
}

func supportedAudioMIME(value string) bool {
	switch value {
	case "audio/flac", "audio/x-flac", "audio/mpeg", "audio/mp4", "audio/x-m4a",
		"audio/ogg", "audio/wav", "audio/x-wav", "audio/vnd.wave", "video/mp4", "video/webm":
		return true
	default:
		return false
	}
}

func extensionForMIME(value string) string {
	switch value {
	case "audio/flac", "audio/x-flac":
		return ".flac"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4", "audio/x-m4a":
		return ".m4a"
	case "audio/ogg":
		return ".ogg"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ".wav"
	}
}

func newHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil || proxy.Scheme == "" || proxy.Host == "" {
			return nil, fmt.Errorf("asr: proxy url %q must be absolute", proxyURL)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Transport: transport}, nil
}

type httpStatusError struct {
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, e.body)
}

func (e *httpStatusError) StatusCode() int { return e.status }
