# L2 核心域 · 第二轮补充 review

> 第二轮独立 review（全程 Fable 5）：拿第一轮清单换角度找**新问题** + 挑战疑似误报。本文件为第一轮 `L2-domain.md` 的**增量补充**，新问题经对抗性 verifier 核实，质疑经中立裁决者裁定。

## 第二轮统计

| 维度 | 数量 |
|------|------|
| 新发现·保留(confirmed+uncertain) | 65 |
| — confirmed | 58 |
| — uncertain | 7 |
| 新发现·refuted(剔除) | 1 |
| 新发现·unverified(verifier无结果) | 0 |
| 质疑·裁定第一轮误报/severity错(pass1-wrong) | 10 |
| 质疑·驳回(pass1-stands) | 0 |
| 质疑·uncertain | 0 |

| 新发现优先级 | P0 | P1 | P2 | P3 |
|------|----|----|----|----|
| 保留 | 0 | 3 | 20 | 42 |

## 对第一轮的质疑裁决

### ✅ 裁定第一轮有误（需在第一轮文档中撤销/调整）

| 单元 | 第一轮条目 | 质疑 | 建议 severity | 裁决理由 |
|------|-----------|------|--------------|---------|
| FWD | `pkg/proxy/forward/clientlimit.go:275` drainEnd map 只写不清导致按客户端 IP 无界增长，且 RecordActivity 在数据面热路径抢用户级全局锁写入永远无人读取的数据 | severity-too-high | P2 | 问题属实但 P1 高估。代码证据:drainEnd 在 clientlimit.go:270/280 只写(覆盖写,每去重 remote IP 一条 entry),读取方 IsDrainExpired(:284)/ClearDrain(:296) 全包零调用方;Release 的回收 timer(:239)只 delete(l.slots, ip) 不清 drainEnd,泄漏确实存在。但每条 entry 约几十字节,上界为该用户 limiter 生命周期内的去重客户端 IP 数,且 forward_manager.go:774 会 delete(m.userClientLimiters, username),limiter 可随用户拆除被 GC——是慢泄漏而非无界膨胀。锁为用户级共享(forward_manager.go:261),activityReader.Read(relay_tcp.go:380-386)每 ≤32KB Read 抢一次,临界区仅一次 map 赋值,属性能损耗非稳定性事故。应与同问题的 P2 条目统一为 P2。 |
| FWD | `pkg/proxy/forward/relay_tcp.go:120` acceptLoop 对非 ctx 关闭的 Accept 错误无退避直接 continue，持续性错误会 100% CPU 空转 | severity-too-low | P2 | 质疑方向成立(原 P3 确实过低),但提议的 P1 过冲。relay_tcp.go:113-122 确认:Accept 错误且 ctx 未取消时无 sleep、无日志、直接 continue;EMFILE 下 Accept 立即返回错误且 backlog 连接仍在,会真热自旋,每个 relay 烧满一核,多规则时多核。但触发前提是进程已处于 fd 耗尽等事故态,自旋是事故放大器而非独立故障源,且修复是加 net.Error 判断 + 指数退避 + 日志的小改动。与 :116 的 P1 条目是同一缺陷的重复录入,统一方向应是把 P1 那条降到 P2,而非把本条抬到 P1。最终定级 P2。 |
| FWD | `pkg/proxy/forward/clientlimit.go:275` drainEnd map 只写不清导致按客户端 IP 无界增长，且 RecordActivity 在数据面热路径抢用户级全局锁写入永远无人读取的数据 | severity-too-high | P2 | 与 idx 0 同一 finding,质疑成立。核实:IsDrainExpired/ClearDrain 确无任何调用方(grep 全包仅接口定义与实现自身),drainEnd 纯写不读;clientlimit.go:280 是 map 覆盖写,增长上界为去重 remote IP 数,每条几十字节,属慢泄漏;forward_manager.go:774 可删除 limiter 使整体可 GC。锁竞争限于同一用户的数据面 goroutine(limiter 按 username 共享,forward_manager.go:261),每 32KB Read 一次 map 赋值级临界区,不构成进程级稳定性风险。P2 定级准确。 |
| FWD | `pkg/proxy/forward/relay_tcp.go:116` acceptLoop 对持续性 Accept 错误无退避、无日志：fd 耗尽（EMFILE）时热循环空转 100% CPU | severity-too-high | P2 | 质疑成立,P1 高估。relay_tcp.go:113-122 确认缺退避/缺日志属实:非 ctx 关闭的 Accept 错误直接 continue,EMFILE 下会 100% CPU 热自旋。但该路径只在进程已 fd 耗尽等事故态下触发,是事故放大器而非独立故障源;Stop()(relay_tcp.go:90-93)先 cancel 再 Close listener,正常关闭路径不受影响;修复为加 net.Error 判断 + 指数退避 + 日志的局部小改动。与同轮 :120/:121 两条重复报描述同一问题,应统一收敛为 P2。 |
| FWD | `pkg/proxy/forward/relay_tcp.go:120` acceptLoop 对非 ctx 关闭的 Accept 错误无退避直接 continue，持续性错误会 100% CPU 空转 | severity-too-low | P2 | 质疑成立,原 P3 过低,应升为 P2。relay_tcp.go:113-122 确认:每个 TCPRelay 的 acceptLoop 独立 goroutine,EMFILE/ENFILE 下 Accept 立即返错且无任何 sleep/日志,各 relay 同时热自旋,多规则时可烧满多核并阻碍 fd 回收后的恢复——高于纯清理级(P3)。Go http.Server 对同类场景实现 5ms-1s 指数退避可作修复参照。与 :116(应降)/:121 两条重复报统一为 P2。 |
| UM | `pkg/proxy/usermanager/usermanager.go:1504` SyncUpsertUser「非更新但采纳 LoginPassword」分支改字段不重算 Hash，破坏 Hash 不变式 | severity-too-high | P3 | 质疑成立。代码证据：(1) 全仓 ComputeHash 唯一调用点是 stampVersion（usermanager.go:330），不存在任何本地重算校验 Hash==ComputeHash(user) 的运行时消费者；CompareDigests（sync/digest.go:66-69）只比较两端存储的 Hash 字符串。(2) hash.go:57-62 明确注释：LP 仅非空时参与哈希，就是为让无 LP 记录与旧节点哈希兼容。(3) 若在 1504-1509 分支采纳 LP 后重算 Hash，本节点会与持有同版本(同 UpdatedAtUs/OriginNode)但无 LP 的对端在 digest 比较中命中 same-version hash-mismatch 分支持续拉全量，而对端回传的记录版本不更新且 LP 为空，SyncUpsertUser:1499 直接 skip，无法收敛，反而制造心跳级抖动——不重算是有意的兼容性权衡。残余风险仅为：同版本同 Hash 但 LP 内容分叉时 digest 自愈无法检测，该分叉在用户下一次正常修改（mutateUser→stampVersion 会把 LP 计入新 Hash）时自动收敛。无崩溃、无数据丢失、可自愈，降为 P3 恰当。 |
| UM | `pkg/proxy/usermanager/usermanager.go:2237` GetAllDeltaTraffic 读取与重置分两次独立取 sc.mu，tick 插入会丢失流量 | severity-too-high | P2 | 质疑成立，问题存在但严重度应为 P2。核实：GetAllDeltaTraffic（usermanager.go:2233-2246）中 GetStats()（L2237，RLock 后即释放）与 resetAllDeltas()（L2243，另取写锁）之间插入 collect() 时，该 tick 的 delta 会被清零且永不进入上报流，race 窗口真实存在。但持久化累计流量走独立路径：collect() 在 sc.mu 持有下同步调用 onCollect（L1932-1937），onCollect（L2113-2144）用累计值 TotalUplink/TotalDownlink 与 m.lastSeenUserTotal 差分累加 user.TrafficTotalUplink/Downlink 并 store.Save 落库，与 Delta 字段清零完全解耦，DB 侧计费/累计总量不丢。丢失面仅限 DrainStats（bandwidth_stats_collector.go:35）→GetBandWidthStats RPC（pkg/rpc/server/end_node_user.go:338）的 Prometheus/center 上报通道，上限一个采集周期增量且需精确交错才触发。与第一轮 line 2242 同问题条目的 P2 定级统一为 P2 合理；line 2239 的共享 map 并发读写（GetStats 返回 sc.stats 浅拷贝共享 ByUser map，L2239 无锁遍历可与 collect 并发写冲突）是另一条独立 P1，不受影响。 |
| UM | `pkg/proxy/usermanager/usermanager.go:2237` GetAllDeltaTraffic 读取与重置分两次独立取 sc.mu，tick 插入会丢失流量 | severity-too-high | P2 | 与 idx 1 为同一 finding 的同向质疑，结论相同。质疑中的代码引用全部核实无误：L2237 GetStats 与 L2243（质疑写 L2243/L2242 均指 resetAllDeltas 调用）之间插入 collect() 丢一个 tick 的 delta；onCollect 在 sc.mu 持有下同步执行（usermanager.go:1926-1938 注释与代码一致），TrafficTotalUplink/Downlink 经 L2113-2144 差分累加落 SQLite，不经过 Delta 计数器，DB 不丢数。丢失量上限为单采集周期增量、仅在 scrape 与 collect tick 精确交错时发生，影响限于 Prometheus/center 拉取口径的统计偏差，属 P2 而非 P0/P1 正确性损坏。该条与第一轮 line 2242 的 P2 条目实为重复，统一为 P2。 |
| CERT | `pkg/certmgmt/lego/solver_dns_slim.go:73` DNSChallengeConfig.TimeoutSec 文档称默认 10 秒，但 <=0 时不注入任何超时选项，实际走 lego 内部默认 | false-positive | — | 质疑成立。solver_dns_slim.go:73-75 在 TimeoutSec<=0 时确实不注入 AddDNSTimeout，但模块缓存中 lego v4.9.0 的实际默认值就是 10 秒：challenge/dns01/nameserver_unix.go:8 `var dnsTimeout = 10 * time.Second`（AddDNSTimeout 在 nameserver.go:56 覆写的正是这个包级变量），且 dns_challenge.go:60 Challenge 构造时 `dnsTimeout: 10 * time.Second`。本项目仅部署 Linux（Windows 才是 20s），因此不注入选项时落到的 lego 默认与 domain/types.go:34 的 `// default 10` 文档完全一致，文档与行为无偏差，第一轮该条系误报。 |
| CERT | `pkg/certmgmt/lego/issuer.go:288` ctx 未透传给 lego 的 Obtain/Renew，自动续期取消无法中断在途 ACME 调用 | false-positive | — | 质疑成立。go.mod 锁定 github.com/go-acme/lego/v4 v4.9.0，模块缓存中 certificate/certificates.go:107 为 `func (c *Certifier) Obtain(request ObtainRequest) (*Resource, error)`，:401 为 `func (c *Certifier) Renew(certRes Resource, bundle, mustStaple bool, preferredChain string) (*Resource, error)`，两者均不接受 context。issuer.go:27/107/199 的 Issue/Renew/Revoke 虽持有 ctx，但 issuer.go:289 的调用点根本不存在可透传的参数——'ctx 未透传' 在该 lego 版本下无从修复，属上游库 API 限制而非本仓库遗漏；作为 file:line 级缺陷不成立，至多可作为依赖升级建议记录。 |

