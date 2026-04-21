# v2raymg HTTP API Reference

> 更新时间: 2026-04-21 (v4 - 新增 GET /api/containers)
> 基于 `pkg/http/init.go` 路由注册表及全部 Handler 源码生成

## 技术栈

- **框架**: Gin
- **认证**: JWT Bearer Token + X-Token（向后兼容）
- **架构**: Center/End Node 集群模式，HTTP Handler → RPC Client → End Node gRPC Server

---

## 认证机制

### 1. JWT Bearer Token（推荐）

通过 `/api/login` 获取 JWT，后续请求在 Header 中携带：

```http
Authorization: Bearer <jwt-token>
```

JWT 中包含 `username` 和 `role`（`admin` 或 `normal`），决定接口权限。

### 2. X-Token（管理员直通）

使用配置文件中的 admin token，通过 `X-Token` Header 鉴权：

```http
X-Token: <admin-token>
```

X-Token 持有者自动被视为 admin 角色，但 **没有用户上下文**（无 username）。

### 3. 权限等级

| 等级 | 说明 |
|------|------|
| Public | 无需认证 |
| User | 任何已认证用户（JWT 或 X-Token） |
| Admin | admin 角色 JWT 或 X-Token |

---

## target 参数说明

大部分接口支持 `target` 参数，用于指定操作的目标节点：

| 值 | 行为 |
|----|------|
| 空/不传 | 默认当前节点 |
| `all` | 所有可用节点 |
| 节点名 | 指定节点 |

---

## API 端点一览

| Method | Path | 权限 | 功能 |
|--------|------|------|------|
| **认证** |
| POST | `/api/login` | Public | 用户登录，获取 JWT |
| POST | `/api/logout` | User | 注销当前 JWT |
| **用户管理** |
| GET | `/api/user` | User | 获取用户列表（admin 看全部，普通用户看自己） |
| GET | `/api/profile` | User | 获取当前用户完整资料 |
| POST | `/api/user` | Admin | 添加用户 |
| PUT | `/api/user` | Admin | 更新用户 |
| DELETE | `/api/user` | Admin | 删除用户 |
| POST | `/api/user/reset` | Admin | 重置用户代理密钥 |
| POST | `/api/user/reset-token` | User | 重置用户 auth token |
| PUT | `/api/user/:name/role` | Admin | 设置用户角色 |
| PUT | `/api/user/:name/bandwidth` | Admin | 设置用户带宽限制 |
| PUT | `/api/user/:name/client-limit` | Admin | 设置用户连接数限制 |
| PUT | `/api/profile/password` | User | 修改密码 |
| POST | `/api/rotateInboundPort` | User | 指定 inbound 端口轮换 |
| POST | `/api/rotateAllPorts` | User | 全部端口轮换 |
| POST | `/api/copyUserBetweenNodes` | Admin | 节点间复制用户 |
| **Inbound 管理** |
| GET | `/api/inbound` | Admin | 获取 inbound 配置 |
| POST | `/api/inbound` | Admin | 添加 inbound（raw JSON） |
| POST | `/api/inbound/fast` | Admin | 快速添加 inbound |
| GET | `/api/inbounds` | Admin | 列出所有 inbound（跨容器） |
| DELETE | `/api/inbounds` | Admin | 删除 inbound（按容器+名称） |
| **证书管理** |
| GET | `/api/getCerts` | Admin | 列出证书 |
| POST | `/api/cert` | Admin | 申请证书 |
| POST | `/api/cert/transfer` | Admin | 传输证书到其他节点 |
| DELETE | `/api/cert` | Admin | 删除证书 |
| **节点 & 集群** |
| GET | `/api/node` | Admin | 获取集群所有节点 |
| GET | `/api/containers` | Admin | 获取节点已启用的 container 类型列表 |
| GET | `/api/status` | User | 获取节点状态指标 |
| PUT | `/api/gateway` | Admin | 启用/禁用 gateway 模式 |
| PUT | `/api/pingCheck` | Admin | 启用/禁用 ping 检测 |
| GET | `/api/node/:name/groups` | Admin | 获取节点分组（需启用集群同步） |
| PUT | `/api/node/:name/groups` | Admin | 设置节点分组（需启用集群同步） |
| **订阅** |
| GET | `/sub` | Public | 获取代理订阅 |
| **其他** |
| POST | `/api/update` | Admin | 更新代理二进制版本 |
| POST | `/api/authHysteria2` | Public | Hysteria2 认证回调 |
| GET | `/help/*relativePath` | Public | API 帮助信息 |

