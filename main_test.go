package main_test

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"testing"
	"time"

	pb "github.com/alextanhongpin/go-grpc-test/helloworld/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

var lis *bufconn.Listener

func TestMain(m *testing.M) {
	lis = bufconn.Listen(bufSize)
	s := grpc.NewServer(
		unaryInterceptor(),
	)
	pb.RegisterGreeterServiceServer(s, &server{})
	go func() {
		if err := s.Serve(lis); err != nil {
			panic(err)
		}
	}()

	code := m.Run()
	lis.Close()
	os.Exit(code)
}

func TestSayHello(t *testing.T) {
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithInsecure(),
		withUnaryInterceptor(),
		withStreamInterceptor(),
	)
	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := pb.NewGreeterServiceClient(conn)

	// Send token.
	// md := metadata.New(map[string]string{})
	md := metadata.Pairs("authorization", "sometoken")
	ctx = metadata.NewOutgoingContext(ctx, md)

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
		t.Fatalf("SayHello failed: %v", err)
	}
	t.Log("response message:", resp.GetMessage())
	t.Log("resp:", resp)
	t.Log("response header:", header)
}

type server struct {
	pb.UnimplementedGreeterServiceServer
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
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
	//return nil, status.Errorf(codes.Internal, "unable to send 'x-response-id' header")

	return &pb.SayHelloResponse{
		Message: "Hello " + in.GetName(),
	}, nil
}

func withUnaryInterceptor() grpc.DialOption {
	return grpc.WithUnaryInterceptor(grpc.UnaryClientInterceptor(
		func(ctx context.Context, method string, req, res interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			fmt.Println("UNARY INTERCEPTOR")
			fmt.Println("method:", method)
			fmt.Println("req:", req)
			fmt.Println("res:", res)
			fmt.Println("state:", cc.GetState())
			start := time.Now()
			// This must be called.
			err := invoker(ctx, method, req, res, cc, opts...)
			fmt.Println("invocation took:", time.Since(start))
			return err
		},
	))
}

func withStreamInterceptor() grpc.DialOption {
	return grpc.WithStreamInterceptor(grpc.StreamClientInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		fmt.Println("STREAM INTERCEPTOR")
		fmt.Println("desc:", desc)
		fmt.Println("cc:", cc)
		fmt.Println("method:", method)
		return streamer(ctx, desc, cc, method, opts...)
	}))
}

func authorize(ctx context.Context) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.InvalidArgument, "Retrieving metadata is failed")
	}

	fmt.Println("md:", md)
	authHeader, ok := md["authorization"]
	if !ok {
		return status.Errorf(codes.Unauthenticated, "Authorization token is not supplied")
	}

	fmt.Println("got auth header:", authHeader)
	return nil
}

func unaryInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
			defer func(start time.Time) {
				fmt.Println("TOOK:", time.Since(start))
			}(time.Now())
			fmt.Println("SERVER")
			fmt.Println("req:", req)
			fmt.Printf("info: %#v\n", info)
			fmt.Printf("info.FullMethod: %#v\n", info.FullMethod)
			fmt.Printf("info.Server: %#v\n", info.Server)
			if err := authorize(ctx); err != nil {
				return nil, err
			}

			h, err := handler(ctx, req)
			fmt.Println("res", h)
			return h, err
		},
	)
}
