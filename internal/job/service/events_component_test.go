package service_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/common/id"
	"github.com/feruzabad/debroxy/internal/job/ports/in"
)

// sseEvent is one parsed server-sent event; comment is set for ": ping".
type sseEvent struct {
	id, name, data, comment string
}

type stream struct {
	t        *testing.T
	response *http.Response
	events   chan sseEvent
	retry    string
}

// openStream opens GET /api/v1/events as user and reads events in the background.
func (h *harness) openStream(user, query string) *stream {
	h.t.Helper()
	response, err := http.DefaultClient.Do(h.streamRequest(user, query))
	require.NoError(h.t, err)
	h.t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(h.t, http.StatusOK, response.StatusCode)
	s := &stream{t: h.t, response: response, events: make(chan sseEvent, 64)}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	require.NoError(h.t, err)
	s.retry = strings.TrimSpace(first)
	go func() {
		defer close(s.events)
		var current sseEvent
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case line == "":
				if current != (sseEvent{}) {
					s.events <- current
				}
				current = sseEvent{}
			case strings.HasPrefix(line, ":"):
				current.comment = strings.TrimSpace(strings.TrimPrefix(line, ":"))
			case strings.HasPrefix(line, "id: "):
				current.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				current.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				current.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return s
}

func (h *harness) streamRequest(user, query string) *http.Request {
	h.t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, h.server.URL+"/api/v1/events"+query, nil)
	require.NoError(h.t, err)
	request.Header.Set("Authorization", "Bearer "+user+"-token")
	return request
}

// refusedStream requests a stream that is expected to be refused.
func (h *harness) refusedStream(user, query string) (int, http.Header) {
	h.t.Helper()
	response, err := http.DefaultClient.Do(h.streamRequest(user, query))
	require.NoError(h.t, err)
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode, response.Header
}

// next returns the next event that is not a ping.
func (s *stream) next() sseEvent {
	s.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-s.events:
			require.True(s.t, ok, "the stream ended")
			if event.comment == "ping" {
				continue
			}
			return event
		case <-timeout:
			s.t.Fatal("no event within 5 s")
		}
	}
}

// quiet asserts that only pings arrive until the second ping.
func (s *stream) quiet() {
	s.t.Helper()
	pings := 0
	for pings < 2 {
		select {
		case event, ok := <-s.events:
			require.True(s.t, ok, "the stream ended")
			if event.comment == "ping" {
				pings++
				continue
			}
			s.t.Fatalf("unexpected event %s: %s", event.name, event.data)
		case <-time.After(5 * time.Second):
			s.t.Fatal("no ping within 5 s")
		}
	}
}

func (s *stream) ended() {
	s.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-s.events:
			if !ok {
				return
			}
		case <-timeout:
			s.t.Fatal("the stream did not end")
		}
	}
}

func decode[T any](t *testing.T, data string) T {
	t.Helper()
	var value T
	require.NoError(t, json.Unmarshal([]byte(data), &value), data)
	return value
}

func TestEventStream_sends_the_callers_own_job_changes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	alice := h.openStream("alice", "")
	bob := h.openStream("bob", "")

	assert.Equal(t, "no-store", alice.response.Header.Get("Cache-Control"))
	assert.Equal(t, "no", alice.response.Header.Get("X-Accel-Buffering"))
	assert.Equal(t, "text/event-stream", alice.response.Header.Get("Content-Type"))
	assert.Equal(t, "retry: 3000", alice.retry)
	ready := alice.next()
	assert.Equal(t, "ready", ready.name)
	assert.Equal(t, "1", ready.id)
	assert.JSONEq(t, `{"scope":"own"}`, ready.data)
	assert.Equal(t, "ready", bob.next().name)

	jobID := id.New()
	submitted, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, submitted.StatusCode(), string(submitted.Body))

	sawJob, sawQuota := false, false
	for !sawJob || !sawQuota {
		event := alice.next()
		switch event.name {
		case "job.upserted":
			job := decode[map[string]any](t, event.data)
			assert.Equal(t, jobID, job["id"])
			assert.NotContains(t, job, "ownerUsername", "owner names are for scope=all")
			assertNoInternalFields(t, []byte(event.data))
			sawJob = true
		case "quota.updated":
			assert.JSONEq(t, `{"slotsUsed":1,"maxActiveJobs":3}`, event.data)
			sawQuota = true
		default:
			t.Fatalf("unexpected event %s", event.name)
		}
	}
	bob.quiet()

	released, err := h.client.ReleaseJobWithResponse(ctx, jobID, as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, released.StatusCode())
	sawRelease := false
	for !sawRelease {
		event := alice.next()
		if event.name != "job.released" {
			continue
		}
		body := decode[map[string]any](t, event.data)
		assert.Equal(t, jobID, body["jobId"])
		assert.Equal(t, jobID, body["historyEntry"].(map[string]any)["jobId"])
		sawRelease = true
	}
	bob.quiet()
}

func TestEventStream_scope_all_is_for_admins(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	forbidden, _ := h.refusedStream("alice", "?scope=all")
	assert.Equal(t, http.StatusForbidden, forbidden)
	invalid, _ := h.refusedStream("alice", "?scope=everyone")
	assert.Equal(t, http.StatusBadRequest, invalid)

	root := h.openStream("root", "?scope=all")
	assert.JSONEq(t, `{"scope":"all"}`, root.next().data)
	jobID := id.New()
	submitted, err := h.client.SubmitJobWithResponse(ctx, magnetBody(jobID, sintel), as("alice"))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, submitted.StatusCode(), string(submitted.Body))

	event := root.next()
	require.Equal(t, "job.upserted", event.name, "no quota events in scope=all")
	job := decode[map[string]any](t, event.data)
	assert.Equal(t, jobID, job["id"])
	assert.Equal(t, "alice", job["ownerUsername"])
}

func TestEventStream_limits_open_streams(t *testing.T) {
	t.Parallel()
	h := newHarness(t) // two per user, three in total

	h.openStream("alice", "").next()
	h.openStream("alice", "").next()
	perUser, header := h.refusedStream("alice", "")
	assert.Equal(t, http.StatusServiceUnavailable, perUser)
	assert.Equal(t, "5", header.Get("Retry-After"))

	h.openStream("bob", "").next()
	total, _ := h.refusedStream("carol", "")
	assert.Equal(t, http.StatusServiceUnavailable, total)
}

func TestEventStream_ends_when_the_session_ends(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	alice := h.openStream("alice", "")
	alice.next()

	h.revoked.Store("alice", true)

	alice.ended()
	again := h.openStream("alice", "")
	assert.Equal(t, "ready", again.next().name, "the closed stream freed its slot")
}

func TestEventStream_reports_provider_back_off(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	alice := h.openStream("alice", "")
	alice.next()

	h.upstream.failListing(true)
	err := h.app.Commands.RefreshProviderState.Handle(context.Background(), in.RefreshProviderState{ProviderID: "torbox", Cause: in.RefreshCauseSafety})
	require.Error(t, err)

	event := alice.next()
	assert.Equal(t, "provider.stale", event.name)
	assert.JSONEq(t, `{"providerId":"torbox","stale":true}`, event.data)
}
