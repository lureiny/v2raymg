# v2raymg HTTP API Reference

> 生成时间: 2026-03-19
> 用于前端页面开发参考

## 技术栈

- **框架**: Gin
- **认证**: `X-Token` Header（大部分接口需要）
- **架构**: Center/End Node 集群模式，HTTP Handler → RPC Client → End Node gRPC Server

---

## API 端点一览

| Method | Path | 认证 | 功能 |
|--------|------|------|------|
| **用户管理** |
| GET | `/user` | X-Token | 获取用户列表 |
| POST | `/user` | X-Token | 添加用户 |
| PUT | `/user` | X-Token | 更新用户（密码/过期时间） |
| DELETE | `/user` | X-Token | 删除用户 |
| POST | `/user/reset` | X-Token | 重置用户代理密钥 |
| DELETE | `/users` | X-Token | 批量删除用户 |
| POST | `/copyUserBetweenNodes` | X-Token | 节点间复制用户 |
| POST | `/rotatePort` | 用户密码 | 用户自助端口轮换 |
| **Inbound 管理** |
| GET | `/inbound` | X-Token | 获取 inbound 配置 |
| POST | `/inbound` | X-Token | 添加 inbound（raw JSON） |
| DELETE | `/inbound` | X-Token | 删除 inbound |
| POST | `/inbound/fast` | X-Token | 快速添加 inbound |
| **节点 & 集群** |
| GET | `/node` | X-Token | 获取集群所有节点 |
| GET | `/tag` | X-Token | 获取节点的所有 inbound tag |
| PUT | `/gateway` | X-Token | 启用/禁用 gateway 模式 |
| PUT | `/pingCheck` | X-Token | 启用/禁用 ping 检测 |
| **证书管理** |
| GET | `/getCerts` | X-Token | 列出证书 |
| POST | `/cert` | X-Token | 申请证书 |
| POST | `/cert/transfer` | X-Token | 传输证书到其他节点 |
| **订阅** |
| GET | `/sub` | 无需 | 获取代理订阅 |
| **其他** |
| POST | `/update` | X-Token | 更新代理二进制版本 |
| POST | `/authHysteria2` | 无需 | Hysteria2 认证回调 |
| GET | `/metrics` | X-Token | Prometheus 指标 |
| GET | `/help/*relativePath` | 无需 | API 帮助信息 |

---

## 核心 Request/Response 结构

### 用户相关

#### POST /user - 添加用户

```json
{
  "target": "节点名",        // 可选，默认本节点
  "user": "用户名",
  "pwd": "密码",
  "tags": "tag1,tag2",      // 绑定的 inbound tags，逗号分隔
  "expire": 1700000000,     // 过期时间戳（与 ttl 二选一）
  "ttl": 86400              // 存活秒数（优先于 expire）
}
```

响应: `200 "Succ"` 或错误信息

#### PUT /user - 更新用户

```json
{
  "target": "节点名",
  "user": "用户名",
  "pwd": "新密码",          // 可选
  "expire": 1700000000,     // 可选
  "ttl": 86400              // 可选（优先）
}
```

#### DELETE /user - 删除用户

```json
{
  "target": "节点名",
  "user": "用户名",
  "tags": "tag1,tag2"       // 可选，指定从哪些 inbound 删除
}
```

#### POST /user/reset - 重置用户密钥

```json
{
  "target": "节点名",
  "user": "用户名"
}
```

#### GET /user - 获取用户列表

Query: `?target=节点名`

响应:
```json
{
  "节点名": [
    {
      "name": "user1",
      "passwd": "xxx",
      "expire_time": 1700000000,
      "tags": ["tag1", "tag2"],
      "downlink": 12345678,
      "uplink": 87654321
    }
  ]
}
```

#### DELETE /users - 批量删除用户

```json
{
  "target": "节点名",
  "users": "user1,user2,user3"
}
```

#### POST /copyUserBetweenNodes - 节点间复制用户

```json
{
  "src_node": "源节点",
  "dst_node": "目标节点"
}
```

#### POST /rotatePort - 用户自助端口轮换

```json
{
  "user": "用户名",
  "pwd": "用户密码"    // 使用用户自身密码鉴权，无需 admin token
}
```

响应:
```json
{"code": 0, "msg": "ok"}
// 或
{"code": 401, "msg": "invalid user or password"}
```

---

### Inbound 相关

#### GET /inbound - 获取 inbound 配置

Query: `?target=节点名&src_tag=inbound-tag`（src_tag 可选，不传则返回全部）

响应: inbound 配置 JSON

#### POST /inbound - 添加 inbound（raw JSON）

```json
{
  "target": "节点名",
  "bound_raw_string": "base64编码的inbound配置JSON"
}
```

#### DELETE /inbound - 删除 inbound

```json
{
  "target": "节点名",
  "src_tag": "inbound-tag"
}
```

#### POST /inbound/fast - 快速添加 inbound

```json
{
  "target": "节点名",
  "tag": "inbound-tag",
  "protocol": "vless",      // vless | vmess | trojan | ss | shadowsocks
  "stream": "tcp",          // tcp | ws | quic | mkcp | grpc | http
  "domain": "example.com",  // 证书域名
  "port": 443,
  "is_xtls": false,
  "self_signed": false,
  "container": "xray"       // xray（默认）| snell
}
```

Protocol 类型:
- `vless` - VLESS 协议
- `vmess` - VMess 协议
- `trojan` - Trojan 协议
- `ss` / `shadowsocks` - Shadowsocks 协议

Stream 类型:
- `tcp` - TCP
- `ws` - WebSocket
- `quic` - QUIC
- `mkcp` - mKCP
- `grpc` - gRPC
- `http` - HTTP/2

---

### 节点 & 集群

