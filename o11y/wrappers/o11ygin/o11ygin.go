package o11ygin

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/honeycombio/beeline-go/propagation"
	"github.com/honeycombio/beeline-go/trace"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/honeycomb"
	"github.com/circleci/ex/o11y/wrappers/baggage"
)

func Middleware(provider o11y.Provider, serverName string, queryParams map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := o11y.WithProvider(c.Request.Context(), provider)
		ctx = o11y.WithBaggage(ctx, baggage.Get(ctx, c.Request))
		ctx, span := startSpanOrTraceFromHTTP(ctx, c, provider, serverName)
		defer span.End()

		c.Request = c.Request.WithContext(ctx)

		// pull out any variables in the URL, add the thing we're matching, etc.
		for _, param := range c.Params {
			span.AddRawField("handler.vars."+param.Key, param.Value)
		}

		// pull out any GET query params
		if queryParams != nil {
			for key, value := range c.Request.URL.Query() {
				if _, ok := queryParams[key]; ok {
					if len(value) > 1 {
						span.AddRawField("handler.query."+key, value)
					} else if len(value) == 1 {
						span.AddRawField("handler.query."+key, value[0])
					} else {
						span.AddRawField("handler.query."+key, nil)
					}
				}
			}
		}

		// Server OTEL attributes
		span.AddRawField("http.server_name", serverName)
		span.AddRawField("http.route", c.FullPath())
		span.AddRawField("http.client_ip", c.ClientIP())

		// Common OTEL attributes
		span.AddRawField("http.method", c.Request.Method)
		span.AddRawField("http.url", c.Request.URL.String())
		span.AddRawField("http.target", c.Request.URL.Path)
		span.AddRawField("http.host", c.Request.Host)
		span.AddRawField("http.scheme", c.Request.Host)
		span.AddRawField("http.user_agent", c.Request.UserAgent())
		span.AddRawField("http.request_content_length", c.Request.ContentLength)

		defer func() {
			// Common OTEL attributes
			span.AddRawField("http.status_code", c.Writer.Status())
			span.AddRawField("http.response_content_length", c.Writer.Size())

			span.RecordMetric(o11y.Timing("handler",
				"server_name", "http.method", "http.route", "http.status_code", "has_panicked"))
		}()
		// Run the next function in the Middleware chain
		c.Next()
	}
}

func Recovery() func(c *gin.Context) {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err interface{}) {
		c.AbortWithStatus(http.StatusInternalServerError)
		ctx := c.Request.Context()
		span := o11y.FromContext(ctx).GetSpan(ctx)
		_ = o11y.HandlePanic(ctx, span, err, c.Request)
	})
}

func startSpanOrTraceFromHTTP(ctx context.Context, c *gin.Context, p o11y.Provider, serverName string) (
	context.Context, o11y.Span) {

	span := p.GetSpan(ctx)
	if span == nil {
		// there is no trace yet. We should make one! and use the root span.
		beelineHeader := c.Request.Header.Get(propagation.TracePropagationHTTPHeader)
		prop, _ := propagation.UnmarshalHoneycombTraceContext(beelineHeader)

		var tr *trace.Trace
		ctx, tr = trace.NewTrace(ctx, prop)
		span = honeycomb.WrapSpan(tr.GetRootSpan())
		span.AddRawField("name", fmt.Sprintf("http-server %s: %s %s", serverName, c.Request.Method, c.FullPath()))
	} else {
		// we had a parent! let's make a new child for this handler
		ctx, span = o11y.StartSpan(ctx, fmt.Sprintf("http-server %s: %s %s", serverName, c.Request.Method, c.FullPath()))
	}
	return ctx, span
}
