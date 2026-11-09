package gemini_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/chat"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gemini"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

const textResponse = `{"candidates":[{"content":{"parts":[{"text":"done"}]},"finishReason":"STOP"}]}`

func exchange(t *testing.T, request chat.Request, reply string) (map[string]any, chat.Result, error) {
	t.Helper()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(server.Close)
	client, err := gemini.NewChat(
		route.New(
			gateway.MustURL(server.URL),
			nil,
			nil,
		),
		upstream.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Stream(t.Context(), request)
	if err != nil {
		return captured, chat.Result{}, err
	}
	for _, err := range response.Stream {
		if err != nil {
			return captured, chat.Result{}, err
		}
	}
	result, err := response.Result()
	return captured, result, err
}

func messageParts(body map[string]any, index int) []any {
	return body["contents"].([]any)[index].(map[string]any)["parts"].([]any)
}

func TestNewRejectsInvalidRoute(t *testing.T) {
	if _, err := gemini.NewChat(gateway.Route{}); err == nil {
		t.Fatal("invalid route accepted")
	}
}

func TestOversizedMediaWarnsWithoutReplacingGeminiError(t *testing.T) {
	const official = `{"error":{"code":400,` +
		`"message":"File content exceeded the size limit. max_bytes_fetched: 15728640",` +
		`"status":"INVALID_ARGUMENT"}}`
	for _, media := range []chat.MediaRef{
		{URI: "https://cdn.example/clip.mp4", MIMEType: "video/mp4", Bytes: 16 << 20},
		{URI: "https://cdn.example/speech.mp3", MIMEType: "audio/mpeg", Bytes: 16 << 20},
	} {
		t.Run(media.MIMEType, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(official))
			}))
			t.Cleanup(server.Close)
			warned := 0
			client, err := gemini.NewChat(
				route.New(
					gateway.MustURL(server.URL+"/v1beta1/projects/p/locations/global/publishers/google"),
					nil,
					nil,
				),
				upstream.WithHTTPClient(server.Client()),
				gemini.WithWarn(func(context.Context, string, ...any) { warned++ }),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Stream(t.Context(), chat.Request{Model: "test-model", Messages: []chat.Message{{
				Role: chat.RoleUser, Text: "inspect", Media: []chat.MediaRef{media},
			}}})
			if warned != 1 || requests != 1 {
				t.Fatalf("warnings=%d requests=%d; warning must not block the request", warned, requests)
			}
			if !errors.Is(err, chat.ErrRejected) ||
				!strings.Contains(err.Error(), "max_bytes_fetched: 15728640") ||
				!strings.Contains(err.Error(), "INVALID_ARGUMENT") {
				t.Fatalf("error = %v; want original Gemini rejection", err)
			}
		})
	}
}

func TestGenerateContentCarriesImageAndProtocolState(t *testing.T) {
	captured, _, err := exchange(t, chat.Request{Model: "test-model", Messages: []chat.Message{
		{
			Role: chat.RoleAssistant,
			ToolCalls: []chat.ToolCall{
				{
					ID:            "call-1",
					ProviderID:    "provider-1",
					Name:          "view_images",
					Args:          json.RawMessage(`{}`),
					ProtocolState: "signature",
				},
			},
		},
		{
			Role:       chat.RoleTool,
			ToolCallID: "call-1",
			Text:       `{"prompt":"inspect"}`,
			Media: []chat.MediaRef{
				{
					MIMEType: "image/png",
					Data:     []byte("png"),
				},
			},
		},
	}}, textResponse)
	if err != nil {
		t.Fatal(err)
	}
	call := messageParts(captured, 0)[0].(map[string]any)
	result, ok := messageParts(captured, 1)[0].(map[string]any)["functionResponse"].(map[string]any)
	if !ok {
		t.Fatalf("missing functionResponse: %+v", captured)
	}
	image := result["parts"].([]any)[0].(map[string]any)["inlineData"].(map[string]any)
	if call["thoughtSignature"] != "signature" ||
		call["functionCall"].(map[string]any)["id"] != "provider-1" ||
		result["id"] != "provider-1" || result["name"] != "view_images" ||
		image["data"] != "cG5n" || image["mimeType"] != "image/png" {
		t.Fatalf("request = %+v", captured)
	}
	payload := result["response"].(map[string]any)
	if payload["result"].(map[string]any)["prompt"] != "inspect" ||
		payload["images"].([]any)[0].(map[string]any)["$ref"] != image["displayName"] {
		t.Fatalf("tool image response = %+v", result)
	}
}

