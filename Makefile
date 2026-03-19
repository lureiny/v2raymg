.PHONY: build build-full clean proto

# Default build: slim DNS providers (alidns/cloudflare/dnspod/route53/tencentcloud/namecheap/godaddy)
build:
	go build -ldflags="-s -w" -o bin/v2raymg .

# Full build: all lego DNS providers (larger binary)
build-full:
	go build -ldflags="-s -w" -tags full_dns -o bin/v2raymg-full .

clean:
	rm -rf bin/

proto:
	cd pkg/rpc/proto && \
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		rpc_server.proto
