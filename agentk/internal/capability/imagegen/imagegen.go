// Package imagegen 是生图能力的模型供应商适配层。
// 包结构与 videogen 一致：generate_image 工具只表达稳定的能力语义（名字与 schema），
// 具体模型端点由静态模型目录配置并实现本包 Client 接口，切换模型不影响 agent。
//
// 与 videogen 的关键差别：这些生图 API 都是同步的（无任务 id、不可轮询），
// Capability Activity 同步执行，不走 job 机制。
// 产物流向：生成字节 → blob 上传 → 返回 URL，
// agent 可把该 URL 作为生视频工具的首帧入参；要求公网 URL 的视频供应商需配置 CDN。
package imagegen

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Request 是不含供应商协议细节的公共生图参数。
type Request struct {
	// Prompt 画面内容与风格描述
	Prompt string
	// AspectRatio 画幅 "1:1" / "9:16" / "16:9"；空用 "1:1"。
	// 具体分辨率由供应商实现映射，因为各后端支持的 size 枚举不同。
	AspectRatio string
}

// Image 生成结果：图片字节与 MIME 类型。
type Image struct {
	Data []byte
	MIME string
	// Params 是本次实际发往上游的参数，例如 size、quality 等供应商参数。
	// 公共入参只有画幅，具体分辨率与画质档由供应商映射或配置补充，
	// 不由此回报就无从知道同一个 "1:1" 在各端点上究竟出了多大的图。
	// 只记录标量参数：prompt 已经存在，无需重复；图片字节会使事件日志过大。
	Params map[string]any
}

// Client 是一个生图模型端点：同步生成，返回图片字节。
type Client interface {
	Generate(ctx context.Context, req Request) (Image, error)
}

// maxImageBytes 图片响应/下载体积上限（gpt-image 高分辨率 PNG 可达十几 MB）。
const maxImageBytes = 64 << 20

// download 拉取结果 URL 的图片字节；部分供应商返回临时 URL 而不是 b64。
func download(ctx context.Context, client *http.Client, imageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download result: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download result %s: HTTP %d", imageURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("download result exceeds %d bytes", maxImageBytes)
	}
	return data, nil
}

// mergeParams 将供应商回报的实际参数叠加到公共参数上；同名时以供应商值为准，
// 因为它才是实际发送的值。
func mergeParams(base, dialect map[string]any) map[string]any {
	for k, v := range dialect {
		base[k] = v
	}
	return base
}

func imageSize(sizes map[string]string, aspectRatio, defaultRatio string) (string, error) {
	if aspectRatio == "" {
		aspectRatio = defaultRatio
	}
	size, ok := sizes[aspectRatio]
	if !ok {
		return "", fmt.Errorf("imagegen: unsupported aspect ratio %q", aspectRatio)
	}
	return size, nil
}

// newHTTPClient 按可选代理构造 HTTP 客户端（IDC 环境访问公网 API 用；空则直连）。
// 无全局超时：单次请求由调用方 ctx（工具超时）约束。
func newHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("imagegen: parse proxy url %q: %w", proxyURL, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("imagegen: proxy url %q must be absolute", proxyURL)
		}
		transport.Proxy = http.ProxyURL(u)
	}
	return &http.Client{Transport: transport}, nil
}

func snippet(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
