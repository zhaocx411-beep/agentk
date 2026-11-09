package capcatalog

import (
	"errors"
	"fmt"
	"os"
)

// New 按 kind 装配 contrib Manifest 动态目录：mem（零依赖单进程）或
// kedis（多实例共享）。核心静态目录由装配侧通过 NewComposite 合入。
// 与 deltabuf 同源同命：进程内状态在多实例下一律不成立，故共用同一个 Redis 资源。
//
//	AGENTK_REDIS_RESOURCE  kedis 必填，标准版 Redis 的 KCC 逻辑资源名；
//	                       环境后缀由 Kess 解析，不要手工拼接。需 Kess Agent 在岗
func New(kind string) (Catalog, error) {
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
		return nil, fmt.Errorf("unknown capability catalog kind %q (want mem or kedis)", kind)
	}
}
