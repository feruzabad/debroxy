// Package provider is delivery's adapter to the provider context (C5-03,
// DEP-4): it opens the upstream stream; delivery never sees a CDN URL.
package provider

import (
	"context"

	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
	providerin "github.com/feruzabad/debroxy/internal/provider/ports/in"
)

// StreamOpener implements out.DeliveryStreamOpener over provider's operation.
type StreamOpener struct {
	app providerin.Application
}

var _ out.DeliveryStreamOpener = StreamOpener{}

func NewStreamOpener(app providerin.Application) StreamOpener {
	if app.Operations.OpenDeliveryStream == nil {
		panic("nil provider application")
	}
	return StreamOpener{app: app}
}

// OpenDeliveryStream passes the request context, so a client disconnect
// cancels the upstream request (ADR 0010 rule 21). Provider's neutral
// errors carry no secrets; delivery reports them as provider-unavailable.
func (o StreamOpener) OpenDeliveryStream(ctx context.Context, request out.StreamRequest) (out.Stream, error) {
	stream, err := o.app.Operations.OpenDeliveryStream.Handle(ctx, providerin.OpenDeliveryStreamRequest{
		ProviderID: request.Target.ProviderID, Kind: providerin.Kind(request.Target.Kind), ProviderItemID: request.Target.ProviderItemID,
		Target: providerin.DeliveryTarget{FileID: request.Target.FileID, Bundle: request.Target.Bundle},
		Method: request.Method, Range: request.Range, IfRange: request.IfRange,
	})
	if err != nil {
		return out.Stream{}, err
	}
	return out.Stream{Status: stream.Status, Headers: stream.Headers, RangeSupported: stream.RangeSupported, Body: stream.Body}, nil
}
