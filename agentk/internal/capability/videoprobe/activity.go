package videoprobe

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"agentk/internal/artifact"
	"agentk/internal/netguard"
	"agentk/internal/temporalx"
)

// ActivityName is the public Temporal activity name.
const ActivityName = "agentk.capability.probe_video"

// ActivityInput contains only the public video probe parameters.
type ActivityInput struct {
	VideoURL string `json:"videoUrl"`
}

// Activity executes video metadata inspection.
type Activity struct {
	storage      artifact.Storage
	client       *http.Client
	allowPrivate bool
}

func NewActivity(storage artifact.Storage) *Activity {
	return &Activity{storage: storage, client: http.DefaultClient}
}

func (a *Activity) Run(ctx context.Context, in ActivityInput) (Output, error) {
	const op = "capability probe_video"
	videoURL := strings.TrimSpace(in.VideoURL)
	if videoURL == "" {
		return Output{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrl is required"))
	}
	parsed, err := url.Parse(videoURL)
	if err != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Output{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("videoUrl must be an absolute HTTP or HTTPS URL"))
	}

	var source videoSource
	if a.storage != nil {
		if object, ok := a.storage.Resolve(videoURL); ok {
			source = func(dst io.Writer) (int64, string, error) {
				reader, err := a.storage.Open(ctx, object)
				if err != nil {
					return 0, "", err
				}
				defer func() { _ = reader.Close() }()
				size, err := copyVideo(dst, reader)
				return size, mime.TypeByExtension(path.Ext(object.Key)), err
			}
		}
	}
	if source == nil {
		client := a.client
		if client == nil {
			client = http.DefaultClient
		}
		if !a.allowPrivate {
			if _, err := netguard.ValidatePublicHTTPURL(ctx, videoURL); err != nil {
				return Output{}, temporalx.ClassifyActivityError(op,
					temporalx.InvalidActivityInput("unsafe videoUrl: %v", err))
			}
			clone := *client
			clone.CheckRedirect = netguard.PublicRedirectPolicy(client.CheckRedirect)
			client = &clone
		}
		source = func(dst io.Writer) (int64, string, error) {
			return downloadVideo(ctx, client, videoURL, dst)
		}
	}

	out, err := probe(ctx, videoURL, source)
	if err != nil {
		return Output{}, temporalx.ClassifyActivityError(op, err)
	}
	return out, nil
}
