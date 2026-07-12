# L2 核心域 review — forward / usermanager / xrayapi / certmgmt

> **本文件为第一轮结果。** 第二轮独立复审（换角度找新问题 + 挑战疑似误报）见 [`L2-pass2.md`](L2-pass2.md)：第二轮新增 65 条保留（其中 3 条 P1，含 FWD limitedReader 烧预算、UM UserEvent 活指针竞态、CERT "dns" 别名静默降级 HTTP-01），并裁定第一轮 10 条为重复/过severity。处理时请两份合看。
>
> Finder → 对抗性 verifier 两级流程产出。下表为**保留**（confirmed + uncertain）的 findings，已剔除 verifier 判定 refuted 的 3 条。uncertain 多为依赖第三方协议/上游行为、无法在仓库内确证的条目，处理前需人工核对。

## 统计

| 维度 | 数量 |
|------|------|
| 保留条目 | 86 |
| — confirmed | 81 |
| — uncertain | 5 |
| — 其它(unverified) | 0 |
| refuted(已剔除) | 3 |

| 优先级 | P0 | P1 | P2 | P3 |
|--------|----|----|----|----|
| 保留条目 | 0 | 12 | 37 | 37 |

## 速查表

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P1 | ✓ | 正确性 | FWD | `pkg/proxy/forward/relay_tcp.go:322` | TCP 半关闭 drain 机制读取的是本地快照 deadline，活跃反向流量无法续期，连接在 drainSec 后被强制掐断 |
| 2 | P1 | ✓ | 架构 | FWD | `pkg/proxy/forward/forward_manager.go:193` | 转发监听默认绑定 "0.0.0.0" 仅 IPv4，违反“转发监听默认双栈 best-effort”架构约束，IPv6 客户端无法接入用户端口 |
| 3 | P1 | ✓ | 正确性 | FWD | `pkg/proxy/forward/relay_tcp.go:277` | TCP 单向结束后的 drain 状态机失效：活跃反方向传输在固定 2s 后被强制断开，RecordActivity 重置机制形同虚设 |
| 4 | P1 | ✓ | 正确性 | FWD | `pkg/proxy/forward/ratelimit.go:123` | 未设限时建立的 TCP 连接永久绕过用户限速：LimitReader 在包装时刻短路返回原始 reader，事后 SetUserBandwidthLimit 对既有连接无效 |
| 5 | P1 | ✓ | 资源 | FWD | `pkg/proxy/forward/clientlimit.go:275` | drainEnd map 只写不清导致按客户端 IP 无界增长，且 RecordActivity 在数据面热路径抢用户级全局锁写入永远无人读取的数据 |
| 6 | P1 | ✓ | 资源 | FWD | `pkg/proxy/forward/relay_tcp.go:116` | acceptLoop 对持续性 Accept 错误无退避、无日志：fd 耗尽（EMFILE）时热循环空转 100% CPU |
| 7 | P1 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:1361` | ReleaseBindPort 释放单个端口却删除该用户全部转发规则 (U-03) |
| 8 | P1 | ✓ | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:2237` | GetAllDeltaTraffic 读取与重置分两次独立取 sc.mu，tick 插入会丢失流量 (U-02) |
| 9 | P1 | ✓ | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:2239` | GetStats 返回共享 map,GetAllDeltaTraffic/DrainStats 在锁外迭代与 collect() 并发写导致并发 map 读写崩溃 |
| 10 | P1 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:1599` | SyncUpsertUser 更新分支 token 冲突再生后仍采纳远端 Hash，AuthToken 跨节点分叉且 digest 无法自愈 |
| 11 | P1 | ✓ | 正确性 | CERT | `pkg/certmgmt/lego/cert_store.go:44` | SaveCert 就地覆盖四个文件非原子，续期时 .crt 已换新而 .key 仍旧值，代理热加载会拿到证书/私钥不匹配对 |
| 12 | P1 | ✓ | 并发 | CERT | `pkg/certmgmt/service/rpc_adapter.go:60` | AddCertificates/DeleteCert 绕过 per-domain 锁，与自动续期 RenewDomain / Issue 并发写同一批证书文件 |
| 13 | P2 | ✓ | 正确性 | FWD | `pkg/proxy/forward/clientlimit.go:307` | SetConfig/SetUserClientLimitConfig 不回填 Recycle/Drain 默认值：运行时下发 MaxClients 而 drain=0 时半关闭连接 ≤100ms 被杀、槽位立即回收失去防换 IP 能力 |
| 14 | P2 | ✓ | 错误处理 | FWD | `pkg/proxy/forward/relay_udp.go:178` | UDP readLoop 遇到非超时读错误直接 return 且零日志：数据面静默死亡，规则仍显示活跃、端口继续占用 |
| 15 | P2 | ✓ | 资源 | FWD | `pkg/proxy/forward/clientlimit.go:280` | drainEnd map 只增不减（ClearDrain 无调用方），且 RecordActivity 在 TCP 每次 Read/UDP 每个包上抢用户级互斥锁写这张无人读取的 map |
| 16 | P2 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:606` | UpdateRateLimit 对数据面是彻底 no-op，rule 级限速字段仅在用户首条规则时被“提升”为用户级限额，接口注释与实际行为不符 |
| 17 | P2 | ✓ | 并发 | FWD | `pkg/proxy/forward/relay_tcp.go:144` | maxConns 检查基于 dial 成功后才自增的 activeConns，拨号窗口内可无限突破 MaxConnections |
| 18 | P2 | ✓ | 资源 | FWD | `pkg/proxy/forward/relay_tcp.go:121` | acceptLoop 对持续性 Accept 错误无退避地 continue，EMFILE 等场景下热自旋烧满 CPU |
| 19 | P2 | ✓ | 错误处理 | FWD | `pkg/proxy/forward/forward_manager.go:346` | RemoveRulesByUser/RemoveRulesByInbound 两段式删除遇到并发已删的 key 即中止并返回错误，剩余规则残留 |
| 20 | P2 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:491` | QueryTrafficStats 带过滤条件且 Reset=true 时会清零所有规则的计数并丢弃未匹配规则的流量 |
| 21 | P2 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:170` | rule 级限速字段语义失真：仅在用户限速器首建时作为种子生效并被提升为用户级，后续规则的字段被静默忽略；UpdateRateLimit 实际是无提示的 no-op |
| 22 | P2 | ✓ | 错误处理 | FWD | `pkg/proxy/forward/relay_udp.go:177` | UDP readLoop 非超时错误静默退出：relay 变僵尸但规则/端口在 manager 中仍显示活跃，无日志无自愈 |
| 23 | P2 | ✓ | 并发 | FWD | `pkg/proxy/forward/forward_manager.go:345` | RemoveRulesByUser/ByInbound 收集与删除两段式非原子：并发删除触发 'not found' 会中断批删，剩余规则泄漏 |
| 24 | P2 | ✓ | 错误处理 | FWD | `pkg/proxy/forward/forward_manager.go:294` | 自动分配端口与内核临时端口区间重叠且 bind 失败不换端口重试，AddRule 会因瞬时端口占用直接失败 |
| 25 | P2 | ✓ | 测试缺口 | FWD | `pkg/proxy/forward/relay_test.go:379` | 测试缺口：drain 状态机、既有连接限速生效、QueryTrafficStats 过滤+Reset 三个关键行为均无覆盖 |
| 26 | P2 | ✓ | 错误处理 | FWD | `pkg/proxy/forward/clientlimit.go:304` | SetUserClientLimitConfig→SetConfig 不回填默认值，drain/recycle 为 0 时连接被立即强制关闭 |
| 27 | P2 | ✓ | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:1013` | RotateUserPortForInbound 全程不持 m.mu，与 RemoveUser/GetBindPort 交错会复活转发规则、污染 tombstone 状态 |
| 28 | P2 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:1591` | SyncUpsertUser 更新分支 token 冲突重生后不 re-stamp，导致 AuthToken 与 Hash/版本跨节点分裂 |
| 29 | P2 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:867` | GetBindPort 未检查 IsDeleting，可为 tombstone 用户分配端口并建 relay (U-05) |
| 30 | P2 | ✓ | 资源 | UM | `pkg/proxy/usermanager/usermanager.go:445` | emitEvent 通道满时静默丢弃事件，丢失 Remove/PortBind 导致 relay/inbound 泄漏 |
| 31 | P2 | ✓ | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:370` | mutateUser/GetBindPort/ReleaseBindPort 在持 m.mu 写锁期间执行 store.Save(SQLite IO) (U-04) |
| 32 | P2 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:1504` | SyncUpsertUser「非更新但采纳 LoginPassword」分支改字段不重算 Hash，破坏 Hash 不变式 |
| 33 | P2 | ✓ | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:2242` | GetAllDeltaTraffic 读取与重置分两次独立取锁,中间 collect() 累积的 delta 被丢弃 (U-02) |
| 34 | P2 | ✓ | 错误处理 | UM | `pkg/proxy/usermanager/usermanager.go:453` | emitEvent 对满的订阅通道非阻塞丢弃事件,丢失 UserEventRemove/PortBind 会泄漏 relay/inbound 端口 |
| 35 | P2 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/sync/version.go:10` | IsNewer 仲裁忽略内容 Hash:同版本+同 OriginNode 但内容不同的记录永不收敛,并造成心跳持续拉全量抖动 |
| 36 | P2 | ✓ | 资源 | UM | `pkg/proxy/usermanager/usermanager.go:436` | emitEvent 对满 channel 非阻塞静默丢弃事件,UserEventRemove/PortBind 丢失导致中继/inbound 泄漏 |
| 37 | P2 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/types/types.go:1157` | NewAddUserOperation/NewRemoveUserOperation 把 JSON 塞进声称为 protobuf 的 TypedMessage，xray 反序列化必失败 |
| 38 | P2 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/types/types.go:242` | buildReceiverConfigProto 忽略 Sscanf 错误并塌陷端口区间，非法/空端口静默变 0 |
| 39 | P2 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/types/types.go:868` | h2/http 传输的 header 设置被写成 JSON 却标为 proto 类型，且 transport ProtocolName 与 stream 不一致 |
| 40 | P2 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:210` | hotreload buildConfig 把 api inbound 端口写死 62789 不加 PortOffset，与新实例 gRPC 地址及旧实例 api 端口全冲突 |
| 41 | P2 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotupdate/update.go:95` | hotupdate 死代码：新配置未做端口偏移，但映射按 oldPort+offset 计算，新实例必与旧实例抢占同端口 |
| 42 | P2 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:299` | builder.buildReceiverSettings 丢弃全部 streamSettings，导致 WS/TLS/Reality 变体实际产出纯 TCP receiver |
| 43 | P2 | ✓ | 错误处理 | XAPI | `pkg/xrayapi/hotreload/manager.go:342` | SwitchForwardRules 先删旧转发规则再加新规则,AddRule 失败或循环中途失败时无回滚,用户转发被永久拆除且状态不一致 |
| 44 | P2 | ✓ | 资源 | XAPI | `pkg/xrayapi/hotreload/manager.go:432` | switch-forward 失败时已 Start 的 newExecutor 既不 Stop 也不随结果返回,xray 子进程被孤儿化泄漏 |
| 45 | P2 | ✓⚠️协议 | 错误处理 | CERT | `pkg/certmgmt/service/manager.go:42` | 默认续期提前量仅 24h，任何超过 24h 的 ACME 故障/限流都会导致证书过期；legacy 30 天默认已成死代码 |
| 46 | P2 | ✓⚠️协议 | 安全 | CERT | `pkg/certmgmt/service/rpc_adapter.go:65` | DeleteCert 只删本地文件，从不向 CA 发起 ACME 吊销，被删证书在有效期内仍可被滥用 |
| 47 | P2 | ✓ | 安全 | CERT | `pkg/certmgmt/lego/cert_store.go:19` | 证书文件名仅替换 '*'，未过滤 '/'、'..'，域名可路径穿越写/删任意文件 |
| 48 | P2 | ✓⚠️协议 | 安全 | CERT | `pkg/certmgmt/lego/solver_dns.go:22` | DNS-01 凭证通过进程级环境变量传递，obtain/renew 窗口期内全进程可见 |
| 49 | P2 | ?⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/client.go:48` | grpc.Client.conn.Invoke 传入 JSON []byte，grpc 默认 proto codec 会在编码期直接失败 |
| 50 | P3 | ✓ | 测试缺口 | FWD | `pkg/proxy/forward/relay_test.go:735` | 测试缺口：半关闭后反向持续活跃流量、IPv6/双栈监听、UDP readLoop 错误退出均无测试覆盖 |
| 51 | P3 | ✓ | 错误处理 | FWD | `pkg/proxy/forward/forward_manager.go:24` | WithPortRange 静默吞掉 NewPortAllocator 错误，非法端口范围时悄悄回退默认 allocator |
| 52 | P3 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:498` | QueryTrafficStats 构造记录时遗漏 Protocol 字段，聚合/过滤结果中协议维度恒为空 |
| 53 | P3 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:296` | AddRule 失败回滚不清理本次新建的用户级 limiter/config 条目 |
| 54 | P3 | ✓ | 资源 | FWD | `pkg/proxy/forward/port_allocator.go:71` | Allocate 每次全范围扫描构建 available 切片：默认 5.5 万端口范围下每次分配 O(n) 遍历 + 大切片分配 |
| 55 | P3 | ✓ | 架构 | FWD | `pkg/proxy/forward/ratelimit.go:33` | NewTokenBucketLimiter 的 burst 形参被完全忽略，API 签名误导调用方 |
| 56 | P3 | ✓ | 架构 | FWD | `pkg/proxy/forward/forward_manager.go:837` | Close 持写锁串行 Stop 全部 relay 并各自 wg.Wait，规则多时长时间阻塞所有 API；且不清理 per-user map 与未触发的 recycle 定时器 |
| 57 | P3 | ✓ | 资源 | FWD | `pkg/proxy/forward/port_allocator.go:72` | PortAllocator.Allocate 每次构建整段可用端口切片，默认 5.5 万范围下 O(n) 开销 |
| 58 | P3 | ✓ | 资源 | FWD | `pkg/proxy/forward/relay_tcp.go:120` | acceptLoop 对非 ctx 关闭的 Accept 错误无退避直接 continue，持续性错误会 100% CPU 空转 |
| 59 | P3 | ✓ | 测试缺口 | FWD | `pkg/proxy/forward/forward_manager.go:482` | 测试缺口：QueryTrafficStats 的 reset+过滤语义、TCP drain 截断、RemoveRulesByUser 部分失败均无覆盖 |
| 60 | P3 | ✓ | 错误处理 | UM | `pkg/proxy/usermanager/usermanager.go:975` | ReleaseBindPort 的 finalize 文档承诺与实现/RemoveUser「永不物理删除」矛盾，且注释已成孤儿 (U-06) |
| 61 | P3 | ✓ | 资源 | UM | `pkg/proxy/usermanager/usermanager.go:1877` | statsCollector 聚合表与 prevCounters 从不清理，用户/inbound 长期 churn 造成无界增长 |
| 62 | P3 | ✓ | 测试缺口 | UM | `pkg/proxy/usermanager/usermanager.go:1311` | 测试缺口：ReleaseBindPort 单端口语义、Rotate 与 RemoveUser 并发未覆盖 |
| 63 | P3 | ✓ | 资源 | UM | `pkg/proxy/usermanager/usermanager.go:2154` | StartMaintenance 后台 goroutine 无停止机制;StartTrafficStats 重复调用会覆盖 onCollect 并忽略新 interval |
| 64 | P3 | ✓⚠️协议 | 安全 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:312` | buildTLSSettings 向固定共享 /tmp 路径写死伪证书并置 allowInsecure=true，含 TOCTOU |
| 65 | P3 | ✓ | 错误处理 | XAPI | `pkg/xrayapi/reality/keys.go:85` | KeyStore.LoadOrGenerate 把任意 loadKeys 错误当成空库，会覆盖已存在但读失败的 Reality 密钥文件 |
| 66 | P3 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:217` | buildConfig 丢弃原配置的 Outbounds 与 Routing,新实例失去出站与路由配置 |
| 67 | P3 | ✓ | 资源 | XAPI | `pkg/xrayapi/reality/keys.go:84` | LoadOrGenerate 把 loadKeys 的任意错误(非 NotExist)当成空库,随后 saveKeys 覆盖原文件,瞬时读错误/文件损坏会摧毁所有已存 Reality 密钥 |
| 68 | P3 | ✓ | 错误处理 | XAPI | `pkg/xrayapi/hotreload/manager.go:359` | HealthCheck 仅 sleep(500ms)+IsRunning,gRPC 就绪探测被禁用,转发流量可能切到尚未就绪/配置错误的新实例 |
| 69 | P3 | ✓ | 并发 | XAPI | `pkg/xrayapi/reality/keys.go:82` | KeyStore 无任何锁,LoadOrGenerate/DeleteKey 均为读-改-写文件,并发调用丢失更新 |
| 70 | P3 | ✓ | 错误处理 | XAPI | `pkg/xrayapi/reality/keys.go:108` | saveKeys 失败仅打印 warning,LoadOrGenerate 仍返回密钥当作已持久化,下次加载会生成不同密钥 |
| 71 | P3 | ✓ | 架构 | XAPI | `pkg/xrayapi/hotupdate/update.go:54` | hotupdate 包与 hotreload.Manager 近乎重复且完全无调用者,并存在独立分歧(NewBinaryPath 无回退、配置不做端口 offset) |
| 72 | P3 | ✓ | 测试缺口 | XAPI | `pkg/xrayapi/reality/keys.go:1` | 测试缺口:reality、hotreload、hotupdate、grpc/client 四个可测模块均无 *_test.go,失败/回滚/端口 offset 路径完全未覆盖 |
| 73 | P3 | ✓⚠️协议 | 错误处理 | CERT | `pkg/certmgmt/lego/issuer.go:63` | 注册成功后 SaveAccount 错误被 `_ =` 丢弃，账号 KID 未持久化则每次运行重复注册 |
| 74 | P3 | ✓⚠️协议 | 正确性 | CERT | `pkg/certmgmt/lego/solver_dns_slim.go:73` | DNSChallengeConfig.TimeoutSec 文档称默认 10 秒，但 <=0 时不注入任何超时选项，实际走 lego 内部默认 |
| 75 | P3 | ✓ | 并发 | CERT | `pkg/certmgmt/service/manager.go:113` | 多 SAN 证书 Issue 仅锁 domains[0]，其余 SAN 域无互斥；domainMu 条目只增不回收 |
| 76 | P3 | ✓ | 测试缺口 | CERT | `pkg/certmgmt/service/rpc_adapter.go:37` | 测试缺口：SaveCert 部分失败原子性、AddCertificates/DeleteCert 与续期并发、Revoke、rpc_adapter 导入路径均无覆盖 |
| 77 | P3 | ✓⚠️协议 | 正确性 | CERT | `pkg/certmgmt/lego/issuer.go:210` | Revoke 在证书私钥解析失败时回退临时随机 EC256 密钥，无法产生有效吊销 |
| 78 | P3 | ✓ | 错误处理 | CERT | `pkg/certmgmt/service/manager.go:146` | GetCert 把 LoadCert 的 I/O 错误当作『证书不存在』返回 nil，掩盖瞬时故障 |
| 79 | P3 | ✓⚠️协议 | 架构 | CERT | `pkg/certmgmt/service/manager.go:170` | service.Config 无 CAURL 字段，buildIssueRequest 永不设置 CAURL，无法切换 LE staging |
| 80 | P3 | ✓⚠️协议 | 正确性 | CERT | `pkg/certmgmt/lego/issuer.go:288` | ctx 未透传给 lego 的 Obtain/Renew，自动续期取消无法中断在途 ACME 调用 |
| 81 | P3 | ✓ | 资源 | CERT | `pkg/certmgmt/service/manager.go:103` | domainMu 锁条目只增不减且 Issue 仅锁 domains[0]，多 SAN 证书其余域名不受保护 |
| 82 | P3 | ✓ | 测试缺口 | CERT | `pkg/certmgmt/lego/cert_store.go:1` | cert_store/account_store/rpc_adapter/client_factory 无单测，导入与路径穿越等关键分支未覆盖 |
| 83 | P3 | ? | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:2113` | StartTrafficStats 重复调用时无锁写 sc.onCollect，与 collect() 的读构成数据竞态 |
| 84 | P3 | ?⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/types/types.go:537` | buildShadowsocksInboundConfig 无视 method 一律构造 shadowsocks_2022.ServerConfig，传统 SS 方法会被错配 |
| 85 | P3 | ?⚠️协议 | 正确性 | CERT | `pkg/certmgmt/lego/issuer.go:219` | Revoke 硬编码 LEDirectoryProduction，且解析私钥失败时用临时 EC256 密钥签名必然被 CA 拒绝 |
| 86 | P3 | ?⚠️协议 | 正确性 | CERT | `pkg/certmgmt/lego/solver_dns.go:52` | WithDNSCredentials 恢复环境变量时对原本未设置的变量用 Setenv(k,"") 而非 Unsetenv，留下空值污染进程环境 |

## 详细条目

### 1. [P1·confirmed] TCP 半关闭 drain 机制读取的是本地快照 deadline，活跃反向流量无法续期，连接在 drainSec 后被强制掐断

- **位置**：`pkg/proxy/forward/relay_tcp.go:322` · 维度：正确性 · 单元：FWD
- **问题**：handleConn 在任一方向 copy 结束时调用 clientLimiter.OnSingleDirectionEnd 拿到 drainDeadline 本地快照并置 drainActive=true（relay_tcp.go:227-232/249-254）。设计意图是活跃读会续期 drain：recordActivity（relay_tcp.go:206-210，注释明言 'called on each real read/write' 重置 drain deadline）确实会更新 remoteIPClientLimiter.drainEnd（clientlimit.go:275-281），但 relay 侧从不回读该 map——IsDrainExpired/ClearDrain（clientlimit.go:284/296）在整个仓库无任何调用方。结果是 idleTick 每 100ms 用一次性的本地快照判定 time.Now().After(drainDeadline)，一旦一个方向 EOF（例如客户端 shutdown-write 后继续下载、或 xray 向客户端方向传播半关闭），另一方向即使正在持续搬运数据，也会在 SingleDirectionDrainSec（默认 2 秒）后被 clientConn.Close()+targetConn.Close() 强制断开。由于 AddRule 对每条规则都无条件附加 clientLimiter（含 passthrough，forward_manager.go:250-265），该行为覆盖所有 TCP 转发连接。另外三处判定条件 `!time.Now().IsZero()`（relay_tcp.go:277/300/322）恒为 true，疑为 `!drainDeadline.IsZero()` 的笔误，进一步印证该状态机实现与意图脱节。修复方向：drain 判定改为查询 limiter 的最新 drainEnd（或把续期写回 handleConn 本地状态）。
- **证据**：relay_tcp.go:319-330 idleTick 分支 `if drainActive && !time.Now().IsZero() && time.Now().After(drainDeadline) { ... clientConn.Close(); targetConn.Close() ... }` 中 drainDeadline 是 227/251 行写入的一次性快照；clientlimit.go:275-281 RecordActivity 只写 l.drainEnd map；`grep -rn IsDrainExpired\|ClearDrain pkg --include=*.go | grep -v _test` 仅命中定义处（clientlimit.go:284/296），无调用方。
- **核实**：亲读 relay_tcp.go 证实：drainDeadline 仅在 229/251 行由 OnSingleDirectionEnd 写入本地快照；recordActivity(206-210)→limiter.RecordActivity 只写 clientlimit.go:280 的 drainEnd map，relay 从不回读该 map。grep 确认 IsDrainExpired/ClearDrain(clientlimit.go:284/296) 全仓无任何调用方，属死代码，印证状态机意图脱节。idleTick 分支(322)用 time.Now().After(drainDeadline) 判定本地快照，活跃反向流量无法续期。`!time.Now().IsZero()`(277/300/322)恒为 true 确为笔误。clientLimiter 由 AddRule 对每条 TCP 规则无条件附加(forward_manager.go:250-265,含 passthrough)，remoteIP 对 TCP 均非空，故一旦一方向 EOF、另一方向持续搬运超 SingleDirectionDrainSec(默认2s)即被强制断开。触发需‘半关闭+另向持续>2s’，代理隧道场景略窄但真实存在。行号准确。

### 2. [P1·confirmed] 转发监听默认绑定 "0.0.0.0" 仅 IPv4，违反“转发监听默认双栈 best-effort”架构约束，IPv6 客户端无法接入用户端口

- **位置**：`pkg/proxy/forward/forward_manager.go:193` · 维度：架构 · 单元：FWD
- **问题**：AddRule 在 rule.ListenAddr 为空时默认 listenAddr="0.0.0.0"（forward_manager.go:191-195），随后 TCPRelay 用 net.Listen("tcp", "0.0.0.0:port")、UDPRelay 用 net.ListenPacket("udp", ...) 绑定。Go 对显式 "0.0.0.0" 主机地址会创建 AF_INET（IPv4-only）套接字；要获得内核双栈需使用空主机（":port"，AF_INET6 + V6ONLY=0）或显式双 socket 降级方案。上层 usermanager 构造 ForwardRule 时从不设置 ListenAddr（pkg/proxy/usermanager/usermanager.go:914-926），因此生产路径全部落入 IPv4-only 默认值——纯 IPv6 或 IPv6 优先的客户端连接用户转发端口会直接失败。审查约束 #6 声明“转发监听默认双栈 best-effort”，但包内不存在任何双栈绑定/降级实现（预期的 listen.go/relay_multi.go 文件不存在，全仓库也无 dual-stack 相关代码）。修复：默认 listen 地址改为 ":port"，并在仅 IPv4 环境（bind AF_INET6 失败）时降级回 "0.0.0.0"。
- **证据**：forward_manager.go:191-195 `listenAddr := rule.ListenAddr; if listenAddr == "" { listenAddr = "0.0.0.0" }; fullListenAddr := fmt.Sprintf("%s:%d", listenAddr, port)`；types.go:37 注释固化该默认；`grep -rn dual|双栈 pkg --include=*.go` 无实现代码命中；usermanager.go:914-926 构造规则不含 ListenAddr。
- **核实**：forward_manager.go:191-195 确将空 ListenAddr 默认为 "0.0.0.0"，net.Listen("tcp","0.0.0.0:port") 在 Go 中生成 AF_INET(IPv4-only) 监听（标准可验证行为，非第三方运行时）。PROJECT_GUIDE.md:73 明确声明‘转发监听默认双栈 best-effort’并引用 pkg/proxy/forward/listen.go + relay_multi.go——但 ls 确认这两文件不存在，grep 全仓无 dual/双栈/ListenStack/listen_stack/tcp6/v6only 相关实现。types.go 注释亦固化 "0.0.0.0" 默认，usermanager.go:914-925 构造 ForwardRule 从不设 ListenAddr/ListenStack。即文档声明的双栈默认在代码里完全缺失，纯 IPv6 客户端无法接入用户转发端口，属文档架构约束与实现的真实缺口。protocolRelated=false 正确。行号准确。

### 3. [P1·confirmed] TCP 单向结束后的 drain 状态机失效：活跃反方向传输在固定 2s 后被强制断开，RecordActivity 重置机制形同虚设

- **位置**：`pkg/proxy/forward/relay_tcp.go:277` · 维度：正确性 · 单元：FWD
- **问题**：handleConn 中 drainDeadline 是 OnSingleDirectionEnd 返回值的一次性本地快照（relay_tcp.go:229/251），此后 select 循环（uploadDone/downloadDone/idleTick 三处）只对照这个过期不变的本地变量判断 `time.Now().After(drainDeadline)`。设计上应由 RecordActivity 在对端仍有数据活动时重置 drain 截止时间（relay_tcp.go:205-210 注释明示 'called on each Read to reset drain deadline'），但 RecordActivity 只写入 remoteIPClientLimiter.drainEnd map（clientlimit.go:275-281），relay 从不回读——IsDrainExpired/ClearDrain 在全仓库没有任何调用者。另外三处判断条件 `!time.Now().IsZero()` 恒为 true（疑为 `!drainDeadline.IsZero()` 笔误），使代码失去对零值 deadline 的保护。后果：由于 AddRule 现在总是给每个 relay 附加 clientLimiter（即使 passthrough，构造函数把 SingleDirectionDrainSec 回填为 2），任何 TCP 连接只要一个方向 EOF（例如客户端发完请求后 CloseWrite 半关闭，或目标端先 FIN），另一方向即使正在活跃传输大流量，也会在 2 秒后被 idleTick 分支强制 Close 双向连接，下载/上传被中途切断。
- **证据**：relay_tcp.go:277/300/322 `if drainActive && !time.Now().IsZero() && time.Now().After(drainDeadline)`；drainDeadline 仅在 229/251 两处赋值（方向结束时刻的快照）；clientlimit.go:275-281 RecordActivity 只写 l.drainEnd，grep 全仓库 IsDrainExpired/ClearDrain 零调用者；clientlimit.go:92-94 构造函数将 SingleDirectionDrainSec<=0 回填为 2，故 passthrough 限制器也返回 2s deadline
- **核实**：核实无误。drainDeadline 为 relay_tcp.go:229/251 方向结束时刻的一次性本地快照,select 循环(277/300/322)只比对该固定本地变量;设计意图由 RecordActivity 重置,但 RecordActivity 只写 clientlimit.go:280 的 drainEnd map,而 relay 从不回读——IsDrainExpired/ClearDrain 全仓库零调用者(grep 证实)。且 AddRule:250-265 对每个 TCP relay(含 passthrough)都附加 clientLimiter,构造函数 clientlimit.go:92-94 把 SingleDirectionDrainSec<=0 回填为 2,故 OnSingleDirectionEnd 恒返回 now+2s。任一方向 EOF(半关闭)后,反方向即使活跃传输也会在固定 2s 被 idleTick 强制 Close 双向。`!time.Now().IsZero()` 恒真的笔误因 drainActive 门控未造成额外后果,但重置机制确实失效导致传输截断。P1 可辩护(静默数据截断)。

### 4. [P1·confirmed] 未设限时建立的 TCP 连接永久绕过用户限速：LimitReader 在包装时刻短路返回原始 reader，事后 SetUserBandwidthLimit 对既有连接无效

- **位置**：`pkg/proxy/forward/ratelimit.go:123` · 维度：正确性 · 单元：FWD
- **问题**：TCPRelay.copyWithCount 在每条连接建立时调用一次 limiter.LimitReader（relay_tcp.go:356-361）。TokenBucketLimiter.LimitReader 在 IsUnlimited()==true 时直接返回未包装的原始 reader（ratelimit.go:122-127）。用户初始无限速时（AddRule 总是先创建 rate=0 的 passthrough 桶，forward_manager.go:170-184），此时建立的所有连接拿到的是裸 reader；之后管理员调 SetUserBandwidthLimit→SetRate 虽然原地更新了共享桶，但这些连接已经不经过桶，永远不被限速。这与 manager.go:58 接口契约 'Setting a value immediately applies to all existing and new rules' 及 forward_manager.go:164-166 'existing relays will see the change immediately' 直接矛盾。代理转发场景连接以长连接为主（一条隧道可存活数小时），意味着新设的限速可被既有连接无限期规避，用户只需不断线即可。对照：UDP 路径每包调用 WaitN（内部动态检查 IsUnlimited），限速变更立即生效，两种传输行为不对称。反方向（有限速→取消限速）则因 limitedReader 内部 waitForTokens 的 fast path 立即生效，进一步说明只有这一个方向有问题。integration_test.go:450-469 只断言共享桶的 GetRate 值变化，未验证既有连接实际被节流，测试掩盖了该缺陷。
- **证据**：ratelimit.go:122-127 `func (l *TokenBucketLimiter) LimitReader(r io.Reader) io.Reader { if l.IsUnlimited() { return r } ... }`；relay_tcp.go:356-361 每连接仅包装一次；manager.go:58 'Setting a value immediately applies to all existing and new rules'
- **核实**：核实无误。copyWithCount(relay_tcp.go:356-361)每连接仅调用一次 LimitReader;ratelimit.go:122-127 在 IsUnlimited()==true 时直接返回裸 reader。AddRule:170-184 在用户无 rule 级限速时创建 rate=0 passthrough 桶,此期间建立的连接拿到裸 reader,后续 SetUserBandwidthLimit→SetRate 虽原地更新共享桶,但既有连接已不经桶,永久绕过。与 manager.go:58/forward_manager.go:164-166 契约矛盾。UDP 路径每包 WaitN 动态查 IsUnlimited 立即生效,方向不对称属实;反向(限速→取消)因 waitForTokens 快路径立即生效也属实。代理长连接场景可无限期规避新设限速,P1 恰当。

### 5. [P1·confirmed] drainEnd map 只写不清导致按客户端 IP 无界增长，且 RecordActivity 在数据面热路径抢用户级全局锁写入永远无人读取的数据

- **位置**：`pkg/proxy/forward/clientlimit.go:275` · 维度：资源 · 单元：FWD
- **问题**：remoteIPClientLimiter.drainEnd（clientlimit.go:79）由 OnSingleDirectionEnd 和 RecordActivity 持续写入 per-remoteIP 条目，但唯一的删除入口 ClearDrain 和唯一的读取入口 IsDrainExpired 在全仓库没有调用者；Release 的 recycle 定时器只删 slots 不删 drainEnd。该限速器是用户级共享实例、生命周期与进程等长（仅 DropUser 释放），因此 drainEnd 随历史上出现过的唯一客户端 IP:port 数无界增长（TCP 传 IP，UDP relay 传 IP——但 TCP 每个连接、UDP 每个包都会写入）。同时 RecordActivity 被挂在数据面最热的路径上：TCP 每次 32KB Read（relay_tcp.go:380-386 activityReader → 205-210 recordActivity）和 UDP 每个数据包（relay_udp.go:326-328/378-380）都要 Lock 同一把用户级互斥锁 l.mu 并做 map 写入。同一用户所有规则、所有连接在这把锁上串行化，而写入的 deadline 又永远无人消费——纯粹的锁竞争开销加内存泄漏。修复方向：要么让 relay 真正消费 drainEnd（同时修复上一条 drain 失效问题），要么整体移除 drainEnd/RecordActivity 机制。
- **证据**：clientlimit.go:275-281 RecordActivity 每次 `l.mu.Lock(); l.drainEnd[remoteIP] = time.Now().Add(...)`；clientlimit.go:283-301 IsDrainExpired/ClearDrain 定义后全仓库 grep 无任何调用；Release/recycleTimer 回调（223-241）只 delete(l.slots, ip) 不清 drainEnd
- **核实**：核实无误。drainEnd(clientlimit.go:79)由 OnSingleDirectionEnd:270 与 RecordActivity:280 持续写入,唯一删除入口 ClearDrain 与唯一读取入口 IsDrainExpired 全仓库零调用(grep 证实);Release/recycleTimer 回调 223-241 只 delete slots 不清 drainEnd。限速器为用户级共享、进程级生命周期(仅 DropUser 释放),drainEnd 随历史唯一 remoteIP 无界增长(remoteIP 为 IP 而非 IP:port,略缩小量级但仍永不回收=泄漏)。RecordActivity 挂在数据面最热路径:TCP 每次 32KB Read(activityReader.Read n>0→onActivity→RecordActivity)与 UDP 每包都 Lock 同一把用户级 l.mu 写永无人消费的 deadline,同用户全部规则/连接在此锁串行化。锁竞争+泄漏均成立,P1 可辩护。

### 6. [P1·confirmed] acceptLoop 对持续性 Accept 错误无退避、无日志：fd 耗尽（EMFILE）时热循环空转 100% CPU

- **位置**：`pkg/proxy/forward/relay_tcp.go:116` · 维度：资源 · 单元：FWD
- **问题**：acceptLoop 在 Accept 返回错误且 ctx 未取消时直接 continue（relay_tcp.go:114-122），无任何退避、计数或日志。当进程文件描述符耗尽（代理网关高并发下 EMFILE 是现实故障模式）或 listener 出现其他持续性错误时，Accept 立即返回错误，循环变成紧密自旋，单条规则一个 goroutine 打满一个核；多条规则同时自旋会放大故障。标准做法（如 net/http.Server.Serve）是对 Temporary/持续错误做指数退避（5ms→1s），并记录日志便于定位。对照同包 UDP readLoop 对非超时错误选择了直接 return（另一个极端，见另一条 finding），两种 relay 对同类故障的行为完全不一致。
- **证据**：relay_tcp.go:113-122 `conn, err := r.listener.Accept(); if err != nil { select { case <-r.ctx.Done(): return; default: continue } }` — 无 sleep/backoff/log
- **核实**：relay_tcp.go:113-122 属实：Accept 出错且 ctx 未取消时无 sleep/backoff/log 直接 continue。EMFILE(fd 耗尽)时 Accept 立即返回错误，形成紧循环空转打满 CPU；且任何非临时错误也会无限自旋。对照 net/http.Server.Serve 的指数退避标准做法，缺陷成立。severity P1 相对同批 P2 偏高但对代理网关的可用性影响可辩护，不改。行号锚点(116)落在错误处理块内，准确。

### 7. [P1·confirmed] ReleaseBindPort 释放单个端口却删除该用户全部转发规则

- **位置**：`pkg/proxy/usermanager/usermanager.go:1361` · 维度：正确性 · 单元：UM · 旧 review: U-03
- **问题**：ReleaseBindPortRequest 只带一个 BindPort，函数体也只从 user.BindPorts/PortMappings 里移除这一个端口(1322-1349)，但收尾却调用 m.forwardMgr.RemoveRulesByUser(req.Username)(1361)，把该用户跨所有 inbound 的 relay 全部拆掉。多 inbound 用户释放某一个端口时，其它 inbound 的转发链路被误删,客户端连接中断且 user.BindPorts 仍保留那些端口(状态与实际 relay 不一致)。forward 层已有单规则 RemoveRule(ruleKey),此处应按被释放端口定位对应规则删除,而非 RemoveRulesByUser。
- **证据**：usermanager.go:1361 `m.forwardMgr.RemoveRulesByUser(req.Username)`；上文 1323-1330 仅移除 req.BindPort 一个端口。RotateUserPortForInbound 处 (1052) 证明单规则 RemoveRule(ruleKey) 可用。
- **核实**：已核实:ReleaseBindPort(1311-1372)只从 user.BindPorts/PortMappings 移除 req.BindPort 单个端口,收尾却调用 forwardMgr.RemoveRulesByUser(1361)。forward_manager.go:335-351 的 RemoveRulesByUser 遍历删除所有 mr.rule.Username==username 的规则,规则 key 为 container:inbound:user,故会拆掉该用户跨全部 container/inbound 的 relay。仓库确实支持一个用户多 inbound/多容器(RotateAllUserPorts 1130-1161 依赖 GetRulesByUser 返回多条规则;ReleaseBindPort 被 hysteria/snell/xray/mihomo 四类容器各自调用)。单规则 RemoveRule(ruleKey) 存在且在 RotateUserPortForInbound 1052 已被用作精确删除。多 inbound 用户释放一个端口即误删其它 relay,状态与实际不一致,P1 成立。行号准确。

### 8. [P1·confirmed] GetAllDeltaTraffic 读取与重置分两次独立取 sc.mu，tick 插入会丢失流量

- **位置**：`pkg/proxy/usermanager/usermanager.go:2237` · 维度：并发 · 单元：UM · 旧 review: U-02
- **问题**：GetAllDeltaTraffic 先 GetStats() 拿快照(内部 sc.mu.RLock 后释放,2237),再 resetAllDeltas() 单独 sc.mu.Lock 清零(2243)。两次加锁之间 collectLoop 的 collect() 可完整跑一轮,把新增量累进 DeltaUplink/Downlink;紧接着 resetAllDeltas 把这批还没被读到的增量一并清零。该路径服务于 DrainStats→Prometheus/center 上报,丢失的 delta 直接造成计费/统计漏计。应在同一把 sc.mu 临界区内原子完成 读取+重置。
- **证据**：usermanager.go:2237 `agg := m.statsCollector.GetStats()`(GetStats 1943-1947 取 RLock 后释放),2243 `m.statsCollector.resetAllDeltas()`(2009-2011 另取 Lock);collect() 1858 持 sc.mu 累进 Delta。
- **核实**：已核实:GetAllDeltaTraffic(2233-2246)先 GetStats()(1943-1947 取 RLock 拿 sc.stats 结构体副本后释放),再 resetAllDeltas()(2009 单独取 Lock)。两次加锁之间 collectLoop 的 collect()(1858 持 sc.mu 累进 DeltaUplink/Downlink)可插入一轮,增量未被 result 读到却被随后 resetAllDeltas 清零 → 漏计。更严重:AggregatedStats 的 map 是引用,GetStats 返回的 agg.ByUser 与 sc.stats.ByUser 同一底层 map,2239 无锁 range 与 collect() 1884/1893 无锁并发写构成 Go '并发 map 迭代+写' → 可触发运行时 fatal 崩溃。消费者 bandwidth_stats_collector.go:35 DrainStats→proto.Stats 上报。读+重置未在同一 sc.mu 临界区,P1 成立。行号准确。

### 9. [P1·confirmed] GetStats 返回共享 map,GetAllDeltaTraffic/DrainStats 在锁外迭代与 collect() 并发写导致并发 map 读写崩溃

- **位置**：`pkg/proxy/usermanager/usermanager.go:2239` · 维度：并发 · 单元：UM
- **问题**：statsCollector.GetStats()(usermanager.go:1943-1947)在 sc.mu.RLock 下 return sc.stats,但 AggregatedStats 内的 ByUser/ByInbound/ByContainer 是 map,struct 值拷贝只拷贝 map header,返回的 map 与 sc.stats 同一底层对象。GetAllDeltaTraffic(2237-2244)拿到 agg 后在未持有 sc.mu 的情况下 for _, v := range agg.ByUser 迭代;collectLoop 每 1s 调 collect() 在 sc.mu.Lock 下写 sc.stats.ByUser(1884-1893)。GetAllDeltaTraffic 由 BandwidthStatsCollector.DrainStats 触发,经 pkg/rpc/server/end_node_user.go:338 的 GetBandWidthStats RPC 周期性调用,与 1s collect tick 高度并发。Go runtime 对并发 map 读+写抛 fatal error(concurrent map read and map write),不可 recover,直接崩溃进程。GetTrafficStats/GetGlobalTrafficStats 同样把共享 map 暴露给 RPC/HTTP 调用方。
- **证据**：GetStats(): sc.mu.RLock(); defer sc.mu.RUnlock(); return sc.stats(1943-1947);GetAllDeltaTraffic: agg := m.statsCollector.GetStats(); ... for _, v := range agg.ByUser(2237-2241)在锁外;collect() 在 sc.mu.Lock 下 sc.stats.ByUser[userKey] = existing(1893)。
- **核实**：证实为真实的致命并发 map 读写。GetStats(1943-1947)在 RLock 下 return sc.stats(值拷贝仅复制 map header,底层 map 与 sc.stats 共享);GetAllDeltaTraffic(2237-2241)在未持 sc.mu 情况下 range agg.ByUser;collect()(1858-1893)在 sc.mu.Lock 下写 sc.stats.ByUser[userKey]=existing。RPC 触发链真实:end_node_user.go:338 GetBandWidthStats→DrainStats(bandwidth_stats_collector.go:35)→GetAllDeltaTraffic。并发 map 读+写触发 Go runtime fatal error 且不可 recover,P1 成立。唯一事实偏差:cmd/server.go:260 StartTrafficStats(0),newStatsCollector(1771-1773)将 0 兜底为 1 分钟而非 detail 所称的 1s——仅降低触发频率,不改变竞态存在性,单次命中即崩溃进程。

### 10. [P1·confirmed] SyncUpsertUser 更新分支 token 冲突再生后仍采纳远端 Hash，AuthToken 跨节点分叉且 digest 无法自愈

- **位置**：`pkg/proxy/usermanager/usermanager.go:1599` · 维度：正确性 · 单元：UM
- **问题**：在“既有用户 + incoming 更新”分支(1589-1618)中,若 setAuthTokenLocked(existing, incoming.AuthToken) 因 token 与本地另一用户冲突而失败,代码用 setAuthTokenLocked(existing, "") 本地重新生成一个新 token(1591-1593),但随后仍无条件执行 existing.Hash = incoming.Hash(1599)且不调用 stampVersion。这与 Case 2 新用户分支(1560-1566)的处理不一致——后者在 token 再生后会调用 m.stampVersion(incoming) 重算 UpdatedAtUs/OriginNode/Hash。结果:本节点存的 existing.AuthToken 是本地新生成值,而 existing.Hash 却是按 incoming.AuthToken(源节点的 token)算出的哈希。ComputeHash(hash.go:46) 把 AuthToken 纳入摘要,因此存储的 Hash 与真实内容不符;同一用户在本节点与源节点持有不同 AuthToken,而两节点 ListDigests 上报的 (UpdatedAtUs,OriginNode,Hash) 完全相同,CompareDigests 永远不会判定需要重新拉取,分叉被永久掩盖。客户端订阅用的 AuthToken 会在一个节点鉴权通过、另一个节点鉴权失败。修复应像 Case 2 一样在 token 再生后 stampVersion。
- **证据**：1591 `if err := m.setAuthTokenLocked(existing, incoming.AuthToken); err != nil {` → 1592 `_ = m.setAuthTokenLocked(existing, "")`(无 stampVersion);1599 `existing.Hash = incoming.Hash`。对照 Case 2:1564-1565 `_ = m.setAuthTokenLocked(incoming, "")` 后紧跟 `m.stampVersion(incoming)`。测试 TestSyncUpsertUser_UpdateConflictingTokenRegenerated(usermanager_test.go:1795)只断言 token 被再生,未校验 Hash 与内容一致。
- **核实**：已核实。update 分支(usermanager.go:1591-1593)在 setAuthTokenLocked(existing, incoming.AuthToken) 因 token 与本地另一用户冲突而失败时,调用 setAuthTokenLocked(existing, "") 本地重新生成 token,但随后 1599 行无条件 existing.Hash = incoming.Hash 且不 stampVersion。ComputeHash(hash.go:46 writeField(u.AuthToken))确实把 AuthToken 纳入摘要,故存储的 Hash(基于 incoming.AuthToken)与实际内容(本地新 token)不符。对照 Case 2 新用户分支(1560-1566)在再生 token 后紧跟 stampVersion(incoming) 重算 Hash——两分支处理确不一致。ListDigests(1648-1653)上报 (UpdatedAtUs,OriginNode,Hash) 三者均采自 incoming,与源节点完全相同,CompareDigests(digest.go:60-70)不会触发 needFull,AuthToken 分叉被永久掩盖。测试 TestSyncUpsertUser_UpdateConflictingTokenRegenerated(1795)只断言 token 被再生且为合法 UUIDv4,未校验 Hash 与内容一致,确未覆盖此失真。触发前提(incoming.AuthToken 为合法 UUIDv4 且被本地另一用户占用)属边界场景,但代码显式防御该路径,后果为安全相关字段的静默永久分叉,P1 可接受。行号 1599 准确。

### 11. [P1·confirmed] SaveCert 就地覆盖四个文件非原子，续期时 .crt 已换新而 .key 仍旧值，代理热加载会拿到证书/私钥不匹配对

- **位置**：`pkg/certmgmt/lego/cert_store.go:44` · 维度：正确性 · 单元：CERT
- **问题**：SaveCert 依次对 crt(53)、key(58)、resource(67)、meta(81) 分别做 atomicWrite（每文件 tmp+rename 原子），但四个文件作为一组不是原子的。续期流程(issuer.go:189)与导入(rpc_adapter.go:60)都用同一批固定路径就地覆盖，而设计明确依赖代理核心对证书文件做热加载(renew_scheduler.go:70-72 注释)。因此：(a) 正常成功续期也存在一个窗口——新 .crt 已 rename 完成、.key 尚未写入，此刻监听 .crt 变化的代理会加载 新crt+旧key 的不匹配对，导致 TLS 握手失败；(b) 若在 key/res/meta 任一步写失败(磁盘满/权限)，磁盘上留下 新crt+旧key，直到下一次续期才可能修复，而默认续期窗口仅 24h(见另一条)。应先把四个文件写到临时目录再整体切换，或至少保证 key 先于 crt 落地。
- **证据**：cert_store.go:53 atomicWrite(crtPath,...) 后 58 atomicWrite(keyPath,...)；atomicWrite(34-41) 仅单文件 rename，四文件间无事务；调用方 issuer.go:189 SaveCert 原路径覆盖续期证书。
- **核实**：核实属实。cert_store.go:53 先 atomicWrite(crtPath) 后 :58 atomicWrite(keyPath),atomicWrite(:34-41) 仅单文件 rename,四文件间无事务。issuer.go:189(续期)与 rpc_adapter.go:60(导入)均对同一批固定路径就地覆盖;renew_scheduler.go:69-72 注释明确依赖代理核心 file hot-reload(xray ~1h ticker / mihomo fswatch)。因此存在(a)新 crt 已落盘、key 未写的短暂不匹配窗口,以及(b)key/res/meta 写失败时磁盘残留 新crt+旧key 直到下一次续期的持久不一致。crt 先于 key 落地的顺序确实放大风险。P1 可接受(scenario b 破坏 TLS 且持久)。行号 44=SaveCert 定义准确。

### 12. [P1·confirmed] AddCertificates/DeleteCert 绕过 per-domain 锁，与自动续期 RenewDomain / Issue 并发写同一批证书文件

- **位置**：`pkg/certmgmt/service/rpc_adapter.go:60` · 维度：并发 · 单元：CERT
- **问题**：Manager 用 domainMu(manager.go:103-106) 保证同域 Issue/RenewDomain 串行。但 rpc_adapter 的 AddCertificates(:60 直接调 SaveCert) 与 DeleteCert(:66 直接调 DeleteCert) 完全不取该锁。这两个方法由 gRPC EndNodeServer 处理器(end_node_cert.go:21/40)并发触发，同时后台 StartAutoRenew goroutine(cmd/server.go:166)持锁跑 RenewDomain。因此当运维对域名 X 导入外部证书、而自动续期正好在续 X 的 ACME 证书时：两个 SaveCert 并发写同一路径，且 atomicWrite 的临时文件是固定的 path+".tmp"(cert_store.go:36)——两个写者互相覆盖对方的 .tmp 再各自 rename，最终 crt/key/meta 可能来自不同证书；DeleteCert 与 RenewDomain 并发则出现删一半又写一半的残缺状态。所有落盘路径应统一经 domainLock。
- **证据**：rpc_adapter.go:60 return certmgmtlego.SaveCert(...) 与 :66 DeleteCert(...) 均无 m.domainLock；对比 manager.go:113-115/124-126 Issue/RenewDomain 均 mu.Lock；atomicWrite tmp 固定为 path+".tmp"(cert_store.go:36)。
- **核实**：核实属实。rpc_adapter.go:60 AddCertificates 直接 SaveCert、:66 DeleteCert 直接删文件,均不取 domainLock;而 manager.go:113-115/124-126 的 Issue/RenewDomain 持锁。TransferCert/DeleteCert 由 end_node_cert.go:21/40 gRPC 处理器触发,StartAutoRenew(cmd/server.go:166)后台并发跑 RenewDomain。atomicWrite 临时文件固定为 path+".tmp"(cert_store.go:36),两写者对同一 .tmp 与目标路径无锁并发,确实可能交叉覆盖出 crt/key/meta 来源不一致或删写各半。触发路径真实存在,非 protocol 依赖。P1 合理。

### 13. [P2·confirmed] SetConfig/SetUserClientLimitConfig 不回填 Recycle/Drain 默认值：运行时下发 MaxClients 而 drain=0 时半关闭连接 ≤100ms 被杀、槽位立即回收失去防换 IP 能力

- **位置**：`pkg/proxy/forward/clientlimit.go:307` · 维度：正确性 · 单元：FWD
- **问题**：newRemoteIPClientLimiter 会把 RecycleDelaySec<=0 回填为 60、SingleDirectionDrainSec<=0 回填为 2（clientlimit.go:88-94），AddRule 路径在 effectiveMaxClients>0 时也做了回填（forward_manager.go:230-237）。但运行时更新路径完全绕过回填：DefaultForwardManager.SetUserClientLimitConfig 把调用方原始 config 直接存入 userClientLimitConfigs 并调用 limiter.SetConfig（forward_manager.go:728-742），而 SetConfig 仅裸赋值 l.config = config（clientlimit.go:304-308）。上游 usermanager.applyRuntimeSideEffects 传的就是用户记录里的原始 recycleDelaySec/drainSec（usermanager.go:1183-1193），管理员只设置 MaxClients 时这两个值为 0。后果：1) Release 中 recycleDelay=0 → time.AfterFunc(0) 立刻删除槽位（clientlimit.go:210/223），MaxClients 的“断开后槽位保留期”被清零，换源 IP 立即抢占配额；2) OnSingleDirectionEnd 返回 time.Now()+0（clientlimit.go:269），deadline 即时过期，配合 relay_tcp 的 drain 状态机，该用户所有 TCP 连接在任一方向半关闭后 ≤100ms（idleTick 周期）被强制断开。且被污染的 config 也成为后续 AddRule 的 storedConfig 种子（forward_manager.go:211-215）。修复：SetConfig（或 manager 层）统一走与构造函数相同的 normalize 逻辑。
- **证据**：clientlimit.go:304-308 `func (l *remoteIPClientLimiter) SetConfig(config ClientLimitConfig) { l.mu.Lock(); defer l.mu.Unlock(); l.config = config }` 无任何默认值回填；对照 clientlimit.go:88-94 构造函数的回填逻辑；forward_manager.go:734 直接 `m.userClientLimitConfigs[username] = config`；usermanager.go:1189-1193 透传原始 int 值。
- **核实**：clientlimit.go:304-308 SetConfig 仅 `l.config=config` 裸赋值，无 88-94 构造函数那样的 RecycleDelaySec<=0→60、SingleDirectionDrainSec<=0→2 回填。forward_manager.go:734 SetUserClientLimitConfig 直接存 config 并调 SetConfig。触发路径可达且经文档化 API 暴露：HTTP handler(set_user_client_limit_handler.go:24-26/60-65) 将 recycle_delay_sec/drain_sec 设为可选指针，省略即 0；end_node_user.go:115-122 在 MaxClients!=0 时以 SetUserClientLimit(name,mc,0,0) 转发；usermanager.SetUserClientLimit(1266-1270) 不回填即写入用户记录，applyRuntimeSideEffects(1189-1193) 原样透传 {mc>0,0,0}。后果确凿：Release 中 recycleDelay=0→time.AfterFunc(0) 立即删槽(clientlimit.go:210/223)；OnSingleDirectionEnd 返回 now+0 即时过期(269)，叠加 idx0 的 drain 状态机使半关闭连接≤100ms 被杀。污染 config 亦成 AddRule storedConfig 种子(forward_manager.go:211-215)。P2 合理，行号准确。

### 14. [P2·confirmed] UDP readLoop 遇到非超时读错误直接 return 且零日志：数据面静默死亡，规则仍显示活跃、端口继续占用

- **位置**：`pkg/proxy/forward/relay_udp.go:178` · 维度：错误处理 · 单元：FWD
- **问题**：readLoop 对 ReadFrom 的错误处理是：ctx 已取消则退出；net.Error 超时则 continue；其余错误一律 return（relay_udp.go:169-179）。一旦发生持续性/一次性非超时错误（如 ENOBUFS、内存压力下的临时 socket 错误、平台特有的 ICMP 触发错误），唯一的上行搬运 goroutine 永久退出：不写任何日志（整个 relay_udp.go/relay_tcp.go 均无 pkg/log 调用）、不通知 ForwardManager、不尝试重建。此后该规则在 GetAllRules/GetRule 中仍显示为活跃，端口仍被 allocator 占用，gcLoop 还在运行，但客户端流量全部黑洞——只有下行 downstreamLoop 在 session 空闲超时后逐个消亡。对一个长期运行的多租户转发面，这类静默死亡既不可观测也不可自愈。至少应记录 error 日志并区分可重试错误（continue + 退避）与致命错误（上报/自杀该规则）。
- **证据**：relay_udp.go:168-179 `n, clientAddr, err := r.conn.ReadFrom(buf); if err != nil { select { case <-r.ctx.Done(): return; default: if ne, ok := err.(net.Error); ok && ne.Timeout() { continue }; return } }`——default 分支的 return 无日志无通知；`grep -n "log\." relay_udp.go relay_tcp.go` 无命中。
- **核实**：relay_udp.go:168-179 确证：ReadFrom 出错时，ctx.Done 则 return、net.Error 超时则 continue、其余错误 default 分支一律 return，杀死唯一上行 readLoop。grep 确认 relay_udp.go/relay_tcp.go/relay.go 全无 pkg/log 调用，故静默；无 ForwardManager 通知、无重建。此后规则仍在 GetAllRules 显示活跃、端口仍被占用、gcLoop 仍运行，上行流量黑洞，仅下行 session 靠 idle 超时逐个消亡——数据面静默死亡且不可自愈的设计缺口成立。唯一保留意见：监听态 UDP PacketConn 的持续性非超时 ReadFrom 错误在 Linux 上较罕见，实际触发频率不高，但可观测性/自愈缺失本身确凿，P2 可接受。行号准确。

### 15. [P2·confirmed] drainEnd map 只增不减（ClearDrain 无调用方），且 RecordActivity 在 TCP 每次 Read/UDP 每个包上抢用户级互斥锁写这张无人读取的 map

- **位置**：`pkg/proxy/forward/clientlimit.go:280` · 维度：资源 · 单元：FWD
- **问题**：remoteIPClientLimiter.drainEnd（clientlimit.go:79）的条目由 OnSingleDirectionEnd（:270）和 RecordActivity（:280）写入，唯一的删除入口 ClearDrain（:296-301）在全仓库没有任何调用方；槽位回收（Release 的 AfterFunc 删 slots）也不清理 drainEnd。由于 limiter 是用户级共享实例、生命周期直到 DropUser，每个曾连接过的 remote IP 都会在 map 里永久驻留一条 time.Time——对暴露公网的转发端口（会被扫描器/大量真实客户端命中）这是无上界的内存增长。更糟的是写入频率：TCP 侧 activityReader 在每次成功 Read（≤32KB 一次）都调用 RecordActivity（relay_tcp.go:380-386 → clientlimit.go:275-281 加 l.mu 全局锁 + map 赋值），UDP 侧 forwardUpstream/downstreamLoop 每个数据包各一次（relay_udp.go:326-328/378-380）。同一用户所有规则的所有连接在高吞吐下持续竞争这把锁，而写入的值因 finding（relay_tcp.go:322 drain 快照）根本无人读取——纯开销纯泄漏。建议：要么让 relay 真正消费 drainEnd 并在 Release/recycle 时清理，要么整体移除该 map 与 per-read 回调。
- **证据**：clientlimit.go:275-281 `func (l *remoteIPClientLimiter) RecordActivity(remoteIP string) { l.mu.Lock(); defer l.mu.Unlock(); l.drainEnd[remoteIP] = time.Now().Add(...) }`；删除入口仅 ClearDrain（:296-301），grep 全仓库无调用；relay_tcp.go:380-386 activityReader 每次 Read 触发。
- **核实**：clientlimit.go:79 drainEnd map 由 OnSingleDirectionEnd(270)/RecordActivity(280) 写入；唯一删除入口 ClearDrain(296-301) 全仓 grep 无调用方；Release(187-242) 及其 AfterFunc 只操作 slots map，从不清理 drainEnd；limiter 为用户级共享实例、生命周期至 DropUser，故每个曾连接的 remote IP 永久驻留一条 time.Time，对公网转发端口无上界增长。写入频率经证实：relay_tcp.go:380-386 activityReader 每次成功 Read(≤32KB) 触发→clientlimit.go:275-281 抢用户级 l.mu 全局锁写 map；relay_udp.go:326-328/378-380 每包一次。该共享锁被同用户所有规则所有连接竞争，而写入值因 idx0 无人读取——纯泄漏纯开销成立。P2 合理，行号准确。

### 16. [P2·confirmed] UpdateRateLimit 对数据面是彻底 no-op，rule 级限速字段仅在用户首条规则时被“提升”为用户级限额，接口注释与实际行为不符

- **位置**：`pkg/proxy/forward/forward_manager.go:606` · 维度：正确性 · 单元：FWD
- **问题**：relay 实际持有的是用户级 userBandwidthLimiter 的 up/down 桶（forward_manager.go:185-188），rule.UploadBytesPerSec/DownloadBytesPerSec 只在 m.userBandwidth[username] 不存在时用于种子化用户级桶（:170-184）。由此产生三个问题：1) UpdateRateLimit（:606-625）只改 mr.rule 字段，既不更新用户级桶也不重建 relay——注释声称 'New connections will use the new limits'（manager.go:50-53 亦然），但新连接用的仍是同一个未变的用户级 limiter，该 API 对流量完全无效；2) 同一用户的第二条规则若带不同的 rule 级限速，被静默忽略（用户桶已存在，不再种子化）；3) rule 级字段的语义实际是“可能成为用户级限额的一次性种子”，与字段名/注释表达的 per-rule 语义相悖，容易让 rpc/http 层调用方误以为设置成功。建议：要么让 UpdateRateLimit 落到 userBandwidth（等价 SetUserBandwidthLimit），要么删除该方法与 rule 级限速字段，收敛到单一的用户级 API。
- **证据**：forward_manager.go:615-617 `mr.rule.UploadBytesPerSec = uploadBPS; mr.rule.DownloadBytesPerSec = downloadBPS` 后仅有注释，无任何 limiter 操作；:170 `if _, ok := m.userBandwidth[rule.Username]; !ok {` 才读取 rule 级限速；:185-188 relay 一律使用 userLimiter.UploadLimiter()/DownloadLimiter()。
- **核实**：forward_manager.go:606-625 UpdateRateLimit 仅改 mr.rule.UploadBytesPerSec/DownloadBytesPerSec(615-617)，无任何 limiter 操作、不动 userBandwidth 桶、不重建 relay。relay 实际用 userLimiter.UploadLimiter()/DownloadLimiter()(185-188)，rule 级限速仅在 `if _, ok:=m.userBandwidth[username]; !ok`(170) 首条规则时种子化用户桶——三项子claim(数据面 no-op、第二条规则不同限速被静默忽略、rule 级字段实为一次性用户级种子)均由代码直证。manager.go:50-53 接口注释‘applies the new limit to new connections’与实际不符。保留意见：grep 确认 UpdateRateLimit 除接口定义与实现外无任何生产调用方(仅测试)，属死且坏的 API，当前无实际调用方受害，实际危害低于 detail 所述‘误导 rpc/http 层’——就此而言 P2 略偏高(可视 P3)，但机械行为claim完全成立，故 confirmed。行号准确。

### 17. [P2·confirmed] maxConns 检查基于 dial 成功后才自增的 activeConns，拨号窗口内可无限突破 MaxConnections

- **位置**：`pkg/proxy/forward/relay_tcp.go:144` · 维度：并发 · 单元：FWD
- **问题**：acceptLoop 在接受连接时用 `atomic.LoadInt64(&r.activeConns) >= int64(r.maxConns)` 做上限检查（relay_tcp.go:144），但 activeConns 直到 handleConn 中 DialTimeout 成功后才 +1（:163-175，注释明言 '+1 only happens after connection is fully established'）。拨号是异步 goroutine 且超时长达 5 秒：当目标容器端口暂时不可达/半死（进程重启、backlog 满）时，acceptLoop 可以在 5 秒窗口内接受任意数量的新连接，每个都通过 maxConns 检查并各起一个 handleConn goroutine + 一次未完成的 dial——MaxConnections 完全失效，且瞬时 goroutine/fd 数不受控（每个 pending 占 1 个已 accept 的 fd + 1 个 dialing fd）。对照 clientLimiter 用 pending 计数解决了同样的问题（Acquire 先 +pending）。建议 maxConns 也采用 pending+active 双计数，或在 accept 时先行 +1、dial 失败再回退。UDP 侧无此问题（establishSession 同步建会话）。
- **证据**：relay_tcp.go:144 `if r.maxConns > 0 && atomic.LoadInt64(&r.activeConns) >= int64(r.maxConns)` 在 acceptLoop；:163 `targetConn, err := net.DialTimeout("tcp", r.targetAddr, defaultDialTimeout)`（5s）之后 :175 才 `atomic.AddInt64(&r.activeConns, 1)`——检查与自增之间隔着异步慢路径。
- **核实**：属实。relay_tcp.go:144 用 activeConns 做上限检查，而 activeConns 直到 handleConn 内 DialTimeout(5s，:163) 成功后 :175 才自增；handleConn 是 :153 go 出去的异步 goroutine，acceptLoop 单线程接受但每次都立即放行。目标端口半死/backlog 满时，5s 拨号窗口内所有新连接均通过 maxConns 检查并各起 goroutine+dial，MaxConnections 失效。对照 clientlimit.go:104-119 Acquire 先 Add pendingConns，确实用 pending 计数堵住了同样的窗口，说明 maxConns 是唯一未做 pending 保护的路径。触发路径真实存在。

### 18. [P2·confirmed] acceptLoop 对持续性 Accept 错误无退避地 continue，EMFILE 等场景下热自旋烧满 CPU

- **位置**：`pkg/proxy/forward/relay_tcp.go:121` · 维度：资源 · 单元：FWD
- **问题**：acceptLoop 的错误分支在 ctx 未取消时一律视为 transient 并立即 continue（relay_tcp.go:114-122），没有 net.Error 分类、没有指数退避、也没有日志。当进程 fd 耗尽（EMFILE/ENFILE，代理转发面在连接风暴下并不罕见）或监听 socket 进入持续报错状态时，Accept 会连续立刻返回错误，该 goroutine 变成 100% CPU 的热循环；每条规则一个 acceptLoop，多规则同时进入该状态会烧掉多核。Go 标准做法（net/http.Server.Serve）是对 Temporary/持续错误做 5ms→1s 指数退避并记录日志。UDP readLoop 走了另一个极端（直接退出，见另一条 finding），两者都应统一为“记日志 + 有界退避重试 + 致命错误上报”。
- **证据**：relay_tcp.go:113-122 `conn, err := r.listener.Accept(); if err != nil { select { case <-r.ctx.Done(): return; default: // transient error, continue; continue } }` —— default 分支无 sleep/backoff/log。
- **核实**：属实。relay_tcp.go:113-122 的错误分支在 ctx 未取消时只有裸 continue，无 net.Error 分类、无退避、无日志。EMFILE/ENFILE 或监听 socket 持续报错时 Accept 立即返回错误，该 goroutine 进入 100% CPU 热自旋，每规则一个 acceptLoop。net/http.Server.Serve 的标准做法确有指数退避，此处缺失，属真实健壮性缺陷。P2 偏高但可接受（需 fd 耗尽等条件触发）。

### 19. [P2·confirmed] RemoveRulesByUser/RemoveRulesByInbound 两段式删除遇到并发已删的 key 即中止并返回错误，剩余规则残留

- **位置**：`pkg/proxy/forward/forward_manager.go:346` · 维度：错误处理 · 单元：FWD
- **问题**：两个批量删除方法都先持锁收集 keysToRemove，释放锁后再逐个调用 RemoveRule（forward_manager.go:335-351/355-371）。窗口期内若某条规则已被并发的 RemoveRule/Close/另一批量删除移除，RemoveRule 会返回 'rule not found'（:320-323），批量循环在 :346-348 直接 `return err`——后续 key 不再处理。结果：一次用户删除/inbound 下线只拆掉了部分 relay，剩余规则继续监听端口、占用 allocator 配额并对已删除用户继续转发流量，而调用方（usermanager 的用户删除、inbound 销毁路径）只拿到一个语义上并不致命的 not-found 错误。批量删除对 not-found 应视为幂等成功（continue），仅对真实的 Stop/释放失败中止或聚合返回。
- **证据**：forward_manager.go:343-350 `m.mu.Unlock(); for _, key := range keysToRemove { if err := m.RemoveRule(key); err != nil { return err } }`；RemoveRule 对缺失 key 返回错误（:320-323 `return fmt.Errorf("forward_manager: rule %q not found", ruleKey)`）。
- **核实**：属实。forward_manager.go:345-350 与 365-370 释放锁后逐个 RemoveRule，遇 err 立即 return；RemoveRule 对缺失 key 在 :321-322 返回 not-found 错误。窗口期内并发 RemoveRule/Close/另一批量删除移除了某 key 时，批量循环在该 key 处中止，keysToRemove 中其后的规则不再处理，残留 relay 继续监听端口、占配额、对已删用户转发。not-found 语义上应视为幂等成功。manager 有用户删除/inbound 销毁/Close 多个入口，并发可达。触发路径真实。

### 20. [P2·confirmed] QueryTrafficStats 带过滤条件且 Reset=true 时会清零所有规则的计数并丢弃未匹配规则的流量

- **位置**：`pkg/proxy/forward/forward_manager.go:491` · 维度：正确性 · 单元：FWD
- **问题**：QueryTrafficStats 对 rules 全量迭代，先执行 `mr.counter.Snapshot(query.Reset)`（Reset=true 时原子 swap 清零），之后才应用 Username/ContainerType/InboundTag/RuleKey 过滤并 continue（forward_manager.go:490-517）。被过滤掉的规则其计数已被 swap 为 0，而 swap 出来的值被直接丢弃——这部分流量从统计口径中永久消失。架构约束明确'流量统计只信 forward 层 relay 计数'，该层是计费/配额的唯一事实来源，任何静默清零都是数据丢失。当前仓库内该方法暂无非测试调用方（usermanager 走 GetAllTrafficRecords(false)），但它是 ForwardManager 公开接口（manager.go:46-48）且 TrafficQuery 同时暴露过滤字段与 Reset 字段，调用组合完全合法。修复：应先过滤后 Snapshot，或在文档禁止过滤+Reset 组合。
- **证据**：forward_manager.go:490-517：`upload, download, active := mr.counter.Snapshot(query.Reset)` 在四个 `if query.X != "" && ... { continue }` 过滤之前执行；traffic.go:58-64 Snapshot(reset=true) 用 atomic.SwapInt64(&tc.upload, 0) 清零
- **核实**：forward_manager.go:491 对全部 rules 先执行 mr.counter.Snapshot(query.Reset)，四个过滤 continue 在 506-517 之后；traffic.go:58-64 Snapshot(reset=true) 用 atomic.SwapInt64 清零。被过滤规则的计数已被 swap 为 0 且 swapped 值既不进 matchingRecords 也不累加 total，永久丢失，属实。已核实全仓库仅 test mock 实现该方法、无非测试调用方(usermanager 走 GetAllTrafficRecords(false))，故为潜伏缺陷；但 QueryTrafficStats 是公开接口且 TrafficQuery 同时暴露过滤字段与 Reset，组合合法可达。缺陷成立，P2 合理。行号准确。

### 21. [P2·confirmed] rule 级限速字段语义失真：仅在用户限速器首建时作为种子生效并被提升为用户级，后续规则的字段被静默忽略；UpdateRateLimit 实际是无提示的 no-op

- **位置**：`pkg/proxy/forward/forward_manager.go:170` · 维度：正确性 · 单元：FWD
- **问题**：ForwardRule.UploadBytesPerSec/DownloadBytesPerSec 注释声称是 per-rule 限速（types.go:47-51），但实现上只有当 `m.userBandwidth[rule.Username]` 不存在时才用第一条规则的值种子化用户级共享桶（forward_manager.go:170-184）——这有两个偏差：(1) 第一条规则的 rule 级限速被提升为用户级，限制该用户之后所有规则/inbound 的聚合带宽；(2) 该用户后续 AddRule 携带的不同限速值被静默丢弃，无日志无错误。配套的 UpdateRateLimit（forward_manager.go:606-625）只修改 mr.rule 的两个字段副本供 GetRule 读取，对运行中 relay 及其共享桶零影响，却返回 nil；其注释与 manager.go:50-53 接口文档都声称 'New connections will use the new limits'——但 relay 和 limiter 都不会因此重建，新连接同样拿旧限速，文档承诺与实现不符。调用方（rpc/http 层）如果依赖该接口实现'修改单规则限速'功能将得到成功返回但完全无效果。建议：要么移除 rule 级限速字段与 UpdateRateLimit（统一走 SetUserBandwidthLimit），要么真正实现 per-rule 桶。
- **证据**：forward_manager.go:170-184 `if _, ok := m.userBandwidth[rule.Username]; !ok { ... Seed from rule-level limits ... }`（仅首建生效）；forward_manager.go:615-624 UpdateRateLimit 仅 `mr.rule.UploadBytesPerSec = uploadBPS` 后直接 return nil，注释自认 'existing relay connections keep the old limiter'
- **核实**：forward_manager.go:170-184 仅在 userBandwidth[username] 不存在(该用户首条规则)时才用 rule 级限速种子化用户级共享桶；同用户后续 AddRule 因 !ok 为 false 跳过种子化，rule 级字段被静默忽略，且 limiterUp/Down 取自共享用户桶——语义确实被提升为用户级。UpdateRateLimit(606-625) 只改 mr.rule 两个字段副本、注释自认 existing relay 保留旧 limiter、return nil，而 relay 与共享桶均不重建，新连接同样拿旧限速，与 manager.go:50-53 及方法注释‘new connections will use the new limits’矛盾。文档-实现不符成立，P2 合理。行号准确。

### 22. [P2·confirmed] UDP readLoop 非超时错误静默退出：relay 变僵尸但规则/端口在 manager 中仍显示活跃，无日志无自愈

- **位置**：`pkg/proxy/forward/relay_udp.go:177` · 维度：错误处理 · 单元：FWD
- **问题**：readLoop 在 ReadFrom 返回非超时、非 ctx-done 的错误时直接 return（relay_udp.go:169-179），整个 UDP 转发面停止工作：不再接收任何客户端包。但 gcLoop 仍在运行、规则仍留在 DefaultForwardManager.rules、端口仍被 allocator 占用，GetAllRules/GetAllTraffic 全部显示该规则正常。没有任何日志输出，没有向 manager 上报失败状态，也没有重建 socket 的自愈逻辑——用户表现为'该端口的 UDP 突然全部丢包'且服务端无任何可观测线索。与 TCP acceptLoop 对错误无限 continue 的策略（见另一条 finding）方向相反，两种 relay 对持续性 I/O 错误缺乏统一的、可观测的降级策略。至少应记录 error 日志；更完整的方案是向 manager 暴露 relay 健康状态或自动重建 listen socket。
- **证据**：relay_udp.go:169-179：`if err != nil { select { case <-r.ctx.Done(): return; default: if ne, ok := err.(net.Error); ok && ne.Timeout() { continue }; return } }` — 非超时错误路径无日志直接 return
- **核实**：relay_udp.go:169-179 属实：ReadFrom 返回非超时、非 ctx-done 错误时无日志直接 return，readLoop 退出后不再接收任何客户端包；而 gcLoop 仍运行、规则仍在 rules map、端口仍被 allocator 占用、GetAllRules/Traffic 仍显示正常，无可观测线索也无自愈。且代码对 transient 与 permanent 错误不加区分，任何一次非超时错误即永久终止转发面，鲁棒性缺陷成立。P2 合理，行号锚点落在错误处理块内准确。

### 23. [P2·confirmed] RemoveRulesByUser/ByInbound 收集与删除两段式非原子：并发删除触发 'not found' 会中断批删，剩余规则泄漏

- **位置**：`pkg/proxy/forward/forward_manager.go:345` · 维度：并发 · 单元：FWD
- **问题**：两个批删方法都先持锁收集 key，释放锁后逐个调 RemoveRule（forward_manager.go:335-371）。窗口期内若同一 key 已被并发的 RemoveRule/Close/另一批删移除，RemoveRule 返回 'rule not found' 错误，循环 `if err := m.RemoveRule(key); err != nil { return err }` 立即中断——列表中排在其后的规则不再被删除，relay 与端口继续存活。调用方 usermanager.go:580/1391 忽略返回值，既不知道批删中断也不会重试，形成规则泄漏（用户已删除但转发端口仍开放，属安全暴露面）。另外窗口期内如果同 key 的规则被重新 AddRule，批删会误删新规则。修复：not-found 应视为幂等成功继续循环，或整个批删在一次持锁内完成（先 relay.Stop 的耗时问题可用先摘表后停机的两段提交解决）。
- **证据**：forward_manager.go:335-351：`m.mu.Unlock()` 后 `for _, key := range keysToRemove { if err := m.RemoveRule(key); err != nil { return err } }`；RemoveRule（320-322）对不存在的 key 返回 error；usermanager.go:580 `m.forwardMgr.RemoveRulesByUser(username)` 未检查返回值
- **核实**：forward_manager.go:335-351(及 355-371 的 ByInbound)确为两段式：持锁收集 key、解锁后逐个 RemoveRule 且 `if err != nil { return err }`；RemoveRule(320-322)对不存在 key 返回 error。并发 RemoveRule/Close/另一批删在窗口期移除同一 key 会使 not-found 中断循环，其后规则不再删除，relay 与端口泄漏。已核实调用方 usermanager.go:580 忽略返回值、1391 用 `_ =` 丢弃，既不感知中断也不重试。窗口期同 key 被 re-AddRule 时会误删新规则的边界亦成立。缺陷成立，P2 合理，行号准确。

### 24. [P2·confirmed] 自动分配端口与内核临时端口区间重叠且 bind 失败不换端口重试，AddRule 会因瞬时端口占用直接失败

- **位置**：`pkg/proxy/forward/forward_manager.go:294` · 维度：错误处理 · 单元：FWD
- **问题**：默认端口池 10000-65535（forward_manager.go:34-38, global.go:13-17）与 Linux 默认 ip_local_port_range（32768-60999）大面积重叠。PortAllocator 只保证进程内唯一，无法感知 OS 层占用：本进程或宿主机其他进程的出站连接随时可能占用池内端口作为源端口。Allocate 选中这样的端口后 relay.Start 的 net.Listen 失败，AddRule 释放端口并直接返回错误（forward_manager.go:294-299），没有换一个端口重试的循环。上层（usermanager 恢复 PortMappings、批量为用户建规则）遇到这种概率性失败只能整体失败或跳过该用户。修复成本很低：对自动分配路径（rule.ListenPort==0）在 Start 失败且错误为 EADDRINUSE 时循环重新 Allocate 若干次；或文档要求部署时将端口池与 ip_local_port_range 错开。
- **证据**：forward_manager.go:294-299 `if err := relay.Start(); err != nil { m.allocator.Release(port); m.traffic.Remove(key); ... return nil, ... }` 无重试；默认范围 10000-65535 覆盖 Linux 临时端口 32768-60999
- **核实**：默认端口池 10000-65535(global.go:13-17、forward_manager.go:34-38)与 Linux 默认 ip_local_port_range(32768-60999)大面积重叠属实；PortAllocator 仅保证进程内唯一无法感知 OS 层占用。forward_manager.go:294-299 relay.Start(net.Listen)失败即 Release+return，自动分配路径(ListenPort==0)无换端口重试循环，属实。监听端口撞上被内核用作出站源端口的临时端口时 bind 可概率性 EADDRINUSE，虽发生频率取决于部署/SO_REUSEADDR 但重叠与缺重试的代码事实确凿，属真实健壮性缺陷。P2 合理，行号准确。

### 25. [P2·confirmed] 测试缺口：drain 状态机、既有连接限速生效、QueryTrafficStats 过滤+Reset 三个关键行为均无覆盖

- **位置**：`pkg/proxy/forward/relay_test.go:379` · 维度：测试缺口 · 单元：FWD
- **问题**：三个本次发现的缺陷都恰好落在测试盲区：(1) TestRelay_HalfCloseRecovery（relay_test.go:379）只测两端都关闭后的资源回收，没有'一个方向 EOF 后另一方向持续活跃传输超过 SingleDirectionDrainSec'的用例——正是 drain 失效切断活跃传输的场景；(2) integration_test.go:409-469 的 SetUserBandwidthLimit-after-AddRule 测试只断言共享 TokenBucket 的 GetRate 数值和引用相等，从未在设限前建立连接并验证该连接随后被实际节流——正是 LimitReader 短路旁路的场景；(3) QueryTrafficStats 无带过滤条件+Reset=true 组合、验证未匹配规则计数不被清零的测试。这三个用例应作为修复上述缺陷的回归测试补齐。
- **证据**：relay_test.go:377-427 半关闭测试立即 conn.Close() 两端；integration_test.go:440-469 仅 `tcpLim.GetRate() != 123456` 断言；forward_manager_test.go 无 QueryTrafficStats+Reset+filter 用例（grep 无匹配）
- **核实**：三处测试盲区均属实。(1) TestRelay_HalfCloseRecovery(relay_test.go:379-427)确实立即 conn.Close() 两端，只验证资源回收，无"一方向 EOF 后另一方向持续活跃"用例；且底层缺陷真实存在——handleConn 循环用局部 drainDeadline(relay_tcp.go:229/251/277/322)，RecordActivity 只改 l.drainEnd map 不改局部变量，故 drainSec 到期即截断活跃传输。(2) integration_test.go:411-470 仅断言 GetRate/rate==123456，未在设限前建连并验证节流。(3) grep 全仓库测试无任何 QueryTrafficStats 引用；QueryTrafficStats(forward_manager.go:491)在 filter(506-517)前对每条规则调用 Snapshot(query.Reset)，Reset=true+过滤会清零非匹配规则，缺陷真实。注：与 idx5 高度重叠。

### 26. [P2·confirmed] SetUserClientLimitConfig→SetConfig 不回填默认值，drain/recycle 为 0 时连接被立即强制关闭

- **位置**：`pkg/proxy/forward/clientlimit.go:304` · 维度：错误处理 · 单元：FWD
- **问题**：AddRule 路径在 effectiveMaxClients>0 时会把 RecycleDelaySec 回填 60、DrainSec 回填 2（forward_manager.go:230-237），但 SetUserClientLimitConfig 直接把调用方 config 原样存入并 SetConfig（forward_manager.go:734-739），而 SetConfig 只做 `l.config = config`（clientlimit.go:304-308），无任何回填。若管理员调用 SetUserClientLimitConfig(MaxClients=5) 但把 RecycleDelaySec/SingleDirectionDrainSec 留 0：OnSingleDirectionEnd 返回 now+0（clientlimit.go:269），任一方向刚结束下一次 idleTick(100ms) 即判定 drain 过期强制关闭另一方向；Release 用 recycleDelay=0 触发 time.AfterFunc(0)（clientlimit.go:210,223）几乎立刻回收槽位。这是限连边界值缺乏校验/回填导致的行为异常。
- **证据**：clientlimit.go:304-308 SetConfig 仅 `l.config = config` 无回填；对比 newRemoteIPClientLimiter(clientlimit.go:88-100) 与 AddRule(forward_manager.go:230-237) 均回填默认；OnSingleDirectionEnd 用 SingleDirectionDrainSec(clientlimit.go:269)、Release 用 RecycleDelaySec(clientlimit.go:210)。
- **核实**：确证且比描述更严重。SetUserClientLimitConfig(forward_manager.go:728-741)原样存 config 并对已有 limiter 调 SetConfig；SetConfig(clientlimit.go:304-308)仅 l.config=config，零回填。而 newRemoteIPClientLimiter(88-94)与 AddRule(forward_manager.go:230-237)均回填 60/2。更关键：AddRule 注释(226-229)明写"SetConfig paths backfill defaults anyway"，代码违反自身文档契约。MaxClients>0 且 drain/recycle=0 时，OnSingleDirectionEnd(269)返回 now，下一 idleTick(100ms)即 time.Now().After(drainDeadline) 强制关闭另一方向；Release(210,223) recycleDelay=0 触发 AfterFunc(0)。行号 304 准确。P2 对于管理员可触发+违反文档不变量+截断合法传输，合理。

### 27. [P2·confirmed] RotateUserPortForInbound 全程不持 m.mu，与 RemoveUser/GetBindPort 交错会复活转发规则、污染 tombstone 状态

- **位置**：`pkg/proxy/usermanager/usermanager.go:1013` · 维度：并发 · 单元：UM
- **问题**：Step1-4(1013-1086)对 forwardMgr 做 GetRule/AddRule(临时)/RemoveRule(旧)/RemoveRule(临时)/AddRule(正式) 全部在不持 m.mu 的情况下进行,只有 Step5(1089)才短暂加锁更新 user。若并发 RemoveUser 在中途执行(mutateUser 标 tombstone 并 emit UserEventRemove,随后 DropUser 拆掉该用户所有 relay),rotate 之后建立的正式 relay 会被复活/残留,且 Step5 会给已 tombstone 的 user 追加 BindPorts/PortMappings(1102-1106)并持久化,产生「已删除用户仍有转发规则」的泄漏与状态不一致。emit(1114-1119)也在 m.mu 释放后读取 user 指针,与并发 mutateUser 存在数据竞态面。整个 make-before-break 缺少与 CRUD 的互斥或 tombstone 复检。
- **证据**：usermanager.go:1015 GetRule、1045 AddRule、1052/1060 RemoveRule、1076 AddRule 均在 m.mu 之外;m.mu.Lock 直到 1089 才出现;1090 之后未复检 user.IsDeleting()。
- **核实**：已核实:RotateUserPortForInbound 的 GetRule/AddRule/RemoveRule(1015-1086)全部在 m.mu 之外,m.mu.Lock 直到 1089;Step5(1090)仅 `user,exists:=m.users[username]`,tombstone 用户仍在 map 中故 exists=true,但未复检 user.IsDeleting()→会给正在删除的用户追加 BindPorts/PortMappings(1096-1106)并 store.Save,再 emit(1114)。并发 RemoveUser(mutateUser 352 标 tombstone+emit UserEventRemove,容器 subscriber 异步 RemoveRulesByUser 拆规则)与 rotate 交错时,rotate 在拆除后新建的 finalRule(1076)可残留为'已删除用户仍有 relay'。触发路径真实:两者均为并发 RPC handler(end_node_user.go:345/358 vs RemoveUser)。与仓库 IsDeleting 纪律(GetUser 605、FindUserByToken 626、AddUser 480 均检查)不一致。P2 成立。行号准确。

### 28. [P2·confirmed] SyncUpsertUser 更新分支 token 冲突重生后不 re-stamp，导致 AuthToken 与 Hash/版本跨节点分裂

- **位置**：`pkg/proxy/usermanager/usermanager.go:1591` · 维度：正确性 · 单元：UM
- **问题**：更新分支在 setAuthTokenLocked(existing, incoming.AuthToken) 失败(与本地其它用户 token 冲突)时,重生成一个全新 UUID 写入 existing.AuthToken(1592),随后却照单全收 incoming 的版本字段,包括 existing.Hash = incoming.Hash(1599)、UpdatedAtUs/OriginNode(1597-1598),不重新 stampVersion。结果:本地 AuthToken 是新随机值,但 Hash/版本仍是远端(基于旧 token 计算)的值 → 记录自身违反 Hash==ComputeHash 不变式;而 ListDigests 上报的是 incoming.Hash,对端据此认为「同版本同哈希、已一致」,两节点对同一用户永久持有不同 AuthToken 且不再收敛(split-brain)。对比新用户分支(1562-1566)在同类冲突时会 stampVersion 让本节点成为 origin 从而收敛,两分支处理不一致。
- **证据**：usermanager.go:1591-1593 冲突后 `_ = m.setAuthTokenLocked(existing, "")` 重生 token,后续 1599 `existing.Hash = incoming.Hash` 无 stampVersion;新用户分支 1565 有 `m.stampVersion(incoming)`。ComputeHash(hash.go:46) 含 AuthToken。
- **核实**：已核实:更新分支 1591 setAuthTokenLocked(existing,incoming.AuthToken) 冲突返回 error 时,1592 setAuthTokenLocked(existing,"") 经 54-58 生成新随机 UUID,随后 1597-1599 照抄 incoming 的 UpdatedAtUs/OriginNode/Hash,不 stampVersion → existing.AuthToken 为本地新值,existing.Hash=incoming.Hash(基于远端旧 token)。ComputeHash(hash.go:46)含 AuthToken,故记录违反 Hash==ComputeHash 不变式;ListDigests(1642-1656)上报 existing.Hash=incoming.Hash,对端据 UpdatedAtUs/OriginNode/Hash 判定已一致而不再下发 → 两节点同用户永久持不同 AuthToken(split-brain)。对比新用户分支 1564-1565 冲突后有 stampVersion(incoming) 使本节点成 origin 并触发收敛,两分支处理不一致确凿。token 跨节点冲突为罕见边界,P2 合理。行号准确。

### 29. [P2·confirmed] GetBindPort 未检查 IsDeleting，可为 tombstone 用户分配端口并建 relay

- **位置**：`pkg/proxy/usermanager/usermanager.go:867` · 维度：正确性 · 单元：UM · 旧 review: U-05
- **问题**：GetBindPort 在锁内只校验 exists(867-870)与 IsExpired(873-875),没有任何 user.IsDeleting() 判定,便进入 AddRule 分配端口、写 BindPorts/PortMappings 并 emit UserEventPortBind。若某容器在收到 UserEventRemove 之后、tombstone 清理完成之前(两阶段删除窗口)再次触发 GetBindPort,会给正在删除的用户重新建立转发链路,与随后的 DropUser/清理竞争,造成端口/relay 泄漏,并与 AddUser 重建时「必须无残留 forward 规则」的前提冲突。
- **证据**：usermanager.go:867-875 仅 exists 与 IsExpired 检查;全函数(855-967)无 IsDeleting 调用。对比 AddUser 480 处对 IsDeleting 的显式处理。
- **核实**：已核实:GetBindPort(855-967)锁内仅校验 exists(867)与 IsExpired(873),全函数无 IsDeleting 调用,tombstone 用户(仍在 m.users)会进入 AddRule 分配端口、写 BindPorts/PortMappings、emit UserEventPortBind。与仓库一致纪律相悖:GetUser 605、FindUserByToken 626、AddUser 480 均显式过滤/处理 IsDeleting。两阶段删除窗口真实(RemoveUser 553 MarkDeleting+异步 subscriber 清理),若删除中并发触发 GetBindPort(例如 SyncUpsertUser 复活/容器 setup 与删除竞争)会为删除中用户重建 relay,与 AddUser 480-489'重建前必须无残留 forward 规则'前提冲突,造成端口/relay 泄漏。P2 成立。行号准确。

### 30. [P2·confirmed] emitEvent 通道满时静默丢弃事件，丢失 Remove/PortBind 导致 relay/inbound 泄漏

- **位置**：`pkg/proxy/usermanager/usermanager.go:445` · 维度：资源 · 单元：UM
- **问题**：emitEvent 对 legacy eventCh 与每个 subscriber 均用非阻塞 select+default 丢弃(445-459)。容器侧完全依赖 UserEventRemove 拆 inbound、UserEventPortBind/Release 做端口清理,一旦订阅者消费慢于突发(缓冲 100),关键的删除/端口事件被静默丢弃 → 对应 relay 与 inbound 永久泄漏,且 forward 规则不清、tombstone 无法收尾。当前没有任何丢弃计数/告警/背压,消费速度是隐式契约。建议至少对 Remove/PortBind 这类清理型事件采用阻塞或有界重试,或记录丢弃指标。
- **证据**：usermanager.go:448 `default: // Channel full, event dropped`,456-458 subscriber 同样 `default`;订阅者通道容量在 Subscribe() 404 `make(chan UserEvent, 100)`。
- **核实**：代码属实:emitEvent 445-459 对 legacy eventCh(448 default)与每个 subscriber(456 default)均非阻塞丢弃,Subscribe() 404 缓冲固定 100,无丢弃计数/背压/告警。mutateUser 的 Remove(376-382 携带 BindPorts)、GetBindPort/ReleaseBindPort 的 PortBind 均只经此单一事件通道通知容器层,一旦突发消费慢于缓冲即静默丢弃 → 清理型事件缺失,relay/inbound 与端口无从收尾。行号准确。注意:'永久泄漏'成立的前提是容器消费侧无独立全量对账机制且突发确实超过 100,该对账逻辑在本单元之外未能证伪;但'静默丢弃关键清理事件且无任何可观测性'这一缺陷本身在本单元内已确证。

### 31. [P2·confirmed] mutateUser/GetBindPort/ReleaseBindPort 在持 m.mu 写锁期间执行 store.Save(SQLite IO)

- **位置**：`pkg/proxy/usermanager/usermanager.go:370` · 维度：并发 · 单元：UM · 旧 review: U-04
- **问题**：mutateUser 在 m.mu 写锁内调用 store.Save(370-374);GetBindPort 在写锁内 AddRule+Save(927/953);ReleaseBindPort 在写锁内 RemoveRulesByUser+Save(1361/1366)。SQLite 落盘属阻塞 IO,持写锁执行会串行化所有 List*/Get*/FindUserByToken 等读路径,尤其 onCollect 逐用户 Save 与这些写路径叠加时,users map 的全部读者被 IO 卡住,高并发/大用户量下产生明显 p99 抖动甚至请求堆积。Save 应在快照字段后于锁外进行,或改用异步持久化队列。
- **证据**：usermanager.go:353 `m.mu.Lock()` + 370 `m.store.Save(user)`;GetBindPort 864 Lock 覆盖 927 AddRule 与 953 Save;ReleaseBindPort 1312 Lock 覆盖 1361 RemoveRulesByUser 与 1366 Save。
- **核实**：代码属实:mutateUser 353 m.mu.Lock 覆盖 371 store.Save;GetBindPort 864 Lock 覆盖 927 AddRule 与 953 Save;ReleaseBindPort 1312 Lock 覆盖 1361 RemoveRulesByUser 与 1366 Save,均为写锁(Lock 非 RLock)。List*/Get*/FindUserByToken 走 RLock 会被写锁+落盘 IO 串行阻塞,onCollect(2113-2144)逐用户 Save 亦持 m.mu。作者自己在 2105-2112 的 TODO 也确认了同类并发/IO 顾虑,佐证该性能问题真实。行号准确,P2 合理。

### 32. [P2·confirmed] SyncUpsertUser「非更新但采纳 LoginPassword」分支改字段不重算 Hash，破坏 Hash 不变式

- **位置**：`pkg/proxy/usermanager/usermanager.go:1504` · 维度：正确性 · 单元：UM
- **问题**：当远端版本 not-newer 时,若本地 LoginPassword 为空而远端非空,分支直接 existing.LoginPassword = incoming.LoginPassword 并 store.Save(1504-1508),但不重新 ComputeHash、不更新 UpdatedAtUs、不 emit。由于 ComputeHash 在 LoginPassword 非空时会纳入该字段(hash.go:60-61),改动后本地存储/上报的 u.Hash 不再等于 ComputeHash(u)。对端心跳比对时同版本却哈希不一致,会反复把该用户列入 needFull 拉全量(CompareDigests 66-70),形成持续无收益的重复同步流量。采纳 password 后应同步 stampVersion 或至少重算 Hash 保持不变式。
- **证据**：usermanager.go:1504-1509 仅赋值 LoginPassword + store.Save,无 Hash 重算;ListDigests 1651 上报存储的 u.Hash;CompareDigests digest.go:66-70 同版本哈希不一致即拉全量。
- **核实**：核心缺陷属实:1504-1509 采纳 incoming.LoginPassword 后仅 store.Save,不调用 ComputeHash/stampVersion;而 stampVersion(330)在正常路径保证 u.Hash==ComputeHash(u),ComputeHash(hash.go 60-61)在 LoginPassword 非空时纳入该字段,故此分支后本地 Hash 不再等于 ComputeHash,不变式确被破坏,应重算 Hash——修复方向正确。但 detail 描述的后果链有误:CompareDigests(digest.go 66-70)的'同版本哈希不一致拉全量'不会被触发,因为该 stale hash 在集群内是对称的(SyncUpsertUser 1538/1599 只复制 incoming.Hash 从不重算,唯一能为版本(tsB,originB)产出'含密码'hash 的只有 origin 自身而它没重算),不存在同版本不同 hash 的两节点,因而不会形成'持续无收益的重复同步流量'。实际风险恰相反:hash 碰撞掩盖了密码差异,导致密码在部分节点静默不传播(欠同步),而非风暴。判定 confirmed 是针对已被代码证实的不变式破坏本身;后果机制方向被夸大/写反,严重度维持 P2 尚可(属集群数据一致性正确性问题)。

### 33. [P2·confirmed] GetAllDeltaTraffic 读取与重置分两次独立取锁,中间 collect() 累积的 delta 被丢弃

- **位置**：`pkg/proxy/usermanager/usermanager.go:2242` · 维度：并发 · 单元：UM · 旧 review: U-02
- **问题**：GetAllDeltaTraffic(reset=true)先 GetStats()(取 sc.mu.RLock 读快照后释放),再 resetAllDeltas()(重新取 sc.mu.Lock 清零)。两次加锁之间无互斥,collectLoop 的一个 collect() tick 可在读与重置之间执行并把新增流量累加进 DeltaUplink/Downlink;随后 resetAllDeltas 无条件清零,这部分增量既没被本次 DrainStats 返回也被清掉,Prometheus/center 上报永久丢失该窗口流量。应在同一把 sc.mu 临界区内完成读+重置。
- **证据**：agg := m.statsCollector.GetStats()(2237,内部 RLock 后释放)... if reset { m.statsCollector.resetAllDeltas() }(2242-2244,resetAllDeltas 2009-2011 重新 Lock),两步之间无锁保护。
- **核实**：证实读与重置非原子:GetAllDeltaTraffic 先 GetStats()(2237,内部 RLock 后释放)再 resetAllDeltas()(2242-2244,resetAllDeltas 2009-2011 重新 Lock),两步之间无互斥;若 collect()(每 tick 需 Lock)恰在此窗口执行并累加 delta,则该增量既不在已构造的 result 中、又被无条件清零而永久丢失。逻辑成立。但降为 P2:两次调用紧邻(中间仅一个 append 循环),窗口在微秒级,且默认 collect tick 为 1 分钟一次,命中概率极低、丢失量有界;属流量计量精度问题而非崩溃,P1 偏高。

### 34. [P2·confirmed] emitEvent 对满的订阅通道非阻塞丢弃事件,丢失 UserEventRemove/PortBind 会泄漏 relay/inbound 端口

- **位置**：`pkg/proxy/usermanager/usermanager.go:453` · 维度：错误处理 · 单元：UM
- **问题**：emitEvent 向 legacy channel 和每个 subscriber 都用 select{ case ch<-event: default: } 非阻塞发送,通道满即静默丢事件(445-459)。容器订阅者依赖 UserEventRemove 拆 inbound、依赖 UserEventPortBind/Release 调 ReleaseBindPort 回收端口。订阅通道容量仅 100,在批量删除/过期清理/集群大批同步(SyncUpsertUser 逐条 emit)或订阅方消费慢时会溢出;一旦丢掉 Remove/PortBind 事件,对应 forward relay 与后端 inbound 端口不会被清理,形成端口与进程资源泄漏,且无任何日志/计数暴露丢弃。缺乏背压或至少丢弃告警。
- **证据**：select { case m.eventCh <- event: default: }(445-450)与 for _, sub := range m.subscribers { select { case sub <- event: default: } }(453-459);订阅通道 make(chan UserEvent, 100)(404)。
- **核实**：证实 emitEvent(445-459)对 legacy channel 与每个 subscriber 均用 select{case ch<-event: default:} 非阻塞发送,满即静默丢弃,无日志/计数;订阅通道 cap 仅 100(404)。消费方确实依赖 UserEventRemove 拆 relay/释放端口(hysteria/container.go:513、mihomo:837、snell:411、xray:950 均 case UserEventRemove 做拆除)。批量同步/过期清理逐条 emit 且消费方 forwardUserEvents 再入二级 buffered channel,持续突发超过消费速率即可溢出,丢 Remove/PortBind 造成 relay 与 inbound 端口泄漏。机制与后果均成立,缺背压/告警属实,P2 合理(丢弃系有意 drop-over-block,缺陷点在可观测性与关键事件无保障)。

### 35. [P2·confirmed] IsNewer 仲裁忽略内容 Hash:同版本+同 OriginNode 但内容不同的记录永不收敛,并造成心跳持续拉全量抖动

- **位置**：`pkg/proxy/usermanager/sync/version.go:10` · 维度：正确性 · 单元：UM
- **问题**：IsNewer 只比较 UpdatedAtUs,相等时按 OriginNode 字典序 tie-break(version.go:11-14)。当两节点对同一用户持有相同 UpdatedAtUs 且相同 OriginNode 但 Hash 不同(例如同一节点在同一微秒内二次 stampVersion 后分叉传播,或由上面两个 Hash 失真 bug 诱发)时:CompareDigests 的“同版本+同 OriginNode+Hash 不同→needFull”分支(digest.go:66-70)会在每次心跳都把该用户列入待拉全量;拉回后 SyncUpsertUser 首段 `exists && !usync.IsNewer(incoming, existing)`(usermanager.go:1499)因 IsNewer 返回 false 而直接 return false(跳过采纳)。于是每个心跳周期都拉一次全量却被丢弃,既永不收敛又造成持续网络/CPU 抖动。跨 OriginNode 的同版本冲突可由反方向的 tie-break 收敛,但同 OriginNode 情形两侧互相拉取且都跳过,形成死结。仲裁应在版本相等且 Hash 不同的确定性场景下定义一个可收敛规则(如按 Hash 字典序),使 SyncUpsertUser 与 CompareDigests 的判定一致。
- **证据**：version.go:11-14 仅比较 UpdatedAtUs 与 OriginNode;digest.go:66-70 `if remote.UpdatedAtUs == local.UpdatedAtUs && remote.OriginNode == local.OriginNode && remote.Hash != local.Hash { needFull = append(...) }`;usermanager.go:1499 `if exists && !usync.IsNewer(incoming, existing) { ... return false, nil }` —— 两处对“同版本不同 Hash”的判定相反。
- **核实**：已核实。IsNewer(version.go:10-15)仅比较 UpdatedAtUs,相等时按 OriginNode 字典序 tie-break,完全忽略 Hash。CompareDigests(digest.go:66-70)对 remote.UpdatedAtUs==local.UpdatedAtUs && remote.OriginNode==local.OriginNode && remote.Hash!=local.Hash 判为 needFull;而 SyncUpsertUser 首段(usermanager.go:1499 `if exists && !usync.IsNewer(incoming, existing)`)对同一场景 IsNewer 返回 false 而直接 return false(1510)跳过采纳。两处判定确实相反:CompareDigests 每心跳把该用户列入拉全量,拉回后又被 SyncUpsertUser 丢弃,既不收敛又持续抖动;同 OriginNode 时两侧对称互拉互跳形成死结。触发场景(同版本+同 OriginNode+不同 Hash)可由 idx0 的 Hash 失真诱发,或同一节点同微秒二次 stampVersion 后分叉传播产生,均属可达。P2 合理。

### 36. [P2·confirmed] emitEvent 对满 channel 非阻塞静默丢弃事件,UserEventRemove/PortBind 丢失导致中继/inbound 泄漏

- **位置**：`pkg/proxy/usermanager/usermanager.go:436` · 维度：资源 · 单元：UM
- **问题**：emitEvent 对 legacy channel 与每个订阅者都用 `select{ case ch<-event: default: }` 非阻塞发送(445-459),channel 满即静默丢弃且无任何日志或计数。容器订阅者依赖 UserEventRemove 拆除 inbound 并回调 ReleaseBindPort/GetUserPortByDstForCleanup 完成端口清理、依赖 UserEventPortBind 更新订阅端口;一旦订阅者消费慢导致其 100 缓冲 channel 打满,删除/端口变更事件被丢弃,就会遗留无人清理的 relay 与 inbound(资源与集群一致性双重损失)。事件送达是隐式契约却无背压、无重试、无丢弃可观测性,尤其在 SyncUpsertUser 的 tombstone/update 分支(1551、1632)与 RemoveUser 路径上风险最高。至少应对丢弃计数/告警,关键删除事件考虑有界重试或让订阅者拉取对账。
- **证据**：445-450 legacy 通道 `select { case m.eventCh <- event: default: // Channel full, event dropped }`;453-459 订阅者广播同样 `default: // Subscriber channel full, event dropped`。无日志/计数。缓冲仅 100(Subscribe 于 404 `make(chan UserEvent, 100)`)。
- **核实**：已核实代码行为。emitEvent(usermanager.go:445-459)对 legacy 通道与每个订阅者均用 select{case ch<-event: default:} 非阻塞发送,channel 满即静默丢弃,注释明确 "Channel full/Subscriber channel full, event dropped",全程无日志、无计数、无告警。订阅者缓冲仅 100(Subscribe:404 make(chan UserEvent, 100))。UserEventRemove 于 tombstone 分支(1551)、UserEventUpdate 于 update 分支(1632)、RemoveUser 经 mutateUser(376-382)均走此路径;一旦订阅者持续消费慢打满 100 缓冲,删除/端口事件被丢弃,依赖事件驱动清理的 relay/inbound 会泄漏。事实性判断全部成立。触发需订阅者持续背压填满缓冲,属可达但非常态;无背压/重试/丢弃可观测性的缺陷确实存在。P2 可接受。

### 37. [P2·confirmed] NewAddUserOperation/NewRemoveUserOperation 把 JSON 塞进声称为 protobuf 的 TypedMessage，xray 反序列化必失败

- **位置**：`pkg/xrayapi/types/types.go:1157` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：NewAddUserOperation 构造 TypedMessage 时 Type 写死为 "xray.app.proxyman.command.AddUserOperation"（proto 消息类型），但 Value 是 json.Marshal(AddUserOp{User})（types.go:1156-1161）。NewRemoveUserOperation 同样用 json.Marshal 生成 Value（types.go:1171-1175），Type="...RemoveUserOperation"。xray 收到 AlterInbound 后会用 serial.GetInstance(Type) 得到 proto 消息并 proto.Unmarshal(Value)，而 JSON 字节不是合法的 protobuf wire，解码必然失败。二者被 grpc_client.go:309/341 的 AddUser/RemoveUser 直接使用（注释称当前不在生产路径,故列 P2 而非 P0），一旦启用即整条用户增删链路失效。此外 AddUserOperation 的正确 proto 结构里 user 是 field 1，JSON key "user" 也无法映射到字段号。修复应改为 proto.Marshal 一个真正的 command.AddUserOperation。
- **证据**：types.go:1158-1161 `return &serial.TypedMessage{Type: "xray.app.proxyman.command.AddUserOperation", Value: opJSON}`；opJSON 来自 json.Marshal(op)（1157）。RemoveUser 同理 types.go:1171-1175。
- **核实**：核实属实。types.go:1154-1161 用 json.Marshal(AddUserOp{User}) 生成 Value，却把 Type 写成 proto 消息类型 xray.app.proxyman.command.AddUserOperation；RemoveUser 同理(1169-1175)。serial.TypedMessage 是仓库自带 xray 生成类型，其 typed_message.proto 明确注释 Value 为‘序列化的 proto 消息’，同文件 NewTypedMessage/NewTypedMessageFromProto(121,145) 均用 proto.Marshal，唯此二处用 JSON，属仓库内部契约违反(非纯记忆断言第三方)。callers grpc_client.go:309/341 确存在但源码注释(line300)明示当前不在生产路径，P2 合理。confirmed。

### 38. [P2·confirmed] buildReceiverConfigProto 忽略 Sscanf 错误并塌陷端口区间，非法/空端口静默变 0

- **位置**：`pkg/xrayapi/types/types.go:242` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：解析端口用 `fmt.Sscanf(port, "%d", &portFrom); portTo = portFrom`（types.go:242-243），返回值与 error 全部丢弃。若 port 字段为空或非数字，portFrom 保持 0，生成 PortRange{From:0,To:0} —— xray 会以 0 端口起 inbound（无效监听）而无任何报错。若 port 传入区间形式（如 "10000-20000"），只解析出 10000，To=From，区间被静默塌陷成单端口。该函数在生产 AddInbound 路径（grpc_client.go:134 → ParseInboundConfig）被调用。至少应校验 Sscanf 的返回 n/err，端口为 0 或解析失败时返回错误。
- **证据**：types.go:241-243 `var portFrom, portTo uint32; fmt.Sscanf(port, "%d", &portFrom); portTo = portFrom`，随后 249-251 直接用 From/To 构造 PortRange。
- **核实**：核实属实。types.go:241-243 var portFrom,portTo；fmt.Sscanf(port,"%d",&portFrom)；portTo=portFrom，返回值 n 与 err 全丢弃，随后 249-251 直接构造 PortRange。空/非数字端口 portFrom 保持 0(塌陷为 From:0,To:0)；Port 字段类型为 FlexPort(字符串,可承载 "10000-20000" 区间)，区间会被静默塌陷成单端口。经生产 ParseInboundConfig→buildReceiverConfigProto(grpc_client.go:134)可达。缺少输入校验的健壮性缺陷确凿，P2 合理。confirmed。

### 39. [P2·confirmed] h2/http 传输的 header 设置被写成 JSON 却标为 proto 类型，且 transport ProtocolName 与 stream 不一致

- **位置**：`pkg/xrayapi/types/types.go:868` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：network 归一化后 h2/http 均变 "h2"（normalizeNetworkName types.go:578-581），buildStreamConfigProto 的 case "h2","http" 里把 tcpConfig.HeaderSettings.Value 设为 `[]byte(fmt.Sprintf("{\"host\":[\"%s\"]}", host))`，Type="xray.transport.internet.http.Config"（types.go:868-871）—— 又是 JSON 冒充 protobuf，xray 解码 header 必失败。同时该分支把 TransportSettings 的 ProtocolName 设为 "tcp"（types.go:878），而 streamConfig.ProtocolName 已是 "h2"，两者不匹配，xray 找不到 h2 对应的 transport 设置。任何走 h2/http 传输的 inbound 经生产 ParseInboundConfig 都会被破坏。
- **证据**：types.go:868-871 HeaderSettings 用 JSON 字符串；types.go:878 `ProtocolName: "tcp"` 与 streamConfig.ProtocolName=="h2"（599 经 normalizeNetworkName）冲突。
- **核实**：核实属实。normalizeNetworkName 把 h2/http 归一为 "h2"(578-581)，故 streamConfig.ProtocolName=="h2"(599)；而 buildStreamConfigProto 的 case h2/http 把 tcpConfig.HeaderSettings.Value 设为 JSON 字符串(868-871)，HeaderSettings 类型为 serial.TypedMessage(tcp/config.pb.go:27，期望 proto 序列化)，且该分支 TransportConfig.ProtocolName 设为 "tcp"(879)与 streamConfig 的 "h2" 不一致。经生产 ParseInboundConfig(line278)可达。JSON 冒充 proto 及 ProtocolName 内部不一致均属仓库内可证的契约违反。confirmed。

### 40. [P2·confirmed] hotreload buildConfig 把 api inbound 端口写死 62789 不加 PortOffset，与新实例 gRPC 地址及旧实例 api 端口全冲突

- **位置**：`pkg/xrayapi/hotreload/manager.go:210` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：PrepareNewConfig 对所有业务 inbound 加了 PortOffset（applyPortOffset），但 buildConfig 事后追加的 api inbound 端口硬编码 62789（manager.go:210），而 CreateNewExecutor 把新实例 GRPCAPIAddress 设为 127.0.0.1:%d = 62789+PortOffset（manager.go:261）。结果：(1) 新 xray 配置里 api 监听 62789，executor 却按 62789+offset 去连 gRPC，健康检查/操作均连不上；(2) 旧实例仍占用 62789，新实例 api inbound 再绑 62789 → 端口冲突，新 xray 直接启动失败。此外 buildConfig 只输出 Inbounds，丢弃了原配置的 Outbounds/Routing（xrayConfig 有这些字段但从未拷贝，manager.go:217-231）。该 Manager 由 cmd/test_hotreload 实际调用。
- **证据**：manager.go:208-213 api inbound `"port": 62789`；manager.go:261/272 `GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", 62789+m.config.PortOffset)`；buildConfig 返回结构无 Outbounds/Routing。
- **核实**：核实属实。buildConfig 追加的 api inbound 端口硬编码 62789 无偏移(manager.go:210)，而 CreateNewExecutor 的 GRPCAPIAddress=127.0.0.1:62789+PortOffset(261/272)；cmd/test_hotreload/main.go:85 实际以 PortOffset=1000 调用，故 api 监听 62789 而 executor 去连 63789，健康检查/操作连不上，冲突真实触发。另 buildConfig 返回的 xrayConfig 只填 Inbounds，Outbounds/Routing 从未拷贝(loadInboundsFromFile 也只取 Inbounds)，原配置出站/路由被丢弃。均确凿。P2 合理(此为 test/dev 命令而非生产 server)。confirmed。

### 41. [P2·confirmed] hotupdate 死代码：新配置未做端口偏移，但映射按 oldPort+offset 计算，新实例必与旧实例抢占同端口

- **位置**：`pkg/xrayapi/hotupdate/update.go:95` · 维度：正确性 · 单元：XAPI
- **问题**：Execute 的 prepareConfig 只是把旧配置原样拷贝到 NewConfigPath（update.go:205-233，无任何端口偏移），而端口映射却按 NewPort=oldPort+portOffset 生成（update.go:91-97），switchForwardRules 随后把转发规则指向 127.0.0.1:oldPort+offset（update.go:308）。由于新配置端口未偏移，新 xray 会试图绑定与旧实例相同的业务端口 → 端口冲突启动失败；即便启动，也没有进程监听在 oldPort+offset 上，转发规则全部指向空洞。整个 hotupdate 包无任何生产/测试调用者（仅 hotreload 被 cmd/test_hotreload 使用），是 hotreload 的近重复实现且逻辑已破损，属应删除或修正的重复分叉。
- **证据**：update.go:205-232 prepareConfig 逐字拷贝配置无偏移；update.go:93-96 `NewPort: oldPort + portOffset`；update.go:303-308 forward 规则用 old/new 端口做匹配与改写。grep 显示 hotupdate 无调用方。
- **核实**：核实属实。prepareConfig(update.go:205-232)逐字拷贝旧配置到 NewConfigPath，无任何端口偏移；而 mappings 按 NewPort=oldPort+portOffset(95)生成，switchForwardRules 把转发规则 TargetAddr 改写为 127.0.0.1:oldPort+offset(308)；createNewExecutor(271-296)只把 GRPCAPIAddress 偏移，ConfigFilePath 仍指向未偏移的配置——新 xray 绑定与旧实例相同业务端口→冲突，转发规则又指向无人监听的 oldPort+offset。反证死代码成立:全仓库 grep 无任何 import xrayapi/hotupdate(含测试)。逻辑破损且无调用方，属应删/应修的重复分叉。protocolRelated=false，纯仓库代码可证。confirmed。

### 42. [P2·confirmed] builder.buildReceiverSettings 丢弃全部 streamSettings，导致 WS/TLS/Reality 变体实际产出纯 TCP receiver

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:299` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：buildReceiverSettings 接收 streamSettings 参数却完全不使用（仅一句 `// TODO: Handle streamSettings if needed`，builder.go:299），BuildVMessWS/BuildVMessTCPWithTLS/BuildVLESSReality/BuildVLESSWSTLS/BuildTrojanTCPWithTLS 通过 buildWebSocketTLSSettings/buildTLSSettings/buildRealitySettings 构造的 TLS/WS/Reality 设置被静默丢弃，最终 ReceiverConfig 没有任何 StreamConfig，产出的是明文 TCP —— 与方法名承诺完全相反。另外监听地址处理错误：`Ip: []byte(listen)` 直接把 ASCII 字符串塞进 IPOrDomain_Ip（builder.go:294），而 xray 期望的是 4/16 字节原始 IP（对照 types.buildReceiverConfigProto 用 stdnet.ParseIP）。此 builder 目前仅被测试引用。
- **证据**：builder.go:299 `// TODO: Handle streamSettings if needed - requires building StreamConfig proto`，函数体未引用 streamSettings；builder.go:293-296 `Address: &protocol.IPOrDomain_Ip{Ip: []byte(listen)}`。
- **核实**：已核实:buildReceiverSettings(builder.go:277-307)接收streamSettings却完全不用,仅299行TODO;BuildVMessWS/BuildVMessTCPWithTLS/BuildVLESSReality/BuildVLESSWSTLS/BuildTrojanTCPWithTLS(第90-91、121-122、153-154、184-185、238-239行)确实各自构造TLS/WS/Reality设置再传入,随后被静默丢弃,产出的ReceiverConfig无任何StreamConfig。Ip:[]byte(listen)(:294)与仓库内正确实现types.buildReceiverConfigProto用stdnet.ParseIP(types.go:258)对照即为错。均为纯代码级事实、证据全在仓库内,不依赖第三方行为断言;grpc/builder无任何生产import,只被builder_test.go引用,P2定级恰当。

### 43. [P2·confirmed] SwitchForwardRules 先删旧转发规则再加新规则,AddRule 失败或循环中途失败时无回滚,用户转发被永久拆除且状态不一致

- **位置**：`pkg/xrayapi/hotreload/manager.go:342` · 维度：错误处理 · 单元：XAPI
- **问题**：SwitchForwardRules 对每个映射先 fm.RemoveRule(targetRule.RuleKey())(:342),再以新 TargetAddr fm.AddRule(:349)。若 AddRule 失败,旧规则已删除、新规则未建立,直接 return err —— 该用户的转发链路被彻底拆掉且不恢复;多映射时循环中途失败还会留下部分已切换、部分未切换的半成品状态。ExecuteHotReload 在 switch-forward 失败分支(:432-436)仅记录 error 直接返回,不做任何 rollback,旧实例也没停(第7步未执行)。hotupdate.switchForwardRules(update.go:305-311)同构同缺陷。违背 lens 关注的失败回滚要求。
- **证据**：manager.go:342 `if err := fm.RemoveRule(targetRule.RuleKey()); err != nil` ... :349 `if _, err := fm.AddRule(newRule); err != nil { return fmt.Errorf(...) }`;失败分支 manager.go:432-436 无 rollback
- **核实**：已核实:SwitchForwardRules(manager.go:320-354)对每映射先RemoveRule(:342)再以新TargetAddr AddRule(:349),AddRule失败直接return err,旧规则已删新规则未建,无恢复;多映射循环中途失败留半成品。ExecuteHotReload switch-forward失败分支(:432-436)仅记error直接return,不rollback、旧实例未停(第7步未执行)。核心缺陷属实。两点修正:(a)详情称'hotupdate.switchForwardRules(update.go:305-311)同构同缺陷',但本包只有manager.go,不存在update.go,该引用不准(实际ExecuteHotUpdate在manager.go内复用同一有缺陷的SwitchForwardRules,forward规则本身仍不回滚);(b)ExecuteHotReload唯一调用方为cmd/test_hotreload/main.go(演示/手测harness),生产未接线,P1的'用户转发被永久拆除'在生产不可发生,故severity过高。

### 44. [P2·confirmed] switch-forward 失败时已 Start 的 newExecutor 既不 Stop 也不随结果返回,xray 子进程被孤儿化泄漏

- **位置**：`pkg/xrayapi/hotreload/manager.go:432` · 维度：资源 · 单元：XAPI
- **问题**：ExecuteHotReload 在第4步 Start newExecutor 成功后,第6步 SwitchForwardRules 失败时注释 'Don't stop new instance - let caller decide' 直接 return(:432-436)。但 ReloadResult 结构体不含任何 executor 句柄,newExecutor 是局部变量,返回后调用方拿不到引用,无法 Stop —— 已启动的 xray 进程被孤儿化(叠加旧实例仍在运行,形成双进程 + 端口占用)。这是本 lens 重点关注的资源释放缺陷。相较之下 ExecuteHotUpdate 的 switch-forward 失败分支(:537)会 Stop newExecutor,行为不一致。
- **证据**：manager.go:434 `// Don't stop new instance - let caller decide` 后直接 `return result`;ReloadResult(:56-63)无 executor 字段;newExecutor 为 ExecuteHotReload 局部变量
- **核实**：已核实:ExecuteHotReload第6步SwitchForwardRules失败(manager.go:432-436)注释'Don't stop new instance'后直接return,而ReloadResult(:56-63)无executor字段、newExecutor为局部变量,调用方拿不到句柄无法Stop,已Start的xray进程被孤儿化(叠加旧实例仍运行);对照ExecuteHotUpdate同分支(:537)确会newExecutor.Stop(),行为确实不一致。缺陷属实。但唯一调用方为cmd/test_hotreload/main.go(手测harness),非生产链路,P1偏高,下调P2。

### 45. [P2·confirmed] 默认续期提前量仅 24h，任何超过 24h 的 ACME 故障/限流都会导致证书过期；legacy 30 天默认已成死代码

- **位置**：`pkg/certmgmt/service/manager.go:42` · 维度：错误处理 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：renewBeforeDuration 在 Hours/Days 都未配时返回 24h(:42)。buildIssueRequest 又无条件把 RenewBefore = renewBeforeDuration()(>=24h, 恒>0, :178)写入请求，于是 issuer.renewBeforeDuration(req) 里 req.RenewBefore>0 恒成立(issuer.go:244)，其 RenewBeforeDays 与 30 天回退(issuer.go:247-250)永远走不到；manager.go:183-185 把 RenewBeforeDays 补成 30 也纯属死值。真实效果是全局默认仅在到期前 24h 才开始续期。Let's Encrypt 证书 90 天，行业惯例是提前 30 天续。24h 窗口下若遇 ACME 侧限流(每周级)或网络故障持续 >24h，证书直接过期导致全站 TLS 中断，几乎无重试缓冲。建议默认改回 ~30 天并移除误导性的死默认值。
- **证据**：manager.go:42 return defaultRenewBefore(=24h, :31)；:178 RenewBefore: m.cfg.renewBeforeDuration()；issuer.go:244 if req.RenewBefore>0 恒真，:247-250 分支不可达。
- **核实**：代码事实核实属实且不依赖上游行为:manager.go:31 defaultRenewBefore=24h,:35-43 无配置时返回 24h;buildIssueRequest :178 无条件写 RenewBefore=renewBeforeDuration()(恒>0),故 issuer.go:244 req.RenewBefore>0 恒真,:247-250 的 RenewBeforeDays 分支与 30 天回退不可达;manager.go:183-185 补 RenewBeforeDays=30 确为死值。默认续期提前量确实仅 24h。Config 注释(manager.go:23-24)显示 24h 是有意默认,故偏设计取舍+死代码,而非功能 bug;涉及 LE 90 天/限流的风险框架属上游背景,但本条判定的核心(24h 默认+死代码)已由仓库代码直接证实。P2 可接受。

### 46. [P2·confirmed] DeleteCert 只删本地文件，从不向 CA 发起 ACME 吊销，被删证书在有效期内仍可被滥用

- **位置**：`pkg/certmgmt/service/rpc_adapter.go:65` · 维度：安全 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：对外 CertManager.DeleteCert(:65-67) 仅调用 certmgmtlego.DeleteCert 删除本地 .crt/.key/.json/.meta.json，全程不触发任何 ACME 吊销(issuer.Revoke 未被任何调用者接线)。当私钥被认为泄露或域名下线而调用删除时，证书在 CA 侧仍然有效直到自然过期(至多 90 天)，攻击者若已持有该私钥可继续冒用。若产品语义上 DeleteCert 等同下线，应在删本地文件前尝试 Revoke 并记录结果；否则至少在文档/接口层明确“不吊销”。
- **证据**：rpc_adapter.go:66 return certmgmtlego.DeleteCert(m.cfg.Path, d)；全仓 grep 无任何 issuer.Revoke 调用点。
- **核实**：核实属实且核心可在仓库内证实:rpc_adapter.go:66 DeleteCert 仅调 certmgmtlego.DeleteCert(cert_store.go:120-128)删本地 .crt/.key/.json/.meta.json;全仓 grep 确认 issuer.Revoke 无任何调用点,删除路径全程不触发 ACME 吊销。『删本地文件不等于 CA 侧吊销』是可直接推断的逻辑,非依赖隐晦协议细节;文中 90 天为 LE 特定数字仅作风险框架、非判定关键。属真实设计/安全缺口。P2 可接受。

### 47. [P2·confirmed] 证书文件名仅替换 '*'，未过滤 '/'、'..'，域名可路径穿越写/删任意文件

- **位置**：`pkg/certmgmt/lego/cert_store.go:19` · 维度：安全 · 单元：CERT
- **问题**：domainToFilename (cert_store.go:19-21) 只做 strings.ReplaceAll(d, "*", "_")，certPaths (cert_store.go:24-32) 直接 filepath.Join(basePath/certificates, name+".crt")。若 domain 含 '../'，filepath.Join 会规约出 basePath 之外的绝对路径。SaveCert(cert_store.go:44)、DeleteCert(cert_store.go:120) 及 rpc_adapter.AddCertificates(rpc_adapter.go:37) 全部以调用方给的 domain 走此路径。AddCertificates 的 domain 来自 TransferCert RPC（pkg/rpc/server/end_node_cert.go:21-25）→ 上游 domain 又来自 HTTP body(pkg/http/tranfer_cert_handler.go:16-25)，即由管理端 API/集群对端完全控制。攻击者可用形如 '../../etc/xxx' 的 domain 让 end 节点把攻击者提供的私钥/证书内容写到 certificates 目录之外的任意文件，或用 DeleteCert 删除任意 .crt/.key/.json。缺少 domain 合法性校验（非空 + 仅允许合法域名字符）。
- **证据**：func domainToFilename(d string) string { return strings.ReplaceAll(d, "*", "_") } — 无 '/'、'..' 过滤；certPaths: crt = filepath.Join(dir, name+".crt")
- **核实**：路径穿越真实存在且触发链完整核实。cert_store.go:19-21 domainToFilename 仅 ReplaceAll(d,"*","_"),不过滤 '/'、'..';certPaths(:24-32) 直接 filepath.Join(base/certificates, name+ext),含 '../' 的 domain 经 Clean 会规约到 basePath 之外。domain 未经任何校验从 RPC 传入:end_node_cert.go:21-25 TransferCert 把 transferCertReq.Domain 直传 AddCertificates(rpc_adapter.go:37→SaveCert),DeleteCert(:40)同样直传;grep 全链路(certmgmt/rpc server/http handler)无任何 domain 合法性校验/Clean。攻击者(持集群 token 的对端/管理端 API)可用 '../../..' 类 domain 让 end 节点把其提供的 cert/key 写到 certificates 目录外任意 .crt/.key/.json,或 DeleteCert 删除任意同扩展名文件。需集群 token 认证但影响为任意文件写/删,P2 合适。行号(:19)与证据准确。

### 48. [P2·confirmed] DNS-01 凭证通过进程级环境变量传递，obtain/renew 窗口期内全进程可见

- **位置**：`pkg/certmgmt/lego/solver_dns.go:22` · 维度：安全 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：WithDNSCredentials(solver_dns.go:22-57) 用 os.Setenv 把 DNS provider 凭证写入进程级环境变量，跨 NewDNS01Provider 构造 + client.Certificate.Obtain/Renew 的整个（可能数分钟的传播等待）期间持有。dnsGlobalMu 只能串行化本包内并发，进程内任何其它读同名 env 的代码（含被 exec 的子进程继承环境）在窗口期都能读到明文凭证；恢复逻辑靠 map 迭代 os.Setenv，出错路径(36-43)只做部分恢复。这是 lego 依赖 env 的上游设计约束，但多数 provider 支持 NewDNSProviderConfig 直接传结构体，可避免污染全局 env。
- **证据**：for k, v := range creds { if err := os.Setenv(k, v); ... } ... _ = os.Setenv("LEGO_DISABLE_CNAME_SUPPORT", "true") — 凭证进程级可见，窗口 = 整个 obtain/renew
- **核实**：solver_dns.go:22-57 确认 WithDNSCredentials 用 os.Setenv 把凭证写入进程级 env，跨 NewDNS01Provider+Obtain/Renew 整个窗口持有；dnsGlobalMu 仅串行化本包，进程内其它读同名 env 的代码/子进程窗口期可见明文。solver_dns_full.go 用 dns.NewDNSChallengeProviderByName（依赖 env）也印证了上游 env 依赖属实，protocolRelated 部分有仓库材料支撑。核心行为成立。唯一小瑕疵：detail 说『出错路径(36-43)只做部分恢复』不准确——错误分支遍历 originals 恢复的是全部已保存原值，且 LEGO_DISABLE 在循环后(46)才设置，此时尚未污染，恢复其实是完整的；此细节不影响主结论。P2 用于凭证暴露可接受，虽属已被 mutex 串行化并有文档说明的上游约束、利用需进程内另有恶意代码，未做下调。

### 49. [P2·uncertain] grpc.Client.conn.Invoke 传入 JSON []byte，grpc 默认 proto codec 会在编码期直接失败

- **位置**：`pkg/xrayapi/grpc/client.go:48` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：AddInboundWithProto 先 json.Marshal(inboundHandlerConfig) 得到 jsonBytes，再 `conn.Invoke(ctx, ".../AddInbound", jsonBytes, &response)`（client.go:33,48）。grpc-go 默认使用 proto codec，其 Marshal 会把请求参数断言为 proto.Message；[]byte 既非 proto.Message，编码阶段即返回错误，请求根本发不出去。RemoveInbound 同样传 reqData []byte（client.go:60,71）。response 也用 *[]byte 接收，解码同样不成立。整个 grpc.Client 无法工作；虽然生产用的是 pkg/proxy/containers/xray/grpc_client.go（typed stub），此包只被测试引用，但该 API 表面具误导性且与 InboundHandlerConfigToJSON 一起构成错误契约。
- **证据**：client.go:33 `jsonBytes, err := json.Marshal(...)`；client.go:48 `conn.Invoke(ctx, "/xray.app.proxyman.command.HandlerService/AddInbound", jsonBytes, &response)`，无自定义 codec。
- **核实**：代码事实属实：client.go:33 json.Marshal 得 []byte，48 行 conn.Invoke(...,jsonBytes,&response)，grpc.Dial(38)未设自定义 codec，response 亦为 *[]byte。但‘grpc 默认 proto codec 编码期拒绝 []byte 必失败’属第三方(grpc-go)行为，仓库内无任何材料(测试/文档)佐证，按 protocolRelated 规则上限只能 uncertain，不得凭记忆断言。且反证 reachability：全仓库无任何文件 import pkg/xrayapi/grpc(该包连测试都不引用,builder 子包为独立包)，finding 所称‘此包只被测试引用’不准确——实为完全死代码，触发路径不存在。综合降为 uncertain。

### 50. [P3·confirmed] 测试缺口：半关闭后反向持续活跃流量、IPv6/双栈监听、UDP readLoop 错误退出均无测试覆盖

- **位置**：`pkg/proxy/forward/relay_test.go:735` · 维度：测试缺口 · 单元：FWD
- **问题**：本包测试量可观（约 3300 行），但恰好缺失能暴露前述缺陷的场景：1) relay_test.go 的 drain 相关用例（:735-1016）只测 client limiter 的拒绝/回收路径，没有任何用例做 'CloseWrite 一个方向 + 另一方向持续传输超过 drainSec' 的断言——因此 relay_tcp.go 的 drain 强断 bug（本次 P1）从未被捕获；2) 全部测试用 127.0.0.1 监听，无 IPv6 客户端连接 "0.0.0.0" 默认监听的用例，双栈约束（约束 #6）完全无测试兜底；3) relay_udp_test.go 覆盖了 session 生命周期与 Stop drain，但没有注入 ReadFrom 非超时错误验证 readLoop 静默退出后的行为（规则残活、端口占用）；4) MaxConnections 在慢 dial 下的超卖窗口（relay_tcp.go:144）同样无并发压入测试。建议按上述四点补齐回归用例，尤其是 1) 应作为修复 drain bug 的验收测试。
- **证据**：grep drain/CloseWrite relay_test.go 仅命中 SingleDirectionDrainSec 配置字段（:526/735/755/786/818/849/906/996/1016），无 CloseWrite 半关闭数据面用例；relay_test.go/relay_udp_test.go/integration_test.go 中无 "::1"、无 tcp6/udp6、无 ReadFrom 错误注入。
- **核实**：覆盖缺口的事实陈述属实：grep 确认 relay_test.go 无 CloseWrite（仅 forward_manager_test.go:316 有，且该用例 Write 后立即 CloseWrite 触发上行结束即终止，并非“一方向 CloseWrite + 另一方向持续传输超 drainSec”场景）；全仓测试无 ::1/tcp6/udp6/[::] 双栈用例；relay_udp_test.go 仅回显服务器用 ReadFrom(:32)，无向 readLoop 注入非超时错误的用例；relay_test.go:136 MaxConnections 测试用快速 echo 目标，未覆盖慢 dial 超卖窗口。但该条部分以“本次 P1 drain 强断 bug 从未被捕获”自证严重性，而该 P1 不在本次核实范围、未独立确认；纯测试覆盖缺口在缺乏已确认底层 bug 时不应定 P2。

### 51. [P3·confirmed] WithPortRange 静默吞掉 NewPortAllocator 错误，非法端口范围时悄悄回退默认 allocator

- **位置**：`pkg/proxy/forward/forward_manager.go:24` · 维度：错误处理 · 单元：FWD
- **问题**：WithPortRange 选项内部调用 NewPortAllocator，err != nil 时什么都不做（forward_manager.go:23-27）：既不 panic、不记日志，也无法把错误传回 NewForwardManager 的调用方。当配置给出非法范围（如 min>max）时，manager 会带着默认 10000-65535 allocator 静默启动，与运营者的端口规划（防火墙放行范围、与容器 reserved 端口的隔离）不一致，且没有任何信号提示配置未生效——后续排障成本高。建议让 option 记录错误并在 NewForwardManager 聚合返回，或至少 log.Warn。
- **证据**：forward_manager.go:19-27 `alloc, err := NewPortAllocator(cfg); if err == nil { m.allocator = alloc }` —— err 分支为空，无日志无返回。
- **核实**：属实。forward_manager.go:23-26 `alloc, err := NewPortAllocator(cfg); if err == nil { m.allocator = alloc }`，err 分支为空。NewForwardManager 先建默认 10000-65535 allocator(:39) 再 apply options(:54)，故非法范围(如 min>max)时 WithPortRange 静默失败，manager 带默认 allocator 启动且无任何日志/返回信号。P3 合理。

### 52. [P3·confirmed] QueryTrafficStats 构造记录时遗漏 Protocol 字段，聚合/过滤结果中协议维度恒为空

- **位置**：`pkg/proxy/forward/forward_manager.go:498` · 维度：正确性 · 单元：FWD
- **问题**：GetAllTrafficRecords 填充了 Protocol（forward_manager.go:469），但 QueryTrafficStats 构造同类型 ForwardTrafficRecord 时漏掉该字段（:493-503 只有 RuleKey/Username/ContainerType/InboundTag/ListenPort/TargetAddr/流量三元组）。所有走 QueryTrafficStats 的消费方（含 group-by 聚合结果 AggregatedStats）拿到的 Protocol 恒为零值，若上层（collecter/http 展示）依赖该字段做协议维度统计会得到全空数据。属两处构造逻辑不一致的复制粘贴遗漏，建议抽一个 recordFromManagedRule helper 消除重复。
- **证据**：forward_manager.go:493-503 record 初始化无 `Protocol: mr.rule.Protocol,`，对照 :464-475 GetAllTrafficRecords 中同结构包含 `Protocol: mr.rule.Protocol`。
- **核实**：属实。forward_manager.go:493-503 的 ForwardTrafficRecord 初始化确实缺 `Protocol: mr.rule.Protocol`，对照 GetAllTrafficRecords :464-475 含 Protocol(:469)。QueryTrafficStats 产出的 Records 及 AggregatedStats(:519/562 复制自 record)的 Protocol 恒为零值。当前 GroupBy 仅支持 user/container/inbound/rule，group-by 本身不用 Protocol，故影响面限于直接读 record.Protocol 的消费方，属复制粘贴遗漏的一致性缺陷。P3 合理。

### 53. [P3·confirmed] AddRule 失败回滚不清理本次新建的用户级 limiter/config 条目

- **位置**：`pkg/proxy/forward/forward_manager.go:296` · 维度：正确性 · 单元：FWD
- **问题**：AddRule 在 relay.Start 失败时回滚了端口与 traffic counter（forward_manager.go:294-299），但此前可能已经：1) 为该用户新建 userBandwidth 条目并用本条规则的 rule 级限速种子化（:170-184）；2) 新建 userClientLimiters 条目（:259-261）；3) 在 ruleOwnsUserPolicy 时覆写 userClientLimitConfigs（:254-256/262-264）。这些副作用在失败路径全部残留：若这是该用户的第一条（且失败的）规则，用户在 per-user map 中滞留直到 DropUser；更实质的是 (3)——一次失败的 AddRule 已经永久改写了用户级 client-limit 策略，后续其他规则会继承这份来自失败操作的配置。建议把用户级副作用移到 relay.Start 成功之后，或失败时按“本次是否新建/覆写”逐项回滚。
- **证据**：forward_manager.go:294-299 回滚仅 `m.allocator.Release(port); m.traffic.Remove(key)`；:183 `m.userBandwidth[rule.Username] = newUserBandwidthLimiter(...)`、:256/:263 `m.userClientLimitConfigs[rule.Username] = config`、:261 `m.userClientLimiters[rule.Username] = clientLimiter` 均发生在 Start 之前且无回滚。
- **核实**：核实 forward_manager.go:294-299 失败回滚仅 allocator.Release(port)+traffic.Remove(key)。此前的用户级副作用确实残留：170-184 在 map 无该用户时新建 userBandwidth 条目;259-261 无条件新建 userClientLimiters 条目;252-256/262-264 在 ruleOwnsUserPolicy(rule.MaxClients>0) 时 SetConfig 并写 userClientLimitConfigs。这些都在 relay.Start() 之前发生且失败路径不回滚,一次失败的 AddRule 会永久改写用户级 client-limit 策略并让首条失败规则在 per-user map 中滞留到 DropUser。描述与证据准确,P3 质量项恰当。

