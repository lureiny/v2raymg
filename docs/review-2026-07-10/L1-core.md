# L1 契约与持久层 review — store / contracts / params / inbound / container / subscription

> **本文件为第一轮结果。** 第二轮独立复审（换角度找新问题 + 挑战疑似误报）见 [`L1-pass2.md`](L1-pass2.md)：第二轮新增 49 条保留（其中 2 条 P1），并裁定第一轮 7 条为重复/过severity（多为同一 finding 记在相邻两行号、及个别 severity 偏高）。处理时请两份合看。
>
> Finder → 对抗性 verifier 两级流程产出。下表为**保留**（confirmed + uncertain）的 findings，已剔除 verifier 判定 refuted 的 0 条。uncertain 多为依赖第三方协议/上游行为、无法在仓库内确证的条目，处理前需人工核对。

## 统计

| 维度 | 数量 |
|------|------|
| 保留条目 | 57 |
| — confirmed | 53 |
| — uncertain | 4 |
| — 其它(unverified) | 0 |
| refuted(已剔除) | 0 |

| 优先级 | P0 | P1 | P2 | P3 |
|--------|----|----|----|----|
| 保留条目 | 0 | 5 | 20 | 32 |

## 速查表

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P1 | ✓ | 并发 | CON | `pkg/proxy/core/container/base.go:153` | Start 在 Stopping 状态不等待停止完成，叠加 MarkStopped 无条件置 Stopped，晚完成的 Stop 会把并发 Start 成功后的 Running 状态覆盖为 Stopped |
| 2 | P1 | ✓ | 并发 | CON | `pkg/proxy/core/container/base.go:192` | Start 成功路径不校验状态即缓存 stopFunc 并返回 nil：runFunc 期间被 Stop 打断时进程被孤儿化且 stopFunc 永不执行 (CT-06) |
| 3 | P1 | ✓ | 并发 | CON | `pkg/proxy/core/container/base.go:270` | Stop(Running) 与 Start(Stopping) 竞态：MarkStopped 无条件覆盖并发 Start 置的 Running，导致双进程与孤儿进程 |
| 4 | P1 | ✓ | 资源 | SUB | `pkg/proxy/core/subscription/converter/http.go:25` | converter 包内 httpGet 无超时、无响应体上限，fetchClashTemplate 可致 /sub 请求永久阻塞 (SUB-04) |
| 5 | P1 | ✓⚠️协议 | 架构 | SUB | `pkg/proxy/core/subscription/converter/clash.go:177` | Clash 订阅硬依赖 4 个第三方 sub-converter 公网服务，全挂即整体失败无降级（遗留） (SUB-02) |
| 6 | P2 | ✓ | 错误处理 | STO | `pkg/store/user_store.go:95` | port_mappings 读写两侧都静默吞错，一次解析失败会导致下次 Save 用 '{}' 永久覆盖端口映射数据 |
| 7 | P2 | ✓ | 正确性 | STO | `pkg/store/inbound_store.go:90` | prettyPrintJSON 经 interface{}/float64 往返重排并可能破坏 native_json 数值精度，应改用 json.Indent |
| 8 | P2 | ✓⚠️协议 | 安全 | STO | `pkg/store/manager.go:24` | SQLite 数据库文件含全部用户 auth_token 明文，但目录 0755 且未收紧文件权限，违背项目对机密文件 0600 的既有约定 |
| 9 | P2 | ✓ | 测试缺口 | STO | `pkg/store/inbound_store.go:1` | SQLiteInboundStore 完全没有测试；UserStore 的 Get/Delete/Count 与 expiry/port_mappings 往返也无覆盖 |
| 10 | P2 | ✓ | 并发 | STO | `pkg/store/tx.go:17` | WithTx 在 fn panic 时既不 Rollback 也不释放事务，单连接池下会卡死整个存储层 |
| 11 | P2 | ✓ | 正确性 | STO | `pkg/store/migrate.go:43` | Migrate 不检测数据库版本高于代码已知最大版本，旧二进制静默运行在新 schema 上 |
| 12 | P2 | ✓ | 正确性 | CTR | `pkg/proxy/core/inbound/default.go:64` | DefaultInbound 的 Setter 不同步 config_，Config() 与 getter 分叉 (I-01) |
| 13 | P2 | ✓ | 测试缺口 | CTR | `pkg/proxy/core/inbound/inbound.go:3` | core/inbound 包完全无 *_test.go（架构约束#9），且正是 SetTag 分叉 bug 的盲区 |
| 14 | P2 | ✓⚠️协议 | 安全 | CTR | `pkg/proxy/core/params/defaults.go:370` | crypto/rand 失败时静默返回可预测的时间种子“密码”，SS-2022 分支还会产出非法密钥 |
| 15 | P2 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/parser.go:153` | requireUint32 接受 port=0 并声称“adapter 会分配”，但无 adapter 实现该语义，下游 Validate 反而拒绝<100 |
| 16 | P2 | ✓ | 错误处理 | CON | `pkg/proxy/core/container/base.go:261` | Stop 中 stopFunc 失败仍标记为 Stopped，外部进程可能存活，下次 Start 直接造成双进程 |
| 17 | P2 | ✓ | 正确性 | CON | `pkg/proxy/core/container/interface.go:36` | Container 接口对 GetRunFunc 的文档（'Returns nil, nil if not implemented'）与 BaseContainer/Hooks 契约直接矛盾，照文档实现会 panic (CT-02) |
| 18 | P2 | ✓ | 架构 | CON | `pkg/proxy/core/container/subscription.go:7` | GetUserSubscriptions 双契约并存：接口强制方法与 SubscriptionProvider 可选接口重复，后者无任何真实消费方 (CT-03) |
| 19 | P2 | ✓ | 架构 | CON | `pkg/proxy/core/container/registry.go:22` | legacy 全局注册体系（RegisterContainer/GetContainer/SetContainer）已无生产消费方，成为纯死代码但仍在扩大误用面 (CT-09) |
| 20 | P2 | ✓ | 测试缺口 | CON | `pkg/proxy/core/container/registry.go:185` | 测试缺口：新 factoryMap 注册体系与 StartAll auto-Restore/错误累积路径均无测试，唯一并发测试无法覆盖竞态窗口 |
| 21 | P2 | ✓ | 错误处理 | SUB | `pkg/proxy/core/subscription/manager.go:59` | GetSubscription 对单 container 失败静默 continue，无日志，订阅缺协议难排查 (SUB-01) |
| 22 | P2 | ✓ | 安全 | SUB | `pkg/proxy/core/subscription/http.go:86` | ext_sub / *_url 参数导致服务端 SSRF，且外部内容原样反射给请求方 |
| 23 | P2 | ✓⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/converter/clash.go:792` | Clash 模板 proxy-group 经 ProxyGroup 结构体往返，丢失 filter/lazy/tolerance/icon 等字段 |
| 24 | P2 | ✓ | 错误处理 | SUB | `pkg/proxy/core/subscription/manager.go:57` | GetSubscription 逐 container 静默吞错，订阅缺协议无任何日志（遗留） (SUB-01) |
| 25 | P2 | ?⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/converter.go:155` | ParseRuleParam 丢弃第4段规则参数（如 no-resolve/src），Clash 规则语义被改写 |
| 26 | P3 | ✓ | 并发 | STO | `pkg/store/db.go:23` | SetMaxOpenConns(1) 与 WithTx 组合存在潜在自死锁模式：事务回调内任何经 db 直连的调用会无限期阻塞 |
| 27 | P3 | ✓⚠️协议 | 正确性 | STO | `pkg/store/user_store.go:134` | Save 用 INSERT OR REPLACE 更新用户时会把未列出的 created_at 重置为当前时间、level 重置为 0 |
| 28 | P3 | ✓⚠️协议 | 资源 | STO | `pkg/store/db.go:31` | busy_timeout/foreign_keys/cache_size PRAGMA 只施加在建库时的那条连接上，连接被池重建后静默失效 |
| 29 | P3 | ✓ | 架构 | STO | `pkg/store/user_store.go:67` | Load/Get/ListByGroup 三处手工复制 21 列 Scan + expiry 双格式解析 + port_mappings 解析，加列时必然三处同改 |
| 30 | P3 | ✓ | 架构 | STO | `pkg/store/migrations/migrations.go:87` | cluster_users 表（v7-v10 建表+三索引）在代码中已无任何读写方，成为可能残留旧凭据的死 schema |
| 31 | P3 | ✓ | 架构 | STO | `pkg/store/tx.go:19` | store 包仍用标准库 log 直写 stderr，与项目已统一到 pkg/log 的日志路由方向不一致 |
| 32 | P3 | ✓ | 架构 | STO | `pkg/store/provider.go:10` | Provider/SQLiteProvider 全仓库零消费者，架构约束 #7 所述的 Provider 抽象实际是死代码 |
| 33 | P3 | ✓ | 错误处理 | STO | `pkg/store/user_store.go:97` | pkg/store 使用 stdlib log.Printf，绕过项目统一的 pkg/log |
| 34 | P3 | ✓ | 架构 | STO | `pkg/store/user_store.go:45` | 21 列 SELECT+Scan+expiry 双格式解析+port_mappings 容错在 Load/Get/ListByGroup 三处手工复制 |
| 35 | P3 | ✓ | 错误处理 | CTR | `pkg/proxy/core/inbound/inbound.go:116` | InboundError 未实现 Is(target)，WithCause 派生实例无法被 errors.Is 匹配到 sentinel (I-04) |
| 36 | P3 | ✓ | 架构 | CTR | `pkg/proxy/core/contracts/protocol.go:138` | ValidateCombination 名不副实且为死代码：只逐字段校验合法性，从不校验“组合”，且无生产调用者 |
| 37 | P3 | ✓ | 安全 | CTR | `pkg/proxy/core/params/defaults.go:282` | generateSelfSigned 把未净化的 server_name 直接拼进磁盘文件名，存在路径穿越/写失败风险 |
| 38 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/defaults.go:389` | hashShort 文档声称 SHA-256/“SHA-like”，实际实现是 FNV-1a，注释自相矛盾 |
| 39 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/params_ss.go:42` | parseSS 手写 udp 布尔解析，与全包统一的 optionalBool 语义不一致 |
| 40 | P3 | ✓ | 并发 | CON | `pkg/proxy/core/container/manager.go:107` | StartAll/StopAll/RestoreAll 持 RLock 跨外部进程启动与状态恢复全程，阻塞窗口大且有再入死锁隐患 |
| 41 | P3 | ✓ | 架构 | CON | `pkg/proxy/core/container/registry.go:24` | legacy globalRegistry 已退化为只写死代码，唯一写入方是 xray init() 中带硬编码路径且失败即 panic 的默认 factory，应整体删除 (CT-09) |
| 42 | P3 | ✓ | 资源 | CON | `pkg/proxy/core/container/inbound.go:66` | InboundAdapter 全仓无生产调用方，且 inboundAdapterImpl.Config() 在读路径写共享 Extensions map（并发 map 写风险） |
| 43 | P3 | ✓ | 正确性 | CON | `pkg/proxy/core/container/types.go:11` | types.go 便捷常量列表缺 ContainerMihomo，与已注册的容器类型不同步 |
| 44 | P3 | ✓ | 架构 | CON | `pkg/proxy/core/container/process.go:14` | process.go 的 ProcessRunner 兼容别名与 inbound.go 的 Deprecated InboundConfig 系列全仓无引用，纯死代码 |
| 45 | P3 | ✓ | 测试缺口 | CON | `pkg/proxy/core/container/base_test.go:226` | BaseContainer 并发测试只断言最终状态，未校验 runFunc/stopFunc 配对与慢 hook 交错，无法捕获已知状态机竞态 |
| 46 | P3 | ✓ | 并发 | CON | `pkg/proxy/core/container/registry.go:63` | legacy registry 的 instances map 被两把不同锁分别保护，构成真实数据竞争而非仅锁风格问题 (CT-10) |
| 47 | P3 | ✓ | 架构 | CON | `pkg/proxy/core/container/factory.go:26` | BuildOptions.CertReader 是死注入点：全仓库无任何赋值，唯一消费分支永远不触发 |
| 48 | P3 | ✓ | 错误处理 | CON | `pkg/proxy/core/container/manager.go:45` | NewContainerMgr 用第一参数无条件覆盖 opts.StoreMgr，调用方在 opts 中设置的值会被静默清空 |
| 49 | P3 | ✓⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/converter/clash.go:729` | clearClashTemplateNoise 对整份模板做全局字符串替换，可能损坏合法规则/配置行 |
| 50 | P3 | ✓ | 错误处理 | SUB | `pkg/proxy/core/subscription/uri_convert.go:26` | ConvertURIs/ConvertURIsWithOptions 在 converter 为 nil 时返回空串且 error 为 nil |
| 51 | P3 | ✓⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/converter.go:154` | ParseRuleParam 丢弃第 4 段，Clash 规则的 no-resolve/额外参数被静默截断 |
| 52 | P3 | ✓⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/converter/surge.go:155` | Surge 与 Clash 的 Shadowsocks 默认加密方法不一致（缺 method 时结果分歧） |
| 53 | P3 | ✓ | 正确性 | SUB | `pkg/proxy/core/subscription/fake.go:29` | fake.go 每次调用重新以 time.Now().UnixNano() 播种 rand（遗留） (SUB-09) |
| 54 | P3 | ✓ | 测试缺口 | SUB | `pkg/proxy/core/subscription/converter/http.go:12` | 测试缺口：converter/http.go httpGet 与 ensureTemplateGroups 模板字段保真无覆盖 |
| 55 | P3 | ?⚠️协议 | 资源 | STO | `pkg/store/db.go:25` | busy_timeout/foreign_keys/cache_size PRAGMA 只作用于建池时的那一条连接，连接被替换后失效 |
| 56 | P3 | ?⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/uri_convert.go:118` | detectFormat 仅按 "clash" 子串匹配，mihomo/Stash 等 UA 会误落回 base64 通用格式 |
| 57 | P3 | ?⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/codec/shadowsocks.go:122` | ss:// 明文 method:password@host 形式（非 base64、非 2022）解码失败被丢弃 |

## 详细条目

### 1. [P1·confirmed] Start 在 Stopping 状态不等待停止完成，叠加 MarkStopped 无条件置 Stopped，晚完成的 Stop 会把并发 Start 成功后的 Running 状态覆盖为 Stopped

- **位置**：`pkg/proxy/core/container/base.go:153` · 维度：并发 · 单元：CON
- **问题**：BaseContainer.Start() 的 Stopping 分支（base.go:153-157）注释写 "Wait for stop to complete, then start fresh"，但实际不做任何等待：直接把状态从 Stopping 改成 Starting 并在解锁后执行 runFunc。而 Stop() 的 Running 分支（base.go:249-270）是先解锁再执行 stopFunc，因此存在交错：G1 Stop 正在执行 stopFunc（杀旧进程，耗时秒级），G2 Start 看到 Stopping 立即启动新进程并 MarkRunning 成功（state=Running）；随后 G1 的 stopFunc 返回，调用 MarkStopped()——而 MarkStopped（base.go:210-214）不检查当前状态、无条件 state=Stopped。最终结果：新外部进程在运行，但 State()=Stopped、IsRunning()=false，缓存的 stopFunc 永远不会被调用（Stop() 的 Stopped 分支 base.go:231-234 直接短路返回 nil）；任何后续 Start() 会再拉起第二个进程（端口冲突或双进程）。另外并发第二个 Stop 在 Stopping 状态也立即返回 nil（base.go:235-238），Restart()（base.go:288-299）因此可能在旧进程未死时就进入 Start。该路径在运行期真实可达：rpc SetGatewayModel（pkg/rpc/server/end_node_inbound.go:157-159）会在 gRPC handler 里对同一容器调 Stop/Start，可与 StopAll、Update 重启并发。修复方向：MarkStopped 应只允许 Stopping→Stopped 迁移，且 Stopping 期间的 Start/Stop 需等待（如 done channel）而非直接抢状态。
- **证据**：base.go:153-157 `case ContainerStateStopping: // Wait for stop to complete, then start fresh`（无等待，直接 b.state = ContainerStateStarting）；base.go:210-214 `func (b *BaseContainer) MarkStopped() { ... b.state = ContainerStateStopped }`（无条件覆盖）；base.go:256-270 Stop 解锁后执行 stopFunc 再 MarkStopped；调用侧 pkg/rpc/server/end_node_inbound.go:156-160 `_ = c.Stop()` / `_ = c.Start()`
- **核实**：代码证据确证：base.go:153-157 Stopping 分支确实不等待，直接置 Starting；MarkStopped（base.go:210-214）无条件 state=Stopped；Stop 的 Running 分支（base.go:249-270）解锁后执行 stopFunc 再 MarkStopped，交错时序成立——晚归的 stopFunc 会把并发 Start 已达成的 Running 覆盖为 Stopped，此后 Stop() 在 Stopped 分支短路（base.go:231-234），新进程失控；并发第二个 Stop 在 Stopping 立即返回 nil 使 Restart（base.go:288-299）可在旧进程未死时进入 Start。但需修正 finder 引用的触发路径：SetGatewayModel 走的 xray Executor 在 exec_runner.go:413/473 完全覆盖了 Start/Stop（直接调 Runner.Start/Stop），并不经过 BaseContainer 状态机，该路径不触发本竞态。真实可达路径是 mihomo/hysteria/snell（grep 确认三者均未覆盖 Start/Stop，接口调用落在 BaseContainer 上）以及 mihomo updater.go:292-296 的 processCtrl.Stop()/Start()（rpc UpdateProxy 触发），可与 cmd/server.go:275 的 StopAll、并发 Update 请求交错。缺陷真实且生产可达，维持 P1。

### 2. [P1·confirmed] Start 成功路径不校验状态即缓存 stopFunc 并返回 nil：runFunc 期间被 Stop 打断时进程被孤儿化且 stopFunc 永不执行

