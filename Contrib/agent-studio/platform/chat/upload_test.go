package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"studio/platform/core/coretest"
)

type fakeUploadStorage struct {
	key         string
	data        []byte
	contentType string
}

func (f *fakeUploadStorage) Put(
	_ context.Context,
	key string,
	body io.Reader,
	_ int64,
	contentType string,
) (string, error) {
	f.key = key
	f.data, _ = io.ReadAll(body)
	f.contentType = contentType
	return "https://cdn.example/" + key, nil
}

func TestUploadStoresManagedMedia(t *testing.T) {
	storage := &fakeUploadStorage{}
	server := NewServer(coretest.NewFakeAgentK(), DefaultRegistry(), storage)
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "frame.png")
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/uploads", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, body: %s", response.StatusCode, raw)
	}
	var result struct {
		URL  string `json:"url"`
		Type string `json:"type"`
		MIME string `json:"mime"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Type != "image" || result.MIME != "image/png" ||
		!strings.HasPrefix(result.URL, "https://cdn.example/comic-drama/uploads/") {
		t.Fatalf("result = %+v", result)
	}
	if !bytes.Equal(storage.data, png) ||
		!strings.HasSuffix(storage.key, ".png") ||
		storage.contentType != "image/png" {
		t.Fatalf("stored key=%q type=%q data=%x",
			storage.key, storage.contentType, storage.data)
	}
}

func TestUploadUnavailableWithoutSharedStorage(t *testing.T) {
	server, _ := newTestServer(t)
	response, err := http.Post(
		server.URL+"/v1/uploads",
		"multipart/form-data; boundary=missing",
		strings.NewReader("--missing--\r\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
}

func TestClassifyUploadUsesFileSignatureInsteadOfDeclaredMIME(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		declared string
		data     []byte
		wantKind string
		wantMIME string
		wantErr  bool
	}{
		{
			name: "valid PNG with generic MIME", filename: "frame.png",
			declared: "application/octet-stream",
			data:     []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"),
			wantKind: "image",
			wantMIME: "image/png",
		},
		{
			name: "plain text declared as PNG", filename: "frame.png",
			declared: "image/png", data: []byte("not an image"), wantErr: true,
		},
		{
			name: "SVG declared as PNG", filename: "frame.png",
			declared: "image/png", data: []byte("<svg></svg>"), wantErr: true,
		},
		{
			name: "PNG renamed to MP4", filename: "clip.mp4",
			declared: "video/mp4",
			data:     []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"),
			wantErr:  true,
		},
		{
			name: "valid PDF", filename: "report.pdf",
			declared: "application/octet-stream", data: []byte("%PDF-1.7\n"),
			wantKind: "document", wantMIME: "application/pdf",
		},
		{
			name: "valid DOCX zip", filename: "report.docx",
			declared: "application/zip", data: []byte("PK\x03\x04document"),
			wantKind: "document",
			wantMIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name: "valid CSV", filename: "metrics.csv",
			declared: "text/plain", data: []byte("month,revenue\n7,100\n"),
			wantKind: "spreadsheet", wantMIME: "text/csv; charset=utf-8",
		},
		{
			name: "binary renamed to text", filename: "notes.txt",
			declared: "text/plain", data: []byte("hello\x00world"), wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			kind, mimeType, _, err := classifyUpload(
				test.filename, test.declared, test.data,
			)
			if test.wantErr && err == nil {
				t.Fatal("expected signature validation error")
			}
			if !test.wantErr &&
				(err != nil || kind != test.wantKind || mimeType != test.wantMIME) {
				t.Fatalf("kind=%q mime=%q err=%v, want %q %q",
					kind, mimeType, err, test.wantKind, test.wantMIME)
			}
		})
	}
}
