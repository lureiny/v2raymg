# v2raymg 证书管理重构设计文档（修订版 v2）

> 状态：设计稿 · 2026-03-07
> 作者：AgentTeamLeader (Ryan)

---

## 1. 背景与目标

### 1.1 背景

当前项目在仓库内维护了一份 `lego/` 目录（与 lego CLI 结构基本一致），通过 `app.Run(args)` + flags 拼参数的方式申请/续签证书。现状问题：

| 问题 | 说明 |
|------|------|
| 耦合 CLI 语义 | 业务层需要拼接命令行参数，不是领域对象调用 |
| 可维护性弱 | 上游 lego 升级困难，本地 copy 长期漂移 |
| challenge 策略不透明 | `ObtainNewCert` 固定走 DNS challenge，接口未表达意图 |
| 续签 bug | 现有 `RenewCert` 续签触发条件逻辑反了（见 §3.1） |
| DNS 凭据线程不安全 | 通过 `os.Setenv` 注入凭据，多 goroutine 并发时存在竞争 |

### 1.2 目标

- 在 `pkg/certmgmt/` 下落地新的 library 集成能力
- 直接使用 `go.mod` 中已有的 `github.com/go-acme/lego/v4 v4.9.0`，不引入新依赖
- **不改动现有代码**，旧 `lego/`、`global/lego`、RPC/HTTP handlers 继续运行
- 为后续最小切换准备兼容 facade

### 1.3 非目标

- 本阶段不替换生产调用入口
- 不改现有配置字段含义
- 不做证书中心化分发重构（只预留接口）

---

## 2. Challenge 机制说明

### 2.1 HTTP-01

**原理**：ACME CA 向 `http://<domain>/.well-known/acme-challenge/<token>` 发起 HTTP GET，验证文件内容。

**三种实现模式**（lego 均支持）：

| 模式 | 说明 | lego API |
|------|------|----------|
| 内置 server | lego 自己监听指定端口（默认 `:80`），接受 CA 的 HTTP 校验请求 | `http01.NewProviderServer(host, port)` |
| webroot | 把 challenge 文件写入已有 web server 的静态目录，由现有服务承接 HTTP 请求 | `webroot.NewHTTPProvider(dir)` |
| memcached | 把 challenge token 写入 memcached，由外部 web server 从 memcached 读取返回 | `memcached.NewMemcachedProvider(hosts)` |

**关键约束**：
- 内置 server 模式：域名的 80 端口必须能路由到本进程（且每次签发/续签时都需要，不仅仅是首次）
- webroot 模式：`.well-known/acme-challenge/` 目录必须对外可访问
- 不支持 wildcard 证书（wildcard 只能用 DNS-01）
- **续签时与首次签发完全等价，challenge 校验流程全部重走**

### 2.2 DNS-01

**原理**：在域名的 DNS zone 中添加 `_acme-challenge.<domain>` TXT 记录，CA 通过 DNS 查询验证。

**关键细节**：
- lego DNS provider（如 `alidns`）通过 `dns.NewDNSChallengeProviderByName(name)` 加载
- 凭据**通过环境变量读取**，如 `ALICLOUD_ACCESS_KEY`、`ALICLOUD_SECRET_KEY`，不通过构造参数传入
- 支持 wildcard 证书（`*.example.com`）
- 有 DNS 传播等待时间，可配置 resolver 和 timeout
- **续签时也需重走 DNS challenge，凭据必须有效**

**与 HTTP-01 的选择建议**：

| 场景 | 推荐 |
|------|------|
| 有稳定 DNS API 凭据，不想暴露 80 端口 | DNS-01 |
| 需要 wildcard 证书 | 必须 DNS-01 |
| 简单单域名，80 端口可用 | HTTP-01（内置 server） |
| 已有 nginx/caddy 运行 | HTTP-01（webroot） |

### 2.3 TLS-ALPN-01（备注，本次不实现）

通过 443 端口的 TLS ALPN 扩展验证，适合已占用 443 的场景。本次设计不在实现范围内，预留接口位置。

---

## 3. 已知问题与修正

### 3.1 现有续签触发条件 bug（`lego/cert.go` 第 301 行）

