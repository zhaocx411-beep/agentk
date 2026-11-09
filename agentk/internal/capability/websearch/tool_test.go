package websearch

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// stubClient 记录 Search 入参的假检索端点。
type stubClient struct {
	last    Request
	results []Result
	err     error
}

func (s *stubClient) Search(_ context.Context, req Request) ([]Result, error) {
	s.last = req
	return s.results, s.err
}

func TestManifestCapability(t *testing.T) {
	manifest := ManifestCapability()
	if manifest.ID != CapabilityID || manifest.Tool == nil {
		t.Fatalf("manifest = %+v", manifest)
	}

	var schema struct {
		Properties struct {
			Model struct {
				Enum        []string `json:"enum"`
				Description string   `json:"description"`
			} `json:"model"`
			Query struct {
				Type      string `json:"type"`
				MaxLength int    `json:"maxLength"`
			} `json:"query"`
			TimeRange struct {
				Enum []string `json:"enum"`
			} `json:"timeRange"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(manifest.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Properties.Model.Enum) != 2 || schema.Properties.Model.Enum[0] != "volc-search" {
		t.Fatalf("model enum = %v", schema.Properties.Model.Enum)
	}
	if !strings.Contains(schema.Properties.Model.Description, "中文网页覆盖好") {
		t.Fatalf("model description = %q", schema.Properties.Model.Description)
	}
	// 检索词上限进 schema：让模型在写入参时就知道边界，而不是发出去才被上游截断
	if schema.Properties.Query.MaxLength != maxQueryChars {
		t.Fatalf("query maxLength = %d, want %d", schema.Properties.Query.MaxLength, maxQueryChars)
	}
	// 档位 enum 使用公共协议值，不能出现任何供应商的原始取值。
	if !slices.Equal(schema.Properties.TimeRange.Enum, timeRangeOptions) {
		t.Fatalf("timeRange enum = %v, want %v", schema.Properties.TimeRange.Enum, timeRangeOptions)
	}
	// 只有 model 与 query 必填：timeRange 是可选的时效性收窄
	if !slices.Equal(schema.Required, []string{"model", "query"}) {
		t.Fatalf("required = %v", schema.Required)
	}
}
