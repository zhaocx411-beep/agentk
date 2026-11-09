package deltabuf

import (
	"os"
	"testing"
)

// TestKedisContract 在设置 AGENTK_REDIS_RESOURCE 时对真实集群执行 Buffer 契约测试。
// CI 没有 Kess Agent，因此默认跳过；本地可运行
// AGENTK_REDIS_RESOURCE=klingaiAgentkRedis go test ./internal/deltabuf 验证真实集群。
func TestKedisContract(t *testing.T) {
	resource := os.Getenv("AGENTK_REDIS_RESOURCE")
	if resource == "" {
		t.Skip("AGENTK_REDIS_RESOURCE not set; skipping kedis integration test")
	}
	buf, err := NewKedis(resource)
	if err != nil {
		t.Fatalf("init kedis buffer: %v", err)
	}
	runBufferContract(t, func(t *testing.T) Buffer { return buf })
}
