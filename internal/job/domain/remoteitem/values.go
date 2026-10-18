package remoteitem

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"
)

// ID identifies a Remote Item (UUIDv7).
type ID struct{ s string }

func NewID(s string) (ID, error) {
	if _, err := uuid.Parse(s); err != nil {
		return ID{}, fmt.Errorf("invalid remote item ID %q", s)
	}
	return ID{s: strings.ToLower(s)}, nil
}

func (id ID) String() string { return id.s }
func (id ID) IsZero() bool   { return id == ID{} }

// JobID identifies a Job; the caller generates it (A-3).
type JobID struct{ s string }

func NewJobID(s string) (JobID, error) {
	if _, err := uuid.Parse(s); err != nil {
		return JobID{}, InvalidJobIDError{Given: s}
	}
	return JobID{s: strings.ToLower(s)}, nil
}

func (id JobID) String() string { return id.s }
func (id JobID) IsZero() bool   { return id == JobID{} }

// ProviderID names a Provider Account.
type ProviderID struct{ s string }

func NewProviderID(s string) (ProviderID, error) {
	if s == "" {
		return ProviderID{}, errors.New("empty provider ID")
	}
	return ProviderID{s: s}, nil
}

func (id ProviderID) String() string { return id.s }
func (id ProviderID) IsZero() bool   { return id == ProviderID{} }

// ProviderItemID is the provider's identifier of a Remote Item.
type ProviderItemID struct{ s string }

func NewProviderItemID(s string) (ProviderItemID, error) {
	if s == "" {
		return ProviderItemID{}, errors.New("empty provider item ID")
	}
	return ProviderItemID{s: s}, nil
}

func (id ProviderItemID) String() string { return id.s }
func (id ProviderItemID) IsZero() bool   { return id == ProviderItemID{} }

// Kind is the Job Kind (spec §2).
type Kind struct{ s string }

var (
	KindTorrent = Kind{"torrent"}
	KindNZB     = Kind{"nzb"}
	KindWeb     = Kind{"web"}
)

func NewKindFromString(s string) (Kind, error) {
	for _, k := range []Kind{KindTorrent, KindNZB, KindWeb} {
		if k.s == s {
			return k, nil
		}
	}
	return Kind{}, fmt.Errorf("unknown kind %q", s)
}

func (k Kind) String() string { return k.s }
func (k Kind) IsZero() bool   { return k == Kind{} }

// Origin says whether a job created its item or joined it.
type Origin struct{ s string }

var (
	OriginCreated = Origin{"created"}
	OriginJoined  = Origin{"joined"}
)

func NewOriginFromString(s string) (Origin, error) {
	switch s {
	case OriginCreated.s:
		return OriginCreated, nil
	case OriginJoined.s:
		return OriginJoined, nil
	}
	return Origin{}, fmt.Errorf("unknown origin %q", s)
}

func (o Origin) String() string { return o.s }

// FailureReason explains a failed item or a failed job in history, e.g.
// provider-rejected{detail}, submission-lost, merged-into{item}.
type FailureReason struct {
	code   string
	detail string
}

const (
	FailureProviderRejected = "provider-rejected"
	FailureProviderFailed   = "provider-failed"
	FailureSubmissionLost   = "submission-lost"
	FailureMergedInto       = "merged-into"
	FailureMergedRefused    = "merged-into-refused"
	FailureBlockedByDelete  = "blocked-by-deletion"
)

func NewFailureReason(code, detail string) FailureReason {
	return FailureReason{code: code, detail: detail}
}

func (f FailureReason) Code() string   { return f.code }
func (f FailureReason) Detail() string { return f.detail }
func (f FailureReason) IsZero() bool   { return f == FailureReason{} }

// BundleFormat names an archive format, not a provider (ADR 0011 rule 3).
type BundleFormat struct{ s string }

var (
	BundleFormatZip = BundleFormat{"zip"}
	BundleFormatRAR = BundleFormat{"rar"}
)

func NewBundleFormatFromString(s string) (BundleFormat, error) {
	switch s {
	case "zip":
		return BundleFormatZip, nil
	case "rar":
		return BundleFormatRAR, nil
	}
	return BundleFormat{}, fmt.Errorf("unknown bundle format %q", s)
}

func (f BundleFormat) String() string { return f.s }

// BundleAvailability says a provider-built archive of the item exists.
type BundleAvailability struct {
	format         BundleFormat
	rangeSupported bool
}

func NewBundleAvailability(format BundleFormat, rangeSupported bool) BundleAvailability {
	return BundleAvailability{format: format, rangeSupported: rangeSupported}
}

func (b BundleAvailability) Format() BundleFormat { return b.format }
func (b BundleAvailability) RangeSupported() bool { return b.rangeSupported }

// RemoteFile is a file inside the item; FileID is the provider's ID (ADR 0011 rule 5).
type RemoteFile struct {
	fileID    string
	path      string
	sizeBytes int64
}

func NewRemoteFile(fileID, path string, sizeBytes int64) (RemoteFile, error) {
	if fileID == "" {
		return RemoteFile{}, errors.New("empty file ID")
	}
	return RemoteFile{fileID: fileID, path: path, sizeBytes: sizeBytes}, nil
}

func (f RemoteFile) FileID() string   { return f.fileID }
func (f RemoteFile) Path() string     { return f.path }
func (f RemoteFile) SizeBytes() int64 { return f.sizeBytes }

// StallSource says who detected a stall.
type StallSource struct{ s string }

var (
	StallSourceNone     = StallSource{}
	StallSourceProvider = StallSource{"provider"}
	StallSourceDebroxy  = StallSource{"debroxy"}
)

func NewStallSourceFromString(s string) (StallSource, error) {
	switch s {
	case "":
		return StallSourceNone, nil
	case "provider":
		return StallSourceProvider, nil
	case "debroxy":
		return StallSourceDebroxy, nil
	}
	return StallSource{}, fmt.Errorf("unknown stall source %q", s)
}

func (s StallSource) String() string { return s.s }

// Telemetry is volatile progress data, not part of the Lifecycle (spec §2).
type Telemetry struct {
	Progress       float64
	BytesPerSecond int64
	ETA            *time.Duration
	Peers          *int
	Stalled        bool
	StallSource    StallSource
}

// Role of an actor, as job sees it.
type Role struct{ s string }

var (
	RoleAdmin = Role{"admin"}
	RoleUser  = Role{"user"}
)

func NewRoleFromString(s string) (Role, error) {
	switch s {
	case "admin":
		return RoleAdmin, nil
	case "user":
		return RoleUser, nil
	}
	return Role{}, fmt.Errorf("unknown role %q", s)
}

// Actor is the user performing an action.
type Actor struct {
	UserID string
	Role   Role
}

func (r Role) String() string { return r.s }

func (a Actor) IsAdmin() bool { return a.Role == RoleAdmin }