- **位置**：`pkg/proxy/core/container/base.go:192` · 维度：并发 · 单元：CON · 旧 review: CT-06
- **问题**：Start() 在解锁状态下执行 runFunc（base.go:179-188），期间并发 Stop() 走 Starting 分支（base.go:239-248）：置 Stopping、清空 b.stopFunc、close(stopChan)、立即 MarkStopped——此时 stopFunc 尚未被缓存（Start 在 runFunc 成功后才缓存，base.go:191-193），所以没有任何停止逻辑执行。随后 runFunc 成功返回，Start 的成功路径（base.go:190-195）不检查状态是否仍为 Starting，无条件 `b.stopFunc = stopFunc` 并返回 nil：调用者拿到 nil 认为启动成功，但 State()=Stopped（MarkRunning 因状态不是 Starting 而不生效），外部进程已被 runFunc 拉起且再也无人能停它——后续 Stop() 在 Stopped 分支短路，缓存的 stopFunc 成为死引用；后续 Start() 会再启动第二个进程。修复：runFunc 返回后需在锁内确认状态仍是 Starting，若已被 Stop 打断应立即执行 stopFunc 回滚并返回启动被取消的错误。
- **证据**：base.go:190-195 `// Success - save stop function and mark as running\n b.mu.Lock(); b.stopFunc = stopFunc; b.mu.Unlock(); b.MarkRunning(); return nil`（无状态校验）；base.go:239-248 Stop 的 Starting 分支清 stopFunc 后直接 MarkStopped；base.go:231-234 Stopped 分支短路使缓存的 stopFunc 永不执行
- **核实**：代码证据确证：Start 在解锁状态执行 runFunc（base.go:179-188）；并发 Stop 的 Starting 分支（base.go:239-248）置 Stopping、清 stopFunc（此时本就是 nil）、close(stopChan) 后立即 MarkStopped，无任何停止逻辑执行；runFunc 成功返回后 Start 成功路径（base.go:190-195）不校验状态，无条件缓存 stopFunc 并返回 nil，MarkRunning 因 state=Stopped≠Starting 不生效。结果：调用者收到 nil 但 State()=Stopped，进程已拉起且后续 Stop() 在 Stopped 分支短路、缓存的 stopFunc 永不执行，后续 Start 会拉起第二个进程。与旧 review CT-06 重复（CT-06："Stop 在 Starting 状态时立即 MarkStopped，但 runFunc 可能还在跑，进程成为孤儿"），finder 未标 legacyId；本条补充了 Start 仍返回 nil、stopFunc 死引用的细节。可达性同 idx 0：经 mihomo/hysteria/snell 的 BaseContainer.Start/Stop 及 mihomo updater 重启流程，xray 主路径因覆盖方法不受影响。

### 3. [P1·confirmed] Stop(Running) 与 Start(Stopping) 竞态：MarkStopped 无条件覆盖并发 Start 置的 Running，导致双进程与孤儿进程

- **位置**：`pkg/proxy/core/container/base.go:270` · 维度：并发 · 单元：CON
- **问题**：Stop() 的 Running 分支在 base.go:256 解锁后执行 stopFunc（杀外部进程，可能耗时数秒），执行完毕在 base.go:270 调 MarkStopped()。而 MarkStopped（base.go:210-214）是无条件 b.state = Stopped，不像 MarkRunning（base.go:200-206）只允许 Starting→Running。期间若有并发 Start()：它在 base.go:153-157 看到 Stopping 状态时注释写 'Wait for stop to complete' 但实际完全不等待——立即重建 stopChan、置 Starting、解锁执行 runFunc 拉起新外部进程，成功后缓存新 stopFunc（192）并 MarkRunning（194）。随后旧 Stop 的 MarkStopped 把 Running 强行覆盖为 Stopped。后果：(1) 新进程实际存活但状态为 Stopped，IsRunning 返回 false；(2) 后续再调 Start() 走 Stopped 分支会拉起第二个进程（端口冲突/进程泄漏），且 base.go:192 覆盖缓存的 stopFunc，第一个进程永远无法通过 Stop() 停止。该交错在 rpc/hotreload 触发的并发 Restart 场景下真实可达。与旧条目 CT-05（Start-during-Starting 不等待）、CT-06（Stop-during-Starting 孤儿 runFunc）是不同交错，未被旧清单覆盖。修复方向：MarkStopped 应与 MarkRunning 对称（仅 Stopping→Stopped），且 Start 在 Stopping 状态应等待或拒绝。
- **证据**：base.go:153-157 case ContainerStateStopping: 注释 'Wait for stop to complete, then start fresh' 但直接 b.stopChan = make(...); b.state = Starting，无任何等待；base.go:210-214 MarkStopped: `b.state = ContainerStateStopped`（无状态前置检查）；base.go:270 Stop 的 Running 分支在 stopFunc 返回后无条件调 b.MarkStopped()；base.go:191-194 并发 Start 成功路径 `b.stopFunc = stopFunc; ... b.MarkRunning()`
- **核实**：核实成立，交错逐行可复现：Stop() Running 分支在 base.go:251-256 置 Stopping、清 stopFunc、close(stopChan) 后解锁执行 stopFunc（外部进程 kill，耗时不定），完成后 base.go:270 调 MarkStopped；MarkStopped（base.go:210-214）无任何状态前置检查直接 b.state = Stopped，与 MarkRunning（base.go:200-206 仅 Starting→Running）不对称。并发 Start() 在 base.go:153-157 见 Stopping 时注释写 'Wait for stop to complete' 但实际立即重建 stopChan、置 Starting，解锁跑 runFunc 拉起新进程，base.go:192 缓存新 stopFunc、194 MarkRunning 成功置 Running；随后旧 Stop 的 MarkStopped 把 Running 覆盖为 Stopped。后果链（新进程存活但 IsRunning=false → 再次 Start 走 Stopped 分支拉起第二个进程并覆盖 stopFunc → 首进程孤儿）成立。触发路径核实：ContainerMgr.StartAll/StopAll（manager.go:107/149）都只持 RLock 调 c.Start()/c.Stop()，两者可并发；rpc 层直接调 Restart 也无外层串行化。与 CT-05（Start-during-Starting）、CT-06（Stop-during-Starting）确为不同交错，非重复。P1 恰当。

### 4. [P1·confirmed] converter 包内 httpGet 无超时、无响应体上限，fetchClashTemplate 可致 /sub 请求永久阻塞

- **位置**：`pkg/proxy/core/subscription/converter/http.go:25` · 维度：资源 · 单元：SUB · 旧 review: SUB-04
- **问题**：converter/http.go 的 httpGet 用裸 `&http.Client{}`（无 Timeout），且 `io.ReadAll(resp.Body)` 无大小上限。fetchClashTemplate（clash.go:700-718）顺序请求 4 个第三方公网 sub-converter，任一 URL 建立连接后不返回数据/慢速挂起，都会让该 HTTP GET 无限期阻塞，从而使承载它的 /sub handler goroutine 永久挂住；在并发订阅场景下会累积 goroutine、耗尽连接/内存。父包 http.go 的 httpGet 已设 10s 超时与 4MB 上限（http.go:19-21,39,50），本副本却与之不一致。属旧 review 已记录且仍存在。
- **证据**：converter/http.go:25 `client := &http.Client{}`（无 Timeout）；converter/http.go:36 `data, err := io.ReadAll(resp.Body)`（无 LimitReader）；对比 subscription/http.go:39 `&http.Client{Timeout: httpTimeout}`、:50 `io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))`
- **核实**：确证。converter/http.go:25 `client := &http.Client{}` 无 Timeout，:36 `io.ReadAll(resp.Body)` 无上限；同包父层 subscription/http.go:19-21 已定义 httpTimeout=10s、maxResponseBody=4MB 并在 :39/:50 使用，两套实现确实不一致。触发路径完整可达：/sub handler（pkg/http/sub_handler.go:199 ConvertURIsWithOptions）→ ClashConverter.ConvertWithOptions（clash.go:177 无条件 fetchClashTemplate，注释明确 'Always go through the external template'，无本地 fallback）→ fetchClashTemplate（clash.go:704 逐个调用本包 httpGet 请求 4 个第三方 subconverter，clash.go:47-52）。任一第三方建连后挂起即令 /sub goroutine 无限期阻塞，help 文本（sub_handler.go:373 '强制依赖远程模板'）也印证该路径必经。与旧 review SUB-04 重复（docs/review-2026-04-09.md，finder 未标 legacyId）；P1 与旧条目定级一致。

### 5. [P1·confirmed] Clash 订阅硬依赖 4 个第三方 sub-converter 公网服务，全挂即整体失败无降级（遗留）

- **位置**：`pkg/proxy/core/subscription/converter/clash.go:177` · 维度：架构 · 单元：SUB · ⚠️协议相关（需人工核对上游） · 旧 review: SUB-02
- **问题**：ConvertWithOptions 无条件调用 fetchClashTemplate（clash.go:177），后者依次请求 subConverterURLs 中 4 个第三方公网服务（sub.xeton.dev/api.dler.io/sub.maoxiongnet.com/sub.id9.cc，clash.go:47-52），4 个全部失败则返回 error，ConvertWithOptions 直接 'fetch clash template failed' 报错（clash.go:178-180），明确不降级到本地 buildLocalConfig（该函数仍存在于 clash.go:944 但已无生产调用）。触发条件：这些公网服务被墙/下线/限流时，所有 Clash 客户端订阅整体失败。影响：核心订阅功能可用性完全受制于不可控的第三方。此为 2026-04-09 已记录问题，现状未变。
- **证据**：clash.go:47-52 subConverterURLs 4 个硬编码 URL；clash.go:177-180 fetch 失败即 return err；clash.go:944 buildLocalConfig 仅测试引用
- **核实**：核实成立,且为旧 review SUB-02(P1,docs/review-2026-04-09.md:310)的重复,finder 已在 detail 中标注"2026-04-09 已记录"但未填 legacyId。clash.go:47-52 硬编码 4 个第三方公网 URL(sub.xeton.dev/api.dler.io/sub.maoxiongnet.com/sub.id9.cc)。ConvertWithOptions(159)无条件调 fetchClashTemplate(177),fetchClashTemplate(700-718)依次尝试 4 个 URL,全失败返回 error,177-180 直接 return "fetch clash template failed",无降级。buildLocalConfig(944)仍存在但生产路径不再调用(仅注释 158 明确"fail fast; no silent fallback",及测试引用)。第三方全挂即所有 Clash 订阅整体失败。P1 合理。

### 6. [P2·confirmed] port_mappings 读写两侧都静默吞错，一次解析失败会导致下次 Save 用 '{}' 永久覆盖端口映射数据

- **位置**：`pkg/store/user_store.go:95` · 维度：错误处理 · 单元：STO
- **问题**：SQLiteUserStore.Load/Get/ListByGroup 在 port_mappings 列 JSON 解析失败时只 log.Printf 告警不返回错误，u.PortMappings 保持 nil；而 Save 侧（user_store.go:126-131）对 json.Marshal 采用 `if data, err := json.Marshal(...); err == nil` 模式，nil/空 map 时直接写 '{}'。两者组合形成数据丢失链：任何原因导致某行 port_mappings 损坏（磁盘/手工编辑/未来格式变更）→ 启动加载得到 nil → usermanager 后续任何一次 Save（如流量累计更新）就把该列覆盖为 '{}'，端口映射被静默永久清除，用户的确定性端口分配随之漂移。PortMappings 是 map[uint32]uint32，Marshal 侧确实不会失败，但 Unmarshal 侧的容错语义应改为返回错误（拒绝加载损坏行）或至少保留原始字节避免回写覆盖。
- **证据**：pkg/store/user_store.go:94-99 `if err := json.Unmarshal(...); err != nil { log.Printf(...) }`（Get 218-222、ListByGroup 277-281 同样拷贝）；pkg/store/user_store.go:126-131 `portMappingsJSON := "{}"; if user.PortMappings != nil && len(...)>0 { if data, err := json.Marshal(...); err == nil { ... } }`
- **核实**：代码直读确证。pkg/store/user_store.go:94-99（Load）、218-222（Get）、277-281（ListByGroup）三处对 port_mappings 的 json.Unmarshal 失败都只 log.Printf 不返回错误，u.PortMappings 保持 nil；Save 侧 126-131 在 PortMappings 为 nil/空时无条件写 "{}"（json.Marshal 用 `err == nil` 模式吞错，但 map[uint32]uint32 确实不会 Marshal 失败，主要风险在 Unmarshal 侧）。触发链存在：pkg/proxy/usermanager/usermanager.go 有 15+ 处 store.Save 调用点，包括 2140 行附近的周期性流量写回（注释明言 'issues store.Save per active user'），任何一行 port_mappings 损坏 → 启动加载得 nil → 下次流量写回即用 '{}' 永久覆盖。需要前置条件（行数据已损坏），属韧性/数据丢失链问题，P2 合理。非旧 review 重复（docs/review-2026-04-09.md 无 pkg/store 条目）。

### 7. [P2·confirmed] prettyPrintJSON 经 interface{}/float64 往返重排并可能破坏 native_json 数值精度，应改用 json.Indent

- **位置**：`pkg/store/inbound_store.go:90` · 维度：正确性 · 单元：STO
- **问题**：SQLiteInboundStore.Save 在写库前调用 prettyPrintJSON：json.Unmarshal 到 interface{} 再 MarshalIndent。这不是纯格式化：所有数值经 float64 中转，任何绝对值超过 2^53 的整数会被静默改值（如 9007199254740993 → 9007199254740992），≥1e21 的数会变成科学计数法字符串；同时对象键被按字典序重排，持久化字节与容器层原始生成的 native JSON 不再一致。inbound native_json 是 xray/hysteria/snell/mihomo 的原生配置，属于'按 tag 存原生 JSON'的透传数据，持久层不应对其做有损归一化。用 json.Indent(&buf, raw, "", "  ") 可以在保留原始 token（大整数、键序）的同时完成缩进与合法性校验。当前配置字段（端口/超时/带宽）实际值都远小于 2^53，故定 P2 而非 P1，但这是持久层的正确性地雷。
- **证据**：pkg/store/inbound_store.go:90-96 `func prettyPrintJSON(raw []byte) ([]byte, error) { var v interface{}; if err := json.Unmarshal(raw, &v); ...; return json.MarshalIndent(v, "", "  ") }`，由 Save（38-42 行）在每次落库前调用
- **核实**：代码+实证确证。pkg/store/inbound_store.go:90-96 prettyPrintJSON 确实 Unmarshal 到 interface{} 再 MarshalIndent，Save（39 行）每次落库前调用。我在本仓库用临时测试实测（Go 1.24.4，与项目同工具链）：输入 {"big":9007199254740993,"b_key":1,"a_key":2,"huge":10000000000000000000000} 往返后输出 big=9007199254740992（静默改值）、huge=1e+22（科学计数法）、键序被重排为字典序（a_key 排到 b_key 前）——三项主张全部复现。native_json 是容器原生配置透传数据，持久层做有损归一化确属正确性隐患；当前实际配置数值远小于 2^53，P2（而非 P1）恰当。json.Indent 替代方案成立。

### 8. [P2·confirmed] SQLite 数据库文件含全部用户 auth_token 明文，但目录 0755 且未收紧文件权限，违背项目对机密文件 0600 的既有约定

- **位置**：`pkg/store/manager.go:24` · 维度：安全 · 单元：STO · ⚠️协议相关（需人工核对上游）
- **问题**：users 表持久化 auth_token（订阅/接入凭据，明文）和 login_password（bcrypt）。NewStoreManager 用 os.MkdirAll(dir, 0755) 建目录，Open 后也没有对 DB 文件及 WAL 模式产生的 -wal/-shm 伴随文件做 chmod；SQLite（modernc.org/sqlite 走 POSIX open 语义）默认按 0644&umask 创建数据库文件，通常即 world-readable。同一主机上的其他本地用户可直接读走全部代理凭据。项目其他持有机密的落盘路径已统一 0600/0700（reality keys.go:189、xrayapi builder.go:368-371、mihomo process.go:189-190 因嵌 token 用 0600、params/defaults.go:249 scratch 目录 0700），唯独最集中的凭据库没有收紧。建议 MkdirAll 用 0700，Open 成功后对 dsn、dsn+"-wal"、dsn+"-shm" chmod 0600。依据：SQLite 上游默认以 0644（受 umask 约束）创建主库及伴随文件，modernc 纯 Go 实现沿用该语义。
- **证据**：pkg/store/manager.go:24 `os.MkdirAll(dir, 0755)`；pkg/store/db.go:16-39 Open 无任何权限处理；对照项目约定 pkg/proxy/containers/mihomo/process.go:189-190 `0600 because the file may embed the REST API bearer token`
- **核实**：protocolRelated 条目，但未凭记忆：我用本仓库 go.mod 锁定的 modernc.org/sqlite v1.46.1 写临时测试实测，Open 创建的 test.db、test.db-wal、test.db-shm 三个文件权限均为 0644（world-readable），上游行为在仓库内实证成立。pkg/store/manager.go:24 `os.MkdirAll(dir, 0755)` 确认，pkg/store/db.go Open（16-39 行）无任何 chmod。users 表 auth_token 明文（migrations v14 将 password 重命名为 auth_token，Save/Load 全程明文存取）确认。项目对照约定确认：pkg/proxy/containers/mihomo/process.go:190 有注释 '0600 because the file may embed the REST API bearer token' 并用 0600 写文件。同主机低权限用户可读走全部订阅凭据，P2 恰当。

### 9. [P2·confirmed] SQLiteInboundStore 完全没有测试；UserStore 的 Get/Delete/Count 与 expiry/port_mappings 往返也无覆盖

