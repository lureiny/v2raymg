---
title: 端口管理与流量统计
layer: details
---

## 1. PortAllocator 端口池

位置:`pkg/proxy/forward/port_allocator.go:10`。

```go
type PortAllocator struct {
    mu        sync.Mutex
    minPort   uint32          // 下界(含)
    maxPort   uint32          // 上界(含)
    allocated map[uint32]bool // 当前分配集合
    reserved  map[uint32]bool // 保留(系统)端口
    nextHint  uint32          // 轮询模式指针
    useRandom bool            // 当前强制为 true
}

type PortAllocatorConfig struct {
    MinPort   uint32
    MaxPort   uint32
    Reserved  []uint32
    UseRandom bool
}
```

### 分配语义

- `Allocate()`:在 `[minPort, maxPort]` 中随机选一个空闲端口,标记到 allocated。
- `AllocateSpecific(port)`:指定端口,用于重启恢复时按 `User.PortMappings` 复用历史端口。
- `Release(port)`:归还到池。
- 判空条件:端口不在 `allocated` **且** 不在 `reserved`。

### 默认范围

对外默认 `10000–65535`(代码构造时指定)。保留端口由上层按需传入。

---

## 2. ForwardRule 转发规则

位置:`pkg/proxy/forward/types.go:18`。

```go
type ForwardRule struct {
    ID            string
    Username      string
    ContainerType contracts.ContainerType  // "xray" / "snell" / ...
    InboundTag    string                   // 如 "vmess-tcp"
    Protocol      contracts.Protocol       // 如 "vmess"

    ListenAddr    string   // 默认 "0.0.0.0"
    ListenPort    uint32   // 用户侧外向端口(由 allocator 填充)
    TargetAddr    string   // "127.0.0.1:{dstPort}" 指向 xray inbound 后端

    // 运行时限制,从 User 复制
    UploadBytesPerSec     int64
    DownloadBytesPerSec   int64
    MaxConnections        int
    MaxClients            int
    ClientRecycleDelaySec int
    ClientDrainSec        int
}

func (r *ForwardRule) RuleKey() string {
    return string(r.ContainerType) + ":" + r.InboundTag + ":" + r.Username
}
```

### RuleKey 的作用

RuleKey 是幂等键。`forwardMgr.AddRule(rule)` 对同一 RuleKey 的重复调用会直接返回已有规则,不会重复分配端口。它也是 TrafficCounter、QueryTrafficStats 聚合的主键。

---

## 3. User 端侧的端口字段

位置:`pkg/proxy/core/contracts/user.go:64`。

```go
type UserSpec struct {
    // ...

    // 该用户当前占用的所有外向端口
    BindPorts []uint32 `json:"bind_ports,omitempty"`

    // dstPort(inbound 后端) -> forwardPort(外向端口)
    // 用于重启后确定性复用同一个外向端口
    PortMappings map[uint32]uint32 `json:"port_mappings,omitempty"`
}
```

`PortMappings` 持久化到 Store,是重启幂等的关键。

---

## 4. GetBindPort 绑定流程

入口:`UserManager.GetBindPort` (`pkg/proxy/usermanager/usermanager.go:802`)。

```go
type GetBindPortRequest struct {
    Username      string
    TargetPort    uint32                  // inbound 后端端口 dstPort
    ContainerType contracts.ContainerType
    InboundTag    string
    Protocol      contracts.Protocol
}
```

完整步骤:

1. **校验用户**:用户必须存在且未过期。
2. **幂等短路**:`ruleKey = "{ct}:{tag}:{user}"`,若 `forwardMgr.GetRule(ruleKey)` 已存在,直接返回 `existingRule.ListenPort`。
3. **查历史映射**:`preferredPort := user.PortMappings[targetPort]`,若有则用它(重启恢复)。
4. **构造 ForwardRule**:填好 Listen/Target 与从 User 复制的限速/连接限制。
5. **forwardMgr.AddRule(rule)**:
   - 端口分配:`preferredPort != 0` 走 `AllocateSpecific`,否则走 `Allocate`。
   - 建 TrafficCounter(`traffic.GetOrCreate(ruleKey)`)。
   - 建/复用 userBandwidthLimiter、clientLimiter。
   - 启 Relay 监听 `0.0.0.0:allocatedPort`。
6. **回写 User**:`BindPorts.append(port)`、`PortMappings[targetPort] = port`,持久化到 Store。
7. **发事件**:`UserEventPortBind`。

---

## 5. ReleaseBindPort 释放流程

入口:`UserManager.ReleaseBindPort` (`pkg/proxy/usermanager/usermanager.go:1225`)。

```go
type ReleaseBindPortRequest struct {
    Username string
    BindPort uint32
}
```

步骤:

1. 从 `user.BindPorts` 移除端口。
2. 从 `user.PortMappings` 移除对应项(反查)。
3. 调 `forwardMgr.RemoveRulesByUser(username)` 关规则 → Relay.Stop → PortAllocator.Release。
4. 发 `UserEventPortBind` 事件(解绑语义)。
5. 持久化 User。

---

## 6. Relay 数据平面

位置:`pkg/proxy/forward/relay.go`。

