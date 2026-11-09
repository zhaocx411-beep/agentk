package artifact

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LocalStorage 是本地开发和单实例 demo 使用的产物存储。
type LocalStorage struct {
	dir    string
	prefix string
}

func NewLocalStorage(dir, base string) *LocalStorage {
	return &LocalStorage{
		dir:    dir,
		prefix: strings.TrimSuffix(base, "/") + "/files/",
	}
}

// Dir 返回本地产物根目录，供应用挂载与 URL 对应的 /files/ 读取端点。
func (s *LocalStorage) Dir() string { return s.dir }

func (s *LocalStorage) objectPath(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || path.Clean(key) != key || strings.HasPrefix(key, "../") {
		return "", fmt.Errorf("artifact: invalid local object key %q", key)
	}
	return filepath.Join(s.dir, filepath.FromSlash(key)), nil
}

func (s *LocalStorage) Put(_ context.Context, key string, data []byte, _ string) (Object, error) {
	file, err := s.objectPath(key)
	if err != nil {
		return Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return Object{}, err
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return Object{}, err
	}
	return Object{Key: key, URL: s.prefix + key, Visibility: Private}, nil
}

func (s *LocalStorage) Resolve(uri string) (Object, bool) {
	key, ok := strings.CutPrefix(uri, s.prefix)
	if !ok {
		return Object{}, false
	}
	if _, err := s.objectPath(key); err != nil {
		return Object{}, false
	}
	return Object{Key: key, URL: uri, Visibility: Private}, true
}

func (s *LocalStorage) Open(_ context.Context, object Object) (io.ReadCloser, error) {
	file, err := s.objectPath(object.Key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("artifact: open %q: %w", object.Key, err)
	}
	return f, nil
}

// LocalDirFromEnv 返回本地产物目录。
//
//	AGENTK_FILES_DIR  可选，产物落盘目录；缺省系统临时目录下 agentk-files
func LocalDirFromEnv() string {
	if dir := os.Getenv("AGENTK_FILES_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(os.TempDir(), "agentk-files")
}

// LocalBaseURLFromEnv 返回本地产物 URL 的前缀，即应用挂载 /files/ 读取端点的
// 地址前缀。产物 URL 要能被回读方解析，故它必须与实际挂载位置一致。
//
//	AGENTK_FILES_BASE_URL  可选，覆盖该前缀；不设用调用方给的兜底值
func LocalBaseURLFromEnv(fallback string) string {
	return envOr("AGENTK_FILES_BASE_URL", fallback)
}
