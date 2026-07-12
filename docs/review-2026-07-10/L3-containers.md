# L3 容器层 review — xray / mihomo / hysteria / snell

> Finder → 对抗性 verifier 两级流程产出。下表为**保留**（confirmed + uncertain）的 findings，已剔除 verifier 判定 refuted 的 1 条。uncertain 多为依赖第三方协议/上游行为、无法在仓库内确证的条目，处理前需人工核对。

## 统计

| 维度 | 数量 |
|------|------|
| 保留条目 | 61 |
| — confirmed | 55 |
| — uncertain | 6 |
| — 其它(unverified) | 0 |
| refuted(已剔除) | 1 |

| 优先级 | P0 | P1 | P2 | P3 |
|--------|----|----|----|----|
| 保留条目 | 1 | 5 | 27 | 28 |

## 速查表

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P0 | ✓ | 并发 | HC | `pkg/proxy/containers/hysteria/container.go:74` | certWaitStopCh 无锁读写 + 停止后台协程仍可能启动进程造成孤儿进程 (H-04) |
| 2 | P1 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/exec_runner.go:468` | IsRunning 直接委托 Runner，子进程自行崩溃后仍恒报 running，无自愈 (X-03) |
| 3 | P1 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/exec_runner.go:2090` | FastAddInbound security=none 简单路径生成无凭据 inbound，且会阻断该用户全部订阅 |
| 4 | P1 | ✓ | 错误处理 | HC | `pkg/proxy/containers/hysteria/container.go:77` | run() 立即返回 nil，证书等待/进程启动失败仅记日志，容器却被标记为 Running (H-05) |
| 5 | P1 | ✓ | 并发 | HC | `pkg/proxy/containers/hysteria/container.go:454` | userEventCh 字段无锁读写 + 向已关闭 channel 发送导致进程 panic |
| 6 | P1 | ✓ | 并发 | SC | `pkg/proxy/containers/snell/container.go:360` | forwardUserEvents 与 closeUserEventCh 对 userEventCh 存在数据竞争，停机时可 send on closed channel 触发 panic |
| 7 | P2 | ✓⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/grpc_client.go:388` | QueryStats 把 JSON 当 protobuf wire 直接 conn.Invoke，流量统计恒不可用 (X-07) |
| 8 | P2 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/exec_runner.go:382` | generateDefaultConfig 硬编码 APIPort=62789，与动态分配的 grpcAPIAddress 端口不一致 (X-02) |
| 9 | P2 | ✓ | 资源 | XC | `pkg/proxy/containers/xray/exec_runner.go:164` | 订阅用户事件后从不 Unsubscribe，Stop 也不关闭 userEventCh，两个 goroutine 永久泄漏 |
| 10 | P2 | ✓ | 资源 | XC | `pkg/proxy/containers/xray/exec_runner.go:1990` | FastAddInbound gRPC 成功后 store.Save 失败即返回，inbound 遗留在运行中 xray 且 temp 证书泄漏 |
| 11 | P2 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/exec_runner.go:2338` | Restore 的 pem 分支恒失败：存储的 native JSON 是 certificateFile 路径而非内联 PEM |
| 12 | P2 | ✓ | 并发 | XC | `pkg/proxy/containers/xray/exec_runner.go:553` | RemoveInboundConfig 两阶段锁按 tag 存在性删除，并发同 tag re-add 会误删新 inbound 并泄漏其 temp 文件 |
| 13 | P2 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/exec_runner.go:289` | killProcessOnPort 仅按本地端口全局匹配 /proc/net/tcp，动态端口下可能误杀无关进程 |
| 14 | P2 | ✓⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/inbound_adapter.go:864` | validateRealityShortId 强制恰好 16 位十六进制，拒绝合法的短 shortId |
| 15 | P2 | ✓ | 架构 | XC | `pkg/proxy/containers/xray/exec_runner.go:480` | Restart 直接调 Runner.Restart，绕过 BaseContainer 状态机且不重建 reconcile 循环 (X-04) |
| 16 | P2 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/exec_runner.go:842` | RemoveInboundNative 全程不删 store，而 AddInboundNative 会写 store，持久化状态不一致 (X-05) |
| 17 | P2 | ✓ | 资源 | XC | `pkg/proxy/containers/xray/grpc_client.go:169` | 每个 gRPC 操作新建并 Dial 连接，AddInbound 内还硬编码 500ms sleep (X-08) |
| 18 | P2 | ✓⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/inbound_adapter.go:416` | buildStreamSettings 缺 h2/http 分支，VMess+h2 的 http_path/http_host 被静默丢弃 |
| 19 | P2 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/subscription.go:497` | VLESS/Trojan 分享链接的 path/host/sni 等查询参数未做 URL 编码 |
| 20 | P2 | ✓ | 资源 | XC | `pkg/proxy/containers/xray/exec_runner.go:923` | 用户事件 goroutine 无停止机制，Stop 后仍向已死 xray 下发用户变更且泄漏 |
| 21 | P2 | ✓ | 正确性 | MC | `pkg/proxy/containers/mihomo/container.go:835` | handleUserEvent 的 Add 分支不做 IsUserVisible 过滤，集群分组用户会在非所属节点上短暂获得转发端口/订阅 |
| 22 | P2 | ✓⚠️协议 | 安全 | MC | `pkg/proxy/containers/mihomo/config.go:86` | external_controller 未强制 loopback，配合空 secret 可把 mihomo 无鉴权管理 API 暴露到公网 |
| 23 | P2 | ✓ | 并发 | MC | `pkg/proxy/containers/mihomo/container.go:1009` | reconcileStopCh 读写无锁，与 mu 保护的 userEventHandlerStarted 不一致，Start/Stop 并发下存在 data race |
| 24 | P2 | ✓ | 正确性 | HC | `pkg/proxy/containers/hysteria/container.go:463` | 重启/升级后 userEventCh 不再重建,事件驱动用户处理永久失效,仅剩 30s reconcile |
| 25 | P2 | ✓ | 正确性 | HC | `pkg/proxy/containers/hysteria/container.go:335` | Update 启动失败回滚旧二进制后 hc.cfg.Version 仍为目标版本,Version() 谎报 |
| 26 | P2 | ✓ | 资源 | HC | `pkg/proxy/containers/hysteria/container.go:157` | 构造时 Subscribe 无对应 Unsubscribe,forwardUserEvents 协程随每次实例泄漏 (H-01) |
| 27 | P2 | ✓ | 测试缺口 | HC | `pkg/proxy/containers/hysteria/container_fastadd_test.go:1` | 测试仅覆盖 FastAddInbound,核心并发/生命周期/配置生成/订阅全无测试 |
| 28 | P2 | ✓ | 正确性 | SC | `pkg/proxy/containers/snell/container.go:111` | userEventCh 停机置 nil 后永不重建，Update/重启后事件驱动的用户同步永久失效，只剩 30s reconcile |
| 29 | P2 | ✓ | 资源 | SC | `pkg/proxy/containers/snell/container.go:112` | Subscribe 后从不 Unsubscribe，forwardUserEvents goroutine 与订阅者槽位泄漏 (H-01) |
| 30 | P2 | ✓ | 安全 | SC | `pkg/proxy/containers/snell/downloader.go:92` | snell-server 二进制下载无完整性校验、无大小上限，可被中间人替换或 zip-bomb 撑爆磁盘后以 0755 落盘执行 |
| 31 | P2 | ✓ | 测试缺口 | SC | `pkg/proxy/containers/snell/container.go:1` | snell 容器包零测试覆盖：Update 回滚状态机、reconcile、INI 生成、PSK 生成、URI、并发锁分段全部无 *_test.go |
| 32 | P2 | ?⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/profilegen/shadowsocks.go:199` | validate2022Password 用 base64.StdEncoding 解码,拒绝合法的无 padding 2022 密钥 (SS-02) |
| 33 | P2 | ?⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/inbound_adapter.go:551` | Reality backward-compat 分支写入 xray 不识别的单数 key "shortId",Reality 握手 shortId 缺失 (A-02) |
| 34 | P3 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/exec_runner.go:946` | handleUserEvent 的 Add 分支不做可见性校验，与 Update 分支不一致，会为不可见用户建 forward 规则 |
| 35 | P3 | ✓ | 错误处理 | XC | `pkg/proxy/containers/xray/updater.go:186` | Update 重启失败时 swapper.Rollback 返回值被丢弃，回滚失败无感知 (X-12) |
| 36 | P3 | ✓ | 架构 | XC | `pkg/proxy/containers/xray/inbound_adapter.go:741` | RealityKeyStore 一整套持久化实现是死代码，生产从不使用 |
| 37 | P3 | ✓⚠️协议 | 错误处理 | XC | `pkg/proxy/containers/xray/subscription.go:20` | 错误分类依赖上游/自身错误字符串前缀匹配，脆弱且易回归 |
| 38 | P3 | ✓⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/profilegen/shadowsocks.go:88` | Shadowsocks 参数校验把 reality 列为合法 security,SS+reality 非法组合未被拦截 (SS-01) |
| 39 | P3 | ✓⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/subscription.go:560` | Reality 订阅 SNI 用 math/rand 随机挑选,订阅内容非确定,重复拉取每次不同 |
| 40 | P3 | ✓⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/subscription.go:464` | SOCKS5 分享链接用 base64.StdEncoding 编码 userinfo,含 +/ / /= 时破坏 URL authority |
| 41 | P3 | ✓⚠️协议 | 错误处理 | XC | `pkg/proxy/containers/xray/exec_runner.go:830` | AddInboundNative 从私钥派生 Reality 公钥失败时静默丢弃,订阅 URI 缺失 pbk 且无日志 |
| 42 | P3 | ✓ | 正确性 | XC | `pkg/proxy/containers/xray/profilegen/trojan_tls.go:87` | generateRandomPassword 注释称 hex 实为 base64,输出含 +/ /= 与注释不符 (T-01) |
| 43 | P3 | ✓ | 资源 | MC | `pkg/proxy/containers/mihomo/container.go:815` | user-event handler goroutine 永不退出：Close() 从不关闭 userEventCh，与"clean shutdown"注释矛盾 |
| 44 | P3 | ✓ | 并发 | MC | `pkg/proxy/containers/mihomo/container.go:1006` | reconcileStopCh 无锁访问,与同一 run hook 中受 mu 保护的 userEventHandlerStarted 不一致,Start/Stop 重叠时数据竞争 |
| 45 | P3 | ✓ | 并发 | MC | `pkg/proxy/containers/mihomo/container.go:955` | FastAddInbound 与同 tag RemoveInboundConfig 竞态可留下永不回收的孤儿转发规则,与注释声称的清理保证矛盾 |
| 46 | P3 | ✓ | 资源 | MC | `pkg/proxy/containers/mihomo/downloader.go:104` | gunzip 解压对 io.Copy 无大小上限:更新/自动下载路径存在 gzip 解压炸弹风险(stable 无 checksum 校验) |
| 47 | P3 | ✓ | 测试缺口 | MC | `pkg/proxy/containers/mihomo/container_test.go:985` | 测试缺口：无用例验证 Close() 能终止 user-event handler goroutine（本可捕获泄漏） |
| 48 | P3 | ✓ | 安全 | HC | `pkg/proxy/containers/hysteria/process.go:147` | hysteria.yaml 以 0644 落盘,内含 trafficStats secret 被本机任意用户可读 |
| 49 | P3 | ✓ | 正确性 | HC | `pkg/proxy/containers/hysteria/process.go:130` | httpPort 未校验,未配置 HTTPPort 时鉴权回调 URL 端口为 0,全部用户鉴权失败 (H-06) |
| 50 | P3 | ✓⚠️协议 | 架构 | HC | `pkg/proxy/containers/hysteria/downloader.go:15` | 二进制下载硬编码 linux-amd64,arm64 节点下载错误架构二进制无法运行 |
| 51 | P3 | ✓ | 错误处理 | HC | `pkg/proxy/containers/hysteria/container.go:620` | restoreInboundConfig 反序列化失败静默吞掉,port 依赖 float64 断言易随存储格式变化失效 |
| 52 | P3 | ✓ | 错误处理 | SC | `pkg/proxy/containers/snell/container.go:557` | GetUserSubscriptions 未做 sc.userMgr nil 检查，userMgr 缺失时订阅请求 panic (SN-03) |
| 53 | P3 | ✓ | 安全 | SC | `pkg/proxy/containers/snell/container.go:257` | snell.conf 以 0644 写入且含明文 PSK，本机任意用户可读取共享 PSK |
| 54 | P3 | ✓ | 正确性 | SC | `pkg/proxy/containers/snell/container.go:388` | handleUserEvent 的 Add 分支不校验 IsUserVisible，为非本节点可见用户临时分配转发端口 |
| 55 | P3 | ✓ | 正确性 | SC | `pkg/proxy/containers/snell/container.go:225` | Update 在 Start 前即改写 sc.cfg.Version，回滚路径不还原，且版本变更不持久化 |
| 56 | P3 | ✓ | 错误处理 | SC | `pkg/proxy/containers/snell/container.go:338` | FastAddInbound 对非默认 tag 返回裸 fmt.Errorf，未走统一错误码体系 (H-03) |
| 57 | P3 | ✓ | 正确性 | SC | `pkg/proxy/containers/snell/container.go:523` | restoreInboundConfig 无条件用 store 记录覆盖 Decode 生成的 port (SN-02) |
| 58 | P3 | ?⚠️协议 | 正确性 | XC | `pkg/proxy/containers/xray/subscription.go:382` | VMess 分享链接用 base64.RawURLEncoding,与主流客户端使用的标准 base64 存在互通风险 |
| 59 | P3 | ?⚠️协议 | 正确性 | MC | `pkg/proxy/containers/mihomo/subscription.go:394` | vmess+reality 订阅 URI 丢失 reality 参数，仅 Clash 转换路径可用，裸 vmess:// 客户端拿到的是不可用配置 |
| 60 | P3 | ?⚠️协议 | 正确性 | HC | `pkg/proxy/containers/hysteria/container.go:272` | Reload 静默 no-op,证书轮换/配置变更后无法热加载 (H-07) |
| 61 | P3 | ?⚠️协议 | 正确性 | SC | `pkg/proxy/containers/snell/container.go:575` | 订阅 URI 硬编码 version=5，与 cfg.Version 脱钩，配置为 snell v4 时订阅参数错误 |

## 详细条目

### 1. [P0·confirmed] certWaitStopCh 无锁读写 + 停止后台协程仍可能启动进程造成孤儿进程

- **位置**：`pkg/proxy/containers/hysteria/container.go:74` · 维度：并发 · 单元：HC · 旧 review: H-04
- **问题**：run()（GetRunFunc 返回的启动闭包，在 BaseContainer.Start 解锁后执行）在 container.go:74 无锁写入 hc.certWaitStopCh，随后 go waitForCertAndStart()；该后台协程在 container.go:225 的 select 循环里持续无锁读 hc.certWaitStopCh，而 stop 路径的 closeCertWaitStopCh(container.go:241-249) 也无锁读该字段并 close。Start/Stop 并发（或 Update/Restart 触发的第二次 Start 在 container.go:74 重新 make 覆盖该字段）与后台协程的读构成确定性 data race。更严重的是：stop 只 close certWaitStopCh，但若 waitForCertAndStart 的 goroutine 已越过 select 进入 ticker 分支且 hasCert()==true，会在 stopProcess 已经返回之后调用 startProcess()（container.go:230），把 hc.runner 指向一个新启动的 hysteria 进程且无人再 Stop——进程被孤儿化；hc.runner 字段本身也在 startProcess(process.go:91) 与 stopProcess(process.go:104-112) 间无锁读写。
- **证据**：container.go:74 `h.c.certWaitStopCh = make(chan struct{})`(无锁);:225 `case <-hc.certWaitStopCh`;:230 startProcess 可在 stop 之后执行;closeCertWaitStopCh:242 无锁读同一字段。process.go:91 `hc.runner = runner` 与 :104 `if hc.runner == nil` 无锁。
- **核实**：核实属实。:74 在 run() 闭包(BaseContainer.Start 已解锁后执行)无锁写 certWaitStopCh;:225 后台 goroutine 无锁读、:242 closeCertWaitStopCh 无锁读并 close。单次 Start 内因 goroutine 创建有 happens-before,但 Update/Restart 的第二次 Start 会在 :74 重新 make,而前一个 waitForCertAndStart goroutine 无任何 wait 保证其已退出(不同于 reconcileWg),两者对同一字段的读/写构成确定性 data race。孤儿进程 TOCTOU 也成立:goroutine 越过 select 进入 ticker 分支且 hasCert()==true 时,会在 stopProcess(:104 runner==nil 直接 return)之后调用 startProcess(:230)启动新进程且无人 Stop;hc.runner 在 process.go:91/104-112 亦无锁。P0 在并发生命周期操作下可辩护,保留。