func TestGenerateContentCarriesUserImageVideoAndAudioBeforeText(t *testing.T) {
	captured, _, err := exchange(t, chat.Request{Model: "test-model", Messages: []chat.Message{{
		Role: chat.RoleUser, Text: "compare",
		Media: []chat.MediaRef{
			{MIMEType: "image/png", Data: []byte("png")},
			{URI: "https://cdn.example/clip.mp4", MIMEType: "video/mp4"},
			{URI: "https://cdn.example/speech.mp3", MIMEType: "audio/mpeg"},
		},
	}}}, textResponse)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "cG5n"}},
		map[string]any{"fileData": map[string]any{"mimeType": "video/mp4", "fileUri": "https://cdn.example/clip.mp4"}},
		map[string]any{"fileData": map[string]any{"mimeType": "audio/mpeg", "fileUri": "https://cdn.example/speech.mp3"}},
		map[string]any{"text": "compare"},
	}
	if len(captured["contents"].([]any)) != 1 || !reflect.DeepEqual(messageParts(captured, 0), want) {
		t.Fatalf("request = %+v", captured)
	}
}

func TestResultNormalizesMissingFunctionCallArgs(t *testing.T) {
	_, got, err := exchange(
		t,
		chat.Request{
			Model: "test-model",
		},
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"provider-1",`+
			`"name":"list_artifacts"},"thoughtSignature":"signature"}]},"finishReason":"STOP"}]}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", got.ToolCalls)
	}
	var args map[string]any
	if err := json.Unmarshal(got.ToolCalls[0].Args, &args); err != nil || args == nil || len(args) != 0 {
		t.Fatalf("args = %s, error = %v; want empty object", got.ToolCalls[0].Args, err)
	}
	replayed, _, err := exchange(t, chat.Request{Model: "test-model", Messages: []chat.Message{
		{Role: chat.RoleAssistant, ToolCalls: got.ToolCalls},
		{Role: chat.RoleTool, ToolCallID: got.ToolCalls[0].ID, Text: `{}`},
	}}, textResponse)
	if err != nil {
		t.Fatal(err)
	}
	part := messageParts(replayed, 0)[0].(map[string]any)
	call := part["functionCall"].(map[string]any)
	if !reflect.DeepEqual(call["args"], map[string]any{}) || call["id"] != "provider-1" ||
		call["name"] != "list_artifacts" || part["thoughtSignature"] != "signature" {
		t.Fatalf("replayed call = %+v", part)
	}
}

func TestResultRejectsEmptyOrUnreplayableContent(t *testing.T) {
	for name, raw := range map[string]string{
		"empty safety result": `{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`,
		"text after function call": `{"candidates":[{"content":{"parts":[` +
			`{"functionCall":{"id":"p1","name":"tool","args":{}}},` +
			`{"text":"after"}]},"finishReason":"STOP"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := exchange(t, chat.Request{Model: "test-model"}, raw); err == nil {
				t.Fatal("expected malformed candidate to be rejected")
			}
		})
	}
}

func TestToolResponseIsAlwaysAnObject(t *testing.T) {
	for resultText, want := range map[string]map[string]any{
		`{"ok":true}`: {"ok": true},
		`"done"`:      {"result": "done"},
		`[1,2]`:       {"result": []any{float64(1), float64(2)}},
		`null`:        {"result": nil},
	} {
		t.Run(resultText, func(t *testing.T) {
			captured, _, err := exchange(t, chat.Request{Model: "test-model", Messages: []chat.Message{
				{
					Role: chat.RoleAssistant,
					ToolCalls: []chat.ToolCall{
						{
							ID:         "call-1",
							ProviderID: "provider-1",
							Name:       "tool",
							Args:       json.RawMessage(`{}`),
						},
					},
				},
				{Role: chat.RoleTool, ToolCallID: "call-1", Text: resultText},
			}}, textResponse)
			if err != nil {
				t.Fatal(err)
			}
			result, ok := messageParts(captured, 1)[0].(map[string]any)["functionResponse"].(map[string]any)
			if !ok {
				t.Fatalf("missing functionResponse: %+v", captured)
			}
			if !reflect.DeepEqual(result["response"], want) {
				t.Errorf("result %s encoded as %#v, want %#v", resultText, result["response"], want)
			}
		})
	}
}

func TestToolUsesJSONSchemaField(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","oneOf":[{"required":["a"]},{"required":["b"]}]}`)
	captured, _, err := exchange(t, chat.Request{Model: "test-model", Tools: []chat.ToolDef{{
		Name: "choose", InputSchema: schema,
	}}}, textResponse)
	if err != nil {
		t.Fatal(err)
	}
	tool := captured["tools"].([]any)[0].(map[string]any)
	declaration := tool["functionDeclarations"].([]any)[0].(map[string]any)
	var want map[string]any
	if err := json.Unmarshal(schema, &want); err != nil {
		t.Fatal(err)
	}
	if _, exists := declaration["parameters"]; exists {
		t.Fatalf("unexpected parameters field: %+v", declaration)
	}
	if declaration["name"] != "choose" || !reflect.DeepEqual(declaration["parametersJsonSchema"], want) {
		t.Fatalf("declaration = %+v", declaration)
	}
}
