// Package blob 是 AgentCut 的 BlobStore 客户端：媒体字节与 Timeline 只读
// 快照的唯一二进制存储。匿名 PUT 范式与 comic-drama 一致（服务端持有
// endpoint/bucket，浏览器不接触存储凭证）。
package blob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"

	"agentcut/entity"
	"agentcut/timeline"
)

// Object 是 Storage.Get 返回的对象流及元数据。调用方负责关闭 Body。
type Object struct {
	Body         io.ReadCloser
	ContentType  string
	Size         int64
	ContentRange string
}

// Storage 是 AgentCut 使用的最小对象存储能力。Delete 必须幂等：对象不存在
// 时也返回 nil。
type Storage interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) (string, error)
	Get(ctx context.Context, key string) (*Object, error)
	Delete(ctx context.Context, key string) error
	Backend() string
}

// RangeStorage 是可选的 HTTP Range 能力。远端 Blob 实现透传单段 bytes Range；
// 本地存储的 os.File 由 API 层通过 io.ReadSeeker + http.ServeContent 处理。
type RangeStorage interface {
	GetRange(ctx context.Context, key, byteRange string) (*Object, error)
}

var ErrRangeNotSatisfiable = errors.New("blob range not satisfiable")

// Config 从环境变量读取。Endpoint 和 Bucket 必须同时配置或同时缺省。
type Config struct {
	Endpoint string // AGENTCUT_BLOB_ENDPOINT
	Bucket   string // AGENTCUT_BLOB_BUCKET
	CDNBase  string // AGENTCUT_BLOB_CDN_BASE（可选，配置后返回公网 URL）
}

// Store 是匿名 PUT 的 BlobStore 客户端。
type Store struct {
	config Config
	client *http.Client
}

// MissingConfiguration 返回 Blob HTTP 后端缺失的环境变量名。
func MissingConfiguration() []string {
	var missing []string
	if strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_ENDPOINT")) == "" {
		missing = append(missing, "AGENTCUT_BLOB_ENDPOINT")
	}
	if strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_BUCKET")) == "" {
		missing = append(missing, "AGENTCUT_BLOB_BUCKET")
	}
	return missing
}

// NewStoreFromEnv 返回 (nil, nil) 表示未配置。local 后端必须显式启用，
// 防止生产环境配置遗漏时静默写入本机磁盘。
func NewStoreFromEnv() (Storage, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_BACKEND")))
	if backend == "local" {
		root := strings.TrimSpace(os.Getenv("AGENTCUT_LOCAL_BLOB_ROOT"))
		if root == "" {
			root = ".agentcut-blob"
		}
		return NewLocalStore(root)
	}
	if backend != "" && backend != "blob" {
		return nil, fmt.Errorf("AGENTCUT_BLOB_BACKEND must be blob or local")
	}
	config := Config{
		Endpoint: strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_ENDPOINT")),
		Bucket:   strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_BUCKET")),
		CDNBase:  strings.TrimSpace(os.Getenv("AGENTCUT_BLOB_CDN_BASE")),
	}
	if config.Endpoint == "" && config.Bucket == "" {
		return nil, nil
	}
	if missing := MissingConfiguration(); len(missing) > 0 {
		return nil, fmt.Errorf("blob storage configuration missing %s", strings.Join(missing, ", "))
	}
	return NewStore(config), nil
}

// NewStore 用显式配置构造（测试用）。
func NewStore(config Config) *Store {
	return &Store{config: config, client: &http.Client{Timeout: 5 * time.Minute}}
}

func (*Store) Backend() string { return "blob" }

// objectURL 拼出对象的直连 URL。
func (s *Store) objectURL(key string) string {
	return strings.TrimSuffix(s.config.Endpoint, "/") + "/" +
		strings.Trim(s.config.Bucket, "/") + "/" + strings.TrimLeft(key, "/")
}

// Put 上传对象，返回可访问 URL（配置了 CDN 则返回 CDN URL）。
func (s *Store) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), body)
	if err != nil {
		return "", err
	}
	req.ContentLength = size
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("put object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("put object: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if s.config.CDNBase != "" {
		return strings.TrimSuffix(s.config.CDNBase, "/") + "/" + strings.TrimLeft(key, "/"), nil
	}
	return s.objectURL(key), nil
}

// Get 下载对象。
func (s *Store) Get(ctx context.Context, key string) (*Object, error) {
	return s.get(ctx, key, "")
}

func (s *Store) GetRange(ctx context.Context, key, byteRange string) (*Object, error) {
	byteRange = strings.TrimSpace(byteRange)
	if !strings.HasPrefix(byteRange, "bytes=") ||
		strings.ContainsAny(byteRange, "\r\n,") {
		return nil, ErrRangeNotSatisfiable
	}
	return s.get(ctx, key, byteRange)
}

func (s *Store) get(ctx context.Context, key, byteRange string) (*Object, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, err
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, os.ErrNotExist
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		resp.Body.Close()
		return nil, ErrRangeNotSatisfiable
	}
	expectedStatus := http.StatusOK
	if byteRange != "" {
		expectedStatus = http.StatusPartialContent
	}
	if resp.StatusCode != expectedStatus {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("get object: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if byteRange != "" && strings.TrimSpace(resp.Header.Get("Content-Range")) == "" {
		resp.Body.Close()
		return nil, fmt.Errorf("get object: HTTP 206 response has no Content-Range")
	}
	return &Object{
		Body: resp.Body, ContentType: resp.Header.Get("Content-Type"),
		Size: resp.ContentLength, ContentRange: resp.Header.Get("Content-Range"),
	}, nil
}

