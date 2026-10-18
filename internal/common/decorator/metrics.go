package decorator

import (
	"context"
	"strings"
)

// MetricsClient counts handled actions. The backend is not chosen yet
// (spec OQ-07); NoOpMetricsClient is used until then.
type MetricsClient interface {
	Inc(key string, value int)
}

// NoOpMetricsClient discards all metrics.
type NoOpMetricsClient struct{}

func (NoOpMetricsClient) Inc(string, int) {}

type commandMetricsDecorator[C any] struct {
	base   CommandHandler[C]
	client MetricsClient
}

func (d commandMetricsDecorator[C]) Handle(ctx context.Context, cmd C) error {
	err := d.base.Handle(ctx, cmd)
	countResult(d.client, "commands", actionName(cmd), err)
	return err
}

type queryMetricsDecorator[Q any, R any] struct {
	base   QueryHandler[Q, R]
	client MetricsClient
}

func (d queryMetricsDecorator[Q, R]) Handle(ctx context.Context, query Q) (R, error) {
	result, err := d.base.Handle(ctx, query)
	countResult(d.client, "queries", actionName(query), err)
	return result, err
}

type operationMetricsDecorator[Req any, Res any] struct {
	base   OperationHandler[Req, Res]
	client MetricsClient
}

func (d operationMetricsDecorator[Req, Res]) Handle(ctx context.Context, request Req) (Res, error) {
	result, err := d.base.Handle(ctx, request)
	countResult(d.client, "operations", actionName(request), err)
	return result, err
}

func countResult(client MetricsClient, kind, name string, err error) {
	outcome := "success"
	if err != nil {
		outcome = "failure"
	}
	client.Inc(strings.Join([]string{kind, name, outcome}, "."), 1)
}
