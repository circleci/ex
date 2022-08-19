package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"

	"github.com/circleci/ex/grpc/internal/testgrpc"
	"github.com/circleci/ex/o11y"
	"github.com/circleci/ex/testing/testcontext"
)

func TestDial(t *testing.T) {
	srvCtx := testcontext.Background()
	srv, cleanup, err := startGRPCServer(srvCtx, "localhost:0")
	assert.NilError(t, err)
	t.Cleanup(cleanup)

	t.Run("with-timeout", func(t *testing.T) {
		ctx := testcontext.Background()
		con, err := Dial(ctx, Config{
			Host:        srv.addr,
			ServiceName: "testgrpc.PingPong",
			Timeout:     time.Millisecond * 50,
		})
		assert.NilError(t, err)
		cl := testgrpc.NewPingPongClient(con)

		t.Run("good", func(t *testing.T) {
			r, err := cl.Ping(ctx, &testgrpc.PingRequest{Caller: "me"})
			assert.NilError(t, err)
			assert.Check(t, cmp.Equal(r.Message, "pong me"))
		})

		t.Run("times-out", func(t *testing.T) {
			srv.reset()
			r, err := cl.Ping(ctx, &testgrpc.PingRequest{Caller: "time me out"})
			s, ok := status.FromError(err)
			assert.Check(t, ok)
			assert.Check(t, cmp.Equal(s.Code(), codes.DeadlineExceeded))
			assert.Check(t, r == nil)

			assert.Check(t, cmp.Equal(srv.callCount(), 1))
		})
	})

	t.Run("no-timeout", func(t *testing.T) {
		ctx := testcontext.Background()
		con, err := Dial(ctx, Config{
			Host:        srv.addr,
			ServiceName: "testgrpc.PingPong",
		})
		assert.NilError(t, err)
		cl := testgrpc.NewPingPongClient(con)

		t.Run("long-call-ok", func(t *testing.T) {
			srv.reset()
			r, err := cl.Ping(ctx, &testgrpc.PingRequest{Caller: "time me out"})
			assert.NilError(t, err)
			assert.Check(t, cmp.Equal(r.Message, "pong time me out"))

			// Note that when we hit the unary interception timeout the retry mechanism is not triggered
			assert.Check(t, cmp.Equal(srv.callCount(), 1))
		})

		t.Run("unavailable-retries", func(t *testing.T) {
			srv.reset()
			r, err := cl.Ping(ctx, &testgrpc.PingRequest{Caller: "unavailable"})
			s, ok := status.FromError(err)
			assert.Check(t, ok)
			assert.Check(t, cmp.Equal(s.Code(), codes.Unavailable))
			assert.Check(t, r == nil)

			assert.Check(t, cmp.Equal(srv.callCount(), 3))
		})

		t.Run("deadline-retries", func(t *testing.T) {
			srv.reset()
			r, err := cl.Ping(ctx, &testgrpc.PingRequest{Caller: "deadline"})
			s, ok := status.FromError(err)
			assert.Check(t, ok)
			assert.Check(t, cmp.Equal(s.Code(), codes.DeadlineExceeded))
			assert.Check(t, r == nil)

			// When we do not hit the unary interception timeout but the server method responds with
			// a deadline exceeded error we do retry
			assert.Check(t, cmp.Equal(srv.callCount(), 3))
		})
	})
}

func startGRPCServer(ctx context.Context, host string) (srv *pingPongServer, stop func(), err error) {
	lis, err := net.Listen("tcp", host)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen: %w", err)
	}

	server := grpc.NewServer(grpc.ConnectionTimeout(5 * time.Second))
	srv = &pingPongServer{}
	testgrpc.RegisterPingPongServer(server, srv)

	go func() {
		if err := server.Serve(lis); err != nil {
			o11y.Log(ctx, "ERROR - failed to serve", o11y.Field("error", err))
		}
	}()

	srv.addr = lis.Addr().String()

	return srv, server.Stop, nil
}

type pingPongServer struct {
	testgrpc.UnsafePingPongServer

	addr  string
	mu    sync.RWMutex
	calls int
}

func (s *pingPongServer) Ping(ctx context.Context, req *testgrpc.PingRequest) (*testgrpc.PingReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls++

	switch req.Caller {
	case "time me out":
		time.Sleep(time.Millisecond * 100)
	case "unavailable":
		return nil, status.Error(codes.Unavailable, "busy")
	case "deadline":
		return nil, context.DeadlineExceeded // this will be mapped to the grpc status error
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &testgrpc.PingReply{Message: "pong " + req.Caller}, nil
}

func (s *pingPongServer) callCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.calls
}

func (s *pingPongServer) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.calls = 0
}
