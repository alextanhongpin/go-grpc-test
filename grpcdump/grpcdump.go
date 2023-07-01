package grpcdump

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/alextanhongpin/core/test/testutil"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

const OriginServer = "server"
const OriginClient = "client"

// NOTE: hackish implementation to extract the dump from the grpc server.
var testIDs = make(map[string]*Dump)

var lis *bufconn.Listener

const addr = "bufnet"

func DialContext(t *testing.T, ctx context.Context, opts ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()

	opts = append([]grpc.DialOption{grpc.WithContextDialer(bufDialer)}, opts...)
	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
	})

	return conn
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

// NewRecorder generates a new unique id for the request, and propagates it
// from the client request to the server.
// The request/response will then be dumped from the server and set to the
// global map with this id.
// The client can then retrieve the dump using the same id.
// The id is automatically cleaned up after the test is done.
func NewRecorder(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	// Generate a new unique id per test.
	id := uuid.New().String()
	t.Cleanup(func() {
		dump := testIDs[id]
		testutil.DumpYAML(t, dump, testutil.FileName(dump.FullMethod))

		b, err := dump.AsText()
		if err != nil {
			t.Fatal(err)
		}
		testutil.DumpText(t, string(b))
		delete(testIDs, id)
	})

	ctx = metadata.AppendToOutgoingContext(ctx, "x-test-id", id)

	return ctx
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

type Message struct {
	Origin  string `json:"origin"` // server or client
	Message any    `json:"message"`
}

type serverStreamWrapper struct {
	grpc.ServerStream
	header   metadata.MD
	messages []Message
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

	s.messages = append(s.messages, Message{
		Origin:  OriginServer,
		Message: m,
	})

	return nil

}

func (s *serverStreamWrapper) RecvMsg(m interface{}) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}

	s.messages = append(s.messages, Message{
		Origin:  OriginClient,
		Message: m,
	})

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

			testIDs[id] = &Dump{
				FullMethod: info.FullMethod,
				Metadata:   header,
				Messages:   w.messages,
				Error:      NewError(err),
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

			testIDs[id] = &Dump{
				FullMethod: info.FullMethod,
				Metadata:   header,
				Messages: []Message{
					{Origin: OriginClient, Message: req},
					{Origin: OriginServer, Message: res},
				},
				Error: NewError(err),
			}

			return res, err
		},
	)
}
