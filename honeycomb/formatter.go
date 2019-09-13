package honeycomb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// TextFormatter provides human readable output from honeycomb JSON output.
// It writes the formatted output to the wrapped io.Writer.
type TextFormatter struct {
	// FieldPrefixes are compared against the keys in entry.data. If any keys
	// match the prefix, the field will be included in the formatted output.
	FieldPrefixes []string
	W             io.Writer
}

// DefaultTextFormat writes to stderr.
var DefaultTextFormat = &TextFormatter{
	W:             os.Stderr,
	FieldPrefixes: []string{"app", "request", "response"},
}

func (h *TextFormatter) Write(raw []byte) (int, error) {
	data := &entry{}
	err := json.Unmarshal(raw, data)
	if err != nil {
		return 0, err
	}
	_, err = h.W.Write(format(data, h.FieldPrefixes))
	return len(raw), err
}

type entry struct {
	Dataset string                 `json:"dataset"`
	Time    time.Time              `json:"time"`
	Data    map[string]interface{} `json:"data"`
}

func format(e *entry, fields []string) []byte {
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "%s %s %.3fms %s",
		e.Time.Format("15:04:05"),
		formatTraceID(e.Data["trace.trace_id"]),
		e.Data["duration_ms"],
		e.Data["name"])

	for _, k := range keys(e.Data) {
		for _, field := range fields {
			if strings.HasPrefix(k, field+".") {
				fmt.Fprintf(buf, " %s=%s", k, e.Data[k])
			}
		}
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func formatTraceID(raw interface{}) string {
	traceID, ok := raw.(string)
	if !ok {
		return "unkwn"
	}
	return traceID[len(traceID)-5:]
}

func keys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
