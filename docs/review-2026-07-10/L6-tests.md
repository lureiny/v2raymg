# L6 测试层 review — systemtest 真链路 / 单测覆盖 / CI 安全

> Finder → 对抗性 verifier 两级流程产出。下表为**保留**（confirmed + uncertain）的 findings，已剔除 verifier 判定 refuted 的 2 条。uncertain 多为依赖第三方协议/上游行为、无法在仓库内确证的条目，处理前需人工核对。

## 统计

| 维度 | 数量 |
|------|------|
| 保留条目 | 35 |
| — confirmed | 35 |
| — uncertain | 0 |
| — 其它(unverified) | 0 |
| refuted(已剔除) | 2 |

| 优先级 | P0 | P1 | P2 | P3 |
|--------|----|----|----|----|
| 保留条目 | 0 | 10 | 16 | 9 |

## 速查表

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P1 | ✓⚠️协议 | 测试缺口 | TEST-CHAIN | `pkg/proxy/systemtest/mihomo_vless_matrix_test.go:283` | VLESS 矩阵是唯一没有 subscription_chain 子测试的协议矩阵，全部用手写 buildVLESSClashProxy 绕过 converter |
| 2 | P1 | ✓ | 测试缺口 | TEST-GAPS | `pkg/cluster/node_manager.go:97` | 已确认 P0（NodeManager 无锁读 map 导致整进程崩溃）零回归测试，cluster_test.go 全部为串行 happy-path |
| 3 | P1 | ✓ | 测试缺口 | TEST-GAPS | `.github/workflows/ci.yml:52` | CI 只跑 `go test ./... -count=1`，未开 -race：全仓 10+ 条已确认数据竞争 P0/P1 即使补了并发测试也测不出来 |
| 4 | P1 | ✓⚠️协议 | 并发 | TEST-GAPS | `pkg/rpc/client/end_node_rpc.go:458` | pkg/rpc/client（集群 RPC 扇出客户端，692 行）整目录零测试，且正是多条已确认并发 P1 的调用现场 |
| 5 | P1 | ✓⚠️协议 | 安全 | TEST-GAPS | `pkg/rpc/server/end_node_server.go:137` | RPC 鉴权链路（authRemoteNode 拦截器 + methodRspMap 共享单例 + CenterNodeServer 无鉴权）没有任何测试 |
| 6 | P1 | ✓ | 正确性 | TEST-GAPS | `pkg/proxy/forward/ratelimit_test.go:25` | TestTokenBucketLimiter_LimitReaderPassthrough 把已确认 P1 缺陷行为固化为断言，修复限速 bypass 必先改此测试 |
| 7 | P1 | ✓ | 测试缺口 | TEST-GAPS | `pkg/collecter/ping/ping.go:222` | pkg/collecter + pkg/collecter/ping 共 11 个源文件零测试，其中 3 条已确认 P1 均为一行断言即可抓住的 bug |
| 8 | P1 | ✓⚠️协议 | 架构 | TEST-GAPS | `pkg/proxy/containers/snell/container.go:356` | snell 容器 5 源文件（959 行）零测试，同层 xray/mihomo/hysteria 均有测试；已确认 send-on-closed panic P1 无保护 |
| 9 | P1 | ✓ | 测试缺口 | TEST-GAPS | `pkg/proxy/usermanager/usermanager.go:2233` | GetAllDeltaTraffic/DrainStats/GetStats（两条已确认 P1：并发 map 崩溃 + tick 丢流量）在全部 9 个 usermanager 测试文件中零调用 |
| 10 | P1 | ✓ | 资源 | TEST-QUALITY | `pkg/rpc/server/end_node_rotate_test.go:15` | newRotateTestServer 创建真实 ForwardManager 但从不 Close,泄漏 30000-40000 段真实监听——commit 5454539 修复漏掉的同类问题 |
| 11 | P2 | ✓⚠️协议 | 架构 | TEST-CHAIN | `pkg/proxy/systemtest/e2e_server_helpers_test.go:616` | E2E 全容器矩阵客户端侧用 320 行手写 uriToClashProxy 复制 converter 逻辑，ClashConverter 漂移不会被唯一的 HTTP-API 级 E2E 捕获 |
| 12 | P2 | ✓⚠️协议 | 测试缺口 | TEST-CHAIN | `.github/workflows/ci.yml:52` | CI 只跑 go test ./...（无 -tags=integration），整个 systemtest 真链路矩阵在 CI 中零执行 |
| 13 | P2 | ✓⚠️协议 | 测试缺口 | TEST-CHAIN | `pkg/proxy/systemtest/e2e_server_test.go:208` | E2E 矩阵永远以空 hystBin/snellBin 启动服务器，snell 协议在整个 systemtest 包中零真实链路覆盖 |
| 14 | P2 | ✓ | 正确性 | TEST-CHAIN | `pkg/proxy/systemtest/mihomo_protocol_matrix_test.go:207` | RemoveUser 负控制复用带连接池的 httpClient，keep-alive 连接可能在规则释放后仍存活导致负控制假失败（flaky） |
| 15 | P2 | ✓ | 错误处理 | TEST-CHAIN | `pkg/proxy/systemtest/xray_fastadd_connectivity_test.go:157` | TestFastAddConnectivity 依赖 AutoDownload 拉取 xray 二进制，无网络时 t.Fatalf 而非 skip，与包内 mihomo 测试的 CI-safe skip 语义不一致 |
| 16 | P2 | ✓ | 测试缺口 | TEST-CHAIN | `pkg/proxy/systemtest/xray_protocol_matrix_test.go:375` | 三个 //go:build ignore 僵尸文件永不编译，其中 TestXrayDynamicInbound_Concurrent 的并发 AddInbound 覆盖并未被替代文件继承 |
| 17 | P2 | ✓ | 测试缺口 | TEST-GAPS | `pkg/proxy/core/container/base_test.go:226` | TestBaseContainer_Concurrent_StartStop 的形状抓不住 base.go 三条已确认 Start/Stop 竞态 P1，最终态断言给了虚假信心 |
| 18 | P2 | ✓⚠️协议 | 测试缺口 | TEST-GAPS | `pkg/proxy/containers/hysteria/container.go:74` | hysteria 容器唯一单测只覆盖 FastAdd 配置，P0 certWaitStopCh 竞态与「run() 立即返回 nil 假 Running」生命周期完全无测试 |
| 19 | P2 | ✓ | 测试缺口 | TEST-GAPS | `pkg/certmgmt/lego/cert_store.go:44` | certmgmt 两条已确认 P1 所在文件（cert_store.go、rpc_adapter.go）恰好是该模块仅有的无测试文件 |
| 20 | P2 | ✓ | 测试缺口 | TEST-GAPS | `pkg/proxy/usermanager/usermanager.go:1361` | ReleaseBindPort「释放一个端口却删光用户全部转发规则」的 P1 路径，现有测试全部只用单端口场景，缺陷被测试绿灯掩盖 |
| 21 | P2 | ✓ | 测试缺口 | TEST-GAPS | `pkg/proxy/systemtest/xray_protocol_matrix_test.go:375` | 全仓唯一的并发 inbound 添加测试 TestXrayDynamicInbound_Concurrent 被 //go:build ignore 禁用，宣称的替代文件没有并发用例 |
| 22 | P2 | ✓ | 正确性 | TEST-QUALITY | `pkg/proxy/containers/xray/exec_runner_startup_test.go:57` | TestExecutor_EnsureBinary_AutoDownloadNoUpdater 用 `_ = err` 丢弃结果,零断言,且注释宣称的行为与实现相反 |
| 23 | P2 | ✓ | 并发 | TEST-QUALITY | `pkg/proxy/usermanager/usermanager_test.go:861` | 三个 TrafficStats 测试依赖固定 50ms sleep 等待异步 ForceCollect goroutine,无轮询,同包已有的 waitFor 模式未复用 |
| 24 | P2 | ✓ | 并发 | TEST-QUALITY | `pkg/proxy/forward/relay_test.go:119` | TestRelay_MultipleConnections 用固定 50ms/200ms sleep 后精确断言 ActiveConnections==5/==0,调度敏感 |
| 25 | P2 | ✓ | 并发 | TEST-QUALITY | `pkg/proxy/forward/relay_test.go:548` | TestClientLimiter_CleanupExpiredSlots 对 1s 回收定时器只留 100ms 余量,定时器延迟即失败 |
| 26 | P2 | ✓ | 错误处理 | TEST-QUALITY | `pkg/proxy/containers/xray/testmain_test.go:122` | startTestXray 固定端口 62779,启动失败/端口被占时仍返回非空 addr,后续测试 fail 而非 skip |
| 27 | P3 | ✓ | 并发 | TEST-CHAIN | `pkg/proxy/systemtest/mihomo_e2e_test.go:298` | runE2EProtocolCase 在子测试内 Stop/Start 共享的 rig.container，中途失败会让后续协议子测试在容器已停状态下产生误导性级联失败 |
| 28 | P3 | ✓ | 正确性 | TEST-CHAIN | `pkg/proxy/systemtest/mihomo_hy2_matrix_test.go:26` | waitUDPListener 用 UDP Dial 作 readiness gate 实际是 no-op：无监听者时 connect 也立即成功 |
| 29 | P3 | ✓ | 资源 | TEST-CHAIN | `pkg/proxy/systemtest/e2e_server_helpers_test.go:432` | e2eServer.shutdown 的 cmd.Wait() 无超时，v2raymg 若在 SIGTERM 后挂住会阻塞 t.Cleanup 直到整个 go test -timeout 爆炸 |
| 30 | P3 | ✓ | 资源 | TEST-CHAIN | `pkg/proxy/systemtest/helpers_test.go:68` | freeTCPPort/freeUDPPort bind-then-close 分配存在 TOCTOU 竞态，是全包（尤其 e2e 每 server 4+ 端口）的 CI flake 源 |
| 31 | P3 | ✓ | 测试缺口 | TEST-CHAIN | `pkg/proxy/systemtest/README.md:34` | README 引用已删除的 xray_socks5_system_test.go/TestXrayContainerSocks5WebsiteAccess，且未覆盖 mihomo 矩阵、E2E server、HYSTERIA_BIN 等现有入口 |
| 32 | P3 | ✓ | 架构 | TEST-GAPS | `pkg/buildinfo/buildinfo.go:1` | 判定为可接受的无测试目录：buildinfo、cmd/cli/common、prometheus_desc、xrayapi/grpc、core/inbound——不建议为凑约束 #9 补测试 |
| 33 | P3 | ✓ | 测试缺口 | TEST-QUALITY | `pkg/proxy/containers/xray/testmain_test.go:48` | findXrayBinary 不支持 XRAY_BIN 环境变量也不查 PATH,xray 包集成测试在几乎所有环境静默 skip |
| 34 | P3 | ✓ | 错误处理 | TEST-QUALITY | `pkg/proxy/systemtest/mihomo_helpers_test.go:60` | ensureMihomoBin 在 GitHub 下载失败时 t.Fatalf 而非 t.Skip,与同包 hysteria/公网探测的 skip 语义不一致 |
| 35 | P3 | ✓ | 并发 | TEST-QUALITY | `pkg/proxy/forward/ratelimit_test.go:487` | TestTokenBucketLimiter_WaitNUnlimited 对 wall-clock 上界断言 50ms,CPU 饥饿的 CI 上可能偶发超时 |

