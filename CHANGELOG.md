# CHANGELOG.md

> 从 v2.6.0 起,发布条目以 `## <日期> — <版本> <主题>` 开头。完整的设计取舍写在
> 对应的 annotated tag message 里(`git tag -l --format='%(contents)' v2.9.0`),
> 这里只留"改了什么、升级要注意什么"。

## 2026-11-26 — v2.10.0 服务端链式代理 + 两层 Tag 隔离 + 内嵌 MCP + 反代 grpcs:// 与泄漏修复

本次版本合入四组主要能力与稳定性修复：服务端链式代理（Chained Proxy）与两层 Tag 物理隔离、内嵌无状态 MCP Server（`/mcp`）、反代感知广播地址（`grpcs://`）与 `http_token` 自动生成，以及多项内存 / Goroutine 泄漏修复。

### Highlights

#### 1. 服务端链式代理（Chained Proxy）与两层 Tag 物理隔离（Migration 18）
- **1:1 链式入站（`inbound_type="chain"`）**：`POST /api/inbound/fast` 支持在新建 inbound 时绑定 `chain_uri`（支持 `socks5`/`http`/`ss`/`vmess`/`vless`/`trojan`/`hy2`/`tuic` 及家宽 URI 兼容）与可选 `remark`、`access_tags`；禁止原地篡改已有 inbound 的类型或后端 URI，并在全容器与持久化层做全局 tag 防覆盖校验
- **Loopback 收口与 Fail-Closed 零直连泄漏**：`FastAddInbound` 默认监听地址收紧为 `127.0.0.1`，chain 入站强制锁定 `listen` / `listen_addr` 为 `127.0.0.1`；`mihomo` 采用 `Mode: "rule"` + `IPv6: true` + `listener.proxy` + 顶部 `IN-NAME` 规则双重锁定，`xray` 启用 `RoutingService` 并按 `ado` → `adrules -append` → `AddInbound` 顺序注入（失败逆序回滚，重启等待 gRPC 就绪后重放），后端断连时严格 Fail-Closed，不回落直连
- **后端地址安全准入（方案 A）**：`pkg/proxy/core/chain` 统一拦截未指定地址、非标准/尾部点号数值 IPv4、多播/广播、链路本地/云元数据段（`169.254.0.0/16`、`fe80::/10`）及本机管理/已分配端口；默认拒绝 loopback，显式配置 `end_node.allow_loopback_chain_target: true` 后可放行（仍拦截管理/已分配端口）；域名原样保留以支持 DDNS（Xray 出站设 `targetStrategy: "AsIs"`）
- **两层 Tag 命名空间物理隔离**：Layer 1（Node 层 `local_node_groups` ↔ `UserSpec.TargetGroup`）与 Layer 2（Inbound 层 `InboundMeta.AccessTags` ↔ `UserSpec.InboundTags`）物理隔离、允许同名重用且绝不交叉穿透，同时约束数据面（4 容器 Forward 端口分配与本地 `AuthHysteria2`）与用户视图（`/sub`、`/api/profile`、普通用户 `GET /api/user`）
- **运行期免重启改签与脱敏查询**：新增 `PUT /api/inbounds/tags`（gRPC `SetInboundAccessTags` / MCP `set_inbound_tags`）跨节点原子更新 `access_tags` 并即时对账，不重启代理进程；`GET /api/inbounds`（及 MCP `list_inbounds`）仅返回脱敏后的 `chain_protocol` 与 `chain_target`，绝不回显原始 `chain_uri`；多 inbound 订阅输出 `<NodeName>-<remark|tag>` 并在 Clash/Surge 转换时自动去重

#### 2. 内嵌无状态 MCP Server（`/mcp`）
- 在现有 `end_node.http_port` 上挂载 Stateless Streamable HTTP MCP 端点 `/mcp`（基于官方 Go SDK），通过统一 `Endpoint[Req, Resp]` 抽象与 HTTP API 共享执行函数、参数校验、鉴权中间件与多节点 gRPC 扇出（详见 `docs/mcp-guide.md`）
- 新增 `end_node.mcp.enabled`（默认 `false`，关闭时返回 404）与 `end_node.mcp.read_only`（默认 `true`）；全开放时向 Admin 暴露 33 个工具（9 只读 + 24 写，9 个高危写工具标记 `DestructiveHint: true`）、向普通用户暴露 7 个工具，物理隔离 `adminServer` 与 `userServer`
- MCP 响应层对 `auth_token` 统一脱敏为 `"[REDACTED]"`，且新增路由/MCP 漂移守卫测试防止新接口遗漏

#### 3. 内存 / Goroutine 泄漏与热点分配修复
- **Ping 采集器有界化**：关闭 `pro-bing` 的 `RecordRtts`/`RecordTTLs` 并限定每轮 `Count` 与 `Timeout`，以原子索引与 `WaitGroup` 收口生命周期；新增 `end_node.enable_ping_check`（默认 `false`），双探测均关闭时短路 `PingCollector` 并在停服时停止
- **Xray 生命周期与退出清理**：`xray.Executor` 以 `lifecycleMu` 串行化启动/停止，启动失败回滚事件订阅与对账循环，新增幂等 `Close()` 退订 `UserManager` 事件
- **JWT 黑名单与状态采集**：`tokenBlacklist` 记录过期时间并支持懒清理与周期扫除；`GetStatus` 行数统计改用 `sync.Pool` 复用 32 KB 缓冲流式读取，消除每秒高频大块内存分配

