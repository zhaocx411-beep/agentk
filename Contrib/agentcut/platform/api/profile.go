package api

import (
	"fmt"

	apiv1 "git.corp.kuaishou.com/klingai-agentk/agentk-sdk.git/go/gen/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
)

// timelineToolNames 是 Profile 里 contrib.agentcut.* 工具的声明名。
// Agent Loop 不注入 session 上下文，projectId 必须经 toolDefaults 固化到
// 每个项目专属的 Session Profile 里，模型调用时无需（也无法）自己填。
var timelineToolNames = []string{
	"timeline_get",
	"timeline_edit_clips",
	"timeline_edit_tracks",
	"timeline_set_captions",
	"timeline_edit_media",
	"timeline_edit_transcript",
}

// BuildSessionProfile 以 JSON 模板（protojson 编码的 agentk.api.v1.Profile）
// 为基础，为指定项目注入 toolDefaults，返回可直接随 CreateSession 提交的
// Profile 消息。模板里出现 proto 未定义的字段会直接报错（防契约漂移）。
func BuildSessionProfile(template []byte, projectID string) (*apiv1.Profile, error) {
	profile := &apiv1.Profile{}
	if err := protojson.Unmarshal(template, profile); err != nil {
		return nil, fmt.Errorf("parse profile template: %w", err)
	}

	if profile.ToolDefaults == nil {
		profile.ToolDefaults = map[string]*structpb.Struct{}
	}
	configuredTools := make(map[string]bool, len(profile.Tools))
	for _, tool := range profile.Tools {
		configuredTools[tool.GetName()] = true
	}
	for _, name := range timelineToolNames {
		if !configuredTools[name] {
			continue
		}
		defaults := profile.ToolDefaults[name]
		if defaults == nil {
			defaults = &structpb.Struct{}
		}
		if defaults.Fields == nil {
			defaults.Fields = map[string]*structpb.Value{}
		}
		defaults.Fields["projectId"] = structpb.NewStringValue(projectID)
		profile.ToolDefaults[name] = defaults
	}
	return profile, nil
}
