package websearch

import (
	"context"
	"os"
	"testing"
	"time"
)

// 临时联调用例：走真实端点验证参数拼装与响应解析。
// 凭证从环境变量取，不落文件；未设置即跳过。
func TestLiveVolc(t *testing.T) {
	key := os.Getenv("LIVE_VOLC_KEY")
	if key == "" {
		t.Skip("LIVE_VOLC_KEY not set")
	}
	client, err := NewVolc(VolcConfig{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	results, err := client.Search(ctx, Request{Query: "深圳明天天气", TimeRange: TimeRangeWeek})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	t.Logf("volc 返回 %d 条", len(results))
	for i, r := range results {
		if i >= 3 {
			break
		}
		t.Logf("[%d] %s | %s | %.60s", i+1, r.Title, r.URL, r.Summary)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
}

// 连续执行 4 个查询，测量修复后该链路的实际单次成功率；Activity 侧仍由
// maxAttempts 处理瞬时失败。
func TestLiveBrightSERP(t *testing.T) {
	key, zone := os.Getenv("LIVE_BD_KEY"), os.Getenv("LIVE_BD_ZONE")
	if key == "" || zone == "" {
		t.Skip("LIVE_BD_KEY/LIVE_BD_ZONE not set")
	}
	client, err := NewBrightSERP(BrightSERPConfig{APIKey: key, Zone: zone})
	if err != nil {
		t.Fatal(err)
	}
	queries := []string{
		"golang generics tutorial",
		"temporal workflow retry policy",
		"深圳明天天气",
		"rust async runtime comparison",
	}
	var ok int
	for i, q := range queries {
		if i > 0 {
			time.Sleep(20 * time.Second) // 拉开到冷却期之外，量的是单次成功率
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		results, err := client.Search(ctx, Request{Query: q, TimeRange: TimeRangeYear})
		cancel()
		if err != nil {
			t.Logf("[%s] 失败: %v", q, err)
			continue
		}
		ok++
		t.Logf("[%s] %d 条; 首条: %s | %s", q, len(results), results[0].Title, results[0].URL)
	}
	t.Logf("brightserp 成功 %d/%d", ok, len(queries))
	if ok == 0 {
		t.Fatal("全部失败")
	}
}
