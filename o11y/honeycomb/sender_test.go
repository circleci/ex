package honeycomb

import (
	"testing"
	"time"

	"github.com/honeycombio/libhoney-go/transmission"
	"gotest.tools/v3/assert"
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
		assert.Assert(t, err)
		assert.Equal(t, 1, a.Started)
		assert.Equal(t, 1, b.Started)
	})

	t.Run("Stop", func(t *testing.T) {
		err := sender.Stop()
		assert.Assert(t, err)
		assert.Equal(t, 1, a.Stopped)
		assert.Equal(t, 1, b.Stopped)
	})

	t.Run("Add", func(t *testing.T) {
		ev := transmission.Event{
			Timestamp:  time.Time{}.Add(time.Second),
			SampleRate: 2,
			Dataset:    "dataset",
			Data:       map[string]interface{}{"key": "val"},
		}

		sender.Add(&ev)

		assert.Equal(t, len(a.Events()), 1)
		assert.DeepEqual(t, ev, *a.Events()[0])
		assert.Equal(t, len(b.Events()), 1)
		assert.DeepEqual(t, ev, *b.Events()[0])
	})

	t.Run("TxResponses takes the first one", func(t *testing.T) {
		assert.Equal(t, a.TxResponses(), sender.TxResponses())
		assert.Assert(t, b.TxResponses() != sender.TxResponses())
	})

	t.Run("SendResponse", func(t *testing.T) {
		assert.Equal(t, sender.SendResponse(transmission.Response{}), false)
	})
}
