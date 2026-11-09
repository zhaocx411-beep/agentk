package main

import (
	_ "embed"
	"net/http"
)

var (
	//go:embed openapi.yaml
	openAPIYAML []byte

	//go:embed scalar.html
	scalarHTML []byte
)

func registerAPIDocs(mux *http.ServeMux) {
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, _ *http.Request) {
		writeStatic(w, "text/html; charset=utf-8", scalarHTML)
	})
	mux.HandleFunc("GET /docs/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		writeStatic(w, "application/yaml; charset=utf-8", openAPIYAML)
	})
}

func writeStatic(w http.ResponseWriter, contentType string, content []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
