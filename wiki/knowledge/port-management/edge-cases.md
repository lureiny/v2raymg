---
title: 端口管理与流量统计
layer: edge
---

## FAQ

**Q: 同一个用户对同一个 inbound 反复调用 GetBindPort 会分到多个端口吗?**

A: 不会。`forwardMgr` 用 `RuleKey = "{ct}:{tag}:{user}"` 做幂等;第二次调用直接返回已存在的 `ListenPort`。这是协议要求的幂等语义,不是偶发行为,可以依赖。

**Q: 容器/服务重启后,之前用户的端口还是同一个吗?**

A: 是,只要 `User.PortMappings[dstPort]` 还在。恢复流程会读 `PortMappings` 拿到 `preferredPort`,然后走 `PortAllocator.AllocateSpecific(preferredPort)` 分回原端口。如果该端口被别人占了,`GetBindPort` 会回落到自动分配 —— 也就是**用户端口静默换了一个,旧订阅失效**。2.9 的启动预扫就是为了让这件事不发生:所有持久化 inbound 端口在任何 forward 规则之前先被认领。

**Q: inbound 的端口是谁分配的?会不会跟用户的 forward 端口撞上?**

A: 同一个 `PortAllocator`,所以撞不上(2.9 起)。入口是 `container.ClaimInboundPort`:传 0 表示"你挑",走 `AllocatePort()` 抽签;传具体值表示"就要这个",走 `AllocateSpecificPort()` 认领。删除 inbound 时 `ReleaseInboundPort` 归还。

2.9 之前有**三个互不知情的决策者**:forward 的 `PortAllocator`、xray 自己的 `generateRandomPort(4000,5000)`(只有 1001 个取值,不去重)、以及 `NewDefaultInbound` 里 `port==0 → 10000` 的硬编码。线上 `listen udp 127.0.0.1:10000: bind: address already in use` 就是第三个造成的 —— 任何两个不显式传 port 的 inbound 都落在 10000。

**Q: 显式指定的 inbound 端口被占了,会自动换一个吗?**

A: **不会,直接报 `ErrPortInUse` 拒绝。** 与 forward 侧"pinned `ListenPort` 不重试"是同一条原则:运维指定的端口可能已经写进防火墙规则、DNS 或客户端配置,静默换掉会在别处坏掉,却看起来像成功。只有 `port=0`(没人选)才允许抽签。

**Q: hysteria / snell 的端口为什么不在容器里认领?**

A: 它们是单 inbound 容器,端口写死在容器 YAML 里,不存在"分配"。由启动预扫统一认领 —— 容器构造(`LoadFromConfig`)发生在预扫之后,如果容器再认领一次,会撞上我们自己刚才的认领,而这跟真实冲突无法区分。注意 snell 默认的 **16160 落在 forward 池里**(hysteria 的 9443 是刻意选在池子下方的),所以这条不是理论问题。

**Q: 一个用户可以有多个端口吗?**

A: 可以。用户有多个 inbound(多个 protocol+tag)就会有多个 ForwardRule → 多个 ListenPort。`User.BindPorts` 是列表,`PortMappings` 是 map(每个 dstPort 映射一个 forwardPort)。

**Q: 流量统计的准确性如何,会不会漏掉握手阶段?**

A: Relay 在 TCP 层做字节转发,从 `Dial` 后的第一个字节就计数,包括协议握手和后续数据,不漏。但因为是 TCP 字节计数,不是应用层有效载荷计数,所以**包含代理协议头部开销**。

**Q: 为什么不用 xray 自带的统计 (container.QueryStats)?**

A: 三个原因:① 不同 inbound 协议的统计精度/完整性不一致;② 耦合容器内部实现;③ 口径需要统一。所以项目约定:**流量 ground truth 只来自 Forward 层**,UserManager 只做聚合和落库。

**Q: Delta 和 Total 的区别,什么时候用哪个?**

A: `Total*` 是累计量,持久化到 DB,适合展示用户历史消耗。`Delta*` 是自上次 `reset=true` 以来的增量,**专为 Prometheus 抓取设计**——Prometheus 每次拉取后重置,这样多实例聚合不会重复计数。两个字段互相独立维护。

**Q: MaxClients 和 MaxConnections 有什么区别?**

