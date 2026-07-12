# L4 集群层 review — cluster / rpc / collecter

> Finder → 对抗性 verifier 两级流程产出。下表为**保留**（confirmed + uncertain）的 findings，已剔除 verifier 判定 refuted 的 0 条。uncertain 多为依赖第三方协议/上游行为、无法在仓库内确证的条目，处理前需人工核对。

## 统计

| 维度 | 数量 |
|------|------|
| 保留条目 | 46 |
| — confirmed | 46 |
| — uncertain | 0 |
| — 其它(unverified) | 0 |
| refuted(已剔除) | 0 |

| 优先级 | P0 | P1 | P2 | P3 |
|--------|----|----|----|----|
| 保留条目 | 1 | 12 | 18 | 15 |

## 速查表

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P0 | ✓ | 并发 | CLU | `pkg/cluster/node_manager.go:97` | NodeManager.Get/HaveNode/GetAllNode 无锁读 map，与 Add/Delete 并发触发 fatal: concurrent map read and map write，整进程崩溃 |
| 2 | P1 | ✓ | 并发 | CLU | `pkg/cluster/node_manager.go:105` | GetAllNode 直接返回内部 map，调用方在锁外 range 时并发 Add/Delete 触发并发 map 迭代崩溃 |
| 3 | P1 | ✓ | 资源 | CLU | `pkg/cluster/node.go:39` | GetGrpcClientConn 在连接非 Ready 时无条件重新 Dial 且不 Close 旧连接，配合非阻塞 Dial 造成 gRPC 连接泄漏与并发写竞态 |
| 4 | P1 | ✓ | 并发 | CLU | `pkg/cluster/cluster.go:102` | Node 心跳/token 字段(GetHeartBeatTime/OutToken/ReportHeartBeatTime)无同步，鉴权写与心跳读跨 goroutine 数据竞争 |
| 5 | P1 | ✓ | 安全 | RPC | `pkg/rpc/server/center_node_server.go:119` | CenterNodeServer 完全无鉴权且明文：违反 center/end 双向 token 鉴权约束，可投毒节点目录并枚举集群 |
| 6 | P1 | ✓ | 并发 | RPC | `pkg/rpc/server/end_node_server.go:137` | methodRspMap 共享单例被 authRemoteNode 反射写入并被 newEmptyRsp 直接返回：并发数据竞争 + 跨请求错误信息串号 |
| 7 | P1 | ✓ | 正确性 | RPC | `pkg/rpc/server/end_node_cluster.go:319` | registerOrHeartBeatToEndNode 遇到 invalid 节点用 return 而非 continue：一个失效节点使本轮所有后续节点漏发心跳/注册 |
| 8 | P1 | ✓ | 并发 | RPC | `pkg/rpc/server/end_node_cluster.go:256` | cluster.Node 的 OutToken/ReportHeartBeatTime/grpcClientConn 在心跳 goroutine 与 client 扇出/RegisterNode 间无锁读写：数据竞争 |
| 9 | P1 | ✓ | 并发 | RPC | `pkg/rpc/server/end_node_cluster.go:212` | cluster.Node 的 OutToken/ReportHeartBeatTime/grpcClientConn 在心跳 goroutine 与客户端扇出间无锁并发读写 |
| 10 | P1 | ✓ | 正确性 | COL | `pkg/collecter/ping/icmp_ping_checker.go:88` | ICMP 探测间隔/超时配置被静默忽略：pingLoop 使用包级全局常量而非配置值 |
| 11 | P1 | ✓ | 正确性 | COL | `pkg/collecter/ping_collector.go:150` | 域名配置的 ICMP 节点结果被 cleanupStaleResults 在每次远程 reload 时误删，100 样本环形缓冲反复清零 |
| 12 | P1 | ✓⚠️协议 | 错误处理 | COL | `pkg/collecter/ping/icmp_ping_checker.go:89` | `go pi.pinger.Run()` 丢弃返回错误：无 CAP_NET_RAW 时全部 ICMP 探测静默失败且永不重试 |
| 13 | P1 | ✓ | 正确性 | COL | `pkg/collecter/ping/ping.go:222` | basePingChecker.Stop 将 isRunning 置为 true（应为 false），ICMP checker 停止后状态永远为 running 且无法重启，SetPingCheck 成为单向开关 |
| 14 | P2 | ✓ | 正确性 | CLU | `pkg/cluster/end_node_cluster.go:319` | registerOrHeartBeatToEndNode 遇到无效节点用 return 直接退出整个循环，导致其后所有节点漏发心跳/注册 |
| 15 | P2 | ✓ | 并发 | CLU | `pkg/cluster/node_manager.go:122` | NodeManager.Filter 两段式(RLock 复制→Lock 换指针)之间的并发 Add/Delete 会被静默丢失 |
| 16 | P2 | ✓ | 安全 | CLU | `pkg/cluster/cluster.go:98` | AuthRemoteNode 对 GetHeartBeatTime==0 的节点跳过超时校验，空 InToken 的 gossip 节点可被空 token 冒名鉴权通过 |
| 17 | P2 | ✓ | 资源 | CLU | `pkg/cluster/node.go:44` | 节点被 Filter/Delete 移除时缓存的 grpcClientConn 从不 Close，节点频繁增删下连接持续泄漏 |
| 18 | P2 | ✓ | 安全 | CLU | `pkg/rpc/server/center_node_server.go:68` | Center HeartBeat 不校验集群 token 即接纳并 gossip 任意上报节点，形成节点表投毒/SSRF 放大面 |
| 19 | P2 | ✓ | 错误处理 | RPC | `pkg/rpc/server/end_node_server.go:122` | authRemoteNode 对 nil NodeAuthInfo/Node 无防护：反射 .Elem().Interface() 触发 panic 崩溃整个 gRPC server |
| 20 | P2 | ✓ | 安全 | RPC | `pkg/rpc/server/end_node_server.go:130` | authRemoteNode 本地 token 等价旁路：InToken 等于本地节点 token 即无条件放行 per-node 鉴权 |
| 21 | P2 | ✓ | 资源 | RPC | `pkg/rpc/client/end_node_rpc.go:478` | 全局限流信号量 ch 在 wg.Add 前阻塞式获取，持有调用方 goroutine：大集群扇出与其它并发调用相互挤占易阻塞 |
| 22 | P2 | ✓ | 测试缺口 | RPC | `pkg/rpc/server/center_node_server.go:31` | 测试缺口：center 心跳、unaryServerInterceptor/authRemoteNode 鉴权路径与心跳漂移校验均无单测 |
| 23 | P2 | ✓ | 并发 | RPC | `pkg/rpc/server/end_node_inbound.go:161` | SetGatewayModel/SetPingCheck 无锁写 s.cfg，与拦截器每请求读 OnlyGateway 构成数据竞争 |
| 24 | P2 | ✓ | 架构 | RPC | `pkg/rpc/server/end_node_inbound.go:78` | AddInbound 直接把调用方 base64 xray 原生 JSON 灌入 AddInboundNative，跨层暴露原生类型且可注入任意入站 |
| 25 | P2 | ✓ | 测试缺口 | RPC | `pkg/rpc/server/end_node_server.go:120` | 鉴权拦截器/心跳并发路径缺测试覆盖：nil panic、共享 Rsp 竞争、return 心跳饥饿、center 无鉴权均无用例 |
| 26 | P2 | ✓ | 测试缺口 | RPC | `pkg/rpc/server/end_node_server.go:146` | 鉴权拦截器/RegisterNode/HeartBeat/center 心跳等安全与集群核心路径无单元测试 |
| 27 | P2 | ✓ | 安全 | COL | `pkg/collecter/ping/remote_loader.go:44` | RemoteLoader 不强制 HTTPS 且响应体无大小上限：节点源被劫持可将全集群节点变成内网 TCP 端口扫描器，并可 OOM 节点 |
| 28 | P2 | ✓⚠️协议 | 资源 | COL | `pkg/collecter/ping/icmp_ping_checker.go:82` | pro-bing 回调经无缓冲 channel 交接与 pingLoop 退出存在竞态：节点删除/停机时 runLoop 永久阻塞，goroutine 与 raw socket 泄漏 |
| 29 | P2 | ✓ | 正确性 | COL | `pkg/collecter/ping/ping.go:173` | calculateStDev 返回的是方差而非标准差（缺少开方），StDevDelay 指标数值错误 |
| 30 | P2 | ✓ | 正确性 | COL | `pkg/collecter/ping/ping.go:156` | findMin 无有效样本时返回 math.MaxFloat64，经 float32 转换后 MinDelay=+Inf 传播到 RPC 与 prometheus |
| 31 | P2 | ✓ | 测试缺口 | COL | `pkg/collecter/ping_collector.go:1` | pkg/collecter 与 pkg/collecter/ping 两个包均无任何 *_test.go，违反架构约束 9 |
| 32 | P3 | ✓ | 错误处理 | CLU | `pkg/cluster/cluster.go:123` | Cluster.IsValid/RegisteredLocal/RegisteredRemote 对 Get 返回值不判空即调方法，节点不存在时 nil 解引用 panic |
| 33 | P3 | ✓ | 正确性 | CLU | `pkg/cluster/cluster_manager.go:7` | getNodeFilterByCurrentTime 的 currentTime 参数被完全忽略，过滤逻辑与命名/入参语义不符 |
| 34 | P3 | ✓ | 测试缺口 | CLU | `pkg/cluster/cluster_test.go:1` | 测试缺口：无并发/竞态测试，未覆盖 GetGrpcClientConn、Filter 丢更新、空 token 鉴权路径与 return-vs-continue 循环缺陷 |
| 35 | P3 | ✓ | 错误处理 | RPC | `pkg/rpc/client/context.go:9` | 固定 1 秒 RPC 超时用于 ObtainNewCert/UpdateProxy/UpsertClusterUsers 等慢操作，注定超时失败 |
| 36 | P3 | ✓ | 安全 | RPC | `pkg/rpc/server/end_node_server.go:70` | OnlyGateway 白名单用 strings.Contains 子串匹配，语义脆弱易误放行 |
| 37 | P3 | ✓ | 资源 | RPC | `pkg/rpc/client/context.go:12` | NewContext 丢弃 WithTimeout 的 cancel 造成计时器泄漏，且固定 1s 超时用于心跳批量推用户可截断同步 |
| 38 | P3 | ✓ | 架构 | RPC | `pkg/rpc/server/end_node_server.go:71` | isOnlyGatewayMethod 用 strings.Contains 做子串匹配判定网关白名单，脆弱且易误放行 |
| 39 | P3 | ✓ | 并发 | COL | `pkg/collecter/ping/ping.go:83` | ConvertToProtoPingResult 持有读锁后递归获取同一 RWMutex 读锁，存在经典写者插队死锁隐患 |
| 40 | P3 | ✓ | 并发 | COL | `pkg/collecter/ping/icmp_ping_checker.go:166` | IcmpPingChecker.pingerMap 无锁读写且 Start 的 isRunning 检查非原子，并发 Start 会产生双 reconcile goroutine 数据竞争 |
| 41 | P3 | ✓ | 并发 | COL | `pkg/collecter/ping/tcp_ping_checker.go:61` | pingLoop 向 resultChan 的发送不受 ctx 保护：停机时若缓冲耗尽，TCP 侧 wg.Wait 挂死、ICMP 侧 goroutine 泄漏 |
| 42 | P3 | ✓ | 正确性 | COL | `pkg/collecter/ping/icmp_ping_checker.go:186` | 节点元数据（Geo/ISP/端口）在 reload 后不生效：reconcile 仅按 host 键复用旧 pingerInfo，指标标签长期陈旧并产生重复序列 |
| 43 | P3 | ✓ | 资源 | COL | `pkg/collecter/ping_collector.go:61` | ICMP/TCP 探测均禁用时仍加载节点源并启动远程 reload 轮询 goroutine，空耗 HTTP 请求 |
| 44 | P3 | ✓ | 错误处理 | COL | `pkg/collecter/ping_collector.go:109` | startChecker 丢弃 context cancel 函数（go vet lostcancel），且派生 ctx 毫无作用 |
| 45 | P3 | ✓ | 错误处理 | COL | `pkg/collecter/node_collector.go:28` | NewNodeCollector 初始化失败直接 panic 且 Register 错误被忽略 |
| 46 | P3 | ✓ | 架构 | COL | `pkg/collecter/ping/node_manager.go:93` | NodeManager 以 host 单键合并节点：同一 host 的多端口 TCP 探测或多源配置互相覆盖 |

## 详细条目

### 1. [P0·confirmed] NodeManager.Get/HaveNode/GetAllNode 无锁读 map，与 Add/Delete 并发触发 fatal: concurrent map read and map write，整进程崩溃