### 2. [P1·confirmed] IsRunning 直接委托 Runner，子进程自行崩溃后仍恒报 running，无自愈

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:468` · 维度：正确性 · 单元：XC · 旧 review: X-03
- **问题**：Executor.IsRunning() 直接 return e.Runner.IsRunning()（exec_runner.go:468-470），而底层 process.Runner.IsRunning 仅判断 p.cmd!=nil && p.cmd.Process!=nil（下层已确认 P-01/X-03），Start 后无人 Wait，xray 子进程崩溃退出后 cmd 不会被置 nil。Updater.Update 用 processCtrl.IsRunning() 决定是否 Stop/Start（updater.go:183），崩溃后 IsRunning 仍返回 true，导致对已死进程执行 Stop（无害）但也会让上层健康检查误判容器存活。该缺陷根因在下层 runner，但 xray 容器无任何补偿（无 watchdog/Wait），故在本层可见且影响 Update 重启决策。
- **证据**：exec_runner.go:468-470 func IsRunning(){return e.Runner.IsRunning()}；updater.go:183 if u.processCtrl.IsRunning(){...Stop()}。
- **核实**：属实：exec_runner.go:468-470 IsRunning 直接 return e.Runner.IsRunning()；底层 tools/process/runner.go:147-150 IsRunning 仅 return p.cmd!=nil && p.cmd.Process!=nil，Start(:57-97) 只在 :97 赋值 p.cmd、无 Wait 也无 reaper goroutine（:121 的 go func 在 Stop 内，非 Start），故子进程自行崩溃后 p.cmd 不被置 nil，IsRunning 恒真。xray 容器层无 watchdog/Wait 补偿。updater.go:183 确用 processCtrl.IsRunning() 决定 Stop/Start（崩溃后 Stop 无害但 Update 仍能重启），真正后果是上层健康检查误判容器存活、崩溃的 xray 不被自愈拉起。行号 468 及触发链准确，P1 保留。

### 3. [P1·confirmed] FastAddInbound security=none 简单路径生成无凭据 inbound，且会阻断该用户全部订阅

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:2090` · 维度：正确性 · 单元：XC
- **问题**：buildFastAddSimpleSpec 对 VMess/VLESS(security=none)把 UUID 写进 extensions["uuid"]、对 Trojan 写进 extensions["password"]（exec_runner.go:2098-2116），但从不设置 extensions["users"]。而 Adapter.ToProvider 只有在 spec.Extensions["users"] 存在时才向 settings 注入 clients（inbound_adapter.go:151-179），并且不消费 "uuid"/"password" 这两个键。结果：VMess/VLESS/Trojan 的 security=none FastAdd inbound 生成的 native JSON 中 clients 为空数组，随后 FastAddInbound 从 native JSON 提取 defaultClientUUID/defaultPassword（exec_runner.go:1886-1914）得到空串。订阅生成时 getCredentialForUser 因 defaultClientUUID/defaultPassword 为空返回 "default client uuid/password not found"（exec_runner.go:1497-1508）。该错误不匹配 isUserNotFoundError 的 "user ... not found in inbound" 前缀，于是 GetUserSubscriptions 走 fail-fast 分支直接 return nil,err（subscription.go:62-71），导致该用户在所有 inbound 上的订阅整体失败——一个坏 inbound 污染整个 /sub 结果。测试仅断言 protocol/port/listen（fast_add_inbound_test.go:131-207），未覆盖凭据，故漏检。
- **证据**：exec_runner.go:2091-2117 只写 extensions["uuid"/"password"/"method"]，无 extensions["users"]；inbound_adapter.go:151 `if usersIface, ok := spec.Extensions["users"]` 是注入 clients 的唯一入口；exec_runner.go:1497 `if uuid == "" { return "", fmt.Errorf("default client uuid not found ...") }`。
- **核实**：核实属实,链路完整。buildFastAddSimpleSpec(exec_runner.go:2090)只写 extensions[uuid/password/method],不写 extensions["users"];Adapter.ToProvider 唯一注入 clients 的入口是 spec.Extensions["users"](inbound_adapter.go:151),buildSettings(:319-331)对 VMess/VLESS/Trojan 返回空 clients 数组。故 native JSON 的 clients 为空,defaultClientUUID/defaultPassword 提取(:1893)得空串。getCredentialForUser(:1497、1506)返回 "default client uuid/password not found for inbound"。isUserNotFoundError(subscription.go:20-27)注释明确写着要把此凭据错误与 "user not found" 区分开,故不匹配→GetUserSubscriptions(:62-71)fail-fast 返回 nil,err,污染该用户所有 inbound 的 /sub。P1 恰当。

### 4. [P1·confirmed] run() 立即返回 nil，证书等待/进程启动失败仅记日志，容器却被标记为 Running

- **位置**：`pkg/proxy/containers/hysteria/container.go:77` · 维度：错误处理 · 单元：HC · 旧 review: H-05
- **问题**：run()(container.go:60-80) 在 `go waitForCertAndStart()` 后立即 return nil，BaseContainer.Start 随即 MarkRunning(base.go:194) 把状态置为 Running。但真正的进程启动发生在后台 waitForCertAndStart 里，其所有失败路径只 slog.Error 后 return（container.go:173/198/231），startProcess 里的下载失败、证书不可用、runner.Start 失败都不会反馈给 Start 调用方。结果：证书永远签发不出来 / 二进制下载失败 / 端口占用时，hysteria 进程从未起来，但集群管理面认为该容器 Running 且健康，订阅与鉴权全线不可用却无任何上层告警。此外由于 run 快速返回，若 Stop 恰在 Starting 窗口调用，BaseContainer.Stop 在 Starting 分支(base.go:239-248)根本不执行 stopFunc，reconcile/certwait/进程全部泄漏。
- **证据**：container.go:77 `go h.c.waitForCertAndStart()` 后 :79 `return nil`;:173/:198/:231 `slog.Error("hysteria: start process failed", ...)` 仅日志;base.go:194 Start 成功即 MarkRunning。
- **核实**：核实属实。:77 go waitForCertAndStart 后 :79 立即 return nil,base.go:182→194 runFunc 成功即 MarkRunning。真实进程启动在后台,下载失败/证书不可用/runner.Start 失败(:173/:198/:231 及 startProcess 返回错误)仅 slog,不反馈给 Start,容器却报 Running。Starting 窗口调用 Stop 命中 base.go:239-248 分支,确实不执行 stopFunc,run() 内 :70-71 已启动的 reconcileLoop/userEventHandler 与 certWait goroutine 全部泄漏。P1 合理。

### 5. [P1·confirmed] userEventCh 字段无锁读写 + 向已关闭 channel 发送导致进程 panic

- **位置**：`pkg/proxy/containers/hysteria/container.go:454` · 维度：并发 · 单元：HC
- **问题**：closeUserEventCh(container.go:838-843) 在 stop 时 `close(hc.userEventCh); hc.userEventCh = nil`,全程无锁;而 forwardUserEvents(container.go:450-458) 在另一 goroutine 里 `if hc.userEventCh != nil { select { case hc.userEventCh <- event: } }` 也无锁读该字段并向其发送。二者对 hc.userEventCh 的读/写构成确定性 data race。更致命的是 nil 检查与 select 发送之间存在窗口:检查通过后 closeUserEventCh 恰好 close,则 select 分支向已关闭 channel 发送触发 panic,直接 crash 整个 v2raymg 守护进程。由于 userMgr 订阅源永不关闭(见 H-01),forwardUserEvents 在 Stop 后仍活着并持续尝试发送,只要 Stop 瞬间有用户事件到达即可命中。
- **证据**：container.go:452-456 `if hc.userEventCh != nil { select { case hc.userEventCh <- event: default: } }`;:838-842 `close(hc.userEventCh); hc.userEventCh = nil`,均无 hc.mu 保护。
- **核实**：核实属实。closeUserEventCh(:839-842)无锁 close(hc.userEventCh) 再置 nil;forwardUserEvents(:452-456)无锁 nil 检查后 select 发送。二者对字段构成 data race;且 close() 与 =nil 是两条语句,forwardUserEvents 在 :454 重新读取字段若落在 close 之后、nil 之前,则向已关闭 channel 发送触发 panic 崩溃守护进程。由 idx5 证实订阅源永不关闭,forwardUserEvents 在 Stop 后仍存活,窗口可命中。P1 合理。

### 6. [P1·confirmed] forwardUserEvents 与 closeUserEventCh 对 userEventCh 存在数据竞争，停机时可 send on closed channel 触发 panic

- **位置**：`pkg/proxy/containers/snell/container.go:360` · 维度：并发 · 单元：SC
- **问题**：forwardUserEvents(container.go:356-365) 在无锁情况下先读 `sc.userEventCh != nil`(358)，再在 select 中对 `sc.userEventCh <- event`(360) 二次读取同一字段；stop hook 调 closeUserEventCh(698-703) 同样无锁 `close(sc.userEventCh)` 并置 nil。因为始终没有 Unsubscribe(见另一条 H-01 finding)，源 channel 永不关闭，forwardUserEvents 在容器 Stop 之后仍在运行并持续接收上游事件。停机瞬间若有事件到达：goroutine 在 nil 检查处读到非 nil 的 channel P，随后 closeUserEventCh 关闭 P 并置 nil，goroutine 在 select 处若仍观察到已关闭的 P，则 `sc.userEventCh <- event` 向已关闭 channel 发送直接 panic 崩溃整个进程。这是无同步的字段读写竞争，go race 会告警。修复：用 sc.mu 或原子指针保护 userEventCh 读写，或改为通过关闭一个独立的 quit channel 让 forwardUserEvents 退出而非置 nil。
- **证据**：container.go:358 `if sc.userEventCh != nil {` 与 360 `case sc.userEventCh <- event:` 无锁两次读；container.go:700-701 `close(sc.userEventCh); sc.userEventCh = nil` 无锁写；无 Unsubscribe 导致 source 不关闭、forwardUserEvents 在 stop 后继续运行
- **核实**：核实属实。forwardUserEvents(356-364)对 sc.userEventCh 在 358 行 nil 检查、360 行 select 发送均为无锁读；closeUserEventCh(698-703)无锁 close+置 nil 写。因 source(=userMgr.Subscribe())从不 Unsubscribe、永不关闭，`for event := range source` 永不退出，forwardUserEvents 在 Stop 后持续运行。字段并发读写本身即 go race 可检出的数据竞争;停机瞬间上游有事件到达时,select 处若读到已 close 但尚未置 nil 的旧 channel,向已关闭 channel 发送即 panic 崩溃进程。触发路径真实(Update 在 196 调 Stop,同时 UserManager 可随时 emitEvent)。P1 合理。

### 7. [P2·confirmed] QueryStats 把 JSON 当 protobuf wire 直接 conn.Invoke，流量统计恒不可用

- **位置**：`pkg/proxy/containers/xray/grpc_client.go:388` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游） · 旧 review: X-07
- **问题**：QueryStats 用 json.Marshal(QueryStatsRequest{...}) 得到的 []byte 直接作为请求体传给 conn.Invoke(ctx, "/xray.app.stats.command.HandlerService/QueryStats", reqData, &response)（grpc_client.go:385-388），并把响应按 JSON 反序列化（:401-403）。gRPC 默认 codec 期望 proto.Message，[]byte 不是 proto.Message，Invoke 会直接编解码失败或把 JSON 字节当作 protobuf 帧发送，xray 端无法识别，因此 container 级流量查询实际恒定失败。虽然架构约束 #5 规定流量只信 forward relay 计数、QueryStats 非主链路，但该函数对外仍是 RuntimeAPI 契约方法，任何调用者拿到的都是错误或空数据。修复需生成/使用 StatsService 的 protobuf stub。
- **证据**：grpc_client.go:381-403：reqData,_ := json.Marshal(QueryStatsRequest{...}); conn.Invoke(ctx, "/xray.app.stats.command.HandlerService/QueryStats", reqData, &response)；随后 json.Unmarshal(response,&resp)。
- **核实**：代码属实：grpc_client.go:385-388 用 json.Marshal 得到 []byte 直接传给 conn.Invoke，而 grpc.Dial(:373) 只带 insecure creds、未设自定义 codec，默认 proto codec 的 Marshal 会对 []byte 做 proto.Message 类型断言失败（返回 'want proto.Message' 错误），响应侧对 *[]byte 的 Unmarshal 同样失败，故 QueryStats 恒返回 error。该失败点在 grpc-go 客户端 codec，属库层确定性行为、非 xray 字段解析，protocolRelated 约束不构成阻碍（无需断言 xray stats service 行为即可判定失败）；行 371 注释也自认是无 proto stub 的 JSON fallback。但 P0 过高：架构约束#5 明确流量只信 forward relay，且 grep 全仓 pkg/cmd/internal 中 QueryStats 无任何 xray 包外调用者，实际影响为潜在的失效契约方法，下调 P2。行号 388 准确。

### 8. [P2·confirmed] generateDefaultConfig 硬编码 APIPort=62789，与动态分配的 grpcAPIAddress 端口不一致

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:382` · 维度：正确性 · 单元：XC · 旧 review: X-02
- **问题**：NewExecutor 在 GRPCAPIAddress 为空时用 net.Listen("127.0.0.1:0") 动态分配一个端口写入 e.grpcAPIAddress（exec_runner.go:128-136），所有 gRPC 操作都连 e.grpcAPIAddress。但 generateDefaultConfig 生成默认 config 时把 API dokodemo inbound 的监听端口写死为 62789（exec_runner.go:382 APIPort:62789），且未调用同文件已存在的 parsePortFromAddr(e.grpcAPIAddress)。当走动态端口路径（如系统测试或未显式配置 grpc_port 时），xray 的 API inbound 实际监听 62789，而客户端连的是动态端口，AddInbound/ListInbounds/RemoveInbound 全部连不上。register.go 默认把 GRPCAPIAddress 设成 127.0.0.1:62789 掩盖了该问题，但只要 GRPCAPIAddress 传空即触发。
- **证据**：exec_runner.go:382 APIPort:62789（注释“与 GRPCAPIAddress 默认值保持一致”）；:129 ln,_:=net.Listen("tcp","127.0.0.1:0") 动态分配；generateDefaultConfig 未使用 parsePortFromAddr(exec_runner.go:361)。
- **核实**：代码不一致属实：NewExecutor 在 GRPCAPIAddress 为空时 net.Listen 127.0.0.1:0 动态分配端口写入 e.grpcAPIAddress(:128-137)，而 generateDefaultConfig 把 API inbound 端口写死 62789(:382) 且不调用同文件的 parsePortFromAddr(:361)，动态端口路径下客户端连动态端口、xray 却监听 62789，必然连不上。但触发面比 finding 描述窄：register.go Decode(:40) 恒把 GRPCAPIAddress 默认设为 127.0.0.1:62789、ConfigFilePath 默认 /tmp/xray-config.json，生产注册路径 GRPCAPIAddress 永不为空、且默认端口 62789 与硬编码值一致；finding 所称'未显式配置 grpc_port 时'触发不成立（grpc_port 未配也走 62789 默认，两端仍一致）。真正触发需直接 NewExecutor 传空 GRPCAPIAddress 且走 EnsureConfig 生成默认 config（测试/内嵌场景），即动态端口特性被 generateDefaultConfig 静默打穿。属真实潜在 bug 但非生产主路径，P1 下调 P2。行号 382 准确。

### 9. [P2·confirmed] 订阅用户事件后从不 Unsubscribe，Stop 也不关闭 userEventCh，两个 goroutine 永久泄漏

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:164` · 维度：资源 · 单元：XC
- **问题**：NewExecutor 里 go e.forwardUserEvents(cfg.UserManager.Subscribe())（exec_runner.go:164）订阅 UserManager，startUserEventHandler 起第二个 goroutine range e.userEventCh（:928-932）。但全包内无任何 Unsubscribe 调用（grep 确认 xray 目录 0 命中，mihomo 容器才有 Unsubscribe），Executor.Stop() 只 stopReconcileLoop()+Runner.Stop()（:473-477），既不关闭 UserManager 侧订阅 channel 也不 close(e.userEventCh)。因此容器 Stop 后 forwardUserEvents 仍 range 在 UserManager 的订阅 channel 上、userEventHandler 仍 range 在 userEventCh 上，两个 goroutine 永不退出；更糟的是停止后的容器仍会继续消费事件并对 e.inbounds 调 AddUser/GetBindPort，向一个已停的容器创建 forward 规则。center 多次创建/重启 xray 容器会持续累积泄漏。
- **证据**：exec_runner.go:164 go e.forwardUserEvents(cfg.UserManager.Subscribe())；:928 go func(){for event:=range e.userEventCh{...}}；:473-477 Stop() 仅 stopReconcileLoop()+Runner.Stop()，无 Unsubscribe/close。
- **核实**：属实：NewExecutor:164 go forwardUserEvents(cfg.UserManager.Subscribe()) 订阅，:434 startUserEventHandler 起第二个 range e.userEventCh 的 goroutine(:928-932)。grep 确认 xray 目录 0 处 Unsubscribe；Stop():473-477 仅 stopReconcileLoop()+Runner.Stop()，既不 Unsubscribe/关闭 UserManager 侧订阅 channel、也不 close(e.userEventCh)。故两个 goroutine（forwardUserEvents range source、handler range userEventCh）Stop 后永不退出，且停止后仍会消费事件对已停容器执行 syncUserToInbound。反复创建/重启 xray 容器持续累积泄漏。行号 164 准确，P2 保留。

