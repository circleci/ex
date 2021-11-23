package fakestatsd_test

import (
	"testing"

	"github.com/DataDog/datadog-go/statsd"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	"github.com/circleci/ex/testing/fakestatsd"
)

func TestExample(t *testing.T) {
	// The server closes automatically at the end of the test
	s := fakestatsd.New(t)

	stats, err := statsd.New(s.Addr(),
		statsd.WithNamespace("com.mycompany."),
		statsd.WithTags([]string{"version:1.2.3"}),
	)
	assert.Assert(t, err)
	t.Cleanup(func() {
		err = stats.Close()
		assert.Assert(t, err)
	})

	t.Run("Send some stats", func(t *testing.T) {
		err = stats.Count("my_count", 1, []string{"mytag:value"}, 1)
		assert.Check(t, err)
	})

	t.Run("Check stats were sent", func(t *testing.T) {
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if len(s.Metrics()) == 0 {
				return poll.Continue("no metrics found")
			}
			return poll.Success()
		})
		assert.Check(t, cmp.DeepEqual([]fakestatsd.Metric{
			{Name: "com.mycompany.my_count", Value: "1|c|", Tags: "version:1.2.3 mytag:value"},
		}, s.Metrics()))
	})
}