- **位置**：`pkg/cluster/node_manager.go:97` · 维度：并发 · 单元：CLU
- **问题**：NodeManager 用 lock(RWMutex) 保护 *nodes，但 Get(97)、HaveNode(38)、GetAllNode(105) 三个读取路径完全不加锁直接读 (*nm.nodes)。而 Add(31)/Delete(44) 在 Lock 下写同一张 map。每个 RPC 都会经过 interceptor→authRemoteNode→AuthRemoteNode→NodeManager.Get(cluster.go:91)，同时 RegisterNode 走 Add、周期 filter 走 Delete/Filter、center HeartBeat(center_node_server.go:69 cluster.Get + :75 Add)也走这两条路径。Go runtime 对『同一 map 并发读写』是不可恢复的 fatal error，会 panic 整个 center/end 进程，非单请求失败。这是本层最易触发、后果最重的缺陷。
- **证据**：node_manager.go:38 `func (nm *NodeManager) HaveNode(...) { _, ok := (*nm.nodes)[key] }`(无锁);:97 `func (nm *NodeManager) Get(...) { if n, ok := (*nm.nodes)[nodeName]; ...}`(无锁);:31 Add / :44 Delete 均在 nm.lock.Lock() 下写 map。调用链 end_node_server.go:130 AuthRemoteNode→cluster.go:91 Get 与 end_node_cluster.go:93 Add 并发。
- **核实**：亲自核实成立。node_manager.go:38 HaveNode、:97-102 Get、:105-106 GetAllNode 均直接解引用 (*nm.nodes) 读 map，无任何锁；Add(:31-35)/Delete(:44-48) 在 Lock 下写同一张 map。并发触发路径真实：end 节点每个 gRPC 请求经 unaryServerInterceptor(end_node_server.go:151)→authRemoteNode(:120)→clusterState.AuthRemoteNode(:130)→cluster.go:91 NodeManager.Get，与 RegisterNode handler 的 clusterState.Add(end_node_cluster.go:93)、心跳 goroutine 的 addRemoteNode→Add(end_node_cluster.go:378) 天然并发（gRPC 每请求一个 goroutine）；center 侧 center_node_server.go:69 cluster.Get 与 :75 cluster.Add 在多个 end 节点并发心跳时同样构成无锁读+写。Go runtime 对并发 map 读写抛不可恢复 fatal（写方持锁不影响读方触发 hashWriting 检测），整进程崩溃。无任何上层串行化机制排除该路径。P0 恰当。

### 2. [P1·confirmed] GetAllNode 直接返回内部 map，调用方在锁外 range 时并发 Add/Delete 触发并发 map 迭代崩溃

- **位置**：`pkg/cluster/node_manager.go:105` · 维度：并发 · 单元：CLU
- **问题**：GetAllNode 直接返回 *nm.nodes 内部 map 本体（未复制、未加锁）。cluster.go:64 GetProtoNodesWithFilter 与 end_node_cluster.go:308 registerOrHeartBeatToEndNode 都对其结果直接 range 遍历。遍历期间只要有另一 goroutine 调用 Add(RegisterNode/addRemoteNode)或 Delete(filter/注册失败旁路),即构成 map 并发迭代+写，同样是 runtime fatal。GetProtoNodesWithFilter 又被 end/center 的 HeartBeat 响应构造使用,属高频路径。修复应返回副本或在 RLock 下完成遍历。
- **证据**：node_manager.go:105 `func (nm *NodeManager) GetAllNode() map[string]*Node { return *nm.nodes }`;消费点 cluster.go:64 `for key, node := range cluster.NodeManager.GetAllNode()`、end_node_cluster.go:308 `for _, node := range s.clusterState.GetAllNode()`。
- **核实**：核实成立。node_manager.go:105-106 `return *nm.nodes` 直接返回内部 map 本体，无复制无锁。消费点确认：cluster.go:64 GetProtoNodesWithFilter 对其 range（被 end 节点 HeartBeat handler end_node_cluster.go:129 和 center HeartBeat center_node_server.go:77 高频调用）；end_node_cluster.go:308 registerOrHeartBeatToEndNode 也直接 range。遍历期间 RegisterNode 的 Add(end_node_cluster.go:93)、registerToEndNode 失败旁路的 Delete(:203)、addRemoteNode 的 Add(:378) 均可并发发生，构成 map 并发迭代+写的 runtime fatal。与 idx 0 同为崩溃级缺陷，但作为该缺陷簇的第二表现形式排 P1 合理。

### 3. [P1·confirmed] GetGrpcClientConn 在连接非 Ready 时无条件重新 Dial 且不 Close 旧连接，配合非阻塞 Dial 造成 gRPC 连接泄漏与并发写竞态

- **位置**：`pkg/cluster/node.go:39` · 维度：资源 · 单元：CLU
- **问题**：grpc.Dial(默认非阻塞)返回的 ClientConn 初始为 IDLE,首次 RPC 前 GetState()!=Ready 恒成立。GetGrpcClientConn 的判断是 `conn==nil || GetState()!=Ready` 就重新 Dial 并覆盖 node.grpcClientConn,旧连接从不 Close。对于尚未建连或已下线的节点,heartbeat 定时器每个 tick、以及 ReqToMultiEndNodeServer 的并发 goroutine(end_node_rpc.go:485)每次调用都会新建一个 ClientConn 丢弃前一个,连接数随时间/节点数线性泄漏(fd、goroutine)。同时该字段写入无任何锁,registerToEndNode/heartbeatToEndNode/ReqToMultiEndNodeServer 多 goroutine 并发写 node.grpcClientConn 构成数据竞争。
- **证据**：node.go:39-41 `if node.grpcClientConn == nil || node.grpcClientConn.GetState() != connectivity.Ready { ... node.grpcClientConn, err = grpc.Dial(addr, ...) }`,无 defer Close 旧 conn、无 mutex;并发调用点 end_node_cluster.go:169/222、end_node_rpc.go:485。
- **核实**：核实成立。node.go:37-44：条件 `conn==nil || GetState()!=Ready` 满足即重新 grpc.Dial 并直接覆盖 node.grpcClientConn，旧 conn 从不 Close；grpc.Dial 非阻塞返回 IDLE 态，对未建连/不可达/闲置回落 IDLE 的节点每次调用都泄漏一个 ClientConn（各自持有重连 goroutine/fd）。触发频率核实：心跳 ticker 每 10s 调 registerToEndNode/heartbeatToEndNode(end_node_cluster.go:169/222)，泄漏随时间线性累积。并发写竞态也属实：HTTP handler 经 GetTargetNodes(http_server.go:98-108，GetNodesWithFilter 返回共享 *Node 指针)→NewEndNodeClient→ReqToMultiEndNodeServer 的 goroutine(end_node_rpc.go:485) 与心跳 goroutine 对同一 node.grpcClientConn 字段无锁并发读写，-race 必报。P1 恰当。

### 4. [P1·confirmed] Node 心跳/token 字段(GetHeartBeatTime/OutToken/ReportHeartBeatTime)无同步，鉴权写与心跳读跨 goroutine 数据竞争

- **位置**：`pkg/cluster/cluster.go:102` · 维度：并发 · 单元：CLU
- **问题**：Node 结构体的 GetHeartBeatTime、ReportHeartBeatTime、OutToken 等字段无任何锁保护。AuthRemoteNode(cluster.go:102)在 RPC interceptor goroutine 写 n.GetHeartBeatTime;registerToEndNode/heartbeatToEndNode(end_node_cluster.go:212/213/256/272/278)在心跳 goroutine 写 OutToken/ReportHeartBeatTime;center HeartBeat(center_node_server.go:70)写 GetHeartBeatTime;而 filter goroutine 与 IsValid/RegisteredRemote/IsCompleteRegister 并发读同一批字段。这些 int64/string 读写在多核上是数据竞争(-race 必报),可能读到撕裂/过期值,导致节点被误判有效或误删。属系统性并发缺陷,需为 Node 状态字段引入锁或原子操作。
- **证据**：cluster.go:102 `n.GetHeartBeatTime = time.Now().Unix()`(interceptor 路径);end_node_cluster.go:212 `node.OutToken = token`、:278 `node.ReportHeartBeatTime = time.Now().Unix()`(心跳 goroutine);读取方 node.go:51 IsValid、:66 RegisteredRemote 与 filter 并发。
- **核实**：核实成立。Node 结构体（node.go:15-25）所有状态字段无锁。写点确认：cluster.go:102 AuthRemoteNode 在 gRPC interceptor goroutine 写 n.GetHeartBeatTime；end_node_cluster.go:212 node.OutToken=token、:213/:273/:278 ReportHeartBeatTime、:256/:265/:272 OutToken="" 在心跳 goroutine 写；center_node_server.go:70 在 center RPC handler 写 GetHeartBeatTime；RegisterNode handler(end_node_cluster.go:76/:82) 写 InToken/GetHeartBeatTime。读点确认：filter goroutine(end_node_server.go:208) 调 IsValid(node.go:51)，HTTP 路径 GetTargetNodes 与 ReqToMultiEndNodeServer(:475 RegisteredRemote) 跨 goroutine 读同一批 int64/string 字段。多方向无同步读写，构成确定的数据竞争，可导致节点有效性误判。P1 恰当。

### 5. [P1·confirmed] CenterNodeServer 完全无鉴权且明文：违反 center/end 双向 token 鉴权约束，可投毒节点目录并枚举集群

- **位置**：`pkg/rpc/server/center_node_server.go:119` · 维度：安全 · 单元：RPC
- **问题**：CenterNodeServer.Start() 用 grpc.NewServer() 创建服务，既不注册 unaryServerInterceptor 也不注册 EncryptMessageCodec；HeartBeat handler(31 行)只做时间戳漂移(30s)和 node.IsComplete() 校验，完全不校验任何 token。对端 heartbeatToCenterNode(end_node_cluster.go:345-352)发送 Token:"" 且不带 grpc.ForceCodec，即明文传输。后果：(1) 任何能触达 center RPC 端口的攻击者可用任意 clusterName+完整 node 元信息发心跳，被 s.clusters.Add / cluster.Add(75 行)无条件注入集群目录；该目录随后通过 HeartBeatRsp.NodesMap 返回给合法 end 节点，被 addRemoteNode(end_node_cluster.go:363-388)加入本地集群列表，进而在 registerOrHeartBeatToEndNode 里被 end 节点主动发起连接/注册（节点目录投毒 + 让 end 节点向攻击者指定 host:port 发起出站连接）。(2) 只要知道/猜到 clusterName 即可通过一次心跳拿到该集群全部有效节点的 host/port/name（信息泄漏）。架构约束#8 明确要求集群通信 AES-GCM 加密、center/end 双向 token 鉴权，center 侧二者皆缺。
- **证据**：Start(): grpcServer := grpc.NewServer()（无 UnaryInterceptor、无 encoding.RegisterCodec）; HeartBeat 无 token 校验，else 分支 s.clusters.Add(clusterName, node) / cluster.Add(node); heartbeatToCenterNode: c.HeartBeat(rpcClient.NewContext(), heartBeatReq) 无 ForceCodec。
- **核实**：已核实 center_node_server.go:119 用 grpc.NewServer() 创建，既无 UnaryInterceptor 也无 encoding.RegisterCodec；HeartBeat handler(31-88)只做时间戳漂移与 IsComplete 校验，无任何 token 校验，未知 cluster 时无条件 s.clusters.Add(85)、已知 cluster 时 cluster.Add(75)。对端 heartbeatToCenterNode(345-352)确以 Token:"" 发送，conn 走 node.go:41 insecure.NewCredentials 且默认 proto codec，故明文。返回的 NodesMap(77-81)经 addRemoteNode(363-388)被 end 节点 s.clusterState.Add 注入本地集群，后续 registerOrHeartBeatToEndNode 会对其发起出站连接/注册。投毒+枚举路径真实可达，center 侧无鉴权中间件保护。P1 合理。

### 6. [P1·confirmed] methodRspMap 共享单例被 authRemoteNode 反射写入并被 newEmptyRsp 直接返回：并发数据竞争 + 跨请求错误信息串号

- **位置**：`pkg/rpc/server/end_node_server.go:137` · 维度：并发 · 单元：RPC
- **问题**：methodRspMap(74 行)对每个方法只存一个全局共享的 *proto.XxxRsp 实例。authRemoteNode 鉴权失败分支(137-140 行)对该共享实例反射写 Code=400 与 Msg=errMsg 后直接返回；newEmptyRsp(116-118 行)在 OnlyGateway 拒绝路径也直接返回同一共享实例。这些 Rsp 随后由 gRPC 在各自 goroutine 中序列化。因此:(1) 两个并发请求命中同一方法的鉴权失败/网关拒绝时，会对同一结构体字段并发写并同时被 codec 读，构成 data race；(2) 一个客户端可能收到另一个并发请求写入的 Msg（含对端 host/name 的鉴权错误详情），即跨请求信息串号/泄漏。正确做法是每次 new 一个响应对象。
- **证据**：var methodRspMap = map[string]interface{}{...单实例...}; authRemoteNode: rspValue := reflect.ValueOf(methodRspMap[...]); rspValue.Elem().FieldByName("Code").SetInt(400); ...SetString(errMsg); return false, rspValue.Interface(); newEmptyRsp: return methodRspMap[fullMethod[methodPrefixLen:]], nil。
- **核实**：methodRspMap(74-114)对每个方法仅存一个全局共享 *proto.XxxRsp 单例。authRemoteNode 鉴权失败分支(137-140)对该共享实例反射写 Code=400、Msg=errMsg 后 return；newEmptyRsp(116-118)在 OnlyGateway 拒绝路径直接返回同一共享实例。这些 Rsp 由 gRPC 在各请求 goroutine 中序列化，两并发请求命中同一方法的鉴权失败/网关拒绝路径时对同一结构体字段并发写并被 codec 读，构成真实 data race，且 Msg 可跨请求串号泄漏。正解应每次 new。P1 合理。

### 7. [P1·confirmed] registerOrHeartBeatToEndNode 遇到 invalid 节点用 return 而非 continue：一个失效节点使本轮所有后续节点漏发心跳/注册

