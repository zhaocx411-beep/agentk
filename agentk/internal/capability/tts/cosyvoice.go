package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const DefaultCosyVoiceBaseURL = "https://dashscope.aliyuncs.com"

type CosyVoiceConfig struct {
	BaseURL  string
	APIKey   string
	Model    string
	ProxyURL string
	Client   *http.Client
}

type CosyVoice struct {
	cfg  CosyVoiceConfig
	http *http.Client
}

func NewCosyVoice(cfg CosyVoiceConfig) (*CosyVoice, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultCosyVoiceBaseURL
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("tts: cosyvoice baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("tts: cosyvoice api key is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("tts: cosyvoice model is required")
	}
	client := cfg.Client
	if client == nil {
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &CosyVoice{cfg: cfg, http: client}, nil
}

func (c *CosyVoice) endpoint() string {
	base := strings.TrimSuffix(c.cfg.BaseURL, "/")
	if strings.HasSuffix(base, "/api/v1") {
		return base + "/services/audio/tts/SpeechSynthesizer"
	}
	return base + "/api/v1/services/audio/tts/SpeechSynthesizer"
}

func (c *CosyVoice) ValidateRequest(req Request) error {
	if err := validateBaseRequest(req); err != nil {
		return err
	}
	if req.Speed != 0 || req.LanguageBoost != "" || req.LanguageType != "" ||
		req.Emotion != "" || len(req.PronunciationDict) > 0 || req.OptimizeInstructions {
		return fmt.Errorf("tts: cosyvoice supports text, voiceId and instructions only")
	}
	if cosyVoiceInstructionLength(req.Instructions) > 100 {
		return fmt.Errorf("tts: cosyvoice instructions exceed the 100-character weighted limit")
	}
	return nil
}

func (c *CosyVoice) Synthesize(ctx context.Context, req Request) (Audio, error) {
	if err := c.ValidateRequest(req); err != nil {
		return Audio{}, err
	}
	input := map[string]any{
		"text": req.Text, "voice": req.VoiceID, "format": "wav", "sample_rate": 24000,
	}
	if req.Instructions != "" {
		input["instruction"] = req.Instructions
	}
	payload, err := json.Marshal(map[string]any{"model": c.cfg.Model, "input": input})
	if err != nil {
		return Audio{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Audio{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	resp, err := c.http.Do(hreq)
	if err != nil {
		return Audio{}, fmt.Errorf("tts: cosyvoice synthesize: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioBytes*2+1))
	if err != nil {
		return Audio{}, fmt.Errorf("tts: cosyvoice read response: %w", err)
	}
	if len(raw) > maxAudioBytes*2 {
		return Audio{}, fmt.Errorf("tts: cosyvoice response exceeds %d bytes", maxAudioBytes*2)
	}
	var body struct {
		RequestID string `json:"request_id"`
		Code      string `json:"code"`
		Message   string `json:"message"`
		Output    struct {
			Audio struct {
				Data      string `json:"data"`
				URL       string `json:"url"`
				ID        string `json:"id"`
				ExpiresAt int64  `json:"expires_at"`
			} `json:"audio"`
		} `json:"output"`
		Usage struct {
			Characters int64 `json:"characters"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Audio{}, fmt.Errorf("tts: cosyvoice parse response: %w (%s)", err, responseSnippet(raw))
	}
	if resp.StatusCode != http.StatusOK || body.Code != "" {
		return Audio{}, fmt.Errorf("tts: cosyvoice synthesize: HTTP %d code=%q message=%q",
			resp.StatusCode, body.Code, body.Message)
	}
	data, contentType, err := resolveAudioBytes(ctx, c.http, body.Output.Audio.Data, body.Output.Audio.URL)
	if err != nil {
		return Audio{}, err
	}
	params := map[string]any{
		"voiceId": req.VoiceID, "format": "wav", "sampleRate": 24000,
	}
	if req.Instructions != "" {
		params["instructions"] = req.Instructions
	}
	return Audio{
		Data: data, MIME: contentType, Params: params,
		Metadata: Metadata{
			RequestID: body.RequestID, AudioID: body.Output.Audio.ID,
			ExpiresAt: body.Output.Audio.ExpiresAt, AudioSizeBytes: int64(len(data)),
			UsageCharacters: body.Usage.Characters, Format: "wav", SampleRate: 24000,
		},
	}, nil
}

// CosyVoice 文档按加权字符限制 instruction：汉字计 2，其他字符计 1。
func cosyVoiceInstructionLength(value string) int {
	total := 0
	for _, r := range value {
		if r >= 0x3400 && r <= 0x9fff {
			total += 2
		} else {
			total++
		}
	}
	return total
}
