# Mihomo Container 实施计划

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
| 阶段 10 | TODO | |
| 阶段 11 | TODO | |

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

## 参考

- `docs/mihomo-container-design.md` — 本计划的设计依据
- `docs/container-design-principles.md` — 三原则 + 模式矩阵
- `docs/xray-container-architecture.md` / `docs/snell-container-design.md` — 已有 container 实现参照
- `pkg/proxy/containers/snell/container.go:368-467` — user event handler + forward 模板(stage 5 抄,多 inbound 扩展)
- `pkg/proxy/containers/xray/exec_runner.go:523-593` — 多 inbound CRUD 模板(stage 4 抄)
- `docs/cluster-user-implementation-plan.md` — 阶段式实施计划风格参照
