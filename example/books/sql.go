package books

import (
	"context"
	"github.com/google/uuid"

	"github.com/circleci/ex/db"
	"github.com/circleci/ex/o11y"
)

func queryGetBookByID(ctx context.Context, q db.Querier, id uuid.UUID) (book *Book, err error) {
	ctx, span := db.Span(ctx, "service", "query_get_book_by_id")
	defer o11y.End(span, &err)
	span.AddField("id", id)

	book = &Book{}
	err = q.GetContext(ctx, book, getBookByIDSQL, id)
	if err != nil {
		return nil, err
	}

	return book, nil
}

// language=PostgreSQL
var getBookByIDSQL = `
SELECT
	id,
	name,
	price
FROM
	books
WHERE
	id = $1
LIMIT 1
;`

func queryInsertBook(ctx context.Context, q db.Querier, bookToAdd ToAdd) (id uuid.UUID, err error) {
	ctx, span := db.Span(ctx, "service", "query_insert_book")
	defer o11y.End(span, &err)

	err = q.GetContext(ctx, &id, insertBookSQL,
		bookToAdd.Name,
		bookToAdd.Price,
	)
	if err != nil {
		return uuid.UUID{}, err
	}
	return id, nil
}

// language=PostgreSQL
var insertBookSQL = `
INSERT INTO books (
	id,
	name,
	price
)
VALUES (
	gen_random_uuid()::text,
	$1,
	$2
)
RETURNING 
	id
;`