- **位置**：`pkg/rpc/server/end_node_cluster.go:319` · 维度：正确性 · 单元：RPC
- **问题**：registerOrHeartBeatToEndNode 遍历 s.clusterState.GetAllNode()（map，遍历顺序随机），当某节点 !node.IsValid() 时(315-320 行)记录日志后执行 return，直接退出整个函数，跳过该轮所有尚未处理的节点。应为 continue。由于 map 顺序随机，任意一个暂时失效的节点都会间歇性地阻断对其余健康节点的心跳与注册，导致健康节点因 ReportHeartBeatTime 超时被判为无效、进而被 filter 清理，造成集群成员抖动/脑裂。
- **证据**：for _, node := range s.clusterState.GetAllNode() { ... if !node.IsValid() { log.Info("skip heartbeat to invalid node", ...); return } ch <- struct{}{}; wg.Add(1); ... }
- **核实**：registerOrHeartBeatToEndNode(305-330)遍历 s.clusterState.GetAllNode()(map，顺序随机)，node !IsValid() 时(315)记录日志后在 319 行 return 直接退出整个函数，跳过本轮所有后续节点，应为 continue。任一暂失效节点会间歇性阻断对其余健康节点的心跳/注册，导致健康节点因超时被 filter 清理，成员抖动。行号 319 与 return 精确匹配。P1 合理。

### 8. [P1·confirmed] cluster.Node 的 OutToken/ReportHeartBeatTime/grpcClientConn 在心跳 goroutine 与 client 扇出/RegisterNode 间无锁读写：数据竞争

- **位置**：`pkg/rpc/server/end_node_cluster.go:256` · 维度：并发 · 单元：RPC
- **问题**：同一批 *cluster.Node 指针同时被后台心跳循环与 HTTP 触发的 client 扇出使用。心跳协程在 heartbeatToEndNode 里对失败节点写 node.OutToken=""(256/265/272 行)、成功时写 node.ReportHeartBeatTime(213/278 行)；RegisterNode handler 写 n.InToken/n.GetHeartBeatTime(73/75/82 行)；而 client 端 ReqToMultiEndNodeServer 在另一 goroutine 读 n.OutToken(end_node_rpc.go:494)。此外 node.GetGrpcClientConn()(node.go:39-41)在无锁下读写 node.grpcClientConn，可被心跳协程与 client 扇出并发调用同一 node。这些字段均无 mutex/atomic 保护，构成系统性 data race，可能读到半更新的 token 或并发写坏 grpcClientConn 指针。
- **证据**：heartbeatToEndNode: node.OutToken = "" / node.ReportHeartBeatTime = time.Now().Unix()（无锁）; end_node_rpc.go ReqToMultiEndNodeServer goroutine: nodeAuthInfo := &proto.NodeAuthInfo{Token: n.OutToken, ...}; node.go GetGrpcClientConn: node.grpcClientConn, err = grpc.Dial(...)（无锁写）。
- **核实**：同一批 *cluster.Node 指针被后台心跳循环与 HTTP 触发的 client 扇出共享。heartbeatToEndNode 无锁写 node.OutToken=""(256/265/272) 与 node.ReportHeartBeatTime(213/273/278)；RegisterNode 写 n.InToken/GetHeartBeatTime(76/82)；client 扇出 goroutine 读 n.OutToken(end_node_rpc.go:494)；node.go:37-43 GetGrpcClientConn 无锁 read-check-then-write node.grpcClientConn，可被心跳协程与 client 扇出并发调用同一 node。字段均无 mutex/atomic 保护，构成系统性 data race。P1 合理。

### 9. [P1·confirmed] cluster.Node 的 OutToken/ReportHeartBeatTime/grpcClientConn 在心跳 goroutine 与客户端扇出间无锁并发读写

- **位置**：`pkg/rpc/server/end_node_cluster.go:212` · 维度：并发 · 单元：RPC
- **问题**：cluster.Node（pkg/cluster/node.go:15-25）的 OutToken、ReportHeartBeatTime、grpcClientConn 字段无任何锁保护。心跳后台循环 heartbeatToEndNode 在失败时写 node.OutToken=""(256/265/272)、registerToEndNode 写 node.OutToken=token(212) 与 ReportHeartBeatTime(213)；与此同时 HTTP 扇出路径 client/end_node_rpc.go:494 读取 n.OutToken 构造 NodeAuthInfo，GetGrpcClientConn(node.go:39-42) 又会并发读写 node.grpcClientConn。这些是同一批 *cluster.Node 指针，在两条独立 goroutine 上并发访问，构成系统性数据竞争：可能读到半写的 token 或并发写 grpcClientConn 指针导致连接对象错乱/泄漏。RegisterNode handler 还会并发写 n.InToken/n.GetHeartBeatTime(76/82) 进一步加剧。
- **证据**：heartbeatToEndNode: node.OutToken = "" (无锁写); registerToEndNode: node.OutToken = token; client 侧: Token: n.OutToken (无锁读); node.go: node.grpcClientConn, err = grpc.Dial(...) (无锁写)
- **核实**：属实,系统性数据竞争成立。三条并发路径已逐一核实共享同一批 *cluster.Node 指针:(1) cmd/server.go 中同一 clusterMgr 同时注入 rpcServer(clusterState)与 httpServer(clusterNodes);(2) NodeManager.GetNodesWithFilter(node_manager.go:109)返回共享 map 中原指针给 HTTP fan-out,goroutine 内无锁读 n.OutToken(end_node_rpc.go:494)并读写 node.grpcClientConn(node.go:39-43,check-then-Dial 非原子);(3) 心跳后台 goroutine(rpcServer.Start 启动)在 registerToEndNode 写 node.OutToken/ReportHeartBeatTime(end_node_cluster.go:212-213)、heartbeatToEndNode 失败路径写 OutToken=""(256/265/272);(4) RegisterNode handler 并发写 n.InToken(76)/n.GetHeartBeatTime(82)。node.go:15-25 确认所有字段无锁。string 撕裂读与 grpcClientConn 指针并发写(连接泄漏)风险真实,-race 必报。P1 合理。

### 10. [P1·confirmed] ICMP 探测间隔/超时配置被静默忽略：pingLoop 使用包级全局常量而非配置值

- **位置**：`pkg/collecter/ping/icmp_ping_checker.go:88` · 维度：正确性 · 单元：COL
- **问题**：PingCollectorConfig.ICMPPingInterval/ICMPPingTimeout 经 WithICMPPingInterval/WithICMPPingTimeout 写入 ipc.interval/ipc.timeout，reconcile 循环也把它们换算后传入 newPingerInfo 存进 pi.interval/pi.timeout，但 pingLoop 实际使用的是包级全局变量：第 88 行 `pi.pinger.Interval = time.Second * time.Duration(pingInterval)`（全局恒为 5），第 111 行丢包判定用全局 `pingTimeout.Microseconds()`（恒为 1s）。pi.interval/pi.timeout 存而不用。用户在配置中调整 ICMP 探测频率/超时完全无效且无任何告警，cmd/server.go:189-190 传入的配置为死配置。TCP 侧（tcp_ping_checker.go）正确使用了 pi.interval/pi.timeout，两者行为不一致。
- **证据**：icmp_ping_checker.go:88 `pi.pinger.Interval = time.Second * time.Duration(pingInterval)`；:111 `if time.Now().UnixMicro()-p.SendTime.UnixMicro() > pingTimeout.Microseconds()`；全局定义 :13-17 `pingInterval = 5 // s` `pingTimeout = time.Second`；pi.interval/pi.timeout 在 pingLoop 中零引用
- **核实**：属实。icmp_ping_checker.go:88 确为 `pi.pinger.Interval = time.Second * time.Duration(pingInterval)`（包级全局，:15 恒为 5），:111 丢包判定用全局 `pingTimeout.Microseconds()`（:16 恒为 time.Second）。pingLoop（:67-122）中对 pi.interval/pi.timeout 零引用，而 Start 的 reconcile（:176-183、:192）确实把配置值换算后传入 newPingerInfo 存进这两个字段，存而不用。配置链路核实：ping_collector.go:82-87 WithICMPPingInterval/Timeout 写入 ipc.interval/ipc.timeout，cmd/server.go:189-190 传入 cfg.EndNode.Ping.ICMPPingInterval/Timeout，全程无告警地失效。对照 tcp_ping_checker.go:50（ticker 用 pi.interval）、:71（DialTimeout 用 pi.timeout），TCP 侧行为正确，不一致确凿。

### 11. [P1·confirmed] 域名配置的 ICMP 节点结果被 cleanupStaleResults 在每次远程 reload 时误删，100 样本环形缓冲反复清零

- **位置**：`pkg/collecter/ping_collector.go:150` · 维度：正确性 · 单元：COL
- **问题**：ICMP 聚合 key 中的 PingIp 取 `pi.pinger.IPAddr().String()`（解析后的 IP，icmp_ping_checker.go:105），而 cleanupStaleResults 的 hostSet 用配置中的 node.Host 构建（ping_collector.go:137-140）。当节点以域名配置时，extractHostFromNodeName 从 "{geo}_{isp}_{ip}" 解析出的是 IP，永远不在 hostSet 中，结果被 delete。叠加 node_manager.go:72-73 中 remote reload 回调无条件调用 updateNodes+notifyChange（即使节点列表毫无变化），意味着配置了 remote source（update_interval>0）且 ICMP 目标为域名时，该目标的全部延迟历史（avg/max/min/stdev/loss 的 100 样本窗口）每个 reload 周期被整体清空一次，聚合统计长期只基于个位数样本，且每周期刷一条 "removed stale ping result" 日志。TCP 不受影响（key 用配置 host:port）。
- **证据**：ping_collector.go:117 key=`fmt.Sprintf("%s_%s_%s", result.Geo, result.ISP, result.PingIp)`，ICMP 的 PingIp 来自 icmp_ping_checker.go:105 `pi.pinger.IPAddr().String()`；ping_collector.go:139 `hostSet[n.Host]`、:149-151 不在 hostSet 即 delete；node_manager.go:71-74 StartReload 回调每次 reload 无条件 `nm.updateNodes(nodes); nm.notifyChange()`
- **核实**：属实。ICMP 结果 PingIp 取 pi.pinger.IPAddr().String()（icmp_ping_checker.go:105/:115，域名会被 pro-bing NewPinger 解析为 IP），聚合 key 为 "{geo}_{isp}_{ip}"（ping_collector.go:117）；cleanupStaleResults 的 hostSet 用配置 node.Host（域名）构建（:137-140），extractHostFromNodeName 取下划线分隔的最后一段即解析后 IP（:161-177），域名节点必然不在 hostSet → delete + Info 日志（:149-152）。触发链核实：node_manager.go:71-74 StartReload 回调每个 tick 无条件 updateNodes+notifyChange（不比对内容是否变化），notifyChange 调 pc.cleanupStaleResults（ping_collector.go:66-68）。删除后 startChecker（:122-126）在下一条结果到来时以全新 PingResult 重建，100 样本窗口从 1 开始，聚合长期只基于 update_interval 内累积的少量样本。TCP key 用配置 host:port，不受影响。触发需 remote 源 update_interval>0 且 ICMP 节点配置为域名，均为文档化支持的配置。

### 12. [P1·confirmed] `go pi.pinger.Run()` 丢弃返回错误：无 CAP_NET_RAW 时全部 ICMP 探测静默失败且永不重试

- **位置**：`pkg/collecter/ping/icmp_ping_checker.go:89` · 维度：错误处理 · 单元：COL · ⚠️协议相关（需人工核对上游）
- **问题**：pinger 设置了 SetPrivileged(true)（icmp_ping_checker.go:26），需要 root 或 CAP_NET_RAW。pingLoop 中第 89 行 `go pi.pinger.Run()` 完全丢弃 Run 的 error 返回值：无权限时 Run 在创建 raw socket 或首次 sendICMP 时即返回错误（pro-bing runLoop `if err := p.sendICMP(conn); err != nil { return err }`），无任何日志。此后 pingLoop goroutine 仍带着 500ms ticker 空转，pingerInfo 留在 pingerMap 中，reconcile 循环因 key 已存在永不重建——ICMP 功能整体静默失效，运维只能看到指标缺失而无任何错误线索。另外 :194 的 `log.Error("start ping failed", ...)` 也未携带 err 字段。应捕获 Run 错误、记日志并将失败的 pingerInfo 从 map 中移除以便下个 tick 重试。
- **证据**：icmp_ping_checker.go:89 `go pi.pinger.Run()`（Run 签名为 `func (p *Pinger) Run() error`）；:26 `pinger.SetPrivileged(true)`；:194 `log.Error("start ping failed", "geo", node.Geo, "isp", node.ISP, "host", node.Host)` 未含 err
- **核实**：属实，且上游行为已用本仓库 go.mod 钉定的 pro-bing v0.6.1 本地模块源码（/home/node/go/pkg/mod/github.com/prometheus-community/pro-bing@v0.6.1/ping.go）实证，非凭记忆：Run() 签名为 `func (p *Pinger) Run() error`（:518），RunWithContext 中 `if conn, err = p.listen(); err != nil { return err }`，listen() 走 icmp.ListenPacket(ip4:icmp)（privileged 模式，因 icmp_ping_checker.go:26 SetPrivileged(true)），无 root/CAP_NET_RAW 时创建 raw socket 即失败返回错误。icmp_ping_checker.go:89 `go pi.pinger.Run()` 丢弃该错误，无任何日志。后续后果链核实：pi.start（:50-58）中 newPinger 只做解析不建 socket，成功后 pingerInfo 已入 pingerMap，reconcile（:186-189）因 key 存在只做 transfer 永不重建；pingLoop 带 500ms ticker 空转。:194 log.Error 确未携带 err 字段。

### 13. [P1·confirmed] basePingChecker.Stop 将 isRunning 置为 true（应为 false），ICMP checker 停止后状态永远为 running 且无法重启，SetPingCheck 成为单向开关

