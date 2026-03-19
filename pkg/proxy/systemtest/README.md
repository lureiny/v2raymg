# System Test (Proxyrefactor)

## Purpose

Validate reconstructed framework availability with a simple end-to-end flow:
1. Start xray container
2. Add/use socks5 inbound
3. Access website through proxy

## Quick Start

```bash
# Set xray binary path
export XRAY_BIN=/path/to/xray

# Run all tests (unit + integration)
go test ./pkg/proxyrefactor/... -tags=integration -v

# Run only unit tests
go test ./pkg/proxyrefactor/... -v

# Run only system tests (requires XRAY_BIN)
go test ./pkg/proxyrefactor/systemtest -tags=integration -v
```

## Test Cases

### 1) Real Integration Test (preferred)
File: `xray_socks5_system_test.go` (build tag: `integration`)

Run:
```bash
XRAY_BIN=/path/to/xray go test ./pkg/proxyrefactor/systemtest -tags=integration -run TestXrayContainerSocks5WebsiteAccess -v
```

Success criteria:
- xray starts successfully
- socks5 port becomes ready
- request to `http://example.com` via socks5 succeeds
- response body contains `Example Domain`

Failure criteria:
- xray start/port readiness/request fails
- response status abnormal
- body mismatch (proxy chain likely broken)

### 2) Degraded Validation (works in restricted env)
File: `degraded_socks5_chain_test.go`

Run:
```bash
go test ./pkg/proxyrefactor/systemtest -run TestDegradedLocalSocks5ProxyChain -v
```

What it validates:
- local socks5 proxy chain works (client -> socks5 -> origin)
- does not require xray binary or external network

What it does NOT validate:
- real xray process startup
- real xray socks inbound behavior

## Protocol Matrix Testing

The protocol matrix test (`xray_protocol_matrix_test.go`) validates all supported protocols:

| Protocol | Test | Connectivity |
|----------|------|--------------|
| SOCKS5 | TestXrayDynamicInbound_ProtocolMatrix | Full |
| HTTP | TestXrayDynamicInbound_ProtocolMatrix | Full |
| VMess | TestXrayDynamicInbound_ProtocolMatrix | Listen only |
| VLESS | TestXrayDynamicInbound_ProtocolMatrix | Listen only |
| Trojan | TestXrayDynamicInbound_ProtocolMatrix | Listen only |
| Shadowsocks | TestXrayDynamicInbound_ProtocolMatrix | Listen only |

Run:
```bash
XRAY_BIN=/path/to/xray go test ./pkg/proxyrefactor/systemtest -tags=integration -run TestXrayDynamicInbound_ProtocolMatrix -v
```

## Examples Reference

For protocol configuration examples, see `pkg/proxyrefactor/examples/inbound/`:

| Example File | Description |
|--------------|-------------|
| `socks5.go` | SOCKS5 proxy configuration |
| `http.go` | HTTP proxy with authentication |
| `vmess.go` | VMess protocol |
| `vless.go` | VLESS protocol |
| `trojan.go` | Trojan protocol |
| `shadowsocks.go` | Shadowsocks protocol |
| `reality.go` | VLESS + Reality (stealth) |

### Test → Example Mapping

| Test (A: Direct Reference) | Example File |
|---------------------------|--------------|
| xray_socks5_system_test.go | socks5.go |
| xray_protocol_matrix_test.go | socks5.go, http.go, vmess.go, vless.go, trojan.go, shadowsocks.go |
| xray_e2e_protocol_test.go | All protocol examples |

| Test (B: Light Modification) | Suggested Example Reference |
|------------------------------|----------------------------|
| xray_dynamic_inbound_system_test.go | Base: socks5.go + vmess.go |

## Notes

- This test package is independent and does not modify legacy code paths.
- Fits current constraint: implement/test first, no main-flow integration.
- All integration tests require `XRAY_BIN` environment variable set.
- Unit tests (without integration tag) can run without xray binary.
