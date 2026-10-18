package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/auth"
	"github.com/feruzabad/debroxy/internal/common/clock"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/common/server"
	"github.com/feruzabad/debroxy/internal/delivery/adapters/in/http/client"
	"github.com/feruzabad/debroxy/internal/delivery/service"
	identityin "github.com/feruzabad/debroxy/internal/identity/ports/in"
	jobin "github.com/feruzabad/debroxy/internal/job/ports/in"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

const (
	content = "0123456789abcdefghij" // the file and the bundle, 20 bytes
	cdnURL  = "https://cdn.provider.example/secret-path"
)

type operation[Req, Res any] func(context.Context, Req) (Res, error)

func (o operation[Req, Res]) Handle(ctx context.Context, request Req) (Res, error) {
	return o(ctx, request)
}

// jobApp fakes job: alice's job-1 is ready with one file and a zip bundle.
func jobApp() jobin.Application {
	view := jobin.JobView{ID: "job-1", OwnerID: "alice", State: "ready", ProviderID: "torbox", ProviderItemID: "77",
		Delivery: jobin.DeliveryView{PerFile: true, Bundle: &jobin.BundleView{Format: "zip"}},
		Files:    []jobin.FileView{{ID: "3", Path: "Sintel/Sintel.mp4", SizeBytes: 20}}}
	notFound := commonerrors.NewNotFoundError("job not found", "job-not-found")
	return jobin.Application{Queries: jobin.Queries{
		JobDetails: operation[jobin.JobDetails, jobin.JobView](func(_ context.Context, q jobin.JobDetails) (jobin.JobView, error) {
			if q.JobID != "job-1" || q.Actor.UserID != "alice" {
				return jobin.JobView{}, notFound
			}
			return view, nil
		}),
		DeliveryTarget: operation[jobin.DeliveryTarget, jobin.DeliveryTargetResult](func(_ context.Context, q jobin.DeliveryTarget) (jobin.DeliveryTargetResult, error) {
			if q.JobID != "job-1" || q.Actor.UserID != "alice" {
				return jobin.DeliveryTargetResult{}, notFound
			}
			if q.Bundle {
				return jobin.DeliveryTargetResult{ProviderID: "torbox", Kind: "torrent", ProviderItemID: "77", Bundle: true, Name: "Sintel"}, nil
			}
			if q.FileID != "3" {
				return jobin.DeliveryTargetResult{}, commonerrors.NewConflictError("not available", "target-not-available")
			}
			return jobin.DeliveryTargetResult{ProviderID: "torbox", Kind: "torrent", ProviderItemID: "77", FileID: "3", Name: "Sintel/Sintel.mp4", RangeSupported: true}, nil
		}),
	}}
}

// cdn fakes provider's OpenDeliveryStream: files honour Range, the zip
// ignores it (ADR 0011). block makes bodies wait for their context.
type cdn struct {
	block     atomic.Bool
	cancelled chan struct{}
	once      sync.Once
}

// blockingBody serves nothing until its request context ends.
type blockingBody struct {
	ctx  context.Context
	cdn  *cdn
	done chan struct{}
	once sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) {
	select {
	case <-b.ctx.Done():
		b.cdn.once.Do(func() { close(b.cdn.cancelled) })
		return 0, b.ctx.Err()
	case <-b.done:
		return 0, io.EOF
	}
}

func (b *blockingBody) Close() error {
	b.once.Do(func() { close(b.done) })
	return nil
}

