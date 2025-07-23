package rabbitfixture

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/makasim/amqpextra"
	amqp "github.com/rabbitmq/amqp091-go"
	"gotest.tools/v3/assert"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/internal/types"
	"github.com/circleci/ex/testing/rabbitfixture/internal/rabbit"
)

func Dialer(ctx context.Context, t types.TestingTB, u string) *amqpextra.Dialer {
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

func New(ctx context.Context, t types.TestingTB) (rawurl string) {
	t.Helper()
	return NewWithConnection(ctx, t, Connection{})
}

type Connection struct {
	Host     string
	User     string
	Password secret.String
}

func NewWithConnection(ctx context.Context, t types.TestingTB, con Connection) (rawurl string) {
	t.Helper()
	ctx, span := o11y.StartSpan(ctx, "rabbitfixure: vhost")
	defer span.End()

	if con.Host == "" {
		con.Host = "localhost"
	}
	if con.User == "" {
		con.User = "guest"
	}
	if con.Password.Raw() == "" {
		con.Password = "guest"
	}

	vhost := fmt.Sprintf("%s-%s", t.Name(), randomSuffix())
	span.AddField("vhost", vhost)
	rawurl = fmt.Sprintf("amqp://%s:%s@%s/%s", con.User, con.Password.Raw(), con.Host, vhost)
	span.AddField("url", rawurl)

	client := rabbit.NewClient(fmt.Sprintf("http://%s:15672", con.Host), con.User, con.Password)

	_, err := client.ListVHosts(ctx)
	assert.Assert(t, err)

	// Delete is idempotent, and will not error for non-existent vhost
	err = client.DeleteVHost(ctx, vhost)
	assert.Assert(t, err)

	err = client.PutVHost(ctx, vhost, rabbit.VHostSettings{})
	assert.Assert(t, err)

	err = client.UpdatePermissionsIn(ctx, vhost, "guest", rabbit.Permissions{
		Configure: ".*",
		Write:     ".*",
		Read:      ".*",
	})

	t.Cleanup(func() {
		err = client.DeleteVHost(ctx, vhost)
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