#### 4. 反代感知的广播地址（`grpcs://`）+ `http_token` 自动生成（Migration 17）
- 新增 `end_node.reverse_proxy.grpc_addr`，形如 `grpcs://cluster.example.com:443`。留空 = 广播 `grpc://{proxy_host}:{rpc_port}`，**与 v2.9 逐字节一致**。缺 scheme 按 `grpc://`，缺端口按 grpc→80 / grpcs→443（比照 http/https）；本节点 gRPC server 仍为明文 h2c，TLS 由反代终止
- `proto.Node` 新增 `bool tls = 6`。**传输方式被声明，不被探测** —— 没有任何 TLS 失败回退明文的降级逻辑
- `static_nodes` 支持 URL 标量（`"grpcs://node2.example.com:443"`），与原 `{host, port}` mapping 共存
- **`end_node.http_token` 留空时自动生成**，存进数据库（`local_secrets`，migration 17），重启保持不变，**只在生成那一次把明文打进日志**。它是纯本机凭据，从不跨节点比较
- 顺带修一个**既有鉴权绕过**：`getAuthHandlerFunc` 原无空值守卫，`http_token` 为空时不带 `X-Token` 的请求 `"" == ""` 直接通过 —— `/api/metrics` 此前无鉴权；现空 token 一律拒绝
- 顺带修 `IsComplete` / `StaticNode.IsValide` 的 `Port > 1000`（防止 443 被丢弃）以及拨号地址拼接改用 `net.JoinHostPort`（修复 IPv6 字面量）

### ⚠️ 升级注意

**不配 `reverse_proxy` / `mcp` / `chain` 入站时默认行为与 v2.9 兼容**，proto / 配置 / 静态节点写法全部向后兼容。

**数据库迁移与回滚（Migration 17 & 18）**：Migration 17 与 18 仅包含 `ADD COLUMN ... DEFAULT ...` 与新建 `local_secrets` 表，迁移器对 `Version <= maxVersion` 是跳过而非报错（只有版本**断档**才报错），因此 **v2.9 二进制可以直接打开 v2.10 的库**（已实测）。但需注意：若在 v2.10 中已经创建了 `inbound_type="chain"` 的入站或配置了 `access_tags`/`inbound_tags`，回滚到 v2.9 后老二进制会忽略这些新增列（chain 入站将失去链式出站隔离语义、Layer 2 tag 门控不再执行），请在回滚前先清理相关 chain 入站或确认影响范围。

**唯一会因默认行为收紧而受影响的场景**：`enable_prometheus: true` **且** `http_token` 留空**且**抓取端不带 `X-Token` 的组合。此前 `getAuthHandlerFunc` 没有空值守卫，空 token 时 `"" == ""` 让 `/api/metrics` 事实上无鉴权；现在 token 会被自动生成、空 token 也一律拒绝，这个端点开始要求认证。升级前请给抓取端配上 token（启动日志会打印一次），或显式设置 `http_token`。其余 `/api/*` 路由不受影响（主鉴权中间件原本就有 `xtoken != ""` 守卫，JWT 登录不受影响）。

**其他配置与内核注意**：
- **Xray 26.2.6+ 链式出站限制**：Xray 26.2.6 已移除 `allowInsecure`、`network: "http"`（`h2`）与 `security: "xtls"`，在 Xray 容器上添加含这三类参数的 `chain_uri` 会被直接拒绝，请改用 `mihomo` 容器（或将传输改为 `xhttp`）。
- **`end_node.enable_ping_check` 默认 `false`**：如依赖节点间 ICMP/TCP ping 指标，请在配置中显式设为 `true`（或通过 `SetPingCheck` RPC 开启）。
- **`FastAddInbound` 默认监听地址收紧为 `127.0.0.1`**：如需直连监听公网接口请显式传入 `listen_addr`（`chain` 入站强制锁定 `127.0.0.1`）。
- **启用 `grpcs://` 必须按顺序走**：
  ```
  1. 全集群升到 v2.10(不配 reverse_proxy)  → 行为与 v2.9 一致,零风险
  2. 逐台配 grpc:// 反代(无 TLS)           → 老节点也能拨通,仍然兼容
  3. 全部升完后才改成 grpcs://              → 此时已无老节点
  ```
  老节点不认识线上的 `tls` 标志，只会用明文拨 —— 所以第 3 步之前开 grpcs 会让老节点连不上该节点。配了 `grpcs://` 时启动会打 WARN 提醒这个前置条件。
- **已知后果**：CLI 的 `--domain` 缺省取 `node.GetHost()`，配了反代后该值变成集群面地址，不再适合当证书域名。配了反代就显式传 `--domain`。根因是 `proxy_host` 一个字段扛了三重身份（广播拨号 / 订阅链接 / ping 目标），拆分留作后续。

## 2026-07-28 — v2.9.1 管理 API 监听地址与 gRPC 解耦

`end_node.http_listen` 一直存在,但它留空时的行为与文档相反:`config.go` 和
`config.example.yaml` 都写着"回退到 listen",而代码自 `f3ebd40`(2026-03 重写)起
实际用 `127.0.0.1`。分歧跨越六个版本没被发现 —— 解析是 `cmd/server.go` 里一行内联
默认值,零测试。本次按文档语义统一:**留空继承 `listen`**。

### Highlights

- 解析收敛为 `EndNodeConfig.ResolveHTTPListen()`,带 9 个 case 的表驱动测试。
  `http_listen` 与 `listen` 都为空时回退 `127.0.0.1`(绑空 host 等于绑所有网卡)
- 新增 `HTTPListenIsInherited()`,把"继承来的暴露"与"显式配置的暴露"区分开
- 启动日志三态:**因继承而对外可达 → WARN**(没人显式选择过)、显式对外可达 → INFO、
  仅本机 → INFO。不用逐台翻配置就能从日志确认集群的暴露面
- `config.example.yaml` / `PROJECT_GUIDE.md` 改为描述真实语义,并写清集群配法

### ⚠️ 升级注意(行为变更)

**升级前必须检查**:`end_node.listen` 为 `0.0.0.0`(或任何非 loopback 地址)且**未配置**
`end_node.http_listen` 的节点,升级后管理 API 会从**仅本机变为对外可达**。

