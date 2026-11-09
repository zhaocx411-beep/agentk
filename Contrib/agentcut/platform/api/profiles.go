package api

import (
	"net/http"
	"strings"
)

const defaultProfileID = "agentcut"

// ProfileDefinition 是服务端可用于创建 agent-k Session 的 Profile。
// ProfileJSON 永不通过列表接口返回。
type ProfileDefinition struct {
	ProfileID   string
	Name        string
	Description string
	ProfileJSON []byte
	IsDefault   bool
}

type profileSummary struct {
	ID          string `json:"id"`
	ProfileID   string `json:"profileId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"isDefault,omitempty"`
}

func (s *Server) availableProfiles() []ProfileDefinition {
	if len(s.Profiles) > 0 {
		return s.Profiles
	}
	// 兼容现有测试和嵌入方只设置 Server.Profile 的构造方式。
	return []ProfileDefinition{{
		ProfileID:   defaultProfileID,
		Name:        "剪辑 Agent",
		Description: "通用视频剪辑 Agent",
		ProfileJSON: s.Profile,
		IsDefault:   true,
	}}
}

func normalizeProfileID(id string) string {
	id = strings.TrimSpace(id)
	if id == "agentcut-default" {
		return defaultProfileID
	}
	return id
}

func (s *Server) resolveProfile(id string) (ProfileDefinition, bool) {
	id = normalizeProfileID(id)
	if id == "" {
		for _, profile := range s.availableProfiles() {
			if profile.IsDefault {
				return profile, true
			}
		}
		profiles := s.availableProfiles()
		if len(profiles) > 0 {
			return profiles[0], true
		}
		return ProfileDefinition{}, false
	}
	for _, profile := range s.availableProfiles() {
		if profile.ProfileID == id {
			return profile, true
		}
	}
	return ProfileDefinition{}, false
}

func (s *Server) handleListProfiles(w http.ResponseWriter, _ *http.Request) {
	definitions := s.availableProfiles()
	profiles := make([]profileSummary, 0, len(definitions))
	for _, profile := range definitions {
		profiles = append(profiles, profileSummary{
			ID: profile.ProfileID, ProfileID: profile.ProfileID, Name: profile.Name,
			Description: profile.Description, IsDefault: profile.IsDefault,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}
