# 99 · 全量分层 code review 汇总（2026-07-10）

> 本文件是 `docs/review-2026-07-10/` 全套分层 review 的**总汇总与处理入口**。方法论见 [`00-overview.md`](00-overview.md)，各层明细见对应 `L*-*.md`。**这是一次纯 review，未改任何生产代码**（除本目录文档外）。

## 1. 范围与方法

- **范围**：`pkg/`、`cmd/` 全量（跳过 `*.pb.go` 等生成代码）。以架构分层自底向上递进：
  L0 基础设施 → L1 契约与持久 → L2 核心域 → L3 容器 → L4 集群/RPC → L5 HTTP/CLI 接口 → L6 测试。
- **方法**：每单元多 lens finder → **对抗性 verifier（反驳优先，默认怀疑）** 两级流程。下层已确认 P0/P1 结论注入上层 reviewer，用于判断“接口误用/本可拦截”。
- **递进增强**：L1、L2 完成后按用户要求**独立重跑第二轮**（换角度找新问题 + 挑战第一轮疑似误报），结果见 `L1-pass2.md` / `L2-pass2.md`。
- **模型**：全程 Fable 5（`claude-fable-5`）。运行期间遭遇多次瞬时 503 网关错误，均通过定点补跑恢复，**最终无 unverified 残留**。
- **误报控制**：第三方协议/上游行为类结论标 `protocolRelated`，verifier 仅在仓库内材料（wiki/docs/config/测试）能确证时才 confirmed，否则最高 uncertain。

## 2. 总体统计

| 层 | 保留 | confirmed | uncertain | refuted(剔除) | P0 | P1 | P2 | P3 |
|----|-----:|----------:|----------:|----:|---:|---:|---:|---:|
| L0 基础设施 | 30 | 30 | 0 | 0 | 0 | 5 | 11 | 14 |
| L1 契约/持久（一轮） | 57 | 53 | 4 | 0 | 0 | 5 | 20 | 32 |
| L1 第二轮补充 | 49 | 43 | 6 | 1 | 0 | 2 | 10 | 37 |
| L2 核心域（一轮） | 86 | 81 | 5 | 3 | 0 | 12 | 37 | 37 |
| L2 第二轮补充 | 65 | 58 | 7 | 0 | 0 | 3 | 20 | 42 |
| L3 容器 | 61 | 55 | 6 | 1 | 1 | 5 | 27 | 28 |
| L4 集群/RPC | 46 | 46 | 0 | 0 | 1 | 12 | 18 | 15 |
| L5 HTTP/CLI | 46 | 46 | 0 | 0 | 0 | 3 | 22 | 21 |
| L6 测试 | 35 | 35 | 0 | 2 | 0 | 10 | 16 | 9 |
| **合计** | **475** | **447** | **28** | **7** | **2** | **57** | **181** | **235** |

> 另有 L1/L2 第二轮对第一轮提出 **17 条质疑**（全部裁定 `pass1-wrong`），绝大多数是**第一轮把同一 finding 记在相邻两个行号**造成的重复、以及个别 severity 偏高——详见各 pass2 文档“对第一轮的质疑裁决”。这些**未自动改写第一轮结论**，需人工合并去重。

## 3. P0 —— 必须优先修（2 条）

> **状态更新（2026-07-12）**：两条 P0 均已在分支 `fix/p0-concurrency` 修复并通过"修复前必炸、修复后必绿"的 -race 验证；CI 新增 targeted `-race` 步骤（`pkg/cluster` + hysteria 容器包）。同簇 P1 未随修，仍按 §4 主题簇处理。