## 第二轮新发现（保留条目）

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P1 | ✓ | 正确性 | FWD | `pkg/proxy/forward/ratelimit.go:205` | limitedReader 先扣 token 再 Read，短读不退还：小包/交互式流量按 32KB 粒度烧预算，共享桶被空耗导致同用户全部连接远低于配置限速 |
| 2 | P1 | ✓ | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:380` | UserEvent.User 携带 m.users 内的活指针，订阅者在锁外读字段与 mutateUser 写构成系统性数据竞态 |
| 3 | P1 | ✓ | 正确性 | CERT | `pkg/certmgmt/service/manager.go:188` | challenge.type 配置为 "dns" 别名时被 manager 接受填充 DNS 配置，但 issuer 只认 "dns01"，实际静默降级为 HTTP-01 并监听 :80 |
| 4 | P2 | ✓ | 正确性 | FWD | `pkg/proxy/forward/ratelimit.go:260` | WaitN 超时返回 ctx.Err() 时已部分消费的 token 不退还：UDP 饱和时被丢弃的数据包仍烧预算，并窃取同用户 TCP 流的共享桶配额 |
| 5 | P2 | ✓ | 并发 | UM | `pkg/proxy/usermanager/usermanager.go:717` | ListUsers/GetUser/UserEvent.User 暴露共享 *contracts.User，PortMappings 并发 map 读写可致进程 fatal |
| 6 | P2 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/bandwidth_stats_collector.go:35` | DrainStats 先 reset 全部 delta 再按 ListUsers 过滤，tombstone/过期/组不可见用户的流量被清零丢弃 |
| 7 | P2 | ✓ | 错误处理 | UM | `pkg/proxy/usermanager/usermanager.go:1083` | RotateUserPortForInbound 失败路径：旧/临时 relay 均已删除后 final AddRule 失败直接返回，用户断流且状态无回滚 |
| 8 | P2 | ✓ | 错误处理 | UM | `pkg/proxy/usermanager/usermanager.go:1083` | RotateUserPortForInbound Step-4 双重失败后旧/临时 relay 均已删除，该 inbound 完全断流且无回滚 |
| 9 | P2 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:45` | 全部 VMess/VLESS Build* 变体验证 uuid 后彻底丢弃，产出的 User 无 Account，inbound 无任何认证凭据 |
| 10 | P2 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:294` | buildReceiverSettings 把 listen 字符串的 ASCII 字节直接塞进 IPOrDomain_Ip，产出非法 IP 字节 |
| 11 | P2 | ✓⚠️协议 | 安全 | XAPI | `pkg/xrayapi/types/types.go:360` | buildVMessUser 对缺失 id 的 client 静默回落为全零 UUID 账号，形成可被公知凭据登录的用户 |
| 12 | P2 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:331` | SwitchForwardRules/RollbackUpdate 每个端口映射只切换第一条匹配规则，同目标端口的其余转发规则在旧实例停止后全部黑洞 |
| 13 | P2 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:331` | SwitchForwardRules 对同一 target 端口的多条转发规则只切换第一条，其余用户在旧实例停止后流量全部黑洞 |
| 14 | P2 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:443` | ExecuteHotReload 成功后不持久化任何状态：OldConfigPath/currentConfig 不更新，二次 reload 丢失上次新增 inbound 且与仍在运行的新实例端口冲突 |
| 15 | P2 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:569` | Manager.ExecuteHotUpdate 的 prepareForUpdate 不应用 PortOffset，新实例与仍在运行的旧实例监听端口完全相同，更新必然失败 |
| 16 | P2 | ✓ | 并发 | XAPI | `pkg/xrayapi/hotreload/manager.go:342` | SwitchForwardRules 的 RemoveRule→AddRule 窗口内监听端口被释放回 allocator，可被并发 AddRule 随机分配抢占；且直接操作 ForwardManager 绕过 usermanager 端口账本（约束#4） |
| 17 | P2 | ✓ | 正确性 | CERT | `pkg/certmgmt/service/rpc_adapter.go:74` | GetCertInfo 返回 typed-nil interface，调用方 `cert == nil` 判断永远为 false，缺证书时永不触发自动签发 |
| 18 | P2 | ✓ | 正确性 | CERT | `pkg/certmgmt/service/rpc_adapter.go:42` | AddCertificates 不校验私钥合法性及 cert/key 配对，坏配对就地覆盖同域正常证书，代理热加载后 TLS 中断 |
| 19 | P2 | ✓⚠️协议 | 并发 | CERT | `pkg/certmgmt/lego/account_store.go:107` | 并发签发不同域名时共享账号私钥存在 TOCTOU 竞态，导致 keys/<email>.key 与 account.json 的注册 KID 不匹配，后续 ACME 请求 JWS 校验全部失败 |
| 20 | P2 | ✓ | 错误处理 | CERT | `pkg/certmgmt/lego/cert_store.go:146` | ListCerts 对读失败/JSON 反序列化失败的 .meta.json 静默 continue，损坏的证书元数据会让该域名彻底从自动续期扫描中消失，直到过期无告警 |
| 21 | P2 | ?⚠️协议 | 安全 | XAPI | `pkg/xrayapi/hotreload/manager.go:210` | buildConfig 生成的 api inbound 缺 listen 字段，dokodemo API 口绑 0.0.0.0 暴露无鉴权 HandlerService |
| 22 | P2 | ?⚠️协议 | 错误处理 | CERT | `pkg/certmgmt/service/renew_scheduler.go:64` | 续期失败无退避：进入续期窗口后每 1 分钟重跑一次完整 ACME 流程，1 小时内即撞 Let's Encrypt failed-validation 限流 |
| 23 | P2 | ?⚠️协议 | 正确性 | CERT | `pkg/certmgmt/lego/issuer.go:35` | ACME 账号私钥双份存储（keys/<email>.key 与 account.json 内嵌 PEM）失同步时，用新生成的 key 配旧 registration KID，所有 ACME 请求被 CA 拒绝 |
| 24 | P3 | ✓ | 正确性 | FWD | `pkg/proxy/forward/clientlimit.go:182` | CancelAcquire 直接删除处于 recycling 状态的槽位：同 IP 重连一次拨号失败即瞬间释放配额，RecycleDelaySec 防换 IP 保护被绕过 |
| 25 | P3 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:251` | MaxClients<0 与 storedConfig 的交互顺序相关：首条规则为 -1 时用户级限制永远不生效，且注释宣称的 per-rule passthrough 因共享限流器根本不成立 |
| 26 | P3 | ✓ | 安全 | FWD | `pkg/proxy/forward/forward_manager.go:20` | WithPortRange 构造 allocator 时丢失 UseRandom：走该 option 的实例退化为 round-robin 顺序分配，端口可预测 |
| 27 | P3 | ✓ | 架构 | FWD | `pkg/proxy/forward/types.go:126` | RuleKey 不含 Network 维度：同一 (container, inbound, user) 无法同时建 TCP 和 UDP 转发，GetBindPort 会把错误传输类型的既有端口直接返回 |
| 28 | P3 | ✓ | 资源 | FWD | `pkg/proxy/forward/relay_tcp.go:370` | 数据面缓冲零复用：每条 TCP 连接常驻 2×32KB、每个 UDP 会话 64KB 拷贝缓冲，均为一次性分配无 sync.Pool |
| 29 | P3 | ✓ | 正确性 | FWD | `pkg/proxy/forward/ratelimit.go:261` | WaitN 在 ctx 超时返回错误时不回滚已消耗的部分 token：UDP 每丢一包都泄漏 token，饱和时同桶 TCP 流被无谓扣血 |
| 30 | P3 | ✓ | 错误处理 | FWD | `pkg/proxy/forward/forward_manager.go:254` | AddRule 在 relay.Start 之前就对既有共享 limiter 执行 SetConfig 并覆盖 userClientLimitConfigs，Start 失败不回滚：失败的 AddRule 仍永久改写了用户级限连策略且已对运行中 relay 生效 |
| 31 | P3 | ✓ | 安全 | FWD | `pkg/proxy/forward/forward_manager.go:19` | WithPortRange 构造的 allocator 未设置 UseRandom，端口分配退化为 round-robin 顺序模式，与其余两个构造入口强制随机的行为不一致 |
| 32 | P3 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:690` | GetUserBandwidthLimit 文档承诺“显式设为 unlimited 返回 (0, true)”，实现却在清零后返回 (0, false)，调用方无法区分“显式无限”与“从未设置” |
| 33 | P3 | ✓ | 正确性 | FWD | `pkg/proxy/forward/forward_manager.go:552` | QueryTrafficStats 聚合行以组内首条记录整体为基底，RuleKey/ListenPort/TargetAddr 等规则级字段被随机保留到聚合结果中误导消费方；且 inbound 分组键单/多维格式不一致 |
| 34 | P3 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:356` | mutateUser 不过滤 tombstone：Set*/Update* 可作用于已删除用户并触发容器为其重建 relay，进而永久阻塞同名 AddUser |
| 35 | P3 | ✓ | 安全 | UM | `pkg/proxy/usermanager/usermanager.go:907` | ruleKey 用 ":" 拼接但 username/inboundTag 无字符集校验，可构造 key 碰撞导致跨用户端口/流量互串 |
| 36 | P3 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:510` | AddUser 在设置 LoginPassword 之前 stampVersion，用户创建即违反 Hash==ComputeHash(user) 不变式 |
| 37 | P3 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:2220` | GetUserDeltaTraffic/GetUserStats 对多 inbound 用户只返回任意一条记录，reset=true 却清空该用户全部 inbound 的 delta |
| 38 | P3 | ✓ | 资源 | UM | `pkg/proxy/usermanager/usermanager.go:1642` | tombstone 永不物理删除且 ListDigests 全量包含，users map/DB/每次心跳 digest 载荷随历史删除数无界增长 |
| 39 | P3 | ✓ | 架构 | UM | `pkg/proxy/usermanager/usermanager.go:82` | UserEventExpire 定义后全仓库无发送点，过期统一走 UserEventRemove，订阅方无法区分过期与删除 |
| 40 | P3 | ✓ | 测试缺口 | UM | `pkg/proxy/usermanager/usermanager_test.go:1` | 测试缺口：tombstone 上的 mutateUser 变更、DrainStats 过滤丢流量、多 inbound delta 语义均无覆盖 |
| 41 | P3 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:1536` | SyncUpsertUser 墓碑分支只采纳版本字段+TargetGroup，其余参与 Hash 的内容字段与远端分叉且 digest 永远一致，无法自愈 |
| 42 | P3 | ✓ | 正确性 | UM | `pkg/proxy/usermanager/usermanager.go:908` | GetBindPort 命中已有规则时不校验 TargetAddr 与 req.TargetPort 一致，backend 端口变更后返回指向旧 target 的转发端口 |
| 43 | P3 | ✓ | 测试缺口 | UM | `pkg/proxy/usermanager/rotate_inbound_port_test.go:9` | 测试缺口：GetBindPort 重启幂等（BindPorts 重复累积）与 Rotate Step-4 失败断流路径均无覆盖 |
| 44 | P3 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:381` | buildWebSocketTLSSettings 把 network 写成 "tcp"，即使 streamSettings 接通 wsSettings 也永远不会被消费 |
| 45 | P3 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:394` | buildRealitySettings 只带 publicKey/shortId，缺服务端 Reality 必需的 privateKey/dest/serverNames，BuildVLESSReality 签名先天不足 |
| 46 | P3 | ✓⚠️协议 | 错误处理 | XAPI | `pkg/xrayapi/types/types.go:420` | vless/trojan 构建器对缺失 id/password 的 client 仍 append 带 nil Account 的 User |
| 47 | P3 | ✓⚠️协议 | 错误处理 | XAPI | `pkg/xrayapi/types/types.go:657` | TLS certificateFile/keyFile 读取失败被静默吞掉，产出无证书内容的 tls.Config |
| 48 | P3 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:297` | GetPortMapping 对 port 仅做 float64 断言，字符串端口静默得 0，端口映射丢失导致转发规则不切换 |
| 49 | P3 | ✓ | 架构 | XAPI | `pkg/xrayapi/hotreload/manager.go:114` | Manager 的 mu/currentConfig 是死缓存：全包无任何写入路径，RWMutex 保护恒为 nil 的字段 |
| 50 | P3 | ✓ | 架构 | XAPI | `pkg/xrayapi/hotreload/manager.go:546` | drain-old 阶段的 500ms 等待无意义：RemoveRule 时 relay.Stop 已通过 ctx cancel 强制断开全部活动连接，所谓零停机切换实为全量断连 |
| 51 | P3 | ✓ | 正确性 | XAPI | `pkg/xrayapi/hotreload/manager.go:297` | string 端口在 hotreload 链路上静默断裂：applyPortOffset 保留字符串类型，GetPortMapping 只做 float64 断言，映射丢失导致该 inbound 的转发规则永不切换 |
| 52 | P3 | ✓⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:45` | 所有 VMess/VLESS Build* 方法校验 uuid 非空后将其完全丢弃，产出的 User 无 Account，VLESS Config 还缺 Decryption |
| 53 | P3 | ✓⚠️协议 | 安全 | XAPI | `pkg/xrayapi/types/types.go:360` | buildVMessUser 对缺失/非法 id 兜底为全零 UUID 账户，且 buildVMessInboundConfig 吞掉 user 构建错误静默丢用户 |
| 54 | P3 | ✓ | 资源 | XAPI | `pkg/xrayapi/grpc/client.go:44` | grpc.Client 的 Dial/Invoke 全部使用无超时的 context.Background()，xray 无响应时调用方永久阻塞；RemoveInbound 还忽略 json.Marshal 错误 |
| 55 | P3 | ✓ | 架构 | XAPI | `pkg/xrayapi/hotreload/manager.go:376` | Manager.Rollback 是恒返回 nil 的 no-op stub；Config 的 HealthCheckTimeout/HealthCheckInterval/GRPCAddress/UseNewBinary 四字段从未被读取 |
| 56 | P3 | ✓ | 错误处理 | CERT | `pkg/certmgmt/lego/account_store.go:53` | SaveAccount 直接 os.WriteFile 非原子写 account.json，损坏后 LoadAccount 返回硬错误且无自愈路径，签发/续期永久失败 |
| 57 | P3 | ✓⚠️协议 | 错误处理 | CERT | `pkg/certmgmt/service/rpc_adapter.go:26` | ObtainNewCert 无条件全新签发，不检查既有证书新鲜度，center 侧批量下发时易撞 LE duplicate-certificate 限流（5 张/周） |
| 58 | P3 | ✓ | 正确性 | CERT | `pkg/certmgmt/lego/issuer.go:306` | 多 SAN 签发只以 domains[0] 落盘一份 meta，其余 SAN 域名对 GetCertFiles/GetCert/续期完全不可见 |
| 59 | P3 | ✓ | 安全 | CERT | `pkg/certmgmt/lego/account_store.go:29` | 账号目录用 email 与 CA host 原样拼进 filepath.Join，未过滤 '..'/分隔符，构成 cert_store 之外第二个未被 pass1 覆盖的路径穿越落盘点 |
| 60 | P3 | ✓ | 资源 | CERT | `pkg/certmgmt/lego/cert_store.go:40` | atomicWrite 在 os.Rename 失败时不清理 <path>.tmp，残留含私钥 PEM 的临时文件；且失败后目标文件保持旧值使『原子写』语义在错误路径退化 |
| 61 | P3 | ✓ | 正确性 | CERT | `pkg/certmgmt/service/rpc_adapter.go:42` | AddCertificates 导入外部证书时不校验私钥可解析、也不校验 key 与 cert 匹配，坏的密钥对会被落盘并被代理热加载，运行期 TLS 握手才以不透明错误失败 |
| 62 | P3 | ?⚠️协议 | 正确性 | FWD | `pkg/proxy/forward/relay_udp.go:181` | 零长度 UDP 数据报被静默丢弃且不刷新会话活跃时间：合法的 0 字节 keepalive 会话仍会被 idle GC 掐掉 |
| 63 | P3 | ?⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/grpc/builder/builder.go:255` | builder.BuildShadowsocks 与 types 路径同病：任意 method 一律构造 shadowsocks_2022.ServerConfig |
| 64 | P3 | ?⚠️协议 | 正确性 | XAPI | `pkg/xrayapi/types/types.go:257` | buildReceiverConfigProto 把 listen="::" 当作 "0.0.0.0" 同类直接丢弃，IPv6 监听意图静默降级为 IPv4 any |
| 65 | P3 | ?⚠️协议 | 错误处理 | XAPI | `pkg/xrayapi/types/types.go:608` | buildStreamConfigProto 在 security=tls/xtls/reality 但对应 settings 块缺失时仍设置 SecurityType，产出带安全类型却无 SecuritySettings 的 StreamConfig |

### 详细

#### 1. [P1·confirmed] limitedReader 先扣 token 再 Read，短读不退还：小包/交互式流量按 32KB 粒度烧预算，共享桶被空耗导致同用户全部连接远低于配置限速

- **位置**：`pkg/proxy/forward/ratelimit.go:205` · 维度：正确性 · 单元：FWD
- **问题**：limitedReader.Read 先调用 waitForTokens(len(p)) 从桶中扣除 allowed（上限为 min(tokens, maxChunk, 32KB copy buffer)）个 token，然后才执行 lr.r.Read(p[:allowed])。TCP Read 返回的字节数通常小于 allowed（socket 缓冲里只有一个 MSS、目标端慢速滴流、交互式小包等），差额 token 永不退还。由于该桶是用户级共享桶（TCP 上下行 + UDP WaitN 同一实例），一条高频小包连接（如 50pps×200B 的交互流量）每秒可烧掉 50×32KB=1.6MB 的预算却只传输 10KB，直接把同用户其它连接饿死；单连接场景下有效吞吐也可坍缩到配置值的 1/20 量级。正确做法是 Read 返回后按实际 n 退还 allowed-n。
- **证据**：ratelimit.go:203-207 `allowed := lr.limiter.waitForTokens(len(p)); return lr.r.Read(p[:allowed])`；waitForTokens 在 :193 `l.tokens -= float64(allowed)` 后无任何退还路径；relay_tcp.go:370 传入的 buf 恒为 32KB。
- **核实**：代码证实：ratelimit.go:203-207 limitedReader.Read 先 waitForTokens(len(p)) 扣减 token（:193 l.tokens -= allowed，全文件无退还路径），再 lr.r.Read(p[:allowed])，短读差额永久损失。relay_tcp.go:15/370 证实 copy buffer 恒为 32KB，故 allowed 上限为 min(tokens, maxChunk≥16KB, 32KB)。桶为用户级共享单实例（forward_manager.go:170-188 同一 userBandwidthLimiter 供该用户所有规则），且 relay.go Limiter 契约要求 WaitN 与 LimitReader 共用同一桶，UDP 也消耗它。TCP Read 常返回单个 MSS/交互式小包，每次却烧掉最多 maxChunk 的 token，共享桶被无效消耗，同用户其它连接吞吐被压低、单连接有效吞吐远低于配置值。烧速受桶补充速率约束（不会超过 rate），但这正是问题所在：预算被未传输的字节占满。触发条件仅为配置了带宽限制+存在短读，极常见。P1 对核心限速数据面在常见流量形态下整体失真的缺陷是合理的。

#### 2. [P1·confirmed] UserEvent.User 携带 m.users 内的活指针，订阅者在锁外读字段与 mutateUser 写构成系统性数据竞态

- **位置**：`pkg/proxy/usermanager/usermanager.go:380` · 维度：并发 · 单元：UM
- **问题**：mutateUser（L380 evt.User = user）、AddUser、GetBindPort、SyncUpsertUser、RotateUserPortForInbound 发出的事件都直接携带受 m.mu 保护的 *contracts.User 活指针。容器订阅者在自己的 goroutine 中锁外消费：如 xray exec_runner.go L948/L960 syncUserToInbound(event.Username, event.User) 直接读 AuthToken/ExpiryTime 等字段，与此同时任何 mutateUser（SetUserRole/UpdateUserPassword/SyncUpsertUser 更新分支）都在持 m.mu 写同一结构体的字段——m.mu 对订阅者不可见，构成 go race detector 可检出的数据竞态。除撕裂读外还有语义问题：事件入队到被消费之间用户可能已被后续 mutation 改写，订阅者看到的是『未来状态』而非事件发生时的状态（例如 PortBind 事件消费时 user 已被 MarkDeleting）。第一轮只在 risks 里提到 GetUser 返回指针，未覆盖事件通道这条更隐蔽、跨 goroutine 必然并发的路径。修复应在 emit 前做浅拷贝快照。
- **证据**：usermanager.go L380/L534/L962/L1117/L1584/L1635 全部传活指针；xray/exec_runner.go L948-960 在事件处理 goroutine 中解引用 event.User；contracts.User 字段（Role/LoginPassword/ExpiryTime 等）在 mutateUser 闭包内被写（L361）
- **核实**：数据竞态真实存在且系统性：mutateUser L380 evt.User = user 携带 m.users 内活指针，AddUser L535、GetBindPort L962、RotateUserPortForInbound L1117、SyncUpsertUser L1584/L1635 同样传活指针；xray exec_runner.go L163-164 Subscribe 后经 forwardUserEvents 转发到独立 goroutine（L923-930 startUserEventHandler），handleUserEvent L948/L960 调 syncUserToInbound 把指针传给 inbound.AddUser 解引用字段，L957 IsUserVisible(event.User) 直接读 deleting/expiry 状态——全程无 m.mu 保护；与此同时任何后续 mutateUser 闭包（L361）在持 m.mu 下写同一 *contracts.User 的 string/time.Time 字段。事件入队（缓冲 100）到消费之间的窗口使读写并发几乎必然，go race detector 可检出，string 撕裂读属未定义行为。『订阅者看到未来状态』的语义问题同样成立。P1 恰当，修复方向（emit 前浅拷贝）正确。

#### 3. [P1·confirmed] challenge.type 配置为 "dns" 别名时被 manager 接受填充 DNS 配置，但 issuer 只认 "dns01"，实际静默降级为 HTTP-01 并监听 :80

- **位置**：`pkg/certmgmt/service/manager.go:188` · 维度：正确性 · 单元：CERT
- **问题**：buildIssueRequest 的 switch `case string(domain.ChallengeDNS01), "dns":` 显式支持 "dns" 别名并填充 req.Challenge.DNS，但 req.Challenge.Type 被原样赋值为 ChallengeType("dns")（manager.go:180）。LegoIssuer.Issue/Renew 的分支判断是 `req.Challenge.Type == domain.ChallengeDNS01`（issuer.go:80/154），"dns" 不等于 "dns01"，于是走 else 分支 setupHTTPChallenge；此时 req.Challenge.HTTP 为 nil，setupHTTPChallenge 回退默认 {Mode:"server", ListenAddr:":80"}。后果：用户配置 dns 挑战（常见于 wildcard 域名或 80 端口不通的机器）时，凭证被完全忽略、进程尝试抢占 :80 起 HTTP-01，wildcard 域名签发必失败且报错信息与配置无关。同理任何大小写/拼写变体（DNS01、dns-01）也静默走 HTTP-01，validateIssueRequest 只检查 Type 非空不校验合法值。修法：在 buildIssueRequest 中归一化 Type 为 domain.ChallengeDNS01，并让 validateIssueRequest 拒绝未知类型；该映射路径无任何测试覆盖。
- **证据**：manager.go:180 `Type: domain.ChallengeType(m.cfg.Challenge.Type)`；manager.go:188 `case string(domain.ChallengeDNS01), "dns":`；issuer.go:80 `if req.Challenge.Type == domain.ChallengeDNS01 && req.Challenge.DNS != nil`；issuer.go:275-276 HTTP 回退默认 `{Mode: "server", ListenAddr: ":80"}`。
- **核实**：全链路仓库内直证：manager.go:180 原样赋 ChallengeType("dns")，:188 case 显式接受 "dns" 别名并填充 DNS 配置；domain/types.go:15 常量为 "dns01"；issuer.go:80/154 严格相等比较不匹配 → else 分支 setupHTTPChallenge，此时 req.Challenge.HTTP 为 nil，issuer.go:275-276 回退 {Mode:"server", ListenAddr:":80"}；validateIssueRequest（issuer.go:267）只查非空。且实际比 finding 更严重：pkg/proxy/appconfig/loader.go:83 的 legacy 配置迁移路径硬编码 Challenge.Type="dns" 并填入旧配置的 DNS provider/credentials——所有从旧配置迁移的 DNS 用户都必然踩中该失配，凭证被静默忽略、进程抢占 :80 走 HTTP-01。grep 确认 certmgmt 测试无 "dns" 别名覆盖。P1 恰当。

#### 4. [P2·confirmed] WaitN 超时返回 ctx.Err() 时已部分消费的 token 不退还：UDP 饱和时被丢弃的数据包仍烧预算，并窃取同用户 TCP 流的共享桶配额

- **位置**：`pkg/proxy/forward/ratelimit.go:260` · 维度：正确性 · 单元：FWD
- **问题**：WaitN 循环内 reserveTokens 每轮实际扣减 got 个 token（remaining -= got），若在凑齐 n 之前 ctx（UDP 路径为 10ms）先到期，直接 return ctx.Err()，已扣除的 (n-remaining) 个 token 不退还，而调用方 forwardUpstream/downstreamLoop 会把整个数据包丢弃（relay_udp.go:299-303, 361-367）。在带宽饱和场景下，每个被丢的 UDP 包都白白消耗了部分预算，形成"越丢越限、越限越丢"的正反馈：实际转发吞吐显著低于配置速率；且因为桶与同用户 TCP LimitReader 共享，UDP 丢包烧掉的 token 同时压低 TCP 吞吐。应在 ctx 到期分支把已消费的 token 加回桶中。
- **证据**：ratelimit.go:247-252 `got, wait := l.reserveTokens(remaining); if got > 0 { remaining -= got ... }`，:258-261 `case <-ctx.Done(): timer.Stop(); return ctx.Err()` 无退还；relay.go:32-35 明确要求同一桶覆盖 TCP 流与 UDP 包。
- **核实**：代码证实：ratelimit.go:242-263 WaitN 循环内 reserveTokens 每轮真实扣减（:249 remaining -= got，:303 l.tokens -= allowed），ctx.Done 分支（:259-261）直接 return ctx.Err()，已扣的 n-remaining 个 token 无任何退还。UDP 调用方证实：relay_udp.go:16 udpLimiterWait=10ms，forwardUpstream :297-304 与 downstreamLoop :361-367 在 WaitN 返回错误时直接丢包（return/continue）。桶与同用户 TCP LimitReader 共享（relay.go Limiter 契约明确要求同一桶覆盖 TCP 流与 UDP 包，forward_manager.go 按用户共享实例）。饱和时每个被丢的包仍消耗部分预算并挤占 TCP 配额，属实。影响程度依负载而定（10ms 内补充量通常足以覆盖单包，仅重度饱和/多消费者竞争时浪费显著），P2 恰当。

#### 5. [P2·confirmed] ListUsers/GetUser/UserEvent.User 暴露共享 *contracts.User，PortMappings 并发 map 读写可致进程 fatal

- **位置**：`pkg/proxy/usermanager/usermanager.go:717` · 维度：并发 · 单元：UM
- **问题**：所有查询接口（ListUsers:717、GetUser:614、GetUserForSync:1663）和事件载荷（mutateUser:380 evt.User=user、GetBindPort:962、SyncUpsertUser:1584/1636）都直接把 m.users 中的活指针交给调用方/订阅者，而 User 含 map 字段 PortMappings。容器侧在自己的 goroutine 无锁读该 map（如 xray exec_runner.go:1397 `if port, ok := user.PortMappings[in.port]; ok`），与 GetBindPort:949 `user.PortMappings[req.TargetPort]=...`、RotateUserPortForInbound:1106、ReleaseBindPort:1347 的持 m.mu 写并发时，是 Go 运行时直接 throw 的 concurrent map read/write（不可 recover）。BindPorts 切片和标量字段同样存在 data race。事件应携带深拷贝快照，或查询接口返回拷贝。
- **证据**：usermanager.go:717 `users = append(users, user)` 返回 map 内指针；:949 持 m.mu 写 `user.PortMappings[req.TargetPort] = createdRule.ListenPort`；containers/xray/exec_runner.go:1397 在无锁 goroutine 中 `port, ok := user.PortMappings[in.port]`。
- **核实**：指针暴露属实：ListUsers:717/GetUser:614/GetUserIncludingDeleting 等均返回 m.users 活指针，GetBindPort:949、RotateUserPortForInbound:1106、ReleaseBindPort:1345-1347 持 m.mu 写 PortMappings/BindPorts。但『进程 fatal 的并发 map 读写』所依赖的唯一无锁 map 读点 exec_runner.go:1397 位于 XrayInbound.ListUsers，全仓 grep 无任何生产调用方（仅测试可达）；容器 AddUser/事件路径读端口均经持锁访问器（GetUserPortByDst/GetUserPortByDstForCleanup）。当下真实存在的是非 fatal 的 data race：end_node_user.go GetUserProfile 无锁读 TrafficTotalUplink/Downlink（写端在 m.mu 下）、userToProtoUser 无锁读 ExpiryTime(多字撕裂)、mutateUser:378 evt.Ports 直接别名 user.BindPorts 而 GetBindPort:945 对同切片 append。缺陷确凿且 XrayInbound.ListUsers 是任何新调用方即引爆的地雷，但 fatal 触发路径今天不可达，P1 依据不成立，降为 P2。

#### 6. [P2·confirmed] DrainStats 先 reset 全部 delta 再按 ListUsers 过滤，tombstone/过期/组不可见用户的流量被清零丢弃

- **位置**：`pkg/proxy/usermanager/bandwidth_stats_collector.go:35` · 维度：正确性 · 单元：UM
- **问题**：DrainStats 调 GetAllDeltaTraffic(true) 排空所有用户的 delta（包括 deleting/expired/组不可见用户的条目），但组装结果时只保留 ListUsers()（过滤 IsDeleting/IsExpired/组可见性）里的用户：deltaMap 中不属于活跃可见用户的条目被直接丢弃。用户在两次 scrape 之间被删除或过期时，其最后一段流量已被 reset 却永远不会上报给 center/Prometheus；集群模式下组不可见但仍有存量 relay 流量的用户则持续漏报。与第一轮的『读取与重置窗口丢失』不同，这是过滤逻辑本身的确定性丢失。应以 delta 条目为准输出（或对不在 users 列表的条目也生成 Stats），而不是以当前活跃用户列表为准。
- **证据**：bandwidth_stats_collector.go:29 `users := c.mgr.ListUsers()`（活跃+可见过滤），:35 `deltas := c.mgr.GetAllDeltaTraffic(true)`（无条件重置全部），:44-74 仅遍历 users 输出，deltaMap 中其余 username 的条目被丢弃。
- **核实**：逐行核实成立：bandwidth_stats_collector.go:29 ListUsers 过滤 IsDeleting/IsExpired/组可见性，:35 GetAllDeltaTraffic(true) 经 usermanager.go:2233-2246 遍历 statsCollector 全部 ByUser 条目后 resetAllDeltas 无条件清零，:44-74 仅按 users 列表输出。statsCollector.collect()（:1863-1918）按 forward 快照累积 ByUser，用户删除/过期/组变更不会清理其条目（drainForUserLocked 仅被 ResetUserTotalTraffic 调用），故 tombstone/过期/不可见用户在最后一个采集窗口的 delta 会被 reset 后丢弃，永不上报。属确定性数据丢失（计费/监控口径），P2 恰当。唯一缓解是 len(users)==0 时提前 return 不 reset，不改变主场景结论。

#### 7. [P2·confirmed] RotateUserPortForInbound 失败路径：旧/临时 relay 均已删除后 final AddRule 失败直接返回，用户断流且状态无回滚

- **位置**：`pkg/proxy/usermanager/usermanager.go:1083` · 维度：错误处理 · 单元：UM
- **问题**：Step 3 已 RemoveRule 旧 relay，Step 4 先 RemoveRule 临时 relay 再重建正式规则；若重建失败且 auto-allocate 重试（:1080-1083）也失败（如端口耗尽、forward 层内部错误），函数直接返回错误：此时该 inbound 的旧 relay、临时 relay 均已销毁，用户彻底断流，而 user.BindPorts/PortMappings 仍指向已死的旧端口并被持久化状态引用，订阅链接继续下发死端口。也没有任何补偿逻辑（如用 oldListenPort 重建旧规则）。另外 Step 4 的『删临时→建正式』之间本身存在无 relay 窗口，注释宣称的 make-before-break 在该窗口并不成立。第一轮 :1013 只覆盖了不持 m.mu 的并发交错，未覆盖此失败路径的资源/状态残留。
- **证据**：usermanager.go:1052 删旧规则；:1060 删临时规则；:1076-1084 两次 AddRule 失败即 `return 0, errors.Wrap(...)`，无任何恢复旧 relay 或修正 BindPorts/PortMappings 的代码。
- **核实**：失败路径逐行核实：:1052 RemoveRule 删旧 relay，:1060 RemoveRule 删临时 relay，:1076 首次 AddRule 失败后 :1080-1083 auto-allocate 重试仍失败即 `return 0, errors.Wrap(..."failed to finalize forward rule"...)`——此时旧/临时 relay 均已销毁，无任何用 oldListenPort 重建旧规则的补偿代码；user.BindPorts/PortMappings 要到 :1088 之后（Step 5）才更新，失败路径完全不触及，持久化状态与订阅继续指向已死的 oldListenPort。『删临时→建正式』窗口无 relay 且端口可被外部进程抢占（:1078 注释自认）也属实。触发需临时 AddRule 成功后两次连续失败，概率低但 forward 层内部错误/端口耗尽可达，P2 恰当。

#### 8. [P2·confirmed] RotateUserPortForInbound Step-4 双重失败后旧/临时 relay 均已删除，该 inbound 完全断流且无回滚

- **位置**：`pkg/proxy/usermanager/usermanager.go:1083` · 维度：错误处理 · 单元：UM
- **问题**：make-before-break 的承诺只覆盖到 Step 3：Step 3（L1052）已删旧规则，Step 4 先删临时规则（L1060）再以正式 key 重建；若重建失败（L1076），回退自动分配也失败（L1081-1084）则直接 return error。此时旧 relay、临时 relay、正式 relay 三者都不存在，用户在该 inbound 上彻底断流——比不轮换更糟。且 user.BindPorts/PortMappings 仍记录 oldListenPort（Step 5 未执行），订阅链接继续发布一个无监听者的死端口，也不会触发任何自愈（容器不会因为轮换失败而重新调用 GetBindPort）。函数掌握重建旧规则所需的全部快照（existingRule），失败时至少应尝试用 oldListenPort 恢复旧规则，或在错误路径清理 BindPorts/PortMappings 并 emit 事件让容器重建。
- **证据**：L1052 RemoveRule(ruleKey) 删旧；L1060 RemoveRule(tmpRuleKey) 删临时；L1081-1084 二次 AddRule 失败直接返回，无任何用 existingRule/oldListenPort 恢复的代码；rotate_inbound_port_test.go 只测 UserNotFound/NoRule/成功路径，无 Step-4 失败用例
- **核实**：亲读 :1013-1102 证实：Step 3(:1052) RemoveRule 删旧 relay，Step 4(:1060) 先删临时 relay，正式 AddRule 失败(:1076-1077)后回退 ListenPort=0 再 AddRule(:1081)，二次失败直接 return error(:1083)——此刻旧/临时/正式三条规则均不存在，该 inbound 完全断流；Step 5 未执行，BindPorts/PortMappings 仍指向 oldListenPort，订阅继续发布死端口。函数确实持有重建所需全部快照（existingRule 含 oldListenPort/TargetAddr/限速等）却无任何恢复代码。更糟的是事后无法用同一 API 自愈：再调 RotateUserPortForInbound 会在 :1016 因 GetRule 为 nil 报 no forward rule found。rotate_inbound_port_test.go 的 9 个用例均无 AddRule 失败注入。虽需两次连续 AddRule 失败才触发（概率低），但错误路径留下断流+脏状态且无自愈通道，P2 可以维持。

#### 9. [P2·confirmed] 全部 VMess/VLESS Build* 变体验证 uuid 后彻底丢弃，产出的 User 无 Account，inbound 无任何认证凭据

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:45` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：BuildVMess/BuildVMessWS/BuildVMessTCPWithTLS/BuildVLESSReality/BuildVLESSWSTLS 均先检查 uuid!=""，随后构造 protocolpb.User{Email, Level} 却从不设置 user.Account（不构造 vmess.Account/vless.Account TypedMessage），uuid 参数在函数体内再未被引用。对比同文件 BuildTrojanTCPWithTLS(L204-225) 正确构造了 trojan.Account 并塞入 user.Account，可见 vmess/vless 路径是遗漏而非设计。另外 vlessinboundpb.Config 的 Decryption 字段未设置（xray 要求显式 "none"）。这些 builder 产出的 InboundHandlerConfig 一旦真正下发，xray 端要么构建 handler 失败，要么产生无凭据用户。该问题独立于第一轮已报的 buildReceiverSettings 丢 streamSettings（L299）——即使修好 stream 层，proxy 层账号仍是空的。
- **证据**：L39-52: `if uuid == "" { return nil, ... }` 之后 `user := &protocolpb.User{Email: email, Level: 0}`，uuid 无任何后续使用；vlessConfig/vmessConfig 仅含该无 Account 的 user。
- **核实**：逐一读过 builder.go 五个变体（L39-52/70-82/101-113/132-145/164-176）：uuid 仅做空检查后再无引用，protocolpb.User 只设 Email/Level，从不构造 vmess/vless Account TypedMessage；对照同文件 BuildTrojanTCPWithTLS L204-225 正确构造 trojan.Account，遗漏成立。仓库内上游 proto 镜像 internalproto/upstream/common/protocol/user.proto 注释明确 account 'Must be the account proto in one of the proxies'，佐证无 Account 即违反契约；vless Decryption 未设也与 types.go:456-461（缺省显式补 'none' 并注释）自证矛盾，上游行为断言有仓库材料支撑。注意：grpc/builder 包全仓库无生产 import（仅 builder_test.go），当前为潜伏缺陷；但第一轮 item 42 在同一死代码前提下维持 P2，为保持一致不做降级。

