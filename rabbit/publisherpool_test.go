package rabbit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/makasim/amqpextra"
	"github.com/makasim/amqpextra/consumer"
	"github.com/makasim/amqpextra/publisher"
	amqp "github.com/rabbitmq/amqp091-go"
	"golang.org/x/sync/errgroup"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/poll"

	"github.com/circleci/ex/internal/syncbuffer"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/testing/fakestatsd"
	"github.com/circleci/ex/testing/rabbitfixture"
)

const queueName = "queue-name"

func TestPublisherPool_PublishJSON(t *testing.T) {
	ctx, metricsFixture := newMetricsFixture(t)

	u := rabbitfixture.New(ctx, t)
	consumerDialer := createQueueAndListener(ctx, t, u)

	var received sync.Map
	setupConsumer(ctx, t, consumerDialer, &received)
	pool := createPool(ctx, t, u)

	type payload struct {
		A string `json:"a"`
		B int    `json:"b"`
	}

	t.Run("Send JSON message", func(t *testing.T) {
		err := pool.PublishJSON(ctx,
			publisher.Message{Key: queueName},
			payload{A: "value-of-a", B: 1234},
		)
		assert.Check(t, err)
	})

	t.Run("Check message was received", func(t *testing.T) {
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			size := syncMapLen(&received)
			if size != 1 {
				return poll.Continue("not enough messages received: %d", size)
			}

			return poll.Success()
		})

		got := ""
		received.Range(func(k, _ interface{}) bool {
			got = k.(string)
			return true
		})
		assert.Check(t, cmp.DeepEqual(`{"a":"value-of-a","b":1234}`, got))
	})

	t.Run("Check metrics", func(t *testing.T) {
		gotMetric := metricsFixture.waitForMetric(t)
		expectedTags := []string{"exchange:", "key:queue-name", "content_type:application/json; charset=utf-8"}
		assert.Check(t, cmp.DeepEqual(expectedTags, gotMetric.Tags))
	})
}

func TestPublisherPool_Publish(t *testing.T) {
	ctx, metricsFixture := newMetricsFixture(t)

	u := rabbitfixture.New(ctx, t)
	consumerDialer := createQueueAndListener(ctx, t, u)

	var received sync.Map
	setupConsumer(ctx, t, consumerDialer, &received)
	pool := createPool(ctx, t, u)

	t.Run("Send JSON message", func(t *testing.T) {
		err := pool.Publish(ctx, publisher.Message{
			Key: queueName,
			Publishing: amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte("hello rabbit"),
			},
		})
		assert.Check(t, err)
	})

	t.Run("Check message was received", func(t *testing.T) {
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			size := syncMapLen(&received)
			if size != 1 {
				return poll.Continue("not enough messages received: %d", size)
			}
			return poll.Success()
		})

		got := ""
		received.Range(func(k, _ interface{}) bool {
			got = k.(string)
			return true
		})
		assert.Check(t, cmp.DeepEqual("hello rabbit", got))
	})

	t.Run("Check metrics", func(t *testing.T) {
		gotMetric := metricsFixture.waitForMetric(t)
		expectedTags := []string{"exchange:", "key:queue-name", "content_type:text/plain"}
		assert.Check(t, cmp.DeepEqual(expectedTags, gotMetric.Tags))
	})
}

func TestPublisherPool_MandatoryRouting(t *testing.T) {
	var buf syncbuffer.SyncBuffer
	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format: "text",
		Writer: &buf,
	}))

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	t.Cleanup(cancel)

	u := rabbitfixture.New(ctx, t)
	pool := createPool(ctx, t, u)

	t.Run("Check sending fails", func(t *testing.T) {
		err := pool.Publish(ctx,
			publisher.Message{Key: "does not exist"},
		)
		assert.Check(t, err)
	})

	t.Run("Check message was returned", func(t *testing.T) {
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			if !strings.Contains(buf.String(), "returned message") {
				return poll.Continue("no returned message found")
			}

			return poll.Success()
		})

		s := buf.String()
		t.Log(s)

		assert.Check(t, cmp.Contains(s, "app.reply_code=312"))
		assert.Check(t, cmp.Contains(s, "app.routing_key=does not exist"))
		assert.Check(t, cmp.Contains(s, "error=NO_ROUTE"))
		assert.Check(t, cmp.Contains(s, "result=error"))
	})
}
func TestPublisherPool_OptionalRouting(t *testing.T) {
	var buf syncbuffer.SyncBuffer
	ctx := o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format: "text",
		Writer: &buf,
	}))

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	t.Cleanup(cancel)

	u := rabbitfixture.New(ctx, t)
	pool := createPool(ctx, t, u)

	t.Run("Check sending does not fail", func(t *testing.T) {
		err := pool.PublishOptional(ctx,
			publisher.Message{Key: "does not exist"},
		)
		assert.Equal(t, err, nil)
	})
}