---

## 接口详细说明

### 认证

#### POST /api/login - 用户登录

**Request:**
```json
{
  "username": "用户名",
  "password": "密码"
}
```
支持 JSON body 或 form data。

**Response (200):**
```json
{
  "token": "jwt-token-string",
  "expire": 1704067200,
  "role": "normal|admin",
  "username": "用户名"
}
```

**Error:**
- `400` "username and password are required"
- `401` "invalid username or password"

---

#### POST /api/logout - 注销 JWT

将当前 JWT 加入黑名单。仅支持 JWT Bearer 认证，X-Token 不支持。

**Request:** 无 body

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

**Error:**
- `401` "JWT required for /logout"

---

### 用户管理

#### GET /api/user - 获取用户列表

**Query:** `?target=节点名`

**Response (200) - Admin 视图:**
```json
{
  "节点名": [
    {
      "name": "user1",
      "auth_token": "token",
      "expire_time": 1704067200,
      "downlink": 12345678,
      "uplink": 87654321,
      "role": "normal",
      "upload_bps": 1000000,
      "download_bps": 500000,
      "max_clients": 5,
      "group": "default"
    }
  ]
}
```

**Response (200) - 普通用户视图:**
```json
{
  "节点名": [
    {
      "name": "user1",
      "auth_token": "token",
      "expire_time": 1704067200,
      "downlink": 12345678,
      "uplink": 87654321,
      "role": "normal",
      "traffic_limit": 10737418240,
      "traffic_used_uplink": 1000000,
      "traffic_used_downlink": 2000000,
      "inbounds": [
        {"node": "node1", "tag": "vless-tcp", "container": "xray", "port": 443}
      ]
    }
  ]
}
```

---

#### GET /api/profile - 获取当前用户资料

**Query:** `?target=节点名`

所有角色返回完整资料（含 inbounds、流量详情），格式同上面的普通用户视图。

---

#### POST /api/user - 添加用户

**Request:**
```json
{
  "target": "节点名",
  "user": "用户名",
  "pwd": "密码",
  "expire": 1704067200,
  "ttl": 86400,
  "role": "normal|admin",
  "group": "分组名",
  "upload_bps": 1000000,
  "download_bps": 500000,
  "max_clients": 5,
  "client_recycle_delay_sec": 60,
  "client_drain_sec": 2
}
```
- `expire` 和 `ttl` 可选，`ttl` 优先
- 其他字段均可选

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### PUT /api/user - 更新用户

