package db

import (
	"context"
	"fmt"

	"github.com/circleci/ex/o11y"
)

// Recommendations for naming here are taken from
// https://github.com/open-telemetry/opentelemetry-specification/blob/7ae3d066c95c716ef3086228ef955d84ba03ac88/specification/trace/semantic_conventions/database.md

func Span(ctx context.Context, entity, queryName string) (context.Context, o11y.Span) {
	ctx, span := o11y.StartSpan(ctx, fmt.Sprintf("db: %s.%s", entity, queryName))
	span.RecordMetric(o11y.Timing("db.query", "db.entity", "db.query_name", "result"))
	span.AddRawField("db.system", "postgresql")
	span.AddRawField("db.entity", entity)
	span.AddRawField("db.query_name", queryName)
	return ctx, span
}