#### GET /node - 获取集群节点列表

响应: `[]cluster.Node`

```json
[
  {
    "name": "node1",
    "host": "192.168.1.1",
    "port": 62789,
    "cluster_name": "default"
  }
]
```

#### GET /tag - 获取节点的 inbound tags

Query: `?target=节点名`

响应: `{"节点名": ["tag1", "tag2"]}`

#### PUT /gateway - 设置 gateway 模式

```json
{
  "target": "节点名",
  "enable_gateway_model": true
}
```

#### PUT /pingCheck - 设置 ping 检测

```json
{
  "target": "节点名",
  "enable_ping_check": true
}
```

---

### 证书管理

#### GET /getCerts - 列出证书

Query: `?target=节点名`

响应: `{"节点名": ["domain1.com", "domain2.com"]}`

#### POST /cert - 申请证书

```json
{
  "target": "节点名",
  "domain": "example.com"
}
```

#### POST /cert/transfer - 传输证书

```json
{
  "target": "目标节点名",   // 不含本节点
  "domain": "example.com"
}
```

---

### 订阅

#### GET /sub - 获取订阅

Query 参数:
- `user` - 用户名（必填）
- `pwd` - 密码（必填）
- `target` - 节点名（可选，默认 all）
- `tags` - 过滤 tags，逗号分隔（可选）
- `exclude_protocols` - 排除协议，逗号分隔（可选）
- `use_sni` - 使用 SNI（默认 true）
- `fake` - 返回假数据（默认 false）

响应: 根据 User-Agent 自动转换为对应格式
- Surge 客户端 → Surge 配置
- Clash 客户端 → Clash 配置
- 其他 → base64 编码的 URI 列表

---

### 其他

#### POST /update - 更新代理版本

```json
{
  "target": "节点名",
  "version_tag": "v1.8.0"   // 默认 "latest"
}
```

#### POST /authHysteria2 - Hysteria2 认证

```json
{
  "addr": "客户端地址",
  "auth": "认证密码（明文或base64）",
  "tx": 0
}
```

响应:
```json
{"ok": true, "id": "用户名"}
// 或
403 ""
```

#### GET /metrics - Prometheus 指标

Query: `?target=节点名`

响应: Prometheus 文本格式指标

#### GET /help - API 帮助

- `/help` - 返回所有接口帮助
- `/help/<path>` - 返回指定接口帮助

---

## 数据模型

### User

```go
type User struct {
    Name       string   `json:"name"`        // 用户名
    Passwd     string   `json:"passwd"`      // 密码
    ExpireTime int64    `json:"expire_time"` // 过期时间戳
    Tags       []string `json:"tags"`        // 绑定的 inbound tags
    Downlink   int64    `json:"downlink"`    // 下行流量（字节）
    Uplink     int64    `json:"uplink"`      // 上行流量（字节）
}
```

### Node

```go
type Node struct {
    Name        string `json:"name"`         // 节点名
    Host        string `json:"host"`         // 主机地址
    Port        int32  `json:"port"`         // gRPC 端口
    ClusterName string `json:"cluster_name"` // 集群名
}
```

### BuilderType (协议/传输层类型)

```go
// Protocol
VLESSSettingBuilderType  = 10
VMESSSettingBuilderType  = 11
TrojanSettingBuilderType = 12
SSSettingBuilderType     = 13

// Stream
TCPBuilderType  = 20
WSBuilderType   = 21
QuicBuilderType = 22
MkcpBuilderType = 23
GrpcBuilderType = 24
HttpBuilderType = 25
```

---

## 认证机制

### 1. Admin Token (X-Token)

大部分接口通过 `X-Token` Header 认证:

```http
GET /user?target=node1
X-Token: your-admin-token
```

错误响应: `401 "invalid token"`

### 2. 用户密码鉴权

`/rotatePort` 和 `/sub` 使用用户自身密码:

```json
// /rotatePort
{"user": "username", "pwd": "user_password"}
```

### 3. 无需认证

- `/sub` - 订阅接口（通过 user+pwd 参数）
- `/authHysteria2` - Hysteria2 回调
- `/help` - 帮助信息

---

## 错误处理

成功响应: `200 "Succ"`

失败响应: `200 "node: node1 > err: error message"`

批量操作失败时返回 `joinFailedList`:
```
node: node1 > err: xxx|node: node2 > err: yyy
```

---

## 前端功能模块规划

1. **Dashboard** - 节点状态、集群概览、流量统计
2. **用户管理** - 增删改查、过期时间、流量统计、批量操作
3. **Inbound 管理** - 列表、快速添加、删除、配置查看
4. **证书管理** - 列表、申请、传输
5. **订阅管理** - 生成订阅链接、二维码
6. **系统设置** - Gateway 模式、Ping 检测、版本更新

---

## 代码位置参考

| 模块 | 文件 |
|------|------|
| HTTP Server | `pkg/http/http_server.go` |
| Handler 注册 | `pkg/http/init.go` |
| 用户 Handler | `pkg/http/user_handler.go` |
| Inbound Handler | `pkg/http/bound_handler.go`, `pkg/http/fastAddInbound_handler.go` |
| 节点 Handler | `pkg/http/node_handler.go`, `pkg/http/tag_handler.go` |
| 证书 Handler | `pkg/http/cert_handler.go`, `pkg/http/get_certs_handler.go`, `pkg/http/tranfer_cert_handler.go` |
| 订阅 Handler | `pkg/http/sub_handler.go` |
| 认证 | `pkg/http/auth_with_token_handler.go` |
| RPC Client | `pkg/rpc/client/end_node_rpc.go` |
| Proto 定义 | `pkg/rpc/proto/rpc_server.pb.go` |
| Cluster Node | `pkg/cluster/node.go` |
