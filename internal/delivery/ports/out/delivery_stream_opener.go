package out

import (
	"context"
	"io"
)

// DeliveryStreamOpener is what delivery needs from provider (C5-03, DEP-4):
// the upstream stream for a target. It never sees a CDN URL.
type DeliveryStreamOpener interface {
	OpenDeliveryStream(ctx context.Context, request StreamRequest) (Stream, error)
}

type StreamRequest struct {
	Target  Target
	Method  string
	Range   string
	IfRange string
}

// Stream is provider's answer; the caller closes Body. Headers hold only
// provider's whitelist.
type Stream struct {
	Status         int
	Headers        map[string]string
	RangeSupported bool
	Body           io.ReadCloser
}
