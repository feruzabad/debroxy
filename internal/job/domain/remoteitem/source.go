package remoteitem

import (
	"bytes"
	"crypto/md5"  //nolint:gosec // TorBox identifies web items by the MD5 of their URL (torbox.md T11)
	"crypto/sha1" //nolint:gosec // BitTorrent v1 info hashes are SHA-1 by definition
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// SourceType says what a user submitted (spec §2 Source).
type SourceType struct{ s string }

var (
	SourceMagnet      = SourceType{"magnet"}
	SourceTorrentFile = SourceType{"torrent-file"}
	SourceNZBFile     = SourceType{"nzb-file"}
	SourceNZBURL      = SourceType{"nzb-url"}
	SourceWebURL      = SourceType{"web-url"}
)

func NewSourceTypeFromString(s string) (SourceType, error) {
	for _, t := range []SourceType{SourceMagnet, SourceTorrentFile, SourceNZBFile, SourceNZBURL, SourceWebURL} {
		if t.s == s {
			return t, nil
		}
	}
	return SourceType{}, InvalidSourceError{Reason: fmt.Sprintf("unknown source type %q", s)}
}

func (t SourceType) String() string { return t.s }
func (t SourceType) IsZero() bool   { return t == SourceType{} }

// Kind of the job a source creates.
func (t SourceType) Kind() Kind {
	switch t {
	case SourceNZBFile, SourceNZBURL:
		return KindNZB
	case SourceWebURL:
		return KindWeb
	}
	return KindTorrent
}

// Source is what a user submitted, exactly as given; the provider receives
// the URL as the first submitter gave it (spec §5.1).
type Source struct {
	sourceType SourceType
	magnet     string
	file       []byte
	fileName   string
	url        string
	password   string
}

func NewMagnetSource(magnet string) Source { return Source{sourceType: SourceMagnet, magnet: magnet} }
func NewTorrentFileSource(file []byte, fileName string) Source {
	return Source{sourceType: SourceTorrentFile, file: file, fileName: fileName}
}
func NewNZBFileSource(file []byte, fileName string) Source {
	return Source{sourceType: SourceNZBFile, file: file, fileName: fileName}
}
func NewNZBURLSource(rawURL string) Source { return Source{sourceType: SourceNZBURL, url: rawURL} }

// NewWebURLSource: the password is used only by the first submitter (spec §5.1).
func NewWebURLSource(rawURL, password string) Source {
	return Source{sourceType: SourceWebURL, url: rawURL, password: password}
}

func (s Source) Type() SourceType { return s.sourceType }
func (s Source) Magnet() string   { return s.magnet }
func (s Source) File() []byte     { return s.file }
func (s Source) FileName() string { return s.fileName }
func (s Source) URL() string      { return s.url }
func (s Source) Password() string { return s.password }
func (s Source) IsZero() bool     { return s.sourceType.IsZero() }

// Fingerprint identifies a source's content for deduplication (DEC-07,
// ADR 0004 rule 8).
type Fingerprint struct{ s string }

func NewFingerprintFromString(s string) (Fingerprint, error) {
	for _, prefix := range []string{"btih:", "btmh:", "nzb:", "nzburl:", "url:"} {
		if strings.HasPrefix(s, prefix) && len(s) > len(prefix) {
			return Fingerprint{s: s}, nil
		}
	}
	return Fingerprint{}, fmt.Errorf("invalid fingerprint %q", s)
}

func (f Fingerprint) String() string { return f.s }
func (f Fingerprint) IsZero() bool   { return f == Fingerprint{} }

// TorrentHash is the btih of a torrent fingerprint, the hash providers'
// cached checks take; ok is false for every other fingerprint.
func (f Fingerprint) TorrentHash() (hash string, ok bool) {
	return strings.CutPrefix(f.s, "btih:")
}

// Identify computes the fingerprint and the provider hash of a source. The
// provider hash is the btih for torrents (empty for v2-only torrents) and
// the MD5 of the URL string as given for web URLs (spec §5.1); NZB sources
// have none (ASM-U02).
func Identify(source Source) (Fingerprint, string, error) {
	switch source.sourceType {
	case SourceMagnet:
		return identifyMagnet(source.magnet)
	case SourceTorrentFile:
		return identifyTorrentFile(source.file)
	case SourceNZBFile:
		if len(source.file) == 0 {
			return Fingerprint{}, "", InvalidSourceError{Reason: "empty NZB file"}
		}
		sum := sha256.Sum256(source.file)
		return Fingerprint{s: "nzb:" + hex.EncodeToString(sum[:])}, "", nil
	case SourceNZBURL:
		normalized, err := NormalizeURL(source.url)
		if err != nil {
			return Fingerprint{}, "", err
		}
		return Fingerprint{s: "nzburl:" + normalized}, "", nil
	case SourceWebURL:
		normalized, err := NormalizeURL(source.url)
		if err != nil {
			return Fingerprint{}, "", err
		}
		sum := md5.Sum([]byte(source.url)) //nolint:gosec // provider convention, not security
		return Fingerprint{s: "url:" + normalized}, hex.EncodeToString(sum[:]), nil
	}
	return Fingerprint{}, "", InvalidSourceError{Reason: "no source"}
}

// NormalizeURL lowercases scheme and host, drops the default port and the
// fragment, and keeps the query (spec §5.1).
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", InvalidSourceError{Reason: "not an absolute http(s) URL"}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = host + ":" + port
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u.Host = host
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

func identifyMagnet(magnet string) (Fingerprint, string, error) {
	u, err := url.Parse(strings.TrimSpace(magnet))
	if err != nil || !strings.EqualFold(u.Scheme, "magnet") {
		return Fingerprint{}, "", InvalidSourceError{Reason: "not a magnet link"}
	}
	var btmh string
	for _, xt := range u.Query()["xt"] {
		lower := strings.ToLower(xt)
		switch {
		case strings.HasPrefix(lower, "urn:btih:"):
			hash, err := btihHex(xt[len("urn:btih:"):])
			if err != nil {
				return Fingerprint{}, "", err
			}
			return Fingerprint{s: "btih:" + hash}, hash, nil
		case strings.HasPrefix(lower, "urn:btmh:"):
			btmh = lower[len("urn:btmh:"):]
		}
	}
	if btmh != "" {
		if _, err := hex.DecodeString(btmh); err != nil || len(btmh) < 4 {
			return Fingerprint{}, "", InvalidSourceError{Reason: "malformed btmh"}
		}
		return Fingerprint{s: "btmh:" + btmh}, "", nil
	}
	return Fingerprint{}, "", InvalidSourceError{Reason: "magnet has no btih or btmh"}
}

// btihHex accepts 40 hex or 32 base32 characters and returns lowercase hex.
func btihHex(value string) (string, error) {
	switch len(value) {
	case 40:
		if _, err := hex.DecodeString(value); err == nil {
			return strings.ToLower(value), nil
		}
	case 32:
		if decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(value)); err == nil && len(decoded) == 20 {
			return hex.EncodeToString(decoded), nil
		}
	}
	return "", InvalidSourceError{Reason: "malformed btih"}
}

