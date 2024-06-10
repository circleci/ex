package otel

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/circleci/ex/o11y"
)

type helpers struct {
	p          Provider
	disableW3c bool // temporary option whilst we have split datasets
}

// ExtractPropagation pulls propagation information out of the context
func (h helpers) ExtractPropagation(ctx context.Context) o11y.PropagationContext {
	if h.disableW3c {
		return o11y.PropagationContext{}
	}

	m := http.Header{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(m))

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
	if h.disableW3c {
		return h.p.StartSpan(ctx, "root")
	}

	// TODO support single ca.Parent
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(ca.Headers))

	// Make a new span - the trace propagation info in the context will be used
	// N.B we update the name of this span at the calling site.
	return h.p.StartSpan(ctx, "root")
}

// TraceIDs return standard o11y ids
func (h helpers) TraceIDs(ctx context.Context) (traceID, parentID string) {
	sc := trace.SpanFromContext(ctx).SpanContext()
	return sc.TraceID().String(), "" // TODO - do we ever use parent
}
