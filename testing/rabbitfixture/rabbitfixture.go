package rabbitfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/makasim/amqpextra"
	rabbithole "github.com/michaelklishin/rabbit-hole/v2"
	"github.com/streadway/amqp"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/o11y"
)

func Dialer(ctx context.Context, t testing.TB, u string) *amqpextra.Dialer {
	dialer, err := amqpextra.NewDialer(
		amqpextra.WithContext(ctx),
		amqpextra.WithURL(u),
		amqpextra.WithConnectionProperties(amqp.Table{
			"connection_name": "rabbitfixture",
		}),
	)
	assert.Assert(t, err)
	t.Cleanup(dialer.Close)
	return dialer
}

func New(ctx context.Context, t testing.TB) (rawurl string) {
	t.Helper()
	ctx, span := o11y.StartSpan(ctx, "rabbitfixure: vhost")
	defer span.End()
	vhost := fmt.Sprintf("%s-%s", t.Name(), randomSuffix())
	span.AddField("vhost", vhost)
	rawurl = fmt.Sprintf("amqp://guest:guest@localhost/%s", vhost)
	span.AddField("url", rawurl)

	rmqc, err := rabbithole.NewClient("http://localhost:15672", "guest", "guest")
	assert.Assert(t, err)

	bo := backoff.NewExponentialBackOff()
	bo.MaxElapsedTime = 5 * time.Second
	if os.Getenv("CI") == "true" {
		bo.MaxElapsedTime = 32 * time.Second
	}
	err = backoff.Retry(func() error {
		_, err := rmqc.ListNodes()
		return err
	}, backoff.WithContext(bo, ctx))
	assert.Assert(t, err)

	// Delete is idempotent, and will not error for non-existent vhost
	_, err = rmqc.DeleteVhost(vhost)
	assert.Assert(t, err)

	_, err = rmqc.PutVhost(vhost, rabbithole.VhostSettings{})
	assert.Assert(t, err)

	_, err = rmqc.UpdatePermissionsIn(vhost, "guest", rabbithole.Permissions{
		Configure: ".*",
		Write:     ".*",
		Read:      ".*",
	})
	assert.Assert(t, err)

	t.Cleanup(func() {
		_, err = rmqc.DeleteVhost(vhost)
		assert.Check(t, err)
	})

	return rawurl
}

func randomSuffix() string {
	bytes := make([]byte, 10)
	if _, err := rand.Read(bytes); err != nil {
		return "not-random--i-hope-thats-ok"
	}
	return hex.EncodeToString(bytes)
}
