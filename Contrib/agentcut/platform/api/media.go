package api

import (
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"agentcut/platform/blob"
	platformmedia "agentcut/platform/media"
	"agentcut/timeline"
)

func (s *Server) handleMediaContent(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	if s.Blob == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "blob storage is not configured",
			"code":    "BLOB_NOT_CONFIGURED",
			"missing": blob.MissingConfiguration(),
		})
		return
	}
	state, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load timeline")
		return
	}
	mediaID := r.PathValue("mediaID")
	if err := timeline.ValidateMediaID(mediaID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var selected *timeline.Media
	for i := range state.Doc.Media {
		if state.Doc.Media[i].MediaID == mediaID {
			selected = &state.Doc.Media[i]
			break
		}
	}
	if selected == nil || selected.BlobKey == "" {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}
	prefix := fmt.Sprintf("agentcut/%s/source/%s/", ref.ProjectID, mediaID)
	if !strings.HasPrefix(selected.BlobKey, prefix) {
		writeError(w, http.StatusForbidden, "media blob key is outside project scope")
		return
	}
	streamBlobObject(w, r, s.Blob, selected.BlobKey, mediaID, ref.ProjectID, "media")
}

// handleMediaPlayback serves a browser-compatible derivative for audio-only
// uploads. Browsers do not decode every valid WAV codec (notably pcm_f64le);
// the original source remains authoritative for editing/export.
func (s *Server) handleMediaPlayback(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	if s.Blob == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "blob storage is not configured", "code": "BLOB_NOT_CONFIGURED",
		})
		return
	}
	state, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load timeline")
		return
	}
	mediaID := r.PathValue("mediaID")
	if err := timeline.ValidateMediaID(mediaID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var selected *timeline.Media
	for i := range state.Doc.Media {
		if state.Doc.Media[i].MediaID == mediaID {
			selected = &state.Doc.Media[i]
			break
		}
	}
	if selected == nil || selected.Kind != timeline.MediaAudio || selected.BlobKey == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "audio media not found", "code": "MEDIA_NOT_FOUND",
		})
		return
	}
	sourcePrefix := fmt.Sprintf("agentcut/%s/source/%s/", ref.ProjectID, mediaID)
	if !strings.HasPrefix(selected.BlobKey, sourcePrefix) {
		writeError(w, http.StatusForbidden, "media blob key is outside project scope")
		return
	}

	previewKey := blob.AudioPreviewKeyForGeneration(
		ref.ProjectID, mediaID, selected.Generation)
	if existing, getErr := s.Blob.Get(r.Context(), previewKey); getErr == nil {
		_ = existing.Body.Close()
		streamBlobObject(w, r, s.Blob, previewKey, mediaID+".mp3", ref.ProjectID, "audio preview")
		return
	} else if !os.IsNotExist(getErr) {
		writeError(w, http.StatusBadGateway, "failed to inspect audio preview")
		return
	}

	source, err := s.Blob.Get(r.Context(), selected.BlobKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read audio source")
		return
	}
	defer source.Body.Close()
	if source.Size > platformmedia.MaxFFmpegSourceBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error": "audio source exceeds 2 GiB", "code": "MEDIA_TOO_LARGE",
		})
		return
	}
	input, err := os.CreateTemp("", "agentcut-playback-*"+filepath.Ext(selected.BlobKey))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create audio preview input")
		return
	}
	inputPath := input.Name()
	defer os.Remove(inputPath)
	written, copyErr := io.Copy(input, io.LimitReader(
		source.Body, platformmedia.MaxFFmpegSourceBytes+1))
	closeErr := input.Close()
	if written > platformmedia.MaxFFmpegSourceBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error": "audio source exceeds 2 GiB", "code": "MEDIA_TOO_LARGE",
		})
		return
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		writeError(w, http.StatusBadGateway, "failed to stage audio preview")
		return
	}

	output, err := os.CreateTemp("", "agentcut-playback-*.mp3")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create audio preview output")
		return
	}
	outputPath := output.Name()
	_ = output.Close()
	defer os.Remove(outputPath)
	previewer := s.AudioPreviewer
	if previewer == nil {
		previewer = platformmedia.FFAudioPreviewer{Timeout: 5 * time.Minute}
	}
	if err := previewer.TranscodeForPreview(r.Context(), inputPath, outputPath); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "server could not create a browser-compatible audio preview: " + err.Error(),
			"code":  "AUDIO_PREVIEW_TRANSCODE_FAILED",
		})
		return
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect audio preview output")
		return
	}
	preview, err := os.Open(outputPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open audio preview output")
		return
	}
	_, putErr := s.Blob.Put(r.Context(), previewKey, preview, info.Size(), "audio/mpeg")
	closeErr = preview.Close()
	if err := errors.Join(putErr, closeErr); err != nil {
		writeError(w, http.StatusBadGateway, "failed to store audio preview")
		return
	}
	streamBlobObject(w, r, s.Blob, previewKey, mediaID+".mp3", ref.ProjectID, "audio preview")
}

