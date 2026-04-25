# Mihomo Container 实施计划

> **状态(2026-04-25)**:Stage 1-10 落地 + Phase 1-7 协议扩展全部完成。MVP 三协议(vmess/trojan/shadowsocks)+ VLESS / VMess+ / Trojan+ / Shadowsocks+ / Hysteria2 / TUIC / AnyTLS 已全部接入并系统测试覆盖。本文档保留作为**历史规划记录**;反映当前架构状态请优先看:
> - `docs/mihomo-container-design.md` § 协议与字段扩展(模式矩阵)
> - `wiki/knowledge/mihomo-container/` 三层 wiki 页
> - 用户级 memory `project_protocol_expansion_status.md`(各 Phase 进度与设计决策)
>
> 下面"扩展协议作为独立后续任务"等措辞反映的是规划阶段的初始决策,**当前已不再准确**(扩展协议已全部完成)。

## 目标

按 `docs/mihomo-container-design.md` 落地 mihomo container 到 `pkg/proxy/containers/mihomo/`,遵循 `docs/container-design-principles.md`(模式 B + per-user listener 变种)。

约束:

- 不动 `core/contracts` 结构(仅加 `ContainerMihomo` 常量)
- 不动 forward 层,不动 UserManager,不动 InboundStore
- 不动其他 container 代码
- 所有 mihomo 特有字段走 `InboundSpec.Extensions["mihomo"]`

## 总体策略

10 个阶段串行推进,每个阶段一个可独立 review 的 PR。阶段 1~3 出最小可运行骨架(空 listener 能启停);阶段 4~6 补齐业务能力(inbound/user/restore);阶段 7~9 补齐运维面(订阅/HTTP API/updater);阶段 10 做系统测试与规模摸底。

原则:

- 每阶段交付物必须能跑通一个窄场景(非仅编译通过)
- 先手工整包部署 mihomo 二进制,auto_download 放最后
- Alpha 分支锁一个 commit,开发期间不跟随上游
- 测试用例随着每个阶段增量补齐,不堆到最后

## 决策记录(2026-04-22 已定)

| # | 决策 | 最终值 |
|---|------|--------|
| D1 | 与 `docs/review-2026-04-09.md` 剩余 P0 的优先级 | **并行推进,mihomo 不阻塞 review** |
| D2 | mihomo Alpha 分支版本锁定 | **不锁定**。开发期直接读 Alpha HEAD 作为功能参考;运行时对齐 xray 模式 —— 未指定 release 时 `auto_download` 拉 latest。代码仓库只是参考上游"是否支持某功能",不作产物依赖 |
| D3 | MVP 期开发便利性 | 阶段 1~8 开发时**允许手工预装**二进制到 `binary_path` 加快迭代;阶段 9 交付后**生产默认走 auto_download + latest**(与 xray 一致) |
| D4 | 首批支持的协议范围(阶段 4 profilegen + 阶段 7 clash converter 覆盖) | **vmess / trojan / shadowsocks 三个**,最小骨架验证架构。vless / hysteria2 / tuic / anytls 作为独立后续任务扩展,不纳入 MVP |
| D5 | listener 数量膨胀阈值 | **阶段 10 实测再定**。超过阈值后评估是否切换到共享 listener + 重建模式(见 R1)|

## 进度与实施日志

| 阶段 | 状态 | 备注 |
|------|------|------|
| 阶段 1 | **DONE** 2026-04-22 | |
| 阶段 5 前置(ForwardManager 扩接口) | **DONE** 2026-04-22 | 原本在阶段 5 内部,因 mihomo 阶段 5 会立即用到而拆成独立 PR |
| 阶段 2 | **DONE** 2026-04-22 | auto-download 基础能力从阶段 9 提前到此 |
| Review 修复(阶段 1+2+前置) | **DONE** 2026-04-22 | |
| 阶段 3 | **DONE** 2026-04-22 | |
| 阶段 4 | **DONE** 2026-04-22 | 含两轮 review + 修复 |
| 阶段 5 | **DONE** 2026-04-22 | 包含周期 reconcile loop(原属阶段 6 的一半);剩余 Restore 入口 + Reload 语义留给阶段 6 |
| 阶段 6 | **DONE** 2026-04-23 | |
| 阶段 7 | **DONE** 2026-04-23 | |
| 阶段 8 | **DONE** 2026-04-23 | HTTP 层本就多态,改动集中在 RPC 层:getContainerByType 去 switch + ListInbound 改 Types() 迭代 |
| 阶段 9 | **DONE** 2026-04-23 | Updater 热替换 + SHA256(Alpha 有 checksums.txt)+ AutoDownload 字段 + Version 重命名 ReleaseTag 默认 latest |
| 阶段 10a(A2-3 post-Start liveness probe) | **DONE** 2026-04-23 | ProcessController 扩 WaitReady + Updater Step 7 接入;MihomoContainer 实现走 restClient /version 探活。原属阶段 9 review 遗留 P1,按计划前置到 stage 10 |
| 阶段 10b(功能集成测试) | **DONE** 2026-04-23 | 跳过规模测试(用户决策 D6);mihomo_protocol_matrix + mihomo_restore 两个 integration 测试,客户端复用 xray 作 vmess/trojan/ss outbound;MIHOMO_BIN 优先缺省走 Updater 下载 |
| 阶段 11(文档 + wiki) | **DONE** 2026-04-23 | CHANGELOG 2 条新条目、README 补 Supported Proxy Kernels 段、wiki 新建 mihomo-container 概念页并登记 _manifest.json |
| 阶段 11+(真实 E2E 系统测试) | **DONE** 2026-04-24 | TestMihomoE2E_RealInternet:mihomo 双开做 server+client,curl→client→server→google.com 三协议 × 5 步全绿;P2-9 CERTAIN 并落实 trojan cert_file/key_file 扩展 |
| 阶段 12(params 统一归一 + certmgr 联动) | **DONE** 2026-04-24 | 新建 `pkg/proxy/core/params/defaults.go`:凭据 fill + cert 4 源归一(cert_file/certificate/domain/self_signed);RPC FastAddInbound 接入;mihomo 侧 SAFE_PATHS env 接受 certmgr 路径 + CertScratchDir 写自签到 DataDir + CertSource 驱动 RemoveInboundConfig 清理。顺手修 mihomo factory 漏 wire UserManager 的生产 bug |
| 协议扩展 Phase 1(VLESS) | **DONE** 2026-04-25 | ProtocolParams 基础设施 + VLESS tcp/ws/grpc/xhttp/splithttp + tls/reality;9/10 系统测试通过(ws-reality skip) |
| 协议扩展 Phase 2(VMess) | **DONE** 2026-04-25 | VMess 高级特性迁移到 ProtocolParams;6/7 通过(ws-reality skip) |
| 协议扩展 Phase 3(Trojan) | **DONE** 2026-04-25 | Trojan tcp/ws/grpc + tls/reality 矩阵 + subscription chain cross-cutting test |
| 协议扩展 Phase 4(Shadowsocks) | **DONE** 2026-04-25 | SS 迁移 ProtocolParams,默认 SIP022 cipher,obfs/v2ray-plugin/shadow-tls 插件支持;含 12 条 P0-P3 review 修复 |

### 阶段 1(2026-04-22 完成)

**交付**:

- `pkg/proxy/core/contracts/protocol.go` 加 `ContainerMihomo` 常量 + `IsValid` switch 一行
- `pkg/proxy/containers/mihomo/` 新建:`config.go`(5 字段 MihomoConfig 骨架)/ `register.go`(Factory)/ `container.go`(嵌入 BaseContainer,接口占位实现)/ `container_test.go`(工厂加载验收)
- `config/config.example.yaml` 追加 mihomo 段,`enabled: false`

**实际变动**(相对阶段 1 计划原文):

- 构造函数 `NewMihomoContainer(cfg MihomoConfig)` 不带 options;阶段 4/5 需要 `WithStoreMgr/WithUserManager` 时再加
- 占位错误用 inline `fmt.Errorf("mihomo: ... not implemented")`;`Update` 用 `container.ErrNotSupported`(已有);未新建错误类型

**验证**:`go build ./...` + `go test ./pkg/proxy/containers/mihomo/... -race` 通过

### 阶段 5 前置(2026-04-22 完成)

**动机**:mihomo 阶段 5 的 per-user 127.0.0.1 listener 需要一个"纯分配端口、不创建转发链路"的接口。用户 A 决策确定用全局端口分配器而非 mihomo 内部独立池,这要求 `ForwardManager` 暴露该能力。属于阶段 5 的**架构前置**,独立为一次可 review 的小 PR。

**交付**:

- `pkg/proxy/forward/manager.go`:`ForwardManager` interface 新增 `AllocatePort() (uint32, error)` 和 `ReleasePort(port uint32)` 两个方法,Godoc **显式标注**"不创建转发链路"(用户明确约束,见 memory `feedback_forward_port_allocator_api.md`)
- `pkg/proxy/forward/forward_manager.go`:`DefaultForwardManager` 实现,`AllocatePort` 带 `closed` 检查,`ReleasePort` 幂等无锁
- `pkg/proxy/forward/forward_manager_test.go`:新增 `TestForwardManager_AllocatePort_Basic` / `_AfterClose` / `_SharesPoolWithAddRule` 三个测试

**顺带修复**(接口扩展引发):

- `pkg/proxy/containers/xray/exec_runner_test.go` 的 `mockForwardManagerForTest`:补 `AllocatePort`/`ReleasePort` 存根
- `pkg/proxy/usermanager/usermanager_test.go` 的 `mockForwardManager` 和 `mockStatsForwardManager`:同上

**验证**:`go test ./pkg/proxy/forward/... -race -count=1` 全绿(含新旧测试)

### 阶段 2(2026-04-22 完成)

**交付**:

- `pkg/proxy/containers/mihomo/config.go`:`MihomoConfig` 加 `Version` 字段 + 完整 `Decode` 校验(非空、`ExternalController` 须可解析为 `host:port`)
- `pkg/proxy/containers/mihomo/downloader.go` + `downloader_test.go`:GitHub API 查 release → 按前缀 `mihomo-linux-amd64-v1-alpha-` 且后缀 `.gz` 匹配 asset → 下载 → gunzip → chmod 0755 → atomic rename
- `pkg/proxy/containers/mihomo/rest_client.go` + `rest_client_test.go`:`RESTClient.GetVersion` + Bearer auth + 10s 超时 + 区分 401/5xx/超时
- `pkg/proxy/containers/mihomo/process.go`:`startProcess` / `stopProcess` / `generateConfigFile` / `waitForVersion`(指数退避 50ms→200ms cap)
- `pkg/proxy/containers/mihomo/container.go`:hooks 接真实 startProcess/stopProcess;新增 `mu sync.RWMutex` 保护 `runner`/`restClient`/`cachedVersion`
- `pkg/proxy/containers/mihomo/config_test.go`:Decode 默认值/覆盖/6 种校验失败
- `pkg/proxy/containers/mihomo/integration_test.go`:`//go:build integration`,真实 mihomo 下载 + 启停 + `/version` 探活
- `config/config.example.yaml`:mihomo 段补 `version: Prerelease-Alpha`

**实际变动**(相对阶段 2 计划原文):

- `MihomoConfig` 字段**最终为 6 个**(BinaryPath/ConfigFilePath/DataDir/ExternalController/Secret/Version),**没有** `InternalPortRange`(用户 A 决策走全局 port allocator,字段删除)或 `ReconcileInterval`(推到阶段 6 真正需要时加)
- **auto-download 从阶段 9 提前到阶段 2**:原计划 D3 说阶段 1~8 允许手工预装,但 Start 的完整语义必须包含"binary 不存在则下载"(hysteria 同构模式)。阶段 2 实现 GitHub API 查 release + 按 asset 前缀匹配 + gunzip;阶段 9 仍负责 SHA256 校验、`Update(ctx, req)` 热替换、OS/arch 检测、`latest` 自动解析等完整 updater 能力
- mihomo Alpha 资产默认选 `linux-amd64-v1-alpha-*.gz`(对应 `GOAMD64=v1`,最兼容)——不是 `linux-amd64-alpha-*.gz`(后者实际是 `GOAMD64=v3`,见 Makefile 查证,记在 memory `reference_mihomo_protocol_facts.md`)
- external-controller 和 secret **只写 config.yaml**,不重复走命令行 flag(mihomo 两种都支持,写一处避免源不一致)

**顺带修复**(非阶段 2 但被阻塞发现的既有 bug):

- `pkg/proxy/tools/github_release_client.go`:`FetchRelease` 对非 `"latest"` tag 构造的 URL 缺 `/tags/` 段,GitHub 返回 404。已修 + Godoc 说明。唯一既有调用者(xray updater)用 mock 测试,从未在真实 API 上触发

**验证**:
- `go build ./...` + `go test ./... -race -count=1` 全绿
- `go test -tags=integration ./pkg/proxy/containers/mihomo/...`:真实下载 `Prerelease-Alpha` → 启 mihomo → `/version` 返 `alpha-5a5e312` → 清停,通过

### Review 修复(2026-04-22 完成)

阶段 1+2+前置结束后用 general-purpose agent 做了一轮 code review,结果与处置:

| 发现 | 优先级 | 处置 | 说明 |
|------|-------|------|------|
| `Version()` 无锁读 `cachedVersion` | P1 | **已修** | `container.go` 加 `mu sync.RWMutex`;`Version()` RLock;`startProcess/stopProcess` 在字段写入点 Lock |
| `waitForVersion` 中 `time.After` 未 Stop 导致 timer 泄漏 | P1 | **已修** | 换 `time.NewTimer + Stop + drain`;为阶段 6 reconcile 循环铺好模板 |
| config 文件 mode `0644` 可能泄漏 Secret | P2 | **已修** | `generateConfigFile` 改 `0600` |
| downloader 硬编 linux-amd64 | P2 | **已修** | 公开入口 `downloadMihomo` 加 `runtime.GOOS/GOARCH` 检查,非 linux/amd64 早 fail;测试入口 `downloadMihomoWith` 不加(保持测试可移植) |
| 占位方法返 `fmt.Errorf` 而非 `container.ErrNotSupported` | P1→P2 | **驳回** | 阶段 4 就会替换为真实实现,届时不再返错。`errors.Is(ErrNotSupported)` 在这里没价值 |
| 集成测试没 watchdog | P1 | **驳回** | `go test -timeout` 已兜底 |
| `NewMihomoContainer` 永返 nil error | P2 | **驳回** | 保留 error return 为后续 options 留空间 |
| `findMihomoV1AssetURL` filter UNCERTAIN | P2 | **驳回** | 集成测试已实证(真实下载 `alpha-5a5e312`),资产命名已查证 Makefile;review agent 对第三方规范的误报 |

验证同阶段 2(全绿)。

### 阶段 3(2026-04-22 完成)

**前置核实**(R5):