```go
// 现有代码（错误）
if time.Now().Before(cert.ExpireTime) {
    return nil  // 证书未过期就跳过 → 实际上是"过期后才续签"
}
```

正确逻辑应为"距过期剩余不足 N 天时触发续签"：

```go
// 正确逻辑（新模块实现）
const renewBeforeDays = 30
if time.Until(cert.NotAfter) > renewBeforeDays * 24 * time.Hour {
    return nil  // 剩余时间充足，跳过
}
// 否则触发续签
```

> 现有 `lego/` 代码不改动，bug 在新模块中修正。

### 3.2 DNS 凭据环境变量并发竞争

现有 `SetEnvs(certManager.Secrets)` 直接调用 `os.Setenv`，在多 goroutine 并发申请证书时存在 env 竞争。

新模块中对 DNS challenge 的签发/续签操作**加全局序列化锁**（独立于 domain 级锁之上）：

```
dns_global_mutex（整个进程只有一个，保证同一时刻只有一个 DNS challenge 在执行）
├── 设置环境变量
├── 执行 Obtain/Renew
└── 清理环境变量（恢复原值）
```

### 3.3 旧 `lego/` 包的 `init()` 副作用

旧 `lego/cert.go` 的 `init()` 会调用 `initCwd()`（`os.Chdir`）和 `initApp()`（初始化全局 `cli.App`）。

新模块**禁止 import 旧 `lego/` 包**，直接 import `github.com/go-acme/lego/v4`。

---

## 4. 总体架构

```
pkg/certmgmt/
├── domain/
│   ├── types.go          # 领域类型与接口定义
│   └── errors.go         # 结构化错误
├── lego/
│   ├── client_factory.go  # lego.Client 构建（account 初始化）
│   ├── account_store.go   # ACME account key 持久化（独立于 cert key）
│   ├── cert_store.go      # cert/key/resource JSON 持久化
│   ├── solver_http.go     # HTTP-01 provider 装配（内置/webroot/memcached）
│   ├── solver_dns.go      # DNS-01 provider 装配（env 代理 + 全局锁）
│   └── issuer.go          # Obtain/Renew/Revoke 核心流程
└── service/
    ├── manager.go          # 业务服务入口（对齐旧 CertManager 语义）
    └── renew_scheduler.go  # 续签调度（定时检查，提前 N 天触发）
```

---

## 5. 领域模型

### 5.1 Challenge 配置

```go
type ChallengeType string

const (
    ChallengeHTTP01 ChallengeType = "http01"
    ChallengeDNS01  ChallengeType = "dns01"
)

type HTTPChallengeConfig struct {
    Mode        string   // "server"（默认）| "webroot" | "memcached"
    ListenAddr  string   // 内置 server 模式，默认 ":80"
    WebRoot     string   // webroot 模式：静态文件目录
    MemcachedHosts []string // memcached 模式：host:port 列表
    ProxyHeader string   // 反代场景，默认 "Host"
}

type DNSChallengeConfig struct {
    ProviderName               string            // 如 "alidns"、"cloudflare"
    Credentials                map[string]string // key=env var name, value=value（内部转 Setenv）
    Resolvers                  []string          // 自定义 DNS resolver
    DisableCompletePropagation bool
    TimeoutSec                 int // 默认 10
}

type ChallengeConfig struct {
    Type ChallengeType
    HTTP *HTTPChallengeConfig // Type=http01 时有效
    DNS  *DNSChallengeConfig  // Type=dns01 时有效
}
```

### 5.2 签发请求与证书记录

```go
type IssueRequest struct {
    Domains        []string
    Email          string
    Challenge      ChallengeConfig
    KeyType        string // "ec256"（默认）| "rsa2048" | "rsa4096"
    PreferredChain string
    Bundle         bool   // 是否捆绑 issuer cert，默认 true
    RenewBeforeDays int   // 提前 N 天续签，默认 30
}

// CertificateRecord 是对外暴露的证书信息，不含 lego 内部结构
type CertificateRecord struct {
    Domain          string
    CertFile        string    // PEM cert 文件路径
    KeyFile         string    // PEM key 文件路径
    ResourceFile    string    // lego resource JSON 路径（续签需要）
    NotAfter        time.Time
    ObtainedBy      string    // "http01" | "dns01" | "imported"
    ObtainedAt      time.Time
}
```

