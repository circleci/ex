package otel

import (
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel/attribute"
)

func attr(key string, vi any) attribute.KeyValue {
	val := deref(vi)
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
			if isNil(s) {
				return attribute.Key(key).String("")
			}
			return attribute.Key(key).String(s.String())
		}
		if isNil(val) {
			return attribute.Key(key).String("")
		}
		return attribute.Key(key).String(fmt.Sprintf("%v", v))
	}
}

func deref(i any) (o any) {
	if i == nil {
		return i
	}
	v := reflect.ValueOf(i)
	if v.Kind() == reflect.Ptr {
		if v.IsZero() || !v.IsValid() {
			return i
		}
		p := v.Elem()
		return p.Interface()
	}
	return i
}

func isNil(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case
		reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Ptr,
		reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
