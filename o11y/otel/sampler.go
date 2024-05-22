package otel

import (
	"hash/crc32"
	"math"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type deterministicSampler struct {
	sampleKeyFunc func(map[string]any) string
	sampleRates   map[string]int
}

func (s deterministicSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	tsc := trace.SpanContextFromContext(p.ParentContext).TraceState()

	fields := map[string]any{}
	for _, attr := range p.Attributes {
		fields[string(attr.Key)] = attr.Value
	}
	fields["name"] = p.Name

	key := s.sampleKeyFunc(fields)
	rate, ok := s.sampleRates[key] // no rate found means keep
	if !ok || shouldKeep(p.TraceID.String(), rate) {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Tracestate: tsc,
		}
	}

	return sdktrace.SamplingResult{
		Decision:   sdktrace.Drop,
		Tracestate: tsc,
	}
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

func (s deterministicSampler) Description() string {
	return "deterministicSampler"
}
