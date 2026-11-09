package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"agentcut/platform/blob"
)

var safeSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func main() {
	timeout := flag.Duration("timeout", 30*time.Second, "whole canary timeout")
	flag.Parse()

	user := strings.TrimSpace(os.Getenv("AGENTCUT_DEV_USER"))
	if !safeSegment.MatchString(user) {
		log.Fatal("AGENTCUT_DEV_USER is required and must be a safe personal key segment")
	}
	store, err := blob.NewStoreFromEnv()
	if err != nil {
		log.Fatalf("blob configuration: %v", err)
	}
	if store == nil {
		log.Fatal("blob storage is not configured")
	}
	if store.Backend() != "blob" {
		log.Fatalf("blobcheck requires AGENTCUT_BLOB_BACKEND=blob, got %q", store.Backend())
	}

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		log.Fatalf("generate object suffix: %v", err)
	}
	key := fmt.Sprintf(
		"agentcut-e2e/%s/%s-%s/canary.txt",
		user,
		time.Now().UTC().Format("20060102T150405.000000000Z"),
		hex.EncodeToString(suffix),
	)
	payload := []byte("agentcut blob canary\n")
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := blob.RunCanary(ctx, store, key, payload)
	if err != nil {
		log.Fatalf("canary failed (key=%q): %v", key, err)
	}
	log.Printf(
		"blob canary passed: key=%q uri=%q contentRange=%q deleted=true",
		result.Key, result.URI, result.ContentRange,
	)
}
