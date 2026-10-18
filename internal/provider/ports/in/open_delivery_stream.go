package in

import (
	"io"

	"github.com/feruzabad/debroxy/internal/common/decorator"
)

// OpenDeliveryStreamOperation performs the upstream CDN request for one
// delivery target (ADR 0010 rule 3, DEC-40). The CDN URL never leaves
// provider; the caller receives a stream and must close Body.
type OpenDeliveryStreamOperation decorator.OperationHandler[OpenDeliveryStreamRequest, DeliveryStream]

// DeliveryTarget is a file of the item, or the provider-built bundle (ADR 0011).
type DeliveryTarget struct {
	FileID string
	Bundle bool
}

type OpenDeliveryStreamRequest struct {
	ProviderID     string
	Kind           Kind
	ProviderItemID string
	Target         DeliveryTarget
	// Method is GET or HEAD.
	Method string
	// Range and IfRange are the client's headers; they are forwarded only
	// when the target supports ranges (ADR 0010 rule 17).
	Range   string
	IfRange string
}

// DeliveryStream is the upstream answer. Headers holds only the whitelist
// of ADR 0010 rule 18 (Content-Length, Content-Range, Content-Type,
// Last-Modified, ETag). RangeSupported is false for targets without range
// support and whenever the upstream answered a ranged request with the
// whole body (rule 20).
type DeliveryStream struct {
	Status         int
	Headers        map[string]string
	RangeSupported bool
	Body           io.ReadCloser
}
