# Mihomo Container 设计文档

## 概述

引入 [mihomo](https://github.com/MetaCubeX/mihomo)(Clash Meta 继任者)作为新的 container 类型,覆盖 vmess/vless/trojan/shadowsocks/hysteria2/tuic/anytls 等协议的服务端实现,并天然对齐 Clash 生态客户端。

定位:与 xray/hysteria/snell 并列的 container,使用 exec 子进程 + mihomo external-controller 的 HTTP REST API 控制。所有共享设施(UserManager、forward 层、certmgmt、store、process.Runner、BaseContainer、Factory、clash 订阅转换)直接复用,新增内容压缩到 `pkg/proxy/containers/mihomo/` 一个目录。

## 关键问题:能否像 xray 一样动态管理 inbound

| 层级 | mihomo 原生能力 | 本方案采用 |
|------|-----------------|-----------|
| `listeners:` 数组级(命名 listener) | `listener.PatchInboundListeners(map, tunnel, dropOld)` 按 name 做 map diff,增删单个 listener **不影响其他 listener 的既有连接**。REST API 侧通过 `PUT /configs` 提交全量 yaml 达到相同效果(name 未变的 listener 不重建) | ✓ 用作 Inbound 增删入口 |
| 单 listener 内 users 数组级 | 所有实现(vmess/vless/trojan/ss/hysteria2/tuic)都在 `New()` 时将 users 快照给内部 service,**无导出的 AddUser/RemoveUser**。users 变更必须重建整个 listener,其上所有既有连接断 | ✗ 不依赖 |

**结论**:listener 级等价于 xray 的 `AddInboundHandler/RemoveInboundHandler`,**不重启进程即可增删 inbound**。user 级 API 缺失**不是阻塞项** —— v2raymg 所有 container 的业务用户路径都走 forward 层,**xray 自己也不用 `AddUser/RemoveUser` gRPC**(`XrayInbound.AddUser` 注释明确 `Note: This does NOT call xray gRPC AddUser`,参见 `exec_runner.go:1325-1387`)。协议级认证在 xray/snell 是共享凭据,mihomo 需要 per-user UUID,下文 per-user listener 模型解决这点。

**回退路线**:采用"每用户一个独立 listener"模型(下文"User 隔离模型"一节),让 user 增删天然降级为 listener 级操作,恢复到不干扰其他用户连接的语义。

## 架构

```
 客户端 ── 公网端口 ──┐
                     │  forwardmgr relay
                     ▼
              127.0.0.1:<per-user 内部端口>
                     │
                     ▼
       mihomo 进程(单进程,N 个命名 listener)
         listener "<tag>__<username>" → single user { uuid/password }
```

- 一个 mihomo 进程,监听 mihomo external-controller(默认 127.0.0.1:9090)
- mihomo `listeners:` 数组中,每个 (inbound_tag, username) 组合对应一条独立的命名 listener,单用户、listen 在 127.0.0.1:<unique_internal_port>
- `forwardmgr` 为每个用户分配一个公网端口,中继到该用户对应的 mihomo 内部端口
- 流量统计仍源自 forward 层(对齐决策 #5)
- mihomo 的 proxies/rules/DNS 等出站能力**不启用**(本场景仅用其入站能力)

## 文件结构

```
pkg/proxy/containers/mihomo/
├── register.go          # Factory 注册到 core/container/registry
├── config.go            # MihomoConfig (ConfigObj) + YAML 骨架生成
├── container.go         # Container 接口实现,组合 BaseContainer + RESTClient + process.Runner
├── rest_client.go       # mihomo external-controller HTTP 客户端封装
├── inbound.go           # MihomoInbound 实现 core/inbound.Inbound
├── profilegen.go        # (inbound_spec, user_spec) -> mihomo listener YAML 片段
├── adapter.go           # contracts.InboundSpec -> MihomoInbound
├── reconcile.go         # 启动后从 InboundStore 重放 listeners
├── ports.go             # 内部端口池(分配 127.0.0.1:internal_port)
├── subscription.go      # GetUserSubscriptions(复用 core/subscription/converter/clash)
├── updater.go           # 二进制下载/校验/原子替换(可复用 xray/updater 的同构实现或抽到 tools)
└── process.go           # mihomo 进程启动参数构造(-d 数据目录 / -ext-ctl 等)
```

## Container 接口实现

| 方法 | 行为 |
|------|------|
| `Init(config)` | 接收 `*MihomoConfig`,准备目录/数据文件骨架;不启动进程 |
| `Start()` | 生成初始 config.yaml → `process.Runner.Start()` → 等 `GET /version` 就绪 → 启动 UserEventHandler → `reconcileUsers()` 重放 store 中的 listener |
| `Stop()` | 停 UserEventHandler → `process.Runner.Stop()`(SIGINT,5s 超时后 SIGKILL) |
| `Restart()` | `Stop()` + `Start()`;等价于 BaseContainer 默认实现 |
| `Reload()` | 仅重拉当前 state 调 `PUT /configs`,**不重启进程**(用于更新证书/全局配置) |
| `IsRunning()` | BaseContainer 状态 + `GET /version` 双验证 |
| `Version()` | `GET /version` 返回的 mihomo 版本号 |
| `Type()` | `contracts.ContainerMihomo` |
| `ConfigFile()` | `MihomoConfig.ConfigFilePath` |
| `Update(ctx,req)` | updater 下载新二进制 → 校验 → 原子替换 → 按 RestartPolicy 重启 |
| `GetRunFunc()` | 返回 `process.Runner` 的 run/stop |
| `FastAddInbound(tag,params)` | profilegen 构造 listener spec → 更新 store → PATCH /configs(逻辑上增加一条 listener)|
| `RemoveInboundConfig(tag)` | 释放所有相关用户端口 → store 删除 → PUT /configs(逻辑上移除该 tag 的所有 listener) |
| `GetInboundConfig(tag)` | 查 `c.inbounds[tag]` |
| `ListInboundConfigs()` | 列出所有 inbound(不展开到 per-user listener)|
| `UserEventChannel()` | 返回 `c.userEventCh`(与 hysteria/snell 同模式,UserManager.Subscribe 转发) |
| `GetUserSubscriptions(req)` | 返回 clash YAML 订阅(见订阅一节) |

## User 隔离模型

每个上层 "inbound" 展开为若干条 mihomo listener:

```
v2raymg inbound "vmess-tag1"  (上层概念,含用户集合)
   └── mihomo listener "vmess-tag1__alice"  127.0.0.1:61234  users: [ { uuid: <alice-uuid> } ]
   └── mihomo listener "vmess-tag1__bob"    127.0.0.1:61235  users: [ { uuid: <bob-uuid> } ]
```

**User 事件处理**(对齐 hysteria container.go:473-570 的 per-user forward 模型):

```
UserEventAdd(username, user)
  1. 从内部端口池分配 internal_port
  2. profilegen: 构造 listener spec
       name:   <inbound_tag>__<username>
       type:   <protocol>  (vmess/vless/trojan/ss/hysteria2/tuic...)
       listen: 127.0.0.1
       port:   <internal_port>
       users:  [ 该用户对应的凭据 ]
       tls/cert/transport: 来自 inbound 的共享配置
  3. REST: PUT /configs  (merge 新 listener)
  4. forwardmgr.GetBindPort(Username, InboundTag, TargetPort=internal_port, Protocol, Network)
  5. 记录 addedUsers[username][inbound_tag] = { internal_port, listener_name }

UserEventRemove(username)
  1. 遍历该用户在所有 inbound 下的 listener
  2. REST: PUT /configs (从 listeners 数组剔除这些 listener 名)
  3. forwardmgr.ReleaseBindPort + 释放 internal_port
  4. 清理 addedUsers[username]

UserEventUpdate(username)
  - 同 hysteria:按 IsUserVisible 判断,增则建 listener+端口,减则释放
```

**互斥与幂等**:listener 变更与 forward 规则操作都是幂等的(GetBindPort/PUT 都能重入)。container 自身持一个 `sync.Mutex` 保护 `addedUsers` map,调用 REST 时释放锁。

## 配置

### AppConfig yaml

```yaml
containers:
  containers:
    - type: mihomo
      enabled: true
      config:
        binary_path: /usr/local/bin/mihomo
        config_file_path: /etc/v2raymg/mihomo.yaml
        data_dir: /var/lib/v2raymg/mihomo
        external_controller: "127.0.0.1:9090"
        secret: ""                         # 留空即无 secret
        auto_download: true
        release:
          provider: github
          owner: MetaCubeX
          repo: mihomo
          asset_name: "mihomo-linux-amd64-*.gz"
          checksum_asset_name: "mihomo-linux-amd64-*.sha256"
        internal_port_range: [30000, 40000]  # 给 per-user listener 用
        reconcile_interval: 30s
```

### 初始 mihomo config.yaml(Start 时生成)

```yaml
mixed-port: 0
allow-lan: false
external-controller: "127.0.0.1:9090"
secret: ""
log-level: info
mode: global         # 不启用规则匹配,出站直连
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
listeners: []        # 启动时为空,由 reconcile 重放填充
```

核心原则:mihomo 的出站/规则/DNS 能力**不启用**,只作为入站服务。

## REST API 使用

封装在 `rest_client.go`:

| 操作 | HTTP | 路径 | 参数 |
|------|------|------|------|
| 获取全量配置 | GET | /configs | — |
| 全量更新(提交完整 yaml) | PUT | /configs | body: `{"path":"<file>"}` 或 `{"payload":"<yaml>"}`;query: `force=false` |
| 部分更新(调 socks-port/tun 等) | PATCH | /configs | body: json 字段 |
| 查询版本 | GET | /version | — |
| 更新 GEO 库 | POST | /configs/geo | — |

**listener 增删走 PUT /configs,不走 PATCH**:

- PATCH 支持的字段(port/socks-port/tun/tuic-server/ss-config/vmess-config)都是 top-level singleton,不适合命名多 listener
- PUT /configs 配合 `force=false` 时,mihomo 内部走 `PatchInboundListeners` 做 name-keyed diff —— 未变的 listener 不重建,既有连接不受影响

**操作并发**:container 内串行调 REST(单线程 user event handler + 互斥锁),mihomo 内部每类 listener 有独立 mutex,双端都是安全的。

## 持久化与 Restore

- **InboundStore** 持久化上层 inbound(tag/protocol/共享 tls/transport 配置),不持久化展开后的 per-user listener
- **UserManager + forwardmgr** 本身持久化 port mapping,重启后可恢复用户端口
- **Restore 流程**(由 ContainerMgr.StartAll 自动触发,因为 container 实现 Restorable):
  1. 从 InboundStore 读全部 inbound
  2. 从 UserManager 读当前可见用户
  3. 对每对 (inbound, user) 生成 listener spec,聚合成完整 listeners 数组
  4. 一次性 PUT /configs
  5. 对账 forward 层:缺失的 GetBindPort 补申请,多余的 ReleaseBindPort

## Subscription Converter

**直接复用** `pkg/proxy/core/subscription/converter/clash/`。mihomo = Clash Meta,订阅格式原生对口。

`GetUserSubscriptions(req)` 步骤:

1. 遍历该用户在 mihomo container 下所有 inbound
2. 每个 inbound 对应一个 Clash `proxies:` 条目,server 用节点 proxy_host,port 用 forwardmgr 分配给该用户的公网端口,uuid/password/cipher 从 UserSpec 读
3. 返回 `contracts.SubscriptionSpec{ ClientType: "clash", ... }`

对于 v2ray/surge 等非 clash 客户端,由订阅层在渲染阶段决定是否包含 mihomo 入站的代理(取决于具体协议的兼容性,vmess/vless/trojan/ss/hysteria2/tuic 几乎都可跨输出)。

## 协议与字段扩展(决策延后项)

最小侵入路线(推荐先走):

- `contracts.ContainerType` 新增 `ContainerMihomo = "mihomo"`,`IsValid()` 补一行
- `contracts.Protocol` **不动**;mihomo 特有的 tuic/anytls 通过 `InboundSpec.Extensions["mihomo"]` 承载
- `contracts.Transport` **不动**

若后续需要把 TUIC 作为上层一等公民(HTTP API 列表、订阅生成显式支持),再行:

- 新增 `ProtocolTUIC Protocol = "tuic"`
- 相应扩展 clash converter 输出 `type: tuic`
- HTTP API `/inbound/fast_add` 增加 tuic 协议路径

此项留待 MVP 落地后再决定。

## 开发步骤

### Step 1 骨架与 Factory 注册
- `register.go`:`init()` 中 `container.RegisterFactory(contracts.ContainerMihomo, &mihomoFactory{})`
- `config.go`:`MihomoConfig.Decode(map[string]any)` 解析 AppConfig yaml
- `container.go`:嵌入 `*BaseContainer`,实现所有 Container 方法骨架(多数返回 nil 或待填)
- `contracts.ContainerMihomo` 常量注册

### Step 2 进程与 REST 客户端
- `process.go`:`process.Runner` 启动 mihomo,参数 `-d <data_dir> -f <config_file> -ext-ctl <addr>`
- `rest_client.go`:GET/PUT/PATCH /configs、GET /version
- `Start()` 启动后轮询 `GET /version`(最多 10s)确认就绪

### Step 3 Inbound 生命周期
- `inbound.go`:`MihomoInbound` 实现 `core/inbound.Inbound`,extension map 存 tls/transport/协议特有字段
- `adapter.go`:`contracts.InboundSpec -> MihomoInbound`
- `profilegen.go`:`(MihomoInbound, UserSpec) -> listener YAML map`
- `FastAddInbound`/`RemoveInboundConfig`/`GetInboundConfig`/`ListInboundConfigs` 落地
- 持久化到 InboundStore

### Step 4 User Event 与 forwardmgr 集成
- `container.go`:`forwardUserEvents` + `startUserEventHandler` + `handleUserEvent`(抄 hysteria 模板)
- `ports.go`:内部端口分配/释放(端口池)
- UserEventAdd/Remove/Update 完整链路打通
- 一次性 `PUT /configs` 提交当前完整 listeners 数组

### Step 5 Restore 与 Reconcile
- 实现 `Restorable.Restore(ctx)`:从 InboundStore + UserManager 重建全量 listeners
- `reconcile.go`:周期性对账 forward 层与 store 状态
- `Reload()`:仅重提 PUT /configs,不重启进程

### Step 6 Subscription 与 HTTP API
- `subscription.go`:调 clash converter 生成订阅
- 更新 `pkg/http/` 相关 handler(如果需要让 HTTP API 识别 mihomo container)

### Step 7 更新器与自动下载
- `updater.go`:从 GitHub Release 下载,校验 sha256,原子替换
- `AutoDownload=true` 时 Start 前自动执行

### Step 8 测试
- 单元测试:rest_client、profilegen、adapter、ports
- 集成测试:真实 mihomo 二进制,走 `pkg/proxy/systemtest/`
- 端到端:AddUser → 查公网端口可连 → RemoveUser → 端口不可连

## 风险与未决

- **listener 数量膨胀**:规模 N 用户 × M 协议 = N×M 条 listener。若单节点上万用户,mihomo yaml 解析/加载时间需实测。超出可接受阈值时,考虑回退到"多用户共享 listener + 变更时重建 + 依赖客户端重连"模式
- **mihomo 版本漂移**:Alpha 分支的 config schema 可能 breaking change。需要在 rest_client 层做版本探测,不兼容时降级或报错
- **TLS 证书热更**:mihomo 的 listener 在 cert 变更时是否能只重建自己而不重启进程,待验证。若不行,`Reload()` 需要降级为 `Restart()`
- **subscription 协议过滤**:当前 clash converter 支持哪些协议输出待核对,缺失的协议需补 converter 支持
- **决策 #1 兼容性**:mihomo 是 Go 库但本方案坚持 exec,与 xray/hysteria/snell 一致,不破坏决策

## 参考

- mihomo Alpha 分支关键文件:
  - `listener/listener.go`:`PatchInboundListeners`、`ReCreate*`、per-type mutex
  - `listener/parse.go`:`ParseListener(map) -> InboundListener`,支持 vmess/vless/trojan/ss/hysteria2/tuic/anytls/mieru/sudoku/trusttunnel/http/socks/mixed/redir/tproxy/tun/tunnel
  - `hub/executor/executor.go`:`ApplyConfig(cfg, force)`、`updateListeners`
  - `hub/route/configs.go`:`PUT/PATCH/POST /configs`
  - `listener/config/{vmess,trojan,hysteria2,tuic}.go`:各协议 listener 配置结构与 users 字段
- v2raymg 对标实现:
  - `pkg/proxy/containers/hysteria/container.go:473-570`(per-user forward 模板)
  - `pkg/proxy/containers/snell/container.go:382-460`(per-user forward 模板)
  - `pkg/proxy/containers/xray/register.go`(Factory 注册范式)
  - `docs/snell-container-design.md`(同风格设计文档)