处理方式:给所有不该对外提供管理 API 的节点显式加上

```yaml
end_node:
  http_listen: 127.0.0.1
```

隐藏必须显式 —— 留空的方向是暴露。升级后这些节点的启动日志会打 WARN 点名该问题,
但请在升级前就配好,不要依赖事后看日志。

## 2026-07-28 — v2.9.0 端口分配只剩一个权威

线上 mihomo 报 `listen udp 127.0.0.1:10000: bind: address already in use`。10000
不是运气不好,是硬编码的:`NewDefaultInbound` 把 `port=0` 换成 10000,所有不显式传
端口的 inbound 都落在同一个地址,第一个绑上、其余静默失败(mihomo 接受配置、只在
自己日志里报绑定错误,v2raymg 照样落库并广告该 listener)。

### Highlights

- **进程内只剩一个 `PortAllocator`**。用户侧 forward 端口与四个容器的 inbound
  后端端口共用同一张分配表。此前有三个互不知情的决策者:forward 的分配器、xray 私有的
  `generateRandomPort(4000,5000)`(1001 个取值、不查可用性、不与自己的 inbound 去重)、
  以及 `port==0 → 10000` 的兜底常量
- **抽签范围 ≠ 记账范围**。`min_port/max_port` 只约束 `Allocate()` 的随机抽签;
  `AllocateSpecific()` 接受记账范围内任意端口。此前用抽签范围校验显式端口,导致范围外的
  端口永远登记不进来(于是无人阻止 `Allocate()` 再发一次),也让收窄抽签范围变成破坏性变更
- **显式端口绝不静默替换**。运维或持久化记录指定的端口要么拿到、要么报 `ErrPortInUse`,
  与 forward 侧对 pinned `ListenPort` 的既有规则一致。只有 `port=0` 才抽签
- **启动预扫**(`cmd/server.go` 第 2b 步):持久化 inbound 端口在任何容器构造之前认领。
  放进容器只能保证单容器内有序,而生产上的撞车是跨容器的
- **管理端口进 `Reserved`**(rpc / http / xray gRPC API / hysteria traffic-stats /
  mihomo external-controller)。xray 的 62789 不能漏 —— `killProcessOnPort` 会 SIGKILL
  任何持有该端口的进程,只挡 `pid > 1`,不看进程名也不排除自己
- 删除 `forward.GlobalForwardManager` 死单例,改经 `container.BuildOptions.PortClaimer`
  窄接口注入;删除 xray 中定义了却从未赋值/读取的 `ExecutorConfig.PortAllocator`
- 修正 `config.example.yaml` / `config/test-endnode.yaml` 的 `minport`/`maxport`/
  `userandom` 拼写 —— 与 yaml tag 不匹配、一直被静默丢弃,**从来没有人配置的端口范围真正生效过**

### 升级注意

- **无 proto / DB schema / 集群协议变更**,混部与滚动升级不受影响,现有配置文件不用改
- 存量库里两条 inbound 撞同一端口(很常见:两个 `port=0` 都被改写成 10000)时,启动会打
  ERROR 点名双方,**但不阻止启动**,行为与升级前一致 —— 只是从静默变成可见
- 新建 xray inbound 的自动端口从 `[4000,5000]` 改为共享抽签范围;存量 inbound 恢复原端口
  不受影响。注意 xray inbound 绑 `0.0.0.0`,与 mihomo/hysteria/snell 绑 loopback 不同
- `FastAddInbound` 显式指定已占用端口时现在返回 `ErrPortInUse`,而非报告成功后绑不上
- forward 端口恰好等于某 inbound 端口的用户会被重新分配一次,需重拉订阅。这类部署升级前
  本来就是坏的(两者抢同一端口),等于把静默故障换成一次可见变更

## 2026-07-28 — v2.8.1 种子只剩地址,名字一律从线上学

`static_nodes[].name` 从配置结构里彻底移除。种子只回答"往哪儿拨";目录自 v2.8.0 起按节点
自己的持久化身份索引,运维写在地址旁的标签从来不是任何东西的证据。

### Highlights

- YAML 解码是宽松模式,老配置里残留的 `name:` 被静默丢弃,**升级零改动**
- 自我识别从"猜名字"换成"问出来":`assertResponder` 比对应答方 `node_id` 与本机 id。
  原先按名字过滤只能抓到"名字恰好一模一样"那一种拼法,漏掉的自引用种子会被装上本机身份,
  导致节点**永远给自己发心跳**
- 名字漂移改为每次应答都收敛(id 相同、名字不同则原地刷新),不再只在首次注册时传播

### 升级注意

与 v2.8.0 完全兼容,可混合部署、可滚动升级。无 proto / schema / API 变更。种子在完成
首次交互前 `/api/node` 里 `name` 为空(默认心跳 10s,最多空 10s)。

## 2026-07-27 — v2.8.0 节点身份改由持久化 UUID 决定

节点目录从按 name 索引改为按 `node_id` 索引 —— 首次启动随机生成、持久化在本地 DB
(`local_node_identity` 单行表)的 UUID,节点自维护,**没有任何配置项**。name / host / port
全部降为可变属性。

### Highlights

- 改地址是同一条目的原地替换,不再是"陌生人跟自己的旧条目抢位置"。换址后集群收敛从
  ~80s(或旧地址仍可达时永不收敛)降到 ~5.5s
- **注册一律接受**,不因身份/地址不符而拒绝 —— 该撤回旧声明的实例已经不存在了,拒绝在结构上
  无解。"感知顶替"改由告警承担
- "删库但不改配置"的盲区靠**应答方身份**堵住:每个响应带 `responder_node_id`,调用方发现
  地址被别的身份接管就立刻置脏旧 id;置脏期拒绝 gossip 更新但不拒绝该节点自己的直接注册,
  且不立即回收(`DirtyNodeTTL = 3×NodeTimeOut = 180s`)