### 54. [P3·confirmed] Allocate 每次全范围扫描构建 available 切片：默认 5.5 万端口范围下每次分配 O(n) 遍历 + 大切片分配

- **位置**：`pkg/proxy/forward/port_allocator.go:71` · 维度：资源 · 单元：FWD
- **问题**：PortAllocator.Allocate 每次持锁从 minPort 遍历到 maxPort 构建完整 available 切片（port_allocator.go:70-76），默认范围 10000-65535 意味着每次分配约 5.5 万次 map 查询和一个最多 5.5 万元素的 []uint32 分配（约 220KB），发生在 AddRule/AllocatePort 的锁内路径上。批量场景（节点重启恢复全部用户规则、批量建用户）会放大为 O(rules × range)。随机性并不需要全量物化：可以在 [min,max] 内 crypto/rand 取随机起点后线性探测空闲端口（期望 O(1)~O(占用率相关)），或维护空闲计数 + 稀疏结构。当前实现同为 Available()（:172-184）的问题。功能正确，纯效率问题。
- **证据**：port_allocator.go:70-76 `var available []uint32; for p := pa.minPort; p <= pa.maxPort; p++ { if !pa.allocated[p] && !pa.reserved[p] { available = append(available, p) } }` 在 pa.mu 锁内、每次 Allocate 都执行。
- **核实**：port_allocator.go:70-76 确实在 pa.mu 锁内每次 Allocate 都从 minPort 到 maxPort 全量遍历构建 available 切片(即使 round-robin 分支也照建,仅用于空判);Available():172-184 同样全扫。功能正确,纯 O(range) 效率问题,批量恢复/建用户场景放大为 O(rules×range)。精确默认范围不影响该效率结论的成立。P3 恰当。

