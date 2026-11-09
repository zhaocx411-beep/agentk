// Package kconfwatch 把"从 kconf 取文档"和"文档怎么生效"切开：
// 本包只负责初始拉取与变更监听，拿到的字节流交给调用方的 Apply 回调，
// 不认识文档语义；取数通道与解释、校验职责相互独立。
//
// 需要本机 Kess Agent 在岗（kconf SDK 的硬前提）。
package kconfwatch

import (
	"crypto/sha256"
	"fmt"
	"log"
	"sync"

	"git.corp.kuaishou.com/infra/infra-framework-go.git/kconf"
)

// Apply 接收 kconf 下发的新文档；返回错误表示整份拒绝（调用方保留旧版）。
// Watch 保证对同一 key 的 Apply 调用串行，回调内无需自行加锁。
type Apply func(raw []byte) error

// kconf 包级函数以变量注入，单测替换 fake，不依赖 Kess Agent。
var (
	getString = func(key string) (string, error) {
		v, err := kconf.GetStringConfig(key)
		return string(v), err
	}
	addWatcher = func(key string, w kconf.StringWatcher) error {
		return kconf.AddStringWatcher(key, w)
	}
)

// Watch 挂监听并做一次全量拉取。按 kconf 文档，AddWatcher 注册后会立即
// 触发一次回调，所以正常路径初始版本经 watcher 生效；显式拉取是兜底
// （回调时序无保证、且需要一个同步的错误信号）。返回非 nil 表示初始配置
// 未生效（拉取失败或被 Apply 拒绝），调用方自行决定退路（如退回本地文件）；
// 只要 watcher 挂上了，kconf 恢复后的更新仍会经 Apply 热生效。
func Watch(key string, apply Apply) error {
	w := &watcher{key: key, apply: apply}
	if err := addWatcher(key, w); err != nil {
		return fmt.Errorf("kconfwatch: add watcher %s: %w", key, err)
	}
	raw, err := getString(key)
	if err != nil {
		return fmt.Errorf("kconfwatch: initial fetch %s: %w", key, err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.applied {
		// watcher 的首次回调（或间隙里的更新）已成功生效，初始快照作废。
		// 只看"成功生效过"：若回调送来的是坏配置（被拒），这里仍要用
		// 拉取到的版本再试——否则 Watch 返回成功但 Apply 从未发生。
		return nil
	}
	if err := apply([]byte(raw)); err != nil {
		return fmt.Errorf("kconfwatch: initial apply %s: %w", key, err)
	}
	w.applied = true
	return nil
}

type watcher struct {
	key   string
	apply Apply

	mu      sync.Mutex
	applied bool // 任意一次 Apply 成功过
}

func (w *watcher) OnStringValueUpdated(key string, val kconf.String) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	raw := []byte(val)
	if err := w.apply(raw); err != nil {
		// 坏配置整份拒绝、保留旧版。必须返回 nil：返回错误会让 kconf
		// 每 3 秒无限重试同一份坏配置，等下一次人工更新即可。
		log.Printf("kconfwatch: %s: 新配置被拒绝，保留旧版继续服务: %v", key, err)
		return nil
	}
	w.applied = true
	sum := sha256.Sum256(raw)
	log.Printf("kconfwatch: %s: 新配置生效 (%d bytes, sha256=%x)", key, len(raw), sum[:6])
	return nil
}

func (w *watcher) OnValueRemoved(key string) error {
	// 误删保护：不动当前配置，只告警。恢复 key 后走正常更新通道。
	log.Printf("kconfwatch: %s: 配置在 kconf 被删除，保留当前版本继续服务", key)
	return nil
}
