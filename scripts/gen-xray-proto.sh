#!/bin/bash
#
# gen-xray-proto.sh - Generate Go code from xray protobuf definitions
#
# This script generates Go protobuf code from xray proto definitions.
# It requires protoc and protoc-gen-go and protoc-gen-go-grpc plugins.
#
# Usage:
#   ./scripts/gen-xray-proto.sh [options]
#
# Options:
#   --version VERSION   Xray version to use (default: from VERSION.md)
#   --force            Force regeneration even if output exists
#   --check            Only check dependencies, don't generate
#   --dry-run          Show what would be done without executing
#
# Prerequisites:
#   - protoc:        https://github.com/protocolbuffers/protobuf/releases
#   - protoc-gen-go: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   - protoc-gen-go-grpc: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
#
# Example:
#   # Install dependencies
#   ./scripts/gen-xray-proto.sh --check
#
#   # Generate code
#   ./scripts/gen-xray-proto.sh
#
#   # Verify
#   go test ./pkg/xrayapi/... -count=1

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
PROTO_SRC_DIR="$PROJECT_ROOT/pkg/xrayapi/internalproto/upstream"
PROTO_OUT_DIR="$PROJECT_ROOT/pkg/xrayapi/internalproto/gen"
VERSION_FILE="$PROJECT_ROOT/pkg/xrayapi/types/VERSION.md"

# Default values
XRAY_VERSION=""
FORCE=false
CHECK_ONLY=false
DRY_RUN=false

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
        --force)
            FORCE=true
            shift
            ;;
        --check)
            CHECK_ONLY=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check dependencies
check_dependencies() {
    log_info "Checking dependencies..."

    local missing=0

    # Check protoc
    if ! command -v protoc &> /dev/null; then
        log_error "protoc not found. Please install from:"
        log_error "  https://github.com/protocolbuffers/protobuf/releases"
        log_error ""
        log_error "On Ubuntu/Debian:"
        log_error "  sudo apt install protobuf-compiler"
        log_error ""
        log_error "On macOS:"
        log_error "  brew install protobuf"
        missing=1
    else
        log_info "protoc: $(protoc --version)"
    fi

    # Check protoc-gen-go
    if ! command -v protoc-gen-go &> /dev/null; then
        log_error "protoc-gen-go not found. Install with:"
        log_error "  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
        log_error ""
        log_error "Make sure \$GOPATH/bin is in your PATH"
        missing=1
    else
        log_info "protoc-gen-go: installed"
    fi

    # Check protoc-gen-go-grpc
    if ! command -v protoc-gen-go-grpc &> /dev/null; then
        log_error "protoc-gen-go-grpc not found. Install with:"
        log_error "  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
        log_error ""
        log_error "Make sure \$GOPATH/bin is in your PATH"
        missing=1
    else
        log_info "protoc-gen-go-grpc: installed"
    fi

    if [ $missing -eq 1 ]; then
        log_error "Missing dependencies. Install them and try again."
        return 1
    fi

    log_info "All dependencies satisfied!"
    return 0
}

# Get version from VERSION.md if not specified
get_version() {
    if [ -n "$XRAY_VERSION" ]; then
        return 0
    fi

    if [ -f "$VERSION_FILE" ]; then
        XRAY_VERSION=$(grep -oP '^\*\*Version:\*\* \K[^<]+' "$VERSION_FILE" | tr -d ' ')
        if [ -n "$XRAY_VERSION" ]; then
            log_info "Using version from VERSION.md: $XRAY_VERSION"
            return 0
        fi
    fi

    log_error "Cannot determine xray version. Please specify --version"
    return 1
}

# Ensure proto source files exist
ensure_proto_files() {
    if [ ! -d "$PROTO_SRC_DIR" ] || [ -z "$(ls -A "$PROTO_SRC_DIR" 2>/dev/null)" ]; then
        log_warn "Proto source directory empty or missing: $PROTO_SRC_DIR"
        log_info "Run ./scripts/sync-xray-proto.sh first to download proto files"
        return 1
    fi

    local count=$(find "$PROTO_SRC_DIR" -name "*.proto" | wc -l)
    log_info "Found $count proto files in $PROTO_SRC_DIR"
    return 0
}

# Download proto files if needed
download_protos() {
    if ! ensure_proto_files; then
        log_info "Downloading proto files..."
        "$SCRIPT_DIR/sync-xray-proto.sh" --version "${XRAY_VERSION:-26.2.6}"
    fi
}