### 10. [P2·confirmed] FastAddInbound gRPC 成功后 store.Save 失败即返回，inbound 遗留在运行中 xray 且 temp 证书泄漏

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:1990` · 维度：资源 · 单元：XC
- **问题**：FastAddInbound 先在 :1960 通过 gRPC api.AddInbound 把 inbound 真正加进运行中的 xray；随后加锁，若 storeMgr.InboundStore().Save 失败（DB 错误等）直接 e.inboundsMu.Unlock()+return（exec_runner.go:1990-1992），此时：(1) inbound 已在 xray 运行时生效但没写进 e.inbounds、没进 store —— 变成孤儿 inbound（占用端口、无法通过 RemoveInboundConfig 清理，因为 map 里没有）；(2) 之前 certShouldCleanup 创建的 temp cert/key 文件不会被删除（清理逻辑只在 gRPC 失败分支 :1962-1966 执行）。同样地，:1977 的“map 中已存在则 return ErrInboundAlreadyExists”分支也在 gRPC 已成功之后返回，同样遗留孤儿 inbound + temp 文件泄漏。正确做法是 store/map 失败时回滚 gRPC AddInbound 并清理 temp 文件。
- **证据**：exec_runner.go:1960 api.AddInbound(nativeInbound.JSON) 已生效；:1977-1980 exists 分支直接 return 未清理；:1990-1993 Save 失败 return，未回滚 gRPC、未删 tempCertFiles。
- **核实**：核心属实：FastAddInbound 在 :1960 api.AddInbound 已把 inbound 加进运行中的 xray；随后 :1990 storeMgr.Save 失败即 Unlock+return(:1990-1993)，未回滚 gRPC AddInbound、未删 xrayInbound.tempCertFiles，temp cert 清理只在 gRPC 失败分支(:1962-1966) 执行 → 孤儿 inbound（占端口、不在 e.inbounds map 故 RemoveInboundConfig 清不掉）+ temp 证书泄漏，此路径确定可达。:1977 exists 分支同样在 gRPC 成功后 return 未清理，但可达性较低（若 tag 已在 map 说明先前已加进 xray，:1960 AddInbound 会先命中 'existing tag found' 于 :1968 返回，正常到不了 :1977；仅并发窗口理论存在）。Save 失败路径足以坐实 finding。行号 1990 准确，P2 保留。

### 11. [P2·confirmed] Restore 的 pem 分支恒失败：存储的 native JSON 是 certificateFile 路径而非内联 PEM

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:2338` · 维度：正确性 · 单元：XC
- **问题**：FastAddInbound 的 pem 证书路径（resolveFastAddCert case cert&&key）把 PEM 写入 temp 文件并返回文件路径（exec_runner.go:2024-2027），adapter.buildStreamSettings 因此在 native JSON 里写的是 certificates[0].certificateFile/keyFile（inbound_adapter.go:503-505），而 cert_source 被记为 "pem"。重启后 Restore 对 cert_source=="pem" 走 extractPEMFromNativeJSON（exec_runner.go:2339），该函数只读 certificates[0].certificate/key 这两个字符串数组（:2528-2536），native JSON 里根本没有这两个字段（只有 certificateFile），于是 extractPEMFromNativeJSON 恒返回 error → Restore log.Error 后 continue，pem 类 inbound 永远无法恢复。即使 temp 文件在 /tmp 仍存在（本可按 file 语义 as-is 恢复），也因为强行走 pem 分支而失败。
- **证据**：inbound_adapter.go:503-505 certEntry["certificateFile"]=certFile；exec_runner.go:2503-2536 extractPEMFromNativeJSON 只找 certificates[0].certificate/key 数组；:2339-2343 解析失败即 continue。
- **核实**：属实且为纯仓内往返一致性问题：FastAddInbound pem 路径 resolveFastAddCert(:2024-2027) 把 PEM 写 temp 文件、返回文件路径并记 certSource='pem'；该路径经 profilegen（vless_profiles.go:394-395、vmess_tls.go:175-176 设 extensions cert_file/key_file）→ inbound_adapter.go:503-505 在 native JSON 写 certificates[0].certificateFile/keyFile，而非内联 certificate/key。Restore 对 cert_source=='pem'(:2338) 走 extractPEMFromNativeJSON(:2490-2536)，该函数仅读 certificates[0].certificate/key 的字符串数组，native JSON 里根本无此字段 → joinLines 恒返回 error → Restore log.Error 后 continue(:2340-2342)，pem 类 inbound 永远无法恢复。（且 adapter 内联分支写的是字符串而非数组，即便走内联也对不上，缺陷更稳固。）行号 2338 准确，P2 保留。

### 12. [P2·confirmed] RemoveInboundConfig 两阶段锁按 tag 存在性删除，并发同 tag re-add 会误删新 inbound 并泄漏其 temp 文件

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:553` · 维度：并发 · 单元：XC
- **问题**：RemoveInboundConfig 阶段1在锁内取到 xrayIn 引用后解锁（exec_runner.go:528-530），阶段2-4 在锁外做端口释放/删 store/gRPC，阶段5重新加锁只判断 `if _,exists:=e.inbounds[tag];!exists{return}`（:556），存在则清理阶段1捕获的旧 xrayIn.tempCertFiles 并 delete(e.inbounds,tag)（:561-567）。若在阶段2-5窗口内有并发 FastAddInbound 用同一 tag 重新注册了一个全新的 XrayInbound，阶段5会：(1) 按 tag 把新 inbound 从 map 删除（它只比对 tag，不比对是否同一对象），(2) 删除的是旧对象的 tempCertFiles、泄漏新对象的 temp 文件，(3) 不会把新 inbound 从运行中 xray 移除。结果是活跃 inbound 被静默摘除且证书临时文件泄漏。应在阶段5比对对象身份（xrayIn == 当前 map 值）后再删。
- **证据**：exec_runner.go:528-530 阶段1解锁；:556 只判断 tag 是否存在；:561-567 清理旧 xrayIn.tempCertFiles 并 delete(e.inbounds,tag)。
- **核实**：代码核实：RemoveInboundConfig 阶段1在锁内取 xrayIn 后解锁(:528-530)，阶段5重新加锁仅判断 `if _,exists:=e.inbounds[tag];!exists{return}`(:556)，随后清理阶段1捕获的旧 xrayIn.tempCertFiles 并 delete(e.inbounds,tag)(:561-567)，全程不比对对象身份。FastAddInbound 确实存在(:1652)且在 map 插入(约:1960-2000)前后同样只用 inboundsMu 分段加锁、无更高层串行化，故阶段2-5窗口内并发同 tag re-add 会：按 tag 误删新对象、泄漏新对象 temp 文件、且新 inbound 未从运行中 xray 移除。TOCTOU 结构性成立。属低概率竞态，P2 略偏高但可接受。

### 13. [P2·confirmed] killProcessOnPort 仅按本地端口全局匹配 /proc/net/tcp，动态端口下可能误杀无关进程

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:289` · 维度：正确性 · 单元：XC
- **问题**：Start 会对 parsePortFromAddr(e.grpcAPIAddress) 调 killProcessOnPort（exec_runner.go:426）。该函数遍历 /proc/net/tcp，只比对 local_address 的端口部分（:289-303，strings.EqualFold(parts[1],hexPort)），完全忽略本地 IP、忽略 socket 状态（LISTEN/ESTABLISHED/TIME_WAIT 一视同仁），然后按 inode 找到 owner 进程直接 proc.Kill()（SIGKILL）。当 GRPCAPIAddress 走动态分配（net.Listen ":0" 得到 ephemeral 端口，通常落在 32768-60999），该端口同时也是内核给其它进程 outbound 连接分配 source port 的范围；若此刻系统里任何进程有一条 source port 恰等于该端口的连接，就会被误 SIGKILL。此外只读 /proc/net/tcp（IPv4）不含 tcp6。默认固定 62789 时风险低，但动态端口路径下是真实的误杀面。
- **证据**：exec_runner.go:289-303 仅按 hexPort 匹配 local 端口、不校验 IP/状态；:344 proc.Kill()；Start 调用点 :426。
- **核实**：代码核实：killProcessOnPort 只比对 /proc/net/tcp 的 fields[1] 端口部分(:301 strings.EqualFold)，忽略本地 IP 与 socket 状态，按 inode 找 owner 直接 proc.Kill()(:344)，Start 在 :426 调用。且动态端口路径不仅存在、还是默认：NewExecutor 在 GRPCAPIAddress 为空时用 net.Listen("127.0.0.1:0") 取 ephemeral 端口(:129-133)，该端口落在内核 source-port 范围，任何进程恰有此本地端口的连接都会被误 SIGKILL；只读 IPv4 /proc/net/tcp 不含 tcp6 亦属实。误杀面真实。

### 14. [P2·confirmed] validateRealityShortId 强制恰好 16 位十六进制，拒绝合法的短 shortId

- **位置**：`pkg/proxy/containers/xray/inbound_adapter.go:864` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游）
- **问题**：validateRealityShortId 要求 len(shortId)==16 否则报错（inbound_adapter.go:864-866）。而 xray Reality 的 shortId 语义是 0-16 位十六进制（偶数长度、最长 8 字节=16 hex），服务端可配置任意合法长度、客户端 shortId 需与服务端某一项匹配。因此外部传入合法的 8 位/12 位等 shortId（用户从别处迁移或对齐既有部署）会被 validateRealityShortIds（ToProvider 前置校验，:184-188）直接拒绝、整个 FastAddInbound 失败。内部自动生成的是 16 位（generateRandomRealityShortIDHex 8 字节），自洽，但对外部输入过严。建议放宽为“偶数长度且 ≤16 的 hex”。此判断依赖 xray Reality shortId 约定，建议复核 xray 文档确认长度上限/奇偶要求。
- **证据**：inbound_adapter.go:864-866 if len(shortId)!=16 { return err }；调用点 ToProvider:185-188 validateRealityShortIds。
- **核实**：protocolRelated 但有仓库内实证：validateRealityShortId 硬性要求 len==16(inbound_adapter.go:864-866)，在 ToProvider 前置 validateRealityShortIds 校验(:185-188)拒绝一切非16位输入。而本仓库真正喂给 xray-core proto 的解析层 pkg/xrayapi/types/types.go:742-751/768-782 对 shortId/shortIds 仅要求“合法 hex 且解码后 ≤8 字节”(即 ≤16 hex、偶数长度)，明确接受更短值。故适配层比 xray 层与 xray-core 语义更严，合法的 8/12 位 shortId 会被误拒。内部材料确证上游行为，可 confirmed。P2 合理。

### 15. [P2·confirmed] Restart 直接调 Runner.Restart，绕过 BaseContainer 状态机且不重建 reconcile 循环

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:480` · 维度：架构 · 单元：XC · 旧 review: X-04
- **问题**：Executor.Restart() 直接 return e.Runner.Restart()（exec_runner.go:480-482），既不走 BaseContainer 状态机，也不 stop/start reconcileLoop 与 userEventHandler。Reload() 走 BaseContainer.Restart()（:462-464），但 executorHooks.GetRunFunc 的 run/stop 只是 Runner.Start/Stop（:108-117），同样不触碰 reconcile 循环。二进制换新或重启后：reconcileStopCh/reconcileWg 仍指向旧 goroutine，若之前 Stop 过则 reconcile 已停不再重启；in-memory inbounds 也不会重新 syncInboundsFromXray（新 xray 进程只有 config.json 里的 inbound，动态加的全丢）。多套重启入口语义不一致。
- **证据**：exec_runner.go:480-482 Restart(){return e.Runner.Restart()}；:108-117 hooks 只调 Runner.Start/Stop；reconcile 由 startReconcileLoop/stopReconcileLoop 独立管理。
- **核实**：代码核实：Executor.Restart() 直接 `return e.Runner.Restart()`(:480-482)，不走 BaseContainer 状态机；Reload() 走 BaseContainer.Restart()(:462-464)，而 executorHooks.GetRunFunc 的 run/stop 仅 Runner.Start/Stop(:108-117)，均不触碰 reconcile 循环。syncInboundsFromXray 只在 Restore 中调用(:2402)，Start 的步骤0-4 也未调用，故进程换新/重启后动态加入的 in-memory inbounds 不会重新同步、新 xray 只保留 config.json 里的 inbound。多重启入口语义不一致成立。P2 合理。

### 16. [P2·confirmed] RemoveInboundNative 全程不删 store，而 AddInboundNative 会写 store，持久化状态不一致

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:842` · 维度：正确性 · 单元：XC · 旧 review: X-05
- **问题**：AddInboundNative 成功后会 storeMgr.InboundStore().Save(rec)（exec_runner.go:704-711），但对应的 RemoveInboundNative（:842-881）全程不调用 storeMgr，只释放端口、gRPC RemoveInbound、删 map。于是通过 Native 接口加入并持久化的 inbound 被 RemoveInboundNative 删除后，DB 里仍残留记录，重启 Restore 会把它“复活”。对比 RemoveInboundConfig（:540-544 会 Delete store）语义不一致。此外 gRPC 失败分支（:864-866）删 map 但不清 tempCertFiles，也有 temp 文件泄漏。
- **证据**：exec_runner.go:704-711 AddInboundNative Save；:842-881 RemoveInboundNative 无 storeMgr 调用；对照 :540-544 RemoveInboundConfig 有 Delete。
- **核实**：代码核实：AddInboundNative 成功后 storeMgr.InboundStore().Save(:704-714)；RemoveInboundNative(:842-881)全程无 storeMgr 调用，仅释放端口/gRPC/删 map，且成功与 gRPC 失败分支(:864-866)均不清理 tempCertFiles。对照 RemoveInboundConfig 有 Delete(:540-544)与 temp 清理(:561-565)。Restore(:2260-2265)从 InboundStore().Load() 重放记录，故经 Native 接口加入并持久化、再经 RemoveInboundNative 删除的 inbound，重启会被“复活”。持久化不一致与 temp 泄漏均属实。P2 合理。

### 17. [P2·confirmed] 每个 gRPC 操作新建并 Dial 连接，AddInbound 内还硬编码 500ms sleep

- **位置**：`pkg/proxy/containers/xray/grpc_client.go:169` · 维度：资源 · 单元：XC · 旧 review: X-08
- **问题**：ListInbounds/RemoveInbound/AddUser/RemoveUser/QueryStats 各自 grpc.Dial+defer Close（grpc_client.go:256/280/320/352/373），AddInbound 每次调用开头无条件 time.Sleep(500*time.Millisecond)（:169）再带 3 次重试。高频用户/inbound 同步下反复建连、每次 AddInbound 至少多等 500ms，既浪费又拖慢批量恢复（Restore 里每个 inbound 一次 AddInboundNative→AddInbound）。且重试是否 retryable 靠错误字符串匹配（:222-226），脆弱。建议持久化单条连接或连接池，去掉固定 sleep 改用 WaitForReady（该文件已有 WaitForReady 实现但 AddInbound 未用）。
- **证据**：grpc_client.go:169 time.Sleep(500ms)；:256,:280,:320,:352,:373 各自 grpc.Dial；:222-226 基于字符串判断 retryable。
- **核实**：代码核实：AddInbound 开头无条件 time.Sleep(500ms)(:169)再带 3 次重试；ListInbounds/RemoveInbound/AddUser/RemoveUser/QueryStats 各自 grpc.Dial+defer Close(:256/280/320/352/373)；重试 retryable 靠错误字符串匹配(:222-226)脆弱；WaitForReady 已实现(:79)但 AddInbound 未用。Restore 每 inbound 一次 AddInboundNative→AddInbound，批量恢复反复建连且每条至少多等 500ms，效率/健壮性问题真实。属性能优化类，P2 偏高(更接近 P3)但不改判。

### 18. [P2·confirmed] buildStreamSettings 缺 h2/http 分支，VMess+h2 的 http_path/http_host 被静默丢弃