- migration 16:新增 `local_node_identity` / `cluster_seeds`,`cluster_nodes` 改以
  `node_id` 为主键

### 升级注意

`ComputeNodesSum` 不折入 `node_id`、心跳线上的 `nodesMap` 仍以 name 为 key —— 与 v2.7
保持 wire 兼容,**无需全集群同批升级**。proto 字段全部 additive。

## 2026-07-26 — v2.7.0 移除 center node 与 cluster name

`static_nodes` 是唯一的入网途径,动态发现的节点持久化到 `cluster_nodes` 表以抗重启。

### Highlights

- **center node 彻底移除**:它唯一的作用是发现,而 `static_nodes` 覆盖同样的能力且更好
  (带 `isLocal`、永不被 60s 超时回收、注册双向)。同时 center 是集群面唯一一条默认明文的
  链路,还携带集群主密钥
- **cluster name 一并移除** —— token 才是唯一的成员边界,名字是只会误拒合法节点的冗余第二因子
- 旧配置里的 `node_type: center` / `center_node` / `center_token` 一律忽略并在启动时逐条 WARN

### 升级注意

破坏性变更(`feat(cluster)!`):需全集群升级。`Node.cluster_name` 在 proto 里保留为 `reserved 3`。

## 2026-07-26 — v2.6.0 心跳目录摘要增量同步 + 转发层双栈

### Highlights

- 心跳不再每轮回带全量 `nodesMap`:两端各带 `nodes_sum`,稳态只比摘要;不一致时一次往返双向收敛。
  杀手开关 `end_node.cluster.node_sum_sync`(默认 true)
- 摘要必须排序后折叠(`cluster.ComputeNodesSum`,只折 name/host/port,不折运行期状态)——
  不排序会让已收敛的两端算出不同摘要,变成永久假性不一致
- 转发监听默认双栈(IPv4 + IPv6),由 `forward.listen_stack` 控制;单规则可用
  `ForwardRule.ListenAddr` / `ListenStack` 覆盖。双栈下两半都是 best-effort
- `forward.AddRule` 在 bind 撞上 EADDRINUSE 时改到另一个端口重试(仅限自动分配的端口;
  pinned 端口绝不静默替换)
- 新增 1-center + 3-end 集群回归套件,CI 强制

## 2026-04-25 — Mihomo Protocol Expansion Phase 6 (TUIC)

Adds TUIC v5 support to the mihomo container's structured `ProtocolParams`
path. TUIC is the second QUIC/UDP protocol after Hysteria2 — it reuses
the same forward-layer UDP rule and `waitUDPListener` test pattern, so
Phase 6 lands as a focused per-protocol addition rather than another
infrastructure expansion. v4 (token-based) is intentionally unsupported;
v2raymg targets v5 (uuid+password) for per-user attribution.

### Highlights

- New `parseTUIC`: required `uuid`+`password`, **strict
  `uuid.Parse` validation** (mihomo Alpha silently zeros invalid uuids
  via `FromStringOrNil`, which would collapse all users onto a single
  zero-uuid key), Transport forced nil (QUIC fixed), Security forced
  TLS (reality / none rejected). `congestion_controller` whitelist
  `bbr|cubic|new_reno`; `udp_relay_mode` whitelist `native|quic`.
- `MihomoInbound.AddUser` now picks UDP for TUIC via the same
  `forwardNetworkForProtocol` hook hy2 introduced.
- `fillTuicListener` emits `users: { <uuid>: <password> }` (v5 single-user;
  v4 `token:` is mutually exclusive and never written),
  `certificate`/`private-key`, `alpn: [h3]` default, plus
  **explicit `congestion-controller: bbr` default**. Mihomo's
  parse-time bbr default lives in `listener/parse.go:116` and runs only
  through `parse.ParseListener`; emit the value explicitly so the
  listener's wire-level behaviour is obvious from the yaml.
- `ClashProxy` gains five TUIC client-only fields:
  `congestion-controller`, `udp-relay-mode`, `reduce-rtt`,
  `heartbeat-interval`, `disable-sni`. Note **`reduce-rtt`** (not
  `zero-rtt-handshake`) — that's the actual mihomo client tag name;
  `TestConvertTuic_ReduceRTT` locks a yaml-marshal assertion against
  the wrong key name.
- `convertTuic` reads from Extensions and converts
  `heartbeat_interval` to integer milliseconds, accepting **both Go
  duration strings ("10s")** and **raw ms ints ("10000")** so an
  operator copying mihomo's literal default doesn't silently land on 0.
- New codec `tuic.go` (dae-spec v5 only): query keys use underscores
  (`congestion_control`, `udp_relay_mode`, `disable_sni`) per
  mihomo's URI parser; v4 token form (userinfo without colon)
  rejected loudly; `allow_insecure=1` honoured on Decode but never
  Encoded (mihomo strips it; the clash path conveys
  `skip-cert-verify` directly through `ClashProxy`).
- HTTP handler gains five TUIC convenience fields:
  `congestion_controller`, `udp_relay_mode`, `zero_rtt_handshake`,
  `heartbeat_interval`, `disable_sni`.

### Tests

- 50+ new unit tests across `params_tuic_test.go`, `tuic_test.go`
  (codec round-trip incl. IPv6 host, v4 rejection),
  `profilegen_tuic_test.go` (golden yaml + listener-only / client-only
  field separation), `subscription_tuic_test.go`, and
  `clash_protocol_test.go` (incl. `disable_sni` end-to-end,
  `heartbeat_interval` numeric/duration/negative).
- Sentinel tests in five mihomo `*_test.go` files migrated from
  TUIC → AnyTLS (anytls is the new "unsupported" sentinel until
  Phase 7).
