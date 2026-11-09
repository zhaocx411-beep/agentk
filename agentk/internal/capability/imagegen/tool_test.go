package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

// stubClient 记录最近一次请求，返回固定图片。
type stubClient struct {
	last Request
	img  Image
	err  error
}

func (s *stubClient) Generate(_ context.Context, req Request) (Image, error) {
	s.last = req
	if s.err != nil {
		return Image{}, s.err
	}
	return s.img, nil
}

// stubStorage 记录写入并返回固定前缀 URL。
type stubStorage struct {
	lastKey  string
	lastData []byte
	lastMIME string
	err      error
}

func (u *stubStorage) Put(_ context.Context, key string, data []byte, contentType string) (artifact.Object, error) {
	u.lastKey, u.lastData, u.lastMIME = key, data, contentType
	if u.err != nil {
		return artifact.Object{}, u.err
	}
	return artifact.Object{Key: key, URL: "http://blob.test/bucket/" + key}, nil
}

func (u *stubStorage) Resolve(uri string) (artifact.Object, bool) {
	if strings.TrimSpace(uri) == "" {
		return artifact.Object{}, false
	}
	return artifact.Object{URL: uri, Visibility: artifact.Private}, true
}
func (u *stubStorage) Open(context.Context, artifact.Object) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

// 工具面是纯声明：执行体指向公开的 capability activity，候选模型进 schema
// 的 model enum，选择权在 LLM。
func TestGenerateImageManifest(t *testing.T) {
	manifest := ManifestCapabilities()[0]
	if manifest.ID != CapabilityID || manifest.Tool == nil {
		t.Fatalf("manifest = %+v", manifest)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, manifest.InputSchema); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
	s := compact.String()
	for _, model := range []string{
		`"z-image-turbo"`,
		`"gpt-image-2"`,
		`"gemini-3-pro-image"`,
		`"gemini-3.1-flash-image"`,
	} {
		if !strings.Contains(s, model) {
			t.Fatalf("schema missing model %s: %s", model, s)
		}
	}
	if !strings.Contains(s, `"required":["model","prompt"]`) {
		t.Fatalf("model must be required: %s", s)
	}
}

func TestMergeParamsDialectWins(t *testing.T) {
	got := mergeParams(map[string]any{"aspectRatio": "1:1", "size": "guess"},
		map[string]any{"size": "1024x1024"})
	if got["size"] != "1024x1024" || got["aspectRatio"] != "1:1" {
		t.Fatalf("merged = %+v", got)
	}
}
