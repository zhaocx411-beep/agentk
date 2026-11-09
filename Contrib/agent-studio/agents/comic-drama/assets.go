package comicdrama

import "studio/platform/core"

// Projects 把内存里的漫剧任务映射成资产库项目。只读缓存的折叠现场，
// 不回源 agent-k：资产随详情页轮询自然更新，避免列表页放大上游压力。
func (s *Server) Projects() []core.AssetProject {
	dramas := s.store.List()
	projects := make([]core.AssetProject, 0, len(dramas))
	for _, drama := range dramas {
		project := core.AssetProject{
			ID:        drama.ID,
			Kind:      "drama",
			Title:     drama.Title,
			Agent:     "漫剧工坊",
			CreatedAt: drama.CreatedAt,
			UpdatedAt: drama.UpdatedAt,
		}
		switch s.mode {
		case core.ModeAgent:
			project.Assets = agentProjectAssets(drama.State)
		case core.ModePipeline:
			project.Assets = manifestAssets(drama.Manifest)
		}
		if len(project.Assets) == 0 {
			continue
		}
		projects = append(projects, project)
	}
	return projects
}

// agentProjectAssets 折叠现场 → 资产清单。Films 里可能有不经 Assets
// 的旧契约成片 URL（result.videoUrl），补录为 video。
func agentProjectAssets(state DramaState) []core.Asset {
	assets := make([]core.Asset, 0, len(state.Assets)+len(state.Films))
	seen := make(map[string]struct{}, len(state.Assets))
	for _, asset := range state.Assets {
		seen[asset.URI] = struct{}{}
		assets = append(assets, core.Asset{
			Kind: asset.Kind, URI: asset.URI, Tool: asset.Tool, At: asset.At,
		})
	}
	for _, url := range state.Films {
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		assets = append(assets, core.Asset{Kind: "video", URI: url, Tool: "compose_video"})
	}
	return assets
}

// manifestAssets pipeline 完成清单 → 资产清单（运行中 manifest 为 nil）。
// URI 去重：复用的分集资产与单集成片（即全剧成片）不重复计入。
func manifestAssets(manifest *core.DramaManifest) []core.Asset {
	if manifest == nil {
		return nil
	}
	var assets []core.Asset
	seen := make(map[string]struct{})
	add := func(kind, uri string) {
		if uri == "" {
			return
		}
		if _, dup := seen[uri]; dup {
			return
		}
		seen[uri] = struct{}{}
		assets = append(assets, core.Asset{Kind: kind, URI: uri})
	}
	for _, character := range manifest.Characters {
		add("image", character.ImageURL)
	}
	for _, episode := range manifest.Episodes {
		for _, asset := range episode.Assets {
			add("image", asset.ImageURL)
		}
		for _, shot := range episode.Shots {
			add("image", shot.FrameURL)
			add("video", shot.VideoURL)
		}
		add("video", episode.Film)
	}
	add("video", manifest.Film)
	return assets
}
