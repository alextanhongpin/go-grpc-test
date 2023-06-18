# GRPC

## Stream Interceptor

```go
func withStreamInterceptor() grpc.DialOption {
	return grpc.WithStreamInterceptor(grpc.StreamClientInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		fmt.Println("desc:", desc)
		fmt.Println("cc:", cc)
		fmt.Println("method:", method)
		return streamer(ctx, desc, cc, method, opts...)
	}))
}
```

Usage:
```go
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithInsecure(),
		withStreamInterceptor(),
	)
```

## Unary Interceptor


```go
func withUnaryInterceptor() grpc.DialOption {
	return grpc.WithUnaryInterceptor(grpc.UnaryClientInterceptor(
		func(ctx context.Context, method string, req, res interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
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
```

Usage:
```go
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithInsecure(),
		withUnaryInterceptor(),
	)
```

## Context

From client:
```go
	header := metadata.Pairs("authorization", "sometoken")
	// Or
	header := metadata.New(map[string]string{"x-response-id": "res-123"})
	// This will overwrite all key-value.
	ctx = metadata.NewOutgoingContext(ctx, header)

	// To append new key-value:
	ctx = metadata.AppendToOutgoingContext(ctx, "hello", "world")
```

From server:

```go
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
```