## 详细条目

### 1. [P1·confirmed] VLESS 矩阵是唯一没有 subscription_chain 子测试的协议矩阵，全部用手写 buildVLESSClashProxy 绕过 converter

- **位置**：`pkg/proxy/systemtest/mihomo_vless_matrix_test.go:283` · 维度：测试缺口 · 单元：TEST-CHAIN · ⚠️协议相关（需人工核对上游）
- **问题**：项目不变量要求每个 mihomo 协议矩阵至少一条 cross-cutting 用例走 GetUserSubscriptions→ClashConverter→spawnMihomoClient。vmess/trojan/ss/hy2/tuic/anytls 六个矩阵均已实现（如 mihomo_tuic_matrix_test.go:81 subscription_chain_tls_bbr），唯独 Phase 1 的 TestMihomoVLESSMatrix 10 个 case 全部通过 buildVLESSClashProxy 手搓 clash proxy map（line 283 调用、301 定义），文件甚至未 import converter 包，converter 中也没有 ConvertVLESSForTest 导出。VLESS 恰是转换面最复杂的协议（reality/xtls-rprx-vision flow/xhttp/splithttp 别名折叠/grpc），而 vmess 的历史教训（mihomo_vmess_matrix_test.go:146-149 注释记录的 convertVMess 缺 reality 分支 P0，因矩阵绕过 converter 而漏检）说明这类回归只有订阅链路子测试能守住。convertVLess 的 reality/xhttp 回归目前完全无系统级防守。
- **证据**：line 283: proxy := buildVLESSClashProxy(tc, uuid, "127.0.0.1", int(forwardPort))；全文件无 converter import（grep converter 在该文件 0 命中）；对照 mihomo_vmess_matrix_test.go:150 t.Run("subscription_chain_reality", ...)
- **核实**：读取 mihomo_vless_matrix_test.go 全文核实：10 个 case 全部经 runVLESSMatrixCase→buildVLESSClashProxy(line 283 调用、301 定义)手搓 clash map，import 块（line 5-14）无 converter 包。对照 grep：vmess(150 subscription_chain_reality)/ss(72)/tuic(81)/trojan(82,91)/hy2(97)/anytls(100) 六个矩阵均有 subscription_chain 子测试并调用 Convert*ForTest；converter/clash.go 只导出 ConvertVMess/Trojan/SS/Hysteria2/Tuic/AnyTLSForTest，无 VLESS 版本。全 systemtest 包 grep 无任何 convertVLess 系统级调用（仅 converter 包内单测 clash_protocol_test.go）。vmess 历史教训注释属实（mihomo_vmess_matrix_test.go:146-149 记录 convertVMess 缺 reality 分支的 P0 因矩阵绕过 converter 而漏检）。项目 memory 明确不变量'至少一条 cross-cutting 走 GetUserSubscriptions→Convert→spawnMihomoClient，不要用手写 buildXxxClashProxy 绕过 converter'，VLESS 是唯一违反者，P1 恰当。

### 2. [P1·confirmed] 已确认 P0（NodeManager 无锁读 map 导致整进程崩溃）零回归测试，cluster_test.go 全部为串行 happy-path

- **位置**：`pkg/cluster/node_manager.go:97` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：node_manager.go:97 Get、:38 HaveNode、:105 GetAllNode 均不取 nm.lock 直接读 map，与 Add/Delete/Filter 并发即触发 fatal: concurrent map read and map write（下层 CLU P0）。pkg/cluster/cluster_test.go 共 333 行、23 个测试，全部是单 goroutine 的字段/校验断言（IsValid/IsSameCluster/AuthRemoteNode/LoadStaticNode），无一处 go func/WaitGroup。修复该 P0（补 RLock）后没有任何测试能防止回退；一个 10 goroutine 混合 Get/Add/Delete + -race 的测试即可锁定。
- **证据**：node_manager.go:97-102 `func (nm *NodeManager) Get(nodeName string) *Node { if n, ok := (*nm.nodes)[nodeName]; ok {...` 无锁；cluster_test.go 全文 grep "go func|sync.WaitGroup" 零命中
- **核实**：亲读 node_manager.go：Get(:97-102)、HaveNode(:38-41)、GetAllNode(:105-107，直接返回 *nm.nodes)均不取 nm.lock，而 Add(:32)/Delete(:45)/Filter(:138)/Clear(:91) 持写锁，并发混用必触发 concurrent map read and map write。cluster_test.go 全文 333 行 grep 'go func|sync.WaitGroup' 零命中，22 个 Test 函数全部串行；pkg/cluster 目录仅此一个测试文件。修 P0 后确无回归测试兜底，P1 定级恰当。

### 3. [P1·confirmed] CI 只跑 `go test ./... -count=1`，未开 -race：全仓 10+ 条已确认数据竞争 P0/P1 即使补了并发测试也测不出来

- **位置**：`.github/workflows/ci.yml:52` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：下层已确认的数据竞争包括：CLU node_manager P0、hysteria certWaitStopCh P0、snell userEventCh P1、UM UserEvent 活指针 P1、UM GetStats 共享 map P1、RPC methodRspMap 反射写 P1、cluster.Node 心跳字段 P1 等。这些全部依赖 race detector 才能在单测中稳定暴露；当前 CI 命令没有 -race，意味着即便按其他 findings 补齐并发回归测试，CI 也大概率绿灯通过。建议 CI 增加 `go test -race ./...`（或至少对 pkg/cluster、pkg/proxy/usermanager、pkg/proxy/forward、pkg/rpc/server、pkg/proxy/containers/... 子集开启）。
- **证据**：.github/workflows/ci.yml:52 `run: go test ./... -count=1`；仓内 grep "race" 于 workflows/Makefile 零命中
- **核实**：亲读 .github/workflows/ci.yml：第 52 行确为 `run: go test ./... -count=1`，无 -race；grep 'race' 于 .github/workflows/（ci.yml+build.yml）与 Makefile 均零命中。且至少 node_manager.go 无锁读 map 的竞争已实证存在（见 idx 0），此类缺陷不开 race detector 时并发测试大概率静默通过，结论成立。

### 4. [P1·confirmed] pkg/rpc/client（集群 RPC 扇出客户端，692 行）整目录零测试，且正是多条已确认并发 P1 的调用现场

- **位置**：`pkg/rpc/client/end_node_rpc.go:458` · 维度：并发 · 单元：TEST-GAPS · ⚠️协议相关（需人工核对上游）
- **问题**：ReqToMultiEndNodeServer(end_node_rpc.go:458) 对全部节点起 goroutine 扇出，经包级全局信号量 `var ch = make(chan struct{}, 64)`(:94) 限流，goroutine 内读 n.OutToken、调 n.GetGrpcClientConn()——这两处与心跳 goroutine 的无锁读写正是下层 RPC P1（end_node_cluster.go:256/212）和 CLU P1（node.go:39 连接泄漏）的另一半竞争方。该目录 3 个源文件无任何 *_test.go，违反架构约束 #9；getReqAndCallbakcFunc 分发表、失败节点聚合(failedList)、RegisteredRemote 过滤等纯逻辑完全可在单测中用 bufconn/fake 覆盖。
- **证据**：ls pkg/rpc/client → context.go/end_node_rpc.go/end_node_rpc_type.go 无测试；end_node_rpc.go:480 `go func(n *cluster.Node)` 内直接使用 n.OutToken/n.GetGrpcClientConn()
- **核实**：亲验 ls pkg/rpc/client：仅 context.go(14行)/end_node_rpc.go(692行)/end_node_rpc_type.go(39行)，无任何 *_test.go。end_node_rpc.go:94 `var ch = make(chan struct{}, MaxConcurrencyClientNum)` 包级全局信号量、:458 ReqToMultiEndNodeServer、:480 `go func(n *cluster.Node)` 内 :485 调 n.GetGrpcClientConn()、:494 读 n.OutToken，均与描述一致。getReqAndCallbakcFunc(:451)、failedList 聚合(:488/:510)、RegisteredRemote 过滤(:475) 确为可单测纯逻辑。

### 5. [P1·confirmed] RPC 鉴权链路（authRemoteNode 拦截器 + methodRspMap 共享单例 + CenterNodeServer 无鉴权）没有任何测试