// Delete 幂等删除对象。
func (s *Store) Delete(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("delete object: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}

// ---- Key 规范（后端详细设计 10.2 节） ----

// SourceKey 是源素材的对象 key。
func SourceKey(projectID, mediaID, sanitizedFilename string) string {
	if timeline.ValidateMediaID(mediaID) != nil {
		return ""
	}
	return fmt.Sprintf("agentcut/%s/source/%s/%s", projectID, mediaID, sanitizedFilename)
}

// ThumbnailKey 是素材缩略图（视频 sprite / 图片单帧）的对象 key。
func ThumbnailKey(projectID, mediaID string) string {
	return ThumbnailKeyForGeneration(projectID, mediaID, 0)
}

func ThumbnailKeyForGeneration(projectID, mediaID string, generation uint64) string {
	if timeline.ValidateMediaID(mediaID) != nil {
		return ""
	}
	if generation == 0 {
		return fmt.Sprintf("agentcut/%s/thumbnails/%s.jpg", projectID, mediaID)
	}
	return fmt.Sprintf("agentcut/%s/derived/%s/g%d/thumbnail.jpg", projectID, mediaID, generation)
}

// ASRAudioKey 是视频为 ASR 提取的内部音频，不登记为独立 Timeline media。
func ASRAudioKey(projectID, mediaID string) string {
	return ASRAudioKeyForGeneration(projectID, mediaID, 0)
}

func ASRAudioKeyForGeneration(projectID, mediaID string, generation uint64) string {
	if timeline.ValidateMediaID(mediaID) != nil {
		return ""
	}
	if generation == 0 {
		return fmt.Sprintf("agentcut/%s/derived/%s/asr.mp3", projectID, mediaID)
	}
	return fmt.Sprintf("agentcut/%s/derived/%s/g%d/asr.mp3", projectID, mediaID, generation)
}

// AudioPreviewKeyForGeneration is a browser-compatible MP3 derivative for
// audio-only uploads whose source codec may not be decodable by HTMLMediaElement.
func AudioPreviewKeyForGeneration(projectID, mediaID string, generation uint64) string {
	if timeline.ValidateMediaID(mediaID) != nil {
		return ""
	}
	if generation == 0 {
		return fmt.Sprintf("agentcut/%s/derived/%s/preview.mp3", projectID, mediaID)
	}
	return fmt.Sprintf("agentcut/%s/derived/%s/g%d/preview.mp3", projectID, mediaID, generation)
}

// SeparatedAudioSourceKey is the authoritative source object for an audio
// media created by "separate audio". It deliberately uses the normal source
// namespace so media content/playback authorization treats it exactly like an
// uploaded independent audio file.
func SeparatedAudioSourceKey(projectID, audioMediaID string) string {
	return SourceKey(projectID, audioMediaID, "separated.mp3")
}

// SnapshotKey 是 Timeline 只读快照的对象 key。
func SnapshotKey(projectID string, revision uint64) string {
	return fmt.Sprintf("agentcut/%s/snapshots/%d.json", projectID, revision)
}

// SnapshotLatestKey 是最新快照的固定 key。
func SnapshotLatestKey(projectID string) string {
	return fmt.Sprintf("agentcut/%s/snapshots/latest.json", projectID)
}

// OutputKey 是渲染成片的对象 key。
func OutputKey(projectID, renderKey string) string {
	return fmt.Sprintf("agentcut/%s/output/%s.mp4", projectID, renderKey)
}

// ---- SnapshotTimeline Activity ----

// SnapshotActivities 实现 entity.SnapshotActivityName。Store 为 nil 时降级为
// 跳过（本地开发无 BlobStore 也能跑，快照只是只读备份，不是权威状态）。
type SnapshotActivities struct {
	Store Storage
}

// SnapshotTimeline 把 {doc, revision} 写进按 revision 的历史 key 和 latest key。
func (a *SnapshotActivities) SnapshotTimeline(ctx context.Context, req entity.SnapshotRequest) error {
	if a.Store == nil {
		return temporal.NewNonRetryableApplicationError(
			"snapshot storage is not configured; set AGENTCUT_BLOB_ENDPOINT and AGENTCUT_BLOB_BUCKET or AGENTCUT_BLOB_BACKEND=local",
			"BLOB_NOT_CONFIGURED", nil)
	}
	payload, err := json.Marshal(map[string]any{
		"projectId": req.ProjectID,
		"revision":  req.Revision,
		"doc":       req.Doc,
	})
	if err != nil {
		return err
	}
	for _, key := range []string{
		SnapshotKey(req.ProjectID, req.Revision),
		SnapshotLatestKey(req.ProjectID),
	} {
		if _, err := a.Store.Put(ctx, key, bytes.NewReader(payload), int64(len(payload)), "application/json"); err != nil {
			return fmt.Errorf("snapshot %s: %w", key, err)
		}
	}
	return nil
}
