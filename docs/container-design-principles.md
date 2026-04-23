# Container 开发基础原则

本文件给出 v2raymg **接入新代理内核**(新增 `pkg/proxy/containers/<name>/`)时必须遵循的设计原则。任何新 container 在开工前都应读完这份文档,并在设计文档里**显式声明**自己属于哪种适配模式。

现有四个 container(xray / hysteria / snell / mihomo)都是这些原则的实例,可作对照。

---

## 原则 1:外部进程优先,不做 Go 库嵌入

**规则**:只要代理内核提供可运行的独立二进制,就用 exec 子进程 + 命令通道(gRPC / HTTP REST / 配置文件 reload)管理,**不要直接 import 它的 Go 包**。

**为什么**:

- 代理内核(xray-core / mihomo / sing-box 等)的依赖图非常庞杂(DNS、TLS、QUIC、各种传输层),嵌入一个就会把 v2raymg 的二进制体积和编译图放大数倍,并且版本升级牵连全项目
- 不同内核之间还会有互不兼容的依赖版本冲突(例如 quic-go 的版本漂移)
- exec 隔离了崩溃域 —— 代理内核挂了不会把 v2raymg 管理面一起拉下水
- 升级代理内核只需要换二进制,不需要重编 v2raymg

**例外**:

- 代理内核**不提供**独立二进制,只能作为库集成
- 代理内核的**运行时控制需要极高频**(毫秒级)且没有合理的 IPC 通道

**实现要点**:

- 进程生命周期走 `pkg/proxy/tools/process.Runner`,不要自己拼 `exec.Command`
- 二进制自动下载 / 校验 / 原子替换走 `pkg/proxy/tools/{binary_swapper,checksum,downloader,github_release_client}`
- 运行时控制通道(gRPC / REST)封装在 container 子目录下的 `grpc_client.go` / `rest_client.go`,不要泄漏到 core 层
- 对应决策 #1(exec 管理外部进程)

---

## 原则 2:根据底层动态 inbound 能力,选对应适配模式

进入 container 设计阶段的**第一个问题**就是:**底层内核支不支持多 inbound?支持的话,有没有运行时 API 增删 inbound?**

回答决定你落入下列哪种模式:

### 模式 A:底层不支持多 inbound(单进程 = 单 inbound)

**代表**:snell、hysteria

**做法**:

- 启动**一个**进程,进程自身对外只监听一个业务协议
- Container 对外暴露**一个占位 inbound**(tag 固定,例如 `snell-default` / `hysteria-default`),保持 Container 接口形状与其他 container 一致
- `FastAddInbound` 只接受占位 tag,其他拒绝
- `RemoveInboundConfig` 通常不真删进程,仅 disable 标志(供停用场景用)
- 多用户隔离**全部**由 forward 层完成(见原则 3)
- 参考:`pkg/proxy/containers/snell/container.go`、`pkg/proxy/containers/hysteria/container.go`、`docs/snell-container-design.md`

### 模式 B:底层支持多 inbound + 运行时 API 动态增删

**代表**:xray(gRPC `AddInboundHandler/RemoveInboundHandler`)、mihomo(`listener.PatchInboundListeners` / `PUT /configs` name-keyed diff)

**做法**:

- 进程启动时 inbound 列表可以为空或非空,之后通过运行时 API 动态 patch
- `FastAddInbound` 直接调用运行时 API 注册新 inbound
- `RemoveInboundConfig` 直接调用运行时 API 删除
- Container 内部维护 `inbounds map[tag]Inbound`,与底层进程状态保持同步
- `Restart()` / `Restore()` 后需要重放(reconcile)所有 inbound 回底层,依赖 `InboundStore` 持久化
- 参考:`pkg/proxy/containers/xray/exec_runner.go` 的 FastAddInbound/RemoveInboundConfig、`docs/xray-container-architecture.md`、`docs/mihomo-container-design.md`

