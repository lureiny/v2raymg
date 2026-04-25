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

**Q: 进程 crash 后 v2raymg 会自动拉起吗?**

A: **不会**。production 没有 auto-restart watchdog。运维需通过 HTTP API 的 Restart 端点触发,底层走 `container.Stop()` + `container.Start()`,Restore 和 reconcileUsers 负责恢复状态。

**Q: 同一台机器可以跑多个 mihomo 容器实例吗?**

A: 可以,但 `ExternalController` 必须各不相同(`apiPort` 冲突会导致第二个实例 Start 失败)。`BinaryPath` 可以共享(只读 mmap);`ConfigFilePath` 和 `DataDir` 必须独立。

**Q: 升级成功但新 binary 立刻 crash 会发生什么?**

A: stage 10a 之前会误报 `Restarted=true`。stage 10a 后:`Updater.Update` 的 Step 7 在 `Start` 返回后跑 `WaitReady`(GET /version 探活,10s 超时),失败则走 `Stop + Rollback + ErrUpdateFailed{Stage:"restart"}`。rollback 也失败时两 cause 通过 `errors.Join` 合并,operator 需手动从 `<BinaryPath>.bak` 恢复。

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
