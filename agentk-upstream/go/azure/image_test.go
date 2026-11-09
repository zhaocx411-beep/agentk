package azure_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/azure"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/route"
)

func TestImageGenerateAndEdit(t *testing.T) {
	for _, edit := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "edit"}[edit], func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				operation := "generations"
				if edit {
					operation = "edits"
				}
				if r.Method != "POST" ||
					r.URL.Path != "/openai/deployments/image-model/images/"+operation ||
					r.URL.Query().Get("api-version") != "2024-02-01" ||
					r.Header.Get("api-key") != "secret" {
					t.Errorf("request=%s %s %v", r.Method, r.URL, r.Header)
				}
				if !edit {
					var b struct {
						Prompt, Size, Quality string
						N                     int
					}
					if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
						t.Error(err)
						return
					}
					if b.Prompt != "画猫" || b.Size != "1920x1088" || b.Quality != "medium" || b.N != 1 {
						t.Errorf("body=%+v", b)
					}
				} else {
					mr, err := r.MultipartReader()
					if err != nil {
						t.Error(err)
						return
					}
					fields := map[string]string{}
					var files []azure.ImageFile
					for {
						p, err := mr.NextPart()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Error(err)
							return
						}
						data, err := io.ReadAll(p)
						closeErr := p.Close()
						if err != nil || closeErr != nil {
							t.Errorf("read=%v close=%v", err, closeErr)
							return
						}
						if p.FormName() == "image[]" {
							files = append(files, azure.ImageFile{Data: data, Filename: p.FileName(), MIME: p.Header.Get("Content-Type")})
						} else {
							fields[p.FormName()] = string(data)
						}
					}
					if len(files) != 2 {
						t.Errorf("files=%v", files)
						return
					}
					if files[0].Filename != "one.png" ||
						files[0].MIME != "image/png" ||
						string(files[0].Data) != "one" ||
						files[1].Filename != "two.jpg" ||
						files[1].MIME != "image/jpeg" ||
						string(files[1].Data) != "two" {
						t.Errorf("files=%v", files)
					}
					if fields["prompt"] != "画猫" ||
						fields["size"] != "1920x1088" ||
						fields["quality"] != "medium" ||
						fields["n"] != "1" {
						t.Errorf("fields=%v", fields)
					}
				}
				_, _ = io.WriteString(w, `{"data":[{"b64_json":"cG5n"}]}`)
			}))
			defer srv.Close()
			api, err := azure.NewImage(
				route.New(
					gateway.MustURL(srv.URL+"/openai/deployments/image-model"),
					nil,
					gateway.HeaderKey{
						Name:  "api-key",
						Value: "secret",
					},
				),
				upstream.WithHTTPClient(srv.Client()),
			)
			if err != nil {
				t.Fatal(err)
			}
			req := azure.ImageRequest{
				APIVersion: "2024-02-01",
				Prompt:     "画猫",
				Size:       "1920x1088",
				Quality:    "medium",
				Images: []azure.ImageFile{
					{
						Data:     []byte("one"),
						Filename: "one.png",
						MIME:     "image/png",
					},
					{
						Data:     []byte("two"),
						Filename: "two.jpg",
						MIME:     "image/jpeg",
					},
				},
			}
			var got azure.Image
			if edit {
				got, err = api.Edit(t.Context(), req)
			} else {
				got, err = api.Generate(t.Context(), req)
			}
			if err != nil || calls != 1 || string(got.Data) != "png" {
				t.Fatalf("response=%+v calls=%d error=%v", got, calls, err)
			}
		})
	}
}
func TestImageResponses(t *testing.T) {
	for _, tc := range []struct {
		name                string
		status              int
		body, code, message string
		wantError           bool
	}{
		{"url", 200, `{"data":[{"url":"https://cdn.test/image?signature=a%2Bb"}]}`, "", "", false},
		{"empty data", 200, `{"data":[]}`, "", "", true},
		{"empty item", 200, `{"data":[{}]}`, "", "", true},
		{"invalid base64", 200, `{"data":[{"b64_json":"!invalid!"}]}`, "", "", true},
		{"invalid json", 200, `{`, "", "", true},
		{
			"rejected",
			400,
			`{"error":{"code":"ContentPolicyViolation","message":"blocked"}}`,
			"ContentPolicyViolation",
			"blocked",
			true,
		},
		{"non json", 502, `bad gateway`, "", "bad gateway", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("apim-request-id", "req-id")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			api, err := azure.NewImage(route.New(gateway.MustURL(srv.URL), nil, nil), upstream.WithHTTPClient(srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			got, err := api.Generate(t.Context(), azure.ImageRequest{Prompt: "p"})
			if (err != nil) != tc.wantError || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if tc.name == "url" && got.URL != "https://cdn.test/image?signature=a%2Bb" {
				t.Fatalf("image=%+v", got)
			}
			if tc.message != "" {
				var e *upstream.APIError
				if !errors.As(
					err,
					&e,
				) || e.StatusCode != tc.status || e.Code != tc.code || e.Message != tc.message || e.RequestID != "req-id" {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}
