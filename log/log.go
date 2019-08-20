package log

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/circleci/distributor/o11y"
)

type logKey struct{}

type client struct{}

func New() *client {
	return &client{}
}

type trace struct {
	id     uuid.UUID
	fields map[string]interface{}
}

type span struct {
	name     string
	trace    *trace
	id       uuid.UUID
	parentID uuid.UUID
	started  time.Time
	fields   map[string]interface{}
}

func (s *span) AddField(key string, val interface{}) {
	s.fields[key] = val
}

func (s *span) Send() {
	st := struct {
		Name     string
		ID       uuid.UUID              `json:"id"`
		TraceID  uuid.UUID              `json:"trace_id"`
		ParentID uuid.UUID              `json:"parent_id"`
		Started  time.Time              `json:"started"`
		Duration time.Duration          `json:"duration"`
		Fields   map[string]interface{} `json:"fields"`
	}{
		Name:     s.name,
		ID:       s.id,
		TraceID:  s.trace.id,
		ParentID: s.parentID,
		Started:  s.started,
		Fields:   map[string]interface{}{},
		Duration: time.Since(s.started),
	}
	for k, v := range s.trace.fields {
		st.Fields[k] = v
	}
	for k, v := range s.fields {
		st.Fields[k] = v
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	_ = e.Encode(st) // who cares if we fail
}

func (c *client) GetSpanFromContext(ctx context.Context) o11y.Span {
	return c.getSpan(ctx)
}

func (c *client) getSpan(ctx context.Context) *span {
	if span, ok := ctx.Value(logKey{}).(*span); ok {
		return span
	}
	return nil
}

func (c *client) StartSpan(ctx context.Context, name string) (context.Context, o11y.Span) {
	parent := c.getSpan(ctx)
	span := &span{
		name:    name,
		id:      uuid.New(),
		started: time.Now(),
		fields:  map[string]interface{}{},
	}
	if parent == nil {
		span.trace = &trace{
			id:     uuid.New(),
			fields: map[string]interface{}{},
		}
	} else {
		span.parentID = parent.id
		span.trace = parent.trace
	}
	return context.WithValue(ctx, logKey{}, span), span
}

func (c *client) AddFieldToTrace(ctx context.Context, key string, val interface{}) {
	span := c.getSpan(ctx)
	if span == nil {
		return
	}
	span.trace.fields[key] = val
}

func (c *client) Flush(ctx context.Context) {}

func (c *client) Close(_ context.Context) {}
