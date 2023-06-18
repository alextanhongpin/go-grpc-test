package grpcdump

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// NOTE: hackish implementation to extract the dump from the grpc server.
var testIDs = make(map[string]*Dump)

var lis *bufconn.Listener

const addr = "bufnet"

func DialContext(ctx context.Context, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append([]grpc.DialOption{grpc.WithContextDialer(bufDialer)}, opts...)
	return grpc.DialContext(ctx, addr, opts...)
}

func ListenAndServe(fn func(*grpc.Server), opts ...grpc.ServerOption) func() {
	lis = bufconn.Listen(bufSize)

	opts = append([]grpc.ServerOption{
		unaryInterceptor(),
		streamInterceptor(),
	}, opts...)

	srv := grpc.NewServer(opts...)

	fn(srv)
	done := make(chan bool)
	go func() {
		defer close(done)
		if err := srv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			panic(err)
		}
	}()

	return func() {
		srv.Stop()
		lis.Close()
		<-done
	}
}

type Dump struct {
	Addr       string
	FullMethod string
	Header     metadata.MD
	Request    []any
	Response   []any
	Code       codes.Code     `json:"-"`
	Status     *status.Status `json:"-"`
	err        error          `json:"-"`
	Error      string
	IsStream   bool
}

func NewRecorder(t *testing.T, ctx context.Context) (context.Context, func() *Dump) {
	t.Helper()

	// Generate a new unique id per test.
	id := uuid.New().String()
	t.Cleanup(func() {
		delete(testIDs, id)
	})

	ctx = metadata.AppendToOutgoingContext(ctx, "x-test-id", id)

	return ctx, func() *Dump {
		return testIDs[id]
	}
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

type serverStreamWrapper struct {
	grpc.ServerStream
	header   metadata.MD
	sendMsgs []any
	recvMsgs []any
}

func (s *serverStreamWrapper) SendHeader(md metadata.MD) error {
	if err := s.ServerStream.SendHeader(md); err != nil {
		return err
	}
	s.header = md
	return nil
}

func (s *serverStreamWrapper) SendMsg(m interface{}) error {
	if err := s.ServerStream.SendMsg(m); err != nil {
		return err
	}

	s.sendMsgs = append(s.sendMsgs, m)

	return nil

}

func (s *serverStreamWrapper) RecvMsg(m interface{}) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}

	s.recvMsgs = append(s.recvMsgs, m)

	return nil
}

func streamInterceptor() grpc.ServerOption {
	return grpc.StreamInterceptor(
		func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {

			ctx := stream.Context()
			header, _ := metadata.FromIncomingContext(ctx)

			// Extract the test-id from the header.
			// We do not want to log this, so delete it from the
			// existing header.
			id := header.Get("x-test-id")[0]
			header.Delete("x-test-id")

			w := &serverStreamWrapper{ServerStream: stream}
			err := handler(srv, w)
			sts, _ := status.FromError(err)

			testIDs[id] = &Dump{
				Addr:       addrFromContext(ctx),
				FullMethod: info.FullMethod,
				Header:     header,
				Request:    w.recvMsgs,
				Response:   w.sendMsgs,
				Code:       grpc.Code(err),
				Status:     sts,
				err:        err,
				Error:      errMessage(err),
				IsStream:   true,
			}

			return err
		},
	)
}

func unaryInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
			var header metadata.MD
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				header = md
			}

			// Extract the test-id from the header.
			// We do not want to log this, so delete it from the
			// existing header.
			id := header.Get("x-test-id")[0]
			header.Delete("x-test-id")

			res, err := handler(ctx, req)
			sts, _ := status.FromError(err)
			code := grpc.Code(err)

			testIDs[id] = &Dump{
				Addr:       addrFromContext(ctx),
				FullMethod: info.FullMethod,
				Header:     header,
				Request:    []any{req},
				Response:   []any{},
				Code:       code,
				Status:     sts,
				err:        err,
				Error:      errMessage(err),
				IsStream:   false,
			}

			return res, err
		},
	)
}

func addrFromContext(ctx context.Context) string {
	var addr string
	if pr, ok := peer.FromContext(ctx); ok {
		if tcpAddr, ok := pr.Addr.(*net.TCPAddr); ok {
			addr = tcpAddr.IP.String()
		} else {
			addr = pr.Addr.String()
		}
	}
	return addr
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}
