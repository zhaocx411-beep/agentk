package tts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"agentk/internal/artifact"
	"agentk/internal/modelcontract"
	"agentk/internal/temporalx"
	"agentk/internal/tool"
)

const (
	MiniMaxActivityName   = "agentk.capability.generate_speech_minimax"
	QwenActivityName      = "agentk.capability.generate_speech_qwen"
	CosyVoiceActivityName = "agentk.capability.generate_speech_cosyvoice"
)

type MiniMaxInput struct {
	Model             string   `json:"model"`
	Text              string   `json:"text"`
	VoiceID           string   `json:"voiceId"`
	Speed             float64  `json:"speed,omitempty"`
	LanguageBoost     string   `json:"languageBoost,omitempty"`
	Emotion           string   `json:"emotion,omitempty"`
	PronunciationDict []string `json:"pronunciationDict,omitempty"`
}

type QwenInput struct {
	Model                string `json:"model"`
	Text                 string `json:"text"`
	VoiceID              string `json:"voiceId"`
	LanguageType         string `json:"languageType,omitempty"`
	Instructions         string `json:"instructions,omitempty"`
	OptimizeInstructions bool   `json:"optimizeInstructions,omitempty"`
}

type CosyVoiceInput struct {
	Model        string `json:"model"`
	Text         string `json:"text"`
	VoiceID      string `json:"voiceId"`
	Instructions string `json:"instructions,omitempty"`
}

type CapabilityOutput struct {
	Artifacts []tool.ResultArtifact `json:"artifacts"`
	Metadata  Metadata              `json:"metadata,omitempty"`
}

type ModelLookup interface {
	TTSClient(name string) (Client, bool)
}

type Activity struct {
	models  ModelLookup
	storage artifact.Storage
}

func NewActivity(models ModelLookup, storage artifact.Storage) *Activity {
	return &Activity{models: models, storage: storage}
}

func (a *Activity) RunMiniMax(ctx context.Context, in MiniMaxInput) (CapabilityOutput, error) {
	return a.run(ctx, "capability generate_speech_minimax", in.Model, in.Text, in.VoiceID,
		modelcontract.FamilyTTSMiniMax, Request{
			Text: in.Text, VoiceID: in.VoiceID, Speed: in.Speed,
			LanguageBoost: in.LanguageBoost, Emotion: in.Emotion,
			PronunciationDict: in.PronunciationDict,
		})
}

func (a *Activity) RunQwen(ctx context.Context, in QwenInput) (CapabilityOutput, error) {
	return a.run(ctx, "capability generate_speech_qwen", in.Model, in.Text, in.VoiceID,
		modelcontract.FamilyTTSQwen, Request{
			Text: in.Text, VoiceID: in.VoiceID, LanguageType: in.LanguageType,
			Instructions: in.Instructions, OptimizeInstructions: in.OptimizeInstructions,
		})
}

func (a *Activity) RunCosyVoice(ctx context.Context, in CosyVoiceInput) (CapabilityOutput, error) {
	return a.run(ctx, "capability generate_speech_cosyvoice", in.Model, in.Text, in.VoiceID,
		modelcontract.FamilyTTSCosyVoice, Request{
			Text: in.Text, VoiceID: in.VoiceID, Instructions: in.Instructions,
		})
}

func (a *Activity) run(
	ctx context.Context,
	op string,
	model string,
	text string,
	voiceID string,
	family modelcontract.Family,
	req Request,
) (CapabilityOutput, error) {
	if a.models == nil || a.storage == nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("tts model lookup and storage are required")))
	}
	if model == "" {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model is required"))
	}
	def, ok := modelcontract.Lookup(model)
	if !ok || def.Family != family {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("model %q is not valid for this capability", model))
	}
	if err := validateBaseRequest(req); err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	client, ok := a.models.TTSClient(model)
	if !ok {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("tts model %q is not configured", model)))
	}
	if err := client.ValidateRequest(req); err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	audio, err := client.Synthesize(ctx, req)
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": synthesize", err)
	}
	object, err := a.storage.Put(ctx, capabilityAudioKey(ctx, audio.MIME), audio.Data, audio.MIME)
	if err != nil {
		return CapabilityOutput{}, temporalx.ClassifyActivityError(op+": store audio", err)
	}
	return CapabilityOutput{
		Artifacts: []tool.ResultArtifact{{
			Type: "audio", URI: object.URL,
			Description: fmt.Sprintf(
				"将文本 %q 合成为语音（模型：%s，音色：%s）",
				text, model, voiceID),
			Model: model, Params: audio.Params,
		}},
		Metadata: audio.Metadata,
	}, nil
}

func capabilityAudioKey(ctx context.Context, mime string) string {
	sum := sha256.Sum256([]byte(temporalx.ActivityIdempotencyKey(ctx)))
	ext := ".bin"
	switch strings.ToLower(mime) {
	case "audio/mpeg", "audio/mp3":
		ext = ".mp3"
	case "audio/wav", "audio/x-wav", "audio/vnd.wave":
		ext = ".wav"
	case "audio/ogg":
		ext = ".ogg"
	}
	return fmt.Sprintf("capabilities/audio/%x%s", sum[:16], ext)
}