#### 10. [P2·confirmed] buildReceiverSettings 把 listen 字符串的 ASCII 字节直接塞进 IPOrDomain_Ip，产出非法 IP 字节

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:294` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：`Ip: []byte(listen)` 将 "192.168.1.10" 这类点分字符串按 ASCII 编码（12 字节）写入 IPOrDomain.Ip，而 xray 的 IPOrDomain.Ip 期望 4/16 字节的原始 IP。xray 端 AsAddress() 对非法长度返回 nil，监听地址意图丢失（回落 AnyIP 或产生 nil 地址错误）。同包 types.buildReceiverConfigProto(L258) 用 stdnet.ParseIP 正确解析并区分 IP/域名，builder 路径应复用同一逻辑；且 builder 路径对域名型 listen 也会错误地走 Ip 分支而非 Domain 分支。
- **证据**：L291-297: `if listen != "" && listen != "0.0.0.0" && listen != "::" { receiverConfig.Listen = &protocol.IPOrDomain{Address: &protocol.IPOrDomain_Ip{Ip: []byte(listen)}} }`，无 ParseIP、无 Domain 分支。
- **核实**：builder.go:291-297 确为 `Ip: []byte(listen)`，无 ParseIP、无 Domain 分支；"192.168.1.10" 的 ASCII 编码是 12 字节。仓库内上游镜像 internalproto/upstream/common/net/address.proto 注释直接确证 'IP address. Must by either 4 or 16 bytes.'，同包 types.go:257-273 用 stdnet.ParseIP 并区分 IP/Domain 的正确实现构成对照，上游断言有仓库材料支撑。需注意两点：其一该包无任何生产调用方（死代码，同 idx 0）；其二此点与第一轮已 confirmed 的 item 42（builder.go:299）问题文本中'另外监听地址处理错误：Ip: []byte(listen)'部分重复，本条并非全新发现，修复时应与 item 42 合并处理。

#### 11. [P2·confirmed] buildVMessUser 对缺失 id 的 client 静默回落为全零 UUID 账号，形成可被公知凭据登录的用户

- **位置**：`pkg/xrayapi/types/types.go:360` · 维度：安全 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：clientMap 中没有 "id"（或类型不是 string）时，代码不是跳过/报错，而是构造 Id="00000000-0000-0000-0000-000000000000" 的 vmess.Account 并加入 inbound 用户列表。全零 UUID 是公开常量，任何客户端都能用它通过 vmess 认证——一个字段名拼错（如 "uuid" 而非 "id"）的配置就会静默产出一个对全网开放的账号。对比 buildVLessInboundConfig(L404) 缺 id 时至少不构造账号，此处行为最危险。应改为返回错误或跳过该 client。
- **证据**：L354-362: `if id, ok := clientMap["id"].(string); ok { account = &vmess.Account{Id: id} } else { account = &vmess.Account{Id: "00000000-0000-0000-0000-000000000000"} }`，随后无条件 append 到 config.User。
- **核实**：types.go:354-362 确认：clientMap 无 "id"（或非 string）时静默构造 Id 为全零 UUID 的 vmess.Account 并经 L377-380 包进 TypedMessage、L329-331 无条件 append 进 config.User；对照 buildVLessInboundConfig L404-418 缺 id 时不构造账号，行为差异成立。触发路径为活路径：ParseInboundConfig(types.go:206)→buildProxySettingsProto→buildVMessInboundConfig，生产调用在 pkg/proxy/containers/xray/grpc_client.go:134 AddInbound，非死代码。上游断言有仓库材料支撑：upstream/proxy/vmess/account.proto 注释明确 Id 即 'ID of the account, in the form of a UUID'（即认证凭据），user.proto 明确 account 为协议凭据载体；全零 UUID 为格式合法的公知常量，构成公知凭据。触发需上层送入缺 id 的 vmess client 配置（如字段拼错），非默认路径，但作为静默安全回退 P2 定级可接受。

#### 12. [P2·confirmed] SwitchForwardRules/RollbackUpdate 每个端口映射只切换第一条匹配规则，同目标端口的其余转发规则在旧实例停止后全部黑洞

- **位置**：`pkg/xrayapi/hotreload/manager.go:331` · 维度：正确性 · 单元：XAPI
- **问题**：SwitchForwardRules 找到第一条 TargetAddr=="127.0.0.1:<oldPort>" 的规则即 break（L331），但 ForwardRule 的 RuleKey 是 container:tag:username，同一个 inbound 端口完全可以有多条规则（多个用户/多个监听端口指向同一 target）。未被切换的规则仍指向旧端口，Step 7 oldExecutor.Stop() 后这些用户的转发直接断连，且流程返回 Success=true。RollbackUpdate(L652 break) 和 hotupdate.switchForwardRules(L312 break) 存在同样的首条匹配缺陷。应收集全部匹配规则批量切换。
- **证据**：L324-334: `for _, rule := range rules { if ... == rule.TargetAddr { targetRule = rule; break } }` 单规则变量+break；forward/types.go L125 RuleKey="container:tag:username" 证明同 target 多规则合法。
- **核实**：三处首条匹配缺陷均核实：manager.go SwitchForwardRules L324-334 单规则变量 targetRule + break（L331），每个 mapping 只切一条；RollbackUpdate L649-664 同样命中即 break（实际 break 在 L662，finding 引 L652 为匹配行，无碍）；hotupdate/update.go L299-315 结构相同（break 在 L312）。多规则同 target 的合法性有双重仓库证据：pkg/proxy/forward/types.go L123-127 RuleKey="container_type:inbound_tag:username" 且注释明示 "allows multiple forward rules per user"；生产路径 usermanager.go:914-926 为每个用户各建一条 TargetAddr=fmt.Sprintf("127.0.0.1:%d", req.TargetPort) 的规则——同一 inbound 多用户必然产生多条同 TargetAddr 规则。切换后未处理的规则仍指旧端口，ExecuteHotReload L443/ExecuteHotUpdate L550 停旧实例后这些用户流量断连且 Success=true。protocolRelated=false，证据全在仓库内。P2 合理（多用户静默断连，与第一轮同文件 #40 的 P2 定级口径一致，尽管当前仅 dev 工具可达）。

#### 13. [P2·confirmed] SwitchForwardRules 对同一 target 端口的多条转发规则只切换第一条，其余用户在旧实例停止后流量全部黑洞

- **位置**：`pkg/xrayapi/hotreload/manager.go:331` · 维度：正确性 · 单元：XAPI
- **问题**：RuleKey 为 container_type:inbound_tag:username（pkg/proxy/forward/types.go:125），因此同一个 inbound 端口会有 N 个用户 N 条规则，TargetAddr 全部是 127.0.0.1:<oldPort>。SwitchForwardRules 对每个 mapping 找到第一条匹配规则后立即 break（line 330-331），只迁移这一条；随后 ExecuteHotReload/ExecuteHotUpdate 停掉旧实例，剩余 N-1 个用户的 relay 仍指向已死端口，连接全部失败且无任何报错。同样的 break-after-first 缺陷存在于 RollbackUpdate（manager.go:662）、hotupdate.switchForwardRules（update.go:312）和 hotupdate.Rollback（update.go:183）。且由于 map 迭代无序，哪个用户被迁移是随机的。
- **证据**：manager.go:329-331 `if fmt.Sprintf("127.0.0.1:%d", mapping.OldPort) == rule.TargetAddr { targetRule = rule; break }` —— 每个 mapping 只取一条；RuleKey() 不含 target，多个用户共享同一 TargetAddr 是常态
- **核实**：manager.go:325-334 确认每个 mapping 找到首条 TargetAddr 匹配的规则即 break，只迁移一条。RuleKey 为 container:inbound:user（forward/types.go:125-127），usermanager.go:921 确认每用户规则的 TargetAddr 均为 127.0.0.1:<inbound端口>，同一 inbound 的 N 个用户共享同一 TargetAddr 是常态路径。ExecuteHotReload/ExecuteHotUpdate 随后 oldExecutor.Stop()（manager.go:443/550），剩余 N-1 条规则的 relay 指向已停实例端口，连接失败且 SwitchForwardRules 返回 nil 无任何报错。RollbackUpdate（manager.go:662）、hotupdate.switchForwardRules（update.go:312）、hotupdate.Rollback（update.go:183）均有相同的 break-after-first。GetAllRules 遍历 map 无序，被迁移的用户确实随机。唯一缓解因素是该包目前仅被带 build ignore 标签的 cmd/test_hotreload 引用，未接入生产链路，P2 恰当。

#### 14. [P2·confirmed] ExecuteHotReload 成功后不持久化任何状态：OldConfigPath/currentConfig 不更新，二次 reload 丢失上次新增 inbound 且与仍在运行的新实例端口冲突

- **位置**：`pkg/xrayapi/hotreload/manager.go:443` · 维度：正确性 · 单元：XAPI
- **问题**：Step 7 停掉旧实例后直接返回 Success，NewConfigPath 从未被提升为 OldConfigPath，m.currentConfig 也从未写入（全包无任何对 currentConfig 的赋值，mu 写锁从未被使用）。此时实际运行的实例监听的是 old+PortOffset 端口并包含新增 inbound，但磁盘上的 OldConfigPath 还是旧内容。下一次 ExecuteHotReload 会：1) 从过期的 OldConfigPath 读 inbound，上一轮通过 newInbounds 加入的 inbound 全部丢失；2) 对旧端口再次加同一个 PortOffset，得到与当前运行实例完全相同的端口集（含 62789+offset 的 gRPC 地址），新实例 bind 必然失败。整个流程只能安全执行一次，不可重入。
- **证据**：manager.go:442-447 只有 oldExecutor.Stop() 与填充 result；grep 全包无 `m.currentConfig =` 赋值、无 `mu.Lock()`；CreateNewExecutor(261) 固定使用 62789+PortOffset，二次调用与首轮新实例地址相同
- **核实**：grep 全包核实：m.currentConfig 除 manager.go:84 声明与 114/118 读取外无任何赋值；mu.Lock() 从未被调用（写锁全包零使用）；config.OldConfigPath 从未被更新。ExecuteHotReload Step 7（manager.go:442-447）停旧实例后直接返回。GetCurrentInbounds 因 currentConfig 恒为 nil 永远从过期的 OldConfigPath 读取，上一轮 newInbounds 加入的 inbound 在二次 reload 时丢失；CreateNewExecutor（261/272）固定 62789+PortOffset，二次调用的新实例 gRPC/业务端口与首轮仍在运行的新实例完全相同，bind 必然失败。流程不可重入属实。

#### 15. [P2·confirmed] Manager.ExecuteHotUpdate 的 prepareForUpdate 不应用 PortOffset，新实例与仍在运行的旧实例监听端口完全相同，更新必然失败

- **位置**：`pkg/xrayapi/hotreload/manager.go:569` · 维度：正确性 · 单元：XAPI
- **问题**：prepareForUpdate 用 GetCurrentInbounds() 的原始端口直接 buildConfig（不调 applyPortOffset），且 buildConfig 又注入固定 62789 的 api inbound；而旧实例此时仍在运行并占用同一组端口。新实例 Start 后 bind 全部失败，xray 启动即退出，health-check 阶段必失败——ExecuteHotUpdate 在当前实现下永远无法成功。同时 GetPortMapping 比较新旧两份端口相同的文件，产出 OldPort==NewPort 的映射，即使侥幸走到 switch-forward 也是无效切换。这与 pass1 已报的 update.go:95（hotupdate 包）和 manager.go:210（api 端口）是三处独立缺陷：本条是 Manager.ExecuteHotUpdate 路径上业务端口不偏移的问题。
- **证据**：manager.go:568-569 `// Build config with current inbounds (same ports)` / `newCfg := m.buildConfig(currentInbounds)` —— 与 PrepareNewConfig(154-159) 不同，这里没有 applyPortOffset 调用；createUpdateExecutor(593) 却按 62789+PortOffset 配置 gRPC 地址，与配置文件内 62789 的 api inbound 自相矛盾
- **核实**：manager.go:561-588 prepareForUpdate 直接 m.buildConfig(currentInbounds)，注释明写 'same ports'，无任何 applyPortOffset 调用（对比 PrepareNewConfig:154-159 有偏移）；buildConfig:207-214 还注入固定 62789 的 api inbound。ExecuteHotUpdate 的 stop-old 在 Stage 6，Stage 2 启动新实例时旧实例仍占用同一组端口（含 62789），新实例 bind 失败即退出，HealthCheck 的 IsRunning 检查必失败。createUpdateExecutor:593 却按 62789+PortOffset 连 gRPC，与配置内 62789 自相矛盾。GetPortMapping 对两份端口相同的文件产出 OldPort==NewPort 的无效映射也属实。与 pass1 报的 hotupdate 包 update.go 问题是不同函数的独立缺陷。

#### 16. [P2·confirmed] SwitchForwardRules 的 RemoveRule→AddRule 窗口内监听端口被释放回 allocator，可被并发 AddRule 随机分配抢占；且直接操作 ForwardManager 绕过 usermanager 端口账本（约束#4）

- **位置**：`pkg/xrayapi/hotreload/manager.go:342` · 维度：并发 · 单元：XAPI
- **问题**：DefaultForwardManager.RemoveRule 会 `m.allocator.Release(mr.rule.ListenPort)`（forward_manager.go:325），随后本函数再以相同 ListenPort 调 AddRule 走 AllocateSpecific 重新占用。两次调用之间端口在 allocator 空闲池中，任何并发的用户建流程（AddRule ListenPort=0 随机 Allocate）都可能抢到该端口，导致后续 AllocateSpecific 失败——这正是触发 pass1 已报的"AddRule 失败无回滚"的现实路径，用户转发被永久拆除。另外架构约束#4 要求转发规则创建/释放走 usermanager 的 GetBindPort/ReleaseBindPort，此处（及 RollbackUpdate:654-660、hotupdate 对应函数）直接 RemoveRule/AddRule，usermanager 的绑定记录与 forward 层实际状态可能漂移。
- **证据**：manager.go:342 `fm.RemoveRule(targetRule.RuleKey())` → :349 `fm.AddRule(newRule)`；forward_manager.go RemoveRule 中 `m.allocator.Release(mr.rule.ListenPort)`，AddRule 中 `AllocateSpecific(rule.ListenPort)` 失败即返回错误
- **核实**：forward_manager.go:327 RemoveRule 中 m.allocator.Release(mr.rule.ListenPort) 将端口放回池中；SwitchForwardRules（manager.go:342-351）随后以保留了原 ListenPort 的副本调 AddRule，走 AllocateSpecific（forward_manager.go:148-151），已被占用即整体返回错误且被移除的规则无回滚。port_allocator.go:64-107 确认并发 Allocate()（random 或 round-robin）在窗口期可选中刚释放的端口，ForwardManager 是全局单例，用户建流程与热切换并发是现实场景。PROJECT_GUIDE.md:70 约束#4 原文'forward 规则创建/释放必须走 usermanager(GetBindPort/ReleaseBindPort)'属实，且 usermanager 的 user.PortMappings 以 TargetPort 为 key，热切换改 TargetAddr 后映射漂移。RollbackUpdate:654-660 与 hotupdate 对应函数同样直接 Remove/Add。

#### 17. [P2·confirmed] GetCertInfo 返回 typed-nil interface，调用方 `cert == nil` 判断永远为 false，缺证书时永不触发自动签发

