# v2raymg HTTP API Reference

> 更新时间: 2026-11-27 (v5 - 新增服务端链式代理、两层 Tag 物理隔离、PUT /api/inbounds/tags、TUIC/AnyTLS、GET /api/metrics 与内嵌 /mcp)
> 基于 `pkg/http/init.go`、`pkg/http/metric_handler.go`、`pkg/http/mcp.go` 及全部 Handler 源码生成

## 技术栈

- **框架**: Gin
- **认证**: JWT Bearer Token + X-Token（向后兼容）
- **架构**: Center/End Node 集群模式，HTTP Handler → RPC Client → End Node gRPC Server
- **AI 接入**: 内嵌 Stateless Streamable HTTP MCP Server（`/mcp`），与 HTTP 接口共享统一 `Endpoint[Req, Resp]` 执行抽象

---

## 认证机制

### 1. JWT Bearer Token（推荐）

通过 `/api/login` 获取 JWT，后续请求在 Header 中携带：

```http
Authorization: Bearer <jwt-token>
```

JWT 中包含 `username` 和 `role`（`admin` 或 `normal`），决定接口权限。

### 2. X-Token（管理员直通）

使用配置文件中的 `end_node.http_token`（v2.10+ 留空时自动生成并持久化到 `local_secrets`，仅在首次生成时打印日志），通过 `X-Token` Header 鉴权：

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

大部分接口支持 `target` 参数，用于指定操作的目标节点（同时接受节点 `name` 或 `node_id`，当集群中存在重名节点 `duplicate_name: true` 时建议传入 `node_id`）：

| 值 | 行为 |
|----|------|
| 空/不传 | **写操作**（增删改、重置、轮换等）默认当前节点；**跨节点读操作**（`GET /api/user`、`GET /api/profile`、`GET /api/inbounds`、`GET /api/containers`、`GET /api/metrics`、`GET /sub`）默认 `"all"` |
| `all` | 所有可用节点 |
| 节点名 / `node_id` | 指定节点 |

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
| POST | `/api/user/reset-traffic` | Admin | 重置用户累计持久化流量（节点自治） |
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
| POST | `/api/inbound/fast` | Admin | 快速添加 inbound（支持 direct / chain 链式代理与 access_tags） |
| GET | `/api/inbounds` | Admin | 列出所有 inbound（跨容器，含类型、access_tags 与脱敏链式摘要） |
| PUT | `/api/inbounds/tags` | Admin | 动态更新 inbound 的 Layer 2 访问控制标签（`access_tags`），免重启生效 |
| DELETE | `/api/inbounds` | Admin | 删除 inbound（按容器+名称） |
| **证书管理** |
| GET | `/api/getCerts` | Admin | 列出证书 |
| POST | `/api/cert` | Admin | 申请证书 |
| POST | `/api/cert/transfer` | Admin | 传输证书到其他节点 |
| DELETE | `/api/cert` | Admin | 删除证书 |
| **节点 & 集群** |
| GET | `/api/node` | Admin | 获取集群所有节点 |
| GET | `/api/containers` | Admin | 获取节点已启用的 container 列表及 `supports_chain` 能力 |
| GET | `/api/status` | User | 获取节点状态指标 |
| PUT | `/api/gateway` | Admin | 启用/禁用 gateway 模式 |
| PUT | `/api/pingCheck` | Admin | 启用/禁用 ping 检测 |
| GET | `/api/node/:name/groups` | Admin | 获取节点分组（需启用集群同步） |
| PUT | `/api/node/:name/groups` | Admin | 设置节点分组（需启用集群同步） |
| **订阅** |
| GET | `/sub` | Public | 获取代理订阅 |
| **其他** |
| POST | `/api/update` | Admin | 更新代理二进制版本 |
| POST | `/api/authHysteria2` | Public | Hysteria2 认证回调（含两层 Tag 访问校验） |
| GET | `/api/metrics` | User/Admin | Prometheus 监控指标（需开启 `enable_prometheus`） |
| GET | `/help/*relativePath` | Public | API 帮助信息 |
| POST/GET/DELETE | `/mcp` | User/Admin | 内嵌无状态 MCP Server（需开启 `end_node.mcp.enabled`） |

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

**Query:** `?target=节点名`（省略或为空时默认 `"all"`，支持节点名或 `node_id`）

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
      "client_recycle_delay_sec": 60,
      "client_drain_sec": 2,
      "group": "default",
      "inbound_tags": ["home-sh"]
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

**两层 Tag 物理隔离与访问控制说明:**
- **Layer 1（Node 层）**：用户 `group` ↔ 节点 `groups`（控制用户在哪些节点可见）。
- **Layer 2（Inbound 层）**：用户 `inbound_tags` ↔ 入站 `access_tags`（控制用户在可见节点上能使用哪些受限入站；`access_tags` 为空的入站对本节点所有可见用户公开，非空时需与用户 `inbound_tags` 存在非空交集）。两层物理隔离，允许同名重用且绝不交叉穿透。
- 普通用户视图（需 JWT 认证；`X-Token` 为 admin 角色，返回 Admin 视图）中 `inbounds` 仅返回同时通过 Layer 1 与 Layer 2 校验并已分配 Forward 端口的入站；管理员控制面可通过 `GET /api/inbounds` 查看全量入站。