```
Client:52345 ──TCP──► 0.0.0.0:10050        Relay 监听
                           │
                           ├─ acceptLoop()
                           │    ├─ clientLimiter.Acquire(remoteIP)   按 IP 限 MaxClients
                           │    ├─ 检查 maxConns                      并发上限
                           │    └─ go handleConn(clientConn, remoteIP)
                           │
                           ├─ handleConn:
                           │    ├─ net.DialTimeout("tcp", "127.0.0.1:54321")
                           │    ├─ counter.IncrConns()
                           │    ├─ clientLimiter.ConfirmAcquire()
                           │    ├─ 启双向 copyWithCount
                           │    │    ├─ upload  : AddUpload(n)     原子
                           │    │    └─ download: AddDownload(n)   原子
                           │    └─ 连接关闭:counter.DecrConns() + clientLimiter.Release
                           │
                           ▼
                        xray inbound 后端(127.0.0.1:54321)
```

要点:

- Relay 是纯 TCP 字节转发器,**不解析任何代理协议**(VLESS/VMess/Trojan 的握手都在 xray 侧完成)。
- 用户区分不是靠解析协议,而是靠 "外向端口 ↔ dstPort" 的一对一对应。
- 限速应用 token bucket,上下行分别限流。

---

## 7. 流量统计

### 7.1 为什么只用 Forward 层

设计决策(CLAUDE.md 项目约定):

- 协议透明:VLESS/VMess/Trojan 的 container 侧统计粒度和完整性不一致。
- 容器解耦:避免每个 container 实现都要提供统计接口。
- 口径唯一:所有用户看到的流量都来自同一个来源。

### 7.2 多层级数据结构

`pkg/proxy/forward/types.go:88`:

```go
type TrafficSnapshot struct {
    Username          string
    Upload            int64
    Download          int64
    ActiveConnections int64
}

type ForwardTrafficRecord struct {
    RuleKey            string
    Username           string
    ContainerType      contracts.ContainerType
    InboundTag         string
    Protocol           contracts.Protocol
    ListenPort         uint32
    TargetAddr         string
    UplinkBytes        int64
    DownlinkBytes      int64
    ActiveConnections  int64
}

type TrafficQueryResult struct {
    Records         []ForwardTrafficRecord
    AggregatedStats map[string]ForwardTrafficRecord  // 按 GroupBy 聚合
    TotalUplink     int64
    TotalDownlink   int64
}
```

### 7.3 Forward 层读取 API

`pkg/proxy/forward/manager.go:382`:

| API | 用途 |
|---|---|
| `GetTraffic(ruleKey, reset)` | 单规则快照 |
| `GetAllTraffic(reset)` | 全量简化统计(ByRule map) |
| `GetAllTrafficRecords(reset)` | 全量带元数据记录 |
| `QueryTrafficStats(TrafficQuery{GroupBy: "user\|container\|inbound\|rule"})` | 多维过滤+聚合 |

### 7.4 UserManager 聚合层

`pkg/proxy/usermanager/usermanager.go:1562`。

```go
type TrafficStats struct {
    DeltaUplink   int64  // 自上次重置以来的增量
    DeltaDownlink int64
    TotalUplink   int64  // 持久化到 DB 的累计
    TotalDownlink int64
}

type AggregatedStats struct {
    ByUser      map[string]UserTrafficStats
    ByInbound   map[string]InboundTrafficStats
    ByContainer map[string]ContainerTrafficStats
    Global      GlobalTrafficStats
}
```

采集流程:

1. `StartTrafficStats(interval)` 启动后台协程。
2. 每轮调 `forwardMgr.GetAllTrafficRecords(reset=false)` 拿快照。
3. 与上一轮 `prevCounters` 算差值 → 累加到 `Delta*` 和 `Total*`。
4. 聚合到 ByUser/ByInbound/ByContainer/Global。
5. `onCollect` 回调持久化:`user.TrafficTotalUplink += delta_up` 落库。

### 7.5 查询 API

| API | 语义 |
|---|---|
| `GetUserTrafficStats(user)` | 累计量(管理后台) |
| `GetUserDeltaTraffic(user, reset=true)` | 增量(Prometheus 抓取) |
| `GetAllDeltaTraffic(reset)` | 全量增量 |
| `GetInboundTrafficStats(ct, tag)` | 按 inbound 聚合 |
| `GetContainerTrafficStats(ct)` | 按 container 聚合 |
| `GetGlobalTrafficStats()` | 全局 |
| `ResetUserTotalTraffic(user)` | 清零累计 |

---

## 8. 字段映射速查表

| 语义 | 存储位置 | 字段名 |
|---|---|---|
| 用户名 | User | `Username` |
| 外向端口 | User.BindPorts / ForwardRule | `ListenPort` |
| inbound 后端端口 | ForwardRule.TargetAddr / User.PortMappings key | `dstPort` |
| 规则唯一键 | ForwardRule.RuleKey() | `ct:tag:user` |
| 上行字节 | TrafficCounter / ForwardTrafficRecord | `upload` / `UplinkBytes` |
| 下行字节 | TrafficCounter / ForwardTrafficRecord | `download` / `DownlinkBytes` |
| 活跃连接 | TrafficCounter / ForwardTrafficRecord | `activeConns` / `ActiveConnections` |
| 增量 | UserTrafficStats | `DeltaUplink` / `DeltaDownlink` |
| 累计 | User + UserTrafficStats | `TrafficTotalUplink` / `TotalUplink` |
