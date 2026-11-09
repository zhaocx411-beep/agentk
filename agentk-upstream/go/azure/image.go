package azure

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	upstream "git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/gateway"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/clientconfig"
	"git.corp.kuaishou.com/klingai-agentk/agentk-upstream.git/go/internal/httpcall"
)

type ImageClient struct {
	base string
	http *http.Client
}

func NewImage(r gateway.Route, opts ...upstream.Option) (*ImageClient, error) {
	base, c, err := clientconfig.New(r, opts...)
	if err != nil {
		return nil, err
	}
	return &ImageClient{strings.TrimRight(base.String(), "/"), c}, nil
}

type ImageFile struct {
	Data     []byte
	Filename string
	MIME     string
}
type ImageRequest struct {
	APIVersion, Prompt, Size, Quality string
	Images                            []ImageFile
}
type Image struct {
	Data []byte
	URL  string
}

func (c *ImageClient) Generate(ctx context.Context, req ImageRequest) (Image, error) {
	raw, resp, err := httpcall.JSON(
		ctx, c.http, http.MethodPost,
		c.base+"/images/generations?api-version="+url.QueryEscape(req.APIVersion),
		struct {
			Prompt  string `json:"prompt"`
			Size    string `json:"size"`
			Quality string `json:"quality"`
			N       int    `json:"n"`
		}{req.Prompt, req.Size, req.Quality, 1},
	)
	if err != nil {
		return Image{}, err
	}
	return decodeImage(raw, resp)
}
func (c *ImageClient) Edit(ctx context.Context, req ImageRequest) (Image, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, image := range req.Images {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="%s"`, image.Filename))
		header.Set("Content-Type", image.MIME)
		part, err := mw.CreatePart(header)
		if err != nil {
			return Image{}, err
		}
		if _, err = part.Write(image.Data); err != nil {
			return Image{}, err
		}
	}
	for _, field := range [][2]string{
		{"prompt", req.Prompt},
		{"size", req.Size},
		{"quality", req.Quality},
		{"n", "1"},
	} {
		if err := mw.WriteField(field[0], field[1]); err != nil {
			return Image{}, err
		}
	}
	if err := mw.Close(); err != nil {
		return Image{}, err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.base+"/images/edits?api-version="+url.QueryEscape(req.APIVersion),
		&body,
	)
	if err != nil {
		return Image{}, err
	}
	request.Header.Set("Content-Type", mw.FormDataContentType())
	raw, resp, err := httpcall.Do(c.http, request)
	if err != nil {
		return Image{}, err
	}
	return decodeImage(raw, resp)
}
func decodeImage(raw []byte, resp *httpcall.Response) (Image, error) {
	if resp.StatusCode != 200 {
		return Image{}, apiError(raw, resp)
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Image{}, err
	}
	if len(out.Data) == 0 {
		return Image{}, fmt.Errorf("no image in response: %s", httpcall.Snippet(raw))
	}
	item := out.Data[0]
	if item.B64 != "" {
		data, err := base64.StdEncoding.DecodeString(item.B64)
		return Image{Data: data}, err
	}
	if item.URL != "" {
		return Image{URL: item.URL}, nil
	}
	return Image{}, fmt.Errorf("no b64_json/url in response: %s", httpcall.Snippet(raw))
}
