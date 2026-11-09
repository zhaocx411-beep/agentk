package blob

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// LocalStore 是显式启用的本地开发存储。它不适合多副本生产部署。
type LocalStore struct {
	root string
}

func NewLocalStore(root string) (*LocalStore, error) {
	abs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve local blob root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create local blob root: %w", err)
	}
	return &LocalStore{root: abs}, nil
}

func (*LocalStore) Backend() string { return "local" }

func (s *LocalStore) safePath(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(key)))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid blob key")
	}
	full := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("blob key escapes storage root")
	}
	return full, nil
}

func (s *LocalStore) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) (string, error) {
	path, err := s.safePath(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create object directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agentcut-*")
	if err != nil {
		return "", fmt.Errorf("create temporary object: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if size >= 0 {
		body = io.LimitReader(body, size+1)
	}
	written, copyErr := io.Copy(tmp, &contextReader{ctx: ctx, reader: body})
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", fmt.Errorf("write object: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close object: %w", closeErr)
	}
	if size >= 0 && written != size {
		return "", fmt.Errorf("object size mismatch: expected %d, wrote %d", size, written)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("publish object: %w", err)
	}
	// local:// 只作内部标识；浏览器应使用受鉴权的 media content API。
	return "local://" + filepath.ToSlash(key), nil
}

func (s *LocalStore) Get(_ context.Context, key string) (*Object, error) {
	path, err := s.safePath(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return &Object{Body: f, ContentType: contentType, Size: info.Size()}, nil
}

func (s *LocalStore) Delete(_ context.Context, key string) error {
	path, err := s.safePath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}
