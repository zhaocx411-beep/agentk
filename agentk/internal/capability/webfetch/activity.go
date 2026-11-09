package webfetch

import (
	"context"
	"fmt"
	"strings"

	"agentk/internal/temporalx"
)

type ProductDetailInput struct {
	ProductURL string `json:"productUrl"`
	MaxImages  int    `json:"maxImages,omitempty"`
}

type ProductDetailClient interface {
	FetchProductDetail(
		ctx context.Context,
		input ProductDetailInput,
		idempotencyKey string,
	) (ProductDetailOut, error)
}

type ProductDetailLookup interface {
	ProductDetailClient() (ProductDetailClient, bool)
}

type ProductDetailActivity struct {
	clients ProductDetailLookup
}

func NewProductDetailActivity(clients ProductDetailLookup) *ProductDetailActivity {
	return &ProductDetailActivity{clients: clients}
}

func (a *ProductDetailActivity) Run(
	ctx context.Context,
	input ProductDetailInput,
) (ProductDetailOut, error) {
	const op = "capability fetch_product_detail"
	if a.clients == nil {
		return ProductDetailOut{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf("product detail client lookup is required")))
	}
	if err := validateProductDetailInput(input); err != nil {
		return ProductDetailOut{}, temporalx.ClassifyActivityError(op,
			temporalx.InvalidActivityInput("%v", err))
	}
	client, ok := a.clients.ProductDetailClient()
	if !ok {
		return ProductDetailOut{}, temporalx.ClassifyActivityError(op,
			temporalx.ConfigurationError(fmt.Errorf(
				"fetch_product_detail capability is not configured")))
	}
	out, err := client.FetchProductDetail(
		ctx, input, temporalx.ActivityIdempotencyKey(ctx))
	if err != nil {
		return ProductDetailOut{}, temporalx.ClassifyActivityError(op, err)
	}
	return out, nil
}

func validateProductDetailInput(input ProductDetailInput) error {
	if strings.TrimSpace(input.ProductURL) == "" {
		return fmt.Errorf("productUrl is required")
	}
	if input.MaxImages < 0 || input.MaxImages > absoluteMaxImages {
		return fmt.Errorf("maxImages must be between 1 and %d", absoluteMaxImages)
	}
	target, err := validateTarget(input.ProductURL)
	if err != nil {
		return err
	}
	if productPlatform(target.Hostname()) == "" {
		return fmt.Errorf("unsupported product host %q", target.Hostname())
	}
	return nil
}
