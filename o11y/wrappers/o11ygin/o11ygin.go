package o11ygin

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/honeycombio/beeline-go/wrappers/common"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/o11y/wrappers/baggage"
)

func Middleware(provider o11y.Provider, serverName string, queryParams map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// get a new context with our trace from the request, and add common fields
		ctx, span := common.StartSpanOrTraceFromHTTP(c.Request)
		defer span.Send()

		ctx = o11y.WithProvider(ctx, provider)
		ctx = o11y.WithBaggage(ctx, baggage.Get(ctx, c.Request))
		c.Request = c.Request.WithContext(ctx)

		// pull out any variables in the URL, add the thing we're matching, etc.
		for _, param := range c.Params {
			span.AddField("handler.vars."+param.Key, param.Value)
		}

		// pull out any GET query params
		if queryParams != nil {
			for key, value := range c.Request.URL.Query() {
				if _, ok := queryParams[key]; ok {
					if len(value) > 1 {
						span.AddField("handler.query."+key, value)
					} else if len(value) == 1 {
						span.AddField("handler.query."+key, value[0])
					} else {
						span.AddField("handler.query."+key, nil)
					}
				}
			}
		}

		provider.AddFieldToTrace(ctx, "server_name", serverName)
		span.AddField("name", fmt.Sprintf("http-server %s: %s %s", serverName, c.Request.Method, c.FullPath()))
		span.AddField("request.route", c.FullPath())

		// Run the next function in the Middleware chain
		c.Next()
		span.AddField("response.status_code", c.Writer.Status())

		honeycomb.WrapSpan(span).RecordMetric(o11y.Timing("handler",
			"server_name", "request.method", "request.route", "response.status_code", "has_panicked"))
	}
}
