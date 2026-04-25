---
title: mihomo 容器
layer: edge
---

## FAQ

**Q: 默认 `release_tag` 是 `latest`,会拉到 Alpha 吗?**

A: 不会。GitHub `/releases/latest` API 只返回最新的 non-prerelease,也就是 stable(当前 `v1.19.x`)。要跟 Alpha 分支必须显式设 `release_tag: Prerelease-Alpha`。stage 9 前默认值是 Prerelease-Alpha,stage 9 切到了 latest;升级运维需要注意 config 变更。

**Q: stable 版本没 SHA256 校验是不是不安全?**

A: 是 known gap。mihomo 的 stable release 当前(2026-04)**不发 `checksums.txt`**,只有 Alpha 发。Updater 的策略是"有就校验没就跳过 + log.Debug"。如果 stable 将来开始发 checksums.txt,同一代码路径自动生效。另一条路径是用每个 asset API response 里的 `digest` 字段,但需要改 `tools.GitHubReleaseClient` 的 JSON 解码,留作 follow-up。

**Q: `AutoDownload=false` 什么时候用?**

A: 当二进制是生产流水线外部管理(DEB/RPM 包、容器镜像预装)的时候。此时 Start 发现 binary 缺失直接 fail(不静默下载),`Update()` 返 `ErrNotSupported`。

**Q: trojan 能否跑在 plain TCP 上(无 TLS)?**

A: **不能**。stage 11+ 的真实 E2E 测试(`TestMihomoE2E_RealInternet`)确认了 P2-9:mihomo Alpha 的 trojan listener 运行时拒绝启动,报 `disallow using Trojan without both certificates/reality/ss config`。Phase 3 后 trojan 新 FastAdd 走 ProtocolParams,必须是 `security=tls` 或 `security=reality`;TLS 模式需要 cert/key,Reality 模式需要 reality target/serverNames/key/shortId。生产 trojan TLS inbound 的 cert 文件路径必须在 `MihomoConfig.DataDir` 下或被 SAFE_PATHS 接受。

**Q: trojan+ws+reality 能跑吗?**

A: 当前 mihomo Alpha 上不能稳定通过。`TestMihomoTrojanMatrix` 实测 tcp/grpc reality 通过,ws+reality 客户端连接报 `unexpected status: 200 OK`,与 VLESS/VMess ws+reality 的已知栈顺序限制同类。系统测试保留该 case 但 skip,等 upstream 修复后再解封。

**Q: 为什么 SS 默认 cipher 用 `2022-blake3-aes-256-gcm` 而不是 `aes-256-gcm`?**

A: SIP022(2022-blake3-* 系列)是 Shadowsocks 当前的推荐默认,提供前向安全和更强抗探测能力,mihomo 和大部分 Clash 衍生客户端都已支持。Phase 4 把 `FillDefaults` / `defaultSSMethod`(profilegen 和 subscription)/ `convertShadowsocks` 的默认全部统一到 `2022-blake3-aes-256-gcm`,确保 server listener、订阅 method 字段、URI 和 client clash 配置同步切换。如果客户端仍只支持 classic AEAD,显式传 `cipher=aes-256-gcm` 即可。

**Q: 用 SIP022 cipher 时密码格式有要求吗?**

A: 有。SIP022 把"密码"当作 raw key material,服务端期待 standard base64 编码的随机字节(`2022-blake3-aes-256-gcm` 需要 32 字节 → 44 字符 base64;`2022-blake3-aes-128-gcm` 需要 16 字节 → 24 字符 base64)。`FillDefaults::randomSSPassword` 会按 cipher 自动选择:`2022-` 前缀的 cipher 走 `base64.StdEncoding.EncodeToString(rand.Read(N))`;classic AEAD 仍走 hex 字符串(16 字节 → 32 字符 hex)。客户端手动指定密码时也要遵守该约束,否则 mihomo listener 会以 `EOF` / 解码失败的形式报错。

**Q: shadow-tls 插件为什么只写到订阅,不下发到 mihomo listener?**

