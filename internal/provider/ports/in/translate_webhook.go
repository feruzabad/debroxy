package in

import "github.com/feruzabad/debroxy/internal/common/decorator"

// TranslateWebhookOperation checks the path secret and turns a webhook into a refresh hint (spec §5.6); a rejected webhook is ErrWebhookRejected.
type TranslateWebhookOperation decorator.OperationHandler[TranslateWebhookRequest, WebhookSignal]

// TranslateWebhookRequest carries the request as plain values (DEP-6).
type TranslateWebhookRequest struct {
	ProviderID string
	PathSecret string
	Headers    map[string][]string
	Body       []byte
}

// WebhookSignal: webhooks carry no item identity; they only ask for a
// refresh (spec §5.6, DEC-11).
type WebhookSignal struct {
	RefreshRequested bool
}