- 查 mihomo Alpha `hub/executor/executor.go::ApplyConfig` + `listener/listener.go::PatchInboundListeners` + `hub/route/configs.go::configRouter` + `hub/route/errors.go::HTTPError`
- 结论:`PUT /configs?force=false` 内部总调 `PatchInboundListeners(map, tunnel, true)`,按 name 做 diff,同 name + Config.Equal 的 listener **不重建**,既有连接不断;force 只影响 HTTP/SOCKS/Mixed 等 top-level singleton port listener,命名 listener 不受 force 影响
- 错误 body schema 为 `{"message":"..."}`(HTTPError struct)
- R5 关闭,设计文档与 stage 4/5 策略假设完全属实
- 事实已记到 memory `reference_mihomo_protocol_facts.md`

**交付**:

- `pkg/proxy/containers/mihomo/rest_client.go`:抽 `do(ctx, method, url, op, body)` 私有辅助统一请求构造(Content-Type / Bearer / JSON marshal / 错误 body 解析);新增 `GetConfigs / PutConfigs / PatchConfigs / PostConfigsGeo`;新增 `parseErrorBody` 工具提取 `{"message":"..."}`;`GetVersion` 改走 `do`
- `pkg/proxy/containers/mihomo/rest_client_test.go`:保留阶段 2 的 5 个 GetVersion 测试;新增 13 个针对新方法的测试(成功路径 / 错误 body 解析 JSON 与 raw text / force=true|false query / payload body JSON encoding / PatchConfigs fields 序列化 / PostConfigsGeo 无 body / unauthorized / context 取消 / 空 secret 不发 Authorization header)

**实际变动**(相对阶段 3 计划原文):

- 不新增 typed `RESTError` struct,沿用 `fmt.Errorf("mihomo rest: %s: HTTP %d: %s", op, status, message)` 惯例(与 hysteria/snell/xray 已有 HTTP 错误风格一致,且无 caller 需要 `errors.As`)
- 错误 body 读取限制在 1024 字节(`maxErrorBodyBytes`),非 JSON 时 fallback 到 trimmed raw text(截 256 字节),防止恶意服务端 OOM
- `PutConfigs_ContextCanceled` 测试的 server handler 用 `select { case <-r.Context().Done(): case <-time.After(500ms): }` 防止测试 hang —— PUT 带 body 时 `r.Context().Done()` 不如 GET 那样立即随 client 取消触发(GetVersion 走同样写法正常,此处加 fallback 保险);client 侧 ctx 50ms 取消仍是实际被验证的行为
- 不写 integration test(阶段 2 已覆盖 `/version` 真实握手,阶段 3 新方法的真实触达在阶段 4 做完 inbound 增删后一并验证)

**验证**:

- `go build ./...` 通过
- `go test ./pkg/proxy/containers/mihomo/... -race -count=1`:22 个测试全绿(含 5 GetVersion / 13 新方法 / 3 config / 1 factory)
- `go test ./... -race -count=1`(全仓):通过

### 阶段 5(2026-04-22 完成)

**模型修正(开工前)**:原计划把 mihomo stage 5 的 user 集成参照 snell(单 inbound);用户指正应参照 xray(多 inbound),因为 mihomo 在"多 inbound × 多 user"这一骨架上与 xray 完全同构,真正的差异只在外部进程本身(动态接口 / 基础配置 / inbound 配置格式 / 下载地址)。切到 xray 模板后,`addedUsers` 挂 inbound 本身(而不是 container 的 nested map),container 层只做事件分发,锁粒度天然清晰。

**交付**:

- `pkg/proxy/containers/mihomo/inbound.go`:`MihomoInbound` 增 `userMgr` + `addedUsers` + `addedUsersMu`;`NewMihomoInbound` 初始化 addedUsers;新方法 `SetUserManager` / `hasAddedUser` / `listAddedUsers` / `markAddedUser` / `unmarkAddedUser` / `AddUser(email,user)→port` / `RemoveUser(email)` / `ReleaseAllUserPorts()`,全部照抄 `XrayInbound` 的模板(含 stale-tracking fast-path 和 `GetUserPortByDstForCleanup` 用法)
- `pkg/proxy/containers/mihomo/container.go`:新字段 `userMgr` / `userEventCh` / `reconcileStopCh` / `reconcileWg`;新 Option `WithUserManager`(构造期 `go forwardUserEvents(um.Subscribe())`,避免早期事件丢);新方法 `forwardUserEvents` / `startUserEventHandler` / `handleUserEvent` / `syncUserToInbound` / `removeUserFromInbounds` / `snapshotInbounds` / `reconcileUsers` / `reconcileUsersForInbound` / `startReconcileLoop` / `stopReconcileLoop`;`FastAddInbound` 拆出 `addInboundLocked` 把"mutate+push"留在 inboundsMu 内、reconcileUsersForInbound 放锁外;`RemoveInboundConfig` 在 store delete 前补 `inb.ReleaseAllUserPorts`;`restoreAndPushInbounds` 对每个 restored inbound 调 `SetUserManager`;`UserEventChannel` 返回 `c.userEventCh`
- `mihomoHooks.GetRunFunc`:run 顺序改为 `startUserEventHandler → startReconcileLoop → startProcess → restoreAndPushInbounds → reconcileUsers`;stop 顺序 `stopReconcileLoop → stopProcess`
- `pkg/proxy/containers/mihomo/inbound_test.go`:新增 7 个 user 相关测试(AddUser 分配+跟踪 / idempotent / stale-tracking 恢复 / RemoveUser 拆除 / never-added no-op / ReleaseAllUserPorts 按 tag / noUserManager 边界)+ 复用型 helper `newTestUserManager`
- `pkg/proxy/containers/mihomo/container_test.go`:新增 7 个事件/reconcile 测试(Add 事件 fan-out / Remove 事件 fan-out / Update 可见性翻转 / FastAddInbound 新建 inbound 时回填已有用户 / RemoveInboundConfig 按 tag 释放 / reconcileUsers 增补+清理 / reconcileUsers 覆盖 N×M)+ 复用型 helper `newTestContainerWithUserMgr`(多带一个 `forward.ForwardManager` 返回值给测试用来直查 rule 状态)

**关键决策**:

| # | 决策 | 选定 |
|---|------|------|
| E1 | reconcile loop 放 stage 5 还是 stage 6 | **stage 5**。函数本体和 loop 同 concern,xray 本来就是一起上的;stage 6 只剩 Restorable.Restore 入口 + Reload 语义(都轻) |
| — | addedUsers 挂哪里 | **挂 inbound**,不挂 container。匹配 xray;锁粒度天然分片 |
| — | reconcileInterval 做成 MihomoConfig 字段? | **不做**。hardcode `30*time.Second`,stage 6 如果需要再抽出 |
| — | FastAddInbound reconcile 在 inboundsMu 内还是外 | **锁外**。参 xray;避免 GetBindPort 阻塞 List/Get/handleUserEvent 的 inboundsMu 读 |
| — | RemoveInboundConfig ReleaseAllUserPorts 失败是否回滚 inbound 删除 | **不回滚**。log + 继续(inbound 已被 REST 拆掉,剩下的孤儿 forward rule 由 reconcile 扫) |
| — | Stop 时是否释放 forward 规则 | **不释放**。与 xray/snell/hysteria 一致;restore 会在下次 Start 重建 |

**小前提:ReleaseInboundPorts 的语义**:`UserManager.ReleaseInboundPorts(tag)` 只清 forward rules,**不清** `user.PortMappings`(intentional,见 usermanager.go 注释:ports 可能在其他 inbound 复用)。测试若要断言"释放后不可再查到端口映射",必须走 ForwardManager 的 `GetRule`,而不是 `UserManager.GetUserPortByDst` —— 后者仍会命中遗留的 PortMappings。相应测试 `TestMihomoInbound_ReleaseAllUserPorts_ReleasesByTag` 和 `TestRemoveInboundConfig_ReleasesAllUserPortsOnTag` 都按此写法。

**实际变动**(相对重新设计的 stage 5 计划):

- 没有引入 container 级 userStateMu/addedUsers nested map —— 照抄 xray 后,addedUsers 分散到各个 inbound 上,不需要 container 级锁
- `FastAddInbound` 抽出的 `addInboundLocked` 是相对原 stage 4 代码的小重构(defer unlock 改为显式 unlock 以便 reconcile 跑锁外);其他 CRUD 逻辑未变

**验证**:

- `go test ./pkg/proxy/containers/mihomo/... -race -count=1`:**80 个**测试全绿(stage 4 的 66 个 + stage 5 新增 14 个:7 个 inbound + 7 个 container)
- `go test ./... -race -count=1`(全仓):通过,无新回归

### 阶段 4 开工前提示

- `rest_client.go` 的 `PutConfigs(ctx, yamlPayload, false)` 为"全量下发 listeners 数组"提供单一入口;阶段 4 `FastAddInbound/RemoveInboundConfig` 的实现模式是:改 `c.inbounds` map → 序列化整份 yaml → `PutConfigs`
- listener name 规则:`<inbound_tag>__<username>`(阶段 5 per-user);阶段 4 `FastAddInbound` 每个 inbound 先只下发 listener 壳子(users=[])验证增删链路,user 填充在阶段 5
- mihomo yaml 骨架见 `process.go` 的 `mihomoInitialConfig` struct;阶段 4 需要扩展 `Listeners []any` 字段的结构化类型

### 阶段 6(2026-04-23 完成)

**交付**:

- `pkg/proxy/containers/mihomo/container.go`:
  - `Restore(ctx context.Context) error` —— `Restorable` 接口入口,`restoreAndPushInbounds + reconcileUsers` 组合,幂等;在 `ContainerMgr.StartAll` 路径下即使 Start hook 跑过一次,Restore 再跑一次也只是一次 REST PUT(name-keyed diff,无 listener 重建)+ 一次 user 对账(addedUsers fast-path skip)
  - `Reload()` 已实现为 `pushConfig()`,阶段 4 就位,阶段 6 补详尽 Godoc 说明证书/共享凭据热更语义
  - 新增 `reconcileDrift()`:先 `pushConfig()`(map → mihomo 漂移修正,name-keyed diff,未变更 listener 不重建)再 `reconcileUsers()`(user → forward 漂移修正);push 失败仅 log,不阻断 user 对账
  - `startReconcileLoop` 的 tick 回调从直接调 `reconcileUsers()` 改为 `reconcileDrift()`
- `pkg/proxy/containers/mihomo/container_test.go`:新增 7 个 stage 6 测试
  - `TestRestore_ReloadsStoreAndConvergesUsers` —— 完整 Restorable 流
  - `TestRestore_Idempotent` —— 双调 port 不变、inbounds map 不变、forward rule 不换
  - `TestRestore_ReturnsRestoreError` —— restore fail 则不走 reconcile(不会在失败 mihomo 上建 forward rule)
  - `TestRestore_NilStoreMgr` —— 无 store 场景 Restore 正常
  - `TestReconcileDrift_PushesConfigAndReconcilesUsers` —— 周期 tick 行为
  - `TestReconcileDrift_ContinuesAfterPushFailure` —— push 失败不阻断 user reconcile
  - `TestRestore_AfterStartHook_IsNoOp` —— StartAll 路径幂等契约

**实际变动**(相对阶段 6 计划原文):

- **不加 `MihomoConfig.ReconcileInterval`**:设计文档已标注"MVP 暂不抽,生产需调优再加",30s hardcode 与 xray/snell 一致
- **不做 store-vs-map 三向漂移主动检测**:运行时 FastAddInbound/RemoveInboundConfig 保证两者同步,重启窗口已被 `restoreAndPushInbounds` 覆盖;在 reconcile loop 里重复 store Load 是浪费
- **map-vs-mihomo 漂移通过"周期 pushConfig"被动对账**:mihomo 的 `PatchInboundListeners(dropOld=true)` 在 name-keyed diff 下,未变 listener 不重建 → 周期 PUT 是安全且开销低的
- **Restore 和 Start hook 幂等共存**:Start hook 内置 restore+reconcile 保证"Start 自包含"(单测可以只调 Start 不调 Restore);ContainerMgr.StartAll 路径的第二次 Restore 调用是设计允许的冗余,测试用 `TestRestore_AfterStartHook_IsNoOp` 锁定该契约

**验证**:

- `go test ./pkg/proxy/containers/mihomo/... -race -count=10`:通过(10 次轮询,65s 全绿)
- `go test ./... -race -count=1`:通过

### 阶段 7(2026-04-23 完成)

**交付**:

- `pkg/proxy/containers/mihomo/subscription.go`:
  - `GetUserSubscriptions(req)` —— 遍历 `c.snapshotInbounds()`,对每个有 forward 端口映射的 inbound 生成一条 `SubscriptionSpec`;无映射的 inbound 跳过;不支持的协议 log+skip
  - `buildSubscriptionSpec(inb, req, port)` —— per-inbound spec 构造,按协议分支映射共享凭据:vmess→UUID、trojan→Password、ss→Password+Cipher(cipher 为空则 `defaultSSMethod=aes-256-gcm`,匹配 clash converter 的兜底)
  - URI 生成复用 `core/subscription/codec` 的 `VMessNode.Encode / TrojanNode.Encode / ShadowsocksNode.Encode` —— 全部在 codec 层完成,本包零 URI 组装代码
  - 请求参数校验:空 username / 空 host → 早返回 error;`userMgr==nil` → 返 (nil, nil) 与 snell/hysteria 对齐
- `pkg/proxy/containers/mihomo/container.go`:删除阶段 4 的占位 `GetUserSubscriptions` 实现
- `pkg/proxy/containers/mihomo/subscription_test.go`:新增 12 个测试
  - 参数校验:`MissingUsername` / `MissingHost` / `NilUserMgr`
  - 协议正例:`VMess` / `Trojan` / `ShadowsocksExplicitCipher`(含 URI 字段 round-trip 断言)
  - 单元级 defense-in-depth:`BuildSubscriptionSpec_ShadowsocksDefaultCipher`(因为 profilegen 拒绝无 cipher 的 ss,实际流程不能走到 GetUserSubscriptions,但保留 fallback 是对未来协议字段变迁的韧性)
  - 聚合:`AggregatesMultipleInbounds`
  - 漂移场景:`SkipsInboundsWithoutMapping`(用户没 bind 到某 inbound 时静默跳过) / `EmptyContainer`(零 inbound 返 `[]`)
  - 错误分支:`BuildSubscriptionSpec_UnsupportedProtocolErrors` / `BuildSubscriptionSpec_EmptyCredentialErrors`(vmess/trojan/ss 各一)

**实际变动**(相对阶段 7 计划原文):