- **位置**：`pkg/store/inbound_store.go:1` · 维度：测试缺口 · 单元：STO
- **问题**：pkg/store 目录下没有 inbound_store_test.go：Save 的 prettyPrintJSON 格式化、非法 JSON 报错路径、INSERT OR REPLACE 语义、Load/Delete 全部零测试（架构约束 #9 要求每个可测模块有 *_test.go）。user_store_test.go 只覆盖 role/login_password 往返和 ListByGroup，未测 Get（含 ErrNoRows→(nil,nil) 契约）、Delete、Count、expiry_time 双格式解析（RFC3339 与 legacy '2006-01-02 15:04:05' 回退分支）、port_mappings JSON 往返及损坏数据容错。这些恰恰是本模块地图里标注的最高风险区（21 列三份手工 Scan 拷贝），加字段时最需要回归网。
- **证据**：目录清单无 inbound_store_test.go（pkg/store/ 下仅 db_test.go、manager_test.go、node_groups_store_test.go、user_store_test.go）；user_store_test.go 的测试函数列表为 Role_RoundTrip/LoginPassword_RoundTrip/DefaultRole/RoleAndLoginPassword_DoNotInterfer/UpdateRole/ListByGroup/UpdateLoginPassword，无任何 Get/Delete/Count/expiry/port_mappings 用例。
- **核实**：目录清单核实：pkg/store/ 下测试文件仅 db_test.go、manager_test.go、node_groups_store_test.go、user_store_test.go，无 inbound_store_test.go；inbound_store.go 的 Save/prettyPrintJSON/Load/Delete（38/55/81/89 行）确实零测试。user_store_test.go 的测试函数列表与 finder 所述完全一致（Role_RoundTrip/LoginPassword_RoundTrip/DefaultRole/RoleAndLoginPassword_DoNotInterfer.../UpdateRole/ListByGroup/UpdateLoginPassword），无 Get（含 ErrNoRows→(nil,nil) 契约，user_store.go:201-203）、Delete、Count、expiry 双格式回退（85/210/269 行）、port_mappings 往返的用例。PROJECT_GUIDE.md:76 约束 #9 '每个可测模块对应 *_test.go' 属实。P2 恰当。

### 10. [P2·confirmed] WithTx 在 fn panic 时既不 Rollback 也不释放事务，单连接池下会卡死整个存储层

- **位置**：`pkg/store/tx.go:17` · 维度：并发 · 单元：STO
- **问题**：WithTx 的 defer 只在 retErr != nil 时执行 Rollback（tx.go:16-22）。若 fn 内部 panic，defer 仍会运行但 retErr 为 nil，事务既未 Commit 也未 Rollback，其占用的连接永远不会归还连接池。由于 db.go:23 SetMaxOpenConns(1) 把连接池限成单连接，只要这个 panic 被上层（如 gin 的 recover 中间件或任何 recover）捕获而进程不退出，之后所有 DB 操作（用户读写、inbound 持久化、node groups）都会永久阻塞在等待唯一连接上，等效全库死锁。WithTx 是架构约束 #7 指定的通用 Tx 抽象，会被后续代码广泛复用，应改为无条件 `defer tx.Rollback()`（Commit 成功后 Rollback 是 no-op，返回 sql.ErrTxDone 可忽略）或在 defer 中检测 recover。
- **证据**：pkg/store/tx.go:16-22 `defer func() { if retErr != nil { if rbErr := tx.Rollback(); ... } }()`；pkg/store/db.go:23 `sqlDB.SetMaxOpenConns(1)`。panic 路径 retErr==nil → 不 Rollback → 唯一连接被泄漏的 tx 持有。
- **核实**：代码缺陷本身确证：tx.go:16-22 的 defer 仅在 retErr != nil 时 Rollback，fn panic 时命名返回值 retErr 仍为 nil，事务既不 Commit 也不 Rollback，其持有的连接不归还；db.go:23 SetMaxOpenConns(1) 使该连接是唯一连接，一旦 panic 被 recover 而进程存活即等效全库死锁。但触发路径核实结果比 finder 描述弱：当前 WithTx 仅有两个调用方——migrate.go:59 applyMigration（启动路径，panic 直接崩进程）和 node_groups_store.go:70 Set（由 pkg/rpc/server/end_node_node_groups.go 的 gRPC handler 调用，end_node_server.go:146-164 的 unaryServerInterceptor 无 recover，grpc-go 默认也不 recover，panic 同样崩进程）。gin.Default()(pkg/http/init.go:13) 的 Recovery 中间件虽在同进程存在，但现有 gin handler（node_groups_handler.go 走 RPC 转发）无一transitively 调用 WithTx。即：今天 panic 的实际后果是进程崩溃而非静默死锁，'卡死整个存储层'是未来复用场景下的潜在缺陷。缺陷真实且修复廉价（无条件 defer tx.Rollback()），但缺少现行触发路径，P1 降为 P2。

### 11. [P2·confirmed] Migrate 不检测数据库版本高于代码已知最大版本，旧二进制静默运行在新 schema 上

- **位置**：`pkg/store/migrate.go:43` · 维度：正确性 · 单元：STO
- **问题**：Migrate 对 m.Version <= maxVersion 的迁移直接 continue（migrate.go:44-46），若数据库 schema_migrations 的 MAX(version) 已经超过当前二进制携带的最大迁移版本（回滚部署、多节点混跑新旧版本共享数据目录），循环会把所有迁移全部跳过并返回 nil，旧代码随后直接操作它不认识的新 schema。以 v14 为例（password 列改名 auth_token），v14 之前的二进制在已升级的库上启动时 Migrate 不报错，直到第一次 SELECT password 才在业务路径深处失败，报错点远离根因。应在 Migrate 末尾比较 maxApplied 与 sorted 里的最大版本，超出时返回明确错误（或至少 warning 日志）。
- **证据**：pkg/store/migrate.go:43-55：循环只处理 `m.Version > maxVersion` 的项，函数无任何 `dbVersion > len(migrations)` 类检查即 return nil。migrations/migrations.go:136 的 v14 RENAME COLUMN 是一个真实的不兼容变更实例。
- **核实**：核实 migrate.go:43-55：循环对 m.Version <= maxVersion 直接 continue，函数末尾无任何 maxVersion（DB 侧）> sorted 最大版本（代码侧）的检查即 return nil。若 schema_migrations 的 MAX(version)=15 而二进制只带 14 个迁移，全部跳过、静默成功。migrations.go:134-137 的 v14 RENAME COLUMN password→auth_token 确是不兼容变更实例：v13 二进制在 v14 库上启动 Migrate 通过，直到 user_store 首次 SELECT password 才在业务深处报 no such column。回滚部署/混跑版本共享数据目录时可触发，P2 恰当。

### 12. [P2·confirmed] DefaultInbound 的 Setter 不同步 config_，Config() 与 getter 分叉

- **位置**：`pkg/proxy/core/inbound/default.go:64` · 维度：正确性 · 单元：CTR · 旧 review: I-01
- **问题**：DefaultInbound 同时保存两份状态：独立字段 tag_/protocol_/port_/listenAddr_（default.go:13-17）和构造时快照的 config_ *Config（default.go:18,35-41）。SetTag(default.go:64)/SetProtocol(67)/SetPort(70)/SetListenAddr(73-78) 只改独立字段，从不回写 config_。因此任何 Set* 之后，Tag()/Port()/ListenAddr() 返回新值，而 Config() 返回的仍是构造时旧值。实际触发：mihomo adapter.go:63 与 hysteria container.go:152 调 SetListenAddr("127.0.0.1") 改绑定地址，但 core/container/inbound.go:67 的 Config() 取到的 ListenAddr 仍是 "0.0.0.0"。凡是消费 Config() 而非 getter 的下游都会读到过期字段。修复应让 Set* 同步更新 config_，或让 Config() 从 getter 现算。
- **证据**：default.go:64 `func (i *DefaultInbound) SetTag(tag string) { i.tag_ = tag }`；config_ 在 35-41 构造后再不更新；Config()(58) 直接 `return i.config_`
- **核实**：核实成立但触发路径描述过强。default.go:64-78 四个 Set* 确实只写独立字段，config_（default.go:18，仅在 NewDefaultInbound 35-41 构造时快照）从不回写，Config()(58) 直接返回旧快照——状态分叉本身确凿。SetListenAddr 的生产调用点也确认存在（mihomo/inbound.go:153,171,562、mihomo/adapter.go:63、hysteria/container.go:152,266,655、snell/container.go:107,152,541），这些实例的 Config().ListenAddr 全部停留在 "0.0.0.0"。但 finder 声称的『实际触发』不准确：全仓 grep 显示生产代码中 .Config() 的唯一调用点就是 core/container/inbound.go:67 自身（inboundAdapterImpl.Config 的内部转发），没有任何下游真正消费 Config() 的返回值，因此当前不存在读到过期字段的实际数据流，属于潜伏的接口契约违反而非现行 bug。与旧 review I-01 重复（docs/review-2026-04-09.md，且 docs/review-2026-07-10/10-legacy-status.md:155 已标 still-present），finder 未标 legacyId。鉴于无现行消费者，P1 偏高，降为 P2。

### 13. [P2·confirmed] core/inbound 包完全无 *_test.go（架构约束#9），且正是 SetTag 分叉 bug 的盲区

- **位置**：`pkg/proxy/core/inbound/inbound.go:3` · 维度：测试缺口 · 单元：CTR
- **问题**：pkg/proxy/core/inbound 目录仅有 default.go 与 inbound.go，没有任何 *_test.go。这违反 PROJECT_GUIDE 第9条“每个可测模块应有 *_test.go”。该包含有纯逻辑可测点：DefaultInbound 的 getter/setter/Config 一致性、NewDefaultInbound 的空 tag→"default-inbound"、port 0→10000 替换、Validate 的 [100,65535] 边界、InboundError 的 Error/Unwrap。缺测直接导致同目录的 SetTag/SetPort 不同步 config_（见 I-01）这类回归无人拦截。
- **证据**：`ls pkg/proxy/core/inbound/*_test.go` → No such file；目录仅 default.go / inbound.go
- **核实**：核实成立。pkg/proxy/core/inbound/ 目录确认仅有 default.go 与 inbound.go，无任何 *_test.go。PROJECT_GUIDE.md:76 第9条确有『每个可测模块对应 *_test.go』的约束。该包纯逻辑可测点真实存在：NewDefaultInbound 的空 tag→"default-inbound"/port 0→10000（default.go:22-28）、Validate 的 [100,65535] 边界（default.go:100）、InboundError 的 Error/Unwrap（inbound.go:105-114），且 idx 0 的 Set*/Config 分叉正是缺测盲区（legacy I-01 两轮 review 仍 still-present 即为佐证）。P2 恰当。

### 14. [P2·confirmed] crypto/rand 失败时静默返回可预测的时间种子“密码”，SS-2022 分支还会产出非法密钥

- **位置**：`pkg/proxy/core/params/defaults.go:370` · 维度：安全 · 单元：CTR · ⚠️协议相关（需人工核对上游）
- **问题**：randomSSPassword(defaults.go:369-372) 和 randomHex(defaults.go:383-385) 在 rand.Read 出错时都吞掉错误并返回 `fmt.Sprintf("fallback-%x", time.Now().UnixNano())`——一个可由时间猜测的字符串，被当作 trojan/hysteria2/tuic/anytls/ss 的凭据或自签证书文件名后缀写入。这是一条静默弱密钥路径：调用方无法感知凭据不再是随机的。更严重的是 SS 2022-blake3-* 分支（defaults.go:362-373）本应返回 base64 编码的 32/16 字节密钥（SIP022），fallback 返回的 `fallback-...` 既不可预测性可控又不是合法的 32 字节 base64，会让 mihomo/xray 的 SS listener 直接拒绝启动。虽然 Linux 上 getrandom 几乎不失败，但把加密失败降级成“弱且可能非法的密钥”是错误的容错策略，应向上返回 error。
- **证据**：defaults.go:370 `return fmt.Sprintf("fallback-%x", time.Now().UnixNano())`（2022 密钥分支）；defaults.go:384 同款 fallback 用于 randomHex 生成 trojan/hy2/tuic/anytls 密码与证书文件后缀
- **核实**：核实成立，且上游行为断言有仓库内材料支撑（protocolRelated 要求满足）。代码证据：defaults.go:369-371 SS-2022 分支在 rand.Read 失败时返回 `fmt.Sprintf("fallback-%x", time.Now().UnixNano())`，defaults.go:383-385 randomHex 同款 fallback；randomHex 被用于 vmess/trojan/hy2/tuic/anytls 密码（defaults.go:121,144,155,159）及证书文件名后缀（defaults.go:281），错误被吞、调用方无从感知凭据退化为时间可预测串。SS-2022 非法密钥断言由仓库内材料确证：defaults.go:355-358 注释自述 SIP022 需 32/16 字节 base64 raw key，wiki/knowledge/mihomo-container/edge-cases.md:34 明确记载『客户端手动指定密码时也要遵守该约束，否则 mihomo listener 会以 EOF/解码失败的形式报错』——fallback 串含 '-' 且非合法 base64 key material，确会违反该约束。旧 review 无对应条目（core/params 为新层），非重复。P2 恰当（Linux 上 getrandom 几乎不失败，但静默弱密钥是错误容错策略）。

### 15. [P2·confirmed] requireUint32 接受 port=0 并声称“adapter 会分配”，但无 adapter 实现该语义，下游 Validate 反而拒绝<100

- **位置**：`pkg/proxy/core/params/protocolparams/parser.go:153` · 维度：正确性 · 单元：CTR
- **问题**：requireUint32 的注释（parser.go:153）写“0 is accepted — the adapter layer interprets it as 'allocate one'”，且 case int/int32/int64/float64 均允许 0 通过。但没有任何 adapter 真会对 port 0 做分配：mihomo requirePort(adapter.go:151) 同样放行 0，随后 NewMihomoInbound→Validate→DefaultInbound.Validate(default.go:100) 以 `port_ < 100` 直接拒绝。于是 port 0 永远走不通，注释描述的契约不存在。同时这形成校验地板不一致：contracts.InboundSpec.Validate(inbound.go:30) 与 inbound.Config/DefaultInbound.Validate 都用 [100,65535]，而 protocolparams 层放行 [0,65535]，同一请求在不同层得到不同判定，且错误信息落在最下层（“port out of range”）而非解析层。应统一地板或修正注释。
- **证据**：parser.go:153 注释 vs default.go:100 `if i.port_ < 100 || i.port_ > 65535 { return ErrInboundPortOutOfRange }`；两处 requirePort/requireUint32 均对 0 返回 nil
- **核实**：核心断言成立但触发机制描述有误。成立部分：parser.go:153 注释『0 is accepted — the adapter layer interprets it as allocate one』描述的分配契约确实不存在——protocolparams.Parse 的唯一生产调用方是 mihomo/adapter.go，端口最终经 NewMihomoInboundFromProtocolParams→NewDefaultInbound（default.go:26-28），没有任何 adapter 做端口分配；protocolparams.go:53 注释也只说 0 'only meaningful when the adapter knows how to allocate'，即作者自知无人实现。校验地板不一致也属实：parser 放行 [0,65535]，contracts/inbound 层用 [100,65535]，port 1-99 会穿过解析层在最底层才报 'port out of range'。有误部分：port=0 并非『Validate 以 port_<100 拒绝、永远走不通』——NewDefaultInbound(default.go:26-28) 会把 0 静默替换为固定端口 10000，Validate 随后通过；实际行为是 0 被悄悄映射到硬编码 10000（多 inbound 传 0 会端口冲突），比 finder 描述的『被拒』更隐蔽。修复方向（统一地板或修正注释/实现真分配）仍然成立，P2 保留。

### 16. [P2·confirmed] Stop 中 stopFunc 失败仍标记为 Stopped，外部进程可能存活，下次 Start 直接造成双进程

- **位置**：`pkg/proxy/core/container/base.go:261` · 维度：错误处理 · 单元：CON
- **问题**：Stop() 的 Running 分支在 stopFunc 返回错误时（base.go:259-266），注释明确写 "Even if stop function fails, we still mark as stopped"，把 state 强制置为 Stopped 后返回错误。对管理外部进程的容器来说，stopFunc 失败通常意味着进程可能还活着（kill 失败、超时等），此时状态机却已宣布 Stopped：任何后续 Start()（包括 Restart 的重试、StartAll）都会直接再 exec 一个新进程——旧进程占着端口则新进程启动失败，或者两个进程同时存活跑流量（流量统计、转发规则均会错乱）。且 stopFunc 引用已在进入分支时清空（base.go:252），失败后无法重试停止。更合理的语义：stop 失败保持 Stopping（或引入 Failed 态）并保留 stopFunc 供重试，由上层决定强杀。当前 StopAll（manager.go:148-159）只收集错误不重试，进程泄漏会静默发生在节点关停/重启流程中。
- **证据**：base.go:259-266 `if err := stopFunc(); err != nil { // Even if stop function fails, we still mark as stopped\n b.mu.Lock(); b.state = ContainerStateStopped; ... return errors.Wrap(errors.ErrContainerStopFailed, ...) }`；base.go:252 `b.stopFunc = nil // clear cached stop function`
- **核实**：代码证据确证：base.go:259-266 stopFunc 失败时注释明确写 "Even if stop function fails, we still mark as stopped" 并强制 state=Stopped；base.go:252 在进入分支时已清空 b.stopFunc，失败后无法重试停止。对外部进程容器（mihomo/hysteria/snell 均直接使用 BaseContainer.Stop，且 mihomo hooks 的 stop 是真实杀进程逻辑），kill 失败/超时后状态机已宣布 Stopped，后续 Start（StartAll、updater 重启）会直接再 exec 新进程，造成端口冲突或双进程。StopAll（manager.go:148-159）确实只用 errors.Join 收集错误不重试。P2 恰当。

### 17. [P2·confirmed] Container 接口对 GetRunFunc 的文档（'Returns nil, nil if not implemented'）与 BaseContainer/Hooks 契约直接矛盾，照文档实现会 panic

- **位置**：`pkg/proxy/core/container/interface.go:36` · 维度：正确性 · 单元：CON · 旧 review: CT-02
- **问题**：interface.go:33-37 把 GetRunFunc 声明为 Container 公共接口方法，且文档写 'Returns nil, nil if not implemented'，暗示返回 nil 是合法实现。但 base.go 的 Hooks 契约（base.go:44-53）明确 'run: MUST NOT be nil - BaseContainer will error if run is nil'，且 Start() 在拿到 nil runFunc 时直接 panic（base.go:172-175 'GetRunFunc() returned nil run function - hooks must provide run function'）。一个新容器实现者若按接口文档返回 nil,nil，任何经由 BaseContainer.Start 的路径都会 panic 而非 error。这是旧条目 CT-02（GetRunFunc 不该暴露在公共接口上）的具体危害面：接口层与实现层对同一方法给出互斥的契约。修复时若暂不能从接口移除，至少要把接口文档改为与 Hooks 一致的 MUST NOT nil 语义。
- **证据**：interface.go:33-37 `// GetRunFunc returns the run and stop functions ... // Returns nil, nil if not implemented. GetRunFunc() (func() error, func() error)`；base.go:46-47 `// - run: ... MUST NOT be nil`；base.go:172-175 `if runFunc == nil { b.state = ContainerStateStopped; b.mu.Unlock(); panic("BaseContainer: GetRunFunc() returned nil run function ...") }`
- **核实**：核实成立。interface.go:33-37 文档明确写 'Returns nil, nil if not implemented.'，暗示 nil 合法；而 base.go:46-47 Hooks 契约写 'run: MUST NOT be nil - BaseContainer will error if run is nil'，base.go:172-175 在 runFunc==nil 时直接 panic（且 panic 前已把 state 置回 Stopped 并解锁，说明这是刻意的 fail-fast）。两份契约对同一方法互斥。危害面核实：测试中的 mock 容器（pkg/rpc/server/fastadd_params_test.go:44、pkg/proxy/core/subscription/manager_test.go:44）确实按接口文档返回 nil,nil——它们不经 BaseContainer 所以不炸；但新容器实现者若参照接口文档实现 Hooks（所有真实容器 xray/mihomo/hysteria/snell 均走 BaseContainer+Hooks 路径），Start 即 panic。与旧 review CT-02（GetRunFunc 不应暴露为公开接口，P2）同源，本条是其文档矛盾这一具体危害面的补充，finder 已在 detail 声明关联。P2 恰当。

