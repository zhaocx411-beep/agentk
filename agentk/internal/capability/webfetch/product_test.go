package webfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"agentk/internal/artifact"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func runProductDetail(t *testing.T, client ProductDetailClient, productURL string) ProductDetailOut {
	t.Helper()
	out, err := client.FetchProductDetail(
		context.Background(), ProductDetailInput{ProductURL: productURL}, "test")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func productClientForServer(t *testing.T, srv *httptest.Server) ProductDetailClient {
	t.Helper()
	imageClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader([]byte("\x89PNG\r\n\x1a\nimage"))),
		}, nil
	})}
	client, err := NewProductDetailClient(ApifyConfig{
		BaseURL: srv.URL, Token: "secret-token",
		TaobaoActor: "zen-studio~taobao-detail-scraper",
		AmazonActor: "junglee~amazon-crawler",
		Client:      srv.Client(), ImageClient: imageClient,
		Artifacts: artifact.NewLocalStorage(t.TempDir(), "http://artifacts.test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFetchTaobaoProductDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost ||
			r.URL.Path != "/v2/acts/zen-studio~taobao-detail-scraper/run-sync-get-dataset-items" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Has("maxItems") {
			t.Errorf("query must not set Apify run cost limit: %s", r.URL.RawQuery)
		}
		var input struct {
			Items []string `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.Items) != 1 || input.Items[0] != "100" {
			t.Errorf("input = %+v", input)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"input":"service-input",
			"_detailFetched":true,
			"resolvedUrl":"https://item.taobao.com/item.htm?id=100&skuId=200",
			"title":"红色衬衫",
			"price":"199.00",
			"mainPictureUrl":"//img.alicdn.com/main.jpg",
			"pictures":["//img.alicdn.com/main.jpg","//img.alicdn.com/gallery.jpg"],
			"descriptionHtml":"<img src=\"//img.alicdn.com/detail.jpg\">",
			"skus":[
				{"skuId":"200","propsIds":"color:red;size:m","propsNames":"红色;M"},
				{"skuId":201,"propsIds":"color:blue","propsNames":"蓝色"}
			],
			"propsImages":{
				"color:red":"//img.alicdn.com/red.jpg",
				"size:m":"//img.alicdn.com/red.jpg"
			}
		}]`))
	}))
	t.Cleanup(srv.Close)

	got := runProductDetail(t, productClientForServer(t, srv),
		"https://item.taobao.com/item.htm?id=100&skuId=200")
	if got.Platform != "taobao" || len(got.Artifacts) != 3 ||
		!strings.HasPrefix(got.Artifacts[0].URI, "http://artifacts.test/files/") ||
		got.SourceMainImage != "https://img.alicdn.com/main.jpg" ||
		got.SelectedSKUID != "200" || got.SelectedOptions != "红色;M" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.SelectedVariantImages) != 1 ||
		!strings.HasPrefix(got.SelectedVariantImages[0], "http://artifacts.test/files/") ||
		len(got.SourceSelectedVariantImages) != 1 ||
		got.SourceSelectedVariantImages[0] != "https://img.alicdn.com/red.jpg" {
		t.Fatalf("variant images = %v", got.SelectedVariantImages)
	}
	if len(got.Images) != 3 || got.Images[0] != got.Artifacts[0].URI ||
		len(got.SourceImages) != 3 ||
		got.SourceImages[1] != "https://img.alicdn.com/red.jpg" ||
		got.SourceImages[2] != "https://img.alicdn.com/gallery.jpg" {
		t.Fatalf("images = %v", got.Images)
	}
	if got.Product["title"] != "红色衬衫" || got.Product["price"] != "199.00" {
		t.Fatalf("product business fields = %+v", got.Product)
	}
	for _, removed := range []string{"input", "_detailFetched", "resolvedUrl", "descriptionHtml"} {
		if _, ok := got.Product[removed]; ok {
			t.Errorf("product must omit service field %q: %+v", removed, got.Product)
		}
	}
}