### 55. [P3·confirmed] NewTokenBucketLimiter 的 burst 形参被完全忽略，API 签名误导调用方

- **位置**：`pkg/proxy/forward/ratelimit.go:33` · 维度：架构 · 单元：FWD
- **问题**：NewTokenBucketLimiter(bytesPerSec, burst int64) 声明接收 burst，但函数体从不读取该参数，burst 一律按 max(rate/4, 64KB) 重算（ratelimit.go:33-57）。现有调用方 initLimiters 传的 NewTokenBucketLimiter(uploadRate, uploadRate)（ratelimit.go:358-359）看似在配置 burst=rate，实际毫无效果；未来调用者若想定制 burst 也会被静默忽略。应删掉该形参或真正使用它（<=0 时才回退公式），并同步注释。纯接口清理项，无运行时行为错误。
- **证据**：ratelimit.go:33 `func NewTokenBucketLimiter(bytesPerSec int64, burst int64) *TokenBucketLimiter {` 函数体 :34-56 仅使用 bytesPerSec（r := float64(bytesPerSec)），burst 形参无任何引用；:358 调用 `NewTokenBucketLimiter(uploadRate, uploadRate)`。
- **核实**：ratelimit.go:33 函数签名含 burst int64,但函数体 34-56 只用 bytesPerSec(r:=float64(bytesPerSec)),burst 一律按 b=max(r/4,64KB) 重算,形参零引用。调用方 initLimiters :358-359 传 NewTokenBucketLimiter(uploadRate,uploadRate) 中的第二实参完全无效。纯 API 误导/清理项,无运行时错误,P3 恰当。

