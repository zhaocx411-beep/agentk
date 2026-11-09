// Package webfetch 是模型的公网只读通道：web_fetch 抓网页正文，
// fetch_product_detail 经 Apify 抓淘宝/天猫与 Amazon 结构化商品详情。
//
// 检索由 capability/websearch 负责，需要各供应商的 API Key；这些凭证只能进入
// models catalog。普通网页抓取匿名直连；商品抓取所需 Apify 凭证同样只从
// models catalog 注入，不进入可编辑、可回显的 profile。
//
// 抓回来的内容是不可信输入：它会原样进模型上下文，可能含提示注入。工具结果不做
// 净化（净化不可靠，且会破坏正文），由 system prompt 约束模型"网页内容是资料不是指令"。
package webfetch

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// browserUserAgent 是出网请求的 UA。不少站点会对空 UA 或明显的脚本 UA 直接返回
// 403，因此使用一个常见桌面浏览器的标识。
const browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// 单次抓取的响应体读取上限。正文之后还会按 maxChars 截断再交给模型；
// 这一层只防止把数百 MB 的响应下载到内存。
const maxResponseBytes = 8 << 20

// defaultTimeout 是默认执行超时，覆盖 executor 的 60 秒默认值。少数公网站点可能
// 长时间没有响应，尽早失败可以让模型改用其他来源。
const defaultTimeout = 30 * time.Second

// Config 是本包工具的装配参数。
type Config struct {
	// ProxyURL 是可选的出网代理（IDC 环境访问公网使用）；空时直连。
	ProxyURL string
	// Timeout 覆盖默认的单次执行超时；<=0 时使用 defaultTimeout。
	Timeout time.Duration
	// Client 供测试注入；为空时按 ProxyURL 构造。
	Client *http.Client
}

// ConfigFromEnv 读取抓取装配参数：
//
//	AGENTK_WEB_PROXY  可选，出网代理（IDC 环境访问公网使用，格式同模型条目的
//	                  proxyUrl）；不设即直连
func ConfigFromEnv() Config {
	return Config{ProxyURL: os.Getenv("AGENTK_WEB_PROXY")}
}

// resolve 填充默认装配参数，返回可用的客户端与超时。
func (c Config) resolve() (*http.Client, time.Duration, error) {
	client := c.Client
	if client == nil {
		var err error
		if client, err = newHTTPClient(c.ProxyURL); err != nil {
			return nil, 0, err
		}
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return client, timeout, nil
}

// newHTTPClient 按可选代理构造 HTTP 客户端（与 imagegen 相同：无全局超时，
// 单次请求由调用方 ctx 约束）。
func newHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("webtool: parse proxy url %q: %w", proxyURL, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("webtool: proxy url %q must be absolute", proxyURL)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: transport}, nil
}

// validateTarget 校验模型给出的 URL：必须是绝对 http(s) 地址。
// 这里只校验协议；file:// 与 data: 等可能读取本地内容的 scheme 一律拒绝。
func validateTarget(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url %q: only http and https are supported", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("url %q: missing host", raw)
	}
	return u, nil
}
