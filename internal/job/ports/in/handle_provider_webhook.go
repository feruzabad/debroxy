package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// HandleProviderWebhook turns a provider webhook into a coalesced refresh
// (spec §5.6). The request is passed as plain values (DEP-6). A rejected
// webhook is remoteitem.ErrWebhookRejected; the adapter answers 404 without
// telling the reasons apart (RISK-07).
type HandleProviderWebhook struct {
	ProviderID string
	PathSecret string
	Headers    map[string][]string
	Body       []byte
}

type HandleProviderWebhookHandler decorator.CommandHandler[HandleProviderWebhook]