- Integration test `pkg/proxy/systemtest/mihomo_tuic_matrix_test.go`:
  three matrix cases (tls baseline / tls+bbr+reduce-rtt / tls+cubic)
  plus a cross-cutting subscription-chain case walking
  `GetUserSubscriptions → ConvertTuicForTest → spawnMihomoClient`.
  UDP listener readiness reuses Phase 5's `waitUDPListener`.
- `go test ./... -race -count=1` — 36 packages green.

### Docs

- README `Supported Proxy Kernels` table updated to include TUIC under
  the mihomo row.
- Wiki `wiki/knowledge/mihomo-container/{index,details,edge-cases}.md`
  refreshed with TUIC schema facts and Phase 6 status.

## 2026-04-25 — Mihomo Protocol Expansion Phase 5 (Hysteria2)

Adds Hysteria2 to the mihomo container's structured `ProtocolParams`
path — the first QUIC/UDP protocol to land on this container, completing
Phase 5 of the protocol expansion plan. Hysteria2 in mihomo has no
legacy SharedCred path; the older single-inbound `hysteria` container is
unchanged and stays in service for legacy deployments.

### Highlights

- New `parseHysteria2`: required `password`, Transport forced nil
  (QUIC fixed), Security forced TLS, `obfs ∈ {"", "salamander"}` with
  `obfs_password` mandatory under salamander, `up`/`down` and
  `ignore_client_bandwidth` allowed to coexist.
- `MihomoInbound.AddUser` now picks the forward-layer transport per
  protocol (`hy2 → udp`, others remain TCP); Phase 6 TUIC reuses the
  same hook.
- `fillHysteria2Listener` emits mihomo's exact yaml schema:
  `users: { default: <password> }`, `certificate`/`private-key`,
  `alpn: [h3]` default, plus optional obfs/up/down/ignore-client-bandwidth/masquerade.
- `ClashProxy` gains `Up`/`Down`; `convertHysteria2` reads them from
  Extensions. **Masquerade is server-only** and intentionally NOT
  propagated to client config (mihomo's outbound schema lacks it);
  `TestConvertHysteria2_DropsMasquerade` locks this with a
  `yaml.Marshal` + `strings.Contains` reverse assertion.
- Hysteria2 URI standard only carries 5 query keys; Up/Down/Masquerade
  travel via `SubscriptionSpec.Extensions`, not the URI, preserving
  third-party client compatibility.
- HTTP handler gains six hy2 convenience fields:
  `Obfs`, `ObfsPassword`, `Up`, `Down`, `Masquerade`, `IgnoreClientBandwidth`.

### Tests

- 33 new unit tests across `params_hysteria2_test.go`,
  `profilegen_hy2_test.go`, `subscription_hy2_test.go`,
  `clash_protocol_test.go`, and `TestValidateHysteria2_ErrorPaths`
  (9 cases + baseline).
- Regression test for the legacy hysteria container at
  `pkg/proxy/containers/hysteria/container_fastadd_test.go`.
- Integration test `pkg/proxy/systemtest/mihomo_hy2_matrix_test.go`:
  three matrix cases plus a cross-cutting subscription-chain case
  walking `GetUserSubscriptions → ConvertHysteria2ForTest →
  spawnMihomoClient`. New `waitUDPListener` helper polls UDP dial
  (50ms × ≤60 iterations) instead of a fixed sleep.
- `go test ./... -race -count=1` — 36 packages green.

### Docs

- `docs/mihomo-container-implementation-plan.md` adds "协议扩展 Phase 5"
  with upstream schema research and verification commands.
- `docs/http-api-reference.md` adds Hysteria2 row to the protocol
  matrix and a worked example.
- `wiki/knowledge/mihomo-container/{index,details,edge-cases,.meta.yaml}`
  refreshed with hy2 schema facts and 5 new FAQ entries.

## 2026-04-24 — FastAddInbound Params Normalisation + Mihomo certmgr Integration

Unifies the "caller picks a protocol + port, the system fills the rest" UX
across every container by introducing a single container-agnostic
normalisation layer between the RPC FastAddInbound handler and the
container's own FastAddInbound. The layer handles protocol-level defaults
(credentials + TLS cert sourcing) so individual adapters no longer have
to each reinvent the "generate if absent" logic — and so that mihomo
(which used to reject empty credentials at the adapter) now accepts the
same CLI default path that xray has always handled.

### `pkg/proxy/core/params/defaults.go` (new)

- `FillDefaults(params, protocol, certMgr, scratchDir) error` — two jobs:
  1. **Credential fill** — missing vmess uuid / trojan password / ss
     password+cipher are generated on the spot. Matches xray's
     long-standing behaviour.
  2. **Cert source normalisation** for TLS-required protocols (currently
     trojan). Accepts four equivalent inputs:
       - `cert_file` + `key_file` (paths)
       - `certificate` + `key` (PEM content; materialised to scratchDir)
       - `domain` (looked up via CertManager)
       - `self_signed: true` (generated + materialised)
     On success, `params["cert_file"]` and `params["key_file"]` hold
     absolute paths every downstream adapter can consume directly.
     `params["cert_source"]` records provenance so the container layer
     knows whether v2raymg owns the files (and must clean them up).
  3. **Trojan TLS enforcement** — a trojan request with no cert source
     returns a clear error before the container ever sees it.
- 20 unit tests cover every protocol × every source path + error modes.

### RPC layer

- `pkg/rpc/server/end_node_inbound.go::FastAddInbound` now calls
  `FillDefaults` after the `extra_params` merge but before handing
  `params` to `c.FastAddInbound`. A small adapter bridges
  `pkg/rpc/server.CertManager` to `params.CertManager`.

### Mihomo container changes (all backward-compatible)

- `process.Runner.RunnerConfig.Env []string` — new field; the child
  process's env is `os.Environ() + Env` when non-nil.
- `MihomoContainer.CertScratchDir() string` — returns DataDir. The RPC
  handler type-asserts this interface to decide where to materialise
  cert files so they satisfy mihomo's SAFE_PATHS rule.
- `MihomoContainer.WithSafePathRoots(roots ...string)` — adds directories
  to the `SAFE_PATHS` env var set at mihomo process startup. The
  mihomo factory wires `certmgmt.Manager.Path()` in automatically via
  `BuildOptions.CertManager`, so certmgr-issued cert paths (outside
  DataDir) get accepted by mihomo's path safety check.
- `pkg/certmgmt/service.Manager.Path() string` — new getter exposing
  the storage root so the mihomo container can whitelist it.
- `MihomoSharedCred.CertSource` — new JSON field ("", file, pem,
  domain, self_signed). `RemoveInboundConfig` now deletes cert+key
  files when source is pem/self_signed (material v2raymg wrote); it
  leaves file/domain sources untouched (caller- or certmgr-managed).
- `mihomoFactory.New` now also wires `BuildOptions.UserManager` via
  `WithUserManager`. Previously unwired — production mihomo user
  events did not flow through ContainerMgr-built containers. Unit
  tests bypassed the factory so this was hidden; production tripped
  over it once the container actually loaded from config.

### E2E

- `TestMihomoE2E_FillDefaults_TrojanSelfSigned` (integration) — drives
  trojan through the FillDefaults self_signed source, verifies:
  cert lands under DataDir, mihomo boots the listener, chain reaches
  the configured target, RemoveInboundConfig deletes cert+key files.
  Shares rig with `TestMihomoE2E_RealInternet`.

### CLI

- `cmd/cli/suggest.go::containerSuggest` description expanded to
  `xray (default) / snell / hysteria / mihomo`. No other CLI changes:
  the v2raymg HTTP API's `extra_params` stays the escape hatch for
  uncommon fields; CLI wiring for it is left as a future task.

### Migration

No config changes. Existing deployments keep working — callers that
hand-crafted full `params` with all credentials + cert paths continue
to pass through unchanged. The new behaviour only kicks in when a
field is absent.

## 2026-04-24 — Mihomo Container Stage 11+: Real E2E System Test

End-to-end system test that runs mihomo as BOTH the v2raymg-managed server
and the SOCKS5 client, proving the full production chain
(`curl → mihomo-client → forward port → mihomo-server → google.com`) and
three disruption scenarios that prove the chain really goes through the
server.

### New integration test: `TestMihomoE2E_RealInternet`

File: `pkg/proxy/systemtest/mihomo_e2e_test.go`. Three protocol subtests
(vmess / trojan / shadowsocks), each running a five-step sequence:

1. **positive_baseline** — HTTP GET https://www.google.com (or
   `MIHOMO_E2E_TARGET` override) via SOCKS5 → mihomo-client → forward port
   → mihomo-server → DIRECT → target. Must return < 500.
