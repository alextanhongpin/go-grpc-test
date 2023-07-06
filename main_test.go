package main_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/alextanhongpin/core/test/testutil"
	"github.com/alextanhongpin/go-grpc-test/grpcdump"
	"github.com/alextanhongpin/go-grpc-test/grpctest"
	pb "github.com/alextanhongpin/go-grpc-test/helloworld/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) {
	stop := grpctest.ListenAndServe(func(srv *grpc.Server) {
		pb.RegisterGreeterServiceServer(srv, &server{})
	},
		grpcdump.UnaryInterceptor(),
		grpcdump.StreamInterceptor(),
	)
	code := m.Run()
	stop()
	os.Exit(code)
}

func TestBidrectionalStreaming(t *testing.T) {
	ctx := context.Background()
	conn := grpcDialContext(t, ctx)

	// Create a new client.
	client := pb.NewGreeterServiceClient(conn)

	// Create a new recorder.
	ctx = dumpGRPC(t, ctx)
	ctx = metadata.AppendToOutgoingContext(ctx, "hello-bin", "greeting", "hello", "greeting")

	stream, err := client.Chat(ctx)
	if err != nil {
		t.Error(err)
	}

	done := make(chan bool)

	go func() {
		for {
			_, err := stream.Recv()
			if err == io.EOF {
				close(done)
				return
			}
			if err != nil {
				t.Error(err)
			}
		}
	}()

	for _, msg := range []string{"foo", "bar"} {
		if err := stream.Send(&pb.ChatRequest{
			Message: msg,
		}); err != nil {
			t.Error(err)
		}
	}
	stream.CloseSend()

	<-done
}

func TestStreaming(t *testing.T) {
	ctx := context.Background()
	conn := grpcDialContext(t, ctx)

	// Create a new client.
	client := pb.NewGreeterServiceClient(conn)

	// Create a new recorder.
	ctx = dumpGRPC(t, ctx)

	stream, err := client.ListGreetings(ctx, &pb.ListGreetingsRequest{
		Name: "John Appleseed",
	})
	if err != nil {
		t.Error(err)
	}

	done := make(chan bool)

	go func() {
		defer close(done)
		for {
			_, err := stream.Recv()
			if err == io.EOF {
				break
			}

			if err != nil {
				t.Error(err)
			}

			t.Log(stream.Header())
			t.Log(stream.Trailer())
		}
	}()

	<-done
}

func TestSayHello(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		conn := grpcDialContext(t, ctx)
		client := pb.NewGreeterServiceClient(conn)
		// Send token.
		md := metadata.New(map[string]string{
			"authorization": "xyz",
		})

		//md := metadata.Pairs("authorization", "sometoken")
		ctx = metadata.NewOutgoingContext(ctx, md)
		ctx = dumpGRPC(t, ctx)

		// Anything linked to this variable will fetch response headers.
		_, err := client.SayHello(ctx, &pb.SayHelloRequest{
			Name: "John Doe",
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		conn := grpcDialContext(t, ctx)
		client := pb.NewGreeterServiceClient(conn)
		// Send token.
		md := metadata.New(map[string]string{
			"authorization": "abc",
		})

		//md := metadata.Pairs("authorization", "sometoken")
		ctx = metadata.NewOutgoingContext(ctx, md)
		ctx = dumpGRPC(t, ctx)

		// Anything linked to this variable will fetch response headers.
		_, err := client.SayHello(ctx, &pb.SayHelloRequest{
			Name: "John Doe",
		})
		if err != nil {
			t.Errorf("SayHello failed: %v", err)
		}
	})
}

type server struct {
	pb.UnimplementedGreeterServiceServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.SayHelloRequest) (*pb.SayHelloResponse, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no token present")
	}

	ctx = metadata.NewOutgoingContext(ctx, md)

	token := md.Get("authorization")[0]
	if token != "xyz" {
		return nil, status.Error(codes.Unauthenticated, "no token present")
	}

	ctx = metadata.AppendToOutgoingContext(ctx, "key", "val", "key-bin", string([]byte{96, 102}))
	header, _ := metadata.FromOutgoingContext(ctx)
	if err := grpc.SendHeader(ctx, header); err != nil {
		return nil, status.Errorf(codes.Internal, "unable to send 'key' header")
	}

	trailer := metadata.New(map[string]string{
		"trailer-1": "hello",
	})
	if err := grpc.SetTrailer(ctx, trailer); err != nil {
		return nil, status.Errorf(codes.Internal, "unable to send 'trailer-1' trailer")
	}

	return &pb.SayHelloResponse{Message: "Hello " + in.GetName()}, nil
}

func (s *server) ListGreetings(in *pb.ListGreetingsRequest, srv pb.GreeterService_ListGreetingsServer) error {
	ctx := srv.Context()
	ctx = metadata.AppendToOutgoingContext(ctx, "key", "val", "key-bin", "value")

	header, _ := metadata.FromOutgoingContext(ctx)
	if err := srv.SendHeader(header); err != nil {
		return status.Errorf(codes.Internal, "unable to send 'key' header")
	}

	for i := 0; i < 3; i++ {
		srv.Send(&pb.ListGreetingsResponse{
			Message: fmt.Sprintf("Hi %s-%d", in.GetName(), i),
		})
	}

	trailer := metadata.New(map[string]string{
		"trailer-1": "hello",
	})
	srv.SetTrailer(trailer)

	return nil
}

func (s *server) Chat(stream pb.GreeterService_ChatServer) error {
	ctx := stream.Context()

	ctx = metadata.AppendToOutgoingContext(ctx, "key", "value")
	header, _ := metadata.FromOutgoingContext(ctx)
	if err := stream.SendHeader(header); err != nil {
		return status.Errorf(codes.Internal, "unable to send 'key' header")
	}

	trailer := metadata.New(map[string]string{
		"trailer-1": "hello",
	})
	stream.SetTrailer(trailer)

	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if err := stream.Send(&pb.ChatResponse{
			Message: "REPLY: " + in.GetMessage(),
		}); err != nil {
			return err
		}
	}
}

func dumpGRPCUnary(t *testing.T, ctx context.Context) (context.Context, []grpc.CallOption) {
	t.Helper()
	ctx, flush := grpcdump.NewRecorder(ctx)

	var trailer, header metadata.MD
	t.Cleanup(func() {
		dump := flush()

		testutil.DumpYAML(t, dump, testutil.FileName(dump.FullMethod))

		b, err := dump.AsText()
		if err != nil {
			t.Fatal(err)
		}

		testutil.DumpText(t, string(b))
	})

	return ctx, []grpc.CallOption{grpc.Header(&header), grpc.Trailer(&trailer)}
}

func dumpGRPC(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	ctx, flush := grpcdump.NewRecorder(ctx)

	t.Cleanup(func() {
		dump := flush()
		fmt.Println("DUMP", dump)

		testutil.DumpYAML(t, dump, testutil.FileName(dump.FullMethod))

		b, err := dump.AsText()
		if err != nil {
			t.Fatal(err)
		}

		testutil.DumpText(t, string(b))
	})

	return ctx
}

func grpcDialContext(t *testing.T, ctx context.Context) *grpc.ClientConn {
	conn, err := grpctest.DialContext(ctx,
		grpc.WithInsecure(),
		grpcdump.WithUnaryInterceptor(),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		conn.Close()
	})

	return conn
}
