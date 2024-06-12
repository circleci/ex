package otel

import (
	"fmt"
	"time"

	"github.com/circleci/ex/o11y"
)

func extractAndSendMetrics(mp o11y.MetricsProvider) func([]o11y.Metric, map[string]any) {
	return func(metrics []o11y.Metric, fields map[string]any) {

		for _, m := range metrics {
			tags := extractTagsFromFields(m.TagFields, fields)
			switch m.Type {
			case o11y.MetricTimer:
				val, ok := getField(m.Field, fields)
				if !ok {
					continue
				}
				valFloat, ok := toMilliSecond(val)
				if !ok {
					panic(m.Field + " can not be coerced to milliseconds")
				}
				_ = mp.TimeInMilliseconds(m.Name, valFloat, tags, 1)
			case o11y.MetricCount:
				var valInt int64 = 1
				if m.Field != "" {
					val, ok := getField(m.Field, fields)
					if !ok {
						continue
					}
					valInt, ok = toInt64(val)
					if !ok {
						panic(m.Field + " can not be coerced to int")
					}
				}
				if m.FixedTag != nil {
					tags = append(tags, fmtTag(m.FixedTag.Name, m.FixedTag.Value))
				}
				_ = mp.Count(m.Name, valInt, tags, 1)
			case o11y.MetricGauge:
				val, ok := getField(m.Field, fields)
				if !ok {
					continue
				}
				valFloat, ok := toFloat64(val)
				if !ok {
					panic(m.Field + " can not be coerced to float")
				}
				_ = mp.Gauge(m.Name, valFloat, tags, 1)
			}
		}
	}
}

func extractTagsFromFields(tags []string, fields map[string]any) []string {
	result := make([]string, 0, len(tags))
	for _, name := range tags {
		val, ok := getField(name, fields)
		if ok {
			result = append(result, fmtTag(name, val))
		}
	}
	return result
}

func getField(name string, fields map[string]any) (any, bool) {
	val, ok := fields[name]
	if !ok {
		// Also support the app. prefix, for interop with honeycomb's prefixed fields
		val, ok = fields["app."+name]
	}
	return val, ok
}

func toInt64(val any) (int64, bool) {
	switch v := val.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func toFloat64(val any) (float64, bool) {
	if i, ok := val.(float64); ok {
		return i, true
	}
	if i, ok := toInt64(val); ok {
		return float64(i), true
	}
	return 0, false
}

func toMilliSecond(val any) (float64, bool) {
	if f, ok := toFloat64(val); ok {
		return f, true
	}
	d, ok := val.(time.Duration)
	if !ok {
		p, ok := val.(*time.Duration)
		if !ok {
			return 0, false
		}
		d = *p
	}
	return float64(d.Milliseconds()), true
}

func fmtTag(name string, val any) string {
	return fmt.Sprintf("%s:%v", name, val)
}
