package app

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Mode 选择单进程开发环境或多实例部署环境。
type Mode string

const (
	// ModeStandalone 使用内嵌 Worker 和进程内 deltabuf、capability catalog。
	ModeStandalone Mode = "standalone"
	// ModeDeployed 使用独立 Worker，以及 Kedis deltabuf 和 capability catalog。
	ModeDeployed Mode = "deployed"
)

const (
	// AuthAPIKey 使用 Service Principal 的 Bearer API Key。
	AuthAPIKey = "apikey"
	// AuthOff 为每个请求注入固定的开发身份。
	AuthOff = "off"
)

const (
	storeMem   = "mem"
	storeMySQL = "mysql"
	storeKDB   = "kdb"
)

// APIConfig 是 API 进程的运行配置。Mode、Store 和 Auth 是相互独立的配置轴，必须
// 显式设置，避免遗漏配置时以错误的部署模式启动。
type APIConfig struct {
	Mode   Mode
	Store  string
	Auth   string
	Listen string
}

func APIConfigFromEnv() (APIConfig, error) {
	cfg := APIConfig{
		Mode:   Mode(strings.TrimSpace(os.Getenv("AGENTK_MODE"))),
		Store:  strings.TrimSpace(os.Getenv("AGENTK_STORE")),
		Auth:   strings.TrimSpace(os.Getenv("AGENTK_AUTH")),
		Listen: envOr("AGENTK_LISTEN", ":8080"),
	}
	switch cfg.Mode {
	case ModeStandalone, ModeDeployed:
	default:
		return APIConfig{}, fmt.Errorf(`env AGENTK_MODE must be %q or %q, got %q`,
			ModeStandalone, ModeDeployed, cfg.Mode)
	}
	switch cfg.Store {
	case storeMem, storeMySQL, storeKDB:
	default:
		return APIConfig{}, fmt.Errorf(`env AGENTK_STORE must be %q, %q or %q, got %q`,
			storeMem, storeMySQL, storeKDB, cfg.Store)
	}
	switch cfg.Auth {
	case AuthAPIKey, AuthOff:
	default:
		return APIConfig{}, fmt.Errorf(`env AGENTK_AUTH must be %q or %q, got %q`,
			AuthAPIKey, AuthOff, cfg.Auth)
	}

	// 多实例部署要求所有实例读取同一个持久化 Store。
	if cfg.Mode == ModeDeployed && cfg.Store == storeMem {
		return APIConfig{}, fmt.Errorf("AGENTK_MODE=%s requires a persistent AGENTK_STORE (%s or %s), got %s",
			ModeDeployed, storeMySQL, storeKDB, storeMem)
	}
	// API Key 验证依赖持久化的组织、Service Principal 和密钥记录。
	if cfg.Auth == AuthAPIKey && cfg.Store == storeMem {
		return APIConfig{}, fmt.Errorf("AGENTK_AUTH=%s requires a persistent AGENTK_STORE (%s or %s), got %s",
			AuthAPIKey, storeMySQL, storeKDB, storeMem)
	}
	// 允许使用持久化 Store 调试 AuthOff，但明确提示数据库密钥不会生效。
	if cfg.Auth == AuthOff && cfg.Store != storeMem {
		log.Printf("warning: AGENTK_AUTH=%s with a persistent store — every request runs as the fixed dev principal, API keys in the database are ignored",
			AuthOff)
	}
	return cfg, nil
}

// WorkerConfig 是独立 Worker 的运行配置，仅接受持久化 Store；它不读取 API 的
// Mode、Auth 或 Listen 配置。
type WorkerConfig struct {
	Store string
}

func WorkerConfigFromEnv() (WorkerConfig, error) {
	cfg := WorkerConfig{Store: strings.TrimSpace(os.Getenv("AGENTK_STORE"))}
	switch cfg.Store {
	case storeMySQL, storeKDB:
		return cfg, nil
	default:
		return WorkerConfig{}, fmt.Errorf(`env AGENTK_STORE for agentk-worker must be %q or %q, got %q`,
			storeMySQL, storeKDB, cfg.Store)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
