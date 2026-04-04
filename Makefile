.PHONY: build build-full clean proto

# Version info injected at build time.
# Prefer git tag (e.g. v1.0.0); fall back to short commit hash when no tag exists.
VERSION    := $(shell git describe --tags --exact-match 2>/dev/null || git rev-parse --short HEAD)
COMMIT     := $(shell git rev-parse --short HEAD)
BUILD_TIME := $(shell TZ=Asia/Shanghai date +"%Y-%m-%dT%H:%M:%S+08:00")
PKG        := github.com/lureiny/v2raymg/pkg/buildinfo
LDFLAGS    := -s -w \
              -X $(PKG).Version=$(VERSION) \
              -X $(PKG).Commit=$(COMMIT) \
              -X $(PKG).BuildTime=$(BUILD_TIME)

# Default build: slim DNS providers (alidns/cloudflare/dnspod/route53/tencentcloud/namecheap/godaddy)
build:
	go build -ldflags="$(LDFLAGS)" -o bin/v2raymg .

# Full build: all lego DNS providers (larger binary)
build-full:
	go build -ldflags="$(LDFLAGS)" -tags full_dns -o bin/v2raymg-full .

clean:
	rm -rf bin/

proto:
	cd pkg/rpc/proto && \
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		rpc_server.proto
