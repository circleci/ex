package mongoex

import (
	"context"
	"fmt"

	"github.com/circleci/ex/o11y"
)

// Span provides a o11y span to ensure our database queries are reported consistently.
func Span(ctx context.Context, entity, queryName string) (context.Context, o11y.Span) {
	ctx, span := o11y.StartSpan(ctx, fmt.Sprintf("db: %s.%s", entity, queryName))
	span.RecordMetric(o11y.Timing("db.query", "db.entity", "db.query_name", "result"))
	span.AddRawField("db.system", "mongo")
	span.AddRawField("db.entity", entity)
	span.AddRawField("db.query_name", queryName)
	return ctx, span
}