- **位置**：`pkg/collecter/ping/ping.go:222` · 维度：正确性 · 单元：COL
- **问题**：basePingChecker.Stop 中 `bpc.isRunning.Store(true)` 显然是笔误（cancel 之后应 Store(false)）。TcpPingChecker 覆写了 Stop 修正为 Store(false)（tcp_ping_checker.go:201-206），但 IcmpPingChecker 继承基类实现。后果：(1) Stop 后 IsRunning() 恒返回 true，状态失真；(2) IcmpPingChecker.Start 开头 `if ipc.isRunning.Load() { return }`（icmp_ping_checker.go:155）导致停止后再调 Start 直接 no-op，且基类 ctx 已 cancel 无法复用。触发路径：center 通过 RPC SetPingCheck(enable=false)（pkg/rpc/server/end_node_metric.go:13-14）调用 StopPing 后，再 SetPingCheck(enable=true) 只改配置位、checker 无法恢复，ping 功能直到进程重启前永久下线。
- **证据**：ping.go:219-224 `func (bpc *basePingChecker) Stop() { if bpc.cancel != nil && bpc.isRunning.Load() { bpc.cancel(); bpc.isRunning.Store(true) } }`；对照 tcp_ping_checker.go:204 `tpc.isRunning.Store(false)`；end_node_metric.go:13-14 disable 时调 s.pingCollector.StopPing()
- **核实**：属实。ping.go:219-224 basePingChecker.Stop 中 cancel 后 `bpc.isRunning.Store(true)`，明显应为 false（对照 tcp_ping_checker.go:201-206 覆写版本 Store(false)）。IcmpPingChecker 无 Stop 覆写（icmp_ping_checker.go 全文核实），继承缺陷实现。触发路径真实：pkg/rpc/server/end_node_metric.go:11-18 SetPingCheck(enable=false) 调 s.pingCollector.StopPing() → ping_collector.go:195-204 逐 checker.Stop()；再 enable=true 时 handler 只置 s.cfg.Ping.EnableICMPPing，不调任何重启逻辑,即便调 Start 也会被 icmp_ping_checker.go:155 的 isRunning 守卫挡回，且基类 ctx 已 cancel 无法复用——ICMP ping 直到进程重启前无法恢复,IsRunning() 恒 true 状态失真。

### 14. [P2·confirmed] registerOrHeartBeatToEndNode 遇到无效节点用 return 直接退出整个循环，导致其后所有节点漏发心跳/注册

- **位置**：`pkg/cluster/end_node_cluster.go:319` · 维度：正确性 · 单元：CLU
- **问题**：在遍历所有节点发心跳/注册的循环中,当某节点 !node.IsValid() 时执行的是 `return` 而非 `continue`(第320行)。这会直接结束整个 registerOrHeartBeatToEndNode 函数,map 迭代中排在该无效节点之后的所有有效节点本轮全部被跳过。map 遍历顺序随机,因此只要集群里存在任意一个暂时失效(或刚 addRemoteNode 进来 GetHeartBeatTime=0、IsValid 仅靠 CreateTime 撑 60s)的节点,就会概率性地让部分正常节点长期收不到心跳,进而被对端判定超时下线,集群成员表持续抖动。应改为 continue。
- **证据**：end_node_cluster.go:315-320 `if !node.IsValid() { log.Info("skip heartbeat to invalid node", ...) return }` —— 位于 `for _, node := range s.clusterState.GetAllNode()` 循环体内,应为 continue。
- **核实**：控制流缺陷属实：pkg/rpc/server/end_node_cluster.go:315-320（注意实际文件在 pkg/rpc/server/ 而非 finding 所写的 pkg/cluster/），`if !node.IsValid()` 分支内是 `return`（:319），位于 `for _, node := range s.clusterState.GetAllNode()`（:308）循环体内，会终止整轮心跳/注册，其后节点全部漏发，应为 continue。但影响被 finding 高估：end_node_server.go:203-212 的 filter goroutine 每 20s（clearInvalidNodeInterval）清除非 local 的 invalid 节点，故无效节点最多存活 ~20s，最多导致其后节点漏 1-3 个心跳 tick（间隔 10s），远小于 NodeTimeOut=60s，孤立事件不足以让正常节点被对端判超时下线；且 isLocal 节点在 :313 先刷新 ReportHeartBeatTime 恒有效不触发此分支。『长期收不到心跳、成员表持续抖动』仅在节点频繁死亡的持续 churn 下才近似成立。确认为 bug，但降为 P2。

### 15. [P2·confirmed] NodeManager.Filter 两段式(RLock 复制→Lock 换指针)之间的并发 Add/Delete 会被静默丢失

- **位置**：`pkg/cluster/node_manager.go:122` · 维度：并发 · 单元：CLU
- **问题**：Filter 先在 RLock 下把存活节点复制进 tmpNM,RUnlock 后再取 Lock 把 nm.nodes 指针整体换成 tmpNM。两段临界区之间(RUnlock 与 Lock 之间)如果有 Add(RegisterNode 注册新节点)或 Delete 发生在旧 map 上,换指针后这些改动全部丢失:刚注册成功、已返回 token 给对端的节点会凭空消失,对端却认为注册成功不再重注册,直至超时。应在单个写锁内完成过滤重建,或用删除而非整体换指针。
- **证据**：node_manager.go:122-141 Filter:先 `nm.lock.RLock()`...`nm.lock.RUnlock()`(复制阶段),再 `nm.lock.Lock(); defer Unlock(); nm.nodes = tmpNM`(换指针阶段);两段之间 Add(:31)/Delete(:44) 作用于旧 *nodes 被丢弃。
- **核实**：竞态窗口核实存在：node_manager.go:122-141，Filter 先 RLock 复制存活节点到 tmpNM 后 RUnlock(:136)，再另取 Lock 将 nm.nodes 整体换成 tmpNM(:138-140)；两段临界区之间发生在旧 map 上的 Add（RegisterNode end_node_cluster.go:93 / addRemoteNode :378）在换指针后确实丢失。filter goroutine 每 20s（end）/10s（center）运行，与注册 RPC 并发。但影响有自愈路径：被丢的节点下次心跳到本地时 HeartBeat handler Get 返回 nil→code 104(end_node_cluster.go:123-127)，对端 heartbeatToEndNode 收到非零 code 置 OutToken=""(:272)→下一 tick 重新注册，约 10-20s 内恢复，并非『直至超时』；且竞态窗口仅 RUnlock 到 Lock 之间的微秒级间隙，触发概率低。确认为真实 lost-update 缺陷，但降为 P2。

### 16. [P2·confirmed] AuthRemoteNode 对 GetHeartBeatTime==0 的节点跳过超时校验，空 InToken 的 gossip 节点可被空 token 冒名鉴权通过

- **位置**：`pkg/cluster/cluster.go:98` · 维度：安全 · 单元：CLU
- **问题**：AuthRemoteNode 的超时判断是 `n.GetHeartBeatTime != 0 && n.GetHeartBeatTime+HeartBeatTimeout < now`——当本地记录的 GetHeartBeatTime 为 0 时超时检查被完全跳过。通过 addRemoteNode(end_node_cluster.go:378)从心跳 gossip 学来的节点,其 InToken=""、GetHeartBeatTime=0。攻击者(或误配节点)只要知道该节点的 host/port/cluster/name 四元组,发送 NodeAuthInfo{Token:"", Node:<该四元组>} 即可:Get 命中→Compare 通过→InToken ""=="" 通过→超时被跳过→鉴权成功并被刷新心跳,随后可调用 AddUsers/DeleteUsers/GetSub 等全部非注册 RPC。虽然 gRPC codec 以 cluster.Token 为 AES key 作第一道门槛(下层已记该 key 无 KDF、空 token 退化为公开常量),但本层的『空 token 即通过』使 node 级第二因子形同虚设,二者叠加可致越权。应对未完成注册(InToken 为空或 GetHeartBeatTime==0)的节点直接拒绝鉴权。
- **证据**：cluster.go:96-99 `else if (*node).InToken != n.InToken { return wrong token } else if n.GetHeartBeatTime != 0 && n.GetHeartBeatTime+int64(HeartBeatTimeout) < time.Now().Unix() {...}`;gossip 节点来源 end_node_cluster.go:378-385 `Add(&cluster.Node{ InToken:"", GetHeartBeatTime:0, ...})`。
- **核实**：核实属实。cluster.go:98 `n.GetHeartBeatTime != 0 && ...` 当本地记录心跳为 0 时整段超时判断被短路跳过。end_node_cluster.go:378-385 addRemoteNode 学来的 gossip 节点确实 InToken=""、GetHeartBeatTime=0。攻击路径成立：AuthRemoteNode 里 Get 命中→Compare(四元组)通过→(*node).InToken("")!=n.InToken("") 为假→GetHeartBeatTime==0 使超时被跳过→进入 else 鉴权成功并刷新心跳。上层 authRemoteNode(end_node_server.go:130) 因 err==nil 直接放行。node 级第二因子对空 token gossip 节点形同虚设,属真实鉴权边界缺陷,行号准确,P2 可接受。

### 17. [P2·confirmed] 节点被 Filter/Delete 移除时缓存的 grpcClientConn 从不 Close，节点频繁增删下连接持续泄漏

- **位置**：`pkg/cluster/node.go:44` · 维度：资源 · 单元：CLU
- **问题**：Node 内缓存了惰性建立的 grpcClientConn,但 NodeManager.Delete(node_manager.go:44)、Filter(:140)、Clear(:93) 以及 CenterClusterManager.DeleteNode/Filter 在丢弃 *Node 时都没有调用其 grpcClientConn.Close()。集群成员表在超时清理/重注册场景下会不断删除再重新学习节点,每个被丢弃 Node 的底层 gRPC 连接(含其 keepalive goroutine 与 fd)都成为悬挂资源。全仓无一处对 cluster 节点连接调用 Close(grep 确认)。应在节点移除时释放其连接。
- **证据**：node_manager.go:44 Delete 仅 `delete((*nm.nodes), key)`;:140 Filter 换指针丢弃旧 *Node;node.go:24 `grpcClientConn *grpc.ClientConn` 无 Close 调用点(grep server 包仅文件句柄 Close)。
- **核实**：核实属实。grep 全 cluster 包仅 node.go:41 Dial,无任何 grpcClientConn.Close 调用点。Delete(node_manager.go:44)/Filter(:122-141 换指针)/Clear(:90-94 换 map) 丢弃 *Node 时均不释放其惰性建立的连接。end 侧节点通过 registerToEndNode/heartbeatToEndNode→GetGrpcClientConn 确会 Dial 对端,超时被 FilterTimeoutNode 丢弃后连接及其 keepalive goroutine/fd 悬挂,节点增删下持续泄漏。行号准确。

### 18. [P2·confirmed] Center HeartBeat 不校验集群 token 即接纳并 gossip 任意上报节点，形成节点表投毒/SSRF 放大面

- **位置**：`pkg/rpc/server/center_node_server.go:68` · 维度：安全 · 单元：CLU
- **问题**：Center 的 HeartBeat 处理仅校验 node.IsComplete(),未做任何 cluster token 鉴权,便把上报者写入对应 clusterName 的成员表(cluster.Add,第75/85行),并在响应 NodesMap 中把该 cluster 全部有效节点回吐给调用方。任何能通过 codec 层(cluster.Token 为 AES key)的一方都能向任意 clusterName 注入伪造 host:port 节点;这些节点经 addRemoteNode 下发到真实 end 节点后,end 会主动 GetGrpcClientConn Dial 攻击者指定地址(结合下层已记的 RPC 无重放防护/弱 key),构成对内网地址的连接放大/SSRF。center 侧还叠加了本报告 F1 的 NodeManager.Get(:69) 无锁读崩溃。center 应对上报做 cluster token 校验。
- **证据**：center_node_server.go:68-86:`if cluster := s.clusters.GetCluster(clusterName); cluster != nil { if n := cluster.Get(nodeName); n != nil { n.GetHeartBeatTime=... } else { cluster.Add(node) } ...}` —— 无 IsSameCluster/token 校验;对比 end 的 RegisterNode(end_node_cluster.go:39)有 IsSameCluster。
- **核实**：核实属实,且比报告更严重。center_node_server.go:68-86 HeartBeat 仅校验 IsComplete,无 IsSameCluster/token 校验即写入 cluster.Add 并在 NodesMap 回吐全 cluster 节点。进一步核实:center 客户端 heartbeatToCenterNode(end_node_cluster.go:344-352) 以 Token="" 且不带 ForceCodec 发送,center 服务端 grpc.NewServer()(:119)无 codec 无 interceptor——即根本无加密门槛,任何可达 center 端口者都能向任意 clusterName 注入伪造 host:port 节点,经 addRemoteNode 下发后 end 主动 Dial 攻击者地址,节点表投毒/SSRF 面成立。行号准确。

### 19. [P2·confirmed] authRemoteNode 对 nil NodeAuthInfo/Node 无防护：反射 .Elem().Interface() 触发 panic 崩溃整个 gRPC server

