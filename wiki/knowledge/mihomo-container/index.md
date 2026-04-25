---
title: mihomo 容器
aliases:
  - mihomo
  - clash-meta
  - meta-container
  - mihomo-container
answers:
  - mihomo 容器是什么模式的?为什么 listener 数量和用户数解耦?
  - 当前支持哪些协议?vless/vmess/trojan/ss/hy2/tuic 各自支持哪些 transport 和 security?
  - anytls 为什么还不支持?
  - 用户怎么接入 mihomo,为什么 listener 配置里没有用户?
  - mihomo 二进制从哪里下载,默认拉哪个 release?
  - Updater 怎么校验二进制的 SHA256?为什么 stable 不校验?
  - 进程重启后 inbound 和用户端口怎么恢复?
  - reconcile loop 在做什么?多久跑一次?
  - 怎么手动切到 Alpha 分支?
  - SS 默认 cipher 是什么?SIP022 cipher 的密码格式有什么要求?
  - SS 支持哪些插件?shadow-tls 为什么只写到订阅不下发到 listener?
  - trojan+ws+reality / vmess+ws+reality 为什么 skip?
  - Hysteria2 在 mihomo 容器和 hysteria 容器有什么区别?哪个先上?
  - hy2 listener 为什么要用 `users:{default:...}` 而不是顶层 password?
  - hy2:// URI 为什么不带 up/down/masquerade 参数?
  - hy2 inbound 的 forward 转发是 TCP 还是 UDP?哪里区分的?
  - mihomo 客户端配置为什么不写 masquerade?
  - tuic 为什么只做 v5 不做 v4?
  - tuic listener 的 `users` map key 必须是 uuid,跟 hy2 的 `default` 用户名有什么不同?
  - tuic URI query 为什么是 `congestion_control` 不是 `congestion-controller`?
  - 为什么 ZeroRTTHandshake 在 listener yaml 里不出现,只在客户端订阅生效?
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

当前协议状态:原 MVP 的 vmess / trojan / shadowsocks 已可用;协议扩展任务已完成 VLESS(Phase 1)、VMess 高级特性(Phase 2)、Trojan 高级特性(Phase 3)、Shadowsocks 增强(Phase 4)、Hysteria2(Phase 5)和 TUIC(Phase 6)。VLESS/VMess/Trojan/Shadowsocks/Hysteria2/TUIC 新增配置全部走 `ProtocolParams` 结构化路径,其中 Trojan 支持 tcp/ws/grpc × tls/reality,Shadowsocks 默认 cipher 升级为 `2022-blake3-aes-256-gcm` 并支持 obfs / v2ray-plugin / shadow-tls 三种插件(shadow-tls 仅落入订阅供客户端使用,服务端 listener 跑 plain SS),Hysteria2 是首个 QUIC/UDP 协议、强制 TLS、可选 salamander obfs / 带宽宣告 / 服务端 masquerade 伪装,TUIC 是第二个 QUIC/UDP 协议、**仅 v5**(uuid+password,v4 token 不支持)、强制 TLS、`congestion-controller` 默认 bbr 由 profilegen 显式写出。Hysteria2 / TUIC 都是 Phase 5/6 在 mihomo 容器的**首次出现**,无 legacy SharedCred 路径;forward 层为 hy2 / tuic 自动用 UDP 规则,其它协议保持 TCP 默认。历史持久化记录仍可通过 legacy SharedCred 兼容读取(vless/hy2/tuic 除外,因为是新引入)。anytls 尚未实现 parser/profilegen/subscription 分支。

## 关键事实