- **位置**：`pkg/rpc/server/end_node_server.go:137` · 维度：安全 · 单元：TEST-GAPS · ⚠️协议相关（需人工核对上游）
- **问题**：end_node_server.go:137 `rspValue := reflect.ValueOf(methodRspMap[...])` 反射写包级共享 map 值并由 newEmptyRsp(:116) 跨请求返回同一实例（下层 P1：并发数据竞争+错误信息串号）；center_node_server.go:119 `grpcServer := grpc.NewServer()` 无 interceptor、无 token 校验（下层 P1：可投毒节点目录）。pkg/rpc/server 现有 7 个 _test.go 全部只测 handler 业务逻辑（cluster_user/rotate/inbound/wiring/fastadd），grep authRemoteNode/methodRspMap/CenterNodeServer 在测试中零命中。鉴权是 center/end 架构的安全边界，修复上述两条 P1 时无回归测试兜底。
- **证据**：grep -rn "CenterNodeServer|methodRspMap|newEmptyRsp" pkg/rpc/server/*_test.go 零命中；center_node_server.go:119-121 直接 NewServer+Register 无任何鉴权装配
- **核实**：亲验 grep 'authRemoteNode|methodRspMap|CenterNodeServer|newEmptyRsp' 于 pkg/rpc/server 全部 7 个 _test.go 零命中。end_node_server.go:116-118 newEmptyRsp 直接返回 methodRspMap 中共享单例；:137 `rspValue := reflect.ValueOf(methodRspMap[...])` 后 :138-139 反射写 Code/Msg，跨请求同实例。center_node_server.go:119 `grpcServer := grpc.NewServer()` 无 interceptor 参数，grep interceptor 于该文件零命中，注册后直接 Serve，确无鉴权。

### 6. [P1·confirmed] TestTokenBucketLimiter_LimitReaderPassthrough 把已确认 P1 缺陷行为固化为断言，修复限速 bypass 必先改此测试

- **位置**：`pkg/proxy/forward/ratelimit_test.go:25` · 维度：正确性 · 单元：TEST-GAPS
- **问题**：下层 FWD P1（ratelimit.go:123）：未设限时建立的 TCP 连接因 LimitReader 在包装时刻短路返回原始 reader，事后 SetUserBandwidthLimit 对既有连接永久无效。而 ratelimit_test.go:25-34 恰好断言 `if lr != r { t.Error("unlimited limiter should return original reader") }`（:36 LimitWriter 同理）——即把「返回原始 reader」当作契约保护起来。正确修法（返回持有 limiter 引用的稳定包装器，如同文件 TestUserBandwidthLimiter_StableRef_UnlimitedToLimited 已经对 UserBandwidthLimiter 层要求的那样）会直接被这两个测试打红。应将断言改为「行为透传（无节流）」而非「指针相等」，并新增「wrap 后再 SetRate 生效」的回归测试。
- **证据**：ratelimit_test.go:29-33 `lr := l.LimitReader(r); if lr != r { t.Error(...) }` 与 ratelimit.go:123 的短路实现互为镜像
- **核实**：亲读 ratelimit_test.go:25-34/:36-44：确以指针相等断言 `if lr != r { t.Error(...) }` 固化 unlimited 时返回原始 reader/writer 的行为，与 ratelimit.go:122-127 LimitReader 的 IsUnlimited 短路互为镜像。relay_tcp.go:343-371 copyWithCount 在连接建立时一次性包装 reader（:357/:360），此后 io.CopyBuffer 持有该 reader 至连接结束，unlimited 时拿到的原始 reader 永不感知后续限速。同文件 :340 TestUserBandwidthLimiter_StableRef_UnlimitedToLimited 确对上层要求 stable ref。正确修法（返回持 limiter 引用的包装器）会被这两个 passthrough 测试打红，结论成立。

### 7. [P1·confirmed] pkg/collecter + pkg/collecter/ping 共 11 个源文件零测试，其中 3 条已确认 P1 均为一行断言即可抓住的 bug

- **位置**：`pkg/collecter/ping/ping.go:222` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：pkg/collecter/ping（9 文件 1221 行）与 pkg/collecter 顶层（2 文件 250 行）无任何 *_test.go。已确认缺陷：(1) ping.go:222 `bpc.isRunning.Store(true)` —— Stop() 把 isRunning 置 true（应为 false），停止后状态永远 running、SetPingCheck 变单向开关，一个 `Stop(); if IsRunning() {fail}` 的测试就能抓住；(2) icmp_ping_checker.go:88-89 配置的 interval/timeout 被全局常量覆盖、`go pi.pinger.Run()` 丢弃错误（无 CAP_NET_RAW 时静默全失败）；(3) ping_collector.go:150 cleanupStaleResults 对域名节点误删 100 样本环形缓冲。node_manager/options/file_loader 等纯逻辑文件完全可单测，无外部依赖借口。
- **证据**：ping.go:219-223 `func (bpc *basePingChecker) Stop() { if bpc.cancel != nil && bpc.isRunning.Load() { bpc.cancel(); bpc.isRunning.Store(true) } }`；find pkg/collecter -name '*_test.go' 零结果
- **核实**：亲验 find pkg/collecter -name '*_test.go' 零结果（顶层 2 文件 + ping 目录 9 文件均无测试）。(1) ping.go:219-223 Stop() 确为 `bpc.cancel(); bpc.isRunning.Store(true)`，应为 false，停止后 IsRunning 永真；(2) icmp_ping_checker.go:88 pingLoop 内 `pi.pinger.Interval = time.Second*time.Duration(pingInterval)` 用包常量覆盖 newPingerInfo 传入的 pi.interval（配置的 ipc.interval 在 :176 计算后传入却被丢弃），:89 `go pi.pinger.Run()` 丢弃返回错误；(3) ping_collector.go:135-156 cleanupStaleResults 以 node.Host 建 hostSet，而 ICMP 结果 nodeName 按 :159 注释为 '{geo}_{isp}_{ip}'，域名节点提取出 IP 与 hostSet 中的域名不匹配即误删。三处均一行断言可抓，结论成立。

### 8. [P1·confirmed] snell 容器 5 源文件（959 行）零测试，同层 xray/mihomo/hysteria 均有测试；已确认 send-on-closed panic P1 无保护

- **位置**：`pkg/proxy/containers/snell/container.go:356` · 维度：架构 · 单元：TEST-GAPS · ⚠️协议相关（需人工核对上游）
- **问题**：pkg/proxy/containers/snell 是四个协议容器中唯一完全没有 *_test.go 的（mihomo 有 26 个测试文件、xray 5 个、hysteria 1 个），直接违反架构约束 #9。container.go:356 forwardUserEvents 在独立 goroutine 中裸读 sc.userEventCh 并向其发送，与 closeUserEventCh 无同步（下层 SC P1：停机时 send on closed channel panic 整进程）。config.go 的配置渲染、downloader.go 的版本/校验逻辑、handleUserEvent 的端口绑定路径均为可离线单测的纯逻辑；用户事件 channel 的关停竞态可仿照 hysteria 的场景用 -race 测试锁定。
- **证据**：container.go:356-365 `func (sc *SnellContainer) forwardUserEvents(source <-chan usermanager.UserEvent) { for event := range source { if sc.userEventCh != nil { select { case sc.userEventCh <- event: ...`；ls pkg/proxy/containers/snell 无测试文件
- **核实**：ls 确认 pkg/proxy/containers/snell 仅 5 个 .go 源文件（config 64 + container 703 + downloader 97 + process 62 + register 33 = 959 行）无任何 *_test.go；同层 hysteria 有 container_fastadd_test.go、xray 有 9 个测试文件、mihomo 有 25 个。container.go:356-364 forwardUserEvents 在独立 goroutine 中裸读 sc.userEventCh 并 send，:697-702 closeUserEventCh close 后置 nil，两者无任何锁同步（stop 闭包 :81 调用），send-on-closed panic 竞态真实存在且零测试保护。细节小误差：mihomo 实为 25 个测试文件（非 26）、xray 9 个（非 5），不影响结论。

### 9. [P1·confirmed] GetAllDeltaTraffic/DrainStats/GetStats（两条已确认 P1：并发 map 崩溃 + tick 丢流量）在全部 9 个 usermanager 测试文件中零调用

- **位置**：`pkg/proxy/usermanager/usermanager.go:2233` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：usermanager.go:2233 GetAllDeltaTraffic 先 `agg := m.statsCollector.GetStats()` 拿到共享 map，锁外 range agg.ByUser，再第二次取锁 resetAllDeltas（:2242）——下层两条 P1：与 collect() 并发写导致 concurrent map read/write 崩溃；读取与重置之间插入 tick 会丢流量。grep 全部 pkg/proxy/usermanager/*_test.go（含 bandwidth_stats_collector_test.go）对 GetAllDeltaTraffic/DrainStats/GetStats( 零命中，Prometheus 导出这条主干路径完全没有单测。需要：并发 collect+drain 的 -race 测试，以及「drain 期间新增流量不丢失」的原子性断言。
- **证据**：usermanager.go:2237-2243 `agg := m.statsCollector.GetStats(); ... for _, v := range agg.ByUser { ... } if reset { m.statsCollector.resetAllDeltas() }`；测试目录 grep 零命中
- **核实**：usermanager.go:2233 GetAllDeltaTraffic 确认：:2237 `agg := m.statsCollector.GetStats()`（:1943 GetStats 在 RLock 下返回 sc.stats 结构体浅拷贝，ByUser map 为共享引用），随后锁外 range agg.ByUser，再 :2242 二次取锁 resetAllDeltas——与 collect() 并发写 map 可致 concurrent map read/write 崩溃，且 GetStats 与 resetAllDeltas 两次独立加锁之间插入 tick 会把新增 delta 清零丢失。grep 全部 8 个 usermanager 测试文件对 GetAllDeltaTraffic/DrainStats/GetStats( 零命中；bandwidth_stats_collector_test.go 亲读确认仅 1 个编译期接口断言测试，DrainStats（rpc/server/end_node_user.go:338 的 Prometheus 导出主干）零行为测试。细节小误差：测试文件为 8 个而非 9 个，不影响结论。

### 10. [P1·confirmed] newRotateTestServer 创建真实 ForwardManager 但从不 Close,泄漏 30000-40000 段真实监听——commit 5454539 修复漏掉的同类问题

- **位置**：`pkg/rpc/server/end_node_rotate_test.go:15` · 维度：资源 · 单元：TEST-QUALITY
- **问题**：newRotateTestServer 每次调用 forward.NewDefaultForwardManager(PortAllocatorConfig{MinPort:30000, MaxPort:40000}) 但没有 t.Cleanup(fwdMgr.Close())。本文件 8 个测试中 TestRotateInboundPort_Success(:67)、TestRotateAllPorts_Success(:171)、TestRotateAllPorts_PartialSuccess(:210) 通过 GetBindPort/RotateInboundPort 触发 forward_manager.go:283 NewTCPRelay + relay.Start(),绑定真实 127.0.0.1 监听,测试结束后泄漏到进程退出。这与 commit 5454539("test: make mihomo factory and usermanager rotate tests CI-safe")在 usermanager/testhelper_test.go 和 sync_runtime_effects_test.go 修掉的 EADDRINUSE 泄漏是完全同一模式,该修复漏掉了 rpc/server 这份拷贝。go test ./... 各包测试二进制在同一 CI 机器上并发运行,usermanager 测试同样使用 30000-40000 随机分配,泄漏的监听会放大随机撞端口的偶发失败概率。修法与 5454539 相同:t.Cleanup(func(){ _ = fwdMgr.Close() })。
- **证据**：end_node_rotate_test.go:15-24: fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{MinPort: 30000, MaxPort: 40000}) ... mgr := usermanager.NewUserManager(fwdMgr, "test-node"); return &EndNodeServer{userMgr: mgr} —— 全函数无任何 Close/t.Cleanup;对比 usermanager/testhelper_test.go:19-22 已加 t.Cleanup(func() { _ = fwdMgr.Close() }) 并注明 EADDRINUSE 原因
- **核实**：end_node_rotate_test.go:13-24 核实：newRotateTestServer 创建 NewDefaultForwardManager(30000-40000) 后无任何 Close/t.Cleanup,整个 pkg/rpc/server 也无 TestMain 兜底。usermanager.go:855 GetBindPort 确实走 forwardMgr.AddRule → forward_manager.go:283 NewTCPRelay + relay.Start() 绑定真实 127.0.0.1 监听;TestRotateInboundPort_Success(:61)、TestRotateAllPorts_Success(:163)、_PartialSuccess(:203) 均调用 GetBindPort。对照 usermanager/testhelper_test.go:19-22,commit 5454539 在同构造上已加 t.Cleanup(func(){ _ = fwdMgr.Close() }) 并注明 EADDRINUSE 泄漏原因——本文件是漏掉的同类拷贝。detail 中引用的测试行号略有偏差(Success 在 :61/:163/:203 而非 :67/:171/:210),不影响结论。

### 11. [P2·confirmed] E2E 全容器矩阵客户端侧用 320 行手写 uriToClashProxy 复制 converter 逻辑，ClashConverter 漂移不会被唯一的 HTTP-API 级 E2E 捕获

- **位置**：`pkg/proxy/systemtest/e2e_server_helpers_test.go:616` · 维度：架构 · 单元：TEST-CHAIN · ⚠️协议相关（需人工核对上游）
- **问题**：TestE2EAllContainersProtocolMatrix 是唯一穿过 v2raymg 二进制 + HTTP API + GET /sub 的端到端测试。服务端 URI 生成是真实的，客户端也用了生产 codec.Decode（line 617），但 Node→clash proxy 的映射由 8 个手写 *NodeToProxy 函数（line 643-938）完成，与 converter/clash.go 的 convertXxx 字段级重复：alpn 默认值、reality-opts、xhttp/splithttp 折叠、client-fingerprint fallback 等都是两份独立实现。converter 若与 mihomo schema 漂移，E2E 依旧绿（测试用的是自己的正确映射）；反之测试映射先烂掉会产生假失败。建议至少对每个协议加一条从 /sub 请求 clash 格式（或对 codec Node 反构 SubscriptionSpec 后走 ConvertXxxForTest）的用例，把这 320 行维护面收敛掉。
- **证据**：line 616-641 uriToClashProxy switch 分发到 vlessNodeToProxy/... 8 个手写映射函数（643-938 行），无一处引用 pkg/proxy/core/subscription/converter；模块地图 risks 也标注其为"最易 drift 的代码"
- **核实**：e2e_server_helpers_test.go:616 uriToClashProxy 经 codec.Decode(617) 后 switch 分发到 8 个手写 *NodeToProxy 函数（vlessNodeToProxy:643 至 snellNodeToProxy:922，文件共 939 行），import 块只有 codec 包、无 converter 引用（grep 0 命中）。e2e_server_test.go:308-311 确认矩阵走 getRawSub（URI 列表）→uriToClashProxy，从不请求 clash 格式订阅，故 ClashConverter 在这条唯一穿过 v2raymg 二进制+HTTP API 的 E2E 中零执行——converter 漂移不会使该测试变红，测试自带映射烂掉则产生假失败。字段级重复属实（vlessNodeToProxy 有独立的 tls/servername/skip-cert-verify/reality-opts 分支）。P2 合理（converter 本身另有 mihomo 矩阵 in-process subscription_chain 兜底，非完全裸奔）。

### 12. [P2·confirmed] CI 只跑 go test ./...（无 -tags=integration），整个 systemtest 真链路矩阵在 CI 中零执行

- **位置**：`.github/workflows/ci.yml:52` · 维度：测试缺口 · 单元：TEST-CHAIN · ⚠️协议相关（需人工核对上游）
- **问题**：ci.yml 的 Test step 是 `go test ./... -count=1`，systemtest 包除 degraded_socks5_chain_test.go（无 build tag 的进程内 SOCKS5 玩具链路）外全部带 integration tag，被完全跳过。这意味着：subscription_chain 强制不变量、六个协议矩阵、restore/负控制，全部只在开发者手工运行时生效——协议扩展（Phase 7 anytls 之后）的回归在 CI 无任何守护。mihomo 矩阵已经设计为可自举（ensureMihomoBin 自动从 GitHub 下载 Alpha、TestFastAddConnectivity 的 xray AutoDownload），完全可以加一个 nightly/manual-dispatch job 至少跑 TestMihomoProtocolMatrix + 各 subscription_chain 子测试。建议按严重度补一条最小 CI 通道。
- **证据**：ci.yml:52 `run: go test ./... -count=1`；grep systemtest .github/workflows/*.yml 无命中；systemtest 24 个文件中 23 个带 //go:build integration 或 ignore
- **核实**：ci.yml:51-52 Test step 确为 `go test ./... -count=1`，无 -tags 参数；.github/workflows/ 仅 ci.yml 与 build.yml 两个文件，grep integration/systemtest 均 0 命中。systemtest 包 24 个文件（23 个 _test.go + README）中，21 个带 //go:build integration，2 个带 //go:build ignore（xray_e2e_protocol_test.go、xray_protocol_connectivity_test.go），仅 degraded_socks5_chain_test.go 无 tag 会在 CI 执行。因此全部协议矩阵、subscription_chain 不变量、restore/负控制在 CI 零执行，回归只靠手工运行守护。测试自举能力属实（ensureMihomoBin 自动下载、TestFastAddConnectivity AutoDownload），加 nightly job 可行。P2 恰当。

### 13. [P2·confirmed] E2E 矩阵永远以空 hystBin/snellBin 启动服务器，snell 协议在整个 systemtest 包中零真实链路覆盖

- **位置**：`pkg/proxy/systemtest/e2e_server_test.go:208` · 维度：测试缺口 · 单元：TEST-CHAIN · ⚠️协议相关（需人工核对上游）
- **问题**：startE2EServer(t, xrayBin, mihomoBin, "", "", tmpDir) 硬编码禁用 hysteria/snell 容器，即使操作者设置了 HYSTERIA_BIN/SNELL_BIN 环境变量也不会启用。generateE2EConfig 的 snell 分支（e2e_server_helpers_test.go:206-219）、snellNodeToProxy（:922）等基础设施已经完整写好但从未被任何测试触发（dead test code）。hysteria 至少还有 hysteria_udp_forward_test.go 的独立 UDP 链路测试兜底；snell 容器（含 PSK 生成、订阅 URI、clash 转换）没有任何系统级验证。建议参照 XRAY_BIN 模式：env 存在则启用对应容器并向矩阵追加 case，否则 per-case skip。
- **证据**：e2e_server_test.go:206-208 注释 "hysteria/snell disabled for brevity" + `srv := startE2EServer(t, xrayBin, mihomoBin, "", "", tmpDir)`；buildE2EMatrix() 中无 snell case；grep 显示 snellNodeToProxy 仅被 uriToClashProxy 的 switch 引用、运行时不可达
- **核实**：e2e_server_test.go:206-208 注释'hysteria/snell disabled for brevity'+ `srv := startE2EServer(t, xrayBin, mihomoBin, "", "", tmpDir)` 硬编码空串，全包 grep 无 SNELL_BIN 读取（HYSTERIA_BIN 仅在 hysteria_udp_forward_test.go:136 独立测试中使用）。startE2EServer(helpers:298) 的 snellBin!="" 分支（354-361）、generateE2EConfig 的 Type:"snell" 容器分支（206-219 区域实测在 207-219）、snellNodeToProxy(:922) 基础设施完整存在；buildE2EMatrix()(e2e_server_test.go:54-171) 逐行核实无任何 snell case，snellNodeToProxy 仅被 uriToClashProxy switch 引用、运行时不可达（dead test code）。snell 全包零真实链路覆盖属实，hysteria 有独立 UDP 测试兜底属实。P2 恰当。

### 14. [P2·confirmed] RemoveUser 负控制复用带连接池的 httpClient，keep-alive 连接可能在规则释放后仍存活导致负控制假失败（flaky）

- **位置**：`pkg/proxy/systemtest/mihomo_protocol_matrix_test.go:207` · 维度：正确性 · 单元：TEST-CHAIN
- **问题**：runMihomoProtocolCase 的正向 GET（line 185）与 RemoveUser 后的负向 GET（line 207）使用同一个 httpClient。http.Transport 默认启用 keep-alive，正向请求建立的 TCP 链路（socks→forward relay→mihomo→origin）在 RemoveRule 后是否被切断取决于 forward relay 是否主动关闭既有连接；若不切断，负向 GET 复用池中连接会成功，触发 line 210 t.Fatalf——这是环境敏感的 flake 源。同包 mihomo_e2e_test.go:278-289 明确为同一问题禁用了 DisableKeepAlives 并写了大段注释，此文件没有做。另外 line 205-206 注释声称 "Short per-connection timeout" 但代码并未设置任何 timeout（继承 helpers 的 15s 默认）。修复：负向前 tr.CloseIdleConnections() 或新建 DisableKeepAlives 的 client。
- **证据**：line 181-207：httpClient 由 httpClientViaSocks5 创建后先做正向 GET，再直接 `negResp, negErr := httpClient.Get(targetURL)`；对照 mihomo_e2e_test.go:287-289 `tr.DisableKeepAlives = true` 及其注释 "A pooled keep-alive connection ... could succeed even after the underlying chain is broken"
- **核实**：mihomo_protocol_matrix_test.go:181 httpClientViaSocks5 创建 client，185 正向 GET（读完 body 并 Close，连接会归还空闲池），202 RemoveUser 后 207 直接用同一 client 负向 GET，全程无 DisableKeepAlives/CloseIdleConnections。helpers_test.go:93-103 核实 httpClientViaSocks5 返回裸 &http.Transport{}（keep-alive 默认开启）+ 15s Timeout。若 forward relay 释放规则后不主动切断既有 TCP 链路，池中连接复用可成功→触发 210 t.Fatalf，为环境敏感 flake。对照 mihomo_e2e_test.go:271-289 确认同包已为完全相同的问题写了大段注释并设 tr.DisableKeepAlives=true，本文件缺失该防护。附带问题也属实：204-206 注释声称'Short per-connection timeout'但代码未设置任何 timeout（继承 15s 默认）。P2 恰当。

### 15. [P2·confirmed] TestFastAddConnectivity 依赖 AutoDownload 拉取 xray 二进制，无网络时 t.Fatalf 而非 skip，与包内 mihomo 测试的 CI-safe skip 语义不一致

- **位置**：`pkg/proxy/systemtest/xray_fastadd_connectivity_test.go:157` · 维度：错误处理 · 单元：TEST-CHAIN
- **问题**：该测试是 xray 侧 14 组合的权威覆盖，serverExec 用 AutoDownload:true（line 148）在 Start() 时从网络下载 xray。没有任何前置探测：无 XRAY_BIN 且无 egress 的环境里 Start 失败直接 t.Fatalf（line 157），而不是 skip。包内其他测试的惯例是先探测再 skip（mihomo 矩阵：`os.Getenv("MIHOMO_BIN") == "" && !probeURL("https://github.com")` → t.Skip；xray_dynamic_inbound_system_test.go：XRAY_BIN 未设 → t.Skip）。checkNetwork 的结果（line 122）只用于 reality 子 case 的 skip，不守护下载路径。建议对齐：支持 XRAY_BIN 直用 + 无 bin 且 github 不可达时 skip。
- **证据**：line 143-158：`AutoDownload: true` + `if err := serverExec.Start(); err != nil { t.Fatalf("start server: %v", err) }`，无 probeURL/XRAY_BIN 前置；对照 mihomo_vless_matrix_test.go:35-37 的 skip 门
- **核实**：xray_fastadd_connectivity_test.go:133 `xrayBin := filepath.Join(tmpDir, "xray")` 指向全新 TempDir（二进制必不存在），完全不读 XRAY_BIN 环境变量；144-150 ExecutorConfig AutoDownload:true；155-158 `if err := serverExec.Start(); err != nil { t.Fatalf(...) }`——无网络时下载失败直接 Fatalf 而非 Skip。line 122 hasNetwork := checkNetwork()==nil 确实只在 169-170 用于 reality 子 case 的 skip，不守护下载路径。对照包内惯例属实：mihomo_vless_matrix_test.go:35-37 MIHOMO_BIN+probeURL 双条件 skip 门；xray_dynamic_inbound_system_test.go 用 XRAY_BIN 未设即 skip。即使设置了 XRAY_BIN 该测试也照样走下载，语义不一致确凿。t.Fatalf 实际在 157 行，Start 调用在 156 行，原行号锚点可接受。因 CI 本就不跑 integration tag（idx 2），影响面限于无 egress 的手工运行环境，P2 偏上限但可接受。

### 16. [P2·confirmed] 三个 //go:build ignore 僵尸文件永不编译，其中 TestXrayDynamicInbound_Concurrent 的并发 AddInbound 覆盖并未被替代文件继承

- **位置**：`pkg/proxy/systemtest/xray_protocol_matrix_test.go:375` · 维度：测试缺口 · 单元：TEST-CHAIN
- **问题**：xray_protocol_matrix_test.go、xray_e2e_protocol_test.go、xray_protocol_connectivity_test.go 均带 //go:build ignore + FIXME 头，声明"coverage superseded by xray_fastadd_connectivity_test.go"。但 fastadd 文件是纯顺序执行（无 goroutine/t.Parallel），line 375 的 TestXrayDynamicInbound_Concurrent（10 goroutine 并发 AddInbound + errCh 收集）没有任何替代——xray gRPC 动态 inbound 的并发安全性覆盖实际丢失了，supersede 声明不准确。ignore 文件不参与编译也不受重构约束，只会持续腐化。建议：把并发用例移植到 fastadd 文件（用当前 contracts API 重写），然后删除三个僵尸文件。
- **证据**：xray_protocol_matrix_test.go:1 `//go:build ignore`、:375 `func TestXrayDynamicInbound_Concurrent`；grep xray_fastadd_connectivity_test.go 无 goroutine/errCh/Parallel；FIXME 头（line 3-5）仅提及 UserSpec 字段过时，未说明并发覆盖去向
- **核实**：属实。三个文件头部均为 //go:build ignore + FIXME，且 xray_protocol_matrix/xray_protocol_connectivity 的 FIXME 声称 "Coverage superseded by xray_fastadd_connectivity_test.go"。TestXrayDynamicInbound_Concurrent 确在 xray_protocol_matrix_test.go:375，用 goroutine+errCh 并发调 exec.AddInbound；grep 确认 xray_fastadd_connectivity_test.go 中 go func/t.Parallel/errCh/WaitGroup 计数为 0，全包其余 go func 仅为 degraded_socks5_chain/hysteria_udp_forward 的辅助 server，pkg/proxy 下也无其他针对真实 xray gRPC AddInbound 的并发测试——supersede 声明确实不覆盖并发用例。细节小误：并发度是 numInbounds=5 个 goroutine，不是 10 个；且该文件在 FIXME 前就已编译不过（"already broken before this change"），即并发覆盖在本次 supersede 之前已经死亡，finding 的核心结论（声明不准确、覆盖丢失、僵尸文件腐化）仍成立。

### 17. [P2·confirmed] TestBaseContainer_Concurrent_StartStop 的形状抓不住 base.go 三条已确认 Start/Stop 竞态 P1，最终态断言给了虚假信心

- **位置**：`pkg/proxy/core/container/base_test.go:226` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：base_test.go:226 的并发测试只是 10 个 goroutine 各自顺序执行 Start→sleep(1ms)→Stop，最后断言 State==Stopped——对 base.go:153（Start 在 Stopping 态不等待）、:192（Start 成功路径不校验状态即缓存 stopFunc，runFunc 期间被 Stop 打断则进程孤儿化）、:270（MarkStopped 无条件覆盖并发 Start 置的 Running）这三条 P1 而言，触发条件是「Stop 进行中时 Start 插入」的特定交错，且症状是孤儿进程/双进程而非最终 State 值，该测试既造不出交错也断言不到症状。需要用可阻塞的 hook（stopFunc 卡在 channel 上时并发 Start）编排 Stopping→Start 交错，断言 Start 的返回值/最终 stopFunc 归属/hooks 调用次数。
- **证据**：base_test.go:230-247：goroutine 内 `bc.Start(); time.Sleep(1ms); bc.Stop()`，唯一断言 `bc.State() != ContainerStateStopped`；无阻塞 hook、无对 stopFunc/进程句柄的断言
- **核实**：base_test.go:226-247 亲读确认：10 个 goroutine 各自 `bc.Start(); time.Sleep(1ms); bc.Stop()`，唯一断言为最终 `bc.State() != ContainerStateStopped`，无阻塞 hook、无 stopFunc/进程句柄断言。base.go 亲读确认三条竞态均存在：Start 的 Stopping 分支（~:153）不等待旧 stop 完成即置 Starting；Start 成功路径（~:192）runFunc 返回后无条件缓存 stopFunc——若 Stop 在 runFunc 期间插入（Starting→Stopping→Stopped），MarkRunning 因状态非 Starting 不生效但进程已被 runFunc 拉起，后续 Stop 在 Stopped 态直接返回不调 stopFunc → 孤儿进程；MarkStopped（~:270）无条件置 Stopped。这些交错的症状是孤儿/双进程而非最终 State 值，mock hooks 即时返回也造不出 Stopping 窗口，该测试确实抓不住，最终态断言给虚假信心成立。

### 18. [P2·confirmed] hysteria 容器唯一单测只覆盖 FastAdd 配置，P0 certWaitStopCh 竞态与「run() 立即返回 nil 假 Running」生命周期完全无测试

- **位置**：`pkg/proxy/containers/hysteria/container.go:74` · 维度：测试缺口 · 单元：TEST-GAPS · ⚠️协议相关（需人工核对上游）
- **问题**：container.go:74 `h.c.certWaitStopCh = make(chan struct{})` 在 run 闭包内无锁写、stop 闭包(:85)closeCertWaitStopCh 并发读关（下层 HC P0：停止后后台协程仍可能启动进程造成孤儿进程）；:77-79 waitForCertAndStart 放后台后 run 立即返回 nil，证书等待/进程启动失败容器仍标记 Running（HC P1）。而 pkg/proxy/containers/hysteria 唯一的测试文件 container_fastadd_test.go 只有 1 个 TestFastAddInbound_LegacyPath_Regression，Start/Stop/waitForCertAndStart/userEventCh(:454 P1) 全部裸奔。cert-wait 逻辑可注入 fake 证书源在单测中编排「Stop 先于证书就绪」的交错。
- **证据**：container.go:73-77 run 闭包内 `h.c.certWaitStopCh = make(chan struct{}); go h.c.waitForCertAndStart(); return nil`；grep -n 'func Test' container_fastadd_test.go 仅 1 个测试
- **核实**：container.go:74 亲读确认 run 闭包内无锁写 `h.c.certWaitStopCh = make(chan struct{})`，:77 `go h.c.waitForCertAndStart()` 后 :79 立即 return nil（BaseContainer 随即标记 Running，证书等待/进程启动失败仍显示 Running）；stop 闭包 :85 调 closeCertWaitStopCh（:241-250，无锁 select-default 关闭，只防双关不防与 run 的写竞态，waitForCertAndStart 在 :225 检查通过后到启动进程之间 Stop 插入仍可产生停止后拉起的孤儿进程）。grep 确认 pkg/proxy/containers/hysteria 唯一测试文件 container_fastadd_test.go 仅 1 个 TestFastAddInbound_LegacyPath_Regression，Start/Stop/waitForCertAndStart/userEventCh(:454) 全无覆盖。

### 19. [P2·confirmed] certmgmt 两条已确认 P1 所在文件（cert_store.go、rpc_adapter.go）恰好是该模块仅有的无测试文件

- **位置**：`pkg/certmgmt/lego/cert_store.go:44` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：pkg/certmgmt 测试覆盖不错（solver_dns/solver_http/issuer/manager/renew_scheduler/renew_window/errors），但缺口精确落在两条 P1 上：(1) cert_store.go:44 SaveCert 对 crt/key/resource/meta 四文件逐个 atomicWrite——单文件原子但集合不原子，续期中途失败即 crt 新 key 旧，代理热加载拿到不匹配对（CERT P1），无 cert_store_test.go 验证「失败注入后 crt/key 必须同代」；(2) service/rpc_adapter.go:60 AddCertificates 直接 `return certmgmtlego.SaveCert(...)` 绕过 per-domain 锁，与自动续期 RenewDomain 并发写同一批文件（CERT P1），无 rpc_adapter_test.go。两处均可用 t.TempDir + 注入失败/并发写单测。
- **证据**：cert_store.go:44-60 SaveCert 顺序四次 atomicWrite 无回滚/无临时目录整体 rename；ls pkg/certmgmt/**/ 无 cert_store_test.go、无 rpc_adapter_test.go
- **核实**：cert_store.go:44 SaveCert 亲读确认对 crt/key/resource/meta 顺序四次 atomicWrite（:45 起），单文件 tmp+rename 原子但集合无回滚/无整体 rename，中途失败即 crt 新 key 旧。rpc_adapter.go:60 AddCertificates 确认直接 `return certmgmtlego.SaveCert(m.cfg.Path, record, resource)`，全函数无 domainLock 调用；对照 manager.go:113/:124 Issue/RenewDomain 均先 `m.domainLock(d).Lock()`——绕锁与自动续期并发写同批文件成立。find 确认 pkg/certmgmt 下无 cert_store_test.go、无 rpc_adapter_test.go（已有测试覆盖 errors/issuer/solver_dns/solver_http/manager/renew_scheduler/renew_window）。小瑕疵：account_store.go、client_factory.go 等也无专属测试文件，'仅有的无测试文件'措辞略过强，但两条 P1 所在文件零测试的核心事实成立。