- **不扩展 clash converter**:scan `pkg/proxy/core/subscription/converter/clash.go` 发现 `convertVMess / convertTrojan / convertShadowsocks` 已覆盖 MVP 三协议所需的最小字段集合(ss 默认 cipher 一致、vmess 无 transport 场景简单、trojan plain TLS 默认)。D4 风险 R4 降为低
- **订阅层不处理 ExcludeProtocols**:xray 也没做,留给上层 HTTP handler 统一过滤,保持 container 职责单一
- **不写 integration 测试**:集成测试需要真实 mihomo 启动 + 真实客户端连接,这条链路在阶段 10(规模/协议矩阵)再统一覆盖;阶段 7 的价值在"spec 生成正确"而非"end-to-end 可连"

**验证**:

- `go build ./...` 通过
- `go test ./pkg/proxy/containers/mihomo/... -race -count=10`:80+12=92 个测试全绿
- `go test ./... -race -count=1`:全仓通过,无回归

### Stage 6+7 Review 修复(2026-04-23 完成)

用 general-purpose agent 做了 stage 6+7 code review,共 15 条发现。按 P0/P1/P2/P3 处置:

| 发现 | 优先级 | 处置 | 说明 |
|------|-------|------|------|
| `restoreAndPushInbounds` 失败时 map 已修改 | P1 | **文档化** | Restore Godoc 明确"失败时 map 反映 store 状态,reconcileDrift 会重推达成最终一致";改"两阶段 swap"会失去重启后即时可见性,不划算 |
| `snapshotInbounds` 陈旧指针竞态 | P1 | **文档化** | Restore Godoc 注明"设计用于相对静默窗口;热态下陈旧指针无 correctness 问题只有幂等开销" |
| Start retry 后 goroutine 泄漏 | P1 | **已修** | `MihomoContainer` 加 `userEventHandlerStarted bool`;`startUserEventHandler` 走该 guard;`startReconcileLoop` 改为 `reconcileStopCh != nil` 则跳过。Stop 的 `stopReconcileLoop` 末尾把 `reconcileStopCh` 置 nil,合法 Stop+Start 仍能拉起新 loop |
| reconcileUsers 对 deleting 用户重复 Remove | P1 | **驳回** | 幂等;Stage 5 既有行为,非 Stage 6 引入 |
| `Close()` 持锁跨 `Unsubscribe` | P2 | **已修** | 锁内捕获 `sub/stopCh` 引用后释放锁,外部调用 `Unsubscribe`。Godoc 同步 |
| `Close()` 并发 close channel panic | P2 | **已修** | `MihomoContainer.closeOnce sync.Once` 包装;同时发现并修 race:`forwardStopCh` 不能在 close 后 nil(forwardUserEvents goroutine 从 select 读取,并发 nil-write 会 race);现在只 close,不 nil |
| `reconcileDrift` Godoc 过度宣称"detect drift" | P2 | **已修** | 措辞改为"enforces our map as source of truth against mihomo";明确不做 probe,只重推 |
| subscription 测试 helper 绕过 FastAddInbound | P2 | **已修** | 删除 `wireAndAllocate` / `setUserManagerOnInbound`;改用 `c.FastAddInbound(tag, params)` 驱动(production shape),`SkipsInboundsWithoutMapping` 保留手工 inboundsMu 直接写以构造"未 reconcile"窗口 |
| trojan URI over plain TCP(UNCERTAIN) | P2 | **阶段 8 前核实** | 已核实 mihomo Alpha `TrojanOption.Certificate/PrivateKey` 都 omitempty,decode 能过;**未核实** runtime 是否真能以 plain TCP 启动、常见 trojan 客户端是否接受 plain URI。进入阶段 8 前需用真实客户端握手验证,必要时改用自签 cert / 移除 trojan MVP / URI 加 allowInsecure |
| subscription 静默跳过 inbound | P2 | **已修** | 跳过时 `log.Debugf` 一行 |
| `TestRestore_Idempotent` PUT 计数脆弱 | P2 | **已修** | 改为 `srv.bodyCount()-pushesAfterFirst == 1` 严格 delta 断言;加注释说明"依赖测试路径不启 reconcile loop,未来 refactor 若破坏此假设会显式 fail" |
| `TestReconcileDrift_ContinuesAfterPushFailure` 核心不变式不足 | P2 | **已修** | 加 `putsBeforeTick` 捕获 + delta ≥ 1 断言,锁定"push 被尝试了,只是失败" |
| `reconcileUsers` 与 snell 样式差异 | P3 | **驳回** | 有意为之 |
| buildSubscriptionSpec 总分配空 Extensions | P3 | **驳回** | 保持统一 |
| `defaultSSMethod` 常量缺跨文件链接 | P3 | **已修** | 注释指向 `core/subscription/converter/clash.go` convertShadowsocks |

**本轮代码改动**:

- `pkg/proxy/containers/mihomo/container.go`:idempotent guard / sync.Once / Godoc 改写 / Restore Godoc 补失败后置条件 + 并发语义
- `pkg/proxy/containers/mihomo/subscription.go`:debug log + `defaultSSMethod` 注释
- `pkg/proxy/containers/mihomo/container_test.go`:测试精度(P2-11/P2-12)
- `pkg/proxy/containers/mihomo/subscription_test.go`:helper 清理,改走 production shape

**验证**:

- `go vet ./pkg/proxy/containers/mihomo/...` 通过
- `go test ./pkg/proxy/containers/mihomo/... -race -count=3`:全绿
- `go test ./... -race -count=1` × 5:全仓 5/5 通过,无回归

**阶段 8 开工前必办**:按 P2-9 的 UNCERTAIN 标注,跑一次真实 trojan 客户端 ↔ 真实 mihomo 握手。建议在阶段 10 系统测试前完成,阶段 8 的 HTTP 层改动不依赖此结论。

### 阶段 9(2026-04-23 完成)

**开工前核实**(WebFetch `api.github.com/repos/MetaCubeX/mihomo/releases/latest` + `/tags/Prerelease-Alpha`):

- 正式 release(`v1.19.24`,prerelease=false)asset 命名 `mihomo-linux-amd64-v1-v1.19.24.gz`(精确 tag 嵌在文件名里);**无 `checksums.txt`** 文件;每个 asset 的 API response 附带 `digest` 字段但要改 `tools.GitHubReleaseClient` 解码才能用
- Alpha(prerelease=true)asset 命名 `mihomo-linux-amd64-v1-alpha-<hash>.gz`(含 git short hash);**有 `checksums.txt`**(SHA256)
- 两套 release 的 asset matching 规则天然要分开:Alpha 按前缀 + `.gz` 后缀匹配,正式按 `"mihomo-linux-amd64-v1-" + tag + ".gz"` 精确匹配(排除 `-go120-` / `-go123-` toolchain 变体)

**决策回顾**(用户确认):Q1 A(SHA256 有就做)/ Q2 A(维持 linux/amd64-only)/ Q3 A(加 AutoDownload 默认 true)/ Q4 B + 默认 `latest`(`/releases/latest`,**默认从 Alpha 切到 stable**)/ Q5 A(downloader.go 保留)/ Q6 A(重启后重读 `cachedVersion`)/ Q7 A(`Version` → `ReleaseTag` 顺手重命名)

**交付**:

- `pkg/proxy/containers/mihomo/config.go`:
  - `MihomoConfig.Version` 重命名 `ReleaseTag string`,默认值从 `"Prerelease-Alpha"` 改 `"latest"`(**默认行为从 Alpha 切到 stable**;Alpha 部署需显式 `release_tag: Prerelease-Alpha`)
  - 新增 `AutoDownload bool`,默认 `true`;`false` 时 Start 遇 missing binary 直接 fail、`Update()` 仍 `ErrNotSupported`
  - `validate()` 同步改名、Decode 读 `release_tag` / `auto_download` key
- `pkg/proxy/containers/mihomo/updater.go`(新建):
  - `Updater` + `UpdaterConfig{BinaryPath/Owner/Repo/DownloadDir}` + `NewUpdater`,仿 xray 的 `ReleaseClient / AssetDownloader / BinarySwapper / ProcessController` 四个 seam
  - `Update(ctx, req)` 7 步:runtime check → fetch → pick asset → download → verify(optional)→ gunzip → swap → restart,post-swap 失败走 `Rollback`
  - `pickLinuxAmd64V1Asset(release)`:`TagName == "Prerelease-Alpha"` → 前缀匹配 `mihomo-linux-amd64-v1-alpha-*.gz`;其他 → 精确 `mihomo-linux-amd64-v1-<tag>.gz`
  - `extractChecksumFor(path, assetName)`:解析 coreutils 格式 `<hex>  <name>` 的 checksums.txt
  - `gunzipToTemp(src, dir)`:gunzip → 临时文件 → chmod 0755(复用 downloader.go 的 `gunzipFile`)
  - `ErrUpdateFailed{Stage, Cause, Wrapped}` + `ErrRestartFailed` sentinel
- `pkg/proxy/containers/mihomo/container.go`:
  - 新字段 `updater *Updater`
  - 新 Option `WithUpdater(u *Updater)`(测试可注入 fake);`NewMihomoContainer` 在 BaseContainer 构造完后若 `cfg.AutoDownload` 且 `c.updater == nil` 则默认构造一个 `NewUpdater` + `SetProcessController(c)`
  - `Update(ctx, req)` 改为委托给 updater,成功且 `Restarted=true` 时调 `refreshCachedVersionAfterRestart` 重读 `/version` 刷新 `cachedVersion`(Q6);失败 / 无 updater 走原有 `ErrNotSupported` 语义
- `pkg/proxy/containers/mihomo/process.go`:
  - `startProcess` 的"binary not found"路径从直接调 `downloadMihomo` 改为调 `c.updater.Update(RestartPolicyNever)`,单一下载入口
  - `c.cfg.Version` 引用同步改 `c.cfg.ReleaseTag`
  - AutoDownload=false 或 updater==nil 时遇 missing binary 直接报错
- `pkg/proxy/containers/mihomo/downloader.go`:
  - 删除 dead production 入口 `downloadMihomo`(updater 已取代,且 `downloader_test.go` 只测 `downloadMihomoWith`)
  - 保留 `downloadMihomoWith` / `findMihomoV1AssetURL` / `mihomoV1AssetPrefix` / `gunzipFile`(gunzipFile 被 updater.gunzipToTemp 复用;其余保留是因为 downloader_test.go 还在跑,用户 Q5 决定不强行合并)
  - 更新 package doc 说明"production 入口在 updater.go,这里只留测试支撑"
- `pkg/proxy/containers/mihomo/config_test.go`:默认值改 `"latest"`、overrides 测 `"Prerelease-Alpha"` / `auto_download=false`、validation 改 `"release_tag"`
- `pkg/proxy/containers/mihomo/integration_test.go`:`Version` → `ReleaseTag`,补 `AutoDownload: true`
- `pkg/proxy/containers/mihomo/container_test.go`:`sed` 批量把 4 处 `Version:` 改 `ReleaseTag:`
- `pkg/proxy/containers/mihomo/updater_test.go`(新建,27 个测试):
  - `NewUpdater` 默认值 + validation
  - `Update` 全流程:DryRun / EmptyTag→latest / FetchFailure / Stable happy path / Alpha happy path (含 checksum 匹配) / Checksum mismatch / Checksum missing entry / Download failure / Missing asset (stable + alpha) / BadGzip extract fails / Swap failure / RestartPolicyNever 跳过 pc / NilProcessCtrl 跳过 pc / Stop 失败 rollback / Start 失败 rollback
  - `pickLinuxAmd64V1Asset` 4 个纯函数 subtests(Alpha 前缀 / Stable 精确 / Alpha 无匹配 / Stable 无匹配)
  - `extractChecksumFor` 3 个(含 `no entry for` 错误) + `gunzipToTemp` 2 个(成功 + bad gzip)
  - OS/arch guard 在非 linux/amd64 skip(保留形式供将来跨平台 CI 启用)
  - Container 层集成:AutoDownload 构造 updater / AutoDownload=false 不建 / WithUpdater 覆盖 / Update 无 updater 返 `ErrNotSupported` / Update 有 updater 转发结果
- `config/config.example.yaml`:mihomo 段 `version: Prerelease-Alpha` → `release_tag: latest` + 新增 `auto_download: true` + 两行注释说明"set to Prerelease-Alpha to track the permanent alpha pre-release"

**实际变动**(相对阶段 9 计划原文):

- **SHA256 只在 Alpha 生效**:原计划模糊写"sha256 校验",实际验证正式 release 没发 `checksums.txt`,只有 API response 的 `digest` 字段。走 `digest` 字段要改 `tools.GitHubReleaseClient` 的 JSON 解码 + 引入新字段;成本不值这次阶段的进度。策略:有 `checksums.txt` 就校验(Alpha 命中)、无就 `log.Warn` + 跳过(stable 命中)。Q1 A 的"有就做没就留空"得以落地。如果未来 mihomo stable 加了 checksums.txt,同一代码路径自动生效无需改动。改接 API digest 留作 follow-up
- **默认行为改变 = 运维需要关注**:`release_tag` 默认 `latest` → GitHub `/releases/latest` → 最新 stable(v1.19.x)。阶段 1~8 期间的部署如果依赖"默认拉 Alpha 跑新功能",必须升级 config 加 `release_tag: Prerelease-Alpha` 才能保持原行为。config.example.yaml 加了注释提醒;CHANGELOG / 升级 notes 待阶段 11 补
- **未做 OS/arch 动态检测**:按 Q2 A 决定,仍硬编 linux/amd64。runtime check 只在 Update 入口判一次,非 linux/amd64 直接返 `fetch` 阶段错误。跨平台 Updater 作为独立后续任务
- **downloader.go 的 `downloadMihomo` 删除了**:Q5 A 说"保留 downloader.go 不合并",但 `downloadMihomo` 是顶层 wrapper 且已无生产/测试引用,保留等于死代码。按 CLAUDE.md "certain that something is unused, you can delete it completely" 原则清理。余下函数(`downloadMihomoWith` / 常量 / `findMihomoV1AssetURL` / `gunzipFile`)保留:gunzipFile 有 updater 复用;其他支撑自己的测试
- **未做 `downloader_test.go` 合并到 `updater_test.go`**:与 Q5 A 决定一致。`TestFindMihomoV1AssetURL` 和 `TestDownloadMihomoWith_*` 功能上被 updater_test 覆盖(`TestPickLinuxAmd64V1Asset_*` + `TestUpdater_Update_Alpha_*`),但独立保留不伤害
- **cachedVersion 刷新是 best-effort**:`refreshCachedVersionAfterRestart` 拿 `restClient` 读 `/version`;restClient 为 nil(container 从未 Start)或 REST 错误时 `log.Warn` 保留旧值,不 panic、不把 cachedVersion 清空 —— 设计上 Update 已成功重启了进程,version 探测只是显示层;下次 Reload / reconcileDrift 会隐式重试

**验证**:

- `go build ./...` 通过
- `go test ./pkg/proxy/containers/mihomo/... -race -count=1` × 3 全绿(27 个 stage 9 新增 + stage 1-7 的全部 existing)
- `go test ./... -race -count=1` 全仓 32 包全绿,无回归
- 未跑 integration(真实下载 + swap + restart),阶段 10 系统测试时统一跑

