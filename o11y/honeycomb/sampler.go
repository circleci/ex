package honeycomb

import (
	"fmt"
	"hash/crc32"
	"math"

	dynsampler "github.com/honeycombio/dynsampler-go"
)

type TraceSampler struct {
	// keyFunc takes the event's fields map and returns a single string key
	// which will be used as the lookup into the sampling strategy
	KeyFunc func(map[string]interface{}) string

	Sampler dynsampler.Sampler
}

// Hook implements beeline.Config.Samplerhook
func (s *TraceSampler) Hook(fields map[string]interface{}) (sample bool, rate int) {
	// Always sample in spans that have been set to do so via the `SetSpanSampledIn` function
	if v, ok := fields["meta.keep.span"]; ok {
		if keep, ok := v.(bool); ok && keep {
			return true, 1
		}
	}

	key := s.KeyFunc(fields)
	rate = s.Sampler.GetSampleRate(key)
	if shouldSample(fmt.Sprintf("%v", fields["trace.trace_id"]), rate) {
		return true, rate
	}
	return false, 0
}

// shouldSample deterministically decides whether to sample
// true means keep, false means drop
//
// See https://github.com/honeycombio/beeline-go/blob/master/sample/deterministic_sampler.go
func shouldSample(determinant string, rate int) bool {
	if rate == 1 {
		return true
	}

	threshold := math.MaxUint32 / uint32(rate) //nolint:gosec
	v := crc32.ChecksumIEEE([]byte(determinant))

	return v < threshold
}