- **位置**：`pkg/certmgmt/service/rpc_adapter.go:74` · 维度：正确性 · 单元：CERT
- **问题**：GetCertInfo 声明返回 interface{}，实现为 `return m.GetCert(d)`，而 GetCert 返回 *domain.CertificateRecord（未找到时为 nil 指针）。nil 指针装箱进 interface{} 后接口值非 nil。唯一的生产调用方 pkg/rpc/server/end_node_inbound.go:233 写的是 `if cert := s.certManager.GetCertInfo(domain); cert == nil { ObtainNewCert(...) }`——对真实 Manager 该条件恒为 false，导致 FastAddInbound 带 domain+tls 时即使本地没有证书也跳过签发，inbound 直接用不存在的证书路径启动，TLS 必然失败。注释还明确写着 'Callers only need a nil check'，误导调用方。修法：返回具体类型 *domain.CertificateRecord，或在适配层做 `if r := m.GetCert(d); r != nil { return r }; return nil`。
- **证据**：rpc_adapter.go:73-75 `func (m *Manager) GetCertInfo(d string) interface{} { return m.GetCert(d) }`；GetCert 返回 *domain.CertificateRecord；end_node_inbound.go:233 `if cert := s.certManager.GetCertInfo(domain); cert == nil`。Go 语义下 (*T)(nil) 装箱后 interface != nil。
- **核实**：typed-nil 装箱缺陷本身确凿：rpc_adapter.go:73-75 返回 interface{} 包裹 *domain.CertificateRecord 的 nil，pkg/rpc/server/cert_manager.go:14 接口声明 interface{}，end_node_inbound.go:233 的 `cert == nil` 对生产 Manager（cmd/server.go:161/171 接线证实）恒为 false，自动签发（ObtainNewCert）永不触发，属死路径。但 detail 声称的后果不成立：证书缺失时下游 params.resolveCertSource（core/params/defaults.go:211-213 对 domain 无证书返回 'no certificate found for domain ...; issue one before adding the inbound'）和 xray resolveFastAddCert（exec_runner.go:2036-2038 返回 ErrCertNotFound）均 fail-closed，FastAddInbound 以 1020/1021 错误返回，不会出现『inbound 用不存在的证书路径启动、TLS 必然失败』。实际影响是自动签发特性完全失效、请求带清晰报错失败（可手动 ObtainNewCert 后重试），非静默上线坏 inbound，故降为 P2。

#### 18. [P2·confirmed] AddCertificates 不校验私钥合法性及 cert/key 配对，坏配对就地覆盖同域正常证书，代理热加载后 TLS 中断

- **位置**：`pkg/certmgmt/service/rpc_adapter.go:42` · 维度：正确性 · 单元：CERT
- **问题**：AddCertificates 只对 certData 调 ParseCertNotAfter，keyData 完全不解析：不检查是否为合法 PEM 私钥，更不检查与证书公钥是否匹配（tls.X509KeyPair 一行即可校验）。由于导入写入与 ACME 证书相同的 canonical 路径（{Path}/certificates/{domain}.crt/.key）且就地覆盖，若管理端通过 TransferCert RPC（pkg/rpc/server/end_node_cert.go:21）传错 key（例如拿错域的 key、或 cert/key 参数顺序颠倒），会把本来工作正常的证书文件对覆盖成不匹配对；xray/mihomo/hysteria 依赖文件热加载，下一次 reload 即开始 TLS 握手失败，且 RPC 返回成功、无任何告警。修法：写盘前用 tls.X509KeyPair(certData, keyData) 校验配对，可选再校验 domain 是否在证书 SAN 中。
- **证据**：rpc_adapter.go:38-60：仅 `len(certData)==0||len(keyData)==0` 与 `ParseCertNotAfter(certData)` 两个检查，keyData 原样进 SaveCert；注释自述 're-import overwrites in place'。
- **核实**：仓库内直证：rpc_adapter.go:37-60 AddCertificates 仅做 len 非空检查与对 certData 的 ParseCertNotAfter，keyData 未做任何 PEM/配对校验即进 SaveCert；SaveCert（cert_store.go:44-60）写入 canonical {Path}/certificates/{domain}.crt/.key 并原子覆盖，与 ACME 证书同路径（rpc_adapter.go:30-35 注释自述 're-import overwrites in place'）。调用链真实：end_node_cert.go:19-30 TransferCert RPC 直通 AddCertificates 且错误码之外无告警。代理靠文件热加载的前提有仓库内佐证：renew_scheduler.go:69-72 注释明确 'proxy cores pick up the new cert via their own file hot-reload'。传错 key/参数颠倒即用坏配对覆盖正常证书且 RPC 返回成功，tls.X509KeyPair 一行可拦。P2 恰当，不依赖第三方行为。

#### 19. [P2·confirmed] 并发签发不同域名时共享账号私钥存在 TOCTOU 竞态，导致 keys/<email>.key 与 account.json 的注册 KID 不匹配，后续 ACME 请求 JWS 校验全部失败

- **位置**：`pkg/certmgmt/lego/account_store.go:107` · 维度：并发 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：Manager.Issue 按 domains[0] 加 per-domain 锁（manager.go:113），不同域名走不同锁可并发；而 LegoIssuer.Issue 第一步就调用共享的 GetOrCreateAccountKey(email)（issuer.go:35，在 DNS 全局锁之外）。GetOrCreateAccountKey 先 os.ReadFile(keyPath)（account_store.go:107），未命中则 GeneratePrivateKey 再 os.WriteFile（129）——读到写之间无任何互斥。首次运行、两个不同域名并发签发时：两个 goroutine 都读到 keyfile 不存在，各自生成不同私钥并写盘（后写覆盖），随后各自用自己内存里的 key 调 Registration.Register 并 SaveAccount(issuer.go:63) 写 account.json（后写覆盖）。最终磁盘上 account.json 的 Registration(KID) 可能对应 key A，而 keys/<email>.key 落的是 key B。下次 LoadAccount 读到 (regA, keyB) 组合，NewClient 用 keyB 签名却携带 accountA 的 KID → ACME 服务端 JWS 验签失败，之后所有签发/续期持续报错，需手工清账号目录才能恢复。ObtainNewCert 由 gRPC handler(end_node_inbound.go:234、end_node_cert.go:11) 并发触发，路径真实可达。
- **证据**：account_store.go:107 raw, err := os.ReadFile(keyPath) 后 120 GeneratePrivateKey / 129 os.WriteFile，无锁；issuer.go:35 在 WithDNSCredentials(80) 之外调用；manager.go:113 只锁 domains[0]，跨域并发
- **核实**：代码逐点核实成立。manager.go:113 Issue 仅按 domains[0] 取 per-domain 锁，两个不同域名的 gRPC 请求（end_node_cert.go:11 ObtainNewCert、end_node_inbound.go:234 FastAddInbound 内 ObtainNewCert，grpc-go 每 RPC 独立 goroutine）可完全并发。issuer.go:35 在任何全局锁之外调用共享的 GetOrCreateAccountKey；account_store.go:107 ReadFile → 120 GeneratePrivateKey → 129 WriteFile 之间无互斥，首次运行双读未命中即各自生成不同 key。随后两 goroutine 都因 LoadAccount 返回 nil 而各自 Register 并 SaveAccount（issuer.go:53-67），SaveAccount 先写 account.json 再写 keys/<email>.key（account_store.go:53/60），交错写入（如 A.json→B.json→B.key→A.key）会留下 account.json=账号B、keyfile=keyA 的错配。后续 Issue/Renew 用 GetOrCreateAccountKey 读 keyfile 取签名 key、用 LoadAccount 取 Registration 构造 NewClient(issuer.go:71/137)。protocolRelated 的上游行为已用仓库依赖实证：go.mod 固定 lego v4.9.0，模块源码 lego/client.go:38/44 用 user.GetPrivateKey() 签名并以 GetRegistration() 的 URI 作 KID（acme/api/internal/secure/jws.go:23-33），key 与 KID 账号不匹配即 RFC 8555 服务端 JWS 验签失败，且错配已持久化、无自愈路径。窗口仅存在于首次建账号且毫秒级并发，概率低但后果持久，P2 恰当。

#### 20. [P2·confirmed] ListCerts 对读失败/JSON 反序列化失败的 .meta.json 静默 continue，损坏的证书元数据会让该域名彻底从自动续期扫描中消失，直到过期无告警

