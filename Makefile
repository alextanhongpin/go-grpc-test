install:
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2


compile:
	@protoc \
		--go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    helloworld/v1/helloworld.proto



server:
	@go run greeter_server/main.go


client:
	@go run greeter_client/main.go
