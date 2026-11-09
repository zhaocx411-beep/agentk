// 资产库 API：聚合各 Agent 服务的项目与产物，只读展示 + 下载代理。
//
// GET /v1/assets            全部项目（漫剧 + 聊天），新→旧排序。
// GET /v1/assets/download   代理下载一条已登记的资产。只代理清单里
//
//	存在的 URI（防开放代理），并带 Content-Disposition 强制另存，
//	绕开跨域 <a download> 失效的问题。
package app

import (
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"time"

	"studio/platform/core"
)

// assetSource 是能贡献资产项目的服务；漫剧与聊天服务都实现它。
type assetSource interface {
	Projects() []core.AssetProject
}

// downloadClient 资产文件可能是几百 MB 的成片，超时给足。
var downloadClient = &http.Client{Timeout: 10 * time.Minute}

func assetProjects(sources []assetSource) []core.AssetProject {
	var projects []core.AssetProject
	for _, source := range sources {
		projects = append(projects, source.Projects()...)
	}
	sort.SliceStable(projects, func(i, j int) bool {
		return projects[i].UpdatedAt.After(projects[j].UpdatedAt)
	})
	return projects
}

func handleListAssets(sources []assetSource) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		projects := assetProjects(sources)
		if projects == nil {
			projects = []core.AssetProject{}
		}
		core.WriteJSON(w, http.StatusOK, map[string]any{"projects": projects})
	}
}

func handleDownloadAsset(sources []assetSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.Query().Get("uri")
		if uri == "" {
			core.WriteError(w, http.StatusBadRequest, "uri is required")
			return
		}
		if !assetKnown(sources, uri) {
			core.WriteError(w, http.StatusNotFound, "asset not found")
			return
		}
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, uri, nil)
		if err != nil {
			core.WriteError(w, http.StatusBadRequest, "invalid asset uri")
			return
		}
		response, err := downloadClient.Do(request)
		if err != nil {
			core.WriteError(w, http.StatusBadGateway, "fetch asset: "+err.Error())
			return
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			core.WriteError(w, http.StatusBadGateway,
				"asset host returned "+response.Status)
			return
		}
		if contentType := response.Header.Get("Content-Type"); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if length := response.Header.Get("Content-Length"); length != "" {
			w.Header().Set("Content-Length", length)
		}
		w.Header().Set("Content-Disposition",
			`attachment; filename="`+downloadFilename(uri)+`"`)
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, response.Body)
	}
}

// assetKnown 校验 URI 出现在任一项目的资产清单里。清单在内存中且规模
// 有限（每任务几十条），线性扫描足够。
func assetKnown(sources []assetSource, uri string) bool {
	for _, source := range sources {
		for _, project := range source.Projects() {
			for _, asset := range project.Assets {
				if asset.URI == uri {
					return true
				}
			}
		}
	}
	return false
}

// downloadFilename 从 URL 路径取文件名；取不出时兜底为 asset。
func downloadFilename(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "asset"
	}
	name := path.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		return "asset"
	}
	return name
}
