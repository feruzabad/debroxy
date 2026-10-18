package query_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
	"github.com/feruzabad/debroxy/internal/delivery/app/query"
	"github.com/feruzabad/debroxy/internal/delivery/domain/signedlink"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
)

var (
	start   = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	logger  = slog.New(slog.DiscardHandler)
	metrics = decorator.NoOpMetricsClient{}
	keys    = []signedlink.Key{{ID: "k1", Secret: []byte(strings.Repeat("s", 32))}}
	alice   = in.Actor{UserID: "alice", Role: "user"}
	movie   = out.Target{ProviderID: "torbox", Kind: "torrent", ProviderItemID: "77", FileID: "3", Name: "Sintel/Sintel.mp4", RangeSupported: true}
	zip     = out.Target{ProviderID: "torbox", Kind: "torrent", ProviderItemID: "77", Bundle: true, Name: "Sintel"}
)

// fakeTargets is a hand-written JobTargets (T-2): alice owns job-1.
type fakeTargets struct{ err error }

func (f fakeTargets) DeliveryTarget(_ context.Context, userID, jobID string, target out.TargetRequest) (out.Target, error) {
	switch {
	case f.err != nil:
		return out.Target{}, f.err
	case userID != "alice" || jobID != "job-1":
		return out.Target{}, commonerrors.NewNotFoundError("job not found", "job-not-found")
	case target.Bundle:
		return zip, nil
	case target.FileID == "3":
		return movie, nil
	}
	return out.Target{}, commonerrors.NewConflictError("not available", "target-not-available")
}

func (f fakeTargets) AllTargets(context.Context, string, string) ([]out.Target, error) {
	return []out.Target{movie, {ProviderID: "torbox", FileID: "4", Name: "Sintel/subs.srt"}}, f.err
}

type fakeUsers struct{ active bool }

func (f fakeUsers) IsActive(context.Context, string) (bool, error) { return f.active, nil }

// trackedBody records whether it was closed.
type trackedBody struct {
	io.Reader
	mu     sync.Mutex
	closed bool
}

func (b *trackedBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

type fakeStreams struct {
	err     error
	request out.StreamRequest
	body    *trackedBody
}

func (f *fakeStreams) OpenDeliveryStream(_ context.Context, request out.StreamRequest) (out.Stream, error) {
	f.request = request
	if f.err != nil {
		return out.Stream{}, f.err
	}
	f.body = &trackedBody{Reader: strings.NewReader("bytes")}
	return out.Stream{Status: 206, RangeSupported: true, Body: f.body, Headers: map[string]string{
		"Content-Range": "bytes 0-4/100", "Content-Length": "5", "Set-Cookie": "upstream=secret", "Location": "https://cdn.example/x",
	}}, nil
}

type fixture struct {
	clock    *clock.Fixed
	issue    in.IssueSignedLinkHandler
	issueAll in.IssueSignedLinksForAllFilesHandler
	open     in.OpenDownloadHandler
	streams  *fakeStreams
}

func newFixture(t *testing.T, targets out.JobTargets, users out.UserStatusChecker, perUser, total int) *fixture {
	t.Helper()
	f := &fixture{clock: clock.NewFixed(start), streams: &fakeStreams{}}
	links := query.LinkConfig{PublicBaseURL: "https://debroxy.example.org/", TTL: 6 * time.Hour, Keys: keys}
	var err error
	f.issue, err = query.NewIssueSignedLinkHandler(targets, links, f.clock, logger, metrics)
	require.NoError(t, err)
	f.issueAll, err = query.NewIssueSignedLinksForAllFilesHandler(targets, links, f.clock, logger, metrics)
	require.NoError(t, err)
	f.open, err = query.NewOpenDownloadHandler(query.DownloadDependencies{
		Users: users, Slots: query.NewStreamSlots(perUser, total), Targets: targets, Streams: f.streams, Clock: f.clock,
	}, keys, logger, metrics)
	require.NoError(t, err)
	return f
}

func tokenOf(t *testing.T, link in.SignedLinkResult) (token, name string) {
	t.Helper()
	rest, ok := strings.CutPrefix(link.URL, "https://debroxy.example.org/dl/")
	require.True(t, ok, link.URL)
	token, name, ok = strings.Cut(rest, "/")
	require.True(t, ok)
	return token, name
}

func TestIssueSignedLink_signs_an_available_target(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fakeTargets{}, fakeUsers{active: true}, 4, 64)

	link, err := f.issue.Handle(context.Background(), in.IssueSignedLink{Actor: alice, JobID: "job-1", FileID: "3"})

	require.NoError(t, err)
	_, name := tokenOf(t, link)
	assert.Equal(t, "Sintel.mp4", name, "the stored file name, last path element")
	assert.Equal(t, start.Add(6*time.Hour), link.ExpiresAt)
	assert.True(t, link.RangeSupported)
	assert.NotContains(t, link.URL, "77", "no provider data in links")

	_, err = f.issue.Handle(context.Background(), in.IssueSignedLink{Actor: alice, JobID: "job-1", FileID: "9"})
	var slugErr commonerrors.SlugError
	require.ErrorAs(t, err, &slugErr)
	assert.Equal(t, "target-not-available", slugErr.Slug())
}

