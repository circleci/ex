package rabbit

import (
	"context"
	"encoding/json"
	"errors"

	pool "github.com/jolestar/go-commons-pool/v2"
	"github.com/makasim/amqpextra"
	"github.com/makasim/amqpextra/publisher"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/circleci/ex/o11y"
)

type PublisherPool struct {
	pool *pool.ObjectPool
	name string
}

func NewPublisherPool(ctx context.Context, name string, dialer *amqpextra.Dialer,
	opts ...publisher.Option) *PublisherPool {

	opts = append(opts,
		publisher.WithConfirmation(10),
		publisher.WithInitFunc(func(conn publisher.AMQPConnection) (publisher.AMQPChannel, error) {
			ch, err := conn.(*amqp.Connection).Channel()
			if err != nil {
				return nil, err
			}

			ch.NotifyReturn(newReturnedMessageHandler(ctx))
			return ch, nil
		}),
	)

	create := func(ctx context.Context) (interface{}, error) {
		return dialer.Publisher(opts...)
	}
	destroy := func(ctx context.Context, object *pool.PooledObject) error {
		pub := object.Object.(*publisher.Publisher)
		pub.Close()
		return nil
	}
	factory := pool.NewPooledObjectFactory(create, destroy, nil, nil, nil)

	poolConfig := pool.NewDefaultPoolConfig()
	poolConfig.MaxTotal = 16
	poolConfig.MaxIdle = 16

	return &PublisherPool{
		name: name,
		pool: pool.NewObjectPool(context.Background(), factory, poolConfig),
	}
}

func (p *PublisherPool) Close(ctx context.Context) (err error) {
	ctx, span := o11y.StartSpan(ctx, "pool: close")
	defer o11y.End(span, &err)

	p.pool.Close(ctx)
	return nil
}

func (p *PublisherPool) Publish(ctx context.Context, msg publisher.Message) (err error) {
	ctx, span := o11y.StartSpan(ctx, "pool: publish")
	defer o11y.End(span, &err)
	span.AddField("exchange", msg.Exchange)
	span.AddField("key", msg.Key)
	span.AddField("content_type", msg.Publishing.ContentType)

	return p.publish(ctx, msg)
}

const JSON = "application/json; charset=utf-8"

func (p *PublisherPool) PublishJSON(ctx context.Context, msg publisher.Message, v interface{}) (err error) {
	ctx, span := o11y.StartSpan(ctx, "pool: publish_json")
	defer o11y.End(span, &err)
	span.AddField("exchange", msg.Exchange)
	span.AddField("key", msg.Key)
	span.AddField("content_type", JSON)

	msg.Publishing.ContentType = JSON
	msg.Publishing.Body, err = json.Marshal(v)
	if err != nil {
		return err
	}

	return p.publish(ctx, msg)
}

func (p *PublisherPool) publish(ctx context.Context, msg publisher.Message) error {
	msg.Mandatory = true
	msg.Context = ctx

	obj, err := p.pool.BorrowObject(ctx)
	if err != nil {
		return err
	}
	pub := obj.(*publisher.Publisher)
	defer func() {
		err := p.pool.ReturnObject(ctx, obj)
		if err != nil {
			o11y.LogError(ctx, "pool: error returning publisher to pool", err)
			pub.Close()
		}
	}()

	return pub.Publish(msg)
}

func (p *PublisherPool) MetricName() string {
	return p.name
}

func (p *PublisherPool) Gauges(_ context.Context) map[string]float64 {
	return map[string]float64{
		"active":    float64(p.pool.GetNumActive()),
		"idle":      float64(p.pool.GetNumIdle()),
		"destroyed": float64(p.pool.GetDestroyedCount()),
		"max_total": float64(p.pool.Config.MaxTotal),
		"max_idle":  float64(p.pool.Config.MaxIdle),
		"min_idle":  float64(p.pool.Config.MinIdle),
	}
}

func newReturnedMessageHandler(ctx context.Context) chan amqp.Return {
	returnNotify := make(chan amqp.Return)
	go func() {
		// The Go channel will be closed by the AMQP channel on shutdown
		for r := range returnNotify {
			o11y.LogError(ctx, "pool: returned message", errors.New(r.ReplyText),
				o11y.Field("reply_code", r.ReplyCode),
				o11y.Field("exchange", r.Exchange),
				o11y.Field("routing_key", r.RoutingKey),
			)
		}
	}()
	return returnNotify
}