### 56. [P3·confirmed] Close 持写锁串行 Stop 全部 relay 并各自 wg.Wait，规则多时长时间阻塞所有 API；且不清理 per-user map 与未触发的 recycle 定时器

- **位置**：`pkg/proxy/forward/forward_manager.go:837` · 维度：架构 · 单元：FWD
- **问题**：Close（forward_manager.go:829-846）在持有 m.mu 写锁期间对每条规则调用 mr.relay.Stop()——TCPRelay.Stop 会 wg.Wait 等待所有活跃连接的 handleConn 退出（drain/copyWg 路径最长可达数秒），UDPRelay.Stop 也要 reap 全部 session 并 wg.Wait。数百条规则时 Close 是长时间的全局锁持有，期间 GetRule/GetAllTraffic 等所有 API 阻塞（进程退出场景影响有限，但 Close 也被 ResetGlobalForwardManager 等路径复用）。此外 Close 只清 rules，userBandwidth/userClientLimiters/userClientLimitConfigs 三个 map 及 clientSlot 中尚未触发的 time.AfterFunc recycle 定时器均未清理，定时器在 Close 后仍会触发（虽只操作 limiter 自身 map，无害但属残留）。改进：先持锁摘下 rules 快照并置 closed，释放锁后再并行 Stop。
- **证据**：forward_manager.go:830-843：`m.mu.Lock(); defer m.mu.Unlock(); ... for key, mr := range m.rules { mr.relay.Stop() ... }`；relay_tcp.go:89-95 Stop 内 r.wg.Wait()；Close 未 delete userBandwidth/userClientLimiters/userClientLimitConfigs
- **核实**：代码属实。Close(forward_manager.go:830-843)持 m.mu.Lock() 并 defer Unlock，循环内串行 mr.relay.Stop()；TCPRelay.Stop(relay_tcp.go:89-95)内 r.wg.Wait() 会阻塞至活跃连接 handleConn 退出(含 drain/copyWg 路径)。Close 只重置 m.rules，未 delete userBandwidth/userClientLimiters/userClientLimitConfigs 三个 map，clientSlot 中 time.AfterFunc recycle 定时器亦未清理(残留但基本无害)。ResetGlobalForwardManager(global.go:55)确实复用 Close，非仅进程退出路径。发现自身已合理限定为 P3 并承认影响有限，评级恰当。

