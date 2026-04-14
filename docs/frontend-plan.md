# v2raymg 前端接口扩展方案

## 目标

为 v2raymg 增加前端管理界面所需的后端接口，支持普通用户和管理员登录，分离权限。

---

## 一、用户角色模型

User 是全局概念，与底层 xray/v2ray 代理容器解耦。

**role 存储**：在 `users` 表新增 `role` 列（migration），默认值 `normal`，不单独建表。同时在 `contracts.UserSpec` 中增加 `Role` 和 `LoginPassword` 字段，由 `user_store.go` 的 Load/Save 一并处理。

**角色说明**：
- `admin`：管理员，拥有全部接口权限
- `normal`：普通用户，只能访问有限的个人接口

---

## 二、双密码字段设计

### 2.1 字段职责

| 字段 | 用途 | 格式 | 说明 |
|------|------|------|------|
| `password` | 代理协议层（Hysteria2 等） | 明文 | 保持现状，不做任何改动 |
| `login_password` | 前端登录认证 | SHA256+bcrypt | 新增字段，与协议层完全解耦 |

**原则**：两套密码独立管理，互不干扰。`password` 字段的语义和用法不变，存储端继续保留明文以供 Hysteria2 等协议直接使用。

### 2.2 login_password 初始化（启动时阻塞执行）

服务启动时，对所有 `login_password` 为空的用户，自动用 `password` 字段的值做 SHA256+bcrypt 后填入 `login_password`，**一次性完成**，完成后 `password` 字段不再变更：

```
启动 → 遍历 users 表 → login_password 为空？
  → 是：SHA256(password) → bcrypt → 写入 login_password（兼容迁移）
  → 否：跳过
```

此过程阻塞，**且必须在 UserManager 加载用户之前完成**，保证内存中的用户对象已携带 `login_password`，无需重启即可登录。

**兼容性说明**：`login_password` 初始值来源于 `password`（代理协议密码），保证现有用户无需任何操作即可直接使用原密码登录前端，实现无缝切换。后续计划支持独立订阅地址（不依赖固定用户密码），届时 `login_password` 可单独修改，与 `password` 完全解耦。迁移逻辑只在 `login_password` 为空时触发，不会覆盖已有值。

### 2.3 login_password 实时同步

- **AddUser**：新建用户时，同步根据 `password` 生成 `login_password`，用户创建后立即可登录 `/login`，无需重启。
- **UpdateUser / UpdateUserPassword**：修改 `password` 时，同步更新 `login_password`。新密码立即生效，旧密码立即失效。

---

## 三、密码存储与验证

### 3.1 login_password 存储格式

```
原始密码 → SHA256 → bcrypt → login_password 字段
```

- **SHA256**：确定性输出，固定 64 字符 hex string。中间人拦截到的是该服务的派生 hash，无法还原原始密码用于其他服务
- **bcrypt**：慢 hash + 自动内嵌 salt，防彩虹表/暴力破解

### 3.2 写入时处理（新建/修改登录密码）

```go
func isSHA256Hex(s string) bool {
    matched, _ := regexp.MatchString(`^[0-9a-f]{64}$`, s)
    return matched
}

func hashLoginPassword(input string) (string, error) {
    sha256hex := input
    if !isSHA256Hex(input) {
        h := sha256.Sum256([]byte(input))
        sha256hex = hex.EncodeToString(h[:])
    }
    hash, err := bcrypt.GenerateFromPassword([]byte(sha256hex), bcrypt.DefaultCost)
    return string(hash), err
}
```

### 3.3 登录验证

客户端传入的密码可以是**明文**或**SHA256 hex**，后端统一处理：

```go
func verifyLoginPassword(stored, input string) bool {
    sha256hex := input
    if !isSHA256Hex(input) {
        h := sha256.Sum256([]byte(input))
        sha256hex = hex.EncodeToString(h[:])
    }
    return bcrypt.CompareHashAndPassword([]byte(stored), []byte(sha256hex)) == nil
}
```

---

## 四、双 Token 认证架构

