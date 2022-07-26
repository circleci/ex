package release

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Handler(list *List) func(c *gin.Context) {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var req Requirements
		err := c.BindJSON(&req)
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if err = req.Validate(); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		if req.Version == "" {
			req.Version = list.Latest()
		}

		rel, err := list.Lookup(ctx, req)

		switch {
		case errors.Is(err, ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"status": "Resource not found"})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"status": "An internal error has occurred"})
		default:
			c.JSON(http.StatusOK, rel)
		}
	}
}
