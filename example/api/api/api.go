package api

import (
	"context"
	"net/http"

	"github.com/circleci/ex/httpserver/ginrouter"
	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/example/service"
)

type API struct {
	router *gin.Engine
	store  *service.Store
}

type Options struct {
	Store *service.Store
}

func New(ctx context.Context, opts Options) *API {
	r := ginrouter.Default(ctx, "api")
	a := &API{
		router: r,
		store:  opts.Store,
	}

	r.GET("/api/hello", a.getHelloWorld)

	return a
}

func (a *API) Handler() http.Handler {
	return a.router
}
