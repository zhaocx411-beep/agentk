package anthropic

import (
	"fmt"
	"math"
	"slices"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
)

type Config struct {
	MaxTokens int64
	// Empty disables automatic prefix caching; supported TTLs are 5m and 1h.
	CacheTTL    string
	Temperature *float64
	Effort      string
}

func (c Config) validate() error {
	if c.MaxTokens <= 0 {
		return fmt.Errorf("maxTokens must be positive")
	}
	if !slices.Contains([]string{"", "5m", "1h"}, c.CacheTTL) {
		return fmt.Errorf("cache TTL must be 5m or 1h")
	}
	if c.Temperature != nil && (math.IsNaN(*c.Temperature) || *c.Temperature < 0 || *c.Temperature > 1) {
		return fmt.Errorf("temperature must be between 0 and 1")
	}
	if !slices.Contains([]string{"", "low", "medium", "high", "xhigh", "max"}, c.Effort) {
		return fmt.Errorf("invalid effort %q", c.Effort)
	}
	return nil
}

func (c Config) apply(model string, p *sdk.MessageNewParams) error {
	if c.Effort != "" && strings.HasPrefix(model, "claude-haiku-4-5") {
		return fmt.Errorf("%s does not support effort", model)
	}
	if c.Temperature != nil {
		adaptive := strings.HasPrefix(model, "claude-sonnet-5") || strings.HasPrefix(model, "claude-sonnet-4-6")
		if adaptive && *c.Temperature != 1 {
			return fmt.Errorf("%s only accepts temperature 1", model)
		}
		// Some adaptive-thinking gateways reject even the compatibility value 1.
		if !adaptive {
			p.Temperature = sdk.Float(*c.Temperature)
		}
	}
	if c.Effort == "xhigh" && strings.HasPrefix(model, "claude-sonnet-4-6") {
		return fmt.Errorf("%s does not support xhigh effort", model)
	}
	p.OutputConfig.Effort = sdk.OutputConfigEffort(c.Effort)
	if c.CacheTTL != "" {
		p.CacheControl = sdk.NewCacheControlEphemeralParam()
		p.CacheControl.TTL = sdk.CacheControlEphemeralTTL(c.CacheTTL)
	}
	return nil
}
