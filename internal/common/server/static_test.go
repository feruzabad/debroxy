package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/common/server"
)

func TestStaticHandler(t *testing.T) {
	t.Parallel()
	files := fstest.MapFS{
		"index.html":          {Data: []byte("<!doctype html><title>debroxy</title>")},
		"assets/app-1a2b.js":  {Data: []byte("console.log(1)")},
		"favicon.svg":         {Data: []byte("<svg/>")},
		"assets/nested/x.css": {Data: []byte("body{}")},
	}
	handler := server.NewStaticHandler(files, slog.New(slog.DiscardHandler))
	csp := "default-src 'self'; img-src 'self' data:; media-src 'self'; style-src 'self'; script-src 'self'; " +
		"connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

	testCases := []struct {
		name, method, path string
		status             int
		body, cache        string
	}{
		{"root", http.MethodGet, "/", http.StatusOK, "<!doctype html><title>debroxy</title>", "no-cache"},
		{"index_file", http.MethodGet, "/index.html", http.StatusOK, "<!doctype html><title>debroxy</title>", "no-cache"},
		{"client_route", http.MethodGet, "/jobs/0190a8f2", http.StatusOK, "<!doctype html><title>debroxy</title>", "no-cache"},
		{"hashed_asset", http.MethodGet, "/assets/app-1a2b.js", http.StatusOK, "console.log(1)", "public, max-age=31536000, immutable"},
		{"nested_asset", http.MethodGet, "/assets/nested/x.css", http.StatusOK, "body{}", "public, max-age=31536000, immutable"},
		{"other_file", http.MethodGet, "/favicon.svg", http.StatusOK, "<svg/>", "no-cache"},
		{"directory_falls_back", http.MethodGet, "/assets/", http.StatusOK, "<!doctype html><title>debroxy</title>", "no-cache"},
		{"head", http.MethodHead, "/admin/users", http.StatusOK, "", "no-cache"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))

			assert.Equal(t, tc.status, response.Code)
			assert.Equal(t, tc.body, response.Body.String())
			assert.Equal(t, tc.cache, response.Header().Get("Cache-Control"))
			assert.Equal(t, csp, response.Header().Get("Content-Security-Policy"))
			assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
		})
	}

	t.Run("reserved_paths_are_not_the_client", func(t *testing.T) {
		t.Parallel()
		for _, path := range []string{"/api/v1/nope", "/api", "/dl/x", "/webhooks/torbox/x", "/healthz", "/readyz"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			assert.Equal(t, http.StatusNotFound, response.Code, path)
			assert.JSONEq(t, `{"slug":"not-found","message":"no such route"}`, response.Body.String(), path)
			assert.Empty(t, response.Header().Get("Content-Security-Policy"), path)
		}
	})

	t.Run("other_methods", func(t *testing.T) {
		t.Parallel()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/jobs", nil))
		assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
		assert.Equal(t, "GET, HEAD", response.Header().Get("Allow"))
		assert.Equal(t, csp, response.Header().Get("Content-Security-Policy"))
	})
}