### 5.3 核心接口

```go
// Issuer 签发器接口
type Issuer interface {
    Issue(ctx context.Context, req IssueRequest) (*CertificateRecord, error)
    Renew(ctx context.Context, record *CertificateRecord, req IssueRequest) (*CertificateRecord, error)
    Revoke(ctx context.Context, record *CertificateRecord) error
}

// Store 证书存储接口（分离 account key 与 cert key）
type Store interface {
    // ACME account 相关（按 CA URL + email 分目录）
    SaveAccount(caURL, email string, account *AccountData) error
    LoadAccount(caURL, email string) (*AccountData, error)

    // 证书相关（按 domain 存储）
    SaveCert(record *CertificateRecord, resource *LegoResource) error
    LoadCert(domain string) (*CertificateRecord, *LegoResource, error)
    ListCerts() ([]*CertificateRecord, error)
}
```

> **说明**：`AccountData` 含 ACME account private key + registration info；`LegoResource` 是 `certificate.Resource` 的持久化形式，含 `CertURL`、`Certificate`（PEM）、`PrivateKey`（PEM）——续签时必须传入。

---

## 6. 关键流程

### 6.1 首次签发（Issue）

```
1. 参数校验（domains 非空、email 合法、challenge config 完整）
2. 加载或创建 ACME account（account_store）
   └── 路径：<basePath>/accounts/<ca-host>/<email>/account.json
   └── key：<basePath>/accounts/<ca-host>/<email>/keys/<email>.key
3. lego.NewClient(config)
4. 装配 challenge provider：
   HTTP: 内置 server / webroot / memcached
   DNS: SetEnv（全局锁保护）→ dns.NewDNSChallengeProviderByName → 执行后清理 env
5. client.Certificate.Obtain(ObtainRequest{Domains, Bundle, KeyType, ...})
6. 解析 NotAfter，构建 CertificateRecord
7. 持久化：
   └── <certPath>/<domain>.crt
   └── <certPath>/<domain>.key
   └── <certPath>/<domain>.json  ← lego resource，续签必须
8. 返回 CertificateRecord
```

### 6.2 续签（Renew）

```
1. 检查触发条件：time.Until(record.NotAfter) ≤ 续期窗口
   └── 续期窗口 = renewBeforeDuration()：RenewBeforeHours(>0,单位小时) 优先
       → 否则 RenewBeforeDays*24h → 否则默认 24h
   └── 未到期且剩余充足 → 直接返回，不续签
   └── service 层(manager.RenewDomain)与 lego 层(issuer.Renew)用同一套
       优先级,避免窗口 > 30 天时被旧的 30 天默认网误杀
2. 从 store 加载 LegoResource（含 CertURL + Certificate PEM）
3. 构建 lego.Client（同签发流程，同样需要 challenge provider）
4. client.Certificate.Renew(certRes, bundle, mustStaple, preferredChain)
   └── 内部走 Obtain 全流程（重新 challenge 验证）
5. 解析新 NotAfter，更新 CertificateRecord
6. 原子替换证书文件（先写临时文件，rename 替换）
7. 返回新 CertificateRecord
```

**重要**：续签时 challenge 校验全部重走。HTTP-01 续签时 80 端口必须可用；DNS-01 续签时凭据必须有效。

### 6.3 并发控制

```
domain_mutex[domain]   ← 防止同一域名并发签发
     +
dns_global_mutex       ← DNS challenge 独占，防止 env 竞争（仅 DNS-01 时加）
```

### 6.4 自动续期调度与各核热重载（已接线）

**调度入口**:`Manager.StartAutoRenew(ctx, cycleSeconds)` 在 `cmd/server.go`
`runEndNode` 的 cert manager 初始化处启动(随服务生命周期 `ctx` 启停)。
此前该方法已实现但**全仓无人调用**,导致证书过期从不自动更新 —— 本次修复的根因。

- **巡检间隔**:硬编码 `renewCheckInterval = 1 * time.Minute`,**不可配置**。
  过期判断纯本地(读 `NotAfter` 比对),开销极低;`cycleSeconds>0` 仅供测试覆盖。
