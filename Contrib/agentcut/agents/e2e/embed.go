// Package e2eprofile embeds the opt-in, read-only staging E2E profile.
package e2eprofile

import _ "embed"

const (
	ProfileID          = "agentcut-e2e"
	ProfileDisplayName = "AgentCut Staging E2E"
	ProfileDescription = "仅用于受控 staging 验证的 timeline_get 只读 Profile"
)

//go:embed profile.json
var ProfileJSON []byte
