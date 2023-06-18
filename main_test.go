package main_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"testing"

	"github.com/alextanhongpin/core/test/testutil"
	"github.com/alextanhongpin/go-grpc-test/grpcdump"
	pb "github.com/alextanhongpin/go-grpc-test/helloworld/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) {
	stop := grpcdump.ListenAndServe(func(srv *grpc.Server) {
		pb.RegisterGreeterServiceServer(srv, &server{})
	})
	code := m.Run()
	stop()
	os.Exit(code)
}

func TestStreaming(t *testing.T) {
	ctx := context.Background()
	conn := grpcdump.DialContext(t, ctx, grpc.WithInsecure())

	// Create a new client.
	client := pb.NewGreeterServiceClient(conn)

	header := metadata.New(map[string]string{"x-response-id": "res-123"})
	ctx = metadata.NewOutgoingContext(ctx, header)

	// Create a new recorder.
	ctx, rec := grpcdump.NewRecorder(t, ctx)
	defer func() {
		testutil.DumpJSON(t, rec())
	}()

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
			res, err := stream.Recv()
			if err == io.EOF {
				break
			}

			if err != nil {
				t.Error(err)
			}

			t.Log("Got message", res.GetMessage())
			t.Log(stream.Header())
			t.Log(stream.Trailer())
		}
	}()

	<-done
}

func TestSayHello(t *testing.T) {
	ctx := context.Background()
	conn := grpcdump.DialContext(t, ctx, grpc.WithInsecure())
	client := pb.NewGreeterServiceClient(conn)

	// Send token.
	md := metadata.New(map[string]string{
		"authorization": "sometoken",
	})
	//md := metadata.Pairs("authorization", "sometoken")
	ctx = metadata.NewOutgoingContext(ctx, md)
	ctx, rec := grpcdump.NewRecorder(t, ctx)
	defer func() {
		testutil.DumpJSON(t, rec())
	}()

	// Anything linked to this variable will fetch response headers.
	var header metadata.MD
	resp, err := client.SayHello(ctx, &pb.SayHelloRequest{
		Name: "John Doe",
	}, grpc.Header(&header))
	if err != nil {
		sts, ok := status.FromError(err)
		fmt.Println(ok)
		fmt.Println(sts.Code())
		fmt.Println(sts.Details())
		fmt.Println(sts.Err())
		fmt.Println(sts.Message())
		fmt.Println(grpc.ErrorDesc(err))
		t.Errorf("SayHello failed: %v", err)
	}
	t.Log("response message:", resp.GetMessage())
	t.Log("resp:", resp)
	t.Log("response header:", header)
}

type server struct {
	pb.UnimplementedGreeterServiceServer
}

// SayHello implements helloworld.GreeterServer
func (s *server) SayHello(ctx context.Context, in *pb.SayHelloRequest) (*pb.SayHelloResponse, error) {
	log.Printf("Received: %v", in.GetName())

	// Anything linked to this variable will transmit response headers.
	header := metadata.New(map[string]string{"x-response-id": "res-123"})
	if err := grpc.SendHeader(ctx, header); err != nil {
		return nil, status.Errorf(codes.Internal, "unable to send 'x-response-id' header")
	}
	// Uncomment this to return error.
	return nil, status.Errorf(codes.Internal, "unable to send 'x-response-id' header")
	//return &pb.SayHelloResponse{Message: "Hello " + in.GetName()}, nil
}

func (s *server) ListGreetings(in *pb.ListGreetingsRequest, srv pb.GreeterService_ListGreetingsServer) error {
	ctx := srv.Context()
	header := metadata.New(map[string]string{"x-response-id": "res-123"})
	if err := grpc.SetTrailer(ctx, header); err != nil {
		return status.Errorf(codes.Internal, "unable to send 'trailer' header")
	}
	if err := grpc.SendHeader(ctx, header); err != nil {
		return status.Errorf(codes.Internal, "unable to send 'x-response-id' header")
	}

	for i := 0; i < 3; i++ {
		srv.Send(&pb.ListGreetingsResponse{
			Message: fmt.Sprintf("hi user%s-%d", in.GetName(), i),
		})
	}
	return nil
}
