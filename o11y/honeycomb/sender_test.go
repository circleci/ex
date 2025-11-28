package honeycomb

import (
	"testing"
	"time"

	"github.com/honeycombio/libhoney-go/transmission"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// This has been submitted upstream as
// https://github.com/honeycombio/libhoney-go/pull/60

func TestMultiSender(t *testing.T) {
	a := &transmission.MockSender{}
	b := &transmission.MockSender{}
	sender := MultiSender{
		Senders: []transmission.Sender{a, b},
	}

	t.Run("Start", func(t *testing.T) {
		err := sender.Start()
		assert.Check(t, err)
		assert.Check(t, cmp.Equal(1, a.Started))
		assert.Check(t, cmp.Equal(1, b.Started))
	})

	t.Run("Stop", func(t *testing.T) {
		err := sender.Stop()
		assert.Check(t, err)
		assert.Check(t, cmp.Equal(1, a.Stopped))
		assert.Check(t, cmp.Equal(1, b.Stopped))
	})

	t.Run("Add", func(t *testing.T) {
		ev := transmission.Event{
			Timestamp:  time.Time{}.Add(time.Second),
			SampleRate: 2,
			Dataset:    "dataset",
			Data:       map[string]interface{}{"key": "val"},
		}

		sender.Add(&ev)

		assert.Check(t, cmp.Len(a.Events(), 1))
		assert.Check(t, cmp.DeepEqual(ev, *a.Events()[0]))
		assert.Check(t, cmp.Len(b.Events(), 1))
		assert.Check(t, cmp.DeepEqual(ev, *b.Events()[0]))
	})

	t.Run("TxResponses takes the first one", func(t *testing.T) {
		assert.Check(t, cmp.Equal(a.TxResponses(), sender.TxResponses()))
		assert.Check(t, b.TxResponses() != sender.TxResponses())
	})

	t.Run("SendResponse", func(t *testing.T) {
		assert.Check(t, cmp.Equal(sender.SendResponse(transmission.Response{}), false))
	})
}
