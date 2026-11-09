// Package mediaunderstand 提供视频与图片理解能力及各模型端点的适配层。
// 与 imagegen / videogen 一样，understand_media 工具只表达稳定的能力语义（名字与
// schema）；具体模型端点按静态模型契约归类并实现本包 Client 接口，切换模型不会
// 改变 agent 侧调用方式。
//
// 与 imagegen 一致、与 videogen 不同的地方：这类理解接口是同步的（无任务 id、
// 不可轮询），Capability Activity 同步执行，不走 job 机制。
// 方向也和生成类工具相反——它不产出产物，而是消费产物：入参是工具链里已有的
// videoUrl / imageUrl，出参是一段文字。
//
// 视频与图片不分两个工具：底层协议里两者的差别只是每段媒体自带的 MIME，
// 混着问（"这张设定图和这段成片风格一致吗"）本来就是一次调用的事。
package mediaunderstand

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"agentk/internal/artifact"
)

// Request 是一次理解请求，字段不依赖具体 provider 协议。
type Request struct {
	// MediaURLs 待理解的视频/图片位置，按给定顺序进上下文：
	// 受管产物 URL 或公网直链，可混合视频与图片
	MediaURLs []string
	// PreparedMedia 是同进程 capability 已经读取或派生出的内联素材。
	// 普通工具调用留空；read_file 用它把裁过页的 PDF 直接交给 provider，
	// 避免为一次理解写入永久 artifact。
	PreparedMedia []Media
	// Prompt 要模型回答的问题或分析要求
	Prompt string
	// VideoSegment 非 nil 时只理解唯一视频的指定时间区间。区间由支持它的
	// provider 原生裁剪，不在 agent-k 内生成临时视频。
	VideoSegment *VideoSegment
}

// VideoSegment 是原视频时间轴上的半开理解区间。FPS 为 0 表示使用 provider 缺省值。
type VideoSegment struct {
	StartSeconds float64
	EndSeconds   float64
	FPS          float64
}

// Result 理解结果。
type Result struct {
	// Text 模型给出的文字回答
	Text string
	// Params 本次实际发往上游的参数（每段媒体的判定类型与字节数）。
	// 能力层入参只有 URL，究竟按什么类型、多大的一段送进模型是取回那一步定的，
	// 不由此回报就无从判断"模型看到的到底是不是我想给它看的东西"。
	Params map[string]any
}

// Client 是一个媒体理解模型端点：同步执行，返回文字回答。
type Client interface {
	Understand(ctx context.Context, req Request) (Result, error)
}

const (
	// maxMediaItems 单次可带的媒体数量上限。
	maxMediaItems = 10
	// maxTotalBytes 是需要内联的那部分媒体共享的字节预算，不是每段各自的额度，
	// 交给上游自取的那些不占。上游的内联上限远高于此，但请求体越大越容易在网关侧
	// 超时，而 base64 还会把体积撑到 4/3。测试环境放宽到 100MB，以便验证较大视频；
	// 超限直接报错，让模型少给几段或改用更短的片段，而不是等一次必然的失败。
	maxTotalBytes = 100 << 20
	// maxRemoteVideos / maxRemoteImages 是上游对"给 URL 自己去抓"这条路的每请求
	// 配额（视频 1 个、图片 10 张）。超配额的部分回落内联，不是报错——内联没有这条限制。
	maxRemoteVideos = 1
	maxRemoteImages = 10
	// maxRemoteVideoBytes 是上游对 URL 方式视频的体积上限。比内联还紧，
	// 所以大片子反而只能内联。
	maxRemoteVideoBytes = 15 << 20
)

// videoMIME / imageMIME 是 Gemini 文档列出的可内联媒体类型。
// 放在能力层而非 provider 实现中：它同时是工具对 agent 的承诺（"什么样的素材能读"），
// 所有 provider 必须遵守同一份可读类型集合。
var (
	videoMIME = map[string]bool{
		"video/mp4": true, "video/mpeg": true, "video/quicktime": true,
		"video/avi": true, "video/x-flv": true, "video/mpg": true,
		"video/webm": true, "video/wmv": true, "video/3gpp": true,
	}
	imageMIME = map[string]bool{
		"image/png": true, "image/jpeg": true, "image/webp": true,
		"image/heic": true, "image/heif": true,
	}
)

