# 服务端链式代理（Chained Proxy）与两层 Tag 资源隔离设计方案

## 1. 背景、目标与整体架构

当前 `v2raymg` 各代理容器（`xray`、`mihomo`、`hysteria`、`snell`）管理的 Inbound 均在本节点直接出站（`DIRECT` / `freedom`）。当后端存在**非网络优化线路**（如各地家庭宽带、动态公网 IPv4/IPv6 DDNS、软路由或住宅落地代理）时，客户端本地配置前置代理链不够灵活、难以跨设备统一维护且容易暴露后端凭据。

本方案在不破坏现有直连 Inbound 与核心架构原则（见 [PROJECT_GUIDE.md](file:///workspace/v2raymg/PROJECT_GUIDE.md#L65-L93) 与 [container-design-principles.md](file:///workspace/v2raymg/docs/container-design-principles.md#L10-L138)）的前提下，从**代理协议编排**与**多维资源隔离**两个视角构建服务端链式代理体系。

```mermaid
flowchart LR
    Client["授权客户端"] -->|"公网每用户独立端口"| Fwd["Forward 转发层 (TCP/UDP Relay)"]
    Unauth["未授权用户"] -.->|"用户侧隐藏 + 数据面 0 端口分配"| Block["无监听端口 / 零暴露"]

    subgraph Node["v2raymg 前置优化节点"]
        Fwd -->|"1. Node Group 校验\n2. Inbound AccessTags 校验\n3. 流量统计 + 用户级限速"| Loopback["容器本地监听 (127.0.0.1:inboundPort)"]
        Loopback -->|"inbound_type = direct"| DirectOut["默认直连 Outbound\n(DIRECT / freedom)"]
        Loopback -->|"inbound_type = chain\n1:1 专属绑定"| ChainOut["动态链式 Outbound\n(__chain_out__:tag)"]
    end

    DirectOut --> Origin1["常规目标网站"]
    ChainOut -->|"解析后端 URI 拨号\n(保留 DDNS 域名 + 原始目标域名)"| HomeProxy["后端非优化代理\n(各地家宽 / 软路由 / 落地机)"]
    HomeProxy -->|"家宽原生 IP + 当地 ISP DNS"| Origin2["目标业务 / 地区限定服务"]
```

> [!IMPORTANT]
> **四大架构铁律（Invariants）**
> 1. **全新 Inbound 1:1 不可变绑定**：链式代理仅允许在**创建全新 Inbound** 时绑定唯一后端 Outbound（`inbound_type="chain"`），严禁挂载到任何存量 Inbound，创建后类型与后端拓扑不可原地篡改。
> 2. **两层 Tag 命名空间物理隔离与严格求值顺序**：Node 层标签（`local_node_groups` $\leftrightarrow$ `User.TargetGroup`）与 Inbound 层标签（`Inbound.AccessTags` $\leftrightarrow$ `User.InboundTags`）物理隔离、允许同名重复；服务端恒定执行 **`先判 Node 对用户可用` $\Rightarrow$ `再判 Inbound 对用户可见`**，且同时作用于**用户侧视图（`/sub`、`/api/profile`）**与**数据面（Forward 端口分配）**（管理员控制面始终返回全量 Inbound 以便管理）。
> 3. **零直连泄漏（Zero IP-Leak / Fail-Closed）**：新增链式 Inbound 严格遵循 **“先建 Outbound 与路由规则 $\rightarrow$ 再开 Inbound 监听 $\rightarrow$ 最后开 Forward 端口”**；后端家宽断连时严格报错中断，**绝不回退**前置机本地 `DIRECT` 暴露机房 IP。
> 4. **公网唯一入口收口**：容器侧后端监听强制绑定 `127.0.0.1`，所有外部流量 100% 经过 Forward 层完成用户端口隔离、精准计费与带宽保护。

---

## 2. 需求 1 设计：Inbound 类型化、新建唯一约束与 Container 能力矩阵

### 2.1 领域模型与持久化 Schema 扩展（SQLite Migration 18）

当前 `inbounds` 表（[migrations.go](file:///workspace/v2raymg/pkg/store/migrations/migrations.go#L33-L42)、[inbound_store.go](file:///workspace/v2raymg/pkg/store/inbound_store.go#L9-L15)）仅存储 `tag`、`container_type`、`cert_source`、`cert_domain`、`native_json`，无法表达 Inbound 类型、可见性标签与链式出站配置。新增 **Migration Version 18**：

```sql
-- Version 18: chain proxy inbound metadata & two-layer inbound visibility tags
ALTER TABLE users ADD COLUMN inbound_tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE inbounds ADD COLUMN inbound_type TEXT NOT NULL DEFAULT 'direct';
ALTER TABLE inbounds ADD COLUMN access_tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE inbounds ADD COLUMN chain_outbound_uri TEXT NOT NULL DEFAULT '';
ALTER TABLE inbounds ADD COLUMN remark TEXT NOT NULL DEFAULT '';
```

同步扩展领域与存储结构：
- **类型枚举**（定义于 [inbound.go](file:///workspace/v2raymg/pkg/proxy/core/contracts/inbound.go)）：
  - `InboundTypeDirect = "direct"`（默认，兼容全部历史存量记录）
  - `InboundTypeChain = "chain"`（1:1 专属链式出站）
- **`store.InboundRecord`**（[inbound_store.go](file:///workspace/v2raymg/pkg/store/inbound_store.go#L9-L15)）：新增 `InboundType string`、`AccessTags []string`、`ChainOutboundURI string`、`Remark string`。
- **消除双容器持久化分叉**：
  - `xray`：在 `XrayInbound`（[exec_runner.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/exec_runner.go#L1281-L1331)）持有上述元数据，并修复 `AddInboundNative`（[exec_runner.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/exec_runner.go#L790-L800)）硬编码默认 `InboundRecord` 覆盖元数据的问题。
  - `mihomo`：当前 `MihomoContainer.persistInbound` 与 `restoreAndPushInbounds`（[container.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/container.go#L717-L731)、[L820-L845](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/container.go#L820-L845)）只读写 `NativeJSON` 而忽略 `InboundRecord` 其他列，需同步持久化并恢复 `InboundType`、`AccessTags`、`ChainOutboundURI`、`Remark`。

### 2.2 强制“仅限全新 Inbound 绑定”与前置校验流水线

> [!WARNING]
> **查缺补漏：跨容器同名 `tag` 静默覆盖风险**
> SQLite `inbounds` 表以 `tag TEXT PRIMARY KEY` 为全局主键，且 `SQLiteInboundStore.Save` 使用 `INSERT OR REPLACE`（[inbound_store.go](file:///workspace/v2raymg/pkg/store/inbound_store.go#L45)），`ForwardManager.RemoveRulesByInbound(tag)` 也按全局 `tag` 清理转发规则。若仅在单个容器的 `c.inbounds[tag]` 内查重，在 `mihomo` 新建与 `xray` 同名的链式 Inbound 会直接抹掉 `xray` 的持久化记录！

因此，在 [end_node_inbound.go](file:///workspace/v2raymg/pkg/rpc/server/end_node_inbound.go#L165-L310) `EndNodeServer.FastAddInbound` **最顶部**（早于 `ObtainNewCert` ACME 申请与 `params.FillDefaults` 磁盘自签证书生成，防止校验失败遗留孤儿证书文件）执行严格前置校验：

1. **Tag 自动补全与字符白名单校验**：
   - 若请求省略 `tag`，服务端自动生成 `<protocol>[-chain]-<8位随机hex>`（补齐 [fastAddInbound_handler.go](file:///workspace/v2raymg/pkg/http/fastAddInbound_handler.go#L32) 文档承诺但未实现的自动命名）；
   - 强制校验 `tag` 匹配 `^[a-zA-Z0-9._-]{1,64}$` 且**不以保留前缀 `__` 开头**（杜绝 Mihomo `IN-NAME,<tag>,<proxy>` 规则逗号注入与 `ForwardRule.RuleKey()` 冒号分隔符歧义）。
2. **跨容器 + InboundStore 全局唯一性拦截**：
   - 遍历 `s.containerMgr.Types()` 所有已注册容器及 `InboundStore`，只要 `tag` 已存在立即拒绝（`ErrInboundAlreadyExists`），**从根源封死挂载到已有 Inbound 或覆盖存量 Inbound 的任何路径**。
   - 原生 `POST /api/inbound`（`AddInbound`）固定仅允许 `direct` 类型，同样接入全局 `tag` 查重。
   - 不提供修改已有 Inbound `inbound_type` 或 `chain_uri` 的 API；如需更换家宽 URI，先 `DELETE /api/inbounds` 原子销毁再新建。
3. **参数自洽校验**：
   - `inbound_type == ""` 时：传入非空 `chain_uri` 自动归一为 `"chain"`，否则为 `"direct"`；
   - `inbound_type == "chain"` 但 `chain_uri == ""` $\rightarrow$ `400 Bad Request`；
   - `inbound_type == "direct"` 但 `chain_uri != ""` $\rightarrow$ `400 Bad Request`。

### 2.3 Container 链式代理二级能力检查（`ChainCapable`）

在 [interface.go](file:///workspace/v2raymg/pkg/proxy/core/container/interface.go) 新增可选能力接口，兼顾“容器级是否支持链式路由”与“容器内核是否支持目标后端协议”：

```go
// ChainCapable 由支持动态链式出站的 Container 实现（遵循开闭原则，不污染模式 A 容器）。
type ChainCapable interface {
    SupportsChainOutbound() bool
    ValidateChainTarget(spec *chain.OutboundSpec) error
}
```

| Container | 适配模式 | `SupportsChainOutbound` | 支持的前置 Inbound 协议 | 支持拨号的后端 `chain_uri` 协议 | 架构依据与边界说明 |
| :--- | :---: | :---: | :--- | :--- | :--- |
| **`mihomo`** | 模式 B | **支持（主力推荐）** | `vless`, `vmess`, `trojan`, `ss`, `hysteria2`, `tuic`, `anytls` | `vless`(加密 `none`), `vmess`, `trojan`, `ss`(含 `obfs`/`v2ray-plugin`/`shadow-tls`), `hy2`/`hysteria2`(含 `user:pass` 与 `mport`), `tuic`(v5), `anytls`, `socks5`, `http(s)` | REST `PUT /configs?force=false` 原生支持 `proxies[]` + `listener.proxy` 1:1 绑定且热更不断流；**不支持** `httpupgrade`/`xhttp` 出站及 Xray 专属 VLESS `mlkem768` 加密（由 `ValidateChainTarget` 拦截并提示改用 `xray`） |
| **`xray`** | 模式 B | **支持** | `vless`, `vmess`, `trojan`, `ss` | `vless`(含 `httpupgrade`/`xhttp`/`reality`/`encryption`), `vmess`, `trojan`, `ss`(纯协议及 2022，不含外部 SIP003 插件进程), `socks5`, `http(s)` | 通过 `HandlerService` + `RoutingService` 动态增删 Outbound 与 `inboundTag -> outboundTag` 规则；**不支持** `hy2`/`tuic`/`anytls` 出站（由 `ValidateChainTarget` 拦截并提示改用 `mihomo`） |
| **`hysteria`** | 模式 A | **不支持** | 固定 `hysteria-default` | — | 单进程单固定占位 Inbound，无法按新建 Inbound 挂载独立上游（需用 Hy2 做前置入口时请指定 `container="mihomo"`） |
| **`snell`** | 模式 A | **不支持** | 固定 `snell-default` | — | 单进程单固定 Inbound，且官方闭源 `snell-server` 二进制无上游代理出站能力 |

同步扩展 `GET /api/containers` 与 `GET /api/inbounds` 返回容器 `supports_chain` 布尔值与 Inbound 的 `inbound_type` / `access_tags` / 脱敏后端摘要，便于管理员和 MCP 工具在创建前发现能力。

---

## 3. 需求 2 设计：Node Tag + Inbound Tag 两层物理隔离

### 3.1 双命名空间物理隔离模型

为确保“Node 的 tag 跟 Inbound 的 tag 完全隔离、允许重名且互不污染”，两层权限采用独立字段存储与传输：

| 隔离层级 | 资源侧标签定义 | 用户侧标签定义 | 命名空间与重名隔离语义 | 缺省行为 |
| :--- | :--- | :--- | :--- | :--- |
| **第一层：Node 层** | 节点 `local_node_groups`（`[]string`，API `/api/node/:name/groups`） | `UserSpec.TargetGroup`（JSON/Proto `"group"`） | 独立命名空间 A | 单机模式恒放行；集群模式下节点未设组或 `user.TargetGroup == ""` 默认可用，否则须 `TargetGroup ∈ local_node_groups` |
| **第二层：Inbound 层** | Inbound `AccessTags`（`[]string`，JSON/Proto `"access_tags"`） | `UserSpec.InboundTags`（`[]string`，JSON/Proto `"inbound_tags"`） | **独立命名空间 B**（允许与 Node group 完全同名，如均含 `"home"` 或 `"vip"`，绝不交叉匹配） | **`len(AccessTags) == 0` 默认对已通过第一层的用户公开**；非空则严格白名单（**Fail-Closed**，要求 `AccessTags ∩ User.InboundTags ≠ ∅`） |

**反例验证**：设某节点 `groups = ["vip"]`，其上有一条直连 Inbound `A`（`access_tags = []`）与一条家宽链式 Inbound `B`（`access_tags = ["vip"]`）：
- 用户 Alice（`group = "vip"`, `inbound_tags = []`）：通过第一层 Node 校验 $\rightarrow$ 可见并使用直连 `A`；但在第二层因 `inbound_tags` 为空被严格拦截 $\rightarrow$ **不可见且无端口访问链式 `B`**（同名 `"vip"` 绝不穿透！）。
- 用户 Bob（`group = "basic"`, `inbound_tags = ["vip"]`）：即使拥有第二层 `"vip"` 标签，但在第一层 Node 校验即失败 $\rightarrow$ **在该节点上 `A` 和 `B` 均不可用**（严格满足“先满足 Node 对用户可用，才进一步判断 Inbound 是否可见”）。

### 3.2 两阶段权威判定逻辑与存量漏洞前置修复

```mermaid
flowchart TD
    Start(["判定用户 U 能否访问本节点 Inbound I"]) --> CheckState{"U 未删除且未过期?"}
    CheckState -- "否" --> Deny["拒绝访问\n(不发订阅 + 回收/不分配 Forward 端口)"]
    CheckState -- "是" --> Layer1{"第一层: Node 对 U 可用?\nIsUserVisible(U)"}
    Layer1 -- "否" --> Deny
    Layer1 -- "是" --> HasTag{"第二层: len(I.AccessTags) == 0 ?"}
    HasTag -- "是 (无 Tag 默认公开)" --> Allow["允许访问\n(输出订阅 + 分配/保留 Forward 端口)"]
    HasTag -- "否 (有 Tag 私密隔离)" --> Layer2{"I.AccessTags ∩ U.InboundTags ≠ ∅ ?"}
    Layer2 -- "否" --> Deny
    Layer2 -- "是" --> Allow
```

> [!CAUTION]
> **必须同步修复的 3 处存量并发/权限漏洞**
> 1. **新节点首次启动第一层 Node Group Fail-Open 漏洞**：在 [server.go](file:///workspace/v2raymg/cmd/server.go#L288-L295) 中，当前代码先调用 `userMgr.EnableClusterSync(...)`（此时空库 `ngStore.List()` 为空，`m.cachedNodeGroups` 缓存为空 map），随后才调用 `ngStore.Set([]string{DefaultGroup})` 且绕过了 `userMgr.SetNodeGroups`！导致新节点在首次重启前 `len(m.cachedNodeGroups) == 0` 恒成立，`isUserVisibleLocked`（[usermanager.go](file:///workspace/v2raymg/pkg/proxy/usermanager/usermanager.go#L295)）对所有跨组用户一律返回 `true`。**修复**：启动时先向空 `ngStore` 播种默认组，再调用 `EnableClusterSync`。
> 2. **`UserSpec.Clone()` 切片数据竞争风险**：`UserEvent` 跨协程投递依赖 `Clone()`（[user.go](file:///workspace/v2raymg/pkg/proxy/core/contracts/user.go#L114-L130)）。新增 `InboundTags []string` 必须在 `Clone()` 中深拷贝切片，否则 `mutateUser` 修改标签时必触发并发读写竞争。
> 3. **四容器 `UserEventAdd` 分支漏判可见性**：[exec_runner.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/exec_runner.go#L1079-L1081)、[container.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/container.go#L935-L936)、[container.go](file:///workspace/v2raymg/pkg/proxy/containers/hysteria/container.go#L594-L617)、[container.go](file:///workspace/v2raymg/pkg/proxy/containers/snell/container.go#L471-L492) 仅在 `UserEventUpdate` 检查 `IsUserVisible`，收到 `UserEventAdd`（如集群 `SyncUpsertUser` 同步来跨组用户 [usermanager.go](file:///workspace/v2raymg/pkg/proxy/usermanager/usermanager.go#L1602-L1606)）会先分配 Forward 端口直至 30s 对账才拆除。**修复**：`UserEventAdd` 与 `UserEventUpdate` 统一走 `CanUserAccessInbound`，做到未授权用户从创建/同步一刻起即零入口。

在 [usermanager.go](file:///workspace/v2raymg/pkg/proxy/usermanager/usermanager.go) 统一收口两个线程安全入口：
- `CanUserAccessInbound(user *contracts.User, inboundAccessTags []string) bool`
- `CanUserAccessInboundByUsername(username string, inboundAccessTags []string) bool`（在 `m.mu.RLock()` 内直接读取 `m.users[username]` 完整实时记录，解决 [end_node_user.go](file:///workspace/v2raymg/pkg/rpc/server/end_node_user.go#L302-L307) `GetSub` 构造 `SubscriptionRequest` 时仅填 `{Username, AuthToken}` 剥离标签字段的问题）。

### 3.3 数据面（Forward 端口池）+ 用户侧 / 管理员控制面全链路执行矩阵

为什么不能只在 `/sub` 订阅接口隐藏链式 Inbound？因为在 `v2raymg` 共享凭据架构下（所有用户在同一 Inbound 共享底层 UUID/密码，靠 Forward 端口区分用户），**若数据面仍为未授权用户分配了 Forward 端口，攻击者一旦获知公网端口号或同节点其他用户的节点配置即可直接连入耗尽家宽带宽；且 `U` 个普通用户 $\times$ `M` 条私密家宽 Inbound 会白白消耗 `U×M` 个公网端口与监听协程**。

因此，两层隔离在全栈 6 个切面同步闭环：

1. **Forward 端口分配与动态回收（覆盖 `xray` / `mihomo` / `hysteria` / `snell` 全部 4 容器）**：
   - `FastAddInbound` 先将含 `AccessTags` 的 Inbound 持久化并写入容器内存，再执行 `reconcileUsersForInbound(tag)`：仅对 `CanUserAccessInbound == true` 的用户调用 `AddUser`（`GetBindPort`），未授权用户 **0 端口占用、0 Relay 协程**。
   - **运行期免重建动态修改 `access_tags`（`SetInboundAccessTags`）**：在 `Container` 接口新增 `SetInboundAccessTags(tag string, accessTags []string) error`（4 个容器统一实现，直连与链式 Inbound 通用），在锁内原子更新内存与 SQLite `inbounds.access_tags`（DB 写入失败回滚内存），**全程不重启、不重载底层代理进程，既有合法用户的长连接零中断**；随后立即调用升级为**双向对账**的 `reconcileUsersForInbound(tag)`（注意：现有 [exec_runner.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/exec_runner.go#L1204-L1226) 与 [container.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/container.go#L1041-L1060) 的 `reconcileUsersForInbound` 仅有新增分支，必须补齐“遍历 `listAddedUsers()` 对不再满足 `CanUserAccessInbound` 的失权用户立即 `RemoveUser` 回收端口并断开 Relay”的清理分支）。
   - 容器 `handleUserEvent(Add/Update)` 与 30s `reconcileUsers` 对每个 `(user, inbound)` 对执行二次校验：**原先不可见变为可见 $\rightarrow$ 立即 `AddUser` 分配端口；原先可见因撤销 `inbound_tags` 或切换 `group` 变为不可见 $\rightarrow$ 立即 `RemoveUser` 销毁 Forward 规则并释放公网端口**。
   - `UserManager.SetNodeGroups`（[usermanager.go](file:///workspace/v2raymg/pkg/proxy/usermanager/usermanager.go#L267-L281)）更新节点分组后主动触发一次事件广播，消除最长 30s 的对账延迟窗口。
2. **清理旧 Inbound 端口残留（修复 `ReleaseInboundPorts` 越权撞口隐患）**：
   - 当前 `ReleaseInboundPorts`（[usermanager.go](file:///workspace/v2raymg/pkg/proxy/usermanager/usermanager.go#L1398-L1419)）删除规则时保留了 `user.PortMappings[dstPort]`。若旧公开 Inbound 释放后端端口 `P` 后被新建私密链式 Inbound 抽中复用，旧用户的 `GetUserPortByDst` 会误命中残留记录。修复为移除规则时同步清理该 `dstPort` 对应的 `BindPorts` 与 `PortMappings` 项。
3. **订阅生成（`GET /sub`）**：
   - [manager.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/manager.go#L42-L69) 先判 `IsUserVisible(user)`；各容器 `GetUserSubscriptions` 逐 Inbound 判 `CanUserAccessInboundByUsername`；
   - 修复 [end_node_user.go](file:///workspace/v2raymg/pkg/rpc/server/end_node_user.go#L324-L331)：当节点因权限过滤返回 0 条 URI 时，返回 `Code = 0, Uris = nil` 而非 `Code = 300` 错误，避免 `/sub?target=all` 聚合跨节点订阅时狂刷错误日志。
4. **管理员控制面与用户侧查询（`GET /api/inbounds`、`GET /api/profile`、`GET /api/user`）**：
   - **管理员控制面**（`GET /api/inbounds`、`GET /api/inbound`、MCP `list_inbounds`、CLI）**始终完整返回全部 Inbound**（包括带 `access_tags` 的链式/私密 Inbound，后端 `chain_uri` 仅返回脱敏摘要），不受两层用户标签过滤；
   - **用户侧查询**（`GET /api/profile`、普通用户 `GET /api/user`）正常返回用户基本信息，但 `inbounds` 端口列表仅含该用户拥有活跃 Forward 规则的 Inbound（在 `IsUserVisible(user)` 为假时 `inbounds` 为空；且未授权 Inbound 因无 ForwardRule 不会出现在 `GetRulesByUser` 中）；管理员视图 `GET /api/user` 额外输出 `inbound_tags`。
5. **端口轮转（`POST /api/rotateInboundPort`、`POST /api/rotateAllPorts`）**：
   - 未授权用户在私密 Inbound 上无 ForwardRule，调用轮转返回与不存在标签一致的 `no forward rule found`（无存在性探测侧信道）；
   - **顺带修复存量 UDP 轮转退化 Bug**：[usermanager.go](file:///workspace/v2raymg/pkg/proxy/usermanager/usermanager.go#L1035-L1080) `RotateUserPortForInbound` 重建 `tmpRule` / `finalRule` 时补齐拷贝 `Network`、`ListenStack`、`UDPSessionIdleSec`，防止 `mihomo` 的 `hy2`/`tuic` UDP 链式入口轮转端口后退化为 TCP 断流。
6. **集群用户同步与原子创建**：
   - `AddUserRequest` 直接支持携带初始 `Role`、`TargetGroup`、`InboundTags` 原子落库并发一次 `UserEventAdd`（消除 [end_node_user.go](file:///workspace/v2raymg/pkg/rpc/server/end_node_user.go#L76-L95) 先无组创建分配全量端口、再 `SetUserGroup` 回收的瞬态抖动）；同步补齐 [copy_user_between_nodes_handler.go](file:///workspace/v2raymg/pkg/http/copy_user_between_nodes_handler.go#L95-L107) 跨节点复制 `inbound_tags`。
   - `proto.User`（[rpc_server.proto](file:///workspace/v2raymg/pkg/rpc/proto/rpc_server.proto#L7-L25)）新增 `repeated string inbound_tags = 16; bool update_inbound_tags = 17;`；
   - 集群摘要 [hash.go](file:///workspace/v2raymg/pkg/proxy/usermanager/sync/hash.go#L60-L64)：仅当排序去重后的 `len(u.InboundTags) > 0` 时追加 `"inbound_tags"` 标记与 `\x00` 连接串，保证空标签存量用户与旧版本节点 Hash 100% 兼容。

---

## 4. 需求 3 设计：后端代理 URI 解析与双内核动态 Outbound 编排

### 4.1 独立 `pkg/proxy/core/chain` 解析/渲染层与家宽 `codec` 补齐

仓库 [pkg/proxy/core/subscription/codec/](file:///workspace/v2raymg/pkg/proxy/core/subscription/codec/node.go) 已有 7 种协议解码器，但在**真实家庭宽带/软路由导出 URI** 场景下存在若干缺口，且容器层不应反向依赖客户端订阅模板包 `subscription/converter`。因此设计如下分层：

1. **扩展与加固 `pkg/proxy/core/subscription/codec/`**：
   - **新增 `socks5.go`**：支持 `socks5://`、`socks://`、`socks5h://`，同时兼容标准明文 `socks5://user:pass@host:port` 与 v2rayN `socks://BASE64(user:pass)@[ipv6]:port` 格式（家宽光猫/软路由最常见轻量落地协议）。
   - **新增 `http.go`**：支持 `http://`、`https://` 代理 URI（含 Basic Auth、`?sni=`、`?insecure=1`）。
   - **补齐存量解码器 5 处家宽高频缺口**：
     - [vmess.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/codec/vmess.go#L139)：兼容 v2rayN 导出的数字/字符串 `"allowInsecure": 1|"1"|true`（家宽无 80/443 公网端口普遍用自签 TLS）及 `"scy"` 加密方式；
     - [hysteria2.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/codec/hysteria2.go#L90-L92)：完整保留 `hy2://user:pass@host:port` 中的 `:pass`（原实现只取 `Username()` 截断了 userpass 认证），并解析家宽防运营商 UDP QoS 最常用的端口跳跃参数 `?mport=20000-50000` 与 `?pinSHA256=`；
     - [shadowsocks.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/codec/shadowsocks.go#L73-L93)：兼容 OpenWrt PassWall/SSRPlus 导出的整串 Base64 `ss://BASE64(method:pass@host:port)#tag`，完整保留 `shadow-tls` 插件密码与版本；
     - [vless.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/codec/vless.go#L162-L172) / [trojan.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/codec/trojan.go#L149-L155)：补齐 `type=httpupgrade` / `type=h2` 的 `path`、`host` 及 VLESS `encryption` 参数。
2. **新建 `pkg/proxy/core/chain/` 包**：
   - `ParseAndValidate(rawURI string, policy AddressPolicy) (*OutboundSpec, error)`：调用 `codec.Decode` 产出协议无关的 `OutboundSpec`，执行端口范围、防自环及 SSRF 安全策略校验，并提取 URI `#fragment` 作为默认 `Remark`；
   - `RenderMihomoProxy(proxyName string, spec *OutboundSpec) (map[string]any, error)`：渲染 `mihomo` `proxies[]` 字典；
   - `RenderXrayOutbound(outboundTag string, spec *OutboundSpec) ([]byte, error)`：渲染 `xray` 原生 Outbound JSON（`domainStrategy: "AsIs"`）；
   - `RedactURI(rawURI string) string`：统一剥离所有协议（含 `vmess://` 整串 JSON 与 `socks://` Base64 userinfo）的密钥信息供日志与查询脱敏使用。

### 4.2 `mihomo` 容器动态 Outbound 编排（已实机验证）

当前 `mihomo` 使用 `Mode: "global"` + 空 `Proxies` + `Rules: ["MATCH,DIRECT"]`（[container.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/container.go#L785-L798)）。改造极度内聚：

1. **基础配置调整**（[process.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/process.go#L23-L34) & [container.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/container.go#L785-L798)）：
   - `IPv6: true`（确保后端为纯公网 IPv6 DDNS 家宽时正常解析 AAAA 并拨号）；
   - `Mode: "rule"`，兜底规则保持末尾 `"MATCH,DIRECT"`（所有存量 `direct` Inbound 无 `proxy` 字段且不命中链式规则，行为 100% 不变）。
2. **1:1 专属绑定生成**（[config_builder.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/config_builder.go#L22-L41) & [profilegen.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/profilegen.go#L25-L45)）：
   - 对每个 `inbound_type == "chain"` 的 Inbound，生成唯一出站名 `proxyName := "__chain_out__:" + inb.Tag()`；
   - `RenderMihomoProxy` 生成节点字典追加至 `base.Proxies`；
   - **双重路由锁死**：① 在该 listener map 上直接设置 `"proxy": proxyName`（命中 `mihomo` `BaseOption.SpecialProxy`，$O(1)$ 直派绕过规则表遍历）；② 在 `base.Rules` 顶部插入 `"IN-NAME," + inb.Tag() + "," + proxyName` 双保险。
3. **热更与自愈**：复用现有 `rc.PutConfigs(ctx, data, false)` 推送全量 YAML + 30s `reconcileDrift()` 自动对账。经 `Mihomo Meta v1.19.32` 实机测试验证：热增删链式 Inbound 对存量直连与链式长连接 **0 断连**，目标域名原样封装送至后端 **0 本地 DNS 泄漏**，后端断连时 **严格 Fail-Closed 绝不回退 DIRECT**。

### 4.3 `xray` 容器动态 Outbound 编排（已实机验证）

遵循 [PROJECT_GUIDE.md](file:///workspace/v2raymg/PROJECT_GUIDE.md#L67) 核心决策 #1（**不嵌入 `xray-core` Go 库**），无需手写数千行各协议 Outbound 的 Protobuf `TypedMessage` 构造器，直接通过 `exec.CommandContext` + **`cmd.Stdin` 内存管道**调用同二进制 `xray api` 子命令与本地 gRPC `62789` 通信（**零 `/tmp` 明文临时文件落地**）：

```mermaid
sequenceDiagram
    participant API as EndNodeServer
    participant Store as SQLite InboundStore
    participant Exec as Xray Executor
    participant Xray as Xray Process (gRPC :62789)
    participant UM as UserManager / Forward

    API->>Exec: FastAddInbound(tag, type="chain", chain_uri, access_tags)
    Exec->>Exec: ClaimInboundPort(127.0.0.1:port) + RenderXrayOutbound
    Exec->>Xray: 0. 幂等清理残留 rmrules / rmo (忽略不存在)
    Exec->>Xray: 1. xray api ado stdin: {"outbounds":[...]}
    Exec->>Xray: 2. xray api adrules -append stdin: {"routing":{"rules":[...]}}
    Note over Exec,Xray: 若步骤 2 失败 -> 立即 rmo 回滚 Outbound
    Exec->>Xray: 3. gRPC HandlerService.AddInbound (127.0.0.1:port)
    Note over Exec,Xray: 若步骤 3 失败 -> 立即 rmrules + rmo 逆序回滚
    Exec->>Store: 4. Save(InboundRecord 含 inbound_type, access_tags, chain_uri)
    Exec->>UM: 5. reconcileUsersForInbound (仅向通过两层 Tag 的用户开公网端口)
```

> [!TIP]
> **Xray 26.2.6 实机验证关键避坑点**
> - **顶层 JSON 包裹要求**：`xray api ado ... stdin:` 与 `xray api adrules -append ... stdin:` 内部解析顶层 `conf.Config`，**不能传裸对象**（裸传规则会触发 Xray CLI `nil pointer` 崩溃）。必须分别包裹为 `{"outbounds": [<outboundObj>]}` 与 `{"routing": {"rules": [{"type": "field", "ruleTag": "__chain_rule__:<tag>", "inboundTag": ["<tag>"], "outboundTag": "__chain_out__:<tag>"}]}}`。
> - **`EnsureConfig` 自动补齐与路由顺序检查**：[config_renderer.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/config_renderer.go#L156-L161) `api.services` 新增 `"RoutingService"`；[exec_runner.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/exec_runner.go#L283-L293) `EnsureConfig()` 启动时对存量配置文件自动检查补齐 `"RoutingService"`，并校验 `routing.rules` 末尾不存在无条件全匹配直连规则（防止 `-append` 被提前截获导致直连泄漏）。
> - **Sniffing 防篡改**：链式 Inbound 若开启 `sniffing`，强制设置 `"routeOnly": true`，防止前置机嗅探将目标域名改写为前置机视角解析出的 IP。
> - **删除与重启重放闭环**：`RemoveInboundConfig` 按 `ReleaseAllUserPorts` $\rightarrow$ `Store.Delete` $\rightarrow$ `RemoveInbound` $\rightarrow$ `rmrules` $\rightarrow$ `rmo` 严格逆序清理；修复 `Executor.Restart()` 与 [updater.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/updater.go#L183-L195) 升级重启后自动调用 `Restore()` 按正序重放所有链式 Outbound、路由规则与 Inbound。

---

## 5. 查缺补漏：代理工程与资源隔离 8 大深水区风险对策

### 漏洞 1：容器 Inbound 默认监听 `0.0.0.0` 导致绕过 Forward 层与两层 Tag 隔离
- **现状隐患**：`mihomo` 旧路径已绑 `127.0.0.1`（新 `ProtocolParams` 路径仍允许自定义 `listen`），但 `xray` `FastAddInbound`（[exec_runner.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/exec_runner.go#L1900) 等多处）默认监听 `"0.0.0.0"`！由于同 Inbound 所有用户共享一把 UUID/密码，一旦后端端口暴露在公网网卡，任何扫到该端口的人都可绕过两层 Tag 权限、绕过流量统计直接盗刷家宽流量。
- **修复方案**：将 `FastAddInbound` 默认监听地址统一收紧为 `"127.0.0.1"`（同步更新 [fast_add_inbound_test.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/fast_add_inbound_test.go#L162) 单测），并对所有 `inbound_type == "chain"` 的请求**硬性锁定 `127.0.0.1`**（传入其他地址直接拒绝）。

### 漏洞 2：自环死锁风暴、管理端口 SSRF 与家宽 DDNS / 组网地址策略（已确认方案 A）
- **现状隐患**：若管理员误将 `chain_uri` 指向本节点自身的公网转发端口、容器端口或管理端口（`62789`、`9090`、`http_port`、`rpc_port`），要么瞬间形成无限代理死循环耗尽全节点 FD，要么引发通过代理探测本地管理 API 的 SSRF；反之，若直接照搬 [ssrf.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/ssrf.go#L44-L89) 一刀切封禁所有非公网 IP，则会误杀通过 **Tailscale（`100.64.0.0/10`）/ WireGuard（RFC1918）** 内网打洞接入前置机的无公网 IP 家宽。
- **定稿策略（方案 A）**：
  1. **无条件硬拦截**：未指定地址（`0.0.0.0`/`::`）、多播、云元数据与链路本地段（`169.254.0.0/16`、`fe80::/10`）、以及目标指向本机 IP / Loopback / `ProxyHost` 且端口命中 `reservedManagementPorts` 或 `PortAllocator` 已分配端口的 URI；
  2. **默认放行公网与内网组网段**：默认允许公网 IP/域名 + Tailscale CGNAT（`100.64.0.0/10`）+ RFC1918 私网段（`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`）+ IPv6 ULA（`fc00::/7`）；
  3. **Loopback 默认拦截 + 配置开关**：默认拒绝 `127.0.0.0/8` 与 `::1`，在 `end_node` 配置新增 `allow_loopback_chain_target: false`（默认 `false`，供本机 `frps` 端口映射场景按需显式开启，开启后仍硬拦截本机管理端口与 `PortAllocator` 自管端口）；
  4. **动态 DDNS 域名保鲜**：`chain_uri` 若为 DDNS 域名，在 `mihomo` 与 `xray`（`domainStrategy: "AsIs"`）中均原样保留域名，绝不在添加时解析固化为静态 IP，确保家宽 PPPoE 重拨换 IP 后自动随 DNS TTL 恢复。

### 漏洞 3：限速与并发控制边界（已确认：复用现有用户级限速）
- **定稿边界**：首期不引入单 Inbound 独立限速桶，直接 100% 复用现有 Forward 层的用户级带宽限速（`upload_bps` / `download_bps` $\rightarrow$ `DefaultForwardManager.userBandwidth[username]`，见 [forward_manager.go](file:///workspace/v2raymg/pkg/proxy/forward/forward_manager.go#L103)）与用户级最大客户端限制（`max_clients` $\rightarrow$ `userClientLimiters[username]`）。
- **实现注意点**：在 `FastAddInbound` 与 Forward 规则创建路径中**不暴露也不误填** `ForwardRule` 上未独立成桶的 `UploadBytesPerSec` / `DownloadBytesPerSec` 规则级字段，避免与现有用户级共享令牌桶产生覆盖串扰，保持 Forward 数据面零改动、高稳定。

### 漏洞 4：多 Inbound / 链式节点在客户端订阅中的重名覆盖与报错
- **现状隐患**：当前 [exec_runner.go](file:///workspace/v2raymg/pkg/proxy/containers/xray/exec_runner.go#L1612-L1626) 与 [subscription.go](file:///workspace/v2raymg/pkg/proxy/containers/mihomo/subscription.go#L106-L109) 生成订阅时全部使用节点 `NodeName`（忽略 `inbound.Tag()`），且 `/sub` RPC 只传 URI 字符串导致下游 [uri_convert.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/uri_convert.go#L139-L157) 反解后 `InboundTag` 为空。当同一前置机挂载“上海电信家宽”、“北京联通家宽”等多个同协议链式 Inbound 时，Clash（[clash.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/converter/clash.go#L769)）与 Surge（[surge.go](file:///workspace/v2raymg/pkg/proxy/core/subscription/converter/surge.go#L249)）会产出完全同名的节点直接报错拒绝加载！
- **修复方案**：
  1. 支持创建时指定 `remark`（未传且为 `chain` 时自动取 `chain_uri` 的 `#fragment`，仍为空则回退 `inbound.Tag()`）；
  2. 在 `xray` 与 `mihomo` 生成订阅 URI 源头将非默认/链式 Inbound 的 `NodeName` 拼接为 `<NodeName>-<remark|tag>` 写入 `#fragment`；
  3. 在 `ClashConverter` 与 `SurgeConverter` 输出前增加同名自动追加 `_2`、`_3` 兜底去重。

### 漏洞 5：UDP / QUIC 跨链式承载兼容性与 MTU 分片陷阱
- **协议组合指引**：
  - **后端为 TCP 类代理（VLESS / VMess / Trojan / SS / AnyTLS）**：`mihomo` 与 `xray` 均支持 UDP-over-TCP（UoT / XUDP），客户端 DNS/游戏/QUIC UDP 流量可正常封装穿透；
  - **后端为裸 `http(s)://` 代理**：HTTP CONNECT **仅支持 TCP**，不支持 UDP 转发（文档与日志明确提示）；
  - **双层 QUIC 套娃预警**：若前置入口选 `hy2`/`tuic` 且后端家宽也是 `hy2`/`tuic`，虽 `mihomo` 支持拨号，但双层加密叠加会使内层 QUIC 负载超过外层 ~1200B MTU 阈值导致握手超时或吞吐骤降。**最佳实践**：前置机入口优先使用 `vless+reality/tls` 或 `anytls`（TCP），后端家宽按线路质量选 `hy2`（带 `mport` 防 QoS）或 `vless`/`ss`/`socks5`。

### 漏洞 6：后端 URI 凭据全链路脱敏（日志 / HTTP / MCP）
- `chain_outbound_uri` 含家宽密码/UUID，仅存储在本地 SQLite `inbounds` 表供 `Restore` 重放；
- 日志统一调用 `chain.RedactURI`；`GET /api/inbounds` 与对应 MCP 工具 `list_inbounds` 仅输出 `inbound_type`、`access_tags`、`remark`、`chain_protocol` 与脱敏后的 `chain_target`（`host:port`），**绝不在任何只读 API 或 MCP 响应中回显原始 `chain_uri`**。

### 漏洞 7：流量计费口径清晰化
- 严格遵守 [PROJECT_GUIDE.md](file:///workspace/v2raymg/PROJECT_GUIDE.md#L69) 决策 #5：流量统计 100% 取自 Forward 层 `TrafficCounter`（键为 `{container}:{inboundTag}:{username}`）。链式 Inbound 上产生的流量天然计入对应授权用户的累计与增量统计，且在 `GetBandwidthStats` 中可按 `inboundTag` 单独观测每条家宽线路的实时上下行负载，无需改动计费采集器。

### 漏洞 8：仓库三大 CI 架构守卫 100% 对齐
- 新增/扩展 HTTP 接口一律走 `Endpoint[Req, Resp]` + `registerEndpoint` 自动暴露为 MCP Tool（通过 `TestMCP_RouteDriftGuard`）；
- 新增路由同步登记 [e2e_covered_routes.txt](file:///workspace/v2raymg/pkg/http/testdata/e2e_covered_routes.txt) 并在 [cluster_e2e_api_test.go](file:///workspace/v2raymg/pkg/proxy/systemtest/cluster_e2e_api_test.go) 补充 3 节点真集群跨节点断言（通过 `TestRouteCoverage`）；
- 新增 gRPC 方法同步登记 [end_node_server.go](file:///workspace/v2raymg/pkg/rpc/server/end_node_server.go#L96-L136) `methodRspMap`，并更新 [PROJECT_GUIDE.md](file:///workspace/v2raymg/PROJECT_GUIDE.md) 与 `cmd/cli/`。

---

## 6. 接口契约与分阶段实施路线

### 6.1 核心 API 契约（向后兼容 + 自动同步 MCP）

1. **`POST /api/inbound/fast`（MCP: `fast_add_inbound`）扩展字段**：
   ```json
   {
     "target": "hk-front-01",
     "container": "mihomo",
     "tag": "chain-sh-telecom",
     "remark": "上海电信家宽",
     "protocol": "vless",
     "security": "reality",
     "reality_target": "www.microsoft.com:443",
     "inbound_type": "chain",
     "chain_uri": "hy2://user:pass@sh-home.ddns.example:28443?sni=bing.com&insecure=1&mport=20000-40000#SH-Home",
     "access_tags": ["home-sh", "internal-vip"]
   }
   ```
   - 不传 `inbound_type`、`chain_uri`、`access_tags` 时，行为与现有直连公开 Inbound 100% 一致。
2. **`PUT /api/inbounds/tags`（新增，MCP: `set_inbound_tags`，gRPC: `SetInboundAccessTags`）**：
   ```json
   {
     "target": "hk-front-01",
     "container": "mihomo",
     "name": "chain-sh-telecom",
     "access_tags": ["home-sh"]
   }
   ```
   - 支持跨节点（`target`）对任意容器（`xray` / `mihomo` / `hysteria` / `snell`）下的已有 Inbound 动态更新可见性标签；
   - 传 `"access_tags": []` 即清空标签恢复为对本节点可用用户默认公开；
   - 不触碰代理内核、不断开仍然合法的长连接，即时持久化并执行双向 `reconcileUsersForInbound(name)` 回收失权用户端口 / 分配新授权用户端口。
3. **`POST /api/user` & `PUT /api/user` & `GET /api/user`（MCP: `add_user` / `update_user` / `list_users`）**：
   - 新增 `"inbound_tags": ["home-sh"]`（`PUT /api/user` 使用 `*[]string` 精准区分“不传保持原样”与传 `"inbound_tags": []` 清空用户所有 Inbound 标签）。
4. **`GET /api/inbounds` & `GET /api/containers`（MCP: `list_inbounds` / `get_node_containers`）增强返回**：
   - `GET /api/inbounds` 每个条目返回 `container`、`name`、`inbound_type`（`"direct"`/`"chain"`）、`access_tags`、`remark`、以及链式专有的脱敏 `chain_protocol` 与 `chain_target`；
   - `GET /api/containers` 每个容器增加 `supports_chain` 能力标识。

### 6.2 4 阶段落地实施计划

| 阶段 | 核心交付内容 | 验证门禁 |
| :--- | :--- | :--- |
| **Phase 1：存储迁移、存量加固与两层 Tag 隔离** | SQLite Migration 18；修复 `server.go` 首启分组顺序、`UserSpec.Clone()`、`ReleaseInboundPorts` 残留、`RotateUserPortForInbound` UDP 退化、`xray` 默认 `127.0.0.1`；落地 `CanUserAccessInbound`、集群同步 Hash、4 容器数据面+控制面过滤、`PUT /api/inbounds/tags` 动态改签与订阅防重名 | `usermanager`、`store`、`http`、`containers/*` 单元测试全绿 |
| **Phase 2：`codec` 补齐与 `chain` 解析/防环/渲染包** | 新增 `codec/{socks5,http}.go` 并修复 5 处家宽解码缺口；实现 `pkg/proxy/core/chain`（方案 A 防自环/组网地址校验、`RenderMihomoProxy`、`RenderXrayOutbound`、`RedactURI`） | 覆盖各协议 URI 矩阵、Tailscale/RFC1918/Loopback 准入、IPv6/DDNS 与脱敏表驱动单测 |
| **Phase 3：`mihomo` & `xray` 动态链式出站** | `ChainCapable` 接口与跨容器全局 `tag` 前置查重；`mihomo`（`rule` 模式 + `IPv6` + `listener.proxy`）与 `xray`（`RoutingService` + `stdin:` 零泄漏时序 + `Restore`/`Restart` 重放） | 容器生命周期、失败回滚、进程重启恢复单测 |
| **Phase 4：真双跳系统测试与 3 节点集群 E2E** | 编写前置容器 $\rightarrow$ 模拟家宽后端 $\rightarrow$ Echo Server 真双跳连通性、远端 DNS 零泄漏、后端宕机 Fail-Closed 测试；补齐 `TestClusterE2E`（含 `PUT /api/inbounds/tags` 跨节点验证）、`TestRouteCoverage`、`TestMCP_RouteDriftGuard`、`cmd/cli` 与 `PROJECT_GUIDE.md` | 全量测试 + 真双跳集成测试 + 3 节点集群 E2E 全绿 |

---

## 7. 已确认的设计决策记录

1. **后端 `chain_uri` 地址准入策略（方案 A）**：无条件硬拦截本机管理端口/已分配代理端口自环与云元数据段（`169.254.0.0/16`）；默认放行公网地址 + Tailscale（`100.64.0.0/10`）+ WireGuard/内网（RFC1918、`fc00::/7`）；默认拦截 `127.0.0.0/8` / `::1`，提供 `end_node.allow_loopback_chain_target` 开关按需放行本机 `frps` 映射端口。
2. **Inbound 可见性标签支持运行期动态修改**：`inbound_type` 与 `chain_uri` 仅新建时锁定不可变；新增 `PUT /api/inbounds/tags`（及 MCP `set_inbound_tags`）支持跨节点、跨全部 4 容器免重启动态修改 `access_tags`，即时持久化并双向重算 Forward 端口分配与回收。
3. **限速策略保持精简**：首期不引入 Inbound 级共享限速桶，完全复用现有用户级带宽限速（`upload_bps` / `download_bps`）与客户端数限制（`max_clients`）。
