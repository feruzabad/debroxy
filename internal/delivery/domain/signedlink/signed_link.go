// Package signedlink is the Signed Link value of spec §7.1 (DEC-17,
// ADR 0010 rules 11–13): an HMAC-signed, expiring capability to download
// one target of one job. It carries no provider data.
package signedlink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

// version is the payload format version (field v).
const version = "1"

// minimumKeyBytes: an HMAC-SHA256 key shorter than the hash is weak.
const minimumKeyBytes = 32

var (
	// ErrInvalid: the token is malformed, its key is unknown, or its
	// signature does not match.
	ErrInvalid = commonerrors.NewAuthorizationError("the download link is not valid", "invalid-signed-link")
	// ErrExpired: the token was valid but its expiry has passed.
	ErrExpired = commonerrors.NewAuthorizationError("the download link has expired", "signed-link-expired")
)

// Key is one signing key. The first key of a Signer signs; all verify.
type Key struct {
	ID     string
	Secret []byte
}

// Target is a file of the job, or its bundle (ADR 0011).
type Target struct {
	fileID string
	bundle bool
}

func FileTarget(fileID string) (Target, error) {
	if fileID == "" || strings.ContainsAny(fileID, "\n") {
		return Target{}, errors.New("invalid file ID")
	}
	return Target{fileID: fileID}, nil
}

func BundleTarget() Target { return Target{bundle: true} }

func (t Target) FileID() string { return t.fileID }
func (t Target) IsBundle() bool { return t.bundle }

// encode is the tgt field: "f:<FileID>" or "b".
func (t Target) encode() string {
	if t.bundle {
		return "b"
	}
	return "f:" + t.fileID
}

func decodeTarget(s string) (Target, error) {
	if s == "b" {
		return BundleTarget(), nil
	}
	if fileID, ok := strings.CutPrefix(s, "f:"); ok {
		return FileTarget(fileID)
	}
	return Target{}, ErrInvalid
}

// Link is what a verified token grants.
type Link struct {
	KeyID     string
	UserID    string
	JobID     string
	Target    Target
	ExpiresAt time.Time
}

// Signer signs with its first key and verifies with all of them, so keys
// can rotate; removing a key revokes every link it signed (rule 12).
type Signer struct {
	keys []Key
}

func NewSigner(keys []Key) (Signer, error) {
	if len(keys) == 0 {
		return Signer{}, errors.New("no signing key")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		switch {
		case key.ID == "" || strings.ContainsAny(key.ID, "\n"):
			return Signer{}, fmt.Errorf("invalid signing key ID %q", key.ID)
		case seen[key.ID]:
			return Signer{}, fmt.Errorf("duplicate signing key ID %q", key.ID)
		case len(key.Secret) < minimumKeyBytes:
			return Signer{}, fmt.Errorf("signing key %q is shorter than %d bytes", key.ID, minimumKeyBytes)
		}
		seen[key.ID] = true
	}
	return Signer{keys: append([]Key(nil), keys...)}, nil
}

// Sign returns base64url(payload) "." base64url(HMAC-SHA256(key, payload)).
// The payload holds the fields v, kid, uid, jid, tgt and exp, one per line.
func (s Signer) Sign(userID, jobID string, target Target, expiresAt time.Time) (string, error) {
	if userID == "" || jobID == "" || strings.ContainsAny(userID+jobID, "\n") || target == (Target{}) {
		return "", errors.New("incomplete signed link")
	}
	key := s.keys[0]
	payload := strings.Join([]string{version, key.ID, userID, jobID, target.encode(), strconv.FormatInt(expiresAt.Unix(), 10)}, "\n")
	return encode([]byte(payload)) + "." + encode(sign(key.Secret, []byte(payload))), nil
}

// Verify checks the signature in constant time, then the key and the
// expiry (rule 14).
func (s Signer) Verify(token string, now time.Time) (Link, error) {
	encodedPayload, encodedSignature, ok := strings.Cut(token, ".")
	if !ok {
		return Link{}, ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return Link{}, ErrInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil {
		return Link{}, ErrInvalid
	}
	fields := strings.Split(string(payload), "\n")
	if len(fields) != 6 || fields[0] != version {
		return Link{}, ErrInvalid
	}
	key, ok := s.key(fields[1])
	if !ok || !hmac.Equal(signature, sign(key.Secret, payload)) {
		return Link{}, ErrInvalid
	}
	target, err := decodeTarget(fields[4])
	if err != nil {
		return Link{}, ErrInvalid
	}
	exp, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil {
		return Link{}, ErrInvalid
	}
	link := Link{KeyID: key.ID, UserID: fields[2], JobID: fields[3], Target: target, ExpiresAt: time.Unix(exp, 0).UTC()}
	if !now.Before(link.ExpiresAt) {
		return Link{}, ErrExpired
	}
	return link, nil
}

func (s Signer) key(id string) (Key, bool) {
	for _, key := range s.keys {
		if key.ID == id {
			return key, true
		}
	}
	return Key{}, false
}

func sign(secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return mac.Sum(nil)
}

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
