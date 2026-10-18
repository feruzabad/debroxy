// Package sqlite stores the job context in the shared SQLite database
// (ADR 0002). Rows are the sqlc structs; marshal/unmarshal translate them
// to and from the domain (ddd-rules OUT-1).
package sqlite

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/feruzabad/debroxy/internal/job/adapters/out/sqlite/internal/sqlcgen"
	"github.com/feruzabad/debroxy/internal/job/domain/remoteitem"
)

func toMillis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func toNullMillis(t time.Time) sql.NullInt64 {
	if t.IsZero() {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: true}
}

func fromNullMillis(ms sql.NullInt64) time.Time {
	if !ms.Valid {
		return time.Time{}
	}
	return time.UnixMilli(ms.Int64).UTC()
}

func toNullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// sourceRow is the stored Source; the file is base64 in JSON.
type sourceRow struct {
	Type     string `json:"type"`
	Magnet   string `json:"magnet,omitempty"`
	File     string `json:"file,omitempty"`
	FileName string `json:"file_name,omitempty"`
	URL      string `json:"url,omitempty"`
	Password string `json:"password,omitempty"`
}

type submissionRow struct {
	StartedAt        int64   `json:"started_at"`
	AttemptStartedAt int64   `json:"attempt_started_at,omitempty"`
	Attempts         int     `json:"attempts"`
	NotBefore        int64   `json:"not_before,omitempty"`
	FollowUpsDue     []int64 `json:"follow_ups_due,omitempty"`
	DeferredBehind   string  `json:"deferred_behind,omitempty"`
	Cached           bool    `json:"cached,omitempty"`
}

type telemetryRow struct {
	Progress       float64 `json:"progress"`
	BytesPerSecond int64   `json:"bytes_per_second"`
	ETASeconds     *int64  `json:"eta_seconds,omitempty"`
	Peers          *int    `json:"peers,omitempty"`
	Stalled        bool    `json:"stalled,omitempty"`
	StallSource    string  `json:"stall_source,omitempty"`
}

func marshalRemoteItem(item *remoteitem.RemoteItem) (sqlcgen.UpsertRemoteItemParams, error) {
	params := sqlcgen.UpsertRemoteItemParams{
		ID:                   item.ID().String(),
		ProviderID:           item.ProviderID().String(),
		ProviderItemID:       toNullString(item.ProviderItemID().String()),
		ProviderHash:         item.ProviderHash(),
		Kind:                 item.Kind().String(),
		Fingerprint:          item.Fingerprint().String(),
		Name:                 item.Name(),
		SizeBytes:            item.SizeBytes(),
		State:                item.State().String(),
		FailureCode:          item.FailureReason().Code(),
		FailureDetail:        item.FailureReason().Detail(),
		PerFileDelivery:      boolToInt(item.PerFileDelivery()),
		LastProgressAt:       toMillis(item.LastProgressAt()),
		LastObservationAt:    toMillis(item.LastObservationAt()),
		ChargeHolderJobID:    toNullString(item.ChargeHolder().String()),
		ChargeHolderReleased: boolToInt(item.ChargeHolderReleased()),
		CreatedAt:            toMillis(item.CreatedAt()),
		DeletedAt:            toNullMillis(item.DeletedAt()),
	}
	if bundle := item.Bundle(); bundle != nil {
		params.BundleFormat = toNullString(bundle.Format().String())
		params.BundleRangeSupported = boolToInt(bundle.RangeSupported())
	}
	if expires := item.ExpiresAt(); expires != nil {
		params.ExpiresAt = toNullMillis(*expires)
	}
	if source := item.Source(); !source.IsZero() {
		encoded, err := json.Marshal(sourceRow{
			Type: source.Type().String(), Magnet: source.Magnet(), File: base64.StdEncoding.EncodeToString(source.File()),
			FileName: source.FileName(), URL: source.URL(), Password: source.Password(),
		})
		if err != nil {
			return params, err
		}
		params.SourceJson = sql.NullString{String: string(encoded), Valid: true}
	}
	if submission, ok := item.Submission(); ok {
		row := submissionRow{
			StartedAt: toMillis(submission.StartedAt), AttemptStartedAt: toMillis(submission.AttemptStartedAt),
			Attempts: submission.Attempts, NotBefore: toMillis(submission.NotBefore), DeferredBehind: submission.DeferredBehind.String(),
			Cached: submission.Cached,
		}
		if submission.AttemptStartedAt.IsZero() {
			row.AttemptStartedAt = 0
		}
		if submission.NotBefore.IsZero() {
			row.NotBefore = 0
		}
		for _, due := range submission.FollowUpsDue {
			row.FollowUpsDue = append(row.FollowUpsDue, toMillis(due))
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return params, err
		}
		params.SubmissionJson = sql.NullString{String: string(encoded), Valid: true}
	}
	telemetry := item.Telemetry()
	row := telemetryRow{
		Progress: telemetry.Progress, BytesPerSecond: telemetry.BytesPerSecond, Peers: telemetry.Peers,
		Stalled: telemetry.Stalled, StallSource: telemetry.StallSource.String(),
	}
	if telemetry.ETA != nil {
		seconds := int64(telemetry.ETA.Seconds())
		row.ETASeconds = &seconds
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		return params, err
	}
	params.TelemetryJson = string(encoded)
	return params, nil
}