func TestTaobaoActorItem(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "tmall detail URL uses numeric item ID",
			raw:  "https://detail.tmall.com/item.htm?id=1033676019099&skuId=6215038590577",
			want: "1033676019099",
		},
		{
			name: "short URL remains intact",
			raw:  "https://e.tb.cn/h.example?tk=abc",
			want: "https://e.tb.cn/h.example?tk=abc",
		},
		{
			name: "non-numeric ID remains intact",
			raw:  "https://item.taobao.com/item.htm?id=not-a-number",
			want: "https://item.taobao.com/item.htm?id=not-a-number",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, err := url.Parse(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := taobaoActorItem(target); got != test.want {
				t.Fatalf("taobaoActorItem() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFetchAmazonProductDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/acts/junglee~amazon-crawler/run-sync-get-dataset-items" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Has("maxItems") {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		var input struct {
			URLs []struct {
				URL string `json:"url"`
			} `json:"categoryOrProductUrls"`
			MaxItems int `json:"maxItemsPerStartUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.URLs) != 1 || input.MaxItems != 1 {
			t.Errorf("input = %+v", input)
		}
		_, _ = w.Write([]byte(`[{
			"input":"service-input",
			"asin":"B09X7MPX8L",
			"title":"Example product",
			"price":{"value":12.5,"currency":"USD"},
			"highResolutionImages":["https://m.media-amazon.com/high.jpg","https://m.media-amazon.com/high.jpg","https://m.media-amazon.com/side.jpg"],
			"thumbnailImage":"https://m.media-amazon.com/thumb.jpg"
		}]`))
	}))
	t.Cleanup(srv.Close)

	got := runProductDetail(t, productClientForServer(t, srv),
		"https://www.amazon.com/dp/B09X7MPX8L")
	if got.Platform != "amazon" || len(got.Artifacts) != len(got.Images) ||
		!strings.HasPrefix(got.Artifacts[0].URI, "http://artifacts.test/files/") ||
		got.SourceMainImage != "https://m.media-amazon.com/high.jpg" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.Images) != 2 || len(got.SourceImages) != 2 ||
		got.SourceImages[1] != "https://m.media-amazon.com/side.jpg" {
		t.Fatalf("images = %v", got.Images)
	}
	if len(got.SelectedVariantImages) != 0 || got.Product["title"] != "Example product" {
		t.Fatalf("product detail = %+v", got)
	}
	limited, err := productClientForServer(t, srv).FetchProductDetail(
		context.Background(),
		ProductDetailInput{ProductURL: "https://www.amazon.com/dp/B09X7MPX8L", MaxImages: 1},
		"limited",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited.Images) != 1 || len(limited.SourceImages) != 1 {
		t.Fatalf("maxImages=1 result = %+v", limited)
	}
}

func TestImportProductImagesKeepsSourceIndexesAligned(t *testing.T) {
	imageClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := []byte("\x89PNG\r\n\x1a\nimage")
		if strings.Contains(req.URL.Path, "side") {
			status = http.StatusBadGateway
			body = nil
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(body)),
		}, nil
	})}
	out := ProductDetailOut{
		Platform: "amazon",
		SourceImages: []string{
			"https://m.media-amazon.com/main.jpg",
			"https://m.media-amazon.com/side.jpg",
		},
		SelectedVariantImages: []string{"https://m.media-amazon.com/side.jpg"},
	}
	err := importProductImages(context.Background(), &out, 2, ApifyConfig{
		ImageClient: imageClient,
		Artifacts:   artifact.NewLocalStorage(t.TempDir(), "http://artifacts.test"),
	}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Images) != 1 || len(out.SourceImages) != 1 ||
		out.SourceImages[0] != "https://m.media-amazon.com/main.jpg" ||
		len(out.SelectedVariantImages) != 0 || len(out.SourceSelectedVariantImages) != 0 {
		t.Fatalf("managed/source images are not aligned: %+v", out)
	}
}

func TestProductDetailAmazonFallbacks(t *testing.T) {
	product := productDetail{
		ThumbnailImage:    "thumb",
		GalleryThumbnails: []string{"gallery"},
	}
	got, err := productDetailOutput("amazon", "https://amazon.com/dp/x", product)
	if err != nil {
		t.Fatal(err)
	}
	if got.MainImage != "thumb" {
		t.Fatalf("main image = %q", got.MainImage)
	}
	product.ThumbnailImage = ""
	got, err = productDetailOutput("amazon", "https://amazon.com/dp/x", product)
	if err != nil {
		t.Fatal(err)
	}
	if got.MainImage != "gallery" {
		t.Fatalf("main image = %q", got.MainImage)
	}
}

func TestProductDetailScopeAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	client := productClientForServer(t, srv)

	for _, productURL := range []string{
		"https://example.com/product/1",
		"https://www.amazon.com/s?k=keyboard",
		"file:///tmp/product.json",
		"",
	} {
		if _, err := client.FetchProductDetail(
			context.Background(), ProductDetailInput{ProductURL: productURL}, "test",
		); err == nil {
			t.Errorf("expected scope error for %q", productURL)
		}
	}
	if _, err := client.FetchProductDetail(context.Background(), ProductDetailInput{
		ProductURL: "https://detail.tmall.com/item.htm?id=100",
	}, "test"); err == nil ||
		!strings.Contains(err.Error(), "returned no product") {
		t.Fatalf("empty dataset error = %v", err)
	}
}

func TestProductDetailManifestAndClientValidation(t *testing.T) {
	manifest := ManifestCapability()
	if manifest.ID != ProductDetailCapabilityID ||
		manifest.TemporalType != ProductDetailActivityName ||
		manifest.Tool == nil ||
		manifest.Tool.MaxAttempts != 1 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if !json.Valid(manifest.InputSchema) || !json.Valid(manifest.OutputSchema) {
		t.Fatalf("schemas = %s / %s", manifest.InputSchema, manifest.OutputSchema)
	}
	storage := artifact.NewLocalStorage(t.TempDir(), "http://artifacts.test")
	if _, err := NewProductDetailClient(ApifyConfig{}); err == nil {
		t.Fatal("missing token and actors must fail")
	}
	client, err := NewProductDetailClient(ApifyConfig{
		Token: "token", TaobaoActor: "taobao~detail", AmazonActor: "amazon~detail",
		Timeout: 12 * time.Second, Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.(*apifyProductDetailClient).timeout != 12*time.Second {
		t.Fatalf("timeout = %s", client.(*apifyProductDetailClient).timeout)
	}
}

// TestProductDetailLive 是显式启用的真实 Actor 回归测试。默认跳过，避免普通单测
// 消耗外部服务额度；发布接入变更时用 APIFY_API_TOKEN 单独运行。
func TestProductDetailLive(t *testing.T) {
	token := os.Getenv("APIFY_API_TOKEN")
	if token == "" {
		t.Skip("APIFY_API_TOKEN is not set")
	}
	client, err := NewProductDetailClient(ApifyConfig{
		Token: token, TaobaoActor: "zen-studio~taobao-detail-scraper",
		AmazonActor: "junglee~amazon-crawler",
		Artifacts:   artifact.NewLocalStorage(t.TempDir(), "http://artifacts.test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, productURL := range []string{
		"https://item.taobao.com/item.htm?id=899406464301&skuId=5921120335009",
		"https://www.amazon.sg/Panini-Marvel-Anthology-Trading-Treasure/dp/B0FYPMBZB6",
	} {
		t.Run(productURL, func(t *testing.T) {
			got := runProductDetail(t, client, productURL)
			if got.Product["title"] == nil || len(got.Artifacts) == 0 ||
				got.Artifacts[0].URI == "" || len(got.Images) == 0 {
				t.Fatalf("incomplete live product detail: %+v", got)
			}
			for _, removed := range []string{"input", "inputType", "scrapedAt", "descriptionHtml"} {
				if _, ok := got.Product[removed]; ok {
					t.Fatalf("live product contains service field %q", removed)
				}
			}
		})
	}
}
