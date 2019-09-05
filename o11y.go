package o11y

import "context"

type Provider interface {
	// AddGlobalField adds data which should apply to every span in the application
	//
	// eg. version, service, k8s_replicaset
	AddGlobalField(key string, val interface{})

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
	StartSpan(ctx context.Context, name string) (context.Context, Span)

	// AddFieldToTrace is for adding useful information to the root span.
	//
	// This will be propagated onto every child span.
	//
	// eg. build-url, plan-id, project-id, org-id etc
	AddFieldToTrace(ctx context.Context, key string, val interface{})

	Close(ctx context.Context)
}

type Span interface {
	// AddField is for adding useful information to the currently active span
	//
	// eg. result, http.status_code
	//
	// Refer to the opentelemetry draft spec for naming inspiration
	// https://github.com/open-telemetry/opentelemetry-specification/blob/master/specification/data-semantic-conventions.md
	AddField(key string, val interface{})

	// End sets the duration of the span and tells the related provider that the span is complete
	// so it can do it's appropriate processing. The span should not be used after End is called.
	End()
}

type providerKey struct{}

// WithProvider returns a child context which contains the Provider. The Provider
// can be retrieved with FromContext.
func WithProvider(ctx context.Context, p Provider) context.Context {
	return context.WithValue(ctx, providerKey{}, p)
}

// FromContext returns the provider stored in the context, or nil if none exists.
func FromContext(ctx context.Context) Provider {
	provider, ok := ctx.Value(providerKey{}).(Provider)
	if !ok {
		return nil
	}
	return provider
}

// StartSpan starts a span from a context that must contain a provider for this to have any effect.
func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	p := FromContext(ctx)
	if p == nil {
		return ctx, nil
	}
	return p.StartSpan(ctx, name)
}
