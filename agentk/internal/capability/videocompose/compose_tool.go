package videocompose

import (
	"encoding/json"
	"time"

	"agentk/internal/tool"
)

const (
	ComposeToolName     = "compose_video"
	ComposeActivityName = "agentk.capability.compose_video"
)

// BuildComposeTool declares the richer multi-asset composition tool.
func BuildComposeTool() tool.Spec {
	return tool.Spec{
		Name: ComposeToolName,
		Description: "把 1～20 个视频按 videoUrls 顺序合成为一个 MP4/H.264+AAC 成片，" +
			"可叠加最多 8 条配音/背景音频，为每条设置时间偏移、裁剪、音量和循环；也可烧录一份 SRT/ASS 字幕。" +
			"视频会统一画幅、30fps 和音频规格。当前不支持图片片段、转场或 Overlay；不接受原始 FFmpeg 参数。",
		InputSchema: json.RawMessage(`{
			"type":"object","required":["videoUrls"],
			"properties":{
				"videoUrls":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"string"},"description":"按成片顺序排列的可下载视频 URL；系统 artifact URL 可直接使用"},
				"size":{"type":"string","enum":["auto","1080p"],"default":"auto","description":"auto 跟随首段显示尺寸；1080p 保持首段比例归一到 1080 档"},
				"audioTracks":{
					"type":"array","maxItems":8,
					"description":"要叠加到视频时间线的配音或背景音轨",
					"items":{
						"type":"object","required":["url"],
						"properties":{
							"url":{"type":"string","description":"系统 audio artifact URL 或公网 HTTP/HTTPS 音频 URL"},
							"startSeconds":{"type":"number","minimum":0,"description":"该音轨在成片时间线上的开始时间"},
							"trimStartSeconds":{"type":"number","minimum":0,"description":"从源音频第几秒开始取"},
							"durationSeconds":{"type":"number","exclusiveMinimum":0,"description":"可选保留时长；缺省使用剩余音频"},
							"volume":{"type":"number","minimum":0,"maximum":2,"default":1,"description":"该音轨音量，0 为静音"},
							"loop":{"type":"boolean","default":false,"description":"源音频不足时是否循环到成片结束"}
						}
					}
				},
				"preserveOriginalAudio":{"type":"boolean","default":true,"description":"是否保留拼接视频原声；false 时只听 audioTracks"},
				"subtitleUrl":{"type":"string","description":"可选 SRT/ASS 字幕 artifact URL 或公网 HTTP/HTTPS URL；字幕会永久烧录进画面"},
				"subtitleFormat":{"type":"string","enum":["srt","ass"],"default":"srt","description":"subtitleUrl 的文件格式"},
				"subtitleSegments":{
					"type":"array","maxItems":500,
					"description":"直接烧录的字幕时间轴，通常原样映射 transcribe_audio 返回的 segments；不要与 subtitleUrl 同时使用",
					"items":{
						"type":"object","required":["startSeconds","endSeconds","text"],
						"properties":{
							"startSeconds":{"type":"number","minimum":0},
							"endSeconds":{"type":"number","exclusiveMinimum":0},
							"text":{"type":"string","minLength":1}
						}
					}
				}
			}
		}`),
		Binding: tool.ActivityBinding{
			ActivityName: ComposeActivityName, StartToCloseTimeout: 16 * time.Minute, MaxAttempts: 2,
		},
	}
}