### 57. [P3·confirmed] PortAllocator.Allocate 每次构建整段可用端口切片，默认 5.5 万范围下 O(n) 开销

- **位置**：`pkg/proxy/forward/port_allocator.go:72` · 维度：资源 · 单元：FWD
- **问题**：Allocate 每次调用都遍历 [minPort,maxPort] 构建 available 切片（port_allocator.go:70-76），默认范围 10000-65535 约 5.5 万个元素，随机模式再用 crypto/rand 选一个。Available() 同样每次 O(range) 全扫（port_allocator.go:172-184）。在高频加/删规则或大量 AllocatePort 的场景下，每次分配都做一次五万级别的堆分配+遍历，CPU 与 GC 压力偏高。可改为维护空闲集合或按已分配数在范围内随机试探。
- **证据**：port_allocator.go:71-76 `for p := pa.minPort; p <= pa.maxPort; p++ { if !allocated && !reserved { available = append(...) } }`；Available() port_allocator.go:178-183 同样全扫。
- **核实**：代码属实。Allocate(port_allocator.go:64-107)每次遍历 [minPort,maxPort] 构建 available 切片(71-76)，Available()(172-184)同样全扫。默认 globalConfig(global.go:13-16)为 10000-65535，即 55536 元素，与"5.5万"一致，随机模式再 crypto/rand 选一。属真实 O(range) 每次分配开销。发现已合理限定为 P3 性能项(端口分配通常非超高频)，评级恰当。