---

#### GET /api/profile - 获取当前用户资料

**Query:** `?target=节点名`（省略或为空时默认 `"all"`）

需要 JWT 认证（使用 `X-Token` 请求无用户上下文，返回 `401 "JWT required"`）。所有角色返回完整资料（含 `inbounds`、流量详情），格式同上面的普通用户视图；其中 `inbounds` 同样受 Layer 1（节点 `group`）与 Layer 2（入站 `access_tags` / 用户 `inbound_tags`）过滤。

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
  "inbound_tags": ["home-sh"],
  "upload_bps": 1000000,
  "download_bps": 500000,
  "max_clients": 5,
  "client_recycle_delay_sec": 60,
  "client_drain_sec": 2
}
```
- `expire` 和 `ttl` 可选，`ttl` 优先
- `inbound_tags`（可选，`[]string`）：Layer 2 入站访问标签数组；每个标签需匹配 `^[a-zA-Z0-9._-]{1,64}$`（自动去除首尾空白、去重并升序排序，非法标签返回 `400`）。省略或传 `[]` 表示仅可访问未设置 `access_tags` 的公开入站
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
  "inbound_tags": ["home-sh"],
  "upload_bps": 1000000,
  "download_bps": 500000,
  "max_clients": 5,
  "client_recycle_delay_sec": 60,
  "client_drain_sec": 2
}
```
- `inbound_tags`（`*[]string` 三态语义）：**省略或传 `null`** 保持原样；传 **`[]`** 清空全部 Layer 2 标签；传非空数组则全量替换并即时重算各容器 Forward 端口分配/回收（规则同 `^[a-zA-Z0-9._-]{1,64}$`，非法返回 `400`）
- 配额字段（`upload_bps` / `download_bps` / `max_clients` / `client_recycle_delay_sec` / `client_drain_sec`）：`0` 表示保持不变，`-1` 表示取消限制

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

#### POST /api/user/reset-traffic - 重置用户累计流量

清零指定用户在目标节点上的持久化累计流量计数器（`traffic_total_uplink` / `traffic_total_downlink`）。

- **节点自治**：每个目标节点各自清零本地累计值；集群同步不会把流量字段从对端写回本地，所以调用方需要对每个目标节点分别下发（通过 `target`）才能实现全集群重置。
- **精准 drain**：重置时会把 forward 层 counter 锚定进 statsCollector 的 prev 基准，下一轮采集从 0 开始，不会把重置前未采集的流量回灌到新周期。
- 重置前的累计值没有保留快照；如需历史请在外部统计系统（Prometheus / 业务方）侧保留。

**Request:**
```json
{
  "target": "节点名",
  "user": "用户名"
}
```

