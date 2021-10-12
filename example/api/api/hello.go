package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (a *API) getHelloWorld(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"hello": "world!"})
}
