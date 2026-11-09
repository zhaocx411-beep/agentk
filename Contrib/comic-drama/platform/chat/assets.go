package chat

import "studio/platform/core"

// Projects 把内存里的聊天会话映射成资产库项目。只读缓存的折叠现场，
// 不回源 agent-k；无产物的会话不出现在资产库里。
func (s *Server) Projects() []core.AssetProject {
	chats := s.chats.List()
	projects := make([]core.AssetProject, 0, len(chats))
	for _, chat := range chats {
		var assets []core.Asset
		for _, message := range chat.State.Messages {
			for _, artifact := range message.Artifacts {
				switch artifact.Type {
				case "image", "video", "audio":
				default:
					continue
				}
				if artifact.URI == "" {
					continue
				}
				assets = append(assets, core.Asset{
					Kind: artifact.Type, URI: artifact.URI, At: message.CreatedAt,
				})
			}
		}
		if len(assets) == 0 {
			continue
		}
		projects = append(projects, core.AssetProject{
			ID:        chat.ID,
			Kind:      "chat",
			Title:     chat.Title,
			Agent:     s.agents.NameOf(chat.AgentID),
			CreatedAt: chat.CreatedAt,
			UpdatedAt: chat.UpdatedAt,
			Assets:    assets,
		})
	}
	return projects
}
