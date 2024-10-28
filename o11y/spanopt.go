package o11y

type SpanConfig struct {
	Kind SpanKind
}

type SpanOpt func(SpanConfig) SpanConfig

// WithSpanKind sets the SpanKind of a Span.
func WithSpanKind(kind SpanKind) SpanOpt {
	return func(cfg SpanConfig) SpanConfig {
		cfg.Kind = kind
		return cfg
	}
}

// SpanKind is the role a Span plays in a Trace.
type SpanKind int

// These are direct copies of the otel values - see them for documentation
const (
	SpanKindInternal SpanKind = 1
	SpanKindServer   SpanKind = 2
	SpanKindClient   SpanKind = 3
	SpanKindProducer SpanKind = 4
	SpanKindConsumer SpanKind = 5
)
