package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/preparemedia"
	"agentcut/timeline"
)

// maxUploadBytes 是单文件上限（MVP 服务端中转上传，不支持分片）。
const maxUploadBytes int64 = 2 << 30 // 2 GiB

// handleUpload 实现服务端中转上传：
//
//	multipart 字段：file（必填）、kind（video/audio/image 必填）、
//	durationMs（video/audio 可选的客户端提示；服务端 ffprobe 结果为准）
//
// 成功后把 addMedia op（source=system，幂等键 upload:{mediaId}）发给
// Timeline 实体 Workflow，返回登记好的 media 与新 revision。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	if s.Blob == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "blob storage is not configured",
			"code":    "BLOB_NOT_CONFIGURED",
			"missing": blob.MissingConfiguration(),
			"hint":    "set AGENTCUT_BLOB_ENDPOINT and AGENTCUT_BLOB_BUCKET, or AGENTCUT_BLOB_BACKEND=local for development",
		})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxUploadBytes {
		writeError(w, http.StatusBadRequest, "file must be between 1 byte and 2 GiB")
		return
	}

	kind := timeline.MediaKind(r.FormValue("kind"))
	switch kind {
	case timeline.MediaVideo, timeline.MediaAudio, timeline.MediaImage:
	default:
		writeError(w, http.StatusBadRequest, "kind must be one of video/audio/image")
		return
	}

	var clientDurationMs int64
	if raw := strings.TrimSpace(r.FormValue("durationMs")); raw != "" {
		clientDurationMs, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || clientDurationMs <= 0 {
			writeError(w, http.StatusBadRequest, "durationMs must be a positive integer when provided")
			return
		}
	}

	// 取文档 fps 用于毫秒 → 帧换算。
	st, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load timeline")
		return
	}
	fps := st.Doc.Metadata.FPS

	tmp, err := os.CreateTemp("", "agentcut-upload-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare upload")
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()
	written, err := io.Copy(tmp, io.LimitReader(file, maxUploadBytes+1))
	if err != nil || written <= 0 || written > maxUploadBytes {
		writeError(w, http.StatusBadRequest, "file must be between 1 byte and 2 GiB")
		return
	}

	var probe *timeline.MediaProbe
	durationMs := int64(0)
	if kind != timeline.MediaImage {
		prober := s.Prober
		if prober == nil {
			prober = platformmedia.FFProber{Timeout: 15 * time.Second}
		}
		pctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		detected, probeErr := prober.Probe(pctx, tmpPath)
		cancel()
		if probeErr != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "server could not validate media duration: " + probeErr.Error(),
				"code":  "MEDIA_PROBE_FAILED",
				"hint":  "install ffprobe on cut-apiserver and upload a supported media file",
			})
			return
		}
		probe = &detected
		durationMs = detected.DurationMs
		if clientDurationMs > 0 && abs64(clientDurationMs-durationMs) > 500 {
			log.Printf("WARN: upload duration corrected projectId=%s file=%q clientMs=%d probeMs=%d",
				ref.ProjectID, header.Filename, clientDurationMs, durationMs)
		}
	}

	mediaID := newID("med")
	key := blob.SourceKey(ref.ProjectID, mediaID, sanitizeFilename(header.Filename))
	contentType := header.Header.Get("Content-Type")
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare upload")
		return
	}
	url, err := s.Blob.Put(r.Context(), key, tmp, written, contentType)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to store upload: "+err.Error())
		return
	}
	if s.Blob.Backend() == "local" {
		url = fmt.Sprintf("/v1/projects/%s/media/%s/content", ref.ProjectID, mediaID)
	}

	media := timeline.Media{
		MediaID:          mediaID,
		URI:              url,
		BlobKey:          key,
		Kind:             kind,
		DurationInFrames: framesFromMs(durationMs, fps),
		Probe:            probe,
	}
	processing := preparemedia.InitialProcessing(media)
	media.Processing = &processing
	res, herr := s.applyOps(r.Context(), ref.ProjectID, "upload:"+mediaID, "system", st.Revision,
		[]timeline.Operation{{Op: timeline.OpAddMedia, Media: &media}})
	if herr != nil {
		// baseRevision 在读取和提交之间被并发修改：按最新 revision 重试一次。
		if herr.status == http.StatusConflict {
			latest, lerr := s.getTimelineState(r.Context(), ref.ProjectID)
			if lerr == nil {
				res, herr = s.applyOps(r.Context(), ref.ProjectID, "upload:"+mediaID+":retry", "system", latest.Revision,
					[]timeline.Operation{{Op: timeline.OpAddMedia, Media: &media}})
			}
		}
		if herr != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
			referenced, queryErr := s.timelineReferencesMediaBlob(cleanupCtx, ref.ProjectID, mediaID, key)
			if queryErr != nil {
				// An uncertain update outcome plus an unavailable query is not
				// enough evidence to delete bytes that may now be authoritative.
				log.Printf("WARN: skip orphan upload cleanup because timeline reference check failed projectId=%s mediaId=%s key=%q error=%v",
					ref.ProjectID, mediaID, key, queryErr)
			} else if !referenced {
				if err := s.Blob.Delete(cleanupCtx, key); err != nil {
					log.Printf("ERROR: orphan upload blob cleanup failed projectId=%s mediaId=%s key=%q error=%v",
						ref.ProjectID, mediaID, key, err)
				}
			}
			cancel()
			herr.write(w)
			return
		}
	}
	media.Generation = res.AddedMediaGenerations[mediaID]
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	if _, err := s.startMediaPrepare(startCtx, ref.ProjectID, mediaID, ref.SessionID); err != nil {
		log.Printf("ERROR: start media preparation failed projectId=%s mediaId=%s error=%v",
			ref.ProjectID, mediaID, err)
	}
	cancel()
	writeJSON(w, http.StatusCreated, map[string]any{
		"media":    media,
		"revision": res.Revision,
	})
}

func (s *Server) timelineReferencesMediaBlob(
	ctx context.Context, projectID, mediaID, blobKey string,
) (bool, error) {
	state, err := s.getTimelineState(ctx, projectID)
	if err != nil {
		return false, err
	}
	for _, media := range state.Doc.Media {
		if media.MediaID == mediaID && media.BlobKey == blobKey {
			return true, nil
		}
	}
	return false, nil
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// framesFromMs 按文档 fps 把毫秒换算成帧（四舍五入）。
func framesFromMs(ms int64, fps int) int64 {
	if ms <= 0 {
		return 0
	}
	return int64(math.Round(float64(ms) * float64(fps) / 1000.0))
}

// sanitizeFilename 只保留安全字符，防止对象 key 注入。
func sanitizeFilename(name string) string {
	base := filepath.Base(name)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "file"
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" || strings.Trim(out, "._") == "" {
		return "file"
	}
	if len(out) > 128 {
		out = out[len(out)-128:]
	}
	return out
}