2. **negative_stop_server** — `container.Stop()`; same GET must fail.
3. **recover_restart_server** — `container.Start()`; GET must succeed
   AGAIN and the user's forward port must be the same (PortMappings
   invariant).
4. **negative_change_credential** — `RemoveInboundConfig` +
   `FastAddInbound` on the same tag+port with fresh credentials;
   PortMappings keeps the forward port stable, so the only changed
   variable is the listener's uuid/password/cipher. Client still armed
   with old credentials → handshake fails.
5. **negative_remove_user** — `RemoveUser`; forward rule is torn down;
   same GET must fail.

Run:
```bash
# MIHOMO_BIN optional (Updater downloads Alpha if unset)
# XRAY_BIN is NOT required for this test
go test -tags=integration ./pkg/proxy/systemtest -run TestMihomoE2E_RealInternet -v
```

### Trojan TLS certificate support (P2-9 resolved)

The E2E test surfaced P2-9 as CERTAIN (not UNCERTAIN as the stage 6+7
review left it): mihomo Alpha rejects trojan listeners at boot time with
`disallow using Trojan without both certificates/reality/ss config`. And
once a cert is attached, mihomo enforces a SAFE_PATHS rule — the cert file
MUST live under mihomo's DataDir (`-d` argument).

FastAddInbound now supports optional `cert_file` / `key_file` trojan
params:

- `pkg/proxy/containers/mihomo/inbound.go::MihomoSharedCred` adds
  `CertFile` / `KeyFile` fields with `omitempty` JSON tags (store format
  stays compatible with vmess/ss records). `Validate` enforces the pair
  rule: both set or both empty.
- `pkg/proxy/containers/mihomo/adapter.go::extractSharedCred` reads
  `cert_file` / `key_file` when present, ignoring them when absent (non-
  trojan callers and legacy unit-test fixtures are unaffected).
- `pkg/proxy/containers/mihomo/profilegen.go::BuildListener` emits the
  mihomo yaml fields `certificate` / `private-key` when the pair is set.
- `pkg/proxy/systemtest/mihomo_helpers_test.go::mihomoTestRig` exposes
  `dataDir` so integration tests can write cert files under mihomo's home
  directory (SAFE_PATHS).

## 2026-04-23 — Mihomo Container Stage 10

### Stage 10a: Post-Start Liveness Probe (A2-3, P1)

Closes the gap where `pkg/proxy/containers/mihomo/updater.go::Update` reported
`Restarted=true` if the new binary forked+execed successfully but then crashed
on startup (GOAMD64 mismatch, missing dynamic deps, config parse error). The
old binary was already swapped; there was no auto-rollback path.

- `ProcessController` interface adds `WaitReady(ctx context.Context) error`.
  `MihomoContainer.WaitReady` probes `GET /version` via its internal REST
  client, returning `ctx.Err()` on timeout. A nil `restClient` (container
  never Started through its own hooks) returns an error so the Updater
  triggers rollback instead of silently passing.
