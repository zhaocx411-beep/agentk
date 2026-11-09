package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// stubEditClient 记录最近一次请求，返回固定图片。
type stubEditClient struct {
	last EditRequest
	img  Image
	err  error
}

func (s *stubEditClient) Edit(_ context.Context, req EditRequest) (Image, error) {
	s.last = req
	if s.err != nil {
		return Image{}, s.err
	}
	return s.img, nil
}

// 工具面是纯声明：执行体指向 edit capability activity，候选模型进 schema
// 的 model enum，选择权在 LLM。
func TestEditImageManifest(t *testing.T) {
	manifest := ManifestCapabilities()[1]
	if manifest.ID != EditCapabilityID || manifest.Tool == nil {
		t.Fatalf("manifest = %+v", manifest)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, manifest.InputSchema); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
	s := compact.String()
	if !strings.Contains(s, "referenceImages") ||
		!strings.Contains(s, `"qwen-image-edit-plus-2025-10-30"`) ||
		!strings.Contains(s, `"required":["model","prompt","referenceImages"]`) {
		t.Fatalf("schema = %s", s)
	}
}
