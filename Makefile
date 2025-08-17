v2ray:
	go build -tags v2ray -o bin/v2raymg main.go

xray:
	go build -tags xray -o bin/v2raymg main.go

proto:
	cd server/rpc/proto
	protoc  --plugin=/root/go/bin/protoc-gen-go --plugin=/root/go/bin/protoc-gen-go-grpc --go_out=. --go-grpc_out=. rpc_server.proto
	mv github.com/lureiny/v2raymg/server/proto/* ./
	rm -rf proto/github.com
	cd -

