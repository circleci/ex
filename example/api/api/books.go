package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/circleci/ex/example/books"
	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

func (a *API) getBook(c *gin.Context) {
	type response struct {
		ID    uuid.UUID `json:"id"`
		Name  string    `json:"name"`
		Price string    `json:"price"`
	}

	ctx := c.Request.Context()

	idString := c.Param("id")

	id, err := uuid.Parse(idString)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{})
		return
	}

	book, err := a.store.ByID(ctx, id)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{})
		return
	}

	c.JSON(http.StatusOK, response(*book))
}

func (a *API) postBook(c *gin.Context) {
	type request struct {
		Name  string `json:"name" validate:"required"`
		Price string `db:"price" validate:"required"`
	}
	type response struct {
		ID uuid.UUID `json:"id"`
	}

	ctx := c.Request.Context()

	var req request
	err := c.BindJSON(&req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{})
		return
	}

	err = validate.Struct(req)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{})
		return
	}

	id, err := a.store.Add(ctx, books.ToAdd(req))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{})
		return
	}

	c.JSON(http.StatusOK, response{
		ID: id,
	})
}
