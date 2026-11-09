package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const DefaultDashScopeBaseURL = "https://dashscope.aliyuncs.com"

// DefaultDashScopeVoices 是 DashScope（Qwen3-TTS）provider 在 models catalog 未配
// voices 时的内置候选，覆盖官方非实时音色表中支持 Qwen3-TTS-Flash 的系统音色。
// catalog 显式配置 voices 时以配置为准（整表覆盖，不与默认合并）。
var DefaultDashScopeVoices = []VoiceOption{
	{ID: "Cherry", Description: "芊晓：阳光亲切女声"},
	{ID: "Serena", Description: "佩霞：温婉女声"},
	{ID: "Ethan", Description: "晨煦：阳光稳重男声"},
	{ID: "Christie", Description: "千雪：二次元萌系女声"},
	{ID: "Rono", Description: "元兔：俏皮活泼女声"},
	{ID: "Vivian", Description: "十三：甜美女声"},
	{ID: "Ryan", Description: "沐白：清爽大气男声"},
	{ID: "Rana", Description: "兰月：温柔女声"},
	{ID: "Kai", Description: "凯：未来感男声"},
	{ID: "Nafish", Description: "不败枭：低沉男声"},
	{ID: "Bella", Description: "碧宝：俏皮稳重女声"},
	{ID: "Jennifer", Description: "珍妮弗：知性磁性女声"},
}

var qwenLanguageTypes = map[string]bool{
	"Auto": true, "Chinese": true, "English": true, "German": true,
	"Italian": true, "Portuguese": true, "Spanish": true, "Japanese": true,
	"Korean": true, "French": true, "Russian": true,
}

type DashScopeConfig struct {
	BaseURL  string
	APIKey   string
	Model    string
	ProxyURL string
	Client   *http.Client
}

type DashScope struct {
	cfg  DashScopeConfig
	http *http.Client
}

func NewDashScope(cfg DashScopeConfig) (*DashScope, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultDashScopeBaseURL
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("tts: dashscope baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("tts: dashscope api key is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("tts: dashscope model is required")
	}
	client := cfg.Client
	if client == nil {
		if client, err = newHTTPClient(cfg.ProxyURL); err != nil {
			return nil, err
		}
	}
	return &DashScope{cfg: cfg, http: client}, nil
}

func (d *DashScope) endpoint() string {
	base := strings.TrimSuffix(d.cfg.BaseURL, "/")
	if strings.HasSuffix(base, "/api/v1") {
		return base + "/services/aigc/multimodal-generation/generation"
	}
	return base + "/api/v1/services/aigc/multimodal-generation/generation"
}

func (d *DashScope) ValidateRequest(req Request) error {
	if err := validateBaseRequest(req); err != nil {
		return err
	}
	if req.Speed != 0 || req.LanguageBoost != "" || req.Emotion != "" || len(req.PronunciationDict) > 0 {
		return fmt.Errorf("tts: speed, languageBoost, emotion and pronunciationDict are only supported by MiniMax")
	}
	if req.LanguageType == "" {
		req.LanguageType = "Auto"
	}
	if !qwenLanguageTypes[req.LanguageType] {
		return fmt.Errorf("tts: unsupported Qwen languageType %q", req.LanguageType)
	}
	if req.OptimizeInstructions && strings.TrimSpace(req.Instructions) == "" {
		return fmt.Errorf("tts: optimizeInstructions requires instructions")
	}
	if req.Instructions != "" && !strings.Contains(strings.ToLower(d.cfg.Model), "instruct") {
		return fmt.Errorf("tts: instructions require a Qwen TTS instruct model")
	}
	return nil
}

