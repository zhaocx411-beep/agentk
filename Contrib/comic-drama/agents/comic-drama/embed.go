// 编译期内嵌漫剧工坊前端与缺省 Profile，部署时无需额外静态文件。
package comicdrama

import (
	"embed"
	"io/fs"

	"studio/platform/core"
)

//go:embed web
var webFS embed.FS

//go:embed profile-agent.json profile-pipeline.json
var profileFS embed.FS

// WebFS 漫剧工坊静态资源（index.html 与 art-modern.css，均以绝对路径引用）。
func WebFS() fs.FS {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	return sub
}

// IndexPage 漫剧工坊页面（挂在 /agents/comic-drama/）。
func IndexPage() []byte {
	page, err := webFS.ReadFile("web/index.html")
	if err != nil {
		panic(err)
	}
	return page
}

// DefaultProfile 按运行模式返回内嵌的缺省 Profile；PROFILE_FILE 指定
// 磁盘路径时以磁盘为准（见 cmd/apiserver）。
func DefaultProfile(mode core.DramaMode) []byte {
	name := "profile-agent.json"
	if mode == core.ModePipeline {
		name = "profile-pipeline.json"
	}
	raw, err := profileFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return raw
}
