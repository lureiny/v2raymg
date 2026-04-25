---
title: mihomo 容器
aliases:
  - mihomo
  - clash-meta
  - meta-container
  - mihomo-container
answers:
  - mihomo 容器是什么模式的?为什么 listener 数量和用户数解耦?
  - MVP 支持哪些协议?为什么不支持 vless/hysteria2/tuic?
  - 用户怎么接入 mihomo,为什么 listener 配置里没有用户?
  - mihomo 二进制从哪里下载,默认拉哪个 release?
  - Updater 怎么校验二进制的 SHA256?为什么 stable 不校验?
  - 进程重启后 inbound 和用户端口怎么恢复?
  - reconcile loop 在做什么?多久跑一次?
  - 怎么手动切到 Alpha 分支?
tags:
  - module
  - proxy
  - container
  - mihomo
  - forward
confidence: high
layer: index
---

## 概述

mihomo 容器是 v2raymg 对 MetaCubeX/mihomo(原 Clash.Meta)内核的适配层,位于 `pkg/proxy/containers/mihomo/`。采用 `docs/container-design-principles.md` 的**模式 B**(进程外 + REST API 热更),listener 仍是**共享凭据模型**:每个 inbound 对应一条 mihomo listener,所有绑到这个 inbound 的用户共享同一把协议凭据,用户级隔离完全由 forward 层的端口分配提供。这意味着 listener 数量 = inbound 数量(典型 1~10),与用户规模解耦。

当前协议状态:原 MVP 的 vmess / trojan / shadowsocks 已可用;协议扩展任务已完成 VLESS(Phase 1)、VMess 高级特性(Phase 2)和 Trojan 高级特性(Phase 3)。VLESS/VMess/Trojan 新增配置走 `ProtocolParams` 结构化路径,其中 Trojan 支持 tcp/ws/grpc × tls/reality;Shadowsocks 仍走 legacy SharedCred 路径。hysteria2 / tuic / anytls 尚未实现 parser/profilegen/subscription 分支。

## 关键事实

- **upstream**: github.com/MetaCubeX/mihomo
- **architecture-mode**: 模式 B(外部进程 + REST 热更)+ 共享凭据 listener
- **mvp-protocols**: vmess / trojan / shadowsocks
- **protocolparams-done**: vless / vmess / trojan
- **legacy-sharedcred**: shadowsocks(以及历史 vmess/trojan 持久化记录的兼容读取)
- **trojan-tls-requirement**: 必须带 cert_file + key_file(mihomo Alpha runtime 硬约束)
- **trojan-advanced-support**: tcp / ws / grpc + tls / reality;ws+reality 在 mihomo Alpha 上游存在栈顺序限制,系统测试 skip
- **trojan-cert-safe-paths**: cert 文件必须位于 MihomoConfig.DataDir 下(mihomo SAFE_PATHS)
- **default-release-tag**: latest(GitHub /releases/latest,即最新 stable)
- **alpha-release-tag**: Prerelease-Alpha(需显式设置)
- **auto-download-default**: true
- **binary-platform**: linux/amd64 only(GOAMD64=v1 资产)
- **alpha-asset-prefix**: mihomo-linux-amd64-v1-alpha-
- **stable-asset-pattern**: mihomo-linux-amd64-v1-<tag>.gz(精确匹配)
- **external-controller-default**: 127.0.0.1:9090
- **config-file-mode**: 0600(防凭据泄漏)
- **default-rules**: [MATCH, DIRECT]
- **listener-count-model**: = inbound 数,与用户规模解耦
- **user-ingress-path**: forward 层(不出现在 mihomo listener yaml 中)
- **listener-name**: inbound tag(驱动 mihomo PatchInboundListeners 的 name-keyed diff)
- **reconcile-interval**: 30s
- **readiness-timeout**: 10s
- **sha256-source**: checksums.txt(仅 Alpha 发布;stable 跳过校验 log.Debug)
- **rest-endpoints**: GET /version, PUT /configs, PATCH /configs, POST /configs/geo
- **restart-policy-on-update**: Always(默认,可被 UpdateRequest 覆盖)
- **post-restart-probe**: WaitReady → GET /version(stage 10a;失败走 rollback)

## 数据流概览

```
client ──► forward port (per user)
            │
            └─► relay ──► 127.0.0.1:<inbound_port>  (mihomo listener)
                            │
                            └─► DIRECT ──► target origin
```

listener 只持一把共享凭据;把"哪个用户"的区分下推到 forward 层。mihomo 内部的 rules 保持 `[MATCH, DIRECT]`,不参与用户路由。

## 核心数据源

- 设计文档:`docs/mihomo-container-design.md`
- 实施计划 + 逐阶段变更日志:`docs/mihomo-container-implementation-plan.md`
- 代码:`pkg/proxy/containers/mihomo/`
  - `container.go` — 生命周期 + 用户事件 + Restore/Reload
  - `process.go` — startProcess/stopProcess + mihomoInitialConfig
  - `inbound.go` / `adapter.go` / `profilegen.go` — InboundSpec 与 mihomo yaml 的双向映射
  - `rest_client.go` — GET /version / PUT /configs 等 REST 访问
  - `updater.go` — 下载 + SHA256 + 原子 swap + Start + WaitReady + rollback
  - `subscription.go` — 用户订阅生成(vless/vmess/trojan 走 ProtocolParams;ss 仍走 SharedCred;URI 复用 codec 层)

深入实现细节见 details.md;FAQ 和反例见 edge-cases.md;关联概念见 related.md。
