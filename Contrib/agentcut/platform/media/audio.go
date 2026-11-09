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

type AudioExtractor interface {
	ExtractForASR(ctx context.Context, inputPath, outputPath string) error
}

type AudioPreviewer interface {
	TranscodeForPreview(ctx context.Context, inputPath, outputPath string) error
}

type EditingAudioExtractor interface {
	ExtractForEditing(ctx context.Context, inputPath, outputPath string) error
}

// FFAudioExtractor produces a compact mono MP3 accepted by transcribe_audio.
type FFAudioExtractor struct {
	Binary  string
	Timeout time.Duration
}

func (e FFAudioExtractor) ExtractForASR(ctx context.Context, inputPath, outputPath string) error {
	if err := validateSourceFile(inputPath); err != nil {
		return err
	}
	binary := strings.TrimSpace(e.Binary)
	if binary == "" {
		binary = "ffmpeg"
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, err := acquireFFmpeg(ctx)
	if err != nil {
		return fmt.Errorf("wait for ffmpeg slot: %w", err)
	}
	defer release()
	cmd := exec.CommandContext(ctx, binary,
		"-y", "-i", inputPath,
		"-t", fmt.Sprintf("%d", maxMediaDurationSec),
		"-vn", "-ac", "1", "-ar", "16000", "-b:a", "32k",
		"-fs", fmt.Sprintf("%d", maxASROutputBytes+1),
		"-f", "mp3", outputPath,
	)
	var commandOutput cappedBuffer
	commandOutput.max = maxCommandOutputBytes
	cmd.Stdout = &commandOutput
	cmd.Stderr = &commandOutput
	err = cmd.Run()
	if err == nil {
		info, statErr := os.Stat(outputPath)
		if statErr != nil {
			return fmt.Errorf("stat ASR audio output: %w", statErr)
		}
		if info.Size() <= 0 || info.Size() > maxASROutputBytes {
			return fmt.Errorf("ASR audio output size %d is outside (0, 25 MiB]", info.Size())
		}
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("ffmpeg ASR audio extraction timed out after %s", timeout)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("ffmpeg not found in PATH")
	}
	return fmt.Errorf("ffmpeg ASR audio extraction failed: %w: %s", err, commandOutput.String())
}

// FFAudioPreviewer normalizes arbitrary uploaded audio (for example
// pcm_f64le WAV, which Chromium cannot decode) to browser-compatible stereo
// MP3. Rendering still reads the original source; this derivative is preview-only.
type FFAudioPreviewer struct {
	Binary  string
	Timeout time.Duration
}

func (p FFAudioPreviewer) TranscodeForPreview(
	ctx context.Context,
	inputPath string,
	outputPath string,
) error {
	if err := validateSourceFile(inputPath); err != nil {
		return err
	}
	err := RunFFmpeg(ctx, p.Binary, p.Timeout,
		"-y", "-i", inputPath,
		"-t", fmt.Sprintf("%d", maxMediaDurationSec),
		"-vn", "-ac", "2", "-ar", "48000", "-b:a", "192k",
		"-map_metadata", "-1", "-f", "mp3", outputPath,
	)
	if err != nil {
		return fmt.Errorf("transcode browser audio preview: %w", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return fmt.Errorf("stat browser audio preview: %w", err)
	}
	if info.Size() <= 0 || info.Size() > maxFFmpegSourceBytes {
		return fmt.Errorf("browser audio preview size %d is outside (0, 2 GiB]", info.Size())
	}
	return nil
}

// ExtractForEditing creates a standalone, browser- and ffmpeg-compatible audio
// source. Unlike the bounded mono ASR derivative, this preserves stereo at
// 48 kHz and is suitable as an independent Timeline media source.
func (p FFAudioPreviewer) ExtractForEditing(
	ctx context.Context,
	inputPath string,
	outputPath string,
) error {
	return p.TranscodeForPreview(ctx, inputPath, outputPath)
}