- **位置**：`pkg/rpc/server/end_node_server.go:122` · 维度：错误处理 · 单元：RPC
- **问题**：authRemoteNode 无条件执行 reqValue.Elem().FieldByName("NodeAuthInfo").Elem().Interface().(proto.NodeAuthInfo)。若请求的 NodeAuthInfo 为 nil 指针，.Elem() 得到 zero Value，.Interface() 会 panic(reflect: call of reflect.Value.Interface on zero Value)。grpc-go 默认不 recover handler/interceptor 的 panic，将导致服务进程崩溃。虽然到达此处需先通过 cluster-token 加密 codec 解码（即需集群成员身份），但任一集群成员或构造出合法加密帧的一方发一个缺失 NodeAuthInfo 的请求即可打崩对端 end 节点，属可用性/DoS 缺口；同理若 NodeAuthInfo.Node 为 nil，后续 node.Host 等解引用也会 panic。应在反射取值前判空并返回鉴权失败。
- **证据**：nodeAuthInfo := reqValue.Elem().FieldByName("NodeAuthInfo").Elem().Interface().(proto.NodeAuthInfo)（无 IsNil/IsValid 检查）。
- **核实**：authRemoteNode(120-143) line 122 无条件 reqValue.Elem().FieldByName("NodeAuthInfo").Elem().Interface().(proto.NodeAuthInfo)，无 IsNil/IsValid 检查。NodeAuthInfo 为 nil 指针时 .Elem() 得 zero Value，.Interface() 触发 panic；grpc-go 默认不 recover handler/interceptor panic，请求 goroutine 未捕获 panic 会终止整个进程。interceptor(146-164)在所有方法(含 RegisterNode，special-case 在 123 行之后)前调用 authRemoteNode，故 nil NodeAuthInfo 必先 panic。触发需先过 cluster-token 加密 codec(227 注册)即需集群成员身份，故 P2 定级恰当，属可用性/DoS 缺口。

### 20. [P2·confirmed] authRemoteNode 本地 token 等价旁路：InToken 等于本地节点 token 即无条件放行 per-node 鉴权

- **位置**：`pkg/rpc/server/end_node_server.go:130` · 维度：安全 · 单元：RPC
- **问题**：authRemoteNode 中 `if err := s.clusterState.AuthRemoteNode(&node); err != nil && localNode.Token != node.InToken` —— 当 AuthRemoteNode 返回错误（节点不存在/token 不匹配/超时）时，只要请求携带的 InToken 恰等于本地节点自身的 Token 即放行。localNode.Token 是进程启动时生成的随机 uuid(end_node_init.go:33)，用于本地自调用，外部攻击者难以获知，故非直接可被外部利用；但该短路意味着任何持有该值的调用方可绕过全部 per-node 校验，且旁路成功时返回的是请求方自带的、未经校验的 node 元信息(Host/Name)用于后续日志。鉴权关键路径以这种隐式等价旁路实现，脆弱且难以审计，建议改为显式的本地自调用判定（如 isLocal 标志）而非 token 值比较。
- **证据**：if err := s.clusterState.AuthRemoteNode(&node); err != nil && localNode.Token != node.InToken { ...返回鉴权失败... } return true, nil, nodeAuthInfo.Node（err!=nil 但 token==localNode.Token 时落到放行分支）。
- **核实**：authRemoteNode line 130 `if err := s.clusterState.AuthRemoteNode(&node); err != nil && localNode.Token != node.InToken` —— AuthRemoteNode 返回错误时，只要 InToken==localNode.Token 即落到放行分支 return true。localNode.Token 为进程启动随机 uuid(cluster/end_node_init.go:33 localNode.Token = uuid.New().String())，外部攻击者难获知，故非直接可外部利用，但确为隐式 token 等价旁路，绕过全部 per-node 校验且旁路成功返回请求方自带未校验的 node 元信息。代码行为与描述完全一致，P2(脆弱/难审计的加固项)定级恰当。

### 21. [P2·confirmed] 全局限流信号量 ch 在 wg.Add 前阻塞式获取，持有调用方 goroutine：大集群扇出与其它并发调用相互挤占易阻塞

- **位置**：`pkg/rpc/client/end_node_rpc.go:478` · 维度：资源 · 单元：RPC
- **问题**：所有 ReqToMultiEndNodeServer 调用共享一个容量 64 的全局 `ch`(MaxConcurrencyClientNum)。扇出循环里对每个节点先 `ch <- struct{}{}` 再 wg.Add(1) 起 goroutine。该获取是阻塞式且在调用方 goroutine 内同步执行：当集群规模大或并发 HTTP 请求多、64 个令牌被占满时，调用方会卡在 `ch <-` 上（且此时并未受传入 ctx 控制，无超时），一个慢/挂起的下游节点足以拖垂整个进程的扇出吞吐。信号量为进程级全局而非每次调用独立，跨请求相互影响，缺乏背压/超时保护。
- **证据**：var ch = make(chan struct{}, MaxConcurrencyClientNum)（全局）; for _, node := range c.nodes { ...; ch <- struct{}{}; wg.Add(1); go func(n *cluster.Node){ defer func(){ <-ch; wg.Done() }(); ... } }。
- **核实**：属实。end_node_rpc.go:16,94 确认 ch 为包级全局、容量 64(MaxConcurrencyClientNum);478 行 `ch <- struct{}{}` 在调用方 goroutine 内先于 wg.Add 阻塞获取,普通 channel send 不受任何 ctx/超时控制。且下游 RPC 的 reqCtx 继承 HTTP request ctx(gin 默认无 deadline),挂起的下游节点可无限期占用令牌;所有 HTTP handler 各自 NewEndNodeClient 但共享同一全局信号量,跨请求相互挤占成立。P2 恰当。

### 22. [P2·confirmed] 测试缺口：center 心跳、unaryServerInterceptor/authRemoteNode 鉴权路径与心跳漂移校验均无单测

