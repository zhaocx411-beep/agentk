package webfetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agentk/internal/artifact"
	"agentk/internal/tool"
)

// ProductDetailToolName 是淘宝/天猫与 Amazon 商品详情页结构化抓取工具名。
const ProductDetailToolName = "fetch_product_detail"

const (
	defaultApifyBaseURL  = "https://api.apify.com"
	defaultApifyTimeout  = 310 * time.Second
	maxApifyBodyBytes    = 8 << 20
	defaultMaxImages     = 6
	absoluteMaxImages    = 20
	maxProductImageBytes = 15 << 20
)

// ApifyConfig 是商品详情抓取工具的 Apify 接入配置。凭证来自 models catalog，
// 不进入可编辑、可回显的 profile。
type ApifyConfig struct {
	BaseURL     string
	Token       string
	TaobaoActor string
	AmazonActor string
	Timeout     time.Duration
	Client      *http.Client
	// ImageClient 仅供测试注入；生产环境使用带重定向校验的独立客户端。
	ImageClient *http.Client
	Artifacts   artifact.Storage
}

// ProductDetailOut 在保留平台网页业务字段的同时，提供一组跨平台稳定字段。
// Product 已过滤 Apify 调用元数据、内部状态键和重复的 descriptionHtml；
// Images 始终以主图开头并去重，可直接交给下游媒体工具。
type ProductDetailOut struct {
	Platform                    string                `json:"platform"`
	ProductURL                  string                `json:"productUrl"`
	SourceMainImage             string                `json:"sourceMainImage"`
	SourceImages                []string              `json:"sourceImages"`
	MainImage                   string                `json:"-"`
	Artifacts                   []tool.ResultArtifact `json:"artifacts"`
	Images                      []string              `json:"images"`
	SelectedSKUID               string                `json:"selectedSkuId,omitempty"`
	SelectedOptions             string                `json:"selectedOptions,omitempty"`
	SourceSelectedVariantImages []string              `json:"sourceSelectedVariantImages,omitempty"`
	SelectedVariantImages       []string              `json:"selectedVariantImages,omitempty"`
	Product                     map[string]any        `json:"product"`
}

type productDetail struct {
	ASIN                 string            `json:"asin"`
	HighResolutionImages []string          `json:"highResolutionImages"`
	ThumbnailImage       string            `json:"thumbnailImage"`
	GalleryThumbnails    []string          `json:"galleryThumbnails"`
	MainPictureURL       string            `json:"mainPictureUrl"`
	Pictures             []string          `json:"pictures"`
	SKUs                 []productSKU      `json:"skus"`
	PropsImages          map[string]string `json:"propsImages"`
	ResolvedURL          string            `json:"resolvedUrl"`
	Error                string            `json:"error"`
	ErrorDescription     string            `json:"errorDescription"`
	Raw                  map[string]any    `json:"-"`
}

type productSKU struct {
	SKUID      json.RawMessage `json:"skuId"`
	PropsIDs   string          `json:"propsIds"`
	PropsNames string          `json:"propsNames"`
}

func (p *productDetail) UnmarshalJSON(data []byte) error {
	type productDetailAlias productDetail
	var decoded productDetailAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = productDetail(decoded)
	p.Raw = raw
	return nil
}

type apifyProductDetailClient struct {
	cfg     ApifyConfig
	base    *url.URL
	client  *http.Client
	timeout time.Duration
}

type apifyStatusError struct {
	status int
	body   string
}

func (e *apifyStatusError) Error() string {
	return fmt.Sprintf("apify actor returned status %d: %s", e.status, e.body)
}

func (e *apifyStatusError) StatusCode() int { return e.status }

func NewProductDetailClient(cfg ApifyConfig) (ProductDetailClient, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("product_detail: apify token is required")
	}
	if strings.TrimSpace(cfg.TaobaoActor) == "" || strings.TrimSpace(cfg.AmazonActor) == "" {
		return nil, fmt.Errorf("product_detail: taobao and amazon actors are required")
	}
	if cfg.Artifacts == nil {
		return nil, fmt.Errorf("product_detail: artifact storage is required")
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultApifyBaseURL
	}
	base, err := validateTarget(baseURL)
	if err != nil {
		return nil, fmt.Errorf("product_detail: apify base url: %w", err)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultApifyTimeout
	}
	client := cfg.Client
	if client == nil {
		client, err = newHTTPClient("")
		if err != nil {
			return nil, err
		}
	}
	return &apifyProductDetailClient{
		cfg: cfg, base: base, client: client, timeout: timeout,
	}, nil
}