// identifyTorrentFile hashes the bencoded info dictionary: SHA-1 for v1 and
// hybrid torrents (btih), SHA-256 multihash for v2-only torrents (btmh).
func identifyTorrentFile(file []byte) (Fingerprint, string, error) {
	info, err := infoDictionary(file)
	if err != nil {
		return Fingerprint{}, "", InvalidSourceError{Reason: "not a torrent file: " + err.Error()}
	}
	keys, err := dictionaryKeys(info)
	if err != nil {
		return Fingerprint{}, "", InvalidSourceError{Reason: "not a torrent file: " + err.Error()}
	}
	if !keys["pieces"] && keys["file tree"] {
		sum := sha256.Sum256(info)
		return Fingerprint{s: "btmh:1220" + hex.EncodeToString(sum[:])}, "", nil
	}
	sum := sha1.Sum(info) //nolint:gosec // BitTorrent v1 info hash
	hash := hex.EncodeToString(sum[:])
	return Fingerprint{s: "btih:" + hash}, hash, nil
}

// infoDictionary returns the raw bytes of the top-level "info" value.
func infoDictionary(data []byte) ([]byte, error) {
	if len(data) == 0 || data[0] != 'd' {
		return nil, fmt.Errorf("not a bencoded dictionary")
	}
	pos := 1
	for pos < len(data) && data[pos] != 'e' {
		key, next, err := bencodeString(data, pos)
		if err != nil {
			return nil, err
		}
		end, err := skipBencode(data, next, 0)
		if err != nil {
			return nil, err
		}
		if key == "info" {
			if data[next] != 'd' {
				return nil, fmt.Errorf("info is not a dictionary")
			}
			return data[next:end], nil
		}
		pos = end
	}
	return nil, fmt.Errorf("no info dictionary")
}

func dictionaryKeys(dict []byte) (map[string]bool, error) {
	keys := map[string]bool{}
	pos := 1
	for pos < len(dict) && dict[pos] != 'e' {
		key, next, err := bencodeString(dict, pos)
		if err != nil {
			return nil, err
		}
		keys[key] = true
		if pos, err = skipBencode(dict, next, 0); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

// maxBencodeDepth guards against hostile nesting.
const maxBencodeDepth = 64

// skipBencode returns the end position of the value starting at pos.
func skipBencode(data []byte, pos, depth int) (int, error) {
	if depth > maxBencodeDepth {
		return 0, fmt.Errorf("nesting too deep")
	}
	if pos >= len(data) {
		return 0, fmt.Errorf("unexpected end")
	}
	switch c := data[pos]; {
	case c == 'i':
		end := bytes.IndexByte(data[pos:], 'e')
		if end < 0 {
			return 0, fmt.Errorf("unterminated integer")
		}
		return pos + end + 1, nil
	case c == 'l' || c == 'd':
		pos++
		for pos < len(data) && data[pos] != 'e' {
			var err error
			if c == 'd' {
				if _, pos, err = bencodeString(data, pos); err != nil {
					return 0, err
				}
			}
			if pos, err = skipBencode(data, pos, depth+1); err != nil {
				return 0, err
			}
		}
		if pos >= len(data) {
			return 0, fmt.Errorf("unterminated container")
		}
		return pos + 1, nil
	case c >= '0' && c <= '9':
		_, end, err := bencodeString(data, pos)
		return end, err
	}
	return 0, fmt.Errorf("invalid bencode at %d", pos)
}

func bencodeString(data []byte, pos int) (string, int, error) {
	colon := bytes.IndexByte(data[pos:], ':')
	if colon < 0 {
		return "", 0, fmt.Errorf("malformed string")
	}
	length, err := strconv.Atoi(string(data[pos : pos+colon]))
	start := pos + colon + 1
	if err != nil || length < 0 || start+length > len(data) {
		return "", 0, fmt.Errorf("malformed string length")
	}
	return string(data[start : start+length]), start + length, nil
}
