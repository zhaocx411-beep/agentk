package webfetch

import (
	"io"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// skipElements 是正文提取时整棵子树丢弃的元素：脚本、样式、内联 SVG 等
// 不属于正文，保留只会占用上下文。head 不整体丢弃，因为标题来自其中；head 中
// 其他可能包含文本的元素已经单独列出。
var skipElements = map[atom.Atom]bool{
	atom.Script:   true,
	atom.Style:    true,
	atom.Noscript: true,
	atom.Svg:      true,
	atom.Iframe:   true,
	atom.Template: true,
	atom.Meta:     true,
	atom.Link:     true,
	atom.Base:     true,
}

// blockElements 是取完文本后要断行的元素：HTML 里的段落边界在纯文本里
// 只剩换行，不补的话相邻段落会粘成一句话。
var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true, atom.Hr: true,
	atom.Li: true, atom.Ul: true, atom.Ol: true, atom.Dt: true, atom.Dd: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Tr: true, atom.Table: true, atom.Section: true, atom.Article: true,
	atom.Header: true, atom.Footer: true, atom.Nav: true, atom.Aside: true,
	atom.Blockquote: true, atom.Pre: true, atom.Figcaption: true, atom.Form: true,
}

// extractText 把 HTML 文档解析成标题与正文纯文本，图片与视频地址内联留在原位。
// 只做"去噪 + 保留段落"，不使用 readability 一类启发式算法识别正文。这样会保留
// 导航栏和页脚等噪声，但不会因正文识别错误而丢掉真正内容。
// pageURL 是页面的落地地址，用于把相对路径的媒体地址补全成绝对地址。
func extractText(r io.Reader, pageURL string) (title, text string, err error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", "", err
	}
	// 页面地址无法解析时不设置基地址：绝对媒体地址仍可使用，相对地址则丢弃。
	base, _ := url.Parse(pageURL)
	seen := make(map[string]bool)
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		//exhaustive:ignore 第三方枚举；这里只处理与正文相关的两种节点。
		switch n.Type {
		case html.TextNode:
			writeCollapsed(&b, n.Data)
			return
		case html.ElementNode:
			if skipElements[n.DataAtom] {
				return
			}
			if n.DataAtom == atom.Title && title == "" {
				title = strings.TrimSpace(nodeText(n))
				return
			}
		}
		block := n.Type == html.ElementNode && blockElements[n.DataAtom]
		if block {
			b.WriteByte('\n')
		}
		// 媒体标记独占一行：<img> 是内联元素，但把地址插进句子中间会割断正文。
		// 标记保留在原位置而不汇总成清单，因为相邻文字是模型区分主图、推荐位和
		// 页脚图标的唯一依据；汇总会丢失这个位置信号。
		if marker := mediaMarker(n, base, seen); marker != "" {
			b.WriteString("\n" + marker + "\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if block {
			b.WriteByte('\n')
		}
	}
	walk(doc)
	return title, tidyLines(b.String()), nil
}

// tinyPixels 是"图标级尺寸"的判定线：元素显式声明的宽高均不超过该值时，不把它
// 当作媒体素材。
const tinyPixels = 40

// dropExtensions 是不当图片素材看的扩展名：svg 是站点用来画 UI 的矢量图形，
// ico 是站点图标。它们进入正文只会增加噪声，也不能作为视频生成的首帧。
var dropExtensions = []string{".svg", ".ico"}

// mediaMarker 为图片/视频元素生成一行内联标记，不是媒体、地址不可用或已出现过
// 则返回空串。标记使用模型可识别的 Markdown 图片或链接语法，并与周围文字分行。
// 只带地址不带 alt——alt 几乎总在重复邻近的正文，而一个页面上百张图时，
// 同时保留 alt 可能消耗单次上下文预算的八成。
func mediaMarker(n *html.Node, base *url.URL, seen map[string]bool) string {
	if n.Type != html.ElementNode {
		return ""
	}
	// 图片使用 Markdown 图片语法；Markdown 没有视频语法，因此视频使用带标签的
	// 链接。两者格式不同，模型不会混淆图片和视频。
	open := ""
	//exhaustive:ignore 第三方枚举；全部 HTML 标签中只有这三个携带目标媒体。
	switch n.DataAtom {
	case atom.Img:
		open = "![]("
	case atom.Video:
		open = "[video]("
	case atom.Source:
		// <source> 只认 <video> 下的：<picture> 里的候选和它必备的 <img> 兜底
		// 指同一张图，两处都取会把同一张图重复内联；<audio> 不在本工具范围内。
		if n.Parent == nil || n.Parent.DataAtom != atom.Video {
			return ""
		}
		open = "[video]("
	default:
		return ""
	}
	abs := absoluteURL(base, mediaURL(n))
	if abs == "" || seen[abs] || tinyByAttrs(n) {
		return ""
	}
	if n.DataAtom == atom.Img && hasAnySuffix(abs, dropExtensions) {
		return ""
	}
	seen[abs] = true
	return open + abs + ")"
}

