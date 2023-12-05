package otel

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/circleci/ex/o11y"
)

type helpers struct{}

// ExtractPropagation pulls propagation information out of the context
func (h helpers) ExtractPropagation(ctx context.Context) o11y.PropagationContext {
	m := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, mapCarrier(m))

	return o11y.PropagationContext{
		// TODO support single ca.Parent
		Headers: m,
	}
}

// InjectPropagation adds propagation header fields into the returned root span returning
// the context carrying that span. This should always return a span. If no propagation is
// found then a new span named root is returned. It is expected that callers of this will
// rename the returned span.
func (h helpers) InjectPropagation(ctx context.Context, ca o11y.PropagationContext) (context.Context, o11y.Span) {
	p := o11y.FromContext(ctx)
	provider, ok := p.(*OTel)
	if !ok {
		return provider.StartSpan(ctx, "root")
	}

	// TODO support single ca.Parent
	ctx = otel.GetTextMapPropagator().Extract(ctx, mapCarrier(ca.Headers))
	sp := trace.SpanFromContext(ctx)
	if sp.SpanContext().IsValid() {
		return ctx, provider.startSpan(sp)

	}
	// If there was no context propagation make a span
	return provider.StartSpan(ctx, "root")
}

// TraceIDs return standard o11y ids
func (h helpers) TraceIDs(ctx context.Context) (traceID, parentID string) {
	sc := trace.SpanFromContext(ctx).SpanContext()
	return sc.TraceID().String(), "" // TODO - do we ever use parent
}

type mapCarrier map[string]string

// Get returns the value associated with the passed key.
func (m mapCarrier) Get(key string) string {
	return m[key]
}

// Set stores the key-value pair.
func (m mapCarrier) Set(key string, value string) {
	m[key] = value
}

// Keys lists the keys stored in this carrier.
func (m mapCarrier) Keys() []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