### 4.1 静态 Token（X-Token，break-glass admin）

- 来源：现有 `config.yaml` 中的 `end_node.http_token` 字段
- 用途：**break-glass 紧急入口**，不依赖任何已有 admin 用户。典型场景：系统刚部署，尚无任何 admin 用户时，用 X-Token 调用 `/user/:name/role` 将第一个普通用户提升为 admin。
- 权限：等同于 `role=admin`，但不携带 `username`（不代表具体用户）
- 注意：X-Token 不能访问 `/profile`（该接口需要具体用户信息）

### 4.2 临时 Token（JWT）

- 来源：用户调用 `POST /login` 登录后颁发
- 用途：前端正常操作，区分普通用户和管理员
- 包含字段：`username`, `role`, `expire`, `jti`（用于 logout 黑名单）
- 过期时间：可配置（如 24 小时）

---

## 五、权限控制原则

**后端是唯一授权来源。** 前端根据 `role` 显示不同页面（管理员面板 vs 普通用户面板），但前端的 role 判断仅用于 UI 展示，不构成访问控制。所有敏感接口在后端通过 `adminOnly` 中间件独立校验，普通用户即使伪造请求也会被拦截（403）。

**JWT role 防篡改**：`role` 字段在 JWT payload 中由服务端签名（HS256），客户端无法在不知道 `jwt_secret` 的情况下修改。`jwt_secret` 泄露即等同于系统沦陷，需妥善保管（不可提交至代码仓库）。

**jwt_secret 配置要求**：`end_node.jwt_secret` 必须在配置文件中显式设置，最小长度 16 个字符。服务启动时 `Validate()` 会 fail-fast，不满足条件则拒绝启动。`jwt_secret` 为空时 middleware 也会拒绝所有 Bearer JWT，双重保护。

---

## 六、接口清单（新增 + 改造）

### 6.1 新增接口

| Method | Path | 认证 | 角色 | 功能 |
|--------|------|------|------|------|
| POST | `/login` | 无 | 公开 | 用户名+密码登录，返回 JWT |
| POST | `/logout` | JWT | 用户 | 登出（加入内存黑名单） |
| GET | `/profile` | JWT（仅） | 用户 | 获取当前登录用户信息；X-Token 不可用 |
| PUT | `/user/:name/role` | X-Token 或 admin JWT | 管理员 | 设置用户角色（admin/normal） |

### 6.2 现有接口改造（加权限控制）

| Method | Path | 认证 | 角色 | 功能 |
|--------|------|------|------|------|
| GET | `/user` | JWT | 管理员 | 获取用户列表 |
| POST | `/user` | JWT | 管理员 | 添加用户 |
| PUT | `/user` | JWT | 管理员 | 更新用户 |
| DELETE | `/user` | JWT | 管理员 | 删除用户 |
| POST | `/user/reset` | JWT | 管理员 | 重置用户密钥 |
| DELETE | `/users` | JWT | 管理员 | 批量清理用户 |
| GET | `/node` | JWT | 管理员 | 获取节点列表 |
| GET/POST/DELETE | `/inbound*` | JWT | 管理员 | inbound 管理 |
| GET | `/inbounds` | JWT | 管理员 | inbound 列表（新端点） |
| DELETE | `/inbounds` | JWT | 管理员 | 按名称删除 inbound |
| POST | `/rotateInboundPort` | JWT 或 X-Token | 用户 | 普通用户可轮换自己的指定 inbound 端口；admin/X-Token 可指定用户 |
| POST | `/rotateAllPorts` | JWT 或 X-Token | 用户 | 普通用户可轮换自己的全部端口；admin/X-Token 可指定用户 |
| POST | `/cert` | JWT | 管理员 | 申请证书 |
| POST | `/cert/transfer` | JWT | 管理员 | 传输证书 |
| POST | `/update` | JWT | 管理员 | 升级 proxy |
| POST | `/copyUserBetweenNodes` | JWT | 管理员 | 复制用户 |
| PUT | `/gateway` | JWT | 管理员 | 开关 gateway 模式 |
| PUT | `/pingCheck` | JWT | 管理员 | 开关 ping 检测 |
| GET | `/metrics` | X-Token | 管理员 | Prometheus metrics（保留静态 token） |