- **续期窗口**:见 6.2,配置项 `renew_before_hours`(小时,默认 24h)。
- **导入证书跳过**:`ObtainedBy=="imported"` 的证书由外部管理(无 ACME
  resource/account),`runRenewCycle` 直接跳过,不会每分钟尝试 ACME 续期。
- **原地替换**:续期走 `SaveCert` 的 temp+rename 原子覆盖,路径稳定为
  `<certPath>/certificates/<domain>.crt|.key`,文件内容变、路径不变。

**各代理核对续期后的证书"零重启"热重载**(已核对上游源码):

| 核 | 续期后动作 | 机制 | 前提 |
|----|-----------|------|------|
| xray | 无 | `transport/internet/tls/config.go`:`OneTimeLoading=false`+双路径时,默认 ~1h ticker 重读文件、比对字节、有变即换。内联字节仅作初始基线 | xray-core 较新版;v2raymg 始终传 `CertificatePath`+`KeyPath` 且不设 `OneTimeLoading` |
| mihomo | 无 | `component/ca` 用 `fswatch`(fsnotify)监听证书文件父目录,改动即重载 | mihomo **≥ v1.19.18**;证书以文件路径下发(非内联) |
| hysteria | 无 | `GetCertificate` 回调每次握手比 ModTime,变了重读 | hysteria2 v2.x |
| snell | 不涉及 | 无 TLS(纯 PSK) | — |

> **设计取舍**:certmgmt 续期后**不**主动重启/重载任何 container —— 各核自身按文件
> 热重载即可,主动重启反而会瞬断连接(尤其 hysteria 的 UDP 会话)。v2raymg 自己的
> `Reload()`/同路径 `PUT /configs` 对证书更新是 no-op,但**无需**它们生效。
>
> **部署前提**:上述"零重启"依赖部署的二进制足够新(项目有二进制自动升级,通常已满足)。
> 线上可用 `xray version` / `mihomo -v` / hysteria 版本确认;并确认续期写入的是
> `certificates/<domain>` 原路径(就地覆盖)。xray 传播延迟最多 ~1h,续期默认提前 24h,余量充足。

---

## 7. 存储目录结构

```
<certPath>/
├── accounts/
│   └── acme-v02.api.letsencrypt.org/
│       └── user@example.com/
│           ├── account.json
│           └── keys/
│               └── user@example.com.key   ← ACME account private key
└── certificates/
    ├── example.com.crt
    ├── example.com.key                     ← cert private key（与 account key 无关）
    └── example.com.json                    ← lego Resource（续签必须）
```

> 与旧 `lego/` 的 `.lego/` 目录**完全独立**，不会互相覆盖。

---

## 8. 配置方案（向后兼容）

新模块内部读取现有配置字段，在 `service.Manager` 初始化时做映射：

```go
// 新配置字段（可选，未配置时按 challenge.type 决定）
type Config struct {
    Email   string
    Path    string
    Challenge struct {
        Type string // "dns01"（默认，对齐旧行为）| "http01"
        DNS  struct {
            ProviderName               string
            Credentials                map[string]string // key=env_var_name
            Resolvers                  []string
            DisableCompletePropagation bool
            TimeoutSec                 int
        }
        HTTP struct {
            Mode           string // "server" | "webroot" | "memcached"
            ListenAddr     string
            WebRoot        string
            MemcachedHosts []string
            ProxyHeader    string
        }
    }
    RenewBeforeDays int // 默认 30
}
```

**兼容规则**：
- 未配置 `challenge.type` → 默认 `dns01` + `cert.dns_provider` + `cert.secrets`
- 现有 `cert.secrets` 中的 key/value 直接作为 DNS env var 注入

---

## 9. 兼容 Facade（Phase B 接入点）

`service.Manager` 提供与旧 `lego.CertManager` 完全相同的方法签名：

```go
func (m *Manager) ObtainNewCert(domain string) error
func (m *Manager) RenewCert(domain string) error
func (m *Manager) GetCert(domain string) *lego.Certificate
func (m *Manager) GetAllCert() []*proto.Cert
func (m *Manager) AddCertificates(domain string, keyData, certData []byte) error
func (m *Manager) AutoRenewCert(cycle int64)
```

