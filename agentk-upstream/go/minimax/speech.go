package minimax

import (
	"context"
	"encoding/hex"
	"encoding/json"
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

type SpeechClient struct {
	base string
	http *http.Client
}

func NewSpeech(r gateway.Route, opts ...upstream.Option) (*SpeechClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &SpeechClient{strings.TrimRight(base.String(), "/"), c}, nil
}

type SpeechRequest struct {
	SampleRate, Bitrate, Channel                          int
	Format                                                string
	Volume                                                float64
	Pitch                                                 int
	Model, GroupID, Text, VoiceID, LanguageBoost, Emotion string
	Speed                                                 float64
	PronunciationDict                                     []string
}
type Metadata struct {
	TraceID                 string
	AudioLengthMs           int64
	AudioSizeBytes          int64
	UsageCharacters         int64
	WordCount               int64
	InvisibleCharacterRatio float64
	SampleRate              int64
	Bitrate                 int64
	Format                  string
	Channel                 int64
}

type SpeechResponse struct {
	Audio    []byte
	Metadata Metadata
}

func (m *SpeechClient) Synthesize(ctx context.Context, req SpeechRequest) (SpeechResponse, error) {
	voiceSetting := map[string]any{
		"voice_id": req.VoiceID,
		"speed":    req.Speed,
		"vol":      req.Volume,
		"pitch":    req.Pitch,
	}
	if req.Emotion != "" {
		voiceSetting["emotion"] = req.Emotion
	}
	payload := map[string]any{
		"model":          req.Model,
		"text":           req.Text,
		"language_boost": req.LanguageBoost,
		"voice_setting":  voiceSetting,
		"audio_setting": map[string]any{
			"sample_rate": req.SampleRate,
			"bitrate":     req.Bitrate,
			"format":      req.Format,
			"channel":     req.Channel,
		},
	}
	if len(req.PronunciationDict) > 0 {
		payload["pronunciation_dict"] = map[string]any{"tone": req.PronunciationDict}
	}

	endpoint, err := url.Parse(m.base + "/v1/t2a_v2")
	if err != nil {
		return SpeechResponse{}, err
	}
	query := endpoint.Query()
	query.Set("GroupId", req.GroupID)
	endpoint.RawQuery = query.Encode()
	raw, resp, err := httpcall.JSON(ctx, m.http, http.MethodPost, endpoint.String(), payload)
	if err != nil {
		return SpeechResponse{}, err
	}
	if resp.StatusCode != 200 {
		return SpeechResponse{}, httpcall.Error(resp, "", httpcall.Snippet(raw))
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
		return SpeechResponse{}, fmt.Errorf("tts: minimax parse response: %w (%s)", err, httpcall.Snippet(raw))
	}
	if body.BaseResp.StatusCode != 0 {
		return SpeechResponse{}, &upstream.APIError{
			StatusCode: resp.StatusCode,
			Code:       strconv.Itoa(body.BaseResp.StatusCode),
			Message:    body.BaseResp.StatusMsg,
			RequestID:  body.TraceID,
		}
	}
	if body.Data.Status != 2 {
		return SpeechResponse{}, fmt.Errorf(
			"tts: minimax non-streaming response status is %d, want 2 (traceId=%s)",
			body.Data.Status, body.TraceID)
	}
	if body.Data.Audio == "" {
		return SpeechResponse{}, fmt.Errorf("tts: minimax response has no audio (traceId=%s)", body.TraceID)
	}
	audio, err := hex.DecodeString(body.Data.Audio)
	if err != nil {
		return SpeechResponse{}, fmt.Errorf("tts: minimax decode audio: %w (traceId=%s)", err, body.TraceID)
	}
	metadata := Metadata{
		TraceID: body.TraceID, AudioLengthMs: body.ExtraInfo.AudioLength,
		AudioSizeBytes: body.ExtraInfo.AudioSize, UsageCharacters: body.ExtraInfo.UsageCharacters,
		WordCount: body.ExtraInfo.WordCount, InvisibleCharacterRatio: body.ExtraInfo.InvisibleCharacterRatio,
		SampleRate: body.ExtraInfo.AudioSampleRate, Bitrate: body.ExtraInfo.Bitrate,
		Format: body.ExtraInfo.AudioFormat, Channel: body.ExtraInfo.AudioChannel,
	}
	return SpeechResponse{Audio: audio, Metadata: metadata}, nil
}
