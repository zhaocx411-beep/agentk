package videogen

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// 可灵 Omni 视频生成端点：POST /omni-video/{model}（对齐官方「Omni 视频生成」文档）。
// 与图生视频同基址同鉴权同轮询（见 kling.go 的 klingTransport），区别全在素材：
// contents 除首尾帧外还收元素参考图（refer_image）、特征参考视频（feature_video）
// 与待编辑视频（base_video），prompt 用 @id 指名道姓地引用它们。

const klingOmniRoute = "omni-video"

// Omni 的素材数量上限（官方文档，按有无参考视频分档；本客户端不支持主体库，
// 故上限就是参考图自己的额度）。
const (
	klingOmniMaxReferImages          = 7
	klingOmniMaxReferImagesWithVideo = 4
)

// KlingOmniRequest 是可灵 Omni 接口的入参。素材各按角色分字段给 URL，
// 客户端负责编号并装进 contents——prompt 里按 @image_1 / @video_1 引用。
type KlingOmniRequest struct {
	// Prompt 文本提示词，必填；可用 @image_1 / @video_1 指定素材
	Prompt string `json:"prompt"`
	// FirstFrame 首帧图，可选；给了它输出画幅就跟随该图
	FirstFrame string `json:"firstFrame,omitempty"`
	// LastFrame 尾帧图，可选；必须与首帧同时给（不支持仅尾帧）
	LastFrame string `json:"lastFrame,omitempty"`
	// ReferImages 元素参考图（形象/风格/构图），依次编号 image_1..image_N
	ReferImages []string `json:"referImages,omitempty"`
	// FeatureVideo 特征参考视频（运镜/动作/风格参考），编号 video_1；与 BaseVideo 互斥
	FeatureVideo string `json:"featureVideo,omitempty"`
	// BaseVideo 待编辑视频（在原视频上改），编号 video_1；与 FeatureVideo 互斥
	BaseVideo string `json:"baseVideo,omitempty"`
	// Resolution 720p / 1080p / 4k；空取 1080p
	Resolution string `json:"resolution,omitempty"`
	// Duration 秒数 "3"~"15"；空取 "5"
	Duration string `json:"duration,omitempty"`
	// AspectRatio 16:9 / 9:16 / 1:1；无首帧且无参考视频时必须有值（空则取 16:9），
	// 其余情况留空表示跟随素材
	AspectRatio string `json:"aspectRatio,omitempty"`
	// Audio native（画面适配声音）/ original（保留参考视频原声）/ off。
	// 空值通常取 native；编辑已有视频时取 original，特征参考视频模式受上游限制取 off。
	Audio string `json:"audio,omitempty"`
	// MultiShot 是否生成多镜头视频；空取 true
	MultiShot *bool `json:"multiShot,omitempty"`
}

// Normalize 校验入参、落实缺省并把文档里的素材组合约束兑现成明确的错误，
// 幂等。越界组合在提交前就拒绝——一次生成是要计费的，不拿它去试上游。
func (r KlingOmniRequest) Normalize() (KlingOmniRequest, error) {
	if strings.TrimSpace(r.Prompt) == "" {
		return r, fmt.Errorf("videogen: prompt is required")
	}
	r.FirstFrame = strings.TrimSpace(r.FirstFrame)
	r.LastFrame = strings.TrimSpace(r.LastFrame)
	r.FeatureVideo = strings.TrimSpace(r.FeatureVideo)
	r.BaseVideo = strings.TrimSpace(r.BaseVideo)
	if r.LastFrame != "" && r.FirstFrame == "" {
		return r, fmt.Errorf("videogen: kling omni supports first frame or first+last frame, not last frame alone")
	}
	if r.FeatureVideo != "" && r.BaseVideo != "" {
		return r, fmt.Errorf("videogen: kling omni accepts at most one reference video (featureVideo or baseVideo)")
	}
	hasVideo := r.FeatureVideo != "" || r.BaseVideo != ""
	// 待编辑视频模式：画面来自原视频，首尾帧无处安放。
	if r.BaseVideo != "" && (r.FirstFrame != "" || r.LastFrame != "") {
		return r, fmt.Errorf("videogen: kling omni baseVideo cannot be combined with first/last frame")
	}

	limit := klingOmniMaxReferImages
	if hasVideo {
		limit = klingOmniMaxReferImagesWithVideo
	}
	referImages, err := trimmedList("kling omni", "referImages", r.ReferImages, limit)
	if err != nil {
		return r, err
	}
	r.ReferImages = referImages

	seconds, err := parseDuration("kling omni", r.Duration, klingMinDuration, klingMaxDuration, klingDefaultDuration)
	if err != nil {
		return r, err
	}
	r.Duration = strconv.Itoa(seconds)
	if r.Resolution, err = pickEnum("kling omni", "resolution", r.Resolution,
		klingDefaultResolution, "720p", "1080p", "4k"); err != nil {
		return r, err
	}
	defaultAudio := klingDefaultAudio
	if r.BaseVideo != "" {
		defaultAudio = "original"
	} else if r.FeatureVideo != "" {
		defaultAudio = "off"
	}
	if r.Audio, err = pickEnum("kling omni", "audio mode", r.Audio,
		defaultAudio, "native", "original", "off"); err != nil {
		return r, err
	}
	// original 是"保留参考视频原声"，没有参考视频就无声可留。
	if r.Audio == "original" && !hasVideo {
		return r, fmt.Errorf("videogen: kling omni audio %q requires a reference video", r.Audio)
	}
	if r.FeatureVideo != "" && r.Audio != "off" {
		return r, fmt.Errorf("videogen: kling omni featureVideo does not support audio output, audio must be off")
	}
	if r.BaseVideo != "" && r.Audio == "native" {
		return r, fmt.Errorf("videogen: kling omni baseVideo does not support audio %q", r.Audio)
	}
	if r.MultiShot == nil {
		multiShot := true
		r.MultiShot = &multiShot
	}
	if r.BaseVideo != "" && *r.MultiShot {
		return r, fmt.Errorf("videogen: kling omni baseVideo does not support multi-shot output")
	}
	// 既没有首帧也没有参考视频时，画幅无从推断，文档要求显式给出。
	if r.AspectRatio == "" && r.FirstFrame == "" && !hasVideo {
		r.AspectRatio = "16:9"
	}
	if r.AspectRatio != "" {
		if r.AspectRatio, err = pickEnum("kling omni", "aspect ratio", r.AspectRatio,
			"", "16:9", "9:16", "1:1"); err != nil {
			return r, err
		}
	}
	return r, nil
}

