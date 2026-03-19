# v2raymg

Proxy node management tool for v2ray/xray. Supports multi-node cluster deployment with center/end node architecture.

## Architecture

- **Center Node** — cluster coordinator, handles node discovery and state synchronization
- **End Node** — runs proxy containers (xray), exposes HTTP management API and gRPC for cluster communication

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

All endpoints (except `/sub` and `/authHysteria2`) require `?token=<http_token>`.

### Subscription

| Method | Path | Description |
|--------|------|-------------|
| GET | `/sub` | Get proxy subscription (supports user/tag/protocol/SNI filters) |

### User Management

| Method | Path | Description |
|--------|------|-------------|
| GET | `/user` | List all users |
| POST | `/user` | Add user |
| PUT | `/user` | Update user password/expiry |
| DELETE | `/user` | Delete user |
| POST | `/user/reset` | Reset user proxy key |
| DELETE | `/users` | Bulk delete users |
| POST | `/copyUserBetweenNodes` | Copy users between nodes |

### Inbound Management

| Method | Path | Description |
|--------|------|-------------|
| GET | `/inbound` | Get inbound configuration |
| POST | `/inbound` | Add inbound (raw JSON) |
| DELETE | `/inbound` | Delete inbound by tag |
| POST | `/inbound/fast` | Quick add inbound (protocol + stream selection) |

### Node & Cluster

| Method | Path | Description |
|--------|------|-------------|
| GET | `/node` | Get all cluster nodes |
| GET | `/tag` | Get all inbound tags from target node |
| PUT | `/gateway` | Enable/disable gateway mode |
| PUT | `/pingCheck` | Enable/disable ping checking |

### Certificate Management

| Method | Path | Description |
|--------|------|-------------|
| GET | `/getCerts` | List available certificates |
| POST | `/cert` | Request new certificate |
| POST | `/cert/transfer` | Transfer cert to remote node |

### Statistics & Operations

| Method | Path | Description |
|--------|------|-------------|
| GET | `/stat` | Get traffic statistics |
| GET | `/metrics` | Prometheus metrics endpoint |
| POST | `/update` | Update proxy binary version |
| POST | `/adaptive` | Auto-assign ports to inbound tags |
| POST | `/adaptiveOp` | Manage port pool (add/delete ports) |

### Authentication

| Method | Path | Description |
|--------|------|-------------|
| POST | `/authHysteria2` | Hysteria2 auth service (no token required) |

## Configuration

See [`config/config.example.yaml`](config/config.example.yaml) for the full configuration reference.
