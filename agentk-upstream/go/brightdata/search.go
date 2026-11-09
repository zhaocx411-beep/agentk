package brightdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type SearchClient struct {
	endpoint string
	http     *http.Client
}

func NewSearch(r gateway.Route, opts ...upstream.Option) (*SearchClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &SearchClient{base.String(), c}, nil
}

type Request struct{ Query, Zone, Language, Country, TimeRange string }
type Result struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
}

func (c *SearchClient) Search(ctx context.Context, req Request) ([]Result, error) {
	// Bright Data requires q before the remaining query parameters.
	target := "https://www.google.com/search?q=" + url.QueryEscape(req.Query) +
		"&hl=" + url.QueryEscape(req.Language) + "&gl=" + url.QueryEscape(req.Country)
	if req.TimeRange != "" {
		target += "&tbs=" + url.QueryEscape(req.TimeRange)
	}
	target += "&brd_json=1"
	body := struct {
		Zone   string `json:"zone"`
		URL    string `json:"url"`
		Format string `json:"format"`
	}{req.Zone, target, "raw"}
	raw, resp, err := httpcall.JSON(ctx, c.http, http.MethodPost, c.endpoint, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, httpcall.Error(resp, "", httpcall.Snippet(raw))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("upstream returned an empty body (the fetch failed on their side)")
	}
	var out struct {
		Organic []Result `json:"organic"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w; body: %s", err, httpcall.Snippet(raw))
	}
	return out.Organic, nil
}
