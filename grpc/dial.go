package grpc

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Dial(ctx context.Context, host, serviceName string) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		ctx,
		ProxyProofTarget(host),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithDefaultServiceConfig(ServiceConfig(serviceName)),
	)
}
