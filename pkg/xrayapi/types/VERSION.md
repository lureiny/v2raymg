# Xray Proto Version

This file tracks the xray version used for protobuf definitions.

**Version:** 26.2.6
**Updated:** 2026-03-05 03:16:10 UTC

## Generation Info

- **Generator:** scripts/gen-xray-proto.sh
- **Source:** https://github.com/XTLS/Xray-core
- **Proto Mirror Dir:** /home/node/.openclaw/projects/v2raymg/pkg/xrayapi/internalproto/upstream
- **Generated Output Dir:** /home/node/.openclaw/projects/v2raymg/pkg/xrayapi/internalproto/gen
- **Generated Go Files:** 92
- **Download Time:** 2026-03-05 03:16:10 UTC

## Usage

To regenerate:

    ./scripts/gen-xray-proto.sh --version 26.2.6

## Notes

- Proto files are mirrored from xray-core repository
- Go code is generated using protoc with grpc plugins
- Generated code replaces minimal stub types
- Legacy JSON serialization still available as fallback