- `UpdaterConfig` adds `ReadinessTimeout time.Duration` (default 10s, matches
  `process.go::readinessTimeout`). `Update` also falls back to 10s when the
  config was built without `NewUpdater` (mirrors the existing
  `RestartPolicy` "" → Always normalisation).
- `Update` Step 7 now: `Stop(if running) → Start → WaitReady(bounded) →
  failure: Stop + Rollback + ErrUpdateFailed{Stage:"restart"}`. A failure in
  the WaitReady cleanup Stop is joined into the returned cause via
  `errors.Join`, so `errors.Is(err, readinessCause)` and `errors.Is(err,
  stopCause)` both succeed.

### Stage 10b: Functional Integration Tests

New `pkg/proxy/systemtest/` integration tests targeting a real mihomo binary
(MVP D4 scope — vmess / trojan / shadowsocks). Scale testing skipped per
user decision; R1 (listener count blow-up) already neutralised by the
shared-credential model (listener count = inbound count, decoupled from
user count).

- `mihomo_helpers_test.go`: `ensureMihomoBin` (MIHOMO_BIN > Updater download
  of Alpha), `startMihomoContainer` (real ForwardManager + UserManager +
  SQLite store), `addMihomoUserAndWaitForPort` / `removeMihomoUserAndWaitForPortRelease`
  (real AddUser/RemoveUser through the user-event pipeline).
- `mihomo_protocol_matrix_test.go`: `TestMihomoProtocolMatrix` — table-driven
  over vmess / trojan / shadowsocks. Uses xray as the protocol client
  (D8: zero new dependencies; pattern mirrors xray_fastadd_connectivity).
  Each subtest runs a positive handshake through the SOCKS5→proxy chain,
  then confirms RemoveUser actually tears down the forward rule (negative
  control).
- `mihomo_restore_test.go`: `TestMihomoRestore_RecoversInboundAndUser` —
  Stop + Start round-trip with PortMappings stability assertion and fresh
  handshake through the restored chain.
- `pkg/proxy/systemtest/README.md` cleaned of 5 residual `./pkg/proxyrefactor/...`
  paths + new "Mihomo (stage 10b)" section.
- `helpers_test.go::noopForwardManager`: added missing `DropUser` /
  `AllocatePort` / `ReleasePort` methods (pre-existing bug; interface
  incomplete since stage 5 forward-manager extension).

Run: `XRAY_BIN=... MIHOMO_BIN=... go test ./pkg/proxy/systemtest -tags=integration -run TestMihomo -v`

## 2026-04-23 — Mihomo Container Stage 8-9

### Stage 9: Updater + auto-download — **BREAKING**

- **`MihomoConfig.Version` → `MihomoConfig.ReleaseTag`**(字段重命名)。旧 yaml key `version` 不再识别。运维升级时 config 里 `mihomo.version: ...` 必须改为 `mihomo.release_tag: ...`。
- **默认值改变**:`release_tag` 默认从 `"Prerelease-Alpha"` 改为 `"latest"`。`"latest"` 走 GitHub `/releases/latest`,只返回最新 non-prerelease(stable v1.19.x);**不会** 返 Alpha pre-release。想继续跑 Alpha 的部署必须显式设 `release_tag: Prerelease-Alpha`。
- 新增 `MihomoConfig.AutoDownload bool`,默认 `true`。false 时 binary 缺失直接报错、`Update()` 返 `ErrNotSupported`。
- 新增 `pkg/proxy/containers/mihomo/updater.go`:`Update(ctx, req)` 热替换(fetch → pick asset → download → verify → gunzip → swap → restart),按 `RestartPolicy` 决定重启;SHA256 当 release 发 `checksums.txt`(目前仅 Alpha)就校验,否则 warn + 跳过;失败路径 rollback,rollback 本身也失败时通过 `errors.Join` 合并返回。
- `pkg/proxy/tools/VerifyChecksum` 从 `strings.Contains + ==` 宽松比对改为 TrimSpace + ToLower 后严格相等,避免短/畸形 expected 被假过(安全 primitives 不得松匹配)。
- 删除 `downloader.go::downloadMihomo` wrapper(updater 已取代)。

### Stage 8: HTTP API 对齐 mihomo

- `pkg/rpc/server/end_node_inbound.go::getContainerByType` 由硬编 switch 改为"别名规范化 + ContainerMgr.Get 多态",mihomo 及未来新 container 自动工作。
- `pkg/rpc/server/end_node_inbound.go::ListInbound` 硬编 3-container slice 改为 `containerMgr.Types()` 迭代。
- 相关 HTTP help / design doc 同步补 mihomo。

## 2026-02-22 — T-032 目录重组与引用迁移（container 体系）
- 目录重组：
  - `core/container`：抽象接口与基础类型
  - `containers/xray`：xray 具体实现
  - `tools`：通用下载/校验/替换工具
- 引用迁移：xray 侧改为本地 native 类型，移除 provider 路径依赖
- 回归：`go test ./pkg/proxyrefactor/... -count=1` 通过
- QA 结论：PASS_WITH_RISK（Update 失败路径完整性需后续继续强化）

## 2026-02-22 — T-031 Domain 解耦第一阶段
- 新增 domain 禁止项清单与自动 gate
- 新增 InboundSpecV2（Mode/Entry/TargetRef/PolicyRef/Meta）
- 新增 TargetRef 解析契约与错误判定
- mapper 契约补齐 To/From 方向与 warnings 语义
- QA 结论：PASS_WITH_RISK（逆向语义完善留待后续阶段）

