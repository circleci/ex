package o11y

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/config/secret"
	"github.com/circleci/ex/o11y/honeycomb"
)

func TestO11Y_SecretRedacted(t *testing.T) {
	// confirm that honeycomb uses the json marshaller and that we dont see secrets
	buf := bytes.Buffer{}
	provider := honeycomb.New(honeycomb.Config{
		Writer: &buf,
	})
	ctx := context.Background()
	ctx, span := provider.StartSpan(ctx, "secret test")
	s := secret.String("super-secret")
	span.AddField("secret", s)
	span.End()
	provider.Close(ctx)
	assert.Check(t, !strings.Contains(buf.String(), "super-secret"), buf.String())
	assert.Check(t, cmp.Contains(buf.String(), "REDACTED"))
}

func TestO11Y_SecretRedactedColor(t *testing.T) {
	// This test can be run to *VISUALLY* confirm that color honeycomb
	// formatter uses the json marshaller under the hood and that we dont see secrets.
	// Once you provide a writer the Format is ignored so this test does
	// not assert on anything since we cant catch the output
	//
	// The test is left in so that it can be eyeballed with -v
	// The other buffer based test does assert and it is very likely if secrets
	// leak in this test then the above test will fail in any case
	provider := honeycomb.New(honeycomb.Config{
		Format: "color",
	})
	ctx := context.Background()
	ctx, span := provider.StartSpan(ctx, "secret test")
	s := secret.String("super-secret")
	span.AddField("secret", s)
	span.End()
	provider.Close(ctx)
}

func TestSetup_DoesNotError(t *testing.T) {
	ctx := context.Background()
	ctx, cleanup, err := Setup(ctx, Config{
		Statsd:            "127.0.0.1:8125",
		RollbarToken:      "qwertyuiop",
		RollbarDisabled:   true,
		RollbarEnv:        "production",
		RollbarServerRoot: "github.com/circleci/ex",
		HoneycombEnabled:  false,
		HoneycombDataset:  "does-not-exist",
		HoneycombKey:      "1234567890",
		SampleTraces:      false,
		Format:            "color",
		Version:           "1.2.3",
		Service:           "test-service",
		StatsNamespace:    "test.service",
		Mode:              "banana",
		Debug:             true,
	})
	assert.Assert(t, err)
	cleanup(ctx)
}