- **位置**：`pkg/certmgmt/lego/cert_store.go:146` · 维度：错误处理 · 单元：CERT
- **问题**：ListCerts 遍历 certificates/*.meta.json，os.ReadFile 出错(146-148)或 json.Unmarshal 出错(151-152)都直接 continue，既不记日志也不返回错误。自动续期 runRenewCycle(renew_scheduler.go:54) 完全依赖 ListCerts 的结果决定续哪些域名——只要某个域名的 meta.json 被截断/写坏（例如 SaveCert 四文件非原子写中途崩溃，正是 pass1 cert_store.go:44 的场景），该域名就从续期集合里静默消失，永不再被续期，最终静默过期造成线上 TLS 中断，且运维侧毫无信号。应至少 log.Error 记录跳过的文件，或让上层能感知部分失败。
- **证据**：cert_store.go:146-153 raw,err:=os.ReadFile(...); if err!=nil {continue}; ... if json.Unmarshal(...)!=nil {continue} —— 无日志无错误上报；renew_scheduler.go:54 records := m.ListCerts() 是续期唯一数据源
- **核实**：cert_store.go:146-148 os.ReadFile 出错 continue、151-152 json.Unmarshal 出错 continue，均无日志无错误聚合（函数返回的 error 只覆盖 ReadDir 失败）。renew_scheduler.go:53-54 runRenewCycle 唯一数据源就是 m.ListCerts()，meta.json 损坏的域名从续期集合静默消失且每分钟循环都不会产生任何信号（manager.go:157-160 的 log.Error 只在 ListCerts 返回 err 时触发，continue 路径不返回 err）。SaveCert 虽单文件原子（atomicWrite），但四个文件整体非原子，写 meta 前崩溃/磁盘满可产生半损状态；即使不谈来源，任意外因损坏 meta.json 都触发本问题。GetCert/LoadCert 对同一损坏会报错（unmarshal 失败返回 err），说明续期扫描路径的静默是不一致的例外行为。最终结果是证书静默过期、TLS 中断且运维无信号，P2 恰当。

#### 21. [P2·uncertain] buildConfig 生成的 api inbound 缺 listen 字段，dokodemo API 口绑 0.0.0.0 暴露无鉴权 HandlerService

- **位置**：`pkg/xrayapi/hotreload/manager.go:210` · 维度：安全 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：buildConfig 硬编码的 api inbound 只有 tag/port/protocol/settings.address，没有 "listen":"127.0.0.1"。xray 对缺省 listen 的 inbound 绑定 0.0.0.0，即 62789 管理端口对所有网卡开放；HandlerService/StatsService/LoggerService 无任何鉴权，外网可直接 AddInbound/AddUser。第一轮 L210 的 finding 只覆盖了端口不加 PortOffset 的冲突问题，未覆盖监听面暴露。prepareForUpdate 路径（L569）复用同一 buildConfig，同样受影响。标准 xray api inbound 模板均带 "listen":"127.0.0.1"。
- **证据**：L207-214: `{"tag":"api","port":62789,"protocol":"dokodemo-door","settings":{"address":"127.0.0.1"}}`——settings.address 只是 dokodemo 的转发目标，不是监听地址；map 中无 listen 键。
- **核实**：仓库内事实确凿：hotreload/manager.go L207-214 的 api inbound map 确无 "listen" 键（settings.address 只是 dokodemo 转发目标），prepareForUpdate L569 复用同一 buildConfig；且与仓库自身生产模板 pkg/proxy/containers/xray/config_renderer.go:166-173（同款 dokodemo api inbound 显式 "listen":"127.0.0.1"）直接矛盾，偏离项目自身约定成立。但暴露结论的关键前提——"xray 对缺省 listen 的 JSON inbound 绑 0.0.0.0"——是上游行为：仓库无 xray-core 源码/vendor，wiki 与 docs 均无该缺省行为记载，config_renderer 显式设置与 types.go:257 的跳过逻辑只能旁证作者假设、不能确证上游；按 protocolRelated 规则最高 uncertain。另注意触发面收窄：hotreload 包全仓库仅 cmd/test_hotreload 开发工具引用，且第一轮 #40 已确认的 62789 硬编码冲突在部分场景会让新实例根本起不来。若上游缺省确为 0.0.0.0 则 P2 成立（无鉴权 HandlerService 暴露），建议修复时直接补 listen:127.0.0.1——成本为零且无论上游缺省如何都正确。

#### 22. [P2·uncertain] 续期失败无退避：进入续期窗口后每 1 分钟重跑一次完整 ACME 流程，1 小时内即撞 Let's Encrypt failed-validation 限流

- **位置**：`pkg/certmgmt/service/renew_scheduler.go:64` · 维度：错误处理 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：renewCheckInterval 固定 1 分钟，runRenewCycle 对每个到期域调 RenewDomain；失败仅 log.Error 后 continue，没有任何按域退避/失败计数状态。一旦某域进入续期窗口且续期持续失败（DNS 凭证过期、80 端口被占、CA 故障等），每分钟都会发起一次完整的 ACME order+challenge。Let's Encrypt 的 failed validation 限额是每账号每 hostname 每小时 5 次，5 分钟内就会耗尽，之后每次尝试都被 429 拒绝，错误日志刷屏且真正的故障原因被限流错误淹没；恢复后还要等限流窗口过去。与 pass1 的『默认 lead 24h 太短』是不同问题——那是窗口长度，这是窗口内的重试频率。修法：对失败域记录 nextAttempt 时间做指数退避（如 1m→5m→30m→1h），或至少把失败域的重试间隔与扫描间隔解耦。
- **证据**：renew_scheduler.go:14 `const renewCheckInterval = time.Minute`；renew_scheduler.go:64-68 失败仅 `log.Error(...)` 后 `continue`，Manager 无任何失败退避状态字段（manager.go:71-76 只有 cfg/issuer/domainMu）。
- **核实**：代码事实全部核实属实：renew_scheduler.go:14 renewCheckInterval=1 分钟固定；:64-67 RenewDomain 失败仅 log.Error 后 continue；manager.go:71-76 Manager 无任何按域失败计数/退避字段，进入续期窗口且持续失败时确实每分钟重发完整 ACME order（RenewDomain→issuer.Renew 每次都走完整 challenge）。『无退避的每分钟全量重试』作为设计缺陷成立。但该条 protocolRelated，核心危害量化依赖『LE failed-validation 每账号每 hostname 每小时 5 次』这一上游限额，仓库内材料无法确证——docs/certmgmt-design.md:452 仅提及『Let's Encrypt 每周 5 个重复域名』限流（另一种限额），未提 failed-validation 时限。按规则第三方行为无仓库佐证时封顶 uncertain。

#### 23. [P2·uncertain] ACME 账号私钥双份存储（keys/<email>.key 与 account.json 内嵌 PEM）失同步时，用新生成的 key 配旧 registration KID，所有 ACME 请求被 CA 拒绝

- **位置**：`pkg/certmgmt/lego/issuer.go:35` · 维度：正确性 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：Issue/Renew 的签名密钥来自 GetOrCreateAccountKey（只读 keys/<email>.key，缺失则静默生成新 key，account_store.go:107-132），而 registration（KID）来自 LoadAccount 读 account.json。account.json 里也内嵌 private_key_pem（AccountData.PrivateKeyPEM），但该字段从未被用于恢复签名密钥。若 keys 文件丢失/损坏而 account.json 完好（部分删除、备份恢复不完整），GetOrCreateAccountKey 会生成全新密钥，随后 NewClient 用新 key + 旧 KID 构造 client——ACME 服务器对 KID 与签名 key 不匹配的请求一律拒绝（RFC 8555 JWS 校验），Issue/Renew 从此永久失败且错误信息（unauthorized/malformed）无法提示操作者根因。修法：keys 文件缺失时优先从 account.json 的 PrivateKeyPEM 恢复并回写 keys 文件；两者都无才生成新 key，且此时应把 reg 置 nil 走重新注册。
- **证据**：issuer.go:35 `GetOrCreateAccountKey(...)` 与 issuer.go:41 `LoadAccount(...)` 独立取 key 和 reg；account_store.go:119-124 keys 文件缺失即 GeneratePrivateKey；account_store.go:86-93 LoadAccount 会读内嵌/旁路 key PEM 但调用方（issuer.go:45-47）只取 accountData.Registration，PrivateKeyPEM 弃用。
- **核实**：代码侧事实全部核实属实：issuer.go:35/123 签名 key 取自 GetOrCreateAccountKey（account_store.go:107-114 只读 keys/<email>.key，:119-131 缺失即静默生成新 key 并写盘），issuer.go:41/128 registration 取自 LoadAccount，issuer.go:45-48/132-135 只用 accountData.Registration，PrivateKeyPEM（account_store.go:86-93 会加载）从未用于恢复签名密钥；keys 文件丢失而 account.json 完好时确实产生新 key + 旧 KID 的 client 且 reg 非 nil 不会重注册。docs/certmgmt-design.md:454 也将『account key 丢失』列为已知风险。但该条 protocolRelated，最终危害『ACME 服务器对 KID 与签名 key 不匹配的请求一律拒绝』属 RFC 8555/CA 上游行为，仓库内 wiki/docs/测试均无材料确证该拒绝语义，按规则封顶 uncertain。

#### 24. [P3·confirmed] CancelAcquire 直接删除处于 recycling 状态的槽位：同 IP 重连一次拨号失败即瞬间释放配额，RecycleDelaySec 防换 IP 保护被绕过

- **位置**：`pkg/proxy/forward/clientlimit.go:182` · 维度：正确性 · 单元：FWD
- **问题**：状态机漏洞：IP A 最后一条连接 Release 后槽位进入 recycling（继续占用 MaxClients 配额，60s 定时器保护期内阻止其它 IP 抢占）。此时 IP A 重连：Acquire（:111-120）会 Stop 定时器、清 recycling 标志、pending=1；若随后对容器的拨号失败（TCP handleConn relay_tcp.go:166-170 或 UDP establishSession relay_udp.go:238-249），CancelAcquire 走到 active==0 && pending==0 分支直接 delete(l.slots, remoteIP)——原本还应维持数十秒的 recycling 保护窗被立即清零，任意新 IP 马上可占用该槽位。攻击者甚至可以在目标 inbound 短暂不可用时故意触发该路径提前腾出槽位。CancelAcquire 应在槽位原本处于 recycling 状态时恢复 recycling 标志并重新挂定时器，而不是删除槽位。
- **证据**：clientlimit.go:111-117 Acquire 停表并清 recycling；:174-183 CancelAcquire `if active == 0 && pending == 0 { delete(l.slots, remoteIP) }` 未检查/恢复 recycling 语义。
- **核实**：状态机路径逐行核实为真：Release（clientlimit.go:209-241）置 recycling=1 并挂 60s 定时器；同 IP 再 Acquire（:109-120）Stop 定时器、清 recycling、pending=1；随后拨号失败时 TCP relay_tcp.go:163-170、UDP relay_udp.go:237-250 均调用 CancelAcquire，其 :177-183 在 active==0&&pending==0 时直接 delete(l.slots, remoteIP)，recycling 保护窗被清零，Acquire 的槽位计数（:123-133 明确把 recycling 槽计入配额以阻挡新 IP）立即少 1，新 IP 可即刻抢占。缺陷真实。但降为 P3：触发需要「recycling 期间同 IP 重连 + 后端拨号失败」同时发生，拨号目标是本机容器 inbound（127.0.0.1:port），远端客户端无法直接令其失败，只能蹭容器重启/rotate 的窗口；影响也仅是同一用户配额下另一 IP 提前数十秒接入，非跨用户安全边界破坏。

#### 25. [P3·confirmed] MaxClients<0 与 storedConfig 的交互顺序相关：首条规则为 -1 时用户级限制永远不生效，且注释宣称的 per-rule passthrough 因共享限流器根本不成立

- **位置**：`pkg/proxy/forward/forward_manager.go:251` · 维度：正确性 · 单元：FWD
- **问题**：AddRule 中 rule.MaxClients<0 分支（:206-208）无条件令 effectiveMaxClients=0，忽略 m.userClientLimitConfigs 里管理员先前存的用户级限制。若该 -1 规则是用户的第一条规则，会以 passthrough 配置 newRemoteIPClientLimiter（:260）；之后添加的 MaxClients==0 规则虽然从 storedConfig 算出 effectiveMaxClients>0（:211-215），但因 existingLimiter 已存在且 ruleOwnsUserPolicy=false，永远不会调用 SetConfig（:251-258）——管理员配置的用户级 MaxClients 静默失效，除非事后再手动调一次 SetUserClientLimitConfig。反向顺序则相反：限制型规则先建，后加的 -1 规则拿到的是同一个受限 limiter，:243-247 注释声称的 "We still attach a passthrough limiter to the relay" 与代码事实（:258 复用 existing 受限实例）直接矛盾。用户级共享单实例的设计下 per-rule passthrough 语义本身不可实现，需要么在 -1 分支也参考 storedConfig，要么在文档/接口上取消 per-rule passthrough 承诺。
- **证据**：forward_manager.go:206-208 `else if rule.MaxClients < 0 { effectiveMaxClients = 0 }`；:251-258 existing limiter 仅在 ruleOwnsUserPolicy（rule.MaxClients>0）时 SetConfig；:260 首建时直接用被 -1 归零的 config。
- **核实**：代码事实全部核实：forward_manager.go:206-208 MaxClients<0 无条件 effectiveMaxClients=0 且不参考 userClientLimitConfigs；:251-258 已存在 limiter 时仅 ruleOwnsUserPolicy（MaxClients>0）才 SetConfig，MaxClients==0 的后续规则即便从 storedConfig 算出正值也不会应用；:243-247 注释宣称 -1 规则附加 passthrough limiter，而 :258 实际复用共享（可能受限的）实例，注释与行为矛盾属实。但降为 P3：在仓库现有调用链中该场景基本不可达——GetBindPort 的 rule.MaxClients 恒等于 user.MaxClients（usermanager.go:922），RPC 边界把负值钳为 0（end_node_user.go:115-119），且用户限制的任何变更都经 applyRuntimeSideEffects→SetUserClientLimitConfig（usermanager.go:1183-1195，forward_manager.go:728-742）对既有 limiter 就地生效，会即时修复不一致。故实际是 forward 包 API 层面的语义/注释缺陷，而非当前部署可触发的静默失效。

#### 26. [P3·confirmed] WithPortRange 构造 allocator 时丢失 UseRandom：走该 option 的实例退化为 round-robin 顺序分配，端口可预测

- **位置**：`pkg/proxy/forward/forward_manager.go:20` · 维度：安全 · 单元：FWD
- **问题**：NewForwardManager 的默认 allocator 显式 UseRandom:true（:34-38，注释称随机分配用于避免可预测端口模式，NewDefaultForwardManager 甚至强制 UseRandom=true，:105）。但 WithPortRange 重建 allocator 时 cfg 只填 MinPort/MaxPort（:19-22），UseRandom 零值 false，于是端口按 minPort 起顺序发放（port_allocator.go:91-102 round-robin 路径）。任何通过 WithPortRange 定制范围的部署都会得到连续、可预测的用户端口序列，便于外部扫描枚举活跃用户端口，与项目内"随机分配是唯一支持模式"的约定冲突。与第一轮已报的"错误被吞"（同函数 :24）是两个独立缺陷。
- **证据**：forward_manager.go:19-22 `cfg := PortAllocatorConfig{MinPort: minPort, MaxPort: maxPort}` 缺 UseRandom；对照 :37 默认 `UseRandom: true` 与 :105 `allocCfg.UseRandom = true // always random; round-robin is not a supported use-case`。
- **核实**：代码缺陷属实：forward_manager.go:19-22 WithPortRange 构造的 PortAllocatorConfig 只填 MinPort/MaxPort，UseRandom 零值 false；port_allocator.go:83-103 证实 useRandom=false 时走 nextHint round-robin 顺序分配，端口连续可预测；与 :34-38 默认 UseRandom:true 及 :105 "always random; round-robin is not a supported use-case" 的项目约定直接冲突。但降为 P3：全仓库无任何非测试代码调用 WithPortRange（仅函数定义本身），生产路径 cmd/server.go:115 与 global.go:41 都走 NewDefaultForwardManager，后者强制 UseRandom=true；cmd/test_hotreload 用无 option 的 NewForwardManager 也拿到随机默认。属潜伏的 API 陷阱，当前无触发部署。

#### 27. [P3·confirmed] RuleKey 不含 Network 维度：同一 (container, inbound, user) 无法同时建 TCP 和 UDP 转发，GetBindPort 会把错误传输类型的既有端口直接返回

- **位置**：`pkg/proxy/forward/types.go:126` · 维度：架构 · 单元：FWD
- **问题**：RuleKey = container:inbound:username（:125-127），Network 不参与。需要 TCP+UDP 双转发的 inbound（典型如 shadowsocks 开 UDP relay、或未来同端口 TCP+QUIC）第二条规则会撞 key：AddRule 报 "already exists"。更隐蔽的是上层 usermanager.GetBindPort（usermanager.go:907-910）用同样的 key 先查重，第二次以 Network=udp 请求时会命中第一次建的 TCP 规则并直接返回其端口——调用方拿到一个只中继 TCP 的端口当作 UDP 转发端口用，UDP 流量整体黑洞且无任何报错。要么把 ResolvedNetwork 并入 RuleKey，要么在 Validate/GetBindPort 层显式拒绝并说明。流量计数也因 key 合并无法区分两种传输。
- **证据**：types.go:125-127 `return string(r.ContainerType) + ":" + r.InboundTag + ":" + r.Username`；usermanager.go:907-910 同构 key 查重后 `return existingRule.ListenPort, nil`，不比对 Network；forward_manager.go:140-143 撞 key 报错。
- **核实**：代码事实核实：types.go:125-127 RuleKey 确不含 Network；usermanager.go GetBindPort（约 :906-912）用同构 key 调 GetRule 命中即直接 return existingRule.ListenPort，不比对 req.Network，若第二次以不同 Network 请求会拿到错误传输类型的既有端口，UDP 流量静默黑洞的机制真实存在；forward_manager.go:140-143 撞 key 报 already exists 亦属实。但降为 P3：当前所有调用方对每个 inbound 只请求单一 network——hysteria 恒传 "udp"（container.go:497-504 等三处），mihomo forwardNetworkForProtocol（inbound.go:588-595）仅 hysteria2/tuic 映射 udp 其余为空（默认 tcp），xray 不传 Network；仓库内不存在对同一 (container,inbound,user) 先后请求 TCP 与 UDP 的路径，缺陷为设计局限/潜伏坑（未来 ss UDP relay 类需求会触发），非当前可触发故障。

#### 28. [P3·confirmed] 数据面缓冲零复用：每条 TCP 连接常驻 2×32KB、每个 UDP 会话 64KB 拷贝缓冲，均为一次性分配无 sync.Pool

- **位置**：`pkg/proxy/forward/relay_tcp.go:370` · 维度：资源 · 单元：FWD
- **问题**：copyWithCount 每个方向 make 32KB buf（每条 TCP 连接两个 goroutine 各一份，:370），UDP downstreamLoop 每会话 make 64KB（relay_udp.go:335），生命周期与连接/会话等长且不复用。高并发下（数千连接）产生 ~64KB/conn 的堆常驻与连接建立期的分配洗刷，GC 压力可观。这是转发热路径，建议引入 sync.Pool 或按 relay 级复用（TCP 每连接 goroutine 独占语义下可安全池化，UDP downstream 同理；readLoop 的单 buf 已经做对了）。
- **证据**：relay_tcp.go:370 `buf := make([]byte, defaultBufferSize)`（每 handleConn 调两次 copyWithCount）；relay_udp.go:335 `buf := make([]byte, udpReadBufferSize)` 每会话一份。
- **核实**：属实。relay_tcp.go:370 copyWithCount 内 make([]byte, defaultBufferSize)（defaultBufferSize=32KB，relay_tcp.go:15），handleConn 在 :224 与 :246 两个方向 goroutine 各调一次，即每条 TCP 连接常驻 2×32KB；relay_udp.go:335 downstreamLoop 每会话 make 64KB（udpReadBufferSize=64KB，relay_udp.go:15）。grep 全包无任何 sync.Pool。缓冲生命周期与连接/会话等长且每次新建，高并发下 GC/分配压力描述准确。属性能优化建议而非正确性缺陷，P3 恰当。

#### 29. [P3·confirmed] WaitN 在 ctx 超时返回错误时不回滚已消耗的部分 token：UDP 每丢一包都泄漏 token，饱和时同桶 TCP 流被无谓扣血

- **位置**：`pkg/proxy/forward/ratelimit.go:261` · 维度：正确性 · 单元：FWD
- **问题**：WaitN 的循环里 reserveTokens 每轮实际扣减桶内 token（remaining -= got），但当 ctx 在凑齐 n 之前到期（UDP 路径 forwardUpstream/downstreamLoop 只给 10ms 预算，relay_udp.go:298/362），直接 return ctx.Err()，已扣掉的 n-remaining 个 token 既没送出任何字节也不归还。在 UDP 打满限速的场景下，每个被丢弃的大报文（最多 64KB）都白白烧掉最多一整个 maxChunk 的 token；持续丢包时桶被反复掏空，与该用户共享同一桶的 TCP 流拿不到 token，聚合有效吞吐显著低于配置值，极端情况下 UDP 自身也因每次都凑不齐而持续偷跑 token 形成低效循环。应在 ctx 到期路径把已消耗的 token 加回桶（tokens += consumed）。
- **证据**：ratelimit.go:242-265: `remaining -= got` 后 `case <-ctx.Done(): timer.Stop(); return ctx.Err()` 无任何补偿；reserveTokens (271-305) 内 `l.tokens -= float64(allowed)` 是真实扣减。调用方 relay_udp.go:299-303 在 err!=nil 时直接丢包 return。
- **核实**：属实。ratelimit.go:247-252 循环中 reserveTokens 真实扣减桶存量（:303 l.tokens -= float64(allowed)），remaining 递减记录已消耗量；:259-261 ctx.Done 分支直接 return ctx.Err()，对已消耗的 n-remaining 个 token 无任何归还逻辑。UDP 调用方 relay_udp.go:298-303/:362-367 只给 10ms 预算且 err!=nil 时直接丢包，被扣 token 对应的字节从未发送。userBandwidthLimiter 桶按用户在 TCP/UDP relay 间共享（forward_manager.go 的 userBandwidth 共享设计及 TestIntegration_SetBandwidthAfterAddRule_AppliesToBothRelays 可证），饱和时确实压低同用户聚合有效吞吐。但影响限于单用户自身的桶、方向是过度限速而非绕过限速，仅在 UDP 持续打满限速时显现，属边界条件下的吞吐退化而非功能失效，降为 P3 更恰当。

#### 30. [P3·confirmed] AddRule 在 relay.Start 之前就对既有共享 limiter 执行 SetConfig 并覆盖 userClientLimitConfigs，Start 失败不回滚：失败的 AddRule 仍永久改写了用户级限连策略且已对运行中 relay 生效

- **位置**：`pkg/proxy/forward/forward_manager.go:254` · 维度：错误处理 · 单元：FWD
- **问题**：当 rule.MaxClients>0（ruleOwnsUserPolicy）且该用户已有共享 remoteIPClientLimiter 时，AddRule 先执行 limiter.SetConfig(config) 并写 m.userClientLimitConfigs[rule.Username] = config（251-257 行），之后才 relay.Start()（294 行）。若 Start 失败（端口被外部进程占用等），回滚只做 allocator.Release + traffic.Remove（295-296 行），策略变更不撤销：管理员此前通过 SetUserClientLimitConfig 设置的旧配置被覆盖丢失（无处可查旧值），且新配置已经即时作用于该用户所有正在运行的 relay。这与第一轮已报的“新建 limiter/config 条目泄漏”不同——这里是对既有策略的破坏性覆盖在失败路径下不可恢复。应把策略提交移到 Start 成功之后，或失败时恢复旧 config。
- **证据**：forward_manager.go:251-257 先 SetConfig+覆盖 storedConfig；294-299 Start 失败仅回滚端口与 counter。SetConfig（clientlimit.go:304-308）对活跃 relay 即时生效。
- **核实**：属实。forward_manager.go:251-257：当 ruleOwnsUserPolicy（rule.MaxClients>0）且用户已有共享 limiter 时，先 limiter.SetConfig(config) 并覆盖 m.userClientLimitConfigs[rule.Username]，旧值无备份；relay.Start() 在 :294，失败路径 :295-298 仅 allocator.Release + traffic.Remove，不恢复 config。clientlimit.go:303-308 SetConfig 直接替换 l.config，对该用户所有运行中 relay 即时生效（Acquire 读 l.config）。即 AddRule 失败后策略变更已不可逆地生效且旧配置丢失。与第一轮'新建条目泄漏'确属不同问题（这里是既有策略的破坏性覆盖）。触发需要 Start 失败叠加 rule.MaxClients>0 且用户已有 limiter，条件较窄，P3 恰当。

#### 31. [P3·confirmed] WithPortRange 构造的 allocator 未设置 UseRandom，端口分配退化为 round-robin 顺序模式，与其余两个构造入口强制随机的行为不一致

- **位置**：`pkg/proxy/forward/forward_manager.go:19` · 维度：安全 · 单元：FWD
- **问题**：NewForwardManager 默认 cfg 显式 UseRandom:true（37 行），NewDefaultForwardManager 无条件强制 allocCfg.UseRandom=true（105 行，注释称 round-robin 不是受支持模式），但 WithPortRange 里新建的 PortAllocatorConfig 只填 Min/MaxPort（19-22 行），UseRandom 为零值 false——用该 option 构造的 manager 会顺序分配端口。PortAllocatorConfig 注释本身写明推荐随机以避免可预测端口模式（port_allocator.go:30-32），顺序分配让外部可预测下一个用户端口（配合已报的与内核临时端口重叠问题，也更易被抢占/探测）。应在 WithPortRange 中同样置 UseRandom=true。
- **证据**：forward_manager.go:17-27 cfg 无 UseRandom；对照 forward_manager.go:37 与 105 行 `allocCfg.UseRandom = true // always random; round-robin is not a supported use-case`。systemtest/hysteria_udp_forward_test.go:170 已在使用该 option。
- **核实**：属实。forward_manager.go:19-22 WithPortRange 新建 PortAllocatorConfig 仅填 Min/MaxPort，UseRandom 零值 false，并用它替换掉 NewForwardManager 默认的 UseRandom:true allocator（:34-38）；对照 NewDefaultForwardManager:105 无条件强制 UseRandom=true 且注释明言 round-robin 不是受支持模式。port_allocator.go:84-103 确认 useRandom=false 时走顺序 round-robin 分支，且 :30-32 注释自述推荐随机以避免可预测端口。三个构造入口行为不一致属实。附注：生产代码中未发现 WithPortRange 调用方（仅 systemtest 使用），实际暴露面有限，P3 恰当。

#### 32. [P3·confirmed] GetUserBandwidthLimit 文档承诺“显式设为 unlimited 返回 (0, true)”，实现却在清零后返回 (0, false)，调用方无法区分“显式无限”与“从未设置”

- **位置**：`pkg/proxy/forward/forward_manager.go:690` · 维度：正确性 · 单元：FWD
- **问题**：注释（688-690 行）称 Returns 0 with ok=true if the limit is explicitly set to unlimited (bytesPerSec == 0)。但 SetUserBandwidthLimit(user, kind, 0) 的实现把该方向的 set 标志清为 false（646-659 行 UpdateRates(..., false, ...)），GetUserBandwidthLimit 随后因 IsUploadSet()/IsDownloadSet() 为 false 而返回 (0, false)（704-713 行）。依赖文档语义做集群同步/展示的调用方会把“管理员明确解除限速”误判为“从未配置”，可能触发用默认值重新覆盖等错误决策。需要么修文档，要么保留 set=true 且 rate=0 的状态。
- **证据**：forward_manager.go:690 文档 vs 646-659（bytesPerSec==0 时 UpdateRates 传 false 清 set 标志）与 704-713（!IsUploadSet() → return 0,false）。
- **核实**：属实。forward_manager.go:690 文档写明 'Returns 0 with ok=true if the limit is explicitly set to unlimited (bytesPerSec == 0)'，但 SetUserBandwidthLimit 在 bytesPerSec==0 时（:646-658）把对应方向的 set 标志经 UpdateRates 清为 false（limiter 不存在时更是直接 return，无任何状态），GetUserBandwidthLimit :704-713 因 IsUploadSet()/IsDownloadSet() 为 false 返回 (0,false)。且创建路径仅在 bytesPerSec>0 时建 limiter，'rate=0 且 set=true' 的文档状态在现实现中不可达。文档与实现矛盾确凿，调用方无法区分'显式解除'与'从未设置'，P3 恰当。

#### 33. [P3·confirmed] QueryTrafficStats 聚合行以组内首条记录整体为基底，RuleKey/ListenPort/TargetAddr 等规则级字段被随机保留到聚合结果中误导消费方；且 inbound 分组键单/多维格式不一致

- **位置**：`pkg/proxy/forward/forward_manager.go:552` · 维度：正确性 · 单元：FWD
- **问题**：GroupBy 聚合时 `aggregated[groupKey] = record` 直接存入首条完整记录，仅累加 Uplink/Downlink/ActiveConnections（552-559 行）。按 user 聚合的行会携带该用户任意一条规则的 RuleKey、ListenPort、TargetAddr、InboundTag、ContainerType——map 迭代序随机，前端/上报侧若展示这些字段会得到每次查询都可能不同的伪数据。另外单维 "inbound" 的组键是 containerType+":"+inboundTag（544 行），而多维 buildGroupKey 中 inbound 维度只取 record.InboundTag（578-579 行），同名维度两种键格式，跨接口消费时对不上。聚合行应清空非分组维度字段（或仅保留分组键+三个累加值）。
- **证据**：forward_manager.go:552-559 else 分支整条 record 入 map；544 行 `groupKey = string(record.ContainerType) + ":" + record.InboundTag` vs 578-579 行 buildGroupKey 只 append record.InboundTag。
- **核实**：代码核实属实。forward_manager.go:558 else 分支将整条 record 存入 aggregated map，仅在 552-556 累加 Uplink/Downlink/ActiveConnections；源记录来自遍历 m.rules（第 490 行，类型为 map[string]*managedRule，第 86 行），map 迭代序随机，因此按 user/container 等聚合时，聚合行携带的 RuleKey/ListenPort/TargetAddr/InboundTag/ContainerType 是该组内任意一条规则的值，且每次查询可能不同。ForwardTrafficRecord 各字段均有 json tag，AggregatedStats 是 TrafficQueryResult 的 JSON 导出字段（types.go:234），伪数据会随 API 结果暴露。第二点也属实：544 行单维 inbound 键为 ContainerType+":"+InboundTag（与 types.go:232 文档一致），而 buildGroupKey 578-579 行多维模式只取 InboundTag，同名维度两种键格式，多维如 "user,inbound" 还会把不同容器上同名 inbound 合并。当前仓库内暂无消费方读取这些聚合字段（仅接口声明），P3 定级合理。

#### 34. [P3·confirmed] mutateUser 不过滤 tombstone：Set*/Update* 可作用于已删除用户并触发容器为其重建 relay，进而永久阻塞同名 AddUser

- **位置**：`pkg/proxy/usermanager/usermanager.go:356` · 维度：正确性 · 单元：UM
- **问题**：mutateUser 的 lookup（:356 `user, exists := m.users[username]`）不检查 IsDeleting，因此 SetUserRole/SetUserGroup/UpdateUser/UpdateUserPassword/SetUserBandwidthLimit/SetUserClientLimit/ResetAuthToken 全部可对 tombstone 生效：改字段、re-stamp 版本（tombstone 以更新版本继续在集群传播）、并 emit UserEventUpdate（evt.User=tombstone）。容器的 Update 处理只查组可见性不查 IsDeleting（hysteria container.go:530-566），会调 GetBindPort 为该用户分配端口——GetBindPort 也不查 IsDeleting（第一轮 :867 已确认）——已删除用户重新获得活 relay。后果链：AddUser 对 tombstone 的重建前置条件是 forwardMgr.GetRulesByUser 为空（:485），该 relay 使同名用户永远无法重建，除非人工清理。RemoveUser 与并发/重试的 Update 请求（center 下发场景）交错即可触发。mutateUser 应对除 RemoveUser 外的 eventType 拒绝 deleting 用户。
- **证据**：usermanager.go:356-366 lookup 后直接 mutate/stamp/save/emit，无 IsDeleting 分支；SetUserRole:759 等闭包均不检查；AddUser:484-488 要求 GetRulesByUser 为空才允许 tombstone 重建。
- **核实**：核心缺陷确认：mutateUser:356-366 仅查存在性不查 IsDeleting，SetUserRole:758/SetUserGroup:766/UpdateUser:776 等全部经此路径，对 tombstone 生效并 re-stamp 版本、emit UserEventUpdate(evt.User=tombstone)。触发链也确认：hysteria container.go:528-566 Update 分支只查 IsUserVisible（isUserVisibleLocked 仅比对 TargetGroup，不查 IsDeleting）+addedUsers（Remove 时已删），随即 GetBindPort，而 GetBindPort:864-875 只查 IsExpired 不查 IsDeleting——已删除用户确会重获活 relay。但『永久阻塞同名 AddUser、除非人工清理』被反驳：hysteria/snell/xray/mihomo 均有周期 reconcile 循环（xray exec_runner.go:1044-1052、hysteria container.go:733-771 等），tombstone 不在 ListUsers 的 visibleSet 中，下一 tick 即被识别为 tracked-but-not-visible 并 ReleaseBindPort 清理 relay，AddUser 阻塞窗口约为一个 reconcile 间隔（默认 30s）而非永久。实际影响为：已删用户短暂恢复连通 + tombstone 版本churn跨集群传播 + AddUser 短暂失败，降为 P3。

#### 35. [P3·confirmed] ruleKey 用 ":" 拼接但 username/inboundTag 无字符集校验，可构造 key 碰撞导致跨用户端口/流量互串

- **位置**：`pkg/proxy/usermanager/usermanager.go:907` · 维度：安全 · 单元：UM
- **问题**：GetBindPort:907、RotateUserPortForInbound:1014 与 forward.ForwardRule.RuleKey()（types.go:126）都用 `container:inboundTag:username` 直接拼接，而 validateUserCredentials(:1424) 只检查非空，不限制字符集。inbound="a:b"+user="c" 与 inbound="a"+user="b:c" 生成同一 key：GetRule 命中他人规则时 GetBindPort 直接把他人的 ListenPort 返回给该用户（:908-911），两个用户共享同一 relay，流量计入他人（统计 key 同样碰撞）、限速/客户端数限额互相干扰；RemoveRule 也会误删他人规则。轮换用的 tmpTag `inboundTag+":_r"`（:1028）同样占用该命名空间，真实 inbound tag 以 ":_r" 结尾时会与轮换临时规则冲突。应在用户创建/inbound 注册时校验禁止 ":"（或改用不可碰撞的编码）。
- **证据**：usermanager.go:907 `ruleKey := string(req.ContainerType) + ":" + req.InboundTag + ":" + req.Username`；:1424-1431 validateUserCredentials 仅检查空串；forward/types.go:126 RuleKey 同构。
- **核实**：机制确凿：usermanager.go:907、:1014 与 forward/types.go RuleKey()（:125-127）均为裸":"拼接，validateUserCredentials:1424-1431 仅查非空，RPC AddUsers（end_node_user.go:76）路径同样无字符集校验，inbound="a:b"/user="c" 与 inbound="a"/user="b:c" 碰撞、GetBindPort:908-911 直接返回他人 ListenPort、tmpTag ":_r"（:1028）占用同命名空间均属实。但严重度存疑：用户创建仅有持 auth token 的管理端 CLI/RPC 与集群同步路径，无自助注册面；含":"的用户名需管理员主动构造，inbound tag 以":_r"结尾属运营者配置失误。真实但触发前提受控且概率低，属输入校验硬化项，降为 P3。

