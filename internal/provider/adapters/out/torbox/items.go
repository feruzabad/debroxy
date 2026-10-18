package torbox

import (
	"strconv"
	"strings"
	"time"

	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// etaUnknown is TorBox's "unknown" ETA sentinel (100 days, TB-N08).
const etaUnknown = 8640000

// torrentItem is a /torrents/mylist entry (torbox.md §3). Unknown fields are ignored (NFR-11).
type torrentItem struct {
	ID               int64      `json:"id"`
	Hash             string     `json:"hash"`
	Name             string     `json:"name"`
	Size             int64      `json:"size"`
	DownloadState    string     `json:"download_state"`
	Peers            int        `json:"peers"`
	Progress         float64    `json:"progress"`
	DownloadSpeed    int64      `json:"download_speed"`
	ETA              int64      `json:"eta"`
	ExpiresAt        *string    `json:"expires_at"`
	DownloadPresent  bool       `json:"download_present"`
	DownloadFinished bool       `json:"download_finished"`
	Files            []itemFile `json:"files"`
}

// webItem is a /webdl/mylist entry.
type webItem struct {
	ID               int64      `json:"id"`
	Hash             string     `json:"hash"`
	Name             string     `json:"name"`
	Size             int64      `json:"size"`
	DownloadState    string     `json:"download_state"`
	Progress         float64    `json:"progress"`
	DownloadSpeed    int64      `json:"download_speed"`
	ETA              int64      `json:"eta"`
	ExpiresAt        *string    `json:"expires_at"`
	DownloadPresent  bool       `json:"download_present"`
	DownloadFinished bool       `json:"download_finished"`
	Error            *string    `json:"error"`
	Files            []itemFile `json:"files"`
}

// itemFile: id is 0-based and is not the position in the .torrent (spec §5.9).
type itemFile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// queuedItem is a /queued/getqueued entry. Its shape is unverified
// (ASM-T20), so only the fields every item has are read.
type queuedItem struct {
	ID   int64  `json:"id"`
	Hash string `json:"hash"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// stateTable is spec §8.1: one explicit map from TorBox download_state to
// the neutral state, plus whether TorBox reports a stall.
var stateTable = map[string]struct {
	state   in.State
	stalled bool
}{
	"metaDL":             {in.StateResolvingMetadata, false},
	"metaDL_done":        {in.StateResolvingMetadata, false},
	"stalledDL":          {in.StateDownloading, true},
	"stalled (no seeds)": {in.StateDownloading, true},
	"downloading":        {in.StateDownloading, false},
	"checkingResumeData": {in.StateDownloading, false},
	"paused":             {in.StateDownloading, false},
	"processing":         {in.StateProcessing, false},
	"uploading":          {in.StateProcessing, false},
	"compressing":        {in.StateProcessing, false},
	"completed":          {in.StateReady, false},
	"cached":             {in.StateReady, false},
	"error":              {in.StateFailed, false},
}

// mapState applies the table. completed/cached count as ready only while
// the download is present; before that TorBox is still processing
// (TB-N09). A finished, present download is ready whatever the state says:
// TorBox seeds after completion and reports "uploading" or
// "uploading (no peers)" (TB-N15). An item error field means failed
// (ASM-T22).
func (g *Gateway) mapState(downloadState string, present, finished bool, itemError string) (in.State, bool) {
	if itemError != "" {
		return in.StateFailed, false
	}
	if strings.HasPrefix(downloadState, "failed") {
		return in.StateFailed, false
	}
	if present && finished {
		return in.StateReady, false
	}
	if strings.HasPrefix(downloadState, "uploading") {
		downloadState = "uploading"
	}
	entry, known := stateTable[downloadState]
	if !known {
		g.logOnce("state:"+downloadState, "unknown TorBox download_state; the item keeps its state", "download_state", downloadState)
		return in.StateUnknown, false
	}
	if entry.state == in.StateReady && !present {
		return in.StateProcessing, false
	}
	return entry.state, entry.stalled
}

func (g *Gateway) torrentObservation(item torrentItem) in.Observation {
	state, stalled := g.mapState(item.DownloadState, item.DownloadPresent, item.DownloadFinished, "")
	peers := item.Peers
	return in.Observation{
		ProviderItemID:  strconv.FormatInt(item.ID, 10),
		Kind:            in.KindTorrent,
		ProviderHash:    strings.ToLower(item.Hash),
		State:           state,
		Name:            item.Name,
		SizeBytes:       item.Size,
		Files:           files(item.Files),
		Bundle:          &in.Bundle{Format: in.BundleZip, RangeSupported: false},
		PerFileDelivery: true,
		ExpiresAt:       g.parseTime(item.ExpiresAt),
		Telemetry: in.Telemetry{
			Progress: item.Progress, BytesPerSecond: item.DownloadSpeed,
			ETA: eta(item.ETA), Peers: &peers, Stalled: stalled,
		},
	}
}

// webObservation: web items deliver their single file; TorBox's zip is
// verified for torrents only and not offered for web items (ADR 0011).
func (g *Gateway) webObservation(item webItem) in.Observation {
	itemError := ""
	if item.Error != nil {
		itemError = *item.Error
	}
	state, stalled := g.mapState(item.DownloadState, item.DownloadPresent, item.DownloadFinished, itemError)
	return in.Observation{
		ProviderItemID:  strconv.FormatInt(item.ID, 10),
		Kind:            in.KindWeb,
		ProviderHash:    strings.ToLower(item.Hash),
		State:           state,
		Name:            item.Name,
		SizeBytes:       item.Size,
		Files:           files(item.Files),
		PerFileDelivery: true,
		ExpiresAt:       g.parseTime(item.ExpiresAt),
		Telemetry:       in.Telemetry{Progress: item.Progress, BytesPerSecond: item.DownloadSpeed, ETA: eta(item.ETA), Stalled: stalled},
		FailureReason:   truncate(itemError, 200),
	}
}

func queuedObservation(item queuedItem) in.Observation {
	kind := in.KindTorrent
	switch strings.ToLower(item.Type) {
	case "webdl", "web", "webdownload":
		kind = in.KindWeb
	case "usenet":
		kind = in.KindNZB
	}
	return in.Observation{
		ProviderItemID: strconv.FormatInt(item.ID, 10),
		Kind:           kind,
		ProviderHash:   strings.ToLower(item.Hash),
		State:          in.StateQueued,
		Name:           item.Name,
	}
}

func files(items []itemFile) []in.RemoteFile {
	result := make([]in.RemoteFile, 0, len(items))
	for _, f := range items {
		result = append(result, in.RemoteFile{FileID: strconv.FormatInt(f.ID, 10), Path: f.Name, SizeBytes: f.Size})
	}
	return result
}

func eta(seconds int64) *time.Duration {
	if seconds <= 0 || seconds >= etaUnknown {
		return nil
	}
	d := time.Duration(seconds) * time.Second
	return &d
}

func (g *Gateway) parseTime(value *string) *time.Time {
	if value == nil || *value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		g.logOnce("time:"+*value, "unparseable TorBox timestamp", "value", *value)
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}
