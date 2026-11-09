package deltabuf

import (
	"errors"
	"fmt"
	"os"
)

// New 根据 kind 创建 mem（单进程）或 kedis（多实例共享）缓冲。
// 配置错误直接返回，不从 kedis 降级到 mem；多实例若各自使用内存缓冲，SSE 只有
// 偶然命中写入实例时才能读取到增量。
//
//	AGENTK_REDIS_RESOURCE  kedis 必填，标准版 Redis 的 KCC 逻辑资源名；
//	                       环境后缀由 Kess 解析，运行时需要 Kess Agent
func New(kind string) (Buffer, error) {
	switch kind {
	case "mem":
		return NewMemory(), nil
	case "kedis":
		resource := os.Getenv("AGENTK_REDIS_RESOURCE")
		if resource == "" {
			return nil, errors.New("env AGENTK_REDIS_RESOURCE is required")
		}
		return NewKedis(resource)
	default:
		return nil, fmt.Errorf("unknown delta buffer kind %q (want mem or kedis)", kind)
	}
}
