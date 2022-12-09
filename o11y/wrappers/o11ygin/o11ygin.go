package o11ygin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/wrappers/baggage"
)

// Middleware for Gin router
//
//nolint:funlen
func Middleware(provider o11y.Provider, serverName string, queryParams map[string]struct{}) gin.HandlerFunc {
	m := provider.MetricsProvider()
	return func(c *gin.Context) {
		before := time.Now()

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
		span.AddRawField("meta.type", "http_server")
		span.AddRawField("http.server_name", serverName)
		span.AddRawField("http.route", c.FullPath())
		span.AddRawField("http.client_ip", c.ClientIP())

		// Common OTEL attributes
		span.AddRawField("http.method", c.Request.Method)
		span.AddRawField("http.url", c.Request.URL.String())
		span.AddRawField("http.target", c.Request.URL.Path)
		span.AddRawField("http.host", c.Request.Host)
		span.AddRawField("http.scheme", c.Request.URL.Scheme)
		span.AddRawField("http.user_agent", c.Request.UserAgent())
		span.AddRawField("http.request_content_length", c.Request.ContentLength)

		defer func() {
			// Common OTEL attributes
			span.AddRawField("http.status_code", c.Writer.Status())
			span.AddRawField("http.response_content_length", c.Writer.Size())

			if m != nil {
				_ = m.TimeInMilliseconds("handler",
					float64(time.Since(before).Nanoseconds())/1000000.0,
					[]string{
						"http.server_name:" + serverName,
						"http.method:" + c.Request.Method,
						"http.route:" + c.FullPath(),
						"http.status_code:" + strconv.Itoa(c.Writer.Status()),
						//TODO: "has_panicked:"+,
					},
					1,
				)
			}
		}()
		// Run the next function in the Middleware chain
		c.Next()
	}
}

func ClientCancelled() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		defer func() {
			if errors.Is(ctx.Err(), context.Canceled) {
				c.Status(499)
			}
		}()
		c.Next()
	}
}

func Recovery() func(c *gin.Context) {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, err interface{}) {
		c.AbortWithStatus(http.StatusInternalServerError)
		ctx := c.Request.Context()
		span := o11y.FromContext(ctx).GetSpan(ctx)

		// Most likely caused by one side of the proxy disappearing. Not really a panic
		// https://github.com/golang/go/issues/28239
		if origErr, ok := err.(error); ok && errors.Is(origErr, http.ErrAbortHandler) {
			// prevent reporting to rollbar for this expected error, report as an error instead
			o11y.AddResultToSpan(span, origErr)
			return
		}

		_ = o11y.HandlePanic(ctx, span, err, c.Request)
	})
}

func startSpanOrTraceFromHTTP(ctx context.Context,
	c *gin.Context, p o11y.Provider, serverName string) (context.Context, o11y.Span) {

	span := p.GetSpan(ctx)
	if span == nil {
		// there is no trace yet. We should make one! and use the root span.
		ctx, span := p.Helpers().InjectPropagation(ctx, o11y.PropagationContextFromHeader(c.Request.Header))
		span.AddRawField("name", fmt.Sprintf("http-server %s: %s %s", serverName, c.Request.Method, c.FullPath()))
		return ctx, span
	} else {
		// we had a parent! let's make a new child for this handler
		ctx, span = o11y.StartSpan(ctx, fmt.Sprintf("http-server %s: %s %s", serverName, c.Request.Method, c.FullPath()))
	}
	return ctx, span
}
