// Package texttrace is a span exporter for otel that outputs to the ex text console format
package texttrace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/circleci/ex/colourise"
)

var zeroTime time.Time

var _ trace.SpanExporter = &Exporter{}

// New creates an Exporter with the passed options.
func New(w io.Writer) (*Exporter, error) {

	return &Exporter{
		w:          w,
		timestamps: true,
		colour:     true,
	}, nil
}

// Exporter is an implementation of trace.SpanSyncer that writes spans to stdout.
type Exporter struct {
	timestamps bool
	colour     bool

	w io.Writer

	stoppedMu sync.RWMutex
	stopped   bool
}

// ExportSpans writes spans in json format to stdout.
func (e *Exporter) ExportSpans(_ context.Context, spans []trace.ReadOnlySpan) error {
	e.stoppedMu.RLock()
	stopped := e.stopped
	e.stoppedMu.RUnlock()
	if stopped {
		return nil
	}

	if len(spans) == 0 {
		return nil
	}

	stubs := tracetest.SpanStubsFromReadOnlySpans(spans)

	for i := range stubs {
		stub := &stubs[i]
		// Remove timestamps
		if !e.timestamps {
			stub.StartTime = zeroTime
			stub.EndTime = zeroTime
			for j := range stub.Events {
				ev := &stub.Events[j]
				ev.Time = zeroTime
			}
		}

		_, _ = e.w.Write(e.format(stub))
	}
	return nil
}

// Shutdown is called to stop the exporter, it preforms no action.
func (e *Exporter) Shutdown(ctx context.Context) error {
	e.stoppedMu.Lock()
	e.stopped = true
	e.stoppedMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return nil
}

// MarshalLog is the marshaling function used by the logging system to represent this exporter.
func (e *Exporter) MarshalLog() any {
	return struct {
		Type           string
		WithTimestamps bool
	}{
		Type:           "stdout",
		WithTimestamps: e.timestamps,
	}
}

func (e *Exporter) format(ev *tracetest.SpanStub) []byte {
	buf := new(bytes.Buffer)
	_, _ = fmt.Fprintf(buf, "%s %s %.3fms %s",
		ev.EndTime.Format("15:04:05"),
		e.applyColour(formatTraceID(ev.SpanContext.TraceID().String())),
		float64(ev.EndTime.Sub(ev.StartTime).Microseconds())/1000,
		e.applyColour(ev.Name),
	)

	data := map[string]any{}
	for _, a := range ev.Attributes {
		data[string(a.Key)] = a.Value.Emit()
	}

	for _, k := range sortedKeys(ev.Attributes) {
		if e.exclude(k) {
			continue
		}
		label := k // we have to copy the key, so we can use the original to lookup the data
		if k == "error" && e.colour {
			label = colourise.ErrorHighlight(k)
		}
		_, _ = fmt.Fprintf(buf, " %s=%v", label, data[k])
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func (e *Exporter) exclude(k string) bool {
	switch k {
	case "name", "version", "service", "duration_ms":
		return true
	}
	// these are noisy prefixes so exclude them from the output.
	for _, prefix := range []string{"trace", "meta"} {
		if strings.HasPrefix(k, prefix+".") {
			return true
		}
	}
	return false
}

func (e *Exporter) applyColour(value string) string {
	if !e.colour {
		return value
	}
	return colourise.ApplyColour(value)
}

func formatTraceID(raw string) string {
	return raw[len(raw)-5:]
}

func sortedKeys(m []attribute.KeyValue) []string {
	keys := make([]string, 0, len(m))
	for _, k := range m {
		keys = append(keys, string(k.Key))
	}
	sort.Strings(keys)
	return keys
}
