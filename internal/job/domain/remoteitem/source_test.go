package remoteitem_test

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
)

func TestIdentify_magnets(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name, magnet, fingerprint, hash string
	}{
		{"hex_btih", "magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0&dn=debian", "btih:7acf8fb590b2060dd9c3146ef770169d593433b0", "7acf8fb590b2060dd9c3146ef770169d593433b0"},
		{"uppercase_hex", "magnet:?xt=urn:btih:7ACF8FB590B2060DD9C3146EF770169D593433B0", "btih:7acf8fb590b2060dd9c3146ef770169d593433b0", "7acf8fb590b2060dd9c3146ef770169d593433b0"},
		{"base32_btih", "magnet:?xt=urn:btih:PLHY7NMQWIDA3WODCRXPO4AWTVMTIM5Q", "btih:7acf8fb590b2060dd9c3146ef770169d593433b0", "7acf8fb590b2060dd9c3146ef770169d593433b0"},
		{"btmh_only", "magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e", "btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e", ""},
		{"btih_wins_over_btmh", "magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e&xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433b0", "btih:7acf8fb590b2060dd9c3146ef770169d593433b0", "7acf8fb590b2060dd9c3146ef770169d593433b0"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fingerprint, hash, err := remoteitem.Identify(remoteitem.NewMagnetSource(tc.magnet))

			require.NoError(t, err)
			assert.Equal(t, tc.fingerprint, fingerprint.String())
			assert.Equal(t, tc.hash, hash)
		})
	}
}

func TestIdentify_rejects_bad_magnets(t *testing.T) {
	t.Parallel()

	for _, magnet := range []string{"", "http://example.org", "magnet:?dn=x", "magnet:?xt=urn:btih:zz", "magnet:?xt=urn:btih:7acf8fb590b2060dd9c3146ef770169d593433bg"} {
		_, _, err := remoteitem.Identify(remoteitem.NewMagnetSource(magnet))
		var invalid remoteitem.InvalidSourceError
		assert.ErrorAs(t, err, &invalid, magnet)
	}
}

// Synthetic torrents (single-file, multi-file and hybrid v1+v2), hex-encoded;
// each info hash is the SHA-1 of the bencoded info dictionary, computed
// independently of this code.
func TestIdentify_torrent_files(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ torrent, btih string }{
		"single_file": {
			"64383a616e6e6f756e636532363a7564703a2f2f747261636b65722e696e76616c69643a31333337343a696e666f64363a6c656e677468693130343835373665343a6e616d6531363a646562726f78792d746573742e69736f31323a7069656365206c656e6774686932363231343465363a70696563657338303a" + strings.Repeat("01", 80) + "6565",
			"fcfc69bffa95f4640dcd73aba5bcca2574aaed3f",
		},
		"multi_file": {
			"64383a616e6e6f756e636532363a7564703a2f2f747261636b65722e696e76616c69643a31333337343a696e666f64353a66696c65736c64363a6c656e677468693130303065343a706174686c313a61373a6f6e652e6d6b76656564363a6c656e67746869323065343a706174686c373a74776f2e737274656565343a6e616d6531333a646562726f78792d6d756c746931323a7069656365206c656e67746869313633383465363a70696563657332303a" + strings.Repeat("02", 20) + "6565",
			"8f97a85be6a70df9df87f978e9d706741703ca89",
		},
		"hybrid_v1_v2": {
			"64383a616e6e6f756e636532363a7564703a2f2f747261636b65722e696e76616c69643a31333337343a696e666f64393a66696c6520747265656431363a646562726f78792d746573742e69736f64303a64363a6c656e67746869313034383537366531313a70696563657320726f6f7433323a" + strings.Repeat("03", 32) + "656565363a6c656e67746869313034383537366531323a6d6574612076657273696f6e693265343a6e616d6531363a646562726f78792d746573742e69736f31323a7069656365206c656e6774686932363231343465363a70696563657338303a" + strings.Repeat("01", 80) + "6565",
			"ec7182868a0b6d7932e08770cd1678fc2fa23d49",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			content, err := hex.DecodeString(tc.torrent)
			require.NoError(t, err)

			fingerprint, hash, err := remoteitem.Identify(remoteitem.NewTorrentFileSource(content, name+".torrent"))

			require.NoError(t, err)
			assert.Equal(t, "btih:"+tc.btih, fingerprint.String())
			assert.Equal(t, tc.btih, hash)
		})
	}
}

func TestIdentify_v2_only_torrent(t *testing.T) {
	t.Parallel()
	torrent := []byte("d4:infod9:file treede12:meta versioni2e4:name1:xee")

	fingerprint, hash, err := remoteitem.Identify(remoteitem.NewTorrentFileSource(torrent, "x.torrent"))

	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(fingerprint.String(), "btmh:1220"))
	assert.Empty(t, hash, "v2-only torrents have no btih")
}

func TestIdentify_rejects_broken_torrent_files(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"", "not bencode", "d4:name1:xe", "d4:info", "d4:infoi1ee", "d4:info" + strings.Repeat("l", 100) + strings.Repeat("e", 100)} {
		_, _, err := remoteitem.Identify(remoteitem.NewTorrentFileSource([]byte(content), "x"))
		var invalid remoteitem.InvalidSourceError
		assert.ErrorAs(t, err, &invalid, content)
	}
}

func TestIdentify_nzb_and_web(t *testing.T) {
	t.Parallel()

	nzb, nzbHash, err := remoteitem.Identify(remoteitem.NewNZBFileSource([]byte("<nzb/>"), "x.nzb"))
	require.NoError(t, err)
	assert.Equal(t, "nzb:97a2b50ea93204052efc9e6691691a80144a679a0f0d554fd50ec023c4700f66", nzb.String(), "SHA-256 of the raw bytes (ASM-U03)")
	assert.Empty(t, nzbHash)

	web, webHash, err := remoteitem.Identify(remoteitem.NewWebURLSource("https://archive.org/details/AvengeDe1942", "secret"))
	require.NoError(t, err)
	assert.Equal(t, "url:https://archive.org/details/AvengeDe1942", web.String())
	assert.Equal(t, "72e1ebf4583c7aa997f05156c61a2c7d", webHash, "TorBox's hash of this URL (torbox.md T11)")
	other, _, err := remoteitem.Identify(remoteitem.NewWebURLSource("https://archive.org/details/AvengeDe1942", "different"))
	require.NoError(t, err)
	assert.Equal(t, web, other, "the password is not part of the fingerprint")
}

func TestNormalizeURL(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"HTTPS://Example.ORG:443/Path?Q=1#frag": "https://example.org/Path?Q=1",
		"http://example.org:80":                 "http://example.org/",
		"http://example.org:8080/x":             "http://example.org:8080/x",
		"https://[2001:DB8::1]/x":               "https://[2001:db8::1]/x",
	} {
		normalized, err := remoteitem.NormalizeURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, normalized, raw)
	}
	_, err := remoteitem.NormalizeURL("ftp://example.org/x")
	assert.Error(t, err)
}