- **位置**：`pkg/rpc/server/center_node_server.go:31` · 维度：测试缺口 · 单元：RPC
- **问题**：架构约束#9 要求每个可测模块有 *_test.go。现有 7 个 server 测试仅覆盖 inbound/rotate/reset/cluster-user/nodegroups/wiring，鉴权与集群通信这些最敏感的正确性/安全路径完全没有测试：CenterNodeServer.HeartBeat（漂移拒绝、新集群/新节点加入、NodesMap 过滤）、EndNodeServer.unaryServerInterceptor（OnlyGateway 过滤、authRemoteNode 反射鉴权成功/失败、methodRspMap 覆盖完整性、nil NodeAuthInfo 边界）、HeartBeat 时间戳漂移与 digest 比对、RegisterNode 重名/异集群/wrong-node 分支均无覆盖。上述多条 P1/P2 缺陷（共享单例竞争、return/continue、nil panic）本可由针对性单测拦截。
- **证据**：pkg/rpc/server/*_test.go 中 grep CenterNodeServer|unaryServerInterceptor|authRemoteNode 无任何命中；center_node_server.go 与 end_node_server.go 无对应测试文件。
- **核实**：属实。pkg/rpc/server/ 仅 7 个测试文件(cluster_user/inbound/reset_auth_token/reset_user_traffic/rotate/wiring/fastadd_params),grep CenterNodeServer|unaryServerInterceptor|authRemoteNode 在全部 *_test.go 中零命中;center_node_server.go 与 end_node_server.go 无对应测试文件。PROJECT_GUIDE.md:76 确认架构约束 #9('每个可测模块对应 *_test.go')存在。CenterNodeServer.HeartBeat(漂移拒绝/新集群/NodesMap 过滤,center_node_server.go:31-88)、unaryServerInterceptor/authRemoteNode 反射鉴权(end_node_server.go:120-164)、RegisterNode 各分支(end_node_cluster.go:40-101)均无覆盖。

### 23. [P2·confirmed] SetGatewayModel/SetPingCheck 无锁写 s.cfg，与拦截器每请求读 OnlyGateway 构成数据竞争

- **位置**：`pkg/rpc/server/end_node_inbound.go:161` · 维度：并发 · 单元：RPC
- **问题**：SetGatewayModel handler 写 s.cfg.OnlyGateway = ...（161 行），SetPingCheck 写 s.cfg.Ping.EnableICMPPing（end_node_metric.go:16），而 unaryServerInterceptor 在每个 RPC 上读 s.cfg.OnlyGateway（end_node_server.go:148）。s 是全局单例 endNodeServer，这些写与拦截器的读跨 goroutine 并发且无同步 → 数据竞争，-race 下必报；OnlyGateway 是网关白名单过滤的关键开关，撕裂读还可能导致鉴权/过滤瞬时行为不确定。应将可变运行期配置用原子/互斥保护。
- **证据**：SetGatewayModel: s.cfg.OnlyGateway = setGatewayModelReq.GetEnableGatewayModel(); interceptor: if s.cfg.OnlyGateway && !isOnlyGatewayMethod(info.FullMethod)
- **核实**：属实。end_node_inbound.go:161 SetGatewayModel handler 无锁写 s.cfg.OnlyGateway;end_node_metric.go:16 SetPingCheck 无锁写 s.cfg.Ping.EnableICMPPing;end_node_server.go:148 拦截器在每个入站 RPC 上无锁读 s.cfg.OnlyGateway。s 为全局单例 endNodeServer(end_node_server.go:26),gRPC handler 每请求独立 goroutine,写与并发请求的拦截器读构成数据竞争,-race 必报。OnlyGateway 为网关过滤关键开关,竞争窗口内过滤行为不确定。P2 恰当。

### 24. [P2·confirmed] AddInbound 直接把调用方 base64 xray 原生 JSON 灌入 AddInboundNative，跨层暴露原生类型且可注入任意入站

- **位置**：`pkg/rpc/server/end_node_inbound.go:78` · 维度：架构 · 单元：RPC
- **问题**：AddInbound 将 InboundInfo 解 base64（失败则原样当 JSON）后直接 rapi.AddInboundNative(inboundJSON)（78 行），未经 core/contracts 领域模型校验即把 xray 原生配置写入运行进程。这违反架构约束#2『上层只用 core/contracts 领域模型，禁止跨层用 xray 原生类型』；安全上等价于允许任意持集群 token 者注入任意监听/协议/路由的 inbound（端口占用、绕过限速/统计等）。应经领域模型解析校验后再落地，而非透传原生 JSON。
- **证据**：inboundJSON, decErr := base64.StdEncoding.DecodeString(inboundOpReq.GetInboundInfo()); if decErr != nil { inboundJSON = []byte(inboundOpReq.GetInboundInfo()) }; err = rapi.AddInboundNative(inboundJSON)
- **核实**：end_node_inbound.go:73-78 与 evidence 逐字一致：InboundInfo base64 解码（失败则原样当 JSON）后直接 rapi.AddInboundNative(inboundJSON)，无任何 core/contracts 领域模型解析校验。PROJECT_GUIDE.md:68 确认架构约束#2 原文禁止上层跨层使用 xray 原生配置类型，而该 RPC handler 明确透传原生 xray JSON（第 72 行注释自认）。触发路径真实可达：AddInbound 在 methodRspMap 中注册且经 unaryServerInterceptor 鉴权，finding 已正确限定为'持集群 token 者'，未夸大为未鉴权。P2 合理：违反明文架构约束 + 原生配置绕过领域校验落地运行进程。

### 25. [P2·confirmed] 鉴权拦截器/心跳并发路径缺测试覆盖：nil panic、共享 Rsp 竞争、return 心跳饥饿、center 无鉴权均无用例

- **位置**：`pkg/rpc/server/end_node_server.go:120` · 维度：测试缺口 · 单元：RPC
- **问题**：现有 *_test.go 覆盖了 FastAddInbound 参数矩阵、ListInbound、Rotate/Reset、UpsertClusterUsers 语义等 handler 逻辑，但本层最危险的路径无任何测试：(1) authRemoteNode 对缺失 NodeAuthInfo 的 nil-panic 行为；(2) 鉴权失败复用 methodRspMap 单例的并发串号；(3) registerOrHeartBeatToEndNode 的 return-vs-continue 心跳饥饿；(4) CenterNodeServer.HeartBeat 无 token 校验/伪节点注入；(5) cluster.Node 字段跨 goroutine 竞争（无 -race 并发用例）。这些正是最易回归且后果最重的分支，违反约束#9『每个可测模块应有 *_test.go』的实质意图。建议补齐拦截器鉴权分支表驱动测试 + go test -race 的心跳并发用例。
- **证据**：server/*_test.go 仅覆盖 handler 业务分支；authRemoteNode/unaryServerInterceptor/heartBeat 循环/center HeartBeat 无对应测试文件与并发(-race)用例
- **核实**：grep 全部 server/*_test.go，authRemoteNode/unaryServerInterceptor/HeartBeat/RegisterNode 零命中，且无任何 -race 并发用例。所列五个未覆盖危险路径逐一核实真实存在：(1) end_node_server.go:122 对 nil NodeAuthInfo 做 reflect .Elem().Interface() 必 panic（grpc-go 默认不 recover）；(2) :137-140 鉴权失败反射改写 methodRspMap 全局单例 Rsp 并返回，并发下竞争/串号真实；(3) end_node_cluster.go:315-320 遇 invalid 节点 return 而非 continue，中断本轮对其余所有节点的心跳；(4) center_node_server.go:31-88 HeartBeat 无 token 校验、Start()(:119) 无拦截器、客户端为 insecure 明文连接（cluster/node.go:41），伪节点可注入 NodesMap；(5) cluster.Node 字段（OutToken/ReportHeartBeatTime 等）跨 goroutine 无锁读写。约束#9 见 PROJECT_GUIDE.md:76。P2 合理。

### 26. [P2·confirmed] 鉴权拦截器/RegisterNode/HeartBeat/center 心跳等安全与集群核心路径无单元测试

- **位置**：`pkg/rpc/server/end_node_server.go:146` · 维度：测试缺口 · 单元：RPC
- **问题**：架构约束 9 要求每个可测模块有 *_test.go，但 pkg/rpc 现有 7 个测试仅覆盖 FastAddInbound 参数矩阵、ListInbound、Rotate/Reset、UpsertClusterUsers 同步语义、NodeGroups、wiring。安全与集群一致性核心零覆盖：unaryServerInterceptor/authRemoteNode（含 OnlyGateway 过滤、nil NodeAuthInfo、鉴权失败响应）、RegisterNode 的 token 生成/重名/异 cluster 分支、EndNode/Center HeartBeat（含时钟漂移、digest 比对、NodesMap 过滤）、addRemoteNode 节点发现，以及 client 的 ReqToMultiEndNodeServer 扇出（成功/失败/未注册跳过/信号量并发）均无测试。上述 F1-F6 缺陷之所以能潜伏，正因这些路径无回归防护。
- **证据**：server 目录测试文件仅: end_node_cluster_user_test / end_node_inbound_test / reset_auth_token_test / reset_user_traffic_test / rotate_test / wiring_test / fastadd_params_test；client 目录无任何 *_test.go
- **核实**：与 idx 2 高度重叠但独立核实均属实：ls 确认 server 目录测试文件恰为 evidence 所列 7 个，client 目录（context.go/end_node_rpc.go/end_node_rpc_type.go）零测试文件。ReqToMultiEndNodeServer 扇出逻辑真实存在于 client/end_node_rpc.go:458 起（含信号量 ch、succList/failedList 锁、未注册跳过），无任何测试。RegisterNode 的 token 生成/重名/异 cluster 分支（end_node_cluster.go:19-101）、EndNode/Center HeartBeat 时钟漂移与 digest 比对（:103-162, center:31-88）、addRemoteNode(:363) 均零覆盖。PROJECT_GUIDE.md:76 约束#9 明文要求每个可测模块有 *_test.go。注意本条与 idx 2 实质为同一缺陷的两次报告，合并处理即可。

### 27. [P2·confirmed] RemoteLoader 不强制 HTTPS 且响应体无大小上限：节点源被劫持可将全集群节点变成内网 TCP 端口扫描器，并可 OOM 节点

- **位置**：`pkg/collecter/ping/remote_loader.go:44` · 维度：安全 · 单元：COL
- **问题**：RemoteLoader.Load 对配置 URL 直接 GET 并 yaml 解码整个 resp.Body，无 io.LimitReader、无 Content-Length 校验、不要求 https、无响应内容鉴权。若 node source 配置为 http:// 或远端被攻破，MITM/控制者可下发任意节点列表：(1) TcpPingChecker 会对任意 host:port（含 127.0.0.1、内网网段）以配置间隔发起 net.DialTimeout，连通性与 RTT 通过 GetPingMetric RPC → center 的 prometheus /metrics（pkg/http/prometheus_desc/ping_desc.go）对外可读，构成低速内网端口扫描+结果回传原语；ICMP 侧同理可作为 flood 源；(2) 超大 YAML 响应直接读入内存可 OOM 探测节点。建议：限制响应体大小（如 1-4MB）、告警非 https 源、对节点数量设上限。
- **证据**：remote_loader.go:44-56 `resp, err := l.client.Do(req)` 后直接 `yaml.NewDecoder(resp.Body).Decode(&nf)`，无大小限制；tcp_ping_checker.go:71 对加载节点执行 `net.DialTimeout("tcp", addr, pi.timeout)`；探测结果经 end_node_metric.go:28 GetPingResults 导出至 center prometheus
- **核实**：属实。remote_loader.go:35-57 对配置 URL 直接 GET 后 yaml.NewDecoder(resp.Body).Decode 整读,无 io.LimitReader/Content-Length 校验/https 强制/节点数上限;NewRemoteLoader(:24-32)对 scheme 无任何检查。下游原语核实:加载节点默认 Usage 含 tcp、Port 默认 80(:60-67),tcp_ping_checker.go:71 对任意 host:port(含内网地址,无网段过滤)按配置间隔 net.DialTimeout,连通性/RTT 经 ping_collector.GetPingResults → end_node_metric.go:20-32 GetPingMetric RPC 导出,pkg/http/prometheus_desc/ping_desc.go 以 geo/isp/ping_ip 标签暴露。低速内网扫描+结果回传与超大响应 OOM 两条路径均成立。前提是节点源配置为 http 或远端被攻破,属加固类问题,P2 合理。

### 28. [P2·confirmed] pro-bing 回调经无缓冲 channel 交接与 pingLoop 退出存在竞态：节点删除/停机时 runLoop 永久阻塞，goroutine 与 raw socket 泄漏

- **位置**：`pkg/collecter/ping/icmp_ping_checker.go:82` · 维度：资源 · 单元：COL · ⚠️协议相关（需人工核对上游）
- **问题**：OnSend/OnRecv 回调向无缓冲的 sendCh/recvCh 发送（icmp_ping_checker.go:70-87），唯一接收者是 pingLoop 的 select。pro-bing v0.6.1 的 runLoop 在同一 goroutine 内同步调用 processPacket→OnRecv/sendICMP→OnSend。当 pi.ctx 取消时 pingLoop 走 Done 分支调用 pi.pinger.Stop() 后 return，而 Stop() 只 close(p.done) 不等待 runLoop 退出；若此刻 runLoop 正阻塞在 OnRecv/OnSend 回调内向已无接收者的 unbuffered channel 发送，则它永远回不到 select 检查 p.done——runLoop goroutine、errgroup 及 raw ICMP socket fd 永久泄漏。每次节点 reload 删除或 StopPing 都有一次竞态窗口（回调在飞行中），远程源频繁增删节点时会累积。修复：给 sendCh/recvCh 加缓冲并在回调中用 select+ctx.Done 非阻塞发送。
- **证据**：icmp_ping_checker.go:70-74 `pi.pinger.OnSend = func(p *ping.Packet) { sendCh <- ... }`、:82-87 OnRecv 同样阻塞发送，channel 声明 :68-69 无缓冲；pro-bing@v0.6.1 ping.go runLoop 内 `err := p.processPacket(r)` 同步调用 OnRecv，Stop() 仅 `close(p.done)` 不等待
- **核实**：属实,上游行为已读本地钉定的 pro-bing v0.6.1 源码实证:runLoop(ping.go:609-658)在同一 goroutine 内同步调用 processPacket(→:879-880 调 OnRecv)与 sendICMP(→:946-954 调 OnSend);Stop()(:661-675)仅 close(p.done) 不等待;run() 中 conn.Close 依赖 g.Wait() 返回(RunWithContext :540 defer conn.Close())。本仓库侧:icmp_ping_checker.go:68-69 sendCh/recvCh 无缓冲,:70-75/:82-87 回调阻塞发送,唯一接收者是 pingLoop 的 select;ctx 取消时 pingLoop 走 :95-98 调 pinger.Stop() 后 return,若此刻 runLoop 正阻塞在回调内向已无接收者的 channel 发送,则永远回不到 select 检查 p.done,runLoop goroutine、errgroup 及 raw socket fd 永久泄漏(NewPinger 默认 Timeout=MaxInt64,timeout ticker 也救不回)。每次节点删除/StopPing 均有竞态窗口,远程源频繁 reload 时可累积。

### 29. [P2·confirmed] calculateStDev 返回的是方差而非标准差（缺少开方），StDevDelay 指标数值错误

- **位置**：`pkg/collecter/ping/ping.go:173` · 维度：正确性 · 单元：COL
- **问题**：calculateStDev 计算 sumSquaredDiff/num 后直接返回，未做 math.Sqrt，函数名、GetStDev 及 proto 字段 StDevDelay、prometheus 指标 ping_st_dev 全部声称是标准差，实际导出的是方差（单位 ms²）。对延迟波动大的链路误差可达数量级（如真实 stdev 20ms 会显示 400），center 侧监控面板与任何基于该值的调度判断都会失真。另外该实现对方差用的样本均值来自 calculateAvg（只统计 >0 样本），口径本身一致，问题仅在缺少开方。
- **证据**：ping.go:159-174 `func calculateStDev(...) ... return sumSquaredDiff / float64(num)`，无 math.Sqrt；消费链 ping.go:91 `StDevDelay: float32(r.GetStDev())` → prometheus_desc/ping_desc.go:118-124 ping_st_dev gauge
- **核实**：ping.go:159-173 calculateStDev 计算 sumSquaredDiff/float64(num) 后直接 return，全函数无 math.Sqrt，返回的是方差而非标准差。消费链核实：ping.go:91 StDevDelay: float32(r.GetStDev()) → proto 字段 st_dev_delay → pkg/http/prometheus_desc/ping_desc.go:46 定义 ping_st_dev gauge、:122 用 GetStDevDelay 填值。指标名与实际语义不符，数值失真随波动幅度平方放大，P2 恰当。

### 30. [P2·confirmed] findMin 无有效样本时返回 math.MaxFloat64，经 float32 转换后 MinDelay=+Inf 传播到 RPC 与 prometheus

- **位置**：`pkg/collecter/ping/ping.go:156` · 维度：正确性 · 单元：COL
- **问题**：findMin 以 math.MaxFloat64 为初值，当窗口内没有 >0 的样本（节点 100% 丢包或刚启动全为 -1/-2）时原样返回；ConvertToProtoPingResult 中 `float32(r.GetMin())` 将 math.MaxFloat64 溢出为 +Inf，proto.PingResult.MinDelay=+Inf 经 GetPingMetric RPC 传到 center，最终 prometheus ping_min 输出 +Inf，报警规则和 min() 类聚合会被污染。同文件 findMax（:139-147）在无样本时返回 -1（invalidResult），calculateAvg/calculateStDev/calculateLoss 也返回 -1，唯独 findMin 不一致。应在 findMin 结尾判断未找到样本时返回 invalidResult。
- **证据**：ping.go:149-157 `min := math.MaxFloat64; ... return min` 无 invalidResult 回退；ping.go:89 `MinDelay: float32(r.GetMin())`（float32(math.MaxFloat64) == +Inf）；对照 findMax :140 初值 -1
- **核实**：ping.go:149-156 findMin 初值 math.MaxFloat64，窗口内无 >0 样本时原样返回，与 findMax(:140 初值 -1)、calculateAvg/StDev/Loss（返回 invalidResult）不一致。ping.go:89 float32(r.GetMin()) 运行时转换溢出为 +Inf。触发场景真实：TCP 探测失败 Update(-2)（tcp_ping_checker.go:73）、ICMP 丢包 Update(-2)（icmp_ping_checker.go:116），100% 丢包节点窗口内全为 -2。传播路径核实：GetPingMetric RPC（pkg/rpc/server/end_node_metric.go:28 调 GetPingResults）→ ping_min gauge（ping_desc.go:40、:104）。P2 恰当。

### 31. [P2·confirmed] pkg/collecter 与 pkg/collecter/ping 两个包均无任何 *_test.go，违反架构约束 9

- **位置**：`pkg/collecter/ping_collector.go:1` · 维度：测试缺口 · 单元：COL
- **问题**：整个模块（node_collector、PingCollector 聚合、PingResult 环形缓冲统计、NodeManager diff 更新、file/remote loader、ICMP/TCP checker reconcile）没有一个测试文件。其中 PingResult 的统计函数（avg/max/min/stdev/loss、-1/-2 哨兵语义）、extractHostFromNodeName 的反解析、NodeManager 的 diff/merge 逻辑都是纯函数或易注入依赖的逻辑，单测成本极低；本次审查发现的 stdev 缺开方、min 返回 MaxFloat64、Stop 置 isRunning=true、ICMP 配置被忽略等多个缺陷均属最基础单测即可拦截的类型。建议至少补齐 ping.go 统计函数、extractHostFromNodeName、nodeManagerImpl.updateNodes、TcpPingChecker.updatePingers（以 NodeManager fake 注入）的测试。
- **证据**：`ls pkg/collecter/*_test.go pkg/collecter/ping/*_test.go` → No such file or directory；对照架构约束 9“每个可测模块应有 *_test.go”
- **核实**：ls 核实 pkg/collecter/ 与 pkg/collecter/ping/ 下均无任何 *_test.go。PROJECT_GUIDE.md:76 架构约束 9 原文为「单元测试必须补全 — 每个可测模块对应 *_test.go」，约束真实存在。且本轮已确认的 stdev 缺开方、findMin 返回 MaxFloat64 等缺陷确属最基础单测即可拦截的类型，佐证成立。

### 32. [P3·confirmed] Cluster.IsValid/RegisteredLocal/RegisteredRemote 对 Get 返回值不判空即调方法，节点不存在时 nil 解引用 panic

- **位置**：`pkg/cluster/cluster.go:123` · 维度：错误处理 · 单元：CLU
- **问题**：IsValid(:124)、RegisteredLocal(:129)、RegisteredRemote(:140) 三个封装直接把 NodeManager.Get(nodeName) 的返回值当作非空 *Node 调用其方法,而 Get 在 key 不存在时返回 nil。IsValid 等方法内部访问 node.GetHeartBeatTime 字段,对 nil receiver 解引用会 panic。当前 rpc/http 调用方多为先 Get 判空后再调 node 自身方法,尚未直接命中这些 cluster 级封装,属潜伏缺陷;一旦有调用方按接口语义传入未知 nodeName 即崩溃。应在封装内判空返回 false。
- **证据**：cluster.go:123-124 `func (cluster *Cluster) IsValid(nodeName string) bool { return cluster.NodeManager.Get(nodeName).IsValid() }`,Get 可返回 nil(node_manager.go:101);IsValid 解引用 node.GetHeartBeatTime(node.go:53)。
- **核实**：核实属实。cluster.go:123-124/128-129/139-140 三个封装直接对 NodeManager.Get(可返回 nil,node_manager.go:101) 的结果调方法。IsValid(node.go:51-56) 为指针接收者且解引用 node.GetHeartBeatTime,对 nil 接收者会 panic。当前调用方多先 Get 判空绕开这些封装,属潜伏缺陷,P3 恰当,行号准确。

### 33. [P3·confirmed] getNodeFilterByCurrentTime 的 currentTime 参数被完全忽略，过滤逻辑与命名/入参语义不符

- **位置**：`pkg/cluster/cluster_manager.go:7` · 维度：正确性 · 单元：CLU
- **问题**：getNodeFilterByCurrentTime(currentTime int64) 返回的闭包内直接调用 n.IsValid()(其内部自取 time.Now()),完全不使用传入的 currentTime。FilterTimeoutNode(cluster.go:134)特意计算 currentTime 再传入,属误导性死参数——调用方以为过滤基于某一固定时刻,实际每个节点各自取当前时间,存在遍历过程中时间漂移不一致的隐患,也说明该抽象未按设计落地。应移除死参数或让闭包真正使用它。
- **证据**：cluster_manager.go:7-11 `func getNodeFilterByCurrentTime(currentTime int64) NodeFilter { return func(n *Node) bool { return n.IsValid() } }` —— currentTime 未被引用;调用方 cluster.go:134-135 传入 time.Now().Unix()。
- **核实**：核实属实。cluster_manager.go:7-11 getNodeFilterByCurrentTime(currentTime int64) 闭包内只调 n.IsValid()(其自取 time.Now),currentTime 完全未被引用;调用方 cluster.go:134-135 特意算 time.Now().Unix() 传入。死参数、命名与语义不符,无功能性 bug,P3(本组最低档)恰当,行号准确。

### 34. [P3·confirmed] 测试缺口：无并发/竞态测试，未覆盖 GetGrpcClientConn、Filter 丢更新、空 token 鉴权路径与 return-vs-continue 循环缺陷

- **位置**：`pkg/cluster/cluster_test.go:1` · 维度：测试缺口 · 单元：CLU
- **问题**：cluster_test.go 仅覆盖单线程的 IsValid/注册状态判定、IsSameCluster、AuthRemoteNode 四分支与 LoadStaticNode 过滤,满足『可测模块有 _test.go』的架构约束,但本层最危险的面全无测试:(1) NodeManager.Get/GetAllNode 无锁读 vs Add/Delete 的数据竞争(应有 -race 并发测试);(2) Filter 两段式丢更新;(3) GetGrpcClientConn 的重复 Dial/泄漏;(4) AuthRemoteNode 对 GetHeartBeatTime==0 空 InToken 节点放行的鉴权边界;(5) registerOrHeartBeatToEndNode 的 return 提前退出。这些正是 F1–F8 缺陷所在,缺测使回归无法被 CI 捕获。建议补 -race 并发用例与鉴权边界用例。
- **证据**：cluster_test.go 全文均为串行断言(如 :169 TestCluster_AuthRemoteNode_Success),无 goroutine/`go test -race` 场景;无针对 GetGrpcClientConn、Filter、GetHeartBeatTime==0 鉴权路径的用例。
- **核实**：核实属实。cluster_test.go 全文为串行断言,无 goroutine/-race 并发用例,未覆盖 NodeManager 无锁读(Get/GetAllNode/HaveNode 均无锁,与 Add/Delete 竞争)、Filter 两段式丢更新、GetGrpcClientConn 重复 Dial/泄漏、以及 AuthRemoteNode 对 GetHeartBeatTime==0 空 InToken 放行这一鉴权边界(现有 _Expired 用例用非零过期时间,恰好绕开了 0 值分支)。属实的测试缺口,P3 恰当。

### 35. [P3·confirmed] 固定 1 秒 RPC 超时用于 ObtainNewCert/UpdateProxy/UpsertClusterUsers 等慢操作，注定超时失败

- **位置**：`pkg/rpc/client/context.go:9` · 维度：错误处理 · 单元：RPC
- **问题**：NewContext() 硬编码 RpcTimeOut=1 秒。ReqToMultiEndNodeServer 在调用方传入 ctx==nil 时用它作为所有扇出请求的 deadline(end_node_rpc.go:497)；心跳循环里 registerToEndNode/heartbeatToEndNode 以及心跳推送 UpsertClusterUsers(end_node_cluster.go:294)也都用 NewContext。ObtainNewCert 走 ACME 签发可达数秒~数十秒、UpdateProxy/FastAddInbound 涉及外部进程重载、集群用户全量推送数据量大时均可能超 1s，这些操作在 1s deadline 下必然 DEADLINE_EXCEEDED。慢操作与心跳/探活共用同一固定超时是不合理的全局约束，应按操作类型区分超时或由调用方传入合适 ctx。
- **证据**：const RpcTimeOut = 1 // 秒; ctx, _ := context.WithTimeout(context.Background(), RpcTimeOut*time.Second); end_node_rpc.go: reqCtx := NewContext(); if ctx != nil { reqCtx = ctx }。
- **核实**：部分成立、部分反驳,降级 P3。NewContext()=1s 属实(context.go:9-14),但全仓库检查了所有 ReqToMultiEndNodeServer 调用点(pkg/http 约 40 处 + prometheus_desc 3 处),全部传入非 nil 的 c.Request.Context()/上层 ctx,nil-ctx 回退 1s 的路径实际无人触发——ObtainNewCert/UpdateProxy/FastAddInbound '注定 DEADLINE_EXCEEDED' 的核心断言不成立(gin request ctx 默认无 deadline)。真实受 1s 约束的只有心跳循环内直连调用:end_node_cluster.go:185(register)/254(heartbeat)/294(UpsertClusterUsers)/352(center heartbeat)。其中 UpsertClusterUsers 全量用户推送在大 payload/跨 WAN 时 1s 偏紧是残余真实问题,register/heartbeat 用 1s 属探活场景可辩护。问题缩水为'集群用户推送与心跳共用 1s 固定超时',P3。

### 36. [P3·confirmed] OnlyGateway 白名单用 strings.Contains 子串匹配，语义脆弱易误放行

- **位置**：`pkg/rpc/server/end_node_server.go:70` · 维度：安全 · 单元：RPC
- **问题**：isOnlyGatewayMethod 用 strings.Contains(onlyGatewayMethods, method) 判断方法是否在网关白名单，其中 onlyGatewayMethods 是用 '|' 拼接的一整个字符串。子串匹配意味着任何是白名单串子串的方法名都会被判为白名单方法——当前固定方法集虽无实际碰撞，但一旦新增方法名恰为某白名单项的子串（如包含 'Node'、'Ping'、'Status' 等片段），就会在 OnlyGateway 节点上被错误放行，绕过网关限制。应改为按 '|' split 后精确相等匹配或用 map 集合。
- **证据**：var onlyGatewayMethods = "HeartBeat|RegisterNode|SetGatewayModel|GetPingMetric|GetNodeMetric|GetBandWidthStats|SetPingCheck|GetStatus"; func isOnlyGatewayMethod(fullMethod string) bool { return strings.Contains(onlyGatewayMethods, fullMethod[methodPrefixLen:]) }。
- **核实**：属实。end_node_server.go:68-72 逐字核实:onlyGatewayMethods 为 '|' 拼接单串,isOnlyGatewayMethod 用 strings.Contains 子串匹配。逐一核对 methodRspMap 中全部现有方法名,当前确无实际碰撞(与 finding 自述一致),但未来新增名为白名单项子串的方法(如 'Ping'、'Status')会在 OnlyGateway 节点被误放行。注意误放行后仍需过 authRemoteNode 鉴权,非直接越权,仅绕过网关模式过滤,P3 恰当。函数定义在 70-71 行,行号准确。

### 37. [P3·confirmed] NewContext 丢弃 WithTimeout 的 cancel 造成计时器泄漏，且固定 1s 超时用于心跳批量推用户可截断同步

- **位置**：`pkg/rpc/client/context.go:12` · 维度：资源 · 单元：RPC
- **问题**：NewContext 使用 ctx, _ := context.WithTimeout(...) 丢弃 cancel（12 行，go vet 明确告警 cancel 必须调用），每次调用泄漏一个计时器至到期；心跳循环每 10s 对每个节点多次调用，长期运行下累积无谓计时器。此外该 1s 固定超时被 heartbeatToEndNode 用于 ReqUpsertClusterUsers 全量用户推送（end_node_cluster.go:294），集群用户较多时单次推送易超 1s 被 DeadlineExceeded 截断，导致 needClusterUsers 反复请求、同步收敛变慢。建议返回并妥善 cancel，且对批量同步用更宽松的超时。
- **证据**：func NewContext() context.Context { ctx, _ := context.WithTimeout(context.Background(), RpcTimeOut*time.Second); return ctx }  // RpcTimeOut=1
- **核实**：client/context.go:11-14 与 evidence 完全一致：context.WithTimeout 的 cancel 被丢弃（go vet lostcancel 类告警成立，计时器泄漏至 1s 到期），RpcTimeOut=1 秒硬编码。end_node_cluster.go:294 确认 ReqUpsertClusterUsers 全量用户推送复用 NewContext() 的 1s 超时，且该调用位于每 10s 一轮（heartbeatInterval，end_node_server.go:57）的 heartbeatToEndNode 内；用户量大时 1s 内推完不现实，DeadlineExceeded 后 needClusterUsers 下轮重复请求、收敛变慢的推理成立。泄漏本身 1s 内自愈、影响有限，P3 恰当。

### 38. [P3·confirmed] isOnlyGatewayMethod 用 strings.Contains 做子串匹配判定网关白名单，脆弱且易误放行

- **位置**：`pkg/rpc/server/end_node_server.go:71` · 维度：架构 · 单元：RPC
- **问题**：onlyGatewayMethods 是竖线连接的字符串，isOnlyGatewayMethod 用 strings.Contains(onlyGatewayMethods, method) 判断。这依赖方法名之间互不为子串：当前实现无误放行，但任何将来新增的方法名若恰为某网关方法名的子串（例如若出现名为 "Status"/"NodeMetric" 的方法）就会在 OnlyGateway 模式被错误豁免过滤，反之亦可能误拦截。网关过滤是安全边界，应改为对分割后的集合做精确相等匹配（map[string]struct{} 或 slice 精确比较）而非子串包含。
- **证据**：var onlyGatewayMethods = "HeartBeat|RegisterNode|SetGatewayModel|GetPingMetric|GetNodeMetric|GetBandWidthStats|SetPingCheck|GetStatus"; return strings.Contains(onlyGatewayMethods, fullMethod[methodPrefixLen:])
- **核实**：end_node_server.go:68-71 与 evidence 逐字一致：onlyGatewayMethods 为竖线拼接字符串，isOnlyGatewayMethod 用 strings.Contains 做子串匹配。逐一比对 methodRspMap 全部 43 个方法名，当前确无任何非网关方法是该串的子串（如 GetSub 不是 GetStatus 的子串），finding 自述'当前实现无误放行'准确、未夸大。但该匹配确实依赖'方法名互不为子串'这一无人守护的隐式不变量，且 OnlyGateway 过滤是安全边界（决定非网关方法是否被短路拒绝），未来新增方法名即可能静默误豁免。前瞻性健壮性问题，P3 恰当。

### 39. [P3·confirmed] ConvertToProtoPingResult 持有读锁后递归获取同一 RWMutex 读锁，存在经典写者插队死锁隐患

- **位置**：`pkg/collecter/ping/ping.go:83` · 维度：并发 · 单元：COL
- **问题**：ConvertToProtoPingResult 在持有 r.mutex.RLock 的情况下（ping.go:81-82）调用 GetMax/GetMin/GetAvg/GetStDev/GetLoss/GetLatestDelay，这些方法内部再次 RLock 同一把锁（loopReadData ping.go:115、GetLatestDelay :72）。Go 的 sync.RWMutex 明确禁止递归读锁：若一个写者（Update 的 Lock，:45）在两次 RLock 之间排队，内层 RLock 将永久阻塞形成死锁。当前之所以未触发，仅因为唯一的写路径（聚合 goroutine 的 existing.Update，ping_collector.go:123）和唯一的读路径（GetPingResults→Convert，:187）恰好被外层 pc.mu 互斥串行化——这是隐式依赖，任何新增的并发调用（如直接对 map 外的 PingResult 并发 Update+Convert，或 checker 侧复用 PingResult）都会引入低概率整锁死锁，且 GetPingResults 持有 pc.mu.RLock 时死锁会级联卡死所有聚合 goroutine。应让 Convert 在单次 RLock 内直接计算，或改用无锁快照。
- **证据**：ping.go:81-93 `r.mutex.RLock(); defer r.mutex.RUnlock(); return &proto.PingResult{ ... MaxDelay: float32(r.GetMax()), ...}`；GetMax→loopReadData ping.go:115 `r.mutex.RLock()` 二次加锁；写者 Update ping.go:45 `r.mutex.Lock()`
- **核实**：代码属实：ConvertToProtoPingResult 在 ping.go:81-82 持 RLock 后，:88-93 调用 GetMax/GetMin/GetAvg/GetStDev/GetLoss/GetLatestDelay，分别经 loopReadData(:115) 或 GetLatestDelay(:72) 对同一 r.mutex 二次 RLock；写者 Update(:45) 用 Lock。sync.RWMutex 文档明确禁止此模式。同时核实了 finding 中的串行化前提：全仓库对共享 PingResult 的 Update 仅在 ping_collector.go:123（持 pc.mu.Lock），Convert 仅在 :187（持 pc.mu.RLock），二者经 pc.mu 互斥，checker 侧 Update 均作用于未共享的新对象，故当前无实际触发路径。属真实存在但当前不可触发的隐性死锁隐患，降为 P3。

### 40. [P3·confirmed] IcmpPingChecker.pingerMap 无锁读写且 Start 的 isRunning 检查非原子，并发 Start 会产生双 reconcile goroutine 数据竞争

- **位置**：`pkg/collecter/ping/icmp_ping_checker.go:166` · 维度：并发 · 单元：COL
- **问题**：IcmpPingChecker 的 pingerMap 在 reconcile goroutine 中直接读取和整体替换（:166-167 `oldPingerMap := ipc.pingerMap; ipc.pingerMap = make(...)`)，没有任何互斥（对照 TcpPingChecker 用 tpc.mu 保护同类 map）。当前靠“Start 只被调用一次”这一约定保持单线程，但 Start 的守卫是 check-then-act：`if ipc.isRunning.Load() { return }`（:155）与末尾 `ipc.isRunning.Store(true)`（:208）之间没有原子性，两个并发 Start 都能通过检查并各自启动 reconcile goroutine，随后两个 goroutine 无锁并发读写/替换 pingerMap（Go map 并发读写直接 fatal panic），并对同一节点重复创建 pinger。叠加前述 Stop 置 isRunning=true 的笔误，一旦有人修复重启路径，这里就是下一个雷。建议与 TCP 侧对齐：加互斥并用 CompareAndSwap 做 Start 守卫。
- **证据**：icmp_ping_checker.go:166-167 无锁 `oldPingerMap := ipc.pingerMap; ipc.pingerMap = make(map[string]*pingerInfo)`；:155 `if ipc.isRunning.Load() { return }` 与 :208 `ipc.isRunning.Store(true)` 非原子；对照 tcp_ping_checker.go:140-142 updatePingers 持 tpc.mu
- **核实**：代码属实：icmp_ping_checker.go:166-167 reconcile goroutine 无锁读取并整体替换 ipc.pingerMap，IcmpPingChecker 结构体无任何互斥字段（对照 tcp_ping_checker.go:87 tpc.mu、:140-142 updatePingers 持锁）；Start 守卫 :155 Load 与 :208 Store 之间非原子，两个并发 Start 均可通过检查并各启动一个 reconcile goroutine，随后无锁并发写同一 map（Go runtime fatal）。当前 Start 仅由 PingCollector.init→startChecker 单线程调用一次，属守卫性缺陷而非现行可达 bug，P3 恰当。附带核实 evidence 提到的 basePingChecker.Stop 置 isRunning=true 笔误（ping.go:222）属实。

### 41. [P3·confirmed] pingLoop 向 resultChan 的发送不受 ctx 保护：停机时若缓冲耗尽，TCP 侧 wg.Wait 挂死、ICMP 侧 goroutine 泄漏

- **位置**：`pkg/collecter/ping/tcp_ping_checker.go:61` · 维度：并发 · 单元：COL
- **问题**：TCP pingLoop 的 `ch <- result`（:61）和 ICMP pingLoop 的两处 `ch <- result`（icmp_ping_checker.go:107、:117）都是裸发送，不在带 ctx.Done 的 select 中。StopPing 后聚合 goroutine（ping_collector.go:110-130）随 ctx 退出，不再消费 resultChan；若此时 1000 容量的缓冲恰好被填满（节点数大或聚合方曾被阻塞），发送方将永久阻塞：ICMP 侧 pingLoop goroutine 泄漏；TCP 侧更严重——stopAllPingers 持 tpc.mu 调 pinger.stop()→wg.Wait()（:40-45、:190-198），pingLoop 卡在发送上导致 wg.Wait 永不返回，reconcile goroutine 连同 tpc.mu 一起永久占用。发送应改为 `select { case ch <- result: case <-ctx.Done(): }`。
- **证据**：tcp_ping_checker.go:59-62 `case <-ticker.C: result := pi.doPing(); ch <- result` 裸发送；:44 `pi.wg.Wait()` 在 stop 中被 stopAllPingers（持 tpc.mu）调用；icmp_ping_checker.go:107/:117 同样裸发送；聚合方 ping_collector.go:113 ctx.Done 即 return 不排空 channel
- **核实**：代码属实：tcp_ping_checker.go:59-61 ticker 分支内 ch <- result 为裸发送（外层 select 的 ctx.Done 分支无法在阻塞发送时生效）；icmp_ping_checker.go:107、:117 同样裸发送。停机链核实：StopPing→checker.Stop 取消 ctx 后，聚合 goroutine（ping_collector.go:113）ctx.Done 即 return 不排空 channel；TCP 侧 reconcile goroutine 在 ctx.Done 分支调 stopAllPingers（:190-198，持 tpc.mu）→ pinger.stop()→wg.Wait()（:40-44），若 1000 缓冲已满则 pingLoop 永久阻塞在发送、wg.Wait 永不返回、tpc.mu 永久占用；ICMP 侧对应 goroutine 泄漏。需缓冲恰好耗尽才触发，概率低，P3 恰当。

### 42. [P3·confirmed] 节点元数据（Geo/ISP/端口）在 reload 后不生效：reconcile 仅按 host 键复用旧 pingerInfo，指标标签长期陈旧并产生重复序列

- **位置**：`pkg/collecter/ping/icmp_ping_checker.go:186` · 维度：正确性 · 单元：COL
- **问题**：ICMP reconcile 对已存在 host 直接搬运旧 pingerInfo（:186-189），不比较 node 的 Geo/ISP 是否变化；TCP updatePingers 对已存在 addr 也保留旧 tcpPingerInfo（tcp_ping_checker.go:177-187），新的 node 指针被丢弃。远程源修正某节点的 Geo/ISP 后，探测继续使用旧元数据生成结果，聚合 key "{geo}_{isp}_{ip}" 与新值不一致；且 cleanupStaleResults 只按 host 判存活，旧 geo_isp 组合的历史序列不会被清理——prometheus 中同一目标出现新旧两套标签序列（旧的还在被继续更新）。修复：reconcile 时比较 nodeInfo 关键字段，变化则重建 pinger，或至少更新 pi.nodeInfo。
- **证据**：icmp_ping_checker.go:186-189 `if pinger, ok := oldPingerMap[node.Host]; ok { ipc.pingerMap[node.Host] = pinger ... }` 未用新 node；tcp_ping_checker.go:178 `if _, exists := tpc.pingerMap[addr]; !exists` 才创建，存量 pinger 保留旧 nodeInfo；清理仅比 host：ping_collector.go:137-151
- **核实**：亲读代码证实：icmp_ping_checker.go:186-189 对已存在 host 直接搬运旧 pingerInfo（`ipc.pingerMap[node.Host] = pinger`），循环变量 node 的新 Geo/ISP 被丢弃，pingLoop 持续以旧 pi.nodeInfo 生成结果标签；tcp_ping_checker.go:177-187 对已存在 addr 只在 `!exists` 时新建，存量 tcpPingerInfo 同样保留旧 nodeInfo。ping_collector.go:135-156 的 cleanupStaleResults 仅以 host 判存活，元数据变化后旧 "{geo}_{isp}_{ip}" 键的结果条目不会被清理且经 GetPingResults 持续对外返回。唯一小瑕疵：稳态下只有旧标签序列被持续更新，新旧双序列并存需节点先被短暂移除再加回（ICMP pinger 重建）才出现，但核心缺陷（元数据更新永不生效+陈旧序列不清理）成立。P3 恰当。

### 43. [P3·confirmed] ICMP/TCP 探测均禁用时仍加载节点源并启动远程 reload 轮询 goroutine，空耗 HTTP 请求

- **位置**：`pkg/collecter/ping_collector.go:61` · 维度：资源 · 单元：COL
- **问题**：NewPingCollector 不管 EnableICMPPing/EnableTCPPing 是否为 false，都会执行 nm.LoadFromSources(cfg.NodeSources)：对 remote 源立即发起 HTTP GET，并在 UpdateInterval>0 时启动永久轮询 goroutine（remote_loader.go:78-110），每周期拉取并 diff 节点、触发 OnChange 回调——而此时没有任何 checker 消费这些节点。cmd/server.go:185 无条件构造 PingCollector，因此禁用 ping 的节点也会持续对外发起周期性 HTTP 请求并维护无用状态。应在两个开关均关闭时跳过 NodeManager 初始化，或延迟到 init() 发现有 checker 启用时再加载。
- **证据**：ping_collector.go:60-71 `nm := ping.NewNodeManager(); nm.LoadFromSources(cfg.NodeSources); nm.OnChange(...)` 在 enable 判断（init 内 :76/:91）之前无条件执行；cmd/server.go:185 无条件 NewPingCollector
- **核实**：证实：ping_collector.go:60-71 在 init()（:76/:91 才检查 enableICMPPing/enableTCPPing）之前无条件执行 nm.LoadFromSources(cfg.NodeSources) 并注册 OnChange；node_manager.go:63-77 对 remote 源立即 loader.Load()（remote_loader.go:35-57 发起 HTTP GET），且 UpdateInterval>0 时 StartReload（remote_loader.go:89-110）启动永久轮询 goroutine 周期拉取；cmd/server.go:185 无条件构造 NewPingCollector。两开关均关闭时确实无任何 checker 消费节点，HTTP 轮询与状态维护为纯空耗。P3 恰当。

### 44. [P3·confirmed] startChecker 丢弃 context cancel 函数（go vet lostcancel），且派生 ctx 毫无作用

- **位置**：`pkg/collecter/ping_collector.go:109` · 维度：错误处理 · 单元：COL
- **问题**：`ctx, _ := context.WithCancel(checker.GetContext())` 丢弃 cancel，是 go vet lostcancel 会报的模式，会导致派生 context 及其在父 ctx 中挂接的取消传播记录直到父 ctx 结束才释放。此处派生本身没有任何意义——聚合 goroutine 的生命周期完全等同于 checker 自身的 ctx，直接传 checker.GetContext() 即可。属于代码卫生问题，但出现在每个 checker 的启动路径上，建议顺手清理以保持 vet 干净。
- **证据**：ping_collector.go:109 `ctx, _ := context.WithCancel(checker.GetContext())`，cancel 被丢弃，ctx 仅原样传入 goroutine
- **核实**：证实：ping_collector.go:109 逐字为 `ctx, _ := context.WithCancel(checker.GetContext())`，cancel 被丢弃（go vet lostcancel 模式），派生 ctx 仅原样传入聚合 goroutine，其生命周期与 checker 自身 ctx 完全一致，派生无任何作用。代码卫生问题，P3 恰当。

### 45. [P3·confirmed] NewNodeCollector 初始化失败直接 panic 且 Register 错误被忽略

- **位置**：`pkg/collecter/node_collector.go:28` · 维度：错误处理 · 单元：COL
- **问题**：NewNodeCollector 在 node_exporter 初始化失败时 panic（:28），DefaultNodeCollector 的 sync.Once 内调用它（:42-44），任何环境性问题（容器内缺 /proc、/sys 挂载差异等）都会让整个 end node 进程在启动时崩溃，而 node 指标只是辅助功能，降级为空指标更合理（GetNodeMetric 已支持 nodeMetricCol==nil 的空返回路径）。同时 :31 `reg.Register(nc)` 的 error 被忽略：注册失败时 Collect 只会静默返回空指标集，无任何日志。建议 NewNodeCollector 返回 (\*NodeCollector, error) 由 cmd/server.go 决定降级，Register 错误至少记日志。
- **证据**：node_collector.go:26-29 `nc, err := prometCollector.NewNodeCollector(slog.Default()); if err != nil { panic(...) }`；:31 `reg.Register(nc)` 返回值未检查；对照 pkg/rpc/server/end_node_metric.go:36-38 nodeMetricCol 为 nil 时可正常返回空
- **核实**：证实：node_collector.go:26-29 在 prometCollector.NewNodeCollector 出错时 panic；:31 reg.Register(nc) 返回的 error 未检查（注册失败则 Gather 静默返回空且无日志）。触发路径真实：cmd/server.go:195 无条件调用 DefaultNodeCollector()，经 :42-44 的 sync.Once 首次执行 NewNodeCollector，环境性初始化失败会使 end node 进程启动即崩溃。对照 pkg/rpc/server/end_node_metric.go:36-38 已有 nodeMetricCol==nil 的空返回降级路径，返回 error 降级方案可行。P3 恰当（严格说 panic 主路径在 :28，行号准确）。

### 46. [P3·confirmed] NodeManager 以 host 单键合并节点：同一 host 的多端口 TCP 探测或多源配置互相覆盖

- **位置**：`pkg/collecter/ping/node_manager.go:93` · 维度：架构 · 单元：COL
- **问题**：nodes map 以 node.Host 为唯一键，LoadFromSources 合并（:92-94）与 updateNodes（:123-125）都执行 `nm.nodes[node.Host] = node`。若用户想对同一主机的多个端口做 TCP 探测（如 host:443 与 host:8443 分别验证不同服务），或不同源（file+remote）对同一 host 提供不同 Usage/Geo/ISP，后写的条目会静默覆盖先写的，只剩一个探测目标，无任何冲突告警。TCP checker 自身按 host:port 管理 pinger（tcp_ping_checker.go:153），说明多端口是预期使用方式，但被 NodeManager 的键设计切断。建议键改为 host:port（或 host+usage 组合），或至少在覆盖时打 warn 日志。
- **证据**：node_manager.go:92-94 `for _, node := range nodes { nm.nodes[node.Host] = node }`；:123-125 updateNodes 同样按 Host 覆盖；对照 tcp_ping_checker.go:152-154 以 `fmt.Sprintf("%s:%d", node.Host, node.Port)` 为键
- **核实**：证实：node_manager.go:92-94（LoadFromSources 合并）与 :123-125（updateNodes）均执行 `nm.nodes[node.Host] = node`，同 host 后写条目静默覆盖先写条目，无 warn 日志；PingNodeInfo 单条仅一个 Port，故同一 host 的多端口 TCP 探测在 NodeManager 层被合并为单一目标。对照 tcp_ping_checker.go:151-154 以 `fmt.Sprintf("%s:%d", node.Host, node.Port)` 为 pinger 键，checker 层确实按 host:port 设计，与 NodeManager 的 host 单键相矛盾。P3 恰当。
