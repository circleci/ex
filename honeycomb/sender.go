package honeycomb

import (
	"errors"

	libhoney "github.com/honeycombio/libhoney-go"
	"github.com/honeycombio/libhoney-go/transmission"
)

// This has been submitted upstream as
// https://github.com/honeycombio/libhoney-go/pull/60

func newSender(send bool) transmission.Sender {
	s := &MultiSender{}

	if send {
		s.Senders = append(s.Senders, &transmission.Honeycomb{
			MaxBatchSize:         libhoney.DefaultMaxBatchSize,
			BatchTimeout:         libhoney.DefaultBatchTimeout,
			MaxConcurrentBatches: libhoney.DefaultMaxConcurrentBatches,
			PendingWorkCapacity:  libhoney.DefaultPendingWorkCapacity,
			UserAgentAddition:    libhoney.UserAgentAddition,
		})
	}

	s.Senders = append(s.Senders, &transmission.WriterSender{})

	return s
}

type MultiSender struct {
	Senders []transmission.Sender
}

// Add calls Add on every configured Sender
func (s *MultiSender) Add(ev *transmission.Event) {
	for _, tx := range s.Senders {
		tx.Add(ev)
	}
}

// Start calls Start on every configured Sender, aborting on the first error
func (s *MultiSender) Start() error {
	if len(s.Senders) == 0 {
		return errors.New("no senders configured")
	}
	for _, tx := range s.Senders {
		if err := tx.Start(); err != nil {
			return err
		}
	}
	return nil
}

// Stop calls Stop on every configured Sender, aborting on the first error
func (s *MultiSender) Stop() error {
	for _, tx := range s.Senders {
		if err := tx.Stop(); err != nil {
			return err
		}
	}
	return nil
}

// TxResponses returns the response channel from the first Sender only
func (s *MultiSender) TxResponses() chan transmission.Response {
	return s.Senders[0].TxResponses()
}

// SendResponse calls SendResponse on every configured Sender
func (s *MultiSender) SendResponse(resp transmission.Response) bool {
	pending := false
	for _, tx := range s.Senders {
		pending = pending || tx.SendResponse(resp)
	}
	return pending
}