### Stage 8+9 Review 修复(2026-04-23 完成)

用 3 个 general-purpose agent 并行审查 stage 8+9(agent 分工:RPC 多态 / Updater 核心 / Container 集成),共 33 条发现。其中一条在核实 mihomo checksums.txt 真实格式时(`curl -sL .../checksums.txt`)暴露的 bug 被追加为 **P0-2**。按 P0→P1→P2→P3 分 6 批处置:

**Batch 1(P0 + P1 功能类)**:

- **P0-1** `pkg/proxy/tools/checksum.go::VerifyChecksum`:改严格相等(TrimSpace + ToLower)。旧逻辑 `strings.Contains(real, expected) || ==` 让短/畸形 expected 被假过,是共享工具老 bug;stage 9 是首个把 SHA256 接到生产路径的 caller,必须修。`TestVerifyChecksum_RejectsSubstring` 锁定行为。
- **P0-2(核实后新增)** `extractChecksumFor`:`strip "./"` 前缀。核实真实 Alpha `checksums.txt` 发现行格式 `<hex>  ./mihomo-...gz` —— 文件名带 `./` 前缀,旧 `fields[last] == assetName` 永远不匹配 → Alpha SHA256 校验永远 "no entry for"。测试 `TestExtractChecksumFor` 改为使用真实 64-hex digest + `./` 前缀的 body,补 `TestExtractChecksumFor_NoDotSlashPrefix` 覆盖兜底。
- **P1-1** updater.go 的 `restartFailure` helper:rollback 失败时 `log.Errorf` 并通过 `errors.Join` 组合 Cause,避免"restart 失败 + rollback 也失败"时 caller 只看到第一个错。
- **P1-3** `ErrUpdateFailed.Unwrap() []error`:同时暴露 Cause 和 Wrapped,`errors.Is(err, ErrRestartFailed)` 和 `errors.Is(err, context.Canceled)` 都能命中。配测试 `TestUpdater_Unwrap_ExposesBothCauseAndWrapped`。
- **P1-4** Updater 加 `sync.Mutex` 串行化 `Update`,避免共享 `DownloadDir` 下 `checksums.txt` / .gz 并发碰撞。测试 `TestUpdater_Update_ConcurrentCallsSerialized`(8 并发 goroutine,`-race` 通过)。
- **P1-5** `NewMihomoContainer` 默认 `DownloadDir = cfg.DataDir`(原为 `filepath.Dir(cfg.BinaryPath)`,在 `/usr/local/bin` 为只读的 hardened host 上必失败;即便 writable 也污染系统目录)。
- **P1-8** 补 3 个 `refreshCachedVersionAfterRestart` 测试(Q6 核心路径原零覆盖):httptest + 真 RESTClient 验证重启后 cachedVersion 刷新 / restClient==nil no-op / REST 500 保留旧值不 blank。

**Batch 2(P1 文档)**:

- **P1-6** `docs/mihomo-container-design.md` config 示例 `version: Prerelease-Alpha` → `release_tag: latest` + `auto_download: true`,附迁移说明。
- **P1-7** `CHANGELOG.md` 加 2026-04-23 条目,明示 `Version`→`ReleaseTag` rename 是 BREAKING、默认值从 Alpha 切 stable 的运行时含义、`VerifyChecksum` 的安全语义修正。

**Batch 3(P2 docs/CLI 漂移)**:

- **A1-1** `cmd/cli/suggest.go::knownContainers` 加 `mihomo`,附注释提醒与 `contracts.ContainerType` 保持一致。
- **A1-2** `docs/http-api-reference.md:744,764` 补 mihomo 枚举。
- **A3-12** `config/config.example.yaml` 补 "Pre-stage-9 this field was named version" 注释。

**Batch 4(P2 updater 资源 + 测试)**:

- **A2-6** 并发 `checksums.txt` 碰撞:P1-4 mutex 已 serialize,无需独立命名,留现状。
- **A2-8** stable 的 "no checksums.txt" `log.Warn` → `log.Debugf`(stable 确实不发 checksums.txt,warn 会每次升级刷日志)。
- **A2-12** runtime guard 从 `runtime.GOOS/GOARCH` 直取改为 package-level `updaterRuntimeGOOS`/`updaterRuntimeGOARCH`,`TestUpdater_Update_NonLinuxAmd64Fails` 从 `t.Skip` 改为 t.Cleanup 覆写 + 断言 stage/cause,**linux/amd64 CI 也会跑**。
- **A2-13** 补测试:`TestUpdater_Update_ContextCancelledMidDownload`(ctx cancel → download stage + errors.Is(context.Canceled))、`TestPickLinuxAmd64V1Asset_AlphaAmbiguous`(多条 `-v1-alpha-` 匹配时锁定"取第一条"契约)、`TestPickLinuxAmd64V1Asset_AlphaRejectsGzSig`(`.gz.sig` 不误匹配)。

**Batch 5(P2 行为细节)**:

- **A1-3** `DeleteInboundByName` Godoc 明确和 FastAddInbound 的"空 container 语义"**有意不对称**:破坏性操作不得默认到 xray。
- **A1-4** `ListInbound` 按 `(container, name)` 排序,脚本 diff 稳定。测试 `TestListInbound_Sorted` 锁定。
- **A3-5** `refreshCachedVersionAfterRestart` 改用独立 `context.WithTimeout(Background, readinessTimeout)`,不继承 caller ctx。测试已覆盖 REST 失败保留旧值分支。
- **A3-6** Updater 入口 normalize 空 `RestartPolicy` → `Always`(显式化原隐式行为,避免将来默认值变更悄然改变语义)。
- **A3-7** `MihomoContainer.Update` 入口 `result.FromVersion = c.Version()`(原为空串,callers 无法拿到 before/after)。
- **A3-9** `downloadTimeout` 2min → 5min(Alpha 路径多一次 checksums.txt RT,2 分钟在慢链路吃紧)。

**Batch 6(P3 nit)**:

- **A1-5** 删 `getSnellContainer`(stage 8 `getContainerByType` 多态化后 0 调用方)。
- **A1-6** `getContainerByType` not-found 错误消息加 `resolved from` 段,empty/`hysteria2` 别名的实际查找能看到。
- **A2-11** `pickLinuxAmd64V1Asset` Alpha 单一匹配契约 —— Godoc 内联,`TestPickLinuxAmd64V1Asset_AlphaAmbiguous` 锁定。
- **A2-14** `alphaAssetPrefix` Godoc 扩成独立一段说明前缀语义(原指向 downloader.go)。
- **A2-15** `MkdirAll(DownloadDir)` 移到 `NewUpdater` 一次性做,`Update` 不再重复。
- **A2-16 驳回**:helper pattern 重构 brittle-if-validation-changes,当前无 validation 变更,test 函数 20 行即可读,不强行 DRY。
- **A3-8** `refreshCachedVersionAfterRestart` Godoc 补"restClient 读+cachedVersion 写跨锁区,Stop 并发 benign(Stop 保留 cachedVersion 语义)"。
- **A3-10 驳回**:`mihomoV1AssetPrefix` vs `alphaAssetPrefix` 重复已有意注释,留现状。

**驳回的其他发现**:

- **A2-7** `defer os.Remove(extractedPath)` 对已被 SwapAtomic rename 的路径会 ENOENT —— `os.Remove` 对 missing file 本来就是 silent no-op,benign。

**验证**:

- `go build ./...` 通过
- `go test ./... -race -count=1` 全仓 32 包全绿,无回归
- mihomo 包:原 27 + 新增 9 = 36 个 stage 9 updater 测试,包含本轮 P1-1 / P1-3 / P1-4 / P1-8 / A2-12 / A2-13 新增

**仍未关闭**:

- **A2-3 post-Start liveness probe**:swap + Start 成功但新 binary 立刻 crash 的场景当前会误报 success,P1 级但修复比较大(需要 Start 后再跑一次 `/version` 探活,并把失败加进 rollback 路径)。单独开任务跟踪,不放 stage 9 review 批次
- **A2-10 checksums.txt 格式**:核实后升级为 P0-2 已修。UNCERTAIN 关闭

### 阶段 8(2026-04-23 完成)

**审阅结论**:`pkg/http/` 下 handler(bound/fastAddInbound/inbound_list/rotate_*/sub/node_containers/user/profile)全部 Container-agnostic —— 走 `req.Container` 透传 + RPC 多态。唯二需要修的 non-polymorphic 点都在 RPC 层:

1. `pkg/rpc/server/end_node_inbound.go::getContainerByType` 原本是 `switch` 硬编 xray/snell/hysteria,mihomo 落 default 分支报 "unsupported container type"。
2. `pkg/rpc/server/end_node_inbound.go::ListInbound` 原本硬编 `[Xray, Snell, Hysteria]` slice,mihomo inbound 在 `GET /inbounds` 不可见。

`POST /inbound`(raw xray JSON)和 `GET /inbound`(xray 原生配置)结构性只支持 xray,mihomo 走 `/inbound/fast`,保留 xray-only 校验不动。

**交付**:

- `pkg/rpc/server/end_node_inbound.go::getContainerByType`:重写为"别名规范化 + ContainerMgr.Get 多态"
  - 剩余 switch 只做两件事:`""` → xray(向后兼容老 xray-only caller)、`"hysteria2"` → hysteria(别名)
  - 其余全走 `containerMgr.Get(ContainerType(s))`,未知/未注册类型统一返 "container %q not found or not enabled"
  - 和 `DeleteInboundByName` 已有的多态写法一致;未来加新 container 不再动这里
- `pkg/rpc/server/end_node_inbound.go::ListInbound`:硬编 slice 改为遍历 `s.containerMgr.Types()`
  - 语义上等同"先 ListContainer 再 per-container ListInbound";走 `Container` 接口 `ListInboundConfigs()`,不依赖具体类型
  - 与 `GetContainers`、`subscription.Manager.GetSubscription` 已有的多态迭代模式对齐
- `pkg/rpc/server/end_node_inbound.go::DeleteInboundByName` Godoc 更新:点出"polymorphic via ContainerMgr",移除"Supports xray, snell, and hysteria"的枚举
- `pkg/http/inbound_list_handler.go`:help 文本 + JSON 字段注释 + DELETE body 示例,"xray/snell/hysteria" → "xray/snell/hysteria/mihomo"
- `pkg/rpc/server/fastadd_params_test.go::captureContainer` 扩展:增 `kind contracts.ContainerType` + `inbounds []inbound.Inbound` 字段,`Type()` 在未设置时返 ContainerXray(向后兼容现有 xray 测试);`ListInboundConfigs()` 返 `c.inbounds`
- `pkg/rpc/server/end_node_inbound_test.go`:新增 7 个测试
  - `TestGetContainerByType`:表驱动 7 case(空串默认 xray / 显式 xray/snell/hysteria/mihomo / hysteria2 别名 / unknown 返 not-found)
  - `TestGetContainerByType_NotRegistered`:mihomo 未注册时返 not-found,不 panic
  - `TestListInbound_PolymorphicAcrossContainers`:xray+mihomo+snell 混注册,断言四条 inbound 都出现
  - `TestListInbound_Empty`:零 container 注册时返 empty list,无错
  - `TestFastAddInbound_MihomoRouted`:`ContainerType="mihomo"` 路由到 mihomo,xray 未被触发;params(uuid/ws_path/transport/port/protocol)透传完整
  - `TestFastAddInbound_MihomoNotRegistered`:mihomo 未注册时 `code=1020` + 错误 msg 含 "mihomo",xray 不吞请求

**实际变动**(相对阶段 8 计划原文):

- **`getContainerByType` 深度简化超出原计划**:原计划是"加 case mihomo 分支",实际用户建议下做了深度重构。Switch 从"类型分派"降级为"别名规范化",真正的容器查找走 Get 多态。对所有未来 container 免维护。风险:`""` → xray 的默认值改到 switch 规范化阶段统一兜底,行为不变;`"hysteria2"` 别名语义保留;unknown 错误信息从 "unsupported container type: foo" 改为 `container "foo" not found or not enabled`(更准确 —— 区分"根本不是合法类型"和"类型合法但这台节点没启用")
- **`ListInbound` 的 Types() 迭代不再依赖硬编 slice**:这是更隐蔽的兼容性缺陷 —— 老写法下即使未来新 container 正确 RegisterFactory 并在 config 里 enable,ListInbound 仍然看不到它。改多态后这个级联问题一并消除
- **Proto 注释漂移延后 stage 11**:`pkg/rpc/proto/rpc_server.proto` 里 line 169/222/238/296 的 "xray / snell / hysteria" 枚举没改(需 `make proto` 重生成 .pb.go,担心无关 diff),统一留给 stage 11 文档对齐
- **未跑 integration 测试**:HTTP↔RPC↔容器的跨层行为已被单测覆盖(mihomo 路由 + param 透传 + ListInbound polymorphic + 未注册报错);真实 mihomo 的 FastAdd / DeleteInbound / ListInbound HTTP 调用在 stage 10 系统测试阶段统一过

**验证**:

- `go build ./...` 通过
- `go test ./pkg/rpc/server/... -race -count=1` 全绿
- `go test ./... -race -count=1` 全仓 32 个包全绿,无回归

## 阶段 1:骨架与 Factory 注册

**目标**:container_mgr 能加载 mihomo,方法调用不 panic,编译通过。

**产出**:

- `contracts/protocol.go`:新增 `ContainerMihomo ContainerType = "mihomo"`,`IsValid()` switch 补一行
- `pkg/proxy/containers/mihomo/register.go`:`init()` 调 `RegisterFactory`
- `pkg/proxy/containers/mihomo/config.go`:`MihomoConfig` 字段定义 + `Decode(map) error` 骨架
- `pkg/proxy/containers/mihomo/container.go`:嵌入 `*BaseContainer`,实现 `Container` 接口所有方法(多数返回 `nil` 或 `errs.ErrNotImplemented`)
- `config/config.example.yaml`:新增 `containers.containers[type=mihomo]` 示例段,默认 `enabled: false`

**验收**:

- `go build ./...` 通过
- 单元测试:ContainerMgr 加载 mihomo(enabled=true)不返回错误,`container.Type()` 返回 `mihomo`

## 阶段 2:进程生命周期

**目标**:Start 能拉起真实 mihomo 二进制,GET /version 能返回版本号。

**产出**:

- `config.go`:完整 `MihomoConfig.Decode`,含 `BinaryPath / ConfigFilePath / DataDir / ExternalController / Secret / InternalPortRange / ReconcileInterval`
- `process.go`:构造命令行参数 `-d <data_dir> -f <config_file>`,组合 `process.Runner`
- `container.go`:生成初始 mihomo `config.yaml`(listeners=[], rules=[MATCH,DIRECT], external-controller 注入)
- `rest_client.go` 骨架:仅 `GetVersion(ctx)`,Secret header 支持
- `Start()`:生成初始 config → Runner.Start() → 轮询 `GET /version`(最多 10s 超时)→ 标记 running
- `Stop()`:Runner.Stop()(SIGINT 5s + SIGKILL)