### 20. [P2·confirmed] ReleaseBindPort「释放一个端口却删光用户全部转发规则」的 P1 路径，现有测试全部只用单端口场景，缺陷被测试绿灯掩盖

- **位置**：`pkg/proxy/usermanager/usermanager.go:1361` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：usermanager.go:1361 在 ReleaseBindPort 中调用 `m.forwardMgr.RemoveRulesByUser(req.Username)`——按用户删除全部规则而非仅 req.BindPort 对应的规则（下层 UM P1：多容器/多 inbound 用户释放一个端口会打断其他端口的活跃转发）。usermanager_test.go:630 TestReleaseBindPort_Idempotent 和 :947 TestReleaseBindPort_DeletingUserStaysAsTombstone 都只给用户绑一个端口，语义上按端口和按用户删除不可区分，所以测试通过。需要补「用户持有 2 个 BindPort，ReleaseBindPort(portA) 后 forwardMgr 中 portB 的规则必须仍在」的回归测试，它当前会红，正好钉住修复。
- **证据**：usermanager.go:1359-1362 `// Remove forward rule via ForwardManager \n if m.forwardMgr != nil { m.forwardMgr.RemoveRulesByUser(req.Username) }`；两个现有 ReleaseBindPort 测试均为单端口
- **核实**：usermanager.go:1361 亲读确认 ReleaseBindPort 调用 `m.forwardMgr.RemoveRulesByUser(req.Username)`（forward_manager.go:335，按用户删全部规则），尽管 req.BindPort 在手且函数上方已精确按端口清理 BindPorts/PortMappings——多端口用户释放单端口会误删其他端口的活跃转发。grep 全部测试确认仅 3 个 ReleaseBindPort 测试（:630 Idempotent 释放不存在的 99999、:947 Tombstone 单规则 12345、:1001 IdempotentWithDeleteState 单端口 12345），全部单端口场景下按端口删与按用户删语义不可区分，缺陷被绿灯掩盖成立；'持有 2 个 BindPort 释放 portA 后 portB 规则仍在'的回归测试确实缺失且当前会红。