### 18. [P2·confirmed] GetUserSubscriptions 双契约并存：接口强制方法与 SubscriptionProvider 可选接口重复，后者无任何真实消费方

- **位置**：`pkg/proxy/core/container/subscription.go:7` · 维度：架构 · 单元：CON · 旧 review: CT-03
- **问题**：同一个方法签名 GetUserSubscriptions(contracts.SubscriptionRequest) 同时存在于两处：(1) Container 接口的强制方法（interface.go:62-65）；(2) subscription.go:5-11 定义的可选能力接口 SubscriptionProvider，其注释称 'optional interface that containers can implement'。实际调用方 core/subscription/manager.go:57 直接调用接口强制方法，全仓库对 SubscriptionProvider 的唯一引用是 xray 包一条 `var _ container.SubscriptionProvider = (*Executor)(nil)` 编译断言——可选接口是死代码，且其 'optional' 语义与接口强制语义互相矛盾，误导新容器实现者以为可以不实现。这是旧条目 CT-03（应改为可选能力接口而非强制方法）的延伸：不但没改成可选，还留下了一个从未被类型断言消费的可选接口壳。二选一：要么把 Container 接口中的该方法移除、调用方改为对 SubscriptionProvider 做类型断言（与 Restorable 模式一致，manager.go:30-32），要么删除 subscription.go。
- **证据**：subscription.go:5-11 SubscriptionProvider 定义；interface.go:62-65 Container 接口内强制 GetUserSubscriptions；pkg/proxy/core/subscription/manager.go:57 `subs, err := c.GetUserSubscriptions(req)` 直接调接口方法；全仓库 grep 仅 pkg/proxy/containers/xray/subscription.go:31 的编译期断言引用 SubscriptionProvider
- **核实**：核实成立。pkg/proxy/core/container/interface.go:62-65 中 GetUserSubscriptions 是 Container 接口的强制方法；subscription.go:5-11 又定义了同签名的"可选"接口 SubscriptionProvider。全仓库 grep 确认 SubscriptionProvider 仅有一处外部引用：pkg/proxy/containers/xray/subscription.go:31 的编译期断言 `var _ container.SubscriptionProvider = (*Executor)(nil)`，无任何类型断言消费方。真实调用链在 pkg/proxy/core/subscription/manager.go:57 直接调接口强制方法 `c.GetUserSubscriptions(req)`，各容器（hysteria/snell/mihomo/xray）都被迫实现该方法。"optional" 注释与接口强制语义确实矛盾。该条与旧 review CT-03（GetUserSubscriptions 应改为可选能力接口）高度重叠，finder 未标 legacyId——本条实质是 CT-03 的延伸/重复（新增事实是死接口壳的存在），建议归并到 CT-03 处理。

### 19. [P2·confirmed] legacy 全局注册体系（RegisterContainer/GetContainer/SetContainer）已无生产消费方，成为纯死代码但仍在扩大误用面

- **位置**：`pkg/proxy/core/container/registry.go:22` · 维度：架构 · 单元：CON · 旧 review: CT-09
- **问题**：旧条目 CT-09 记录了双注册体系并存的问题。当前核实结果更进一步：legacy 面（RegisterContainer/RegisterSingleton/SetContainer/GetContainer/NewContainer/IsSingleton/IsRegistered/GetRegisteredTypes，registry.go:22-173）在生产代码中已无任何调用方——旧 loc 提到的 pkg/xrayapi/hotreload、rpc/server 消费点已迁移到 ContainerMgr；全仓库唯一残留是 pkg/proxy/containers/xray/exec_runner.go:2598-2617 的 init() 仍向 legacy 注册面注册一个 hardcode 默认路径（/usr/local/bin/xray、/tmp/xray-default.json）、出错即 panic 的工厂，其注释还在宣传 'Users can use container.NewContainer(...)' 用法。这套死代码带着上述 CT-10 数据竞争一起保留，任何未来调用者都会踩坑。建议整体删除 legacy 注册面及 xray 的对应 init 注册（ContainerMgr.LoadFromConfig 只认 GetFactory，manager.go:63）。
- **证据**：registry.go:22-173 legacy globalRegistry 全套 API；grep 全仓库（排除 core/container 自身与测试）对 GetContainer/SetContainer/RegisterSingleton/NewContainer/IsSingleton 零生产命中；唯一注册方 pkg/proxy/containers/xray/exec_runner.go:2603 `container.RegisterContainerFunc(contracts.ContainerXray, ...)`（内部 hardcode BinaryPath /usr/local/bin/xray 并在 NewExecutor 出错时 panic）；manager.go:63 只查 GetFactory
- **核实**：核实成立。registry.go:22-173 的 legacy globalRegistry 全套 API（RegisterContainer/RegisterSingleton/SetContainer/GetContainer/NewContainer/UnregisterContainer/IsSingleton/IsRegistered/GetRegisteredTypes）经全仓库 grep 确认在生产代码中零调用方（其余命中均为 protobuf 生成的 GetContainer() 字段 getter，与此无关）；唯一残留是 pkg/proxy/containers/xray/exec_runner.go:2603 的 init() 向 legacy 面注册 hardcode 路径（/usr/local/bin/xray、/tmp/xray-default.json、GRPC 127.0.0.1:62789）且 NewExecutor 出错即 panic 的工厂，注释仍宣传 container.NewContainer/GetContainer 用法。manager.go:63 的 LoadFromConfig 确实只查 GetFactory（新 factoryMap 体系），legacy 注册对生产装配完全无效。测试面 registry_test.go 349 行全部在测这套死代码。该条与旧 review CT-09（双注册体系并存，P1）/CT-10（锁顺序隐患）重叠，finder 已在 detail 中提及但未标 legacyId——属 CT-09 的现状更新（消费方已迁移完毕，可整体删除），建议归并。P2 合理（死代码+误导性注释，非活跃故障）。

### 20. [P2·confirmed] 测试缺口：新 factoryMap 注册体系与 StartAll auto-Restore/错误累积路径均无测试，唯一并发测试无法覆盖竞态窗口

- **位置**：`pkg/proxy/core/container/registry.go:185` · 维度：测试缺口 · 单元：CON
- **问题**：本包测试面向的几乎全是待废弃/低风险路径，真正承载生产流程的部分缺测：(1) 新注册体系 registry.go:175-217 —— RegisterFactory 的静默覆盖语义（与 legacy RegisterContainer 报错语义相反，旧条目 CT-09 提示的行为不对称）、Create()、RegisteredTypes() 均无直接测试，registry_test.go 349 行全部测 legacy globalRegistry；(2) manager.go StartAll 的 auto-Restore 分支（117-121）与错误累积语义（Start 失败 continue 跳过 Restore、Restore 失败仍继续下一容器、errors.Join 聚合）无测试，现有 TestContainerMgr_StartAll_StopAll（manager_test.go:205）只走单容器 happy path；(3) base.go 的竞态窗口（慢 stopFunc 期间并发 Start、Starting 期间 Stop）无测试——TestBaseContainer_Concurrent_StartStop（base_test.go:226-247）用即时返回的 hook 且只断言最终状态，本 review P1 竞态 finding 描述的交错完全测不到。违反约束 #9 的精神：有测试文件但关键可测行为未覆盖。
- **证据**：registry_test.go:91-349 仅测 RegisterContainer/RegisterSingleton/SetContainer 等 legacy API，无 TestRegisterFactory/TestCreate；registry.go:182-189 RegisterFactory 注释 'Overwrites any previously registered factory'（覆盖语义无断言）；manager_test.go:205-238 StartAll 测试的 mock 未验证 Restorable 分支与多容器错误聚合；base_test.go:226-247 并发测试 hook 为 `func() error { return nil }` 即时返回
- **核实**：核实基本成立，有一处措辞需修正。(1) registry_test.go 全部 12 个测试函数（TestRegistry_Register 至 TestRegistry_Init）确实只测 legacy globalRegistry API；不过 RegisterFactory/GetFactory 并非完全零覆盖——manager_test.go:84-92 的 registerMgrFactory 助手直接调 RegisterFactory 且 LoadFromConfig 走 GetFactory，属间接覆盖；但 RegisterFactory 的静默覆盖语义（registry.go:184 注释 'Overwrites any previously registered factory'，与 legacy RegisterContainer 报错语义相反）、Create()（registry.go:200）、RegisteredTypes()（registry.go:209）确实无任何直接测试断言。(2) StartAll：TestContainerMgr_StartAll_StopAll（manager_test.go:205-238）为单容器 happy path，mockMgrContainer 虽实现 Restorable（manager_test.go:59）导致 auto-Restore 分支被执行，但测试未断言 Restore 被调用，也无 Start 失败跳过 Restore、StartAll 内 Restore 失败继续下一容器、多容器 errors.Join 聚合的测试（现有 TestContainerMgr_RestoreAll_errorAccumulated 只覆盖 RestoreAll 路径的错误聚合，非 StartAll:117-121）。(3) TestBaseContainer_Concurrent_StartStop（base_test.go:226-247）的 hook 为 `func() error { return nil }` 即时返回，仅断言最终 State，无法覆盖慢 stopFunc/Starting 期间的交错窗口。综合确认测试缺口存在。lineCorrection：finding 锚在 registry.go:185（RegisterFactory）比 185 行原报告更贴切，维持 185。

### 21. [P2·confirmed] GetSubscription 对单 container 失败静默 continue，无日志，订阅缺协议难排查

- **位置**：`pkg/proxy/core/subscription/manager.go:59` · 维度：错误处理 · 单元：SUB · 旧 review: SUB-01
- **问题**：GetSubscription 遍历各 container 调 GetUserSubscriptions，`if err != nil { continue }`（manager.go:57-60）静默跳过失败的 container，既不记日志也不累积错误。当某个 container（如 xray/mihomo）因内部错误返回 err 时，该协议的节点会从订阅里悄无声息地消失，运维在“订阅少了某协议”时无任何线索。文档注释虽已声明“failed containers are silently skipped”，但至少应 log.Warn 记录 container 类型与错误。旧 review 已记录且仍存在。
- **证据**：manager.go:57-60 `subs, err := c.GetUserSubscriptions(req); if err != nil { continue }`——无 log、无 errs 累积
- **核实**：确证。manager.go:52-62 GetSubscription 遍历 containers，:57-60 `subs, err := c.GetUserSubscriptions(req); if err != nil { continue }`——无 log 调用（manager.go 甚至未 import pkg/log）、无错误累积，单 container 失败时该协议节点从订阅结果中静默消失；函数注释 :40 'failed containers are silently skipped' 只是承认了该行为而非缓解。调用方 GetSubscriptionForClient/WithOptions（manager.go:89/109）也不感知部分失败，最终 /sub 返回的节点列表无任何缺失线索，运维排障确实无迹可循。与旧 review SUB-01 重复（docs/review-2026-04-09.md S7 节，P2，finder 未标 legacyId），代码现状与旧条目描述一致仍未修复；P2 定级与旧 review 一致，恰当。

### 22. [P2·confirmed] ext_sub / *_url 参数导致服务端 SSRF，且外部内容原样反射给请求方

- **位置**：`pkg/proxy/core/subscription/http.go:86` · 维度：安全 · 单元：SUB
- **问题**：订阅接口 /sub（pkg/http/sub_handler.go:64,177,189）把用户提交的 ext_sub、proxy_groups_url、rule_providers_url、rules_url 查询参数直接交给 subscription.FetchExternalSub/FetchXxxFromURL → http.go:86 的 httpGet 发起出站 GET。httpGet(http.go:26)只做 url.Parse+GET，没有任何 host/网段白名单、私网/元数据地址（127.0.0.1、169.254.169.254、10/8 等）过滤或 scheme 限制。ext_sub 拉回的正文会 tryBase64Decode 后按 URI 合并进 allURIs（sub_handler.go:182）并最终写入订阅响应体返回给调用者，因此这是一个可反射内容的 SSRF：任何持有订阅 token 或 user+pwd 的低权限代理用户即可让 center/end 节点探测内网 HTTP 服务并把响应内容取回。触发条件：一次带 ext_sub=http://内网地址 的 /sub 请求。影响：内网端口探测、云元数据/内部服务内容泄漏。父包 httpGet 有 10s 超时与 4MB 上限缓解了 DoS，但未缓解 SSRF 本身。
- **证据**：http.go:26 httpGet 无 host 校验；http.go:86 FetchExternalSub(subURL)；sub_handler.go:177 FetchAndMergeExtSubs(extSubs)→sub_handler.go:182 allURIs=append(allURIs, extURIs...)（反射回响应）；sub_handler.go:189 BuildConvertOptions(...proxy_groups_url/rule_providers_url/rules_url)
- **核实**：核实成立。http.go:26 httpGet 仅做 url.Parse + http.NewRequest("GET") + client.Do，无任何 scheme 限制、host/网段白名单、私网/元数据地址过滤,只有 10s 超时和 4MB 上限(缓解 DoS 而非 SSRF)。触发链完整:sub_handler.go:64 取 ext_sub 查询参数 → 177 FetchAndMergeExtSubs(extSubs)(http.go:129 内部对每个 subURL 调 FetchExternalSub→httpGet) → 182 allURIs=append(allURIs, extURIs...) → 199 ConvertURIsWithOptions → 215 c.String(200, result) 原样回写。FetchExternalSub(http.go:86-112) 把拉回正文 tryBase64Decode 后按行拆成 URI 并入 allURIs,构成"内容可反射"的 SSRF。鉴权在前(token 或 user+pwd,sub_handler.go:75-115),因此任何持订阅凭据的低权限代理用户即可让节点探测内网 HTTP 服务并取回响应。proxy_groups_url/rule_providers_url/rules_url(BuildConvertOptions→FetchXxxFromURL→httpGet)同样无校验,但这三条内容不直接反射。P2 合理。

### 23. [P2·confirmed] Clash 模板 proxy-group 经 ProxyGroup 结构体往返，丢失 filter/lazy/tolerance/icon 等字段

- **位置**：`pkg/proxy/core/subscription/converter/clash.go:792` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：ensureTemplateGroups 对外部模板拉回的每个 proxy-group 先 yaml.Marshal 节点再 Unmarshal 进 ProxyGroup{Name,Type,Proxies,URL,Interval}（clash.go:782-808），但 ProxyGroup 只有这 5 个字段。当某个模板 group 的 proxies 为空（占位组，常见）时走 792-808 分支：以只含 5 字段的 pg 重新 Marshal 覆盖原 Content[i]，模板里该组原有的 filter、lazy、tolerance、icon、disable-udp、hidden 等键被整体丢弃。同样的有损往返也发生在 appendGroupNameToAllOtherGroups（clash.go:1385-1411）——当模板不含名为 Manual 的组时（825 触发），会对所有其它 group 走一遍 ProxyGroup 往返，把它们的额外键全部抹掉。因为 ensureTemplateGroups 在 injectProxiesToTemplate 中无条件执行，即使 patchTemplateWithOptions（用泛型 map 保真）路径也已先被这一步破坏。触发条件：sub-converter 返回的 Clash 模板含带扩展字段的 group（url-test 常带 tolerance/lazy，select 常带 icon）。影响：生成的 Clash 配置丢失分组行为控制字段，用户侧表现为分组测速/图标/UDP 策略静默失效。
- **证据**：clash.go:834-942 ProxyGroup 仅 5 字段；clash.go:782-808 空组分支以 pg 重编码覆盖 Content[i]；clash.go:1385-1411 appendGroupNameToAllOtherGroups 同样 Marshal→ProxyGroup→Marshal
- **核实**：核实成立,且触发条件在本仓库链路下确为常态。ProxyGroup 结构体(clash.go:936-942)只有 Name/Type/Proxies/URL/Interval 五字段。ensureTemplateGroups(765-829)对每个模板 group 走 yaml.Marshal→Unmarshal 到 pg,仅当 len(pg.Proxies)==0 时(792)以 pg 重新 Marshal 覆盖 Content[i](798-808),此时 filter/lazy/tolerance/icon/hidden/disable-udp 等未建模字段被丢弃;非空 group 不覆盖故不受损。关键:模板用单个假节点 clashFakeSub 触发(700-703),clearClashTemplateNoise(721-733)按行删除所有含 FakeSub 的行,导致原本只引用假节点的 proxy-group 的 proxies 变空 → 恰好落入 792 空组分支,所以"占位组常见"属实。第二处 appendGroupNameToAllOtherGroups(1385-1412)在模板不含 Manual 组时(812/825 触发)对所有其它 group 做同样五字段往返,同样抹掉扩展键。整个过程在 injectProxiesToTemplate(757)中无条件执行,先于 patchTemplateWithOptions 的保真 map 路径(1024)。字段丢失机制由代码直接证明;所涉字段名(filter/lazy/tolerance/icon)为 Clash proxy-group 通用键,不依赖对第三方运行时行为的记忆断言。P2 合理。

### 24. [P2·confirmed] GetSubscription 逐 container 静默吞错，订阅缺协议无任何日志（遗留）