**验收**:

- 手工预装一个 mihomo Alpha 二进制
- 单元测试 mock process + http,验证启动流程状态机
- 集成测试(本地):真实二进制启停两次无 goroutine 泄漏,port 释放干净

## 阶段 3:REST 客户端完整化

**目标**:能对 mihomo 下发完整的 config 增删改,为阶段 4 铺路。

**产出**:

- `rest_client.go` 完整方法:`GetConfigs / PutConfigs(yaml, force) / PatchConfigs(fields) / PostConfigsGeo`
- 所有方法统一:context 超时、HTTP 4xx/5xx 转 typed error、secret 鉴权、body marshal
- `rest_client_test.go`:用 `httptest.Server` mock mihomo,覆盖成功路径 + 鉴权失败 + 超时

**验收**:

- 所有 REST 方法单测覆盖成功 + 典型失败路径
- 集成测试:真实 mihomo 上执行 GetConfigs → PutConfigs(修改 log-level)→ GetConfigs 验证生效

## 阶段 4:Inbound 生命周期(共享凭据 listener)

> **模型修正**:原计划写的是 per-user listener(每用户独立 listener);2026-04-22 对照 `docs/container-design-principles.md` 原则 3 发现违反"业务用户路径只走 forward 层"的硬要求,设计切换为**每 inbound 一条 listener + 一把共享凭据**,与 xray / snell 同构。相关设计文档 `docs/mihomo-container-design.md` 已同步重写。

**目标**:`FastAddInbound` / `RemoveInboundConfig` / `GetInboundConfig` / `ListInboundConfigs` 可用。每个 inbound 对应一条 mihomo listener,持**一把共享凭据**(vmess uuid / trojan password / ss password+cipher),所有用户共用。用户不参与 listener 配置(stage 5 通过 forward 层接入)。

**产出**:

- `inbound.go`:`MihomoInbound` 嵌 `inbound.DefaultInbound`,持共享凭据 + TLS/transport 与协议特有字段
- `adapter.go`:`params map → MihomoInbound`,按协议验必填(vmess 要 uuid;trojan 要 password;ss 要 password + cipher);未知协议返 `errs.ErrProtocolNotSupported`
- `profilegen.go`:`MihomoInbound → map[string]any` mihomo listener yaml entry;key 用 mihomo `inbound:"..."` tag 字面值(`ws-path` / `alterId` 等)绕过 KeyReplacer
  - MVP 协议:vmess / trojan / shadowsocks(D4)
- `config_builder.go`:`BuildConfig(initial, []*MihomoInbound) ([]byte, error)`,把 `listeners:` 数组追加到 base yaml
- `container.go`:
  - `inbounds map[tag]*MihomoInbound` + `inboundsMu sync.RWMutex`
  - `storeMgr *store.StoreManager`(通过 `WithStoreMgr` option)
  - `FastAddInbound(tag, params)`:adapter → lock → 拒重复 tag → `InboundStore.Save` → 入 map → rebuild yaml → 若容器运行中调 `PutConfigs(yaml, false)`
  - `RemoveInboundConfig(tag)`:lock → 从 map 摘 → rebuild yaml → 若运行中 `PutConfigs` → `InboundStore.Delete` → 预留钩子用于 stage 5 回收该 inbound 下所有用户 forward rule
  - `GetInboundConfig(tag)` / `ListInboundConfigs()`:查 map
  - Start hook:process ready 后从 InboundStore 加载 → rebuild yaml → 一次性 `PutConfigs(yaml, false)`(未启动时 FastAddInbound 只落 store+map,不调 REST,由 Start 统一推)
- `InboundStore`:复用 `store.InboundRecord`,`ContainerType="mihomo"`,`NativeJSON` 存 MihomoInbound JSON(含共享凭据)

**验收**:

- 单元:profilegen 对 vmess/trojan/ss 各一个 golden map 断言(`map[string]any` 深比较,避免 yaml 顺序);adapter 错误路径(缺必填、未知协议、port 范围);config_builder 空 inbound / 多 inbound 拼装正确
- container 单元:mock REST server 捕获 `PUT /configs` body,断言 listeners 数组(listener name = inbound tag、共享凭据、port、listen 127.0.0.1);mock store 验持久化
- 集成(可选,推迟):GET /configs 不返 listeners 数组,集成验证留到 stage 5 有 forward rule 时用客户端连端口验证
- **不做项**:用户事件订阅 + forward rule(stage 5);周期 reconcile(stage 6);TLS 证书热更路径(stage 6 Reload)

## 阶段 5:User Event Handler + forward 集成

> **模型修正**:原计划依赖内部端口池 + per-user listener 生成,已随 stage 4 模型调整一并废弃。用户对每个 mihomo inbound 走 forward 层,listener 完全不动。

**目标**:UserEventAdd/Remove/Update 全链路打通。对每 (user, mihomo inbound) 分配一个公网端口,走 forward 层 relay 到 `127.0.0.1:<inbound-port>`。**listener 配置不动**。

**产出**:

- `container.go`:
  - `userEventCh chan usermanager.UserEvent` + `UserEventChannel() <-chan`
  - `forwardUserEvents` + `startUserEventHandler`:抄 `pkg/proxy/containers/snell/container.go:356-377`
  - `handleUserEvent`:
    - `Add`:遍历 `c.inbounds`,对每个 inbound 调 `userMgr.GetBindPort({ContainerType: mihomo, InboundTag: inb.Tag, TargetPort: inb.Port, Protocol: inb.Protocol})`
    - `Remove`:遍历 `c.inbounds`,对每个 inbound 调 `GetUserPortByDstForCleanup(user, inb.Port)` → `ReleaseBindPort`
    - `Update`:按 `IsUserVisible` 走 Add/Remove 分支
  - `addedUsers map[username]map[inboundTag]struct{}` + mutex
  - `releaseAllForwardRules(tag)`:给 stage 4 `RemoveInboundConfig` 调,拆掉该 inbound 下所有用户端口
  - 初始 reconcile(Start 钩子补步骤 3-4):process ready + stage 4 PUT /configs 完成后,为每 (可见用户, inbound) 幂等 GetBindPort

**验收**:

- 单元:mock UserManager,验 Add/Remove/Update 事件触发的 GetBindPort/Release 调用序列 + addedUsers 状态机
- 集成(真实 mihomo):FastAddInbound(vmess, 共享 uuid) → AddUser → 用该 uuid 从 v2ray 客户端连用户公网端口握手成功 → RemoveUser → 端口不通 → RemoveInboundConfig → 所有该 inbound 下用户端口被回收
- 并发:`go test -race`,同一用户并发 Add/Remove 幂等

## 阶段 6:Restore + Reload 语义完善

> **范围收缩**:原阶段 6 的"周期性对账"已在阶段 5 落地(见 `startReconcileLoop` / `reconcileUsers`)。阶段 6 聚焦 `Restorable.Restore` 入口 + `Reload` 热更新语义,以及周期对账中**listener/store 侧**漂移的补齐(forward 侧漂移已被阶段 5 reconcileUsers 覆盖)。

**目标**:进程重启或外部状态漂移后,listeners / forward 规则都能收敛到 store + UserManager 的真值。

**产出**:

- `container.go`:实现 `Restorable.Restore(ctx)` —— 复用阶段 5 的 `restoreAndPushInbounds + reconcileUsers` 组合,暴露为 Restorable 接口入口
- 阶段 5 的 `reconcileUsers` 扩展:加入 store vs map vs mihomo 三向漂移检测 —— 当前只做 UserManager ↔ inbound.addedUsers 对账,需要补"inbound 在 store 但不在 c.inbounds"(restart loop)和"listener 在 mihomo 但不在 c.inbounds"(外部干预)的检测
- `Reload()` 实现:不重启进程,重拉整份 listener 配置 `PUT /configs` —— 证书/共享凭据变化后该 inbound 的 listener Close+Listen(该 inbound 既有连接断,跨 inbound 不受影响)
- 可能需要的 MihomoConfig 新字段:`ReconcileInterval`(当前 hardcode 30s,若生产需要调优再抽)

**验收**:

- 集成测试:启动 → Add N 个 inbound + M 个用户 → kill mihomo 进程 → 进程再启 → listeners/forward 完整恢复
- 故意删 mihomo 侧某个 listener 后等 reconcileInterval → 自动补回
- Reload 触发后进程未重启(pid 不变),但 listeners 内容更新

## 阶段 7:订阅生成

**目标**:`GetUserSubscriptions` 返回正确的 Clash YAML,现有 `/sub` HTTP API 无须改动即可服务 mihomo 用户。

**产出**:

- `subscription.go`:`GetUserSubscriptions(req)` 实现
  - 遍历该用户在 mihomo container 下所有 inbound → 展开为 proxies 条目
  - server=节点 proxy_host,port=forward 分配的公网端口,uuid/password 从 UserSpec 取
  - 复用 `core/subscription/converter/clash`
- 覆盖范围(D4):vmess / trojan / shadowsocks 三个。clash converter 预期已覆盖,仅做核对不补写

**验收**:

- 单元测试:固定 inbound+user 输入,比对 YAML 输出字节级一致
- 端到端:Clash Verge / mihomo Party / FlClash 等客户端用生成的订阅连接,golden path 可通

## 阶段 8:HTTP API 对齐

**目标**:现有 HTTP API 能对 mihomo container 做 CRUD,上层代码无须为 mihomo 分支。

**产出**:

- 审阅 `pkg/http/` handler(`bound/user/sub/rotate/node` 等),确认是否依赖具体 `ContainerType`
- 对 `mihomo` 需要特殊分支的位置补逻辑(预期极少,多数走 Container 接口多态)
- 若有 handler 基于 `contracts.Protocol` switch 的,补 mihomo 特有协议或确认走 Extensions 路径

**验收**:

- 现有 `http-api-reference.md` 列出的 API 对 mihomo container 全部可用
- 无新 handler,仅现有 handler 兼容性修复

## 阶段 9:Updater 与自动下载

**目标**:生产默认路径走 auto_download + latest(与 xray 一致);`Update()` 可热替换。

**产出**:

- `updater.go`:复用 `pkg/proxy/tools/{downloader,checksum,binary_swapper,github_release_client}`
- URL 模板:MetaCubeX/mihomo GitHub Release(Alpha pre-release),asset_name 按 OS/ARCH 匹配
- 未指定 `Release.Version` 时查 GitHub latest pre-release tag(Alpha 分支走 pre-release,不走 stable)
- sha256 校验,原子替换(运行中先停进程,替换,再起)
- `Update(ctx, req)` 实现:按 RestartPolicy 决定是否重启
- `AutoDownload=true` 时 Start 前触发

**验收**:

- 从 GitHub 真实下载最新 Alpha 二进制 → 校验通过 → 替换 → 重启 → 新版本号可 GET /version 读出
- 下载失败不破坏现有二进制(原子替换)

## 阶段 10:测试与规模摸底

**目标**:所有前置阶段的测试补齐;规模测试给出 listener 数量阈值;记录到设计文档。

**产出**:

- 系统测试:`pkg/proxy/systemtest/` 新增 mihomo 测试目录,覆盖协议矩阵
- 规模测试:单节点 {100, 500, 1000, 5000} 用户 × {1, 3, 5} 协议,测 `PUT /configs` 延迟、mihomo 启动时 yaml 加载时长、内存占用
- 对 D5 给出实测结论:选 per-user 还是混合模型
- 结果写回 `docs/mihomo-container-design.md` 的"风险与未决"一节

**验收**:

- `go test ./pkg/proxy/containers/mihomo/... -race -count=1` 全绿
- 系统测试(需真实 mihomo):`go test ./pkg/proxy/systemtest -tags=integration -run Mihomo` 通过
- 规模测试报告作为附录

## 阶段 11:文档与 wiki(小尾巴)

**目标**:历史文档与新能力对齐,wiki 登记 mihomo 概念。

**产出**:

- `CHANGELOG.md`:新增条目
- `README.md`:如果对外列了"支持的代理内核",补 mihomo
- `wiki/knowledge/mihomo-container/`(走 `write-wiki-page` skill):概念页,登记在 `_manifest.json`
- `docs/http-api-reference.md`:若 API 行为有差异,补说明

**验收**:文档与代码一致;wiki 能被 `read-wiki-page` skill 路由到

## 依赖关系

```
阶段 1 (骨架)
   │
   ▼
阶段 2 (进程) ──► 阶段 3 (REST client)
   │                    │
   ▼                    ▼
阶段 4 (inbound 元数据) ─► 阶段 5 (user event / per-user listener)
                              │
                              ▼
                        阶段 6 (restore / reconcile)
                              │
                     ┌────────┼────────┐
                     ▼        ▼        ▼
              阶段 7      阶段 8     阶段 9
              (订阅)     (HTTP API) (updater)
                     └────────┼────────┘
                              ▼
                       阶段 10 (测试)
                              │
                              ▼
                       阶段 11 (文档)
```

阶段 7/8/9 可以并行(由 review 容量决定);其他阶段串行。

## 风险登记

| # | 风险 | 触发阶段 | 缓解 |
|---|------|---------|-------|
| ~~R1~~ | ~~listener 数量膨胀导致 mihomo yaml 加载慢~~ | — | **已消除**:共享凭据模型下 listener 数量 = inbound 数量(典型 1~10),与用户规模解耦 |
| R2 | mihomo Alpha schema / REST 行为漂移 | 全程 | D2 决定不锁版本:开发期读 HEAD 做功能参考;rest_client 层探测 mihomo 版本 + 关键字段,不兼容时报清晰错误而非默默降级 |
| R3 | 证书或共享凭据热更时 listener 重建 | 阶段 6 `Reload()` | 该 inbound 上现有连接会随 Close+Listen 断开;跨 inbound 不受影响。可接受(部分用户瞬时重连);若未来要求零中断需另行设计 |
| R4 | clash converter 对 MVP 三协议(vmess/trojan/ss)输出不完整 | 阶段 7 | 阶段 7 先扫描,基本不会缺。hysteria2/tuic/anytls/vless 等扩展协议阶段由对应任务自带 converter 补写 |
| R5 | Alpha 分支 REST API 行为变化(例如 PUT force 语义) | 阶段 3 | **已核实**(2026-04-22):`PUT force=false` 内部总走 `PatchInboundListeners(map, tunnel, dropOld=true)` 做 name-keyed diff,未变 listener 不重建。详见 memory `reference_mihomo_protocol_facts.md` |
| ~~R6~~ | ~~per-user internal_port 池耗尽~~ | — | **已消除**:共享凭据模型下无内部端口池,inbound 的 listen port 由调用方通过 FastAddInbound params 指定 |

## 待解问题(MVP 完成后再评估)

