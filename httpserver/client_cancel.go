package httpserver

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
)

func HandleClientCancel(c *gin.Context) {
	c.Next()
	if errors.Is(c.Request.Context().Err(), context.Canceled) {
		c.Status(499)
	}
}