### 6.3 无需改动的公开接口

| Method | Path | 认证 | 角色 | 功能 |
|--------|------|------|------|------|
| GET | `/sub` | 无 | 公开 | 获取订阅 |
| GET | `/help/*relativePath` | 无 | 公开 | 帮助 |
| GET | `/getCerts` | JWT 或 X-Token | 管理员 | 获取证书列表 |
| POST | `/authHysteria2` | 无 | 公开 | Hysteria2 认证（继续使用 password 明文字段） |

---

## 七、登录流程

```
POST /login
  body: {"username": "xxx", "password": "xxx"}  // password 可以是明文或 SHA256 hex

  1. 验证用户是否存在（通过 userLister.GetUser）
  2. verifyLoginPassword(user.login_password, input)
  3. 读取 user.role（来自 UserSpec，Load 时已从 DB 读入）
  4. 生成 JWT（包含 username, role, expire, jti）
  5. 返回 200:
     {"token": "eyJ...", "expire": 1743000000, "role": "admin"|"normal", "username": "xxx"}

  错误情况（用户不存在 / 密码错误统一返回，不区分，防止枚举）:
     401 Unauthorized

前端访问 API 时:
  Header: Authorization: Bearer <token>
```

---

## 八、接口响应格式

### POST /login（200）

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expire": 1743000000,
  "role": "normal",
  "username": "alice"
}
```

### GET /profile（200）

```json
{
  "username": "alice",
  "role": "normal",
  "expiry_time": "2026-12-31T00:00:00Z",
  "traffic_limit": 107374182400,
  "traffic_used_uplink": 1024000,
  "traffic_used_downlink": 2048000
}
```

- `expiry_time`：零值输出为空字符串，表示永不过期
- `traffic_limit`：0 表示无限制，单位 bytes
- 管理员和普通用户返回相同结构，`role` 字段不同

## 九、UserLister 接口扩展

`login handler` 和 `profile handler` 需要按用户名查询完整 `UserSpec`（含 `LoginPassword`、`Role`），需在 `UserLister` 接口中新增方法：

```go
type UserLister interface {
    ListUsersWithPasswd() map[string]string
    GetUser(username string) (*contracts.User, bool)  // 新增
}
```

`GetUser` 由 `usermanager.UserManager` 实现，返回内存中已加载的用户对象。

---

## 十、路由保护中间件

### 10.1 认证中间件 `authMiddleware`

```go
func authMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // 1. 优先检查 X-Token
        if token := c.GetHeader("X-Token"); token != "" {
            if token == httpServer.token {
                c.Set("role", "admin")
                c.Next()
                return
            }
        }

        // 2. 检查 Bearer JWT
        if authHeader := c.GetHeader("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
            jwtToken := parseJWT(authHeader[7:])
            if jwtToken.Valid && !blacklist.Contains(jwtToken.JTI) {
                c.Set("username", jwtToken.Username)
                c.Set("role", jwtToken.Role)
                c.Next()
                return
            }
        }

        c.String(401, "Unauthorized")
        c.Abort()
    }
}
```

### 10.2 角色检查中间件 `adminOnly`

```go
func adminOnly() gin.HandlerFunc {
    return func(c *gin.Context) {
        role, _ := c.Get("role")
        if role != "admin" {
            c.String(403, "Admin only")
            c.Abort()
            return
        }
        c.Next()
    }
}
```

### 10.3 Logout 黑名单（内存态）

```go
type tokenBlacklist struct {
    mu  sync.RWMutex
    set map[string]struct{}  // key: jti
}
```

进程重启后清空，可接受（JWT 本身有过期时间兜底）。

---

## 十一、数据库变更（Migration）

新增两个字段，通过 migration 机制追加：

```sql
-- migration version 6: add role and login_password to users
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'normal';
ALTER TABLE users ADD COLUMN login_password TEXT NOT NULL DEFAULT '';
```

启动时执行 migration，migration 完成后立即执行 login_password 初始化（见第二节 2.2）。

---

## 十二、文件改动清单

### 12.1 新增文件

| 文件 | 内容 |
|------|------|
| `pkg/http/auth/jwt.go` | JWT 签发、验证、解析、jti 生成 |
| `pkg/http/auth/middleware.go` | authMiddleware、adminOnly 中间件 |
| `pkg/http/auth/password.go` | SHA256 + bcrypt hash/验证逻辑 |
| `pkg/http/auth/blacklist.go` | logout token 内存黑名单 |
| `pkg/http/login_handler.go` | `POST /login` |
| `pkg/http/logout_handler.go` | `POST /logout` |
| `pkg/http/profile_handler.go` | `GET /profile` |
| `pkg/http/set_user_role_handler.go` | `PUT /user/:name/role`（X-Token 专用）|

### 12.2 改动文件

| 文件 | 改动 |
|------|------|
| `pkg/store/migrations/migrations.go` | 新增 migration version 6：`role` + `login_password` 列 |
| `pkg/store/user_store.go` | Load/Save 增加 role、login_password 字段读写 |
| `pkg/store/manager.go` | 启动后执行 login_password 初始化（阻塞） |
| `pkg/proxy/core/contracts/user.go` | `UserSpec` 增加 `Role string`、`LoginPassword string` 字段 |
| `pkg/proxy/appconfig/` | 新增 `JWTSecret string`、`JWTExpireHours int` 字段解析 `end_node.jwt_secret`/`jwt_expire_hours` |
| `pkg/http/http_handler.go` 或 `UserLister` 定义处 | `UserLister` 接口新增 `GetUser(username string) (*contracts.User, bool)` 方法 |
| `pkg/http/init.go` | 注册新 handler，切换全局中间件 |
| `pkg/http/http_server.go` | 注入 token（现有字段）、jwtSecret、jwtExpireHours |
| `pkg/http/rotate_port.go` | 移除 body 中的 `user`/`pwd` 参数，改从 JWT context 取 username，通过 `login_password` 验证 |

### 12.3 无需改动

- `pkg/rpc/proto/rpc_server.proto`
- `pkg/http/auth_hysteria.go`（继续用 `password` 明文字段）

---

## 十三、实现顺序

1. **migration** — 新增 `role`、`login_password` 列（version 6）
2. **`pkg/proxy/core/contracts/user.go`** — `UserSpec` 增加 `Role`、`LoginPassword` 字段
3. **`pkg/store/user_store.go`** — Load/Save 支持新字段
4. **`pkg/store/manager.go`** — 启动时 login_password 初始化
5. **`pkg/proxy/appconfig/`** — 新增 JWTSecret、JWTExpireHours 配置字段解析
6. **`pkg/http/auth/password.go`** — SHA256 + bcrypt 工具函数
7. **`pkg/http/auth/blacklist.go`** — logout 黑名单
8. **`pkg/http/auth/jwt.go`** — JWT 签发与验证
9. **`pkg/http/auth/middleware.go`** — authMiddleware + adminOnly
10. **`UserLister` 接口** — 新增 `GetUser` 方法，`usermanager.UserManager` 实现
11. **`pkg/http/login_handler.go`** — `POST /login`
12. **`pkg/http/logout_handler.go`** — `POST /logout`
13. **`pkg/http/profile_handler.go`** — `GET /profile`
14. **`pkg/http/set_user_role_handler.go`** — `PUT /user/:name/role`
15. **`pkg/http/rotate_port.go`** — 移除 body 参数，从 JWT context 取 username
16. **`pkg/http/init.go`** — 注册新 handler，切换全局中间件
17. **`pkg/http/http_server.go`** — 注入 token（现有）、jwtSecret、jwtExpireHours

---

## 十四、前端页面建议

**普通用户面板**：
- 登录/登出
- 查看个人信息（`GET /profile`）
- 修改密码（后续可增加 `PUT /user/password`）
- 订阅信息（`GET /sub`）
- 端口轮换（`POST /rotateAllPorts`、`POST /rotateInboundPort`）

**管理员面板**：
- 用户管理（CRUD + role 设置）
- 节点管理
- Inbound 管理
- 证书管理
- 系统设置（gateway / pingCheck）
- 监控看板

---

## 十五、配置参考（config.yaml）

JWT 相关配置新增在 `end_node` 块下（与现有 `http_token` 同级）：

```yaml
end_node:
  name: node-1
  http_port: 8080
  http_token: "your-x-token-here"   # 管理员静态 token（现有字段）
  jwt_secret: "your-jwt-secret"     # JWT 签名密钥（新增）
  jwt_expire_hours: 24              # token 过期时间，单位小时（新增，默认 24）
  # ... 其余字段保持不变