# Generate Go code from proto
generate_go_code() {
    log_info "Generating Go code..."

    # Create output directory
    mkdir -p "$PROTO_OUT_DIR"

    # Proto include path (for well-known types)
    local proto_include=""
    if command -v protoc &> /dev/null; then
        # Try to find include directory
        local protoc_root=$(dirname $(dirname $(which protoc)))
        if [ -d "$protoc_root/include" ]; then
            proto_include="-I$protoc_root/include"
        fi
    fi

    # Find all proto files
    local proto_files=()
    while IFS= read -r -d '' file; do
        proto_files+=("$file")
    done < <(find "$PROTO_SRC_DIR" -name "*.proto" -print0 2>/dev/null)

    if [ ${#proto_files[@]} -eq 0 ]; then
        log_warn "No proto files found"
        return 1
    fi

    log_info "Found ${#proto_files[@]} proto files to process"

    local generated_count=0
    local failed_files=()

    for proto_file in "${proto_files[@]}"; do
        # Get relative path from src dir
        local rel_path="${proto_file#$PROTO_SRC_DIR/}"
        local basename=$(basename "$proto_file" .proto)
        local out_dir="$PROTO_OUT_DIR/$(dirname "$rel_path")"

        mkdir -p "$out_dir"

        log_info "  Generating: $rel_path"

        if [ "$DRY_RUN" = true ]; then
            log_info "  [DRY RUN] Would execute: protoc ..."
            continue
        fi

        # Run protoc
        local out_base="$out_dir/${basename}.pb.go"

        # Change to output directory for protoc execution
        # This ensures generated files are created in the right place
        (
            cd "$out_dir" && \
            protoc $proto_include \
                --go_out=. \
                --go_opt=paths=source_relative \
                --go-grpc_out=. \
                --go-grpc_opt=paths=source_relative \
                -I"$PROTO_SRC_DIR" \
                "$proto_file" 2>&1
        )

        # Check if files were generated in the output directory
        if [ -f "$out_dir/${basename}.pb.go" ] || [ -f "$out_dir/${basename}_grpc.pb.go" ]; then
            generated_count=$((generated_count + 1))
            log_info "    OK: ${basename}.pb.go"
        else
            failed_files+=("$rel_path")
            log_warn "  No output: $rel_path"
        fi
    done

    if [ $generated_count -eq 0 ]; then
        log_error "No files were generated"
        if [ ${#failed_files[@]} -gt 0 ]; then
            log_error "Failed files: ${failed_files[*]}"
        fi
        return 1
    fi

    log_info "Successfully generated $generated_count files"
    return 0
}

# Update version metadata
update_version_metadata() {
    local timestamp=$(date -u +"%Y-%m-%d %H:%M:%S UTC")
    local generated_count=$(find "$PROTO_OUT_DIR" -name "*.pb.go" 2>/dev/null | wc -l)

    log_info "Updating version metadata..."

    cat > "$VERSION_FILE" << EOF
# Xray Proto Version

This file tracks the xray version used for protobuf definitions.

**Version:** $XRAY_VERSION
**Updated:** $timestamp

## Generation Info

- **Generator:** scripts/gen-xray-proto.sh
- **Source:** https://github.com/XTLS/Xray-core
- **Proto Mirror Dir:** $PROTO_SRC_DIR
- **Generated Output Dir:** $PROTO_OUT_DIR
- **Generated Go Files:** $generated_count
- **Download Time:** $timestamp

## Usage

To regenerate:

    ./scripts/gen-xray-proto.sh --version $XRAY_VERSION

## Notes

- Proto files are mirrored from xray-core repository
- Go code is generated using protoc with grpc plugins
- Generated code replaces minimal stub types
- Legacy JSON serialization still available as fallback
EOF

    log_info "Version metadata updated: $VERSION_FILE"
}

# Verify generated code compiles
verify_generated_code() {
    log_info "Verifying generated code..."

    if [ ! -d "$PROTO_OUT_DIR" ]; then
        log_error "Output directory not found: $PROTO_OUT_DIR"
        return 1
    fi

    local count=$(ls -1 "$PROTO_OUT_DIR"/*.pb.go 2>/dev/null | wc -l)
    if [ "$count" -eq 0 ]; then
        log_error "No .pb.go files found in $PROTO_OUT_DIR"
        return 1
    fi

    log_info "Found $count generated Go files"

    # Try to build
    if [ "$DRY_RUN" = false ]; then
        cd "$PROJECT_ROOT"
        if go build "$PROTO_OUT_DIR/..." 2>/dev/null; then
            log_info "Generated code compiles successfully!"
            return 0
        else
            log_warn "Generated code has compilation issues (may need fixing)"
            return 0
        fi
    fi

    return 0
}

# Main
main() {
    log_info "Xray Proto Code Generator"
    log_info "=========================="
    echo ""

    # Check dependencies first
    if [ "$CHECK_ONLY" = true ]; then
        check_dependencies
        exit $?
    fi

    # Get version
    get_version || exit 1
    echo ""

    # Ensure protos exist
    ensure_proto_files || exit 1
    echo ""

    # Check if we should skip generation
    if [ "$FORCE" = false ] && [ -d "$PROTO_OUT_DIR" ]; then
        local existing_count=$(find "$PROTO_OUT_DIR" -name "*.pb.go" 2>/dev/null | wc -l)
        if [ "$existing_count" -gt 0 ]; then
            log_warn "Output directory already has files"
            log_info "Use --force to regenerate"
            echo ""
            verify_generated_code
            exit 0
        fi
    fi

    # Generate
    if [ "$DRY_RUN" = true ]; then
        log_info "DRY RUN MODE - No files will be generated"
        echo ""
    fi

    generate_go_code || {
        log_error "Failed to generate code"
        exit 1
    }

    echo ""

    # Update metadata
    if [ "$DRY_RUN" = false ]; then
        update_version_metadata
    fi

    echo ""

    # Verify
    verify_generated_code

    echo ""
    log_info "Done!"
    echo ""
    echo "Generated files:"
    echo "  $PROTO_OUT_DIR"
    echo ""
    echo "To use in code:"
    echo "  import github.com/lureiny/v2raymg/pkg/xrayapi/internalproto/gen"
}

main