字段：
- `user`：必填，用户名；为空返回 `400`。
- `target`：可选，语义见 [target 参数说明](#target-参数说明)。

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

**错误码：**
- `400`：`user` 为空或请求体无效。
- `500`：任一目标节点执行失败（本次请求在首个失败处短路，之前已成功重置的节点**不会回滚**；需要逐节点重置以获得 per-node 粒度的成功/失败反馈）。

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
  "inbound_type": "direct",
  "chain_uri": "",
  "access_tags": [],
  "remark": "",
  "flow": "",
  "skip_cert_verify": false,
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
| `target` | string | 否 | 目标节点名或 `node_id`，空则为本节点 |
| `tag` | string | 否 | inbound 唯一标识，空则自动生成；需匹配 `^[a-zA-Z0-9._-]{1,64}$`，禁止以保留前缀 `__` 开头，并在全容器与持久化存储间全局查重 |
| `protocol` | string | 否 | 入站协议类型，默认 `vless`；支持 `vless` \| `vmess` \| `trojan` \| `ss`/`shadowsocks` \| `hysteria2` \| `tuic` \| `anytls`（后三者仅限 `container: "mihomo"`） |
| `transport` | string | 否 | 传输层协议，默认 `tcp`（也可用 `stream` 字段，向后兼容；`hysteria2`/`tuic`/`anytls` 留空） |
| `domain` | string | 否 | TLS 证书域名；为空时自动使用自签证书 |
| `port` | int | 否 | 监听端口，0 则随机分配 |
| `security` | string | 否 | 安全类型，默认 `tls`；支持 `tls` \| `reality` \| `xtls` \| `none` |
| `self_signed` | bool | 否 | 是否使用自签证书（仅 TLS） |
| `container` | string | 否 | 容器类型，默认 `xray`；支持 `xray` \| `mihomo` \| `hysteria` \| `snell` |

**链式代理与两层 Tag 隔离字段（Chain Proxy & Layer 2 Access Control）:**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `inbound_type` | string | 否 | `"direct"`（默认）或 `"chain"`（1:1 绑定上游代理出站的链式入口）。省略 `inbound_type` 但传入非空 `chain_uri` 时自动归一为 `"chain"` |
| `chain_uri` | string | 条件必填 | 上游代理 URI；`inbound_type="chain"` 时必填，`"direct"` 时禁止传入（违反均返回 `400`）。支持 `vless://`、`vmess://`、`trojan://`、`ss://`、`hysteria2://`/`hy2://`、`tuic://`、`anytls://`、`socks5://`、`http://`/`https://`（受目标容器能力约束） |
| `access_tags` | []string | 否 | Layer 2 访问控制标签数组（需匹配 `^[a-zA-Z0-9._-]{1,64}$`）。为空表示对本节点可见用户全部公开；非空表示仅拥有交集 `inbound_tags` 的用户可使用 |
| `remark` | string | 否 | 可选展示备注；`chain` 入站省略时自动取 `chain_uri` 的 `#fragment`，仍为空时回退为 `tag` |

**链式代理关键约束与安全策略:**
- **1:1 不可变绑定**：仅允许在**新建 inbound** 时绑定 `inbound_type="chain"` 与 `chain_uri`，不支持原地修改已有 inbound 的类型或后端 URI（如需更换上游，请先 `DELETE /api/inbounds` 再新建）。
- **容器能力矩阵**（可通过 `GET /api/containers` 的 `details[].supports_chain` 查询）：
  - `mihomo`（`supports_chain: true`）：支持 `vless`、`vmess`、`trojan`、`ss`、`hysteria2`/`hy2`、`tuic`、`anytls`、`socks5`、`http`/`https` 后端出站；入站同时支持 `vless`/`vmess`/`trojan`/`ss`/`hysteria2`/`tuic`/`anytls`。
  - `xray`（`supports_chain: true`）：支持 `vless`、`vmess`、`trojan`、`ss`（不支持外部 SIP003 plugin）、`socks5`、`http`/`https` 后端出站；**不支持** `hysteria2`/`tuic`/`anytls` 出站；且因 Xray 26.2.6+ 移除了 `allowInsecure`、`network: "http"`（`h2`）、`security: "xtls"` 及 legacy XTLS flow，含此类参数的 `chain_uri` 会被直接拒绝，请改用 `mihomo` 容器（或将传输改为 `xhttp`）。
  - `hysteria` / `snell`（`supports_chain: false`）：不支持创建 `chain` 入站，但完整支持 `access_tags` 与 `PUT /api/inbounds/tags`。
- **Loopback 收口与 Fail-Closed**：`FastAddInbound` 默认监听地址为 `127.0.0.1`；`inbound_type="chain"` 时**强制锁定 `listen` 与 `listen_addr` 均为 `127.0.0.1`**（若 `extra_params` 传入其他监听地址将直接拒绝），用户流量统一经 Forward 层复用既有用户级限速与流量统计。后端断连时严格 Fail-Closed，绝不回落直连。
- **后端地址准入策略（方案 A）**：无条件拦截未指定地址（`0.0.0.0/8`、`::`）、非标准/尾部点号数值 IPv4（如 `127.1`、`2130706433`、`0x7f000001`、`127.0.0.1.`）、多播/广播、链路本地/云元数据段（`169.254.0.0/16`、`fe80::/10`）以及本机管理端口和已分配端口；默认放行公网、Tailscale（`100.64.0.0/10`）、RFC1918 与 IPv6 ULA（`fc00::/7`）；默认拒绝 loopback（`127.0.0.0/8`、`::1`、`localhost`），仅在节点配置显式开启 `end_node.allow_loopback_chain_target: true` 时放行（开启后仍硬拦截管理端口与已分配端口）；域名按原样保留以支持动态 DDNS（Xray 26+ 出站设 `targetStrategy: "AsIs"`）。

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
| `flow` | string | vless | VLESS flow，如 `xtls-rprx-vision`（常配合 Reality 使用） |
| `sniffing_enabled` | bool | 全部 | 启用流量嗅探 |
| `skip_cert_verify` | bool | 全部 | 订阅侧覆盖参数，为 `true` 时客户端配置统一输出 `skip-cert-verify` |

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
| `method` / `cipher` | string | Shadowsocks 加密方式（mihomo 默认 `2022-blake3-aes-256-gcm`，SIP022 系列要求 base64 密钥；non-SIP022 用 hex 字符串） |
| `password` | string | Trojan/Shadowsocks 密码 |
| `udp` | "true"/"false" | Shadowsocks UDP 开关，默认 `true` |
| `utls_fingerprint` | string | Reality 客户端 fingerprint，如 `chrome` |
| `skip_cert_verify` | "true"/"false" | 订阅侧跳过证书校验提示 |

**Shadowsocks 插件便捷字段**（仅 mihomo container 接入）:

| 字段 | 类型 | 说明 |
|------|------|------|
| `plugin` | string | `obfs` / `v2ray-plugin` / `shadow-tls` |
| `plugin_mode` | string | obfs: `http`/`tls`；v2ray-plugin: `websocket`/`quic` |
| `plugin_host` | string | obfs/v2ray-plugin/shadow-tls 的目标 host |
| `plugin_path` | string | v2ray-plugin path |
| `plugin_tls` | bool | v2ray-plugin TLS 开关 |
| `plugin_password` | string | shadow-tls 密码 |
| `plugin_version` | string | shadow-tls 版本（`2` / `3`） |

> shadow-tls 是网络层 wrapper proxy，**不会下发到 mihomo SS listener**；服务端只跑 plain SS，shadow-tls 信息只写入订阅 Extensions 用于客户端 Clash 配置生成。

**Hysteria2 便捷字段**（仅 mihomo container 接入）:

| 字段 | 类型 | 说明 |
|------|------|------|
| `obfs` | string | 当前仅支持 `salamander`；空值代表不开 obfs |
| `obfs_password` | string | obfs=salamander 时必填 |
| `up` | string | 上行带宽宣告，如 `50 Mbps`（mihomo 字符串解析） |
| `down` | string | 下行带宽宣告，如 `100 Mbps` |
| `masquerade` | string | 服务端伪装，URL（`https://...`）/ `file:/path` / `proxy:...`；**仅服务端**,不下发到客户端配置 |
| `ignore_client_bandwidth` | bool | 服务端忽略客户端宣告的 up/down,使用 BBR 拥塞控制；可与 up/down 共存 |

> Hysteria2 是 QUIC over UDP；mihomo 容器创建 hy2 inbound 时 forward 层自动用 UDP 转发规则（其它协议保持 TCP 默认）。证书与 vless/vmess/trojan 共用 `cert_file`/`key_file`/`certificate`/`key`/`domain`/`self_signed` 来源（mihomo yaml 中映射为 `certificate`/`private-key`）。
>
> Hysteria2 的标准 `hysteria2://` URI 仅携带 `obfs`/`obfs-password`/`sni`/`insecure`/`pinSHA256`；`up`/`down`/`masquerade` 不在 URI 标准中,通过订阅 Extensions 透传到客户端 Clash 配置。

**TUIC 便捷字段**（仅 mihomo container 接入）:

| 字段 | 类型 | 说明 |
|------|------|------|
| `congestion_controller` | string | 拥塞控制算法：`bbr` \| `cubic` \| `new_reno` |
| `udp_relay_mode` | string | UDP 中继模式：`native` \| `quic` |
| `zero_rtt_handshake` | bool | 客户端侧提示参数，开启 0-RTT 握手（`reduce-rtt`） |
| `heartbeat_interval` | string | 客户端侧心跳间隔，如 `"10s"` 或纯毫秒数 |
| `disable_sni` | bool | 客户端侧提示参数，关闭 SNI |

**AnyTLS 便捷字段**（仅 mihomo container 接入）:

| 字段 | 类型 | 说明 |
|------|------|------|
| `padding_scheme` | string | 服务端内联 padding scheme 文本（仅服务端） |
| `idle_session_check_interval_seconds` | int | 客户端侧空闲会话检测间隔秒数（`0` 使用运行时默认值） |
| `idle_session_timeout_seconds` | int | 客户端侧空闲会话超时秒数（`0` 使用运行时默认值） |
| `min_idle_session` | int | 客户端侧最小空闲会话数（`0` 使用运行时默认值） |

**Protocol:** `vless` | `vmess` | `trojan` | `ss` / `shadowsocks` | `hysteria2` | `tuic` | `anytls`

**Transport:** `tcp` | `ws` | `h2`（`http` 自动 normalize 为 `h2`）| `grpc` | `httpupgrade` | `xhttp` | `splithttp`（Hysteria2 / TUIC 是 QUIC 固定，AnyTLS 是 TCP 原生，三者无需填写 transport 字段）

> 注意:mihomo container 的 FastAdd 支持范围按协议收窄。HTTP 层接受的 transport 是跨 container 的全集,具体组合以表格为准。

**支持的协议组合:**

| 协议 | 传输 | 安全 | 备注 |
|------|------|------|------|
| VLESS(mihomo) | tcp, ws, grpc, xhttp/splithttp | none, tls, reality | |
| VMess(mihomo) | tcp, ws, grpc | none, tls, reality | |
| Trojan(mihomo) | tcp, ws, grpc | tls, reality | TLS 必填 cert；reality 必填 server_names + short_ids |
| Shadowsocks(mihomo) | tcp | none | 默认 cipher `2022-blake3-aes-256-gcm`；可选 obfs / v2ray-plugin / shadow-tls 插件 |
| Hysteria2(mihomo) | QUIC(固定,不接受 transport 字段) | tls(reality / none 拒绝) | 必填 cert；可选 obfs=salamander、up/down 带宽宣告、ignore_client_bandwidth、masquerade(server-only) |
| TUIC(mihomo) | QUIC(固定,不接受 transport 字段) | tls | 必填 cert；Forward 层走 UDP；可选 `congestion_controller` / `udp_relay_mode` 及客户端侧 hints |
| AnyTLS(mihomo) | TCP(原生,无需 transport 字段) | tls | 必填 cert；可选 `padding_scheme`(server-only) 及客户端侧空闲会话参数 |
| Xray | 见 xray 协议矩阵 | 见 xray 协议矩阵 | `inbound_type="chain"` 时仅支持 `vless`/`vmess`/`trojan`/`ss`(无外部插件)/`socks5`/`http` 出站 |

Trojan+TLS 需要提供证书来源(`cert_file/key_file`、`certificate/key`、`domain` 或 `self_signed`)；Trojan+Reality 使用 `reality-config`，不需要也不会由 `FillDefaults` 强制补 TLS 证书。

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

**示例 - 链式代理（mihomo + VLESS Reality 入口 → Hysteria2 家宽出口 + Layer 2 标签）:**
```json
{
  "target": "node1",
  "container": "mihomo",
  "tag": "chain-home-sh",
  "protocol": "vless",
  "transport": "tcp",
  "security": "reality",
  "reality_target": "www.microsoft.com:443",
  "reality_server_names": ["www.microsoft.com"],
  "flow": "xtls-rprx-vision",
  "inbound_type": "chain",
  "chain_uri": "hy2://user:pass@sh-home.ddns.example:28443?sni=bing.com&insecure=1&mport=20000-40000#SH-Home",
  "access_tags": ["home-sh", "internal-vip"]
}
```

**示例 - 链式代理（xray + VLESS Reality 入口 → SOCKS5/VLESS 出口）:**
```json
{
  "target": "node1",
  "container": "xray",
  "tag": "chain-office-socks",
  "protocol": "vless",
  "transport": "tcp",
  "security": "reality",
  "reality_target": "www.microsoft.com:443",
  "reality_server_names": ["www.microsoft.com"],
  "flow": "xtls-rprx-vision",
  "inbound_type": "chain",
  "chain_uri": "socks5://proxyuser:proxypass@100.64.0.12:1080#Office-Exit",
  "access_tags": ["office"]
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

**示例 - Hysteria2 + 自签 TLS + salamander obfs + 带宽宣告（mihomo container）:**
```json
{
  "tag": "hy2-bundled",
  "protocol": "hysteria2",
  "container": "mihomo",
  "port": 443,
  "self_signed": true,
  "obfs": "salamander",
  "obfs_password": "shh-secret",
  "up": "50 Mbps",
  "down": "100 Mbps",
  "masquerade": "https://www.microsoft.com"
}
```
> Hysteria2 不接受 `transport` 字段（QUIC 固定）；`security` 默认 tls，显式 `reality`/`none` 会被拒绝。

**示例 - Trojan+gRPC+Reality:**
```json
{
  "tag": "trojan-grpc-reality",
  "protocol": "trojan",
  "transport": "grpc",
  "security": "reality",
  "password": "optional-password",
  "grpc_service_name": "TrojanTunnel",
  "reality_target": "www.microsoft.com:443",
  "reality_server_names": ["www.microsoft.com"],
  "reality_short_ids": ["aabbccddeeff0011"],
  "utls_fingerprint": "chrome"
}
```

**示例 - Shadowsocks（mihomo，默认 SIP022 cipher）:**
```json
{
  "tag": "ss-2022",
  "protocol": "shadowsocks",
  "container": "mihomo",
  "port": 8388
}
```
> 不传 `cipher` / `password` 时，`FillDefaults` 会自动设置 `2022-blake3-aes-256-gcm` cipher 并生成对应的 base64(32 bytes) 密码。

**示例 - Shadowsocks+obfs:**
```json
{
  "tag": "ss-obfs",
  "protocol": "shadowsocks",
  "container": "mihomo",
  "port": 8389,
  "cipher": "aes-256-gcm",
  "password": "your-password",
  "plugin": "obfs",
  "plugin_mode": "http",
  "plugin_host": "cdn.example.com"
}
```

**示例 - Shadowsocks+shadow-tls（仅订阅，server 跑 plain SS）:**
```json
{
  "tag": "ss-stls",
  "protocol": "shadowsocks",
  "container": "mihomo",
  "port": 8390,
  "cipher": "aes-256-gcm",
  "password": "your-password",
  "plugin": "shadow-tls",
  "plugin_host": "www.microsoft.com",
  "plugin_password": "stls-pw",
  "plugin_version": "3"
}
```

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

---

#### GET /api/inbounds - 列出所有 Inbound

跨容器（`xray`、`snell`、`hysteria`、`mihomo`）列出全量 inbound 元数据（管理员控制面不受用户标签过滤）。

**Query:** `?target=节点名`（省略或为空时默认 `"all"`，支持节点名或 `node_id`）

**Response (200):**
```json
{
  "node1": [
    {
      "container": "mihomo",
      "name": "chain-home-sh",
      "inbound_type": "chain",
      "access_tags": ["home-sh", "internal-vip"],
      "remark": "SH-Home",
      "chain_protocol": "hysteria2",
      "chain_target": "sh-home.ddns.example:28443"
    },
    {
      "container": "xray",
      "name": "vless-tcp",
      "inbound_type": "direct"
    }
  ]
}
```
> 注：若存在节点但均无返回条目，返回 `[]`。

**字段说明:**

| 字段 | 类型 | 说明 |
|------|------|------|
| `container` | string | 所属容器类型：`xray` \| `snell` \| `hysteria` \| `mihomo` |
| `name` | string | inbound tag（唯一标识） |
| `inbound_type` | string | `"direct"` 或 `"chain"` |
| `access_tags` | []string | Layer 2 访问控制标签（为空或未设置时省略，表示对节点可见用户公开） |
| `remark` | string | 展示备注（非空时返回） |
| `chain_protocol` | string | 仅 `chain` 入站返回，后端出站协议摘要（如 `hysteria2`、`socks5`） |
| `chain_target` | string | 仅 `chain` 入站返回，脱敏后的 `host:port` 目标地址 |

> **安全脱敏**：`GET /api/inbounds`（及对应 MCP 工具 `list_inbounds`）绝不返回包含密码、UUID、私钥等凭据的原始 `chain_uri`，仅返回脱敏后的 `chain_protocol` 与 `chain_target`。

---

#### PUT /api/inbounds/tags - 动态更新 Inbound 访问控制标签（Layer 2）

支持跨节点、跨全部 4 个容器（`xray` / `snell` / `hysteria` / `mihomo`）在运行期原子替换指定 inbound 的 `access_tags`，**不重启代理进程**、不断开仍合法用户的现有连接，并立即持久化到 SQLite 与双向重算 Forward 端口分配/回收。

**Request:**
```json
{
  "target": "node1",
  "container": "mihomo",
  "name": "chain-home-sh",
  "access_tags": ["home-sh", "internal-vip"]
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `target` | string | 否 | 目标节点名、`node_id` 或 `"all"`（省略时默认本节点） |
| `container` | string | **是** | 所属容器：`xray` \| `snell` \| `hysteria` \| `mihomo` |
| `name` | string | **是** | inbound tag/名称 |
| `access_tags` | []string | 否 | 新的 Layer 2 标签数组（全量替换；传入 `[]` 或省略则清空全部标签，恢复对节点可见用户公开）。标签需匹配 `^[a-zA-Z0-9._-]{1,64}$` |

**Response (200):**
```json
{"code": 0, "msg": "ok"}
```

**Error:**
- `400` `"container is required"` / `"name is required"` / `"invalid access_tags: ..."`
- `500` 节点执行失败（如目标容器不存在、指定 inbound tag 不存在或不支持标签更新）
- `502` `"no available node"`

---

#### DELETE /api/inbounds - 删除 Inbound（按容器+名称）

删除指定 inbound；若为 `inbound_type="chain"` 入站，会同步拆除关联的链式出站与专属路由规则。如需更换 `chain_uri` 或在 `direct` / `chain` 间切换，需先调用本接口删除再通过 `POST /api/inbound/fast` 重建。

**Request:**
```json
{
  "target": "节点名",
  "container": "xray|snell|hysteria|mihomo",
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
    "node_id": "7f3a1c2e-...",
    "duplicate_name": false,
    "groups": ["default", "hk"]
  }
]
```

- `node_id`（2.8 新增）是节点的真实身份：首次启动随机生成并持久化在该节点本地 DB，**不可配置**。
  它跨改名、换 host、换端口保持不变，集群目录就是按它索引的。为空表示该节点尚未与我们完成过
  一次交互（例如刚从配置读入的 `static_nodes` 种子），或对端版本低于 2.8。
- `duplicate_name`（2.8 新增）为 true 表示另有条目使用了同一个 `name`。目录改为按身份索引后，
  重名节点不再互相拒绝注册而是共存，因此 `?target=<name>` 在这种情况下是有歧义的 ——
  所有接受 `target` 的接口都同时接受 `node_id`，可用它精确指定。
- `cluster_name` 字段已于 2026-07 随 cluster name 一并移除。

---

#### GET /api/containers - 获取节点已启用的 Container 列表

**Query:** `?target=节点名`（省略或为空时默认 `"all"`，支持节点名或 `node_id`）

返回指定节点（或全部节点）配置中已启用（`enabled: true`）的 container 类型及其链式出站支持能力。通过 gRPC 向目标节点查询，支持多节点并发。

**Response (200):**
```json
{
  "nodes": {
    "node1": ["xray", "mihomo", "hysteria"],
    "node2": ["xray"]
  },
  "failed": {
    "node3": "connection timeout"
  },
  "details": {
    "node1": [
      {"type": "xray", "supports_chain": true},
      {"type": "mihomo", "supports_chain": true},
      {"type": "hysteria", "supports_chain": false}
    ],
    "node2": [
      {"type": "xray", "supports_chain": true}
    ]
  }
}
```
- `details[].supports_chain`（bool）：标识该容器是否实现 `ChainCapable` 接口、可作为 `POST /api/inbound/fast` 中 `inbound_type="chain"` 的宿主容器（对不支持或未返回能力明细的旧版节点，`details` 可能省略对应条目）。

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
| `proxy_groups_url` | string | 否 | 外部 Clash 代理组配置 URL（远程拉取后合并） |
| `rule_providers_url` | string | 否 | 外部 Clash 规则提供者配置 URL |
| `rules_url` | string | 否 | 外部 Clash 规则配置 URL |
| `sub_userinfo` | bool | 否 | 是否返回 `Subscription-Userinfo` 响应头；省略时回退到节点配置 `subscription.enable_userinfo_header`（默认 `true`） |
| `sub_userinfo_format` | string | 否 | 自定义 `Subscription-Userinfo` 内容的 schema（`${var}` 占位符）；省略时回退到节点配置 `subscription.userinfo_header_format`，再为空时使用内置默认 |

**Response:** 根据 User-Agent 或 `client` 参数自动转换格式：
- Clash → YAML 配置
- Surge → Surge 配置
- 其他 → base64 编码 URI 列表

**Response Header `Subscription-Userinfo`:**

当 `sub_userinfo=true`（或省略 + 节点配置开启）时，响应附带按 schema 渲染后的 `Subscription-Userinfo` header。

**Schema 解析顺序（优先级递减）:**
1. 请求 `?sub_userinfo_format=...` query 参数
2. 节点配置 `subscription.userinfo_header_format`
3. 内置默认：`upload=${upload}; download=${download}; total=${total}; expire=${expire}`（流量值为原始字节数，匹配 Clash / sing-box 等主流客户端解析约定）

> `expire` 与 `total` 都使用 `-1` 作为 "无值 / 无限制" 哨兵：`expire <= 0` 或 `total <= 0` 都渲染为 `-1`，其余渲染原值。`expire` 如需可读形式请使用 `${expire_string}`；`total` 的单位变体（`total_kb` 等）保留计算值，不应用哨兵。
>
> **Clash 客户端特例**：当 User-Agent 或 `client=` 参数匹配 `clash`（不区分大小写,子串匹配,覆盖 Clash/ClashX/Clash.Meta/mihomo 等）时，渲染后的 header 中若包含标准键名 `total=-1` 或 `expire=-1` 的分号分隔段，会被整段移除（即客户端看到的是"无该字段"，而非 `=-1`）。仅作用于字面键名 `total` / `expire`；自定义 schema 中使用其他键名（例如 `quota=${total}`）的段不会被剥离。

**数据来源:**
- `upload` / `download`：跨 `target` 指定的所有节点聚合的累计上下行字节数（拉取失败的节点在该值中被忽略，仅记日志）。
- `total` / `expire`：仅来自访问节点本地的 `TrafficLimit` / `ExpiryTime`。因为当前项目没有全局流量统计与配额一致性层，不同节点对同一用户的配额可能不一致，访问节点的快照仅供参考。

**支持的内置变量:**

| 分类 | 变量 | 说明 |
|------|------|------|
| 流量 | `upload`, `download`, `total` | 原始字节数（整数）；`total <= 0`（如用户无配额）渲染为 `-1`，符合 Clash / sing-box "无限流量" 约定。`upload` / `download` 即使为 0 也照常输出 |
| 流量 | `upload_kb`, `download_kb`, `total_kb` | 字节 / 1024，最多 2 位小数（去尾 0） |
| 流量 | `upload_mb`, `download_mb`, `total_mb` | 字节 / 1024²，最多 2 位小数（去尾 0） |
| 流量 | `upload_gb`, `download_gb`, `total_gb` | 字节 / 1024³，最多 2 位小数（去尾 0） |
| 流量 | `upload_auto`, `download_auto`, `total_auto` | 自动选单位并带后缀：`<1KB` 用整数 `B`，`<1MB` 用 `KB`，`<1GB` 用 `MB`，`≥1GB` 用 `GB`（同样最多 2 位小数去尾 0） |
| 时间 | `expire` | 过期 Unix 秒；无过期（或任何 `<=0` 值）= `-1` |
| 时间 | `expire_string` | 本地时区可读时间 `YYYY-MM-DD hh:mm:ss`；无过期 = `never` |
| 标识 | `username` | 当前用户名 |

未知变量替换为空字符串，并在服务端日志 `WARN` 一次。

**示例:**

```
GET /sub?user=alice&pwd=...&sub_userinfo_format=upload=${upload_auto}; expire=${expire_string}
```

响应可能附带：

```
Subscription-Userinfo: upload=1.53 GB; expire=2026-09-15 14:32:22
```

`sub_userinfo` 取值规则：

| 输入 | 行为 |
|------|------|
| 省略 / 空字符串 | 回退到节点配置 `subscription.enable_userinfo_header` |
| `true` / `1` | 强制返回 header |
| `false` / `0` | 强制不返回 header |
| 其他 | 回退到节点配置（不报错） |

**两层 Tag 隔离与多 Inbound 节点命名:**
- 仅同时通过 **Layer 1**（用户 `group` ↔ 节点 `groups`）与 **Layer 2**（用户 `inbound_tags` ↔ 入站 `access_tags`，`access_tags` 为空则公开）校验且正常启用/分配端口的 inbound 会出现在订阅中（对 `chain` 入站返回其前置入口节点）。
- **统一显示名称规则**：无论 Clash、Surge、Qv2ray 还是通用 Base64 订阅（Shadowrocket / v2rayN / Hiddify 等），集群节点统一显示为 `🌿 <PROTO>_<Name>`（协议全大写：`VLESS` / `VMESS` / `TROJAN` / `SS` / `SNELL` / `HYSTERIA2` / `TUIC` / `ANYTLS`）；其中 `chain` 入站的 `<Name>` 直接使用显式 `remark` 或 `chain_uri` 后端代理名（均为空回退 `tag`），不拼接前置节点名；直连入站为 `<NodeName>-<remark>` 或 `<NodeName>`；通过 `ext_sub` 导入的外部订阅保留原节点名。
- **全局同名去重**：所有格式统一参与去重，唯一名称保持原样，发生重名时全部重复项按 `_01`、`_02`、`_03` 编号（如 `🌿 VLESS_HK_01`、`🌿 VLESS_HK_02`）。

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

> **两层 Tag 门控与 Fail-Closed**：本地鉴权会同步校验 Layer 1（节点组）与 Layer 2（`hysteria-default` 的 `access_tags` ↔ 用户 `inbound_tags`）。若 `hysteria-default` 已禁用、元数据不可用或用户缺少匹配标签，一律 Fail-Closed 返回 `403 {"ok": false}`。

---

#### GET /api/metrics - Prometheus 监控指标

仅在配置启用 `enable_prometheus: true` 时注册，返回 Prometheus 文本格式指标（聚合流量、ping 检测与节点系统指标）。

**Query:** `?target=节点名`（可选；不填则聚合所有可用节点）

**认证:** 需要 `X-Token` 或 `Authorization: Bearer <jwt>`（v2.10 起移除了空 token 绕过，启动日志会在首次自动生成 `http_token` 时打印一次）。

---

#### GET /help - API 帮助

- `/help` - 返回所有接口帮助
- `/help/<path>` - 返回指定接口帮助

---

#### POST / GET / DELETE /mcp - 内嵌无状态 MCP Server

在现有 `end_node.http_port` 上挂载符合 Model Context Protocol 的 Stateless Streamable HTTP 端点 `/mcp`，基于统一 `Endpoint[Req, Resp]` 抽象与 HTTP API 共享执行逻辑、参数校验、鉴权中间件与多节点 gRPC 扇出。

- **启用开关**：配置项 `end_node.mcp.enabled`（默认 `false`，关闭时 `/mcp` 返回 `404`）；`end_node.mcp.read_only`（默认 `true`，仅暴露只读工具；设 `false` 后开放写工具，高危写工具标记 `DestructiveHint: true`）。
- **请求方法**：`POST /mcp` 处理 JSON-RPC 2.0（`initialize`、`tools/list`、`tools/call`）；无状态模式下 `GET /mcp` 返回 `405`，`DELETE /mcp` 返回 `204`。
- **鉴权与隔离**：复用 `AuthMiddleware`（`X-Token` 或 `Authorization: Bearer <jwt>`），按角色物理隔离 `adminServer`（全开 33 个工具 / 只读 9 个）与 `userServer`（全开 7 个工具 / 只读 3 个）。
- **凭证脱敏**：MCP 响应对 `auth_token` 统一替换为 `"[REDACTED]"`，`list_inbounds` 对 `chain` 入站仅返回 `chain_protocol` 与 `chain_target`，绝不返回原始 `chain_uri`。详见 [mcp-guide.md](mcp-guide.md)。

---

## 统一响应格式

所有 API 接口（`/sub`、`/help`、`/api/metrics` 和 `/mcp` 除外）统一使用 JSON 响应。

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
| `400` | 请求参数错误 | 缺少必填字段、格式不正确、参数校验失败（如非法 `inbound_type`/`chain_uri`/`access_tags`） |
| `401` | 未认证 | 未提供 Token、Token 无效或需 JWT 身份上下文 |
| `403` | 权限不足 | 非 Admin 访问管理接口、密码错误 |
| `404` | 资源不存在 | 证书不存在、MCP 未启用（`/mcp`）等 |
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
| Handler 注册 | `pkg/http/init.go`, `pkg/http/http_server.go` |
| 统一 Endpoint 与 MCP | `pkg/http/endpoint.go`, `pkg/http/mcp.go` |
| 认证中间件 | `pkg/http/auth/` |
| 登录/登出 | `pkg/http/login_handler.go`, `pkg/http/logout_handler.go` |
| 用户 CRUD 与资料 | `pkg/http/user_handler.go`, `pkg/http/change_password_handler.go` |
| 用户设置与节点复制 | `pkg/http/set_user_role_handler.go`, `pkg/http/set_user_bandwidth_handler.go`, `pkg/http/set_user_client_limit_handler.go`, `pkg/http/copy_user_between_nodes_handler.go` |
| 端口轮换 | `pkg/http/rotate_inbound_port.go`, `pkg/http/rotate_all_ports.go` |
| Inbound 与 Tag 隔离 | `pkg/http/bound_handler.go`, `pkg/http/inbound_list_handler.go`, `pkg/http/fastAddInbound_handler.go` |
| 链式 URI 解析与准入 | `pkg/proxy/core/chain/` |
| 证书 | `pkg/http/cert_handler.go`, `pkg/http/get_certs_handler.go`, `pkg/http/tranfer_cert_handler.go`, `pkg/http/delete_cert_handler.go` |
| 节点 | `pkg/http/node_handler.go`, `pkg/http/node_groups_handler.go`, `pkg/http/node_containers_handler.go` |
| 状态/开关/指标/升级 | `pkg/http/status_handler.go`, `pkg/http/gateway_handler.go`, `pkg/http/check_server_handler.go`, `pkg/http/metric_handler.go`, `pkg/http/update_handler.go` |
| 订阅与 Hysteria2 认证 | `pkg/http/sub_handler.go`, `pkg/http/sub_userinfo_format.go`, `pkg/http/auth_hysteria.go` |
| 帮助 | `pkg/http/help_handler.go` |
| RPC Client / Server | `pkg/rpc/client/end_node_rpc.go`, `pkg/rpc/server/end_node_inbound.go` |
| Proto 定义 | `pkg/rpc/proto/rpc_server.proto` |
