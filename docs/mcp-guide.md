# v2raymg 内嵌 MCP Server 使用与开发指南

v2raymg 在现有 HTTP 管理服务中内嵌了符合 **Model Context Protocol (MCP)** 的服务端点 **`/mcp`**，基于官方 Go SDK（`github.com/modelcontextprotocol/go-sdk`）实现，并与 HTTP 管理接口共享同一套执行函数与权限体系。

---

## 1. 架构特点与协议约定

- **单端口复用**：直接挂载在 `end_node.http_port` 的 **`/mcp`** 路径下，无需额外开放端口或单独部署进程。
- **Stateless Streamable HTTP**：采用无状态 Streamable HTTP（`Stateless: true`、`JSONResponse: true`），不在服务端内存中保留长生命周期会话，适合反向代理、服务重启和短连接 AI 客户端：
  - `POST /mcp`：处理标准 MCP JSON-RPC 2.0 请求（`initialize`、`tools/list`、`tools/call` 等）并返回 JSON 响应。
  - `GET /mcp`：无状态模式下按 MCP 规范返回 `405 Method Not Allowed`。
  - `DELETE /mcp`：无状态模式下按 MCP 规范返回 `204 No Content`。
- **同源执行（Single Source of Truth）**：全部 32 个业务接口均基于 `pkg/http/endpoint.go` 的 `Endpoint[Req, Resp]` 实现，HTTP 与 MCP 直接调用同一个 `Execute` 函数，鉴权、默认值、参数校验与多节点 gRPC 扇出行为完全一致。

---

## 2. 启用与配置

MCP 功能默认**关闭**，启用后默认处于**只读模式**。在节点配置文件的 `end_node` 段下添加 `mcp` 配置（参考 [config/config.example.yaml](../config/config.example.yaml)）：

```yaml
end_node:
  listen: 0.0.0.0
  # 建议仅监听本机或通过 HTTPS 反代对外暴露
  http_listen: 127.0.0.1
  http_port: 8080
  http_token: "your-admin-token"
  jwt_secret: "change-this-to-a-random-secret"

  mcp:
    enabled: true    # 是否挂载 /mcp（默认 false；关闭时请求 /mcp 返回 404）
    read_only: true  # 是否仅暴露只读工具（默认 true；设 false 开放全部写操作）
```

启动 `v2raymg server --conf config.yaml` 后，如果配置生效，日志会打印类似确认信息：

```text
level=INFO msg="mcp server registered" read_only=true admin_tools=9 user_tools=3
```

> **安全建议**：
> - 若 AI 客户端与 `v2raymg` 在同一台主机上，建议保持 `http_listen: 127.0.0.1`。
> - 若需远程接入，请务必通过 Nginx / Caddy 等反向代理提供 **HTTPS**，切勿在公网明文传输 `X-Token` 或 JWT。

---

## 3. 鉴权与权限隔离

`/mcp` 复用 HTTP 层的 `AuthMiddleware`，**未提供凭据、凭据错误、JWT 过期或已登出吊销一律返回 `401 Unauthorized`**：

| 鉴权方式 | HTTP Header | 映射角色 | 说明 |
|---|---|---|---|
| Admin 静态令牌 | `X-Token: <end_node.http_token>` | `admin` | 拥有管理员全部工具权限，但**没有用户名上下文**（`Caller.Username` 为空） |
| 用户 JWT | `Authorization: Bearer <jwt>` | `admin` 或 `normal` | 先通过 `POST /api/login` 获取；携带用户名上下文与角色 |

### 不同配置与角色下的工具可见范围

服务端按角色维护隔离的 `adminServer` 与 `userServer`，普通用户在 `tools/list` 中**完全看不到**管理员工具，执行层也会再次校验权限：

| `mcp.enabled` | `mcp.read_only` | Admin (`X-Token` / Admin JWT) | 普通用户 (`normal` JWT) |
|---|---|---|---|
| `false`（默认） | 任意 | `/mcp` 返回 `404` | `/mcp` 返回 `404` |
| `true` | `true`（默认） | **9 个**只读工具（关闭集群同步时为 **8 个**，不含 `get_node_groups`） | **3 个**只读工具：`get_node_status`、`list_users`、`get_profile` |
| `true` | `false` | **32 个**工具（9 只读 + 23 写；关闭集群同步时为 **30 个**；9 个高危写工具带 `DestructiveHint: true`） | **7 个**工具：3 只读 + `rotate_inbound_port`、`rotate_all_ports`、`change_password`、`reset_auth_token` |

