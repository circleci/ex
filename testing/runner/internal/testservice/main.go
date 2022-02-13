package main

import (
	"net/http"
	"os"

	"github.com/alecthomas/kong"
	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/httpserver"
	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/circleci/ex/httpserver/healthcheck"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/system"
	"github.com/circleci/ex/testing/testcontext"
)

type conf struct {
	AdminOnly bool `name:"admin-only" env:"ADMIN_ONLY" default:"false" help:"Only launch a service with an admin server"`
}

func main() {
	c := &conf{}
	kong.Parse(c)

	err := run(c.AdminOnly)
	if err != nil {
		panic(err)
	}
}

func run(adminOnly bool) error {
	ctx := testcontext.Background()

	sys := system.New()

	var err error
	if !adminOnly {
		r := ginrouter.Default(ctx, "server-name-for-o11y")
		r.GET("/api/env", func(c *gin.Context) {
			c.JSON(http.StatusOK, os.Environ())
		})

		_, err = httpserver.Load(ctx, httpserver.Config{
			Name:    "the-server-name",
			Addr:    "localhost:0",
			Handler: r,
		}, sys)
		if err != nil {
			return err
		}
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