// extMIME 按扩展名判定类型。优先于 Content-Type：CDN 直链常年回
// application/octet-stream，信它会把一段好好的 mp4 判成不支持。
var extMIME = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime",
	".webm": "video/webm", ".avi": "video/avi", ".flv": "video/x-flv",
	".wmv": "video/wmv", ".mpeg": "video/mpeg", ".mpg": "video/mpg",
	".3gp": "video/3gpp",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".webp": "image/webp", ".heic": "image/heic", ".heif": "image/heif",
}

// Media 是取回并完成类型判定的一段媒体。视频与图片使用同一表示，协议层仅以 MIME 区分。
type Media struct {
	URL  string
	MIME string
	// Kind 是 MIME 归约出的粗类别（video / image）
	Kind string
	// Remote 为 true 表示只把 URL 交给上游、由它自己去抓，Data 为空。
	Remote bool
	// Data 是内联下发的字节；Remote 时为空。
	Data []byte
	// Bytes 是已知体积（内联为实际字节数，交上游自取时取 HEAD 报的长度）；
	// 0 表示未知。只用于回报。
	Bytes int
}

// fetchMedia 按序决定每段素材是交给上游按 URL 获取，还是以内联字节发送。
//
// 缺省一律内联，只有 allowRemote 时才考虑把 URL 交给上游自取。这个默认值是实测
// 定的：判断一个 URL 能不能被上游取到，我们这侧没有可靠依据——本机 HEAD 得通只说明
// 它对我们可达，而上游是从公网回来抓的。内网对象存储、staging 域名会遇到 DNS
// 解析失败。而且 URL 方式的视频上限只有 15MB，比内联预算更小，因此它不能扩大可处理
// 素材的范围，只能省去一次字节搬运。没有必要为此让默认行为依赖部署环境；确有公网
// CDN 的部署可显式打开 allowRemote。
//
// 开启后仍是逐项判定，任一条不满足就回落内联（回落总是安全的，所以判定失败一律
// 不算错误）：URL 得公网可达、上游每请求配额（1 个视频 / 10 张图）、视频 15MB 上限。
// 公网可达与否看 artifact.Public——存储方声明"可由公网服务直接读取"，与 videogen
// 各 provider 透传首帧 URL 依赖的是同一条契约；受管私有产物只有本进程读得到，直接内联。
// 认不出的 URL 按外部直链处理——生视频工具登记的产物 URI 就是上游厂商的直链，
// 不在受管存储里，只认受管 URL 等于读不了自己刚生成的视频。
func fetchMedia(ctx context.Context, client *http.Client, storage artifact.Storage, urls []string, allowRemote bool) ([]Media, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("at least one media url is required")
	}
	if len(urls) > maxMediaItems {
		return nil, fmt.Errorf("at most %d media items per call, got %d", maxMediaItems, len(urls))
	}
	f := &fetcher{http: client, storage: storage, budget: maxTotalBytes, allowRemote: allowRemote}
	media := make([]Media, 0, len(urls))
	for i, raw := range urls {
		m, err := f.one(ctx, raw)
		if err != nil {
			return nil, fmt.Errorf("media %d: %w", i, err)
		}
		media = append(media, m)
	}
	return media, nil
}

// inlineRemote 把上一轮交给上游自取的那几段改成内联，已经内联的沿用原字节。
// 用于"上游取不到我们给的 URL"之后的补救。
//
// 不整批重取有两个原因：已内联的字节重下一遍不会改变任何结果，却多给了一次失败
// 机会（素材源在两次调用之间挂掉、URL 过期）；而且预算算的是内联总量，已用掉的
// 那部分必须照原样计入，否则同一组素材首次内联与回退内联会使用不同的预算计算方式。
func inlineRemote(ctx context.Context, client *http.Client, storage artifact.Storage, media []Media) ([]Media, error) {
	f := &fetcher{http: client, storage: storage, budget: maxTotalBytes}
	for _, m := range media {
		if !m.Remote {
			f.budget -= len(m.Data)
		}
	}
	out := make([]Media, 0, len(media))
	for i, m := range media {
		if !m.Remote {
			out = append(out, m)
			continue
		}
		got, err := f.one(ctx, m.URL) // allowRemote 为零值 false，必然走内联
		if err != nil {
			return nil, fmt.Errorf("media %d: %w", i, err)
		}
		out = append(out, got)
	}
	return out, nil
}