1. **[L4/CLU] `pkg/cluster/node_manager.go:97` — NodeManager 并发 map 读写导致整进程崩溃** ✅ **已修复 `38a6620`**
   `Get/HaveNode/GetAllNode` 无锁直接读 `*nodes`，而 `Add/Delete` 在锁下写同一张 map。每个 gRPC 请求经 `authRemoteNode→NodeManager.Get`，与注册/心跳/filter 的写并发；Go runtime 对并发 map 读写抛**不可恢复 fatal**，center/end 进程直接 panic。这是全仓最易触发、后果最重的缺陷。**同簇还有 3 条 CLU P1 + 2 条 RPC P1**（`Node` 心跳/token 字段无同步、`GetAllNode` 返回内部 map 本体、gRPC conn 泄漏），应作为一个“集群节点状态并发安全”整体重构。
   *修法*：Get/HaveNode 加 RLock；GetAllNode 返回 RLock 内浅拷贝快照（连带修掉 L4 #2 的并发迭代 P1）；调用方零改动；新增 `node_manager_race_test.go` 4 个 -race 回归用例。同簇 Node 字段级竞态（#4/#8/#9）、Filter 丢更新（#15）未修。

2. **[L3/HC] `pkg/proxy/containers/hysteria/container.go:74` — certWaitStopCh 无锁读写 + 停机后仍可能拉起孤儿进程** ✅ **已修复 `f4445c9`**
   `run()` 立即返回 nil（P1，容器被误标 Running），证书等待协程在停止后仍可能启动进程成孤儿；`userEventCh` 向已关闭 channel 发送导致 panic（另一条 HC P1）。hysteria 容器生命周期与 channel 同步需整体收口。
   *修法*：按代 `lifecycleSession{ctx,cancel,wg}`，stop = cancel→wg.Wait→stopProcess（连带修掉 HC #5 send-on-closed P1 与 #24 重启后事件失效 P2）；session ctx 下传 `downloadHysteria` 防 Stop 无限挂起；procMu + 跨代 stale-runner 守卫；新增 `container_lifecycle_test.go` 4 个 -race 回归用例。HC run() 假 Running（#4）、BaseContainer Starting 态窗口、snell 同类簇未修（snell 可逐行迁移本 session 骨架）。

## 4. P1 跨层主题（57 条，按复现模式归并）