func TestPublisherPool_Publish_LoadTest(t *testing.T) {
	// Use non-o11y context to keep test logs clear
	ctx := context.Background()

	u := rabbitfixture.New(ctx, t)
	consumerDialer := createQueueAndListener(ctx, t, u)

	var received sync.Map
	setupConsumer(ctx, t, consumerDialer, &received)
	pool := createPool(ctx, t, u)

	t.Run("Send lots of messages in parallel", func(t *testing.T) {
		g, ctx := errgroup.WithContext(ctx)
		for i := 0; i < 10; i++ {
			i := i // copy to avoid overwriting race condition
			g.Go(func() error {
				for j := 0; j < 100; j++ {
					err := pool.Publish(ctx, publisher.Message{
						Key: queueName,
						Publishing: amqp.Publishing{
							ContentType: "text/plain",
							Body:        []byte(fmt.Sprintf("hello there! %d %d", i, j)),
						},
					})
					if err != nil {
						return err
					}
				}
				return nil
			})
		}
		assert.Check(t, g.Wait())
	})

	t.Run("Check received counts", func(t *testing.T) {
		poll.WaitOn(t, func(t poll.LogT) poll.Result {
			size := syncMapLen(&received)
			if size != 1000 {
				return poll.Continue("not enough messages received: %d", size)
			}

			return poll.Success()
		})
	})
}

func setupConsumer(ctx context.Context, t *testing.T, consumerDialer *amqpextra.Dialer, received *sync.Map) {
	c, err := consumerDialer.Consumer(
		consumer.WithContext(ctx),
		consumer.WithQueue(queueName),
		consumer.WithHandler(consumer.HandlerFunc(func(ctx context.Context, msg amqp.Delivery) interface{} {
			ctx, span := o11y.StartSpan(ctx, "testconsumer", o11y.WithSpanKind(o11y.SpanKindConsumer))
			defer span.End()
			body := string(msg.Body)
			received.Store(body, true)
			span.AddField("body", body)

			err := msg.Ack(false)
			assert.Check(t, err)
			return nil
		})),
	)
	assert.Assert(t, err)
	t.Cleanup(c.Close)
}

func createPool(ctx context.Context, t *testing.T, u string) *PublisherPool {
	t.Helper()

	dialer, err := amqpextra.NewDialer(
		amqpextra.WithURL(u),
	)
	assert.Check(t, err)
	t.Cleanup(dialer.Close)

	pool := NewPublisherPool(ctx, "the-name", dialer)
	t.Cleanup(func() {
		assert.Check(t, pool.Close(ctx))
	})
	return pool
}

func createQueueAndListener(ctx context.Context, t *testing.T, u string) *amqpextra.Dialer {
	t.Helper()

	consumerDialer := rabbitfixture.Dialer(ctx, t, u)

	t.Run("Create queue topology", func(t *testing.T) {
		conn, err := consumerDialer.Connection(ctx)
		assert.Assert(t, err)
		t.Cleanup(func() {
			assert.Check(t, conn.Close())
		})

		ch, err := conn.Channel()
		assert.Assert(t, err)
		t.Cleanup(func() {
			assert.Check(t, ch.Close())
		})

		_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
		assert.Assert(t, err)
	})

	return consumerDialer
}

func syncMapLen(received *sync.Map) int {
	receivedCount := 0
	received.Range(func(_, _ interface{}) bool {
		receivedCount++
		return true
	})
	return receivedCount
}

type metricsFixture struct {
	s *fakestatsd.FakeStatsd
}

func newMetricsFixture(t *testing.T) (context.Context, metricsFixture) {
	m := metricsFixture{
		s: fakestatsd.New(t),
	}
	stats, err := statsd.New(m.s.Addr())
	assert.Assert(t, err)

	return o11y.WithProvider(context.Background(), honeycomb.New(honeycomb.Config{
		Format:  "color",
		Metrics: stats,
	})), m
}

func (m *metricsFixture) waitForMetric(t *testing.T) fakestatsd.Metric {
	var metric fakestatsd.Metric
	poll.WaitOn(t, func(t poll.LogT) poll.Result {
		for _, m := range m.s.Metrics() {
			if m.Name == "pool.publish" {
				metric = m
				return poll.Success()
			}
		}
		return poll.Continue("no pool.publish metric found yet")
	}, poll.WithTimeout(time.Second))
	return metric
}
