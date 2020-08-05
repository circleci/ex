package honeycomb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// TextFormatter provides human readable output from honeycomb JSON output.
// It writes the formatted output to the wrapped io.Writer.
type TextFormatter struct {
	w      io.Writer
	colour bool
}

// DefaultTextFormat writes human-readable traces to stderr.
var DefaultTextFormat = &TextFormatter{
	w: os.Stderr,
}

// ColourTextFormat writes colourful human-readable traces to stdout.
var ColourTextFormat = &TextFormatter{
	w:      os.Stdout,
	colour: true,
}

func (h *TextFormatter) Write(raw []byte) (int, error) {
	data := &entry{}
	err := json.Unmarshal(raw, data)
	if err != nil {
		return 0, err
	}
	_, err = h.w.Write(h.format(data))
	return len(raw), err
}

type entry struct {
	Dataset string                 `json:"dataset"`
	Time    time.Time              `json:"time"`
	Data    map[string]interface{} `json:"data"`
}

func (h *TextFormatter) format(e *entry) []byte {
	buf := new(bytes.Buffer)
	_, _ = fmt.Fprintf(buf, "%s %s %.3fms %s",
		e.Time.Format("15:04:05"),
		h.applyColour(formatTraceID(e.Data["trace.trace_id"])),
		e.Data["duration_ms"],
		h.applyColour(fmt.Sprintf("%s", e.Data["name"])),
	)

	for _, k := range sortedKeys(e.Data) {
		if h.exclude(k) {
			continue
		}
		label := k // we have to copy the key, so we can use the original to lookup the data
		if k == "error" && h.colour {
			label = errorHighlight(k)
		}
		_, _ = fmt.Fprintf(buf, " %s=%v", label, e.Data[k])
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func (h *TextFormatter) exclude(k string) bool {
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

func (h *TextFormatter) applyColour(value string) string {
	if !h.colour {
		return value
	}

	i := crc32.Checksum([]byte(value), crc32.IEEETable) % uint32(len(colours))
	return fmt.Sprintf("\033[1;38;5;%dm%s\033[0m", colours[i], value)
}

func formatTraceID(raw interface{}) string {
	traceID, ok := raw.(string)
	if !ok {
		return "unkwn"
	}
	return traceID[len(traceID)-5:]
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