func (c *cdn) app() providerin.Application {
	return providerin.Application{Operations: providerin.Operations{
		OpenDeliveryStream: operation[providerin.OpenDeliveryStreamRequest, providerin.DeliveryStream](func(ctx context.Context, r providerin.OpenDeliveryStreamRequest) (providerin.DeliveryStream, error) {
			headers := map[string]string{"Content-Type": "video/mp4", "ETag": `"e1"`}
			if c.block.Load() {
				return providerin.DeliveryStream{Status: 200, Headers: headers, Body: &blockingBody{ctx: ctx, cdn: c, done: make(chan struct{})}}, nil
			}
			body := content
			status, rangeSupported := http.StatusOK, !r.Target.Bundle
			if rangeSupported && r.Range != "" {
				var from, to int
				if _, err := fmt.Sscanf(r.Range, "bytes=%d-%d", &from, &to); err == nil {
					body, status = content[from:to+1], http.StatusPartialContent
					headers["Content-Range"] = fmt.Sprintf("bytes %d-%d/%d", from, to, len(content))
				}
			}
			headers["Content-Length"] = strconv.Itoa(len(body))
			if r.Method == http.MethodHead {
				body = ""
			}
			return providerin.DeliveryStream{Status: status, Headers: headers, RangeSupported: rangeSupported, Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	}}
}

// users fakes identity's UserStatus; disabled users are listed.
type users struct{ disabled sync.Map }

func (u *users) app() identityin.Application {
	return identityin.Application{Queries: identityin.Queries{
		UserStatus: operation[identityin.UserStatus, identityin.UserStatusResult](func(_ context.Context, q identityin.UserStatus) (identityin.UserStatusResult, error) {
			_, disabled := u.disabled.Load(q.UserID)
			return identityin.UserStatusResult{Exists: true, Active: !disabled}, nil
		}),
	}}
}

type tokens struct{}

func (tokens) Authenticate(_ context.Context, credential string) (auth.Principal, error) {
	user, ok := strings.CutSuffix(credential, "-token")
	if !ok {
		return auth.Principal{}, commonerrors.NewAuthenticationError("unknown session", "session-expired")
	}
	return auth.Principal{UserID: user, Role: "user"}, nil
}

func (t tokens) Check(ctx context.Context, credential string) error {
	_, err := t.Authenticate(ctx, credential)
	return err
}

type harness struct {
	t      *testing.T
	cdn    *cdn
	users  *users
	clock  *clock.Fixed
	server *httptest.Server
	client *client.ClientWithResponses
}

func newHarness(t *testing.T, maxPerUser int) *harness {
	t.Helper()
	h := &harness{t: t, cdn: &cdn{cancelled: make(chan struct{})}, users: &users{}, clock: clock.NewFixed(time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))}
	logger := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	h.server = httptest.NewServer(server.Chain(mux, server.Authenticate(server.AuthenticationConfig{AllowedOrigin: "https://x"}, tokens{}, logger)))
	t.Cleanup(h.server.Close)
	app, err := service.NewComponentTestApplication(service.Config{
		PublicBaseURL: h.server.URL, SignedLinkTTL: 6 * time.Hour, MaxStreamsPerUser: maxPerUser, MaxConcurrentStreams: 64,
		SigningKeys: []service.SigningKey{{ID: "k1", Secret: []byte(strings.Repeat("k", 32))}},
	}, jobApp(), h.cdn.app(), h.users.app(), h.clock, logger)
	require.NoError(t, err)
	service.RegisterHTTPRoutes(mux, app, logger)
	h.client, err = client.NewClientWithResponses(h.server.URL)
	require.NoError(t, err)
	return h
}

func as(user string) client.RequestEditorFn {
	return func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+user+"-token")
		return nil
	}
}

func (h *harness) link(target client.LinkTarget) string {
	h.t.Helper()
	response, err := h.client.IssueSignedLinkWithResponse(context.Background(), "job-1", client.IssueSignedLinkJSONRequestBody{Target: target}, as("alice"))
	require.NoError(h.t, err)
	require.Equal(h.t, http.StatusOK, response.StatusCode(), string(response.Body))
	assert.NotContains(h.t, string(response.Body), "cdn.provider", "no CDN URL in any response")
	return response.JSON200.Url
}

func fileTarget() client.LinkTarget { return client.LinkTarget{FileId: ptr("3")} }
func bundleTarget() client.LinkTarget {
	return client.LinkTarget{Bundle: ptr(true)}
}
func ptr[T any](v T) *T { return &v }

// answer is what a test needs of a response whose body get already read and closed.
type answer struct {
	StatusCode int
	Header     http.Header
}

func get(t *testing.T, method, url string, header map[string]string) (answer, string) {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, url, nil)
	require.NoError(t, err)
	for k, v := range header {
		request.Header.Set(k, v)
	}
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer func() { require.NoError(t, response.Body.Close()) }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return answer{StatusCode: response.StatusCode, Header: response.Header}, string(body)
}

func slugOf(t *testing.T, body string) string {
	t.Helper()
	var decoded struct{ Slug string }
	require.NoError(t, json.Unmarshal([]byte(body), &decoded), body)
	return decoded.Slug
}

