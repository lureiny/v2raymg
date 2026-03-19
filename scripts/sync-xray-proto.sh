#!/bin/bash
#
# sync-xray-proto.sh - Synchronize xray protobuf definitions to upstream mirror
#
# This script downloads and updates xray protobuf definitions from upstream.
# The downloaded files are mirrored to pkg/xrayapi/internalproto/upstream/
#
# Usage:
#   ./scripts/sync-xray-proto.sh [--version VERSION]
#
# Options:
#   --version VERSION   Specify xray version to sync (default: latest)
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
UPSTREAM_DIR="$PROJECT_ROOT/pkg/xrayapi/internalproto/upstream"
VERSION_FILE="$PROJECT_ROOT/pkg/xrayapi/types/VERSION.md"
TEMP_DIR=$(mktemp -d)

# Default version (latest)
XRAY_VERSION="${XRAY_VERSION:-}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --version)
            XRAY_VERSION="$2"
            shift 2
            ;;
        --version=*)
            XRAY_VERSION="${1#*=}"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Get version if not specified
if [ -z "$XRAY_VERSION" ]; then
    echo "Fetching latest xray version..."
    XRAY_VERSION=$(curl -sL https://api.github.com/repos/XTLS/Xray-core/releases/latest 2>/dev/null | grep -oP '"tag_name":\s*"\K[^"]+' | sed 's/v//')
    if [ -z "$XRAY_VERSION" ]; then
        echo "Error: Could not determine xray version"
        exit 1
    fi
fi

echo "Syncing xray proto definitions to version $XRAY_VERSION..."

PROTO_URL="https://raw.githubusercontent.com/XTLS/Xray-core/v${XRAY_VERSION}/"

# Create directory structure
mkdir -p "$UPSTREAM_DIR"

# Array of proto files to download with their directories
PROTO_FILES=(
    # Common - net (needed by many others)
    "common/net/address.proto"
    "common/net/network.proto"
    "common/net/destination.proto"
    "common/net/ip.proto"

    # Common - serial
    "common/serial/typed_message.proto"

    # Common - protocol
    "common/protocol/user.proto"
    "common/protocol/server_spec.proto"
    "common/protocol/billing.proto"

    # App - Proxyman command service (needed for HandlerService)
    "app/proxyman/command/config.proto"
    "app/proxyman/config.proto"
    "app/stats/config.proto"
    "app/logger/config.proto"
    "app/policy/config.proto"

    # Proxy - Inbound configs
    "proxy/vmess/inbound/config.proto"
    "proxy/vless/inbound/config.proto"
    "proxy/trojan/inbound/config.proto"
    "proxy/shadowsocks/inbound/config.proto"
    "proxy/shadowsocks_2022/inbound/config.proto"
    "proxy/socks/inbound/config.proto"
    "proxy/http/inbound/config.proto"
    "proxy/dokodemo/config.proto"
    "proxy/blackhole/config.proto"
    "proxy/freedom/config.proto"

    # Proxy - Outbound configs
    "proxy/vmess/outbound/config.proto"
    "proxy/vless/outbound/config.proto"
    "proxy/trojan/outbound/config.proto"
    "proxybound/config.proto"
    "proxy/shadowsocks_2022/outbound/config.proto"
    "proxy/socks/outbound/config.proto"
    "proxy/http/outbound/config.proto"
    "proxy/dns/config.proto"

    # Transport - Internet
    "transport/internet/config.proto"
    "transport/internet/TCP/config.proto"
    "transport/internet/UDP/config.proto"
    "transport/internet/websocket/config.proto"
    "transport/internet/tls/config.proto"
    "transport/internet/xtls/config.proto"
    "transport/internet/reality/config.proto"
    "transport/internet/grpc/config.proto"
    "transport/internet/http/config.proto"
    "transport/internet/quic/config.proto"
    "transport/internet/splithttp/config.proto"
    "transport/internet/httpupgrade/config.proto"

    # Transport - Mux
    "transport/mux/config.proto"
)

# Download each proto file
for file in "${PROTO_FILES[@]}"; do
    echo "Downloading $file..."
    mkdir -p "$UPSTREAM_DIR/$(dirname $file)"
    if curl -sL "$PROTO_URL$file" -o "$UPSTREAM_DIR/$file" 2>/dev/null; then
        # Check if we got a valid file
        if [ -s "$UPSTREAM_DIR/$file" ]; then
            echo "  OK: $file"
        else
            echo "  SKIP: $file (empty or not found)"
            rm -f "$UPSTREAM_DIR/$file"
        fi
    else
        echo "  SKIP: $file (download failed)"
        rm -f "$UPSTREAM_DIR/$file"
    fi
done

# Remove empty directories
find "$UPSTREAM_DIR" -type d -empty -delete 2>/dev/null || true

# Count downloaded files
DOWNLOADED=$(find "$UPSTREAM_DIR" -name "*.proto" | wc -l)
echo ""
echo "Downloaded $DOWNLOADED proto files to $UPSTREAM_DIR"

# Update version metadata
TIMESTAMP=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
cat > "$VERSION_FILE" << EOF
# Xray Proto Version

This file tracks the xray version used for protobuf definitions.

**Version:** $XRAY_VERSION
**Updated:** $TIMESTAMP

## Generation Info

- **Generator:** scripts/gen-xray-proto.sh
- **Source:** https://github.com/XTLS/Xray-core
- **Proto Mirror Dir:** $UPSTREAM_DIR
- **Generated Output Dir:** pkg/xrayapi/internalproto/gen/
- **Download Time:** $TIMESTAMP

## Current Status

- **Downloaded Proto Files:** $DOWNLOADED
- **Status:** Ready for code generation

## Usage

```bash
# Download proto files
./scripts/sync-xray-proto.sh --version $XRAY_VERSION

# Generate Go code
./scripts/gen-xray-proto.sh --version $XRAY_VERSION

# Verify
go test ./pkg/xrayapi/... -count=1
```

## Notes

- Proto files are mirrored from xray-core repository
- Go code is generated using protoc with grpc plugins
- Generated code replaces minimal stub types
- Legacy JSON serialization still available as fallback
EOF

echo "Version metadata written to $VERSION_FILE"

# Cleanup
rm -rf "$TEMP_DIR"

echo ""
echo "Done!"
echo ""
echo "Current status:"
echo "  - Xray version: $XRAY_VERSION"
echo "  - Proto mirror dir: $UPSTREAM_DIR"
echo "  - Downloaded files: $DOWNLOADED"
echo "  - Version file: $VERSION_FILE"
