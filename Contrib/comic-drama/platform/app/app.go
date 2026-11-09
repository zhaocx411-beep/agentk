// Package app 组装完整的 HTTP 服务：漫剧 API、通用 Chat API、健康检查、
// 内嵌前端与访问日志。各 Agent 服务只负责注册自己的路由。
package app

import (
	"net/http"

	comicdrama "studio/agents/comic-drama"
	"studio/platform/chat"
	"studio/platform/core"
)

// Handler 组合全部路由；agents 注册表同时供 /v1/agents 与前端路由校验使用。
func Handler(
	drama *comicdrama.Server,
	chatServer *chat.Server,
	agents chat.AgentRegistry,
) http.Handler {
	mux := http.NewServeMux()
	drama.Register(mux)
	chatServer.Register(mux)
	// 资产库：聚合漫剧与聊天两类项目的产物。
	sources := []assetSource{drama, chatServer}
	mux.HandleFunc("GET /v1/assets", handleListAssets(sources))
	mux.HandleFunc("GET /v1/assets/download", handleDownloadAsset(sources))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// 内嵌前端。"GET /" 优先级低于以上更具体的 API 路由。
	mux.Handle("GET /", webHandler(agents))
	return core.AccessLog(mux)
}
