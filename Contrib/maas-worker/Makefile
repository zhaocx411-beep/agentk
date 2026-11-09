.PHONY: build generate test

build:
	go build -o bin/maas-gateway ./cmd/maas-gateway
	go build -o bin/maas-worker ./cmd/maas-worker

generate:
	protoc -I proto \
		--go_out=gen/infer --go_opt=paths=source_relative \
		--go-grpc_out=gen/infer --go-grpc_opt=paths=source_relative \
		proto/common.proto proto/infer.proto

test:
	go test ./...