### 21. [P2·confirmed] 全仓唯一的并发 inbound 添加测试 TestXrayDynamicInbound_Concurrent 被 //go:build ignore 禁用，宣称的替代文件没有并发用例

- **位置**：`pkg/proxy/systemtest/xray_protocol_matrix_test.go:375` · 维度：测试缺口 · 单元：TEST-GAPS
- **问题**：xray_protocol_matrix_test.go:1 是 `//go:build ignore`（另两个 stale 文件 xray_e2e_protocol_test.go、xray_protocol_connectivity_test.go 同样被 ignore），文件头 FIXME 声称 coverage 已由 xray_fastadd_connectivity_test.go 接管——但 grep 确认 fastadd 文件与 xray_dynamic_inbound_system_test.go 均无任何 Concurrent 用例，即 :375 的 10-goroutine 并发 AddInbound 测试没有被替代，并发添加 inbound 的回归覆盖实际已丢失。三个 ignore 文件建议要么按 FIXME 迁移到当前 contracts API（至少迁并发用例），要么删除以免误导后续 reviewer 以为覆盖存在。
- **证据**：xray_protocol_matrix_test.go:1 `//go:build ignore`、:375 `func TestXrayDynamicInbound_Concurrent`；grep -rn Concurrent xray_fastadd_connectivity_test.go xray_dynamic_inbound_system_test.go 零命中
- **核实**：逐项核实属实：三个文件（xray_protocol_matrix_test.go、xray_e2e_protocol_test.go、xray_protocol_connectivity_test.go）首行均为 `//go:build ignore`；matrix 文件头 FIXME 确实声称"Coverage superseded by xray_fastadd_connectivity_test.go"；`func TestXrayDynamicInbound_Concurrent` 精确位于 :375。全仓 grep（*_test.go 范围内 concurrent+inbound）仅命中该 ignore 文件 :374/:375/:409/:421/:466；xray_fastadd_connectivity_test.go 与 xray_dynamic_inbound_system_test.go 既无 Concurrent 字样也无任何 `go func`，即并发 AddInbound 的回归覆盖确已丢失，FIXME 的"已被接管"声明对并发用例不成立。原测试本身有 XRAY_BIN skip 守卫、属集成测试，但即便设置了 XRAY_BIN 现在也无法运行，P2 合理。