> **最终状态（2026-07-13，对抗性逐条审计 + 用户逐条决策后）**：分支 `fix/p0-concurrency`（30 commit，本地未 push）。跑了 59 条（2 P0 + 57 P1）逐条对抗审计（auditor→skeptic，默认判 unfixed），查出前述"全清零"过度乐观，剩 7 条残留，现已全部处置：
> - **5 修**：#56 UserEvent 活指针→emit 快照 `a598ed9`；#53 mihomo 删 inbound 毁共享 PEM→survivor-scan `f924a3c`；#46 ReqToMultiEndNodeServer 无 -race 测试→补 `694f4dd`；**#2 跨方法剪接→方案 B 方法绑定**（NodeAuthInfo.dest_method + 客户端拦截器盖章 + 服务端 VerifyDestMethod）`3999e19`；**#31 明文 center 通道→专用 center_token AES 信封**（复用 codec，内层 per-cluster 鉴权不变，隔离保留）`2ae108d`。
> - **2 接受**：#21（续期不匹配已修，残留仅手动 re-issue 的 sub-ms 窗口，POSIX 限制，用户"不用动"）；#54（sub-converter 半可信+TLS，dns/proxy-providers 已裁，用户"保持现状"）。
> - **#2/#31 是破坏性升级**：center + 全 end 需同时升级并配 center_token（详见各 commit message）。**所有 P0/P1 correctness 至此全部闭合或经用户明确接受。**
>
> **旧状态（保留存档）**：P0 **2/2 全修**；P1 **46/52 已修**，落在分支 `fix/p0-concurrency`（14 个 `fix(` commit）。此前"P1 全部 8 簇完成"的表述是**按簇报、非按 finding 报，过度乐观**——8 个主题簇各有 commit，但簇内曾有 9 条 P1 未逐条闭合。**usermanager 计费并发子簇（本属最大并发簇但被 E 提交漏掉）已于 `0835f7a` 补修**：
> - `usermanager.go:2239` **GetStats 共享 map 并发崩溃（P0 级 fatal）** ✅ —— 改 `drainDeltas` 单锁快照+重置 + `GetStats` 深拷贝，map 不再逃逸锁。`TestGetAllDeltaTraffic_ConcurrentDrainAndCollect` 修前 -race 必炸、修后 6/6 绿。
> - `usermanager.go:2237` GetAllDeltaTraffic 两段锁丢流量 ✅ —— 快照+重置原子化（守恒测试 pin）。
> - `usermanager.go:2233` 零回归测试 ✅ —— 新增 `stats_concurrency_test.go`。另修 `GetUserDeltaTraffic` 的同类 per-user 两段锁（`drainUserDelta`）。
> - **残留（本次划出，非回归、当前生产不可达）**：`collect()` 在锁外读 records、锁内应用 delta，两个并发 collect 乱序 → 触发 counter-reset 启发式 → over-count；但生产中 collect 仅由单 collectLoop 驱动，唯一并发入口 `ForceCollect`(`go sc.collect()`) 只有测试调用。列为 latent P2。
>
> **原"其余仍未闭合"的 backlog 现已全部处理（2026-07-13，用户指示"把 backlog 里的全部修复"）**：
> - VLESS subscription_chain 测试（P1）✅ `test(systemtest)` —— 加 `ConvertVLessForTest` + tcp-tls / tcp-reality-vision 两条走真订阅链路的用例。
> - clash 外部 sub-converter 依赖（P1）✅ `fix(subscription)` —— 全部外部源失败时降级到内置最小模板（loud WARN、无注入面），默认可达时行为不变；顺带修好网络 flaky 的 `TestConvertWithOptions_VLessIncluded`。
> - CI `-race` 扩面（P2）✅ `ci` —— 从 2 包扩到全部 campaign 修复的并发包（逐包核实 race-clean）。
> - HC `run()` 假 Running（P2）✅ `fix(hysteria)` —— `IsRunning()/State()` 改反映真实进程（读侧 override，无写态竞态）。
> - forward drainEnd 慢泄漏（P3）✅ `fix(forward)` —— slot 删除处同步清 drainEnd。
> - `pkg/rpc/client` 零测试（P2）✅ `test(rpc/client)` —— NewNodeAuthInfo nonce 新鲜性等。
> - rotate 测试泄漏 listener（P2/P3）✅ `test(rpc)` —— `t.Cleanup` 关闭 ForwardManager。
>
> **剩余唯一 latent 项**：上面 usermanager `collect()` 并发 over-count（当前生产不可达，`ForceCollect` 仅测试调用），保留为 latent P2。
>
> **B1 集群 RPC 加密/鉴权** 用户选**方案 A（一步到位协调式破坏升级，多 cluster 共 center 拓扑）**已实施于 commit `61adb83`（HKDF KDF + 消息类型 AAD + ts/nonce/dest 防重放 + center app 层多 token 鉴权）；部署硬要求（全集群同时升级、center 配全 cluster_tokens、NTP 依赖扩面、无密钥轮换）见 `B1-rpc-crypto-DECISION-NEEDED.md`。 各簇 commit：A1 集群节点并发 `7a5ffbe`；A2 容器状态机+snell `13e5c25`；B2 SSRF/模板 `e0e0170`；B3 xray security=none `c425504`；C 转发数据面 `a2c3c08`；D 证书原子性 `571a7dd`；E 集群同步一致性 `ddffbe7`；F 资源泄漏/僵尸进程 `77106f6`；G 启动/降级静默失败 `3851c81`；H 探测/采集 `a93f985`。

处理时**按主题成簇修**比逐条修更省事——同一根因在多处复现：

