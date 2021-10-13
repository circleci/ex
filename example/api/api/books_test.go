package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/circleci/ex/testing/testcontext"
	"github.com/google/uuid"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/example/books"
)

func TestAPI_getBook(t *testing.T) {
	ctx := testcontext.Background()
	fix := startAPI(ctx, t)

	t.Run("Success", func(t *testing.T) {
		var id uuid.UUID
		assert.Assert(t, t.Run("Add book to store", func(t *testing.T) {
			var err error
			id, err = fix.Store.Add(ctx, books.ToAdd{
				Name:  "Wizard of Oz",
				Price: "$6.99",
			})
			assert.Assert(t, err)
		}))

		t.Run("Check book can be found", func(t *testing.T) {
			m := make(map[string]interface{})
			status := fix.Get(t, fmt.Sprintf("/api/books/%s", id), &m)
			assert.Check(t, cmp.Equal(status, http.StatusOK))
			assert.Check(t, cmp.DeepEqual(map[string]interface{}{
				"id":    id.String(),
				"name":  "Wizard of Oz",
				"price": "$6.99",
			}, m))
		})
	})

	t.Run("Not found", func(t *testing.T) {
		status := fix.Get(t, "/api/books/49d42f42-221f-42fc-8f56-f17ac0af6204", nil)
		assert.Check(t, cmp.Equal(status, http.StatusNotFound))
	})
}

func TestAPI_postBook(t *testing.T) {
	ctx := testcontext.Background()
	type response struct {
		ID uuid.UUID `json:"id"`
	}

	t.Run("Success", func(t *testing.T) {
		fix := startAPI(ctx, t)

		var res response
		assert.Assert(t, t.Run("Add book to store", func(t *testing.T) {
			status := fix.Post(t, "/api/books", map[string]interface{}{
				"name":  "Zardoz",
				"price": "$3.99",
			}, &res)
			assert.Check(t, cmp.Equal(status, http.StatusOK))
			assert.Check(t, res.ID != uuid.Nil)
		}))

		t.Run("Check book can be found", func(t *testing.T) {
			book, err := fix.Store.ByID(ctx, res.ID)
			assert.Assert(t, err)
			assert.Check(t, cmp.DeepEqual(&books.Book{
				ID:    res.ID,
				Name:  "Zardoz",
				Price: "$3.99",
			}, book))
		})
	})

	t.Run("Invalid books", func(t *testing.T) {
		t.Run("Missing name", func(t *testing.T) {
			fix := startAPI(ctx, t)

			status := fix.Post(t, "/api/books", map[string]interface{}{
				"price": "$3.99",
			}, nil)
			assert.Check(t, cmp.Equal(status, http.StatusBadRequest))
		})

		t.Run("Missing price", func(t *testing.T) {
			fix := startAPI(ctx, t)

			status := fix.Post(t, "/api/books", map[string]interface{}{
				"name":  "Zardoz",
			}, nil)
			assert.Check(t, cmp.Equal(status, http.StatusBadRequest))
		})
	})
}
