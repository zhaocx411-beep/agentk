// Package app 负责 API 和 Worker 进程的配置与依赖装配。
// 本包只解析进程部署相关的 AGENTK_MODE、AGENTK_STORE、AGENTK_AUTH 和
// AGENTK_LISTEN；模型、资源存储、鉴权、共享状态等配置由对应的 owning package 解析。
// 独立 Worker 只读取持久化 Store 配置；Temporal 通过服务发现连接，Worker 使用固定
// AgentK task queue。
package app

import (
	"context"
	"fmt"
	"os"

	"agentk/internal/agentloop"
	"agentk/internal/artifact"
	"agentk/internal/authz"
	"agentk/internal/capability"
	"agentk/internal/capcatalog"
	"agentk/internal/invocation"
	"agentk/internal/modelcatalog"
	"agentk/internal/pipeline"
	"agentk/internal/profile"
	"agentk/internal/service"
	"agentk/internal/store/memory"
	"agentk/internal/storeboot"
	"agentk/internal/tool"
)

// storeSet 将同一个 Store 实例暴露为各消费包所需的窄接口。
type storeSet struct {
	service    service.Store
	loop       agentloop.Store
	pipeline   pipeline.Store
	invocation invocation.Store
	// apiKeys 在 mem 模式下为 nil；AuthAPIKey 不允许与 mem Store 组合。
	apiKeys authz.APIKeyStore
}

// buildStores 只允许 MySQL 在 migrate=true 时执行迁移；KDB 始终只检查版本。
// mem Store 无外部依赖且进程重启后丢失，MySQL/KDB 为各消费接口共享同一实例。
func buildStores(ctx context.Context, kind string, migrate bool) (storeSet, func(), error) {
	switch kind {
	case storeMySQL, storeKDB:
		st, closeStore, err := storeboot.Open(ctx, kind, migrate && kind == storeMySQL)
		if err != nil {
			return storeSet{}, nil, err
		}
		return storeSet{
			service: st, loop: st, pipeline: st, invocation: st, apiKeys: st,
		}, closeStore, nil
	case storeMem:
		st := memory.New()
		return storeSet{service: st, loop: st, pipeline: st, invocation: st}, func() {}, nil
	default:
		return storeSet{}, nil, fmt.Errorf("unknown store backend %q (want %s, %s or %s)",
			kind, storeMySQL, storeKDB, storeMem)
	}
}

// buildCapabilityCatalog 合并静态 Core Manifest 和动态 contrib catalog。
// backend 由进程入口独立选择，不与 deltabuf 或其他共享状态绑定成一个配置轴。
func buildCapabilityCatalog(kind string) (capcatalog.Catalog, error) {
	dynamicCapabilityDirectory, err := capcatalog.New(kind)
	if err != nil {
		return nil, err
	}
	manifest, err := capability.CoreManifest()
	if err != nil {
		return nil, err
	}
	return capcatalog.NewComposite(manifest, dynamicCapabilityDirectory)
}

// buildProfileRuntime 返回 Profile runtime 及其共享的模型目录。
func buildProfileRuntime(
	storage artifact.Storage,
	capabilities capcatalog.Catalog,
) (*profile.Runtime, *modelcatalog.Active, error) {
	capabilityTools, err := capability.Tools()
	if err != nil {
		return nil, nil, err
	}
	// 这些工具不属于任何 capability manifest，须单独注册到 Profile runtime。
	builtinTools := append(capabilityTools,
		tool.NewListArtifactsTool(), tool.NewAskUserTool())
	models, err := modelcatalog.ActiveFromEnv(storage)
	if err != nil {
		return nil, nil, err
	}
	return profile.NewRuntime(models, capabilities, builtinTools), models, nil
}

func validateArtifactStorage(mode Mode, storage artifact.Storage) error {
	if mode != ModeDeployed {
		return nil
	}
	if _, local := storage.(*artifact.LocalStorage); local {
		return fmt.Errorf("deployed runtime requires AGENTK_ARTIFACT_STORE=blob")
	}
	return nil
}

func workerID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