func (c *apifyProductDetailClient) FetchProductDetail(
	ctx context.Context,
	input ProductDetailInput,
	idempotencyKey string,
) (ProductDetailOut, error) {
	if err := validateProductDetailInput(input); err != nil {
		return ProductDetailOut{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	maxImages := input.MaxImages
	if maxImages == 0 {
		maxImages = defaultMaxImages
	}
	target, err := validateTarget(input.ProductURL)
	if err != nil {
		return ProductDetailOut{}, fmt.Errorf("%s: %w", ProductDetailToolName, err)
	}
	platform, actor, actorInput, err := productActorInput(target, c.cfg)
	if err != nil {
		return ProductDetailOut{}, fmt.Errorf("%s: %w", ProductDetailToolName, err)
	}
	product, err := runApifyActor(ctx, c.client, c.base, c.cfg.Token, actor, actorInput)
	if err != nil {
		return ProductDetailOut{}, fmt.Errorf("%s: %w", ProductDetailToolName, err)
	}
	out, err := productDetailOutput(platform, target.String(), product)
	if err != nil {
		return ProductDetailOut{}, fmt.Errorf("%s: %w", ProductDetailToolName, err)
	}
	if err := importProductImages(ctx, &out, maxImages, c.cfg, idempotencyKey); err != nil {
		return ProductDetailOut{}, fmt.Errorf("%s: %w", ProductDetailToolName, err)
	}
	out.Artifacts = make([]tool.ResultArtifact, 0, len(out.Images))
	for i, image := range out.Images {
		description := fmt.Sprintf("使用 %s 从 %s 商品页 %s 导入的第 %d 张商品图片",
			ProductDetailToolName, out.Platform, out.ProductURL, i+1)
		if i == 0 {
			description = fmt.Sprintf("使用 %s 从 %s 商品页 %s 导入的商品主图",
				ProductDetailToolName, out.Platform, out.ProductURL)
		}
		out.Artifacts = append(out.Artifacts, tool.ResultArtifact{
			Type: "image", URI: image, Description: description,
		})
	}
	return out, nil
}

func productActorInput(target *url.URL, cfg ApifyConfig) (platform, actor string, input any, err error) {
	platform = productPlatform(target.Hostname())
	switch platform {
	case "taobao", "tmall":
		return platform, cfg.TaobaoActor, map[string]any{
			// Tmall/Taobao 直链可能因 Actor 出口地区不同而重定向到登录页。Actor 支持
			// 直接传数字商品 ID，既能避开该重定向，也能返回完整的 SKU 矩阵。
			"items": []string{taobaoActorItem(target)},
		}, nil
	case "amazon":
		if !amazonProductURL(target) {
			return "", "", nil, fmt.Errorf(
				"amazon URL must be a product detail page (for example /dp/ASIN); search and listing pages are not supported")
		}
		return platform, cfg.AmazonActor, map[string]any{
			"categoryOrProductUrls":               []map[string]string{{"url": target.String()}},
			"maxItemsPerStartUrl":                 1,
			"maxSearchPagesPerStartUrl":           1,
			"maxProductVariantsAsSeparateResults": 0,
			"maxOffers":                           0,
			"scrapeProductDetails":                true,
		}, nil
	default:
		return "", "", nil, fmt.Errorf(
			"unsupported host %q; only Taobao, Tmall and Amazon product pages are supported",
			target.Hostname())
	}
}

func taobaoActorItem(target *url.URL) string {
	itemID := strings.TrimSpace(target.Query().Get("id"))
	if itemID == "" {
		return target.String()
	}
	for _, r := range itemID {
		if r < '0' || r > '9' {
			return target.String()
		}
	}
	return itemID
}

func amazonProductURL(target *url.URL) bool {
	path := strings.ToLower(target.EscapedPath())
	return strings.Contains(path, "/dp/") ||
		strings.Contains(path, "/gp/product/") ||
		strings.Contains(path, "/gp/aw/d/")
}

func productPlatform(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	switch {
	case host == "taobao.com" || strings.HasSuffix(host, ".taobao.com") ||
		host == "tb.cn" || strings.HasSuffix(host, ".tb.cn"):
		return "taobao"
	case host == "tmall.com" || strings.HasSuffix(host, ".tmall.com"):
		return "tmall"
	}
	for suffix := range amazonSuffixes {
		if host == "amazon."+suffix || strings.HasSuffix(host, ".amazon."+suffix) {
			return "amazon"
		}
	}
	return ""
}

var amazonSuffixes = map[string]struct{}{
	"com": {}, "ca": {}, "com.mx": {}, "com.br": {}, "co.uk": {},
	"de": {}, "fr": {}, "it": {}, "es": {}, "nl": {}, "se": {},
	"pl": {}, "com.be": {}, "co.jp": {}, "in": {}, "com.au": {},
	"sg": {}, "ae": {}, "sa": {}, "eg": {}, "com.tr": {},
}

func runApifyActor(
	ctx context.Context,
	client *http.Client,
	base *url.URL,
	token, actor string,
	input any,
) (productDetail, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return productDetail{}, fmt.Errorf("encode actor input: %w", err)
	}
	endpoint := *base
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") +
		"/v2/acts/" + url.PathEscape(actor) + "/run-sync-get-dataset-items"
	query := endpoint.Query()
	query.Set("clean", "true")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return productDetail{}, fmt.Errorf("build apify request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return productDetail{}, fmt.Errorf("call apify actor: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxApifyBodyBytes))
	if readErr != nil {
		return productDetail{}, fmt.Errorf("read apify response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return productDetail{}, &apifyStatusError{
			status: resp.StatusCode,
			body:   strings.TrimSpace(string(raw)),
		}
	}
	var products []productDetail
	if err := json.Unmarshal(raw, &products); err != nil {
		return productDetail{}, fmt.Errorf("decode apify dataset items: %w", err)
	}
	if len(products) == 0 {
		return productDetail{}, fmt.Errorf(
			"apify actor returned no product; the item may be unavailable, login-only, region-restricted, or temporarily blocked")
	}
	if len(products) > 1 {
		return productDetail{}, fmt.Errorf(
			"apify actor returned %d products; provide one product detail URL, not a search or listing URL",
			len(products))
	}
	if products[0].Error != "" {
		message := products[0].Error
		if products[0].ErrorDescription != "" {
			message += ": " + products[0].ErrorDescription
		}
		return productDetail{}, fmt.Errorf("apify actor failed: %s", message)
	}
	return products[0], nil
}

