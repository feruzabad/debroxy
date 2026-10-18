// Package replay serves recorded HTTP transcripts from an httptest server,
// so adapters are tested against real provider answers (ddd-rules T-5). It
// reads the sanitized `.http` transcripts in docs/discovery/:
//
//	### GET https://api.example.org/v1/items?id=1
//	### extra-args: ...
//	### user-agent: ...  status+time: 200 0.3s  at: ...
//
//	HTTP/2 200
//	header: value
//
//	{"body": ...}
//
// Requests match a recording by method, path and query; the `token` query
// parameter is ignored because the transcripts redact it. Recordings of the
// same request are served in the order they were added.
package replay

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Exchange is one recorded request and its response.
type Exchange struct {
	Name   string
	Method string
	URL    *url.URL
	// ExtraArgs holds the recorded curl arguments (form fields, JSON body).
	ExtraArgs string
	Status    int
	Header    http.Header
	Body      []byte
}

// LoadExchange parses one transcript file.
func LoadExchange(path string) (Exchange, error) {
	content, err := os.ReadFile(path) //nolint:gosec // test fixtures chosen by the test
	if err != nil {
		return Exchange{}, err
	}
	exchange, err := parse(content)
	if err != nil {
		return Exchange{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	exchange.Name = filepath.Base(path)
	return exchange, nil
}

// MustLoadExchange loads a transcript or fails the test.
func MustLoadExchange(t testing.TB, path string) Exchange {
	t.Helper()
	exchange, err := LoadExchange(path)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return exchange
}

func parse(content []byte) (Exchange, error) {
	reader := bufio.NewReader(bytes.NewReader(content))
	var exchange Exchange
	// The "###" header ends at the first empty line.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return Exchange{}, fmt.Errorf("transcript ends inside its header")
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		switch {
		case strings.HasPrefix(line, "### extra-args:"):
			exchange.ExtraArgs = strings.TrimSpace(strings.TrimPrefix(line, "### extra-args:"))
		case strings.HasPrefix(line, "### user-agent:"):
		case strings.HasPrefix(line, "### "):
			method, rawURL, ok := strings.Cut(strings.TrimPrefix(line, "### "), " ")
			if !ok {
				return Exchange{}, fmt.Errorf("bad request line %q", line)
			}
			u, err := url.Parse(rawURL)
			if err != nil {
				return Exchange{}, fmt.Errorf("bad request URL: %w", err)
			}
			exchange.Method, exchange.URL = method, u
		}
	}
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		return Exchange{}, fmt.Errorf("missing status line")
	}
	fields := strings.Fields(statusLine)
	if len(fields) < 2 {
		return Exchange{}, fmt.Errorf("bad status line %q", statusLine)
	}
	if exchange.Status, err = strconv.Atoi(fields[1]); err != nil {
		return Exchange{}, fmt.Errorf("bad status %q", fields[1])
	}
	exchange.Header = http.Header{}
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok {
			exchange.Header.Add(strings.TrimSpace(name), strings.TrimSpace(value))
		}
		if err != nil {
			break
		}
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return Exchange{}, err
	}
	exchange.Body = bytes.TrimRight(body, "\r\n")
	if exchange.Method == "" {
		return Exchange{}, fmt.Errorf("no request line")
	}
	return exchange, nil
}

// WithBody returns a copy whose response body is replaced, for tests that
// need an answer no transcript holds (e.g. a listing with several items).
func (e Exchange) WithBody(body []byte) Exchange {
	e.Body = body
	return e
}

// WithRequest returns a copy that matches a different method and URL.
func (e Exchange) WithRequest(method, rawURL string) Exchange {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	e.Method, e.URL = method, u
	return e
}

// RecordedRequest is a request the server received.
type RecordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// Server replays exchanges. An unmatched request fails the test.
type Server struct {
	t        testing.TB
	server   *httptest.Server
	mu       sync.Mutex
	pending  []Exchange
	requests []RecordedRequest
}

func NewServer(t testing.TB, exchanges ...Exchange) *Server {
	t.Helper()
	s := &Server{t: t, pending: slices.Clone(exchanges)}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}

// URL is the base URL; recorded paths are served below it.
func (s *Server) URL() string { return s.server.URL }

func (s *Server) Add(exchanges ...Exchange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, exchanges...)
}

// Requests returns every request received so far.
func (s *Server) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Unserved lists the names of exchanges no request has used yet.
func (s *Server) Unserved() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.pending))
	for _, e := range s.pending {
		names = append(names, e.Name)
	}
	return names
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.requests = append(s.requests, RecordedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body})
	index := slices.IndexFunc(s.pending, func(e Exchange) bool { return matches(e, r) })
	var exchange Exchange
	if index >= 0 {
		exchange = s.pending[index]
		s.pending = slices.Delete(s.pending, index, index+1)
	}
	s.mu.Unlock()

	if index < 0 {
		s.t.Errorf("replay: no recording for %s %s?%s", r.Method, r.URL.Path, withoutToken(r.URL.Query()).Encode())
		w.WriteHeader(http.StatusNotImplemented)
		return
	}
	for name, values := range exchange.Header {
		// A replayed body may be a placeholder ("<binary body: N bytes>"), so
		// its length is recomputed; a HEAD answer keeps the recorded length.
		if strings.EqualFold(name, "Content-Length") && r.Method != http.MethodHead {
			continue
		}
		w.Header()[http.CanonicalHeaderKey(name)] = values
	}
	w.WriteHeader(exchange.Status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(exchange.Body)
	}
}

func matches(e Exchange, r *http.Request) bool {
	if e.Method != r.Method || e.URL.Path != r.URL.Path {
		return false
	}
	want, got := withoutToken(e.URL.Query()), withoutToken(r.URL.Query())
	if len(want) != len(got) {
		return false
	}
	for key, values := range want {
		if !slices.Equal(values, got[key]) {
			return false
		}
	}
	return true
}

func withoutToken(query url.Values) url.Values {
	cleaned := url.Values{}
	for key, values := range query {
		if key != "token" {
			cleaned[key] = values
		}
	}
	return cleaned
}
