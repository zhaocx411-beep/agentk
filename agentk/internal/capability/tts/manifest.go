package tts

import (
	_ "embed"
	"time"

	sdkcapability "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/worker/capability"
)

const (
	MiniMaxCapabilityID   = "agentk.generate_speech_minimax"
	QwenCapabilityID      = "agentk.generate_speech_qwen"
	CosyVoiceCapabilityID = "agentk.generate_speech_cosyvoice"

	MiniMaxToolName   = "generate_speech_minimax"
	QwenToolName      = "generate_speech_qwen"
	CosyVoiceToolName = "generate_speech_cosyvoice"

	activityStartToClose = 5 * time.Minute
	maxActivityAttempts  = 2
)

//go:embed generate_speech_minimax.schema.json
var miniMaxInputSchema []byte

//go:embed generate_speech_qwen.schema.json
var qwenInputSchema []byte

//go:embed generate_speech_cosyvoice.schema.json
var cosyVoiceInputSchema []byte

func ManifestCapabilities() []sdkcapability.Capability {
	return []sdkcapability.Capability{
		{
			ID:             MiniMaxCapabilityID,
			TemporalType:   MiniMaxActivityName,
			Description:    "使用 MiniMax 将文字合成为语音并返回受管音频 URL。",
			InputSchema:    miniMaxInputSchema,
			OutputSchema:   sdkcapability.SchemaOf[CapabilityOutput](),
			TimeoutSeconds: int64(activityStartToClose.Seconds()),
			Tool: &sdkcapability.Tool{
				Description: "使用 MiniMax 将文字合成为语音并返回受管音频 URL。",
				Mode:        sdkcapability.ToolModeBlocking,
				MaxAttempts: maxActivityAttempts,
			},
		},
		{
			ID:             QwenCapabilityID,
			TemporalType:   QwenActivityName,
			Description:    "使用 Qwen TTS 将文字合成为语音并返回受管音频 URL。",
			InputSchema:    qwenInputSchema,
			OutputSchema:   sdkcapability.SchemaOf[CapabilityOutput](),
			TimeoutSeconds: int64(activityStartToClose.Seconds()),
			Tool: &sdkcapability.Tool{
				Description: "使用 Qwen TTS 将文字合成为语音并返回受管音频 URL。",
				Mode:        sdkcapability.ToolModeBlocking,
				MaxAttempts: maxActivityAttempts,
			},
		},
		{
			ID:             CosyVoiceCapabilityID,
			TemporalType:   CosyVoiceActivityName,
			Description:    "使用 CosyVoice 将文字合成为语音并返回受管音频 URL。",
			InputSchema:    cosyVoiceInputSchema,
			OutputSchema:   sdkcapability.SchemaOf[CapabilityOutput](),
			TimeoutSeconds: int64(activityStartToClose.Seconds()),
			Tool: &sdkcapability.Tool{
				Description: "使用 CosyVoice 将文字合成为语音并返回受管音频 URL。",
				Mode:        sdkcapability.ToolModeBlocking,
				MaxAttempts: maxActivityAttempts,
			},
		},
	}
}