- **upstream**: github.com/MetaCubeX/mihomo
- **architecture-mode**: 模式 B(外部进程 + REST 热更)+ 共享凭据 listener
- **mvp-protocols**: vmess / trojan / shadowsocks
- **protocolparams-done**: vless / vmess / trojan / shadowsocks / hysteria2 / tuic
- **legacy-sharedcred**: 历史 vmess/trojan/ss 持久化记录的兼容读取(Phase 1-4 前的旧记录仍可加载;新 FastAdd 全部走 ProtocolParams)。**vless / hysteria2 / tuic 不存在 legacy SharedCred 路径**(Phase 1 / 5 / 6 首次引入)
- **ss-default-cipher**: `2022-blake3-aes-256-gcm`(SIP022;`FillDefaults` 自动生成 base64(32 bytes) 密钥;非 2022 系列继续用 hex 字符串)
- **ss-plugins-supported**: obfs / v2ray-plugin(下发到 mihomo listener 的 `plugin` + `plugin-opts`)、shadow-tls(仅订阅 Extensions,服务端 listener 不下发,因 shadow-tls 是网络层 wrapper 而非 mihomo SS 原生 plugin)
- **trojan-tls-requirement**: 必须带 cert_file + key_file(mihomo Alpha runtime 硬约束)
- **trojan-advanced-support**: tcp / ws / grpc + tls / reality;ws+reality 在 mihomo Alpha 上游存在栈顺序限制,系统测试 skip
- **trojan-cert-safe-paths**: cert 文件必须位于 MihomoConfig.DataDir 下(mihomo SAFE_PATHS)
- **hy2-listener-schema**: `users: map[string]string`(无顶层 password,单用户用 `default` 作 username)、`certificate`/`private-key`(非 cert-file/key-file)、`alpn` 数组默认 `["h3"]`、`obfs` 仅 `"salamander"` 或空、`up`/`down`/`ignore-client-bandwidth`/`masquerade` 全可选
- **hy2-uri-spec-keys**: 上游 hy2:// URI 仅识别 `obfs` / `obfs-password` / `sni` / `insecure` / `pinSHA256`;`up`/`down`/`masquerade` 不在标准里,仅通过订阅 Extensions 透传到客户端 Clash 配置
- **hy2-masquerade-server-only**: mihomo 客户端 outbound schema 没有 `masquerade` 字段,convertHysteria2 故意不传到 ClashProxy
- **hy2-forward-network**: forward 层用 UDPRelay(`forwardNetworkForProtocol(ProtocolHysteria2) → "udp"`,其它协议保持 TCP 默认);Phase 6 TUIC 已留 TODO 钩点
- **hy2-bandwidth-policy**: `up`/`down` 与 `ignore-client-bandwidth` **不互斥**(parser 不做互斥校验,mihomo 运行时自决)
- **tuic-version-policy**: 仅支持 v5(`users: map[uuid]password`);v4 token 鉴权(`token: []string`)在 mihomo 中**与 v5 互斥**(server.go:109-112 按 `len(token)==0` 选 packet overhead),v2raymg 永远不写 `token:` 字段
- **tuic-uuid-validation**: parseTUIC 用 `google/uuid.Parse` **严格校验**(mihomo Alpha 静默零化非法 uuid,会让所有用户共享 zero-uuid key);vless/vmess 不做严格校验,这是有意的协议特异性
- **tuic-listener-schema**: `users: map[uuid]password`(单用户;uuid 来自共享 cred)、`certificate`/`private-key`(同 hy2,文件路径)、`alpn` 默认 `["h3"]`、**`congestion-controller` 默认 `"bbr"` profilegen 显式写**(mihomo 自身的默认在 `listener/parse.go:116` 注入,v2raymg 直接生成 yaml 不走此路径,所以必须显式)
- **tuic-uri-spec-keys**: dae 草案 + mihomo 改;query 用**下划线**(`congestion_control`/`udp_relay_mode`/`disable_sni`),不是连字符;`allow_insecure=1` Decode 接收(NekoBox/Hiddify 兼容)Encode 不发(mihomo 自己 strip,clash 路径靠 ClashProxy.SkipCertVerify)
- **tuic-client-only-fields**: `ZeroRTTHandshake` / `HeartbeatInterval` / `UDPRelayMode` / `DisableSNI` 都是**客户端字段**(mihomo listener struct 没有对应键,Allow0RTT=true 在 server.go:101 强制),只在订阅生成时写入 ClashProxy 的 `reduce-rtt`(注意不是 zero-rtt-handshake!) / `heartbeat-interval`(int ms)/ `udp-relay-mode` / `disable-sni`
- **tuic-heartbeat-format**: `convertTuic` 同时接受 Go 时长字符串(`"10s"`)和**纯毫秒整数**(`"10000"`)—— 后者匹配 mihomo 上游 yaml 字面值,避免操作员复制 default 时静默归零
- **tuic-forward-network**: forward 层用 UDPRelay(`forwardNetworkForProtocol(ProtocolTUIC) → "udp"`,与 hy2 共用同一个 switch 分支)
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
  - `subscription.go` — 用户订阅生成(vless/vmess/trojan/ss/hy2/tuic 全部走 ProtocolParams,vmess/trojan/ss 旧记录走 SharedCred 兼容路径;URI 复用 codec 层。hy2 的 up/down/masquerade 通过 Extensions 透传,不入 URI;tuic 的 ZeroRTTHandshake/HeartbeatInterval/DisableSNI 客户端专用,以 Extensions 透传)

深入实现细节见 details.md;FAQ 和反例见 edge-cases.md;关联概念见 related.md。
