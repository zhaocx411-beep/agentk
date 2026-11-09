// 编译期内嵌通用 Chat 前端（chat.html / chat.css / chat.js）。
package chat

import (
	"embed"
	"io/fs"
)

//go:embed web
var webFS embed.FS

// WebFS 聊天页静态资源；chat.css 与 chat.js 以绝对路径引用。
func WebFS() fs.FS {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	return sub
}

// Page 通用聊天页（挂在 /agents/{agent}/chat/）。
func Page() []byte {
	page, err := webFS.ReadFile("web/chat.html")
	if err != nil {
		panic(err)
	}
	return page
}
