package modelcatalog

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"agentk/internal/artifact"
	"agentk/internal/kconfwatch"
)

// ActiveFromEnv 按环境变量加载模型目录：
//
//	AGENTK_MODELS_KCONF_KEY  可选；设了则 kconf 初始拉取 + watcher 热更（需 Kess
//	                         Agent）；更新失败时保留旧目录
//	AGENTK_MODELS            目录文档路径（见 models.example.yaml，凭证可用 env
//	                         引用）；未设 KCONF_KEY 时必填，设置后作为初始加载失败的回退。
//	                         KConf 恢复后，已启动的 watcher 仍会接管后续更新
func ActiveFromEnv(storage artifact.Storage) (*Active, error) {
	kconfKey := os.Getenv("AGENTK_MODELS_KCONF_KEY")
	if kconfKey == "" {
		path := os.Getenv("AGENTK_MODELS")
		if path == "" {
			return nil, errors.New("env AGENTK_MODELS is required")
		}
		cat, err := Load(path, storage)
		if err != nil {
			return nil, err
		}
		return NewActive(cat), nil
	}

	// mu 保护首次创建；后续更新通过 Active.Swap 原子替换。
	var mu sync.Mutex
	var active *Active
	apply := func(raw []byte) error {
		cat, err := Parse(raw, storage)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		if active == nil {
			active = NewActive(cat)
		} else {
			active.Swap(cat)
		}
		log.Printf("models: kconf %s 目录版本 %s 生效", kconfKey, cat.Version)
		return nil
	}
	if err := kconfwatch.Watch(kconfKey, apply); err != nil {
		path := os.Getenv("AGENTK_MODELS")
		if path == "" {
			return nil, fmt.Errorf("models: kconf 初始目录未生效且未设 AGENTK_MODELS 文件退路: %w", err)
		}
		log.Printf("models: kconf 初始目录未生效，退回文件 %s: %v", path, err)
		cat, ferr := Load(path, storage)
		if ferr != nil {
			return nil, ferr
		}
		mu.Lock()
		defer mu.Unlock()
		if active == nil {
			active = NewActive(cat)
		}
		return active, nil
	}
	mu.Lock()
	defer mu.Unlock()
	return active, nil
}