> **注意**：`get_profile`、`change_password`、`reset_auth_token` 需要用户身份上下文。若使用 `X-Token`（无用户名）调用这三个工具，会返回 `isError: true`（`"JWT required"`）；管理员查看或维护他人账户请改用 `list_users`、`update_user`、`reset_user`、`rotate_inbound_port`（传 `username`）等工具。

---

## 4. 安全护栏与敏感字段脱敏

1. **只读优先默认值**：`end_node.mcp.enabled` 默认 `false`，`end_node.mcp.read_only` 默认 `true`，即使开启也不允许 AI 执行任何变更操作，需显式配置 `read_only: false` 才开放写工具。
2. **代理凭据脱敏（`MCPSanitize`）**：`list_users`、`get_profile` 与 `reset_auth_token` 返回前会在 MCP 适配层深拷贝响应，并将非空的 `auth_token` 统一替换为 `"[REDACTED]"`，避免代理明文密钥进入大模型上下文（原生 HTTP API 行为保持不变，仍返回明文）。
3. **高危注解（`ToolAnnotations`）**：
   - 所有 9 个只读工具标记 `ReadOnlyHint: true`。
   - 9 个破坏性/高影响写工具（`delete_user`、`reset_user`、`reset_user_traffic`、`reset_auth_token`、`rotate_all_ports`、`change_password`、`delete_inbound`、`delete_cert`、`update_node_binary`）标记 `DestructiveHint: true`，配合客户端提示人工确认。
4. **故意不暴露的 6 个 HTTP 路由**：
   - `POST /api/login`、`POST /api/logout`（凭据应由客户端 Header 注入，避免密码/登出进入对话历史）
   - `POST /api/authHysteria2`（Hysteria2 内核鉴权回调）
   - `GET /sub`（聚合订阅文本体量大且含明文节点凭据）
   - `GET /help/*relativePath`（MCP `tools/list` 已内置 Schema 与描述）
   - `GET /api/metrics`（Prometheus 原文体量大，应改用结构化 `get_node_status`）

---

## 5. 客户端接入指南

### 5.1 支持 Streamable HTTP 的 MCP 客户端通用配置

适用于支持远程 Streamable HTTP MCP 的客户端（如 Claude Code、Cursor、Codex 等）：

**管理员模式（`X-Token`）**：

```json
{
  "mcpServers": {
    "v2raymg": {
      "type": "http",
      "url": "https://panel.example.com/mcp",
      "headers": {
        "X-Token": "your-admin-http-token"
      }
    }
  }
}
```

**用户 JWT 模式（通过 `POST /api/login` 获取 `token`）**：

```json
{
  "mcpServers": {
    "v2raymg-user": {
      "type": "http",
      "url": "https://panel.example.com/mcp",
      "headers": {
        "Authorization": "Bearer <your-jwt-token>"
      }
    }
  }
}
```

### 5.2 反向代理配置提示（Nginx 示例）

如果通过 Nginx 对外发布管理接口，需要透传认证头和 `Accept` 头，并关闭代理缓冲：

```nginx
location /mcp {
    proxy_pass http://127.0.0.1:8080/mcp;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Token $http_x_token;
    proxy_set_header Authorization $http_authorization;
    proxy_buffering off;
}
```

### 5.3 使用 `curl` 快速验证

> Streamable HTTP 要求客户端请求携带 `Accept: application/json, text/event-stream`。

**1) 列出当前可用工具（`tools/list`）**：

```bash
curl -sS -X POST "http://127.0.0.1:8080/mcp" \
  -H "X-Token: your-admin-http-token" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
  }' | jq .
```

**2) 调用只读工具查询全集群节点状态（`get_node_status`）**：

```bash
curl -sS -X POST "http://127.0.0.1:8080/mcp" \
  -H "X-Token: your-admin-http-token" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "get_node_status",
      "arguments": {}
    }
  }' | jq .
```

---

## 6. 全量 MCP 工具参考（32 个）

### 通用调用约定