### 22. [P2·confirmed] TestExecutor_EnsureBinary_AutoDownloadNoUpdater 用 `_ = err` 丢弃结果,零断言,且注释宣称的行为与实现相反

- **位置**：`pkg/proxy/containers/xray/exec_runner_startup_test.go:57` · 维度：正确性 · 单元：TEST-QUALITY
- **问题**：该测试注释声称 "When AutoDownload is true but no updater, it silently skips the download and returns no error",但实现 exec_runner.go:234-235 在 AutoDownload=true 且 updater==nil 时明确返回 error("auto-download is enabled but updater is not available");且在此之前 exec_runner.go:227 会 os.MkdirAll(filepath.Dir("/nonexistent/xray"))——非 root CI 上先因权限失败返回 "create download dir" 错误,root 本地跑则真的在根目录创建 /nonexistent 目录(测试写 TempDir 外的文件系统副作用)。测试最后 `_ = err` 什么都不断言,任何行为(包括回归)都会通过。应改为断言 err != nil 且匹配预期错误,并把 BinaryPath 指向 t.TempDir() 内路径避免根目录副作用。
- **证据**：exec_runner_startup_test.go:53-58: err = exec.EnsureBinary(ctx); // When AutoDownload is true but no updater, it silently skips download and returns no error ...; _ = err —— 而 exec_runner.go:234-235: if e.updater == nil { return fmt.Errorf("auto-download is enabled but updater is not available...") }
- **核实**：核心缺陷属实但机制需修正:exec_runner_startup_test.go:41-58 确实 `_ = err` 零断言,注释宣称 silently skips 与实现相悖。但 finding 声称命中 exec_runner.go:234-235 updater==nil 错误分支是错的——NewExecutor(exec_runner.go:177-206) 在 AutoDownload=true 时会自动创建默认 updater(Owner XTLS/Repo Xray-core),该 nil 分支在此测试中不可达。真实行为:非 root CI 下 EnsureBinary 的 os.MkdirAll("/nonexistent") 因 EACCES 返回 create download dir 错误(被丢弃);root 环境(如 Docker CI 容器)下会真的创建 /nonexistent 目录并调用 updater.Update 发起真实 GitHub 网络下载——比 finding 描述的更糟。零断言测试掩盖一切,修法建议(断言错误 + BinaryPath 移入 t.TempDir)成立。

### 23. [P2·confirmed] 三个 TrafficStats 测试依赖固定 50ms sleep 等待异步 ForceCollect goroutine,无轮询,同包已有的 waitFor 模式未复用

- **位置**：`pkg/proxy/usermanager/usermanager_test.go:861` · 维度：并发 · 单元：TEST-QUALITY
- **问题**：ForceTrafficStatsCollection 是异步的(usermanager.go:1991-1993 ForceCollect 为 `go sc.collect()`)。TestUserManager_GetUserTrafficStats(:861)、GetContainerTrafficStats(:896)、GetGlobalTrafficStats(:936) 都是 ForceTrafficStatsCollection() + time.Sleep(50ms) 后直接断言 stats 已就绪;goroutine 调度超过 50ms(CI 上 go test ./... 多包并发、机器负载高时可能)即失败。同包 reset_user_total_traffic_test.go:134-157 已经为完全相同的场景写了 waitForUserTotal 轮询 helper(1s deadline + 20ms 间隔),这三处应改用同样的 poll-until-deadline 模式而非固定 sleep。
- **证据**：usermanager_test.go:860-865: um.ForceTrafficStatsCollection(); time.Sleep(50 * time.Millisecond); stats, found := um.GetUserTrafficStats("demo@v2raymg.local"); if !found { ... t.Error("expected to find user stats") } —— 而 usermanager.go:1991-1993: func (sc *statsCollector) ForceCollect() { go sc.collect() }
- **核实**：usermanager.go:1991-1993 核实 ForceCollect() 为 `go sc.collect()` 纯异步;StartTrafficStats 的 collectLoop 初始 collect 也在独立 goroutine 中。usermanager_test.go:860-861、895-896、935-936 三处均为 ForceTrafficStatsCollection() + time.Sleep(50ms) 后直接断言 stats 就绪,goroutine 调度延迟超 50ms 即失败。同包 reset_user_total_traffic_test.go:133-157 已有 waitForUserTotal 轮询 helper(1s deadline + 20ms 间隔)处理完全相同场景,证明修法现成。行号 861 精确命中 sleep 行。

### 24. [P2·confirmed] TestRelay_MultipleConnections 用固定 50ms/200ms sleep 后精确断言 ActiveConnections==5/==0,调度敏感

- **位置**：`pkg/proxy/forward/relay_test.go:119` · 维度：并发 · 单元：TEST-QUALITY
- **问题**：打开 5 条连接后 time.Sleep(50ms) 然后断言 relay.ActiveConnections() 恰好等于 5(:121);关闭全部后 time.Sleep(200ms) 断言恰好为 0(:131)。连接注册与关闭检测都发生在 relay 的 per-connection goroutine 中,负载高的 CI 上(多包测试并发)50ms 内 5 条连接未必全部完成 accept+dial+注册,200ms 内 EOF 传播/计数递减也未必完成。精确相等断言 + 固定 sleep 是典型 flaky 组合,应改为 poll-until-deadline(如同文件 relay_udp_test.go:573 的轮询写法)。同文件 :81、:157、:248、:330 等多处 sleep 后校验计数器是 >= 下界断言,风险较低,但 :119/:129 两处是精确相等。
- **证据**：relay_test.go:118-131: time.Sleep(50 * time.Millisecond); if relay.ActiveConnections() != 5 { t.Errorf(...) } ... for _, c := range conns { c.Close() }; time.Sleep(200 * time.Millisecond); if relay.ActiveConnections() != 0 { t.Errorf(...) }
- **核实**：relay_test.go:119 time.Sleep(50ms) 后 :121 断言 ActiveConnections() != 5 报错,:129-131 关闭全部后 sleep 200ms 断言恰为 0,均为精确相等。relay_tcp.go 核实:activeConns 在 handleConn goroutine 内 :175 atomic.AddInt64(+1)(位于 accept 后派生的 goroutine,还需先 dial 目标)、:189 defer -1(依赖 EOF 传播),两个方向都异步于测试主线程。5 条连接在 50ms 内全部完成 accept+dial+注册、或 200ms 内全部检测到关闭,在 CI 多测试二进制并发高负载下无保证。精确相等 + 固定 sleep 组合属实。

### 25. [P2·confirmed] TestClientLimiter_CleanupExpiredSlots 对 1s 回收定时器只留 100ms 余量,定时器延迟即失败

- **位置**：`pkg/proxy/forward/relay_test.go:548` · 维度：并发 · 单元：TEST-QUALITY
- **问题**：RecycleDelaySec=1(1 秒回收定时器),测试 time.Sleep(1100ms) 后立即断言 Acquire("192.168.1.2") 必须成功(:551-553)。若 time.AfterFunc 回调在负载下延迟超过 100ms(CI 上多测试二进制并发、CPU 争用时可能),slot 尚未删除,Acquire 返回 false → t.Fatal。余量只有定时器周期的 10%,应改为轮询 Acquire 直到 deadline(例如 3s),或将 sleep 加大到 2x 周期。
- **证据**：relay_test.go:524-553: RecycleDelaySec: 1, // 1 second for testing ... time.Sleep(1100 * time.Millisecond); if !lim.Acquire("192.168.1.2") { t.Fatal("new IP should be allowed after timer expired") }
- **核实**：relay_test.go:525-527 RecycleDelaySec: 1,:548 time.Sleep(1100ms) 后 :551-553 立即断言 Acquire("192.168.1.2") 必须成功。clientlimit.go:210/:223 核实回收确由 time.AfterFunc(1s) 回调删除 slot,回调若在高负载 CI 上延迟超过 100ms(仅 10% 余量),slot 未删,Acquire 返回 false → t.Fatal。行号 548 精确。属实,但相比 idx 2/3,Go timer 延迟超 100ms 需要较极端的 CPU 争用,触发概率偏低,风险在 P2 区间的下沿。

### 26. [P2·confirmed] startTestXray 固定端口 62779,启动失败/端口被占时仍返回非空 addr,后续测试 fail 而非 skip

