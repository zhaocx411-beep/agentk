// Package videocompose 提供进程内 ffmpeg 视频拼接能力（concat_videos）。
//
// 输入和输出均为 URL：输入片段下载到执行期临时目录，ffmpeg 统一转码后按序
// 拼接，成片上传注入的产物存储后返回 URL，临时目录即删。本地文件只是执行期
// scratch，不承载任何跨请求状态，多实例部署安全。产物存储由应用启动期统一装配。
//
// ffmpeg 二进制来自 PATH：Docker 镜像内置 BtbN 静态构建（见 Dockerfile），
// 本地开发用系统安装（brew install ffmpeg）。代码不感知来源，缺失时执行报错。
//
// 当前拼接在进程内执行。如果调用量导致 CPU 争用或长时间占用执行槽，可以把同一段
// 逻辑迁入独立服务并通过异步 job 工具协议接入，workflow 定义无需修改。
package videocompose

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	outputSizeAuto  = "auto"
	outputSize1080p = "1080p"
)

// concat 下载全部片段到临时目录，ffmpeg 归一转码拼接，返回成片字节。
func concat(ctx context.Context, urls []string, size string) ([]byte, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH (镜像内置 BtbN 静态版，本地开发 brew install ffmpeg): %w", err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found in PATH (must be installed with ffmpeg): %w", err)
	}
	dir, err := os.MkdirTemp("", "agentk-concat-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	clips := make([]clipInfo, 0, len(urls))
	for i, u := range urls {
		p := filepath.Join(dir, fmt.Sprintf("in%d.mp4", i))
		if err := download(ctx, u, p); err != nil {
			return nil, fmt.Errorf("download clip %d: %w", i, err)
		}
		info, err := probeClip(ctx, ffprobe, p)
		if err != nil {
			return nil, fmt.Errorf("probe clip %d: %w", i, err)
		}
		clips = append(clips, info)
		args = append(args, "-i", p)
	}

	targetWidth, targetHeight := outputDimensions(clips[0], size)

	// 归一每路视频与音频流后 concat。视频按目标画幅放大后居中裁切，避免边框留白。
	// 缺音轨的片段补静音；已有音轨 pad/trim 到
	// 对应视频时长，避免音画长度不一致把后续片段的声音推偏。
	var fc strings.Builder
	for i, clip := range clips {
		fmt.Fprintf(&fc, "[%d:v]scale=%d:%d:force_original_aspect_ratio=increase,"+
			"crop=%d:%d,setsar=1,fps=30,setpts=PTS-STARTPTS[v%d];",
			i, targetWidth, targetHeight, targetWidth, targetHeight, i)
		if clip.hasAudio {
			fmt.Fprintf(&fc, "[%d:a:0]aresample=48000,"+
				"aformat=sample_fmts=fltp:channel_layouts=stereo,apad,"+
				"atrim=duration=%.6f,asetpts=PTS-STARTPTS[a%d];", i, clip.duration, i)
		} else {
			fmt.Fprintf(&fc, "anullsrc=channel_layout=stereo:sample_rate=48000,"+
				"atrim=duration=%.6f,asetpts=PTS-STARTPTS[a%d];", clip.duration, i)
		}
	}
	for i := range urls {
		fmt.Fprintf(&fc, "[v%d][a%d]", i, i)
	}
	fmt.Fprintf(&fc, "concat=n=%d:v=1:a=1[v][a]", len(urls))

	outPath := filepath.Join(dir, "out.mp4")
	args = append(args,
		"-filter_complex", fc.String(),
		"-map", "[v]",
		"-map", "[a]",
		"-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		outPath,
	)
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: %s", err, tail(stderr.String(), 800))
	}
	return os.ReadFile(outPath)
}

type clipInfo struct {
	duration float64
	hasAudio bool
	width    int
	height   int
	rotation int
}

func probeClip(ctx context.Context, ffprobe, path string) (clipInfo, error) {
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-show_entries", "stream=codec_type,width,height:stream_tags=rotate:stream_side_data=rotation:format=duration",
		"-of", "json",
		path,
	)
	raw, err := cmd.Output()
	if err != nil {
		return clipInfo{}, err
	}
	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			Tags      struct {
				Rotate string `json:"rotate"`
			} `json:"tags"`
			SideDataList []struct {
				Rotation *int `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration floatString `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return clipInfo{}, fmt.Errorf("decode ffprobe output: %w", err)
	}
	duration := float64(probe.Format.Duration)
	if duration <= 0 {
		return clipInfo{}, fmt.Errorf("invalid duration %.6f", duration)
	}
	info := clipInfo{duration: duration}
	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			if info.width == 0 {
				info.width, info.height = stream.Width, stream.Height
				for _, sideData := range stream.SideDataList {
					if sideData.Rotation != nil {
						info.rotation = *sideData.Rotation
						break
					}
				}
				if info.rotation == 0 {
					if rotation, err := strconv.Atoi(stream.Tags.Rotate); err == nil {
						info.rotation = rotation
					}
				}
			}
		case "audio":
			info.hasAudio = true
		}
	}
	if info.width <= 0 || info.height <= 0 {
		return clipInfo{}, fmt.Errorf("missing video dimensions")
	}
	return info, nil
}

func outputDimensions(first clipInfo, size string) (width, height int) {
	width, height = displayDimensions(first)
	if size == outputSizeAuto {
		return evenDimension(width), evenDimension(height)
	}
	if width >= height {
		return evenDimension(int(math.Round(1080 * float64(width) / float64(height)))), 1080
	}
	return 1080, evenDimension(int(math.Round(1080 * float64(height) / float64(width))))
}

func displayDimensions(clip clipInfo) (width, height int) {
	rotation := ((clip.rotation % 360) + 360) % 360
	if rotation == 90 || rotation == 270 {
		return clip.height, clip.width
	}
	return clip.width, clip.height
}

func evenDimension(value int) int {
	if value < 2 {
		return 2
	}
	return value - value%2
}

// ffprobe serializes format.duration as a JSON string.
type floatString float64

func (f *floatString) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	var value float64
	if _, err := fmt.Sscan(text, &value); err != nil {
		return err
	}
	*f = floatString(value)
	return nil
}

// download 拉取一个片段到本地路径。相对 URL 不可用——工具与片段来源可能不同机。
func download(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