- **位置**：`pkg/proxy/core/subscription/manager.go:57` · 维度：错误处理 · 单元：SUB · 旧 review: SUB-01
- **问题**：GetSubscription 遍历 containers 聚合 spec 时，c.GetUserSubscriptions(req) 返回 err 即 continue（manager.go:57-60），既不记日志也不累积错误；doc 注释（manager.go:40）现已明确写成 'failed containers are silently skipped'。触发条件：某个 container（如 mihomo/xray）取用户订阅失败。影响：用户订阅里静默少了整类协议节点，排障时在服务端完全不可见，只能靠对比输出猜测。建议至少 log.Warn 出 kind 与 err。此为 2026-04-09 已记录问题，行为未变。
- **证据**：manager.go:57-60 `subs, err := c.GetUserSubscriptions(req); if err != nil { continue }`；manager.go:40 注释确认为有意静默
- **核实**：核实成立,且为旧 review SUB-01(P2,docs/review-2026-04-09.md:304,行 57-59)的重复,finder 已在 detail 中标注"2026-04-09 已记录"但未填 legacyId。manager.go:57-60 `subs, err := c.GetUserSubscriptions(req); if err != nil { continue }`,既不 log 也不累积错误;doc 注释(manager.go:40)确写为 'failed containers are silently skipped',确认有意静默。某 container 取订阅失败时,用户订阅静默缺失整类协议节点,服务端无日志可查。建议 log.Warn 出 kind+err 成立。P2 合理。

### 25. [P2·uncertain] ParseRuleParam 丢弃第4段规则参数（如 no-resolve/src），Clash 规则语义被改写

- **位置**：`pkg/proxy/core/subscription/converter.go:155` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：ParseRuleParam 用 strings.Split(param, ",") 拆分，但只取 parts[0]/parts[1]/parts[2] 三段（converter.go:160-164），第4段及之后被静默丢弃。Clash/mihomo 的规则常带第4字段修饰符，例如 `IP-CIDR,10.0.0.0/8,DIRECT,no-resolve`、`GEOIP,CN,DIRECT,no-resolve`、`SRC-IP-CIDR,...,...,src`。用户通过 ?rule= 或 rules_url 传入这类规则时，no-resolve 会被丢掉；随后 patchTemplateWithOptions/buildLocalConfig 用 fmt.Sprintf("%s,%s,%s", Type, Value, Policy) 重建（clash.go:1100、clash.go:1002），最终输出 `IP-CIDR,10.0.0.0/8,DIRECT`。缺失 no-resolve 会让本应跳过 DNS 解析的 IP 规则触发 DNS 查询，改变分流/防泄漏行为，属实质性正确性问题。
- **证据**：converter.go:155 `parts := strings.Split(param, ",")`；converter.go:160-164 仅返回 {Type: parts[0], Value: parts[1], Policy: parts[2]}，parts[3:] 未使用；重建处 clash.go:1100 `fmt.Sprintf("%s,%s,%s", ruleConfig.Type, ruleConfig.Value, ruleConfig.Policy)`
- **核实**：代码机制属实：converter.go:155 用 strings.Split 拆分后 160-164 仅取 parts[0..2]，parts[3:] 被静默丢弃；重建处 clash.go:1002 与 clash.go:1100 均以 fmt.Sprintf("%s,%s,%s") 只输出三段，且 rules_url 链路（http.go:213-233 FetchRulesFromURL→ParseRuleParam）确实可达。但该 finding 的实质危害完全依赖第三方（Clash/mihomo）规则语法存在第 4 字段修饰符（no-resolve/src）这一上游事实——我在仓库内全文检索 docs/、wiki/knowledge/、config 示例、测试用例均无 'no-resolve' 或任何 4 字段规则的记载；仓库自身文档（pkg/http/sub_handler.go help 文本 :413-421）声明的接口契约就是 rule=type,value,policy 三段。按 protocolRelated 核实规则，上游第 4 字段语义无仓库内材料佐证，不得凭记忆确证，故最高 uncertain。若上游语义被外部核实，P2 定级合理（静默截断无告警本身也值得修复）。

### 26. [P3·confirmed] SetMaxOpenConns(1) 与 WithTx 组合存在潜在自死锁模式：事务回调内任何经 db 直连的调用会无限期阻塞

- **位置**：`pkg/store/db.go:23` · 维度：并发 · 单元：STO
- **问题**：连接池上限为 1。WithTx 的事务在 Commit/Rollback 前独占这唯一连接；如果 fn 回调内（或回调调用链深处）任何代码走 db.DB().Exec/Query 而非 tx，database/sql 会等待空闲连接——同一 goroutine 内这个等待永远不会满足，且 WithTx 全部调用点都用 context.Background()（migrate.go:59、node_groups_store.go:70），没有超时兜底，结果是整个进程的存储层永久卡死（busy_timeout 是 SQLite 层参数，管不到 Go 连接池等待）。当前两处 WithTx 调用（迁移、node_groups Set）回调内只用 tx，暂无触发点，但没有任何机制防止后续在事务内复用 UserStore/InboundStore 方法（它们全部直接走 db.DB()）。建议：WithTx 增加带超时的 ctx 参数并在 Godoc 显式警告'回调内禁止使用 *DB'，或提供基于 tx 的 store 变体。
- **证据**：pkg/store/db.go:23 `sqlDB.SetMaxOpenConns(1)`；pkg/store/tx.go:11-27 WithTx 持连接直到 Commit；pkg/store/user_store.go 等所有 store 方法直接 `s.db.DB().Exec/Query`，无 tx 版本
- **核实**：机制实证成立但当前无触发点，降为 P3。事实核验：pkg/store/db.go:23 SetMaxOpenConns(1) 确认；pkg/store/tx.go:11-27 WithTx 持连接直到 Commit/Rollback 确认；全部 store 方法直接走 s.db.DB() 无 tx 变体确认；两处调用点（migrate.go:59、node_groups_store.go:70）均用 context.Background() 且回调内只用 tx 确认。我用临时测试实证了死锁机制：在 WithTx 回调内经 db.DB().ExecContext 执行插入，阻塞至 500ms context 超时（无超时则永久阻塞）。但 finder 自己也承认'暂无触发点'——现有代码没有任何路径在事务内走 db 直连，这是纯防御性/文档性诉求（Godoc 警告或 tx-based store 变体），不是现存 bug，P3 更合适。

### 27. [P3·confirmed] Save 用 INSERT OR REPLACE 更新用户时会把未列出的 created_at 重置为当前时间、level 重置为 0

- **位置**：`pkg/store/user_store.go:134` · 维度：正确性 · 单元：STO · ⚠️协议相关（需人工核对上游）
- **问题**：SQLite 的 INSERT OR REPLACE 在主键冲突时先 DELETE 旧行再 INSERT 新行，未出现在列清单里的列取表定义 DEFAULT 而不是保留旧值。users 表（migrations v1/v4 重建后仍含 created_at DATETIME DEFAULT datetime('now') 和 level INTEGER DEFAULT 0）在 Save 的 INSERT 列清单中不含这两列，因此每次用户更新（包括高频的流量累计写回）created_at 都被刷成当前时间，'创建时间'语义永久失真；level 同理归零（该列已无 Go 侧消费者，属死列）。inbounds 表的 Save（inbound_store.go:45-47）有同样的 created_at 重置问题。当前 Go 代码不读 created_at，故仅 P3，但审计/排障时这个字段是误导性的。建议改为 INSERT ... ON CONFLICT(username) DO UPDATE SET ...（SQLite 3.24+ upsert，modernc 支持），或在列清单中带上 created_at 并用 COALESCE 保留。
- **证据**：pkg/store/user_store.go:133-145 `INSERT OR REPLACE INTO users (...列清单不含 created_at/level...) VALUES (..., datetime('now'))`；pkg/store/migrations/migrations.go:50-74 v4 重建后的表仍含 level 与 created_at；SQLite 文档：REPLACE 冲突处理先删除旧行，新行未指定列取默认值
- **核实**：protocolRelated 条目，通过仓库内实证测试确证上游行为（非凭记忆）：用 modernc.org/sqlite v1.46.1 建含 created_at DEFAULT datetime('now')、level DEFAULT 0 的 users 表，插入 created_at='2020-01-01'、level=7 的行，经 SQLiteUserStore.Save 一次后实测 created_at 变为当前时间（2026-07-10T08:40:22Z）、level 归 0——REPLACE 先删后插、未列出列取 DEFAULT 的语义完全复现。代码核验：user_store.go:133-145 INSERT OR REPLACE 列清单确实不含 created_at/level；migrations v4 重建后表仍含这两列（migrations.go:50-74）；inbound_store.go:45-47 同样不含 created_at。grep 确认 Go 侧无 created_at/level 消费者，仅审计误导，P3 恰当。

### 28. [P3·confirmed] busy_timeout/foreign_keys/cache_size PRAGMA 只施加在建库时的那条连接上，连接被池重建后静默失效

- **位置**：`pkg/store/db.go:31` · 维度：资源 · 单元：STO · ⚠️协议相关（需人工核对上游）
- **问题**：Open 通过 sqlDB.Exec 逐条执行 PRAGMA。除 journal_mode=WAL 是持久化到库文件的属性外，busy_timeout、foreign_keys、cache_size 都是 per-connection 状态。database/sql 在驱动返回 ErrBadConn 或连接被淘汰后会新建连接，新连接不会重放这些 PRAGMA：busy_timeout 归 0 后，任何外部进程（sqlite3 CLI、备份工具）短暂持锁都会直接得到 SQLITE_BUSY 而不是等待 5s。当前 MaxOpenConns(1)+默认 MaxIdleConns(2) 使初始连接通常常驻，所以问题是潜伏的而非必现。modernc.org/sqlite 支持 DSN 级 pragma（`file:path?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)`），会在每条新连接建立时自动应用，是上游推荐做法；依据：modernc.org/sqlite 驱动文档的 _pragma query parameter 语义与 database/sql 连接池重建行为。
- **证据**：pkg/store/db.go:25-36 `pragmas := []string{"PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000", ...}; for ... sqlDB.Exec(p)` —— 仅在 Open 时对当前连接执行一次
- **核实**：protocolRelated 条目，通过仓库内实证确证（非凭记忆）。实测：Open 后当前连接 PRAGMA busy_timeout=5000；通过 SetMaxIdleConns(0) 强制连接池淘汰重建后，新连接上 busy_timeout=0——per-connection PRAGMA 在池重建后静默失效的核心主张完全复现。修复建议的依据也在仓库内核实：go.mod 锁定的 modernc.org/sqlite@v1.46.1 driver.go:47-50 明文文档化 `?_pragma=foreign_keys(1)` DSN 参数（每条新连接自动执行），sqlite.go:143 有对应实现。代码核验 db.go:25-36 确为 Exec 逐条执行、仅作用一次。当前 MaxOpenConns(1) 下初始连接常驻使问题潜伏，P3 恰当。

### 29. [P3·confirmed] Load/Get/ListByGroup 三处手工复制 21 列 Scan + expiry 双格式解析 + port_mappings 解析，加列时必然三处同改

- **位置**：`pkg/store/user_store.go:67` · 维度：架构 · 单元：STO
- **问题**：同一段行→contracts.User 的物化逻辑（21 列 Scan、expiry_time 先 RFC3339 后 '2006-01-02 15:04:05' 回退、port_mappings 容错解析）在 Load（67-99 行）、Get（190-222 行）、ListByGroup（252-281 行）各存在一份逐字拷贝，SELECT 列清单也是三份。历史上该表已经历 v3/v5/v6/v12/v13/v14 六轮加列/改名，每轮都要同步 3 个 SELECT + 3 个 Scan + Save 的 INSERT 共 7 处；漏改任何一处就是列错位型运行时错误（Scan 目标数不匹配会报错，但列序换位类错误可能静默串值）。建议提取 `const userColumns` + `scanUser(scanner interface{ Scan(...) error }) (*contracts.User, error)` 单点维护。属维护性/一致性问题，无当前行为 bug。
- **证据**：pkg/store/user_store.go:45-55、174-184、229-240 三份相同 SELECT 列清单；67-79、190-200、252-262 三份相同 Scan；81-99、207-222、266-281 三份相同 expiry/port_mappings 解析
- **核实**：逐行核对 pkg/store/user_store.go：Load(45-55/67-79/81-99)、Get(174-184/190-200/207-222)、ListByGroup(229-240/252-262/266-281) 确为三份逐字相同的 21 列 SELECT+Scan+expiry 双格式解析+port_mappings 容错解析；数了列清单恰好 21 列（username..deletion_state），Save 的 INSERT(133-155) 是第 4 份列清单。migrations.go 确认 v3/v5/v6/v12/v13/v14 六轮加列/改名历史。无当前行为 bug，属维护性问题，P3 恰当。docs/review-2026-04-09.md 中无任何 pkg/store 条目，非重复。

### 30. [P3·confirmed] cluster_users 表（v7-v10 建表+三索引）在代码中已无任何读写方，成为可能残留旧凭据的死 schema

- **位置**：`pkg/store/migrations/migrations.go:87` · 维度：架构 · 单元：STO
- **问题**：migrations v7 创建 cluster_users（含 password 明文列）并在 v8-v10 建三个索引，但全仓库（pkg、cmd，排除测试与 migrations 本身）grep 不到任何对 cluster_users 的 SQL 读写——集群同步字段已在 v12 合并进 users 表（target_group/updated_at_us/origin_node/hash）+ v13 deletion_state。后果：老部署升级后 cluster_users 里的历史用户名+password 永久残留且无人维护/清理（结合 DB 文件权限问题，泄露面扩大）；新部署则平白多一张空表三个索引。迁移记录本身不能删（会造成 fresh 安装的版本 gap），但可以加一个 v15 `DROP TABLE IF EXISTS cluster_users` 迁移完成清场。
- **证据**：pkg/store/migrations/migrations.go:87-113 v7-v10 DDL；`grep -rn "cluster_users" --include="*.go" pkg cmd`（排除测试/migrations）无任何命中，仅 rpc proto 中不相关的 need_cluster_users 字段
- **核实**：migrations.go:87-113 确认 v7 建 cluster_users（含 password TEXT NOT NULL 明文列）、v8-v10 建三索引。全仓库 grep（pkg+cmd，排除测试与 migrations）对 cluster_users 的命中仅有：rpc_server.pb.go 的 need_cluster_users proto 字段（不相关）和 appconfig/config.go:201 一条注释——没有任何 SQL 读写方。集群同步字段确已在 v12(users 表加 target_group/updated_at_us/origin_node/hash)+v13(deletion_state) 合并进 users 表。老部署残留旧凭据、新部署多一张空表的推断成立。P3 恰当。

### 31. [P3·confirmed] store 包仍用标准库 log 直写 stderr，与项目已统一到 pkg/log 的日志路由方向不一致

- **位置**：`pkg/store/tx.go:19` · 维度：架构 · 单元：STO
- **问题**：tx.go（rollback 失败）与 user_store.go 三处 port_mappings 解析告警使用标准库 log.Printf，绕过项目自有的 pkg/log（logger.go/slog.go，含级别与输出路由）。近期 commit 185f62f 已专门把 certmgmt 的日志改走 pkg/log，说明统一日志出口是当前项目约定；store 层这些告警恰是排障时最需要出现在统一日志流里的（rollback 失败、持久化数据损坏）。纯一致性问题，不影响行为。
- **证据**：pkg/store/tx.go:6 `"log"`、:19 `log.Printf("store.WithTx: rollback failed: %v", rbErr)`；pkg/store/user_store.go:7、:97、:220、:279；对照 git log 185f62f "fix(certmgmt): route logs through pkg/log"
- **核实**：核实 pkg/store/tx.go:6 import "log"、:19 log.Printf(rollback failed)；user_store.go:7 import "log"，:97/:220/:279 三处 log.Printf(port_mappings 解析告警)。pkg/log 包存在（logger.go/slog.go/options.go），且 git log 近期 commit 185f62f 'fix(certmgmt): route logs through pkg/log' 证实统一日志出口是当前项目方向。纯一致性问题，P3 恰当。

### 32. [P3·confirmed] Provider/SQLiteProvider 全仓库零消费者，架构约束 #7 所述的 Provider 抽象实际是死代码

- **位置**：`pkg/store/provider.go:10` · 维度：架构 · 单元：STO
- **问题**：provider.go 定义的 Provider 接口和 SQLiteProvider 在整个仓库（含测试）没有任何引用——grep 'store.Provider|SQLiteProvider' 仅命中定义文件本身。实际接线全部走 StoreManager（cmd/server.go:107 NewStoreManager → UserStore()/InboundStore()），NodeGroupsStore 更是绕过两者、在 cmd/server.go:213 直接用 storeMgr.DB() 手工构造，且 StoreManager 与 Provider 都不包含它。PROJECT_GUIDE 架构约束 #7 声称 'DB 操作经 pkg/store/manager 的 Provider+Tx 抽象'，与现状漂移。建议要么删除 Provider（并修订约束文档措辞为 StoreManager），要么把 NodeGroupsStore 纳入 StoreManager/Provider 统一出口，消除 cmd 层直接摸 *store.DB 的旁路。
- **证据**：pkg/store/provider.go:4-27（定义）；全仓 grep 无消费者；cmd/server.go:213 `ngStore := store.NewSQLiteNodeGroupsStore(storeMgr.DB())` 绕过统一入口。
- **核实**：确证。grep 全仓（含 _test.go）'SQLiteProvider|store.Provider|NewSQLiteProvider' 仅命中 pkg/store/provider.go:4-27 定义本身，零消费者。实际接线核实：cmd/server.go:107 storeMgr, err := store.NewStoreManager(cfg.Store.DSN, migrations.All)，pkg/store/manager.go 的 StoreManager 只持有 userStore/inboundStore 两个字段（不含 NodeGroupsStore，也不引用 Provider）；cmd/server.go:213 ngStore := store.NewSQLiteNodeGroupsStore(storeMgr.DB()) 确实绕过 StoreManager/Provider 直接摸 *store.DB。PROJECT_GUIDE.md:74 架构约束 #7 原文为'DB 操作通过 pkg/store/manager 的 Provider + Tx 抽象'，与代码现状（Provider 是死代码、实际走 StoreManager）漂移。P3 恰当（死代码+文档漂移，无运行时影响）。与旧 review 无重复。

### 33. [P3·confirmed] pkg/store 使用 stdlib log.Printf，绕过项目统一的 pkg/log