- **`target` 默认值差异（重要）**：
  - **只读工具**不传 `target` 时默认 `"all"`（扇出查询全部可用节点）；可传入指定节点名或 `node_id`。
  - **写操作工具**不传 `target` 时默认仅作用于**当前直连节点**（`CopyUserBetweenNodes` 使用 `src_node` / `dst_node`，群组管理使用必填 `name`）；如需广播全集群，请显式传 `"target": "all"`。
- **返回与错误格式**：
  - 调用成功时，`result.content[0].text` 为序列化后的 JSON 字符串。
  - 任何校验失败、无节点可用、权限不足或业务错误，均会返回 MCP `isError: true`（包含兼容旧 HTTP 语义返回 `HTTP 200 + code=300/404` 的错误路径）。

### 6.1 只读工具（9 个，`read_only: true` 下可用）

| MCP 工具名 | 对应 HTTP 路由 | 权限 | 参数说明 | 功能与注意事项 |
|---|---|---|---|---|
| `get_node_status` | `GET /api/status` | `ScopeUser` | `target?`（默认 `"all"`） | 查询节点内存、CPU、网络收发/速率、TCP 连接数、Goroutine 与版本 |
| `list_users` | `GET /api/user` | `ScopeUser` | `target?`（默认 `"all"`） | Admin 返回全量用户；普通用户仅返回自身（`auth_token` 已脱敏为 `"[REDACTED]"`） |
| `get_profile` | `GET /api/profile` | `ScopeUser`（需 JWT） | `target?`（默认 `"all"`） | 返回当前登录用户的完整 profile 与 inbound 端口（`auth_token` 已脱敏） |
| `list_nodes` | `GET /api/node` | `ScopeAdmin` | 无 | 获取集群节点目录（含 `node_id`、`duplicate_name`、`groups`） |
| `list_containers` | `GET /api/containers` | `ScopeAdmin` | `target?`（默认 `"all"`） | 获取目标节点已启用的 container 类型（`xray`/`mihomo`/`hysteria`/`snell`） |
| `list_inbounds` | `GET /api/inbounds` | `ScopeAdmin` | `target?`（默认 `"all"`） | 列出全部 container 的 inbound 摘要 |
| `get_inbound` | `GET /api/inbound` | `ScopeAdmin` | `target?`（默认 `"all"`）、`src_tag?`、`container?`（默认 `"xray"`） | 获取 inbound 详细配置（当前仅支持 `container: "xray"`） |
| `list_certs` | `GET /api/getCerts` | `ScopeAdmin` | `target?`（默认 `"all"`） | 列出目标节点上的 TLS 证书信息 |
| `get_node_groups` | `GET /api/node/:name/groups` | `ScopeAdmin`（集群启用） | `name`（必填） | 获取指定节点所属 group 列表 |

### 6.2 写操作工具（23 个，需 `end_node.mcp.read_only: false`）

#### 用户与自助管理（12 个）

| MCP 工具名 | 对应 HTTP 路由 | 权限 | Destructive | 关键参数与说明 |
|---|---|---|---|---|
| `add_user` | `POST /api/user` | `ScopeAdmin` | 否 | `user`（必填）、`pwd?`、`ttl?`/`expire?`（`ttl` 优先）、`role?`、`group?`、`upload_bps?`、`download_bps?`、`max_clients?`、`client_recycle_delay_sec?`、`client_drain_sec?`、`target?` |
| `update_user` | `PUT /api/user` | `ScopeAdmin` | 否 | `user`（必填），其余同 `add_user`；配额字段 `0` 表示保持不变，`-1` 表示取消限制 |
| `delete_user` | `DELETE /api/user` | `ScopeAdmin` | **是** | `user`（必填）、`target?`；标记删除用户并回收端口规则 |
| `reset_user` | `POST /api/user/reset` | `ScopeAdmin` | **是** | `user`（必填）、`target?`；重置用户代理凭据并重建端口规则 |
| `reset_user_traffic` | `POST /api/user/reset-traffic` | `ScopeAdmin` | **是** | `user`（必填）、`target?`；清零用户累计持久化流量计数 |
| `reset_auth_token` | `POST /api/user/reset-token` | `ScopeUser`（需 JWT） | **是** | `target?`；轮换自身代理 `auth_token`（MCP 返回中 `auth_token` 同样脱敏为 `"[REDACTED]"`） |
| `set_user_role` | `PUT /api/user/:name/role` | `ScopeAdmin` | 否 | `name`（必填）、`role`（`"admin"` \| `"normal"`，必填）、`target?` |
| `set_user_bandwidth` | `PUT /api/user/:name/bandwidth` | `ScopeAdmin` | 否 | `name`（必填）、`upload_bps?`、`download_bps?`（至少传其一；显式传 `0` 取消限速，省略保持不变）、`target?` |
| `set_user_client_limit` | `PUT /api/user/:name/client-limit` | `ScopeAdmin` | 否 | `name`（必填）、`max_clients`（必填；`0` 取消限制）、`recycle_delay_sec?`、`drain_sec?`、`target?` |
| `rotate_inbound_port` | `POST /api/rotateInboundPort` | `ScopeUser` | 否 | `container`（必填）、`inbound`（必填）、`port?`（`0` 随机）、`username?`（`X-Token` 必填，普通 JWT 仅能操作自己）、`target?` |
| `rotate_all_ports` | `POST /api/rotateAllPorts` | `ScopeUser` | **是** | `username?`（`X-Token` 必填，普通 JWT 仅能操作自己）、`target?`；轮换用户全部 inbound 转发端口 |
| `change_password` | `PUT /api/profile/password` | `ScopeUser`（需 JWT） | **是** | `old_password`（必填）、`new_password`（必填）、`target?` |

