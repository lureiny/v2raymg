#!/bin/bash
#
# fix-proto-paths.sh - Fix proto import paths and go_package for local generation
#
# This script modifies proto files to use local paths instead of xray-core paths.
#
# Usage:
#   ./scripts/fix-proto-paths.sh
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
UPSTREAM_DIR="$PROJECT_ROOT/pkg/xrayapi/internalproto/upstream"

echo "Fixing proto paths in $UPSTREAM_DIR..."

# Fix go_package options - map xray-core to local paths
# Pattern: github.com/xtls/xray-core/... -> github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/...

find "$UPSTREAM_DIR" -name "*.proto" -type f | while read proto_file; do
    # Fix go_package option
    sed -i 's|github.com/xtls/xray-core/|github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen/|g' "$proto_file"

    echo "Fixed: $proto_file"
done

echo ""
echo "Done! Proto paths have been fixed."

# Show sample of fixed files
echo ""
echo "Sample of fixed go_package options:"
find "$UPSTREAM_DIR" -name "*.proto" -type f -exec grep "option go_package" {} \; | head -5
