package honeycomb

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/honeycombio/libhoney-go/transmission"
)

// TextSender implements the transmission.Sender interface by marshalling events to
// human-readable text and writing to the writer w, with optional colour
//
// the implementation is heavily cribbed from honeycomb's transmission.WriterSender
type TextSender struct {
	w      io.Writer
	colour bool

	responses chan transmission.Response

	sync.Mutex
}

func (t *TextSender) Start() error {
	t.responses = make(chan transmission.Response, 100)
	return nil
}

func (t *TextSender) Stop() error { return nil }

func (t *TextSender) Flush() error { return nil }

func (t *TextSender) Add(ev *transmission.Event) {
	m := t.format(ev)

	t.Lock()
	defer t.Unlock()
	_, _ = t.w.Write(m)
	resp := transmission.Response{
		// TODO what makes sense to set in the response here?
		Metadata: ev.Metadata,
	}
	t.SendResponse(resp)
}

func (t *TextSender) TxResponses() chan transmission.Response {
	return t.responses
}

func (t *TextSender) SendResponse(r transmission.Response) bool {
	select {
	case t.responses <- r:
	default:
		return true
	}
	return false
}

func (t *TextSender) format(ev *transmission.Event) []byte {
	buf := new(bytes.Buffer)
	_, _ = fmt.Fprintf(buf, "%s %s %.3fms %s",
		ev.Timestamp.Format("15:04:05"),
		t.applyColour(formatTraceID(ev.Data["trace.trace_id"])),
		ev.Data["duration_ms"],
		t.applyColour(fmt.Sprintf("%s", ev.Data["name"])),
	)

	for _, k := range sortedKeys(ev.Data) {
		if t.exclude(k) {
			continue
		}
		label := k // we have to copy the key, so we can use the original to lookup the data
		if k == "error" && t.colour {
			label = errorHighlight(k)
		}
		_, _ = fmt.Fprintf(buf, " %s=%v", label, ev.Data[k])
	}
	buf.WriteString("\n")
	return buf.Bytes()
}

func (t *TextSender) exclude(k string) bool {
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

func (t *TextSender) applyColour(value string) string {
	if !t.colour {
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
