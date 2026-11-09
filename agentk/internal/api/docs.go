package api

import (
	_ "embed"
	"net/http"
)

// openapiSpec 同时提供给机器读取的 /openapi.yaml 和浏览器文档 /docs。
// OpenAPI 文件手工维护；docs 页面从 jsDelivr 加载固定大版本的 Scalar，不在仓库
// vendor 前端产物，因此浏览器需要能够访问该 CDN。

//go:embed openapi.yaml
var openapiSpec []byte

const docsPage = `<!doctype html>
<html>
<head>
  <title>AgentK API</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
</head>
<body>
  <div id="app"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1"></script>
  <script>Scalar.createApiReference('#app', { url: '/openapi.yaml' })</script>
</body>
</html>
`

func registerDocs(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openapiSpec)
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPage))
	})
}