- **位置**：`pkg/proxy/containers/xray/inbound_adapter.go:416` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游）
- **问题**：GenerateVMessTLSInboundSpec 接受 Transport="h2"（vmess_tls.go:68-96）并写入 extensions["http_path"]/["http_host"]（vmess_tls.go:159-166）；FastAddInbound 也把 params http_path/http_host 透传（exec_runner.go:1741-1742）。但 Adapter.buildStreamSettings 只处理 ws/grpc/httpupgrade/xhttp（inbound_adapter.go:417-468），没有 h2/http 分支，也不生成 httpSettings。结果 streamSettings.network 被置为 "h2"，但 path/host 完全丢失；且 xray 的 HTTP/2 network 名通常为 "http" 而非 "h2"（需核实上游），network 名可能也不被识别。订阅侧同样无 http_path/http_host（buildSubscriptionExtensions 的 copyKeys 不含这两键，exec_runner.go:1534-1550；extractNativeExtra 也无 httpSettings 分支，exec_runner.go:744-789）。因此 VMess+h2 端到端配置错误。
- **证据**：inbound_adapter.go:417-468 分支仅 ws/grpc/httpupgrade/xhttp；vmess_tls.go:159 `if p.Transport == "h2" { extensions["http_path"]=... }`;exec_runner.go 无 httpSettings 提取（grep httpSettings 仅命中 splithttpSettings）。
- **核实**：核实属实(核心数据丢失部分,不依赖上游断言)。GenerateVMessTLSParams 接受 Transport=h2(http 归一化为 h2,vmess_tls.go:64-68),applyDefaults 甚至给 h2 填默认 http_path=/、http_host,并写入 extensions[http_path/http_host](:159-166)。但 Adapter.buildStreamSettings(inbound_adapter.go:416-468)只有 ws/grpc/httpupgrade/xhttp 分支,无 h2/http 分支,network 被置 h2 却不生成任何 httpSettings——用户设置的 http_path/http_host 在服务端配置中确实无处落地。订阅侧:live 路径 GetSub 用的是 method 版 buildSubscriptionExtensions(exec_runner.go:1521),其 copyKeys(:1534-1550)不含 http_path/http_host,故订阅也丢失(注:subscription.go:189 另有一个自由函数版 copyKeys 含这两键且 :356-358 有 net=h2 处理,但该函数不在 GetSub 实际路径上,两版分叉)。此为可从仓库内代码证明的『捕获后从未被消费』数据丢失缺陷。protocolRelated 注意:finding 中『xray network 名应为 http 而非 h2』的子论断作者已自标『需核实上游』,仓库内无材料佐证,我不予采信、也不作为 confirmed 依据;确认仅基于 http_path/http_host 被静默丢弃这一代码事实。P2 合理。

### 19. [P2·confirmed] VLESS/Trojan 分享链接的 path/host/sni 等查询参数未做 URL 编码

- **位置**：`pkg/proxy/containers/xray/subscription.go:497` · 维度：正确性 · 单元：XC
- **问题**：buildShareLinkParams 直接以字符串拼接 type/security/path/host/serviceName/seed/sni/alpn 等参数（subscription.go:484-577），未 url.QueryEscape。当 ws_path 为常见的早数据形式 "/vmess?ed=2048"（含 ? 与 =）或 host/sni 含特殊字符时，生成的 vless://.../trojan://... URI 查询串会被破坏（"?ed=2048" 会与后续 &type… 混淆），客户端解析出错。同一函数里 VLESS flow 用了 url.QueryEscape（subscription.go:258）、pbk 也编码（:584），但 Trojan flow 又是裸拼（:395），编码策略前后不一致。VMess 走 JSON 不受影响。
- **证据**：subscription.go:497 `params = append(params, "path="+p)`;:499 `"host="+h`;:566 `"sni="+sni`;对比 :258 flow 用 url.QueryEscape、:395 Trojan flow 未编码。
- **核实**：核实属实。buildShareLinkParams(subscription.go:477-588)对 path(:497)、host(:499)、serviceName(:504)、sni(:566)、seed 等均裸字符串拼接,未 url.QueryEscape;仅 pbk(:584)与 VLESS 的 flow(:258)做了编码,而 Trojan 的 flow(:395)又是裸拼,编码策略前后确实不一致。当 ws_path 取早数据形式 "/vmess?ed=2048"(含 ? =)时,拼进 vless://...?type=...&path=/vmess?ed=2048&... 会破坏查询串结构。这是标准 RFC3986 层面的 URL 编码缺陷,不涉及对特定客户端解析行为的臆断,触发场景(带 query 的 ws early-data path)真实存在。host/sni 多为纯域名较少触发,故 P2 合理。

