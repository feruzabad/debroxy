package query

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/feruzabad/debroxy/internal/common/clock"
	"github.com/feruzabad/debroxy/internal/common/decorator"
	"github.com/feruzabad/debroxy/internal/delivery/domain/signedlink"
	"github.com/feruzabad/debroxy/internal/delivery/ports/in"
	"github.com/feruzabad/debroxy/internal/delivery/ports/out"
)

// whitelist is the set of upstream headers a client may see (ADR 0010 rule 18).
var whitelist = []string{"Content-Length", "Content-Range", "Content-Type", "Last-Modified", "ETag"}

type openDownloadHandler struct {
	signer  signedlink.Signer
	clock   clock.Clock
	users   out.UserStatusChecker
	slots   *StreamSlots
	targets out.JobTargets
	streams out.DeliveryStreamOpener
}

// DownloadDependencies are the ports OpenDownload uses.
type DownloadDependencies struct {
	Users   out.UserStatusChecker
	Slots   *StreamSlots
	Targets out.JobTargets
	Streams out.DeliveryStreamOpener
	Clock   clock.Clock
}

func NewOpenDownloadHandler(deps DownloadDependencies, keys []signedlink.Key, logger *slog.Logger, metricsClient decorator.MetricsClient) (in.OpenDownloadHandler, error) {
	switch {
	case deps.Users == nil:
		panic("nil userStatusChecker")
	case deps.Slots == nil:
		panic("nil streamSlots")
	case deps.Targets == nil:
		panic("nil jobTargets")
	case deps.Streams == nil:
		panic("nil deliveryStreamOpener")
	case deps.Clock == nil:
		panic("nil clock")
	}
	signer, err := signedlink.NewSigner(keys)
	if err != nil {
		return nil, err
	}
	return decorator.ApplyQueryDecorators[in.OpenDownload, in.Download](openDownloadHandler{
		signer: signer, clock: deps.Clock, users: deps.Users, slots: deps.Slots, targets: deps.Targets, streams: deps.Streams,
	}, logger, metricsClient), nil
}

// Handle checks, in the order of ADR 0010 rule 14: the token, the user,
// a stream slot, the target, then opens the upstream. The slot is held
// until the caller closes Body.
func (h openDownloadHandler) Handle(ctx context.Context, query in.OpenDownload) (in.Download, error) {
	link, err := h.signer.Verify(query.Token, h.clock.Now())
	if err != nil {
		return in.Download{}, err
	}
	active, err := h.users.IsActive(ctx, link.UserID)
	if err != nil {
		return in.Download{}, err
	}
	if !active {
		return in.Download{}, signedlink.ErrInvalid // the link stops working, without telling why
	}
	release, err := h.slots.acquire(link.UserID)
	if err != nil {
		return in.Download{}, err
	}
	download, err := h.open(ctx, link, query)
	if err != nil {
		release()
		return in.Download{}, err
	}
	download.Body = &releasingBody{ReadCloser: download.Body, release: release}
	return download, nil
}

func (h openDownloadHandler) open(ctx context.Context, link signedlink.Link, query in.OpenDownload) (in.Download, error) {
	request := out.TargetRequest{FileID: link.Target.FileID(), Bundle: link.Target.IsBundle()}
	target, err := h.targets.DeliveryTarget(ctx, link.UserID, link.JobID, request)
	if err != nil {
		return in.Download{}, err
	}
	stream, err := h.streams.OpenDeliveryStream(ctx, out.StreamRequest{Target: target, Method: query.Method, Range: query.Range, IfRange: query.IfRange})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return in.Download{}, err
		}
		return in.Download{}, errors.Join(errProviderUnavailable, err)
	}
	headers := make(map[string]string, len(whitelist))
	for _, name := range whitelist {
		if value, ok := stream.Headers[name]; ok {
			headers[name] = value
		}
	}
	return in.Download{
		Status: stream.Status, Headers: headers, RangeSupported: stream.RangeSupported,
		FileName: signedlink.SanitizeName(query.Name, target.Name), Body: stream.Body,
	}, nil
}

// releasingBody frees the stream slot when the response ends.
type releasingBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (b *releasingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.release)
	return err
}
