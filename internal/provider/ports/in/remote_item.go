package in

import "time"

// Kind is the job kind as provider sees it (spec §2 Job Kind).
type Kind string

const (
	KindTorrent Kind = "torrent"
	KindNZB     Kind = "nzb"
	KindWeb     Kind = "web"
)

// SourceType says what a Source carries.
type SourceType string

const (
	SourceMagnet      SourceType = "magnet"
	SourceTorrentFile SourceType = "torrent-file"
	SourceNZBFile     SourceType = "nzb-file"
	SourceNZBURL      SourceType = "nzb-url"
	SourceWebURL      SourceType = "web-url"
)

// Source is what a user submitted, as given (spec §2 Source). The provider
// receives the URL exactly as the first submitter gave it (spec §5.1).
type Source struct {
	Type     SourceType
	Magnet   string
	File     []byte
	FileName string
	URL      string
	// Password applies to web URLs only.
	Password string
}

// State is the provider-neutral Lifecycle state of spec §5.2 that a
// provider can report.
type State string

const (
	StateQueued                State = "queued"
	StateResolvingMetadata     State = "resolving_metadata"
	StateAwaitingFileSelection State = "awaiting_file_selection"
	StateDownloading           State = "downloading"
	StateProcessing            State = "processing"
	StateReady                 State = "ready"
	StateFailed                State = "failed"
	// StateUnknown: the provider reported a value the adapter does not know.
	// The item keeps its state (DEC-09).
	StateUnknown State = "unknown"
)

// Observation is one provider snapshot of a remote item in neutral terms
// (spec §5.6). ObservedAt is debroxy's clock at request start (DEC-09b).
type Observation struct {
	ProviderItemID  string
	Kind            Kind
	ProviderHash    string
	State           State
	Name            string
	SizeBytes       int64
	Files           []RemoteFile
	Bundle          *Bundle
	PerFileDelivery bool
	ExpiresAt       *time.Time
	Telemetry       Telemetry
	FailureReason   string
	ObservedAt      time.Time
}

// RemoteFile is a file inside a remote item; FileID is the provider's ID,
// never a position (spec §5.9).
type RemoteFile struct {
	FileID    string
	Path      string
	SizeBytes int64
}

// BundleFormat of a provider-built archive.
type BundleFormat string

const (
	BundleZip BundleFormat = "zip"
	BundleRAR BundleFormat = "rar"
)

type Bundle struct {
	Format         BundleFormat
	RangeSupported bool
}

// Telemetry is volatile progress data (spec §2).
type Telemetry struct {
	Progress       float64
	BytesPerSecond int64
	ETA            *time.Duration
	Peers          *int
	Stalled        bool
}

// RemoteItemListing is the result of one listing (spec §5.6 step 1).
// Complete is true only when every page and kind was listed without error.
type RemoteItemListing struct {
	Observations []Observation
	Complete     bool
}