### 58. [P3·confirmed] acceptLoop 对非 ctx 关闭的 Accept 错误无退避直接 continue，持续性错误会 100% CPU 空转

- **位置**：`pkg/proxy/forward/relay_tcp.go:120` · 维度：资源 · 单元：FWD
- **问题**：acceptLoop 在 Accept 出错且非 ctx.Done 时 `default: continue`（relay_tcp.go:115-123），无任何退避。若遇到持续性而非瞬时错误（如 too many open files/fd 耗尽），listener.Accept 会立即再次返回错误，循环变成无退避的紧密自旋，占满一个 CPU 核。标准做法是对 net.Error.Temporary()/临时错误做指数退避后再重试。
- **证据**：relay_tcp.go:115-123 `if err != nil { select { case <-r.ctx.Done(): return; default: continue } }`，无 time.Sleep/退避。
- **核实**：代码属实。acceptLoop(relay_tcp.go:113-123)在 Accept 出错且非 r.ctx.Done() 时走 default: continue，无 time.Sleep/退避。持续性错误(如 EMFILE/fd 耗尽)会导致 listener.Accept 立即再次返回错误，形成无退避紧密自旋占满 CPU 核。行号 120 落在 continue 块内，准确。P3 恰当。

### 59. [P3·confirmed] 测试缺口：QueryTrafficStats 的 reset+过滤语义、TCP drain 截断、RemoveRulesByUser 部分失败均无覆盖

- **位置**：`pkg/proxy/forward/forward_manager.go:482` · 维度：测试缺口 · 单元：FWD
- **问题**：现有测试覆盖了增删/去重/带宽/UDP dispatch 等，但缺少几处关键边界：1) QueryTrafficStats 在 Reset=true 且带过滤时是否会清零并丢失非匹配规则的计数（forward_manager.go:491）无断言；2) TCP relay 在一方向半关、另一方向持续活跃时是否会在 SingleDirectionDrainSec 后被强制截断（relay_tcp.go:277）无用例；3) RemoveRulesByUser 在并发/规则已被删的情况下部分失败、剩余规则未拆的行为（forward_manager.go:346）无用例；4) SetUserClientLimitConfig 传入 0 drain/recycle 的边界行为（clientlimit.go:304）无用例。这些恰是本模块正确性风险最高、最易回归之处。
- **证据**：forward_manager_test.go 中无针对 QueryTrafficStats(Reset=true 且带 Username 过滤) 的用例；relay_test.go 未构造"上行半关+下行持续"场景验证不被截断；无 RemoveRulesByUser 并发部分失败用例。
- **核实**：四处缺口均属实。(1) 无 QueryTrafficStats(Reset=true+Username 过滤)用例(grep 零引用)，且 forward_manager.go:491 确在过滤前 Snapshot(Reset)，reset+过滤缺陷真实；(2) relay_test.go 无"半关+另一方向持续活跃"验证不被 SingleDirectionDrainSec 截断的用例，底层 drain 局部 deadline 缺陷真实；(3) RemoveRulesByUser(forward_manager.go:335-351)遇 RemoveRule 报错即 return，剩余规则不拆——测试仅 TestForwardManager_RemoveByUser(forward_manager_test.go:118-135)happy-path，无部分失败用例；(4) SetUserClientLimitConfig 传 0 drain/recycle 边界(clientlimit.go:304)无用例。注：与 idx0 属同批重复发现(QueryTrafficStats、TCP drain 两点重叠)，非旧 review legacyId 缺失，但两条内容高度交叠。P3 恰当。

### 60. [P3·confirmed] ReleaseBindPort 的 finalize 文档承诺与实现/RemoveUser「永不物理删除」矛盾，且注释已成孤儿

