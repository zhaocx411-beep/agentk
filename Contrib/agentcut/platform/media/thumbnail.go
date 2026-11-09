package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ThumbnailFrameCount 是视频缩略图 sprite 里横向平铈的帧数。
const ThumbnailFrameCount = 10

// thumbnailWidth 是每帧缩放后的宽度（像素），高度按源画面比例自适应。
const thumbnailWidth = 160

// Thumbnailer 生成素材缩略图。实现必须是纯本地进程调用，不经过
// agent-k/Temporal；生成失败应由调用方按 best-effort 处理（记录 warning，
// 不阻断上传）。
type Thumbnailer interface {
	GenerateVideoSprite(ctx context.Context, inputPath string, durationSec float64) ([]byte, error)
	GenerateImageThumbnail(ctx context.Context, inputPath string) ([]byte, error)
}

// FFThumbnailer 是基于本地 ffmpeg 二进制的 Thumbnailer 实现。
type FFThumbnailer struct {
	Binary  string
	Timeout time.Duration
}

// GenerateVideoSprite 从视频里等间隔抽取 ThumbnailFrameCount 帧，横向拼接成
// 一张 JPEG sprite（单个二进制对象，减少 Blob 对象数量）。durationSec 必须 > 0。
func (t FFThumbnailer) GenerateVideoSprite(ctx context.Context, inputPath string, durationSec float64) ([]byte, error) {
	if err := validateMediaDuration(durationSec); err != nil {
		return nil, err
	}
	interval := durationSec / float64(ThumbnailFrameCount)
	if interval < 0.1 {
		interval = 0.1
	}
	filter := fmt.Sprintf("fps=1/%f,scale=%d:-1,tile=%dx1", interval, thumbnailWidth, ThumbnailFrameCount)
	return t.run(ctx, inputPath, filter)
}

// GenerateImageThumbnail 生成单帧缩放缩略图，用于 image kind 素材。
func (t FFThumbnailer) GenerateImageThumbnail(ctx context.Context, inputPath string) ([]byte, error) {
	filter := fmt.Sprintf("scale=%d:-1", thumbnailWidth)
	return t.run(ctx, inputPath, filter)
}

func (t FFThumbnailer) run(ctx context.Context, inputPath, filter string) ([]byte, error) {
	if err := validateSourceFile(inputPath); err != nil {
		return nil, err
	}
	binary := strings.TrimSpace(t.Binary)
	if binary == "" {
		binary = "ffmpeg"
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, err := acquireFFmpeg(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for ffmpeg slot: %w", err)
	}
	defer release()

	out, err := os.CreateTemp("", "agentcut-thumb-*.jpg")
	if err != nil {
		return nil, fmt.Errorf("create temp thumbnail file: %w", err)
	}
	outPath := out.Name()
	_ = out.Close()
	defer os.Remove(outPath)

	cmd := exec.CommandContext(ctx, binary,
		"-y", "-i", inputPath,
		"-vf", filter,
		"-frames:v", "1",
		"-update", "1",
		"-q:v", "4",
		"-fs", fmt.Sprintf("%d", maxThumbnailBytes+1),
		outPath,
	)
	var commandOutput cappedBuffer
	commandOutput.max = maxCommandOutputBytes
	cmd.Stdout = &commandOutput
	cmd.Stderr = &commandOutput
	runErr := cmd.Run()
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("ffmpeg thumbnail timed out after %s", timeout)
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return nil, fmt.Errorf("ffmpeg not found in PATH")
		}
		return nil, fmt.Errorf("ffmpeg thumbnail failed: %w: %s", runErr, commandOutput.String())
	}
	data, err := readFileLimited(outPath, maxThumbnailBytes)
	if err != nil {
		return nil, fmt.Errorf("read generated thumbnail: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty thumbnail")
	}
	return data, nil
}