A: shadow-tls 是一个**网络层 wrapper proxy**,不是 mihomo SS listener 的原生 plugin。架构上它需要单独跑一个 shadow-tls server 进程,把外部 TLS 流量解 wrap 后再转给后端 plain SS。mihomo 服务端只能跑 plain SS;`fillSSListener` 检测到 `plugin=shadow-tls` 时跳过 `plugin` / `plugin-opts` 字段,只把信息记录到订阅 Extensions(`plugin_host` / `plugin_password` / `plugin_version`),供客户端 Clash config 生成对应的 shadow-tls outbound 段。系统测试因此对 shadow-tls 用 `t.Skip("shadow-tls requires external server")`,需要外部基础设施做端到端验证。

**Q: SS 的 udp 字段不传时是 true 还是 false?**

A: 默认 **true**,因为 SS 在实际部署中绝大多数场景都需要 UDP(浏览 / DNS / QUIC)。`parseSS` 仅在显式传入 `udp=false` / `udp="false"` / `udp="0"` 时关闭;空字符串和未识别的字符串都保持默认 true(避免误把 `udp=""` 当成关闭信号)。订阅 Extensions 会把当前 udp 值原样写入,clash converter 的 `convertShadowsocks` 据此决定 `proxy.UDP`,所以 server 端关 UDP 后客户端 clash config 也会同步关。

**Q: 进程 crash 后 v2raymg 会自动拉起吗?**

A: **不会**。production 没有 auto-restart watchdog。运维需通过 HTTP API 的 Restart 端点触发,底层走 `container.Stop()` + `container.Start()`,Restore 和 reconcileUsers 负责恢复状态。

**Q: 同一台机器可以跑多个 mihomo 容器实例吗?**

A: 可以,但 `ExternalController` 必须各不相同(`apiPort` 冲突会导致第二个实例 Start 失败)。`BinaryPath` 可以共享(只读 mmap);`ConfigFilePath` 和 `DataDir` 必须独立。

**Q: 升级成功但新 binary 立刻 crash 会发生什么?**

A: stage 10a 之前会误报 `Restarted=true`。stage 10a 后:`Updater.Update` 的 Step 7 在 `Start` 返回后跑 `WaitReady`(GET /version 探活,10s 超时),失败则走 `Stop + Rollback + ErrUpdateFailed{Stage:"restart"}`。rollback 也失败时两 cause 通过 `errors.Join` 合并,operator 需手动从 `<BinaryPath>.bak` 恢复。

**Q: Hysteria2 在 mihomo 容器和 hysteria 容器有什么区别?**

A: hysteria 容器是**单 inbound legacy**形态:`pkg/proxy/containers/hysteria/container.go::FastAddInbound` 只接受 `defaultInboundTag`,params 全部忽略,协议参数通过 `HysteriaConfig` 进程级配置。mihomo 容器从 Phase 5 起支持 hy2 多 inbound,每条 inbound 独立 password / obfs / 带宽宣告 / masquerade,都通过 `FastAddInbound` 的 ProtocolParams 路径进入 mihomo `listeners:` 数组。Phase 5 实施期间 hysteria 容器**完全不动**,新加的 `container_fastadd_test.go` 锁定它的旧行为,以便后续重构时不被误改。两个容器可以共存(实际部署用同一台机器跑 mihomo 接 hy2 + hysteria 容器跑 legacy 单 inbound,验证迁移路径)。

**Q: hy2 inbound 的 forward 转发是 TCP 还是 UDP?哪里区分的?**

A: UDP。`MihomoInbound.AddUser` 调用 `userMgr.GetBindPort` 时传 `Network: forwardNetworkForProtocol(protocol)`,该 helper 对 `ProtocolHysteria2` / `ProtocolTUIC` 都返回 `"udp"`(Phase 6 落地),其它协议返空(走 GetBindPort 默认 TCP)。注意:**SS 的 UDP 是协议内部 wrap**(SS 协议帧自己携带 UDP payload),不在 forward 层切 Network,所以 SS 走 TCP 默认即可。

**Q: TUIC 为什么只做 v5 不做 v4?**

A: v4 用 `token: []string`(预共享密钥列表)鉴权,**所有 token 在 mihomo 日志里不可区分**(没有用户名 / uuid 概念),无法做 per-user 计费 / 流量统计 / 端口轮转 —— v2raymg 的核心价值就是 per-user 隔离,v4 与之对立。v5 用 `users: map[uuid]password` 显式给每个用户独立 uuid。另:**v4 / v5 在 mihomo listener 中互斥**(`tuic/server.go:109-112` 按 `len(token)==0` 选 packet overhead),同时填两边会让 server 跑成 V4 模式,所以 profilegen 永远只写 `users:` 不写 `token:`。