func unmarshalRemoteItem(row sqlcgen.RemoteItem, files []sqlcgen.RemoteFile, jobs []sqlcgen.Job) (*remoteitem.RemoteItem, error) {
	fields := remoteitem.DatabaseFields{
		ProviderHash:         row.ProviderHash,
		Name:                 row.Name,
		SizeBytes:            row.SizeBytes,
		FailureReason:        remoteitem.NewFailureReason(row.FailureCode, row.FailureDetail),
		PerFileDelivery:      row.PerFileDelivery == 1,
		LastProgressAt:       fromMillis(row.LastProgressAt),
		LastObservationAt:    fromMillis(row.LastObservationAt),
		CreatedAt:            fromMillis(row.CreatedAt),
		DeletedAt:            fromNullMillis(row.DeletedAt),
		ChargeHolderReleased: row.ChargeHolderReleased == 1,
	}
	if row.FailureCode == "" {
		fields.FailureReason = remoteitem.FailureReason{}
	}
	var err error
	if fields.ID, err = remoteitem.NewID(row.ID); err != nil {
		return nil, err
	}
	if fields.ProviderID, err = remoteitem.NewProviderID(row.ProviderID); err != nil {
		return nil, err
	}
	if row.ProviderItemID.Valid {
		if fields.ProviderItemID, err = remoteitem.NewProviderItemID(row.ProviderItemID.String); err != nil {
			return nil, err
		}
	}
	if fields.Kind, err = remoteitem.NewKindFromString(row.Kind); err != nil {
		return nil, err
	}
	if fields.Fingerprint, err = remoteitem.NewFingerprintFromString(row.Fingerprint); err != nil {
		return nil, err
	}
	if fields.State, err = remoteitem.NewStateFromString(row.State); err != nil {
		return nil, err
	}
	if row.BundleFormat.Valid {
		format, err := remoteitem.NewBundleFormatFromString(row.BundleFormat.String)
		if err != nil {
			return nil, err
		}
		bundle := remoteitem.NewBundleAvailability(format, row.BundleRangeSupported == 1)
		fields.Bundle = &bundle
	}
	if row.ExpiresAt.Valid {
		expires := fromMillis(row.ExpiresAt.Int64)
		fields.ExpiresAt = &expires
	}
	if row.ChargeHolderJobID.Valid {
		if fields.ChargeHolder, err = remoteitem.NewJobID(row.ChargeHolderJobID.String); err != nil {
			return nil, err
		}
	}
	if row.SourceJson.Valid {
		if fields.Source, err = unmarshalSource(row.SourceJson.String); err != nil {
			return nil, err
		}
	}
	if row.SubmissionJson.Valid {
		if fields.Submission, err = unmarshalSubmission(row.SubmissionJson.String); err != nil {
			return nil, err
		}
	}
	if fields.Telemetry, err = unmarshalTelemetry(row.TelemetryJson); err != nil {
		return nil, err
	}
	for _, f := range files {
		file, err := remoteitem.NewRemoteFile(f.FileID, f.Path, f.SizeBytes)
		if err != nil {
			return nil, err
		}
		fields.Files = append(fields.Files, file)
	}
	for _, j := range jobs {
		job, err := unmarshalJob(j)
		if err != nil {
			return nil, err
		}
		fields.Claims = append(fields.Claims, job)
	}
	return remoteitem.UnmarshalRemoteItemFromDatabase(fields)
}

