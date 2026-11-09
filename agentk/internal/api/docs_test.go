package api

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"agentk/internal/event"
	"agentk/internal/pipeline"
	"agentk/internal/profile"

	"gopkg.in/yaml.v3"
)

// TestOpenAPISpecServed 验证 /openapi.yaml 可取且是合法 YAML,且路由表里的
// 每个业务路径都在 spec 里(spec 是手工维护的,这里挡"加了路由忘了写文档")。
func TestOpenAPISpecServed(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.url + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		OpenAPI string                 `yaml:"openapi"`
		Paths   map[string]interface{} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("spec is not valid yaml: %v", err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.") {
		t.Fatalf("openapi version = %q, want 3.x", doc.OpenAPI)
	}
	// 与 Handler() 的路由表对照(路径参数名不要求一致,只对齐段结构)
	want := []string{
		"/v1/sessions",
		"/v1/sessions/{sessionId}/messages",
		"/v1/sessions/{sessionId}/events",
		"/v1/sessions/{sessionId}/stream",
		"/v1/sessions/{sessionId}/answers",
		"/v1/sessions/{sessionId}/cancel",
		"/v1/sessions/{sessionId}/pipelines",
		"/v1/sessions/{sessionId}/pipeline-runs",
		"/v1/sessions/{sessionId}/pipeline-runs/{runId}",
		"/v1/capabilities",
		"/v1/me",
	}
	for _, p := range want {
		if _, ok := doc.Paths[p]; !ok {
			t.Errorf("spec is missing path %s", p)
		}
	}
	if len(doc.Paths) != len(want) {
		t.Errorf("spec has %d paths, route table has %d — 检查两边是否同步", len(doc.Paths), len(want))
	}
}

// TestOpenAPIEventKindsInSync 验证 spec 里 Event.kind 的枚举与 event.AllKinds
// 完全一致:新增事件 kind 时必须同步文档,否则下游按枚举做穷尽处理会漏。
func TestOpenAPIEventKindsInSync(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas struct {
				Event struct {
					Properties struct {
						Kind struct {
							Enum []string `yaml:"enum"`
						} `yaml:"kind"`
					} `yaml:"properties"`
				} `yaml:"Event"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openapiSpec, &doc); err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool)
	for _, k := range doc.Components.Schemas.Event.Properties.Kind.Enum {
		got[k] = true
	}
	for _, k := range event.AllKinds {
		if !got[string(k)] {
			t.Errorf("spec kind enum is missing %q", k)
		}
	}
	if len(got) != len(event.AllKinds) {
		t.Errorf("spec has %d kinds, event.AllKinds has %d", len(got), len(event.AllKinds))
	}
}

// TestOpenAPIProfileFieldsInSync 验证 spec 里 Profile 相关 schema 的字段集与
// profile 包的结构体完全一致。spec 声明 additionalProperties: false,代码是
// DisallowUnknownFields——两边字段集相等正是该保的不变量:代码加了字段文档
// 没跟上、或文档写了代码不认的字段,都在这里挡下。
func TestOpenAPIProfileFieldsInSync(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]interface{} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(openapiSpec, &doc); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		schema string
		typ    reflect.Type
	}{
		{"Profile", reflect.TypeOf(profile.Document{})},
		{"ProfileToolEntry", reflect.TypeOf(profile.ToolEntry{})},
		{"ProfilePipelineEntry", reflect.TypeOf(pipeline.ProfileEntry{})},
	}
	for _, c := range cases {
		spec, ok := doc.Components.Schemas[c.schema]
		if !ok {
			t.Errorf("spec is missing schema %s", c.schema)
			continue
		}
		want := jsonFieldNames(c.typ)
		for _, name := range want {
			if _, ok := spec.Properties[name]; !ok {
				t.Errorf("%s: spec is missing field %q", c.schema, name)
			}
		}
		if len(spec.Properties) != len(want) {
			t.Errorf("%s: spec has %d fields, struct has %d — 检查两边是否同步",
				c.schema, len(spec.Properties), len(want))
		}
	}
}

// jsonFieldNames 取结构体导出字段的 json 名(去掉 ,omitempty 等选项)。
func jsonFieldNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if name, _, _ := strings.Cut(tag, ","); name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

// TestDocsPageServed /docs 冒烟:返回 HTML 且指向本服务的 spec。
func TestDocsPageServed(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.url + "/docs")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "/openapi.yaml") {
		t.Fatal("docs page does not reference /openapi.yaml")
	}
}
