package tts

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultMiniMaxBaseURL = "https://api.minimax.chat"
	miniMaxRateLimitCode  = 1002
	maxResponseBytes      = maxAudioBytes*2 + 1<<20
)

// DefaultMiniMaxVoices 是 MiniMax provider 在 models catalog 未配 voices 时的内置候选。
// catalog 显式配置 voices 时以配置为准（整表覆盖，不与默认合并）。
var DefaultMiniMaxVoices = []VoiceOption{
	{ID: "Chinese (Mandarin)_Lyrical_Voice", Description: "中文普通话抒情音色"},
	{ID: "Chinese (Mandarin)_HK_Flight_Attendant", Description: "中文普通话空乘音色"},
	{ID: "Wise_Woman", Description: "威严成熟女声"},
	{ID: "Calm_Woman", Description: "沉稳女声"},
	{ID: "Lively_Girl", Description: "活泼年轻女声"},
	{ID: "Lovely_Girl", Description: "可爱年轻女声"},
	{ID: "Deep_Voice_Man", Description: "低沉威严男声"},
	{ID: "Elegant_Man", Description: "儒雅男声"},
	{ID: "Patient_Man", Description: "温和中年男声"},
	{ID: "Casual_Guy", Description: "随性男声"},
	{ID: "Decent_Boy", Description: "干净年轻男声"},
}

type MiniMaxConfig struct {
	BaseURL  string
	APIKey   string
	GroupID  string
	Model    string
	ProxyURL string
	Client   *http.Client

	// 测试可注入；生产缺省按 5s 起步、60s 封顶进行可取消等待。
	Sleep               func(context.Context, time.Duration) error
	MaxRateLimitRetries int
}

type MiniMax struct {
	cfg      MiniMaxConfig
	http     *http.Client
	endpoint string
}

func NewMiniMax(cfg MiniMaxConfig) (*MiniMax, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultMiniMaxBaseURL
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, fmt.Errorf("tts: minimax baseUrl %q must be an absolute HTTP(S) URL", cfg.BaseURL)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("tts: minimax api key is required")
	}
	if cfg.GroupID == "" {
		return nil, fmt.Errorf("tts: minimax groupId is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("tts: minimax model is required")
	}
	client := cfg.Client
	if client == nil {
		client, err = newHTTPClient(cfg.ProxyURL)
		if err != nil {
			return nil, err
		}
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepContext
	}
	if cfg.MaxRateLimitRetries == 0 {
		cfg.MaxRateLimitRetries = 5
	}
	if cfg.MaxRateLimitRetries < 0 {
		return nil, fmt.Errorf("tts: minimax max rate limit retries must be non-negative")
	}
	endpoint := strings.TrimSuffix(cfg.BaseURL, "/") + "/v1/t2a_v2"
	parsed, _ := url.Parse(endpoint)
	query := parsed.Query()
	query.Set("GroupId", cfg.GroupID)
	parsed.RawQuery = query.Encode()
	return &MiniMax{cfg: cfg, http: client, endpoint: parsed.String()}, nil
}

func (*MiniMax) ValidateRequest(req Request) error {
	if req.LanguageType != "" || req.Instructions != "" || req.OptimizeInstructions {
		return fmt.Errorf("tts: languageType, instructions and optimizeInstructions are only supported by DashScope Qwen TTS")
	}
	_, err := normalizeRequest(req)
	return err
}

