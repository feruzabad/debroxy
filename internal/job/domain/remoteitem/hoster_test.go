package remoteitem_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
)

func TestMatchesHoster(t *testing.T) {
	t.Parallel()
	hosters := []remoteitem.Hoster{
		{Domains: []string{"mega.nz", "mega.co.nz"}, Up: true},
		{Domains: []string{"rapidgator.net"}, Up: false},
	}
	testCases := []struct {
		url  string
		want bool
	}{
		{"https://mega.nz/file/abc", true},
		{"https://WWW.Mega.NZ./file/abc", true},
		{"https://mega.co.nz/x", true},
		{"https://notmega.nz/x", false},
		{"https://mega.nz.evil.example/x", false},
		{"https://rapidgator.net/file/1", false},
		{"://broken", false},
	}
	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, remoteitem.MatchesHoster(tc.url, hosters))
		})
	}
}

func TestCheckDeliveryTarget(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name                      string
		state                     remoteitem.State
		perFile, known, hasBundle bool
		bundle                    bool
		want                      error
	}{
		{"file_ready", remoteitem.StateReady, true, true, false, false, nil},
		{"file_not_ready", remoteitem.StateDownloading, true, true, false, false, remoteitem.ErrTargetNotAvailable},
		{"file_unknown", remoteitem.StateReady, true, false, false, false, remoteitem.ErrTargetNotAvailable},
		{"no_per_file_delivery", remoteitem.StateReady, false, true, true, false, remoteitem.ErrTargetNotAvailable},
		{"bundle_ready", remoteitem.StateReady, false, false, true, true, nil},
		{"no_bundle", remoteitem.StateReady, true, true, false, true, remoteitem.ErrTargetNotAvailable},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, remoteitem.CheckDeliveryTarget(tc.state, tc.perFile, tc.known, tc.hasBundle, tc.bundle))
		})
	}
}

func TestFingerprint_TorrentHash(t *testing.T) {
	t.Parallel()
	torrent, err := remoteitem.NewFingerprintFromString("btih:abc")
	assert.NoError(t, err)
	hash, ok := torrent.TorrentHash()
	assert.True(t, ok)
	assert.Equal(t, "abc", hash)

	web, err := remoteitem.NewFingerprintFromString("url:https://x/")
	assert.NoError(t, err)
	_, ok = web.TorrentHash()
	assert.False(t, ok)
}