### 20. [P2·confirmed] 用户事件 goroutine 无停止机制，Stop 后仍向已死 xray 下发用户变更且泄漏

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:923` · 维度：资源 · 单元：XC
- **问题**：forwardUserEvents（NewExecutor 中 go 启动，exec_runner.go:164）range UserManager.Subscribe() 源通道，startUserEventHandler 起的 goroutine range e.userEventCh（exec_runner.go:928-932）。整包无任何 Unsubscribe 调用，userEventCh 也从不 close。Executor.Stop() 只停 reconcile loop（exec_runner.go:473-477），这两个 goroutine 会一直存活。Stop 之后若有用户增删，handleUserEvent 仍会触发 XrayInbound.AddUser/RemoveUser（经 gRPC 打向已停止的 xray 进程 / 分配释放 forward 端口），产生错误日志与状态漂移，并造成 goroutine 泄漏（与 hysteria/snell 的 H-01/H-02 同类，但 xray 未订阅取消）。xray Executor 通常为进程级单例，影响相对可控，但停机/重建场景存在真实泄漏与副作用。
- **证据**：exec_runner.go:164 `go e.forwardUserEvents(cfg.UserManager.Subscribe())`;exec_runner.go:929 `for event := range e.userEventCh`;exec_runner.go:473-477 Stop 只 stopReconcileLoop + Runner.Stop；全仓 grep 该包无 Unsubscribe/close(userEventCh)。
- **核实**：核实属实。NewExecutor(exec_runner.go:164)无条件 go forwardUserEvents(cfg.UserManager.Subscribe()),start(434)又 go startUserEventHandler(923-932)range e.userEventCh。全包 grep 无 Unsubscribe、也无 close(userEventCh)。Stop()(473-477)仅 stopReconcileLoop()+Runner.Stop(),两个 goroutine 随 Subscribe 源通道存活;Stop 后源通道若继续推事件,handleUserEvent 仍会经 gRPC 打向已停 xray。非协议类,纯代码事实可确证。P2 合理(进程级单例,影响可控但泄漏与副作用真实)。

### 21. [P2·confirmed] handleUserEvent 的 Add 分支不做 IsUserVisible 过滤，集群分组用户会在非所属节点上短暂获得转发端口/订阅

- **位置**：`pkg/proxy/containers/mihomo/container.go:835` · 维度：正确性 · 单元：MC
- **问题**：handleUserEvent 对 UserEventUpdate 会先 `!c.userMgr.IsUserVisible(event.User)` 判定可见性再决定 add/remove（container.go:840-844），但 UserEventAdd 分支直接 `c.syncUserToInbound(event.Username, event.User)`（container.go:835-836），完全不检查可见性。UserManager.AddUser / 集群同步路径（usermanager.go:532-536、1581-1582）对新增/远端同步用户无条件 emit UserEventAdd（不按节点分组过滤）。isUserVisibleLocked（usermanager.go:290-299）在 clusterEnabled 且用户 TargetGroup 不在本节点 cachedNodeGroups 时返回 false。于是一个属于其它分组的用户被 Add 时，mihomo 仍会对本节点每个 inbound 调 GetBindPort 分配 forward 端口，并使其可通过 GetUserSubscriptions 生成可用订阅——即在不属于该用户的节点上短暂开放端口/放行流量，直到 30s 后 reconcileUsers（用 ListUsers 已按可见性过滤，container.go:905/reconcileUsers）把它清掉。属于同文件内自相矛盾的隔离缺口，虽能在 ≤reconcileInterval 内自愈，但集群+分组部署下是真实的越界暴露窗口。建议 Add 分支与 Update 一致，先过 IsUserVisible。
- **证据**：container.go:835-836 Add 分支无可见性判断直接 syncUserToInbound；对比 container.go:840 Update 分支 `!c.userMgr.IsUserVisible(event.User)`；usermanager.go:290-299 isUserVisibleLocked 按 TargetGroup/cachedNodeGroups 决定可见；usermanager.go:532 AddUser 无条件 emit UserEventAdd。
- **核实**：属实。handleUserEvent 的 Add 分支(container.go:835-836)直接 syncUserToInbound 无任何可见性判断,而 Update 分支(840)先 !IsUserVisible;usermanager.go AddUser(532-536)与 SyncUpsertUser 新用户路径(1581-1585)对新增/远端同步用户无条件 emit UserEventAdd;isUserVisibleLocked(290-299)在 clusterEnabled 且 TargetGroup 不在 cachedNodeGroups 时返回 false。故非本分组用户被 Add 时,本节点仍会对每个 inbound 调 AddUser→GetBindPort 分配转发端口,形成越界暴露窗口,直到 30s 后 reconcileUsers(用 visibility-filtered ListUsers,905/911-919)清除。集群+分组部署下确为真实的短暂隔离缺口,P2 可接受。

### 22. [P2·confirmed] external_controller 未强制 loopback，配合空 secret 可把 mihomo 无鉴权管理 API 暴露到公网

- **位置**：`pkg/proxy/containers/mihomo/config.go:86` · 维度：安全 · 单元：MC · ⚠️协议相关（需人工核对上游）
- **问题**：MihomoConfig.validate()（config.go:73-93）只校验 external_controller 非空且是合法 host:port，从不检查它是否绑定回环地址；Secret 允许为空。字段注释（config.go:16-22）自己写明『bind to loopback only — the API is not meant to be exposed』且『When empty, mihomo skips authentication entirely; acceptable as long as ExternalController is bound to 127.0.0.1』，但没有任何代码把这个前提落地为校验。若操作员把 external_controller 配成 `0.0.0.0:9090`（或对外网卡）并沿用默认空 secret，mihomo 的 external-controller REST API（PUT /configs 可任意改写监听器/整个运行配置，等同远程接管代理内核）就会无鉴权对外可达。默认值 127.0.0.1:9090 是安全的，但缺少护栏使一处误配即造成严重暴露。建议：当 host 非回环时强制要求非空 Secret（或直接拒绝非回环绑定）。
- **证据**：config.go:86 仅 net.SplitHostPort 校验；config.go:44 `c.Secret = ""` 默认空；config.go:19-22 注释承认空 secret=无鉴权且依赖『bound to 127.0.0.1』但无对应校验分支。
- **核实**：亲验 config.go validate()(83-88 行)只做非空 + net.SplitHostPort 合法性校验,无任何回环绑定判断;Decode 默认 Secret=""(44 行)、ExternalController=127.0.0.1:9090(43 行)。字段注释(16-22 行)确实自证『bind to loopback only』『空 secret=无鉴权,仅在绑 127.0.0.1 时可接受』却无对应校验分支。protocolRelated 上游行为由仓库材料确证:wiki/knowledge/mihomo-container/edge-cases.md:118 明写『不要绑 0.0.0.0——mihomo REST API 既能 CRUD listener 又能读 stats,暴露到公网等于交出节点 root 权限;Secret 只是第二道防线』,details.md:133 确认 RESTClient 仅在 Secret 非空时加 Authorization: Bearer(即空 secret 不带鉴权头)。故『非回环 + 空 secret = 无鉴权对外可达』成立,缺护栏属实。默认安全但一处误配即严重暴露,P2 可接受。

### 23. [P2·confirmed] reconcileStopCh 读写无锁，与 mu 保护的 userEventHandlerStarted 不一致，Start/Stop 并发下存在 data race

- **位置**：`pkg/proxy/containers/mihomo/container.go:1009` · 维度：并发 · 单元：MC
- **问题**：startReconcileLoop（container.go:1002-1024）与 stopReconcileLoop（container.go:1028-1035）对 c.reconcileStopCh 做无锁读/写/置 nil：start 读判空后 `c.reconcileStopCh = make(...)`（:1006-1009），stop `close` 后 `c.reconcileStopCh = nil`（:1032-1034）。相邻的 userEventHandlerStarted 却是用 c.mu 保护的（:806-811），说明作者已意识到 Start-retry 幂等需要同步，唯独 reconcile 这组字段遗漏了锁。run hook（startReconcileLoop）和 stop hook（stopReconcileLoop）分别由 BaseContainer.Start/Stop 在释放状态锁之后调用（下层 CON CT-05/CT-06 已确认 base.go 在 runFunc 前解锁、且 Stopping 态 Stop 会继续推进），因此两个 hook 可并发执行，对 reconcileStopCh 形成并发读写 data race，极端下还可能 double-close 或对已被置 nil 的 channel 操作。即便不触发下层竞态，锁使用的不一致本身也是隐患。
- **证据**：container.go:1006-1009 与 1032-1034 无锁访问 reconcileStopCh；对比 :806-811 userEventHandlerStarted 用 c.mu 保护；依赖下层 base.go Start/Stop 可并发（_lower_digest CON CT-05/CT-06）。
- **核实**：核实 base.go:Start() 遇 Stopping 态直接置 Starting 并继续执行(153-157 行),解锁后调 runFunc(179-182 行)=startReconcileLoop;而并发的 Stop() Running 分支在解锁后才调 stopFunc(251-260 行)=stopReconcileLoop。两 hook 确可并发。container.go:1006-1009 与 1032-1034 对 reconcileStopCh 做无锁读/make/close/置 nil,而相邻 userEventHandlerStarted(806-812 行)用 c.mu 保护,锁使用不一致属实,构成对 reconcileStopCh 的并发读写 data race,极端下可 double-close 或操作已置 nil 的 channel。触发路径真实,未被上层排除。

### 24. [P2·confirmed] 重启/升级后 userEventCh 不再重建,事件驱动用户处理永久失效,仅剩 30s reconcile

- **位置**：`pkg/proxy/containers/hysteria/container.go:463` · 维度：正确性 · 单元：HC
- **问题**：userEventCh 只在 NewHysteriaContainer(container.go:156) 构造时创建一次。第一次 Stop 后 closeUserEventCh 将其 close 并置 nil(container.go:841),但后续 Start 的 run() 不会重建它;startUserEventHandler(container.go:462-465) 见 `hc.userEventCh == nil` 直接 return,不再消费任何事件。因此任何一次 Stop/Start 循环(包括 Update 二进制升级、BaseContainer.Restart)之后,Add/Remove/Update 用户事件驱动的转发端口分配/回收彻底停摆,只依赖 30s 周期 reconcileUsers 兜底——新增用户最长 30s 才能连上、被删用户端口最长 30s 才回收。原 forwardUserEvents goroutine 仍在运行但因 userEventCh 为 nil 丢弃全部事件,且不会重新 Subscribe。
- **证据**：container.go:156 构造时唯一一次 `hc.userEventCh = make(chan usermanager.UserEvent, 100)`;:463 `if hc.userEventCh == nil { return }`;run()(:60-80) 与 restoreInboundConfig 均未重建 userEventCh。
- **核实**：核实属实。userEventCh 仅在构造 :156 创建一次;closeUserEventCh(:841)close 后置 nil;run()(:60-80)只重建 certWaitStopCh,不重建 userEventCh;restoreInboundConfig 亦不重建;startUserEventHandler(:463)见 nil 直接 return。任一 Stop/Start 循环后事件驱动处理停摆,仅剩 :814 的 30s reconcile 兜底。P2 合理(降级非崩溃,有兜底)。

### 25. [P2·confirmed] Update 启动失败回滚旧二进制后 hc.cfg.Version 仍为目标版本,Version() 谎报

- **位置**：`pkg/proxy/containers/hysteria/container.go:335` · 维度：正确性 · 单元：HC
- **问题**：Update 在 container.go:335 先 `hc.cfg.Version = targetVersion`,再 hc.Start()。若 Start 失败(container.go:338),代码把 backupPath 旧二进制 rename 回原位并重启旧版(container.go:340-341),但 hc.cfg.Version 仍停留在 targetVersion。此后 Version()(container.go:277)返回新版本号,而实际运行的是旧二进制,集群版本上报/后续 Update 判定全部基于错误版本。同理无 backup 的全新安装场景 Start 失败时,进程未起但 Version 已是新值。回滚的两次 os.Rename 和第二次 Start 错误也被丢弃。
- **证据**：container.go:335 `hc.cfg.Version = targetVersion`;:338 `if err := hc.Start(); err != nil {`;:340 `_ = os.Rename(backupPath, hc.cfg.BinaryPath)`,回滚分支未复位 Version。
- **核实**：核实属实。:335 先置 hc.cfg.Version=targetVersion 再 :338 Start;失败回滚分支 :339-342 只 rename 旧二进制并再次 Start,未复位 Version;Version()(:278)返回 hc.cfg.Version,故回滚后谎报目标版本。缺陷真实,惟触发被 idx1 收窄——因 run() 立即 return nil,hc.Start() 几乎不会返回错误,:338 分支很少进入;实际影响有限但代码缺陷成立。P2 合理。

### 26. [P2·confirmed] 构造时 Subscribe 无对应 Unsubscribe,forwardUserEvents 协程随每次实例泄漏

- **位置**：`pkg/proxy/containers/hysteria/container.go:157` · 维度：资源 · 单元：HC · 旧 review: H-01
- **问题**：NewHysteriaContainer(container.go:157) `go hc.forwardUserEvents(hc.userMgr.Subscribe())` 订阅 UserManager,但整个 hysteria 包无任何 Unsubscribe 调用(仅 mihomo/container.go:278 有)。forwardUserEvents(container.go:450) `for event := range source` 依赖 source 关闭才退出,而 stop 的 closeUserEventCh 只关本地 userEventCh、不关订阅源。于是每个 HysteriaContainer 实例的 forwardUserEvents goroutine 与 UserManager 内部订阅项永久驻留;容器被替换/重建(如 ContainerMgr 重复 LoadFromConfig)时旧订阅继续接收事件、无界累积,且 UserManager 每次广播都要向死订阅 channel 发送。
- **证据**：container.go:157 `go hc.forwardUserEvents(hc.userMgr.Subscribe())`;:450 `for event := range source`;:838-843 closeUserEventCh 只 close(hc.userEventCh),不 Unsubscribe;grep 全包无 Unsubscribe。
- **核实**：核实属实。:157 go forwardUserEvents(userMgr.Subscribe());grep 确认 hysteria 包无任何 Unsubscribe(仅 mihomo container.go:278 有)。forwardUserEvents(:451)for range source,Subscribe 返回的 channel 永不被关闭(Unsubscribe 也只从 slice 摘除、不 close),故 goroutine 永不退出;closeUserEventCh 只关本地 userEventCh。emitEvent(usermanager.go:453-458)非阻塞广播到不断增长的订阅 slice,容器重建时旧订阅与 goroutine 无界泄漏。P2 合理。

### 27. [P2·confirmed] 测试仅覆盖 FastAddInbound,核心并发/生命周期/配置生成/订阅全无测试

- **位置**：`pkg/proxy/containers/hysteria/container_fastadd_test.go:1` · 维度：测试缺口 · 单元：HC
- **问题**：hysteria 容器唯一测试 container_fastadd_test.go 只验证 FastAddInbound 的 tag 契约。占本单元全部风险的路径无任何覆盖:waitForCertAndStart 证书轮询/停止竞态(H-04)、handleUserEvent 与 reconcileUsers 并发对账、Update 的 backup/rollback 多分支、generateConfigFile 生成的 YAML(listen=127.0.0.1、auth.http.url、trafficStats)、GetUserSubscriptions 生成的 hysteria2:// URI 与 insecure 语义、traffic.go 的 rx/tx→Upload/Download 换算与 KickUser。这些恰是最易回归且历史已多次出问题的地方,违反‘每个可测模块应有 *_test.go’的架构约束(此处虽有文件但覆盖面严重不足)。
- **证据**：container_fastadd_test.go 仅含 TestFastAddInbound_LegacyPath_Regression;无 process/traffic/subscription/reconcile/update 相关测试文件。
- **核实**：核实本单元唯一测试 container_fastadd_test.go 仅含 TestFastAddInbound_LegacyPath_Regression,目录内无其他 *_test.go(ls 确认)。process.go 的 generateConfigFile(listen/auth.http.url/trafficStats)、traffic.go 的 rx/tx→Upload/Download 与 KickUser、GetUserSubscriptions、reconcile/Update 等高风险路径确无覆盖。覆盖缺口属实;鉴于本单元回归历史,P2 可接受,不覆盖 severity。

### 28. [P2·confirmed] userEventCh 停机置 nil 后永不重建，Update/重启后事件驱动的用户同步永久失效，只剩 30s reconcile

- **位置**：`pkg/proxy/containers/snell/container.go:111` · 维度：正确性 · 单元：SC
- **问题**：userEventCh 仅在 NewSnellContainer(container.go:111) 创建一次；stop hook 的 closeUserEventCh(701) 将其置 nil 后没有任何路径重新 make。run hook 里的 startUserEventHandler(368-377) 开头是 `if sc.userEventCh == nil { return }`，因此一旦执行过一次 Stop（例如 Update 走 sc.Stop()→sc.Start()，container.go:196/228；或任何显式停止后再启动），下一次 Start 时事件消费 goroutine 直接 no-op，forwardUserEvents 又因 channel 为 nil 丢弃所有上游事件。结果：整个进程生命周期内只要发生过一次 Update，snell 的 Add/Remove/Update 事件驱动同步就彻底停摆，用户增删只能靠 30s 周期 reconcile 兜底（新增用户最长延迟 30s 才拿到转发端口，删除同理）。修复：在 run hook 里若 userMgr!=nil 则重新 make(userEventCh) 并重新订阅，或不要在 stop 时把 channel 置 nil。
- **证据**：container.go:111 唯一创建点在构造函数；container.go:701 `sc.userEventCh = nil`；container.go:369 `if sc.userEventCh == nil { return }` 使 startUserEventHandler 在重启后空转；Update 在 196/228 调用 Stop()/Start()
- **核实**：核实属实。userEventCh 唯一创建点在构造函数 111 行(且受 userMgr!=nil 守卫),closeUserEventCh 701 行置 nil 后无任何路径重建;startUserEventHandler 369 行 `if sc.userEventCh == nil { return }` 使重启后事件消费 goroutine 空转,forwardUserEvents 358 行因 nil 丢弃全部上游事件。Update(196 Stop→228 Start,同一 sc 实例)后事件驱动同步彻底失效。已确认 reconcile 循环在 run hook 每次重建(669 新建 reconcileStopCh),故 30s 周期兜底仍有效,与 finding 描述一致。P2 合理。

### 29. [P2·confirmed] Subscribe 后从不 Unsubscribe，forwardUserEvents goroutine 与订阅者槽位泄漏

- **位置**：`pkg/proxy/containers/snell/container.go:112` · 维度：资源 · 单元：SC · 旧 review: H-01
- **问题**：NewSnellContainer 在 112 行 `go sc.forwardUserEvents(sc.userMgr.Subscribe())` 订阅事件，但整个 snell 包无任何 Unsubscribe 调用（对照 mihomo/container.go:278 有 Unsubscribe）。closeUserEventCh(698-703) 只关闭本地 userEventCh，不动上游订阅。因此上游 subscribers 列表中该 channel 永不移除，forwardUserEvents 的 `for event := range source`(357) 永远不会因 source 关闭而退出——容器 Stop 后该 goroutine 仍常驻并持续从 UserManager 消费事件。多次创建/销毁容器会累积泄漏 goroutine 和 UserManager.subscribers 槽位，且 UserManager 广播时仍会向这些死订阅者投递（channel 满则阻塞/丢弃，取决于其广播实现）。修复：stop 时调用 sc.userMgr.Unsubscribe(源 channel) 并让 forwardUserEvents 随 source 关闭而退出。
- **证据**：container.go:112 `go sc.forwardUserEvents(sc.userMgr.Subscribe())`；container.go:357 `for event := range source`；grep 全包无 Unsubscribe；usermanager.go:412 提供 Unsubscribe 但未被调用
- **核实**：核实属实。全包 grep 无 Unsubscribe;NewSnellContainer 112 行 `go sc.forwardUserEvents(sc.userMgr.Subscribe())` 订阅后,closeUserEventCh 只关本地 userEventCh,不动上游订阅槽位。forwardUserEvents 357 行 `for event := range source` 因 source 永不关闭而常驻,Stop 后 goroutine 仍活。对照 mihomo/container.go:278 在 Close 中 Unsubscribe(sub)。usermanager.go:412 提供 Unsubscribe 但 snell 从不调用;emitEvent(453)仍会向死订阅者投递(满则 default 丢弃)。容器重复创建即累积 goroutine+subscribers 槽位泄漏。P2 合理。

### 30. [P2·confirmed] snell-server 二进制下载无完整性校验、无大小上限，可被中间人替换或 zip-bomb 撑爆磁盘后以 0755 落盘执行

- **位置**：`pkg/proxy/containers/snell/downloader.go:92` · 维度：安全 · 单元：SC
- **问题**：downloadSnellServer 从 dl.nssurge.com 拉取 zip 后：(1) 无任何 checksum/签名校验就把 zip 内名为 snell-server 的条目写到 cfg.BinaryPath 并以 0755 可执行位落盘（downloader.go:86-94），供 startProcess 直接 exec——一旦 DNS/TLS 被攻破或上游资产被投毒即形成远程代码执行；(2) 下载 `io.Copy(tmpFile, resp.Body)`(50) 与解压 `io.Copy(outFile, rc)`(92) 均无 io.LimitReader，恶意/异常响应或 zip-bomb 可无上限写盘导致磁盘耗尽。startProcess(process.go:15-21) 在缺二进制时也会自动触发此下载，属默认路径。建议：固定各版本 sha256 白名单校验、对下载与解压加 LimitReader，并考虑 0700。
- **证据**：downloader.go:50 `io.Copy(tmpFile, resp.Body)` 无限制；downloader.go:86 `os.OpenFile(binaryPath, ...|O_TRUNC, 0755)`；downloader.go:92 `io.Copy(outFile, rc)` 无 LimitReader；全流程无 checksum/签名校验
- **核实**：代码事实属实。downloader.go:50 `io.Copy(tmpFile, resp.Body)` 与 92 `io.Copy(outFile, rc)` 均无 io.LimitReader,恶意响应/zip-bomb 可无上限写盘;86 行以 0755 落盘可执行位;全流程无 checksum/签名校验,process.go:15-21 缺二进制时自动触发下载并 exec,属默认路径。磁盘耗尽与供应链投毒风险真实(RCE 需 HTTPS/上游资产被攻破,属条件性)。作为安全加固 finding 成立,P2 可接受。

### 31. [P2·confirmed] snell 容器包零测试覆盖：Update 回滚状态机、reconcile、INI 生成、PSK 生成、URI、并发锁分段全部无 *_test.go

- **位置**：`pkg/proxy/containers/snell/container.go:1` · 维度：测试缺口 · 单元：SC
- **问题**：pkg/proxy/containers/snell/ 目录下无任何 *_test.go 文件（仅 config/container/downloader/process/register 五个源文件）。违反架构约束 #9『每个可测模块应有 *_test.go』。本模块恰恰包含高风险逻辑：Update 的多步 rename+回滚状态机(container.go:174-247)、handleUserEvent 与 reconcileUsers 的锁分段并发正确性(依赖 GetBindPort 幂等)、restoreInboundConfig 的类型断言解析(523-533)、generateConfigFile 的 INI 输出、generatePSK、GetUserSubscriptions 的 URI 拼装。这些都无回归保护，前述若干缺陷（如 userEventCh 置 nil 不重建、URI version 硬编码）本可被单测暴露。建议至少补：Update 各失败分支回滚、reconcile add/remove 幂等、PSK 生成、URI 格式的表驱动测试。
- **证据**：`ls pkg/proxy/containers/snell/` 仅 config.go container.go downloader.go process.go register.go；`grep -rn 'func Test' pkg/proxy/containers/snell/` 无命中
- **核实**：核实属实。ls 与 grep 确认 pkg/proxy/containers/snell/ 仅 config/container/downloader/process/register 五个源文件,无任何 *_test.go、无 `func Test`。Update 回滚状态机(174-247)、handleUserEvent/reconcileUsers 锁分段并发、restoreInboundConfig 类型断言(523-533)、generateConfigFile INI 输出、GetUserSubscriptions URI 拼装(575,含 version=5 硬编码)全无回归保护,违反架构约束#9。前述 idx0/1 等缺陷本可被单测暴露。测试覆盖缺口事实成立,P2 合理。

### 32. [P2·uncertain] validate2022Password 用 base64.StdEncoding 解码,拒绝合法的无 padding 2022 密钥

- **位置**：`pkg/proxy/containers/xray/profilegen/shadowsocks.go:199` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游） · 旧 review: SS-02
- **问题**：SS-2022 系列密钥在生态中常以 base64(带或不带 padding)表示。validate2022Password 只用 `base64.StdEncoding.DecodeString`(shadowsocks.go:199),StdEncoding 强制要求 padding,导致无 padding 但合法的 32/16 字节 2022 密钥被误判为 "not valid base64" 而拒绝。应同时尝试 RawStdEncoding/RawURLEncoding。注意自动生成路径(generateRandomSSPassword,:182)用 StdEncoding 编码(带 padding)所以自生成不受影响,但用户显式传入的合法无 padding 密钥会被拒。
- **证据**：shadowsocks.go:199 `decoded, err := base64.StdEncoding.DecodeString(password)`,随后 :204 `if len(decoded) != keyLen` 长度校验;无 RawStdEncoding 兜底
- **核实**：代码事实成立:validate2022Password(shadowsocks.go:199)仅用 base64.StdEncoding.DecodeString,无 Raw 兜底,StdEncoding 强制 padding。但'无 padding 的 SS-2022 密钥是合法且应被接受的输入'属上游/生态行为断言,protocolRelated 且仓库无佐证——生成路径与全部测试(shadowsocks_test.go:364/408/424/440)均用带 padding 的 StdEncoding,无任何 wiki/docs/测试证明 xray 或客户端接受无 padding 2022 密钥。按对抗规则不能凭记忆确认上游,最高 uncertain。

### 33. [P2·uncertain] Reality backward-compat 分支写入 xray 不识别的单数 key "shortId",Reality 握手 shortId 缺失

- **位置**：`pkg/proxy/containers/xray/inbound_adapter.go:551` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游） · 旧 review: A-02
- **问题**：buildStreamSettings 处理 Reality shortId 时,若只提供单数扩展 reality_short_id(reality_short_ids 为空),会写入 `realitySettings["shortId"] = realityShortID`(inbound_adapter.go:549-551)。而 xray-core realitySettings 的规范字段是数组 `shortIds`,不识别单数 `shortId`,该值被忽略,xray 会以空 shortId 集合运行,可能拒绝携带 shortId 的客户端或降低伪装强度。主路径(reality_short_ids 数组、自动生成)已正确写 shortIds(:546、:555),仅此 backward-compat 分支残留错误 key。profilegen 不产出单数 key,故仅当外部直接以 reality_short_id 扩展调用时触发。
- **证据**：inbound_adapter.go:549-551 `realityShortID := getString("reality_short_id"); if realityShortID != "" { realitySettings["shortId"] = realityShortID }`;对比 :546 `realitySettings["shortIds"] = realityShortIDs`
- **核实**：代码写入事实成立:backward-compat 分支(inbound_adapter.go:549-551)确实写单数 realitySettings["shortId"],主路径写数组 shortIds。但核心危害断言'xray-core 不识别单数 shortId、握手 shortId 缺失'属上游解析行为,protocolRelated 且仓库无材料确证——恰相反,本仓测试 inbound_adapter_test.go:468/486-487 把写单数 shortId 标为'valid backward compat shortId'并断言其存在,说明作者认为该形态有效,与 finding 结论相冲突。无 wiki/docs 证明 xray 忽略单数键,不能凭记忆确认,最高 uncertain。

### 34. [P3·confirmed] handleUserEvent 的 Add 分支不做可见性校验，与 Update 分支不一致，会为不可见用户建 forward 规则

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:946` · 维度：正确性 · 单元：XC
- **问题**：handleUserEvent 对 UserEventUpdate 会先 e.userMgr.IsUserVisible(event.User) 决定 add 还是 remove（exec_runner.go:957-961），但 UserEventAdd 分支无条件 syncUserToInbound（:946-948），不检查可见性。若集群同步/其它路径对一个在本节点分组不可见的用户发出 Add 事件，会立即为其在每个 inbound 上 GetBindPort 创建 forward 端口与规则（占用端口）。周期 reconcileUsers 用 ListUsers()（已按可见性过滤）能在 ≤30s 内把该用户从 inbound 摘除，属于自愈，但存在最长 30s 的错误暴露窗口且与 Update 分支语义不一致。建议 Add 分支同样加 IsUserVisible 判断。
- **证据**：exec_runner.go:946-948 Add 直接 syncUserToInbound；:957-961 Update 先 IsUserVisible；ListUsers(usermanager.go:714) 才做可见性过滤。
- **核实**：核实属实。handleUserEvent 的 Add 分支(exec_runner.go:946-948)无条件 syncUserToInbound，Update 分支(:957-961)才有 IsUserVisible 判断，语义确不一致。触发路径真实存在：usermanager.go:1581 的 SyncUpsertUser(集群同步新用户)会对来自远端、TargetGroup 可能在本节点不可见的用户无条件 emitEvent(UserEventAdd)，不做任何可见性预过滤；本地 AddUser(:532)同理。因此不可见用户会被 syncUserToInbound 建 forward 端口/规则。reconcileUsers 走 ListUsers(已可见性过滤)在 ≤30s 内自愈，暴露窗口有限，P3 合理，行号 946 准确。

### 35. [P3·confirmed] Update 重启失败时 swapper.Rollback 返回值被丢弃，回滚失败无感知

- **位置**：`pkg/proxy/containers/xray/updater.go:186` · 维度：错误处理 · 单元：XC · 旧 review: X-12
- **问题**：Update 第6步在 Stop/Start 失败时调用 u.swapper.Rollback(u.config.BinaryPath, backupPath)（updater.go:186、192）但两处均未接收返回错误、也不记日志，直接返回 restart 错误。若 Rollback 本身失败（结合下层已确认的 binary_swapper 缺陷：SwapAtomic 在目标不存在时返回不存在的 backupPath，Rollback 反而会把新二进制挪走），会留下二进制处于不一致/丢失状态且上层完全无从得知。至少应记录 rollback 错误并在最终错误里带上。
- **证据**：updater.go:186 u.swapper.Rollback(...)（无赋值/无日志）；:192 同样；下层 _lower_digest：binary_swapper.go:25 SwapAtomic/Rollback 缺陷。
- **核实**：核实属实。BinarySwapper.Rollback 接口(updater.go:46)返回 error，但 updater.go:186、192 两处调用均未接收返回值、也无 log，随后直接返回 restart 错误。若 Rollback 失败上层完全无从得知，属真实的错误可观测性缺口。发生在 Stop/Start 失败且回滚又失败的罕见路径，P3 合理。

### 36. [P3·confirmed] RealityKeyStore 一整套持久化实现是死代码，生产从不使用

- **位置**：`pkg/proxy/containers/xray/inbound_adapter.go:741` · 维度：架构 · 单元：XC
- **问题**：RealityKeyStore（LoadOrGenerateRealityKey/GetRealityPublicKey/saveKeys 等，inbound_adapter.go:741-858）设计为按 tag 持久化 Reality 密钥对到文件，但 grep 全仓库（排除 test 与本文件）0 命中，生产路径里 buildStreamSettings 是在需要时 inline 调 GenerateRealityKeyPairWithPublic 生成、并把 private key 塞进 native JSON/extra 持久化（:587-592、extractNativeExtra:827-833）。因此 RealityKeyStore 既未被接线、其 saveKeys 用 0600 写文件且无文件锁的实现也从未生效。属于误导性死代码，建议删除或真正接入（当前 Reality 私钥其实随 native JSON 落 SQLite，不经此 store）。
- **证据**：inbound_adapter.go:741-858 RealityKeyStore 全套；grep RealityKeyStore/LoadOrGenerateRealityKey 仅命中定义与 *_test.go；实际密钥生成在 buildStreamSettings:587-592。
- **核实**：核实属实，且比 finding 描述更彻底——全仓 grep 显示 RealityKeyStore/NewRealityKeyStore/LoadOrGenerateRealityKey 只在 inbound_adapter.go 自身定义内被引用,连 *_test.go 都没有调用者。生产 Reality 私钥确实在 buildStreamSettings:588 inline 用 GenerateRealityKeyPairWithPublic 生成并随 native JSON 落库,不经此 store。saveKeys(:847)用 0600 写文件且无文件锁但从不生效。确为误导性死代码,P3 合理。

### 37. [P3·confirmed] 错误分类依赖上游/自身错误字符串前缀匹配，脆弱且易回归

- **位置**：`pkg/proxy/containers/xray/subscription.go:20` · 维度：错误处理 · 单元：XC · ⚠️协议相关（需人工核对上游）
- **问题**：isUserNotFoundError 用 strings.HasPrefix(errMsg, "user ") + Contains("not found") + Contains("inbound") 来区分“用户不在此 inbound”与其它致命错误（subscription.go:20-28），任何对 GetSub 报错文案的措辞调整都会改变 GetUserSubscriptions 的 fail-fast/skip 行为。类似地 AddInbound 用 strings.Contains(err.Error(), "existing tag found") 判定 xray 的重复 tag（exec_runner.go:1968），重试判定用 "connection error"/" UNAVAILABLE" 等子串匹配（grpc_client.go:222-226）。这些都把控制流绑定在 xray/自身的非结构化错误文本上，升级 xray 或改文案即静默回归。
- **证据**：subscription.go:27 `return strings.HasPrefix(errMsg, "user ") && strings.Contains(errMsg, "not found") && strings.Contains(errMsg, "inbound")`;exec_runner.go:1968 `strings.Contains(err.Error(), "existing tag found")`;grpc_client.go:222-226 子串重试判定。
- **核实**：代码事实全部核实:subscription.go:27 用 HasPrefix("user ")+Contains("not found")+Contains("inbound") 判定;exec_runner.go:1968 Contains("existing tag found");grpc_client.go:222-226 用 "connection error"/" UNAVAILABLE" 等子串判可重试。控制流确实绑定在非结构化错误文本上。此条虽标 protocolRelated,但断言的是本仓代码依赖字符串匹配这一可直接观察的事实,并非对 xray 字段/解析行为的记忆断言,故可 confirmed。P3 妥当(健壮性/回归风险,非当前 bug)。

### 38. [P3·confirmed] Shadowsocks 参数校验把 reality 列为合法 security,SS+reality 非法组合未被拦截

- **位置**：`pkg/proxy/containers/xray/profilegen/shadowsocks.go:88` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游） · 旧 review: SS-01
- **问题**：GenerateShadowsocksParams.Validate 允许 security 取值 reality:`if p.Security != "" && p.Security != "none" && p.Security != "tls" && p.Security != "reality"` 才报错(shadowsocks.go:88-89)。但 Reality 在 xray-core 中只支持 VLESS(inbound_adapter.go:107 也做了 reality 仅限 VLESS 的校验),SS+reality 是无效组合。此处放行后,后续 adapter 才会因协议不符报错或生成无法工作的配置,校验层应直接拒绝以给出清晰错误。
- **证据**：shadowsocks.go:88 `if p.Security != "" && p.Security != "none" && p.Security != "tls" && p.Security != "reality" {` — reality 被当作合法值放行;对比 inbound_adapter.go:107 `if security == "reality" && spec.Protocol != contracts.ProtocolVLess`
- **核实**：代码核实:shadowsocks.go:88 的 Validate 把 reality 列为合法 security 放行;inbound_adapter.go:107 对 security==reality && Protocol!=VLess 才报错。故 SS+reality 在 profilegen 校验层通过、到 adapter 才被拒。'reality 仅限 VLESS'的上游约束由本仓 adapter 代码自证,满足 protocolRelated 的仓库佐证要求。但由于 adapter 会兜底拦截、不会把坏配置下发到 xray,净效果仅是错误定位偏晚,P2 偏高,建议降为 P3。

### 39. [P3·confirmed] Reality 订阅 SNI 用 math/rand 随机挑选,订阅内容非确定,重复拉取每次不同

- **位置**：`pkg/proxy/containers/xray/subscription.go:560` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游）
- **问题**：buildShareLinkParams 对 Reality 的 sni 参数从 reality_server_names 里随机取一个:`sni = names[rand.Intn(len(names))]`(subscription.go:560)。同一用户每次生成订阅会得到不同 SNI,导致:1) 订阅内容不稳定,客户端/上层若对订阅做 hash 比对或缓存会误判为"已变更";2) 输出不可复现,难以排障。而 generateVLESSURI 又已额外把完整 serverNames 全量写入 URI(:267-269),随机 sni 是冗余的。建议改为确定性取首个,或不在 sni 里随机化。math/rand 全局源也非加密安全(此处仅影响可复现性,非安全)。
- **证据**：subscription.go:558-561 `if security == "reality" { if names := extStringSlice(spec.Extensions, "reality_server_names"); len(names) > 0 { sni = names[rand.Intn(len(names))] } }`;import "math/rand"(:7)
- **核实**：核实属实。buildShareLinkParams(subscription.go:477)内 security==reality 时 sni=names[rand.Intn(len(names))](~560,import math/rand),而 generateVLESSURI(246→247 调 buildShareLinkParams,又在 265-273 追加完整 serverNames=)会把随机 sni 与全量 serverNames 同时写进同一 URI。生成链路真实(generateURI→generateVLESSURI/generateTrojanURI 均调用),随机 sni 冗余且导致订阅输出每次不同。此条实质是输出可复现性问题,可在仓库内完全验证,不依赖上游解析。P3 妥当。

