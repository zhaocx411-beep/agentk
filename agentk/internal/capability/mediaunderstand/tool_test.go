package mediaunderstand

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// stubClient 记录最近一次请求，返回固定回答。
type stubClient struct {
	last Request
	res  Result
	err  error
}

func (s *stubClient) Understand(_ context.Context, req Request) (Result, error) {
	s.last = req
	if s.err != nil {
		return Result{}, s.err
	}
	return s.res, nil
}

// 工具面是纯声明：执行体指向公开的 capability activity，候选模型进 schema
// 的 model enum，选择权在 LLM。
func TestUnderstandMediaManifest(t *testing.T) {
	manifest := ManifestCapabilities()[0]
	if manifest.ID != CapabilityID || manifest.Tool == nil {
		t.Fatalf("manifest = %+v", manifest)
	}
	s := compactSchema(t, manifest.InputSchema)
	for _, want := range []string{
		`"gemini-3.1-pro-preview"`,
		`"gemini-3-flash-preview"`,
		`"gemini-3.1-flash-lite"`,
		`"required":["model","mediaUrls","prompt"]`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("schema missing %s: %s", want, s)
		}
	}
}

func TestUnderstandVideoSegmentManifest(t *testing.T) {
	manifest := ManifestCapabilities()[1]
	if manifest.ID != SegmentCapabilityID || manifest.Tool == nil {
		t.Fatalf("manifest = %+v", manifest)
	}
	schema := compactSchema(t, manifest.InputSchema)
	for _, want := range []string{
		`"gemini-3.1-pro-preview"`, `"gemini-3-flash-preview"`, `"gemini-3.1-flash-lite"`,
		`"videoUrl"`, `"startSeconds"`, `"endSeconds"`, `"fps"`,
		`"required":["model","videoUrl","startSeconds","endSeconds","prompt"]`,
	} {
		if !strings.Contains(schema, want) {
			t.Fatalf("schema missing %s: %s", want, schema)
		}
	}
}

func compactSchema(t *testing.T, schema []byte) string {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, schema); err != nil {
		t.Fatalf("invalid schema: %v", err)
	}
	return compact.String()
}

// 媒体类型不进 schema：模型填的类型与字节里实际是什么没有必然联系，
// 判定权在取回那一步。
func TestUnderstandMediaSchemaKeepsMediaTypeOutOfTheModelsHands(t *testing.T) {
	schema := string(ManifestCapabilities()[0].InputSchema)
	for _, forbidden := range []string{`"mimeType"`, `"mediaType"`} {
		if strings.Contains(schema, forbidden) {
			t.Fatalf("schema must not expose %s: %s", forbidden, schema)
		}
	}
	// 数量上限进 schema，模型才知道一次能给几个
	if !strings.Contains(schema, `"maxItems"`) || !strings.Contains(schema, `"mediaUrls"`) {
		t.Fatalf("schema = %s", schema)
	}
}
