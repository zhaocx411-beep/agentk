package websearch

import (
	"strings"
	"testing"
)

func TestValidateRequest(t *testing.T) {
	cases := []struct {
		name string
		req  Request
		ok   bool
	}{
		{"最小合法入参", Request{Query: "深圳天气"}, true},
		{"带合法档位", Request{Query: "q", TimeRange: TimeRangeWeek}, true},
		{"空检索词", Request{Query: "   "}, false},
		{"超长检索词", Request{Query: strings.Repeat("字", maxQueryChars+1)}, false},
		{"恰好到上限", Request{Query: strings.Repeat("字", maxQueryChars)}, true},
		{"非法档位", Request{Query: "q", TimeRange: "hour"}, false},
		{"档位不认上游写法", Request{Query: "q", TimeRange: "OneWeek"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateRequest(c.req)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

// TestNormalizeResults 覆盖公共能力层对各供应商结果的统一整理：无链接的丢掉、
// 摘要截断和结果数上限由公共层统一，不能随供应商变化。
func TestNormalizeResults(t *testing.T) {
	in := []Result{
		{Title: " 有空白 ", URL: " https://a.test ", Summary: " 摘要 "},
		{Title: "没有链接", URL: "  ", Summary: "会被丢掉"},
		{Title: "长摘要", URL: "https://b.test", Summary: strings.Repeat("字", maxSummaryChars+50)},
	}
	got := normalizeResults(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2（无链接的那条应被丢弃）", len(got))
	}
	if got[0].Title != "有空白" || got[0].URL != "https://a.test" || got[0].Summary != "摘要" {
		t.Fatalf("首条未去空白: %+v", got[0])
	}
	if runes := []rune(got[1].Summary); len(runes) != maxSummaryChars+1 {
		t.Fatalf("摘要长度 = %d，想要 %d（截断后加一个省略号）", len(runes), maxSummaryChars+1)
	}

	// 总数封顶到 defaultCount
	many := make([]Result, 0, defaultCount*2)
	for range defaultCount * 2 {
		many = append(many, Result{Title: "t", URL: "https://x.test"})
	}
	if got := normalizeResults(many); len(got) != defaultCount {
		t.Fatalf("len = %d, want %d", len(got), defaultCount)
	}
}

func TestTruncateCountsRunesNotBytes(t *testing.T) {
	// 中文按字节切会切碎多字节序列：截断必须按字符
	s := strings.Repeat("中", 10)
	got := truncate(s, 5)
	if want := strings.Repeat("中", 5) + "…"; got != want {
		t.Fatalf("truncate = %q, want %q", got, want)
	}
	if got := truncate("短", 5); got != "短" {
		t.Fatalf("未超限不该改动: %q", got)
	}
}

func TestResolveEndpoint(t *testing.T) {
	if got, err := resolveEndpoint("", "https://fallback.test/x"); err != nil || got != "https://fallback.test/x" {
		t.Fatalf("空 baseUrl 应回落方言内置端点: %q %v", got, err)
	}
	if got, err := resolveEndpoint("https://gw.test/s", "https://fallback.test"); err != nil || got != "https://gw.test/s" {
		t.Fatalf("显式 baseUrl 应优先: %q %v", got, err)
	}
	if _, err := resolveEndpoint("not-a-url", "https://fallback.test"); err == nil {
		t.Fatal("相对地址应被拒绝")
	}
}

// TestTimeRangeMappingsCoverAllOptions 两个供应商实现都必须覆盖公共协议的每一个档位。
// 漏一个的后果是运行期才发现：模型以为筛了时间，实际调用直接失败。
func TestTimeRangeMappingsCoverAllOptions(t *testing.T) {
	for _, option := range timeRangeOptions {
		if _, ok := volcTimeRange[option]; !ok {
			t.Errorf("volc 缺少档位 %q 的映射", option)
		}
		if _, ok := googleTimeRange[option]; !ok {
			t.Errorf("brightserp 缺少档位 %q 的映射", option)
		}
	}
}