func (d *DashScope) Synthesize(ctx context.Context, req Request) (Audio, error) {
	if err := d.ValidateRequest(req); err != nil {
		return Audio{}, err
	}
	if req.LanguageType == "" {
		req.LanguageType = "Auto"
	}
	input := map[string]any{
		"text": req.Text, "voice": req.VoiceID, "language_type": req.LanguageType,
	}
	if req.Instructions != "" {
		input["instructions"] = req.Instructions
		input["optimize_instructions"] = req.OptimizeInstructions
	}
	payload, err := json.Marshal(map[string]any{"model": d.cfg.Model, "input": input})
	if err != nil {
		return Audio{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Audio{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+d.cfg.APIKey)
	resp, err := d.http.Do(hreq)
	if err != nil {
		return Audio{}, fmt.Errorf("tts: dashscope synthesize: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioBytes*2+1))
	if err != nil {
		return Audio{}, fmt.Errorf("tts: dashscope read response: %w", err)
	}
	if len(raw) > maxAudioBytes*2 {
		return Audio{}, fmt.Errorf("tts: dashscope response exceeds %d bytes", maxAudioBytes*2)
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
			Characters   int64 `json:"characters"`
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Audio{}, fmt.Errorf("tts: dashscope parse response: %w (%s)", err, responseSnippet(raw))
	}
	if resp.StatusCode != http.StatusOK || body.Code != "" {
		return Audio{}, fmt.Errorf("tts: dashscope synthesize: HTTP %d code=%q message=%q",
			resp.StatusCode, body.Code, body.Message)
	}
	data, contentType, err := resolveAudioBytes(ctx, d.http, body.Output.Audio.Data, body.Output.Audio.URL)
	if err != nil {
		return Audio{}, err
	}
	params := map[string]any{
		"voiceId": req.VoiceID, "languageType": req.LanguageType,
	}
	if req.Instructions != "" {
		params["instructions"] = req.Instructions
		params["optimizeInstructions"] = req.OptimizeInstructions
	}
	return Audio{
		Data: data, MIME: contentType, Params: params,
		Metadata: Metadata{
			RequestID: body.RequestID, AudioID: body.Output.Audio.ID,
			ExpiresAt: body.Output.Audio.ExpiresAt, AudioSizeBytes: int64(len(data)),
			UsageCharacters: body.Usage.Characters, InputTokens: body.Usage.InputTokens,
			OutputTokens: body.Usage.OutputTokens, TotalTokens: body.Usage.TotalTokens,
		},
	}, nil
}

func resolveAudioBytes(
	ctx context.Context, client *http.Client, encoded, rawURL string,
) ([]byte, string, error) {
	if encoded != "" {
		if len(encoded) > maxAudioBytes*2 {
			return nil, "", fmt.Errorf("tts: dashscope encoded audio exceeds %d bytes", maxAudioBytes)
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, "", fmt.Errorf("tts: dashscope decode audio: %w", err)
		}
		if len(data) == 0 || len(data) > maxAudioBytes {
			return nil, "", fmt.Errorf("tts: dashscope audio size %d is invalid", len(data))
		}
		return data, detectAudioMIME("", "", data), nil
	}
	target, err := url.Parse(rawURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, "", fmt.Errorf("tts: dashscope response has no audio data or valid url")
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return nil, "", fmt.Errorf("tts: dashscope download audio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("tts: dashscope download audio: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("tts: dashscope read audio: %w", err)
	}
	if len(data) == 0 || len(data) > maxAudioBytes {
		return nil, "", fmt.Errorf("tts: dashscope audio size %d is invalid", len(data))
	}
	return data, detectAudioMIME(target.Path, resp.Header.Get("Content-Type"), data), nil
}

func detectAudioMIME(name, contentType string, data []byte) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	}
	if declared, _, err := mime.ParseMediaType(contentType); err == nil && strings.HasPrefix(declared, "audio/") {
		return declared
	}
	if sniffed := http.DetectContentType(data); strings.HasPrefix(sniffed, "audio/") {
		if parsed, _, err := mime.ParseMediaType(sniffed); err == nil {
			if parsed == "audio/wave" || parsed == "audio/x-wav" || parsed == "audio/vnd.wave" {
				return "audio/wav"
			}
			return parsed
		}
	}
	return "audio/wav"
}
