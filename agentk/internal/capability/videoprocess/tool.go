// Package videoprocess provides constrained single-video transformations.
package videoprocess

import (
	"encoding/json"
	"time"

	"agentk/internal/tool"
)

const (
	ToolName     = "process_video"
	ActivityName = "agentk.capability.process_video"
)

func BuildTool() tool.Spec {
	return tool.Spec{
		Name: ToolName,
		Description: "处理一个已有视频并返回新的 MP4/H.264 视频；有声音时统一编码为 AAC。" +
			"支持裁剪、规格归一化、画幅重构、缩放、0.5～2 倍变速和静音，operations 按顺序执行。" +
			"仅用于单视频变换；多视频拼接或混入其他音频请用 compose_video，不接受原始 FFmpeg 参数。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"required":["videoUrl","operations"],
			"properties":{
				"videoUrl":{"type":"string","description":"系统 artifact URL 或执行机器可下载的公网 HTTP/HTTPS 视频 URL"},
				"operations":{
					"type":"array","minItems":1,"maxItems":8,
					"description":"按数组顺序执行的处理操作",
					"items":{
						"oneOf":[
							{
								"type":"object","required":["type","startSeconds","durationSeconds"],
								"properties":{
									"type":{"type":"string","enum":["trim"],"description":"按时间范围精确裁剪"},
									"startSeconds":{"type":"number","minimum":0,"description":"起始秒数"},
									"durationSeconds":{"type":"number","exclusiveMinimum":0,"description":"保留时长，单位秒"}
								}
							},
							{
								"type":"object","required":["type"],
								"properties":{"type":{"type":"string","enum":["normalize"],"description":"统一为 30fps、方形像素、48kHz 立体声"}}
							},
							{
								"type":"object","required":["type","width","height"],
								"properties":{
									"type":{"type":"string","enum":["reframe","resize"],"description":"reframe 默认保持比例并铺满；resize 默认拉伸"},
									"width":{"type":"integer","minimum":2,"maximum":3840,"description":"目标宽度，会向下取偶数"},
									"height":{"type":"integer","minimum":2,"maximum":3840,"description":"目标高度，会向下取偶数"},
									"fit":{"type":"string","enum":["cover","contain","stretch"],"description":"cover 裁切铺满；contain 黑边完整展示；stretch 拉伸"}
								}
							},
							{
								"type":"object","required":["type","factor"],
								"properties":{
									"type":{"type":"string","enum":["speed"],"description":"同步改变视频和音频速度"},
									"factor":{"type":"number","minimum":0.5,"maximum":2,"description":"速度倍数"}
								}
							},
							{
								"type":"object","required":["type"],
								"properties":{"type":{"type":"string","enum":["mute"],"description":"移除全部音轨"}}
							}
						]
					}
				},
				"quality":{"type":"string","enum":["high","balanced","small"],"default":"balanced","description":"输出质量与文件大小档位"}
			}
		}`),
		Binding: tool.ActivityBinding{
			ActivityName: ActivityName, StartToCloseTimeout: 11 * time.Minute, MaxAttempts: 2,
		},
	}
}