- **并发/数据竞争（最大簇，~18 条）**：集群 `Node` 状态字段与 `NodeManager` map（CLU×3 + RPC×2）、`usermanager` `UserEvent` 活指针 / `GetStats` 共享 map / `GetAllDeltaTraffic` 两段锁丢流量（UM×3）、容器 `userEventCh` send-on-closed（HC/SC/snell 各 1）、`BaseContainer` 状态机 Start/Stop/MarkStopped 竞态（CON×3）。**根因**：节点状态、事件 channel、容器状态机三处缺统一同步原语。`go test` 未开 `-race`（见 L6），这些几乎全在 CI 盲区。
- **资源泄漏 / 僵尸进程（~5 条）**：`process/runner` 与 xray/hysteria `IsRunning` 对自退进程恒报 true、无 Wait 收尸；集群 gRPC `ClientConn` 每 tick 泄漏；`clientlimit.drainEnd` map 只写不清无界增长；测试里 `ForwardManager` 真实监听不 Close。
- **安全（~7 条）**：RPC 加密层无 KDF（短/空 token → 低熵/公开常量密钥）、无重放防护/无 AAD（可整帧重放或跨方法剪接）；`CenterNodeServer` 完全无鉴权且明文（违反双向 token 约束，可投毒节点目录）；`/sub` 公共端点 SSRF（ext_sub / *_url 透传任意 URL）；`fetchClashTemplate` 把第三方返回任意 YAML 原样下发；xray `FastAddInbound security=none` 生成无凭据 inbound。
- **转发数据面正确性（FWD×7）**：TCP 半关闭 drain 用本地快照 deadline、活跃反向流量被 2s 强断（×2）；限速在未设限时短路绕过、`limitedReader` 先扣 token 短读不退还烧预算（×2）；默认仅绑 IPv4 违反双栈约束；`acceptLoop` 对 EMFILE 无退避空转 100% CPU。
- **证书续期原子性（CERT×3）**：`SaveCert` 四文件就地覆盖非原子（热加载可拿到 crt/key 不匹配对）；`rpc_adapter` 导入绕过 per-domain 锁与自动续期并发写；`challenge.type="dns"` 别名被静默降级为 HTTP-01 监听 :80。
- **集群同步一致性（UM×2）**：`ReleaseBindPort` 释放单端口却删用户**全部**转发规则；`SyncUpsertUser` token 冲突再生后仍采纳远端 Hash，AuthToken 跨节点分叉且 digest 无法自愈。
- **启动/降级静默失败（~4 条）**：`cmd/server.go` RPC/HTTP server 在 goroutine 里起、绑定失败静默、节点降级为无管理面僵尸；CLI 30 个 API 封装忽略 HTTP 状态码把 4xx/5xx 当成功；appconfig legacy 迁移提前 return 绕过 jwt_secret 生成致 `/login` 失效。
- **探测/采集（COL×4）**：ICMP 间隔/超时配置被忽略用全局常量、域名 ICMP 结果被每次 reload 误删清零、`pinger.Run()` 丢错致无 CAP_NET_RAW 时静默失败、`Stop` 把 `isRunning` 误置 true。
- **测试守护缺失（L6，10 条 P1）**：见 §5。

## 5. L6 测试层结论（关键）

测试层直接印证了“为什么上面这些缺陷能长期存在”：

- **CI 根本不跑真链路**：`.github/workflows/ci.yml:52` 只有 `go test ./... -count=1`，**未开 `-race`、未带 `-tags=integration`**。全仓 10+ 条已确认数据竞争即使补了并发测试也测不到；整个 systemtest 协议矩阵（23/24 文件带 integration tag）在 CI 零执行。
- **订阅链路不变量被 VLESS 违反**：`mihomo_vless_matrix_test.go` 是**唯一**没有 `subscription_chain` 子测试的协议矩阵，10 个 case 全用手写 `buildVLESSClashProxy` 绕过 converter；而 VLESS 恰是转换面最复杂的协议（reality/vision/xhttp）。vmess 已有“矩阵绕过 converter 漏检 reality P0”的历史教训（见该文件注释），VLESS 目前无系统级防守。**与项目 memory `feedback_systemtest_subscription_chain` 直接吻合。**
- **高危模块零测试**：`pkg/rpc/client`（集群 RPC 扇出，692 行）、`pkg/collecter(+ping)`（11 源文件）、`pkg/proxy/containers/snell`（959 行）整目录零测试，且正是多条已确认并发/资源 P1 的现场。
- **测试固化了缺陷**：`ratelimit_test.go` 把已确认 P1 的限速 bypass 行为写成断言（修复反而会让测试变红）。

