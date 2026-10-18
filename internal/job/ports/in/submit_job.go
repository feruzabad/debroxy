package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// SubmitJob claims or starts a RemoteItem for the source (spec §5.5). The
// caller chooses JobID and reads the job afterwards with JobDetails; a nil
// error with the job still submitting means 202 submission-pending.
type SubmitJob struct {
	Actor       Actor
	JobID       string
	ProviderID  string
	Source      Source
	DisplayName string
}

// Source is what the user submitted. Type is magnet, torrent-file,
// nzb-file, nzb-url or web-url; Password applies to web URLs only.
type Source struct {
	Type     string
	Magnet   string
	File     []byte
	FileName string
	URL      string
	Password string
}

type SubmitJobHandler decorator.CommandHandler[SubmitJob]
