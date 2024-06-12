package otel

import (
	"hash/crc32"
	"math"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type deterministicSampler struct {
	sampleKeyFunc func(map[string]any) string
	sampleRates   map[string]int
}

// shouldSample means should sample in, returning true if the span should be sampled in (kept)
func (s deterministicSampler) shouldSample(p sdktrace.ReadOnlySpan) bool {
	fields := map[string]any{}
	for _, attr := range p.Attributes() {
		fields[string(attr.Key)] = attr.Value.AsInterface()
	}
	fields["name"] = p.Name()

	key := s.sampleKeyFunc(fields)
	rate, ok := s.sampleRates[key] // no rate found means keep
	return !ok || shouldKeep(p.SpanContext().SpanID().String(), rate)
}

// shouldKeep deterministically decides whether to sample. True means keep, false means drop
func shouldKeep(determinant string, rate int) bool {
	if rate == 1 {
		return true
	}

	threshold := math.MaxUint32 / uint32(rate)
	v := crc32.ChecksumIEEE([]byte(determinant))

	return v < threshold
}
