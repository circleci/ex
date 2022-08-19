package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	// Host is the grpc host URL typically in this form: "dns:///service.domain.:80"
	Host string
	// ServiceName should be as it is defined in the service's .proto file
	// (e.g. "package.ServiceName").
	ServiceName string
	// Timeout is the maximum duration we allow any call to take. Note if this timeout is
	// hit then the default retries will not happen, it is up to the caller to decide on retry behaviour.
	Timeout time.Duration
}

// Dial wraps up a standard set of dial behaviours that most grpc clients will want to use.
// Using this will default to some grpc calls retrying if the name resolver does not provide a
// service config.
func Dial(ctx context.Context, conf Config) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithDefaultServiceConfig(ServiceConfig(conf.ServiceName)),
	}
	if conf.Timeout > 0 {
		timeoutInterceptor := func(ctx context.Context, method string, req, reply interface{},
			cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

			ctx, cancel := context.WithTimeout(ctx, conf.Timeout)
			defer cancel()
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		opts = append(opts, grpc.WithUnaryInterceptor(timeoutInterceptor))
	}

	return grpc.DialContext(ctx, ProxyProofTarget(conf.Host), opts...)
}