- **位置**：`pkg/proxy/usermanager/usermanager.go:975` · 维度：错误处理 · 单元：UM · 旧 review: U-06
- **问题**：行 975-978 的注释声称「若用户处于 deleting 且无剩余 forward 规则,将物理删除用户(finalize)」,但真正的 ReleaseBindPort 实现(1311-1372)完全没有 finalize/物理删除逻辑;而 RemoveUser 文档(541-547)又明确「永不物理删除」。两处契约互相矛盾,调用方无法据文档判断 tombstone 生命周期。更糟的是该注释块因函数重排已脱离 ReleaseBindPort,现悬挂在 ResetAuthToken(980)之上,而 1311 处真正的 ReleaseBindPort 反而无 doc。tombstone 无 finalize 也意味着删除用户在 map/DB 中永久累积。
- **证据**：usermanager.go:976-978 注释 finalize 承诺后紧跟 980 `// ResetAuthToken`;真正 ReleaseBindPort 定义在 1311 无 finalize 语句;RemoveUser 542 注释 `it is never physically deleted`。
- **核实**：代码属实:975-978 的注释承诺'user 处于 deleting 且无剩余 forward 规则则物理删除(finalize)',其后 979 空行 980 即 // ResetAuthToken,该注释块已脱离目标函数、悬挂在 ResetAuthToken 上方;真正的 ReleaseBindPort 定义在 1311 且实现 1311-1372 完全无 finalize/物理删除;RemoveUser 文档 542 明确'never physically deleted'。文档契约互相矛盾且注释成孤儿,均确证。tombstone 无 finalize 亦属实(RemoveUser 仅 MarkDeleting,AddUser 479-495 仅在同名重建时覆盖),从不再被引用的删除用户会在 map/DB 长期累积。但该问题的可执行缺陷本质是误导性/孤儿注释(文档层),不产生运行期错误,542 处又将'不物理删除'记为既定设计,故给 severityOverride=P3 更贴切。

### 61. [P3·confirmed] statsCollector 聚合表与 prevCounters 从不清理，用户/inbound 长期 churn 造成无界增长

- **位置**：`pkg/proxy/usermanager/usermanager.go:1877` · 维度：资源 · 单元：UM
- **问题**：collect() 以 "container:inbound:user" 为 key 累进 sc.stats.ByUser 与 sc.prevCounters(1867-1893),但用户被 RemoveUser/DropUser、inbound 被 RemoveRulesByInbound 删除后,这些 key 对应的条目永不删除(resetUserTotal/drainForUserLocked 只清零不 delete)。ByInbound/ByContainer 同理。长期运行且用户或端口频繁轮换(RotateAllUserPorts 每次换端口不改 key,但删用户/删 inbound 会残留)时,map 随历史组合数单调增长,构成内存泄漏;残留条目还被 onCollect 每轮遍历累加,拖慢采集。删除用户/规则时应同步 prune 对应统计 key。
- **证据**：usermanager.go:1877 `sc.prevCounters[userKey] = ...`、1893 `sc.stats.ByUser[userKey] = existing`;resetUserTotal 2035-2041 与 drainForUserLocked 2079-2087 均只置零不 delete;RemoveUser/DropUser 未触碰 statsCollector。
- **核实**：代码属实:collect 1867/1877 写 prevCounters[userKey]、1884/1893 写 ByUser[userKey],key 为 container:inbound:user;1896-1904 ByInbound、1906-1914 ByContainer 同理。全仓 grep 确认这些 map 无任何 delete:resetUserTotal 2032-2041 与 drainForUserLocked 2079-2087 只置零不删,RemoveUser/DropUser/ReleaseInboundPorts(RemoveRulesByInbound 1391)均不触碰 statsCollector。用户/ inbound 永久删除后残留 key 单调累积,构成慢速内存泄漏,且每轮 collect 遍历 records 时并不清理旧 key(旧 key 仅因不再出现于 records 而冻结,不被删)。行号准确,P3 合理。

### 62. [P3·confirmed] 测试缺口：ReleaseBindPort 单端口语义、Rotate 与 RemoveUser 并发未覆盖

- **位置**：`pkg/proxy/usermanager/usermanager.go:1311` · 维度：测试缺口 · 单元：UM
- **问题**：现有测试(usermanager_test.go/rotate_inbound_port_test.go 等)未覆盖:多 inbound 用户 ReleaseBindPort 只应删对应一个 relay(当前实现误删全部,U-03 无回归保护);RotateUserPortForInbound 与并发 RemoveUser/GetBindPort 交错的状态一致性;StartTrafficStats 二次调用对 onCollect 的竞态。这些正是本单元并发/正确性风险最高处,却缺少 race 或多 inbound 断言。补测应以 -race 驱动 rotate 与 remove 并发、并断言释放单端口后其它 inbound relay 仍在。
- **证据**：ReleaseBindPort(usermanager.go:1311)调用 RemoveRulesByUser(1361),测试目录无「多 inbound 释放单端口后保留其它规则」用例;RotateUserPortForInbound(999)全程无锁但无并发删用户的 race 测试。
- **核实**：证实测试缺口真实存在。ReleaseBindPort(1311)在删除单个 BindPort 后调用 forwardMgr.RemoveRulesByUser(1361),而该实现(forward_manager.go:335-352)会删除该用户的全部转发规则,尽管接口另有 RemoveRule(ruleKey) 可按单条删除——因此多 inbound 场景下释放一个端口会误删其它 relay。现有测试仅覆盖幂等/tombstone(usermanager_test.go:630/947/1001),无「多 inbound 释放单端口后其它规则仍在」断言;rotate_inbound_port_test.go 全无 go func/-race,不存在 rotate 与 RemoveUser/GetBindPort 并发一致性测试。P3 恰当。该条实质是为既有 bug U-03 补回归测试的后续项,非旧 finding 直接重复。

### 63. [P3·confirmed] StartMaintenance 后台 goroutine 无停止机制;StartTrafficStats 重复调用会覆盖 onCollect 并忽略新 interval

- **位置**：`pkg/proxy/usermanager/usermanager.go:2154` · 维度：资源 · 单元：UM
- **问题**：StartMaintenance 起了一个 for range ticker.C 的匿名 goroutine(2154-2160),没有任何 stop channel/关闭手段,UserManager 生命周期内无法停止,测试或组件重建时泄漏 goroutine。StartTrafficStats(2095-2146)每次调用都无条件重新赋值 m.statsCollector.onCollect(2113),且当 statsCollector 已存在时不用新 interval 重建 collector;若被调用多次会静默替换回调,第二次 Start() 因 running 已 true 直接 return(1792-1793),新 interval 被忽略。整体生命周期正确性依赖 cmd/server.go 只调用一次的隐式假设,缺乏幂等/防重入保护。
- **证据**：StartMaintenance: go func() { ticker := time.NewTicker(interval); defer ticker.Stop(); for range ticker.C { m.cleanupExpiredUsers() } }()(2154-2160)无 stopCh;StartTrafficStats: m.statsCollector.onCollect = func(...)(2113)每次覆盖;Start(): if sc.running { return }(1792-1793)。
- **核实**：证实两点均属实:StartMaintenance(2150-2161)启动 for range ticker.C 匿名 goroutine 无任何 stopCh,UserManager 生命周期内无法停止(StopTrafficStats 只停 statsCollector),测试/重建会泄漏 goroutine;StartTrafficStats(2095-2145)每次调用无条件重赋 m.statsCollector.onCollect(2113),且 statsCollector 已存在时不按新 interval 重建,二次 Start() 因 sc.running 已 true 提前 return(1792-1793),新 interval 被忽略。正确性依赖 cmd/server.go:260-261 只调一次的隐式假设。P3 恰当,影响低但确为缺乏幂等/停止机制的真实缺陷。

### 64. [P3·confirmed] buildTLSSettings 向固定共享 /tmp 路径写死伪证书并置 allowInsecure=true，含 TOCTOU

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:312` · 维度：安全 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：buildTLSSettings 把证书/私钥路径写死为 /tmp/xray-cert.pem 与 /tmp/xray-key.pem（builder.go:312-313），仅 os.Stat 判断存在与否（builder.go:316，典型 TOCTOU，且世界可读的共享目录任何本地用户可预放/篡改），不存在则写入 generatePlaceholderCert —— 一段语法都不合法的占位 PEM（builder.go:347-366），并在 tlsSettings 里硬编码 allowInsecure:true（builder.go:332）。若此路径被投入使用，会产出带无效证书且关闭校验的 TLS，属明显安全隐患。当前仅测试引用，故列 P3，但应删除或替换为真实证书来源。
- **证据**：builder.go:312-313 固定 /tmp 路径；builder.go:316 `if _, err := os.Stat(certFile); err != nil` 后写文件；builder.go:332 `"allowInsecure": true`；generatePlaceholderCert 写入伪 PEM（347-374）。
- **核实**：已核实:certFile/keyFile写死/tmp(builder.go:312-313),仅os.Stat判存在(:316,TOCTOU),generatePlaceholderCert写入伪PEM(:347-374,0600),tlsSettings硬编码allowInsecure:true(:332)。全部为builder自身行为的代码事实,不涉上游断言。仅builder_test.go引用,P3恰当。

### 65. [P3·confirmed] KeyStore.LoadOrGenerate 把任意 loadKeys 错误当成空库，会覆盖已存在但读失败的 Reality 密钥文件

- **位置**：`pkg/xrayapi/reality/keys.go:85` · 维度：错误处理 · 单元：XAPI
- **问题**：LoadOrGenerate 里 `keys, err := ks.loadKeys(); if err != nil { keys = make(map[string]SavedKey) }`（keys.go:84-87），而 loadKeys 对非 NotExist 的错误（文件损坏、权限不足、JSON 解析失败）都会返回 error（keys.go:170,174）。此时代码把它当作空库，随后为同 tag 生成新密钥并 saveKeys 覆盖写整个文件（keys.go:100-108，WriteFile 0600），会永久丢弃原有全部密钥 —— 对 Reality 而言等于换公钥,所有已下发客户端失联。saveKeys 失败也只 fmt.Printf 警告并照常返回新密钥（keys.go:108-111）。无任何文件锁，多写者并发会互相覆盖。注意生产实际用的是 inbound_adapter.go 的 RealityKeyStore（本包未被生产引用），故列 P3，但本实现本身逻辑不安全。
- **证据**：keys.go:84-87 忽略 loadKeys 错误退化为空 map；keys.go:108-111 saveKeys 失败仅 `fmt.Printf("Warning: failed to save Reality key: %v\n", err)`；loadKeys 对损坏文件返回 error（keys.go:172-175）。
- **核实**：已核实:LoadOrGenerate把loadKeys任意错误退化为空map(keys.go:84-87),而loadKeys对损坏/解析失败返回error(:170,174),随后生成新key并saveKeys整文件覆盖(:100-108,WriteFile 0600),会丢弃原有全部密钥;saveKeys失败仅fmt.Printf警告照常返回(:108-111);无文件锁。已确认pkg/xrayapi/reality包无任何非测试生产import(生产用的是inbound_adapter.go的RealityKeyStore),故实际影响受限,P3恰当。

### 66. [P3·confirmed] buildConfig 丢弃原配置的 Outbounds 与 Routing,新实例失去出站与路由配置

- **位置**：`pkg/xrayapi/hotreload/manager.go:217` · 维度：正确性 · 单元：XAPI
- **问题**：xrayConfig 结构体定义了 Outbounds(:95)和 Routing(:96),但 buildConfig(:205-232)返回时只设置 Log/API/Stats/Policy/Inbounds,从不拷贝原配置的 Outbounds 和 Routing。PrepareNewConfig/prepareForUpdate 生成的新配置因此没有任何出站与路由规则,新 xray 实例即便起来也无法按原策略转发/分流。GetCurrentInbounds 也只从磁盘读 inbounds,原始 outbounds/routing 全程未被读取保留。
- **证据**：buildConfig 返回值(manager.go:217-231)只含 Log/API/Stats/Policy/Inbounds;xrayConfig.Outbounds(:95)/Routing(:96)从未在生成路径被赋值
- **核实**：代码事实属实：buildConfig(manager.go:205-232)返回的 xrayConfig 只赋值 Log/API/Stats/Policy/Inbounds,从不设置 Outbounds(:95)/Routing(:96);GetCurrentInbounds/loadInboundsFromFile 也只读 inbounds。PrepareNewConfig(:183)与 prepareForUpdate(:569)都走 buildConfig,生成的新配置确实无出站与路由。缺陷描述准确。但可达性极弱:ExecuteHotReload/ExecuteHotUpdate 的唯一调用者是 cmd/test_hotreload/main.go,而该文件带 `// +build ignore`,无生产调用路径,实际运行影响为零,故 P2 偏高。

### 67. [P3·confirmed] LoadOrGenerate 把 loadKeys 的任意错误(非 NotExist)当成空库,随后 saveKeys 覆盖原文件,瞬时读错误/文件损坏会摧毁所有已存 Reality 密钥

- **位置**：`pkg/xrayapi/reality/keys.go:84` · 维度：资源 · 单元：XAPI
- **问题**：LoadOrGenerate 中 `keys, err := ks.loadKeys(); if err != nil { keys = make(...) }`(:84-87)。loadKeys(:164-176)对 os.IsNotExist 已单独返回空 map,所以此处 err!=nil 只可能是权限错误、IO 错误或 JSON 解析失败(文件损坏)。这些情况下代码不报错,而是当作空库继续:为该 tag 生成新密钥并 saveKeys(:108)把整个文件覆盖写入,原有其它 tag 的密钥被永久抹除。Reality 公私钥被重置意味着所有已下发客户端全部失效。应区分 NotExist 与真实错误,真实错误必须中止而非覆盖。
- **证据**：keys.go:84-87 `keys, err := ks.loadKeys(); if err != nil { keys = make(map[string]SavedKey) }`;loadKeys keys.go:167 已对 IsNotExist 单独返回;saveKeys keys.go:189 `os.WriteFile(ks.keysFile, data, 0600)` 全量覆盖
- **核实**：代码逻辑属实:keys.go:84-87 对 loadKeys 的任意 err 都置空 map,而 loadKeys:167 已单独处理 IsNotExist,故 err!=nil 只剩权限/IO/JSON 解析错误;随后 saveKeys:189 os.WriteFile 全量覆盖,会抹除其它 tag。判断分支缺失确凿。但可达性:pkg/xrayapi/reality 全包无任何导入者(grep `xrayapi/reality`/`reality.NewKeyStore` 均无命中),属完全死代码;线上真正使用的是 inbound_adapter.go 里同名逻辑的 RealityKeyStore(存在同样缺陷但不在本单元)。'摧毁所有已下发客户端'的运营影响在本文件不会发生,P2 偏高。

### 68. [P3·confirmed] HealthCheck 仅 sleep(500ms)+IsRunning,gRPC 就绪探测被禁用,转发流量可能切到尚未就绪/配置错误的新实例

- **位置**：`pkg/xrayapi/hotreload/manager.go:359` · 维度：错误处理 · 单元：XAPI
- **问题**：HealthCheck(:359-373)只 time.Sleep(500ms) 后调 executor.IsRunning(),gRPC QueryStats 就绪检查被注释禁用(:368-370 TODO)。hotupdate.Execute 的健康检查(update.go:119-126)同样只是 sleep+IsRunning。这意味着'健康'判据实际等于'睡了 500ms 且进程未立刻退出',无法确认 xray API 已监听、配置已加载成功;随后 SwitchForwardRules 就把用户流量切到该实例,若新实例配置无效或还没起监听,切换后即中断。(下层 IsRunning 对自退出进程恒返回 true 的缺陷会进一步放大此问题,此处不重复报下层。)
- **证据**：manager.go:361 `time.Sleep(500 * time.Millisecond)`;:364 `if !executor.IsRunning()`;:368-370 注释禁用 gRPC 健康检查;update.go:119-120 同构
- **核实**：代码事实属实:HealthCheck(manager.go:359-373)仅 time.Sleep(500ms)+IsRunning,:368-370 注释禁用 gRPC 就绪探测;hotupdate/update.go:119-120 同构。健康判据确等于'睡 500ms 且进程未退'。描述准确。但同样只经 +build ignore 的 test_hotreload 触发,无生产路径,P2 偏高。

### 69. [P3·confirmed] KeyStore 无任何锁,LoadOrGenerate/DeleteKey 均为读-改-写文件,并发调用丢失更新

- **位置**：`pkg/xrayapi/reality/keys.go:82` · 维度：并发 · 单元：XAPI
- **问题**：KeyStore(:32-34)只有一个 keysFile 字段,无 sync.Mutex。LoadOrGenerate(:82-114)、DeleteKey(:142-149)都是 loadKeys→修改内存 map→saveKeys 的读-改-写序列,且整体非原子。多个 goroutine(或多进程)并发为不同 tag LoadOrGenerate,或一个删一个加时,后写者会用自己读到的旧快照覆盖文件,丢失对方刚写入的密钥。saveKeys 也是整文件覆盖,无临时文件+rename 的原子写,写一半崩溃会留下截断的 JSON(下次 loadKeys 解析失败,叠加上面的覆盖缺陷会触发密钥重置)。
- **证据**：keys.go:32-34 KeyStore 无锁字段;LoadOrGenerate keys.go:84/101/108 load→改→save;DeleteKey keys.go:143/147/148 load→delete→save;saveKeys keys.go:189 直接 WriteFile 非原子
- **核实**：代码属实:KeyStore(keys.go:32-34)无锁字段;LoadOrGenerate(:84/101/108)与 DeleteKey(:143/147/148)均为 load→改内存 map→saveKeys 的非原子读改写;saveKeys:189 直接 WriteFile 非临时文件+rename,非原子。并发丢更新与写截断风险描述准确。P3 与死代码性质相符,不额外调整。惟需提示:该包无任何生产/测试导入者,为死代码。

### 70. [P3·confirmed] saveKeys 失败仅打印 warning,LoadOrGenerate 仍返回密钥当作已持久化,下次加载会生成不同密钥

- **位置**：`pkg/xrayapi/reality/keys.go:108` · 维度：错误处理 · 单元：XAPI
- **问题**：LoadOrGenerate 在 saveKeys 失败时只 `fmt.Printf("Warning: failed to save Reality key: %v")`(:108-111)并照常返回新生成的 privateKey/publicKey、newlyGenerated=true。调用方据此认为该 tag 密钥已固化并下发给客户端,但磁盘上其实没有写入;进程重启或下次 LoadOrGenerate 会再生成一把不同的密钥,与已下发的公钥不匹配,Reality 握手失败。持久化失败应作为错误返回,让调用方决定。
- **证据**：keys.go:108-111 `if err = ks.saveKeys(keys); err != nil { fmt.Printf("Warning: failed to save Reality key: %v\n", err) }` 之后 `return privateKey, publicKey, true, nil`
- **核实**：代码属实:LoadOrGenerate 在 saveKeys 失败时仅 fmt.Printf 警告(keys.go:108-111)随后 return 新密钥+newlyGenerated=true,未把持久化失败上抛。重启后 tag 未命中会再生成不同密钥,与已下发公钥不匹配,描述准确。P3 合适。同样提示该包为死代码,无生产调用者。

### 71. [P3·confirmed] hotupdate 包与 hotreload.Manager 近乎重复且完全无调用者,并存在独立分歧(NewBinaryPath 无回退、配置不做端口 offset)

- **位置**：`pkg/xrayapi/hotupdate/update.go:54` · 维度：架构 · 单元：XAPI
- **问题**：hotupdate.Execute/Rollback/switchForwardRules 与 hotreload.Manager 的 ExecuteHotUpdate/RollbackUpdate/SwitchForwardRules 是同一 7 阶段流程的近重复实现,整个 hotupdate 包无任何非测试调用者(grep 全仓库无命中)。且二者已经分歧:Execute 直接用 cfg.NewBinaryPath 建 executor(:101),不像 Manager 在 newBinary 为空时回退旧二进制(manager.go:494-497),NewBinaryPath 为空会传入空 BinaryPath;prepareConfig(:205-233)只是把旧配置原样拷贝(端口不做 offset),而 createNewExecutor 却用 62789+offset 的 gRPC 地址,端口既冲突又不匹配。属于死代码 + 漂移风险,建议删除或与 hotreload 合并为单一实现。
- **证据**：grep `xrayapi/hotupdate` 无生产调用者;update.go:101 `createNewExecutor(cfg.NewBinaryPath, ...)` 无空值回退;prepareConfig update.go:205-233 原样拷贝不 offset;与 manager.go:494-497 回退逻辑分歧
- **核实**：各项均核实:grep `hotupdate.Execute`/`hotupdate.Rollback`/`xrayapi/hotupdate` 无任何生产或测试调用者(整包死代码);update.go:101 createNewExecutor(cfg.NewBinaryPath,...) 直传,无 manager.go:494-497 的空值回退;prepareConfig(:205-233)原样拷贝旧配置不做端口 offset,而 mappings 用 oldPort+offset(:95)、createNewExecutor 用 62789+offset gRPC(:272),故新实例仍绑旧端口→与旧实例冲突,而 forward 规则却指向 oldPort+offset→无监听,'既冲突又不匹配'成立。与 hotreload.Manager 七阶段近重复且分歧的判断准确。P3 合适。

### 72. [P3·confirmed] 测试缺口:reality、hotreload、hotupdate、grpc/client 四个可测模块均无 *_test.go,失败/回滚/端口 offset 路径完全未覆盖

- **位置**：`pkg/xrayapi/reality/keys.go:1` · 维度：测试缺口 · 单元：XAPI
- **问题**：本单元仅 types_test.go 与 grpc/builder/builder_test.go 存在。reality/keys.go(密钥生成/加载覆盖/持久化失败)、hotreload/manager.go(端口 offset、switch-forward 回滚、孤儿进程清理)、hotupdate/update.go、grpc/client.go(raw invoke)均无任何测试,违反架构约束 #9(每个可测模块应有 *_test.go)。上面多条 P1/P2(端口冲突、切规则不回滚、密钥被覆盖)恰恰属于最需要单测把关、当前零覆盖的路径。
- **证据**：find pkg/xrayapi -name '*_test.go' 仅返回 types/types_test.go 与 grpc/builder/builder_test.go;reality/hotreload/hotupdate/grpc 目录下无 *_test.go
- **核实**：实测 find pkg/xrayapi -name '*_test.go' 只返回 types/types_test.go 与 grpc/builder/builder_test.go,与 finding 一致。reality/keys.go(190行,含密钥生成/加载/持久化)、hotreload/manager.go(676行,含端口 offset、switch-forward、进程清理)、hotupdate/update.go(329行)、grpc/client.go(73行,raw invoke)均无 *_test.go。且 PROJECT_GUIDE.md 第76行架构约束 #9 明文要求「每个可测模块对应 *_test.go」,这是仓库内可证的约定而非凭记忆。三个大模块确含大量可测逻辑,零覆盖属实。P3 严重度对于测试缺口类元发现恰当,行号指向文件头也合理。

### 73. [P3·confirmed] 注册成功后 SaveAccount 错误被 `_ =` 丢弃，账号 KID 未持久化则每次运行重复注册

- **位置**：`pkg/certmgmt/lego/issuer.go:63` · 维度：错误处理 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：Issue 首次注册后 keyPEM 编码并 `_ = SaveAccount(...)`(:63) 丢弃写盘错误。若 account.json 写入失败(权限/磁盘),下次进程启动 LoadAccount 返回 nil(account_store.go:76-78)，代码再次进入注册分支(issuer.go:53)重复向 ACME 注册。虽然同一账号密钥重复注册 LE 会返回既有账号(幂等)，但持久化失败被静默吞掉意味着 reg(含 KID) 丢失，且掩盖了底层存储故障；应至少记录该错误。
- **证据**：issuer.go:63 `_ = SaveAccount(li.basePath, caURL, req.Email, &domain.AccountData{...})`；LoadAccount 缺文件返回 nil,nil(account_store.go:76-78)。
- **核实**：代码事实核实属实:issuer.go:63 `_ = SaveAccount(...)` 丢弃写盘错误;account_store.go:76-78 LoadAccount 缺文件返回 nil,nil,故 SaveAccount 失败后下次启动会再次进入 issuer.go:53 注册分支。核心缺陷(静默吞掉持久化错误、掩盖存储故障、KID 丢失导致重复注册)可在仓库内直接证实,不依赖上游;文中『LE 幂等返回既有账号』只是缓解性背景,finding 自身亦已 hedge。属真实但影响有限的健壮性问题,量级偏 P3(至少应记录该错误)。

### 74. [P3·confirmed] DNSChallengeConfig.TimeoutSec 文档称默认 10 秒，但 <=0 时不注入任何超时选项，实际走 lego 内部默认

- **位置**：`pkg/certmgmt/lego/solver_dns_slim.go:73` · 维度：正确性 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：domain/types.go:34 注释 `TimeoutSec int // default 10`，但 slim(solver_dns_slim.go:73-75)与 full(solver_dns_full.go:40-42)均只在 TimeoutSec>0 时 AddDNSTimeout，未配置时不设默认 10 秒，实际取 lego 缺省。文档与实现不一致，运维按注释预期 10s 却得到 lego 默认值。应在配置解析处补默认，或修正注释。同样 HTTPChallengeConfig.ProxyHeader 注释 default "Host" 但 solver_http.go:37 仅在非空时 SetProxyHeader(依赖 lego 默认)。
- **证据**：types.go:34 `TimeoutSec int // default 10`；solver_dns_slim.go:73 `if cfg.TimeoutSec > 0` 才 AddDNSTimeout，无 else 默认。
- **核实**：文档/实现不一致确凿:domain/types.go:34 `TimeoutSec int // default 10`,而 solver_dns_slim.go:73 与 solver_dns_full.go:40 均为 `if cfg.TimeoutSec > 0` 才 AddDNSTimeout,无 else 注入默认 10s;manager.go buildIssueRequest 也未补默认。同理 types.go:25 ProxyHeader 注释 default "Host",solver_http.go:37 仅在非空时 SetProxyHeader。该不一致本身在仓库内即可完全证实,不依赖 lego 具体缺省值(无论 lego 用什么默认,都不保证是注释所称的 10s/"Host"),protocolRelated 部分不构成结论前提。行号准确,P3 合适。

