// Package texttrace is a span exporter for otel that outputs to the ex text console format
package texttrace

import (
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
			label = errorHighlight(k)
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

	i := crc32.Checksum([]byte(value), crc32.IEEETable) % uint32(len(colours))
	return fmt.Sprintf("\033[1;38;5;%dm%s\033[0m", colours[i], value)
}

func errorHighlight(s string) string {
	return fmt.Sprintf("\033[1;37;41m%s\033[0m", s)
}

// colours is all ansi colour codes that look ok against black
var colours = []uint8{
	9, 10, 11, 12, 13, 14, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43,
	44, 45, 46, 47, 48, 49, 50, 51, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83,
	84, 85, 86, 87, 92, 93, 94, 95, 96, 97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112,
	113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132, 133, 134, 135,
	136, 137, 138, 139, 140, 141, 142, 143, 144, 146, 147, 148, 149, 150, 151, 152, 153, 154, 155, 156, 157, 158,
	59, 160, 161, 162, 163, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173, 174, 175, 176, 177, 178, 179, 180, 181,
	182, 183, 184, 185, 186, 187, 188, 189, 190, 191, 192, 193, 194, 195, 196, 197, 198, 199, 200, 201, 202, 203, 204,
	205, 206, 207, 208, 209, 210, 211, 212, 213, 214, 215, 216, 217, 218, 219, 220, 221, 222, 223, 224, 225, 226, 227,
	228, 229, 230, 231,
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
