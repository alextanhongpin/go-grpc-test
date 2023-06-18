package main_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/alextanhongpin/core/test/testutil"
	pb "github.com/alextanhongpin/go-grpc-test/helloworld/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

var testIDs = make(map[uuid.UUID]*grpcDump)

var lis *bufconn.Listener

func TestMain(m *testing.M) {
	lis = bufconn.Listen(bufSize)
	s := grpc.NewServer(
		unaryInterceptor(),
		streamInterceptor(),
	)
	pb.RegisterGreeterServiceServer(s, &server{})
	done := make(chan bool)
	go func() {
		defer close(done)
		if err := s.Serve(lis); !errors.Is(err, grpc.ErrServerStopped) && err != nil {
			fmt.Println("ERRR", err, errors.Is(err, grpc.ErrServerStopped))
			panic(err)
		}
		fmt.Println("CLSOING")
	}()

	code := m.Run()
	fmt.Println("CODE")
	s.Stop()
	lis.Close()
	<-done
	os.Exit(code)
}

func TestStreaming(t *testing.T) {
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithInsecure(),
		withStreamInterceptor(),
	)

	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := pb.NewGreeterServiceClient(conn)

	header := metadata.New(map[string]string{"x-response-id": "res-123"})
	ctx = metadata.NewOutgoingContext(ctx, header)
	stream, err := client.ListGreetings(ctx, &pb.ListGreetingsRequest{
		Name: "john",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan bool)
	go func() {
		for {
			res, err := stream.Recv()
			if err == io.EOF {
				close(done)
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
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(bufDialer),
		grpc.WithInsecure(),
		withUnaryInterceptor(),
	)
	if err != nil {
		t.Fatalf("failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := pb.NewGreeterServiceClient(conn)

	id := uuid.New()
	t.Cleanup(func() {
		delete(testIDs, id)
	})

	// Send token.
	md := metadata.New(map[string]string{
		"authorization": "sometoken",
		"x-test-id":     id.String(),
	})
	//md := metadata.Pairs("authorization", "sometoken")
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

	testutil.DumpJSON(t, testIDs[id])
}

type contextKey string

var testContextKey contextKey = "test_ctx"

func withTestContext(ctx context.Context, t *testing.T) context.Context {
	return context.WithValue(ctx, testContextKey, t)
}
func testContext(ctx context.Context) (t *testing.T) {
	t, ok := ctx.Value(testContextKey).(*testing.T)
	if !ok {
		panic("test context not found")
	}
	return t
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
	return nil, status.Errorf(codes.Internal, "unable to send 'x-response-id' header")

	//return &pb.SayHelloResponse{
	//Message: "Hello " + in.GetName(),
	//}, nil
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
		fmt.Println("WITH STREAM INTERCEPTOR")
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

	fmt.Println("AUTHORIZE")
	fmt.Println("md:", md)
	authHeader, ok := md["authorization"]
	if !ok {
		return status.Errorf(codes.Unauthenticated, "Authorization token is not supplied")
	}

	fmt.Println("got auth header:", authHeader)
	return nil
}

type sendRecvWrapper struct {
	grpc.ServerStream
}

func (s *sendRecvWrapper) SendHeader(md metadata.MD) error {
	if err := s.ServerStream.SendHeader(md); err != nil {
		return err
	}
	fmt.Println("SendHeader", md)
	return nil
}

func (s *sendRecvWrapper) SendMsg(m interface{}) error {
	if err := s.ServerStream.SendMsg(m); err != nil {
		return err
	}

	fmt.Println("SendMsg", m)

	return nil

}

func (s *sendRecvWrapper) RecvMsg(m interface{}) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}

	fmt.Println("RecvMsg", m)

	return nil
}

func streamInterceptor() grpc.ServerOption {
	return grpc.StreamInterceptor(
		func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {

			ctx := stream.Context()
			md, _ := metadata.FromIncomingContext(ctx)
			fmt.Println()
			fmt.Println()
			fmt.Println("SERVER STREAM REQUEST", info.FullMethod, info.IsClientStream, info.IsServerStream)
			fmt.Println(grpc.MethodFromServerStream(stream))
			fmt.Println("MD", md)

			var addr string
			if pr, ok := peer.FromContext(ctx); ok {
				if tcpAddr, ok := pr.Addr.(*net.TCPAddr); ok {
					addr = tcpAddr.IP.String()
				} else {
					addr = pr.Addr.String()
				}
			}

			fmt.Println("SERVER ADDR", addr)
			err := handler(srv, &sendRecvWrapper{ServerStream: stream})

			fmt.Println("STREAM ERROR", err, grpc.Code(err))

			sts, ok := status.FromError(err)
			fmt.Println("sts, ok", sts, ok)
			fmt.Println("code", sts.Code())
			fmt.Println("details:", sts.Details())
			fmt.Println("Err", sts.Err())
			fmt.Println("msg", sts.Message())
			fmt.Println()
			fmt.Println()
			return err
		},
	)
}

type grpcDump struct {
	Addr       string
	FullMethod string
	Header     metadata.MD
	Request    []any
	Response   []any
	Code       codes.Code
	Status     *status.Status
	err        error
	IsStream   bool
}

func unaryInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(
		func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
			var header metadata.MD
			if md, ok := metadata.FromIncomingContext(ctx); ok {
				header = md
			}

			testID := header.Get("x-test-id")[0]
			header.Delete("x-test-id")
			id, err := uuid.Parse(testID)
			if err != nil {
				return nil, errors.New("grpcdump: invalid x-test-id")
			}

			if err := authorize(ctx); err != nil {
				return nil, err
			}

			var addr string
			if pr, ok := peer.FromContext(ctx); ok {
				if tcpAddr, ok := pr.Addr.(*net.TCPAddr); ok {
					addr = tcpAddr.IP.String()
				} else {
					addr = pr.Addr.String()
				}
			}

			res, err := handler(ctx, req)

			code := grpc.Code(err)
			sts, _ := status.FromError(err)

			testIDs[id] = &grpcDump{
				Addr:       addr,
				FullMethod: info.FullMethod,
				Header:     header,
				Request:    []any{req},
				Response:   []any{},
				Code:       code,
				Status:     sts,
				err:        err,
				IsStream:   false,
			}

			return res, err
		},
	)
}