## 6. 验证可信度与注意事项（务必阅读）

- **对抗性两级流程**：所有 finding 经独立 verifier 复核，refuted 已剔除（共 7 条，附在各层文档末）。verifier 常做 severity 修正与行号纠偏。
- **L4/L5/L6 确认率偏高**：这三层 verifier confirmed 率接近 100%（做了 severity 下调但极少 refuted）。可能是 finder 精度高，也可能是 verifier 对本层不够苛刻——**建议对 L4/L5/L6 的 P2/P3 尾部人工抽检**。
- **L1/L2 第二轮质疑**：17/17 裁定 pass1-wrong，经核实**主要是第一轮自身的重复条目（同缺陷两行号）与个别过 severity**，非大面积误报；裁决理由已落在 pass2 文档，**未自动改写第一轮**，需人工合并。
- **uncertain（28 条）**：多为第三方协议/上游行为无法在仓库内确证者，处理前需人工对照上游（xray/mihomo/lego/ACME 等）核实。
- **注入 digest 保守处理**：给上层 reviewer 的下层结论 digest **未按第二轮质疑自动删条**（曾观察到模糊匹配误删未被质疑的 P1），宁可多注入不漏注入。
- **不是修复清单**：本套文档只描述问题与证据，**未按项目规则“先描述方案等确认再动手”**。处理时仍须遵守该规则与 P0→P1→P2→P3 顺序。

## 7. 建议处理顺序

1. **P0（2 条）**：先修集群 `NodeManager`/`Node` 并发安全（连带 CLU/RPC 并发簇一起做）、hysteria 容器生命周期。
2. **打开 CI 守护**：`go test -race` + 一条 integration/nightly 通道跑 systemtest 矩阵——否则并发类修复无法回归验证。补 VLESS `subscription_chain` 子测试。
3. **P1 按主题成簇修**（§4 的 8 个主题），安全簇（RPC 鉴权/加密、SSRF、模板注入）建议紧随 P0。
4. **P2/P3**：一致性/清理，可与上述并行或延后；先对 L4/L5/L6 尾部做人工抽检再批量处理。
5. 合并 L1/L2 第一轮与第二轮，去掉重复条目、采纳 severity 调整。

## 8. 文档索引

| 文件 | 内容 |
|------|------|
| [`00-overview.md`](00-overview.md) | 方法论、分层顺序、七维清单、优先级定义 |
| [`01-module-maps.md`](01-module-maps.md) | 21 个模块结构地图 |
| [`10-legacy-status.md`](10-legacy-status.md) | 2026-04-09 旧 review 遗留状态核对 |
| [`L0-foundation.md`](L0-foundation.md) | L0 基础设施（log/err/config/tools/process） |
| [`L1-core.md`](L1-core.md) + [`L1-pass2.md`](L1-pass2.md) | L1 契约/持久（store/contracts/params/inbound/container/subscription）+ 第二轮 |
| [`L2-domain.md`](L2-domain.md) + [`L2-pass2.md`](L2-pass2.md) | L2 核心域（forward/usermanager/xrayapi/certmgmt）+ 第二轮 |
| [`L3-containers.md`](L3-containers.md) | L3 容器（xray/mihomo/hysteria/snell/base） |
| [`L4-cluster.md`](L4-cluster.md) | L4 集群/RPC（cluster/rpc/collecter） |
| [`L5-interface.md`](L5-interface.md) | L5 接口（http/cmd/cli） |
| [`L6-tests.md`](L6-tests.md) | L6 测试（systemtest 真链路/单测缺口/CI 安全） |
| `L*-digest.json` | 各层 P0/P1 confirmed 摘要（机器可读，用于跨层注入） |