- **扩展协议任务**:vless / hysteria2 / tuic / anytls 各作为独立后续任务,复用 MVP 架构,增量补 profilegen + clash converter + 测试
- 是否把 TUIC/anytls 提为 `contracts.Protocol` 一等公民
- 跨节点用户编排(`docs/user-placement-controller-design.md`)如何识别 mihomo container 的可用容量
- `docs/inbound-user-tracker-refactor.md` 延后项落地时,mihomo 如何对齐(共享凭据模型下 `addedUsers` 结构接近 snell,大概率可直接套用统一方案)
- mihomo container 启用 hysteria2 后,是否替代/合并现有 `containers/hysteria/`(单 inbound 专用容器)

### 阶段 10a(2026-04-23 完成)

**动机**:原 Stage 8+9 Review 遗留的 A2-3 post-Start liveness probe 问题。`updater.Update` 的 Step 7 仅靠 `processCtrl.Start()` 返回值判成败;Start 返回 nil 仅证明 fork+exec 成功,新 binary 立刻 crash(GOAMD64 不兼容、缺动态依赖、config 解析错等)时 updater 仍返回 `Restarted=true`,旧 binary 已被 swap 走。用户 D5 决策在开工前将其前置为 stage 10a,避免阶段 10b 的真实二进制替换场景把该 gap 放大成事故。

**交付**:

- `pkg/proxy/containers/mihomo/updater.go`:
  - `ProcessController` 接口新增 `WaitReady(ctx context.Context) error`
  - `UpdaterConfig` 新增 `ReadinessTimeout time.Duration`,`NewUpdater` 默认 10s(与 `process.go::readinessTimeout` 对齐),`Update` 入口兜底 `<=0 → 10s`(沿用 stage 9 Batch 5 A3-6 "显式化默认值"先例,防止直接 assign `cfg` 的 test/caller 撞上零超时)
  - Step 7 流程扩为:`Stop(if running) → Start → WaitReady(bounded by ReadinessTimeout) → 失败走 Stop + restartFailure(joined err)`。任何 WaitReady 失败都被分类为 `restart` stage,与现有 Stop/Start 失败同构;rollback 路径通过既有的 `restartFailure` 复用
  - WaitReady 失败 + 清理 Stop 失败时两者通过 `errors.Join` 合并,caller 可通过 `errors.Is` 同时命中
- `pkg/proxy/containers/mihomo/container.go`:
  - `MihomoContainer.WaitReady(ctx)` 实现:RLock 拿 `restClient` 快照 → `GetVersion(ctx)` → 成功返 nil,失败返 wrap error
  - 不与 `refreshCachedVersionAfterRestart` 合并:WaitReady 是通用 ProcessController 接口,语义是"纯探活、返错就中止";`refreshCachedVersionAfterRestart` 是 container-private 的 best-effort 刷缓存(失败只 warn),职责分离
- `pkg/proxy/containers/mihomo/updater_test.go`:
  - `fakeProcessCtrl` 新增 `readyErr` 字段 + `readyN` 计数 + `WaitReady` 方法(默认遵循 `ctx.Err()`,使 timeout 测试无需设 readyErr 即可生效)
  - 新增 3 个测试:
    - `TestUpdater_Update_StartSucceededButNotReady_Rollback` —— A2-3 主线,Start 成功但 WaitReady 失败 → Stop(pre+post)+ Rollback + `ErrUpdateFailed{Stage:"restart"}` Wraps `ErrRestartFailed` + `errors.Is(readyErr)` 仍可达
    - `TestUpdater_Update_ReadinessFailure_StopAlsoFails` —— 双失败路径,WaitReady + 清理 Stop 都失败,两 cause 通过 `errors.Join` 同时可达
    - `TestUpdater_Update_NotReady_RestartPolicyNever_SkipsProbe` —— 不变式测试,RestartPolicyNever 路径下 Start/Stop/WaitReady 都不应被调用
- `pkg/proxy/containers/mihomo/container_test.go`:新增 3 个 MihomoContainer.WaitReady 单元测试:`_Success`(httptest /version 200)/ `_NilRestClient`(未 attach → err,驱动 Updater rollback)/ `_CtxCancelled`(ctx 已 cancel → errors.Is(context.Canceled))

**实际变动**(相对 stage 10a 方案):

- **未改 `refreshCachedVersionAfterRestart`**:WaitReady 探活成功后 `refreshCachedVersionAfterRestart` 再探一次 `/version`,快速路径下一次额外 RT <50ms,代码清晰度 > 合并开销。刻意不优化
- **ReadinessTimeout 双防御**:NewUpdater 里设默认 10s + Update 入口 `<=0` 兜底 10s。单独哪一处都能让 prod 正确,合起来还覆盖 hand-built `UpdaterConfig` 的 test harness。删了 NewUpdater 的默认会让测试用默认 0 → 立即 cancel;删了 Update 的兜底会让任何绕过 NewUpdater 的 caller 撞零超时。保留两处
- **未扩 xray 的 ProcessController**:xray 的 Updater 也有同名接口(独立副本,不共享)但 xray 有自己的 Start→Ping 流程(`exec_runner.go::Start` 内置 gRPC probe),post-Start crash 不是 xray 的已知 gap。stage 10a 只对 mihomo 收口。xray 若未来被证明也有此 gap,可独立成任务

**验证**:

- `go build ./...` 通过
- `go vet ./pkg/proxy/containers/mihomo/...` 通过
- `go test ./pkg/proxy/containers/mihomo/... -race -count=1`:36+6 = **42 个** updater/container 测试全绿(stage 9 的 36 + 本轮 6)
- `go test ./... -race -count=1` 全仓 32 包全绿,无回归

### 阶段 10b(2026-04-23 完成)

**决策**(开工前用户确认):

| # | 决策点 | 选定 |
|---|-------|------|
| D6 | 规模测试范围 | **跳过**。只做协议矩阵 + restore 功能集成;规模基线留独立任务 |
| D7 | mihomo 二进制来源 | **MIHOMO_BIN 优先,缺省走 Updater**。MIHOMO_BIN 设置但文件不存在 → fail fast 不降级 |
| D8 | 客户端选型 | **抄 xray systemtest 的客户端代码**,xray 作 vmess/trojan/ss 的 outbound client,零新依赖 |

**交付**(全部在 `pkg/proxy/systemtest/`,build tag `integration`):

- `mihomo_helpers_test.go`:
  - `ensureMihomoBin(t, tmpDir) string` —— MIHOMO_BIN 优先解析,否则 `mihomo.NewUpdater` + `Update(Prerelease-Alpha, RestartPolicyNever)` 下载到 tmpDir
  - `mihomoTestRig` struct:`{container, userMgr, forwardMgr, apiAddr}`,生命周期挂 `t.Cleanup`
  - `startMihomoContainer(t, tmpDir, binaryPath)`:构造 `forward.NewDefaultForwardManager(MinPort:40000, MaxPort:50000)` + `usermanager.NewUserManager` + `store.NewStoreManager` + `mihomo.NewMihomoContainer(WithStoreMgr, WithUserManager)`,调 `c.Start()` 走真实启动链路
  - `addMihomoUserAndWaitForPort(rig, username, inboundPort)`:走真实 `AddUser(AddUserRequest)` 触发 UserEventAdd → container.handleUserEvent → GetBindPort;轮询 `GetUserPortByDst` 最多 5s 拿到 forward 端口
  - `removeMihomoUserAndWaitForPortRelease(rig, username, inboundTag)`:走真实 `RemoveUser` + 轮询 `forwardMgr.GetRule` 直到 nil

- `mihomo_protocol_matrix_test.go`(`TestMihomoProtocolMatrix`):
  - MVP 三协议 subtest(`vmess` / `trojan` / `shadowsocks`),共用同一个 MihomoContainer
  - 每个 subtest:FastAddInbound → AddUser → xray client(SOCKS5 in → 协议 outbound → 127.0.0.1:forwardPort)→ HTTP GET via SOCKS5 → 验证 marker;然后 RemoveUser + 访问 → 必须失败(负控,排除 false positive)
  - **trojan plain TCP** 在这里顺带验证 P2-9(无 TLS/cert);若失败需要把 trojan case 降级到 "必须带 TLS"。代码里 inline 注释标注了 fallback 方向
  - shadowsocks cipher 固定 `aes-256-gcm`(clash converter 的默认 fallback、AEAD 2022 规范内;mihomo Alpha 2024+ 和 xray 都支持)

- `mihomo_restore_test.go`(`TestMihomoRestore_RecoversInboundAndUser`):
  - 一个 vmess inbound + 一个用户,pre-restart 全链路 marker 验证
  - `container.Stop()` + `container.Start()`(即 ContainerMgr.StartAll 后的重启路径;不做 hard-kill,因为 v2raymg 目前没有 mihomo-crash-auto-restart,production 重启也走 Stop+Start)
  - post-restart:**forward 端口稳定性**(PortMappings 保证)+ listener 重建 + 新 xray client 再次握手成功
  - 新建独立 `runRestoreHandshake` helper,避免 pre/post client 进程状态串扰

- 顺手修 `pkg/proxy/systemtest/README.md`:
  - 清理 5 处 `./pkg/proxyrefactor/...` 老路径残留为 `./pkg/proxy/...`(CLAUDE.md 里已标注的历史残留)
  - 新增 "Mihomo (stage 10b)" 段落,列出三文件用途 + 前置条件(XRAY_BIN 必须 / MIHOMO_BIN 可选)+ 运行命令
  - 开头 "Purpose" 改写,不再只提 xray

- 顺手修 `pkg/proxy/systemtest/helpers_test.go`:
  - `noopForwardManager` 补 `DropUser` / `AllocatePort` / `ReleasePort` 三个 stage 5 前置加的接口方法。**pre-existing bug**,vet 在 `-tags=integration` 下才暴露(`go build` 不报,因为 noop 结构没在接口位置使用);新加的 `mihomo_helpers_test.go` 用真实 `NewDefaultForwardManager`,不依赖 noop,但 noop 的 interface 不完整本身是问题,stage 10b 顺手修

**未做项**:

- **规模测试**(D6 用户决策跳过):单节点 {100, 500, 1000} × {1, 3} 的 PUT /configs 延迟 / yaml 加载时长 / 内存基线。留作独立后续任务;当前共享凭据模型下 listener 数 = inbound 数(典型 1~10),与用户规模解耦,R1 已消除(设计文档已注),不卡 MVP
- **hard-kill mihomo 进程** 再 Restore:production 无 auto-restart,Stop+Start 覆盖重启路径。若未来加 crash-watch daemon,再补 kill-then-restart 测试(需要扩 MihomoContainer 或走 `pkill -f <config_file>` 外置 shell)
- **trojan plain TCP 核实的自动回退**:P2-9 代码里 inline 注释给出了 TLS fallback 的方向,但未实际写 fallback 分支。真实集成跑发现 trojan 失败时手动切换代码

**编译/ vet 验证**:

- `go build ./...` 通过
- `go build -tags=integration ./pkg/proxy/systemtest/...` 通过
- `go vet -tags=integration ./pkg/proxy/systemtest/...` 通过(pre-existing 3 个 vet 警告分布在 `pkg/log`/`pkg/collecter`/`pkg/rpc/server`,均与 stage 10b 无关)
- `go test ./... -race -count=1` 全仓 32 包全绿,无回归

**实测待办**(integration 路径的行为验证,本地缺 mihomo/xray 二进制 + 可控网络环境):

- `XRAY_BIN=... MIHOMO_BIN=... go test ./pkg/proxy/systemtest -tags=integration -run TestMihomo -v`:预期三协议 subtest 全绿 + restore 绿
- 若 trojan 失败(P2-9),按代码注释改为 TLS 案例,参数里加 cert/key 并在 client 的 outbound 配 TLS + allowInsecure=true

### 阶段 11(2026-04-23 完成)

**交付**:

- `CHANGELOG.md`:新增 `## 2026-04-23 — Mihomo Container Stage 10` 段落,分 10a / 10b 两小节,覆盖 ProcessController 接口扩 WaitReady、Updater Step 7 重组、新 integration 测试清单、运行命令
- `README.md`:
  - 开头 tagline 从"for v2ray/xray" 改为泛化的"orchestrates multiple proxy kernels (xray / hysteria / snell / mihomo)"
  - 新增 `## Supported Proxy Kernels` 表,列出四个 kernel 的 container 名 / MVP 协议 / 备注;指向 `docs/container-design-principles.md` 作为"加新 kernel" 的入口
- `wiki/knowledge/mihomo-container/`(通过 `write-wiki-page` skill):
  - `index.md`(frontmatter + 8 个 answers + 18 条 Key Facts + 数据流图 + 核心文件清单)
  - `details.md`(生命周期 / User 事件管线 / 共享凭据 listener 布局 / REST 客户端 / Updater 7 步 / 订阅生成)
  - `edge-cases.md`(7 条 FAQ + 4 条反例 + 5 条注意事项;覆盖 stage 9 breaking 迁移 / stable 无 SHA256 / trojan plain TCP 的 UNCERTAIN / crash 无 auto-restart 等)
  - `related.md`(依赖 `port-management`;外部资源 MetaCubeX 仓库 + Alpha release + clash.meta wiki)
  - `.meta.yaml`(status=draft, owner=v2raymg-core, source 列 8 份代码+设计文档, changelog 2 条)
  - wiki-lint 全绿 + wiki-stamp + wiki-manifest --rebuild 完成;`_manifest.json` 现在登记了 mihomo-container + port-management 两个概念

**未做项**:

- wiki 的 `confidence` 设为 `high`,`status` 留作 `draft`。`status=verified` 等第一个用 `read-wiki-page` skill 路由到这页并实际纠偏后再升。按契约 "first draft 默认 draft" 惯例
- `docs/http-api-reference.md` 未改(stage 8 review 修复里已补 mihomo 枚举,本阶段无新字段改动)
- `docs/mihomo-container-design.md` 的 R1/D5 规模基线一节未填(D6 跳过规模测试,R1 已在该文档里注明"消除";D5 后续任务承接)

**验证**:`wiki-lint mihomo-container` 0 errors;`wiki-manifest --rebuild` 两个 concept 均被 indexed

### 阶段 11+(2026-04-24 完成)

**动机**:阶段 10b 的 protocol matrix 用 xray 作 client、本地 httptest 作目标,不是"生产实际链路"。用户 stage 11+ 指定:mihomo **server 和 client 都用同一个 binary**(和线上完全同构),访问**真实 google.com**,加三种"动 server 让链路断"的对比实验,证明流量确实经过 server。

**决策**(开工前用户确认 Q1~Q4):

| # | 决策点 | 选定 |
|---|-------|------|
| Q1 | 协议范围 | **三协议全覆盖** vmess / trojan / shadowsocks |
| Q2 | 负向对比数 | **三种**:Stop server + RemoveUser + 改凭据 |
| Q3 | 目标 URL | 默认 `https://www.google.com`,`MIHOMO_E2E_TARGET` env 可覆盖 |
| Q4 | 网络不可达 | 强制 fail(precheck 直连目标),不走 skip |

