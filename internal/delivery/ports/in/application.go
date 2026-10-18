package in

// Application is everything the delivery context can be asked to do. All
// use cases are queries (spec §7.2): links are computed, not stored.
type Application struct {
	Queries Queries
}

type Queries struct {
	IssueSignedLink             IssueSignedLinkHandler
	IssueSignedLinksForAllFiles IssueSignedLinksForAllFilesHandler
	OpenDownload                OpenDownloadHandler
}
