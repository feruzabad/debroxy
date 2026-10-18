package signedlink_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/delivery/domain/signedlink"
)

var (
	now    = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	keyOne = signedlink.Key{ID: "k1", Secret: []byte(strings.Repeat("a", 32))}
	keyTwo = signedlink.Key{ID: "k2", Secret: []byte(strings.Repeat("b", 32))}
)

func mustSigner(t *testing.T, keys ...signedlink.Key) signedlink.Signer {
	t.Helper()
	signer, err := signedlink.NewSigner(keys)
	require.NoError(t, err)
	return signer
}

func TestSigner_round_trip(t *testing.T) {
	t.Parallel()
	signer := mustSigner(t, keyOne)
	target, err := signedlink.FileTarget("10")
	require.NoError(t, err)

	token, err := signer.Sign("alice", "job-1", target, now.Add(6*time.Hour))
	require.NoError(t, err)
	link, err := signer.Verify(token, now)

	require.NoError(t, err)
	assert.Equal(t, signedlink.Link{KeyID: "k1", UserID: "alice", JobID: "job-1", Target: target, ExpiresAt: now.Add(6 * time.Hour)}, link)
	assert.NotContains(t, token, "/", "URL-safe")
}

func TestSigner_rejects_tampering_and_expiry(t *testing.T) {
	t.Parallel()
	signer := mustSigner(t, keyOne)
	token, err := signer.Sign("alice", "job-1", signedlink.BundleTarget(), now.Add(time.Hour))
	require.NoError(t, err)
	payload, signature, _ := strings.Cut(token, ".")
	other, err := signer.Sign("bob", "job-1", signedlink.BundleTarget(), now.Add(time.Hour))
	require.NoError(t, err)
	otherPayload, _, _ := strings.Cut(other, ".")

	testCases := map[string]string{
		"swapped_payload": otherPayload + "." + signature,
		"no_signature":    payload,
		"bad_base64":      payload + ".!!!",
		"garbage":         "x.y",
		"empty":           "",
	}
	for name, token := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := signer.Verify(token, now)
			assert.Equal(t, signedlink.ErrInvalid, err)
		})
	}

	_, err = signer.Verify(token, now.Add(time.Hour))
	assert.Equal(t, signedlink.ErrExpired, err)
}

func TestSigner_rotation(t *testing.T) {
	t.Parallel()
	old := mustSigner(t, keyOne)
	token, err := old.Sign("alice", "job-1", signedlink.BundleTarget(), now.Add(time.Hour))
	require.NoError(t, err)

	rotated := mustSigner(t, keyTwo, keyOne)
	link, err := rotated.Verify(token, now)
	require.NoError(t, err, "old links stay valid while their key is listed")
	assert.Equal(t, "k1", link.KeyID)
	fresh, err := rotated.Sign("alice", "job-1", signedlink.BundleTarget(), now.Add(time.Hour))
	require.NoError(t, err)
	freshLink, err := rotated.Verify(fresh, now)
	require.NoError(t, err)
	assert.Equal(t, "k2", freshLink.KeyID, "the first key signs")

	revoked := mustSigner(t, keyTwo)
	_, err = revoked.Verify(token, now)
	assert.Equal(t, signedlink.ErrInvalid, err, "removing a key revokes its links")
}

func TestNewSigner_rejects_weak_or_duplicate_keys(t *testing.T) {
	t.Parallel()
	for name, keys := range map[string][]signedlink.Key{
		"none":      nil,
		"short":     {{ID: "k", Secret: []byte("short")}},
		"duplicate": {keyOne, keyOne},
		"no_id":     {{ID: "", Secret: keyOne.Secret}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := signedlink.NewSigner(keys)
			assert.Error(t, err)
		})
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()
	testCases := []struct{ name, stored, want string }{
		{"Sintel.mp4", "x", "Sintel.mp4"},
		{"../../etc/passwd", "x", "....etcpasswd"},
		{"a\"b'c\r\nd", "x", "abcd"},
		{"", "Sintel/Sintel.mp4", "Sintel.mp4"},
		{"..", "folder/", "download"},
		{"  ", "", "download"},
		{"Fïlm ½.mkv", "x", "Fïlm ½.mkv"},
	}
	for _, tc := range testCases {
		assert.Equal(t, tc.want, signedlink.SanitizeName(tc.name, tc.stored), "%q/%q", tc.name, tc.stored)
	}
}
