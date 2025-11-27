package grpc

import (
	"strings"

	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func parseFullMethod(fullMethod string) (string, []string) {
	if !strings.HasPrefix(fullMethod, "/") {
		// Invalid format, does not follow `/package.service/method`.
		return fullMethod, nil
	}
	name := fullMethod[1:]
	pos := strings.LastIndex(name, "/")
	if pos < 0 {
		// Invalid format, does not follow `/package.service/method`.
		return name, nil
	}
	service, method := name[:pos], name[pos+1:]

	attrs := make([]string, 0, 4)
	if service != "" {
		attrs = append(attrs, string(semconv.RPCServiceKey), service)
	}
	if method != "" {
		attrs = append(attrs, string(semconv.RPCMethodKey), method)
	}
	return name, attrs
}
