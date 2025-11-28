package honeycomb

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/honeycombio/libhoney-go/transmission"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestTextSender(t *testing.T) {
	//nolint: lll
	testcases := []struct {
		source   *transmission.Event
		expected string
	}{
		{
			source: &transmission.Event{
				Timestamp: time.Date(2019, 9, 12, 19, 1, 12, 137602525, time.UTC),
				Data:      map[string]interface{}{"app.database": "build_queue", "app.dbname": "distributor", "app.host": "localhost:5432", "app.result": "connected", "app.username": "distributor", "duration_ms": 0.075231, "meta.beeline_version": "0.4.4", "meta.local_hostname": "archtop", "meta.span_type": "leaf", "name": "connect to database", "service": "distributor", "trace.parent_id": "223ebb27-c7f3-41c8-86e6-cc47e7e809d0", "trace.span_id": "29d98eb0-81c0-4538-a8b5-8296ff40563f", "trace.trace_id": "9e020857-1248-431f-b2dd-f1541bd1e113", "version": "dev"},
			},
			expected: "19:01:12 1e113 0.075ms connect to database app.database=build_queue app.dbname=distributor app.host=localhost:5432 app.result=connected app.username=distributor\n",
		},
		{
			source: &transmission.Event{
				Timestamp: time.Date(2019, 9, 12, 19, 1, 12, 137602525, time.UTC),
				Data:      map[string]interface{}{"app.address": "127.0.0.1:7624", "app.result": "listening", "app.server_name": "api", "duration_ms": 0.577148, "meta.beeline_version": "0.4.4", "meta.local_hostname": "archtop", "meta.span_type": "leaf", "name": "start-server api", "service": "distributor", "trace.parent_id": "223ebb27-c7f3-41c8-86e6-cc47e7e809d0", "trace.span_id": "ed37fbc5-6309-4526-96a3-29398eb19b5f", "trace.trace_id": "9e020857-1248-431f-b2dd-f1541bd1e113", "version": "dev"},
			},
			expected: "19:01:12 1e113 0.577ms start-server api app.address=127.0.0.1:7624 app.result=listening app.server_name=api\n",
		},
		{
			source: &transmission.Event{
				Timestamp: time.Date(2019, 9, 12, 19, 1, 12, 137602525, time.UTC),
				Data:      map[string]interface{}{"app.address": "127.0.0.1:7625", "app.result": "listening", "app.server_name": "admin", "duration_ms": 0.232612, "meta.beeline_version": "0.4.4", "meta.local_hostname": "archtop", "meta.span_type": "leaf", "name": "start-server admin", "service": "distributor", "trace.parent_id": "223ebb27-c7f3-41c8-86e6-cc47e7e809d0", "trace.span_id": "a641fc73-f2c6-45e2-a627-64cec852f14e", "trace.trace_id": "9e020857-1248-431f-b2dd-f1541bd1e113", "version": "dev"},
			},
			expected: "19:01:12 1e113 0.233ms start-server admin app.address=127.0.0.1:7625 app.result=listening app.server_name=admin\n",
		},
		{
			source: &transmission.Event{
				Timestamp: time.Date(2019, 9, 12, 19, 1, 12, 137602525, time.UTC),
				Data:      map[string]interface{}{"duration_ms": 1.455143, "meta.beeline_version": "0.4.4", "meta.local_hostname": "archtop", "meta.span_type": "root", "name": "startup", "service": "distributor", "trace.span_id": "223ebb27-c7f3-41c8-86e6-cc47e7e809d0", "trace.trace_id": "9e020857-1248-431f-b2dd-f1541bd1e113", "version": "dev"},
			},
			expected: "19:01:12 1e113 1.455ms startup\n",
		},
	}

	for i, tc := range testcases {
		t.Run(fmt.Sprintf("%v", i), func(t *testing.T) {
			buf := new(bytes.Buffer)
			h := &TextSender{
				w: buf,
			}

			h.Add(tc.source)
			assert.Check(t, cmp.Equal(buf.String(), tc.expected))
		})
	}
}