#### 36. [P3·confirmed] AddUser 在设置 LoginPassword 之前 stampVersion，用户创建即违反 Hash==ComputeHash(user) 不变式

- **位置**：`pkg/proxy/usermanager/usermanager.go:510` · 维度：正确性 · 单元：UM
- **问题**：AddUser 的顺序是 :510 `m.stampVersion(user)`（内部 ComputeHash）→ :515-521 才设置 user.LoginPassword。ComputeHash 对非空 LoginPassword 会写入哈希（sync/hash.go:60-62），因此每个带登录密码的新用户，其持久化/同步出去的 Hash 都是按 LP 为空算的；而 UpdateUserPassword 路径经 mutateUser 在闭包之后 re-stamp，Hash 包含 LP。同一逻辑内容在不同路径产生不同 Hash，任何未来在本地重算校验 Hash 的代码（或按 ComputeHash 语义做一致性修复的节点）都会误判。当前 digest 比较只用存储值所以无即时故障，属埋雷型不变式破坏；把 stampVersion 移到 LoginPassword 赋值之后即可修复。
- **证据**：usermanager.go:510 stampVersion 在前，:520 `user.LoginPassword = lp` 在后；sync/hash.go:60-62 `if u.LoginPassword != "" { writeField(u.LoginPassword) }`。
- **核实**：核实成立：usermanager.go:510 stampVersion（内部 :330 Hash=ComputeHash(user)）先于 :515-521 的 LoginPassword 赋值执行，sync/hash.go:60-63 仅在 LoginPassword 非空时 writeField，故带登录密码的新建用户其持久化/同步 Hash 均按 LP 为空计算；UpdateUserPassword 经 mutateUser 在闭包后 re-stamp（:361-368），Hash 含 LP，两路径对同一逻辑内容产生不同 Hash。全仓 ComputeHash 唯一调用点即 stampVersion，digest 比较（sync/digest.go）只用存储 Hash，无即时故障——与 finding 自评一致，属埋雷型不变式破坏，P3 恰当。修复即把 :510 移到 :521 之后。

#### 37. [P3·confirmed] GetUserDeltaTraffic/GetUserStats 对多 inbound 用户只返回任意一条记录，reset=true 却清空该用户全部 inbound 的 delta

- **位置**：`pkg/proxy/usermanager/usermanager.go:2220` · 维度：正确性 · 单元：UM
- **问题**：GetUserStats(:1950-1961) 遍历 ByUser 返回第一个 Username 匹配的条目——map 迭代序随机，多 inbound/多容器用户返回哪个 inbound 的数据不确定，且不是按用户聚合值。GetUserDeltaTraffic(:2220-2229) 基于它取单条 stats 后，若 reset=true 调 resetUserDelta(:1996) 清零该用户**所有** key 的 delta：其余 inbound 自上次以来的增量既没被返回也无法再读，无声丢失。注释称这是 Prometheus scrape 主通路，语义上应聚合全部条目后再整体清零。当前仓库内无外部调用方（grep 确认），属带错误语义的导出 API，建议修正或删除。
- **证据**：usermanager.go:1955-1959 `for _, stats := range sc.stats.ByUser { if stats.Username == username { return &stats, true } }` 只取首个；:2224-2227 单条返回 + `resetUserDelta(username)` 全量清零。
- **核实**：亲读代码证实：collect() 以 containerType:inboundTag:username 为 ByUser key（:1864），多 inbound 用户必有多条记录；GetUserStats(:1950-1961) 以 map 随机迭代序返回首个 Username 匹配条目，非聚合；GetUserDeltaTraffic(:2220-2229) 取该单条后 reset=true 时调 resetUserDelta(:1996-2006) 清零该用户所有 key 的 delta，其余 inbound 的增量确实无声丢失。grep 全仓库确认除定义与 :1844 注释外无任何调用方（包括测试），属带错误语义的死导出 API。P3 恰当。

#### 38. [P3·confirmed] tombstone 永不物理删除且 ListDigests 全量包含，users map/DB/每次心跳 digest 载荷随历史删除数无界增长

- **位置**：`pkg/proxy/usermanager/usermanager.go:1642` · 维度：资源 · 单元：UM
- **问题**：设计上用户只软删（RemoveUser 注释 :542『never physically deleted』），且 ListDigests(:1642-1656) 对包括全部 tombstone 在内的 m.users 逐条生成 digest 参与每次心跳交换。长期运行的多节点集群中，历史删除用户只增不减：内存 map、SQLite 表、以及每个心跳周期在所有节点间传输/比较的 digest 数组都随之线性膨胀，CompareDigests 是 O(所有历史用户)。没有任何 tombstone GC/compaction 机制（如超过保留期且全集群确认后物理清除）。与第一轮 :1877 的 statsCollector 聚合表无界是不同的增长面，这个还叠加了网络与心跳 CPU 成本。
- **证据**：usermanager.go:1646-1655 `for _, u := range m.users { digests = append(...) }` 无 tombstone 过滤；:541-547 RemoveUser 文档明确永不物理删除；全仓库无 tombstone 清理代码。
- **核实**：证实：RemoveUser 文档(:541-547)明确永不物理删除，仅 MarkDeleting；ListDigests(:1642-1656) 遍历全部 m.users 无 tombstone 过滤且注释明示 including tombstones；grep 确认 usermanager.go 无 delete(m.users,...)、无任何对 UserStore.Delete 的用户清理调用，tombstone 仅在同名 AddUser 重建时被替换。CompareDigests 逐条比对为 O(全部历史用户)。docs/heartbeat-optimization-design.md:166 自己也承认『墓碑常驻 m.users（无明显 GC）』并指出需要确定性保留策略。注意 tombstone 参与 digest 是删除传播所必需的设计，但缺 GC/compaction 导致无界增长的问题成立。P3 恰当。

#### 39. [P3·confirmed] UserEventExpire 定义后全仓库无发送点，过期统一走 UserEventRemove，订阅方无法区分过期与删除

- **位置**：`pkg/proxy/usermanager/usermanager.go:82` · 维度：架构 · 单元：UM
- **问题**：UserEventType 定义了 UserEventExpire(:81-82)，但 grep 全仓库仅此一处引用：cleanupExpiredUsers(:1404-1421) 对过期用户直接调 RemoveUser，发出的是 UserEventRemove。容器订阅方因此无法区分『管理员删除』与『到期』（比如到期用户可能需要保留 inbound 只断流、或推送不同的订阅提示），且 UserEvent 文档注释『nil for remove/expire events』维持着一个从不出现的事件类型的假象。要么补上过期路径发 Expire 事件的语义，要么删除该常量避免误导后续订阅者实现。
- **证据**：grep -rn UserEventExpire 全仓库仅 usermanager.go:81-82 定义处命中；cleanupExpiredUsers:1417 `m.RemoveUser(RemoveUserRequest{...})` 复用删除事件。
- **核实**：grep 全仓库（含 cmd/、pkg/ 全部 go 文件）UserEventExpire 仅命中 usermanager.go:81-82 定义处，无任何发送点；cleanupExpiredUsers(:1404-1421) 对过期用户调 RemoveUser，经 mutateUser 发出 UserEventRemove(:376-378)。UserEvent 文档注释(:91)『nil for remove/expire events』确实维持着从不出现的事件类型假象。订阅方无法区分到期与删除属实。P3 恰当。

#### 40. [P3·confirmed] 测试缺口：tombstone 上的 mutateUser 变更、DrainStats 过滤丢流量、多 inbound delta 语义均无覆盖

- **位置**：`pkg/proxy/usermanager/usermanager_test.go:1` · 维度：测试缺口 · 单元：UM
- **问题**：现有测试覆盖 CRUD/tombstone 重建/rotate/reset 等主流程，但本轮发现的几条边界均无测试：(1) 对已 RemoveUser 的用户调 SetUserBandwidthLimit/UpdateUser 等应被拒绝或至少不触发 UserEventUpdate——当前无断言 tombstone 不可变更；(2) BandwidthStatsCollector.DrainStats 在用户被删除/过期/组不可见后是否仍上报其残余 delta（bandwidth_stats_collector_test.go 只有接口断言）；(3) 多 inbound 用户的 GetUserDeltaTraffic(reset=true) 是否聚合全部 inbound 且不丢增量；(4) RotateUserPortForInbound final AddRule 失败（mock 连续失败）后的用户状态。这些正是软删除+统计聚合最易回归的路径。
- **证据**：usermanager_test.go 与 bandwidth_stats_collector_test.go 中 grep 不到 tombstone+SetUser*、DrainStats 过滤、GetUserDeltaTraffic 多 inbound、rotate 失败注入相关用例。
- **核实**：逐项核实：(1) mutateUser(:352-384) 本身不检查 IsDeleting，tombstone 可被 SetUserBandwidthLimit/UpdateUser 等变更并发 UserEventUpdate，测试中无 tombstone 不可变更断言，缺口真实且指向未加防护的行为；(2) bandwidth_stats_collector_test.go 仅有 TestBandwidthStatsCollectorImplementsInterface 一个接口断言，无 DrainStats 行为测试；(3) 两个测试文件 grep 不到任何 GetUserDeltaTraffic 用例；(4) rotate_inbound_port_test.go 实有 9 个用例（多于 finding 所述的 3 类，还含 OldPortReleased/PreferredPort 等），但均无 AddRule 失败注入，Step-4 失败路径确无覆盖。evidence 对 rotate 测试数量描述略有低估但核心缺口全部成立。P3 恰当。

#### 41. [P3·confirmed] SyncUpsertUser 墓碑分支只采纳版本字段+TargetGroup，其余参与 Hash 的内容字段与远端分叉且 digest 永远一致，无法自愈

- **位置**：`pkg/proxy/usermanager/usermanager.go:1536` · 维度：正确性 · 单元：UM
- **问题**：远端墓碑覆盖本地活跃用户时（L1531-1539），只采纳 UpdatedAtUs/OriginNode/Hash/TargetGroup 四个字段，而 ComputeHash 参与计算的 AuthToken、ExpiryTime、Role、Bandwidth*、MaxClients、ClientRecycleDelaySec、ClientDrainSec、LoginPassword 全部保留本地旧值。结果是本地记录持有远端的 Hash 但内容与远端不同——与第一轮发现的 token 冲突 Hash 分叉同类，但路径不同。由于 ListDigests 上报的 Hash 与远端一致，CompareDigests 的『同版本哈希不一致』自愈分支（digest.go L66-70）永远不触发；本地还会经 GetUserForSync 把这份『内容 A + 哈希 B』的分叉记录传播给第三节点，第三节点全量采纳后集群内同一 Hash 对应多种内容。实际影响场景：远端在删除前重置过 AuthToken/改过限速，墓碑同步后本地保留旧 token/旧限速，若之后该用户在另一节点被复活（tie-break 或时间差导致本地版本反而胜出传播），复活的是过期内容。
- **证据**：L1536-1539 只有 4 个字段赋值；对照 L1594-1608 更新分支采纳 11 个字段；hash.go L45-62 显示 AuthToken/Role/带宽等均参与 Hash；digest.go L66-70 的自愈条件是 Hash 不等，此处 Hash 被强行改成相等
- **核实**：机制全部核实属实：L1536-1539 墓碑分支只采纳 UpdatedAtUs/OriginNode/Hash/TargetGroup 四字段，对照 L1594-1608 更新分支采纳全部内容字段；hash.go L45-62 证实 AuthToken/Role/带宽/MaxClients/LoginPassword 均参与 ComputeHash；ListDigests（L1642-1656）上报被采纳的远端 Hash，故 digest.go L66-70 的『同版本哈希不一致』自愈分支永不触发；GetUserForSync（L1660-1664）返回完整本地记录，『内容 A + 哈希 B』的分叉确会传播给第三节点（第三节点 !exists 时 L1517 全量存储该分叉记录）。但『复活过期内容』的加重场景不成立：本地 AddUser 复活（L480-500）会重建全新 user 对象并重新生成 token，远端 active 复活走更新分支全量采纳 incoming 内容，两条复活路径都不会保留墓碑上的陈旧字段（GetUserByToken L626 也排除 deleting 用户，陈旧 token 在墓碑期完全休眠）。实际影响收敛为协议不变量破坏（Hash 与内容脱钩、自愈被屏蔽、分叉传播），无用户可见危害，降为 P3。

#### 42. [P3·confirmed] GetBindPort 命中已有规则时不校验 TargetAddr 与 req.TargetPort 一致，backend 端口变更后返回指向旧 target 的转发端口

- **位置**：`pkg/proxy/usermanager/usermanager.go:908` · 维度：正确性 · 单元：UM
- **问题**：早退路径（L907-911）只按 ruleKey=container:inbound:user 判断规则存在即返回 existingRule.ListenPort，不校验该规则的 TargetAddr 是否等于本次请求的 127.0.0.1:req.TargetPort。inbound 的 backend 监听端口是动态分配的：如果 inbound 重启后 backend 端口变化，而旧规则未被 ReleaseInboundPorts 清理（容器 teardown 路径出错、事件丢失——第一轮已确认 emitEvent 会丢事件），GetBindPort 会把指向旧 backend 死端口的 relay 端口当作有效结果返回，用户流量进入 relay 后转发到无人监听的端口，表现为神秘的连接失败且永不自愈（因为规则『存在』所以永远走早退）。此外该路径也不修复 user.PortMappings[req.TargetPort] 与实际规则的映射（早退时若 PortMappings 缺失/不一致不会补写），幂等性只有一半。建议早退前比对 existingRule.TargetAddr，不一致时删除重建。
- **证据**：L907-911 仅 GetRule(ruleKey) != nil 即 return，无 TargetAddr 比对；L921 新建路径 TargetAddr = fmt.Sprintf("127.0.0.1:%d", req.TargetPort) 证明 target 由每次请求决定；ReleaseInboundPorts L1391 是唯一按 inbound 清规则的路径且依赖容器主动调用
- **核实**：代码事实核实：L907-911 早退仅凭 GetRule(ruleKey) != nil 即返回 existingRule.ListenPort，ruleKey = containerType:inboundTag:username 不含 TargetPort，未与本次请求的 127.0.0.1:req.TargetPort 比对（L921 证明 TargetAddr 由每次请求决定）；早退路径也确实不补写 user.PortMappings。触发前提有条件：需要 inbound backend 端口变化且旧规则未被 ReleaseInboundPorts（L1377-1398，由 xray exec_runner.go L1411 / mihomo inbound.go L709 在 inbound 关闭时主动调用）清理——即 teardown 出错或流程被跳过时才发生，且一旦发生因规则『存在』永远早退、不自愈。ForwardManager 规则为进程内内存态，跨进程重启不复现，进一步收窄触发面。作为有条件触发但无自愈的缺陷，P3 恰当。

#### 43. [P3·confirmed] 测试缺口：GetBindPort 重启幂等（BindPorts 重复累积）与 Rotate Step-4 失败断流路径均无覆盖

- **位置**：`pkg/proxy/usermanager/rotate_inbound_port_test.go:9` · 维度：测试缺口 · 单元：UM
- **问题**：rotate_inbound_port_test.go 只覆盖 UserNotFound/NoRule/成功/preferred 端口/多 inbound 隔离等正路径，没有任何用例注入 AddRule 失败模拟 Step-4 双重失败（本轮新发现的断流场景）；usermanager_test.go 对 GetBindPort 的断言止于单次调用后 BindPorts 长度为 1（L513），没有『带持久化 store 重启 UserManager 后再次 GetBindPort』的用例，无法暴露 BindPorts 跨重启重复累积。DrainStats 也没有『用户被删除后残余 delta 是否上报』的用例（bandwidth_stats_collector_test.go 仅做接口断言）。这三个都是本模块状态机的关键边界，建议补：可注入失败的 fake ForwardManager + 基于同一 SQLite 文件重建 manager 的重启用例。
- **证据**：rotate_inbound_port_test.go L9-214 无失败注入用例；usermanager_test.go L511-514 只验证单次 GetBindPort；bandwidth_stats_collector_test.go 按模块地图仅为接口实现断言
- **核实**：三个测试缺口逐一核实属实：(1) rotate_inbound_port_test.go 共 9 个用例（L9-237）全为正路径/参数校验，无任何 AddRule 失败注入用例，Rotate 失败分支无覆盖；(2) usermanager_test.go TestGetBindPort_AllocatesPort L511-514 仅断言单次调用后 BindPorts 长度为 1，store_test.go 仅测 SQLiteUserStore 本身的 Save/Load/Upsert/Delete，全包无『同一持久化文件重建 UserManager 后再次 GetBindPort』用例——而 GetBindPort L945 无条件 append BindPorts，重启后 ForwardManager 规则（内存态）丢失导致早退失效、按 PortMappings 首选端口重建规则并再次 append，重复累积premise成立；(3) bandwidth_stats_collector_test.go 全文仅一个编译期接口断言，DrainStats 无任何行为测试。P3 恰当。

#### 44. [P3·confirmed] buildWebSocketTLSSettings 把 network 写成 "tcp"，即使 streamSettings 接通 wsSettings 也永远不会被消费

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:381` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：WS 变体的 stream map 是 {"security":"tls","network":"tcp","wsSettings":{...}}。types.buildStreamConfigProto 按 network 分支选择 transport：network="tcp" 时只读 tcpSettings，wsSettings 被无声忽略。也就是说除了第一轮已报的 buildReceiverSettings 丢弃整个 streamSettings（TODO, L299）之外，这份 map 本身就是自相矛盾的——修复 TODO 后 BuildVMessWS/BuildVLESSWSTLS 依然产出 TCP 传输而非 WebSocket。正确值应为 "ws"/"websocket"。
- **证据**：L379-390: settings map 中 `"network": "tcp"` 与 `"wsSettings": {"path": wsPath}` 并存；types.go L813 的 switch 在 network=tcp 分支只查找 "tcpSettings"。
- **核实**：builder.go:379-390 的 map 确实同时含 "network":"tcp" 与 "wsSettings"；消费端在同仓库：types.go normalizeNetworkName("tcp")="tcp"（L574-591），buildStreamConfigProto 的 switch（L813 起）在 ws/websocket 分支才读 wsSettings（L814-840），tcp 分支只读 tcpSettings（L883-899），wsSettings 被无声丢弃。生产者与消费者均在仓库内，自相矛盾纯代码级可证，不依赖第三方断言。前提假设（修 L299 TODO 后走 types.buildStreamConfigProto）合理。P3 恰当（该包本身也是无生产调用的死代码）。

#### 45. [P3·confirmed] buildRealitySettings 只带 publicKey/shortId，缺服务端 Reality 必需的 privateKey/dest/serverNames，BuildVLESSReality 签名先天不足

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:394` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：xray 服务端 Reality inbound 必须提供 privateKey（服务端私钥）、dest/target（回落目标）和 serverNames；publicKey 是发给客户端用的，服务端配置里并不需要。buildRealitySettings 生成的 map 只有 publicKey+shortId，且 BuildVLESSReality 的参数列表 (realityPublicKey, realityShortID) 根本没有 privateKey 入口——即使 streamSettings 接通（第一轮 L299 finding 修复后），types.buildStreamConfigProto 也只会得到一个没有私钥、没有 dest 的 reality.Config，xray 无法启动该 inbound。属于 API 设计层面的字段映射错误，需要改函数签名而不仅是补字段。
- **证据**：L394-404: realitySettings map 仅含 "publicKey" 和 "shortId" 两键；L132 BuildVLESSReality 形参无 privateKey/dest/serverName。
- **核实**：builder.go:394-404 map 仅含 publicKey/shortId，L132 BuildVLESSReality 形参确无 privateKey/dest/serverNames 入口，均已读码核实。上游行为有仓库材料支撑：internalproto/upstream/transport/internet/reality/config.proto 中 dest(2)/server_names(5)/private_key(6) 位于服务端字段区，public_key(23) 位于客户端字段区（21-27）；types.go:761 项目自身注释 'Handle dest/target (required for reality)'，且 L710-725 对 privateKey 做 32 字节强校验——即接通后 reality.Config 的 PrivateKey/Dest 仍为空。属 API 签名级缺陷、需改签名的判断成立。P3 恰当（死代码包）。

#### 46. [P3·confirmed] vless/trojan 构建器对缺失 id/password 的 client 仍 append 带 nil Account 的 User