**交付**:

- `pkg/proxy/systemtest/mihomo_e2e_test.go`(`TestMihomoE2E_RealInternet`,`//go:build integration`):
  - 顶层:网络 precheck(直连目标不通 → t.Fatalf,不 skip)→ ensureMihomoBin → startMihomoContainer(server 侧)→ 三协议 subtest
  - 每协议 subtest 跑 5 步(positive_baseline / negative_stop_server / recover_restart_server / negative_change_credential / negative_remove_user),每步 inline 断言 + log 步骤名,失败时日志像完整 transcript
  - `buildMihomoClientConfig` + `marshalProxyInline` + `yamlScalar` 手写 yaml 字符串(mihomo client 配置),`mixed-port` 做 SOCKS5/HTTP 混合入口,`proxies→proxy-groups→rules` 路由把 SOCKS5 流量全走 upstream
  - `startMihomoClient` 拉起独立 mihomo 进程,等 external-controller API + mixed-port 都就绪
  - `expectProxiedOK` / `expectProxyFail` / `assertDirectReachable` 三个断言 helper:proxied fail 的语义宽松(net error / 5xx 都算"chain down"),proxied ok 宽松为 status<500(google 可能返 3xx 地区重定向)
  - HTTP client 关 keep-alive(`Transport.DisableKeepAlives=true`),每步都是全新 SOCKS5 连接,避免 keep-alive 复用让 stop_server 步骤出现假阳性

- `pkg/proxy/systemtest/mihomo_helpers_test.go::mihomoTestRig`:新增 `dataDir` 字段。mihomo Alpha 的 SAFE_PATHS 规则要求 config 引用的文件路径位于 `-d` 参数指定的 home directory 下,否则 `path is not subpath of home directory` 报错 — trojan cert 必须写在 DataDir 里

**P2-9 CERTAIN + trojan cert 扩展**(E2E 首跑的意外收获):

第一次跑出 baseline fail,mihomo 报 `disallow using Trojan without both certificates/reality/ss config` — 证实 P2-9 的 UNCERTAIN 升级为 CERTAIN:**mihomo Alpha 的 trojan listener 运行时硬约束需要 cert/reality/ss 其一**,不是 "decode 能过" 就完事。用户决策走"扩 FastAddInbound 支持 trojan TLS cert"(而非从 MVP 移除 trojan):

- `pkg/proxy/containers/mihomo/inbound.go::MihomoSharedCred`:加 `CertFile` / `KeyFile` 字段(json omitempty,向后兼容旧 JSON 记录)。`Validate` 加成对校验(both or neither,不允许单侧)
- `pkg/proxy/containers/mihomo/adapter.go::extractSharedCred`:trojan 分支读可选的 `cert_file` / `key_file` 参数;不强制,留给 Validate 和 mihomo runtime 各自拒绝不合法配置。Godoc 说明"缺 cert 会在 mihomo 启动时失败"
- `pkg/proxy/containers/mihomo/profilegen.go::BuildListener`:trojan 分支当 cert 都设时输出 mihomo yaml 字段 `certificate` / `private-key`(路径形式);都为空时保持原 stage 4 行为(只 emit users),保证 profilegen 旧单测不回归
- e2e 中 trojan case 在 `runE2EProtocolCase` 里分支调 `writeTempCert(rig.dataDir)`(CN=localhost 自签),写到 DataDir 下满足 SAFE_PATHS;credential swap 步骤复用同一对 cert(只换 password),失败明确归因为 password mismatch 而非 cert/SNI 冲突
- e2e 中 trojan client proxy 加 `sni: 'localhost'` + `skip-cert-verify: true`

**实际 E2E 跑通**(2026-04-24 08:38,本地环境):

```
--- PASS: TestMihomoE2E_RealInternet (16.54s)
    --- PASS: TestMihomoE2E_RealInternet/vmess (4.61s)
    --- PASS: TestMihomoE2E_RealInternet/trojan (7.39s)
    --- PASS: TestMihomoE2E_RealInternet/shadowsocks (3.87s)
```

三协议 × 5 步 = **15 个断言点全绿**;mihomo Alpha(`alpha-1fea551`)通过 Updater 自动下载;google.com 正向 HTTP GET 成功,三种负向按预期失败。PortMappings 在 restart + credential swap 两个场景下均保持 forward port 稳定(vmess 40139→40139,trojan 49598→49598,ss 40272→40272)。

**实际变动**(相对 stage 11+ 开工方案):

- **trojan cert 扩展**原不在方案,是 E2E 首跑发现 P2-9 CERTAIN 后的决策执行(详见 Q5 用户确认)
- **rig 暴露 dataDir**原不在方案,是 mihomo SAFE_PATHS 二次发现导致必须让 e2e 知道 mihomo home directory
- **HTTP client 关 keep-alive**原不在方案,审计代码时意识到 keep-alive 可能在 stop_server 步骤产生假阳性,主动加
- **subtest 顺序**:原设计 remove_user 在 change_credential 前面;审视时发现"先 remove 再 swap"会让 swap 步骤的证据弱化(无法归因到凭据层),调换为 swap → remove,swap 步骤得以精确到"同 forward port、新凭据、client 旧凭据 → 握手失败"

**未做项**:

- vless / hysteria2 / tuic / anytls 的 e2e 扩展:独立后续任务,架构已预留(e2eCase 表驱动易扩)
- **trojan 的真实生产级 cert 管理**:现在的 adapter 接 cert_file/key_file 路径字符串,交给运维在节点上准备 cert 文件。线上完整路径是"cluster certmgmt 签发 → 存到 DataDir → FastAddInbound 时引用",可以留到 trojan MVP 用户实际落地时打通

**紧跟 stage 11+ 的 stage 1 原始遗漏修复**(2026-04-24):

用户尝试在生产 config 里开启 mihomo container 时直接报错(factory not found)。根因是 `cmd/server.go` 的 blank-import 列表原本只 wire 了 xray / hysteria / snell:

```go
_ "github.com/lureiny/v2raymg/pkg/proxy/containers/hysteria"
_ "github.com/lureiny/v2raymg/pkg/proxy/containers/snell"
_ "github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
```

mihomo 的 `register.go::init()` 因此永远不跑,`container.RegisterFactory(ContainerMihomo, ...)` 未执行,`ContainerMgr` 加载配置遇 `type: mihomo` 落到 "unknown container type" 错误分支。

stage 1 的单测 `TestMihomoFactoryLoadable` 在 mihomo 包内部(`init` 被同包 import 自动触发)过,stage 10b/11+ 的集成测试直接调 `mihomo.NewMihomoContainer`(同包 import,init 也跑),**没有任何一个测试走 ContainerMgr 的配置加载路径** —— 是测试设计的真实盲区。

修复 1 行:`cmd/server.go` 补 `_ "github.com/lureiny/v2raymg/pkg/proxy/containers/mihomo"` blank import。`make build` 通过,用户重启 server 后配置加载正常。

**后续**:可考虑加一个 e2e 测试或冒烟测试走完整的"读 config → ContainerMgr 构造 → Start" 路径,避免未来再漏 wire 新 container。优先级 P3。

**验证**:

- `go build ./...` 通过
- `go test ./pkg/proxy/containers/mihomo/... -race -count=1`:全绿,42 + 0 = 42 测试(stage 10a 后数量稳定,trojan 扩展没引入新单元测试,但 profilegen 和 adapter 现有单元测试全绿覆盖了新分支的"cert 都为空"分支;新分支 "cert 都为非空" 由 e2e 覆盖)
- `go vet ./...`:只剩 3 个 pre-existing 警告(pkg/log/pkg/collecter/pkg/rpc/server),与本轮无关
- `go test -tags=integration ./pkg/proxy/systemtest -run TestMihomoE2E_RealInternet -v`:三协议 5 步 15 断言全绿
- `go test ./... -race -count=1`:32 包全绿,无回归

### 阶段 12(2026-04-24 完成)

**动机**:用户在 CLI 上跑 `FastAddInbound -container mihomo` 直接报 `param "password" required`。
根因链:
- CLI 默认 protocol = trojan(stage 1 前就定的)
- mihomo adapter 对 uuid/password/cipher 合约**严格**,缺一即 `ErrMissingCredential`
- xray 的 adapter 虽然也严格,但 xray 的 FastAdd 实现层(`buildFastAddSimpleSpec` + `profilegen/*`)**自动生成**缺失字段
- mihomo 没有这层"底层自动生成",UX 不对齐
- 加上 mihomo trojan 运行时还要求 cert(stage 11+ P2-9),CLI 默认路径"三重失败"

用户明确决策:
- **方案 B** 抽独立 pkg 做协议级归一(与 container 无关)
- xray 底层 auto-gen 保留不动
- trojan 的 TLS 必需性检查放在新层
- cert 要跟 certmgr 联动(4 源),mihomo 用 SAFE_PATHS env 信任 certmgr 目录(不走软链,避开 mihomo symlink issue)
- CLI 不加 extra_params 字段(维持 scope 最小)

**交付**:

- `pkg/proxy/core/params/defaults.go`(新包 + 20 个单测):
  - `FillDefaults(params, protocol, certMgr, scratchDir) error` — 凭据 fill(vmess uuid / trojan password / ss password+cipher default aes-256-gcm) + trojan TLS 4 源归一(cert_file+key_file / certificate+key / domain via certMgr / self_signed) → params["cert_file"]+params["key_file"]+params["cert_source"]
  - 协议无关:和 xray / mihomo 两边都兼容;xray 底层已有的 auto-gen 在 params 归一后退化为 no-op(cert_file 已在)
- `pkg/rpc/server/end_node_inbound.go::FastAddInbound`:在 extra_params merge 后、c.FastAddInbound 前调 `FillDefaults`;scratchDir 通过 `CertScratchDir()` interface 从 container 拿(mihomo → DataDir,其他 → os.TempDir);certMgrAdapter 包装 `s.certManager.GetCertFiles` → `params.CertManager.GetCert`
- `pkg/certmgmt/service.Manager.Path() string`:暴露 storage root
- `MihomoContainer`:
  - `WithSafePathRoots(roots ...string)` option — 启动时拼 `SAFE_PATHS=<roots>` env var
  - `CertScratchDir() string` — 返 DataDir(SAFE_PATHS 默认信任),让 RPC 归一层把自签/PEM 内容写到 DataDir 下
  - 工厂 `New` 里 type-assert `opts.CertManager.(interface{ Path() string })` → 拼 certmgr 路径进 SAFE_PATHS
  - **顺手修**:工厂之前没 wire `WithUserManager(opts.UserManager)` — production mihomo user 事件链路实际没接,因为 stage 10b/11+ 测试绕过 factory 所以没暴露。stage 12 补上
- `pkg/proxy/tools/process.RunnerConfig.Env []string`:新字段,child 进程 env = `os.Environ() + Env`(非 nil 时)
- `MihomoSharedCred.CertSource` + `shouldCleanupCerts()`:记录 cert 来源("file"/"pem"/"domain"/"self_signed"),`RemoveInboundConfig` 仅清理 v2raymg 自己写的 cert 文件(pem/self_signed),file/domain 不动(运维 / certmgr 管)
- `pkg/proxy/systemtest/mihomo_e2e_test.go::TestMihomoE2E_FillDefaults_TrojanSelfSigned`(新 integration test):走 `self_signed=true` 路径,验证 FillDefaults + mihomo DataDir SAFE_PATHS + end-to-end + cleanup 全链路
- `cmd/cli/suggest.go::containerSuggest` 描述扩:`xray (default) / snell / hysteria / mihomo`

**验证**:

- `go test ./pkg/proxy/core/params/... -race -count=1`:20/20 测试全绿
- `go test ./pkg/proxy/containers/mihomo/... -race -count=1`:全绿
- `go test ./pkg/rpc/server/... -race -count=1`:全绿
- `go test ./... -race -count=1`:34 包全绿,无回归
- `go test -tags=integration ./pkg/proxy/systemtest -run TestMihomoE2E -v`:三协议 15 断言 + FillDefaults self_signed baseline + cleanup 全绿

**未做项(scope 控制)**:

- 让 xray 底层 auto-gen 退出(改依赖 RPC 的 FillDefaults):保留原因是防御式 — 有非 RPC 路径直接调 xray FastAdd(如 test helpers)时仍兜底,scope 变大不值
- CLI `FastAddInbound` 的 `extra_params` JSON 字段:用户明确 scope 不加,trojan cert 路径场景由 HTTP/API 直接传
- E2E 的 `domain` 源路径(需要 mock certmgr + 写 cert):FillDefaults 单测已覆盖,mihomo 的 SAFE_PATHS 对 DataDir 之外路径的运行时接纳靠 env var 语义保证。留作独立任务

### 协议扩展 Phase 3:Trojan 高级特性(2026-04-25 完成)

**范围**:对应 `/home/node/.claude/plans/cosmic-leaping-moon.md` 的 Phase 3(T-M-P3-30 ~ P3-33)。目标是把 Trojan 从 legacy SharedCred FastAdd 路径迁到 ProtocolParams 结构化路径,补齐 mihomo 支持的 transport/security 组合。

**交付**:

- `pkg/proxy/core/params/protocolparams/params_trojan.go`:新增 `parseTrojan`,required `password`,transport 默认 tcp 且仅允许 tcp/ws/grpc,security 默认 tls 且仅允许 tls/reality;显式 `security=none` 和 httpupgrade/xhttp/splithttp/h2 均提前 `ErrInvalidCombination`
- `pkg/proxy/containers/mihomo/adapter.go`:FastAdd dispatch 将 `ProtocolTrojan` 纳入 `protocolparams.Parse → FromProtocolParams`
- `pkg/proxy/containers/mihomo/inbound.go`:新增 `validateTrojan` dual-rail;新记录读 `ProtocolParams.Trojan + SecuritySpec`,旧记录继续读 `SharedCred`
- `pkg/proxy/containers/mihomo/profilegen.go`:新增 `fillTrojanListener`,支持 `ws-path` / `grpc-service-name` / `certificate` + `private-key` / `reality-config`
- `pkg/proxy/containers/mihomo/subscription.go`:新增 `fillTrojanSubscriptionSpec`,通过 `codec.TrojanNode` 输出 URI,并给 clash converter 透传 `transport` / `security` / `server_name` / `reality_public_key` / `reality_short_ids` / `skip_cert_verify` / `utls_fingerprint`
- `pkg/proxy/core/params/defaults.go`:修正 RPC `FillDefaults` 的 Trojan+Reality 路径;`security=reality` 不再被 Trojan 的 TLS 必需性检查强制要求 `cert_file/key_file`
- `pkg/proxy/core/subscription/converter/clash.go`:新增 `ConvertTrojanForTest`,供 mihomo matrix 走订阅转换链路验证 converter parity
- `RemoveInboundConfig` 证书清理扩展到 ProtocolParams TLS block:legacy SharedCred 和新路径均按 `cert_source=pem/self_signed` 清理,file/domain 不动
- 新增 parser/profilegen/subscription 单测和 `pkg/proxy/systemtest/mihomo_trojan_matrix_test.go`

