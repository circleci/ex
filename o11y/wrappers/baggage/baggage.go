package baggage

import (
	"context"
	"net/http"

	"github.com/circleci/ex/o11y"
)

func Get(ctx context.Context, r *http.Request) o11y.Baggage {
	serialized := r.Header.Get("otcorrelations")
	if serialized == "" {
		return o11y.Baggage{}
	}
	b, err := o11y.DeserializeBaggage(serialized)
	if err != nil {
		provider := o11y.FromContext(ctx)
		provider.Log(ctx, "malformed baggage", o11y.Field("baggage", serialized))
	}
	return b
}