func (m *MiniMax) Synthesize(ctx context.Context, req Request) (Audio, error) {
	if err := m.ValidateRequest(req); err != nil {
		return Audio{}, err
	}
	var err error
	req, err = normalizeRequest(req)
	if err != nil {
		return Audio{}, err
	}

	voiceSetting := map[string]any{
		"voice_id": req.VoiceID,
		"speed":    req.Speed,
		"vol":      1.0,
		"pitch":    0,
	}
	if req.Emotion != "" {
		voiceSetting["emotion"] = req.Emotion
	}
	body := map[string]any{
		"model":          m.cfg.Model,
		"text":           req.Text,
		"language_boost": req.LanguageBoost,
		"voice_setting":  voiceSetting,
		"audio_setting": map[string]any{
			"sample_rate": 32000,
			"bitrate":     128000,
			"format":      "mp3",
			"channel":     1,
		},
	}
	if len(req.PronunciationDict) > 0 {
		body["pronunciation_dict"] = map[string]any{"tone": req.PronunciationDict}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Audio{}, err
	}

	for attempt := 0; ; attempt++ {
		audio, metadata, statusCode, err := m.call(ctx, payload)
		if err == nil {
			params := map[string]any{
				"voiceId": req.VoiceID, "speed": req.Speed,
				"languageBoost": req.LanguageBoost, "format": "mp3",
				"sampleRate": 32000, "bitrate": 128000, "channel": 1,
			}
			if req.Emotion != "" {
				params["emotion"] = req.Emotion
			}
			if len(req.PronunciationDict) > 0 {
				params["pronunciationDict"] = req.PronunciationDict
			}
			return Audio{
				Data: audio, MIME: "audio/mpeg", Params: params, Metadata: metadata,
			}, nil
		}
		if statusCode != miniMaxRateLimitCode || attempt >= m.cfg.MaxRateLimitRetries {
			return Audio{}, err
		}
		if err := m.cfg.Sleep(ctx, retryDelay(attempt)); err != nil {
			return Audio{}, fmt.Errorf("tts: minimax rate limit wait: %w", err)
		}
	}
}

func (m *MiniMax) call(ctx context.Context, payload []byte) ([]byte, Metadata, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, Metadata{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax synthesize: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax response exceeds %d bytes", maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax synthesize: HTTP %d: %s", resp.StatusCode, responseSnippet(raw))
	}
	var body struct {
		Data struct {
			Audio  string `json:"audio"`
			Status int    `json:"status"`
		} `json:"data"`
		TraceID   string `json:"trace_id"`
		ExtraInfo struct {
			AudioLength             int64   `json:"audio_length"`
			AudioSampleRate         int64   `json:"audio_sample_rate"`
			AudioSize               int64   `json:"audio_size"`
			Bitrate                 int64   `json:"bitrate"`
			WordCount               int64   `json:"word_count"`
			InvisibleCharacterRatio float64 `json:"invisible_character_ratio"`
			UsageCharacters         int64   `json:"usage_characters"`
			AudioFormat             string  `json:"audio_format"`
			AudioChannel            int64   `json:"audio_channel"`
		} `json:"extra_info"`
		BaseResp struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax parse response: %w (%s)", err, responseSnippet(raw))
	}
	if body.BaseResp.StatusCode != 0 {
		return nil, Metadata{}, body.BaseResp.StatusCode, fmt.Errorf(
			"tts: minimax failed: %d %s (traceId=%s)",
			body.BaseResp.StatusCode, body.BaseResp.StatusMsg, body.TraceID)
	}
	if body.Data.Status != 2 {
		return nil, Metadata{}, 0, fmt.Errorf(
			"tts: minimax non-streaming response status is %d, want 2 (traceId=%s)",
			body.Data.Status, body.TraceID)
	}
	if body.Data.Audio == "" {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax response has no audio (traceId=%s)", body.TraceID)
	}
	if len(body.Data.Audio) > maxAudioBytes*2 {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax audio exceeds %d bytes", maxAudioBytes)
	}
	audio, err := hex.DecodeString(body.Data.Audio)
	if err != nil {
		return nil, Metadata{}, 0, fmt.Errorf("tts: minimax decode audio: %w (traceId=%s)", err, body.TraceID)
	}
	metadata := Metadata{
		TraceID: body.TraceID, AudioLengthMs: body.ExtraInfo.AudioLength,
		AudioSizeBytes: body.ExtraInfo.AudioSize, UsageCharacters: body.ExtraInfo.UsageCharacters,
		WordCount: body.ExtraInfo.WordCount, InvisibleCharacterRatio: body.ExtraInfo.InvisibleCharacterRatio,
		SampleRate: body.ExtraInfo.AudioSampleRate, Bitrate: body.ExtraInfo.Bitrate,
		Format: body.ExtraInfo.AudioFormat, Channel: body.ExtraInfo.AudioChannel,
	}
	return audio, metadata, 0, nil
}

func retryDelay(attempt int) time.Duration {
	delay := 5 * time.Second << attempt
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func responseSnippet(raw []byte) string {
	value := strings.TrimSpace(string(raw))
	if len(value) > 300 {
		return value[:300] + "..."
	}
	return value
}
