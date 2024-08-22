package honeycomb

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/honeycombio/libhoney-go/transmission"

	"github.com/circleci/ex/colourise"
)

// TextSender implements the transmission.Sender interface by marshalling events to
// human-readable text and writing to the writer w, with optional colour
//
// the implementation is heavily cribbed from honeycomb's transmission.WriterSender
type TextSender struct {
	sync.Mutex

	w      io.Writer
	colour bool

	responses chan transmission.Response
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
			label = colourise.ErrorHighlight(k)
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
	return colourise.ApplyColour(value)
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
