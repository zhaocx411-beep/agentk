package tts

import (
	"context"
	"fmt"
)

type voiceRestrictedClient struct {
	client  Client
	allowed map[string]struct{}
}

// RestrictVoices binds a provider client to the voices declared for its model entry.
func RestrictVoices(client Client, voices []VoiceOption) Client {
	allowed := make(map[string]struct{}, len(voices))
	for _, voice := range voices {
		allowed[voice.ID] = struct{}{}
	}
	return &voiceRestrictedClient{client: client, allowed: allowed}
}

func (c *voiceRestrictedClient) ValidateRequest(req Request) error {
	if _, ok := c.allowed[req.VoiceID]; !ok {
		return fmt.Errorf("tts: voiceId %q is not configured for the selected model", req.VoiceID)
	}
	return c.client.ValidateRequest(req)
}

func (c *voiceRestrictedClient) Synthesize(ctx context.Context, req Request) (Audio, error) {
	if err := c.ValidateRequest(req); err != nil {
		return Audio{}, err
	}
	return c.client.Synthesize(ctx, req)
}
