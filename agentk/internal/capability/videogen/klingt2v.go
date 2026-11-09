package videogen

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// 可灵文生视频端点：POST /text-to-video/{model}（对齐官方「文生视频」文档）。
// 与另两个可灵端点同基址同鉴权同轮询（见 kling.go 的 klingTransport），
// 区别在于请求体没有 contents——一个素材都不收，prompt 直接放顶层，
// 画面从零生成，故 settings 里多一个 aspect_ratio（没有首帧可跟随）。

const klingT2VRoute = "text-to-video"

// KlingT2VRequest 是可灵文生视频接口的入参：只有提示词与输出配置。
// 没有任何素材字段——要用图或视频当参考就该去另两个端点，在这里给了也无处安放。
type KlingT2VRequest struct {
	// Prompt 文本提示词，必填
	Prompt string `json:"prompt"`
	// Resolution 720p / 1080p / 4k；空取 1080p
	Resolution string `json:"resolution,omitempty"`
	// Duration 秒数 "3"~"15"；空取 "5"
	Duration string `json:"duration,omitempty"`
	// AspectRatio 16:9 / 9:16 / 1:1；空取 16:9。文生视频没有素材可跟随，
	// 画幅始终由这里决定，故与 Omni 不同——它不留"跟随素材"的空值语义。
	AspectRatio string `json:"aspectRatio,omitempty"`
	// Audio native（画面适配声音）/ off；空取 native
	Audio string `json:"audio,omitempty"`
	// MultiShot 是否生成多镜头视频；空取 true
	MultiShot *bool `json:"multiShot,omitempty"`
}

// Normalize 校验入参并落实缺省，幂等：activity 归一一次用于回执，客户端再归一
// 一次以便被直调时同样安全。
func (r KlingT2VRequest) Normalize() (KlingT2VRequest, error) {
	if strings.TrimSpace(r.Prompt) == "" {
		return r, fmt.Errorf("videogen: prompt is required")
	}
	seconds, err := parseDuration("kling text-to-video", r.Duration,
		klingMinDuration, klingMaxDuration, klingDefaultDuration)
	if err != nil {
		return r, err
	}
	r.Duration = strconv.Itoa(seconds)
	if r.Resolution, err = pickEnum("kling text-to-video", "resolution", r.Resolution,
		klingDefaultResolution, "720p", "1080p", "4k"); err != nil {
		return r, err
	}
	if r.AspectRatio, err = pickEnum("kling text-to-video", "aspect ratio", r.AspectRatio,
		klingDefaultAspectRatio, "16:9", "9:16", "1:1"); err != nil {
		return r, err
	}
	if r.Audio, err = pickEnum("kling text-to-video", "audio mode", r.Audio,
		klingDefaultAudio, "native", "off"); err != nil {
		return r, err
	}
	if r.MultiShot == nil {
		multiShot := true
		r.MultiShot = &multiShot
	}
	return r, nil
}

// Params 归一后的实际生效参数，随提交回执与产物事实一起落账。
func (r KlingT2VRequest) Params() map[string]any {
	return map[string]any{
		"resolution":  r.Resolution,
		"duration":    r.Duration,
		"aspectRatio": r.AspectRatio,
		"audio":       r.Audio,
		"multiShot":   r.MultiShot != nil && *r.MultiShot,
	}
}

// KlingT2V 是可灵文生视频端点的客户端。
//
// 它没有 Basic()：公共最小接口以"首帧图 + 描述"为契约，而本端点不接收任何素材，
// 强行适配只能把首帧丢掉——那是在悄悄换掉调用方要的东西。pipeline 想要纯文生
// 视频，该显式选这个接口。
type KlingT2V struct{ *klingTransport }

func NewKlingT2V(cfg KlingConfig) (*KlingT2V, error) {
	transport, err := newKlingTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &KlingT2V{klingTransport: transport}, nil
}

func (k *KlingT2V) Submit(ctx context.Context, req KlingT2VRequest) (string, error) {
	req, err := req.Normalize()
	if err != nil {
		return "", err
	}
	duration, _ := strconv.Atoi(req.Duration)
	// 没有 contents：prompt 是顶层字段，这正是本端点与另两个可灵端点的形状差异。
	return k.submit(ctx, klingT2VRoute, map[string]any{
		"prompt": req.Prompt,
		"settings": map[string]any{
			"resolution":   req.Resolution,
			"duration":     duration,
			"aspect_ratio": req.AspectRatio,
			"audio":        req.Audio,
			"multi_shot":   *req.MultiShot,
		},
	})
}