### 40. [P3·confirmed] SOCKS5 分享链接用 base64.StdEncoding 编码 userinfo,含 +/ / /= 时破坏 URL authority

- **位置**：`pkg/proxy/containers/xray/subscription.go:464` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游）
- **问题**：generateSOCKS5URI 把 `username:password` 用 `base64.StdEncoding.EncodeToString` 编码后放入 `socks://<b64>@host:port`(subscription.go:463-466)。StdEncoding 可能产出 `+`、`/`、`=`,其中 `/` 出现在 `@` 之前会被 URL 解析器视为 path 起点、authority 提前结束,链接被解析错位;`+`/`=` 也非 userinfo 安全字符。同文件 SS(:429)和 VMess(:382)都用 RawURLEncoding,SOCKS5 应保持一致改用 base64.RawURLEncoding。SOCKS5 在本系统非主用协议,故 P3。
- **证据**：subscription.go:464 `encodedUserinfo := base64.StdEncoding.EncodeToString([]byte(userinfo))`;:466 `fmt.Sprintf("socks://%s@%s:%d#%s", encodedUserinfo, ...)`
- **核实**：代码属实:subscription.go:464 用 base64.StdEncoding 编码 userinfo 后放入 socks://<b64>@host:port(466)。StdEncoding 字符集含 `/`,而 URL authority 在遇到 `/` 时提前结束,`@` 出现在 path 段会导致 authority 解析错位——这是 URL 语法的客观事实,不依赖具体客户端行为。同文件 SS(429)与 VMess(382)均用 RawURLEncoding,内部不一致可直接在仓库内印证。P3 合理(SOCKS5 非主用协议)。虽标 protocolRelated,但核心缺陷是 URL 结构破坏而非上游解析约定,故可 confirmed。

### 41. [P3·confirmed] AddInboundNative 从私钥派生 Reality 公钥失败时静默丢弃,订阅 URI 缺失 pbk 且无日志

- **位置**：`pkg/proxy/containers/xray/exec_runner.go:830` · 维度：错误处理 · 单元：XC · ⚠️协议相关（需人工核对上游）
- **问题**：extractNativeExtra 从 native JSON 的 realitySettings.privateKey 派生公钥:`if pubKey, err := DeriveRealityPublicKey(pk); err == nil { extra["reality_public_key"] = pubKey }`(exec_runner.go:830-832)。DeriveRealityPublicKey 只用 `base64.RawURLEncoding.DecodeString`(inbound_adapter.go:715)。若外部导入的 native 配置里 privateKey 采用标准 base64(带 padding)编码,解码失败,err != nil 分支被直接吞掉,extra 无 reality_public_key,后续 Reality 订阅 URI 的 pbk 缺失,客户端无法完成 Reality 握手,且全程无任何告警。xray 自身 x25519 通常输出 RawURL 编码故多数情况可用,但对标准 base64 私钥会静默失败——至少应记录日志。
- **证据**：exec_runner.go:830 `if pubKey, err := DeriveRealityPublicKey(pk); err == nil {` — err != nil 无 else/日志;inbound_adapter.go:715 `privateKeyBytes, err := base64.RawURLEncoding.DecodeString(privateKeyB64)`(仅 RawURL)
- **核实**：代码属实:exec_runner.go:830 `if pubKey, err := DeriveRealityPublicKey(pk); err == nil {` 无 else 分支、无日志,err!=nil 被静默吞掉;DeriveRealityPublicKey(inbound_adapter.go:715)仅 base64.RawURLEncoding.DecodeString,对标准 base64(含 padding)私钥会解码失败。核心缺陷是『派生失败静默丢弃、订阅 pbk 缺失且无任何告警』,属可在仓库内核实的代码质量问题,『至少应记日志』的建议不依赖上游编码约定。protocolRelated 部分(标准 base64 私钥在实践中是否出现)仅影响触发概率而非缺陷成立性,finding 已自陈 xray 多数输出 RawURL。P3 合理。

### 42. [P3·confirmed] generateRandomPassword 注释称 hex 实为 base64,输出含 +/ /= 与注释不符

- **位置**：`pkg/proxy/containers/xray/profilegen/trojan_tls.go:87` · 维度：正确性 · 单元：XC · 旧 review: T-01
- **问题**：generateRandomPassword 注释写明 "random 32-byte hex string"(trojan_tls.go:83),实现却是 `base64.StdEncoding.EncodeToString`(:87),输出可含 `+`、`/`、`=`。作为 Trojan 密码功能上可用(订阅侧 generateTrojanURI 对 password 做了 url.QueryEscape,subscription.go:400),但注释误导且字符集与建议不符,后续若有人假设其为 hex 会出错。属清理项。
- **证据**：trojan_tls.go:83 注释 `// generateRandomPassword generates a random Trojan password (random 32-byte hex string).`;:87 `return base64.StdEncoding.EncodeToString(b)`
- **核实**：代码属实且非协议相关:trojan_tls.go:83 注释 `random 32-byte hex string`,实现却是 base64.StdEncoding.EncodeToString(87),输出含 +//=,与 hex 描述不符。功能上密码可用(订阅侧 subscription.go:400 对 password 做了 url.QueryEscape),纯注释/字符集误导的清理项,P3 合理。

### 43. [P3·confirmed] user-event handler goroutine 永不退出：Close() 从不关闭 userEventCh，与"clean shutdown"注释矛盾

