// Package tts 提供文字转语音能力及各厂商协议的适配层。
package tts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	defaultSpeed         = 1.0
	defaultLanguageBoost = "Chinese"
	maxAudioBytes        = 64 << 20
	maxTextCharacters    = 9999
)

var supportedLanguageBoosts = []string{
	"Chinese", "Chinese,Yue", "English", "Arabic", "Russian", "Spanish", "French",
	"Portuguese", "German", "Turkish", "Dutch", "Ukrainian", "Vietnamese", "Indonesian",
	"Japanese", "Italian", "Korean", "Thai", "Polish", "Romanian", "Greek", "Czech",
	"Finnish", "Hindi", "Bulgarian", "Danish", "Hebrew", "Malay", "Persian", "Slovak",
	"Swedish", "Croatian", "Filipino", "Hungarian", "Norwegian", "Slovenian", "Catalan",
	"Nynorsk", "Tamil", "Afrikaans", "auto",
}

var supportedEmotions = []string{
	"happy", "sad", "angry", "fearful", "disgusted", "surprised", "calm",
}

// VoiceOption 是部署目录允许使用的一个音色。
type VoiceOption struct {
	ID          string `yaml:"id" json:"id"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Request 是一次与厂商协议无关的语音合成请求。
type Request struct {
	Text                 string
	VoiceID              string
	Speed                float64
	LanguageBoost        string
	LanguageType         string
	Emotion              string
	PronunciationDict    []string
	Instructions         string
	OptimizeInstructions bool
}

// Audio 是语音合成结果。
type Audio struct {
	Data     []byte
	MIME     string
	Params   map[string]any
	Metadata Metadata
}

// Metadata 是上游返回的生成统计与排障信息。
type Metadata struct {
	TraceID                 string  `json:"traceId,omitempty"`
	RequestID               string  `json:"requestId,omitempty"`
	AudioID                 string  `json:"audioId,omitempty"`
	ExpiresAt               int64   `json:"expiresAt,omitempty"`
	AudioLengthMs           int64   `json:"audioLengthMs,omitempty"`
	AudioSizeBytes          int64   `json:"audioSizeBytes,omitempty"`
	UsageCharacters         int64   `json:"usageCharacters,omitempty"`
	InputTokens             int64   `json:"inputTokens,omitempty"`
	OutputTokens            int64   `json:"outputTokens,omitempty"`
	TotalTokens             int64   `json:"totalTokens,omitempty"`
	WordCount               int64   `json:"wordCount,omitempty"`
	InvisibleCharacterRatio float64 `json:"invisibleCharacterRatio,omitempty"`
	SampleRate              int64   `json:"sampleRate,omitempty"`
	Bitrate                 int64   `json:"bitrate,omitempty"`
	Format                  string  `json:"format,omitempty"`
	Channel                 int64   `json:"channel,omitempty"`
}

// Client 是一个同步文字转语音端点。
type Client interface {
	ValidateRequest(Request) error
	Synthesize(ctx context.Context, req Request) (Audio, error)
}

func validateBaseRequest(req Request) error {
	if strings.TrimSpace(req.Text) == "" {
		return fmt.Errorf("tts: text is required")
	}
	if strings.TrimSpace(req.VoiceID) == "" {
		return fmt.Errorf("tts: voiceId is required")
	}
	return nil
}

func normalizeRequest(req Request) (Request, error) {
	if err := validateBaseRequest(req); err != nil {
		return Request{}, err
	}
	if utf8.RuneCountInString(req.Text) > maxTextCharacters {
		return Request{}, fmt.Errorf("tts: text must contain at most %d characters", maxTextCharacters)
	}
	if strings.TrimSpace(req.VoiceID) == "" {
		return Request{}, fmt.Errorf("tts: voiceId is required")
	}
	if req.Speed == 0 {
		req.Speed = defaultSpeed
	}
	if req.Speed < 0.5 || req.Speed > 2.0 {
		return Request{}, fmt.Errorf("tts: speed must be between 0.5 and 2.0")
	}
	if req.LanguageBoost == "" {
		req.LanguageBoost = defaultLanguageBoost
	}
	if !contains(supportedLanguageBoosts, req.LanguageBoost) {
		return Request{}, fmt.Errorf("tts: unsupported language boost %q", req.LanguageBoost)
	}
	if req.Emotion != "" && !contains(supportedEmotions, req.Emotion) {
		return Request{}, fmt.Errorf("tts: unsupported emotion %q", req.Emotion)
	}
	for i, rule := range req.PronunciationDict {
		if strings.TrimSpace(rule) == "" || !strings.Contains(rule, "/") {
			return Request{}, fmt.Errorf("tts: pronunciationDict[%d] must use original/replacement format", i)
		}
	}
	return req, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func newHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil || proxy.Scheme == "" || proxy.Host == "" {
			return nil, fmt.Errorf("tts: proxy url %q must be absolute", proxyURL)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Transport: transport}, nil
}
