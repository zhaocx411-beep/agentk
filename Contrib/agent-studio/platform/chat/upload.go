package chat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"studio/platform/core"
)

const maxUploadBytes int64 = 256 << 20

type UploadStorage interface {
	Put(
		ctx context.Context,
		key string,
		body io.Reader,
		size int64,
		contentType string,
	) (string, error)
}

type BlobUploadConfig struct {
	Endpoint string
	Bucket   string
	CDNBase  string
}

type BlobUploadStorage struct {
	config BlobUploadConfig
	client *http.Client
}

func NewBlobUploadStorageFromEnv() (*BlobUploadStorage, error) {
	config := BlobUploadConfig{
		Endpoint: strings.TrimSpace(os.Getenv("COMIC_DRAMA_BLOB_ENDPOINT")),
		Bucket:   strings.TrimSpace(os.Getenv("COMIC_DRAMA_BLOB_BUCKET")),
		CDNBase:  strings.TrimSpace(os.Getenv("COMIC_DRAMA_BLOB_CDN_BASE")),
	}
	if config.Endpoint == "" && config.Bucket == "" {
		return nil, nil
	}
	if config.Endpoint == "" || config.Bucket == "" {
		return nil, fmt.Errorf(
			"shared upload storage requires COMIC_DRAMA_BLOB_ENDPOINT and COMIC_DRAMA_BLOB_BUCKET",
		)
	}
	return &BlobUploadStorage{
		config: config,
		client: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (s *BlobUploadStorage) Put(
	ctx context.Context,
	key string,
	body io.Reader,
	size int64,
	contentType string,
) (string, error) {
	objectURL := strings.TrimSuffix(s.config.Endpoint, "/") + "/" +
		strings.Trim(s.config.Bucket, "/") + "/" + strings.TrimLeft(key, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL, body)
	if err != nil {
		return "", err
	}
	request.ContentLength = size
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("upload object: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK &&
		response.StatusCode != http.StatusNoContent {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return "", fmt.Errorf(
			"upload object: HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	if s.config.CDNBase != "" {
		return strings.TrimSuffix(s.config.CDNBase, "/") + "/" +
			strings.TrimLeft(key, "/"), nil
	}
	return objectURL, nil
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.uploads == nil {
		core.WriteError(w, http.StatusServiceUnavailable, "shared upload storage is unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		core.WriteError(w, http.StatusBadRequest, "invalid upload")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > maxUploadBytes {
		core.WriteError(w, http.StatusBadRequest, "file must be between 1 byte and 256 MB")
		return
	}

	sniff := make([]byte, min(header.Size, 512))
	read, err := io.ReadFull(file, sniff)
	if err != nil && err != io.ErrUnexpectedEOF {
		core.WriteError(w, http.StatusBadRequest, "failed to read upload")
		return
	}
	sniff = sniff[:read]
	kind, contentType, extension, err := classifyUpload(
		header.Filename,
		header.Header.Get("Content-Type"),
		sniff,
	)
	if err != nil {
		core.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, err := newUploadKey(extension)
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, "failed to create upload")
		return
	}
	body := io.MultiReader(bytes.NewReader(sniff), file)
	url, err := s.uploads.Put(r.Context(), key, body, header.Size, contentType)
	if err != nil {
		core.WriteError(w, http.StatusBadGateway, "failed to store upload")
		return
	}
	core.WriteJSON(w, http.StatusCreated, map[string]any{
		"url":  url,
		"type": kind,
		"mime": contentType,
		"name": filepath.Base(header.Filename),
		"size": header.Size,
	})
}

func classifyUpload(name, _ string, data []byte) (string, string, string, error) {
	extension := strings.ToLower(filepath.Ext(filepath.Base(name)))
	type format struct {
		kind        string
		contentType string
		matches     func([]byte) bool
	}
	prefix := func(signature string) func([]byte) bool {
		return func(data []byte) bool { return bytes.HasPrefix(data, []byte(signature)) }
	}
	riff := func(form string) func([]byte) bool {
		return func(data []byte) bool {
			return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) &&
				bytes.Equal(data[8:12], []byte(form))
		}
	}
	ftyp := func(data []byte) bool {
		return len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp"))
	}
	text := func(data []byte) bool {
		return utf8.Valid(data) && !bytes.Contains(data, []byte{0})
	}
	zipFile := prefix("PK\x03\x04")
	compoundDocument := prefix("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")
	formats := map[string]format{
		".png":  {"image", "image/png", prefix("\x89PNG\r\n\x1a\n")},
		".jpg":  {"image", "image/jpeg", prefix("\xff\xd8\xff")},
		".jpeg": {"image", "image/jpeg", prefix("\xff\xd8\xff")},
		".webp": {"image", "image/webp", riff("WEBP")},
		".gif": {"image", "image/gif", func(data []byte) bool {
			return bytes.HasPrefix(data, []byte("GIF87a")) ||
				bytes.HasPrefix(data, []byte("GIF89a"))
		}},
		".mp4":      {"video", "video/mp4", ftyp},
		".mov":      {"video", "video/quicktime", ftyp},
		".webm":     {"video", "video/webm", prefix("\x1a\x45\xdf\xa3")},
		".mpeg":     {"video", "video/mpeg", isMPEGVideo},
		".mpg":      {"video", "video/mpeg", isMPEGVideo},
		".mp3":      {"audio", "audio/mpeg", isMP3},
		".wav":      {"audio", "audio/wav", riff("WAVE")},
		".ogg":      {"audio", "audio/ogg", prefix("OggS")},
		".m4a":      {"audio", "audio/mp4", ftyp},
		".aac":      {"audio", "audio/aac", isAAC},
		".flac":     {"audio", "audio/flac", prefix("fLaC")},
		".pdf":      {"document", "application/pdf", prefix("%PDF-")},
		".doc":      {"document", "application/msword", compoundDocument},
		".docx":     {"document", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", zipFile},
		".xls":      {"spreadsheet", "application/vnd.ms-excel", compoundDocument},
		".xlsx":     {"spreadsheet", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", zipFile},
		".csv":      {"spreadsheet", "text/csv; charset=utf-8", text},
		".md":       {"text", "text/markdown; charset=utf-8", text},
		".markdown": {"text", "text/markdown; charset=utf-8", text},
		".txt":      {"text", "text/plain; charset=utf-8", text},
	}
	selected, ok := formats[extension]
	if !ok {
		return "", "", "", fmt.Errorf(
			"unsupported file type %q; upload media, PDF, Word, Excel, CSV, Markdown, or text",
			extension,
		)
	}
	if !selected.matches(data) {
		return "", "", "", fmt.Errorf(
			"file content does not match %s extension",
			extension,
		)
	}
	return selected.kind, selected.contentType, extension, nil
}

func isMPEGVideo(data []byte) bool {
	return len(data) >= 4 && data[0] == 0 && data[1] == 0 && data[2] == 1 &&
		(data[3] == 0xba || data[3] == 0xb3)
}

func isMP3(data []byte) bool {
	return bytes.HasPrefix(data, []byte("ID3")) ||
		len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0
}

func isAAC(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xff &&
		(data[1]&0xf6 == 0xf0)
}

func newUploadKey(extension string) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return "comic-drama/uploads/" + hex.EncodeToString(id[:]) + extension, nil
}
