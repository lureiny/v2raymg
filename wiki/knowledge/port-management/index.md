---
title: 端口管理与流量统计
aliases:
  - 端口管理
  - 端口绑定
  - port-management
  - port-binding
  - 流量统计
  - traffic-stats
  - forward-rule
answers:
  - 端口池的范围是多少,默认端口怎么分配?
  - 一个用户可以有几个端口,端口和 inbound 是什么关系?
  - 怎么给用户申请一个转发端口,入口 API 是哪个?
  - 客户端连到端口后流量怎么路由到 xray 的?
  - 流量统计是在哪一层做的,为什么不用 container.QueryStats?
  - 端口绑定重启后还能恢复吗,PortMappings 是干什么的?
  - Forward 规则可以在 inbound 或 container 内部直接创建吗?
  - 怎么读取某个用户的累计流量和增量流量?
  - inbound 的端口是谁分配的?会不会跟 forward 端口撞上?
  - 显式指定的 inbound 端口被占了会怎样?会自动换一个吗?
  - 重启后持久化的 inbound 端口是怎么保住的?
tags:
  - module
  - proxy
  - forward
  - usermanager
  - traffic
confidence: high
layer: index
---

## 概述

端口管理负责在 "用户侧外向端口" 和 "xray inbound 后端端口" 之间建立一对一的 TCP 转发规则,并在转发层(Relay)采集上/下行流量。核心组件有三个:PortAllocator(端口池)、ForwardRule(一条转发规则)、TrafficCounter(原子流量计数器),全部位于 `pkg/proxy/forward/`。用户与端口的关联通过 `UserManager.GetBindPort/ReleaseBindPort` 唯一入口完成,inbound 和 container 禁止直接调用 `forwardMgr.AddRule` 创建业务规则。流量统计只依赖 Forward 层,不依赖 `container.QueryStats`,保证协议无关和口径统一。

## 关键事实

- **port-authority**: 进程级唯一。forward 端口与**所有容器的 inbound 后端端口**共用同一张分配表(2.9 起)
- **port-authority-injection**: `cmd/server.go` 只 new 一个 `DefaultForwardManager`,经 `container.BuildOptions.PortClaimer` 注入。`forward.GlobalForwardManager` 单例**已删除**
- **port-range-default**: 10000–60000(`forward.min_port`/`max_port`,**只约束随机抽签**)
- **port-bookkeeping-range**: 1024–65535(`AllocateSpecific` 的接受范围,与抽签范围解耦)
- **port-alloc-strategy**: 随机分配(useRandom 强制开启)
- **inbound-port-entry**: `container.ClaimInboundPort`(pkg/proxy/core/container/portclaim.go)
- **inbound-port-explicit-policy**: 显式指定的端口冲突即报 `ErrPortInUse`,**绝不静默替换**
- **inbound-port-startup-preclaim**: `cmd/server.go` 第 2b 步,早于任何容器构造/启动
- **port-allocator-file**: pkg/proxy/forward/port_allocator.go:10
- **forward-rule-file**: pkg/proxy/forward/types.go:18
- **rule-key-format**: `{containerType}:{inboundTag}:{username}` 例 `xray:vmess-tcp:user@test.com`
- **bind-entry**: UserManager.GetBindPort (pkg/proxy/usermanager/usermanager.go:802)
- **release-entry**: UserManager.ReleaseBindPort (pkg/proxy/usermanager/usermanager.go:1225)
- **forward-rule-entry-constraint**: 业务规则只能走 UserManager,inbound/container 禁止直接调 forwardMgr.AddRule
- **user-port-fields**: `User.BindPorts []uint32` + `User.PortMappings map[dstPort]forwardPort`
- **user-port-fields-file**: pkg/proxy/core/contracts/user.go:64
- **data-plane**: Relay(纯 TCP 字节转发,不解析协议)
- **data-plane-file**: pkg/proxy/forward/relay.go
- **traffic-counter-type**: 原子 int64(upload/download/activeConns)
- **traffic-source-of-truth**: Forward 层,禁止使用 container.QueryStats
- **traffic-granularity**: Rule(最细)→ 可聚合到 User/Inbound/Container/Global
- **traffic-collector-interval**: UserManager.StartTrafficStats(interval) 轮询差值
- **traffic-persistence**: User.TrafficTotal{Uplink,Downlink} 落库累计,Delta 字段用于 Prometheus 增量

## 三大核心抽象

### PortAllocator —— 进程唯一的端口权威

维护 `allocated` 和 `reserved` 两个 map。**注意它管的不只是 forward 端口** —— 2.9 起,四个容器的 inbound 后端端口也记在同一张表里,所以两边永远不会拿到同一个端口。

有两个必须分清的范围:

| 范围 | 谁受约束 | 值 |
|---|---|---|
| **抽签范围** | `Allocate()`(没人指定端口时随机挑) | `[min_port, max_port]`,默认 10000–60000 |
| **记账范围** | `AllocateSpecific(port)`(显式指定 / 重启恢复) | `[1024, 65535]`,与抽签范围无关 |

拆开的理由:如果 `AllocateSpecific` 也按抽签范围校验,范围外的存量端口就永远登记不进来,也就得不到保护;而且收窄抽签范围会变成破坏性变更 —— 存量 `PortMappings` 里超出新上限的端口恢复失败,回落自动分配,用户端口静默漂移,订阅全废。

定义见 `pkg/proxy/forward/port_allocator.go`。

### ForwardRule —— 一条转发规则

一个用户的一个 inbound 对应一条规则。关键字段:

- `ListenPort`: 用户侧外向端口(客户端连这个)
- `TargetAddr`: inbound 后端地址,形如 `127.0.0.1:{dstPort}`
- `Username / ContainerType / InboundTag / Protocol`: 归属信息
- `MaxConnections / MaxClients / 限速字段`: 从 User 复制下来的运行时限制
- `RuleKey()`: `{containerType}:{inboundTag}:{username}`,作为幂等键

定义见 `pkg/proxy/forward/types.go:18`。

### TrafficCounter —— 原子流量计数器

三个原子 int64:`upload / download / activeConns`。每条 ForwardRule 对应一个 Counter,由 Relay 在字节拷贝时原子累加。定义见 `pkg/proxy/forward/traffic.go:10`。

## 端口↔用户↔流量完整映射链

```
User                  ForwardRule                   Relay                     TrafficCounter
─────────────────────────────────────────────────────────────────────────────────────────
Username ───────────► Username                        │                              │
BindPorts[i] ──┐      RuleKey                         │                              │
PortMappings:  ├────► ListenPort (10050) ──► listen 0.0.0.0:10050 ──┐                │
  54321:10050 ─┘      TargetAddr (127.0.0.1:54321) ─► dial 后端    ◄─┤              │
                                                                    ├──► Up/Down ───┤
xray inbound (dstPort=54321) ◄──────────────────────────────────────┘              │
                                                                                      ▼
                               Collector(轮询) ◄── forwardMgr.GetAllTrafficRecords()
                                     │
                                     ▼
                          User.TrafficTotal* 落库 + ByUser/ByInbound/ByContainer 聚合
```

详细流程、字段映射表、统计 API 见 [details.md](details.md);关键约束与禁止事项见 [edge-cases.md](edge-cases.md)。