A: `MaxConnections` 是全局并发连接数上限(全部 IP 加起来);`MaxClients` 是同一时间活跃的**不同客户端 IP 数**上限。举例:`MaxClients=2, MaxConnections=10` 表示最多 2 个不同 IP,但这 2 个 IP 之间总共可以开 10 条连接。

---

## 注意事项

⚠️ **Forward 规则创建入口唯一**:业务规则只能通过 `UserManager.GetBindPort / ReleaseBindPort` 创建和销毁。**禁止**在 inbound adapter、container 实现、或任何其他模块内部直接调用 `forwardMgr.AddRule / RemoveRule` 来创建业务规则。违反会导致用户状态与 forward 规则不一致、统计丢失、重启不幂等。这是 CLAUDE.md 里明确写的项目约束。

⚠️ **统计只看 Forward 层**:任何流量相关的新需求(限额、报表、告警)都应该读 `forwardMgr` 的 API 或 `UserManager` 的聚合层。**禁止**新增对 `container.QueryStats` 的依赖,也不要自己在 inbound 里再埋一套统计。

⚠️ **PortMappings 不能手动改**:`User.PortMappings` 是重启幂等的关键,由 `GetBindPort/ReleaseBindPort` 内部维护。手动改写(例如运维脚本直接改 DB)会导致恢复时端口冲突或错位。如需迁移端口,走完整的 Release → GetBindPort 流程。

⚠️ **RuleKey 是合约**:`{containerType}:{inboundTag}:{username}` 的三元组是稳定键,被 TrafficCounter、AggregatedStats、日志、事件追溯等多处使用。修改格式会引发连锁不兼容,改前必须全局评估。

⚠️ **端口强制随机分配**:`PortAllocator.useRandom` 当前强制 true,不要改成顺序分配——那样端口可预测,暴露给攻击者有安全风险。

⚠️ **不要给端口加兜底常量**:任何 `if port == 0 { port = <常量> }` 都会让所有省略端口的调用方落在同一个监听地址上,而代理内核只在自己的日志里报绑定失败,v2raymg 看不见 —— 结果是记录落库、订阅照发、后端根本没监听。端口没人选时唯一正确的做法是走 `ClaimInboundPort` 抽签;没有权威可用时**报错**,不要猜。

⚠️ **xray 的 62789 必须保留**:`killProcessOnPort`(`pkg/proxy/containers/xray/exec_runner.go`)会 SIGKILL 任何持有该端口的进程,只挡 `pid<=1`,既不查进程名也不排除自己。forward relay 一旦拿到它,启动 xray 容器就等于 v2raymg 自杀。`reservedManagementPorts` 负责把它加进 `Reserved`。

⚠️ **`forward` 配置 key 必须带下划线**:`min_port` / `max_port`,不是 `minport` / `maxport`。loader 用 `yaml.Unmarshal` 按 struct tag 解析且不开 `KnownFields`,拼错的 key 被**静默丢弃**,配置看起来生效实际没有。2.8.x 之前的 `config.example.yaml` 就是错的。

---

## 反例

### ❌ 在 xray inbound adapter 里创建 forward 规则

```go
// 错误:直接在 inbound 层调 forwardMgr
func (a *XrayInboundAdapter) AddUser(user User) error {
    a.forwardMgr.AddRule(ForwardRule{...})  // ❌ 绕过了 UserManager
}
```

### ✅ 通过 UserManager 入口

```go
// 正确:调用 UserManager 入口,由它负责 forward 规则
port, err := userMgr.GetBindPort(GetBindPortRequest{
    Username:      user.Name,
    TargetPort:    inbound.DstPort,
    ContainerType: "xray",
    InboundTag:    inbound.Tag,
    Protocol:      inbound.Protocol,
})
```

### ❌ 为了"精确计流量"调 container 原生 API

```go
// 错误:新增 xray gRPC 统计依赖
stats := xrayClient.QueryStats(ctx, &StatsRequest{...})  // ❌ 禁止
```

### ✅ 读 forward 层统计

```go
// 正确:从 forward/usermanager 读
stats := userMgr.GetUserTrafficStats(username)
// 或
records := forwardMgr.GetAllTrafficRecords(false)
```
