// Package videoprobe provides technical video metadata inspection via ffprobe.
package videoprobe

import (
	"encoding/json"
	"time"

	"agentk/internal/tool"
)

const (
	// ToolName is the public agent tool name.
	ToolName = "probe_video"

	toolTimeout          = 5 * time.Minute
	activityStartToClose = toolTimeout + time.Minute
)

// BuildTool declares the video metadata tool. It returns metadata only and does
// not create a new artifact.
func BuildTool() tool.Spec {
	return tool.Spec{
		Name: ToolName,
		Description: "获取视频文件的完整技术信息。输入一个可下载的 HTTP/HTTPS 视频 URL，" +
			"返回文件大小、时长、容器、总码率，以及每路视频、音频、字幕和其他流的编码、" +
			"分辨率、帧率、声道等常用字段；同时返回完整 ffprobe 原始信息，适合继续查询未结构化的细节。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"videoUrl": {
					"type": "string",
					"description": "要检查的视频 HTTP/HTTPS URL"
				}
			},
			"required": ["videoUrl"]
		}`),
		Binding: tool.ActivityBinding{
			ActivityName:        ActivityName,
			StartToCloseTimeout: activityStartToClose,
		},
	}
}