```

**安全提示**：`jwt_secret` 泄露后任何人可伪造任意角色的 JWT。应使用足够随机的长字符串（≥32 字节），不可提交至代码仓库，建议通过环境变量或密钥管理工具注入。

---

## 十六、CLI 工具双认证支持

### 16.1 目标

CLI 工具（`cmd/cli`）同时支持两种认证方式，优先使用密码登录获取 JWT，降级使用静态 token。

### 16.2 配置文件（`.v2raymg-tools.yaml`）

```yaml
host: "http://127.0.0.1:8080"
token: ""         # 静态 X-Token（与服务端 http_token 对应）
username: ""      # 登录用户名（配置后优先使用 JWT 登录）
password: ""      # 登录密码
```

两种模式互斥，使用优先级：
1. `username` + `password` 均非空 → JWT 模式（优先）
2. 仅配置 `token` → 静态 X-Token 模式（降级）

### 16.3 认证流程

```
getAuthToken() 被调用时：
  username + password 已配置？
    → 是：检查 JWT 缓存（是否距过期 >60s）
           → 命中：直接返回 "Bearer <cached_token>"
           → 未命中：调用 POST /login
                       → 成功：缓存 token + expire，返回 "Bearer <jwt>"
                       → 失败：降级返回静态 token（X-Token）
    → 否：返回静态 token（X-Token）
