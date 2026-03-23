# Proto Generation Guide

This document describes how to generate Go protobuf code from xray-core proto definitions.

## Quick Start

```bash
# 1. Check dependencies
./scripts/gen-xray-proto.sh --check

# 2. Download proto files from xray-core
./scripts/sync-xray-proto.sh --version 26.2.6

# 3. Generate Go code
./scripts/gen-xray-proto.sh --version 26.2.6

# 4. Verify
go test ./pkg/xrayapi/... -count=1
go test ./pkg/proxyrefactor/... -count=1
go build ./cmd/xraydemo/...
```

## Prerequisites

Install the following tools:

### protoc (Protocol Buffer Compiler)

**Ubuntu/Debian:**
```bash
sudo apt install protobuf-compiler
```

**macOS:**
```bash
brew install protobuf
```

**Manual Install:**
```bash
# Download from https://github.com/protocolbuffers/protobuf/releases
curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v27.1/protoc-27.1-linux-x86_64.zip
unzip protoc-27.1-linux-x86_64.zip -d /path/to/install
export PATH=$PATH:/path/to/install/bin
```

### Go Protoc Plugins

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Make sure `$GOPATH/bin` is in your PATH.

## Workflow

### 1. Sync Proto Files

Download proto definitions from xray-core repository:

```bash
./scripts/sync-xray-proto.sh --version 26.2.6
```

This downloads:
- `common/protocol/user.proto`
- `app/proxyman/command/config.proto`
- `proxy/*/inbound/config.proto`

Output: `pkg/xrayapi/types/proto/`

### 2. Generate Go Code

Generate Go protobuf code:

```bash
./scripts/gen-xray-proto.sh --version 26.2.6
```

Options:
- `--force`: Regenerate even if files exist
- `--check`: Only check dependencies
- `--dry-run`: Show what would be done

Output: `pkg/xrayapi/internalproto/gen/`

### 3. Verify

```bash
go test ./pkg/xrayapi/... -count=1
go test ./pkg/proxyrefactor/... -count=1
```

## Known Limitations

### Proto Dependency Issues

Some xray proto files have complex dependencies:
- Trojan: Uses `server/config.proto` not `inbound/config.proto`
- Shadowsocks 2022: Different proto structure
- Full generation requires downloading all common protos

Current working generation:
- VMess inbound config
- VLESS inbound config

### Integration Status

Generated proto code is available but not yet integrated with the gRPC client. Current implementation uses JSON serialization for backward compatibility.

## Troubleshooting

### "protoc not found"

Install protoc - see Prerequisites section above.

### "common/protocol/user.proto: File not found"

Run sync script first to download dependencies:
```bash
./scripts/sync-xray-proto.sh --version 26.2.6
```

### Generation fails silently

Check that proto source files exist:
```bash
ls -la pkg/xrayapi/types/proto/proxy/*/inbound/
```

## File Structure

```
pkg/xrayapi/
├── types/
│   ├── VERSION.md           # Version tracking
│   └── proto/              # Downloaded proto files
│       ├── common/
│       ├── proxy/
│       │   ├── vmess/inbound/
│       │   └── vless/inbound/
│       └── app/
└── internalproto/
    └── gen/                 # Generated Go code
        └── proxy/
            ├── vmess/inbound/config.pb.go
            └── vless/inbound/config.pb.go
```
