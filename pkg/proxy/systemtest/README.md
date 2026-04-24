# System Test (pkg/proxy)

## Purpose

Validate reconstructed framework availability with end-to-end flows against
real proxy kernels. xray is the primary covered kernel; stage 10b added
mihomo coverage (vmess / trojan / shadowsocks) mirroring the xray fixture.

Typical flow:
1. Start a container (xray or mihomo)
2. Add/use a protocol inbound via FastAdd
3. Access a local httptest origin through the proxy chain
4. (mihomo) Stop + Start the container, verify user port + listener restore

## Quick Start

```bash
# Set xray binary path
export XRAY_BIN=/path/to/xray

# Run all tests (unit + integration)
go test ./pkg/proxy/... -tags=integration -v

# Run only unit tests
go test ./pkg/proxy/... -v

# Run only system tests (requires XRAY_BIN)
go test ./pkg/proxy/systemtest -tags=integration -v
```

## Test Cases

### 1) Real Integration Test (preferred)
File: `xray_socks5_system_test.go` (build tag: `integration`)

Run:
```bash
XRAY_BIN=/path/to/xray go test ./pkg/proxy/systemtest -tags=integration -run TestXrayContainerSocks5WebsiteAccess -v
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
go test ./pkg/proxy/systemtest -run TestDegradedLocalSocks5ProxyChain -v
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
XRAY_BIN=/path/to/xray go test ./pkg/proxy/systemtest -tags=integration -run TestXrayDynamicInbound_ProtocolMatrix -v
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

## Mihomo (stage 10b)

Files:
- `mihomo_helpers_test.go` — binary resolution + rig startup
- `mihomo_protocol_matrix_test.go` — vmess / trojan / shadowsocks FastAdd + user
  handshake + port release negative control (MVP D4 scope)
- `mihomo_restore_test.go` — Stop → Start → forward port stability + handshake
  recovery

Run:
```bash
# XRAY_BIN is always required (used as the protocol client)
# MIHOMO_BIN optional — if unset, the Updater downloads the Alpha release
XRAY_BIN=/path/to/xray MIHOMO_BIN=/path/to/mihomo \
  go test ./pkg/proxy/systemtest -tags=integration -run "TestMihomo" -v
```

Preconditions:
- XRAY_BIN: required. xray is the client speaking vmess / trojan / ss into
  mihomo's listener via the forward layer.
- MIHOMO_BIN: optional. When missing, `ensureMihomoBin` pulls the Alpha
  pre-release into `t.TempDir()` (requires network egress to GitHub).
- Network: required when MIHOMO_BIN is absent.

## Notes

- This test package is independent and does not modify legacy code paths.
- Fits current constraint: implement/test first, no main-flow integration.
- All integration tests require `XRAY_BIN` environment variable set.
- Unit tests (without integration tag) can run without xray binary.
