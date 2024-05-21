package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/circleci/ex/o11y"
)

type Config struct {
	// Host is the grpc host URL typically in this form: "dns:///service.domain.:80" if load balancing is desired
	// otherwise the raw host should be provided without the dns:/// prefix.
	// If the dns:// load balancing scheme is indicated - authority is not accepted and should be empty
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
func Dial(conf Config) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(ServiceConfig(conf.ServiceName)),
	}

	o11yInterceptor := func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		o11y.AddField(ctx, "grpc_service", conf.ServiceName)
		o11y.AddField(ctx, "grpc_method", method)
		err := invoker(ctx, method, req, reply, cc, opts...)
		o11y.AddField(ctx, "grpc_error", err)
		return err
	}

	if conf.Timeout > 0 {
		timeoutInterceptor := func(ctx context.Context, method string, req, reply interface{},
			cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

			ctx, cancel := context.WithTimeout(ctx, conf.Timeout)
			defer cancel()
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		opts = append(opts, grpc.WithChainUnaryInterceptor(o11yInterceptor, timeoutInterceptor))
	} else {
		opts = append(opts, grpc.WithUnaryInterceptor(o11yInterceptor))
	}

	return grpc.NewClient(ProxyProofTarget(conf.Host), opts...)
}
