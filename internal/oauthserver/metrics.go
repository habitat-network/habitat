package oauthserver

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type metrics struct {
	// HandleAuthorize
	authorizeErrCtr     metric.Int64Counter
	authorizeSuccessCtr metric.Int64Counter

	// HandleCallback
	callbackErrCtr     metric.Int64Counter
	callbackSuccessCtr metric.Int64Counter

	// HandleToken
	refreshTokenRequestCtr metric.Int64Counter
}

func newMetrics(meter metric.Meter) (*metrics, error) {
	authorizeErrCtr, err := meter.Int64Counter(
		"oauth.authorize.err",
		metric.WithUnit("Item"),
		metric.WithDescription("counts errors in OAuth /authorize implementation"),
	)
	if err != nil {
		return nil, err
	}

	authorizeSuccessCtr, err := meter.Int64Counter(
		"oauth.authorize.success",
		metric.WithUnit("Item"),
		metric.WithDescription("counts successes in OAuth /authorize implementation"),
	)
	if err != nil {
		return nil, err
	}

	callbackErrCtr, err := meter.Int64Counter(
		"oauth.callback.err",
		metric.WithUnit("Item"),
		metric.WithDescription("counts errors in OAuth /callback implementation"),
	)
	if err != nil {
		return nil, err
	}

	callbackSuccessCtr, err := meter.Int64Counter(
		"oauth.callback.success",
		metric.WithUnit("Item"),
		metric.WithDescription("counts successes in OAuth /callback implementation"),
	)
	if err != nil {
		return nil, err
	}

	refreshTokenRequestCtr, err := meter.Int64Counter(
		"oauth.refresh_token.request",
		metric.WithUnit("Item"),
		metric.WithDescription("counts request to refresh an OAuth token with habitat"),
	)
	if err != nil {
		return nil, err
	}

	return &metrics{
		authorizeErrCtr:        authorizeErrCtr,
		authorizeSuccessCtr:    authorizeSuccessCtr,
		callbackErrCtr:         callbackErrCtr,
		callbackSuccessCtr:     callbackSuccessCtr,
		refreshTokenRequestCtr: refreshTokenRequestCtr,
	}, nil
}

func (m *metrics) authorizeErr(ctx context.Context, err error, reason string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.authorizeErrCtr.Add(
		ctx,
		1,
		metric.WithAttributeSet(
			attribute.NewSet(
				attribute.String("reason", reason),
			),
		),
	)
}

func (m *metrics) authorizeSuccess(ctx context.Context) {
	m.authorizeSuccessCtr.Add(ctx, 1)
}

func (m *metrics) callbackErr(ctx context.Context, err error, reason string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.callbackErrCtr.Add(
		ctx,
		1,
		metric.WithAttributeSet(
			attribute.NewSet(
				attribute.String("reason", reason),
			),
		),
	)
}

func (m *metrics) callbackSuccess() {
	m.callbackSuccessCtr.Add(
		context.Background(),
		1,
	)
}
