package api

import (
	"context"
	"net/http"

	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/gin-gonic/gin"
)

type API struct {
	router *gin.Engine
}

type Options struct {
}

func New(ctx context.Context, opts Options) *API {
	r := ginrouter.Default(ctx, "api")
	a := &API{
		router: r,
	}

	r.GET("/api/hello", a.getHelloWorld)

	return a
}

func (a *API) Handler() http.Handler {
	return a.router
}
