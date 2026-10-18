package realdebrid

import (
	"strconv"
	"strings"

	"github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// torrentInfo is a /torrents/info/{id} answer (realdebrid.md R04). The
// `added` and `ended` timestamps are local time labelled Z (RD-N03) and
// are not read.
type torrentInfo struct {
	ID               string        `json:"id"`
	Filename         string        `json:"filename"`
	OriginalFilename string        `json:"original_filename"`
	Hash             string        `json:"hash"`
	Bytes            int64         `json:"bytes"`
	Progress         float64       `json:"progress"`
	Status           string        `json:"status"`
	Speed            int64         `json:"speed"`
	Seeders          *int          `json:"seeders"`
	Files            []torrentFile `json:"files"`
	Links            []string      `json:"links"`
}

// torrentFile: IDs are 1-based in torrent order; paths start with "/".
type torrentFile struct {
	ID       int64  `json:"id"`
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Selected int    `json:"selected"`
}

// listItem is a /torrents entry. Its status may be stale (RD-N04) and it
// has no files, so listings read info for every item they do not know as
// finished.
type listItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// stateTable is spec §8.2: one explicit map from Real-Debrid status to
// the neutral state.
var stateTable = map[string]in.State{
	"magnet_conversion":       in.StateResolvingMetadata,
	"waiting_files_selection": in.StateAwaitingFileSelection,
	"queued":                  in.StateQueued,
	"downloading":             in.StateDownloading,
	"uploading":               in.StateProcessing,
	"compressing":             in.StateProcessing,
	"downloaded":              in.StateReady,
	"magnet_error":            in.StateFailed,
	"error":                   in.StateFailed,
	"virus":                   in.StateFailed,
	"dead":                    in.StateFailed,
}

func (g *Gateway) mapState(status string) in.State {
	state, known := stateTable[status]
	if !known {
		g.logOnce("state:"+status, "unknown Real-Debrid status; the item keeps its state", "status", status)
		return in.StateUnknown
	}
	return state
}

// isFinished: ready and failed items do not change any more.
func isFinished(state in.State) bool {
	return state == in.StateReady || state == in.StateFailed
}

// observation maps an info answer. Only selected files count. More than one
// selected file is delivered as one RAR (realdebrid.md §4): bundle only,
// no per-file delivery (DEC-29). RAR range support is assumed (ASM-R15).
func (g *Gateway) observation(info torrentInfo) in.Observation {
	state := g.mapState(info.Status)
	var selected []in.RemoteFile
	for _, f := range info.Files {
		if f.Selected == 1 {
			selected = append(selected, in.RemoteFile{FileID: strconv.FormatInt(f.ID, 10), Path: strings.TrimPrefix(f.Path, "/"), SizeBytes: f.Bytes})
		}
	}
	name := info.OriginalFilename
	if name == "" {
		name = info.Filename
	}
	observation := in.Observation{
		ProviderItemID:  info.ID,
		Kind:            in.KindTorrent,
		ProviderHash:    strings.ToLower(info.Hash),
		State:           state,
		Name:            name,
		SizeBytes:       info.Bytes,
		Files:           selected,
		PerFileDelivery: len(selected) == 1,
		Telemetry: in.Telemetry{
			Progress: info.Progress / 100, BytesPerSecond: info.Speed, Peers: info.Seeders,
		},
	}
	if len(selected) > 1 {
		observation.Bundle = &in.Bundle{Format: in.BundleRAR, RangeSupported: true}
	}
	if state == in.StateFailed {
		observation.FailureReason = info.Status
	}
	return observation
}
