package core

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DramaMode 服务运行模式：pipeline（确定性流水线，需要部署 Worker）或
// agent（自由 Agent 会话，只依赖 AgentK Connect API）。
type DramaMode string

const (
	ModePipeline DramaMode = "pipeline"
	ModeAgent    DramaMode = "agent"
)

type Config struct {
	// Mode 运行模式，DRAMA_MODE 环境变量指定，默认 agent。
	Mode          DramaMode
	AgentKBaseURL string
	// AgentKAPIKey Service Principal API Key。staging / 生产必填；
	// 本地 agentk-demo（无鉴权 dev 租户）可留空。
	AgentKAPIKey string
	// ProfileFile 漫剧 profile 的磁盘路径覆盖（PROFILE_FILE）。留空时
	// 使用编译期打进二进制的缺省 profile（按 Mode 选 agent / pipeline 版）。
	ProfileFile string
	Host        string
	Port        int
	// Models pipeline 各阶段使用的 models catalog capability ref。
	// 缺省值对齐 agent-k demo / staging 的目录条目名，部署时按环境覆盖。
	// agent 模式不使用（模型在 profile 里声明）。
	Models ModelRefs
}

// ModelRefs 是 pipeline 输入里的四个模型引用；用户请求不携带模型，
// 由本服务按部署环境统一填写。
type ModelRefs struct {
	Chat  string
	Image string
	Edit  string
	Video string
}

func LoadConfig() (Config, error) {
	port, err := strconv.Atoi(envOrDefault("API_PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("API_PORT must be an integer between 1 and 65535")
	}
	// 默认 agent：只依赖 AgentK Connect API，无需部署 Worker，开箱可用。
	// pipeline 模式需要 Worker 与 agent-k 连同一 Temporal 集群后显式开启。
	mode := DramaMode(envOrDefault("DRAMA_MODE", string(ModeAgent)))
	if mode != ModePipeline && mode != ModeAgent {
		return Config{}, fmt.Errorf("DRAMA_MODE must be %q or %q", ModePipeline, ModeAgent)
	}
	return Config{
		Mode: mode,
		AgentKBaseURL: envOrDefault(
			"AGENTK_BASE_URL",
			"https://agentk-api.staging.kuaishou.com",
		),
		AgentKAPIKey: strings.TrimSpace(os.Getenv("AGENTK_API_KEY")),
		ProfileFile:  strings.TrimSpace(os.Getenv("PROFILE_FILE")),
		Host:         envOrDefault("API_HOST", "127.0.0.1"),
		Port:         port,
		Models: ModelRefs{
			Chat:  envOrDefault("CHAT_MODEL", "deepseek-v4-pro"),
			Image: envOrDefault("IMAGE_MODEL", "z-image-turbo"),
			Edit:  envOrDefault("EDIT_MODEL", "gemini-3-pro-image-edit"),
			// 漫剧视频生成只走可灵 Omni 方言面（首帧 + referImages +
			// audio=native 原生台词声音），必须是可灵 Omni 条目。
			Video: envOrDefault("VIDEO_MODEL", "kling-3.0-omni"),
		},
	}, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