- **位置**：`pkg/xrayapi/types/types.go:420` · 维度：错误处理 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：buildVLessInboundConfig 中若 clientMap 无 "id"，user.Account 保持 nil 但仍执行 `config.Clients = append(config.Clients, user)`（L420）；buildTrojanInboundConfig 同样：无 "password" 时 nil-Account User 被 append 到 config.Users（L497）。xray 端 protocol.User.ToMemoryUser 对 nil Account 会直接报错，导致整个 inbound 添加失败，且错误信息不指向具体哪个 client 字段缺失。三个协议对同一异常输入出现三种行为（vmess 伪造零 UUID、vless/trojan 塞 nil 账号），应统一为显式报错跳过。
- **证据**：L404-420: Account 赋值包在 `if id, ok := ...` 内，append 在 if 外无条件执行；trojan 路径 L485-497 结构相同。
- **核实**：代码核实：vless 路径 types.go L404-418 Account 赋值包在 `if id, ok := clientMap["id"].(string)` 内，L420 的 append 在 if 外无条件执行；trojan 路径 L485-495/L497 结构相同；vmess 路径 L354-362 确在缺 id 时伪造 "00000000-...-000000000000" 零 UUID（且 buildVMessInboundConfig L325-327 还静默吞掉 buildVMessUser 错误）。三协议对同一异常输入三种行为、nil-Account User 被序列化下发均为仓库内确凿事实。上游 ToMemoryUser 对 nil Account 报错的细节仓库内无 xray-core 依赖/vendor/文档可核证，但无论 xray 报错还是忽略，缺凭据 client 被静默下发或伪造零 UUID 都是错误行为，缺陷成立不依赖该第三方断言。P3 恰当。

#### 47. [P3·confirmed] TLS certificateFile/keyFile 读取失败被静默吞掉，产出无证书内容的 tls.Config

- **位置**：`pkg/xrayapi/types/types.go:657` · 维度：错误处理 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：buildStreamConfigProto 处理 certificates 时，`if data, err := os.ReadFile(certFile); err == nil { cert.Certificate = data }`（L657，keyFile 同样在 L668）——文件不存在/权限错误时不报错，只留下 CertificatePath 而 Certificate 为 nil。经 gRPC 下发的 proto 配置里 xray 主要消费字节内容，结果是 TLS inbound 带着空证书被提交，失败要么发生在 xray 端 handler 构建、要么推迟到首次握手，均已丢失"哪个文件读不到"的根因。配置解析阶段本应 fail-fast 返回错误。
- **证据**：L653-672: 两处 `if data, err := os.ReadFile(...); err == nil` 模式，err != nil 分支无任何处理、无日志。
- **核实**：代码核实：types.go L653-660（certificateFile）与 L664-671（keyFile）均为 `if data, err := os.ReadFile(...); err == nil { ... }` 模式，err != nil 分支无返回、无日志，产出的 tls.Certificate 仅有 Path 而 Certificate/Key 字节为 nil，随后 L681 照常 append、L687-691 正常打包返回——配置构建阶段完全丢失读文件失败的根因，确凿。上游 xray 是否会回退按 CertificatePath 自读属第三方行为、仓库内无法确证（finding 中"主要消费字节内容"这一句未获仓库材料支持），但即使 xray 回退读路径，同一不可读文件仍会失败且错误延迟到 xray 端/握手期，本包吞错、不 fail-fast、无日志的缺陷独立成立。P3 恰当，行号准确。

#### 48. [P3·confirmed] GetPortMapping 对 port 仅做 float64 断言，字符串端口静默得 0，端口映射丢失导致转发规则不切换

- **位置**：`pkg/xrayapi/hotreload/manager.go:297` · 维度：正确性 · 单元：XAPI
- **问题**：applyPortOffset(L246-249) 明确支持并保留字符串端口（string 进 string 出），PrepareNewConfig 会把它们写进新配置；但 GetPortMapping 里 `port, _ := in["port"].(float64)`（旧配置 L297、新配置 L304）对字符串端口断言失败得 0。结果 oldByTag[tag]=0、newPort=0，SwitchForwardRules 拿着 "127.0.0.1:0" 去匹配规则必然匹配不到并 continue——该 inbound 的转发规则不被切换，旧实例随后被 Stop，用户流量黑洞，整个过程零错误零日志。同一文件内两个函数对同一字段的类型宽容度不一致是根因。
- **证据**：L296-298: `port, _ := in["port"].(float64); oldByTag[tag] = uint32(port)`；对比 L246 `case string:` 分支说明字符串端口是被上游支持的合法形态。
- **核实**：全链路仓库内核实：applyPortOffset L245-249 有专门 string 分支（string 进 string 出），证明字符串端口是上游支持的合法形态且经 PrepareNewConfig L157/L175 写入新配置文件；loadInboundsFromFile L129 直接 json.Unmarshal，字符串端口读回仍为 string；GetPortMapping L297（旧）/L304（新）`in["port"].(float64)` 断言失败静默得 0，而 mapping 仅按 tag 命中即生成（L307-313），产出 OldPort=0/NewPort=0；SwitchForwardRules L329 用 "127.0.0.1:0" 匹配必然落空 → L336-338 continue 静默跳过；ExecuteHotReload L443 Step 7 无条件 oldExecutor.Stop()，L445 result.Success=true——规则仍指旧端口、旧实例已停、零错误零日志，黑洞链完整。usermanager.go:921 证明规则 TargetAddr 确为 "127.0.0.1:<port>" 形态。protocolRelated=false 正确。P3 合适（仅 cmd/test_hotreload 可达且需字符串端口输入），行号准确。

#### 49. [P3·confirmed] Manager 的 mu/currentConfig 是死缓存：全包无任何写入路径，RWMutex 保护恒为 nil 的字段

- **位置**：`pkg/xrayapi/hotreload/manager.go:114` · 维度：架构 · 单元：XAPI
- **问题**：Manager 声明 `mu sync.RWMutex` 与 `currentConfig *xrayConfig`，GetCurrentInbounds 取 RLock 后检查 currentConfig==nil；但整个包（含 ExecuteHotReload/ExecuteHotUpdate 成功路径）从不给 currentConfig 赋值、从不取写锁，缓存分支永不命中，每次都落盘读 OldConfigPath。更重要的语义问题：热切换成功后 OldConfigPath 指向的仍是旧配置文件，后续再调 GetCurrentInbounds/GetPortMapping 读到的是已被替换实例的旧端口集，二次 reload 会基于过期状态计算映射。要么移除死缓存与锁，要么在切换成功后更新 currentConfig/OldConfigPath。
- **证据**：L83-84 字段声明；L111-118 唯一读点；grep 全包无 `currentConfig =` 赋值、无 `mu.Lock()`；ExecuteHotReload 成功路径(L442-447)不更新任何状态。
- **核实**：亲自核实 pkg/xrayapi/hotreload/manager.go（该包唯一文件）：grep 全包 currentConfig 仅出现在 L84 声明、L114 nil 检查、L118 读取，确无任何赋值；mu 仅有 L111 的 RLock()，无 Lock()，缓存分支永不可达，RWMutex 保护恒为 nil 的字段属实。语义问题同样成立：ExecuteHotReload 成功路径（L442-447）与 ExecuteHotUpdate 成功路径（L555-557）均不更新 currentConfig 或 m.config.OldConfigPath，热切换成功后运行实例实际使用 NewConfigPath（端口已加 PortOffset），但 GetCurrentInbounds/GetPortMapping 仍读旧文件旧端口；若复用同一 Manager 做二次 reload，SwitchForwardRules 按旧文件端口匹配转发规则会全部 miss 并静默 skip。缓解因素：包外唯一调用方 cmd/test_hotreload/main.go 每次新建 Manager 且只做单次 reload，二次 reload 场景目前为潜在路径，故 P3 定级恰当。行号 114 准确。

#### 50. [P3·confirmed] drain-old 阶段的 500ms 等待无意义：RemoveRule 时 relay.Stop 已通过 ctx cancel 强制断开全部活动连接，所谓零停机切换实为全量断连

- **位置**：`pkg/xrayapi/hotreload/manager.go:546` · 维度：架构 · 单元：XAPI
- **问题**：切换转发规则的实现是 RemoveRule+AddRule，而 TCPRelay.Stop（relay_tcp.go:89-95）先 r.cancel() 再 wg.Wait()，handleConn 的 select 收到 ctx.Done 后立即 `clientConn.Close(); targetConn.Close()`（relay_tcp.go:329-334）。即在 switch-forward 阶段所有经过该端口的用户连接就已被硬切断，之后的 drain-old sleep(500ms)（以及 ExecuteHotReload 中隐含的宽限期设计）保护不到任何东西——旧 xray 实例上已无来自 relay 的连接。注释宣称的 'Grace period for existing connections' 与实际行为不符；若目标是平滑迁移，需要 relay 支持仅替换 target 而不重建 listener（如原地更新 TargetAddr），而非 Remove+Add。
- **证据**：manager.go:544-546 `// Stage 5: drain-old (grace period...)` + `time.Sleep(500 * time.Millisecond)`；relay_tcp.go Stop 先 cancel，handleConn `case <-r.ctx.Done(): clientConn.Close(); targetConn.Close()`
- **核实**：relay_tcp.go:89-95 TCPRelay.Stop 先 r.cancel() 再 wg.Wait()；handleConn 的 select 在 ctx.Done 分支（relay_tcp.go:331-336）立即 clientConn.Close()+targetConn.Close()。RemoveRule（forward_manager.go:326）内 mr.relay.Stop() 即已硬切断该端口全部活动连接，switch-forward 阶段本身就是全量断连；之后 Stage 5 的 time.Sleep(500ms)（manager.go:544-546，update.go:141-142 同）对 relay 流量无任何保护作用，'Grace period for existing connections' 注释与实际行为不符。作为设计声明与实现不一致（且切换本身仍能完成，只是非零停机），P3 恰当。

#### 51. [P3·confirmed] string 端口在 hotreload 链路上静默断裂：applyPortOffset 保留字符串类型，GetPortMapping 只做 float64 断言，映射丢失导致该 inbound 的转发规则永不切换

- **位置**：`pkg/xrayapi/hotreload/manager.go:297` · 维度：正确性 · 单元：XAPI
- **问题**：applyPortOffset(246-249) 对 string 端口写回 string（且 Sscanf 会把 "1000-2000" 区间截为 "1000+offset"，解析失败如 "env:PORT" 时静默保留原端口，新实例与旧实例撞端口）；而 GetPortMapping(296-305) 对新旧两份配置都用 `in["port"].(float64)` 断言，string 端口得到 0，生成 OldPort=0/NewPort=0 的映射或干脆 tag 匹配后端口为 0。SwitchForwardRules 找 "127.0.0.1:0" 必然无匹配而 skip，旧实例随后被停掉，该 inbound 上所有用户转发指向死端口，全程无任何错误返回。pass1 只报了 types.go:242 的 Sscanf 问题，本条是 hotreload 自己的独立端口解析链路。
- **证据**：manager.go:297 `port, _ := in["port"].(float64)`（304 同）与 manager.go:246-249 string 分支 `result["port"] = fmt.Sprintf("%d", oldPort+m.config.PortOffset)` 类型不一致
- **核实**：manager.go:297 与 304 均只做 in["port"].(float64) 断言，string 端口得 0；而 applyPortOffset:246-249 对 string 端口写回 string，两侧类型链路断裂属实。string 端口的 inbound 在 GetPortMapping 中产出 OldPort=0（或 tag 匹配后 NewPort=0）的映射，SwitchForwardRules 找 '127.0.0.1:0' 必无匹配（usermanager.go:921 生成的 TargetAddr 是真实端口）而静默 continue，旧实例随后被 Stop，该 inbound 全部用户转发指向死端口且全程无错误返回。附带断言也核实：fmt.Sscanf("%d") 对 '1000-2000' 解析出 1000 且 n=1/err=nil，区间被截为单端口再加 offset；对 'env:PORT' 类解析失败则静默保留原端口，新旧实例撞端口。与 pass1 报的 types.go:242 属不同文件的独立链路。