- **位置**：`pkg/proxy/containers/mihomo/container.go:815` · 维度：资源 · 单元：MC
- **问题**：startUserEventHandler 在 Start 时 spawn 一个 `for event := range c.userEventCh` 的 goroutine（container.go:814-818）。该 goroutine 的唯一退出条件是 userEventCh 被关闭。函数注释（container.go:798-801）明确声称"it exits when userEventCh is closed, which only happens via Close()"，而 Close()（container.go:261-281）只 close(forwardStopCh) 并 Unsubscribe，从头到尾没有 close(c.userEventCh)（grep 全文件确认无 `close(userEventCh)`）。因此即便调用 Close()，handler goroutine 仍永久阻塞在空且永不关闭的 channel 上泄漏。构造函数注释（container.go:232-233）还宣称 mihomo 相比 xray"提供了 clean shutdown path"，但实际只清理了 forwardUserEvents+订阅，handler goroutine 依旧泄漏，属于半吊子清理。生产中容器是长生命周期单例（manager.go 也不调用 Close），影响主要落在反复以 WithUserManager 构造/丢弃容器的测试及未来的优雅停机路径上。修复应在 Close() 中安全关闭 userEventCh（需先确保 forwardUserEvents 已停止不再发送，再 close），并订正注释。
- **证据**：container.go:815 `for event := range c.userEventCh {`（handler 唯一退出=channel close）；container.go:261-281 Close() 仅 `close(stopCh)`(=forwardStopCh) + Unsubscribe，无 close(userEventCh)；container.go:799 注释 `userEventCh is closed, which only happens via Close()` 与实现不符。
- **核实**：属实。全文件 close( 仅出现在 container.go:275(forwardStopCh)与 1032(reconcileStopCh),userEventCh 从未被 close;handler goroutine(815 `for event := range c.userEventCh`)唯一退出条件即 channel 关闭,Close()(261-281)只 close(forwardStopCh)+Unsubscribe,故 Close 后 handler 永久阻塞泄漏,与 799 行注释'exits when userEventCh is closed, which only happens via Close()'及构造注释 232-233 的'clean shutdown'直接矛盾。核实无误。但降为 P3:生产 manager.go 不调用 Close,容器为长生命周期单例,真实泄漏窗口只在测试中反复构造/Start/Close 时出现,外加文档不实;单 goroutine、进程级有界,P2 偏高。

### 44. [P3·confirmed] reconcileStopCh 无锁访问,与同一 run hook 中受 mu 保护的 userEventHandlerStarted 不一致,Start/Stop 重叠时数据竞争

- **位置**：`pkg/proxy/containers/mihomo/container.go:1006` · 维度：并发 · 单元：MC
- **问题**：结构体文档(35-43)明确 mu 只保护 runner/restClient/cachedVersion/userEventHandlerStarted。reconcileStopCh 在 run hook 内由 startReconcileLoop 读写(1006 `if c.reconcileStopCh != nil` / 1009 `c.reconcileStopCh = make(...)`),在 stop hook 内由 stopReconcileLoop 读写(1029 读、1032 close、1034 置 nil),全程不持任何锁;而紧挨着的兄弟字段 userEventHandlerStarted 在同一 run hook(806-812)里是用 mu 保护的——同函数内两个并发状态字段一个加锁一个不加锁,属于内部不一致。下层已确认 base.go(CT-05/CT-06)存在 Start 解锁后执行 runFunc、Stop(Running) 与 Start 重叠的竞态,一旦触发,run hook 的 startReconcileLoop 写 reconcileStopCh 会与 stop hook 的 stopReconcileLoop 读/close/nil 同一字段发生数据竞争(甚至可能 double-close/close-nil)。建议将 reconcileStopCh 纳入 mu 或独立锁。
- **证据**：container.go:1006-1009 startReconcileLoop 无锁读写 reconcileStopCh；container.go:1028-1035 stopReconcileLoop 无锁 close+nil；对照 container.go:806-812 userEventHandlerStarted 走 c.mu.Lock；下层 _lower_digest base.go:149-152/239-248 Start/Stop 竞态。
- **核实**：属实。startReconcileLoop(container.go:1006-1009 读+写 reconcileStopCh)与 stopReconcileLoop(1029/1032/1034 读/close/nil)全程无锁,而同一 run hook 的兄弟字段 userEventHandlerStarted(806-812)受 c.mu 保护,结构注释(34-43/78)也只承诺 mu 保护 userEventHandlerStarted —— 内部不一致坐实。竞态路径亦成立:base.go Start 在执行 runFunc 前解锁(178-179),Stop(Running)将状态置 Stopping 并在锁外跑 stopFunc→stopReconcileLoop(249-260),此时并发 Start 命中 Stopping 分支(153-157)置 Starting 并跑 runFunc→startReconcileLoop,两者对 reconcileStopCh 无锁 read/make 与 close/nil 并发,可致 double-close/close-nil。触发窄(同容器 Stop+Start 重叠),P3 合理。

### 45. [P3·confirmed] FastAddInbound 与同 tag RemoveInboundConfig 竞态可留下永不回收的孤儿转发规则,与注释声称的清理保证矛盾

- **位置**：`pkg/proxy/containers/mihomo/container.go:955` · 维度：并发 · 单元：MC
- **问题**：reconcileUsersForInbound 在 947 行释放 inboundsMu 后,才在 951-955 的锁外循环里对现存用户调用 inb.AddUser→GetBindPort 分配转发端口。RemoveInboundConfig 持 inboundsMu 执行 ReleaseAllUserPorts(tag)(553)→push→删除 map(574)。若时序为:reconcileUsersForInbound 取到 inb 指针并放锁 →RemoveInboundConfig 抢锁跑完 ReleaseInboundPorts(tag)+删 map→reconcileUsersForInbound 继续对这个已被删除的 inb 调 AddUser,则会为一个已不在 map 中的 tag 新建转发规则/PortMapping。此后 reconcileUsers 只遍历 snapshotInbounds()(当前 map,911),已删 tag 不再被遍历,ReleaseInboundPorts 又保留 PortMappings(注释 700-701),故这些孤儿规则无人清理直到进程重启。FastAddInbound 注释(476-482)声称“任何来自竞态 RemoveInboundConfig 的孤儿转发规则会被该 Remove 的 ReleaseAllUserPorts 清掉,周期 reconcile 收敛”——当 AddUser 发生在 ReleaseInboundPorts 之后时该保证不成立。触发条件窄(对同一 tag 并发 add+remove),故定 P3。
- **证据**：container.go:945-947 取 inb 后放锁；container.go:951-955 锁外 AddUser；container.go:553 RemoveInboundConfig 的 ReleaseAllUserPorts；container.go:911 reconcileUsers 仅遍历当前 map;注释 container.go:476-482 与 700-701。
- **核实**：属实。reconcileUsersForInbound 在 947 释放 inboundsMu 后于 951-955 锁外对 inb 调 AddUser(→GetBindPort 分配 PortMapping);RemoveInboundConfig(542-574)持锁跑 ReleaseAllUserPorts(→ReleaseInboundPorts,553)+删 map(574)。若时序为:reconcileUsersForInbound 取指针放锁→Remove 抢锁跑完 release+删 map→reconcileUsersForInbound 继续 AddUser,则为已删 tag 新建转发 PortMapping;而 reconcileUsers 仅遍历 snapshotInbounds()(当前 map,911)、ReleaseInboundPorts 又刻意保留 PortMappings(inbound.go:700-701 注释),故孤儿规则无人回收至进程重启。FastAddInbound 注释(476-482)声称 Remove 的 ReleaseAllUserPorts 会清掉孤儿,在 AddUser 发生于 release 之后时不成立。触发窄(同 tag 并发 add+remove),P3 合理。

### 46. [P3·confirmed] gunzip 解压对 io.Copy 无大小上限:更新/自动下载路径存在 gzip 解压炸弹风险(stable 无 checksum 校验)

- **位置**：`pkg/proxy/containers/mihomo/downloader.go:104` · 维度：资源 · 单元：MC
- **问题**：gunzipFile(85-106)用裸 `io.Copy(w, gzr)`(104)把解压内容全量写盘,无 io.LimitReader、不校验解压后大小;updater.go 的 gunzipToTemp(430-447)复用它作为生产解压路径。虽然 Alpha 分支在解压前有 SHA256 校验(updater.go:249-263),但 stable 分支“当前不发布 checksums.txt”(updater.go:264-269)时直接跳过校验,解压来自 GitHub release 资产的 .gz 无任何大小防护,恶意/损坏的高压缩比 gz 可耗尽 DataDir 磁盘。与下层 xray 已记录的同类问题(X-13, updater zip 无上限,P3)同型。建议对 io.Copy 包一层 io.LimitReader 或基于预期二进制体量设上限。
- **证据**：downloader.go:104 `_, err = io.Copy(w, gzr)` 无 LimitReader；updater.go:438 `gunzipFile(src, tmpPath)` 复用;updater.go:264-269 stable 跳过 checksum 校验分支。
- **核实**：属实。gunzipFile(downloader.go:85-106)用裸 io.Copy(w, gzr)(104)全量写盘,无 io.LimitReader/无解压后大小上限;updater.go gunzipToTemp(430-438)在生产更新路径复用 gunzipFile。校验仅在存在 checksums.txt 时进行(updater.go:249-263),stable 分支无 checksums.txt 时(264-269)直接跳过 SHA256,故来自 GitHub release 资产的 .gz 在 stable 下无任何大小防护,高压缩比恶意/损坏 gz 可耗尽 DataDir。与下层 xray X-13 同型,P3 合理(源为 GitHub 官方发布、影响为磁盘耗尽,属防御性加固)。

### 47. [P3·confirmed] 测试缺口：无用例验证 Close() 能终止 user-event handler goroutine（本可捕获泄漏）

- **位置**：`pkg/proxy/containers/mihomo/container_test.go:985` · 维度：测试缺口 · 单元：MC
- **问题**：TestUserEventPipeline_EndToEnd 在 container_test.go:985 直接 `c.startUserEventHandler()` 拉起 handler goroutine，但整套测试没有任何用例断言 Close() 之后 forwardUserEvents 与 handler goroutine 都被回收（无 goleak / runtime.NumGoroutine 前后对比，grep 无 goleak）。正因为缺这个断言，前述 userEventCh 永不关闭导致的 handler goroutine 泄漏（container.go:815）才一直没被测出——该测试自身 spawn 的 handler 在用例结束后也是泄漏状态。建议补一个『构造→Close→断言相关 goroutine 已退出』的用例，作为 clean-shutdown 契约的回归护栏。
- **证据**：container_test.go:985 spawn handler 但无终止/泄漏断言；grep goleak/NumGoroutine 在 mihomo 包内无命中；对应生产缺陷 container.go:815 + Close() 未 close(userEventCh)。
- **核实**：确认底层生产缺陷真实:Close()(261-281 行)只 close forwardStopCh 并 Unsubscribe,从不 close(userEventCh),而 handler goroutine 在 815 行 `for range c.userEventCh` 只能靠 channel 关闭退出——注释(799 行)声称『userEventCh 由 Close() 关闭』与代码不符,handler 永久泄漏。测试侧:container_test.go:985 直接 startUserEventHandler() 拉起 handler 却无任何终止/泄漏断言;全包 grep goleak/NumGoroutine 命中数为 0,无用例调 container.Close() 后断言 goroutine 回收(仅有的 .Close() 是 sm/srv)。测试缺口与所指生产缺陷均属实,P3 合理。

### 48. [P3·confirmed] hysteria.yaml 以 0644 落盘,内含 trafficStats secret 被本机任意用户可读

- **位置**：`pkg/proxy/containers/hysteria/process.go:147` · 维度：安全 · 单元：HC
- **问题**：generateConfigFile 用 os.WriteFile(...,0644)(process.go:147) 写配置,文件内嵌 TrafficStats.Secret(process.go:135,即 QueryTraffic/KickUser 的 Authorization 凭据)。0644 全局可读,同机任意非特权用户/进程可读取该 secret 后直接访问 127.0.0.1:TrafficStatsPort 的 /traffic(读取并可 clear 清零全部用户流量统计)与 /kick(踢下任意用户),破坏计费与可用性。含机密的配置文件应 0600。
- **证据**：process.go:135 `Secret: hc.cfg.TrafficStatsSecret`;:147 `os.WriteFile(hc.cfg.ConfigFilePath, data, 0644)`;traffic.go:29/:75 `req.Header.Set("Authorization", cfg.TrafficStatsSecret)`。
- **核实**：核实 process.go:147 os.WriteFile(...,0644) 属实,配置内嵌 TrafficStats.Secret(:135),而 traffic.go:29/:75 该 secret 即 /traffic(可 ?clear=1 清零)与 /kick 的 Authorization 凭据。TrafficStats 虽 listen 127.0.0.1(:134),但 0644 全局可读使同机任意本地用户可读取 secret 后经 loopback 调用这两个端点,破坏计费与可用性。含机密配置应 0600。触发路径真实,P3 合理。

### 49. [P3·confirmed] httpPort 未校验,未配置 HTTPPort 时鉴权回调 URL 端口为 0,全部用户鉴权失败

- **位置**：`pkg/proxy/containers/hysteria/process.go:130` · 维度：正确性 · 单元：HC · 旧 review: H-06
- **问题**：generateConfigFile 在 process.go:130 直接拼 `http://127.0.0.1:%d/api/authHysteria2`,httpPort 唯一赋值来自 WithHTTPPort,而 register.go:61 仅在 `opts.HTTPPort != 0` 时才注入。若节点未配置 HTTP 端口,httpPort 保持 0,生成 auth.http.url=http://127.0.0.1:0/... 无效,hysteria 每次用户握手回调该地址都失败,导致所有 hysteria2 用户无法鉴权连接,且无任何启动期校验或告警(对比 register.go 对 Domain 的 fail-fast)。
- **证据**：process.go:130 `URL: fmt.Sprintf("http://127.0.0.1:%d/api/authHysteria2", hc.httpPort)`;register.go:61 `if opts.HTTPPort != 0`;无处校验 httpPort!=0。
- **核实**：核实 httpPort 唯一赋值来自 WithHTTPPort(container.go:126-128),register.go:61 仅在 opts.HTTPPort!=0 时注入;HTTPPort 来自 cmd/server.go:172 cfg.EndNode.HttpPort,该字段(appconfig/config.go:168)无默认值也无任何 HttpPort==0 校验(全仓 grep 无)。故未配置 HTTP 端口时 process.go:130 生成 http://127.0.0.1:0/... 无效回调,与 register.go 对 Domain 的 fail-fast 形成对比。缺口属实。需注意:HttpPort=0 同时会使整个 HTTP 管理服务(server.go:240 同一端口)失效,是更广的整体误配而非仅 hysteria,故 P3 及影响面框定合理,不上调。

### 50. [P3·confirmed] 二进制下载硬编码 linux-amd64,arm64 节点下载错误架构二进制无法运行

- **位置**：`pkg/proxy/containers/hysteria/downloader.go:15` · 维度：架构 · 单元：HC · ⚠️协议相关（需人工核对上游）
- **问题**：downloadHysteria(downloader.go:15) 硬编码 `hysteria-linux-amd64` release 资产名,不区分 runtime.GOARCH。在 arm64 等非 amd64 节点上会下载 amd64 二进制,chmod 后 exec 直接失败(Exec format error),而失败只在后台 startProcess 中记日志(见 H-05),表现为容器 Running 但进程反复起不来。应按目标架构选择资产名(参考 xray 容器对 GOAMD64/架构的处理)。依赖 apernet/hysteria release 资产命名约定,建议核对上游资产列表。
- **证据**：downloader.go:15 `.../download/%s/hysteria-linux-amd64`,无 runtime.GOARCH 分支。
- **核实**：downloader.go:15 硬编码 hysteria-linux-amd64,无 runtime.GOARCH 分支也无 mihomo 式 arch 守卫,核实属实。protocolRelated 但由仓内材料佐证:同仓 snell/downloader.go:15-25 明确支持 arm64→aarch64 并对未知 arch 报错,证明本项目确有 arm64/aarch64 目标节点;mihomo/updater.go:192 则以显式错误守卫非 amd64。hysteria 二者皆无 → arm64 节点静默下载 amd64 二进制、exec 失败。核心缺陷(无 arch 分支/守卫)为代码事实,不依赖上游确切资产名(那只是修复细节),故可 confirmed。

### 51. [P3·confirmed] restoreInboundConfig 反序列化失败静默吞掉,port 依赖 float64 断言易随存储格式变化失效

- **位置**：`pkg/proxy/containers/hysteria/container.go:620` · 维度：错误处理 · 单元：HC
- **问题**：restoreInboundConfig 在 json.Unmarshal 失败时仅 slog.Warn 后 `return nil`(container.go:620-623),把损坏/格式变更的持久化记录当作无记录处理,静默丢失 enabled 状态并回退到默认 inboundEnabled=true——一个此前被 RemoveInboundConfig 禁用的 inbound 会在重启后悄悄重新启用并重建全部转发端口。同时 port 恢复依赖 `data["port"].(float64)`(container.go:628),若上游改用整型/字符串序列化 port,断言失败则 Port 保持 0,后续 inbound 端口与配置生成全部错位,同样无错误上报。
- **证据**：container.go:620-623 `if err := json.Unmarshal(...); err != nil { slog.Warn(...); return nil }`;:628 `if port, ok := data["port"].(float64); ok`;:644 enabled 缺失时默认 true。
- **核实**：核实 container.go:620-623 json.Unmarshal 失败仅 slog.Warn 后 return nil,静默把损坏/格式变更记录当作无记录;inboundEnabled 在 NewHysteriaContainer(:137)初始化为 true,RemoveInboundConfig(:364)置 false 并经 :584 持久化 enabled 字段。故此前被禁用的 inbound 在记录反序列化失败时回退默认 true(:644 缺 key 不覆盖),重启后悄然重新启用并重建转发端口。port 依赖 data["port"].(float64)(:628,JSON 数字入 map[string]any 恒为 float64,当前正确,但格式变更即静默归 0)。机制成立,P3 合理。

### 52. [P3·confirmed] GetUserSubscriptions 未做 sc.userMgr nil 检查，userMgr 缺失时订阅请求 panic

- **位置**：`pkg/proxy/containers/snell/container.go:557` · 维度：错误处理 · 单元：SC · 旧 review: SN-03
- **问题**：GetUserSubscriptions(container.go:550) 在 557 行直接 `sc.userMgr.GetUserPortByDst(req.User.Username, uint32(sc.cfg.Port))`，没有 `if sc.userMgr == nil` 守卫。同文件其他触达 userMgr 的方法都做了守卫（releaseAllForwardRules :281、reconcileUsers :594、startReconcileLoop :665），唯独订阅生成路径没有。若容器通过 NewSnellContainer 而未注入 WithUserManager（工厂 register.go:29-31 仅在 opts.UserManager!=nil 时注入），订阅请求会 nil 解引用 panic。属防御一致性缺陷且是外部可触发路径（/sub HTTP 请求）。
- **证据**：container.go:557 `port, ok := sc.userMgr.GetUserPortByDst(...)` 无 nil 守卫；对照 container.go:281/594/665 均有 `if sc.userMgr == nil` 守卫
- **核实**：代码事实属实:GetUserSubscriptions 557 行 `sc.userMgr.GetUserPortByDst(...)` 无 nil 守卫,而 281/594/665 三处同类方法均有 `if sc.userMgr == nil` 守卫,防御一致性缺陷确实存在。但 finding 声称的"外部可触发 panic"在生产不可达:唯一生产构造点 cmd/server.go:169-174 恒注入 `UserManager: userMgr`,工厂 register.go:29-31 随之注入 WithUserManager,故生产路径 userMgr 恒非 nil,/sub 请求不会 panic。即防御缺陷成立、但实际不可触发,严重度过高,下调至 P3。

### 53. [P3·confirmed] snell.conf 以 0644 写入且含明文 PSK，本机任意用户可读取共享 PSK

- **位置**：`pkg/proxy/containers/snell/container.go:257` · 维度：安全 · 单元：SC
- **问题**：generateConfigFile 用 `os.WriteFile(sc.cfg.ConfigFilePath, ..., 0644)`(container.go:257) 写出含明文 `psk = <PSK>` 的 INI（默认 /etc/v2raymg/snell.conf）。0644 意味着同机任何本地用户可读到这把所有 snell 用户共享的 PSK，从而完全解密/接入 snell 代理。同一 PSK 还明文落在 store 的 NativeJSON(saveInboundConfig:481) 与订阅 URI。含单一共享密钥的配置文件建议 0600。属本机权限边界的信息泄露。
- **证据**：container.go:251-257 `content := fmt.Sprintf("[snell-server]\n...psk = %s...", ..., sc.psk)` 后 `os.WriteFile(..., 0644)`；saveInboundConfig container.go:481 `"psk": sc.psk` 明文入 store
- **核实**：属实。generateConfigFile 用 fmt.Sprintf 写入含明文 `psk = %s`(container.go:251-252) 后 os.WriteFile(..., 0644)(container.go:257)，同机任意本地用户可读。PSK 为整个容器单一共享密钥（GetUserSubscriptions:571/575 所有用户共用 sc.psk），且明文同样落入 store NativeJSON(saveInboundConfig:480)。属本机权限边界的信息泄露，0600 建议合理，非协议相关，P3 恰当。

### 54. [P3·confirmed] handleUserEvent 的 Add 分支不校验 IsUserVisible，为非本节点可见用户临时分配转发端口

- **位置**：`pkg/proxy/containers/snell/container.go:388` · 维度：正确性 · 单元：SC
- **问题**：UserEventAdd 分支(container.go:388-409)只检查 inboundEnabled 与 addedUsers，未调用 sc.userMgr.IsUserVisible(event.User) 就 GetBindPort 建转发规则；而同函数的 UserEventUpdate 分支(428)以及 reconcileUsers 的移除逻辑都以可见性为准。集群同步场景下，别的节点新增的、不属于本节点分组的用户也会触发本地 Add 事件，snell 会为其分配外部转发端口并 track，直到下一次 reconcile(≤30s，reconcileUsers 移除不在 visibleSet 中的 tracked 用户 616-632)才回收。这给了不该在本节点落地的用户最长 30s 的可用转发入口。建议 Add 分支与 Update 分支一致，先 IsUserVisible 再分配。
- **证据**：container.go:388-402 Add 分支无 IsUserVisible 检查直接 GetBindPort；对照 container.go:428 Update 分支 `!sc.userMgr.IsUserVisible(event.User)` 与 reconcileUsers 616-632 的可见性回收
- **核实**：属实且触发路径真实。handleUserEvent 的 UserEventAdd 分支(container.go:388-409)仅检查 inboundEnabled 与 addedUsers，未调 IsUserVisible 即 GetBindPort；而 UserEventUpdate 分支(428)与 reconcileUsers 回收逻辑(616-632, 依赖 ListUsers→isUserVisibleLocked 过滤)都以可见性为准。已核实集群同步路径 SyncUpsertUser 对新远端用户无条件 emitEvent(UserEventAdd)(usermanager.go:1581-1585)，isUserVisibleLocked(usermanager.go:288-298) 会因分组不匹配返回 false，故非本节点分组用户确会触发本地 Add 并分配转发端口，直到下一次 reconcile(≤30s)才回收。P3 合理。

### 55. [P3·confirmed] Update 在 Start 前即改写 sc.cfg.Version，回滚路径不还原，且版本变更不持久化

- **位置**：`pkg/proxy/containers/snell/container.go:225` · 维度：正确性 · 单元：SC
- **问题**：Update 在 215 行 rename 就位后、Start 之前就把 `sc.cfg.Version = targetVersion`(225)。若随后 Start 失败走回滚(228-235)，只把旧二进制 rename 回去并重启，却不把 sc.cfg.Version 还原为 fromVersion——于是运行的是旧二进制、Version() 却报告新版本，状态不一致。此外成功路径的版本变更只存在内存：saveInboundConfig 不写 version，restoreInboundConfig 也只恢复 port/psk/enabled(523-533)，进程重启后 Version 回落到 Decode 默认 v5.0.1，若此时二进制恰好缺失会按默认版本重新下载造成降级。建议：Version 仅在 Start 成功后提交、回滚时还原，并纳入 saveInboundConfig 持久化。
- **证据**：container.go:225 `sc.cfg.Version = targetVersion` 位于 Start(228) 之前；回滚块 228-235 无 `sc.cfg.Version = fromVersion`；saveInboundConfig(475-482) 的 data map 无 version 字段
- **核实**：属实。Update 在 Start 前即 `sc.cfg.Version = targetVersion`(container.go:225)，回滚块(228-235)只 rename 旧二进制并重启，未还原 sc.cfg.Version=fromVersion，导致跑旧二进制而 Version() 报新版本，状态不一致。且 saveInboundConfig 的 data map(475-482)无 version 字段，restoreInboundConfig(523-533)只恢复 port/psk/enabled，更新后的版本既不写 config 也不写 store，重启即丢失。非协议相关，P3 恰当。

### 56. [P3·confirmed] FastAddInbound 对非默认 tag 返回裸 fmt.Errorf，未走统一错误码体系

- **位置**：`pkg/proxy/containers/snell/container.go:338` · 维度：错误处理 · 单元：SC · 旧 review: H-03
- **问题**：FastAddInbound 对 tag != defaultInboundTag 直接 `return fmt.Errorf("snell: only the default inbound %q is supported", ...)`(container.go:338)，RemoveInboundConfig/GetInboundConfig 等同类不支持路径也用裸 fmt.Errorf(267/311/317)。这些错误不携带 proxy errors 的 ErrorCode，调用方无法通过 errors.Is/HasCode 归类处理（与下层 errors 体系设计不一致）。属接口一致性清理项。
- **证据**：container.go:338 `return fmt.Errorf("snell: only the default inbound %q is supported", defaultInboundTag)`；同类 267/311/317 均裸 fmt.Errorf
- **核实**：代码事实属实：container.go:338 及 267/311/317 均用裸 fmt.Errorf，不携带 pkg/proxy/errors 的 ErrorCode（该体系确实存在并定义了 ErrInboundNotFound，支持 HasCode/Code 归类）。调用方无法用 errors.Is/HasCode 归类。需注意这是容器层普遍写法——mihomo 容器同样用裸 `fmt.Errorf("mihomo: inbound %q not found", tag)`(container.go:548/604)，故属跨模块清理项而非 snell 独有缺陷，P3 合理。

### 57. [P3·confirmed] restoreInboundConfig 无条件用 store 记录覆盖 Decode 生成的 port

- **位置**：`pkg/proxy/containers/snell/container.go:523` · 维度：正确性 · 单元：SC · 旧 review: SN-02
- **问题**：restoreInboundConfig 在 523-524 `if port, ok := data["port"].(float64); ok { sc.cfg.Port = int(port) }` 无条件用持久化记录覆盖 port，没有『仅当 cfg.Port 为默认值时才恢复』的保护。当运维在 config 中显式改了新 port 但 store 里还留着旧 port 时，重启后旧 port 静默胜出，配置文件与订阅端口回退到旧值，容易造成『改了配置不生效』的困惑。psk 同理(526-529)。listen 部分因已硬编码 127.0.0.1 而不再是问题。属持久化优先级的设计一致性问题，影响较低。
- **证据**：container.go:523-524 无守卫地 `sc.cfg.Port = int(port)`；526-529 同样覆盖 psk
- **核实**：属实。restoreInboundConfig 在 container.go:523-524 无守卫地 `sc.cfg.Port = int(port)`、526-529 无守卫覆盖 psk，且该函数在 GetRunFunc 中先于 saveInboundConfig 执行，故 store 记录会静默覆盖 Decode/Init 从 config 解析出的新值，出现『改了配置不生效』。listen 已硬编码 127.0.0.1 不受影响。属持久化优先级的设计一致性问题，影响较低，P3 恰当。

### 58. [P3·uncertain] VMess 分享链接用 base64.RawURLEncoding,与主流客户端使用的标准 base64 存在互通风险

- **位置**：`pkg/proxy/containers/xray/subscription.go:382` · 维度：正确性 · 单元：XC · ⚠️协议相关（需人工核对上游）
- **问题**：generateVMessURI 用 `base64.RawURLEncoding.EncodeToString` 编码 vmess JSON(subscription.go:382)。de-facto 的 vmess:// 分享链接(v2rayN/NG 等)普遍采用标准 base64(StdEncoding,含 +/ 与 padding)。RawURL 变体会输出 `-`/`_` 且无 padding,只用 StdEncoding 解码的旧客户端可能无法解析。当前实现有测试固化此选择(subscription_test.go:471 用 RawURLEncoding 解),属有意设计,但与生态默认不一致,需确认目标客户端集合是否都兼容 URL-safe base64。标 protocolRelated 且不确定,供人工复核决定是否切回 StdEncoding。
- **证据**：subscription.go:382 `return fmt.Sprintf("vmess://%s", base64.RawURLEncoding.EncodeToString(b)), nil`;测试 subscription_test.go:471 `base64.RawURLEncoding.DecodeString(encoded)`
- **核实**：代码事实属实:382 用 base64.RawURLEncoding,测试 471 也用 RawURLEncoding 解码,属有意设计。但缺陷主张是『主流 vmess:// 客户端普遍用 StdEncoding、RawURL 变体旧客户端无法解析』——这是纯客户端解析行为断言,protocolRelated=true 且仓库内无 wiki/docs/config 证据佐证目标客户端集合的 base64 兼容性。按对抗规则不得凭记忆确认上游行为;finding 本身也自陈不确定、请求人工复核。故最高 uncertain。

### 59. [P3·uncertain] vmess+reality 订阅 URI 丢失 reality 参数，仅 Clash 转换路径可用，裸 vmess:// 客户端拿到的是不可用配置

- **位置**：`pkg/proxy/containers/mihomo/subscription.go:394` · 维度：正确性 · 单元：MC · ⚠️协议相关（需人工核对上游）
- **问题**：支持矩阵（wiki details.md 表：vmess 支持 none/tls/reality）声明 vmess 可配 reality，profilegen 也会为 vmess listener 写 reality-config（profilegen.go:348-351）。但 fillVMessSubscriptionSpec 的 reality 分支（subscription.go:388-406）只把 reality_public_key/reality_short_ids/server_name 写进 spec.Extensions，未在 codec.VMessNode 上设置任何 reality 字段（VMessNode 无 reality 字段，代码注释 subscription.go:389-393 亦承认）。因此 spec.URI（vmess://base64-json）不携带 pbk/sid/security=reality。只有读取 Extensions 的 clash converter 能正确还原；任何直接消费 vmess:// URI 的通用客户端（NekoBox/Hiddify 等）拿到的是无 reality 信息的 vmess 节点，握手必然失败。相较之下 vless/trojan 的 reality 会写入各自 codec Node（subscription.go:239-263、492-508），vmess 是唯一缺口。属于协议实现层面的功能性缺陷，建议要么给 VMessNode 补 reality 字段并在 Encode 输出，要么在文档/校验层明确 vmess+reality 仅支持 Clash 订阅链路。
- **证据**：subscription.go:388-406 vmess reality 分支只写 spec.Extensions，无 node.* reality 赋值；subscription.go:389-393 注释 `The codec VMessNode has no reality fields, so the URI won't carry pbk/sid`；对比 subscription.go:497-503 trojan reality 设置 node.RealityPublicKey/RealityShortID。
- **核实**：protocolRelated,按规则最高 uncertain。代码事实成立且被自注释确认:vmess reality 分支(subscription.go:388-406)只写 spec.Extensions,不设 codec 节点字段(VMessNode 无 reality 字段),对比 trojan(497-508)设 node.RealityPublicKey/RealityShortID 进 URI —— 这一非对称属实。但'裸 vmess:// 客户端(NekoBox/Hiddify)因缺 pbk/sid 握手必失败'属上游/客户端解析行为,仓库材料无法确证:标准 vmess:// 分享格式(add/port/id/aid/net/tls/sni 的 base64-json)本就无 reality 字段,通用客户端是否支持 vmess+reality 本身存疑;且 wiki(index.md:121)显示项目对 hy2 up/down、tuic 客户端专用字段等本就采用'Extensions-only、不入 URI'的既定设计,vmess+reality 走 Clash 转换很可能是设计取舍而非缺陷。既不能凭记忆断言客户端行为,又无仓库材料证明这是功能性缺陷,故 uncertain。

### 60. [P3·uncertain] Reload 静默 no-op,证书轮换/配置变更后无法热加载

- **位置**：`pkg/proxy/containers/hysteria/container.go:272` · 维度：正确性 · 单元：HC · ⚠️协议相关（需人工核对上游） · 旧 review: H-07
- **问题**：Reload()(container.go:271-274) 直接 return nil,不做任何事。证书续期(certmgmt 轮换 cert/key 文件)或配置变更后,上层调用 Reload 期望 hysteria 重新读取证书,但实际进程仍持有旧证书直到手工 Restart。hysteria2 支持 SIGHUP 热重载配置(protocolRelated:需核对上游),此处既未发信号也未 Restart,证书续期后代理会继续用旧证书直至过期,与 CERT 层非原子续期问题叠加放大影响。
- **证据**：container.go:271-274 `// Reload is a no-op for hysteria.` / `func (hc *HysteriaContainer) Reload() error { return nil }`。
- **核实**：Reload no-op(container.go:271-274)为代码事实,但影响链未成立:(1) 全仓生产代码无任何 .Reload() 调用者(grep 仅命中 _test.go);(2) certmgmt/service/renew_scheduler.go:71-72 明确注释'文件原地覆盖,proxy cores 通过各自文件热重载拾取新证书,本处按设计不重启容器',直接否定了 finding 前提'上层调用 Reload 期望 hysteria 重读证书';(3) hysteria 是否随文件变更自动热重载证书、是否需 SIGHUP 属上游行为,protocolRelated 且仓内 wiki/docs 无实证。是否真为 bug 取决于无法从仓库核实的上游证书重载语义,故最高 uncertain。

### 61. [P3·uncertain] 订阅 URI 硬编码 version=5，与 cfg.Version 脱钩，配置为 snell v4 时订阅参数错误

- **位置**：`pkg/proxy/containers/snell/container.go:575` · 维度：正确性 · 单元：SC · ⚠️协议相关（需人工核对上游）
- **问题**：GetUserSubscriptions 拼 `snell://%s@%s:%d?version=5#%s`(container.go:575) 把协议版本参数写死为 5，而实际运行版本由 cfg.Version 决定（默认 v5.0.1，但 Decode 允许 config 覆盖成 v4.x，config.go:42-44）。若管理员把 snell 配成 v4 版本，snell-server 以 v4 协议监听，订阅却告诉客户端 version=5，导致客户端按 v5 协议握手与服务端不匹配。应从 cfg.Version 解析主版本号填入 URI，而非常量。注：默认版本为 5 时表现正常，故影响面取决于是否使用非 5 版本。snell URI 的 version 参数语义依赖 Surge/snell 上游约定，标记需复核。
- **证据**：container.go:575 URI 常量 `?version=5`；config.go:25 默认 `c.Version = "v5.0.1"` 且 config.go:42-44 允许 config 覆盖 version
- **核实**：代码事实属实：container.go:575 URI 硬编码 `?version=5`，而 config.go:42-44 允许 cfg.Version 被 config 覆盖，downloadSnellServer 又以 cfg.Version 下载对应二进制，故存在『URI 版本参数与实际运行版本脱钩』的代码不一致。但本条 protocolRelated=true，其危害（客户端按 version=5 握手与 v4 server 不匹配）取决于 snell/Surge 上游对 URI version 参数的解析语义，仓库内无材料证实——相反 docs/snell-container-design.md 明确把 version 当作固定 5（设计里 version int // 5、Surge 输出恒为 version=5），并未把 v4 列为支持配置。按核实规则，上游行为不能凭记忆断言，故最高 uncertain。

## 附录：verifier 判定 refuted（不计入，供追溯）

| 单元 | 位置 | 标题 | refuted 理由 |
|------|------|------|-------------|
| HC | `pkg/proxy/containers/hysteria/container.go:432` | FastAddInbound 非默认 tag 返回裸 fmt.Errorf,未走统一错误码体系 | 触发路径属实(container.go:361/405/432 确为裸 fmt.Errorf，行号准确)，但 finding 的核心论据「与其它容器不一致」是事实错误。核对 mihomo(container.go:548/604) 与 snell(container.go:267/311/338)，两者对 not-found/only-default 场景使用与 hysteria 完全相同的裸 fmt.Errorf 模式，全仓仅 xray 一个容器引用 pkg/proxy/errors。因此 hysteria 与直接可比的 mihomo、snell 完全一致，裸 error 是该容器家族的多数约定，不存在所谓不一致，也非 hysteria 特有缺陷。整改成 ErrInboundNotFound 至多是针对整个容器家族的广泛重构偏好，且 finding 的分类/gRPC 映射理由建立在错误前提上。附:detail 将 :411 归为 not-found 有误，:411 实为『inbound is disabled』，语义不同。 |
