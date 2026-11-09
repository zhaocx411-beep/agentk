package media

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// WaveformBucketCount 是振幅包络的固定桶数，均分整段素材时长。
const WaveformBucketCount = 200

// waveformSampleRate 是解码目标采样率（Hz）；波形只用于展示，8kHz 单声道
// 足够勾出振幅轮廓，且大幅降低解码耗时与内存占用。
const waveformSampleRate = 8000

// WaveformExtractor 提取素材的振幅包络。实现必须是纯本地进程调用，不经过
// agent-k/Temporal。
type WaveformExtractor interface {
	Extract(ctx context.Context, inputPath string) ([]float32, error)
}

// FFWaveformExtractor 用本地 ffmpeg 把音频解码成单声道 PCM，再在内存里算出
// 振幅包络。
type FFWaveformExtractor struct {
	Binary  string
	Timeout time.Duration
}

// Extract 返回长度为 WaveformBucketCount 的振幅包络：每桶取绝对值峰值，
// 归一化到 0~1。inputPath 必须包含至少一条音频流。
func (e FFWaveformExtractor) Extract(ctx context.Context, inputPath string) ([]float32, error) {
	if err := validateSourceFile(inputPath); err != nil {
		return nil, err
	}
	bin := strings.TrimSpace(e.Binary)
	if bin == "" {
		bin = "ffmpeg"
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, err := acquireFFmpeg(ctx)
	if err != nil {
		return nil, fmt.Errorf("wait for ffmpeg slot: %w", err)
	}
	defer release()

	pcmFile, err := os.CreateTemp("", "agentcut-waveform-*.pcm")
	if err != nil {
		return nil, fmt.Errorf("create temp pcm file: %w", err)
	}
	pcmPath := pcmFile.Name()
	_ = pcmFile.Close()
	defer os.Remove(pcmPath)

	cmd := exec.CommandContext(ctx, bin,
		"-y", "-i", inputPath,
		"-t", fmt.Sprintf("%d", maxMediaDurationSec),
		"-vn", "-ac", "1", "-ar", fmt.Sprintf("%d", waveformSampleRate), "-f", "s16le",
		"-fs", fmt.Sprintf("%d", maxWaveformPCMBytes+1),
		pcmPath,
	)
	var commandOutput cappedBuffer
	commandOutput.max = maxCommandOutputBytes
	cmd.Stdout = &commandOutput
	cmd.Stderr = &commandOutput
	runErr := cmd.Run()
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("ffmpeg waveform decode timed out after %s", timeout)
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return nil, fmt.Errorf("ffmpeg not found in PATH")
		}
		return nil, fmt.Errorf("ffmpeg waveform decode failed: %w: %s", runErr, commandOutput.String())
	}

	info, err := os.Stat(pcmPath)
	if err != nil {
		return nil, fmt.Errorf("stat decoded pcm: %w", err)
	}
	if info.Size() > maxWaveformPCMBytes {
		return nil, fmt.Errorf("decoded PCM exceeds %d bytes", maxWaveformPCMBytes)
	}
	sampleCount := int(info.Size() / 2)
	if sampleCount == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty pcm output (no audio stream?)")
	}
	pcm, err := os.Open(pcmPath)
	if err != nil {
		return nil, fmt.Errorf("open decoded pcm: %w", err)
	}
	defer pcm.Close()
	reader := bufio.NewReaderSize(pcm, 64<<10)

	peaks := make([]float32, WaveformBucketCount)
	bucketSize := sampleCount / WaveformBucketCount
	if bucketSize < 1 {
		bucketSize = 1
	}
	for b := 0; b < WaveformBucketCount; b++ {
		if b*bucketSize >= sampleCount {
			break
		}
		var peak int32
		var raw [2]byte
		for i := 0; i < bucketSize; i++ {
			if _, err := io.ReadFull(reader, raw[:]); err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					break
				}
				return nil, fmt.Errorf("read decoded pcm: %w", err)
			}
			sample := int32(int16(binary.LittleEndian.Uint16(raw[:])))
			if sample < 0 {
				sample = -sample
			}
			if sample > peak {
				peak = sample
			}
		}
		peaks[b] = float32(peak) / 32768
	}
	return peaks, nil
}
