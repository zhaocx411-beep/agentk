package memory

import (
	"testing"

	"agentk/internal/store/storetest"
)

// 语义断言全部在共用契约里（internal/store/storetest）：MySQL 与内存两个实现
// 跑同一套用例，分叉当场暴露。本文件只做接线。
func TestStoreContract(t *testing.T) {
	storetest.RunStoreContract(t, func(t *testing.T) storetest.Store {
		return New()
	})
}