#### Inbound 管理（3 个）

| MCP 工具名 | 对应 HTTP 路由 | 权限 | Destructive | 关键参数与说明 |
|---|---|---|---|---|
| `add_inbound` | `POST /api/inbound` | `ScopeAdmin` | 否 | `bound_raw_string`（必填，Base64 编码的 xray 原生 inbound JSON）、`container?`（仅 `"xray"`）、`target?` |
| `delete_inbound` | `DELETE /api/inbounds` | `ScopeAdmin` | **是** | `container`（必填：`xray`/`snell`/`hysteria`/`mihomo`）、`name`（必填，inbound tag）、`target?` |
| `fast_add_inbound` | `POST /api/inbound/fast` | `ScopeAdmin` | 否 | 支持 `container`（`xray`/`mihomo`/`hysteria`/`snell`）、`protocol`（`vless`/`vmess`/`trojan`/`ss`/`shadowsocks`/`hysteria2`/`tuic`/`anytls`）、`transport`、`security`（`tls`/`reality`/`xtls`/`none`）、`port`、`domain`、`self_signed`、`tag`、`target` 及各类 Reality / SS 插件 / Hysteria2 / TUIC / AnyTLS 专用参数 |

#### 证书与集群节点控制（8 个）

| MCP 工具名 | 对应 HTTP 路由 | 权限 | Destructive | 关键参数与说明 |
|---|---|---|---|---|
| `apply_cert` | `POST /api/cert` | `ScopeAdmin` | 否 | `domain`（必填）、`target?`；申请 TLS 证书 |
| `transfer_cert` | `POST /api/cert/transfer` | `ScopeAdmin` | 否 | `domain`（必填）、`target?`；将本机已有证书传输到指定 peer 节点 |
| `delete_cert` | `DELETE /api/cert` | `ScopeAdmin` | **是** | `domain`（必填）、`target?`；删除 TLS 证书 |
| `set_node_groups` | `PUT /api/node/:name/groups` | `ScopeAdmin`（集群启用） | 否 | `name`（必填）、`groups`（必填 `[]string`）；替换节点所属 groups |
| `set_gateway` | `PUT /api/gateway` | `ScopeAdmin` | 否 | `enable_gateway_model`（必填 `bool`）、`target?`；切换 `only_gateway` 运行状态 |
| `set_ping_check` | `PUT /api/pingCheck` | `ScopeAdmin` | 否 | `enable_ping_check`（必填 `bool`）、`target?`；启停节点 Ping 探测 |
| `copy_user_between_nodes` | `POST /api/copyUserBetweenNodes` | `ScopeAdmin` | 否 | `src_node?`、`dst_node?`（不可相同）；将源节点用户复制到目标节点 |
| `update_node_binary` | `POST /api/update` | `ScopeAdmin` | **是** | `version_tag?`（默认 `"latest"`）、`target?`；下载更新代理内核二进制并触发重启，高危操作 |

---

## 7. 开发者指南：新增接口一步接入 HTTP + MCP

