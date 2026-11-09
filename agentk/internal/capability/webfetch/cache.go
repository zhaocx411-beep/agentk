package webfetch

import (
	"sync"
	"time"
)

// 抓取缓存只服务"同一页面继续读取下一段"，因此容量和时效都很小。缓存未命中时
// 重新抓取，读取语义不变；多实例部署原本也不保证后续请求进入同一进程。
const (
	cacheTTL        = 10 * time.Minute
	cacheMaxEntries = 16
	// cacheMaxChars 是全部条目正文字符数之和的上限，避免少数大页面占用过多内存。
	cacheMaxChars = 4 << 20
)

type cacheEntry struct {
	page     page
	storedAt time.Time
}

// pageCache 缓存已抓取页面的正文，供 offset 翻页复用。
// 模型读取第二段时不应重新下载整个页面：这不但更慢，还可能取得与首段不一致的
// 快照；如果页面在两次请求之间变化，offset 就会落在错误的位置。
type pageCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	// chars 是当前缓存的正文字符总数，与 entries 同步维护。
	chars int
	// now 供测试注入；nil 时使用 time.Now。
	now func() time.Time
}

func newPageCache() *pageCache {
	return &pageCache{entries: make(map[string]cacheEntry, cacheMaxEntries)}
}

func (c *pageCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// get 取未过期的缓存页；未命中返回 false。
func (c *pageCache) get(url string) (page, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[url]
	if !ok {
		return page{}, false
	}
	if c.clock().Sub(e.storedAt) > cacheTTL {
		c.removeLocked(url)
		return page{}, false
	}
	return e.page, true
}

func (c *pageCache) put(url string, p page) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.entries[url]; dup {
		c.removeLocked(url)
	}
	c.entries[url] = cacheEntry{page: p, storedAt: c.clock()}
	c.chars += len(p.text)
	c.evictLocked()
}

func (c *pageCache) removeLocked(url string) {
	e, ok := c.entries[url]
	if !ok {
		return
	}
	c.chars -= len(e.page.text)
	delete(c.entries, url)
}

// evictLocked 先清过期项，仍超限则按存入先后逐个淘汰最老的。
// 条目数很少，线性扫描比维护 LRU 链更简单，也不会成为性能热点。
func (c *pageCache) evictLocked() {
	now := c.clock()
	for url, e := range c.entries {
		if now.Sub(e.storedAt) > cacheTTL {
			c.removeLocked(url)
		}
	}
	for len(c.entries) > cacheMaxEntries || c.chars > cacheMaxChars {
		oldestURL := ""
		var oldestAt time.Time
		for url, e := range c.entries {
			if oldestURL == "" || e.storedAt.Before(oldestAt) {
				oldestURL, oldestAt = url, e.storedAt
			}
		}
		if oldestURL == "" {
			return
		}
		c.removeLocked(oldestURL)
	}
}