func TestDelivery_range_on_a_file_and_on_a_zip(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)

	fileURL := h.link(fileTarget())
	assert.True(t, strings.HasSuffix(fileURL, "/Sintel.mp4"), fileURL)
	partial, body := get(t, http.MethodGet, fileURL, map[string]string{"Range": "bytes=2-5", "Cookie": "debroxy_session=x"})
	assert.Equal(t, http.StatusPartialContent, partial.StatusCode)
	assert.Equal(t, "2345", body)
	assert.Equal(t, "bytes 2-5/20", partial.Header.Get("Content-Range"))
	assert.Equal(t, "bytes", partial.Header.Get("Accept-Ranges"))
	assert.Equal(t, "attachment; filename*=UTF-8''Sintel.mp4", partial.Header.Get("Content-Disposition"))
	assert.Empty(t, partial.Header.Get("Location"))

	zipURL := h.link(bundleTarget())
	whole, body := get(t, http.MethodGet, zipURL, map[string]string{"Range": "bytes=2-5"})
	assert.Equal(t, http.StatusOK, whole.StatusCode, "the zip ignores Range (C-16)")
	assert.Equal(t, content, body)
	assert.Equal(t, "none", whole.Header.Get("Accept-Ranges"))

	renamed, _ := get(t, http.MethodGet, strings.TrimSuffix(fileURL, "Sintel.mp4")+"my%20film%3B.mp4", nil)
	assert.Equal(t, "attachment; filename*=UTF-8''my%20film%3B.mp4", renamed.Header.Get("Content-Disposition"))
}

func TestDelivery_head_returns_headers_only(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)

	response, body := get(t, http.MethodHead, h.link(fileTarget()), nil)

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Empty(t, body)
	assert.Equal(t, `"e1"`, response.Header.Get("ETag"))
	assert.Equal(t, "bytes", response.Header.Get("Accept-Ranges"))
}

func TestDelivery_expired_link_and_disabled_user(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)
	url := h.link(fileTarget())

	h.users.disabled.Store("alice", true)
	disabled, body := get(t, http.MethodGet, url, nil)
	assert.Equal(t, http.StatusForbidden, disabled.StatusCode)
	assert.Equal(t, "invalid-signed-link", slugOf(t, body))

	h.users.disabled.Delete("alice")
	h.clock.Advance(6 * time.Hour)
	expired, body := get(t, http.MethodGet, url, nil)
	assert.Equal(t, http.StatusForbidden, expired.StatusCode)
	assert.Equal(t, "signed-link-expired", slugOf(t, body))

	tampered, body := get(t, http.MethodGet, strings.Replace(url, "/dl/", "/dl/x", 1), nil)
	assert.Equal(t, http.StatusForbidden, tampered.StatusCode)
	assert.Equal(t, "invalid-signed-link", slugOf(t, body))
}

func TestDelivery_stream_cap_and_disconnect_cancels_upstream(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1)
	url := h.link(fileTarget())
	h.cdn.block.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	first, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, first.StatusCode, "headers arrive before the body")

	capped, body := get(t, http.MethodGet, url, nil)
	assert.Equal(t, http.StatusServiceUnavailable, capped.StatusCode)
	assert.Equal(t, "too-many-streams", slugOf(t, body))
	var cappedBody struct {
		Slug       string `json:"slug"`
		RetryAfter int    `json:"retryAfter"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &cappedBody))
	assert.Equal(t, 5, cappedBody.RetryAfter)
	assert.Equal(t, "5", capped.Header.Get("Retry-After"))

	cancel()
	require.NoError(t, first.Body.Close())
	select {
	case <-h.cdn.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the upstream request was not cancelled")
	}
	h.cdn.block.Store(false)
	assert.Eventually(t, func() bool {
		response, _ := get(t, http.MethodHead, url, nil)
		return response.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond, "the slot is free again")
}

func TestDelivery_all_links_and_refusals(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 4)
	ctx := context.Background()

	all, err := h.client.IssueSignedLinksForAllFilesWithResponse(ctx, "job-1", as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, all.StatusCode(), string(all.Body))
	require.Len(t, all.JSON200.Links, 1)
	assert.Equal(t, "3", *all.JSON200.Links[0].FileId)

	foreign, err := h.client.IssueSignedLinkWithResponse(ctx, "job-1", client.IssueSignedLinkJSONRequestBody{Target: fileTarget()}, as("bob"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, foreign.StatusCode())

	unavailable, err := h.client.IssueSignedLinkWithResponse(ctx, "job-1", client.IssueSignedLinkJSONRequestBody{Target: client.LinkTarget{FileId: ptr("9")}}, as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, unavailable.StatusCode())

	both, err := h.client.IssueSignedLinkWithResponse(ctx, "job-1", client.IssueSignedLinkJSONRequestBody{Target: client.LinkTarget{FileId: ptr("3"), Bundle: ptr(true)}}, as("alice"))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, both.StatusCode())

	for name, call := range map[string]func() (*http.Response, error){
		"link": func() (*http.Response, error) {
			return h.client.IssueSignedLink(ctx, "job-1", client.IssueSignedLinkJSONRequestBody{Target: fileTarget()})
		},
		"all_links": func() (*http.Response, error) { return h.client.IssueSignedLinksForAllFiles(ctx, "job-1") },
	} {
		t.Run(name, func(t *testing.T) {
			response, err := call()
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		})
	}
}
