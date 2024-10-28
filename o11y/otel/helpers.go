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

	// Add stuff to the baggage in the context, so it can be injected into the propagation context.
	flattenDepth := 0
	sp := h.p.getSpan(ctx)
	if sp != nil {
		flattenDepth = sp.flattenDepth
	}
	sm := http.Header{}
	gs := h.p.getGolden(ctx)
	if gs != nil {
		otel.GetTextMapPropagator().Inject(gs.ctx, propagation.HeaderCarrier(sm))
	}
	ctx = o11y.AddExtrasToBaggage(ctx, flattenDepth, sm)

	m := http.Header{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(m))

	return o11y.PropagationContext{
		Headers: m,
	}
}

// InjectPropagation adds propagation header fields into the returned root span returning
// the context carrying that span. This should always return a span. If no propagation is
// found then a new span named root is returned. It is expected that callers of this will
// rename the returned span.
func (h helpers) InjectPropagation(ctx context.Context,
	ca o11y.PropagationContext, opts ...o11y.SpanOpt) (context.Context, o11y.Span) {

	if h.disableW3c {
		return h.p.StartSpan(ctx, "root", opts...)
	}

	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(ca.Headers))

	// Make a new span - the trace propagation info in the context will be used
	// N.B we update the name of this span at the calling site.
	ctx, sp := h.p.StartSpan(ctx, "root", opts...)

	// Check if the baggage indicates this span should be flattened
	fd, goldHeaders := o11y.ExtrasFromBaggage(ctx)
	if fd > 0 {
		os := h.p.getSpan(ctx)
		os.flatten("", fd)
	}
	if len(goldHeaders) > 0 {
		gCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(goldHeaders))
		spec := &golden{
			ctx:  gCtx,
			span: h.p.wrapSpan("", opts, trace.SpanFromContext(gCtx), nil),
		}
		spec.span.AddRawField(metaGolden, true)
		ctx = context.WithValue(ctx, goldenCtxKey{}, spec)
	}
	return ctx, sp
}

// TraceIDs return standard o11y ids
func (h helpers) TraceIDs(ctx context.Context) (traceID, parentID string) {
	sc := trace.SpanFromContext(ctx).SpanContext()
	return sc.TraceID().String(), "" // TODO - do we ever use parent
}

func (h helpers) GoldenTraceID(_ context.Context) string {
	return ""
}