- **位置**：`pkg/store/user_store.go:97` · 维度：错误处理 · 单元：STO
- **问题**：user_store.go 三处 port_mappings 告警（97/220/279 行）和 tx.go:19 的 rollback 失败告警都用标准库 log.Printf。项目日志约定是统一走 pkg/log（近期 commit 185f62f 'fix(certmgmt): route logs through pkg/log' 即为同类修正，usermanager 等消费方也已导入 pkg/log），stdlib log 输出不经统一格式/级别/落盘管道，这几条恰是数据损坏类的重要告警，容易在生产日志采集中丢失。应替换为 pkg/log 的 Warn 级别调用。
- **证据**：pkg/store/user_store.go:7 `"log"` 导入及 97/220/279 行 log.Printf；pkg/store/tx.go:6,19 同样；对照 pkg/proxy/usermanager/usermanager.go:11 已使用 github.com/lureiny/v2raymg/pkg/log。
- **核实**：确证。pkg/store/user_store.go:7 导入 stdlib "log"，97/220/279 三行 log.Printf 输出 port_mappings 解析失败告警（数据损坏类告警且被吞掉不返回错误）；pkg/store/tx.go:6 导入、:19 log.Printf 输出 rollback 失败。项目确有统一日志包 pkg/log（logger.go/slog.go/options.go），pkg/proxy/usermanager/usermanager.go:11 已导入 github.com/lureiny/v2raymg/pkg/log，且近期 commit 185f62f 'fix(certmgmt): route logs through pkg/log' 证实统一走 pkg/log 是项目约定。stdlib log 绕过统一格式/级别管道属实。P3 恰当。与旧 review 无重复。

### 34. [P3·confirmed] 21 列 SELECT+Scan+expiry 双格式解析+port_mappings 容错在 Load/Get/ListByGroup 三处手工复制

- **位置**：`pkg/store/user_store.go:45` · 维度：架构 · 单元：STO
- **问题**：Load（45-108 行）、Get（174-225 行）、ListByGroup（229-290 行）各自维护一份完全相同的 21 列列名清单、Scan 目标列表、expiry_time 双格式解析和 port_mappings 解析逻辑，且必须与 Save 的 INSERT 列序（134-145 行）严格对齐。下次加持久化字段（集群同步演进中很可能发生）需要同步改 4 处 + 迁移 DDL，漏改任何一处都是列错位或字段静默丢失，而现有测试（见 test-gap 条目）接不住这种回归。建议抽取共享的列名常量和 scanUser(rowScanner) 帮助函数，把三份拷贝收敛为一份。
- **证据**：pkg/store/user_store.go:45-55 / 174-184 / 229-240 三段逐字相同的 SELECT 列清单；67-77 / 190-200 / 252-262 三段相同 Scan；81-99 / 207-222 / 266-281 三段相同解析逻辑。
- **核实**：确证。逐行比对 pkg/store/user_store.go：Load(45-55)/Get(174-184)/ListByGroup(230-240) 三段 21 列 SELECT 列清单逐字相同；Scan 目标列表三份拷贝（67-77/190-199/252-261）；expiry_time RFC3339+SQLite datetime 双格式回退解析三份（81-91/207-216/266-275）；port_mappings JSON 容错解析三份（94-99/218-222/277-281）。且列序必须与 Save 的 INSERT OR REPLACE 列清单（134-145）及 21 个占位符严格对齐，新增持久化字段需同步改 4 处 Go 代码 + migrations DDL，漏改即列错位或静默丢字段。抽取 scanUser 帮助函数的建议合理。P3 恰当（可维护性问题，当前无 bug）。与旧 review 无重复。

### 35. [P3·confirmed] InboundError 未实现 Is(target)，WithCause 派生实例无法被 errors.Is 匹配到 sentinel

- **位置**：`pkg/proxy/core/inbound/inbound.go:116` · 维度：错误处理 · 单元：CTR · 旧 review: I-04
- **问题**：InboundError 只实现 Error()/Unwrap()/WithCause()（inbound.go:105-123），没有 Is(target error) bool。sentinel（ErrInboundTagRequired 等，inbound.go:90-96）是具体 *InboundError 指针。经 WithCause(inbound.go:117) 派生的新实例是不同指针，Unwrap 又返回内层 Cause 而非 sentinel，故 errors.Is(wrapped, ErrInboundTagRequired) 恒为 false。mihomo/inbound.go:128 注释明确宣称“Callers can use errors.Is”，与此实现矛盾。当前 WithCause 尚无生产调用点（仅定义），属于潜伏缺陷：一旦有人按注释用 WithCause+errors.Is 就会静默失配。应补 Is 方法比较 Code 或按值语义匹配。
- **证据**：inbound.go:112 `Unwrap() { return e.Cause }`、inbound.go:117 WithCause 返回新指针；无 Is 方法；mihomo/inbound.go:128 “Callers can use errors.Is”
- **核实**：核实成立，一处证据引用有误。InboundError（inbound.go:98-123）确实只有 Error/Unwrap/WithCause，无 Is(target) 方法；sentinel（inbound.go:90-96）是具体指针，WithCause(117-123) 返回新指针且 Unwrap 返回内层 Cause，故 errors.Is(wrapped, ErrInboundTagRequired) 恒 false，推理正确。全仓 grep 确认 WithCause 无生产调用点，属潜伏缺陷，P3 恰当。但 finder 引用的 mihomo/inbound.go:128『Callers can use errors.Is』实际针对的是 ErrProtocolNotSupported/ErrMissingCredential（inbound.go:131-142，均为 errors.New 普通 sentinel，errors.Is 对它们工作正常），与 InboundError 无关，该『矛盾』证据不成立，不影响主结论。与旧 review I-04 重复（10-legacy-status.md:158 标 still-present），finder 未标 legacyId。

### 36. [P3·confirmed] ValidateCombination 名不副实且为死代码：只逐字段校验合法性，从不校验“组合”，且无生产调用者

- **位置**：`pkg/proxy/core/contracts/protocol.go:138` · 维度：架构 · 单元：CTR
- **问题**：ValidateCombination(protocol.go:138-149) 顾名思义应校验 (Protocol,Transport,Security) 三元组是否可共存，但实现只对三者各自 IsValid()，任意“单独合法但组合非法”的搭配（如 hysteria2+ws、shadowsocks+reality）都会通过。函数头注释还写“Provider-specific restrictions are handled by InboundAdapter”，把真正的组合约束外推给了别处，使本函数沦为三次 IsValid 的包装。全仓 grep 显示除 protocol.go 与其 *_test.go 外无任何调用点——它是死代码。要么补上真正的组合矩阵，要么删除以免误导后续实现者以为组合已被把关。
- **证据**：protocol.go:139-147 三个独立 `if !x.IsValid()`；`grep -rn ValidateCombination pkg cmd` 仅命中 protocol.go 定义处，无生产调用
- **核实**：事实核实成立，但需注明历史裁定。protocol.go:138-149 确实只做三次独立 IsValid，不校验任何组合矩阵；全仓 grep 确认 ValidateCombination 除 protocol.go 定义与 protocol_test.go 外无任何调用点，死代码属实。但本条与旧 review C-02 重复，且 docs/review-2026-07-10/10-legacy-status.md:135 已将 C-02 裁定为 **fixed**：理由是 protocol.go:136-137 注释已明示设计意图（组合约束下放 InboundAdapter），且真正的跨字段组合校验已集中到 protocolparams 各协议 validator（params_anytls.go:83-84、params_hysteria2.go:59-60、params_tuic.go:79-80 拒 reality，params_trojan.go:61-64 强制 tls/reality）。因此『名不副实/组合未把关』部分是对已裁定 fixed 条目的重提，残余可行动点仅剩『无调用者的死代码应删除或接线』。P3 恰当。

### 37. [P3·confirmed] generateSelfSigned 把未净化的 server_name 直接拼进磁盘文件名，存在路径穿越/写失败风险

- **位置**：`pkg/proxy/core/params/defaults.go:282` · 维度：安全 · 单元：CTR
- **问题**：generateSelfSigned(defaults.go:271-291) 用 `filepath.Join(scratchDir, "ss-"+commonName+"-"+suffix+"-cert.pem")` 构造证书/密钥落盘路径，commonName 来自 params["server_name"]（resolveCertSource:226），而 server_name 经 RPC extra_params 的 default 分支原样透传（end_node_inbound.go:284）。若 server_name 含 "/" 或 "../"，filepath.Join 会 Clean 出 scratchDir 之外的路径，导致把 0600 证书/密钥写到非预期目录（含覆盖），或因中间目录不存在而 os.WriteFile 失败使 FastAdd 报错。虽然 FastAdd 是管理员接口（信任边界较高），但仍应对 commonName 做 filepath.Base/字符白名单净化，避免文件名注入。
- **证据**：defaults.go:282 `certFile = filepath.Join(scratchDir, "ss-"+commonName+"-"+suffix+"-cert.pem")`；commonName←server_name←end_node_inbound.go:284 default 透传
- **核实**：核实成立。defaults.go:282-283 用 filepath.Join(scratchDir, "ss-"+commonName+...) 构造落盘路径，commonName 直接来自 params["server_name"]（defaults.go:226-229），全链路无任何净化。触发路径完整：pkg/rpc/server/end_node_inbound.go 的 FastAddInbound 中，GetDomain()=="" 且 security==tls 时自动置 self_signed=true（253-255 行），extra_params 的 server_name 走 default 分支原样透传（282-286 行 reqParams[k]=v），随后 FillDefaults（299 行）→ resolveCertSource 第 4 分支 → generateSelfSigned。server_name 含 "../" 时 filepath.Join 的 Clean 会把 0600 的证书/密钥写到 scratchDir（mihomo 为 CertScratchDir/DataDir）之外，或因中间目录不存在使 os.WriteFile 失败。仅限管理员 RPC 接口且需显式构造恶意 server_name，P3 恰当。docs/review-2026-04-09.md 无重复条目。

### 38. [P3·confirmed] hashShort 文档声称 SHA-256/“SHA-like”，实际实现是 FNV-1a，注释自相矛盾

- **位置**：`pkg/proxy/core/params/defaults.go:389` · 维度：正确性 · 单元：CTR
- **问题**：hashShort 的 Godoc（defaults.go:389-392）写“first 16 hex chars of a SHA-like digest of s. Uses SHA-256 via stdlib”，而函数体注释（396）又写“use a simple FNV-1a fold over the bytes for speed”，实际代码（397-402）确实是 FNV-1a。文档与实现直接冲突，会误导读者以为 PEM 证书文件名用的是抗碰撞的 SHA-256。此哈希用于 writePEMToScratch(256) 的文件名去重（相同内容复用同名文件），FNV-1a 的碰撞概率高于 SHA-256——虽然对“内容相同才复用”的用途影响有限，但注释必须与实现一致，否则日后有人据错误注释假设其抗碰撞性。
- **证据**：defaults.go:390-391 注释 “Uses SHA-256 via stdlib” vs defaults.go:396 “use a simple FNV-1a fold” 与 397 `var h uint64 = 14695981039346656037`（FNV-1a offset basis）
- **核实**：核实成立且证据直接。defaults.go:389-392 的 Godoc 声称 "first 16 hex chars of a SHA-like digest" 且 "Uses SHA-256 via stdlib"，而函数体 396 行注释改口 "use a simple FNV-1a fold over the bytes for speed"，397-401 行实现确为 FNV-1a（offset basis 14695981039346656037、prime 1099511628211，且输出为 %016x 即 16 个 hex 字符的 uint64，根本不是任何 SHA 摘要的截断）。该哈希用于 writePEMToScratch（256-258 行）的证书文件名去重。功能上因"碰撞仅意味着复用同名文件、内容相同才安全"的前提被 FNV-1a 削弱（不同 PEM 碰撞会导致 writeFileIfChanged 覆盖另一个 inbound 正在使用的证书文件），但概率极低；主要问题是注释与实现直接矛盾。P3 恰当。docs/review-2026-04-09.md 无重复条目。

### 39. [P3·confirmed] parseSS 手写 udp 布尔解析，与全包统一的 optionalBool 语义不一致

- **位置**：`pkg/proxy/core/params/protocolparams/params_ss.go:42` · 维度：正确性 · 单元：CTR
- **问题**：parseSS(params_ss.go:42-55) 自己 switch 解析 udp，只识别 bool 及字符串 "true"/"1"/"false"/"0"，其余（如 "yes"/"no"/"True"/带空格）一律落到默认 true。而本包其它解析器统一用 optionalBool(transport.go:123-140)，它额外识别 "yes"/"no" 并做 ToLower+TrimSpace。同一份 raw map 里 udp="yes" 会被 parseSS 当默认 true（碰巧对），udp="No" 却也被当 true（错误），而同请求里 self_signed="No" 经 optionalBool 会正确得到 false。布尔解析在包内二义，建议 parseSS 改用 optionalBool 保持一致。
- **证据**：params_ss.go:46-53 仅 case "false","0"/"true","1"，无 ToLower/TrimSpace/yes/no；对比 transport.go:132-137 optionalBool 支持 "yes"/"no" 且做归一化
- **核实**：核实成立。params_ss.go:41-55 手写 udp 解析，string 分支仅匹配字面 "false"/"0"/"true"/"1"，无 ToLower/TrimSpace，也不识别 "yes"/"no"，未匹配值静默落回默认 true；而同包 transport.go:123-140 的 optionalBool 对 string 做 strings.ToLower(strings.TrimSpace(x)) 归一化并识别 "yes"/"no"。同一 raw map 中 udp="No"/"FALSE"/" false " 会被 parseSS 错当 true，而 self_signed 等键经 optionalBool 能正确解析，包内布尔语义确实二义。属一致性/健壮性问题，无直接错误路径（默认 true 与文档契约一致），P3 恰当。docs/review-2026-04-09.md 无重复条目。

### 40. [P3·confirmed] StartAll/StopAll/RestoreAll 持 RLock 跨外部进程启动与状态恢复全程，阻塞窗口大且有再入死锁隐患

- **位置**：`pkg/proxy/core/container/manager.go:107` · 维度：并发 · 单元：CON
- **问题**：StartAll（manager.go:106-124）在整个循环期间持有 m.mu.RLock，循环体内执行 c.Start()（exec 外部进程 + 各容器自身的就绪等待）和 Restorable.Restore(ctx)（重建转发规则/inbound 记录，涉及 DB 与网络），单容器可达秒级、多容器串行累加。期间：1) 任何写锁请求（LoadFromConfig/SetForTest）被阻塞，且按 Go sync.RWMutex 语义，排队中的写者会使后续所有 RLock（rpc handler 高频调用的 Get()/Types()）一并阻塞——cmd/server.go:229 先启动 rpcServer 再在 :264 调 StartAll，启动期 rpc 请求存在长时间卡死窗口；2) 若未来任何容器的 Start/Restore 回调链再入 ContainerMgr 写方法即刻死锁。StopAll/RestoreAll（manager.go:129-159）同样持锁跨慢操作。修复：锁内把 instances 快照成 slice，锁外逐个 Start/Restore/Stop。
- **证据**：manager.go:106-124 `m.mu.RLock(); defer m.mu.RUnlock(); for kind, c := range m.instances { if err := c.Start(); ... r.Restore(ctx) ... }`；cmd/server.go:229 `go rpcServer.Start()` 先于 cmd/server.go:264 `containerMgr.StartAll(ctx)`
- **核实**：结构事实确证：manager.go:106-124 StartAll 全程持 m.mu.RLock 跨 c.Start()（含 xray Start 内 500ms sleep、进程 exec）和 Restorable.Restore；StopAll/RestoreAll（manager.go:129-159）同样。cmd/server.go:229 `go rpcServer.Start()` 先于 :264 StartAll 也属实。但危害被高估：grep 确认 LoadFromConfig 全仓唯一生产调用在 cmd/server.go:175（StartAll 之前，单线程启动段），SetForTest 仅测试用——运行期不存在任何写锁请求，因此 "排队写者使 rpc 的 RLock 卡死" 的场景在当前代码中不可达（无写者时多个 RLock 可自由并发，rpc Get()/Types() 不会被 StartAll 阻塞）；再入死锁也是假设性的（grep 确认所有容器实现均不持有 ContainerMgr 引用）。作为锁跨慢操作的潜在隐患（未来加运行期 LoadFromConfig/热重载即触发）值得修，但当前无实际影响，降为 P3。

### 41. [P3·confirmed] legacy globalRegistry 已退化为只写死代码，唯一写入方是 xray init() 中带硬编码路径且失败即 panic 的默认 factory，应整体删除

- **位置**：`pkg/proxy/core/container/registry.go:24` · 维度：架构 · 单元：CON · 旧 review: CT-09
- **问题**：全仓生产代码对 legacy registry 的唯一交互是 pkg/proxy/containers/xray/exec_runner.go:2599-2617 的 init() 调 RegisterContainerFunc；GetContainer/NewContainer/SetContainer/RegisterSingleton/IsRegistered 等读取入口已无任何生产调用方（grep 全仓仅剩 registry_test.go 和注释），ContainerMgr 只走新的 factoryMap（manager.go:63 GetFactory）。这意味着：1) 该 init 注册的 factory 携带硬编码 BinaryPath=/usr/local/bin/xray、ConfigFilePath=/tmp/xray-default.json，一旦未来有人调用 GetContainer(ContainerXray) 会拿到与真实配置完全无关的实例，且 NewExecutor 失败时直接 panic（exec_runner.go:2613）；2) registry 自身还带着 CT-10 记录的双锁数据竞争（instances map 在 RegisterSingleton/UnregisterContainer/GetRegisteredTypes 中由 mu 保护，在 GetContainer/SetContainer/IsSingleton 中由 singletonM 保护，两把锁互不排斥，理论上可触发 concurrent map read/write panic）。既然已无消费者，建议删除 registry.go 的 legacy 部分及 xray 的 init 注册，一并消除 CT-09/CT-10。
- **证据**：registry.go:22-28 globalRegistry 定义；registry.go:106-108 GetContainer 用 singletonM 读 instances vs registry.go:53-63 RegisterSingleton 用 mu 写 instances；pkg/proxy/containers/xray/exec_runner.go:2603-2614 `container.RegisterContainerFunc(... BinaryPath: "/usr/local/bin/xray" ... panic(...))`；manager.go:63 仅用 GetFactory
- **核实**：代码证据确证：grep 全仓（排除 _test.go）确认 legacy registry 的读取入口（GetContainer/NewContainer/SetContainer/RegisterSingleton/IsRegistered 等）无任何生产调用方（所有 GetContainer 命中均为 proto getter），唯一写入方是 exec_runner.go:2599-2617 的 init()，其 factory 闭包携带硬编码 BinaryPath=/usr/local/bin/xray、ConfigFilePath=/tmp/xray-default.json 且 NewExecutor 失败即 panic；ContainerMgr 只走 factoryMap（manager.go:63 GetFactory）。双锁竞争属实：registry.go 中 instances map 在 RegisterSingleton(:53-63)/UnregisterContainer(:70-82)/GetRegisteredTypes(:137-147) 由 mu 保护，在 GetContainer(:106-108)/SetContainer(:129-131)/IsSingleton(:169-171) 由 singletonM 保护，两锁互不排斥。与旧 review CT-09/CT-10 重复（finder 正文提及但未标 legacyId）。由于已无任何生产消费者，panic 与数据竞争均不可达，属死代码清理建议，降为 P3。

