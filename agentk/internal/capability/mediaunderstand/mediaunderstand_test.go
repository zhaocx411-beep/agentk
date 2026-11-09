package mediaunderstand

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"agentk/internal/artifact"
)

func TestDetectMIME(t *testing.T) {
	cases := []struct {
		name        string
		file        string
		contentType string
		data        []byte
		want        string
	}{
		// 扩展名优先：CDN 的 octet-stream 不该让一段好好的 mp4 判成不支持
		{"扩展名压过 octet-stream", "/cdn/clip.mp4", "application/octet-stream", nil, "video/mp4"},
		{"mov 走 quicktime", "clip.MOV", "", nil, "video/quicktime"},
		{"受管产物 key 的扩展名", "org_1/20260728/img_x.png", "", nil, "image/png"},
		{"jpg 归到 jpeg", "a.jpg", "", nil, "image/jpeg"},
		// 无扩展名（签名 URL 常见）时退到声明类型
		{"退到 Content-Type", "/download", "video/webm", nil, "video/webm"},
		{"Content-Type 带参数", "/download", "image/jpeg; charset=binary", nil, "image/jpeg"},
		// 两者都没有时按字节嗅探
		{"嗅探 png", "/download", "", pngBytes(), "image/png"},
		{"嗅探 mp4", "/download", "", mp4Bytes(), "video/mp4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := detectMIME(c.file, c.contentType, c.data)
			if err != nil || got != c.want {
				t.Fatalf("detectMIME(%q, %q) = %q, %v; want %q", c.file, c.contentType, got, err, c.want)
			}
		})
	}

	// 判不出或判出不支持的类型必须报错，避免模型基于未正确解码的字节生成错误结论
	for _, c := range []struct{ file, contentType string }{
		{"notes.txt", "text/plain"},
		{"/download", "application/pdf"},
		{"/download", ""},
		{"a.gif", "image/gif"}, // gemini 不收 gif
	} {
		if got, err := detectMIME(c.file, c.contentType, []byte("plain text")); err == nil {
			t.Errorf("detectMIME(%q, %q) = %q; want error", c.file, c.contentType, got)
		}
	}
}

// mediaHandler 模拟一个 CDN：HEAD 只回头（类型与长度），GET 回字节。
// allowHead 为 false 时 HEAD 返回 405；不少 CDN 不支持 HEAD。
func mediaHandler(contentType string, body []byte, allowHead bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && !allowHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(body)
	}
}

// 公网可达且在上游配额内的素材只给 URL，字节不经过我们，也不占内联预算。
func TestFetchMediaPrefersRemoteWhenEligible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc("/frame.png", mediaHandler("image/png", pngBytes(), true))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	media, err := fetchMedia(context.Background(), srv.Client(), nil,
		[]string{srv.URL + "/clip.mp4", srv.URL + "/frame.png"}, true)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range media {
		if !m.Remote || len(m.Data) != 0 {
			t.Fatalf("media[%d] = %+v; want remote with no bytes carried", i, m)
		}
	}
	if media[0].MIME != "video/mp4" || media[1].MIME != "image/png" {
		t.Fatalf("media = %+v", media)
	}
	// HEAD 报的长度进 Bytes，供结果回报
	if media[0].Bytes != len(mp4Bytes()) {
		t.Fatalf("bytes = %d", media[0].Bytes)
	}
}

// 走不通 URL 那条路的每种情形都应静默回落内联，而不是报错——内联总是安全的。
func TestFetchMediaFallsBackToInline(t *testing.T) {
	big := append(mp4Bytes(), bytes.Repeat([]byte{7}, maxRemoteVideoBytes)...)
	mux := http.NewServeMux()
	mux.HandleFunc("/clip.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc("/second.mp4", mediaHandler("video/mp4", mp4Bytes(), true))
	mux.HandleFunc("/nohead.mp4", mediaHandler("video/mp4", mp4Bytes(), false))
	mux.HandleFunc("/big.mp4", mediaHandler("video/mp4", big, true))
	// 无扩展名 + octet-stream：HEAD 判不出类型，下载后按字节嗅探才认出来
	mux.HandleFunc("/opaque", mediaHandler("application/octet-stream", mp4Bytes(), true))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cases := []struct {
		name  string
		urls  []string
		index int
	}{
		{"HEAD 不通", []string{srv.URL + "/nohead.mp4"}, 0},
		{"视频超 URL 方式的体积上限", []string{srv.URL + "/big.mp4"}, 0},
		{"HEAD 判不出类型", []string{srv.URL + "/opaque"}, 0},
		// 上游一次只收 1 个视频 URL，第二个必须内联，而不是让整次调用失败
		{"超出每请求视频配额", []string{srv.URL + "/clip.mp4", srv.URL + "/second.mp4"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			media, err := fetchMedia(context.Background(), srv.Client(), nil, c.urls, true)
			if err != nil {
				t.Fatal(err)
			}
			got := media[c.index]
			if got.Remote || len(got.Data) == 0 || got.MIME != "video/mp4" {
				t.Fatalf("media[%d] = {Remote:%v MIME:%q bytes:%d}; want inline video/mp4",
					c.index, got.Remote, got.MIME, len(got.Data))
			}
		})
	}
}

func TestFetchMediaSharesOneByteBudget(t *testing.T) {
	// 两段各自都不超预算，加起来超：预算是所有内联素材共享的总额而非单段上限。
	// HEAD 不通，两段都只能内联。
	half := maxTotalBytes/2 + 1
	body := append(mp4Bytes(), bytes.Repeat([]byte{7}, half)...)
	srv := httptest.NewServer(mediaHandler("video/mp4", body, false))
	t.Cleanup(srv.Close)

	one := srv.URL + "/a.mp4"
	if _, err := fetchMedia(context.Background(), srv.Client(), nil, []string{one}, false); err != nil {
		t.Fatalf("single item within budget: %v", err)
	}
	_, err := fetchMedia(context.Background(), srv.Client(), nil, []string{one, srv.URL + "/b.mp4"}, false)
	if err == nil || !strings.Contains(err.Error(), "shared inline bytes") {
		t.Fatalf("err = %v; want shared-budget rejection", err)
	}
}

func TestFetchMediaLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(mp4Bytes())
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchMedia(context.Background(), srv.Client(), nil, nil, false); err == nil {
		t.Fatal("expected rejection for empty url list")
	}
	tooMany := make([]string, maxMediaItems+1)
	for i := range tooMany {
		tooMany[i] = srv.URL + "/a.mp4"
	}
	if _, err := fetchMedia(context.Background(), srv.Client(), nil, tooMany, false); err == nil ||
		!strings.Contains(err.Error(), "at most") {
		t.Fatalf("err = %v; want item-count rejection", err)
	}
	// 报错要指明是第几个素材出的问题，否则一组 URL 里模型不知道该换哪个
	_, err := fetchMedia(context.Background(), srv.Client(), nil,
		[]string{srv.URL + "/a.mp4", "ftp://host/b.mp4"}, false)
	if err == nil || !strings.Contains(err.Error(), "media 1") {
		t.Fatalf("err = %v; want the failing index", err)
	}
}

func TestFetchMediaRejectsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
	}))
	t.Cleanup(srv.Close)
	if _, err := fetchMedia(context.Background(), srv.Client(), nil, []string{srv.URL + "/a.mp4"}, false); err == nil {
		t.Fatal("expected rejection for empty media")
	}
}

// 受管私有产物只有本进程读得到：既不试 URL，也不绕 HTTP 下载，直接读存储。
func TestFetchMediaReadsPrivateArtifactFromStorage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("private artifact must not be fetched over HTTP (%s)", r.Method)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	uri := srv.URL + "/files/org_1/20260728/img_x.png"
	storage := &fakeStorage{
		objects: map[string]artifact.Object{uri: {
			Key: "org_1/20260728/img_x.png", URL: uri, Visibility: artifact.Private,
		}},
		data: map[string][]byte{uri: pngBytes()},
	}
	media, err := fetchMedia(context.Background(), srv.Client(), storage, []string{uri}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(media) != 1 || media[0].Remote || media[0].MIME != "image/png" || media[0].Kind != "image" {
		t.Fatalf("media = %+v", media)
	}

	storage.openErr = errors.New("blob down")
	if _, err := fetchMedia(context.Background(), srv.Client(), storage, []string{uri}, true); err == nil ||
		!strings.Contains(err.Error(), "blob down") {
		t.Fatalf("err = %v", err)
	}
}

// 受管公共产物（配了 CDN）交上游自取；上游那条路走不通时回落读存储，
// 而不是绕一圈 HTTP——更快，也不受 CDN 生效延迟影响。
func TestFetchMediaPublicArtifactPrefersRemoteThenStorage(t *testing.T) {
	var headOnly bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("managed artifact must not be downloaded over HTTP (%s)", r.Method)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if headOnly {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", strconv.Itoa(len(pngBytes())))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	uri := srv.URL + "/cdn/org_1/img_x.png"
	storage := &fakeStorage{
		objects: map[string]artifact.Object{uri: {
			Key: "org_1/img_x.png", URL: uri, Visibility: artifact.Public,
		}},
		data: map[string][]byte{uri: pngBytes()},
	}

	media, err := fetchMedia(context.Background(), srv.Client(), storage, []string{uri}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !media[0].Remote || len(media[0].Data) != 0 {
		t.Fatalf("public artifact should be handed over as a URL: %+v", media[0])
	}

	headOnly = true // HEAD 405：回落内联，且字节来自存储而非 HTTP
	media, err = fetchMedia(context.Background(), srv.Client(), storage, []string{uri}, true)
	if err != nil {
		t.Fatal(err)
	}
	if media[0].Remote || !bytes.Equal(media[0].Data, pngBytes()) {
		t.Fatalf("fallback must inline from storage: %+v", media[0])
	}
}

// 认不出的 URL 按外部公网直链处理：generate_video 登记的产物就是上游厂商直链，
// 只认受管 URL 等于读不了自己刚生成的视频。
func TestFetchMediaHandlesUnmanagedURL(t *testing.T) {
	srv := httptest.NewServer(mediaHandler("video/mp4", mp4Bytes(), true))
	t.Cleanup(srv.Close)

	storage := &fakeStorage{objects: map[string]artifact.Object{}}
	media, err := fetchMedia(context.Background(), srv.Client(), storage, []string{srv.URL + "/cdn/x.mp4"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(media) != 1 || media[0].Kind != "video" || !media[0].Remote {
		t.Fatalf("media = %+v", media)
	}
}
