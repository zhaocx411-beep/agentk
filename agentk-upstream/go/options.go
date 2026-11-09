package upstream

import (
	"net/http"

	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
)

type Option = clientconfig.Option

func WithHTTPClient(client *http.Client) Option {
	return func(o *clientconfig.Options) { o.HTTPClient = client }
}
