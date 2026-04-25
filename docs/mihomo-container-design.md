# Mihomo Container 设计文档

## 概述

引入 [mihomo](https://github.com/MetaCubeX/mihomo)(Clash Meta 继任者)作为新的 container 类型,覆盖 vmess / trojan / shadowsocks 及后续扩展协议(vless / hysteria2 / tuic / anytls)的服务端实现,并天然对齐 Clash 生态客户端。

定位:与 xray / hysteria / snell 并列的 container,使用 exec 子进程 + mihomo external-controller HTTP REST API 控制。所有共享设施(UserManager、forward 层、certmgmt、store、process.Runner、BaseContainer、Factory、clash 订阅转换)直接复用,新增内容压缩到 `pkg/proxy/containers/mihomo/`。

**适配模式声明**(`docs/container-design-principles.md` 要求):

| Checklist 项 | 值 |
|--------------|-----|
| 原则 1 exec | 是,不嵌入 Go 库 |
| 二进制来源 | GitHub Release 自动下载(MetaCubeX/mihomo Alpha pre-release) |
| 原则 2 模式 | **B**(多 inbound + 运行时 API `PUT /configs` 走 `PatchInboundListeners` 的 name-keyed diff) |
| 运行时控制通道 | mihomo external-controller HTTP REST |
| 协议层 user list | **不持有**(每 inbound 一把共享凭据,见下文) |
| 流量统计入口 | forward 层(决策 #5) |
| 持久化 | InboundStore + UserManager/forward 端口持久化 + `Restorable.Restore` |
| 订阅 | 复用 `core/subscription/converter/clash` |
| 协议枚举扩展 | 不动 `contracts.Protocol`,mihomo 特有字段走 `InboundSpec.Extensions["mihomo"]` |

## 关键决策:共享凭据 + 用户走 forward 层(与 xray/snell 同构)

mihomo 原生支持 per-inbound user list(每 listener `users:` 数组可放多条 `{uuid/password, ...}`),也支持 per-user listener(每 listener 单用户)。为了对齐 `docs/container-design-principles.md` 原则 3("业务用户路径只走 forward 层,无一例外"),v2raymg 选择:

- **每个 inbound 对应一条 mihomo listener**,listener name = inbound tag,listen `127.0.0.1:<inbound-port>`
- listener 持**一把共享凭据**(vmess uuid / trojan password / shadowsocks password+cipher),写进 `listeners[].users[]`(vmess/trojan)或 listener 顶层字段(shadowsocks)
- 业务用户**不进入 mihomo listener 配置**。所有用户用同一把凭据 auth 到 mihomo
- 每个用户通过 `UserManager.GetBindPort` 拿到一个**不同的公网端口**,forward 层 relay 到 `127.0.0.1:<inbound-port>`
- forward 层按用户做流量统计 / 限速 / 客户端数限制
- 用户增删**完全不动 mihomo listener 配置**,只动 forward rule —— 和 snell 同构

这一模型的收益:

- listener 数量 = inbound 数量(典型 1~10),yaml 加载和 `PUT /configs` 开销与用户规模解耦
- 用户增删免去 listener 重建(PatchInboundListeners 的 name-keyed diff 要跨用户精确管理 listener 生死,复杂度高)
- 与 xray/snell 业务路径一致,原则 3 对齐,不破坏"所有 container 用户路径无一例外"的共性保证

显式不采用 per-user listener 模型的原因:per-user listener 带来的"用户隔离"在 v2raymg 架构下已经由 forward 层完成(不同用户不同公网端口、独立流量统计),再在 listener 层做一遍是冗余的;且协议级"一用户一 UUID"并不是 v2raymg 要求的能力(xray 和 snell 都用共享凭据)。

## 架构

```
 客户端 ── 公网端口 ──┐
                     │  forwardmgr relay(per-user 端口、流量统计、限速)
                     ▼
              127.0.0.1:<inbound-port>
                     │
                     ▼
     mihomo 进程(单进程,N 条命名 listener)
       listener "<tag-A>"  127.0.0.1:10001  type: vmess   users: [{uuid: <tag-A 共享凭据>}]
       listener "<tag-B>"  127.0.0.1:10002  type: trojan  users: [{password: <tag-B 共享凭据>}]
       listener "<tag-C>"  127.0.0.1:10003  type: shadowsocks  password/cipher: <tag-C 共享凭据>
```

- 一个 mihomo 进程,external-controller bind `127.0.0.1:9090`
- `listeners:` 数组每个 inbound 一条
- `inbound-port` 由调用方通过 `FastAddInbound` 的 params 指定
- forward 层负责公网端口分配 + relay + per-user 流量统计
- mihomo 的出站 / 路由 / DNS / proxy-groups **不启用**(只用其入站能力)

## 文件结构

```
pkg/proxy/containers/mihomo/
├── register.go          # Factory 注册(stage 1)
├── config.go            # MihomoConfig + Decode(stage 1-2)
├── container.go         # Container 接口,inbounds map,user event handler
├── rest_client.go       # mihomo REST 客户端(stage 3)
├── inbound.go           # MihomoInbound(嵌 DefaultInbound + 共享凭据与协议字段)
├── adapter.go           # params map → MihomoInbound,按协议验必填字段
├── profilegen.go        # MihomoInbound → mihomo listener yaml map
├── config_builder.go    # (base config, inbounds) → 完整 yaml 字节
├── subscription.go      # GetUserSubscriptions(复用 clash converter)
├── process.go           # 进程启停 + 初始 config(stage 2)
├── downloader.go        # GitHub Release asset 下载(stage 2)
└── updater.go           # 二进制原子替换 + checksum(stage 9)
```

相比原 per-user listener 方案:**删除** `ports.go`(无需内部端口池)、`reconcile.go`(listener 级不需要对账,用户级 reconcile 随 snell 模式并入 container.go)。

## Container 接口实现

| 方法 | 行为 |
|------|------|
| `Init(config)` | 接收 `*MihomoConfig`,准备目录/骨架,不启动进程 |
| `Start()` | 生成初始 config.yaml(空 listeners)→ Runner.Start → 轮询 `GET /version` 就绪 → 从 InboundStore 加载已有 inbound 到 map → rebuild yaml → `PUT /configs(force=false)` → 启动 UserEventHandler → Restore forward rule |
| `Stop()` | 停 UserEventHandler → Runner.Stop(SIGINT 5s → SIGKILL) |
| `Restart()` | BaseContainer 默认:Stop + Start |
| `Reload()` | 从 inbounds map 重建 yaml → `PUT /configs`,**不重启进程**(用于证书热更、全局配置调整) |
| `IsRunning()` | BaseContainer 状态 + `GET /version` 双验证 |
| `Version()` | 缓存的 `GET /version` 结果 |
| `Type()` | `contracts.ContainerMihomo` |
| `ConfigFile()` | `MihomoConfig.ConfigFilePath` |
| `Update(ctx, req)` | updater 下载新二进制 → 校验 → 原子替换 → 按 RestartPolicy 重启 |
| `GetRunFunc()` | 返回 `process.Runner` 的 run/stop |
| `FastAddInbound(tag, params)` | adapter 解析 params → 锁 inbounds map → 拒重复 tag → InboundStore.Save → 入 map → rebuild yaml → `PUT /configs(force=false)`(name-keyed diff,其他 listener 不重建) |
| `RemoveInboundConfig(tag)` | 锁 → 从 map 摘 → rebuild yaml → `PUT /configs` → InboundStore.Delete → forward 层回收该 inbound 下所有用户端口 |
| `GetInboundConfig(tag)` | 查 `c.inbounds[tag]` |
| `ListInboundConfigs()` | 列 `c.inbounds` |
| `UserEventChannel()` | 返回 `c.userEventCh`(从 UserManager.Subscribe 转发) |
| `GetUserSubscriptions(req)` | 遍历 mihomo inbound → 对每 (user, inbound) 生成一条 Clash proxy 条目 |

## 用户层:走 forward 层(原则 3)

每个用户对每个 mihomo inbound 分配一个公网端口,走 forward 层 relay 到 `127.0.0.1:<inbound-port>`。逻辑与 snell / xray 同构,只是 mihomo 支持多 inbound,需要对每个 inbound 都拉一条 rule。

```
UserEventAdd(username, user):
  for inb in c.inbounds:
    userMgr.GetBindPort({
      Username:      username,
      ContainerType: mihomo,
      InboundTag:    inb.Tag,
      TargetPort:    inb.Port,       # inbound 本地监听端口
      Protocol:      inb.Protocol,
    })

UserEventRemove(username):
  for inb in c.inbounds:
    if port, ok := userMgr.GetUserPortByDstForCleanup(username, inb.Port); ok:
      userMgr.ReleaseBindPort({Username: username, BindPort: port})

UserEventUpdate(username):
  按 IsUserVisible 判断:可见 → 补 GetBindPort;不可见 → ReleaseBindPort
```

**模板**:`pkg/proxy/containers/snell/container.go:368-467` 的 `forwardUserEvents` / `startUserEventHandler` / `handleUserEvent`,stage 5 按多 inbound 扩展。

**幂等性**:GetBindPort 对已存在 mapping 直接返原 port;ReleaseBindPort 对不存在的 rule 直接 return。并发场景用 `addedUsers map[username]map[inboundTag]struct{}` + mutex 保护,锁只在 map 读写时持有,不覆盖 userMgr 调用。

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
        secret: ""                    # 留空即无 secret,配合 loopback 绑定可接受
        release_tag: latest           # GitHub release tag; "latest" → /releases/latest (最新 stable)
                                      # 设为 "Prerelease-Alpha" 可跟踪 Alpha 永续 pre-release
        auto_download: true           # 二进制缺失时由 updater 下载(stage 9)
```

没有 `internal_port_range`(无内部端口池);没有 `reconcile_interval`(stage 6 添加时再引)。字段名原为 `version`,stage 9 重命名为 `release_tag`(避免与 `Container.Version()` 语义撞名)。默认值原为 `Prerelease-Alpha`,stage 9 改为 `latest` —— 生产默认跑 stable,开发跑 Alpha 请显式覆盖。

### 初始 mihomo config.yaml(Start 时生成)

```yaml
mixed-port: 0
allow-lan: false
external-controller: "127.0.0.1:9090"
secret: ""
log-level: info
mode: global               # 不启用规则匹配,出站直连
proxies: []
proxy-groups: []
rules:
  - MATCH,DIRECT
listeners: []              # 启动后由 InboundStore 加载并通过 PUT /configs 填充
```

## REST API 使用

`rest_client.go`(stage 3 已实现)封装以下端点:

| 操作 | HTTP | 路径 | body / query |
|------|------|------|--------------|
| 查版本 | GET | /version | — |
| 读配置 | GET | /configs | — |
| 全量更新 | PUT | /configs | JSON `{"payload":"<yaml>"}`;query `?force=false` |
| 部分更新 | PATCH | /configs | JSON fields map(log-level/mode/... 等 singleton 字段) |
| 刷新 GEO | POST | /configs/geo | — |

**listener 增删走 `PUT /configs(force=false)`**:

- 内部调用链 `ApplyConfig(cfg, false)` → `updateListeners(general, listeners, false)` → `PatchInboundListeners(map, tunnel, dropOld=true)`
- `PatchInboundListeners` 按 name 做 diff:同 name + `Config().Equal` 保留,不等则 Close+Listen,新 map 缺失的 name 被 Close+delete
- **未变更的 listener 既有连接不受影响**,这是多 inbound 动态增删的基础前提
- 详细核实记录见 memory `reference_mihomo_protocol_facts.md`

**操作并发**:container 内串行调 REST(单线程 user event handler + inbounds mutex),mihomo 内部每类 listener 有独立 mutex,双端都安全。

## 持久化与 Restore

- **InboundStore** 持久化 mihomo inbound(tag / protocol / port / 共享凭据 / TLS 与 transport 字段):`store.InboundRecord{ContainerType="mihomo", NativeJSON=<MihomoInbound JSON>}`
- **UserManager + forward 层**持久化 user→port mapping,进程重启可恢复

**Restore 流程**(`Restorable.Restore(ctx)`,由 ContainerMgr.StartAll 自动触发):

1. 从 InboundStore 读所有 `ContainerType=mihomo` 的 record → 反序列化为 `MihomoInbound` → 入 `c.inbounds`
2. rebuild 完整 yaml → `PUT /configs(force=false)`
3. 从 UserManager 读全部可见用户
4. 对每 (user, inbound) 补 forward rule(GetBindPort 幂等)
5. 对账:发现多余 rule(inbound 已删但 forward 残留)→ ReleaseBindPort

Stage 4 在 `Start()` 钩子里做步骤 1-2 的精简版(无定期 reconcile),stage 5 加步骤 3-4,stage 6 形式化为 `Restorable.Restore` 并加周期 reconcile。

## Subscription Converter

直接复用 `pkg/proxy/core/subscription/converter/clash/`。对每个 (user, inbound):

```yaml
proxies:
  - name: <node_name>-<inbound_tag>
    type: vmess                # 或 trojan / ss
    server: <node_host>        # 节点公网地址
    port: <用户公网端口>         # forward 层为该用户分配的端口
    uuid: <inbound 共享凭据>     # vmess
    # 或 password / cipher(shadowsocks)/ password(trojan)
    tls: <按 inbound 配置>
    ...
```

`GetUserSubscriptions(req)` 步骤:

1. 遍历 `c.inbounds` 里每个 mihomo inbound
2. 从 `forwardMgr.GetUserPortByDst(username, inbound.Port)` 取用户公网端口;无映射跳过
3. 从 inbound 取共享凭据和 TLS/transport 字段
4. 拼 Clash proxy 条目
5. 返 `contracts.SubscriptionSpec{ClientType: "clash", ...}`

非 Clash 客户端(v2ray / surge 等)由订阅层按协议兼容性选渲染。

## 协议与字段扩展

**当前状态(2026-04-25)**:Phase 1-7 全部完成。MVP 的 vmess / trojan / shadowsocks 之外,VLESS / Hysteria2 / TUIC / AnyTLS 已全部提为 `contracts.Protocol` 一等公民,新增配置走 `pkg/proxy/core/params/protocolparams/` 结构化路径。

按 `docs/container-design-principles.md` 的"模式矩阵",所有 7 个协议在 mihomo 容器都属于**模式 B**(单进程外部 + REST 热更 + 共享凭据 listener),listener 数量 = inbound 数量,与用户规模解耦。差异只在 forward 层 Network(TCP vs UDP)和 listener yaml schema:

| 协议 | Phase | forward Network | listener users key | TLS 强制 | 备注 |
|------|-------|-----------------|---------------------|----------|------|
| vless | 1 | tcp | n/a(单凭据 client) | 否(可 plain/tls/reality) | 支持 ws/grpc/h2/httpupgrade/xhttp |
| vmess | 2 | tcp | n/a | 否 | AlterID=0 默认 AEAD-only |
| trojan | 3 | tcp | `password`(数组形式) | 是 | 支持 tcp/ws/grpc × tls/reality |
| shadowsocks | 4 | tcp | n/a | n/a | 默认 cipher `2022-blake3-aes-256-gcm`;支持 obfs/v2ray-plugin/shadow-tls(后者仅订阅) |
| hysteria2 | 5 | **udp** | username(单用户用 `default`) | 是 | QUIC,可选 salamander obfs / 带宽宣告 / masquerade |
| tuic | 6 | **udp** | uuid(单用户) | 是 | QUIC v5;`congestion-controller` 默认 bbr profilegen 显式写 |
| anytls | 7 | tcp(UDP 走 UoT 透明) | username(单用户用 `default`) | 是 | TCP-over-TLS;可选 server-only `padding-scheme` inline 文本;client-only `idle-session-*-seconds` int 秒 |

**契约要点**(所有 Phase 1+ 协议共同遵守):

- `contracts.Protocol` 加常量(`ProtocolVLess` / `ProtocolHysteria2` / `ProtocolTUIC` / `ProtocolAnyTLS`)
- `pkg/proxy/core/params/protocolparams/` 加 `XxxParams` struct + `params_xxx.go` 解析 + `parser.go` 分派
- `pkg/proxy/core/subscription/codec/` 加 `xxx.go` URI Encode/Decode + `node.go` 注册
- `pkg/proxy/core/subscription/converter/clash.go` 给 ClashProxy 加协议特异字段 + `convertXxx` + `ConvertXxxForTest`
- `pkg/proxy/containers/mihomo/{adapter,inbound,profilegen,subscription}.go` switch 加 case + `validateXxx` + `fillXxxListener` + `fillXxxSubscriptionSpec`
- `pkg/http/fastAddInbound_handler.go` 加便捷字段 + help 文本;`pkg/rpc/server/end_node_inbound.go` BuilderType switch 加 case
- 系统测试(`pkg/proxy/systemtest/mihomo_xxx_matrix_test.go`,integration tag):矩阵 case ≥2 + 至少 1 条 cross-cutting 走 GetUserSubscriptions → Convert → spawnMihomoClient(memory feedback `feedback_systemtest_subscription_chain.md` 强制)

**`InboundSpec.Extensions["mihomo"]` 当前不再使用** —— Phase 1-7 都走 `ProtocolParams` 结构化字段,Extensions 只用于 codec/subscription 之间的弱类型 map 传递(`server_name` / `skip_cert_verify` / `idle_session_*_seconds` 等)。

## 开发步骤

与 `docs/mihomo-container-implementation-plan.md` 的阶段对应(架构视角):

| Stage | 本文档对应内容 |
|-------|----------------|
| 1 骨架 | 架构章节的目录结构;Factory 注册 |
| 2 进程 | `process.go` + 初始 config.yaml + `/version` 探活 |
| 3 REST 完整化 | REST API 使用章节的全部端点 |
| **4 Inbound 生命周期** | `inbound.go` / `adapter.go` / `profilegen.go` / `config_builder.go`;`container.go` 的 FastAddInbound / RemoveInboundConfig / Get / List;共享凭据 listener 全量 yaml `PUT /configs` |
| **5 User Event + forward** | 用户层章节的 handleUserEvent 模板接入;`addedUsers` map 扩展到 per-inbound |
| 6 Restore + Reconcile | 持久化与 Restore 章节的 5 步完整版;周期 reconcile 检测漂移 |
| 7 Subscription | Subscription Converter 章节 |
| 8 HTTP API 对齐 | 不动业务 handler,确认现有 HTTP API 在 container 多态下识别 mihomo |
| 9 Updater | `updater.go` 接 `pkg/proxy/tools/{binary_swapper,checksum,downloader}` |
| 10 测试 | 系统测试 + 规模测试 |

## 风险与未决

- **R2 mihomo Alpha schema / REST 行为漂移**:Alpha 分支 HEAD 会动,每次二进制升级都需核实关键字段;rest_client 层做版本字符串探测,不兼容时报清晰错误而非降级
- **R3 TLS 证书热更**:`PUT /configs` 更新 inbound 的 `certificate` / `private-key` 字段时,mihomo 通过 `Config.Equal` 判定差异后 Close+Listen 该 listener,**该 inbound 上现有 TCP 连接会断**;跨 inbound 不受影响。证书刷新 = 部分用户瞬时重连,可接受。若未来要求零中断,需另行设计(通常靠滚动替换)
- **R4 clash converter 对 MVP 三协议输出不完整**:stage 7 先扫描 `core/subscription/converter/clash/`,缺失再补
- **R5 PUT force 语义**:stage 3 已核实并写入 memory `reference_mihomo_protocol_facts.md`,force=false 名字 diff 保留未变更的 listener 连接

不再是风险(相比 per-user listener 方案):

- ~~R1 listener 数量膨胀~~:listener 数量 = inbound 数量,与用户规模解耦
- ~~R6 per-user internal_port 池耗尽~~:无内部端口池

## 参考

- `docs/container-design-principles.md` — 原则 2 模式 B + 原则 3 用户走 forward
- `docs/snell-container-design.md` — 共享凭据 + forward 层的 mode A 对标
- `docs/xray-container-architecture.md` — mode B 对标(也走共享 clients[0] 凭据)
- mihomo Alpha 分支关键文件(已核实,见 memory `reference_mihomo_protocol_facts.md`):
  - `hub/executor/executor.go::ApplyConfig(cfg, force)` — 内部总调 `updateListeners`
  - `listener/listener.go::PatchInboundListeners` — name-keyed diff + `dropOld=true`
  - `listener/parse.go::ParseListener` — `structure.NewDecoder(TagName: "inbound")` + `DefaultKeyReplacer`
  - `listener/inbound/{vmess,trojan,shadowsocks}.go::*Option` — yaml 字段名来自 `inbound:"..."` tag
  - `hub/route/configs.go` + `hub/route/errors.go` — PUT/PATCH /configs body schema 与 HTTPError
- v2raymg 对标实现:
  - `pkg/proxy/containers/snell/container.go:368-467` — `handleUserEvent` 模板(stage 5 抄)
  - `pkg/proxy/containers/xray/exec_runner.go:523-593` — 多 inbound CRUD 模板(stage 4 抄)
  - `pkg/proxy/containers/xray/exec_runner.go` 顶部关于 `XrayInbound.AddUser` 不调 xray gRPC 的注释