func unmarshalJob(row sqlcgen.Job) (remoteitem.Job, error) {
	id, err := remoteitem.NewJobID(row.ID)
	if err != nil {
		return remoteitem.Job{}, err
	}
	origin, err := remoteitem.NewOriginFromString(row.Origin)
	if err != nil {
		return remoteitem.Job{}, err
	}
	return remoteitem.UnmarshalJobFromDatabase(id, row.OwnerID, row.OwnerUsername, row.DisplayName, fromMillis(row.ClaimedAt), origin)
}

func unmarshalSource(encoded string) (remoteitem.Source, error) {
	var row sourceRow
	if err := json.Unmarshal([]byte(encoded), &row); err != nil {
		return remoteitem.Source{}, fmt.Errorf("stored source: %w", err)
	}
	file, err := base64.StdEncoding.DecodeString(row.File)
	if err != nil {
		return remoteitem.Source{}, fmt.Errorf("stored source file: %w", err)
	}
	sourceType, err := remoteitem.NewSourceTypeFromString(row.Type)
	if err != nil {
		return remoteitem.Source{}, err
	}
	switch sourceType {
	case remoteitem.SourceMagnet:
		return remoteitem.NewMagnetSource(row.Magnet), nil
	case remoteitem.SourceTorrentFile:
		return remoteitem.NewTorrentFileSource(file, row.FileName), nil
	case remoteitem.SourceNZBFile:
		return remoteitem.NewNZBFileSource(file, row.FileName), nil
	case remoteitem.SourceNZBURL:
		return remoteitem.NewNZBURLSource(row.URL), nil
	}
	return remoteitem.NewWebURLSource(row.URL, row.Password), nil
}

func unmarshalSubmission(encoded string) (*remoteitem.Submission, error) {
	var row submissionRow
	if err := json.Unmarshal([]byte(encoded), &row); err != nil {
		return nil, fmt.Errorf("stored submission: %w", err)
	}
	submission := &remoteitem.Submission{
		StartedAt: fromMillis(row.StartedAt), AttemptStartedAt: fromMillis(row.AttemptStartedAt),
		Attempts: row.Attempts, NotBefore: fromMillis(row.NotBefore), Cached: row.Cached,
	}
	for _, due := range row.FollowUpsDue {
		submission.FollowUpsDue = append(submission.FollowUpsDue, fromMillis(due))
	}
	if row.DeferredBehind != "" {
		blocker, err := remoteitem.NewID(row.DeferredBehind)
		if err != nil {
			return nil, err
		}
		submission.DeferredBehind = blocker
	}
	return submission, nil
}

func unmarshalTelemetry(encoded string) (remoteitem.Telemetry, error) {
	var row telemetryRow
	if err := json.Unmarshal([]byte(encoded), &row); err != nil {
		return remoteitem.Telemetry{}, fmt.Errorf("stored telemetry: %w", err)
	}
	source, err := remoteitem.NewStallSourceFromString(row.StallSource)
	if err != nil {
		return remoteitem.Telemetry{}, err
	}
	telemetry := remoteitem.Telemetry{
		Progress: row.Progress, BytesPerSecond: row.BytesPerSecond, Peers: row.Peers,
		Stalled: row.Stalled, StallSource: source,
	}
	if row.ETASeconds != nil {
		eta := time.Duration(*row.ETASeconds) * time.Second
		telemetry.ETA = &eta
	}
	return telemetry, nil
}