后续新增 HTTP API 时，无需单独编写两套处理函数或维护重复的参数解析/校验，只需三步：

### 1) 定义请求结构体与 `Endpoint`

```go
type MyActionReq struct {
    // 路径参数：始终最后绑定，覆盖 body/query 同名字段
    Name string `json:"name" path:"name" jsonschema:"Target node or user name"`
    // 查询参数 + 默认值：HTTP 与 MCP 共享 default 语义
    Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target node name, node_id, or 'all'"`
    // 可选 JSON 字段务必加 omitempty，否则 jsonschema-go 会将其标为 required
    Limit *int `json:"limit,omitempty" jsonschema:"Optional limit value"`
}

func (s *HttpServer) myActionEndpoint() Endpoint[MyActionReq, map[string]any] {
    return Endpoint[MyActionReq, map[string]any]{
        Method:         http.MethodPost,
        Path:           "/example/:name",
        Scope:          ScopeAdmin,       // ScopePublic | ScopeUser | ScopeAdmin
        MCPName:        "run_my_action",  // 留空表示不暴露给 MCP
        MCPDescription: "Clear description of what this tool does for LLM callers",
        ReadOnly:       false,
        Destructive:    false,
        OptionalBody:   true,             // 允许 POST/PUT/DELETE 空 body
        MCPSanitize:    nil,              // 若返回含敏感字段，传入深拷贝脱敏函数
        Execute: func(ctx context.Context, caller Caller, req MyActionReq) (map[string]any, error) {
            if req.Name == "" {
                return nil, NewEndpointErr(http.StatusBadRequest, "name is required")
            }
            return map[string]any{"code": 0, "msg": "ok"}, nil
        },
    }
}
```

### 2) 在 `pkg/http/init.go` 注册一次

```go
registerEndpoint(s, adminGroup, s.myActionEndpoint())
```

这会自动完成：
1. 在 Gin 路由中绑定 HTTP handler（自动按 Method 绑定 JSON/Form Body、Query、`default` 与 Path，并保留 `/help/*` 支持）；
2. 自动加入 `s.mcpRegistrars`，在 `RegisterMCP(s)` 时生成带 JSON Schema 与 `ToolAnnotations` 的 MCP Tool。

> 如果仍有测试直接构造并调用旧的 `*Handler.handlerFunc`，可保留该结构体并委托给 `endpointHandlerFunc(handler.getHttpServer().myActionEndpoint())(c)`。

### 3) 遵循两项 CI 门禁测试

仓库已配置两条防漂移测试，新增路由后需同步更新并验证：
1. **HTTP 集成覆盖守卫**（`go test ./pkg/http -run TestRouteCoverage -v`）：在 `pkg/http/testdata/e2e_covered_routes.txt` 与 `pkg/proxy/systemtest/cluster_e2e_api_test.go` 添加对应路由覆盖用例。
2. **MCP 路由防漂移守卫**（`go test ./pkg/http -run TestMCP_RouteDriftGuard -v`）：所有注册到 Gin 的路由都必须经过 `registerEndpoint` 登记（或明确列入 `nonMCPAllowlist`），确保不会出现“加了 HTTP 接口却漏接 MCP”的情况。

---

## 8. 常见问题排查

- **请求 `/mcp` 返回 `404`**：检查配置文件 `end_node.mcp.enabled` 是否设为 `true` 并已重启服务。
- **请求 `/mcp` 返回 `401`**：检查 `X-Token` 是否与配置一致，或 `Authorization: Bearer <jwt>` 是否有效、未过期且未执行过 `/api/logout`。
- **`curl` 调用返回 `406 Not Acceptable`**：`StreamableHTTPHandler` 要求必须同时包含 `Accept: application/json, text/event-stream`。
- **调用 `get_profile` / `change_password` / `reset_auth_token` 返回 `JWT required`**：说明使用的是静态管理员 `X-Token`（无用户名上下文）；请改用通过 `POST /api/login` 签发的 `Bearer JWT`。
- **`tools/list` 看不到写工具**：检查 `end_node.mcp.read_only` 是否为 `false`，且使用的不是 `normal` 普通用户 JWT。
- **`tools/list` 看不到 `get_node_groups` / `set_node_groups`**：这两个工具仅在开启集群用户同步（`cluster_user.enabled: true`）时注册。