// mediaURL 从元素属性里挑一个地址。src 不是首选：懒加载站点把占位图放 src、
// 真图放 data-src；亚马逊这类站点更把压过的缩略图放 src、原图放 data-old-hires
// （后者是 Amazon 自有约定而非标准，但额外检查该属性只需一次查表）。只读取 src
// 会得到低分辨率图片，用作视频生成首帧时尤其明显。
func mediaURL(n *html.Node) string {
	for _, name := range []string{"data-old-hires", "data-src", "data-original", "data-lazy-src", "src"} {
		if v := attrValue(n, name); v != "" {
			return v
		}
	}
	// srcset 兜底且只取第一个候选：按描述符挑最大的那张要完整实现 srcset 的
	// 分隔规则，因为 URL 可以合法包含逗号，例如 ..._SR165,165_.jpg；完整实现的
	// 复杂度高于收益，这里只需要取得一张可用图片。
	if fields := strings.Fields(attrValue(n, "srcset")); len(fields) > 0 {
		return strings.TrimSuffix(fields[0], ",")
	}
	return ""
}

// absoluteURL 把元素上的地址解析成绝对 http(s) 地址；拿不到就返回空串。
// 相对路径按页面落地地址补全。不支持 <base href>：它已由 skipElements 丢弃，且
// 实际站点很少使用。data: 与 javascript: 等内联或伪协议一律丢弃，因为它们不是
// 可交给下游模型继续使用的地址。
func absoluteURL(base *url.URL, raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !u.IsAbs() {
		if base == nil {
			return ""
		}
		u = base.ResolveReference(u)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.Fragment = "" // 同一张图带不同 fragment 是同一个资源，去掉才能去重
	return u.String()
}

// tinyByAttrs 判断元素是否显式声明了图标级尺寸：1x1 的追踪像素和 16px 的 UI
// 图标通过该规则过滤。只有宽高均显式声明且都很小时才丢弃，避免把一边窄、一边宽
// 的横幅误判成图标。
func tinyByAttrs(n *html.Node) bool {
	w, wok := intAttr(n, "width")
	h, hok := intAttr(n, "height")
	return wok && hok && w <= tinyPixels && h <= tinyPixels
}

// attrValue 取属性值（去首尾空白）；没有该属性返回空串。
func attrValue(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

// intAttr 取整数属性值。"100px" 这类带单位的写法不认（HTML 属性里本不合法）。
func intAttr(n *html.Node, name string) (int, bool) {
	v, err := strconv.Atoi(attrValue(n, name))
	if err != nil {
		return 0, false
	}
	return v, true
}

// hasAnySuffix 按扩展名匹配 URL 路径部分，忽略大小写与查询串。
func hasAnySuffix(rawURL string, suffixes []string) bool {
	path := strings.ToLower(rawURL)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	for _, s := range suffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

// writeCollapsed 写入文本节点，内部连续空白折叠成一个空格。
// HTML 源码中的换行通常只是排版而非内容；保留会使正文换行随源码缩进变化。
func writeCollapsed(b *strings.Builder, s string) {
	space := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f', '\v', '\u00a0':
			space = true
		default:
			if space {
				b.WriteByte(' ')
				space = false
			}
			b.WriteRune(r)
		}
	}
	if space {
		b.WriteByte(' ')
	}
}

// nodeText 取一个节点下的全部文本（标题等短文本用）。
func nodeText(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
			continue
		}
		b.WriteString(nodeText(c))
	}
	return b.String()
}

// tidyLines 逐行去除首尾空白、丢弃空行并把连续空行压成一个。块级元素会无条件
// 补换行，因此嵌套 div 可能产生大量空行。
func tidyLines(s string) string {
	var out []string
	blank := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			blank = len(out) > 0
			continue
		}
		if blank {
			out = append(out, "")
			blank = false
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
