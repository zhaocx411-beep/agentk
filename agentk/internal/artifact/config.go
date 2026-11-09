package artifact

import (
	"fmt"
	"os"
)

// NewStorageFromEnv 按 AGENTK_ARTIFACT_STORE 构造产物存储：blob（缺省）或 local。
// 缺省选 blob，防止部署遗漏 Blob 配置时静默写入本机磁盘。
//
// fallbackFilesBase 只在 local 且未设 AGENTK_FILES_BASE_URL 时使用——调用方知道
// 自己监听在哪，本包不猜。
func NewStorageFromEnv(fallbackFilesBase string) (Storage, error) {
	switch kind := envOr("AGENTK_ARTIFACT_STORE", "blob"); kind {
	case "blob":
		cfg := BlobConfigFromEnv()
		if !cfg.Configured() {
			return nil, fmt.Errorf("artifact: storage=blob requires AGENTK_BLOB_ENDPOINT and AGENTK_BLOB_BUCKET")
		}
		return NewBlobStorage(cfg)
	case "local":
		return NewLocalStorage(LocalDirFromEnv(), LocalBaseURLFromEnv(fallbackFilesBase)), nil
	default:
		return nil, fmt.Errorf("artifact: unknown AGENTK_ARTIFACT_STORE %q (want blob or local)", kind)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
