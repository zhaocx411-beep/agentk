package volcsearch

import (
	"context"
	"encoding/json"
	"net/http"

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

type Request struct {
	Query       string
	Count       int
	TimeRange   string
	NeedSummary bool
}
type Result struct {
	Title   string `json:"Title"`
	URL     string `json:"Url"`
	Summary string `json:"Summary"`
	Snippet string `json:"Snippet"`
}

func (c *SearchClient) Search(ctx context.Context, req Request) ([]Result, error) {
	body := struct {
		Query       string
		SearchType  string
		Count       int
		NeedSummary bool
		Filter      struct {
			NeedUrl     bool
			NeedContent bool
		}
		TimeRange string `json:",omitempty"`
	}{Query: req.Query, SearchType: "web", Count: req.Count, NeedSummary: req.NeedSummary, TimeRange: req.TimeRange}
	body.Filter.NeedUrl = true
	raw, resp, err := httpcall.JSON(ctx, c.http, http.MethodPost, c.endpoint, body)
	if err != nil {
		return nil, err
	}
	var out struct {
		ResponseMetadata struct {
			RequestID string
			Error     *struct{ Code, Message string }
		}
		Result struct{ WebResults []Result }
	}
	err = json.Unmarshal(raw, &out)
	detail := out.ResponseMetadata.Error
	if resp.StatusCode != 200 || (err == nil && detail != nil && (detail.Code != "" || detail.Message != "")) {
		var code, message string
		if detail != nil {
			code, message = detail.Code, detail.Message
		}
		if message == "" {
			message = httpcall.Snippet(raw)
		}
		failure := httpcall.Error(resp, code, message)
		if out.ResponseMetadata.RequestID != "" {
			failure.RequestID = out.ResponseMetadata.RequestID
		}
		return nil, failure
	}
	if err != nil {
		return nil, err
	}
	return out.Result.WebResults, nil
}
