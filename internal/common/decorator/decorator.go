// Package decorator defines the generic handler shapes of the application
// layer (CQRS commands and queries, and A-10 operations) and wraps every
// handler with logging and metrics (ddd-rules A-5).
package decorator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// CommandHandler changes state and returns only an error (ddd-rules A-3).
type CommandHandler[C any] interface {
	Handle(ctx context.Context, cmd C) error
}

// QueryHandler reads state.
type QueryHandler[Q any, R any] interface {
	Handle(ctx context.Context, query Q) (R, error)
}

// OperationHandler is the driving-port shape of an anti-corruption-layer
// context such as provider (ddd-rules A-10).
type OperationHandler[Req any, Res any] interface {
	Handle(ctx context.Context, request Req) (Res, error)
}

// ApplyCommandDecorators wraps a command handler with logging, then metrics.
func ApplyCommandDecorators[C any](handler CommandHandler[C], logger *slog.Logger, metricsClient MetricsClient) CommandHandler[C] {
	checkDecoratorDependencies(handler, logger, metricsClient)
	return commandLoggingDecorator[C]{
		base:   commandMetricsDecorator[C]{base: handler, client: metricsClient},
		logger: logger,
	}
}

// ApplyQueryDecorators wraps a query handler with logging, then metrics.
func ApplyQueryDecorators[Q any, R any](handler QueryHandler[Q, R], logger *slog.Logger, metricsClient MetricsClient) QueryHandler[Q, R] {
	checkDecoratorDependencies(handler, logger, metricsClient)
	return queryLoggingDecorator[Q, R]{
		base:   queryMetricsDecorator[Q, R]{base: handler, client: metricsClient},
		logger: logger,
	}
}

// ApplyOperationDecorators wraps an operation with logging, then metrics.
func ApplyOperationDecorators[Req any, Res any](handler OperationHandler[Req, Res], logger *slog.Logger, metricsClient MetricsClient) OperationHandler[Req, Res] {
	checkDecoratorDependencies(handler, logger, metricsClient)
	return operationLoggingDecorator[Req, Res]{
		base:   operationMetricsDecorator[Req, Res]{base: handler, client: metricsClient},
		logger: logger,
	}
}

func checkDecoratorDependencies(handler any, logger *slog.Logger, metricsClient MetricsClient) {
	if handler == nil {
		panic("nil handler")
	}
	if logger == nil {
		panic("nil logger")
	}
	if metricsClient == nil {
		panic("nil metricsClient")
	}
}

// actionName is the use-case name taken from the command, query or
// request type, e.g. "SubmitJob". Payloads are never logged: they can hold
// passwords and uploaded sources.
func actionName(action any) string {
	name := fmt.Sprintf("%T", action)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}