### 42. [P3·confirmed] InboundAdapter 全仓无生产调用方，且 inboundAdapterImpl.Config() 在读路径写共享 Extensions map（并发 map 写风险）

- **位置**：`pkg/proxy/core/container/inbound.go:66` · 维度：资源 · 单元：CON
- **问题**：InboundAdapter/ToInbound 在生产代码中没有任何调用点（grep 全仓仅 contracts/protocol.go:137 的注释提及），属于死代码。若保留，它还有一个实际缺陷：inboundAdapterImpl.Config()（inbound.go:66-75）每次调用都把 a.spec.Extensions 的键值合并写入 DefaultInbound.Config() 返回的原始指针的 Extensions map（core/inbound/default.go:58 直接返回 config_），即在读方法里持续污染底层共享状态；两个 goroutine 并发调 Config() 就是对同一 map 的并发写，可触发 runtime 的 concurrent map writes panic。另外 ToInbound 经 NewDefaultInbound（default.go:23-28）会把空 tag/0 端口静默替换为 "default-inbound"/10000，而外层 Tag()/Port() 覆盖方法又优先返回 spec 原值，导致 Tag() 与 Config().Tag 可能不一致（与 I-01/I-05 同根）。建议直接删除 adapter，或改为构造时一次性合并到副本。
- **证据**：inbound.go:66-75 `func (a *inboundAdapterImpl) Config() *inbound.Config { cfg := a.DefaultInbound.Config(); ... for k, v := range a.spec.Extensions { cfg.Extensions[k] = v } }`；core/inbound/default.go:58 `func (i *DefaultInbound) Config() *Config { return i.config_ }`；全仓 grep 无 ToInbound 生产调用
- **核实**：代码证据确证：grep 全仓（排除 _test.go 与定义文件）确认 InboundAdapter/ToInbound 无生产调用点（仅 contracts/protocol.go:137 注释提及），属死代码。缺陷描述属实：inbound.go:66-75 的 Config() 每次调用把 a.spec.Extensions 写入 DefaultInbound.Config() 返回的裸指针（core/inbound/default.go:58 直接 return i.config_）的 Extensions map，读方法写共享 map，同一 adapter 实例并发 Config() 构成 concurrent map writes（Go race，可 panic）——但注意该 map 是每 adapter 实例私有（ToInbound 每次 NewDefaultInbound 新建），非全局共享。一处细节不准："Tag() 与 Config().Tag 可能不一致" 在 adapter 自身路径上不成立——ToInbound 把 spec.Tag 原样传入 NewDefaultInbound，空 tag 时两侧都是 default-inbound、非空时两侧相同；不一致仅在有人调用嵌入的 SetTag 时出现（即 I-01 的老问题）。核心结论（死代码 + 读路径写 map）成立，P3 恰当。

### 43. [P3·confirmed] types.go 便捷常量列表缺 ContainerMihomo，与已注册的容器类型不同步

- **位置**：`pkg/proxy/core/container/types.go:11` · 维度：正确性 · 单元：CON
- **问题**：types.go:11-16 的便捷常量只导出 Xray/V2ray/Hysteria/Snell 四种，而 contracts.ContainerMihomo（contracts/protocol.go:123）已存在且 mihomo 容器已通过 RegisterFactory 正式注册（pkg/proxy/containers/mihomo/register.go:11）、在 contracts.ContainerType 的合法值校验中也包含 mihomo（protocol.go:128）。这个"便捷列表"实际上已经过期：依赖它做类型枚举或 switch 的调用方会漏掉 mihomo。要么补齐 ContainerMihomo，要么删掉这组冗余别名统一直接用 contracts 常量，避免两处清单继续漂移。
- **证据**：types.go:10-16 `const ( ContainerXray ... ContainerSnell ... )`（无 mihomo）；contracts/protocol.go:123 `ContainerMihomo ContainerType = "mihomo"`；containers/mihomo/register.go:11 `container.RegisterFactory(contracts.ContainerMihomo, &mihomoFactory{})`
- **核实**：核实成立。types.go:11-16 便捷常量仅含 Xray/V2ray/Hysteria/Snell；contracts/protocol.go:123 已有 ContainerMihomo 且 IsValid()（protocol.go:128）包含 mihomo；containers/mihomo/register.go:11 通过 RegisterFactory(contracts.ContainerMihomo, ...) 正式注册。全仓 grep 显示 mihomo 相关代码只能直接引用 contracts.ContainerMihomo，而 hotreload/hotupdate 等旧调用方用的是 container.ContainerXray 别名，两处清单确已漂移。P3 恰当。

### 44. [P3·confirmed] process.go 的 ProcessRunner 兼容别名与 inbound.go 的 Deprecated InboundConfig 系列全仓无引用，纯死代码

- **位置**：`pkg/proxy/core/container/process.go:14` · 维度：架构 · 单元：CON
- **问题**：process.go 提供的 ProcessRunnerConfig/ProcessRunner/NewProcessRunner 三个 Deprecated 别名，以及 inbound.go:80-166 的 Deprecated InboundConfig、NewInboundConfig、ErrInbound* 错误值和 InboundError 类型，在仓库中均无任何外部引用（grep `container.ProcessRunner|container.InboundConfig|container.ErrInbound` 零命中）；所有容器实现已直接使用 tools/process 和 core/inbound。这些兼容层继续留存会误导新代码引用旧入口（例如 inbound.go 的 InboundError 没有 Code 字段，与 core/inbound.InboundError 语义已分叉），建议删除 process.go 全文件及 inbound.go 的 Deprecated 段。
- **证据**：process.go:12-22 三个 Deprecated 别名；inbound.go:80-166 Deprecated InboundConfig/错误值；全仓 grep `container\.ProcessRunner|container\.NewProcessRunner|container\.InboundConfig|container\.ErrInbound` 无 core/container 之外命中
- **核实**：核实成立。全仓 grep `container\.(ProcessRunner|NewProcessRunner|InboundConfig|NewInboundConfig|ErrInbound.*|InboundError)` 在 core/container 之外零命中；包内测试也无引用（仅同名但不同标识符的 RemoveInboundConfig 等接口方法）。process.go:12-22 三个 Deprecated 别名与 inbound.go:80-166 的 Deprecated InboundConfig/错误值/InboundError 均为死代码。语义分叉属实：core/inbound.InboundError（pkg/proxy/core/inbound/inbound.go:99-105）有 Code 字段和 Unwrap/WithCause，container.InboundError（inbound.go:160-166）只有 Message。注意 inbound.go 中非 Deprecated 部分（Inbound 别名、InboundAdapter）仍在使用，finding 正确地只圈定 Deprecated 段。

### 45. [P3·confirmed] BaseContainer 并发测试只断言最终状态，未校验 runFunc/stopFunc 配对与慢 hook 交错，无法捕获已知状态机竞态

- **位置**：`pkg/proxy/core/container/base_test.go:226` · 维度：测试缺口 · 单元：CON
- **问题**：TestBaseContainer_Concurrent_StartStop（base_test.go:226-247）用瞬时返回的 mock hook 并发跑 10 组 Start/Sleep/Stop，只断言最终 State()==Stopped，不统计 runFunc 与 stopFunc 的调用次数是否配对，也没有慢 runFunc/慢 stopFunc 场景。因此本单元已确认的三类竞态（Stop-during-Starting 孤儿进程/CT-06、Start-during-Stopping 状态覆盖、并发调用立即返回 nil/CT-05）在现有测试下全部漏检——这些 bug 恰好都不改变"最终落在 Stopped"这一断言。建议补充：1) 计数型 hook（atomic 计数 run/stop 调用），断言 run 次数==stop 次数（无孤儿进程）；2) 在 runFunc/stopFunc 中注入 sleep + channel 同步构造确定性交错；3) CI 以 -race 运行（当前竞态多为逻辑竞态,race detector 之外还需配对断言）。
- **证据**：base_test.go:226-247 `TestBaseContainer_Concurrent_StartStop` 仅 `if bc.State() != ContainerStateStopped { t.Errorf(...) }`；mockHooks runFunc 为 `func() error { return nil }`（无延迟、无调用计数）
- **核实**：核实成立。base_test.go:226-247 的 TestBaseContainer_Concurrent_StartStop 仅用 `hooks := &mockHooks{runFunc: func() error { return nil }}`（瞬时返回、无计数、无 stopFunc），并发 10 组 Start/Sleep(1ms)/Stop 后只断言 State()==Stopped。已验证 idx 3 描述的 Stop(Running)+Start(Stopping) 竞态即使发生，最终状态也恰好是 Stopped（MarkStopped 无条件覆盖），该断言必然通过——即测试对本单元已知的状态机竞态（CT-05/CT-06 及新发现）全部漏检。测试改进建议合理。P3 恰当。

### 46. [P3·confirmed] legacy registry 的 instances map 被两把不同锁分别保护，构成真实数据竞争而非仅锁风格问题

- **位置**：`pkg/proxy/core/container/registry.go:63` · 维度：并发 · 单元：CON · 旧 review: CT-10
- **问题**：globalRegistry.instances 这同一个 map 在不同方法里由不同的互斥量保护：RegisterSingleton（registry.go:53 持 mu，63 写 instances）、UnregisterContainer（registry.go:70 持 mu，78 delete instances）、GetRegisteredTypes（registry.go:137 持 mu，144 遍历 instances）只持 globalRegistry.mu；而 GetContainer（registry.go:106-108）、IsSingleton（registry.go:169-171）只持 singletonM。两组操作并发时对 map 的读写完全无同步——Go 运行时可直接 fatal（concurrent map read and map write），race detector 必报。旧条目 CT-10 把它记为 P3 锁风格不一致，实际是内存模型层面的数据竞争；当前缓解因素是 legacy 注册面在生产代码中已无并发调用方（见 CT-09 相关 finding），故维持 P2。若保留该注册面，应统一用一把锁保护 instances。
- **证据**：registry.go:52-65 RegisterSingleton 仅 `globalRegistry.mu.Lock()` 下执行 `globalRegistry.instances[kind] = instance`（63 行）；registry.go:74-79 UnregisterContainer 在 mu 下 `delete(globalRegistry.instances, kind)`（78 行）；registry.go:104-108 GetContainer 仅 `globalRegistry.singletonM.Lock()` 下读 `globalRegistry.instances[kind]`；registry.go:140-147 GetRegisteredTypes 在 mu 下遍历 instances
- **核实**：代码事实确证：同一 instances map 由两把锁分别保护——RegisterSingleton（registry.go:53 持 mu、63 写）、UnregisterContainer（70 持 mu、78 delete）、SetContainer 写路径（131 在 singletonM 下写但外层持 mu）、GetRegisteredTypes（137 持 mu.RLock、144 遍历）用 mu；GetContainer（106-108）、IsRegistered（160-162）、IsSingleton（169-171）只用 singletonM。RegisterSingleton 与 GetContainer 并发即构成无同步的 map 写读，确是内存模型层面数据竞争而非仅锁风格问题。但降为 P3：全仓 grep 确认 RegisterSingleton/GetContainer/SetContainer/UnregisterContainer 在生产代码中零调用方（仅 xray/exec_runner.go:2598-2602 的注释提及和包内测试），竞争在当前代码下不可达，属死 API 上的潜伏缺陷，与旧 review 对同类问题的定级（CT-10 P3）一致。与旧 review CT-10 重复（同一 registry.go:104-116 锁使用问题的重新定性，finder 已在 detail 提及但未标 legacyId）。

### 47. [P3·confirmed] BuildOptions.CertReader 是死注入点：全仓库无任何赋值，唯一消费分支永远不触发

- **位置**：`pkg/proxy/core/container/factory.go:26` · 维度：架构 · 单元：CON
- **问题**：factory.go:24-28 在 BuildOptions 中声明 CertReader 匿名接口字段（'Used by containers that require TLS (e.g., Hysteria)'），hysteria 的 factory（pkg/proxy/containers/hysteria/register.go:58-60）据此 `if opts.CertReader != nil { WithCertReader(...) }`。但全仓库 grep 'CertReader:' 无任何赋值点——cmd/server.go:169-174 构造 BuildOptions 只设置 UserManager/CertManager/HTTPPort/ProxyHost。因此该注入点从未生效，hysteria 实际只能依赖 CertManager 的 certIssuer 断言路径拿证书。要么在 cmd/server.go 装配处补上 CertReader（若设计上应注入），要么删除该字段及 hysteria 侧分支，避免误导后续容器实现者以为该依赖可用。
- **证据**：factory.go:24-28 CertReader 字段声明；pkg/proxy/containers/hysteria/register.go:58-60 `if opts.CertReader != nil { hopts = append(hopts, WithCertReader(opts.CertReader)) }`；grep -rn 'CertReader:' pkg cmd 零命中；cmd/server.go:169-174 BuildOptions 构造未含 CertReader
- **核实**：核实成立。factory.go:24-28 声明 BuildOptions.CertReader 匿名接口字段；pkg/proxy/containers/hysteria/register.go:58-60 是唯一消费点 `if opts.CertReader != nil { hopts = append(hopts, WithCertReader(opts.CertReader)) }`。全仓库 grep 'CertReader:'（含 pkg/cmd/internal）零命中，也无 `.CertReader =` 赋值；生产唯一装配点 cmd/server.go:169-174 构造 BuildOptions 只设置 UserManager/CertManager/HTTPPort/ProxyHost。因此该注入分支在任何路径下都不会触发，属死注入点，会误导后续容器实现者。P3 恰当（无功能故障，纯误导性死代码）。

### 48. [P3·confirmed] NewContainerMgr 用第一参数无条件覆盖 opts.StoreMgr，调用方在 opts 中设置的值会被静默清空