// fetcher 持有一次调用内跨素材共享的额度：内联字节预算与上游的 fileUri 配额。
type fetcher struct {
	http    *http.Client
	storage artifact.Storage
	budget  int
	// allowRemote 是否允许把 URL 交给上游自取；false 时一律内联，连 HEAD 都不发
	allowRemote bool
	// remoteVideos / remoteImages 已经派给上游自取的数量
	remoteVideos int
	remoteImages int
}

func (f *fetcher) one(ctx context.Context, rawURL string) (Media, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return Media{}, fmt.Errorf("media url must not be empty")
	}
	if f.storage != nil {
		if object, ok := f.storage.Resolve(rawURL); ok {
			if object.Visibility == artifact.Public {
				if m, ok := f.remote(ctx, rawURL); ok {
					return m, nil
				}
			}
			// 回落内联时从存储读而不是绕一圈 HTTP：更快，也不受 CDN 生效延迟影响。
			return f.inlineFromStorage(ctx, rawURL, object)
		}
	}
	target, err := url.Parse(rawURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return Media{}, fmt.Errorf("media url %q must be an absolute HTTP(S) URL", rawURL)
	}
	if m, ok := f.remote(ctx, rawURL); ok {
		return m, nil
	}
	return f.inlineFromURL(ctx, rawURL, target)
}

// remote 判定这段素材能否只给 URL、由上游自己去抓，并在可以时占用一格配额。
// 一次 HEAD 同时拿到类型与体积——两者都是判定所需，分两次问没有意义。
// 返回 false 表示不满足 URL 直传条件，应回退内联；这不是错误。
func (f *fetcher) remote(ctx context.Context, rawURL string) (Media, bool) {
	if !f.allowRemote {
		return Media{}, false
	}
	target, err := url.Parse(rawURL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return Media{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target.String(), nil)
	if err != nil {
		return Media{}, false
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return Media{}, false // HEAD 不通（不少 CDN 直接 405）就照常下载
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Media{}, false
	}
	// 判不出类型时也回落：下载下来按字节嗅探还有一次机会。
	mimeType, err := detectMIME(target.Path, resp.Header.Get("Content-Type"), nil)
	if err != nil {
		return Media{}, false
	}
	kind := mediaKind(mimeType)
	if kind == "video" {
		if f.remoteVideos >= maxRemoteVideos {
			return Media{}, false
		}
		// 体积未知时直接内联，避免上游超限后只返回含糊的错误。
		if resp.ContentLength <= 0 || resp.ContentLength > maxRemoteVideoBytes {
			return Media{}, false
		}
		f.remoteVideos++
	} else {
		if f.remoteImages >= maxRemoteImages {
			return Media{}, false
		}
		f.remoteImages++
	}
	return Media{
		URL: rawURL, MIME: mimeType, Kind: kind,
		Remote: true, Bytes: int(max(resp.ContentLength, 0)),
	}, true
}

func (f *fetcher) inlineFromStorage(ctx context.Context, rawURL string, object artifact.Object) (Media, error) {
	reader, err := f.storage.Open(ctx, object)
	if err != nil {
		return Media{}, fmt.Errorf("open %s: %w", rawURL, err)
	}
	defer func() { _ = reader.Close() }()
	data, err := f.read(reader, rawURL)
	if err != nil {
		return Media{}, err
	}
	return classify(rawURL, object.Key, "", data)
}

func (f *fetcher) inlineFromURL(ctx context.Context, rawURL string, target *url.URL) (Media, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Media{}, err
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return Media{}, fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Media{}, fmt.Errorf("download %s: HTTP %d", rawURL, resp.StatusCode)
	}
	data, err := f.read(resp.Body, rawURL)
	if err != nil {
		return Media{}, err
	}
	return classify(rawURL, target.Path, resp.Header.Get("Content-Type"), data)
}

