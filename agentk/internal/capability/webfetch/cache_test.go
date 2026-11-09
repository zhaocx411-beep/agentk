package webfetch

import (
	"strings"
	"testing"
	"time"
)

// TestPageCacheTTL 过期项不再命中：翻页是分钟级的动作，隔了很久再翻
// 此时应重新抓取，因为页面可能已经变化，继续按旧快照切 offset 会导致内容错位。
func TestPageCacheTTL(t *testing.T) {
	c := newPageCache()
	now := time.Now()
	c.now = func() time.Time { return now }

	c.put("u", page{text: "内容"})
	if _, ok := c.get("u"); !ok {
		t.Fatal("fresh entry must hit")
	}
	now = now.Add(cacheTTL + time.Second)
	if _, ok := c.get("u"); ok {
		t.Fatal("expired entry must miss")
	}
	if c.chars != 0 {
		t.Fatalf("chars = %d, want the expired entry accounted out", c.chars)
	}
}

// TestPageCacheEvictsOldest 超出条目上限时淘汰最老的，字符计数同步维护。
func TestPageCacheEvictsOldest(t *testing.T) {
	c := newPageCache()
	now := time.Now()
	c.now = func() time.Time { return now }

	for i := 0; i <= cacheMaxEntries; i++ {
		now = now.Add(time.Second)
		c.put(string(rune('a'+i)), page{text: "x"})
	}
	if len(c.entries) != cacheMaxEntries || c.chars != cacheMaxEntries {
		t.Fatalf("entries = %d, chars = %d", len(c.entries), c.chars)
	}
	if _, ok := c.get("a"); ok {
		t.Fatal("oldest entry must be evicted")
	}
	if _, ok := c.get(string(rune('a' + cacheMaxEntries))); !ok {
		t.Fatal("newest entry must survive")
	}
}

// TestPageCacheEvictsBySize 验证单个大页面不会占用过多进程内存：超过字符上限时同样淘汰。
func TestPageCacheEvictsBySize(t *testing.T) {
	c := newPageCache()
	now := time.Now()
	c.now = func() time.Time { return now }

	c.put("small", page{text: "x"})
	now = now.Add(time.Second)
	c.put("huge", page{text: strings.Repeat("x", cacheMaxChars)})

	if _, ok := c.get("small"); ok {
		t.Fatal("older entry must be evicted to stay under the size cap")
	}
	if c.chars > cacheMaxChars {
		t.Fatalf("chars = %d, want <= %d", c.chars, cacheMaxChars)
	}
}

// TestPageCacheReplace 同 URL 重复写入不重复计数（翻页期间可能重抓同一页）。
func TestPageCacheReplace(t *testing.T) {
	c := newPageCache()
	c.put("u", page{text: "abc"})
	c.put("u", page{text: "abcd"})
	if len(c.entries) != 1 || c.chars != 4 {
		t.Fatalf("entries = %d, chars = %d", len(c.entries), c.chars)
	}
}
