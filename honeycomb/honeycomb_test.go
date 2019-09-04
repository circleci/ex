package honeycomb

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestHoneycomb(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		t.Log(event)
		gotEvent = true

		if !strings.Contains(event, `"version":42`) {
			t.Error("missing version data")
		}
		if !strings.Contains(event, `"name":"test-span"`) {
			t.Error("missing span name")
		}
		if !strings.Contains(event, `"app.span-key":"span-value"`) {
			t.Error("missing span data")
		}
		if !strings.Contains(event, `"app.trace-key":"trace-value"`) {
			t.Error("missing trace data")
		}
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New("test-dataset", "foo-bar", url, true)
	h.AddGlobalField("version", 42)

	ctx, span := h.StartSpan(ctx, "test-span")
	h.AddFieldToTrace(ctx, "trace-key", "trace-value")
	h.AddField(ctx, "span-key", "span-value")
	span.End()
	h.Close(ctx)

	if !gotEvent {
		t.Error("never received an event")
	}
}

func honeycombServer(t *testing.T, cb func(string)) string {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := zstd.NewReader(r.Body)
		if err != nil {
			t.Fatal("could not create zip reader", err)
		}
		defer reader.Close()
		defer r.Body.Close()

		b, err := ioutil.ReadAll(reader)
		if err != nil {
			t.Error("could not read request", err)
		}
		cb(string(b))
	}))
	return ts.URL
}