### 模式 C:底层支持多 inbound 但**无**运行时 API

**代表**:暂无(潜在例子:某些只能读配置文件重启的老代理软件)

**做法**:**没有唯一答案,开发者需在设计文档里显式决定并记录 trade-off**。常见选项:

| 选项 | 做法 | 代价 |
|------|------|------|
| C-1 整体重启 | 每次 inbound 变更 → 写新配置文件 → 重启进程 | 全部现有连接断;实现最简单 |
| C-2 SIGHUP 热重载 | 如果进程支持 SIGHUP 重读配置 | 依赖进程实现,通常也会影响现有连接 |
| C-3 退化到模式 A | 只支持单 inbound,放弃多 inbound 能力 | 功能阉割,但实现极简 |
| C-4 多进程池 | 每个 inbound 起一个进程 | 资源浪费,端口/pid 管理复杂 |

**要求**:落到 C 模式的 container,**设计文档必须明确列出选了哪条路、为什么、断连影响评估**,PR review 时会重点看这一节。

### 模式选择 cheatsheet

```
底层内核?
 ├─ 支持多 inbound?
 │   ├─ 有运行时增删 API?─── 模式 B  (xray / mihomo)
 │   └─ 无运行时 API      ─── 模式 C  (开发者决策)
 └─ 不支持多 inbound      ─── 模式 A  (snell / hysteria)
```

---

## 原则 3:用户管理一律走 forward 层

**规则**:业务层(HTTP API、订阅生成、集群用户同步)操作用户时,**只调 UserManager**。forward 层是**公网端口分配、流量统计、限速、客户端数限制**的**唯一权威来源**。Container 只通过 `UserEventChannel` 订阅事件,自己消化。

**为什么**:

- 不同代理协议的用户认证机制千差万别(vmess 要 UUID、hysteria 走 HTTP callback、snell 共享 PSK、mihomo 要 listener-level user list),如果上层业务关心这些差异,每加一个协议就要改 HTTP API 和订阅逻辑
- forward 层是所有 container 的**共同路径**(流量必经),在这里做统计/限速就**唯一、准确、可持久化**
- 代理内核的 `QueryStats` / `traffic` API 各家各样,有的还不可用(snell 根本没有);统一信 forward 层意味着**不用为每个内核实现流量查询**
- 对应决策 #4(转发层统一收口)和 #5(流量统计只信 forward 层)

**分层划分**:

```
 业务层 (HTTP API / 订阅 / 集群同步)
     │  只调
     ▼
 UserManager.Add/Remove/Update/List
     │  发 UserEvent
     ▼
 ┌─ Forward 层(公共路径) ──────────────────────────┐
 │  GetBindPort / ReleaseBindPort                  │
 │  公网端口分配 + 流量统计 + 限速 + 客户端数限制     │
 │  所有 container 必走                             │
 └─────────────────────────────────────────────────┘
     │
     ▼
 ┌─ Container 协议层(内部消化,业务不感知) ────────┐
 │  订阅 UserEvent → 按协议需要同步到内核            │
 │  xray:    共享 clients[0] 的 UUID/password,所有 │
 │           用户订阅同一个 UUID,协议级认证通过   │
 │           共享凭据完成;gRPC AddUser 未使用     │
 │  mihomo:  每 inbound 一条 listener + 一把共享   │
 │           凭据,所有用户订阅同一把凭据;mihomo   │
 │           的 user-level REST API 未使用         │
 │  hysteria: 内核通过 HTTP 回调 v2raymg 做认证     │
 │  snell:    共享 PSK                              │
 └─────────────────────────────────────────────────┘
```

**强制要求**:

- 业务层 **绝不** 直接调 container 的 AddUser/RemoveUser、也 **不** 直接调 `forwardManager.AddRule` —— 必须通过 `UserManager.GetBindPort/ReleaseBindPort`(决策 #4)
- 流量查询 **不信** `container.QueryStats`,只信 forward 层的累计/增量计数(决策 #5)
- Container 可以在协议层实现细节上调用内核的 user API,但**只是同步手段,不是业务接口**;如果不实现也不会阻塞业务(见 hysteria/snell)

---

## 新 Container 接入 Checklist

开一个新 container 之前,在设计文档里逐条回答:

1. **是否走 exec**(原则 1) — 是 / 否(否必须说理由)
2. **底层二进制怎么来** — 手工下载 / GitHub Release 自动下载 / 其他
3. **inbound 适配模式**(原则 2) — A / B / C-?,一行说明为什么
4. **运行时控制通道** — 无(模式 A) / gRPC / HTTP REST / 配置 + SIGHUP / 其他
5. **协议层是否需要持有 user list** — 需要(说明同步机制) / 不需要(说明认证机制)
6. **流量统计入口** — 确认走 forward 层(原则 3)
7. **持久化** — inbound 写 InboundStore,用户端口写 forward 持久化,对账走 `Restorable.Restore`
8. **订阅** — 复用 `core/subscription/converter/`,缺失 client/protocol converter 另行补
9. **协议枚举扩展** — `contracts.Protocol` / `contracts.Transport` / `contracts.ContainerType` 需不需要加常量(不加就走 `InboundSpec.Extensions`)

---

## 现有 Container 选型对照表

| Container | 原则 1 | 原则 2 模式 | 运行时通道 | 协议层 user list | 原则 3 对齐 |
|-----------|--------|-------------|------------|-------------------|-------------|
| **xray** | exec | B(gRPC AddInbound/RemoveInbound) | xray gRPC(仅用于 inbound 增删) | **共享 clients[0] 的 UUID/password,所有用户订阅同一个 UUID**;gRPC AddUser/RemoveUser 有封装但业务路径上**从未被调用** | ✓ 共享凭据 + forward per-user 端口,与 snell 同构 |
| **mihomo** | exec | B(`PatchInboundListeners` / `PUT /configs`) | mihomo HTTP REST | **每 inbound 一条 listener + 一把共享凭据**(vmess uuid / trojan password / ss password+cipher),所有用户共用;mihomo 的 user-level REST API 不使用 | ✓ 共享凭据 + forward per-user 端口,与 xray/snell 同构 |
| **hysteria** | exec | A(单 inbound) | 无(HTTP auth callback) | 不需要(内核回调 v2raymg 做认证) | ✓ 只有 forward 层做 per-user |
| **snell** | exec | A(单 inbound) | 无(共享 PSK) | 不需要(所有用户同一 PSK) | ✓ 只有 forward 层做 per-user |

> **重要事实**:xray 虽然提供了 gRPC `AddUser/RemoveUser` 能力,但 v2raymg 在业务路径上**完全不使用**。`XrayInbound.AddUser/RemoveUser`(`exec_runner.go:1325-1387`)方法注释明确写着 `Note: This does NOT call xray gRPC AddUser`,实现只调 `userMgr.GetBindPort/ReleaseBindPort`。`Executor.AddUser/RemoveUser`(`exec_runner.go:883-893`)是 gRPC 的封装但全仓无调用者,属于历史遗留能力。mihomo 同理:即便 mihomo 本身的 listener 支持 users 数组,v2raymg 也只在 listener 层放**一把共享凭据**,业务用户路径不走 mihomo 的任何 user-level API。结论:**所有 container 的业务用户路径都只走 forward 层,无一例外**。

---

## 相关文档

- `docs/xray-container-architecture.md`、`docs/xray-container-flow.md` — 模式 B 标杆
- `docs/snell-container-design.md` — 模式 A 标杆
- `docs/mihomo-container-design.md` — 模式 B(多 listener + 共享凭据)
- `wiki/knowledge/port-management/` — forward 层端口/流量统计细节
- `CLAUDE.md` 核心设计决策 #1 / #3 / #4 / #5 — 本文档的精简上游
