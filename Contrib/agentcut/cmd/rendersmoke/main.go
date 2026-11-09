// Command rendersmoke starts and polls a render for an existing AgentCut project.
// It is intended for staging/KESS acceptance after the project timeline contains
// real Blob-backed media.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type accepted struct {
	RenderKey string `json:"renderKey"`
}

type status struct {
	Status         string   `json:"status"`
	OutputURI      string   `json:"outputUri"`
	Error          string   `json:"error"`
	WorkflowStatus string   `json:"workflowStatus"`
	Warnings       []string `json:"warnings"`
}

func main() {
	baseURL := flag.String("base-url", envOr("AGENTCUT_API_URL", "http://127.0.0.1:8080"), "cut-apiserver base URL")
	projectID := flag.String("project-id", "", "existing project with a renderable timeline")
	user := flag.String(
		"user",
		envOr("AGENTCUT_USER", envOr("AGENTCUT_DEV_USER", "dev")),
		"trusted AgentCut user subject",
	)
	timeout := flag.Duration("timeout", 2*time.Hour, "overall render timeout")
	interval := flag.Duration("interval", 3*time.Second, "poll interval")
	flag.Parse()
	if strings.TrimSpace(*projectID) == "" {
		fmt.Fprintln(os.Stderr, "-project-id is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client := &http.Client{Timeout: 30 * time.Second}
	path := strings.TrimRight(*baseURL, "/") + "/v1/projects/" + *projectID + "/renders"

	var start accepted
	if err := requestJSON(ctx, client, http.MethodPost, path, *user, &start); err != nil {
		fatal(err)
	}
	if start.RenderKey == "" {
		fatal(fmt.Errorf("render start returned an empty renderKey"))
	}
	fmt.Printf("render accepted: %s\n", start.RenderKey)

	statusURL := path + "/" + start.RenderKey
	for {
		var current status
		if err := requestJSON(ctx, client, http.MethodGet, statusURL, *user, &current); err != nil {
			fatal(err)
		}
		switch current.Status {
		case "completed":
			fmt.Printf("render completed: %s\n", current.OutputURI)
			if len(current.Warnings) > 0 {
				fmt.Printf("warnings: %s\n", strings.Join(current.Warnings, " | "))
			}
			return
		case "failed":
			fatal(fmt.Errorf("render failed (%s): %s", current.WorkflowStatus, current.Error))
		default:
			fmt.Printf("render status: %s\n", current.Status)
		}
		select {
		case <-ctx.Done():
			fatal(ctx.Err())
		case <-time.After(*interval):
		}
	}
}

func requestJSON(ctx context.Context, client *http.Client, method, url, user string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AgentCut-User", user)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: HTTP %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