func productDetailOutput(platform, productURL string, product productDetail) (ProductDetailOut, error) {
	out := ProductDetailOut{
		Platform: platform, ProductURL: productURL,
		Images: []string{}, Product: cleanProductFields(product.Raw),
	}
	if platform == "amazon" || product.ASIN != "" {
		images := amazonImages(product)
		if len(images) == 0 {
			return ProductDetailOut{}, fmt.Errorf("no image returned for %q", productURL)
		}
		out.Platform = "amazon"
		out.SourceMainImage = images[0]
		out.SourceImages = append([]string(nil), images...)
		out.MainImage = images[0]
		out.Images = images
		return out, nil
	}

	mainImage := product.MainPictureURL
	if mainImage == "" && len(product.Pictures) > 0 {
		mainImage = product.Pictures[0]
	}
	mainImage = absoluteMediaURL(mainImage)
	if mainImage == "" {
		return ProductDetailOut{}, fmt.Errorf("no image returned for %q", productURL)
	}
	out.SourceMainImage = mainImage
	out.MainImage = mainImage
	out.Images = appendUniqueImages(out.Images, mainImage)
	skuID := skuIDFromURL(productURL)
	if skuID == "" {
		skuID = skuIDFromURL(product.ResolvedURL)
	}
	if skuID != "" {
		var selected *productSKU
		for i := range product.SKUs {
			if skuIDString(product.SKUs[i].SKUID) == skuID {
				selected = &product.SKUs[i]
				break
			}
		}
		if selected == nil {
			return ProductDetailOut{}, fmt.Errorf(
				"sku %q selected by URL was not found in product detail", skuID)
		}
		for _, propID := range strings.Split(selected.PropsIDs, ";") {
			image := absoluteMediaURL(product.PropsImages[propID])
			out.SelectedVariantImages = appendUniqueImages(out.SelectedVariantImages, image)
			// 选中 SKU 的图片排在普通画廊之前，避免 maxImages 截掉用户明确选择的规格图。
			out.Images = appendUniqueImages(out.Images, image)
		}
		out.SelectedSKUID = skuID
		out.SelectedOptions = selected.PropsNames
	}
	for _, image := range product.Pictures {
		out.Images = appendUniqueImages(out.Images, image)
	}
	out.SourceImages = append([]string(nil), out.Images...)
	return out, nil
}

func cleanProductFields(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		switch key {
		case "input", "inputType", "resolvedUrl", "unNormalizedProductUrl",
			"scrapedAt", "descriptionHtml", "error", "errorDescription":
			continue
		}
		if strings.HasPrefix(key, "_") {
			continue
		}
		out[key] = value
	}
	return out
}