// read 读满剩余预算为止，并从预算里扣掉。预算是需要内联的素材共享的，所以超限的
// 信息里要说清这是总额而不是单段上限——否则模型会以为换个小一点的同一段素材就能过。
func (f *fetcher) read(r io.Reader, source string) ([]byte, error) {
	if f.budget <= 0 {
		return nil, fmt.Errorf("the %d-byte inline budget is already used up before %s; pass fewer or shorter items",
			maxTotalBytes, source)
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(f.budget)+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	if len(data) > f.budget {
		return nil, fmt.Errorf("media %s exceeds the remaining %d of %d shared inline bytes; pass fewer or shorter items",
			source, f.budget, maxTotalBytes)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("media %s is empty", source)
	}
	f.budget -= len(data)
	return data, nil
}

func classify(rawURL, name, contentType string, data []byte) (Media, error) {
	mimeType, err := detectMIME(name, contentType, data)
	if err != nil {
		return Media{}, fmt.Errorf("%s: %w", rawURL, err)
	}
	return Media{
		URL: rawURL, MIME: mimeType, Kind: mediaKind(mimeType),
		Data: data, Bytes: len(data),
	}, nil
}

// detectMIME 判定媒体类型：扩展名 > 声明的 Content-Type > 按字节嗅探。
// name 是文件名或 URL 路径，contentType 可为空。判不出或判出不支持的类型即报错——
// 任意猜测一种类型只会让模型基于未正确解码的字节生成错误结论。
//
// 类型不进工具 schema 让模型填：它填的与字节里实际是什么没有必然联系，填错了
// 仍要按真实字节纠正，不如只探测、再把探测结果随结果回报。
func detectMIME(name, contentType string, data []byte) (string, error) {
	if mimeType, ok := extMIME[strings.ToLower(path.Ext(name))]; ok {
		return mimeType, nil
	}
	if declared, _, err := mime.ParseMediaType(contentType); err == nil && supported(declared) {
		return declared, nil
	}
	if sniffed, _, err := mime.ParseMediaType(http.DetectContentType(data)); err == nil && supported(sniffed) {
		return sniffed, nil
	}
	return "", fmt.Errorf("cannot determine a supported media type (declared %q); "+
		"supported: mp4/mov/webm/avi/flv/wmv/mpeg/3gp video and png/jpeg/webp/heic images", contentType)
}

func supported(mimeType string) bool { return videoMIME[mimeType] || imageMIME[mimeType] }

// mediaKind 把 MIME 归约成回报给调用方的粗类别（video / image）。
func mediaKind(mimeType string) string {
	if videoMIME[mimeType] {
		return "video"
	}
	return "image"
}

// describe 记录每段媒体的实际类型、大小和发送方式，调用方可据此确认系统如何处理
// 输入 URL，并在类型判定错误时定位问题。
// delivery 尤其要回报——同一段素材走 URL 还是内联会落在不同的上游限制上，
// 出问题时这是第一个要看的东西。
func describe(media []Media) []map[string]any {
	out := make([]map[string]any, 0, len(media))
	for _, m := range media {
		delivery := "inline"
		if m.Remote {
			delivery = "url"
		}
		item := map[string]any{
			"url": m.URL, "type": m.MIME, "kind": m.Kind, "delivery": delivery,
		}
		if m.Bytes > 0 {
			item["bytes"] = m.Bytes
		}
		out = append(out, item)
	}
	return out
}

// newHTTPClient 按可选代理构造 HTTP 客户端（行为与 imagegen / webtool 一致：
// 无全局超时，单次请求由调用方 ctx 约束）。
func newHTTPClient(proxyURL string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("mediaunderstand: parse proxy url %q: %w", proxyURL, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("mediaunderstand: proxy url %q must be absolute", proxyURL)
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
