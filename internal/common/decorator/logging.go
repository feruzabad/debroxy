package decorator

import (
	"context"
	"errors"
	"log/slog"
	"time"

	commonerrors "github.com/feruzabad/debroxy/internal/common/errors"
)

type commandLoggingDecorator[C any] struct {
	base   CommandHandler[C]
	logger *slog.Logger
}

func (d commandLoggingDecorator[C]) Handle(ctx context.Context, cmd C) (err error) {
	start := time.Now()
	defer func() { logResult(ctx, d.logger, "command", actionName(cmd), start, err) }()
	return d.base.Handle(ctx, cmd)
}

type queryLoggingDecorator[Q any, R any] struct {
	base   QueryHandler[Q, R]
	logger *slog.Logger
}

func (d queryLoggingDecorator[Q, R]) Handle(ctx context.Context, query Q) (result R, err error) {
	start := time.Now()
	defer func() { logResult(ctx, d.logger, "query", actionName(query), start, err) }()
	return d.base.Handle(ctx, query)
}

type operationLoggingDecorator[Req any, Res any] struct {
	base   OperationHandler[Req, Res]
	logger *slog.Logger
}

func (d operationLoggingDecorator[Req, Res]) Handle(ctx context.Context, request Req) (result Res, err error) {
	start := time.Now()
	defer func() { logResult(ctx, d.logger, "operation", actionName(request), start, err) }()
	return d.base.Handle(ctx, request)
}

// logResult writes one line per handled action. Errors a client caused
// (slug errors) are logged at info level; everything else is an error.
func logResult(ctx context.Context, logger *slog.Logger, kind, name string, start time.Time, err error) {
	attrs := []slog.Attr{
		slog.String(kind, name),
		slog.Duration("duration", time.Since(start)),
	}
	if err == nil {
		logger.LogAttrs(ctx, slog.LevelDebug, kind+" handled", attrs...)
		return
	}

	attrs = append(attrs, slog.String("error", err.Error()))
	var slugErr commonerrors.SlugError
	if errors.As(err, &slugErr) && slugErr.ErrorType() != commonerrors.ErrorTypeUnknown {
		attrs = append(attrs, slog.String("slug", slugErr.Slug()))
		logger.LogAttrs(ctx, slog.LevelInfo, kind+" rejected", attrs...)
		return
	}
	logger.LogAttrs(ctx, slog.LevelError, kind+" failed", attrs...)
}
