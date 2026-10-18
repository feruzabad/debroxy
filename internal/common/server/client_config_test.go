package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/server"
)

func serveConfig(handler *server.ClientConfigHandler) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	handler.Register(mux)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/config", nil))
	return response
}

func TestClientConfig_serves_the_public_facts(t *testing.T) {
	t.Parallel()
	handler := server.NewClientConfigHandler(server.ClientConfig{
		Version:              "0.2.0",
		OIDCEnabled:          true,
		UploadLimitBytes:     10485760,
		SignedLinkTTLSeconds: 21600,
		PasswordMinLength:    12,
	})

	response := serveConfig(handler)

	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "no-cache", response.Header().Get("Cache-Control"))
	assert.Equal(t, "application/json", response.Header().Get("Content-Type"))
	assert.JSONEq(t, `{
		"version": "0.2.0",
		"oidcEnabled": true,
		"uploadLimitBytes": 10485760,
		"signedLinkTTLSeconds": 21600,
		"passwordMinLength": 12
	}`, response.Body.String())
}

func TestClientConfig_reveals_no_secret(t *testing.T) {
	t.Parallel()
	handler := server.NewClientConfigHandler(server.ClientConfig{Version: "dev"})

	response := serveConfig(handler)

	require.Equal(t, http.StatusOK, response.Code)
	for _, field := range []string{"secret", "key", "token", "apiKey", "clientSecret", "webhookSecret"} {
		assert.NotContains(t, response.Body.String(), field)
	}
}

func TestClientConfig_needs_no_authentication(t *testing.T) {
	t.Parallel()
	handler := server.NewClientConfigHandler(server.ClientConfig{Version: "dev"})

	response := serveConfig(handler)

	// The handler runs directly on the mux without the auth middleware; a
	// request without credentials must still answer.
	assert.Equal(t, http.StatusOK, response.Code)
}