**Q: TUIC listener 的 `users` map key 必须是 uuid,跟 hy2 的 `default` 用户名有什么不同?**

A: TUIC v5 的 `users: map[string]string` **key 是 uuid 字符串**(value 是 password),mihomo 用 `uuid.FromStringOrNil` 解析 —— 非法 uuid 会被静默零化,所有"用户"会塌缩到 zero-uuid 一个 key。v2raymg 的 `parseTUIC` 用 `google/uuid.Parse` 严格校验,FastAdd 阶段就拒掉非法 uuid。Hy2 的 `users: { default: <password> }` 只是把 `"default"` 当字符串 username,没有解析约束。两者都是单 key map(共享凭据 + forward 层做 user 隔离的同款模式),只是 key 类型语义不同。

**Q: TUIC URI query 为什么是 `congestion_control` 不是 `congestion-controller`?**

A: TUIC 没有 IETF 标准 share URI,事实标准是 [dae 草案](https://github.com/daeuniverse/dae/discussions/182) + mihomo 的修订(`common/convert/converter.go:106-147`)。mihomo 的 URI 解析器把 query key 用**下划线**:`congestion_control` / `udp_relay_mode` / `disable_sni` / `sni` / `alpn`。其中 `congestion_control` 映射到 mihomo client/server yaml 的 `congestion-controller` 字段(连字符)—— URI 用下划线、yaml 用连字符,是 mihomo 内部约定。`allow_insecure` 在 dae 标准里是 `skip-cert-verify` 的 URI 表达,但 **mihomo 显式 strip 掉这个字段不映射**,v2raymg 的 codec 在 Decode 时仍然兼容接收(NekoBox/Hiddify 互通),Encode 时**永不发出**。

**Q: 为什么 ZeroRTTHandshake 在 listener yaml 里不出现,只在客户端订阅生效?**

A: mihomo Alpha 的 TUIC server 在 `listener/tuic/server.go:101` 硬编码 `Allow0RTT: true`,**listener 没有对应 yaml key 关闭**。客户端 outbound `adapter/outbound/tuic.go::TuicOption` 有 `reduce-rtt bool` 字段(注意:**不是** `zero-rtt-handshake`!),控制客户端发起握手时是否复用之前的 0-RTT 票据。v2raymg 的 `TUICParams.ZeroRTTHandshake` 只在 `fillTuicSubscriptionSpec` 里被读取,写到 `Extensions["zero_rtt_handshake"]` 然后由 `convertTuic` 翻译成 ClashProxy.ReduceRTT(yaml tag `reduce-rtt`)。`profilegen.fillTuicListener` 完全忽略这个字段。同样的 client-only 处理还有 `HeartbeatInterval`(可写 `"10s"` 或纯毫秒数 `"10000"`)和 `DisableSNI`。`TestFillTuicListener_DropsClientOnlyKnobs` 用反向断言锁住这个契约:即使 FastAdd 传了这些字段,listener yaml 也绝不能有它们。

**Q: hy2 mihomo 客户端配置为什么不写 masquerade?**

A: mihomo Alpha 的 `adapter/outbound/hysteria2.go::Hysteria2Option`(客户端 outbound)**没有** `masquerade` 字段 —— masquerade 是服务端伪装(响应未鉴权流量时假装成 nginx / file server / proxy),只在 mihomo `listener/inbound/hysteria2.go::Hysteria2Option` 里。所以 `convertHysteria2` 故意不传 masquerade 到 ClashProxy;`TestConvertHysteria2_DropsMasquerade` 用 `yaml.Marshal` 输出后 `strings.Contains` 反向断言锁住此契约。masquerade 仍写到订阅 spec.Extensions(为了完整性 + 调试可见),只是 client 端永远 drop。

**Q: hy2:// URI 为什么不带 up/down/masquerade 参数?**

A: 上游 hysteria2 URI spec(<https://v2.hysteria.network/docs/developers/URI-Scheme/>)只规定了 5 个 query key:`obfs` / `obfs-password` / `sni` / `insecure` / `pinSHA256`。`up`/`down`/`masquerade` 不在标准里,塞进 URI 会导致 NekoBox / Hiddify 等通用客户端解析失败。所以 `codec.Hysteria2Node.Encode/Decode` 故意不读不写这三字段;它们只通过 `SubscriptionSpec.Extensions` 在我们自己的 mihomo client 那条订阅链路里传。

**Q: hy2 listener 为什么用 `users:{default:...}` 而不是顶层 password?**

A: 实证 mihomo Alpha `listener/inbound/hysteria2.go::Hysteria2Option` 字段:**没有顶层 `password`**,只有 `users: map[string]string`(username → password)。空 users map 会让 mihomo 不强制鉴权(危险),所以 Phase 5 至少写一条 entry。Phase 5 单用户固定用 `"default"` 作 username(见 `forwardNetworkForProtocol` 用户级 memory 决策),后续多用户走"inbound 用户追踪架构统一"延后事项时把多 user 灌进这个 map。

## 反例

### ❌ 把用户字段塞进 listener 配置

mihomo listener 里 `users` 数组是**共享凭据**位,所有用户同一把。不要试图把多用户塞进 listener 的 `users` 数组 —— 即便 mihomo 接受,你也绕开了 forward 层的流量统计和端口分配,违反 `docs/container-design-principles.md` 原则 3。

### ✅ 用户通过 forward 层接入

用户 Add → UserEvent → `handleUserEvent` → 对每个 inbound 调 `GetBindPort` → forward 分配用户端口 → 客户端连用户端口 → relay 到 `127.0.0.1:<inbound_port>` → mihomo listener 认共享凭据。

### ❌ 直接改 mihomo 的 config.yaml

`c.inbounds` map + store 是真值。Reload 走 `PutConfigs` 重推整份,任何手动编辑的 yaml 都会被下次 reconcileDrift 或 FastAddInbound 覆盖。

### ✅ 走 HTTP API 或 FastAddInbound

inbound CRUD 只走 `FastAddInbound` / `RemoveInboundConfig`(store + map + PUT /configs 三处同步);共享凭据变更走 `Reload`(重推整份 yaml,该 inbound 的 listener 重建、其他 inbound 不受影响)。

### ❌ 在测试里用 `AutoDownload=true` 又不给 Updater 能力

`AutoDownload=true` 但 `updater=nil`(没走 WithUpdater 注入也没走 NewMihomoContainer 的默认)→ startProcess 发现 binary 缺失时直接 fail。整合测试应该用 `ensureMihomoBin` + `AutoDownload=false`,binary 路径外部解析完再构造 container。

## 注意事项

⚠️ **stage 9 breaking**:`MihomoConfig.Version` 被重命名为 `MihomoConfig.ReleaseTag`,默认值从 `Prerelease-Alpha` 改为 `latest`。升级前 config 里的 `mihomo.version` 必须改为 `mihomo.release_tag`,且原用 Alpha 跑生产的部署要显式加 `release_tag: Prerelease-Alpha`。详情见 `CHANGELOG.md 2026-04-23` 段。

⚠️ **ExternalController 只绑 loopback**:`127.0.0.1:9090` 是默认,不要绑到 `0.0.0.0` —— mihomo 的 REST API 既能 CRUD listener 又能读 stats,暴露到公网等于把节点 root 权限交出去。Secret 只是第二道防线,不是主要防护。

⚠️ **listener 重建会瞬断该 inbound 所有连接**:`Reload()` 下的共享凭据 / 证书变化触发 mihomo 内部 Close+Listen。跨 inbound 不受影响,但该 inbound 当前连接的用户会瞬断重连。业务需要零中断的场景,换用新 tag 的 inbound(老 inbound 保留到连接稳定后 `RemoveInboundConfig`)。

⚠️ **reconcileUsers 锁在 inboundsMu 外**:FastAddInbound 抽出 `addInboundLocked` 把"mutate + push"留在 inboundsMu 内、`reconcileUsersForInbound` 在锁外跑。目的是避免 GetBindPort 阻塞 List/Get/handleUserEvent 的 inboundsMu 读。修改 FastAddInbound 路径时要保持这个边界。

⚠️ **runtime check 非 linux/amd64 直接 fail**:Updater 的 Step 1 之前就检查 `runtime.GOOS == "linux" && runtime.GOARCH == "amd64"`,否则在 fetch 阶段返错。跨平台支持是独立任务,不在 MVP 范围。