- **位置**：`pkg/proxy/containers/xray/testmain_test.go:122` · 维度：错误处理 · 单元：TEST-QUALITY
- **问题**：startTestXray 等待循环(:124-136)超时后无条件 `return addr, cmd`——即使 5s 内 xray 从未就绪(固定端口 62779 被占导致 xray 绑定失败退出、或其他进程占用该端口返回非 gRPC 响应被误判为就绪)。xrayGRPCAddr 因此非空,newIntegrationExecutorWith(:192) 的 skip 守卫失效,所有依赖它的集成测试将以 connection refused 失败而不是 skip——正是本次审查重点的"缺守卫应 skip 而非 fail"模式。应在轮询超时时 kill 进程并返回 ("", nil)。另两处次要问题:TestMain(:41) 只 Kill 不 Wait,留下 zombie 直到进程退出;findXrayBinary(:50) 候选 "/nonexistent/xray" 上的注释 "present in this test environment" 明显是错误残留。
- **证据**：testmain_test.go:122-138: addr := "127.0.0.1:62779"; deadline := time.Now().Add(5 * time.Second); for ... { if !isConnRefused(errStr) { break } } ; return addr, cmd —— 超时路径没有返回 ("", nil);testmain_test.go:49-51: candidates := []string{"/nonexistent/xray", // present in this test environment
- **核实**：testmain_test.go:122-138 核实:轮询循环 5s 超时(持续 connection refused)后落到无条件 `return addr, cmd`,addr 恒为 "127.0.0.1:62779" 非空;且若 62779 被无关进程占用,非 refused 响应会被 :132-134 误判为就绪 break。TestMain:34 将其赋给 xrayGRPCAddr,newIntegrationExecutorWith:192 的 `if xrayGRPCAddr == ""` skip 守卫因此失效,集成测试将以 connection refused fail 而非 skip。次要项也属实:TestMain:40-41 只 Process.Kill() 不 Wait();:50 `"/nonexistent/xray", // present in this test environment` 注释确为错误残留(os.Stat 必失败,仅误导)。注意触发前提是机器上装有可用 xray 二进制(否则 findXrayBinary 返回 "" 守卫仍生效),但这正是集成测试要覆盖的环境。

### 27. [P3·confirmed] runE2EProtocolCase 在子测试内 Stop/Start 共享的 rig.container，中途失败会让后续协议子测试在容器已停状态下产生误导性级联失败

- **位置**：`pkg/proxy/systemtest/mihomo_e2e_test.go:298` · 维度：并发 · 单元：TEST-CHAIN
- **问题**：TestMihomoE2E_RealInternet 的三个协议子测试顺序共享同一个 mihomoTestRig。每个子测试的 STEP 2 停掉共享容器（line 298 rig.container.Stop()），STEP 3 再启动（line 307）。若 STEP 2 的 expectProxyFail 或 STEP 3 的 Start/waitPort 任何一步 t.Fatalf，该子测试终止但容器停留在 stopped 状态，后续 trojan/shadowsocks 子测试的 FastAddInbound/waitPort 会以与真实缺陷无关的方式失败，掩盖首因。建议在子测试内注册 t.Cleanup 保证容器恢复 running（Start 幂等即可），或每个协议 case 用独立 rig。
- **证据**：line 296-309：`if err := rig.container.Stop(); ...` → expectProxyFail（可 Fatalf）→ `if err := rig.container.Start(); err != nil { t.Fatalf(...) }`，无任何失败路径上的容器状态恢复；rig 由 line 172 startMihomoContainer 在父测试创建并被 e2eCases() 循环共享
- **核实**：属实。mihomo_e2e_test.go:175-179 三个子测试（e2eCases 确认为 vmess/trojan/shadowsocks）顺序共享 line 172 创建的同一 rig。line 298 rig.container.Stop() 后，line 302 expectProxyFail 在 GET 意外成功时 t.Fatalf（line 718），line 307-311 的 Start/waitPort 也各有 Fatalf；子测试内唯一的 t.Cleanup 是 line 237 的 RemoveInboundConfig，无任何路径恢复容器 running 状态。首个协议子测试若在 STEP2/3 之间 Fatalf，容器停留 stopped，后续子测试的 FastAddInbound（REST API 已死）/waitPort 必然以无关方式失败。但影响仅限于"已有失败时的诊断噪音"，不会让健康代码误红或缺陷漏报，首因子测试仍然最先报告，故降为 P3。

### 28. [P3·confirmed] waitUDPListener 用 UDP Dial 作 readiness gate 实际是 no-op：无监听者时 connect 也立即成功

- **位置**：`pkg/proxy/systemtest/mihomo_hy2_matrix_test.go:26` · 维度：正确性 · 单元：TEST-CHAIN
- **问题**：waitUDPListener 以 net.DialTimeout("udp", addr) 成功作为 QUIC 端口就绪信号，但 UDP connect 只写入内核路由项，不发包，即使没有任何进程监听也会立即成功返回（Linux 上仅在先发包收到 ICMP port-unreachable 并回读时才报错）。因此 hy2/tuic 矩阵（mihomo_hy2_matrix_test.go:161、mihomo_tuic_matrix_test.go:140/202）的 3 秒等待第一次循环即通过，等价于没等。目前靠 mihomo client 的 QUIC 握手重传 + 15s HTTP timeout 掩盖，慢机器上 FastAdd PATCH 生效延迟会转化为握手超时 flake。可改为轮询 mihomo REST /listeners（rig.apiAddr 已有）确认 listener 注册，或至少发一个 UDP 探测包并容忍读超时。
- **证据**：line 23-34：`conn, err := net.DialTimeout("udp", addr, 100*time.Millisecond); if err == nil { return nil }`；注释 line 18-20 自认 "a successful Dial only proves the port can be addressed (kernel-level), not that the QUIC listener actually answers"
- **核实**：属实。mihomo_hy2_matrix_test.go:23-34 以 net.DialTimeout("udp") 成功即返回 nil；UDP connect() 在 Linux 上不发包、仅登记内核路由项，无监听者也立即成功（只有先发数据收到 ICMP unreachable 后再读写才报错），故循环第一次即通过，readiness gate 等价于无。注释 line 18-20 自己承认 "only proves the port can be addressed (kernel-level), not that the QUIC listener actually answers"。调用点核实为 hy2:161、hy2:227、tuic:140、tuic:202（finding 漏列 hy2:227，不影响结论）。当前靠 mihomo client QUIC 重传 + 15s 超时兜底，属潜在 flake 源而非现行失败，P3 恰当。

### 29. [P3·confirmed] e2eServer.shutdown 的 cmd.Wait() 无超时，v2raymg 若在 SIGTERM 后挂住会阻塞 t.Cleanup 直到整个 go test -timeout 爆炸

- **位置**：`pkg/proxy/systemtest/e2e_server_helpers_test.go:432` · 维度：资源 · 单元：TEST-CHAIN
- **问题**：shutdown（在 t.Cleanup 与 negative_server_stop 两处调用）向子进程发 SIGTERM 后无条件 cmd.Wait()。v2raymg 的优雅退出涉及多个容器子进程（xray/mihomo）的级联关停，任何一处 hang（正是系统测试想暴露的缺陷类别）都会把 Wait 变成永久阻塞——失败模式从"清晰的用例失败"劣化为"整个包 15 分钟 timeout panic，日志被截断"。runE2ENegativeTest 还依赖 shutdown 同步返回后 5 秒内断链（e2e_server_test.go:381-385），挂住时同样失真。建议 SIGTERM → 带 5-10s deadline 的 Wait → 超时 SIGKILL 并 t.Logf 记录。
- **证据**：line 423-434：`s.stopped = true; if s.cmd.Process != nil { _ = s.cmd.Process.Signal(shutdownSignal); _ = s.cmd.Wait() }` —— 无 context/timer/SIGKILL 升级路径
- **核实**：属实。e2e_server_helpers_test.go:423-434 shutdown 发 shutdownSignal（line 37 确认为 syscall.SIGTERM）后无条件 `_ = s.cmd.Wait()`，无 timer/context/SIGKILL 升级。调用点两处核实：helpers 383-384 的 t.Cleanup（cleanup 挂住会拖垮整个 go test -timeout）和 e2e_server_test.go:381 的 negative 测试（其后 line 385 起 5 秒轮询断链，依赖 shutdown 同步返回）。v2raymg 优雅退出需级联关停 xray/mihomo 子进程，任一 hang 即把用例失败劣化为整包 timeout panic。P3 恰当（仅在被测进程已有缺陷时才触发）。

### 30. [P3·confirmed] freeTCPPort/freeUDPPort bind-then-close 分配存在 TOCTOU 竞态，是全包（尤其 e2e 每 server 4+ 端口）的 CI flake 源

- **位置**：`pkg/proxy/systemtest/helpers_test.go:68` · 维度：资源 · 单元：TEST-CHAIN
- **问题**：freeTCPPort 绑定 :0 取端口后立即 Close 返回，端口在"返回"与"真正使用者（xray/mihomo/v2raymg 子进程）bind"之间可被系统或并行测试进程回收。全包约 40+ 个调用点，startE2EServer 一次分配 http/rpc/xray-grpc/mihomo-api 四个，FastAdd 每 case 再取 1-2 个，碰撞概率随并行度线性增长；碰撞表现为 waitPort 超时或子进程 bind 失败的随机红。在追加 CI integration job（见 ci.yml finding）之前应缓解：TCP 侧可用 SO_REUSEADDR+持有 listener 直到 exec 前释放的窗口最小化，或让被测进程支持 :0 自选端口后回读；至少对 waitPort 失败的 Fatalf 附上"可能为端口竞态"提示降低排查成本。
- **证据**：helpers_test.go:68-76 `ln, _ := net.Listen("tcp", "127.0.0.1:0"); port := ...; ln.Close(); return port`；hysteria_udp_forward_test.go:56-65 同模式 UDP 版；e2e_server_helpers_test.go:306-321 连续 4 次分配
- **核实**：属实。helpers_test.go:68-76 freeTCPPort 为标准 bind-:0-close-return 模式，hysteria_udp_forward_test.go:56-65 freeUDPPort 同模式，e2e_server_helpers_test.go:305-320 一个 server 连续分配 http/rpc/xray-grpc/mihomo-api 四个端口。全包 freeTCPPort()/freeUDPPort( 出现 66 处（超过 finding 声称的 40+），端口在 close 与子进程真正 bind 之间可被回收，TOCTOU 竞态真实存在，碰撞表现为 waitPort 超时或子进程 bind 失败的随机红。当前为潜在 flake 而非确定性失败，P3 恰当。

### 31. [P3·confirmed] README 引用已删除的 xray_socks5_system_test.go/TestXrayContainerSocks5WebsiteAccess，且未覆盖 mihomo 矩阵、E2E server、HYSTERIA_BIN 等现有入口

- **位置**：`pkg/proxy/systemtest/README.md:34` · 维度：测试缺口 · 单元：TEST-CHAIN
- **问题**：README 的"首选集成测试"章节（line 34、38）和 test→example 映射表（line 104）都指向不存在的 xray_socks5_system_test.go 与 TestXrayContainerSocks5WebsiteAccess；同时对当前真正的主力——六个 TestMihomo*Matrix、TestE2EAllContainersProtocolMatrix、TestHysteriaUDPForwardIntegration 及其环境变量（MIHOMO_BIN/HYSTERIA_BIN/V2RAYMG_BIN/SYSTEST_UPSTREAM_URL/MIHOMO_E2E_TARGET）只字未提。对首次运行系统测试的人（或 CI 配置者）这是主要入口文档，漂移成本真实存在。
- **证据**：README.md:34 "File: `xray_socks5_system_test.go` (build tag: `integration`)"、:38 `-run TestXrayContainerSocks5WebsiteAccess`、:104 映射表同名条目；ls 确认文件不存在；README 全文 grep 无 MIHOMO_BIN/SYSTEST_UPSTREAM_URL
- **核实**：核心属实。README.md:34 "File: `xray_socks5_system_test.go`"、:38 `-run TestXrayContainerSocks5WebsiteAccess`、:104 映射表条目均实测存在，而 ls 确认该文件已不在目录中，grep 全包也无该测试函数；TestE2EAllContainersProtocolMatrix（e2e_server_test.go:185）、TestHysteriaUDPForwardIntegration（hysteria_udp_forward_test.go:135）及 SYSTEST_UPSTREAM_URL/HYSTERIA_BIN/V2RAYMG_BIN/MIHOMO_E2E_TARGET 在 README 全文（141 行）零提及。一处证据夸大需修正：MIHOMO_BIN 其实在 README:124-134 的 "Mihomo (stage 10b)" 段有说明，finding 称 "grep 无 MIHOMO_BIN" 不实；六个 TestMihomo*Matrix 也未逐一列出但 mihomo 段落存在部分覆盖。扣除该误差后，主入口文档指向已删除文件、遗漏主力 E2E/UDP 入口的漂移仍成立，P3 恰当。

### 32. [P3·confirmed] 判定为可接受的无测试目录：buildinfo、cmd/cli/common、prometheus_desc、xrayapi/grpc、core/inbound——不建议为凑约束 #9 补测试

- **位置**：`pkg/buildinfo/buildinfo.go:1` · 维度：架构 · 单元：TEST-GAPS
- **问题**：逐一核实后判定以下无测试目录为可接受，无需立案：pkg/buildinfo（12 行版本变量，ldflags 注入）；cmd/cli/common/const.go（33 行纯常量）；pkg/http/prometheus_desc（417 行纯 prometheus.Desc/标签定义，无分支逻辑，错误会在 collector 集成层暴露）；pkg/xrayapi/grpc/client.go（73 行 gRPC 连接薄封装，逻辑在 hotreload 层测更有价值）；pkg/proxy/core/inbound（227 行简单结构与默认值，且被上层 container/params 的现有测试间接覆盖）。将架构约束 #9 的执法重点放在本报告 P1/P2 条目（rpc/client、collecter/ping、snell、hotreload）上，避免低价值测试稀释维护精力。
- **证据**：wc -l: buildinfo.go 12、cmd/cli/common/const.go 33、prometheus_desc 4 文件共 417、xrayapi/grpc/client.go 73、core/inbound 2 文件共 227；均无状态机/并发/IO 分支
- **核实**：豁免判定的事实基础全部核实无误：wc -l 精确匹配——buildinfo.go 12 行、cmd/cli/common/const.go 33 行、prometheus_desc 四文件 66+95+165+91=417 行、xrayapi/grpc/client.go 73 行、core/inbound 两文件 104+123=227 行；find 确认五个目录均无 *_test.go。分支复杂度检查支持"无状态机/并发/IO 分支"结论：buildinfo 0 个 if/switch/for，其余每文件 3-8 个且多为简单校验（如 inbound.go Validate 的 Tag 非空/Port 100-65535 范围检查）。core/inbound 确被 pkg/rpc/server/fastadd_params_test.go、end_node_inbound_test.go 及 pkg/proxy/core/subscription/manager_test.go 间接使用，"被上层测试间接覆盖"的说法成立。作为 triage 说明的 P3 定级恰当。

### 33. [P3·confirmed] findXrayBinary 不支持 XRAY_BIN 环境变量也不查 PATH,xray 包集成测试在几乎所有环境静默 skip

- **位置**：`pkg/proxy/containers/xray/testmain_test.go:48` · 维度：测试缺口 · 单元：TEST-QUALITY
- **问题**：findXrayBinary 只探测三个硬编码路径(/nonexistent/xray、/usr/bin/xray、/usr/local/bin/xray),既不读 XRAY_BIN 环境变量(systemtest 包的约定,见 mihomo_protocol_matrix_test.go:86 与 xray_dynamic_inbound_system_test.go:26),也不 exec.LookPath("xray")。结果是 xray 包内所有经 newIntegrationExecutor 的集成测试(inbound 增删、gRPC API 等)在开发机把 xray 装在其他位置或仅在 PATH 中时全部静默 skip,这部分覆盖实际上长期处于死亡状态且无人察觉(skip 不会让 CI 变红)。建议对齐 systemtest 约定:优先 XRAY_BIN,其次 exec.LookPath,最后硬编码路径。
- **证据**：testmain_test.go:48-62: candidates := []string{"/nonexistent/xray", "/usr/bin/xray", "/usr/local/bin/xray"} —— 无 os.Getenv("XRAY_BIN")、无 exec.LookPath;对比 systemtest/xray_dynamic_inbound_system_test.go:26-31 的 XRAY_BIN + Skip 守卫
- **核实**：testmain_test.go:48-62 实证:findXrayBinary 仅 stat 三个硬编码路径 {"/nonexistent/xray","/usr/bin/xray","/usr/local/bin/xray"},全文件无 os.Getenv("XRAY_BIN")、无 exec.LookPath(grep 全 pkg 仅 systemtest 包用 XRAY_BIN,如 xray_dynamic_inbound_system_test.go:26-32、xray_protocol_matrix_test.go:41 均为 XRAY_BIN+Skip 约定)。该文件无 build tag,默认 go test ./... 就会运行,二进制不在这三个路径时 xrayGRPCAddr 为空,newIntegrationExecutorWith(:192-193) 静默 t.Skip,所有经 newIntegrationExecutor 的集成用例不执行且 CI 不变红。另注意 :50 的 "/nonexistent/xray" 候选带注释 "present in this test environment",是明显的测试环境残留,进一步佐证该探测逻辑未与 systemtest 约定对齐。P3 恰当。

### 34. [P3·confirmed] ensureMihomoBin 在 GitHub 下载失败时 t.Fatalf 而非 t.Skip,与同包 hysteria/公网探测的 skip 语义不一致

- **位置**：`pkg/proxy/systemtest/mihomo_helpers_test.go:60` · 维度：错误处理 · 单元：TEST-QUALITY
- **问题**：MIHOMO_BIN 未设置时 ensureMihomoBin 走 Updater 从 GitHub 下载 Alpha 预发布(3 分钟预算),失败即 t.Fatalf(:60)。同包对外部依赖不可用的处理是 Skip:hysteria_udp_forward_test.go:138-144(HYSTERIA_BIN 未设/无网络均 Skip)、public_upstream_test.go 公网不可达 Skip。虽然文件头注释明确写了"set MIHOMO_BIN in CI to avoid flakes"属有意设计(防止静默丢覆盖),但 GitHub 限流/网络抖动会让所有 mihomo matrix 集成测试硬失败;若维持 fail-fast 语义,至少应区分"网络不可达"(Skip)与"下载内容校验失败"(Fatal)。仅影响 -tags=integration 运行,不影响默认 CI。
- **证据**：mihomo_helpers_test.go:57-60: if err := ...Update(...); err != nil { t.Fatalf("download mihomo Alpha via Updater: %v", err) } —— 对比 hysteria_udp_forward_test.go:143-144: t.Skipf("integration test skipped: no internet: %v", err)
- **核实**：mihomo_helpers_test.go:56-60 实证:MIHOMO_BIN 未设时经 Updater 下载 Alpha,失败即 t.Fatalf(:60);同包 hysteria_udp_forward_test.go:136-145 对 HYSTERIA_BIN 未设/无效/无网络三种情况均 t.Skip/Skipf,skip 语义不一致属实。GitHub 限流或网络抖动会让所有依赖 ensureMihomoBin 的 matrix 集成测试硬失败而非 skip。需注意文件头注释(:26-33)明确写了 fail-fast 是有意设计("misconfigured env is never a valid reason to silently fall through"、"set MIHOMO_BIN in CI to avoid flakes"),finding 自己也承认这点,故这是文档化的设计取舍而非疏漏,只有 -tags=integration 且未设 MIHOMO_BIN 时受影响;区分网络不可达(Skip)与校验失败(Fatal)的建议合理。维持 P3。

### 35. [P3·confirmed] TestTokenBucketLimiter_WaitNUnlimited 对 wall-clock 上界断言 50ms,CPU 饥饿的 CI 上可能偶发超时

- **位置**：`pkg/proxy/forward/ratelimit_test.go:487` · 维度：并发 · 单元：TEST-QUALITY
- **问题**：断言 unlimited 直通路径的 WaitN 耗时 <= 50ms(:487-489);同文件 TestTokenBucketLimiter_WaitNRespectsContext(:505-508)断言 <= 300ms。这类 wall-clock 上界断言在 go test ./... 多包并发、-count=1 且 runner 超卖的 CI 上,GC/调度停顿可能偶发突破 50ms。风险相对低(直通路径无实际等待),但属于纯余量问题:可放宽到 500ms 量级而不损失测试意图(直通 vs 10 秒级真实等待的区分度足够)。
- **证据**：ratelimit_test.go:483-489: start := time.Now(); if err := l.WaitN(context.Background(), 1024*1024); ... if elapsed := time.Since(start); elapsed > 50*time.Millisecond { t.Errorf("unlimited WaitN took %v, expected near-instant", elapsed) }
- **核实**：ratelimit_test.go:483-489 实证:unlimited 直通路径 WaitN 后断言 elapsed > 50ms 即 t.Errorf;:505-507 的姊妹用例上界为 300ms。50ms 是对纯 wall-clock 的上界断言,在超卖的共享 CI runner 上(go test ./... 多包并行编译+运行)goroutine 调度停顿超过 50ms 是现实可能,属真实但低概率的 flake 余量问题;直通(微秒级)与真实等待(该场景需 ~10 秒)的区分度极大,放宽到数百 ms 不损失测试意图。无功能性错误,P3 恰当。

## 附录：verifier 判定 refuted（不计入，供追溯）

| 单元 | 位置 | 标题 | refuted 理由 |
|------|------|------|-------------|
| TEST-GAPS | `pkg/xrayapi/hotreload/manager.go:83` | pkg/xrayapi/hotreload（676 行）与 hotupdate（329 行）零测试，且被 xray 容器生产代码引用，非死代码 | 零测试属实（两目录无 *_test.go，wc -l 与描述一致），但核心论据"被 xray 容器生产代码引用，非死代码"经核实为假：pkg/proxy/containers/xray/grpc_client.go 引用的是 pkg/xrayapi/internalproto/* 与 pkg/xrayapi/types（同父目录的兄弟子包），完全不引用 hotreload/hotupdate。grep 全仓确认 pkg/xrayapi/hotreload 唯一 importer 是 cmd/test_hotreload/main.go，而该文件首行是 `// +build ignore`（永不参与编译的调试脚本）；pkg/xrayapi/hotupdate 在包外零引用。即 hotreload(676行)/hotupdate(329行) 实为死代码，不在任何用户面路径上——finding 把"pkg/xrayapi 被引用"偷换成了"hotreload/hotupdate 被引用"。正确处置是删除或先接入生产链路，而非为死代码补单测；按原 P2 立案会误导测试投入。 |
| TEST-QUALITY | `pkg/proxy/tools/process/runner_test.go:78` | Runner 测试用尾部 runner.Stop() 而非 t.Cleanup,断言失败路径泄漏 `sleep 100` 子进程 | 所述泄漏场景经核对 Runner 实现后不成立。runner.go:139-144 Restart=Stop 后 Start:若 Stop 出错(:111-117 signal 失败),会立即 Kill+Wait,进程已被杀;若 Stop 成功而 Start 失败,旧 sleep 已终止、新进程未启动——即 :79 的 t.Fatalf("failed to restart") 触发时不存在存活的 sleep 100。其余两个被点名的测试无泄漏路径:TestRunner_Start_AlreadyRunning(:100-109) 首次 Start 失败时进程未启动,第二次 Start 的断言用 t.Error(:106) 不中断,尾部 runner.Stop()(:109) 必然执行;TestRunner_PID(:163-172) 同理,Start 成功后只有 t.Errorf(:169),Stop(:172) 必达。t.Error/Errorf 不终止测试函数,finding 把"断言失败"混同于 Fatalf。改用 t.Cleanup 只是风格改进(可覆盖 panic 场景),不构成实际缺陷。 |