- **位置**：`pkg/proxy/core/container/manager.go:45` · 维度：错误处理 · 单元：CON
- **问题**：NewContainerMgr(storeMgr *store.StoreManager, opts BuildOptions) 在 manager.go:45 执行 `opts.StoreMgr = storeMgr`，无条件覆盖。BuildOptions 本身就有 StoreMgr 字段（factory.go:18），API 上存在两个入口表达同一依赖：若调用方按结构体字面量在 opts 里填了 StoreMgr 而第一参传 nil（现有多处测试正是 `NewContainerMgr(nil, BuildOptions{...})` 的写法），opts 里的值会被静默置 nil，容器拿不到持久化句柄（违背约束 #7 的装配预期）且无任何报错。建议去掉冗余的第一参数，或仅在 opts.StoreMgr == nil 时填充。
- **证据**：manager.go:44-50 `func NewContainerMgr(storeMgr *store.StoreManager, opts BuildOptions) *ContainerMgr { opts.StoreMgr = storeMgr; ... }`；factory.go:18 BuildOptions.StoreMgr 字段；调用面示例 cmd/server.go:169（第一参 storeMgr）与 pkg/rpc/server/end_node_inbound_test.go:38（第一参 nil）
- **核实**：核实成立。manager.go:44-50 中 NewContainerMgr 第一行 `opts.StoreMgr = storeMgr` 无条件覆盖，而 factory.go:18 的 BuildOptions 本身就有 StoreMgr 字段，API 上确实存在两个入口表达同一依赖。若调用方在 opts 字面量里填 StoreMgr 而第一参传 nil，会被静默清空。需要说明的是当前无实际受害者：生产唯一调用 cmd/server.go:169 走第一参传 storeMgr，所有测试调用（manager_test.go、pkg/rpc/server/*_test.go、mihomo/container_test.go）均为 NewContainerMgr(nil, BuildOptions{}) 且未在 opts 中设 StoreMgr——因此是纯 API 设计陷阱而非现行 bug，P3 定级恰当。

### 49. [P3·confirmed] clearClashTemplateNoise 对整份模板做全局字符串替换，可能损坏合法规则/配置行

- **位置**：`pkg/proxy/core/subscription/converter/clash.go:729` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：clearClashTemplateNoise 逐行对拉回的 Clash 模板执行 `strings.ReplaceAll(line, ",,", ",")` 和 `strings.ReplaceAll(line, ",dns-failed", "")`（clash.go:729-730），目的是修补假节点触发出来的 `MATCH,,漏网之鱼,dns-failed` 畸形行。但替换作用于模板的所有行，不区分上下文：任何合法含连续逗号的行（例如带空匹配字段的规则、YAML 内联数组 `[a,,b]`、被单引号包裹的含 ",," 字符串值、或字面量以 ",dns-failed" 结尾的策略名）都会被无差别改写，导致规则条数/字段错位。这是基于字符串而非 YAML 结构的脆弱修补，第三方模板一旦更换格式即可能产出错误配置。
- **证据**：clash.go:721-733 clearClashTemplateNoise 中 `line = strings.ReplaceAll(line, ",,", ",")` 与 `line = strings.ReplaceAll(line, ",dns-failed", "")` 对 rawLines 每一行执行，无语义/键名判定
- **核实**：确证（机制层面）。clash.go:721-733 clearClashTemplateNoise 对拉回模板的每一行无条件执行 `strings.ReplaceAll(line, ",,", ",")` 与 `strings.ReplaceAll(line, ",dns-failed", "")`（:729-730），无任何键名/上下文判定，这是对结构化 YAML 做纯文本全局补丁，代码本身即可确证其上下文无关性——任何合法含 ',,' 或以 ',dns-failed' 结尾内容的行都会被改写，且该函数注释自认是针对假节点触发的 'MATCH,,漏网之鱼,dns-failed' 单一格式问题的修补。需注明：finder 列举的具体损坏场景（第三方模板中合法出现 ',,' 的规则/内联数组/引号字符串）属于对上游模板内容的推测，仓库内无实例证实实际发生过损坏，故定级 P3（脆弱性/健壮性问题而非已观察到的错误输出）恰当。用户自传 ?rule= 参数不经过此函数（仅模板数据经过），影响面限于第三方模板内容。

### 50. [P3·confirmed] ConvertURIs/ConvertURIsWithOptions 在 converter 为 nil 时返回空串且 error 为 nil

- **位置**：`pkg/proxy/core/subscription/uri_convert.go:26` · 维度：错误处理 · 单元：SUB
- **问题**：ConvertURIs（uri_convert.go:24-28）与 ConvertURIsWithOptions（uri_convert.go:36-39）在 GetConverterOrDefault 返回 nil 时直接 `return "", nil`，把'没有可用转换器'当成功处理，返回空订阅正文且无错误。这与 Manager.GetSubscriptionForClient 路径（manager.go:100-102 返回明确 error）行为不一致。converter 依赖 init() 自注册，调用方必须 blank-import converter 子包；若某个入口（如未来新加的 handler/工具）漏掉 import，clash/surge 请求会静默返回空串而不是报错，极难定位。/sub HTTP 层（sub_handler.go:199-204）拿到空串 err=nil 会照常回 200，用户看到的是一个空订阅。触发条件：调用方未导入 converter 子包。影响：静默产出空订阅，无错误信号。
- **证据**：uri_convert.go:24-28 `c := GetConverterOrDefault(format); if c == nil { return "", nil }`；对比 manager.go:100-102 返回 fmt.Errorf('no converter registered...')
- **核实**：核实成立。uri_convert.go:24-27 ConvertURIs 与 36-39 ConvertURIsWithOptions 在 GetConverterOrDefault(converter.go:45-54,注册表全空或无 common 时返回 nil)返回 nil 时均 `return "", nil`,把"无可用转换器"当成功、产出空串无错误。对比 manager.go:100-102/120-122 同种情况返回 fmt.Errorf("no converter registered..."),行为确不一致。converter 靠子包 init() 自注册(clash.go:1414 Register),调用方须 blank-import;若入口漏 import,sub_handler.go:199-204 拿到 err=nil 空串会照常 c.String(200,...) 回空订阅,无错误信号,极难定位。触发条件(调用方未导入 converter 子包)属实。P3 合理。

### 51. [P3·confirmed] ParseRuleParam 丢弃第 4 段，Clash 规则的 no-resolve/额外参数被静默截断

- **位置**：`pkg/proxy/core/subscription/converter.go:154` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：ParseRuleParam 用 strings.Split(param, ",") 后只取 parts[0..2]（converter.go:154-164），而 patchTemplateWithOptions/buildLocalConfig 又以 `fmt.Sprintf("%s,%s,%s", Type, Value, Policy)`（clash.go:1002,1100）重建规则行。对于 Clash 常用的 `IP-CIDR,10.0.0.0/8,DIRECT,no-resolve` 这类带第 4 段附加选项的规则，no-resolve（以及 DOMAIN-SUFFIX+src 等其它扩展段）会被静默丢弃，生成的规则语义改变（IP-CIDR 无 no-resolve 会触发 DNS 解析，可能造成解析泄漏/回环）。触发条件：用户通过 rule 参数或 rules_url 提供带 no-resolve 的规则。影响：规则行为与用户意图不符，且无任何告警。
- **证据**：converter.go:154-164 只取 parts[0/1/2]；clash.go:1100 `fmt.Sprintf("%s,%s,%s", ruleConfig.Type, ruleConfig.Value, ruleConfig.Policy)` 重建，丢弃 no-resolve
- **核实**：核实成立。ParseRuleParam(converter.go:154-164)strings.Split(param, ",") 后仅取 parts[0/1/2] 填入 RuleConfig{Type,Value,Policy};RuleConfig 结构体(converter.go:80-84)只有这 3 字段,任何第 4 段(no-resolve/src 等)从解析起即丢弃。生产重建路径 patchTemplateWithOptions:1100 `fmt.Sprintf("%s,%s,%s", ruleConfig.Type, ruleConfig.Value, ruleConfig.Policy)` 只输出 3 段,buildLocalConfig:1002 同样(该函数生产未调用)。对 `IP-CIDR,10.0.0.0/8,DIRECT,no-resolve` 之类规则,no-resolve 被静默截断,语义改变(缺 no-resolve 会触发 DNS 解析)。丢弃机制由结构体+重建代码直接证明;no-resolve 为 Clash 通用第 4 段,DNS 影响为合理推断,不依赖第三方运行时记忆。P3 合理。

### 52. [P3·confirmed] Surge 与 Clash 的 Shadowsocks 默认加密方法不一致（缺 method 时结果分歧）

- **位置**：`pkg/proxy/core/subscription/converter/surge.go:155` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：同一份缺失 method 的 SS spec，SurgeConverter.convertShadowsocks 兜底为 'aes-256-gcm'（surge.go:154-155），而 ClashConverter.convertShadowsocks 兜底为 '2022-blake3-aes-256-gcm'（clash.go:508-510）。两者对完全相同的输入产出互不兼容的加密方法，取决于客户端 UA；若真实服务器用的是另一种，两个客户端至少有一个连不上，且没有告警。触发条件：spec.Extensions 缺 method（例如来自 ext_sub/remote 的裸 URI 投影未带 method）。影响：跨客户端订阅行为不一致、潜在连不上。
- **证据**：surge.go:154-155 `method = "aes-256-gcm"`；clash.go:508-510 `method = "2022-blake3-aes-256-gcm"`
- **核实**：分歧本身是仓库内代码事实，已亲读确证：surge.go:150-156 convertShadowsocks 缺 method 时兜底 'aes-256-gcm'；clash.go:506-510 convertShadowsocks 兜底 '2022-blake3-aes-256-gcm'。两种 cipher 完全不兼容（AEAD vs AEAD-2022，密钥推导都不同），同一 spec 按 UA 分流到不同 converter 时必然产出互斥结果。仓库内还有第三处佐证：containers/xray/subscription.go:413-416 的 generateShadowsocksURI 兜底 'aes-256-gcm' 并注释 '// xray default'，说明 clash converter 的 2022 兜底才是与本仓库其他路径不一致的那个——无需依赖仓库外第三方行为即可确证不一致性，故 protocolRelated 限制不阻止 confirmed。但 finder 给的触发示例不准确：ext_sub/remote 裸 URI 路径（remote.go:35 只填 URI、Protocol 为空，convertSpec 按 Protocol 分发不会进 convertShadowsocks；且 codec/shadowsocks.go:87-88 DecodeShadowsocks 对空 method 直接报错，uri_convert.go:354 ssNodeToSpec 拿到的 method 恒非空）不会触发。真实触发路径是 xray 容器：exec_runner.go:731-732 仅当 inbound settings 顶层有 method 才写入 extra，containers/xray/subscription.go:196-219 buildSubscriptionExtensions 也只在 extra 有时才拷贝，故 settings 无顶层 method 的 SS inbound（如 method 在 clients 内）会产出缺 method 的 spec 进入 converter。路径存在但较窄，P3 恰当。

### 53. [P3·confirmed] fake.go 每次调用重新以 time.Now().UnixNano() 播种 rand（遗留）

- **位置**：`pkg/proxy/core/subscription/fake.go:29` · 维度：正确性 · 单元：SUB · 旧 review: SUB-09
- **问题**：generateRandomIP/generateRandomPort/generateRandomString 各自 `rand.New(rand.NewSource(time.Now().UnixNano()))`（fake.go:29,38,43）。同一次 GenerateFakeSSSub 内三次连续调用可能落在相近纳秒时刻，播种熵低；不过输出仅用于触发外部 sub-converter 的假节点，安全影响低。此为 2026-04-09 已记录问题，未变。建议改用包级共享 rand 或 crypto/rand。
- **证据**：fake.go:29,38,43 三处 `rand.New(rand.NewSource(time.Now().UnixNano()))`
- **核实**：已核实 fake.go:29,38,43 三处确为每次调用 `rand.New(rand.NewSource(time.Now().UnixNano()))`，与描述一致，代码未变。与旧 review SUB-09 重复（docs/review-2026-04-09.md:332，P3，同文件同行号同问题），finder 文字里承认'2026-04-09 已记录'但未标 legacyId。另注意旧 review SUB-08 称 GenerateFakeSSSub 是死代码的结论已过时：pkg/http/sub_handler.go:48 现在调用它，即该弱随机路径确实在线上可达（用户不存在时返回假订阅），但输出仅为诱饵假节点，安全影响低，P3 恰当。

### 54. [P3·confirmed] 测试缺口：converter/http.go httpGet 与 ensureTemplateGroups 模板字段保真无覆盖

- **位置**：`pkg/proxy/core/subscription/converter/http.go:12` · 维度：测试缺口 · 单元：SUB
- **问题**：两个高风险点缺测试：(1) converter/http.go 的 httpGet 无超时/无上限行为无任何单测锁定，回归难以发现；(2) ensureTemplateGroups/appendGroupNameToAllOtherGroups 对含扩展字段（filter/lazy/tolerance/icon）的模板 group 的保真性无测试——现有 converter 表驱动测试主要覆盖单协议 convertXxx 与自定义 group 合并，未构造'带扩展字段的空 proxies 模板组'来验证字段是否被保留。影响：本报告中 P2 的字段丢失缺陷无法被现有测试捕获。建议补一条：喂入带 tolerance/lazy 的模板组，断言输出仍保留这些键。
- **证据**：converter/http.go 无对应 _test.go 覆盖超时/上限；ensureTemplateGroups(clash.go:765) 与 appendGroupNameToAllOtherGroups(clash.go:1385) 的字段保真无测试构造带扩展字段的模板组
- **核实**：两个测试缺口均亲自核实成立：(1) converter/http.go:12-41 httpGet 使用零值 `&http.Client{}`（无 Timeout）且 io.ReadAll 无大小上限，converter 目录下 6 个 _test.go（clash_custom/clash_patch/clash_protocol/clash_template_groups/converter/snell_surge）grep 'httpGet' 零命中，行为完全无测试锁定。(2) clash_template_groups_test.go 仅有两个用例（AddsManualWhenMissingAndLinksToProxy、FillsExistingManualGroup），构造的模板组只含 name/type/proxies 三个键，grep 'tolerance|lazy|filter|icon' 在全部 converter 测试中零命中，即 ensureTemplateGroups(clash.go:765)/appendGroupNameToAllOtherGroups(clash.go:1385) 对含扩展字段模板组的字段保真确实无任何测试覆盖。函数行号与 finding 所引一致。旧 review 中无对应条目（grep httpGet/ensureTemplateGroups 零命中），非重复。P3 恰当。

### 55. [P3·uncertain] busy_timeout/foreign_keys/cache_size PRAGMA 只作用于建池时的那一条连接，连接被替换后失效

- **位置**：`pkg/store/db.go:25` · 维度：资源 · 单元：STO · ⚠️协议相关（需人工核对上游）
- **问题**：Open 用 sqlDB.Exec 逐条设置 PRAGMA（db.go:25-36），但 busy_timeout、foreign_keys、cache_size 都是 per-connection PRAGMA（仅 journal_mode=WAL 持久化在库文件里）。database/sql 在驱动返回 ErrBadConn 或连接损坏时会静默新建连接，新连接不再带这些 PRAGMA：busy_timeout 回落为 0（遇锁立刻返回 SQLITE_BUSY 而非等 5s）、foreign_keys 关闭。当前实际影响被两点缓冲——MaxOpenConns(1) 且未设 ConnMaxLifetime 使连接极少被替换、schema 里目前没有任何 FOREIGN KEY 约束——所以定 P3；但一旦加 FK 或调大连接数就会变成隐蔽正确性问题。标准解法是把 PRAGMA 放进 DSN（modernc.org/sqlite 支持 `?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)` 查询参数，每条新连接自动生效）。依据：SQLite 文档中这三个 PRAGMA 均为连接级状态，及 modernc.org/sqlite README 的 _pragma DSN 参数约定。
- **证据**：pkg/store/db.go:25-36 `pragmas := []string{...}; for ... sqlDB.Exec(p)`；未使用 DSN _pragma 参数，也未设 SetConnMaxLifetime。
- **核实**：仓库内可核实的事实全部成立：db.go:25-36 确实用 sqlDB.Exec 逐条设置 4 个 PRAGMA，db.go:23 SetMaxOpenConns(1)，全文件无 SetConnMaxLifetime、DSN 无 _pragma 参数；pkg/store/migrations/migrations.go 中 grep 无任何 FOREIGN KEY 约束；go.mod 使用 modernc.org/sqlite v1.46.1。但该 finding 的核心失效机制依赖三项第三方行为断言——busy_timeout/foreign_keys/cache_size 是 SQLite per-connection 状态、database/sql 在 ErrBadConn 时静默换连接、modernc.org/sqlite 支持 ?_pragma= DSN 参数——检查 docs/、wiki/knowledge/（仅 mihomo-container、port-management 两个条目）及测试用例，均无材料证实这些上游语义（docs/review-2026-07-10/01-module-maps.md 只描述现状配置，不核证 PRAGMA 连接级语义）。按 protocolRelated 规则不得凭记忆确证第三方行为，故最高判 uncertain。P3 定级与'当前被 MaxOpenConns(1)+无 FK 缓冲'的评估合理。与旧 review docs/review-2026-04-09.md 无重复（该文档不含 pkg/store 条目）。

### 56. [P3·uncertain] detectFormat 仅按 "clash" 子串匹配，mihomo/Stash 等 UA 会误落回 base64 通用格式

- **位置**：`pkg/proxy/core/subscription/uri_convert.go:118` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：detectFormat 用 strings.Contains(ua, "clash") 判定 Clash 格式（uri_convert.go:120）。但当前主流 Clash 生态客户端并非都在 User-Agent 里带 "clash"：mihomo 内核直连（UA 形如 `mihomo/1.x`）、Stash（iOS）等都不含子串，会落到 default 分支返回 FormatCommon，最终返回 base64 URI 列表而非 Clash YAML，用户导入后无分流/策略组。虽然 sub_handler 支持 ?client=clash 显式覆盖（sub_handler.go:52-56）可规避，但仅凭 UA 自动识别时对这些客户端不成立。
- **证据**：uri_convert.go:117-128 detectFormat：`case strings.Contains(ua, string(FormatClash))` 中 FormatClash=="clash"（format.go:10），无 mihomo/meta/stash 等别名匹配
- **核实**：代码侧属实：uri_convert.go:117-128 detectFormat 仅以 strings.Contains(ua, "clash") 判定（FormatClash=="clash"），无 mihomo/meta/stash 别名。但 finding 的前提——mihomo 直连 UA 形如 'mihomo/1.x'、Stash UA 不含 'clash' 子串——是第三方客户端行为，仓库内材料不仅无佐证，反而存在相反断言：pkg/http/sub_userinfo_format.go:145-146 注释明确写 "mihomo's UA usually still contains 'Clash'"，其测试（sub_userinfo_format_test.go TestIsClashClient）用例 'mihomo party Clash' 也按含 clash 处理；docs/ 与 wiki/knowledge/ 均无各客户端 UA 字符串的实证记录。按 protocolRelated 规则不得凭记忆断言第三方 UA 格式，且仓库自身认知与 finder 相矛盾，无法在仓库内裁决谁对。另外 sub_handler.go:52-56 的 ?client= 覆盖（finder 引用行号准确，实际文件为 pkg/http/sub_handler.go）确实提供了规避手段。故 uncertain。

### 57. [P3·uncertain] ss:// 明文 method:password@host 形式（非 base64、非 2022）解码失败被丢弃

- **位置**：`pkg/proxy/core/subscription/codec/shadowsocks.go:122` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：decodeSSUserInfo 对非 2022- 前缀的 userinfo 一律走 base64 解码分支：取 `raw := u.User.String()`（如 "aes-256-gcm:password"）后逐一尝试 4 种 base64 变体（shadowsocks.go:122-135）。而部分外部订阅源/老客户端会直接给出明文形式 `ss://aes-256-gcm:password@host:443`，此串含 ':' 非法 base64，四种变体全部失败，decoded==nil，返回空 method → DecodeShadowsocks 报 "cannot extract cipher method"。在 ext_sub/远程订阅链路里这类 ss 节点会因 codec.Decode 失败而退化为 {URI: raw} 原样透传（urisToSpecs），对 common/qv2ray 尚可用，但对 clash/surge 因 spec.Protocol 为空被彻底丢弃，用户静默少节点。struct 文档虽只声明支持 SIP002/SIP022，但明文形式在真实订阅源中常见，值得补齐或至少显式告警。
- **证据**：shadowsocks.go:117-135：仅 `strings.HasPrefix(username, "2022-")` 走明文分支，其余全部 base64 解码；含 ':' 的明文 method:password 令四种 encoding.DecodeString 均返回 err，decoded 为 nil → 返回 "","" → DecodeShadowsocks:88 报错
- **核实**：代码机制完全属实：shadowsocks.go:117 仅 '2022-' 前缀走明文分支，其余在 :122-135 对 u.User.String()（明文形式含 ':'，如 'aes-256-gcm:password'）尝试 4 种 base64 解码必然全部失败 → decoded==nil → 返回 "",""，DecodeShadowsocks:88 报 'cannot extract cipher method'；随后 urisToSpecs（uri_convert.go:145-152）退化为 {URI: raw} 无 Protocol，clash converter 的 convertSpec（clash.go:242-244 注释明确对不支持协议返回 nil）会丢弃该节点——丢节点链路成立。但两点使其不能 confirmed：(1) ShadowsocksNode 的 struct 文档（shadowsocks.go:14-21）明确声明只支持 SIP002/SIP022 两种形式，明文 method:password@host 属文档化的范围之外，是设计边界而非隐性缺陷；(2) '明文形式在真实订阅源中常见' 是对第三方订阅源/老客户端行为的断言，仓库内 docs/wiki/测试用例（codec 测试全部使用 base64 或 2022- 形式）无任何明文 ss:// 实例佐证。按 protocolRelated 规则最高 uncertain。
