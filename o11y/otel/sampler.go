package otel

import (
	"hash/crc32"
	"math"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type deterministicSampler struct {
	sampleKeyFunc func(map[string]any) string
	sampleRates   map[string]uint
}

// shouldSample means should sample in, returning true if the span should be sampled in (kept)
func (s deterministicSampler) shouldSample(p sdktrace.ReadOnlySpan) (bool, uint) {
	fields := map[string]any{}
	for _, attr := range p.Attributes() {
		fields[string(attr.Key)] = attr.Value.AsInterface()
	}
	// fields used in the existing sample key func
	fields["duration_ms"] = int(p.EndTime().Sub(p.StartTime()).Milliseconds())
	fields["name"] = p.Name()

	key := s.sampleKeyFunc(fields)
	rate, ok := s.sampleRates[key] // no rate found means keep
	if !ok {
		return true, 1 // and is a sample rate of 1/1
	}
	return shouldKeep(p.SpanContext().SpanID().String(), rate), rate
}

// shouldKeep deterministically decides whether to sample. True means keep, false means drop
func shouldKeep(determinant string, rate uint) bool {
	if rate < 2 {
		return true
	}
	if rate > math.MaxUint32 {
		rate = math.MaxUint32
	}

	threshold := math.MaxUint32 / uint32(rate) //nolint:gosec
	v := crc32.ChecksumIEEE([]byte(determinant))

	return v < threshold
}
