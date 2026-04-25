# v2raymg

Proxy node management tool that orchestrates multiple proxy kernels (xray / hysteria / snell / mihomo). Supports multi-node cluster deployment with center/end node architecture.

## Architecture

- **Center Node** — cluster coordinator, handles node discovery and state synchronization
- **End Node** — runs proxy containers, exposes HTTP management API and gRPC for cluster communication

## Supported Proxy Kernels

| Kernel  | Container | Protocols (MVP)                | Notes |
|---------|-----------|--------------------------------|-------|
| xray    | `xray`    | VLESS / VMess / Trojan / Shadowsocks / SOCKS5 / HTTP (plus Reality, ws/grpc/httpupgrade/xhttp transports) | Primary kernel, most feature-complete |
| hysteria | `hysteria` | Hysteria2 (UDP)              | Single-inbound specialization |
| snell   | `snell`   | Snell v4                       | Single-inbound kernel |
| mihomo  | `mihomo`  | VLESS / VMess / Trojan / Shadowsocks / Hysteria2 | Shared-credential listener per inbound; user-level isolation via the forward layer. Hysteria2 uses UDP forward rules. Tracks MetaCubeX/mihomo Alpha |

All kernels plug into the same Container abstraction (`pkg/proxy/core/container`), share the forward layer for user-facing ports (`pkg/proxy/forward`), and are driven by the same HTTP / RPC / subscription / updater surface. Adding a new kernel follows the three principles in `docs/container-design-principles.md`.

## Quick Start

1. Copy and edit the configuration template:

```bash
cp config/config.example.yaml config.yaml
# Edit config.yaml with your settings
```

2. Build:

```bash
make build
# or: go build -o bin/v2raymg .
```

3. Run:

```bash
./bin/v2raymg server --conf config.yaml
```

## CLI

```bash
# Start server
v2raymg server --conf config.yaml

# Interactive management CLI
v2raymg cli --conf .v2raymg-tools.yaml
```

The interactive CLI provides commands for node, user, inbound, certificate, and statistics management with auto-completion.

## HTTP API

Public routes (`/sub`, `/api/login`, `/api/authHysteria2`) do not require authentication. All other routes are under `/api` and require JWT or `X-Token` header.

### Public (No Auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/sub` | Get proxy subscription (supports user/tag/protocol/SNI filters) |
| POST | `/api/login` | User login (returns JWT) |
| POST | `/api/authHysteria2` | Hysteria2 auth service |
| GET | `/help/*` | API help |

### User Routes (Authenticated)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/user` | List all users |
| GET | `/api/profile` | Get current user profile |
| PUT | `/api/profile/password` | Change password |
| POST | `/api/user/reset-token` | Reset auth token |
| POST | `/api/rotateInboundPort` | Rotate inbound port |
| POST | `/api/rotateAllPorts` | Rotate all ports |
| POST | `/api/logout` | Logout |
| GET | `/api/status` | Get node status |

### Admin — User Management

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/user` | Add user |
| PUT | `/api/user` | Update user password/expiry |
| DELETE | `/api/user` | Delete user |
| POST | `/api/user/reset` | Reset user proxy key |
| PUT | `/api/user/:name/role` | Set user role |
| PUT | `/api/user/:name/bandwidth` | Set user bandwidth limit |
| PUT | `/api/user/:name/client-limit` | Set user client limit |
| POST | `/api/copyUserBetweenNodes` | Copy users between nodes |

### Admin — Inbound Management

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/inbound` | Get inbound configuration |
| POST | `/api/inbound` | Add inbound (raw JSON) |
| GET | `/api/inbounds` | List all inbounds (all containers) |
| DELETE | `/api/inbounds` | Delete inbound by container + name |
| POST | `/api/inbound/fast` | Quick add inbound (protocol + stream selection) |

### Admin — Node & Cluster

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/node` | Get all cluster nodes |
| GET | `/api/node/:name/groups` | Get node groups (cluster mode) |
| PUT | `/api/node/:name/groups` | Set node groups (cluster mode) |
| PUT | `/api/gateway` | Enable/disable gateway mode |
| PUT | `/api/pingCheck` | Enable/disable ping checking |

### Admin — Certificate Management

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/getCerts` | List available certificates |
| POST | `/api/cert` | Request new certificate |
| POST | `/api/cert/transfer` | Transfer cert to remote node |
| DELETE | `/api/cert` | Delete certificate |

### Admin — Operations

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/update` | Update proxy binary version |
| GET | `/api/metrics` | Prometheus metrics endpoint |

## Configuration

See [`config/config.example.yaml`](config/config.example.yaml) for the full configuration reference.
