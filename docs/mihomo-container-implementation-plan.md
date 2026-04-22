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
| 阶段 3 | TODO | |
| 阶段 4 | TODO | |
| 阶段 5 | TODO(接口已就位) | |
| 阶段 6 | TODO | |
| 阶段 7 | TODO | |
| 阶段 8 | TODO | |
| 阶段 9 | TODO(MVP 已能 auto-download;此阶段补 SHA256 / Update 热替换 / OS-arch 检测 / latest 解析) | |
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

### 阶段 3 开工前提示

- `rest_client.go` 已有 `GetVersion` 的骨架,新增方法可照其结构(baseURL / secret / httpClient)扩展
- mihomo `/configs` 的 PUT force 语义(本计划 R5)**未验证**,阶段 3 开工前需查 `hub/executor/executor.go` 的 `ApplyConfig`,记录到 memory `reference_mihomo_protocol_facts.md`

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

## 阶段 4:Inbound 生命周期

**目标**:`FastAddInbound` / `RemoveInboundConfig` / `GetInboundConfig` / `ListInboundConfigs` 可用,listener 正确增删。注意此阶段**还不含 user 级 listener 展开**,每个 inbound 先只生成一个"无用户"listener(listener 的 users 数组为空或占位),验证 inbound 元数据管理链路。

**产出**:

- `inbound.go`:`MihomoInbound` 实现 `core/inbound.Inbound`,extensions 存 tls/transport/per-protocol 字段
- `adapter.go`:`contracts.InboundSpec → MihomoInbound`
- `profilegen.go` v0:仅生成 listener 壳子(name/type/listen/port/tls),users 数组留空待阶段 5 填充
  - 覆盖协议(D4):vmess / trojan / shadowsocks 三个;其他协议本阶段不支持,`FastAddInbound` 遇到返回 `errs.ErrProtocolNotSupported`
- `container.go`:
  - `FastAddInbound(tag, params)`:构造 MihomoInbound → InboundStore 持久化 → `PUT /configs` 全量下发
  - `RemoveInboundConfig(tag)`:剔除该 inbound 的所有 listener → 更新 store → `PUT /configs`
  - `GetInboundConfig(tag)` / `ListInboundConfigs()`:查 `c.inbounds` map
- `InboundStore` 新 record 类型:`ContainerType="mihomo"`,`NativeJSON` 存 MihomoInbound 序列化

**验收**:

- 单元测试:profilegen 对每个 D4 协议输出的 YAML 能通过 mihomo ParseListener(可用真实 mihomo 的 `/configs` PUT 验证)
- 集成测试:FastAddInbound → GET /configs 能看到 listener;Remove → GET /configs 不再有该 listener
- 其他 inbound 在 Add/Remove 过程中**连接不受影响**(mihomo name-keyed diff 特性验证)

## 阶段 5:内部端口池 + User Event

**目标**:UserEventAdd/Remove/Update 打通,per-user listener 正确生成,forward 层端口正确分配。

**产出**:

- `ports.go`:内部端口池(`InternalPortRange` 配置驱动,分配/释放/持久化到 store 或内存)
- `profilegen.go` v1:`(MihomoInbound, UserSpec) → listener YAML`,listener name 规则 `<inbound_tag>__<username>`,users 数组单用户
- `container.go`:
  - `UserEventChannel()` 返回 `c.userEventCh`
  - `forwardUserEvents` / `startUserEventHandler`:抄 `hysteria/container.go:473-570` 模板
  - `handleUserEvent`:
    - `Add`:分配 internal_port → profilegen → 合并当前 listeners → `PUT /configs` → `GetBindPort(TargetPort=internal_port)`
    - `Remove`:遍历该用户所有 listener → `PUT /configs` 剔除 → `ReleaseBindPort` → 释放 internal_port
    - `Update`:按 `IsUserVisible` 走 Add/Remove 分支
- `addedUsers map[username]map[tag]{internal_port, listener_name}` + `sync.Mutex` 保护

**验收**:

- 单元测试:mock UserManager 发事件,验证 addedUsers 状态机 + REST 调用序列
- 集成测试:真实 mihomo,AddUser → 公网端口可连 → RemoveUser → 公网端口不可连
- 并发测试:同一用户并发 Add/Remove 幂等(`go test -race`)

## 阶段 6:Restore + Reconcile

**目标**:进程重启后,listeners 和 forward 规则都能自动恢复,无须人工干预。

**产出**:

- `container.go`:实现 `Restorable.Restore(ctx)`
  - 从 InboundStore 读全量 mihomo inbound
  - 从 UserManager 读当前可见用户
  - 展开为全量 listeners + 对应 internal_port 分配
  - 一次性 `PUT /configs`
  - 对账 forward 层:缺失的 `GetBindPort` 补申请,多余的 `ReleaseBindPort`
- `reconcile.go`:周期性对账(`ReconcileInterval`,默认 30s),检测 store/mihomo/forward 三方漂移
- `Reload()` 实现:重拉 Restore 逻辑但**不重启进程**(用于证书/全局配置更新)

**验收**:

- 集成测试:启动 → Add N 个 inbound + M 个用户 → kill mihomo 进程 → 进程再启 → listeners/forward 完整恢复
- 故意删 mihomo 侧某个 listener 后等 ReconcileInterval → 自动补回
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
| R1 | listener 数量膨胀导致 mihomo yaml 加载慢 | 阶段 10 | 规模测试摸底;超阈值切混合模型 |
| R2 | mihomo Alpha schema / REST 行为漂移 | 全程 | D2 决定不锁版本:开发期读 HEAD 做功能参考;rest_client 层探测 mihomo 版本 + 关键字段,不兼容时报清晰错误而非默默降级 |
| R3 | 证书热更时 listener 必须重建 | 阶段 6 `Reload()` | 若不支持仅 patch TLS,Reload 降级为 Restart |
| R4 | clash converter 对 MVP 三协议(vmess/trojan/ss)输出不完整 | 阶段 7 | 阶段 7 先扫描,基本不会缺。hysteria2/tuic/anytls/vless 等扩展协议阶段由对应任务自带 converter 补写 |
| R5 | Alpha 分支 REST API 行为变化(例如 PUT force 语义) | 阶段 3 | rest_client 层探测 mihomo 版本,不兼容时报清晰错误 |
| R6 | per-user internal_port 池耗尽 | 阶段 5 | 池范围配置化;默认 [30000, 40000] 可容 1w 用户 |

## 待解问题(MVP 完成后再评估)

- **扩展协议任务**:vless / hysteria2 / tuic / anytls 各作为独立后续任务,复用 MVP 架构,增量补 profilegen + clash converter + 测试
- 是否把 TUIC/anytls 提为 `contracts.Protocol` 一等公民
- 混合模型(多用户共享 listener)的阈值/开关如何放进 `MihomoConfig`
- 跨节点用户编排(`docs/user-placement-controller-design.md`)如何识别 mihomo container 的可用容量
- `docs/inbound-user-tracker-refactor.md` 延后项落地时,mihomo 如何对齐
- mihomo container 启用 hysteria2 后,是否替代/合并现有 `containers/hysteria/`(单 inbound 专用容器)

## 参考

- `docs/mihomo-container-design.md` — 本计划的设计依据
- `docs/container-design-principles.md` — 三原则 + 模式矩阵
- `docs/xray-container-architecture.md` / `docs/snell-container-design.md` — 已有 container 实现参照
- `pkg/proxy/containers/hysteria/container.go:473-570` — per-user forward 模板
- `docs/cluster-user-implementation-plan.md` — 阶段式实施计划风格参照
