package honeycomb

import (
	"context"
	"errors"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestHoneycomb(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"version":42`))
		assert.Check(t, cmp.Contains(event, `"name":"test-span"`))
		assert.Check(t, cmp.Contains(event, `"app.span-key":"span-value"`))
		assert.Check(t, cmp.Contains(event, `"app.trace-key":"trace-value"`))
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "test-dataset",
		Host:       url,
		SendTraces: true,
	})
	h.AddGlobalField("version", 42)

	ctx, span := h.StartSpan(ctx, "test-span")
	h.AddFieldToTrace(ctx, "trace-key", "trace-value")
	span.AddField("span-key", "span-value")
	span.End()
	h.Close(ctx)

	assert.Assert(t, gotEvent, "expected to receive an event")
}

func TestHoneycombWithError(t *testing.T) {
	// check the response for some expected data
	gotEvent := false
	check := func(event string) {
		gotEvent = true

		assert.Check(t, cmp.Contains(event, `"version":123`))
		assert.Check(t, cmp.Contains(event, `"name":"test-span-with-error"`))
		assert.Check(t, cmp.Contains(event, `"app.span-key":"span-value-error"`))
		assert.Check(t, cmp.Contains(event, `"app.trace-key":"trace-value-error"`))
		assert.Check(t, cmp.Contains(event, `"app.result":"error"`))
		assert.Check(t, cmp.Contains(event, `"app.error":"example error"`))
	}
	// set up a minimal server with the check defined above
	url := honeycombServer(t, check)
	ctx := context.Background()

	h := New(Config{
		Dataset:    "error-dataset",
		Host:       url,
		SendTraces: true,
	})
	h.AddGlobalField("version", 123)

	_ = func() (err error) {
		ctx, span := h.StartSpan(ctx, "test-span-with-error")
		defer span.End(&err)
		h.AddFieldToTrace(ctx, "trace-key", "trace-value-error")
		span.AddField("span-key", "span-value-error")
		return errors.New("example error")
	}()

	h.Close(ctx)

	assert.Assert(t, gotEvent, "expected to receive an event")
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