func TestIssueSignedLinksForAllFiles_keeps_the_stored_order(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fakeTargets{}, fakeUsers{active: true}, 4, 64)

	links, err := f.issueAll.Handle(context.Background(), in.IssueSignedLinksForAllFiles{Actor: alice, JobID: "job-1"})

	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.Equal(t, "3", links[0].FileID)
	assert.Equal(t, "4", links[1].FileID)
}

func TestOpenDownload_streams_with_the_whitelisted_headers(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fakeTargets{}, fakeUsers{active: true}, 4, 64)
	link, err := f.issue.Handle(context.Background(), in.IssueSignedLink{Actor: alice, JobID: "job-1", FileID: "3"})
	require.NoError(t, err)
	token, _ := tokenOf(t, link)

	download, err := f.open.Handle(context.Background(), in.OpenDownload{Token: token, Name: "../evil\".mp4", Method: "GET", Range: "bytes=0-4"})

	require.NoError(t, err)
	assert.Equal(t, 206, download.Status)
	assert.Equal(t, map[string]string{"Content-Range": "bytes 0-4/100", "Content-Length": "5"}, download.Headers)
	assert.Equal(t, "..evil.mp4", download.FileName)
	assert.Equal(t, out.StreamRequest{Target: movie, Method: "GET", Range: "bytes=0-4"}, f.streams.request)
	require.NoError(t, download.Body.Close())
	assert.True(t, f.streams.body.closed)
}

func TestOpenDownload_refusals(t *testing.T) {
	t.Parallel()
	signer, err := signedlink.NewSigner(keys)
	require.NoError(t, err)
	file, err := signedlink.FileTarget("3")
	require.NoError(t, err)
	valid, err := signer.Sign("alice", "job-1", file, start.Add(time.Hour))
	require.NoError(t, err)
	expired, err := signer.Sign("alice", "job-1", file, start)
	require.NoError(t, err)
	foreign, err := signer.Sign("bob", "job-1", file, start.Add(time.Hour))
	require.NoError(t, err)

	testCases := []struct {
		name    string
		token   string
		users   fakeUsers
		streams error
		slug    string
	}{
		{"tampered", valid + "x", fakeUsers{active: true}, nil, "invalid-signed-link"},
		{"expired", expired, fakeUsers{active: true}, nil, "signed-link-expired"},
		{"disabled_user", valid, fakeUsers{active: false}, nil, "invalid-signed-link"},
		{"not_the_owner", foreign, fakeUsers{active: true}, nil, "job-not-found"},
		{"provider_down", valid, fakeUsers{active: true}, errors.New("upstream 503"), "provider-unavailable"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, fakeTargets{}, tc.users, 1, 1)
			f.streams.err = tc.streams

			_, err := f.open.Handle(context.Background(), in.OpenDownload{Token: tc.token, Method: "GET"})

			var slugErr commonerrors.SlugError
			require.ErrorAs(t, err, &slugErr)
			assert.Equal(t, tc.slug, slugErr.Slug())
			if tc.streams != nil || tc.slug == "job-not-found" {
				f.streams.err = nil
				download, err := f.open.Handle(context.Background(), in.OpenDownload{Token: valid, Method: "GET"})
				require.NoError(t, err, "a refusal after taking the slot gives it back")
				require.NoError(t, download.Body.Close())
			}
		})
	}
}

func TestOpenDownload_enforces_the_stream_caps(t *testing.T) {
	t.Parallel()
	f := newFixture(t, fakeTargets{}, fakeUsers{active: true}, 2, 3)
	link, err := f.issue.Handle(context.Background(), in.IssueSignedLink{Actor: alice, JobID: "job-1", Bundle: true})
	require.NoError(t, err)
	token, _ := tokenOf(t, link)
	open := func() (in.Download, error) {
		return f.open.Handle(context.Background(), in.OpenDownload{Token: token, Method: "GET"})
	}

	first, err := open()
	require.NoError(t, err)
	second, err := open()
	require.NoError(t, err)
	_, err = open()
	assert.Equal(t, query.TooManyStreamsError{}, err)

	require.NoError(t, first.Body.Close())
	require.NoError(t, first.Body.Close(), "closing twice frees one slot only")
	third, err := open()
	require.NoError(t, err)
	_, err = open()
	assert.Equal(t, query.TooManyStreamsError{}, err)
	require.NoError(t, second.Body.Close())
	require.NoError(t, third.Body.Close())
}
