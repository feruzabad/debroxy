package server

import (
	"encoding/json"
	"net/http"
)

// ClientConfig is the public, read-only instance configuration served at
// GET /api/v1/config (spec §10.4). It reveals no secret and no per-user
// data; the login page needs it before it has a session.
type ClientConfig struct {
	Version              string `json:"version"`
	OIDCEnabled          bool   `json:"oidcEnabled"`
	UploadLimitBytes     int64  `json:"uploadLimitBytes"`
	SignedLinkTTLSeconds int64  `json:"signedLinkTTLSeconds"`
	PasswordMinLength    int    `json:"passwordMinLength"`
}

// ClientConfigHandler serves GET /api/v1/config. It needs no authentication
// and answers Cache-Control: no-cache so a client always sees fresh facts.
type ClientConfigHandler struct {
	cfg ClientConfig
}

// NewClientConfigHandler returns a handler for the given validated config.
func NewClientConfigHandler(cfg ClientConfig) *ClientConfigHandler {
	return &ClientConfigHandler{cfg: cfg}
}

// Register adds the endpoint to mux.
func (h *ClientConfigHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/config", h.serve)
}

func (h *ClientConfigHandler) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(h.cfg)
}