Phase B 切换时只需改 `global/lego/cert.go` 的 `InitCertManager` 改为返回新 `Manager`，其余调用点不变。

---

## 10. 错误分类

```go
var (
    ErrConfigInvalid              = errors.New("cert: invalid config")
    ErrChallengeSetup             = errors.New("cert: challenge setup failed")
    ErrPortBind                   = errors.New("cert: cannot bind HTTP port (80 occupied?)")
    ErrDNSProviderNotFound        = errors.New("cert: unknown DNS provider")
    ErrDNSProviderAuth            = errors.New("cert: DNS provider auth failed")
    ErrDNSPropagationTimeout      = errors.New("cert: DNS propagation timeout")
    ErrACMERateLimited            = errors.New("cert: ACME rate limited")
    ErrACMEAccountNotRegistered   = errors.New("cert: ACME account not registered")
    ErrStorageIO                  = errors.New("cert: storage I/O error")
    ErrCertResourceMissing        = errors.New("cert: resource JSON missing, cannot renew")
)
```

---

## 11. 安全设计

1. **DNS 凭据**：优先从配置读取，通过 `os.Setenv` 注入（DNS challenge 期间），执行完毕后恢复原值或 `os.Unsetenv`。不落盘明文凭据（现有 config.yaml 的 secrets 字段由用户自行保护）。
2. **文件权限**：account key、cert key 统一 `0600`；目录 `0700`。
3. **日志脱敏**：不打印 API key、HMAC、token 原文。
4. **Challenge server 生命周期**：HTTP-01 内置 server 只在 challenge 窗口内存活，验证完毕后立即关闭。
5. **原子写文件**：续签时先写 `<domain>.crt.tmp`，完成后 `rename` 替换，避免 xray 读到半写文件。

---

## 12. 测试与验收

### 12.1 单元测试

- 配置校验逻辑
- challenge config 各模式的 provider 构建
- store 读写、路径、权限
- domain mutex + dns global mutex 并发安全
- 续签触发条件（剩余 N 天阈值）

### 12.2 集成测试

推荐使用 [Pebble](https://github.com/letsencrypt/pebble) 搭建本地 ACME 环境：
- HTTP-01：内置 server 模式 + webroot 模式各一条链路
- DNS-01：使用 `challtestsrv` mock DNS provider

### 12.3 验收标准

- `pkg/certmgmt` 内通过 API 完成签发/续签（http01 与 dns01 各 1 条成功链路）
- 错误可归类到 `ErrXxx` 类型
- 不影响现有 `lego/` 证书流程
- `go test ./pkg/certmgmt/...` 全绿

---

## 13. 风险与规避

| 风险 | 规避方案 |
|------|----------|
| DNS 传播延迟导致 challenge 失败 | 可配 `DisableCompletePropagation`；增加重试退避 |
| 80 端口被占用（HTTP-01） | 启动前探测端口，明确报 `ErrPortBind`；建议使用 webroot 模式 |
| ACME 限流（Let's Encrypt 每周 5 个重复域名） | 增加 staging CA URL 配置项；记录签发历史 |
| 旧 `.lego/` 目录与新 `certificates/` 目录共存 | 目录完全分离，新模块写 `<certPath>/certificates/`，旧代码写 `.lego/certificates/` |
| account key 丢失 | account key 备份提示（对齐 lego 原有的 rootPathWarningMessage） |

---

## 14. 实施顺序

```
Phase A（本次）
├── Step 1: domain/types.go + domain/errors.go
├── Step 2: lego/account_store.go + lego/cert_store.go
├── Step 3: lego/client_factory.go
├── Step 4: lego/solver_http.go + lego/solver_dns.go
├── Step 5: lego/issuer.go（Obtain + Renew + 并发锁）
├── Step 6: service/renew_scheduler.go（正确的提前 N 天触发逻辑）
├── Step 7: service/manager.go（兼容 facade）
└── Step 8: 单元测试 + Pebble 集成测试

Phase B（后续，单独评审）
└── global/lego/cert.go 切换到 service.Manager（1 处改动）
```