func importProductImages(
	ctx context.Context,
	out *ProductDetailOut,
	maxImages int,
	cfg ApifyConfig,
	idempotencyKey string,
) error {
	sources := out.SourceImages
	if len(sources) == 0 {
		sources = out.Images
	}
	if len(sources) > maxImages {
		sources = sources[:maxImages]
	}
	selectedSources := append([]string(nil), out.SelectedVariantImages...)

	managedBySource := make(map[string]string, len(sources))
	managed := make([]string, 0, len(sources))
	successfulSources := make([]string, 0, len(sources))
	for i, source := range sources {
		data, mimeType, err := downloadProductImage(ctx, cfg.ImageClient, out.Platform, source)
		if err != nil {
			if i == 0 {
				return fmt.Errorf("download main product image: %w", err)
			}
			continue
		}
		object, err := cfg.Artifacts.Put(
			ctx, productImageKey(idempotencyKey, source, i, mimeType), data, mimeType)
		if err != nil {
			if i == 0 {
				return fmt.Errorf("store main product image: %w", err)
			}
			continue
		}
		managedBySource[source] = object.URL
		managed = append(managed, object.URL)
		successfulSources = append(successfulSources, source)
	}
	if len(managed) == 0 {
		return fmt.Errorf("no product image could be imported")
	}
	out.SourceMainImage = successfulSources[0]
	out.SourceImages = successfulSources
	out.MainImage = managed[0]
	out.Images = managed
	out.SourceSelectedVariantImages = nil
	out.SelectedVariantImages = nil
	for _, source := range selectedSources {
		if managedURL := managedBySource[source]; managedURL != "" {
			out.SourceSelectedVariantImages = append(out.SourceSelectedVariantImages, source)
			out.SelectedVariantImages = append(out.SelectedVariantImages, managedURL)
		}
	}
	return nil
}

func downloadProductImage(
	ctx context.Context,
	injected *http.Client,
	platform, rawURL string,
) ([]byte, string, error) {
	target, err := validateProductImageURL(platform, rawURL)
	if err != nil {
		return nil, "", err
	}
	client := injected
	if client == nil {
		client, err = newHTTPClient("")
		if err != nil {
			return nil, "", err
		}
	}
	safeClient := *client
	previousRedirect := safeClient.CheckRedirect
	safeClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if _, err := validateProductImageURL(platform, req.URL.String()); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxProductImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxProductImageBytes {
		return nil, "", fmt.Errorf("image exceeds %d bytes", maxProductImageBytes)
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, "", fmt.Errorf("response is not an image (detected %s)", mimeType)
	}
	return data, mimeType, nil
}

func validateProductImageURL(platform, rawURL string) (*url.URL, error) {
	target, err := validateTarget(rawURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	allowed := false
	switch platform {
	case "taobao", "tmall":
		allowed = host == "alicdn.com" || strings.HasSuffix(host, ".alicdn.com") ||
			host == "tbcdn.cn" || strings.HasSuffix(host, ".tbcdn.cn") ||
			host == "taobaocdn.com" || strings.HasSuffix(host, ".taobaocdn.com")
	case "amazon":
		allowed = host == "media-amazon.com" || strings.HasSuffix(host, ".media-amazon.com") ||
			host == "images-amazon.com" || strings.HasSuffix(host, ".images-amazon.com") ||
			host == "ssl-images-amazon.com" || strings.HasSuffix(host, ".ssl-images-amazon.com")
	}
	if !allowed {
		return nil, fmt.Errorf("image host %q is not allowed for platform %s", host, platform)
	}
	return target, nil
}

func productImageKey(idempotencyKey, source string, index int, mimeType string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%d", idempotencyKey, source, index)))
	extension := ".img"
	switch mimeType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	case "image/gif":
		extension = ".gif"
	case "image/webp":
		extension = ".webp"
	}
	return fmt.Sprintf("capabilities/product-images/%x%s", sum[:16], extension)
}

func appendUniqueImages(images []string, raw string) []string {
	image := absoluteMediaURL(raw)
	if image != "" && !contains(images, image) {
		return append(images, image)
	}
	return images
}

func amazonImages(product productDetail) []string {
	var images []string
	for _, image := range product.HighResolutionImages {
		images = appendUniqueImages(images, image)
	}
	if len(images) == 0 && product.ThumbnailImage != "" {
		images = appendUniqueImages(images, product.ThumbnailImage)
	}
	if len(images) == 0 {
		for _, image := range product.GalleryThumbnails {
			images = appendUniqueImages(images, image)
		}
	}
	return images
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func skuIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if value := u.Query().Get("skuId"); value != "" {
		return value
	}
	return u.Query().Get("sku_id")
}

func skuIDString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	return strings.Trim(string(raw), `"`)
}

func absoluteMediaURL(raw string) string {
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}