```

### 16.4 HTTP 请求头处理

`base_client.go` 的 `setAuthHeader()` 根据 token 前缀自动选择请求头：

```go
func setAuthHeader(req *http.Request, token string) {
    if strings.HasPrefix(token, "Bearer ") {
        req.Header.Set("Authorization", token)  // JWT 模式
    } else if token != "" {
        req.Header.Set("X-Token", token)         // 静态 token 模式
    }
}
```

### 16.5 JWT Token 缓存

缓存保存在进程内存中，CLI 进程退出即清除（CLI 是短生命周期交互工具，无需持久化）：

```go
var jwtCache struct {
    mu     sync.Mutex
    token  string
    expire int64  // Unix timestamp
}
```

每次调用 `getAuthToken()` 时检查 `expire - now > 60s`，避免临界过期。

### 16.6 首次配置交互

`.v2raymg-tools.yaml` 不存在时，CLI 提示用户输入，支持两种模式的引导：

```
please input host:
please input token (leave empty to use username/password login):
  → 留空时继续提示：
    please input username:
    please input password:
```

### 16.7 涉及文件

| 文件 | 改动 |
|------|------|
| `cmd/cli/common/const.go` | 新增 `Login = "login"` 常量 |
| `cmd/cli/client/http_client.go` | 新增 `Login(host, username, password)` 函数 |
| `cmd/cli/client/base_client.go` | 新增 `setAuthHeader()`，替换所有 `X-Token` 直接赋值 |
| `cmd/cli/config.go` | `config` 新增 `Username`/`Password` 字段；新增 `jwtCache`；新增 `getAuthToken()` |
| `cmd/cli/command.go` | 所有 `getToken()` 替换为 `getAuthToken()` |
| `cmd/cli/info.go` | 所有 `getToken()` 替换为 `getAuthToken()` |
