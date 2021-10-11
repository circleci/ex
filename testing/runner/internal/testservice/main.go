package main

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/httpserver/healthcheck"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/system"
	"github.com/circleci/ex/testing/testcontext"
)

func main() {
	err := run()
	if err != nil {
		panic(err)
	}
}

func run() error {
	ctx := testcontext.Background()

	sys := system.New()

	r := ginrouter.Default(ctx, "server-name-for-o11y")
	r.GET("/api/env", func(c *gin.Context) {
		c.JSON(http.StatusOK, os.Environ())
	})

	_, err := httpserver.Load(ctx, "the-server-name", "localhost:0", r, sys)
	if err != nil {
		return err
	}

	_, err = healthcheck.Load(ctx, "localhost:0", sys)
	if err != nil {
		return err
	}

	err = sys.Run(ctx, 0)
	switch {
	case o11y.IsWarning(err):
		break
	case err != nil:
		return err
	}

	return nil
}
