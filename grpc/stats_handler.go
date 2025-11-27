package grpc

import (
	"context"
	"net"
	"strconv"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"

	"github.com/circleci/ex/o11y"
)

type gRPCContextKey struct{}

type gRPCContext struct {
	messagesReceived int64
	messagesSent     int64
	span             o11y.Span
	metricAttrs      []string
	record           bool
}

type serverHandler struct {
	*baseHandler
	provider o11y.Provider
}

// NewServerHandler creates a stats.Handler for a gRPC server.
func NewServerHandler(ctx context.Context) stats.Handler {
	h := &serverHandler{
		provider:    o11y.FromContext(ctx),
		baseHandler: newBaseHandler("server"),
	}

	return h
}

func (h *serverHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return o11y.WithProvider(ctx, h.provider)
}

func (h *serverHandler) HandleConn(context.Context, stats.ConnStats) {
}

func (h *serverHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	ctx = extract(ctx, h.propagators)

	name, attrs := parseFullMethod(info.FullMethodName)
	attrs = append(attrs, string(semconv.RPCSystemGRPC.Key), semconv.RPCSystemGRPC.Value.AsString())
	ctx, span := o11y.StartSpan(ctx, name, o11y.WithSpanKind(o11y.SpanKindServer))
	for i := 0; i < len(attrs); i += 2 {
		span.AddRawField(attrs[i], attrs[i+1])
	}
	span.AddRawField("meta.kind", "grpc_server") // TODO - remove

	gctx := gRPCContext{
		span:        span,
		metricAttrs: attrs,
		record:      true,
	}
	return context.WithValue(ctx, gRPCContextKey{}, &gctx)
}

func (h *serverHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	h.handleRPC(ctx, rs, true)
}

type clientHandler struct {
	*baseHandler
}

func newClientHandler() stats.Handler {
	h := &clientHandler{
		baseHandler: newBaseHandler("client"),
	}

	return h
}

func (h *clientHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	name, attrs := parseFullMethod(info.FullMethodName)
	attrs = append(attrs, string(semconv.RPCSystemGRPC.Key), semconv.RPCSystemGRPC.Value.AsString())
	ctx, span := o11y.StartSpan(ctx, name, o11y.WithSpanKind(o11y.SpanKindClient))
	for i := 0; i < len(attrs); i += 2 {
		span.AddRawField(attrs[i], attrs[i+1])
	}
	span.AddRawField("meta.kind", "grpc_client") // TODO - remove

	gctx := gRPCContext{
		span:        span,
		metricAttrs: attrs,
		record:      true,
	}

	return inject(context.WithValue(ctx, gRPCContextKey{}, &gctx), h.propagators)
}

func (h *clientHandler) HandleRPC(ctx context.Context, rs stats.RPCStats) {
	h.handleRPC(ctx, rs, false)
}

func (h *clientHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (h *clientHandler) HandleConn(context.Context, stats.ConnStats) {
}

type baseHandler struct {
	propagators propagation.TextMapPropagator
	role        string
}

func newBaseHandler(role string) *baseHandler {
	return &baseHandler{
		propagators: otel.GetTextMapPropagator(),
		role:        role,
	}
}

//nolint:gocyclo,funlen
func (h *baseHandler) handleRPC(ctx context.Context, rs stats.RPCStats, isServer bool) {
	provider := o11y.FromContext(ctx)
	span := provider.GetSpan(ctx)
	metricsProvider := provider.MetricsProvider()

	var metricAttrs []string

	gctx, _ := ctx.Value(gRPCContextKey{}).(*gRPCContext)
	if gctx != nil {
		if !gctx.record {
			return
		}
		metricAttrs = make([]string, 0, len(gctx.metricAttrs))
		metricAttrs = append(metricAttrs, gctx.metricAttrs...)
	}

	switch rs := rs.(type) {
	case *stats.Begin:
	case *stats.InPayload:
		if gctx != nil {
			atomic.AddInt64(&gctx.messagesReceived, 1)
			_ = metricsProvider.Count("rpc."+h.role+".request.size", int64(rs.Length), metricAttrs, 1)
		}
	case *stats.OutPayload:
		if gctx != nil {
			atomic.AddInt64(&gctx.messagesSent, 1)
			_ = metricsProvider.Count("rpc."+h.role+".response.size", int64(rs.Length), metricAttrs, 1)
		}

	case *stats.OutTrailer:
	case *stats.OutHeader:
		if p, ok := peer.FromContext(ctx); ok {
			addPeerAttr(span, p.Addr.String())
		}
	case *stats.End:
		var (
			err        error
			statusCode = status.Code(rs.Error)
		)
		// Prefer the client side context error, so we can differentiate between that and the server
		// responding to us with deadline exceeded or canceled
		switch {
		case ctx.Err() != nil:
			err = &Error{
				server: isServer,
				err:    ctx.Err(),
			}
		case rs.Error != nil:
			err = &Error{
				server: isServer,
				err:    rs.Error,
			}
		}

		metricAttrs = append(metricAttrs, string(semconv.RPCGRPCStatusCodeKey), statusCode.String())

		// For the span the status code should be the int, the description will appear in error.type
		span.AddRawField(string(semconv.RPCGRPCStatusCodeKey), int(statusCode))

		elapsedTime := rs.EndTime.Sub(rs.BeginTime)
		_ = metricsProvider.TimeInMilliseconds("rpc."+h.role+".duration", float64(elapsedTime.Milliseconds()), metricAttrs, 1)
		if gctx != nil {
			o11y.End(gctx.span, &err)
			_ = metricsProvider.Count("rpc."+h.role+".requests_per_rpc",
				atomic.LoadInt64(&gctx.messagesReceived),
				metricAttrs,
				1,
			)
			_ = metricsProvider.Count("rpc."+h.role+".responses_per_rpc",
				atomic.LoadInt64(&gctx.messagesSent),
				metricAttrs,
				1,
			)
		}

	default:
		return
	}
}

func addPeerAttr(span o11y.Span, addr string) {
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}

	if host == "" {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		return
	}

	if ip := net.ParseIP(host); ip != nil {
		port = 0
	}
	span.AddRawField(string(semconv.NetworkPeerAddressKey), host)
	span.AddRawField(string(semconv.NetworkPeerPortKey), port)
}
