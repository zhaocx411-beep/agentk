package kconfwatch

import (
	"errors"
	"testing"

	"git.corp.kuaishou.com/infra/infra-framework-go.git/kconf"
)

// fake 注入：替换包内的 kconf 函数变量，测试结束复原。
func stub(t *testing.T, get func(string) (string, error), add func(string, kconf.StringWatcher) error) {
	t.Helper()
	origGet, origAdd := getString, addWatcher
	getString, addWatcher = get, add
	t.Cleanup(func() { getString, addWatcher = origGet, origAdd })
}

func TestWatchInitialApply(t *testing.T) {
	stub(t,
		func(string) (string, error) { return "v1", nil },
		func(string, kconf.StringWatcher) error { return nil })

	var got []string
	err := Watch("k", func(raw []byte) error {
		got = append(got, string(raw))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "v1" {
		t.Fatalf("applied = %v, want [v1]", got)
	}
}

func TestWatchInitialFetchFails(t *testing.T) {
	stub(t,
		func(string) (string, error) { return "", errors.New("kess down") },
		func(string, kconf.StringWatcher) error { return nil })

	applied := false
	err := Watch("k", func([]byte) error { applied = true; return nil })
	if err == nil {
		t.Fatal("want error on initial fetch failure")
	}
	if applied {
		t.Fatal("apply should not be called when fetch fails")
	}
}

func TestWatchAddWatcherFails(t *testing.T) {
	stub(t,
		func(string) (string, error) { return "v1", nil },
		func(string, kconf.StringWatcher) error { return errors.New("bad key") })

	if err := Watch("k", func([]byte) error { return nil }); err == nil {
		t.Fatal("want error on add watcher failure")
	}
}

func TestWatchInitialApplyRejected(t *testing.T) {
	stub(t,
		func(string) (string, error) { return "bad doc", nil },
		func(string, kconf.StringWatcher) error { return nil })

	err := Watch("k", func([]byte) error { return errors.New("parse error") })
	if err == nil {
		t.Fatal("want error when initial doc is rejected")
	}
}

// 更新经 apply 生效；坏配置被拒但向 kconf 返回 nil（不进 3 秒重试风暴）。
func TestWatcherUpdates(t *testing.T) {
	var w kconf.StringWatcher
	stub(t,
		func(string) (string, error) { return "v1", nil },
		func(_ string, sw kconf.StringWatcher) error { w = sw; return nil })

	var got []string
	if err := Watch("k", func(raw []byte) error {
		s := string(raw)
		if s == "bad" {
			return errors.New("parse error")
		}
		got = append(got, s)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := w.OnStringValueUpdated("k", "v2"); err != nil {
		t.Fatalf("good update returned %v, want nil", err)
	}
	if err := w.OnStringValueUpdated("k", "bad"); err != nil {
		t.Fatalf("rejected update returned %v, want nil（避免 kconf 无限重试）", err)
	}
	if err := w.OnValueRemoved("k"); err != nil {
		t.Fatalf("removed returned %v, want nil", err)
	}

	want := []string{"v1", "v2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("applied = %v, want %v", got, want)
	}
}

// 按 kconf 文档，AddWatcher 注册后会立即触发一次回调。
// 首次回调送来好配置：经 watcher 生效一次，显式拉取不再重复 Apply。
func TestWatchImmediateCallbackGood(t *testing.T) {
	var got []string
	apply := func(raw []byte) error { got = append(got, string(raw)); return nil }
	stub(t,
		func(string) (string, error) { return "v1", nil },
		func(_ string, sw kconf.StringWatcher) error {
			// 模拟 SDK 注册即回调
			return sw.OnStringValueUpdated("k", "v1")
		})
	if err := Watch("k", apply); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "v1" {
		t.Fatalf("applied = %v, want [v1]（首次回调已生效，拉取不重复 Apply）", got)
	}
}

// 首次回调送来坏配置（被拒）：显式拉取必须仍尝试生效并把错误交还调用方，
// 否则 Watch 返回成功但配置从未生效，调用方不会走文件退路。
func TestWatchImmediateCallbackRejected(t *testing.T) {
	var w kconf.StringWatcher
	stub(t,
		func(string) (string, error) { return "bad", nil },
		func(_ string, sw kconf.StringWatcher) error {
			w = sw
			return w.OnStringValueUpdated("k", "bad")
		})

	applies := 0
	err := Watch("k", func(raw []byte) error {
		applies++
		return errors.New("parse error")
	})
	if err == nil {
		t.Fatal("want error when config never took effect")
	}
	if applies != 2 {
		t.Fatalf("applies = %d, want 2（回调一次 + 拉取兜底一次）", applies)
	}
}

// 初始拉取完成前 watcher 先送达更新：初始快照作废，不得回退覆盖新版本。
func TestWatchUpdateBeatsInitialFetch(t *testing.T) {
	var w kconf.StringWatcher
	stub(t,
		func(string) (string, error) { return "stale", nil },
		func(_ string, sw kconf.StringWatcher) error { w = sw; return nil })

	var got []string
	apply := func(raw []byte) error { got = append(got, string(raw)); return nil }

	// addWatcher 成功后、getString 返回前，kconf 推来了新版本——
	// 用 stub 的 getString 里触发 watcher 来模拟这个时序。
	stub(t,
		func(string) (string, error) {
			_ = w.OnStringValueUpdated("k", "fresh")
			return "stale", nil
		},
		func(_ string, sw kconf.StringWatcher) error { w = sw; return nil })

	if err := Watch("k", apply); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "fresh" {
		t.Fatalf("applied = %v, want [fresh]（初始快照应作废）", got)
	}
}