### 75. [P3·confirmed] 多 SAN 证书 Issue 仅锁 domains[0]，其余 SAN 域无互斥；domainMu 条目只增不回收

- **位置**：`pkg/certmgmt/service/manager.go:113` · 维度：并发 · 单元：CERT
- **问题**：Issue 只对 domains[0] 加锁(:113)，RenewDomain 对单域加锁(:124)。对同一张多域证书，若一路 Issue([a,b]) 锁 a，另一路 Issue([b,c]) 锁 b，二者共享 SAN b 却互不阻塞，可能并发写相邻资源。当前存储以 domains[0] 为主键，冲突面有限，故严重度低；但结合上面的锁绕过条目，语义上“per-domain 串行”对多 SAN 并不成立。此外 domainMu 用 LoadOrStore 只增不删(:104)，长期运行域名基数大时锁条目内存单调增长。
- **证据**：manager.go:113 mu := m.domainLock(domains[0])；domainLock(:104) LoadOrStore 无删除路径。
- **核实**：全部代码事实核实无误:Issue 仅对 domains[0] 加锁(manager.go:113),RenewDomain 对单域加锁(:124);domainLock 用 LoadOrStore 且无任何删除路径(:104),DeleteCert 亦不回收 mutex,故 domainMu 条目单调增长。多 SAN 场景 Issue([a,b]) 与 Issue([b,c]) 分别锁 a、b 不互斥的语义缺口成立(存储以 record.Domain=domains[0] 为主键,实际文件落到 a.crt/b.crt 不直接碰撞,冲突面确如 finding 所述有限)。属真实但低危的 P3 观察,严重度与行号均恰当。

### 76. [P3·confirmed] 测试缺口：SaveCert 部分失败原子性、AddCertificates/DeleteCert 与续期并发、Revoke、rpc_adapter 导入路径均无覆盖

- **位置**：`pkg/certmgmt/service/rpc_adapter.go:37` · 维度：测试缺口 · 单元：CERT
- **问题**：现有测试覆盖 per-domain 锁(manager_test)、续期窗口优先级(renew_window/issuer_test)、DNS env 恢复、HTTP provider 构造。但本单元最高风险路径无测试：(1) SaveCert 四文件写到一半失败时磁盘一致性/crt-key 匹配；(2) AddCertificates 或 DeleteCert 与 RenewDomain 并发对同域文件的竞态(rpc_adapter 无任何 *_test)；(3) issuer.Revoke 全流程；(4) 外部证书导入(AddCertificates)的落盘布局与 GetCertFiles 回读。建议补 rpc_adapter_test 与 cert_store 的失败注入/并发测试。
- **证据**：pkg/certmgmt/service 下无 rpc_adapter_test.go；cert_store 无失败注入测试；issuer_test.go 仅覆盖 renewBeforeDuration。
- **核实**：测试缺口的事实性陈述准确:service/ 目录仅有 manager_test.go、renew_scheduler_test.go、renew_window_test.go,确无 rpc_adapter_test.go;lego/ 仅有 issuer_test.go、solver_dns_test.go、solver_http_test.go,确无 cert_store_test.go(即无 SaveCert 部分失败/落盘一致性的失败注入测试);issuer_test.go 覆盖续期窗口。AddCertificates/DeleteCert 与 RenewDomain 并发、Revoke 全流程、外部导入回读均无覆盖,均属实。测试类 finding 天然偏软,但所列具体缺口逐条可验证为真,P3 合适。

### 77. [P3·confirmed] Revoke 在证书私钥解析失败时回退临时随机 EC256 密钥，无法产生有效吊销

- **位置**：`pkg/certmgmt/lego/issuer.go:210` · 维度：正确性 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：Revoke(issuer.go:199-235) 先尝试用证书自身私钥签名吊销(RFC 8555 §7.6)；解析失败时回退到 certcrypto.GeneratePrivateKey(EC256) 生成一把与证书、账号都无关的临时密钥(issuer.go:212-217)，再 NewClient(reg=nil) 调 Revoke。ACME 服务器只接受由账号密钥或证书密钥对签名的吊销请求，随机密钥必被拒，回退分支纯属无效——且注释自述『in production this should use stored account key』。此外该方法当前无任何生产调用者（仅在 domain.Issuer 接口中，rpc_adapter 的 DeleteCert 只删本地文件不吊销），是一处会静默失败的死接口。应改用已持久化的账号密钥签名，或在无法签名时明确返回错误而非用无效密钥。
- **证据**：key, err := certcrypto.ParsePEMPrivateKey(legoRes.PrivateKeyPEM)
	if err != nil { // Fallback: generate ephemeral key
		key, err = certcrypto.GeneratePrivateKey(certcrypto.EC256)
- **核实**：issuer.go:210-217 确认：证书私钥 ParsePEMPrivateKey 失败时回退 GeneratePrivateKey(EC256) 生成与证书、账号均无关的临时密钥，再 NewClient(reg=nil) 调 Revoke——该密钥既非账号密钥也非证书密钥对，逻辑上不可能产生有效吊销（RFC 8555 §7.6 只认这两者），无需依赖记忆即可判定回退分支无效；代码注释亦自述『in production this should use stored account key』。grep 确认 LegoIssuer.Revoke 无任何生产调用者（非测试代码中 .Revoke 只出现在 issuer.go 内部的 lego 调用，Manager/rpc_adapter 均不调用，DeleteCert 只删本地文件），确为静默失败的死接口。P3 恰当。

### 78. [P3·confirmed] GetCert 把 LoadCert 的 I/O 错误当作『证书不存在』返回 nil，掩盖瞬时故障

- **位置**：`pkg/certmgmt/service/manager.go:146` · 维度：错误处理 · 单元：CERT
- **问题**：GetCert(manager.go:146-153) 对 LoadCert 的任意错误只 log.Error 后 return nil，调用方 GetCertFiles(rpc_adapter.go:15-21) 因而返回 ok=false，与『该域名无证书』完全无法区分。meta.json 的瞬时读失败(权限抖动/FD 耗尽)会被代理容器的证书查找当成没有证书，进而 TLS 加载失败或回退默认证书；TransferCert 侧则误报 404。区分『不存在(nil,nil)』与『读失败』并向上传播错误更稳妥。
- **证据**：record, _, err := certmgmtlego.LoadCert(m.cfg.Path, d)
	if err != nil { log.Error("certmgmt: GetCert failed", ...); return nil }
- **核实**：cert_store.go:89-117 确认 LoadCert 对『meta 不存在』返回 (nil,nil,nil) 无错，仅对真实 I/O 错误（权限/损坏等）经 ErrStorageIO 返回 error；manager.go:146-153 GetCert 把任意 err 一律 log 后 return nil，与『不存在』不可区分，GetCertFiles(rpc_adapter.go:15-21) 据此返回 ok=false。瞬时读失败被当作无证书成立。行号、非 protocolRelated 均准确，P3 恰当。

### 79. [P3·confirmed] service.Config 无 CAURL 字段，buildIssueRequest 永不设置 CAURL，无法切换 LE staging

- **位置**：`pkg/certmgmt/service/manager.go:170` · 维度：架构 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：domain.IssueRequest 有 CAURL 字段(types.go:54)，但 service.Config(manager.go:15-27) 未暴露任何 CA 目录配置，buildIssueRequest(manager.go:170-206) 也从不填 req.CAURL，effectiveCAURL/NewClient 一律回退 lego.LEDirectoryProduction。结果是签发/续期测试也只能打生产 CA，极易触发 Let's Encrypt 的 new-order/failed-validation 速率限制并把生产账号打入限流。应在 Config 增加 ca_url（默认生产、可配 staging）。
- **证据**：req := domain.IssueRequest{Domains: domains, Email: m.cfg.Email, Bundle: true, ...} — 无 CAURL 赋值；Config 结构体亦无对应字段
- **核实**：manager.go:15-27 Config 确无 CAURL/ca_url 字段，buildIssueRequest(170-206) 构造 IssueRequest 时从不填 req.CAURL；domain/types.go:54 虽有 CAURL 字段，但 Manager 唯一入口 buildIssueRequest 不赋值，effectiveCAURL(issuer.go:253) 空值一律回退 LEDirectoryProduction。故服务层无法切换 staging，核心代码事实完全可核。LE 速率限制为常识性后果但主张的代码缺陷可验证。P3 恰当。

### 80. [P3·confirmed] ctx 未透传给 lego 的 Obtain/Renew，自动续期取消无法中断在途 ACME 调用

- **位置**：`pkg/certmgmt/lego/issuer.go:288` · 维度：正确性 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：Issue/Renew 均接收 context.Context(issuer.go:27,107)，但 obtainCert(issuer.go:288-298) 与 client.Certificate.Renew(issuer.go:163,170) 都不使用 ctx——lego v4 的这些 API 不接受 context。StartAutoRenew 通过 ctx.Done 退出循环(renew_scheduler.go:31,39)，但一旦进入某域名的 obtain/renew（DNS 传播等待可达数分钟），ctx 取消/进程优雅关停都无法打断在途请求，关停会被拖住。属已知上游限制，建议至少在 runRenewCycle 每域前后检查 ctx.Err（已部分做到 renew_scheduler.go:56）并文档化不可中断窗口。
- **证据**：func obtainCert(client *lego.Client, req domain.IssueRequest) ... client.Certificate.Obtain(certificate.ObtainRequest{...}) — 未接收/传递 ctx
- **核实**：issuer.go 确认 Issue/Renew 接收 ctx 但 obtainCert(288-298) 及 client.Certificate.Renew(163,170) 均不传 ctx；仓库内 Obtain 取 ObtainRequest、Renew 取 (res,bundle,bool,chain) 的调用签名本身即证明 lego v4 这些 API 不接受 context（protocolRelated 有仓库材料支撑）。renew_scheduler.go:31,39 靠 ctx.Done 退循环、56 每域前查 ctx.Err，与 detail 描述一致；一旦进入某域 obtain/renew 在途请求确不可中断。P3 恰当。

### 81. [P3·confirmed] domainMu 锁条目只增不减且 Issue 仅锁 domains[0]，多 SAN 证书其余域名不受保护

- **位置**：`pkg/certmgmt/service/manager.go:103` · 维度：资源 · 单元：CERT
- **问题**：domainLock(manager.go:103-106) 用 sync.Map LoadOrStore 按域名创建锁，条目永不回收——长期运行 + 大量不同域名会单调增长(轻微内存泄漏)。同时 Issue 只对 domains[0] 加锁(manager.go:113)，多域名证书的其余 SAN 不加锁；若两张证书共享某个 SAN 且主域不同，对该共享域的并发操作不被串行化。当前拓扑下影响有限，但契约上『同域串行』对非主 SAN 并不成立。
- **证据**：v, _ := m.domainMu.LoadOrStore(d, &sync.Mutex{}) // 只增不删；Issue: mu := m.domainLock(domains[0])
- **核实**：manager.go:103-106 domainLock 用 sync.Map LoadOrStore 建锁且全程无 Delete，长期运行不同域名单调增长（轻微内存增长）属实；Issue(113) 仅对 domains[0] 加锁，多 SAN 证书其余域名不串行化，共享 SAN 且主域不同的并发确不受保护。非 protocolRelated 准确，P3 恰当。

### 82. [P3·confirmed] cert_store/account_store/rpc_adapter/client_factory 无单测，导入与路径穿越等关键分支未覆盖

- **位置**：`pkg/certmgmt/lego/cert_store.go:1` · 维度：测试缺口 · 单元：CERT
- **问题**：现有测试集中在续期窗口、DNS/HTTP provider 构造、哨兵错误(issuer_test/solver_*_test/manager_test/renew_*_test)。缺失：cert_store.go(SaveCert/LoadCert/DeleteCert 的原子性、meta 存在而 resource 缺失返回、domainToFilename 对 '*'/'..' 的处理)、account_store.go(SaveAccount/LoadAccount/GetOrCreateAccountKey)、rpc_adapter.go(AddCertificates 导入往返 + GetCertFiles 参数顺序，正是近期修过分叉 bug 的路径)。按项目约定『每个可测模块应有 *_test.go』，这些纯 I/O、易测且承载安全语义的模块应补测，尤其针对 domain 路径穿越回归。
- **证据**：pkg/certmgmt/lego 下仅 issuer_test.go/solver_dns_test.go/solver_http_test.go；无 cert_store_test.go、account_store_test.go、client_factory_test.go；service 下无 rpc_adapter_test.go
- **核实**：核实属实：pkg/certmgmt/lego 下仅有 issuer_test.go/solver_dns_test.go/solver_http_test.go，确无 cert_store_test.go、account_store_test.go、client_factory_test.go；service 下有 manager/renew_scheduler/renew_window 测试但无 rpc_adapter_test.go。触发路径真实存在：cert_store.go 含 SaveCert/LoadCert/DeleteCert(44/89/120 行)与 domainToFilename(19 行)，且 domainToFilename 仅把 '*' 替换为 '_'(strings.ReplaceAll(d,"*","_"))，对 '..' 完全不处理，路径穿越相关语义确无回归测试；account_store.go 含 SaveAccount/LoadAccount/GetOrCreateAccountKey(37/68/98 行)；rpc_adapter.go 含 AddCertificates/GetCertFiles(37/15 行)。纯 I/O、承载安全语义且易测的模块确实缺测。P3 与 line 1 锚点对测试覆盖类建议均合理。非协议相关。

### 83. [P3·uncertain] StartTrafficStats 重复调用时无锁写 sc.onCollect，与 collect() 的读构成数据竞态

- **位置**：`pkg/proxy/usermanager/usermanager.go:2113` · 维度：并发 · 单元：UM
- **问题**：StartTrafficStats 直接 `m.statsCollector.onCollect = func...`(2113)裸写字段。首次调用因在 Start() 之前赋值尚安全,但函数对重复调用做了防护(statsCollector!=nil 时复用,Start() 内 running 判定为幂等),唯独 onCollect 每次都被无锁覆盖。若在 collectLoop 已运行时二次调用 StartTrafficStats,collect() 在 sc.mu 下读取 sc.onCollect(1932)与此处无锁写并发 → data race,-race 下必报,且可能读到半初始化闭包。onCollect 的赋值应纳入 sc.mu,或整体禁止二次注册。
- **证据**：usermanager.go:2113 无锁赋值 `m.statsCollector.onCollect = ...`;collect() usermanager.go:1932 `if sc.onCollect != nil` 在 sc.mu.Lock(1858) 下读取同一字段;2096 复用已存在 collector。
- **核实**：代码事实成立:2113 `m.statsCollector.onCollect=...` 确为裸写,collect() 1932 在 sc.mu.Lock 下读同一字段;函数 2096 有'复用已存在 collector'逻辑,暗示容忍重复调用。但触发路径当前不可达:生产仅 cmd/server.go:260 调用一次;测试(usermanager_test.go 831/856/891/931、reset_user_total_traffic_test.go 100/204)每处均用新建 UserManager 各调一次,无任何调用点在 collector 运行中二次调用。首次调用中 onCollect 赋值(2113)先于 goroutine 启动(Start 1803)存在 happens-before,无竞态。故 finding 断言'-race 下必报'对当前代码不成立,只有未来出现二次调用才会 race。属潜在硬化项,非现存可复现 bug,建议 P3。行号准确。

### 84. [P3·uncertain] buildShadowsocksInboundConfig 无视 method 一律构造 shadowsocks_2022.ServerConfig，传统 SS 方法会被错配

- **位置**：`pkg/xrayapi/types/types.go:537` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：protocol=="shadowsocks" 时无条件构造 shadowsocks_2022.ServerConfig（types.go:537），把 settings["password"] 直接写入 Key（types.go:546），类型名写死 "xray.proxy.shadowsocks_2022.ServerConfig"（types.go:570、1104）。xray 的 shadowsocks_2022 inbound 只接受 2022-blake3-* 系列 method 且 Key 需为 base64 PSK；若上层传入传统 AEAD 方法（aes-128-gcm/aes-256-gcm/chacha20-ietf-poly1305），会得到类型/密钥语义都错误的配置，xray 侧启动或握手失败。若本项目确定只支持 SS2022 应显式校验 method 并拒绝其它值，而非静默按 2022 处理。
- **证据**：types.go:537 `config := &shadowsocks_2022.ServerConfig{}`；types.go:540-546 method/password→Method/Key；types.go:570 类型名 hardcode，无 method 合法性校验。
- **核实**：代码事实成立:buildShadowsocksInboundConfig(types.go:537)确实无视method一律构造shadowsocks_2022.ServerConfig,password直填Key(:546),类型名hardcode(:570、1104),无method合法性校验。但本条protocolRelated=true,其核心危害断言(xray的shadowsocks_2022 inbound只接受2022-blake3-*且Key需base64 PSK、传统AEAD方法会启动/握手失败)属上游xray行为,仓库内无docs/wiki/config/测试可佐证;且传统SS方法是否真会到达此路径(上层是否只支持SS2022)未被证实。按核实规则protocolRelated无仓库材料支撑时上限为uncertain,故不判confirmed。

### 85. [P3·uncertain] Revoke 硬编码 LEDirectoryProduction，且解析私钥失败时用临时 EC256 密钥签名必然被 CA 拒绝

- **位置**：`pkg/certmgmt/lego/issuer.go:219` · 维度：正确性 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：Revoke 构造 req 时把 CAURL 固定为 lego.LEDirectoryProduction(:219)，忽略证书实际签发的 CA。若证书签自 staging 或自定义 ACME，吊销请求发到生产目录会因找不到该证书而失败。另外 :210-217 的回退逻辑：证书自身私钥解析失败时生成一个全新随机 EC256 密钥用作签名密钥——RFC8555 §7.6 只接受用 账号密钥 或 证书自身密钥对 签名的吊销，随机密钥两者都不是，CA 必然回 unauthorized，回退分支不可能成功，属误导性死逻辑，应直接返回错误。注：Revoke 目前未被 Manager 暴露/接线(DeleteCert 只删本地文件)，但作为吊销路径核心逻辑仍是潜在正确性缺陷。
- **证据**：issuer.go:219 req := domain.IssueRequest{CAURL: lego.LEDirectoryProduction}；:212-216 fallback 生成 certcrypto.GeneratePrivateKey(EC256) 作为签名密钥。
- **核实**：代码事实属实:issuer.go:219 CAURL 硬编码 lego.LEDirectoryProduction,忽略 Issue 里 effectiveCAURL(req.CAURL) 的实际 CA;:210-217 解析私钥失败时 fallback 生成 EC256 临时密钥用于签名。但本条 protocolRelated=true,其核心断言『随机密钥签名必被 CA 拒、回退分支不可能成功』属 RFC8555 §7.6 上游 ACME 行为,仓库内无 docs/wiki/测试可证实,按规则不得凭记忆断言,最高只能 uncertain。且 grep 全仓确认 Revoke 未被任何调用者接线(仅 issuer.go:199 定义 + domain/types.go:91 接口),当前为不可达死代码,实际影响为零。建议 severity 下调。

### 86. [P3·uncertain] WithDNSCredentials 恢复环境变量时对原本未设置的变量用 Setenv(k,"") 而非 Unsetenv，留下空值污染进程环境

- **位置**：`pkg/certmgmt/lego/solver_dns.go:52` · 维度：正确性 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：保存原值用 os.Getenv(:30-32)，对未设置的变量返回空串；恢复阶段(:52-54)统一 os.Setenv(rk, rv)，把原本“不存在”的变量(包括各 DNS 凭证 env 及 LEGO_DISABLE_CNAME_SUPPORT)恢复成“存在但为空”。部分 lego provider 用 env.GetOrDefaultString/LookupEnv 区分未设置与空值，残留的空变量可能改变后续 provider 默认值行为；且 CNAME 抑制标志被恢复为空串而非移除。应记录原始是否存在，未存在时用 os.Unsetenv 恢复。
- **证据**：solver_dns.go:30 originals[k]=os.Getenv(k)(未设置得"")；:52-54 for k,v:=range originals { os.Setenv(k,v) } 无 Unsetenv 分支。
- **核实**：代码事实属实：originals[k]=os.Getenv(k)(:30)对未设置变量得空串，恢复阶段(:52-54)统一 os.Setenv(rk,rv) 无 Unsetenv 分支，故原本未设置的变量被恢复成空值、LEGO_DISABLE_CNAME_SUPPORT 被置空串而非移除，这一点确凿。但本条 protocolRelated=true，其实际危害完全取决于 lego provider 是否用 LookupEnv/GetOrDefault 区分“未设置”与“空值”从而改变默认行为——仓库内 docs/wiki/测试均无材料证实该上游行为(solver_dns_test.go 用 os.Getenv 校验，恰好无法区分空值与未设置,测试通过并不能证伪也不能证实影响)。按对抗规则,第三方行为不得凭记忆断言,故最高 uncertain。进程环境残留本身是真实的低危代码卫生问题,P3 合适。

## 附录：verifier 判定 refuted（不计入，供追溯）

| 单元 | 位置 | 标题 | refuted 理由 |
|------|------|------|-------------|
| UM | `pkg/proxy/usermanager/usermanager.go:2284` | RemoveUser 不清 lastSeenUserTotal，同名用户重建后流量基线残留导致漏计 | 前提(a)属实:lastSeenUserTotal 仅在 2284(ResetUserTotalTraffic)delete,RemoveUser(548-570 走 mutateUser)与 AddUser tombstone 重建(479-529)均不清理,残留条目确实泄漏。但 detail 的核心断言'同名重建导致漏计'不成立:onCollect 2116-2121 的 t 来自对 sc.stats.ByUser 按 username 的聚合,而 ByUser 在删除时也从不被 prune/reset(正是 finding 4),故重建后旧 key 的历史累计仍冻结保留,聚合 t 相对 lastSeenUserTotal(prev)始终 >= 基线。即便复用同一 container:inbound:user key,collect 1871-1873 的计数器回绕处理会把 fresh 小值直接当增量累加到旧 total 之上(ByUser 单调),下一轮 t=旧total+新增、prev=旧total、delta=新增>0,首个采集周期即落盘,不存在'新用户流量在超过历史累计前不落盘'。唯一能把 ByUser total 降到 prev 以下的路径是 resetUserTotal/drainForUserLocked,而它们由 ResetUserTotalTraffic 触发并在 2284 同步 delete lastSeenUserTotal,二者始终同进退。两个 finding 相互矛盾:finding 4 的'从不 prune'恰恰保证了此处基线一致、消除了 finding 5 所述漏计。残留 map 条目本身无害。 |
| UM | `pkg/proxy/usermanager/sync/hash.go:51` | ComputeHash 把 UpdatedAtUs 计入哈希,时钟漂移下相同逻辑修改产生不同 hash 触发多余全量同步 | 机制自相矛盾,证伪。CompareDigests 的 hash 不等触发全量路径(digest.go:66-70)以 remote.UpdatedAtUs == local.UpdatedAtUs 为前置条件。时钟漂移使 UpdatedAtUs 不同时,根本不比较 hash——由 IsNewer(version.go:10-15,先比 UpdatedAtUs)决定是否全量,与 hash 是否含 UpdatedAtUs 无关;此类全量由版本差驱动,移除 hash 中的 UpdatedAtUs 也不会减少。而当 UpdatedAtUs 相等进入 hash 比较路径时,该字段对双方 hash 贡献完全相同,不可能成为 hash 不等的来源。故「把 UpdatedAtUs 计入哈希导致时钟漂移下多余全量同步」不成立。另:该条被标 protocolRelated=true,但属纯内部 sync 逻辑,与第三方协议无关,标注亦不当。 |
| CERT | `pkg/certmgmt/service/renew_scheduler.go:28` | 自动续期后台 goroutine 无 panic recover，任一续期周期 panic 会永久静默停掉全部自动续期 | 代码事实(goroutine :28 无 defer recover)属实,但本条断言的失效模型错误。Go 运行时语义:goroutine 内未被 recover 的 panic 会终止整个进程并打印堆栈,而非“只崩这个 goroutine、进程继续运行、从此静默不再续期”。因此 finding 描述的“永久静默停掉全部自动续期、直到证书过期才暴露”这一核心危害不可能发生——真实后果是进程整体崩溃(高噪声、通常被 supervisor 重启)。recover 的价值恰好与 finding 所述相反(是防止单次 panic 拖垮整个守护进程),故按其陈述的理由与影响予以 refuted。 |