## 2026-02-21 — T-030 注释增强与并发回归闭环
- 补充/完善注释（不改变业务意图）：
  - `container/interface.go`：`Update(...)` 对外契约与参数语义
  - `container/updater.go`：更新主流程、异常分支、回滚意图
  - `container/exec_runner.go`：`Start/Stop` 生命周期与并发约束
  - `forward/forward_manager.go`：锁模型、规则生命周期、失败回滚语义
  - `forward/relay.go`：转发数据路径、优雅关闭语义
- 回归中发现 `ExecRunner.Stop` 与后台 `Wait` 协同存在阻塞风险，修复为：
  - 引入 `done` 信号通道，避免并发重复 `Wait` 导致卡住
  - `Stop` 先取消上下文，再等待进程退出；超时后兜底 `Kill`
- 验证：`go test ./pkg/proxyrefactor/... -count=1` 全量通过

## 2026-02-21 — T-028 Start 自动拉取 xray 二进制
- `ExecRunnerConfig` 增加：
  - `AutoUpdateOnStart`（xray 默认 true，可关闭）
  - `AutoUpdateTag`（默认 latest）
- `Start()` 流程增强：
  - 检测 binary 存在且可执行
  - xray 且缺失时自动触发 `Update(latest, RestartNever)`
  - update 成功后继续 start
  - update 失败返回明确阶段错误（`binary auto-update failed: ...`）
- 仅 xray 生效；v2ray/hysteria 保持原行为
- `runUpdate(...)` 支持“首次安装”路径（binary 不存在时直接 install，不走 swap）
- 新增测试 `exec_runner_autoupdate_test.go` 覆盖：
  - binary 存在不触发 update
  - binary 缺失触发 update 并启动成功
  - update 失败 -> start 失败
  - auto-update disabled -> 缺失直接失败
  - 非 xray 不触发 auto-update

## 2026-02-21 — Phase S 简单系统测试（T-027）
- 新增 `pkg/proxyrefactor/systemtest/xray_socks5_system_test.go`（integration）
  - 流程：启动 xray container -> socks5 inbound -> `http://example.com` 访问验证
  - 运行条件：`XRAY_BIN` 可用 + `-tags=integration`
- 新增 `pkg/proxyrefactor/systemtest/degraded_socks5_chain_test.go`
  - 本地最小 SOCKS5 server + 本地 origin 服务
  - 在受限环境可执行，验证 proxy chain 基础链路
- 新增 `pkg/proxyrefactor/systemtest/README.md`
  - 给出真实/降级两套执行命令
  - 明确成功/失败判据

## 2026-02-21 — Phase U QA 残留风险修复（T-026）
- `updater.go` 可测试性改造：
  - 接口化依赖：ReleaseClient / AssetDownloader / BinarySwapper / ProcessController
  - 抽离 `runUpdate(...)` 纯编排函数，ExecRunner.Update 仅注入默认依赖
- `github_release_client.go` 新增 `BaseURL`，支持 `httptest.Server`
- `updater_test.go` 重构为表驱动 mock 测试，覆盖 FAIL 场景：
  - 下载失败
  - checksum fail
  - swap fail
  - restart fail -> rollback
  - rollback fail
  - restart policy 分支（never / if_was_running / always）
  - FromVersion/ToVersion 断言
  - running/non-running 调用序列断言（Stop 是否触发）
- `github_release_client_test.go` 增加 BaseURL 路径断言

## 2026-02-20 — Phase U Container Update 接口
- container/interface.go: Container 新增 Update(ctx, req)
- update_types.go: UpdateRequest/UpdateResult/RestartPolicy/Updater
- github_release_client.go: tag 标准化 + GitHub Release 拉取 + asset 选择
- downloader.go: 下载到本地文件
- checksum.go: sha256 计算/解析/校验
- swapper.go: 原子替换 + 回滚
- updater.go: ExecRunner.Update 主流程（拉取→下载→校验→替换→回滚→重启策略）
- 新增测试: update_types_test / github_release_client_test / checksum_test / swapper_test / updater_test

## 2026-02-20 — D-008 Container option 注入 ForwardManager
- 新增 global.go: GlobalForwardManager 进程级单例 + SetGlobalConfig + ResetGlobalForwardManager
- 重写 integration.go: NewContainer(id, ...ContainerOption) 取代旧 NewForwardAwareContainer
  - WithForwardManager(fm) option 显式注入
  - 缺省回退到全局单例
  - InboundForwarder 自动继承 container 的 FM，不再需要单独注入
- 新增 global_test.go: 3 用例（单例/重置/功能性）
- 重写 integration_test.go: 11 用例，新增覆盖：
  - 显式注入 option / 默认全局单例 / 双 container 共享全局 / inbound 继承 container 实例

## 2026-02-20 — Phase 6 端口转发层
- T-014: ForwardRule 模型 + ForwardManager 接口
- T-015: PortAllocator（范围端口池 + 分配/回收/冲突检测 + 并发安全）
- T-016: TCP Relay（双向转发 + 优雅关闭 + 连接数限制 + 流量计数钩子）
- T-017: TrafficCounter + TrafficRegistry（atomic 计数 + 按 rule 聚合 + snapshot/reset）
- T-018: TokenBucketLimiter（令牌桶 + LimitedReader/LimitedWriter + 突发支持）
- T-019: DefaultForwardManager（全局组装 + CRUD + 生命周期管理）
- T-020: InboundForwarder + ForwardAwareContainer（集成接口 + 端到端测试）

## 2026-02-20 — 项目收尾（Phase 1-3 xray）
- Ryan 决定取消 Phase 3(v2ray) / Phase 4 / Phase 5
- 已完成交付：Phase 1 + Phase 2 + Phase 3(xray)

## 2026-02-20 — 项目启动
- 创建项目文件 (PROJECT.md, DECISIONS.md, STATUS.md, TASKS.md)
- Phase 1: domain 领域模型 + 统一错误码 + 核心接口
- Phase 2: ExecRunner + ConfigWriter
- Phase 3(xray): InboundAdapter + ConfigRenderer
