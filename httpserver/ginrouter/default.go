package ginrouter

import (
	"context"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/o11y/wrappers/o11ygin"
)

var once sync.Once

func Default(ctx context.Context, serverName string) *gin.Engine {
	once.Do(func() {
		gin.SetMode(gin.ReleaseMode)
	})

	r := gin.New()
	r.Use(
		o11ygin.Middleware(o11y.FromContext(ctx), serverName, nil),
		o11ygin.Recovery(),
		o11ygin.ClientCancelled(),
	)

	r.UseRawPath = true

	return r
}