#### 52. [P3·confirmed] 所有 VMess/VLESS Build* 方法校验 uuid 非空后将其完全丢弃，产出的 User 无 Account，VLESS Config 还缺 Decryption

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:45` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：BuildVMess/BuildVMessWS/BuildVMessTCPWithTLS/BuildVLESSReality/BuildVLESSWSTLS 都只 `if uuid == "" return err`，之后构造 `protocolpb.User{Email, Level}` 从不设置 user.Account（对比 types.go buildVMessUser 会把 vmess.Account{Id: uuid} 包进 TypedMessage）。xray-core 反序列化时 VMess/VLESS user 必须携带 Account 才能注册验证器，无 Account 的 inbound 要么 AddInbound 报错要么无任何可认证用户；BuildVLESSReality 的 realityPublicKey/realityShortID 也只进了会被 buildReceiverSettings 丢弃的 streamSettings。此外 vlessinboundpb.Config 未设 Decryption="none"，xray 校验 decryption 为空会拒绝配置。builder_test.go 只验证序列化往返，从未检查 Account 字段，测试给了虚假信心。当前 builder 无任何外部调用者，故降为 P3，但若按包注释作为 gRPC 动态加 inbound 的正式构建器启用即是 P1。
- **证据**：builder.go:39-67 BuildVMess 中 uuid 仅出现在 line 40 的空检查；user := &protocolpb.User{Email: email, Level: 0} 无 Account；vlessConfig := &vlessinboundpb.Config{Clients: ...} 无 Decryption；grep 全仓库无 builder 包导入者
- **核实**：逐一核实成立。builder.go 五个 VMess/VLESS Build* 方法(40/71/102/133/165 行)均只做 uuid 空检查后完全丢弃该参数,构造的 protocolpb.User 仅有 Email/Level 无 Account——uuid 在产出的配置里任何位置都不出现,这一缺陷不依赖上游语义即成立(builder 自身 Trojan 路径 221-225 行正确设置 Account,types.go buildVMessUser 377-380 行同样,形成包内对照)。vlessinboundpb.Config 确未设 Decryption,而生成 pb 有该字段(config.pb.go:113),且仓库内生产路径 inbound_adapter.go:326 与 types.go:455-457、params_vless.go:64-65 均把 decryption="none" 作为必填缺省,构成仓库内材料佐证。BuildVLESSReality 的 realityPublicKey/shortID 进入的 streamSettings 被 buildReceiverSettings 明确丢弃(builder.go:299 TODO 注释自认未处理)。builder_test.go 存在且全仓库无任何包外导入者(grep 验证),P3 降级合理。

#### 53. [P3·confirmed] buildVMessUser 对缺失/非法 id 兜底为全零 UUID 账户，且 buildVMessInboundConfig 吞掉 user 构建错误静默丢用户

- **位置**：`pkg/xrayapi/types/types.go:360` · 维度：安全 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：clientMap 中 id 缺失或不是 string 时（如上游字段名拼错、id 为数字），buildVMessUser 不报错而是构造 `vmess.Account{Id: "00000000-0000-0000-0000-000000000000"}`——一个公开可猜的固定凭据被静默注入 inbound，任何知道全零 UUID 的客户端即可认证。同时调用方 buildVMessInboundConfig(325-328) 对 buildVMessUser 的 error 一律 `continue`，用户被静默丢弃，配置成功返回但缺人。两处都应显式返回错误。对比 buildVLessInboundConfig(404) 的处理：id 缺失时仅不设 Account（也有问题但至少不伪造凭据）。
- **证据**：types.go:358-362 `} else { account = &vmess.Account{ Id: "00000000-0000-0000-0000-000000000000" } }`；types.go:325-328 `user, err := buildVMessUser(clientMap); if err != nil { continue }`
- **核实**：两处代码事实均核实:types.go:358-362 在 id 缺失或非 string 时确实兜底构造 Id="00000000-0000-0000-0000-000000000000" 的 vmess.Account 并注入配置;types.go:325-328 对 buildVMessUser 的 error 一律 continue 静默丢用户。触发路径真实:Executor.AddInboundNative(exec_runner.go:598)接受任意 native JSON,id 拼错或为数字即触发。伪造固定凭据与静默丢用户是不依赖上游细节的代码级缺陷,且仓库内材料(types.go 用户管理链路、adapter 均以 id 作为写入 Account 的认证凭据)佐证该 id 即凭据。与 buildVLessInboundConfig(404-418 行 id 缺失时仅不设 Account)的对照准确。附注:由于兜底存在,buildVMessUser 实际只在 proto.Marshal 失败时才返回 error,325-328 的 continue 几乎是死分支,但吞错模式本身属实。P3 合理。

#### 54. [P3·confirmed] grpc.Client 的 Dial/Invoke 全部使用无超时的 context.Background()，xray 无响应时调用方永久阻塞；RemoveInbound 还忽略 json.Marshal 错误

- **位置**：`pkg/xrayapi/grpc/client.go:44` · 维度：资源 · 单元：XAPI
- **问题**：AddInboundWithProto 与 RemoveInbound 都以 `metadata.NewOutgoingContext(context.Background(), ...)` 发起 conn.Invoke，无 deadline；grpc.Dial 也未设 WithBlock/超时。若 62789 端口被半开连接、防火墙 DROP 或对端 hang，调用 goroutine 无限期阻塞且无法取消。RemoveInbound line 60 `reqData, _ := json.Marshal(...)` 忽略错误。鉴于该包目前无任何导入者（grep 全仓库为空）且 raw JSON Invoke 本身无法工作（pass1 已报），本条按 P3 记录：若保留该客户端应加 context 参数与超时，否则整包应删除。
- **证据**：client.go:44 `ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs())`（68 同）；client.go:38/62 `grpc.Dial(c.address, grpc.WithTransportCredentials(insecure.NewCredentials()))` 无超时选项；client.go:60 `reqData, _ := json.Marshal(...)`
- **核实**：全部代码事实核实属实:client.go:44 与 68 行均用 metadata.NewOutgoingContext(context.Background(), ...) 无 deadline 发起 conn.Invoke;38/62 行 grpc.Dial 无超时/WithBlock 选项;60 行 reqData, _ := json.Marshal 忽略错误。grep 验证全仓库无任何文件导入 pkg/xrayapi/grpc 包,与 finding 前提一致。细微修正:grpc.Dial 默认非阻塞,Dial 本身不会挂起,真正的无限阻塞发生在无 deadline 的 Invoke 上(对端半开/挂起时永久等待且不可取消),finding 主体结论不受影响。protocolRelated=false,不涉上游语义。P3(建议加 context 参数或删除死代码)恰当。

#### 55. [P3·confirmed] Manager.Rollback 是恒返回 nil 的 no-op stub；Config 的 HealthCheckTimeout/HealthCheckInterval/GRPCAddress/UseNewBinary 四字段从未被读取

- **位置**：`pkg/xrayapi/hotreload/manager.go:376` · 维度：架构 · 单元：XAPI
- **问题**：Rollback(376-384) 无论 RollbackInfo 内容如何都直接 return nil，调用方会误以为回滚成功，真正的回滚逻辑在 RollbackUpdate 里，两个同名概念并存易误用。Config 中 HealthCheckTimeout/HealthCheckInterval 被 cmd/test_hotreload/main.go:86-87 赋值但 HealthCheck 硬编码 sleep(500ms) 从不读取；GRPCAddress、UseNewBinary 全包无引用（ExecuteHotUpdate 用参数 newBinaryPath 判断，不看 UseNewBinary）。hotupdate.createNewExecutor(update.go:271) 的 grpcAddress 形参同样未被使用。这些死接口让调用方以为可配置健康检查与 gRPC 地址，实际全部无效。
- **证据**：manager.go:376-384 `if result.RollbackInfo == nil { return nil } ... return nil`；manager.go:361 硬编码 `time.Sleep(500 * time.Millisecond)`；grep 全仓库 UseNewBinary/hotreload Config.GRPCAddress 无读取点；update.go:271 grpcAddress 参数体内未出现
- **核实**：全部事实核实属实:manager.go:376-384 Rollback 两个分支均 return nil,是恒成功 no-op(注释自称'由调用方处理回滚',且当前全仓库无调用者,误用属潜在风险而非现实事故),真正回滚逻辑在同文件 634 行 RollbackUpdate,两个同名概念并存属实;HealthCheck(358-372)硬编码 time.Sleep(500ms) 且从不读 HealthCheckTimeout/HealthCheckInterval,而 cmd/test_hotreload/main.go:86-87 确在赋值这两个字段;hotreload 包内对 m.config 的读取仅涉及 OldConfigPath/NewConfigPath/OldBinaryPath/PortOffset,GRPCAddress 与 UseNewBinary 无任何读取点(grpc 地址由 62789+PortOffset 硬算);hotupdate/update.go:271 createNewExecutor 的 grpcAddress 形参在函数体内确未出现(体内自行用 portOffset 构造地址)。死配置接口误导调用方的定性成立,P3 合理。

#### 56. [P3·confirmed] SaveAccount 直接 os.WriteFile 非原子写 account.json，损坏后 LoadAccount 返回硬错误且无自愈路径，签发/续期永久失败

- **位置**：`pkg/certmgmt/lego/account_store.go:53` · 维度：错误处理 · 单元：CERT
- **问题**：cert_store.go 为证书文件实现了 atomicWrite（tmp+rename），但 SaveAccount 写 account.json 和 key 文件用的是裸 os.WriteFile：进程在写入中途崩溃/磁盘满会留下截断的 account.json。之后 LoadAccount 对 unmarshal 失败返回 ErrStorageIO 错误（account_store.go:82-84）而不是当作『无账号』，Issue/Renew 在第 41/128 行直接把该错误往上抛——既不会重新注册也不会重建文件，自动续期从此每分钟报错一次，直到人工删除损坏文件。与账号数据的重要性（丢了顶多重新注册，LE 注册免费幂等）相比，『损坏即永久卡死』是最差的失败模式。修法：SaveAccount 复用 atomicWrite；LoadAccount 对 JSON 损坏可降级返回 (nil,nil) 触发重新注册（或至少在错误里提示可删除文件恢复）。
- **证据**：account_store.go:53 `os.WriteFile(accountPath, jsonBytes, 0600)`、:60 `os.WriteFile(keyPath, ...)` 均无 tmp+rename；对比 cert_store.go:35-41 atomicWrite。account_store.go:82-84 unmarshal 失败返回 error；issuer.go:41-44 直接 return。
- **核实**：仓库内直证且行号准确：account_store.go:53 `os.WriteFile(accountPath, jsonBytes, 0600)`、:60 key 文件同样裸写，无 tmp+rename；对照 cert_store.go:35-41 同包已有 atomicWrite 且 SaveCert 全部使用。LoadAccount:82-84 对 unmarshal 失败返回 ErrStorageIO 硬错误而非视作无账号；Issue（issuer.go:41-44）与 Renew（issuer.go:128-131）直接上抛，无重注册/重建自愈路径；叠加 renew_scheduler 每分钟重扫，损坏的 account.json 会导致续期持续报错直至人工删文件。半途崩溃/磁盘满留下截断 JSON 的触发条件成立。修复建议（复用 atomicWrite、损坏降级为重新注册）合理。P3 恰当。

#### 57. [P3·confirmed] ObtainNewCert 无条件全新签发，不检查既有证书新鲜度，center 侧批量下发时易撞 LE duplicate-certificate 限流（5 张/周）

- **位置**：`pkg/certmgmt/service/rpc_adapter.go:26` · 维度：错误处理 · 单元：CERT · ⚠️协议相关（需人工核对上游）
- **问题**：ObtainNewCert 直接 `m.Issue(context.Background(), []string{d})`，不看本地是否已有远未到期的同域证书。center 的 HTTP cert_handler（pkg/http/cert_handler.go:30）会把 ObtainNewCertReq 广播到多个 end node，管理员重复点击或脚本重试都会对同一域名重复走完整 ACME 签发。Let's Encrypt 的 duplicate certificate 限额为每周 5 张（相同域名集合），几次重试后该域一周内无法再签发，连正常续期也被波及。end_node_inbound.go:233 试图用 GetCertInfo==nil 做前置检查（该检查又因 typed-nil bug 失效），说明『已有有效证书则跳过』本就是预期语义，应下沉到 ObtainNewCert 内实现：已有证书且剩余有效期大于续期窗口时直接返回成功（或改走 RenewDomain）。另外 context.Background() 使 gRPC 调用方超时后服务端仍继续长达数分钟的 ACME 流程。
- **证据**：rpc_adapter.go:25-28 无任何既有证书检查；pkg/rpc/server/end_node_cert.go:11 RPC handler 直接透传；pkg/http/cert_handler.go:30 广播到多节点。
- **核实**：rpc_adapter.go:26 直接 m.Issue(context.Background(), []string{d})，无既有证书新鲜度检查；LegoIssuer.Issue（issuer.go:27-103）内部也无跳过逻辑，每次都完整走 ACME Obtain。end_node_cert.go:11 RPC handler 直接透传，cert_handler.go:30 经 ReqToMultiEndNodeServer 广播到全部注册节点，重复触发路径真实存在。typed-nil 旁证属实：GetCertInfo 返回 interface{} 包裹 *CertificateRecord，未找到时为 typed-nil，end_node_inbound.go:233 的 cert==nil 恒 false，说明『已有证书则跳过』的预期语义确实存在且已失效。LE 每周 5 张重复证书限额由仓库内 docs/certmgmt-design.md:452（'ACME 限流（Let's Encrypt 每周 5 个重复域名）'）确证，满足 protocolRelated 的仓库内材料要求。context.Background() 使 gRPC 调用方超时后服务端继续 ACME 流程的表述也与代码一致。P3 恰当：属运营风险，需管理员重复触发+多节点广播叠加才达限额。

#### 58. [P3·confirmed] 多 SAN 签发只以 domains[0] 落盘一份 meta，其余 SAN 域名对 GetCertFiles/GetCert/续期完全不可见

- **位置**：`pkg/certmgmt/lego/issuer.go:306` · 维度：正确性 · 单元：CERT
- **问题**：Manager.Issue 接受 []string 多域名并会把全部 Domains 传给 lego Obtain（issuer.go:290 签出多 SAN 证书），但 saveCertResult 只构造 `Domain: req.Domains[0]` 一条 record，SaveCert 只按 domains[0] 命名写四个文件。之后 GetCertFiles(其它SAN域) 走 LoadCert(该域) 找不到 meta 返回 false——http server、hysteria 容器按 SAN 域名取证书都会失败；自动续期 ListCerts 也只见 domains[0]。当前生产调用链（ObtainNewCert）只传单域所以问题潜伏，但 Issue 的公开签名承诺多域支持，一旦 cmd/server 或未来调用方传多域即触发。pass1 只报了多 SAN 的锁问题（domains[0] 加锁），未覆盖存储/检索不可见这一正确性缺陷。修法：为每个 SAN 域写 meta（指向同一组 crt/key 文件），或收窄 Issue 签名为单域。
- **证据**：issuer.go:305-310 `record := &domain.CertificateRecord{ Domain: req.Domains[0], ... }`；cert_store.go:50 `certPaths(basePath, record.Domain)` 仅按单域名生成路径；rpc_adapter.go:15-21 GetCertFiles 按单域 LoadCert。
- **核实**：代码证据全部属实：issuer.go:288-298 obtainCert 把全部 req.Domains 传入 lego Obtain 签出多 SAN 证书，但 saveCertResult（issuer.go:305-310）只构造 Domain: req.Domains[0] 一条 record；cert_store.go:24-32 certPaths 仅按 record.Domain 单域名生成四个文件路径，SaveCert 只写这一份 meta。其余 SAN 域名走 GetCertFiles→GetCert→LoadCert（按该域名查 meta.json）返回 nil→false，ListCerts 扫描 meta 文件也只见 domains[0]，续期调度对其余 SAN 不可见。『问题潜伏』判断准确：全仓库 Manager.Issue 生产调用方只有 rpc_adapter.go:26 与 hysteria/container.go:208，均传单域切片，当前不触发；但 Issue 公开签名接受 []string 多域，一旦传多域即触发存储/检索不可见缺陷。P3（潜伏正确性缺陷、无现网触发路径）恰当。

#### 59. [P3·confirmed] 账号目录用 email 与 CA host 原样拼进 filepath.Join，未过滤 '..'/分隔符，构成 cert_store 之外第二个未被 pass1 覆盖的路径穿越落盘点

- **位置**：`pkg/certmgmt/lego/account_store.go:29` · 维度：安全 · 单元：CERT
- **问题**：accountDirPath 直接 filepath.Join(basePath, accountsFolder, serverPath, email)（account_store.go:29），keysDir/keyPath 又用 email+".key"（59、104）。email 未做任何 sanitize；serverPath 仅把 CA host 里的 ':'→'_'、'/'→分隔符（28），也没有过滤 '..'。若 email 含 '../' 或绝对路径片段，SaveAccount/GetOrCreateAccountKey 会把 account.json 与账号私钥写到 basePath 之外的任意目录（私钥落盘 0600 但位置可控）。pass1 只在 cert_store.go:19 的 domainToFilename 报了域名路径穿越，这里是账号存储层同类但独立的落盘/写私钥点，属于 lens 要求『核实所有落盘点』的遗漏。email 主要来自服务端配置(Config.Email)、可控性弱于 gRPC 传入的 domain，故定 P3，但仍应与 domain 一样统一走白名单/filepath.Clean 校验。
- **证据**：account_store.go:28-29 strings.NewReplacer(":","_","/",sep).Replace(host) + filepath.Join(...,email)；59 filepath.Join(keysDir, email+".key")；无 '..'/clean 校验
- **核实**：account_store.go:28-29 属实：serverPath 仅做 ':'→'_'、'/'→分隔符替换，email 原样 filepath.Join；59 与 104 再以 email+".key" 拼 keyPath，全程无 '..' 过滤/Clean/白名单。email 含 "../" 时 SaveAccount/GetOrCreateAccountKey 确会把 account.json 与 0600 私钥写到 basePath 之外。可控性核实：buildIssueRequest(manager.go:172) 的 Email 只来自本地 Config.Email，CAURL 恒为空（effectiveCAURL 落到 LE 生产 URL），无 gRPC 直接注入路径，属可信配置侧的防御纵深缺失而非直接可达攻击面——与 finding 自评一致，P3 恰当。作为 cert_store.go domainToFilename 之外独立的落盘点，pass1 确未覆盖。

#### 60. [P3·confirmed] atomicWrite 在 os.Rename 失败时不清理 <path>.tmp，残留含私钥 PEM 的临时文件；且失败后目标文件保持旧值使『原子写』语义在错误路径退化

- **位置**：`pkg/certmgmt/lego/cert_store.go:40` · 维度：资源 · 单元：CERT
- **问题**：atomicWrite 先 os.WriteFile(tmp)（37）再 os.Rename(tmp,path)（40）。若 Rename 失败（跨设备、目标目录权限、磁盘满等），函数返回错误但 <path>.tmp 不被删除。对 .key/.json 而言，tmp 里是完整私钥 PEM（0600），会长期残留在 certificates/ 目录下（形如 domain.key.tmp），既是敏感物料泄漏面，又因为固定命名会与后续同域写入互相干扰。同时错误路径上调用方(SaveCert)拿到 err 直接返回，磁盘保持旧值——这本身是期望的原子性，但残留 tmp 需要 defer os.Remove(tmp) 兜底清理才完整。
- **证据**：cert_store.go:35-41 tmp:=path+".tmp"; os.WriteFile(tmp,...); return os.Rename(tmp,path) —— Rename 失败分支无 os.Remove(tmp)
- **核实**：cert_store.go:35-41 属实：os.WriteFile(tmp) 成功后 os.Rename 失败时直接 return err，无 defer os.Remove(tmp)，<domain>.key.tmp（内容为完整私钥 PEM）残留在 certificates/ 目录。tmp 以 0600 写入且目录 0700，泄漏面有限但确实是敏感物料残留 + 错误路径清理缺失。注意 finding 中『与后续同域写入互相干扰』的说法不准确——固定 tmp 名下次 os.WriteFile 会截断重写，不构成功能干扰——但主体缺陷（Rename 失败不清理）成立，P3 恰当。

#### 61. [P3·confirmed] AddCertificates 导入外部证书时不校验私钥可解析、也不校验 key 与 cert 匹配，坏的密钥对会被落盘并被代理热加载，运行期 TLS 握手才以不透明错误失败

- **位置**：`pkg/certmgmt/service/rpc_adapter.go:42` · 维度：正确性 · 单元：CERT
- **问题**：AddCertificates 仅校验 cert/key 非空(38)，随后 ParseCertNotAfter(certData)(42) 只解析证书 NotAfter，从不解析 keyData、也不做 tls.X509KeyPair 之类的 key↔cert 匹配校验，就直接 SaveCert 覆盖到 canonical 路径(60)。若传入的私钥无法解析或与证书不配对（字段填反、粘贴出错等），错误证书对会被写入 {Path}/certificates/ 并被 xray/hysteria/mihomo 通过文件热加载吃进去，最终在运行期握手阶段才以底层库的不透明报错失败，且已覆盖掉之前可用的证书、无回滚。导入路径应先 tls.X509KeyPair(certData,keyData) 验证配对再落盘。end_node_cert.go:21 的 TransferCert 直接把 gRPC 入参透传到此。
- **证据**：rpc_adapter.go:38-60 仅 len 检查 + ParseCertNotAfter(certData)，无 key 解析/无 X509KeyPair 配对校验，直接 SaveCert 覆盖
- **核实**：rpc_adapter.go:37-60 属实：仅 len(certData)/len(keyData) 非空检查(38-40) + ParseCertNotAfter(certData)(42) 解析证书过期时间，keyData 从不解析、无 tls.X509KeyPair 配对校验，直接 SaveCert 覆盖 canonical 路径（doc 注释自述 're-import overwrites in place'）。调用链核实：end_node_cert.go:19-25 TransferCert 把 gRPC 入参 Domain/KeyDatas/CertData 原样透传，SaveCert 及下游（xray/hysteria/mihomo 文件热加载）无任何配对校验兜底。坏密钥对覆盖掉原可用证书且无回滚，故障延迟到运行期握手才暴露，属真实缺陷；导入方为可信管控面、多为误操作场景，P3 恰当。

#### 62. [P3·uncertain] 零长度 UDP 数据报被静默丢弃且不刷新会话活跃时间：合法的 0 字节 keepalive 会话仍会被 idle GC 掐掉

- **位置**：`pkg/proxy/forward/relay_udp.go:181` · 维度：正确性 · 单元：FWD · ⚠️协议相关（需人工核对上游）
- **问题**：readLoop 对 n==0 的数据报直接 continue（:181-183），既不建会话也不转发；forwardUpstream 对 len(payload)==0 同样直接 return（:293-295）。零长度 UDP 数据报是合法的（RFC 768 允许，部分协议用作 NAT keepalive/探活）。后果：a) 客户端只发 0 字节 keepalive 维持的会话，lastActive 不会刷新，60s 后被 gcLoop 回收，NAT 映射语义被破坏；b) 首包为 0 字节时根本不建会话。至少应让 0 字节包刷新已存在会话的 lastActive 并透传给 upstream。
- **证据**：relay_udp.go:181-183 `if n == 0 { continue }`；:293-295 `if len(payload) == 0 { return }`；lastActive 仅在成功 Write 后更新（:325）。
- **核实**：代码行为完全属实：relay_udp.go:181-183 对 n==0 直接 continue（不建会话、不透传），forwardUpstream:293-295 对空 payload 直接 return，lastActive 仅在成功 Write 后（:325）或 downstream 回包后（:377）刷新，gcLoop 按 sessionIdleTimeout（默认 60s）回收。触发路径真实存在。但该条标记 protocolRelated，其影响论证依赖'合法客户端用 0 字节 UDP 数据报做 NAT keepalive'这一上游协议行为；仓库内材料无法确证——wiki/docs 无相关记载，integration_test.go:395 的 UDP keepalive 测试用的是非空 payload（"keep"），说明本仓库自身的 keepalive 约定并不依赖零长度包。按核实规则，第三方/协议行为无仓库佐证时最高 uncertain。实际危害是否存在取决于真实客户端是否发纯 0 字节保活，仓库内无证据。

#### 63. [P3·uncertain] builder.BuildShadowsocks 与 types 路径同病：任意 method 一律构造 shadowsocks_2022.ServerConfig

- **位置**：`pkg/xrayapi/grpc/builder/builder.go:255` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：第一轮只报了 types.go:537 的 buildShadowsocksInboundConfig；builder.BuildShadowsocks 是另一个独立构造点，同样不区分 method：aes-128-gcm/chacha20-ietf-poly1305 等传统方法也被塞进 ss2022pb.ServerConfig（TypedMessage 类型名 xray.proxy.shadowsocks_2022.ServerConfig），xray 端 ss2022 实现校验 method 必须为 2022-blake3-* 系列且 Key 必须是合法 base64 PSK，传统方法/明文密码会被拒。两处需要一起修（按 method 前缀分发到 shadowsocks 或 shadowsocks_2022），只修 types 路径会留下 builder 死角。
- **证据**：L255-262: `ss2022Config := &ss2022pb.ServerConfig{Method: method, Key: password, ...}` 后直接 `NewTypedMessageFromProto("xray.proxy.shadowsocks_2022.ServerConfig", ...)`，无 method 分发。
- **核实**：代码事实完全成立：builder.go:255-262 无 method 分发、任意 method 连同明文 password 直塞 ss2022pb.ServerConfig，且 upstream 镜像中 proxy/shadowsocks 与 proxy/shadowsocks_2022 确为两套独立 proto，profilegen/shadowsocks.go:14-29 也显示项目同时支持传统方法。但核心危害断言（xray ss2022 实现校验 method 必须 2022-blake3-*、Key 必须合法 base64 PSK、否则拒绝）是上游运行时行为，仓库内无 docs/wiki/测试可确证——proto 本身对 method 只是自由字符串。第一轮对完全相同断言的 item 84（types.go:537）已按同一规则判 uncertain（见 docs/review-2026-07-10/L2-domain.md:701），本条为其 builder 侧镜像，按规则同样上限 uncertain。另注意 builder 包无生产调用方，实际影响面小于 types 路径。

#### 64. [P3·uncertain] buildReceiverConfigProto 把 listen="::" 当作 "0.0.0.0" 同类直接丢弃，IPv6 监听意图静默降级为 IPv4 any

- **位置**：`pkg/xrayapi/types/types.go:257` · 维度：正确性 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：`if listen != "" && listen != "0.0.0.0" && listen != "::"` 把 "::" 排除在 Listen 字段设置之外。ReceiverConfig.Listen 为 nil 时 xray 回落 net.AnyIP（0.0.0.0），Linux 下 Go 对 0.0.0.0 绑定 IPv4；显式要求双栈/IPv6 监听的 "::" 配置被静默改成 IPv4-only，IPv6 客户端连不上且无任何日志。"::" 应被 ParseIP 解析后正常写入 Ip 字段（16 字节 AnyIPv6）。与项目"转发默认双栈 best-effort"的约束方向相悖。
- **证据**：L257: 条件式将 "::" 与 "0.0.0.0" 同等对待跳过；xray 端 Listen==nil 默认 AnyIP=0.0.0.0。
- **核实**：代码事实确凿：types.go L257 条件式确将 "::" 与 "0.0.0.0" 同等排除，显式 "::" 不会写入 ReceiverConfig.Listen（grpc/builder/builder.go:291 有同样模式）。但危害结论整条依赖上游断言——"xray 对 Listen==nil 回落 net.AnyIP(0.0.0.0)、Go 绑 0.0.0.0 为 IPv4-only"：仓库无 xray-core 依赖、无 vendor，wiki/docs/测试均无该 proto 路径缺省行为的记载（第一轮 L2 #2 验证的是本仓库自身 forward 的 net.Listen，不是 xray 内部）；若上游 nil 缺省实为双栈 any，丢弃 "::" 即无害。另外 grep 全仓库无任何生产调用向该函数传 "::"（inbound.Listen 来自用户配置，路径存在但目前纯潜在）。按 protocolRelated 规则上游行为未获仓库内材料确证，最高 uncertain。

#### 65. [P3·uncertain] buildStreamConfigProto 在 security=tls/xtls/reality 但对应 settings 块缺失时仍设置 SecurityType，产出带安全类型却无 SecuritySettings 的 StreamConfig

- **位置**：`pkg/xrayapi/types/types.go:608` · 维度：错误处理 · 单元：XAPI · ⚠️协议相关（需人工核对上游）
- **问题**：line 605-608：只要 security 字段是 tls/xtls 就先写 streamConfig.SecurityType，而 tlsConfig 的构建与 append 整个包在 `if tlsSettings, ok := ...` 内；tlsSettings 缺失时返回的 StreamConfig SecurityType 非空、SecuritySettings 为空。reality 分支（696-700）同样：security=reality 但无 realitySettings 时只留 SecurityType。xray-core 会按 SecurityType 实例化默认配置——TLS 无证书/Reality 无私钥 dest，inbound 或者创建失败或者握手全部失败，且错误发生在 xray 侧而非本层，难以定位。应在 settings 缺失时直接返回错误。该路径被 pkg/proxy/containers/xray/grpc_client.go 的生产 gRPC 加 inbound 流程使用。
- **证据**：types.go:605-610 `if security, ok := ...(security == "tls" || security == "xtls") { streamConfig.SecurityType = "xray.transport.internet.tls.Config"; if tlsSettings, ok := ...` —— SecurityType 赋值在 settings 存在性检查之外；reality 分支 697 同构
- **核实**：代码事实完全属实:types.go:605-608 SecurityType 赋值在 tlsSettings 存在性检查(610 行 if)之外,reality 分支 696-699 同构;且触发路径真实——grpc_client.go:134 ParseInboundConfig→buildReceiverConfigProto:278→buildStreamConfigProto 为生产链路,inbound_adapter.go:522 只在 tlsSettings 非空时才写入该块,security=tls 而无任何 TLS 扩展字段时确会产生 security 有值、tlsSettings 缺失的输入,Executor.AddInboundNative 也接受任意外部 native JSON。但危害主张(xray-core 会按 SecurityType 实例化默认配置、TLS 无证书/Reality 无私钥导致创建或握手失败)完全依赖上游 xray-core 行为:go.mod/go.sum 无 xray-core 依赖,wiki 与 docs 均无相关实证材料,无法在仓库内确证空 SecuritySettings 在 xray 侧的具体后果,按 protocolRelated 规则最高只能 uncertain。防御性改进建议本身合理。

## 附录：verifier 判定 refuted（不计入）

| 单元 | 位置 | 标题 | refuted 理由 |
|------|------|------|-------------|
| UM | `pkg/proxy/usermanager/usermanager.go:945` | GetBindPort 非幂等：重启后重复 append BindPorts，preferred 端口失败改自动分配时旧端口永久残留 | 核心机制不成立：BindPorts 根本不持久化。pkg/store/user_store.go:43 明确注释『Fields not persisted (BindPorts, Extensions, Protocol) are left at zero values』，Save(:133-155) 的列清单无 bind_ports（仅 port_mappings 序列化落库），Load 后 BindPorts 恒为空。NewUserManagerWithStore(:184-190) 直接采用 Load 结果，故重启后 BindPorts 从空开始，GetBindPort 首次 append 不产生重复，『每次重启累积一个重复项并落库』与『旧端口永久残留（跨重启）』均不存在。残余真实内核较小：单进程生命周期内 ReleaseInboundPorts(:1391-1395) 删规则不清 BindPorts，之后 GetBindPort 会重复 append 同值端口（无害），仅 realloc 分支(:931-941)可能让内存中 BindPorts[0] 短暂指向死端口直至重启——与 finding 所述机制和量级不符，且 finding 的 P2 定级完全建立在被证伪的持久化累积上。 |