**真实 integration 结论**:

- `TestMihomoTrojanMatrix`:tcp-tls / ws-tls / grpc-tls / tcp-reality / grpc-reality 全绿
- `TestMihomoTrojanMatrix` 同时覆盖 subscription chain: `subscription_chain_tcp_tls` 和 `subscription_chain_grpc_reality` 走 `GetUserSubscriptions → ConvertTrojanForTest → spawnMihomoClient`,锁住 `fillTrojanSubscriptionSpec` 与 converter 的字段一致性
- `ws-reality` 保留但 skip:mihomo Alpha 客户端连接报 `unexpected status: 200 OK`,与 VLESS/VMess ws+reality 的已知 stack-order 限制同类

**验证**:

- `go test ./...`:全绿
- `go test -tags=integration ./pkg/proxy/systemtest -run '^$'`:integration 编译通过
- `go test -tags=integration ./pkg/proxy/systemtest -run TestMihomoTrojanMatrix -v -timeout 180s`:7 PASS + 1 SKIP

详细开发记录见 `tmp/phase3-dev-report.md`。

### 协议扩展 Phase 4:Shadowsocks 增强(2026-04-25 完成)

**范围**:对应 `/home/node/.claude/plans/cosmic-leaping-moon.md` 的 Phase 4(T-M-P4-40 ~ P4-44)。目标是把 Shadowsocks 从 legacy SharedCred FastAdd 路径迁到 ProtocolParams 结构化路径,默认 cipher 升级到 SIP022,并接入 obfs / v2ray-plugin / shadow-tls 三种插件。

**关键变更**:

- `pkg/proxy/core/params/protocolparams/params_ss.go`(新建):新增 `parseSS`,required `password` + `cipher`,udp 默认 true,plugin 可选;6 个 plugin opt 键(`plugin_mode/host/path/tls/password/version`)读入后用 mihomo canonical 短名(`mode/host/path/tls/password/version`)存入 `SSParams.PluginOpts`
- `pkg/proxy/core/params/protocolparams/parser.go`:Shadowsocks dispatch 从 stub 改为 `parseSS`;`keys.go` 增 `KeyPluginMode/Host/Path/TLS/Password/Version`
- `pkg/proxy/core/params/defaults.go`:默认 cipher 改为 `2022-blake3-aes-256-gcm`;新增 `randomSSPassword(cipher)` —— SIP022 系列(`2022-` 前缀)按 cipher 决定密钥长度(aes-256 → 32 bytes,aes-128 → 16 bytes),`base64.StdEncoding.EncodeToString` 输出;classic AEAD 沿用 `randomHex(16)`。**cipher 必须先于 password 落定**(原顺序倒置会让 randomSSPassword 拿不到正确 cipher)
- `pkg/proxy/containers/mihomo/adapter.go::fastAddBuildInbound`:SS 加入 ProtocolParams 路径
- `pkg/proxy/containers/mihomo/inbound.go`:新增 `validateSS` dual-rail(ProtocolParams 优先 + SharedCred legacy 兜底);消除 `Validate()` 末尾 unreachable `return nil`;`shouldCleanupCerts()` 改为调用 `shouldCleanupCertSource()` 去重
- `pkg/proxy/containers/mihomo/profilegen.go::fillSSListener`:ProtocolParams 路径下发 `password` / `cipher` / `udp`(true 时显式输出);`plugin=shadow-tls` **跳过** `plugin` / `plugin-opts` 字段(shadow-tls 是网络层 wrapper 而非 mihomo SS 原生 plugin);其他 plugin 输出 `plugin-opts` map,`tls` → bool,`version` → `strconv.Atoi` 转 int
- `pkg/proxy/containers/mihomo/subscription.go`:新增 `fillSSSubscriptionSpec / fillSSSubscriptionSpecPP / fillSSSubscriptionSpecLegacy`;Phase 4 路径把 `method` / `udp` / `plugin` / `plugin_*` 全部写入订阅 Extensions(shadow-tls 也写,因为客户端 Clash config 需要这些字段生成 outbound)
- `pkg/proxy/core/subscription/converter/clash.go`:`PluginOpts` struct 增 `Password` / `Version` 字段(shadow-tls);`buildPluginOpts` 增 `plugin_password` / `plugin_version` 读取,空值时返回 nil 避免 emit 空 `plugin-opts`;`convertShadowsocks` 默认 cipher 改为 `2022-blake3-aes-256-gcm`,`UDP` 改为读 `Extensions["udp"]`(Phase 4 之前硬编码 true);新增 `defaultRealityFingerprint = "chrome"` 常量,`convertVLess` / `convertVMess` / `convertTrojan` reality 路径自动 fallback;新增 `ConvertSSForTest` 供 mihomo SS matrix 走订阅链路验证 converter parity
- `pkg/http/fastAddInbound_handler.go`:request struct 增 `Plugin` / `PluginMode` / `PluginHost` / `PluginPath` / `PluginTLS` / `PluginPassword` / `PluginVersion` 便捷字段;PluginTLS 的 `!exists` 冗余 guard 简化

**测试**:

- 单元测试新增 `params_ss_test.go`(10 case)、`profilegen_ss_test.go`(6 case)、`subscription_ss_test.go`(8 case,含 plugin_tls Extensions 路径)
- `defaults_test.go` 新增 SIP022 base64 密码验证测试 + classic AEAD hex 密码验证测试
- 系统测试 `mihomo_ss_matrix_test.go`(integration build tag):4 cipher case(aes-256-gcm / chacha20-ietf-poly1305 / 2022-blake3-aes-256-gcm / 2022-blake3-aes-128-gcm)+ subscription chain cross-cutting test(走 `GetUserSubscriptions → ConvertSSForTest → spawnMihomoClient` 链路)
- shadow-tls server-side e2e 测试用 `t.Skip("shadow-tls requires external server")` 占位

**review 修复**:Phase 4 实现后跑了一次多 agent 后台 review,共识别 12 条 P0-P3 问题(SS password 生成缺陷、unreachable code、UDP 透传缺失、buildPluginOpts 空值返回 non-nil、stale 注释、缺系统测试、version int 转换、UDP 空字符串处理、缺 plugin_tls 测试、cleanup 重复逻辑、PluginTLS guard 冗余、缺 base64 密码测试);全部当场修复,最终 35 packages `go test ./... -race -count=1` 全绿。

**已知限制**:

- shadow-tls server 需外部基础设施,系统测试 skip
- kcp-tun 插件未实现(`params_ss.go` 留 TODO,作为独立任务)
- mihomo Alpha SS listener 无 transport / security 概念,所以 SS 的 ProtocolParams 不读 `pp.Transport` / `pp.Security`(与 vless/vmess/trojan 不同)

**验证命令**:

- `go test ./... -race -count=1 -timeout 300s`:35 packages 全绿
- `go test -tags=integration ./pkg/proxy/systemtest -run TestMihomoSSMatrix -v -timeout 180s`:需外网 + MIHOMO_BIN 或 GitHub egress

### 协议扩展 Phase 5:Hysteria2(2026-04-25 完成)

**范围**:对应 `/home/node/.claude/plans/cosmic-leaping-moon.md` 的 Phase 5(T-M-P5-50 ~ P5-57)。目标是把 Hysteria2 协议接入 mihomo 容器的 ProtocolParams 路径,作为首个 QUIC/UDP 协议在 mihomo 上跑通。Phase 5 是该协议在 mihomo 容器中的**首次出现**(无 legacy SharedCred 路径),hysteria 容器(`pkg/proxy/containers/hysteria/`)保持单 inbound legacy 形态不动。

**事实核对(实施前)**:

- mihomo Alpha `listener/inbound/hysteria2.go::Hysteria2Option` schema:`users: map[string]string`(无顶层 password)、`certificate` / `private-key`(非 cert-file/key-file)、`obfs` / `obfs-password`、`up` / `down` / `ignore-client-bandwidth`、`masquerade`、`alpn`(数组)。键全部 kebab-case
- 上游 `hysteria2://` URI spec(<https://v2.hysteria.network/docs/developers/URI-Scheme/>)只识别 5 个 query key:`obfs` / `obfs-password` / `sni` / `insecure` / `pinSHA256`;`up` / `down` / `masquerade` 不在标准里
- mihomo Alpha 客户端 hy2 outbound schema **没有** `masquerade` 字段(server-only)

**关键变更**:

- `pkg/proxy/core/params/protocolparams/params_hysteria2.go`(新建):`parseHysteria2`,required `password`(FillDefaults 兜底);Transport 强制 nil(QUIC 固定),Security 强制 tls(reality / none 拒绝);obfs ∈ {`""`, `"salamander"`},salamander 必须有 obfs_password;up/down 与 ignore_client_bandwidth **不互斥**(mihomo 允许共存)
- `pkg/proxy/core/params/protocolparams/parser.go`:Hysteria2 dispatch 从 stub 改为 `parseHysteria2`
- `pkg/proxy/core/subscription/codec/hysteria2.go`:`Hysteria2Node` struct 加 `Up` / `Down` / `Masquerade` 三字段。**Encode/Decode 不读不写这三字段** —— 上游 URI spec 不带,放进 URI 会污染标准客户端兼容性。三字段仅通过 `SubscriptionSpec.Extensions` 透传到 ClashConverter
- `pkg/proxy/core/subscription/converter/clash.go`:`ClashProxy` struct 加 `Up` / `Down`(yaml `up` / `down`);`convertHysteria2` 从 `Extensions` 读 `up` / `down` 写入 ClashProxy。**Masquerade 故意不传到客户端** —— mihomo client schema 无该字段。新增 `ConvertHysteria2ForTest` 供系统测试走订阅链路
- `pkg/proxy/containers/mihomo/adapter.go::fastAddBuildInbound`:Hysteria2 加入 ProtocolParams 路径
- `pkg/proxy/containers/mihomo/inbound.go`:新增 `validateHysteria2`(无 legacy SharedCred 路径);新增 `forwardNetworkForProtocol(p)` —— hy2 → `"udp"`,其它协议保持 TCP 默认;`MihomoInbound.AddUser` 调用 `userMgr.GetBindPort` 时填入 `Network` 字段。`ErrProtocolNotSupported` 错误信息更新到当前已支持协议清单
- `pkg/proxy/containers/mihomo/profilegen.go::fillHysteria2Listener`:输出 mihomo yaml `users: { default: <password> }`(单用户用 `"default"` 作 username,后续多用户走 user-tracker refactor)、`certificate` / `private-key`(从 `cert_file` / `key_file` 映射)、`alpn` 默认 `["h3"]`、按需输出 `obfs` / `obfs-password` / `up` / `down` / `ignore-client-bandwidth` / `masquerade`
- `pkg/proxy/containers/mihomo/subscription.go`:新增 `fillHysteria2SubscriptionSpec`,把 `obfs` / `obfs_password` / `up` / `down` / `masquerade` / `skip_cert_verify` / `server_name` 写入 Extensions
- `pkg/http/fastAddInbound_handler.go`:request struct 加 6 个便捷字段(`Obfs` / `ObfsPassword` / `Up` / `Down` / `Masquerade` / `IgnoreClientBandwidth`);convFields 同步;help 文本更新

**测试**:

- 单元测试新增 `params_hysteria2_test.go`(15 case,含 IgnoreClientBandwidth 字符串/bool/unparseable 类型分支)、`profilegen_hy2_test.go`(5 case)、`subscription_hy2_test.go`(3 case)、`container_fastadd_test.go`(hysteria container legacy 路径回归)
- `inbound_test.go::TestValidateHysteria2_ErrorPaths`:9 个错误路径用例(missing Hy2Params / empty password / missing security / security=none / TLS spec nil / missing cert/key / unknown obfs / salamander missing obfs_password)+ baseline 哨兵
- `clash_protocol_test.go`:`TestConvertHysteria2_UpDown` + `TestConvertHysteria2_DropsMasquerade`(后者用 `yaml.Marshal` 输出后断言不含 `masquerade` 子串,真锁住"masquerade 不进客户端配置"的契约)
- 系统测试 `mihomo_hy2_matrix_test.go`(integration build tag):3 矩阵 case(tls baseline / tls+salamander obfs / tls+up/down 限速)+ 1 cross-cutting subscription chain case(走 `GetUserSubscriptions → ConvertHysteria2ForTest → spawnMihomoClient` 链路);新增 `waitUDPListener` helper 用 UDP dial 轮询替代固定 sleep(50ms × ≤60 次,3s deadline)

**review 修复**:Phase 5 实现后并行起 4 个 agent 做了 code review,共识别 P1-P3 共 14 条问题(`TestConvertHysteria2_DropsMasquerade` 测试虚假保证、systemtest 500ms 固定 sleep、validateHysteria2 vs parseHysteria2 双重校验注释缺失、forwardNetworkForProtocol 单 if 不易扩、IgnoreClientBandwidth 测试覆盖不全、buildSubscriptionSpec dispatch docstring 漏 hy2、ErrProtocolNotSupported 文案过时、users 测试未 type-assert string、validateHysteria2 错误路径未覆盖、codec.Hysteria2Node 注释 server-side / client-advertised 矛盾、clash.go 缺 masquerade 反向 grep 锚点、HTTP help 文案过时、`hy2ProxyFromSpec` 漏 cp.UDP、`buildHy2ClashProxy` alpn 注释不一致);全部修复 + gofmt 一把梭对齐。最终 36 packages `go test ./... -race -count=1` 全绿。

**已知限制**:

- mihomo client outbound schema 没有 `masquerade` 字段,只在 server-side 输出
- forwardNetworkForProtocol 已为 Phase 6 TUIC 留 `TODO` 钩点(`return "udp"` 分支)
- waitUDPListener 是 kernel-level connect 探活,不验证 QUIC 握手 —— 实际 connect 由后续 mihomo client 发起的真实流量验证

**验证命令**:

- `go test ./... -race -count=1 -timeout 300s`:36 packages 全绿
- `go test -tags=integration ./pkg/proxy/systemtest -run TestMihomoHy2Matrix -v -timeout 180s`:需外网 + MIHOMO_BIN 或 GitHub egress

## 参考

- `docs/mihomo-container-design.md` — 本计划的设计依据
- `docs/container-design-principles.md` — 三原则 + 模式矩阵
- `docs/xray-container-architecture.md` / `docs/snell-container-design.md` — 已有 container 实现参照
- `pkg/proxy/containers/snell/container.go:368-467` — user event handler + forward 模板(stage 5 抄,多 inbound 扩展)
- `pkg/proxy/containers/xray/exec_runner.go:523-593` — 多 inbound CRUD 模板(stage 4 抄)
- `docs/cluster-user-implementation-plan.md` — 阶段式实施计划风格参照
