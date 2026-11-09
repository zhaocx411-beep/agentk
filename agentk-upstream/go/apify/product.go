package apify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type Product struct {
	ASIN                 string            `json:"asin"`
	HighResolutionImages []string          `json:"highResolutionImages"`
	ThumbnailImage       string            `json:"thumbnailImage"`
	GalleryThumbnails    []string          `json:"galleryThumbnails"`
	MainPictureURL       string            `json:"mainPictureUrl"`
	Pictures             []string          `json:"pictures"`
	SKUs                 []SKU             `json:"skus"`
	PropsImages          map[string]string `json:"propsImages"`
	ResolvedURL          string            `json:"resolvedUrl"`
	Error                string            `json:"error"`
	ErrorDescription     string            `json:"errorDescription"`
	Raw                  map[string]any    `json:"-"`
}

type SKU struct {
	SKUID      json.RawMessage `json:"skuId"`
	PropsIDs   string          `json:"propsIds"`
	PropsNames string          `json:"propsNames"`
}

func (p *Product) UnmarshalJSON(data []byte) error {
	type productAlias Product
	var decoded productAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = Product(decoded)
	p.Raw = raw
	return nil
}

type ProductClient struct {
	base *url.URL
	http *http.Client
}

func NewProduct(r gateway.Route, opts ...upstream.Option) (*ProductClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &ProductClient{base, c}, nil
}

type TaobaoRequest struct {
	Actor string
	Items []string
}
type AmazonRequest struct {
	Actor, URL                                       string
	MaxItems, MaxSearchPages, MaxVariants, MaxOffers int
	ScrapeDetails                                    bool
}

func (c *ProductClient) Taobao(ctx context.Context, req TaobaoRequest) ([]Product, error) {
	return c.run(ctx, req.Actor, struct {
		Items []string `json:"items"`
	}{req.Items})
}
func (c *ProductClient) Amazon(ctx context.Context, req AmazonRequest) ([]Product, error) {
	body := struct {
		URLs           []map[string]string `json:"categoryOrProductUrls"`
		MaxItems       int                 `json:"maxItemsPerStartUrl"`
		MaxSearchPages int                 `json:"maxSearchPagesPerStartUrl"`
		MaxVariants    int                 `json:"maxProductVariantsAsSeparateResults"`
		MaxOffers      int                 `json:"maxOffers"`
		ScrapeDetails  bool                `json:"scrapeProductDetails"`
	}{
		[]map[string]string{{"url": req.URL}},
		req.MaxItems,
		req.MaxSearchPages,
		req.MaxVariants,
		req.MaxOffers,
		req.ScrapeDetails,
	}
	return c.run(ctx, req.Actor, body)
}
func (c *ProductClient) run(ctx context.Context, actor string, input any) ([]Product, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	endpoint := *c.base
	endpoint.Path = strings.TrimRight(
		endpoint.Path,
		"/",
	) + "/v2/acts/" + url.PathEscape(actor) + "/run-sync-get-dataset-items"
	query := endpoint.Query()
	query.Set("clean", "true")
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	raw, resp, err := httpcall.Do(c.http, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, httpcall.Error(resp, "", httpcall.Snippet(raw))
	}
	var out []Product
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	for _, p := range out {
		if p.Error != "" {
			message := p.Error
			if p.ErrorDescription != "" {
				message += ": " + p.ErrorDescription
			}
			return nil, httpcall.Error(resp, p.Error, message)
		}
	}
	return out, nil
}
