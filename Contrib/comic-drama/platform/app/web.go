// 前端路由：Agent 集市、漫剧工坊与通用 Chat 页面的统一入口。
package app

import (
	"io/fs"
	"net/http"
	"strings"

	comicdrama "studio/agents/comic-drama"
	"studio/platform/chat"
	"studio/platform/market"
)

// webHandler 托管全部内嵌前端，挂在 "/" 上：
//   - /                       Agent 集市
//   - /assets/               资产库（跨 Agent 的产物总览）
//   - /agents/comic-drama/   漫剧工坊
//   - /agents/{agent}/chat/  固定 Profile 的通用 Chat
//
// 其他静态资源（art-modern.css、chat.css、chat.js）由各包的内嵌 FS 合并
// 后经 FileServer 提供。API 路由（/v1/...、/healthz）在 mux 里更具体，
// 优先级高于它。
func webHandler(agents chat.AgentRegistry) http.Handler {
	marketPage := market.Page()
	assetsPage := market.AssetsPage()
	workshopPage := comicdrama.IndexPage()
	chatPage := chat.Page()
	files := http.FileServer(http.FS(mergedFS{comicdrama.WebFS(), chat.WebFS()}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			serveEmbeddedPage(w, marketPage)
		case "/assets":
			http.Redirect(w, r, "/assets/", http.StatusMovedPermanently)
		case "/assets/":
			serveEmbeddedPage(w, assetsPage)
		case "/agents/comic-drama":
			http.Redirect(w, r, "/agents/comic-drama/", http.StatusMovedPermanently)
		case "/agents/comic-drama/":
			serveEmbeddedPage(w, workshopPage)
		default:
			if agentID, needsSlash, ok := chatPageAgentID(r.URL.Path); ok {
				if _, exists := agents.Get(agentID); !exists {
					if agentID == "basic-assistant" {
						http.Redirect(w, r, "/", http.StatusSeeOther)
						return
					}
					http.NotFound(w, r)
					return
				}
				if needsSlash {
					http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
					return
				}
				serveEmbeddedPage(w, chatPage)
				return
			}
			files.ServeHTTP(w, r)
		}
	})
}

// mergedFS 依次尝试每个 FS，第一个命中的生效；各包的静态资源文件名
// 互不重叠（art-modern.css vs chat.*）。
type mergedFS []fs.FS

func (m mergedFS) Open(name string) (fs.File, error) {
	for _, candidate := range m {
		if file, err := candidate.Open(name); err == nil {
			return file, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func chatPageAgentID(requestPath string) (agentID string, needsSlash bool, ok bool) {
	const prefix = "/agents/"
	if !strings.HasPrefix(requestPath, prefix) {
		return "", false, false
	}
	remainder := strings.TrimPrefix(requestPath, prefix)
	switch {
	case strings.HasSuffix(remainder, "/chat/"):
		agentID = strings.TrimSuffix(remainder, "/chat/")
	case strings.HasSuffix(remainder, "/chat"):
		agentID = strings.TrimSuffix(remainder, "/chat")
		needsSlash = true
	default:
		return "", false, false
	}
	if agentID == "" || strings.Contains(agentID, "/") {
		return "", false, false
	}
	return agentID, needsSlash, true
}

func serveEmbeddedPage(w http.ResponseWriter, page []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(page)
}
