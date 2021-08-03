package ginrouter

import (
	"context"

	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/wrappers/o11ygin"
)

func Default(ctx context.Context, serverName string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(
		o11ygin.Middleware(o11y.FromContext(ctx), serverName, nil),
		o11ygin.Recovery(),
	)

	r.UseRawPath = true

	return r
}
