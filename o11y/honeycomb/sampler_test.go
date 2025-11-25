package honeycomb

import (
	"fmt"
	"testing"

	dynsampler "github.com/honeycombio/dynsampler-go"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

var samplerTests = []struct {
	scenario string
	fields   map[string]interface{}
	sample   bool
	rate     int
}{
	{
		"normal request",
		map[string]interface{}{
			"trace.trace_id":       "ede23f67-2048-491b-ba71-749a8a00444f",
			"app.server_name":      "api",
			"request.path":         "/",
			"response.status_code": 200,
		},
		true, 1,
	},
	{
		"ready-check with no problems",
		map[string]interface{}{
			"trace.trace_id":       "ede23f67-2048-491b-ba71-749a8a00444f",
			"app.server_name":      "admin",
			"request.path":         "/ready",
			"response.status_code": 200,
		},
		false, 0,
	},
	{
		"sampled in via meta field",
		map[string]interface{}{
			"trace.trace_id":       "ede23f67-2048-491b-ba71-749a8a00444f",
			"app.server_name":      "admin",
			"request.path":         "/ready",
			"response.status_code": 200,
			"meta.keep.span":       true,
		},
		true, 1,
	},
	{
		"ready-check with no problems but trace hits sample rate",
		map[string]interface{}{
			"trace.trace_id":       "9d45eecd-e447-4418-bd9b-1ac3c32346d5",
			"app.server_name":      "admin",
			"request.path":         "/ready",
			"response.status_code": 200,
		},
		true, 1e3,
	},
}

func TestSamplerHook(t *testing.T) {
	sampler := &TraceSampler{
		KeyFunc: func(fields map[string]interface{}) string {
			return fmt.Sprintf("%s %s %d",
				fields["app.server_name"],
				fields["request.path"],
				fields["response.status_code"],
			)
		},
		Sampler: &dynsampler.Static{
			Default: 1,
			Rates: map[string]int{
				"admin /ready 200": 1e3,
			},
		},
	}
	for _, tt := range samplerTests {
		t.Run(tt.scenario, func(t *testing.T) {
			sample, rate := sampler.Hook(tt.fields)
			assert.Check(t, cmp.Equal(sample, tt.sample))
			assert.Check(t, cmp.Equal(rate, tt.rate))
		})
	}
}
