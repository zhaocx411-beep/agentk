package azure

import (
	"encoding/json"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

func apiError(raw []byte, resp *httpcall.Response) *upstream.APIError {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	message := body.Error.Message
	if message == "" {
		message = httpcall.Snippet(raw)
	}
	return httpcall.Error(resp, body.Error.Code, message)
}
