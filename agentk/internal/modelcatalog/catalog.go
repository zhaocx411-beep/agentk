// Package modelcatalog 解析模型目录并构造能力客户端。
// 生产目录由 KConf 下发，凭证明文的安全边界是对应 KConf key 的访问权限；本地目录
// 可通过环境变量引用凭证。目录不落盘，也不得通过 API 回显、写入日志或持久化到
// 可编辑、可回显的 Session Profile。
package modelcatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"agentk/internal/artifact"
	"agentk/internal/capability/asr"
	"agentk/internal/capability/imagegen"
	"agentk/internal/capability/mediaunderstand"
	"agentk/internal/capability/tts"
	"agentk/internal/capability/videogen"
	"agentk/internal/capability/webfetch"
	"agentk/internal/capability/websearch"
	"agentk/internal/chat"
	openaichat "agentk/internal/chat/openai"
	"agentk/internal/event"
	"agentk/internal/modelcontract"
)

// ModelDecl 描述逻辑模型的上游地址、凭证和 provider 配置。
type ModelDecl struct {
	Name string `yaml:"name" json:"name"`
	// BaseURL 的要求由 family 和 provider 决定，最终路径由 client 拼接：chat 必填；
	// Kling video 可省略并使用官网，Seedance 使用截到 /api/v3 的基址；Azure image
	// 使用 /openai/deployments 之前的前缀；qwenedit 使用截到 /api/v1 的网关基址；
	// DashScope image、search 和 TTS 可使用各自默认官网端点；Azure ASR 必填。
	BaseURL string `yaml:"baseUrl" json:"baseUrl,omitempty"`
	// APIVersion 覆盖 Azure image、image_edit 或 ASR 的默认 API 版本。
	APIVersion string `yaml:"apiVersion" json:"apiVersion,omitempty"`
	// ResponseFormat 仅用于 Azure ASR，可取 json 或包含时间轴的 verbose_json。
	ResponseFormat string `yaml:"responseFormat" json:"responseFormat,omitempty"`
	// ModelName 覆盖上游模型标识；为空时使用 Name。它用于将业务别名与 provider
	// model、URL path segment 或 Bright Data zone 解耦。
	ModelName string `yaml:"modelName" json:"modelName,omitempty"`
	// GroupID 仅用于 MiniMax TTS。
	GroupID string `yaml:"groupId" json:"groupId,omitempty"`
	// Voices 限制 TTS 条目允许使用的音色。
	Voices []tts.VoiceOption `yaml:"voices" json:"voices,omitempty"`
	// Headers 是原样发送给内部 gateway 的固定请求头，例如 x-api-key、x-ks-user-key、
	// x-ks-llm-model 和 x-ks-biz-scene。chat 可用它替代 Bearer；Seedance、gptimage、
	// qwenedit 和 Gemini image/media_understand 使用这些 header 完成网关鉴权和路由。
	// 值可能包含凭证，只能来自本目录，不能回显或写入日志。
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`
	// Quality 仅用于 gptimage，可取 low、medium 或 high，默认 medium。
	Quality string `yaml:"quality" json:"quality,omitempty"`
	// ImageSize 仅用于 Gemini image 和 image_edit，可取 512、1K、2K 或 4K，默认 1K。
	// 该值与画幅独立；512 仅受部分 Flash 模型支持，Pro 模型最低为 1K。
	ImageSize string `yaml:"imageSize" json:"imageSize,omitempty"`
	// RemoteFetch 允许 media_understand provider 直接读取公网 URL，默认关闭。
	// 只有 provider 能从公网解析和访问该 URL 时才能启用，内网对象存储地址不适用。
	RemoteFetch bool `yaml:"remoteFetch" json:"remoteFetch,omitempty"`
	// MaxReferenceImages 限制 image_edit 参考图数量；零值通常表示不限，Gemini 使用
	// 保守默认值，因为参考图以内联字节上传并占用输入 token。
	MaxReferenceImages int `yaml:"maxReferenceImages" json:"maxReferenceImages,omitempty"`
	// APIKey 优先于 APIKeyEnv；两者都为空时使用无鉴权端点。
	APIKey string `yaml:"apiKey" json:"apiKey,omitempty"`
	// APIKeyEnv 是本地文件配置引用的环境变量名。
	APIKeyEnv string `yaml:"apiKeyEnv" json:"apiKeyEnv,omitempty"`
	// AccessKey 和 SecretKey 优先于对应环境变量。
	AccessKey string `yaml:"accessKey" json:"accessKey,omitempty"`
	SecretKey string `yaml:"secretKey" json:"secretKey,omitempty"`
	// AccessKeyEnv 和 SecretKeyEnv 仅用于 Kling JWT。
	AccessKeyEnv string `yaml:"accessKeyEnv" json:"accessKeyEnv,omitempty"`
	SecretKeyEnv string `yaml:"secretKeyEnv" json:"secretKeyEnv,omitempty"`
	// ProxyURL 是可选的公网出口代理。
	ProxyURL string `yaml:"proxyUrl" json:"proxyUrl,omitempty"`
}

// ApifyDecl 是目录顶层的商品抓取服务配置。它不是模型，但同样持有外部服务凭证，
// 因此保存在不可回显的 model catalog，而不是 Session Profile。
type ApifyDecl struct {
	BaseURL     string  `yaml:"base_url" json:"base_url,omitempty"`
	Token       string  `yaml:"token" json:"token,omitempty"`
	TokenEnv    string  `yaml:"token_env" json:"token_env,omitempty"`
	TaobaoActor string  `yaml:"taobao_actor" json:"taobao_actor,omitempty"`
	AmazonActor string  `yaml:"amazon_actor" json:"amazon_actor,omitempty"`
	Timeout     float64 `yaml:"timeout" json:"timeout,omitempty"`
}

// ModelsDocument 是模型目录 YAML 的顶层结构。
type ModelsDocument struct {
	// Version 为空时使用文档内容摘要。
	Version string      `yaml:"version" json:"version,omitempty"`
	Models  []ModelDecl `yaml:"models" json:"models"`
	Apify   *ApifyDecl  `yaml:"apify" json:"apify,omitempty"`
}

// ChatModel 将 chat client 与其固定 Dialect 绑定。
// Dialect 也决定 fold 是否能将该模型产生的 reasoning 安全回传给后续调用。
type ChatModel struct {
	Client  chat.Client
	Dialect event.Dialect
}

// Catalog 是已构建客户端并固定凭证的模型目录。
type Catalog struct {
	Version string
	// Chat 按逻辑模型名索引。
	Chat                map[string]ChatModel
	ProductDetailClient webfetch.ProductDetailClient
	// 客户端表按逻辑模型名和 capability 接口索引。EditClients 保留条目级参考图上限；
	// VideoClients 只包含实现通用首帧视频接口的条目，其他 provider 使用各自的窄接口表。
	VideoClients      map[string]videogen.Client
	KlingT2VClients   map[string]videogen.KlingT2VClient
	KlingI2VClients   map[string]videogen.KlingI2VClient
	KlingOmniClients  map[string]videogen.KlingOmniClient
	SeedanceClients   map[string]videogen.SeedanceClient
	ImageClients      map[string]imagegen.Client
	EditClients       map[string]imagegen.EditEntry
	UnderstandClients map[string]mediaunderstand.Client
	SearchClients     map[string]websearch.Client
	TTSClients        map[string]tts.Client
	ASRClients        map[string]asr.Client
}

// Load 从本地 YAML 文件构建模型目录。
func Load(path string, storage artifact.Storage) (*Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("models: read %s: %w", path, err)
	}
	c, err := Parse(raw, storage)
	if err != nil {
		return nil, fmt.Errorf("models: %s: %w", path, err)
	}
	return c, nil
}

// Parse 严格解析并构建完整目录；任一错误使整份目录失效。
func Parse(raw []byte, storage artifact.Storage) (*Catalog, error) {
	var doc ModelsDocument
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse models document: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse models document: multiple YAML documents are not allowed")
		}
		return nil, fmt.Errorf("parse models document: %w", err)
	}
	if doc.Models == nil {
		return nil, fmt.Errorf("models document must declare models")
	}

	chat := make(map[string]ChatModel)
	klingT2VClients := make(map[string]videogen.KlingT2VClient)
	klingI2VClients := make(map[string]videogen.KlingI2VClient)
	klingOmniClients := make(map[string]videogen.KlingOmniClient)
	seedanceClients := make(map[string]videogen.SeedanceClient)
	var klingWorkflow *videogen.KlingWorkflow
	// basicVideo 只包含实现 videogen.Client 最小接口的条目。
	basicVideo := make(map[string]videogen.Client)
	imageClients := make(map[string]imagegen.Client)
	editClients := make(map[string]imagegen.EditEntry)
	understandClients := make(map[string]mediaunderstand.Client)
	searchClients := make(map[string]websearch.Client)
	ttsClients := make(map[string]tts.Client)
	asrClients := make(map[string]asr.Client)
	seen := make(map[string]bool, len(doc.Models))
	for i, m := range doc.Models {
		if m.Name == "" {
			return nil, fmt.Errorf("models[%d]: name is required", i)
		}
		def, ok := modelcontract.Lookup(m.Name)
		if !ok {
			continue
		}
		if seen[m.Name] {
			return nil, fmt.Errorf("duplicate model name %q", m.Name)
		}
		seen[m.Name] = true
		kind := familyKind(def.Family)
		api := string(def.Dialect)
		// Gemini image、image_edit 和 media_understand 共用 gateway 配置规则。
		usesGemini := (kind == "image" || kind == "image_edit" || kind == "media_understand") &&
			api == "gemini"
		usesHeaders := kind == "chat" ||
			(kind == "video" && api == "seedance") ||
			(kind == "image" && api == "gptimage") ||
			(kind == "image_edit" && (api == "qwenedit" || api == "gptimage")) ||
			usesGemini
		if len(m.Headers) > 0 && !usesHeaders {
			return nil, fmt.Errorf("model %q: headers is not valid for family %s", m.Name, def.Family)
		}
		imageKind := kind == "image" || kind == "image_edit"
		if m.Quality != "" && (!imageKind || api != "gptimage") {
			return nil, fmt.Errorf("model %q: quality is only valid for gptimage", m.Name)
		}
		if m.ImageSize != "" && (!imageKind || api != "gemini") {
			return nil, fmt.Errorf("model %q: imageSize is only valid for Gemini image models", m.Name)
		}
		if m.RemoteFetch && kind != "media_understand" {
			return nil, fmt.Errorf("model %q: remoteFetch is only valid for media understanding models", m.Name)
		}
		if m.ModelName != "" && kind != "video" && kind != "search" && kind != "tts" && kind != "asr" && !usesGemini {
			return nil, fmt.Errorf("model %q: modelName is only valid for video, search, tts, asr and gemini models", m.Name)
		}
		if m.GroupID != "" && (kind != "tts" || api != "minimax") {
			return nil, fmt.Errorf("model %q: groupId is only valid for MiniMax TTS models", m.Name)
		}
		if len(m.Voices) > 0 && kind != "tts" {
			return nil, fmt.Errorf("model %q: voices is only valid for TTS models", m.Name)
		}
		usesAPIVersion := kind == "image" && (api == "azure" || api == "gptimage") ||
			kind == "image_edit" && api == "gptimage" ||
			kind == "asr" && api == "azure"
		if m.APIVersion != "" && !usesAPIVersion {
			return nil, fmt.Errorf("model %q: apiVersion is not valid for family %s", m.Name, def.Family)
		}
		if m.ResponseFormat != "" && (kind != "asr" || api != "azure") {
			return nil, fmt.Errorf("model %q: responseFormat is only valid for Azure ASR models", m.Name)
		}
		// Kling video dialect 共享凭证规则。
		usesKling := kind == "video" &&
			(api == "kling" || api == "kling-t2v" || api == "kling-omni")
		// 所有 search dialect 使用 API key。
		usesAPIKey := kind == "chat" || kind == "search" || kind == "tts" || kind == "asr" || usesKling ||
			kind == "image" && (api == "azure" || api == "dashscope")
		if (m.APIKey != "" || m.APIKeyEnv != "") && !usesAPIKey {
			return nil, fmt.Errorf("model %q: apiKey/apiKeyEnv is not valid for family %s", m.Name, def.Family)
		}
		usesKlingKeys := usesKling
		if (m.AccessKey != "" || m.SecretKey != "" || m.AccessKeyEnv != "" || m.SecretKeyEnv != "") && !usesKlingKeys {
			return nil, fmt.Errorf("model %q: access/secret keys are only valid for video kling", m.Name)
		}
		if m.MaxReferenceImages != 0 {
			if kind != "image_edit" {
				return nil, fmt.Errorf("model %q: maxReferenceImages is only valid for image edit models", m.Name)
			}
			if m.MaxReferenceImages < 0 {
				return nil, fmt.Errorf("model %q: maxReferenceImages must be non-negative", m.Name)
			}
		}
		switch kind {
		case "chat":
			if m.BaseURL == "" {
				return nil, fmt.Errorf("model %q: baseUrl is required", m.Name)
			}
			apiKey := m.APIKey // 目录内值优先于环境变量。
			if apiKey == "" && m.APIKeyEnv != "" {
				apiKey = os.Getenv(m.APIKeyEnv)
				if apiKey == "" {
					return nil, fmt.Errorf("model %q: env %s is empty", m.Name, m.APIKeyEnv)
				}
			}
			chat[m.Name] = ChatModel{
				Client: openaichat.New(openaichat.Config{
					BaseURL: m.BaseURL, APIKey: apiKey, Model: m.Name,
					Headers: m.Headers,
				}),
				Dialect: event.DialectOpenAI,
			}
		case "video":
			// Dialect 同时确定 HTTP client 和对应的视频 capability 接口。
			modelName := modelIdentifier(m)
			switch api {
			case "kling":
				if klingWorkflow == nil {
					klingWorkflow = videogen.NewKlingWorkflow(storage, nil)
				}
				client := klingWorkflow.I2V()
				klingI2VClients[m.Name] = client
				basicVideo[m.Name] = klingWorkflow.Basic()
			case "kling-t2v":
				if klingWorkflow == nil {
					klingWorkflow = videogen.NewKlingWorkflow(storage, nil)
				}
				klingT2VClients[m.Name] = klingWorkflow.T2V()
				// Kling T2V 不接受首帧，不实现 videogen.Client 最小接口。
			case "kling-omni":
				if klingWorkflow == nil {
					klingWorkflow = videogen.NewKlingWorkflow(storage, nil)
				}
				client := klingWorkflow.Omni()
				klingOmniClients[m.Name] = client
				basicVideo[m.Name] = klingWorkflow.Basic()
			case "seedance":
				client, err := videogen.NewSeedance(videogen.SeedanceConfig{
					BaseURL: m.BaseURL, Model: modelName, Headers: m.Headers,
					ProxyURL: m.ProxyURL, Artifacts: storage,
				})
				if err != nil {
					return nil, fmt.Errorf("model %q: %w", m.Name, err)
				}
				seedanceClients[m.Name] = client
				basicVideo[m.Name] = client.Basic()
			default:
				panic("modelcontract: unsupported video API " + api)
			}
		case "image":
			var client imagegen.Client
			var err error
			switch api {
			case "azure", "dashscope":
				apiKey := m.APIKey // 目录内值优先于环境变量。
				if apiKey == "" && m.APIKeyEnv != "" {
					if apiKey = os.Getenv(m.APIKeyEnv); apiKey == "" {
						return nil, fmt.Errorf("model %q: env %s is empty", m.Name, m.APIKeyEnv)
					}
				}
				if apiKey == "" {
					return nil, fmt.Errorf("model %q: api key required for image model (apiKey or apiKeyEnv)", m.Name)
				}
				if api == "azure" {
					client, err = imagegen.NewAzure(imagegen.AzureConfig{
						BaseURL: m.BaseURL, APIKey: apiKey, Deployment: m.Name,
						APIVersion: m.APIVersion, ProxyURL: m.ProxyURL,
					})
				} else {
					client, err = imagegen.NewDashScope(imagegen.DashScopeConfig{
						BaseURL: m.BaseURL, APIKey: apiKey, Model: m.Name, ProxyURL: m.ProxyURL,
					})
				}
			case "gptimage":
				// gptimage gateway 使用 Headers 鉴权。
				client, err = imagegen.NewKSGptImage(imagegen.KSGptImageConfig{
					BaseURL: m.BaseURL, APIVersion: m.APIVersion, Quality: m.Quality,
					Headers: m.Headers, ProxyURL: m.ProxyURL, Artifacts: storage,
				})
			case "gemini":
				// Gemini gateway 通过 generateContent 返回 inlineData。
				client, err = imagegen.NewGemini(imagegen.GeminiConfig{
					BaseURL: m.BaseURL, Model: modelIdentifier(m), ImageSize: m.ImageSize,
					Headers: m.Headers, ProxyURL: m.ProxyURL,
				})
			default:
				panic("modelcontract: unsupported image API " + api)
			}
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", m.Name, err)
			}
			imageClients[m.Name] = client
		case "image_edit":
			var editClient imagegen.EditClient
			var err error
			maxRef := m.MaxReferenceImages
			switch api {
			case "qwenedit":
				editClient, err = imagegen.NewQwenEdit(imagegen.QwenEditConfig{
					BaseURL: m.BaseURL, Model: m.Name, Headers: m.Headers, ProxyURL: m.ProxyURL,
					Artifacts: storage,
				})
			case "gptimage":
				editClient, err = imagegen.NewKSGptImage(imagegen.KSGptImageConfig{
					BaseURL: m.BaseURL, APIVersion: m.APIVersion, Quality: m.Quality,
					Headers: m.Headers, ProxyURL: m.ProxyURL, Artifacts: storage,
				})
			case "gemini":
				// Gemini image_edit 复用 generateContent，并通过 artifact storage 读取参考图。
				editClient, err = imagegen.NewGemini(imagegen.GeminiConfig{
					BaseURL: m.BaseURL, Model: modelIdentifier(m), ImageSize: m.ImageSize,
					Headers: m.Headers, ProxyURL: m.ProxyURL, Artifacts: storage,
				})
				// 参考图以内联字节上传；未配置时使用保守上限，避免请求体和输入 token 无界增长。
				if maxRef == 0 {
					maxRef = imagegen.DefaultGeminiMaxReferenceImages
				}
			default:
				panic("modelcontract: unsupported image edit API " + api)
			}
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", m.Name, err)
			}
			editClients[m.Name] = imagegen.EditEntry{
				Name: m.Name, Client: editClient, MaxReferenceImages: maxRef,
			}
		case "media_understand":
			// storage 为 nil 时只能读取 HTTP(S) 资源。
			client, err := mediaunderstand.NewGemini(mediaunderstand.GeminiConfig{
				BaseURL: m.BaseURL, Model: modelIdentifier(m),
				Headers: m.Headers, ProxyURL: m.ProxyURL, Artifacts: storage,
				RemoteFetch: m.RemoteFetch,
			})
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", m.Name, err)
			}
			understandClients[m.Name] = client
		case "search":
			apiKey := m.APIKey // 目录内值优先于环境变量。
			if apiKey == "" && m.APIKeyEnv != "" {
				if apiKey = os.Getenv(m.APIKeyEnv); apiKey == "" {
					return nil, fmt.Errorf("model %q: env %s is empty", m.Name, m.APIKeyEnv)
				}
			}
			if apiKey == "" {
				return nil, fmt.Errorf("model %q: api key required for search model (apiKey or apiKeyEnv)", m.Name)
			}
			var client websearch.Client
			var err error
			switch api {
			case "volc":
				client, err = websearch.NewVolc(websearch.VolcConfig{
					BaseURL: m.BaseURL, APIKey: apiKey, ProxyURL: m.ProxyURL,
				})
			case "brightserp":
				// Bright Data 使用 ModelName 保存上游 zone。
				if m.ModelName == "" {
					return nil, fmt.Errorf("model %q: Bright Data search requires modelName (the Bright Data zone)", m.Name)
				}
				client, err = websearch.NewBrightSERP(websearch.BrightSERPConfig{
					BaseURL: m.BaseURL, APIKey: apiKey, Zone: m.ModelName, ProxyURL: m.ProxyURL,
				})
			default:
				panic("modelcontract: unsupported search API " + api)
			}
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", m.Name, err)
			}
			searchClients[m.Name] = client
		case "tts":
			apiKey := m.APIKey
			if apiKey == "" && m.APIKeyEnv != "" {
				if apiKey = os.Getenv(m.APIKeyEnv); apiKey == "" {
					return nil, fmt.Errorf("model %q: env %s is empty", m.Name, m.APIKeyEnv)
				}
			}
			if apiKey == "" {
				return nil, fmt.Errorf("model %q: api key required for TTS model (apiKey or apiKeyEnv)", m.Name)
			}
			var client tts.Client
			var err error
			switch api {
			case "minimax":
				client, err = tts.NewMiniMax(tts.MiniMaxConfig{
					BaseURL: m.BaseURL, APIKey: apiKey, GroupID: m.GroupID,
					Model: modelIdentifier(m), ProxyURL: m.ProxyURL,
				})
			case "dashscope":
				client, err = tts.NewDashScope(tts.DashScopeConfig{
					BaseURL: m.BaseURL, APIKey: apiKey,
					Model: modelIdentifier(m), ProxyURL: m.ProxyURL,
				})
			case "cosyvoice":
				client, err = tts.NewCosyVoice(tts.CosyVoiceConfig{
					BaseURL: m.BaseURL, APIKey: apiKey,
					Model: modelIdentifier(m), ProxyURL: m.ProxyURL,
				})
			default:
				panic("modelcontract: unsupported TTS API " + api)
			}
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", m.Name, err)
			}
			voices := m.Voices
			if len(voices) == 0 {
				switch api {
				case "minimax":
					voices = tts.DefaultMiniMaxVoices
				case "dashscope":
					voices = tts.DefaultDashScopeVoices
				}
			}
			client = tts.RestrictVoices(client, voices)
			ttsClients[m.Name] = client
		case "asr":
			apiKey := m.APIKey
			if apiKey == "" && m.APIKeyEnv != "" {
				if apiKey = os.Getenv(m.APIKeyEnv); apiKey == "" {
					return nil, fmt.Errorf("model %q: env %s is empty", m.Name, m.APIKeyEnv)
				}
			}
			if apiKey == "" {
				return nil, fmt.Errorf("model %q: api key required for ASR model (apiKey or apiKeyEnv)", m.Name)
			}
			client, err := asr.NewAzure(asr.AzureConfig{
				BaseURL: m.BaseURL, APIKey: apiKey, Deployment: modelIdentifier(m),
				Model: modelIdentifier(m), APIVersion: m.APIVersion, ResponseFormat: m.ResponseFormat,
				ProxyURL: m.ProxyURL, Artifacts: storage,
			})
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", m.Name, err)
			}
			asrClients[m.Name] = client
		default:
			panic("modelcontract: unsupported family " + def.Family)
		}
	}

	c := &Catalog{
		Version: doc.Version,
		Chat:    chat,
	}
	if doc.Apify != nil {
		if doc.Apify.Timeout < 0 || math.IsNaN(doc.Apify.Timeout) || math.IsInf(doc.Apify.Timeout, 0) {
			return nil, fmt.Errorf("apify: timeout must be a finite non-negative number of seconds")
		}
		token := doc.Apify.Token
		if token == "" && doc.Apify.TokenEnv != "" {
			token = os.Getenv(doc.Apify.TokenEnv)
			if token == "" {
				return nil, fmt.Errorf("apify: env %s is empty", doc.Apify.TokenEnv)
			}
		}
		timeout := time.Duration(doc.Apify.Timeout * float64(time.Second))
		client, err := webfetch.NewProductDetailClient(webfetch.ApifyConfig{
			BaseURL: doc.Apify.BaseURL, Token: token,
			TaobaoActor: doc.Apify.TaobaoActor, AmazonActor: doc.Apify.AmazonActor,
			Timeout: timeout, Artifacts: storage,
		})
		if err != nil {
			return nil, fmt.Errorf("apify: %w", err)
		}
		c.ProductDetailClient = client
	}
	if len(klingT2VClients) > 0 {
		c.KlingT2VClients = klingT2VClients
	}
	if len(klingI2VClients) > 0 {
		c.KlingI2VClients = klingI2VClients
	}
	if len(klingOmniClients) > 0 {
		c.KlingOmniClients = klingOmniClients
	}
	if len(seedanceClients) > 0 {
		c.SeedanceClients = seedanceClients
	}
	if len(basicVideo) > 0 {
		c.VideoClients = basicVideo
	}
	if len(imageClients) > 0 {
		if storage == nil {
			return nil, fmt.Errorf("image models require artifact storage")
		}
		c.ImageClients = imageClients
	}
	if len(editClients) > 0 {
		if storage == nil {
			return nil, fmt.Errorf("image edit models require artifact storage")
		}
		c.EditClients = editClients
	}
	if len(understandClients) > 0 {
		c.UnderstandClients = understandClients
	}
	if len(searchClients) > 0 {
		c.SearchClients = searchClients
	}
	if len(ttsClients) > 0 {
		if storage == nil {
			return nil, fmt.Errorf("TTS models require artifact storage")
		}
		c.TTSClients = ttsClients
	}
	if len(asrClients) > 0 {
		c.ASRClients = asrClients
	}
	if c.Version == "" {
		sum := sha256.Sum256(raw)
		c.Version = hex.EncodeToString(sum[:])[:12]
	}
	return c, nil
}

func familyKind(family modelcontract.Family) string {
	switch family {
	case modelcontract.FamilyChat:
		return "chat"
	case modelcontract.FamilyKlingT2V, modelcontract.FamilyKlingI2V,
		modelcontract.FamilyKlingOmni, modelcontract.FamilySeedance:
		return "video"
	case modelcontract.FamilyImage:
		return "image"
	case modelcontract.FamilyImageEdit:
		return "image_edit"
	case modelcontract.FamilyMediaUnderstand:
		return "media_understand"
	case modelcontract.FamilySearch:
		return "search"
	case modelcontract.FamilyTTSMiniMax, modelcontract.FamilyTTSQwen,
		modelcontract.FamilyTTSCosyVoice:
		return "tts"
	case modelcontract.FamilyASR:
		return "asr"
	default:
		panic("modelcontract: unknown family " + family)
	}
}

// modelIdentifier 返回显式上游标识，未设置时使用目录条目名。
func modelIdentifier(m ModelDecl) string {
	if m.ModelName != "" {
		return m.ModelName
	}
	return m.Name
}
