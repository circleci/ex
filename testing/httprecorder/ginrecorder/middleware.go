/*
Package ginrecorder provides a middleware to wire a httprecorder into Gin routers used in test fakes.
*/
package ginrecorder

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/httprecorder"
)

func Middleware(ctx context.Context, rec *httprecorder.RequestRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := rec.Record(c.Request)
		if err != nil {
			o11y.LogError(ctx, "problem recording HTTP request", err)
		}
		c.Next()
	}
}
