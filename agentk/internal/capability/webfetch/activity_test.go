package webfetch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.temporal.io/sdk/temporal"
)

type productDetailLookupStub struct {
	client ProductDetailClient
}

func (s productDetailLookupStub) ProductDetailClient() (ProductDetailClient, bool) {
	return s.client, s.client != nil
}

type productDetailClientStub struct {
	key string
	err error
}

func (s *productDetailClientStub) FetchProductDetail(
	_ context.Context,
	_ ProductDetailInput,
	key string,
) (ProductDetailOut, error) {
	s.key = key
	return ProductDetailOut{Platform: "amazon", Product: map[string]any{}}, s.err
}

func TestProductDetailActivityUsesActivityIdempotencyKey(t *testing.T) {
	client := &productDetailClientStub{}
	out, err := NewProductDetailActivity(productDetailLookupStub{client: client}).Run(
		context.Background(),
		ProductDetailInput{ProductURL: "https://www.amazon.com/dp/B09X7MPX8L"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if out.Platform != "amazon" || client.key != "non-temporal/activity" {
		t.Fatalf("out=%+v key=%q", out, client.key)
	}
}

func TestProductDetailActivityFailsClosedWithoutConfiguration(t *testing.T) {
	_, err := NewProductDetailActivity(productDetailLookupStub{}).Run(
		context.Background(),
		ProductDetailInput{ProductURL: "https://www.amazon.com/dp/B09X7MPX8L"},
	)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestProductDetailActivityValidatesInputBeforeCallingUpstream(t *testing.T) {
	client := &productDetailClientStub{err: errors.New("must not be called")}
	_, err := NewProductDetailActivity(productDetailLookupStub{client: client}).Run(
		context.Background(), ProductDetailInput{ProductURL: "https://example.com/product/1"},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported product host") {
		t.Fatalf("err = %v", err)
	}
	if client.key != "" {
		t.Fatal("invalid input reached the client")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() || appErr.Type() != "InvalidArgument" {
		t.Fatalf("classification = %v", err)
	}
}

func TestProductDetailActivityClassifiesUpstreamRejection(t *testing.T) {
	client := &productDetailClientStub{
		err: &apifyStatusError{status: 400, body: "invalid actor input"},
	}
	_, err := NewProductDetailActivity(productDetailLookupStub{client: client}).Run(
		context.Background(),
		ProductDetailInput{ProductURL: "https://www.amazon.com/dp/B09X7MPX8L"},
	)
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() ||
		appErr.Type() != "UpstreamRequestRejected" {
		t.Fatalf("classification = %v", err)
	}
}
