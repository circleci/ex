package o11y

import "context"

type Provider interface {
	AddGlobalField(key string, val interface{})
	StartSpan(ctx context.Context, name string) (context.Context, Span)
	AddField(ctx context.Context, key string, val interface{})
	AddFieldToTrace(ctx context.Context, key string, val interface{})
	Close(ctx context.Context)
}

type Span interface {
	End()
}

type providerKey struct{}

func WithProvider(ctx context.Context, p Provider) context.Context {
	return context.WithValue(ctx, providerKey{}, p)
}

func FromContext(ctx context.Context) Provider {
	provider, ok := ctx.Value(providerKey{}).(Provider)
	if !ok {
		return nil
	}
	return provider
}

// AddGlobalField adds data which should apply to every span in the application
//
// eg. version, service, k8s_replicaset
func AddGlobalField(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddGlobalField(key, val)
}

// StartSpan begins a new span that'll represent a unit of work
//
// `name` should be a short human readable identifier of the work.
// It can and should include some details to distinguish it from other
// similar spans - like the URL or the DB query name.
//
// The caller is responsible for calling End(), usually via defer:
//
//   ctx, span := o11y.StartSpan(ctx, "GET /help")
//   defer span.End()
func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return FromContext(ctx).StartSpan(ctx, name)
}

// AddField is for adding useful information to the currently active span
//
// eg. result, http.status_code
//
// Refer to the opentelemetry draft spec for naming inspiration
// https://github.com/open-telemetry/opentelemetry-specification/blob/master/specification/data-semantic-conventions.md
func AddField(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddField(ctx, key, val)
}

// AddFieldToTrace is for adding useful information to the current root span.
//
// This will be propagated onto every child span.
//
// eg. build-url, plan-id, project-id, org-id etc
func AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	FromContext(ctx).AddFieldToTrace(ctx, key, val)
}

func Close(ctx context.Context) {
	FromContext(ctx).Close(ctx)
}
