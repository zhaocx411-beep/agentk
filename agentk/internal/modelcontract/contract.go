package modelcontract

type Family string

const (
	FamilyChat            Family = "chat"
	FamilyKlingT2V        Family = "kling_t2v"
	FamilyKlingI2V        Family = "kling_i2v"
	FamilyKlingOmni       Family = "kling_omni"
	FamilySeedance        Family = "seedance"
	FamilyImage           Family = "image"
	FamilyImageEdit       Family = "image_edit"
	FamilyMediaUnderstand Family = "media_understand"
	FamilySearch          Family = "search"
	FamilyTTSMiniMax      Family = "tts_minimax"
	FamilyTTSQwen         Family = "tts_qwen"
	FamilyTTSCosyVoice    Family = "tts_cosyvoice"
	FamilyASR             Family = "asr"
)

type Dialect string

const (
	DialectOpenAI     Dialect = "openai"
	DialectKling      Dialect = "kling"
	DialectKlingT2V   Dialect = "kling-t2v"
	DialectKlingOmni  Dialect = "kling-omni"
	DialectSeedance   Dialect = "seedance"
	DialectAzure      Dialect = "azure"
	DialectDashScope  Dialect = "dashscope"
	DialectGPTImage   Dialect = "gptimage"
	DialectGemini     Dialect = "gemini"
	DialectQwenEdit   Dialect = "qwenedit"
	DialectVolc       Dialect = "volc"
	DialectBrightSERP Dialect = "brightserp"
	DialectMiniMax    Dialect = "minimax"
	DialectCosyVoice  Dialect = "cosyvoice"
)

type Definition struct {
	ID      string
	Family  Family
	Dialect Dialect
}

var definitions = []Definition{
	{ID: "deepseek-v4-pro", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "deepseek-v4-flash", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "qwen3.5-plus", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "qwen3.7-max", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "gpt-5.4", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "claude-sonnet-4-6", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "claude-haiku-4-5", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "glm-5.1", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "kimi-k2.6", Family: FamilyChat, Dialect: DialectOpenAI},
	{ID: "kling-3.0", Family: FamilyKlingI2V, Dialect: DialectKling},
	{ID: "kling-3.0-t2v", Family: FamilyKlingT2V, Dialect: DialectKlingT2V},
	{ID: "kling-3.0-omni", Family: FamilyKlingOmni, Dialect: DialectKlingOmni},
	{ID: "doubao-seedance-2.0", Family: FamilySeedance, Dialect: DialectSeedance},
	{ID: "z-image-turbo", Family: FamilyImage, Dialect: DialectDashScope},
	{ID: "gpt-image-2", Family: FamilyImage, Dialect: DialectGPTImage},
	{ID: "gemini-3-pro-image", Family: FamilyImage, Dialect: DialectGemini},
	{ID: "gemini-3.1-flash-image", Family: FamilyImage, Dialect: DialectGemini},
	{ID: "gpt-image-2-edit", Family: FamilyImageEdit, Dialect: DialectGPTImage},
	{ID: "qwen-image-edit-plus-2025-10-30", Family: FamilyImageEdit, Dialect: DialectQwenEdit},
	{ID: "gemini-3-pro-image-edit", Family: FamilyImageEdit, Dialect: DialectGemini},
	{ID: "gemini-3.1-pro-preview", Family: FamilyMediaUnderstand, Dialect: DialectGemini},
	{ID: "gemini-3-flash-preview", Family: FamilyMediaUnderstand, Dialect: DialectGemini},
	{ID: "gemini-3.1-flash-lite", Family: FamilyMediaUnderstand, Dialect: DialectGemini},
	{ID: "volc-search", Family: FamilySearch, Dialect: DialectVolc},
	{ID: "brightdata-serp", Family: FamilySearch, Dialect: DialectBrightSERP},
	{ID: "minimax-speech-2.8-hd", Family: FamilyTTSMiniMax, Dialect: DialectMiniMax},
	{ID: "qwen3-tts-flash", Family: FamilyTTSQwen, Dialect: DialectDashScope},
	{ID: "cosyvoice-v3.5-plus", Family: FamilyTTSCosyVoice, Dialect: DialectCosyVoice},
	{ID: "azure-whisper", Family: FamilyASR, Dialect: DialectAzure},
}

var byID = func() map[string]Definition {
	result := make(map[string]Definition, len(definitions))
	for _, def := range definitions {
		result[def.ID] = def
	}
	return result
}()

func Lookup(id string) (Definition, bool) {
	def, ok := byID[id]
	return def, ok
}