**Request:**
```json
{
  "target": "节点名",
  "user": "用户名",
  "pwd": "新密码",
  "expire": 1704067200,
  "ttl": 86400,
  "role": "normal|admin",
  "group": "分组名",
  "upload_bps": 1000000,
  "download_bps": 500000,
  "max_clients": 5,
  "client_recycle_delay_sec": 60,
  "client_drain_sec": 2
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### DELETE /api/user - 删除用户

**Request:**
```json
{
  "target": "节点名",
  "user": "用户名"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### POST /api/user/reset - 重置用户代理密钥

**Request:**
```json
{
  "target": "节点名",
  "user": "用户名"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### POST /api/user/reset-token - 重置 Auth Token

仅支持 JWT 认证。重置调用者自己的 auth token。

**Request:** 无 body

**Response (200):**
```json
{
  "code": 0,
  "msg": "ok",
  "auth_token": "new-token"
}
```

---

#### PUT /api/user/:name/role - 设置用户角色

**Path:** `:name` - 用户名

**Request:**
```json
{
  "role": "admin|normal",
  "target": "节点名"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

**Error:**
- `400` "role must be 'admin' or 'normal'"

---

#### PUT /api/user/:name/bandwidth - 设置带宽限制

**Path:** `:name` - 用户名

**Request:**
```json
{
  "upload_bps": 1000000,
  "download_bps": 500000,
  "target": "节点名"
}
```
- 至少设置一个方向
- `0` = 不限制

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### PUT /api/user/:name/client-limit - 设置连接数限制

**Path:** `:name` - 用户名

**Request:**
```json
{
  "max_clients": 3,
  "recycle_delay_sec": 60,
  "drain_sec": 2,
  "target": "节点名"
}
```
- `max_clients` 必填，`0` = 不限制
- 其他字段可选

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### PUT /api/profile/password - 修改密码

仅支持 JWT 认证。

**Request:**
```json
{
  "old_password": "旧密码",
  "new_password": "新密码"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

**Error:**
- `400` "old_password and new_password are required"
- `403` "old password is incorrect"

---

#### POST /api/rotateInboundPort - 指定 Inbound 端口轮换

更换指定 container+inbound 的前置转发端口（make-before-break，无中断）。通过 RPC 调用目标节点执行。

**Request:**
```json
{
  "target": "目标节点名（可选，默认本机）",
  "username": "用户名（可选）",
  "container": "xray",
  "inbound": "inbound-tag",
  "port": 0
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `target` | string | 否 | 目标节点名，空则默认本机 |
| `username` | string | 视认证方式 | 见下方认证规则 |
| `container` | string | **是** | 容器类型，如 `"xray"` |
| `inbound` | string | **是** | inbound tag |
| `port` | uint32 | 否 | `0` = 随机分配，`>0` = 指定端口 |

**认证规则：**
- **X-Token**：`username` 必填（break-glass admin，无用户上下文）
- **admin JWT**：`username` 可选，不填默认操作自身
- **普通用户 JWT**：只能操作自己的 inbound，指定他人 → `403`

**Response (200):**
```json
{"code": 0, "port": 12345, "msg": "ok"}
```

**业务错误 (HTTP 200, code=300):**
```json
{"code": 300, "port": 0, "msg": "错误描述"}
```
用户不存在、inbound 不存在、forward rule 缺失等业务错误通过 `code=300` 返回，HTTP 状态码仍为 200。

**Error:**
- `400` "container and inbound are required"
- `400` "username required for X-Token requests"
- `403` "only admin can rotate another user's inbound port"
- `502` "no available node"

---

#### POST /api/rotateAllPorts - 全部端口轮换

重置用户所有 inbound 的前置转发端口（逐条 make-before-break，无中断）。通过 RPC 调用目标节点执行。

**Request:**
```json
{
  "target": "目标节点名（可选，默认本机）",
  "username": "用户名（可选）"
}
```

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `target` | string | 否 | 目标节点名，空则默认本机 |
| `username` | string | 视认证方式 | 见下方认证规则 |

**认证规则：**
- **X-Token**：`username` 必填
- **admin JWT**：`username` 可选，不填默认操作自身
- **普通用户 JWT**：只能操作自己的端口，指定他人 → `403`

**Response (200):**
```json
{
  "code": 0,
  "ports": {"xray:vless-tcp": 23456, "xray:trojan-tls": 34567},
  "msg": "ok"
}
```

**部分成功 (HTTP 200, code=301):**
```json
{
  "code": 301,
  "ports": {"xray:vless-tcp": 23456},
  "msg": "首个失败的错误描述"
}
```
多个 inbound 中部分轮换成功、部分失败时返回 `code=301`，`ports` 包含已成功轮换的 inbound 映射，`msg` 包含首个失败的错误信息。

**全部失败 (HTTP 200, code=300):**
```json
{"code": 300, "msg": "错误描述"}
```
所有 inbound 均轮换失败（如用户不存在、无 forward rule 等），`ports` 为空。

**Error:**
- `400` "username required for X-Token requests"
- `403` "only admin can rotate another user's ports"
- `502` "no available node"

---

#### POST /api/copyUserBetweenNodes - 节点间复制用户

**Request:**
```json
{
  "src_node": "源节点",
  "dst_node": "目标节点"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

### Inbound 管理

#### GET /api/inbound - 获取 Inbound 配置

**Query:** `?target=节点名&src_tag=inbound-tag&container=xray`

- `src_tag` 可选，不传返回全部
- `container` 默认 `xray`

**Response:** inbound 配置 JSON

---

#### POST /api/inbound - 添加 Inbound（raw JSON）

**Request:**
```json
{
  "target": "节点名",
  "bound_raw_string": "base64编码的inbound配置JSON",
  "container": "xray"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### POST /api/inbound/fast - 快速添加 Inbound

**Request:**
```json
{
  "target": "节点名",
  "tag": "inbound-tag",
  "protocol": "vless",
  "transport": "tcp",
  "domain": "example.com",
  "port": 443,
  "security": "tls",
  "self_signed": false,
  "container": "xray",
  "reality_target": "www.example.com:443",
  "reality_server_names": ["www.example.com"],
  "reality_short_ids": ["0123456789abcdef"],
  "ws_path": "/ws",
  "grpc_service_name": "grpc",
  "http_path": "/",
  "http_host": "example.com",
  "httpupgrade_path": "/",
  "httpupgrade_host": "example.com",
  "xhttp_path": "/xhttp",
  "xhttp_mode": "auto",
  "xhttp_host": "example.com",
  "alpn": "h2,http/1.1",
  "sniffing_enabled": true,
  "extra_params": {
    "tls_min_version": "1.3",
    "tls_reject_unknown_sni": "true"
  }
}
```

**基本字段:**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `target` | string | 否 | 目标节点名，空则为本节点 |
| `tag` | string | 否 | inbound 唯一标识，空则自动生成 |
| `protocol` | string | 否 | 协议类型，默认 `vless` |
| `transport` | string | 否 | 传输层协议，默认 `tcp`（也可用 `stream` 字段，向后兼容） |
| `domain` | string | 否 | TLS 证书域名；为空时自动使用自签证书 |
| `port` | int | 否 | 监听端口，0 则随机分配 |
| `security` | string | 否 | 安全类型，默认 `tls`；支持 `tls` \| `reality` |
| `self_signed` | bool | 否 | 是否使用自签证书（仅 TLS） |
| `container` | string | 否 | 容器类型，默认 `xray` |

**Reality 字段:**

| 字段 | 类型 | 条件 | 说明 |
|------|------|------|------|
| `reality_target` | string | security=reality | 伪装目标，如 `www.example.com:443` |
| `reality_server_names` | []string | 否 | 允许的 SNI 列表，默认取域名 |
| `reality_short_ids` | []string | 否 | short ID 列表，默认自动生成 |

> Reality 密钥对由服务端自动生成，公钥可通过获取 inbound 配置接口查询。

**传输层便捷字段（按需填写）:**

| 字段 | 类型 | 对应传输 | 说明 |
|------|------|----------|------|
| `ws_path` | string | ws | WebSocket 路径，默认 `/ws` |
| `grpc_service_name` | string | grpc | gRPC 服务名，默认 `grpc` |
| `http_path` | string | h2 | HTTP/2 路径，默认 `/` |
| `http_host` | string | h2 | HTTP/2 host，逗号分隔，默认取 `domain` |
| `httpupgrade_path` | string | httpupgrade | HTTPUpgrade 路径，默认 `/` |
| `httpupgrade_host` | string | httpupgrade | HTTPUpgrade Host 头，默认取 `domain` |
| `xhttp_path` | string | xhttp/splithttp | XHTTP 路径，默认 `/` |
| `xhttp_mode` | string | xhttp/splithttp | XHTTP 模式：`auto`/`packet-up`/`stream-one`/`stream-up` |
| `xhttp_host` | string | xhttp/splithttp | XHTTP host，逗号分隔 |
| `alpn` | string | tls | ALPN 协商列表，逗号分隔，如 `h2,http/1.1` |
| `sniffing_enabled` | bool | 全部 | 启用流量嗅探 |

**extra_params（高级参数透传）:**

通过 `extra_params` map 可传递任意参数到 Executor，无需修改 API 定义：

| key | 类型 | 说明 |
|-----|------|------|
| `tls_min_version` | string | TLS 最低版本，如 `1.3` |
| `tls_reject_unknown_sni` | "true"/"false" | 拒绝未知 SNI |
| `ocsp_stapling` | string(int) | OCSP 装订间隔秒数 |
| `sniffing_dest_override` | string | 嗅探目标覆盖，逗号分隔，如 `http,tls,quic` |
| `sniffing_route_only` | "true"/"false" | 仅路由不替换目标 |
| `uuid` | string | VLESS/VMess UUID（默认自动生成） |
| `flow` | string | VLESS flow，如 `xtls-rprx-vision` |
| `method` | string | Shadowsocks 加密方式 |
| `password` | string | Trojan/Shadowsocks 密码 |

**Protocol:** `vless` | `vmess` | `trojan` | `ss` / `shadowsocks`

**Transport:** `tcp` | `ws` | `h2`（`http` 自动 normalize 为 `h2`）| `grpc` | `httpupgrade` | `xhttp` | `splithttp`

**支持的协议组合:**

| 协议 | 传输 | 安全 |
|------|------|------|
| VLESS | tcp, ws, grpc, httpupgrade, xhttp, splithttp | tls, reality |
| VMess | tcp, ws, h2 | tls |
| Trojan | tcp, grpc | tls |
| Shadowsocks | tcp, ws, grpc | none, tls, reality |

**示例 - VLESS+XHTTP+Reality:**
```json
{
  "tag": "vless-xhttp-reality",
  "protocol": "vless",
  "transport": "xhttp",
  "security": "reality",
  "reality_target": "www.example.com:443",
  "reality_server_names": ["www.example.com"],
  "xhttp_path": "/xhttp",
  "sniffing_enabled": true
}
```

**示例 - VLESS+HTTPUpgrade+TLS（自签）:**
```json
{
  "tag": "vless-httpupgrade",
  "protocol": "vless",
  "transport": "httpupgrade",
  "security": "tls",
  "self_signed": true,
  "httpupgrade_path": "/upgrade",
  "alpn": "http/1.1"
}
```

**示例 - VMess+h2+TLS:**
```json
{
  "tag": "vmess-h2",
  "protocol": "vmess",
  "transport": "h2",
  "security": "tls",
  "domain": "example.com",
  "http_path": "/api",
  "http_host": "example.com"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### GET /api/inbounds - 列出所有 Inbound

跨容器（xray, snell, hysteria）列出所有 inbound。

**Query:** `?target=节点名`

**Response (200):**
```json
[
  {"container": "xray", "name": "vless-tcp"},
  {"container": "snell", "name": "snell-proxy"}
]
```

---

#### DELETE /api/inbounds - 删除 Inbound（按容器+名称）

**Request:**
```json
{
  "target": "节点名",
  "container": "xray|snell|hysteria",
  "name": "inbound-tag"
}
```
- `container` 和 `name` 必填

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

### 证书管理

#### GET /api/getCerts - 列出证书

**Query:** `?target=节点名`

**Response (200):**
```json
{
  "节点名": ["domain1.com", "domain2.com"]
}
```

---

#### POST /api/cert - 申请证书

**Request:**
```json
{
  "target": "节点名",
  "domain": "example.com"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### POST /api/cert/transfer - 传输证书

将本地证书传输到目标节点（不含本节点）。

**Request:**
```json
{
  "target": "目标节点名",
  "domain": "example.com"
}
```

**Response (200):**
传输结果 JSON（包含各节点状态）

---

#### DELETE /api/cert - 删除证书

删除指定域名的证书文件（.crt, .key, .json, .meta.json）。

**Request:**
```json
{
  "target": "节点名",
  "domain": "example.com"
}
```
- `domain` 必填

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

### 节点 & 集群

#### GET /api/node - 获取集群节点列表

**Response (200):**
```json
[
  {
    "name": "node1",
    "host": "192.168.1.1",
    "port": 62789,
    "cluster_name": "default",
    "groups": ["default", "hk"]
  }
]
```

---

#### GET /api/containers - 获取节点已启用的 Container 列表

**Query:** `?target=节点名`

返回指定节点（或全部节点）配置中已启用（`enabled: true`）的 container 类型。通过 gRPC 向目标节点查询，支持多节点并发。

**Response (200):**
```json
{
  "nodes": {
    "node1": ["xray", "hysteria"],
    "node2": ["xray"]
  },
  "failed": {
    "node3": "connection timeout"
  }
}
```

---

#### GET /api/status - 获取节点状态

**Query:** `?target=节点名`

**Response (200):**
```json
{
  "nodes": [
    {
      "node_name": "node1",
      "only_gateway": false,
      "cluster_user_enabled": true,
      "mem_total": 8589934592,
      "mem_used": 2147483648,
      "mem_available": 6442450944,
      "num_goroutine": 150,
      "cpu_percent": 25.5,
      "net_rx_bytes": 1000000000,
      "net_tx_bytes": 2000000000,
      "tcp_connections": 1500,
      "net_rx_speed": 10485760.0,
      "net_tx_speed": 20971520.0,
      "version": "v1.0.0"
    }
  ],
  "failed": {
    "node2": "connection timeout"
  }
}
```

---

#### PUT /api/gateway - 设置 Gateway 模式

**Request:**
```json
{
  "target": "节点名",
  "enable_gateway_model": true
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### PUT /api/pingCheck - 设置 Ping 检测

**Request:**
```json
{
  "target": "节点名",
  "enable_ping_check": true
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### GET /api/node/:name/groups - 获取节点分组

> 仅在启用集群同步时可用

**Path:** `:name` - 节点名

**Response (200):**
```json
{
  "groups": ["default", "hk", "sg"]
}
```

---

#### PUT /api/node/:name/groups - 设置节点分组

> 仅在启用集群同步时可用

**Path:** `:name` - 节点名

**Request:**
```json
{
  "groups": ["default", "hk", "sg"]
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

### 订阅

#### GET /sub - 获取代理订阅

**Query 参数:**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `user` | string | 是* | 用户名 |
| `pwd` | string | 是* | 密码 |
| `token` | string | 是* | auth token（与 user+pwd 二选一） |
| `target` | string | 否 | 节点名，默认 all |
| `exclude_protocols` | string | 否 | 排除协议，逗号分隔 |
| `use_sni` | bool | 否 | 使用 SNI，默认 true |
| `fake` | bool | 否 | 返回假数据，默认 false |
| `client` | string | 否 | 客户端格式：clash/surge/qv2ray（覆盖 User-Agent） |
| `ext_sub` | []string | 否 | 外部订阅链接（最多 10 个），拉取后与集群订阅合并 |
| `proxy_group` | []string | 否 | Clash 自定义代理组 |
| `rule_provider` | []string | 否 | Clash 规则提供者 |
| `rule` | []string | 否 | Clash 自定义规则 |

**Response:** 根据 User-Agent 或 `client` 参数自动转换格式：
- Clash → YAML 配置
- Surge → Surge 配置
- 其他 → base64 编码 URI 列表

---

### 其他

#### POST /api/update - 更新代理版本

**Request:**
```json
{
  "target": "节点名",
  "version_tag": "latest"
}
```
- `version_tag` 默认 `"latest"`

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### POST /api/authHysteria2 - Hysteria2 认证回调

供 Hysteria2 服务端调用的认证接口。

**Request:**
```json
{
  "addr": "客户端IP",
  "auth": "密码（明文或base64）",
  "tx": 0
}
```

**Response:**
- 成功: `200 {"ok": true, "id": "用户名"}`
- 失败: `403 {"ok": false}`

---

#### GET /help - API 帮助

- `/help` - 返回所有接口帮助
- `/help/<path>` - 返回指定接口帮助

---

## 统一响应格式

所有 API 接口（`/sub` 和 `/help` 除外）统一使用 JSON 响应。

### 成功响应

```json
{"code": 0, "msg": "ok"}
```

部分接口在成功时返回额外数据字段（如 `auth_token`、`port`、`ports` 等），但始终包含 `code: 0`。

### 错误响应

```json
{"code": <http_status>, "msg": "<错误描述>"}
```

### HTTP 状态码语义

| 状态码 | 含义 | 场景 |
|--------|------|------|
| `200` | 成功 | 操作完成 |
| `400` | 请求参数错误 | 缺少必填字段、格式不正确、参数校验失败 |
| `401` | 未认证 | 未提供 Token 或 Token 无效 |
| `403` | 权限不足 | 非 Admin 访问管理接口、密码错误 |
| `404` | 资源不存在 | 证书不存在等 |
| `500` | 服务端错误 | RPC 调用失败、内部处理出错 |
| `502` | 节点不可用 | 目标节点无法连接或不存在 |

### 多节点错误

当操作涉及多节点时，错误信息中包含各节点的失败详情：

```json
{"code": 500, "msg": "node: node1 > err: xxx|node: node2 > err: yyy"}
```

---

## 代码位置参考

| 模块 | 文件 |
|------|------|
| Handler 注册 | `pkg/http/init.go` |
| 认证中间件 | `pkg/http/auth/` |
| 登录/登出 | `pkg/http/login_handler.go`, `pkg/http/logout_handler.go` |
| 用户 CRUD | `pkg/http/user_add_handler.go`, `user_update_handler.go`, `user_delete_handler.go` |
| 用户列表/资料 | `pkg/http/user_list_handler.go`, `pkg/http/profile_handler.go` |
| 用户设置 | `pkg/http/set_user_role_handler.go`, `set_user_bandwidth_handler.go`, `set_user_client_limit_handler.go` |
| 端口轮换 | `pkg/http/rotate_port_handler.go`, `rotate_inbound_port_handler.go`, `rotate_all_ports_handler.go` |
| 密码修改 | `pkg/http/change_password_handler.go` |
| Inbound | `pkg/http/bound_handler.go`, `inbound_list_handler.go`, `fastAddInbound_handler.go` |
| 证书 | `pkg/http/cert_handler.go`, `get_certs_handler.go`, `tranfer_cert_handler.go`, `delete_cert_handler.go` |
| 节点 | `pkg/http/node_handler.go`, `node_groups_handler.go`, `node_containers_handler.go` |
| 状态 | `pkg/http/status_handler.go` |
| 订阅 | `pkg/http/sub_handler.go` |
| RPC Client | `pkg/rpc/client/end_node_rpc.go` |
| Proto 定义 | `pkg/rpc/proto/rpc_server.proto` |
