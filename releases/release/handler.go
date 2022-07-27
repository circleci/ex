package release

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/circleci/ex/o11y"
)

// For now we only have one implementation for this.
// One desired feature is to have a more complex gradual rollout strategy, like gradual rollout inside a namespace,
// so we do not upgrade the entire customer runner fleet at once. Hence, the interface is worth keeping.
type releaseTypeResolver interface {
	ReleaseType(ctx context.Context) string
}

type HandlerConfig struct {
	List *List

	// Resolver is optional
	Resolver releaseTypeResolver
}

func Handler(cfg HandlerConfig) func(c *gin.Context) {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req Requirements
		err := c.BindJSON(&req)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"message": fmt.Sprintf("bad request: %s", err),
			})
			return
		}

		if err = req.Validate(); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"message": fmt.Sprintf("bad request: %s", err),
			})
			return
		}

		if req.Version == "" {
			if cfg.Resolver == nil {
				req.Version = cfg.List.Latest()
			} else {
				releaseType := cfg.Resolver.ReleaseType(ctx)
				o11y.AddField(ctx, "release_type", releaseType)
				req.Version = cfg.List.LatestFor(releaseType)
			}
		}

		rel, err := cfg.List.Lookup(ctx, req)

		switch {
		case errors.Is(err, ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"message": fmt.Sprintf("no download found for version=%q os=%q arch=%q",
					req.Version,
					req.Platform,
					req.Arch,
				),
			})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{
				"message": "An internal error has occurred",
			})
		default:
			c.JSON(http.StatusOK, rel)
		}
	}
}