// handleMediaThumbnail 流式返回素材缩略图（视频 sprite / 图片单帧），由
// cut-apiserver 上传时本地 ffmpeg 生成，缺失返回 404（生成失败或尚未支持
// 该素材类型都算缺失，不区分）。
func (s *Server) handleMediaThumbnail(w http.ResponseWriter, r *http.Request) {
	ref, ok := s.requireProject(w, r)
	if !ok {
		return
	}
	if s.Blob == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "blob storage is not configured",
			"code":    "BLOB_NOT_CONFIGURED",
			"missing": blob.MissingConfiguration(),
		})
		return
	}
	state, err := s.getTimelineState(r.Context(), ref.ProjectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load timeline")
		return
	}
	mediaID := r.PathValue("mediaID")
	if err := timeline.ValidateMediaID(mediaID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var selected *timeline.Media
	for i := range state.Doc.Media {
		if state.Doc.Media[i].MediaID == mediaID {
			selected = &state.Doc.Media[i]
			break
		}
	}
	if selected == nil || selected.ThumbnailKey == "" {
		writeError(w, http.StatusNotFound, "thumbnail not found")
		return
	}
	legacyKey := blob.ThumbnailKey(ref.ProjectID, mediaID)
	generationKey := blob.ThumbnailKeyForGeneration(
		ref.ProjectID, mediaID, selected.Generation,
	)
	if selected.ThumbnailKey != legacyKey && selected.ThumbnailKey != generationKey {
		writeError(w, http.StatusForbidden, "thumbnail blob key is outside project scope")
		return
	}
	streamBlobObject(w, r, s.Blob, selected.ThumbnailKey, mediaID, ref.ProjectID, "thumbnail")
}

// streamBlobObject 是 handleMediaContent/handleMediaThumbnail 共用的流式响应逻辑。
func streamBlobObject(
	w http.ResponseWriter, r *http.Request, store blob.Storage,
	blobKey, objectName, projectID, logLabel string,
) {
	var object *blob.Object
	var err error
	if byteRange := strings.TrimSpace(r.Header.Get("Range")); byteRange != "" {
		if ranged, ok := store.(blob.RangeStorage); ok {
			object, err = ranged.GetRange(r.Context(), blobKey, byteRange)
		} else {
			object, err = store.Get(r.Context(), blobKey)
		}
	} else {
		object, err = store.Get(r.Context(), blobKey)
	}
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, logLabel+" content not found")
			return
		}
		if errors.Is(err, blob.ErrRangeNotSatisfiable) {
			writeError(w, http.StatusRequestedRangeNotSatisfiable, "requested byte range is not satisfiable")
			return
		}
		writeError(w, http.StatusBadGateway, "failed to read "+logLabel+" content")
		return
	}
	defer object.Body.Close()
	contentType := object.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	filename := objectName
	if path.Ext(filename) == "" {
		ext, _ := mime.ExtensionsByType(contentType)
		if len(ext) > 0 {
			filename += ext[0]
		}
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename=%q`, disposition, filename))
	if _, ok := store.(blob.RangeStorage); ok {
		w.Header().Set("Accept-Ranges", "bytes")
	}

	// 本地 Blob 返回 *os.File，ServeContent 原生处理单段/后缀 Range、HEAD 和
	// 416；远端 Blob 由 RangeStorage 透传 Range 并在这里转发 206 元数据。
	if seeker, ok := object.Body.(io.ReadSeeker); ok {
		w.Header().Set("Accept-Ranges", "bytes")
		http.ServeContent(w, r, filename, time.Time{}, seeker)
		return
	}
	status := http.StatusOK
	if object.ContentRange != "" {
		status = http.StatusPartialContent
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", object.ContentRange)
	}
	if object.Size >= 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", object.Size))
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	// 包一层 Reader，避免 os.File.WriterTo 触发 sendfile；部分容器/sandbox
	// 禁止 sendfile，普通 read/write 流仍然可用。
	reader := struct{ io.Reader }{Reader: object.Body}
	if _, err := io.CopyBuffer(w, reader, make([]byte, 32<<10)); err != nil {
		log.Printf("WARN: %s stream interrupted projectId=%s object=%s error=%v", logLabel, projectID, objectName, err)
	}
}
