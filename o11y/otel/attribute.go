package otel

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
)

func attr(key string, val any) attribute.KeyValue {
	switch v := val.(type) {
	case string:
		return attribute.Key(key).String(v)
	case bool:
		return attribute.Key(key).Bool(v)
	case int:
		return attribute.Key(key).Int64(int64(v))
	case int8:
		return attribute.Key(key).Int64(int64(v))
	case int16:
		return attribute.Key(key).Int64(int64(v))
	case int32:
		return attribute.Key(key).Int64(int64(v))
	case int64:
		return attribute.Key(key).Int64(v)
	case float32:
		return attribute.Key(key).Float64(float64(v))
	case float64:
		return attribute.Key(key).Float64(v)
	default:
		if s, ok := val.(fmt.Stringer); ok {
			return attribute.Key(key).String(s.String())
		}
		return attribute.Key(key).String(fmt.Sprintf("%v", v))
	}
}