// Params 归一后的实际生效参数。
func (r KlingOmniRequest) Params() map[string]any {
	params := map[string]any{
		"resolution": r.Resolution,
		"duration":   r.Duration,
		"audio":      r.Audio,
		"multiShot":  r.MultiShot != nil && *r.MultiShot,
	}
	if r.AspectRatio != "" {
		params["aspectRatio"] = r.AspectRatio
	}
	return params
}

// KlingOmni 是可灵 Omni 端点的客户端。
type KlingOmni struct{ *klingTransport }

func NewKlingOmni(cfg KlingConfig) (*KlingOmni, error) {
	transport, err := newKlingTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &KlingOmni{klingTransport: transport}, nil
}

func (k *KlingOmni) Submit(ctx context.Context, req KlingOmniRequest) (string, error) {
	req, err := req.Normalize()
	if err != nil {
		return "", err
	}
	contents := []map[string]any{{"type": "prompt", "text": req.Prompt}}
	// 图片素材内联规则与图生视频一致：私有产物转 base64，其余原样透传。
	appendImage := func(kind, image, id string) error {
		inlined, err := k.inlineImage(ctx, image)
		if err != nil {
			return err
		}
		item := map[string]any{"type": kind, "url": inlined}
		if id != "" {
			item["id"] = id
		}
		contents = append(contents, item)
		return nil
	}
	if req.FirstFrame != "" {
		if err := appendImage("first_frame", req.FirstFrame, ""); err != nil {
			return "", err
		}
	}
	if req.LastFrame != "" {
		if err := appendImage("last_frame", req.LastFrame, ""); err != nil {
			return "", err
		}
	}
	// 参考图按给定顺序编号 image_1..image_N：这正是文档里 prompt 用 @image_n
	// 引用素材的写法，编号规则由这里定死，模型只管按顺序给 URL。
	for i, image := range req.ReferImages {
		if err := appendImage("refer_image", image, fmt.Sprintf("image_%d", i+1)); err != nil {
			return "", err
		}
	}
	// 视频素材只收 URL（文档不支持 base64），故不走内联。
	if req.FeatureVideo != "" {
		contents = append(contents, map[string]any{
			"type": "feature_video", "url": req.FeatureVideo, "id": "video_1",
		})
	}
	if req.BaseVideo != "" {
		contents = append(contents, map[string]any{
			"type": "base_video", "url": req.BaseVideo, "id": "video_1",
		})
	}

	duration, _ := strconv.Atoi(req.Duration)
	settings := map[string]any{
		"resolution": req.Resolution,
		"duration":   duration,
		"audio":      req.Audio,
		"multi_shot": *req.MultiShot,
	}
	// 画幅留空表示跟随首帧/参考视频，不下发让上游自己判断。
	if req.AspectRatio != "" {
		settings["aspect_ratio"] = req.AspectRatio
	}
	return k.submit(ctx, klingOmniRoute, map[string]any{
		"contents": contents,
		"settings": settings,
	})
}

// Basic 返回本端点的公共最小接口（见 KlingI2V.Basic）。
func (k *KlingOmni) Basic() Client { return klingOmniBasic{k} }

// klingOmniBasic 把 Omni 端点适配成公共最小接口。
type klingOmniBasic struct{ *KlingOmni }

func (c klingOmniBasic) Submit(ctx context.Context, req Request) (string, error) {
	return c.KlingOmni.Submit(ctx, KlingOmniRequest{
		Prompt:     req.Prompt,
		FirstFrame: req.FirstFrame,
		LastFrame:  req.LastFrame,
		Resolution: req.Resolution,
		Duration:   req.Duration,
		Audio:      req.Audio,
	})
}
