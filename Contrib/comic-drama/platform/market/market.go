// Agent 集市页：静态外壳内嵌进二进制，目录数据运行时从 /v1/agents 拉取
// （数据源是各 Agent 目录的 meta.json，经 platform/chat 注册表聚合）。
// 资产库页同属平台级页面，一并托管在这里。
package market

import "embed"

//go:embed web/market.html web/assets.html
var webFS embed.FS

// Page 集市页（挂在 /）。
func Page() []byte {
	return mustRead("web/market.html")
}

// AssetsPage 资产库页（挂在 /assets/）。
func AssetsPage() []byte {
	return mustRead("web/assets.html")
}

func mustRead(name string) []byte {
	page, err := webFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return page
}
