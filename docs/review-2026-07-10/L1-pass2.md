# L1 契约与持久层 · 第二轮补充 review

> 第二轮独立 review（全程 Fable 5）：拿第一轮清单换角度找**新问题** + 挑战疑似误报。本文件为第一轮 `L1-core.md` 的**增量补充**，新问题经对抗性 verifier 核实，质疑经中立裁决者裁定。

## 第二轮统计

| 维度 | 数量 |
|------|------|
| 新发现·保留(confirmed+uncertain) | 49 |
| — confirmed | 43 |
| — uncertain | 6 |
| 新发现·refuted(剔除) | 1 |
| 新发现·unverified(verifier无结果) | 0 |
| 质疑·裁定第一轮误报/severity错(pass1-wrong) | 7 |
| 质疑·驳回(pass1-stands) | 0 |
| 质疑·uncertain | 0 |

| 新发现优先级 | P0 | P1 | P2 | P3 |
|------|----|----|----|----|
| 保留 | 0 | 2 | 10 | 37 |

## 对第一轮的质疑裁决

### ✅ 裁定第一轮有误（需在第一轮文档中撤销/调整）

| 单元 | 第一轮条目 | 质疑 | 建议 severity | 裁决理由 |
|------|-----------|------|--------------|---------|
| STO | `pkg/store/inbound_store.go:90` prettyPrintJSON 经 interface{}/float64 往返重排并可能破坏 native_json 数值精度，应改用 json.Indent | severity-too-high | P3 | 质疑成立。prettyPrintJSON(inbound_store.go:90-96)确实走 interface{}/float64 往返，但检查了 NativeJSON 的全部消费方：hysteria container.go:620、snell container.go:517、mihomo container.go:731(FromNative)、xray exec_runner.go:2489(extractPEMFromNativeJSON) 均通过 json.Unmarshal 语义解析，键序重排无影响。数值字段(port/alterId/timeout/window)取值远低于 2^53，float64 往返精确且 Go 对 ≤2^53 的整数输出无小数点/指数形式；大值字段(UUID/password/PEM/域名)均为字符串。当前 schema 下不存在可触发的数据损坏路径，改 json.Indent 属卫生级修复，降为 P3。 |
| STO | `pkg/store/db.go:25` busy_timeout/foreign_keys/cache_size PRAGMA 只作用于建池时的那一条连接，连接被替换后失效 | false-positive | P3 | 质疑成立。db.go:25 与 db.go:31 都落在同一个 pragma 循环(25-36 行)内，两条 pass1 记录是同一问题的重复。且失效场景无现实触发路径：db.go:23 SetMaxOpenConns(1)，全仓库 grep 确认无 SetConnMaxLifetime/SetConnMaxIdleTime/SetMaxIdleConns，缺省 MaxIdleConns=2>=1，唯一连接用完总回 idle 池；database/sql 仅在 driver.ErrBadConn 或超 lifetime 时丢弃连接，modernc.org/sqlite v1.46.1 为进程内纯 Go 驱动，错误以普通 error 返回，不触发连接淘汰。进程生命周期内 PRAGMA 持续生效(WAL 另为文件级持久属性)。按重复+不可触发处理，保留另一条作 P3 备忘即可。 |
| STO | `pkg/store/db.go:25` busy_timeout/foreign_keys/cache_size PRAGMA 只作用于建池时的那一条连接，连接被替换后失效 | false-positive | P3 | 质疑成立，与 idx 1 同一结论。行号 25 和 31 都在 db.go 的同一 pragma 循环(25-36 行)内，是完全同一问题的重复记录，应合并为一条。代码证据同 idx 1：MaxOpenConns(1)+无任何 lifetime/idle 设置意味着建池时那条连接永不被主动回收，modernc sqlite 无 ErrBadConn 来源，PRAGMA 在当前配置下进程内始终生效。作为独立 P2 正确性缺陷不成立，合并后保留一条 P3 防止未来改动连接池参数时踩坑。 |
| STO | `pkg/store/user_store.go:45` 21 列 SELECT+Scan+expiry 双格式解析+port_mappings 容错在 Load/Get/ListByGroup 三处手工复制 | false-positive | P3 | 质疑成立。user_store.go:45 是 Load 的 SELECT 语句，:67 是同一个 Load 函数内的 rows.Scan 调用，两个行号指向同一份代码、描述同一问题。三处复制本身属实(Load 44-108、Get 173-225、ListByGroup 228-290 各自重复 21 列列表、expiry 双格式解析、port_mappings 容错解析)，但这是一个 finding 被记了两次，应合并保留一条 P3。 |
| STO | `pkg/store/user_store.go:97` pkg/store 使用 stdlib log.Printf，绕过项目统一的 pkg/log | false-positive | P3 | 质疑成立。grep 确认 pkg/store 内 stdlib log 的使用点恰为 tx.go:19 和 user_store.go:97/220/279 四处，是同一个包级问题(store 包未接入 pkg/log)，修复动作单一(统一路由到 pkg/log)。user_store.go:97 这条与 tx.go:19 那条是同一问题的重复记录，应合并保留一条 P3。 |
| CTR | `pkg/proxy/core/params/defaults.go:370` crypto/rand 失败时静默返回可预测的时间种子"密码"，SS-2022 分支还会产出非法密钥 | severity-too-high | P3 | defaults.go 第 29-30 行确认导入 crypto/rand；第 368-371、382-385 行确实有 err!=nil 时返回 fmt.Sprintf("fallback-%x", time.Now().UnixNano()) 的分支。但 go.mod 声明 go 1.24.0 + toolchain go1.24.4，任何能构建本模块的工具链都 ≥ Go 1.24，而 Go 1.24 起 crypto/rand.Read 的契约是永不返回错误（熵源不可用时运行时直接崩溃进程），故该 err 分支为不可达死代码，第一轮描述的"可预测密码/非法 SS-2022 密钥"路径实际不会发生。质疑成立，降为 P3：建议删除死分支并修正第 377-380 行仍在描述该 fallback 的误导性注释。 |
| SUB | `pkg/proxy/core/subscription/converter.go:155` ParseRuleParam 丢弃第4段规则参数（如 no-resolve/src），Clash 规则语义被改写 | severity-too-high | P3 | 缺陷本身属实但严重度确应下调为 P3。代码证据：converter.go:154-165 ParseRuleParam 用 strings.Split(param, ",") 后只取 parts[0..2]，第 4 段（no-resolve/src 等）被静默丢弃。但影响面有限：调用点仅 uri_convert.go:92（HTTP query 自定义 rule 参数）和 http.go:226（rules_url 拉取），不触及任何内置规则路径；输出端 converter/clash.go:1002/:1100 按 type,value,policy 三段渲染，规则仍写入最终配置且合法，mihomo 可正常加载，节点可用。丢 no-resolve 的后果只是 IP 类规则对域名请求多做 DNS 解析（边缘场景可能改变匹配），无崩溃、无配置拒载、无连通性破坏，属语义细节偏移。且 pass1 对同一缺陷已有一条 P3(confirmed) 重复条目，同一缺陷应统一严重度，P2 偏高，定 P3。 |

## 第二轮新发现（保留条目）

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P1 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/defaults.go:256` | PEM 共享文件设计与 cert_source=pem 可删除语义冲突：删除一个 inbound 会摧毁共用同一 PEM 的其他 inbound 的证书文件 |
| 2 | P1 | ✓⚠️协议 | 安全 | SUB | `pkg/proxy/core/subscription/converter/clash.go:700` | fetchClashTemplate 把第三方 sub-converter 返回的任意 YAML 原样作为用户 Clash 配置下发（rules/dns/proxy-providers 注入面） |
| 3 | P2 | ✓⚠️协议 | 正确性 | CTR | `pkg/proxy/core/params/defaults.go:126` | SS: caller 自带 password + 缺省填入 SIP022 cipher，产出非法密钥材料的监听配置 |
| 4 | P2 | ✓ | 正确性 | CTR | `pkg/proxy/core/inbound/default.go:23` | NewDefaultInbound 把 port=0 静默改写为 10000、空 tag 改写为 "default-inbound"，掩盖调用方错误并与 protocolparams 的 port=0 语义冲突 |
| 5 | P2 | ✓ | 错误处理 | CTR | `pkg/proxy/core/params/defaults.go:196` | caller 提供的 certificate/key PEM 内容零校验直接落盘：非 PEM、cert/key 不匹配、超大内容全部晚失败于容器 reload |
| 6 | P2 | ✓⚠️协议 | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/params_ss.go:31` | SS-2022 caller 自带 password 不校验 base64 形状与密钥长度，违背本包'早失败'原则（与 TUIC uuid 校验不对称） |
| 7 | P2 | ✓ | 架构 | CON | `pkg/proxy/core/container/base.go:288` | BaseContainer 状态机与 embedder 覆盖的 Start/Stop 静态派发脱钩：xray Executor 绕过基类后基类状态恒 Stopped，Version() 经 rpc 恒返回空、Reload() 必失败 |
| 8 | P2 | ✓ | 错误处理 | CON | `pkg/proxy/core/container/base.go:182` | Start 失败路径没有任何清理契约：runFunc 半执行的副作用（已启动的 goroutine/句柄）既不触发 stopFunc 也无失败回调，snell 实际因此泄漏 reconcile/事件 goroutine 且重复 Start 会双开 |
| 9 | P2 | ✓ | 正确性 | SUB | `pkg/proxy/core/subscription/uri_convert.go:259` | vmessNodeToSpec 不投影 SkipCertVerify 与 HeaderType，clash/surge 侧读取点永远读不到 |
| 10 | P2 | ✓ | 安全 | SUB | `pkg/http/sub_handler.go:166` | /sub 在 Info 级日志里打印完整节点 URI（含 password/UUID/PSK 明文），抵消模块内的日志脱敏 |
| 11 | P2 | ?⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/codec/hysteria2.go:92` | hy2 userinfo 含未转义冒号时密码被静默截断（userpass auth 场景取不到完整 auth） |
| 12 | P2 | ?⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/converter/clash.go:168` | 订阅节点无去重：cluster + ext_sub 合并后重名节点产生重复 Clash proxy name，mihomo 整份配置拒绝加载 |
| 13 | P3 | ✓ | 错误处理 | STO | `pkg/store/user_store.go:87` | Load 对单行 expiry_time 解析失败即整体返回错误，一行坏数据导致节点启动直接退出 |
| 14 | P3 | ✓ | 错误处理 | STO | `pkg/store/tx.go:18` | Commit 失败时 defer 会对已完结事务再调 Rollback，必然产生一条误导性的 'rollback failed' 日志 |
| 15 | P3 | ✓ | 资源 | STO | `pkg/store/manager.go:94` | InitLoginPasswords 启动期对全量缺口令用户串行 bcrypt，无进度日志与超时控制，升级后首次启动可阻塞数分钟 |
| 16 | P3 | ✓ | 并发 | STO | `pkg/store/migrate.go:33` | Migrate 无跨进程互斥且事务为缺省 DEFERRED，双进程并发迁移时后者以混乱的 DDL 错误失败 |
| 17 | P3 | ✓ | 架构 | STO | `pkg/store/user_store.go:22` | UserStore 接口的 Get/ListByGroup/Count 全仓库无生产消费方，且 Count/ListByGroup 对墓碑与空 target_group 的语义未定义 |
| 18 | P3 | ✓⚠️协议 | 测试缺口 | STO | `pkg/store/migrations/migrations_test.go:218` | v12-v14 三个迁移零断言：v14 RENAME COLUMN 的数据保留、v12 存量行默认值、v13 deletion_state 均未验证，且升级测试注释与实际行为不符 |
| 19 | P3 | ✓ | 正确性 | STO | `pkg/store/manager.go:99` | InitLoginPasswords 直接 UPDATE login_password 绕过 Save 与集群 hash 协议，导致 users.hash/updated_at 与行内容失配 |
| 20 | P3 | ✓ | 架构 | STO | `pkg/store/user_store.go:21` | UserStore 的 Delete/Get/ListByGroup/Count 四个方法全仓库零非测试消费者，tombstone 用户行没有任何硬删路径而无限累积 |
| 21 | P3 | ✓ | 错误处理 | STO | `pkg/store/tx.go:26` | WithTx 在 Commit 失败时 defer 仍对已终结事务调用 Rollback，必然产生 ErrTxDone 噪声日志掩盖真实提交错误 |
| 22 | P3 | ✓ | 并发 | STO | `pkg/store/node_groups_store.go:70` | store 层完全不透传 context：WithTx 调用方一律 context.Background()，全部 Query/Exec 无超时，MaxOpenConns(1) 下挂起操作使后续所有 DB 调用不可取消地排队 |
| 23 | P3 | ✓ | 测试缺口 | STO | `pkg/store/db_test.go:113` | 测试缺口：WithTx 的 panic 路径与 Commit 失败路径、Migrate 中途失败的恢复语义、单连接池并发行为均无任何用例 |
| 24 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/parser.go:163` | requireUint32 的 uint32 分支缺 >65535 上界检查，与其余全部数值分支不一致 |
| 25 | P3 | ✓ | 架构 | CTR | `pkg/proxy/core/params/protocolparams/protocolparams.go:106` | h2 传输是全协议不可达的死路径：文档称'used by vmess'但 parseVMess 拒绝 h2，且 "h2" 无 contracts.Transport 常量、IsValid 为 false |
| 26 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/transport.go:34` | "h2" 是 contracts.Transport 枚举外的伪值，且 "http"→"h2" 归一化大小写敏感，与全解析链的大小写不敏感约定相悖 |
| 27 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/transport.go:164` | optionalInt 用 Sscanf("%d") 解析字符串，接受尾随垃圾——"30s" 被静默解析为 30，恰好击穿 anytls 苦心设计的单位混淆防护 |
| 28 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/params_ss.go:85` | parseSSPluginOpts 的 plugin_tls 又一处手写布尔解析：大小写敏感且不认 "yes"，与 optionalBool 语义第三次分裂 |
| 29 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/contracts/inbound.go:26` | 三个 Validate（InboundSpec/Config/DefaultInbound）都不校验 Protocol.IsValid()，枚举校验器与枚举定义脱节 |
| 30 | P3 | ✓⚠️协议 | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/security.go:197` | autofillReality 不校验 caller 提供的 Reality 公私钥是否配对，错配密钥对静默通过导致全体客户端握手失败且无处报错 |
| 31 | P3 | ✓⚠️协议 | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/security.go:196` | Reality 公私钥对只校验各自格式，不校验公钥与私钥的数学一致性 |
| 32 | P3 | ✓⚠️协议 | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/security.go:152` | parseSignedInt 接受负值且无溢出保护：reality_max_time_diff=-5 或 20 位大数都能通过'必须为整数秒'校验 |
| 33 | P3 | ✓ | 正确性 | CTR | `pkg/proxy/core/params/protocolparams/transport.go:93` | optionalStringSlice 的 []string 分支不过滤空串且直接别名调用方切片，与 []any/逗号分隔分支语义不一致 |
| 34 | P3 | ✓ | 并发 | CON | `pkg/proxy/core/container/base.go:297` | Restart() 无条件补调 MarkRunning，可把仍在执行 runFunc 的 Starting 容器提前标为 Running，随后 Stop 会跳过停止钩子造成孤儿进程 |
| 35 | P3 | ✓ | 架构 | CON | `pkg/proxy/core/container/base.go:280` | StopChan 是零消费方的死 API，且语义残缺：Start 失败路径与 Stopped 态 Stop 永不 close 该 channel，按文档使用必然泄漏等待者 |
| 36 | P3 | ✓ | 资源 | CON | `pkg/proxy/core/container/manager.go:81` | LoadFromConfig 对同类型重复 entry（或重复调用）静默覆盖旧实例且不 Stop：被覆盖实例在 factory.New 时已订阅 UserManager 并启动 goroutine，直接泄漏且事件双份消费 |
| 37 | P3 | ✓ | 正确性 | CON | `pkg/proxy/core/container/registry.go:188` | RegisterFactory 对同类型重复注册静默覆盖，与 legacy RegisterContainer 的重复即报错语义相反，init() 注册冲突完全不可见 |
| 38 | P3 | ✓ | 架构 | CON | `pkg/proxy/core/container/interface.go:21` | Container.Init(config any) 的接口文档直接点名 *xray.ExecutorConfig，且 hotreload/hotupdate 确实借此以 xray 原生配置跨层调用，L1 抽象为具体实现留了类型后门 |
| 39 | P3 | ✓⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/codec/shadowsocks.go:122` | decodeSSUserInfo 用 u.User.String() 取 base64，'/' 被重转义为 %2F 导致 Std 变体解码必失败 |
| 40 | P3 | ✓⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/codec/vmess.go:143` | DecodeVMess 的 TrimPrefix 大小写敏感，与 node.go 大小写不敏感的 scheme 分派不一致 |
| 41 | P3 | ✓ | 正确性 | SUB | `pkg/proxy/core/subscription/uri_convert.go:361` | ssNodeToSpec 不投影 plugin_password/plugin_version，clash buildPluginOpts 对应读取为死代码（shadow-tls 类插件断裂） |
| 42 | P3 | ✓⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/codec/hysteria2.go:106` | hy2/tuic 的 alpn 逗号拆分不过滤空串，尾随逗号产生空 ALPN 条目并进入 Extensions |
| 43 | P3 | ✓⚠️协议 | 安全 | SUB | `pkg/proxy/core/subscription/converter/surge.go:249` | Surge 行格式转换器未对 NodeName 做转义，来自不可信 ext_sub 的 fragment 可注入任意 Surge 配置行 |
| 44 | P3 | ✓ | 错误处理 | SUB | `pkg/proxy/core/subscription/converter/clash.go:133` | MatchProxies 丢弃 regexp 编译错误：用户 proxy_group 中写错正则会静默生成空 group，无日志无报错 |
| 45 | P3 | ✓ | 架构 | SUB | `pkg/proxy/core/subscription/remote.go:35` | RemoteFetcher.Fetch 产出仅含 URI、Protocol 为空的 spec，经 Clash/Surge 转换会被整体丢弃（且当前无生产调用方） |
| 46 | P3 | ?⚠️协议 | 错误处理 | CTR | `pkg/proxy/core/params/protocolparams/params_vmess.go:61` | parseVMess 静默吞掉非法 alter_id（"abc"→0）且不做 0-65535 范围校验，负值可入库 |
| 47 | P3 | ?⚠️协议 | 正确性 | SUB | `pkg/proxy/core/subscription/codec/vless.go:158` | vless/trojan Decode 只识别 insecure=1，不识别生态常用的 allowInsecure=1，skip-cert-verify 丢失 |
| 48 | P3 | ? | 错误处理 | SUB | `pkg/proxy/core/subscription/remote.go:31` | RemoteFetcher.Fetch 错误串内嵌完整 URL（不走 SanitizeURL），且部分失败时错误被完全静默 |
| 49 | P3 | ?⚠️协议 | 安全 | SUB | `pkg/proxy/core/subscription/converter.go:148` | ParseRuleProviderParam 用未清洗的 name 拼 Path，可在用户端 Clash 配置里产生路径穿越 |

### 详细

#### 1. [P1·confirmed] PEM 共享文件设计与 cert_source=pem 可删除语义冲突：删除一个 inbound 会摧毁共用同一 PEM 的其他 inbound 的证书文件

- **位置**：`pkg/proxy/core/params/defaults.go:256` · 维度：正确性 · 单元：CTR
- **问题**：writePEMToScratch 以 cert 内容哈希命名文件并刻意让多个 inbound 共享同一对文件（252-255 行注释：'Two inbounds with the same PEM share the same pair of files — safe because the content is identical'）。但下游删除路径把 cert_source=pem 判定为可删除：mihomo/inbound.go:95-96 shouldCleanupCertSource 返回 true，container.go RemoveInboundConfig Phase 5 直接 os.Remove。运维用同一张泛域名证书 PEM 建两个 trojan/hy2 inbound（完全合理场景），删除其一后共享的 cert/key 文件被物理删除，另一个 inbound 在下次配置 push/进程重启时因证书文件缺失而起不来。另一个衍生问题：keyFile 也只用 cert 内容哈希命名（256-258 行），同 cert 不同 key 的第二次请求会经 writeFileIfChanged 静默覆盖第一个 inbound 正在使用的 key 文件。'共享即安全'的前提只对 cert 文件内容成立，对生命周期（删除）和 key 文件都不成立。修复方向：按 (cert+key) 联合哈希命名并引用计数，或 pem 源也走每-inbound 独立文件。该场景无任何测试覆盖。
- **证据**：defaults.go:252-258 共享文件命名；mihomo/inbound.go:95-96 `return src == "pem" || src == "self_signed"`；mihomo/container.go:585-592 对 cleanupCertFiles() 返回的路径逐个 os.Remove
- **核实**：证据链完整且全部在仓库内：defaults.go:252-258 明确以 cert 内容 FNV 哈希命名并让多 inbound 共享同一对文件（含'safe because the content is identical'注释）；mihomo/inbound.go:95-97 shouldCleanupCertSource 对 'pem' 返回 true，cleanupCertFiles(111-126) 返回 TLS.CertFile/KeyFile；container.go:585-592 RemoveInboundConfig Phase 5 逐个 os.Remove。两个 inbound 用相同 certificate PEM 创建时得到相同路径，删除其一即物理删除另一 inbound 的证书文件，下次 pushConfigWithSnapshot 全量推送或重启时该文件已缺失。衍生问题也成立：keyFile 命名只用 hashShort(certPEM)（256-258 行），同 cert 不同 key 时 writeFileIfChanged(331-337) 检测到内容差异会直接覆盖在用的 key 文件。无引用计数逻辑，params 包测试目录也无该场景覆盖。触发前提（同一 PEM 建多个 TLS inbound + 删除其一）是合理运维场景，且 mihomo 是全量配置推送，坏文件可能影响整体加载，P1 合理。

#### 2. [P1·confirmed] fetchClashTemplate 把第三方 sub-converter 返回的任意 YAML 原样作为用户 Clash 配置下发（rules/dns/proxy-providers 注入面）

- **位置**：`pkg/proxy/core/subscription/converter/clash.go:700` · 维度：安全 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：ConvertWithOptions 强制走 fetchClashTemplate，从 sub.xeton.dev / api.dler.io / sub.maoxiongnet.com / sub.id9.cc 拉回完整 Clash 骨架。代码仅做 clearClashTemplateNoise（字符串替换）+ yaml.Unmarshal + 注入 proxies，对模板其余内容（dns、rules、rule-providers、proxy-providers、script 等）不做任何校验或白名单，直接 yaml.Marshal 回给用户客户端。这些第三方公网服务一旦被投毒/劫持/DNS 污染，即可向每一个订阅用户注入任意 DNS nameserver、RULE-SET/proxy-providers 远程 URL、甚至把全部流量 MATCH 到攻击者可控的 provider。这不是 pass1#177 的可用性问题（4 个全挂才失败），而是一个信任边界/内容注入问题：外部不可信内容成为用户端路由与 DNS 配置。
- **证据**：clash.go:700-718 fetchClashTemplate 只对 data 做 clearClashTemplateNoise 后 yaml.Unmarshal，返回的 nodeMap 除 proxies/部分 proxy-groups 外原样 yaml.Marshal 输出（ConvertWithOptions:189-193）；无对 dns/rules/rule-providers/proxy-providers 的任何过滤。
- **核实**：代码核实属实：clash.go:47-52 硬编码 4 个公网 sub-converter 服务，fetchClashTemplate(700-717) 仅做 clearClashTemplateNoise（720-733，纯字符串行过滤，非安全清洗）+ yaml.Unmarshal，模板的 dns/rules/rule-providers/proxy-providers 等字段无任何白名单或校验，经 ConvertWithOptions:189-193 原样 marshal 下发给用户；patchTemplateWithOptions 也只做增量 patch 不做过滤。且 ConvertWithOptions:175-180 强制走该路径无本地 fallback，/sub 帮助文本确认这是强制依赖。信任边界问题成立：任一服务运营方恶意或被攻破即可向全部 clash 用户注入 DNS/路由/远程 provider。需修正一点：httpGet 走 HTTPS+默认证书校验，'DNS 污染/劫持'向量基本被 TLS 缓解，剩余向量是服务本身恶意/被攻破——但 4 个无审计第三方任一可控即全量注入，P1 仍可站住。

#### 3. [P2·confirmed] SS: caller 自带 password + 缺省填入 SIP022 cipher，产出非法密钥材料的监听配置

- **位置**：`pkg/proxy/core/params/defaults.go:126` · 维度：正确性 · 单元：CTR · ⚠️协议相关（需人工核对上游）
- **问题**：fillCredentials 的 shadowsocks 分支：cipher 为空时无条件缺省为 "2022-blake3-aes-256-gcm"（defaults.go:126-131），但只有在 password 也为空时才用 randomSSPassword 生成匹配的 base64 32 字节密钥（133-135）。若 caller 只提供了 password（如 "mypassword"）而未指定 cipher，结果是 SIP022 cipher 搭配一个非 base64、长度不符的密码——SIP022 要求 password 是恰好 32 字节密钥的标准 base64 编码。parseSS 也不做 cipher×password 兼容性校验（params_ss.go 只 requireString），错误一路穿透到 mihomo 监听器创建时才失败，FastAdd 请求表面成功后监听器起不来。应在 cipher 缺省为 2022 系前检查 caller 密码是否为合法 SIP022 密钥，不合法则回退到经典 AEAD cipher 或直接报错。
- **证据**：defaults.go:126 `if isEmpty(params, "cipher") { params["cipher"] = "2022-blake3-aes-256-gcm" }`；133-135 仅当 password 为空才 `params["password"] = randomSSPassword(cipher)`；defaults_test.go 只测了「cipher 给定→生成密码」和「双双缺省」两种矩阵，没有「password 给定 + cipher 缺省」的用例。
- **核实**：触发路径读码确证：defaults.go:126-131 在 cipher 为空时无条件填 2022-blake3-aes-256-gcm，133-135 仅当 password 也为空才按 cipher 生成合法密钥——caller 只给 password 时产出 SIP022 cipher + 任意字符串密码的组合。parseSS（params_ss.go:31-39）只做 requireString，无 cipher×password 兼容校验。上游行为有仓库材料确证：wiki/knowledge/mihomo-container/edge-cases.md:32-34 明确记载 SIP022 要求 standard base64 编码的 32/16 字节 raw key、不合法密码导致 mihomo listener EOF/解码失败，details.md:77 记载 hex 密码导致 'SIP022 listener 启动失败'。defaults_test.go 测试矩阵确无 'password 给定 + cipher 缺省' 用例。P2 恰当：FastAdd 表面成功、监听器起不来，且 Phase 4 升级默认 cipher 后老调用方（只传 password）会静默踩坑。

#### 4. [P2·confirmed] NewDefaultInbound 把 port=0 静默改写为 10000、空 tag 改写为 "default-inbound"，掩盖调用方错误并与 protocolparams 的 port=0 语义冲突

- **位置**：`pkg/proxy/core/inbound/default.go:23` · 维度：正确性 · 单元：CTR
- **问题**：NewDefaultInbound（default.go:23-28）对空 tag 和 port==0 做魔法缺省：tag→"default-inbound"、port→10000。protocolparams.ProtocolParams.Port 的文档明确写 "0 means let the container choose"，requireUint32 也刻意放行 0；但真实消费路径 NewMihomoInboundFromProtocolParams(pp)（mihomo/inbound.go:166）把 pp.Port 直接传入 NewDefaultInbound，port=0 被静默钉死为固定的 10000——既不是分配也不是报错。两个都传 port=0 的 FastAdd 会得到两个都声称监听 10000 的 inbound，且后续 Validate 因 10000 在 [100,65535] 内而全部通过，错误完全不可见。与 pass1 parser.go:153 的发现互补：那条指出无 adapter 实现分配语义，本条指出 sink 端把 0 变成固定端口而非拒绝，二者叠加使 "0=分配" 成为静默陷阱。tag 侧在 mihomo 路径被 fastAddBuildInbound 的 pp.Tag=tag 覆盖而缓解，但 hysteria/snell 直接调用 NewDefaultInbound 的路径仍暴露该缺省。修复方向：构造函数不做魔法缺省，port=0/空 tag 交给 Validate 报错。
- **证据**：default.go:23-28 `if tag == "" { tag = "default-inbound" }` / `if port == 0 { port = 10000 }`；protocolparams.go:54 注释 "0 means let the container choose"；mihomo/inbound.go:166 `base := inbound.NewDefaultInbound(pp.Tag, pp.Protocol, pp.Port)`；rpc/server/end_node_inbound.go:244 将请求 port 原样放入 map，未拦截 0。
- **核实**：全链路逐环节确证：default.go:23-28 魔法缺省（空 tag→default-inbound、port 0→10000）属实；protocolparams.go:52-54 与 parser.go:153 均声明 0 表示 '让容器分配'，requireUint32 刻意放行 0；HTTP handler（fastAddInbound_handler.go）对 port 无任何校验，RPC handler（end_node_inbound.go:244）原样透传；adapter.go:220-225 → inbound.go:166 把 pp.Port 直传 NewDefaultInbound，port=0 被钉死为固定 10000；DefaultInbound.Validate（default.go:100）对 10000 放行，两个 port=0 请求产生两个声称监听 10000 的 inbound 且错误不可见。tag 侧缓解描述准确：FromProtocolParams:248-250 先拒空 tag、adapter.go:224 用权威 tag 覆盖；hysteria/snell container 直接调用 NewDefaultInbound 的路径存在（container.go 多处）。语义冲突与静默陷阱定性成立，P2 恰当。

#### 5. [P2·confirmed] caller 提供的 certificate/key PEM 内容零校验直接落盘：非 PEM、cert/key 不匹配、超大内容全部晚失败于容器 reload

- **位置**：`pkg/proxy/core/params/defaults.go:196` · 维度：错误处理 · 单元：CTR
- **问题**：resolveCertSource 的 PEM 分支（190-204 行）对 certificate/key 字符串不做任何结构校验就写盘并标记 cert_source=pem：不调用 pem.Decode 验证是 PEM、不用 tls.X509KeyPair 验证 cert/key 配对、也没有大小上限（对比同层 params_anytls.go:95-97 对 padding_scheme 设 8KiB 上限并解释'accidental file-content paste'的风险——证书字段面对完全相同的粘贴错误却无防护）。错误粘贴（贴错文件、贴了公钥、cert 与 key 不配对）会顺利通过 FastAdd 和 Parse，直到 mihomo 配置 push/xray reload 时才以容器侧晦涩错误暴露，且此时坏 inbound 已进 InboundStore，可能反复污染后续整体配置加载。用 tls.X509KeyPair(certPEM, keyPEM) 一次调用即可在参数层完成全部三项校验。
- **证据**：defaults.go:190-204 仅判空后即调用 writePEMToScratch；248-266 行写盘无任何内容检查；对比 params_anytls.go:16-17 maxAnyTLSPaddingSchemeBytes=8192 的防粘贴设计
- **核实**：defaults.go:190-204 的 PEM 分支只做双方非空互斥检查（193-194 行），随即 writePEMToScratch 落盘并写 cert_source=pem；writePEMToScratch(248-266) 只有 MkdirAll+写文件，无 pem.Decode、无 tls.X509KeyPair 配对校验、无大小上限。对比同层 params_anytls.go:16-17 对 padding_scheme 设 8192 字节上限并注释'accidental file-content paste'防护，证书字段确实面对同类粘贴错误无对称防护。坏内容通过 FastAdd（end_node_inbound.go:299 FillDefaults）与 Parse 后入库，错误只能在容器 reload 时暴露，且 mihomo 是全量 snapshot 推送（container.go:558-567），坏记录会持续影响后续推送。P2 error-handling 恰当。

#### 6. [P2·confirmed] SS-2022 caller 自带 password 不校验 base64 形状与密钥长度，违背本包'早失败'原则（与 TUIC uuid 校验不对称）

- **位置**：`pkg/proxy/core/params/protocolparams/params_ss.go:31` · 维度：正确性 · 单元：CTR · ⚠️协议相关（需人工核对上游）
- **问题**：parseSS 对 password 只做 requireString。当 cipher 为 2022-blake3-aes-256-gcm/128-gcm 时，SIP022 要求 password 是 base64 编码的恰好 32/16 字节密钥；FillDefaults 只在缺省时生成合法值（randomSSPassword），caller 显式提供的短口令（如 "mypassword"）会原样通过 Parse 并持久化，直到 mihomo/xray 监听器初始化时解 base64 失败才报错——错误出现在配置 push 阶段，定位困难且坏记录已入 InboundStore。同文件同层的 parseTUIC（params_tuic.go:49-52）正是以'mihomo would silently zero it'为由对 uuid 做了早校验，SS-2022 密钥面对同类下游行为却无对称防护。建议：cipher 前缀为 2022- 时校验 base64.StdEncoding 可解码且长度等于 32/16。
- **证据**：params_ss.go:31-39 仅 requireString(password)+requireString(cipher)；defaults.go:362-375 randomSSPassword 说明 2022 系密钥必须是 base64 的 16/32 字节；对比 params_tuic.go:49-52 的 uuid.Parse 早校验
- **核实**：params_ss.go:31-39 对 password/cipher 仅 requireString，无 2022- 系密钥形状校验，代码事实确凿。SIP022 对密钥的要求不依赖外部记忆——仓库自身材料确证：defaults.go:353-361 randomSSPassword 注释明确写明'SIP022 (2022-blake3-* family) requires a raw-key-material password encoded as standard base64: 32/16 bytes'，且 FillDefaults 只在 isEmpty 时生成（133-136 行），caller 显式提供的任意短口令原样通过。与 params_tuic.go:49-52 对 uuid 的早校验（理由'mihomo would silently zero it'）不对称也属实。晚失败于配置 push 的具体上游报错形态无法在仓库内逐字核证，但'无效密钥通过早校验层并入库'这一缺陷本身已由仓库代码完全证实。P2 合理。

#### 7. [P2·confirmed] BaseContainer 状态机与 embedder 覆盖的 Start/Stop 静态派发脱钩：xray Executor 绕过基类后基类状态恒 Stopped，Version() 经 rpc 恒返回空、Reload() 必失败

- **位置**：`pkg/proxy/core/container/base.go:288` · 维度：架构 · 单元：CON
- **问题**：BaseContainer.Restart() 内部静态调用 b.Stop()/b.Start()，且 State/IsRunning 只反映基类状态机；Go 嵌入没有虚派发，embedder 一旦覆盖 Start/Stop，基类状态就永久漂移，而基类未提供任何一致性保障或检测。xray Executor 正是这样：Executor.Start()（exec_runner.go:414）直接走 killProcessOnPort/EnsureBinary/Runner.Start()，从不经过 BaseContainer.Start，也没有任何地方调 MarkRunning（全仓 grep 无 MarkRunning 调用），因此 xray 的基类状态永远是 Stopped。后果一：Version()（exec_runner.go:501-503）以 e.BaseContainer.IsRunning() 为门槛，恒 false → 生产路径 pkg/rpc/server/end_node_inbound.go:129 的 c.Version() 对 xray 恒返回 ""。后果二：Reload()（exec_runner.go:462-464）调 e.BaseContainer.Restart()，Stop() 在 Stopped 态直接幂等返回（base.go:231-234，不会停真实进程），Start() 再执行 runFunc=Runner.Start()，而进程仍在运行，Runner.Start 必返回 "process already running"（tools/process/runner.go:62），Reload 永远失败。这是 base.go 契约层面的设计漏洞：要么禁止覆盖（提供模板方法+钩子），要么 Restart/IsRunning 不应默认绑定基类状态。
- **证据**：base.go:288-299 `if err := b.Stop(); ... b.Start()`（静态派发）；exec_runner.go:414 Executor.Start 直接 `e.Runner.Start()` 不触碰基类；exec_runner.go:502 `if !e.BaseContainer.IsRunning() { return "" }`；runner.go:62 `return fmt.Errorf("process already running")`；end_node_inbound.go:129 `currentVersion := c.Version()`
- **核实**：全部证据核实成立：Executor.Start(exec_runner.go:413)完全绕过 BaseContainer（直接 killProcessOnPort/EnsureBinary/Runner.Start），全仓 grep 确认 MarkRunning 除 base.go 内部外零调用，也无任何显式 BaseContainer.Start() 调用，故 xray 基类状态永为 Stopped。Version()(exec_runner.go:502)显式以 e.BaseContainer.IsRunning() 为门槛恒返回空串，生产路径 end_node_inbound.go:129(UpdateProxy RPC，getXrayContainer 经 containerMgr 返回 *Executor)确实受影响——同版本去重判断永不命中，请求当前版本会触发多余的下载+重启，Update 结果 FromVersion 也恒为空。Reload()(exec_runner.go:463)走 BaseContainer.Restart：Stop 在 Stopped 态幂等返回不停真实进程(base.go:231-234)，Start 再执行 runFunc=Runner.Start()，进程存活时 runner.go:61-62 必返回 'process already running'，Reload 必失败。唯一缓释：全仓无 .Reload() 生产调用者（仅 interface.go:26 接口声明），该后果目前为潜伏；Version 后果是活的。综合影响是管理路径功能缺陷而非数据丢失/崩溃，降为 P2。

#### 8. [P2·confirmed] Start 失败路径没有任何清理契约：runFunc 半执行的副作用（已启动的 goroutine/句柄）既不触发 stopFunc 也无失败回调，snell 实际因此泄漏 reconcile/事件 goroutine 且重复 Start 会双开

- **位置**：`pkg/proxy/core/container/base.go:182` · 维度：错误处理 · 单元：CON
- **问题**：runFunc 返回错误时（base.go:182-188）BaseContainer 只把状态置回 Stopped 并返回包装错误：不调用 stopFunc（此时也未缓存）、不 close 本次 Start 新建的 stopChan、更没有给 Hooks 提供 onStartFailed 之类的清理入口；而后续 Stop() 见 Stopped 直接幂等返回（base.go:231-234），清理永远没有机会执行。具体容器的 runFunc 普遍是复合操作：snell 的 run 钩子先 startUserEventHandler()、startReconcileLoop() 再 startProcess()（snell/container.go:60-76）。startProcess 失败时，已启动的 user event 处理 goroutine 和 30s reconcile ticker goroutine 全部泄漏且无法停止（stopFunc 里的 stopReconcileLoop/closeUserEventCh 不会被调用）；若随后重试 Start 成功，startReconcileLoop 无防重保护，直接 `sc.reconcileStopCh = make(chan struct{})` 覆盖旧 channel（snell/container.go:664-670），第一条 reconcile goroutine 从此永远无法被 close 到，且两条 loop 并发执行 reconcileUsers。根因在 base.go 契约：要么规定 runFunc 必须自我原子化（文档完全未提），要么失败时给容器清理钩子。
- **证据**：base.go:182-188 失败仅 `b.state = ContainerStateStopped` 后返回；snell/container.go:70-75 run 钩子先 `h.c.startUserEventHandler(); h.c.startReconcileLoop()` 再 `return h.c.startProcess()`；snell/container.go:669 `sc.reconcileStopCh = make(chan struct{})` 无已启动判断
- **核实**：核实成立：base.go:182-188 runFunc 失败仅置 Stopped 并返回，不调 stopFunc（未缓存）、不 close stopChan、Hooks 接口(base.go:44-53)无失败清理入口；随后 Stop() 因 Stopped 幂等短路(base.go:231-234)，清理永无机会。snell 未覆盖 Start/Stop（方法列表核实无 Start/Stop 定义），确实经 BaseContainer.Start 执行 run 钩子：container.go:71-75 先 startUserEventHandler()、startReconcileLoop() 再 return startProcess()，startProcess 失败时事件处理 goroutine 和 30s reconcile goroutine 泄漏。重试 Start 时 startReconcileLoop(container.go:664-686)无防重，reconcileWg 重复 Add 且 `sc.reconcileStopCh = make(...)` 无锁覆盖，两条 reconcile loop 并发执行 reconcileUsers。一处细节不准：旧 goroutine 的 select 每轮重读 sc.reconcileStopCh 字段，close 新 channel 实际能同时终止两条 loop，'第一条永远无法被 close 到'不成立——但换来的是对该字段的无同步读写数据竞争，且失败后未重试期间泄漏依旧。核心缺陷（Start 失败无清理契约+snell 实际泄漏+重试双 loop）成立，P2 恰当。

#### 9. [P2·confirmed] vmessNodeToSpec 不投影 SkipCertVerify 与 HeaderType，clash/surge 侧读取点永远读不到

- **位置**：`pkg/proxy/core/subscription/uri_convert.go:259` · 维度：正确性 · 单元：SUB
- **问题**：DecodeVMess 会从 JSON `allowInsecure` 解析出 VMessNode.SkipCertVerify（vmess.go:203-206），从 `type` 解析 HeaderType；但 vmessNodeToSpec（uri_convert.go:259-306）只投影 security/server_name/utls_fingerprint/alter_id/transport/各 path host，唯独漏掉 skip_cert_verify 和 header_type。而 clash.go:293 convertVMess 与 surge.go:107 convertVMess 都有 `ext["skip_cert_verify"]` 的读取逻辑（该分支对 vmess 是死代码），且 vmess 也没有 hy2/tuic/anytls 那种 `strings.Contains(spec.URI, "insecure=1")` 的 fallback（clash.go:557/617/671 有，293 没有）。结果：自签证书 vmess+tls URI 转 clash/surge 后丢失 skip-cert-verify，客户端证书校验失败节点不可用；net=tcp&type=http 的 HTTP 伪装节点退化为裸 tcp 同样无法连接。其余 7 个协议的 nodeToSpec 都投影了 skip_cert_verify，仅 vmess 缺失，属明显遗漏而非设计。
- **证据**：uri_convert.go:228/332/398/428/465 五处其他协议均写 ext["skip_cert_verify"]，vmessNodeToSpec（259-306）无此写入；clash.go:293 `if skipVerify, _ := ext["skip_cert_verify"].(bool)` 对 vmess 恒为 false。
- **核实**：核实成立，且找到了比 finding 更完整的生产触发链。codec/vmess.go:203-206 DecodeVMess 确实解析 allowInsecure→SkipCertVerify、:175-179 解析 Type→HeaderType；uri_convert.go:259-306 vmessNodeToSpec 逐行核对确无 skip_cert_verify/header_type 投影，而其余协议的 nodeToSpec 在 :228/:332/:398/:428/:465 五处均投影 skip_cert_verify。converter/clash.go:293 与 converter/surge.go:107 的 ext["skip_cert_verify"] 读取对 URI 解码链恒为 false，且 vmess（base64 体）无法像 hy2/tuic/anytls 那样做 URI 子串 fallback（clash.go:557/617/671）。生产链路确认：end_node_user.go:318-330 RPC 只回传 spec.URI（容器直写的 Extensions 在 RPC 边界丢弃），sub_handler.go:199 对 URI 重新 ConvertURIs→nodeToSpec；而 xray/subscription.go:333-335 自签场景在 vmess JSON 写 allowInsecure:true、mihomo/subscription.go:379-381 也置 node.SkipCertVerify（Encode 时进 JSON），两容器的自签 vmess+tls 节点经 /sub 输出 clash/surge 后必然丢失 skip-cert-verify。一处小瑕疵不影响结论：header_type 在 clash/surge converter 里本就无任何读取点（grep 仅 xray 容器有 mkcp/quic_header_type），'读取点读不到'的表述只对 skip_cert_verify 精确；HTTP 伪装退化为裸 tcp 的现象本身仍真实。P2 恰当。

#### 10. [P2·confirmed] /sub 在 Info 级日志里打印完整节点 URI（含 password/UUID/PSK 明文），抵消模块内的日志脱敏

- **位置**：`pkg/http/sub_handler.go:166` · 维度：安全 · 单元：SUB
- **问题**：core/subscription 模块内所有出站/告警日志都刻意脱敏（uri_convert.go redactURI、http.go SanitizeURL）。但其唯一生产调用方 sub_handler 在默认开启的 Info 级别把整条节点 URI 列表原样打印（"uris", v），v 是各 end node 返回的 vless/vmess/trojan/ss/hy2/tuic URI，其中直接内嵌用户凭据（UUID、password、PSK）。任何能读到服务日志的人即可拿到全部用户的代理凭据。这使模块内的脱敏努力形同虚设。应改为只打印 count，或复用 redactURI 逐条脱敏。
- **证据**：sub_handler.go:166 log.Info("[SubHandler] node URIs", "node", n, "count", len(v), "uris", v)；对照 uri_convert.go:163 redactURI 与 http.go:119 SanitizeURL 的脱敏约定。
- **核实**：完全属实：sub_handler.go:166 `log.Info("[SubHandler] node URIs", ..., "uris", v)` 将各 end node 返回的完整节点 URI 列表原样打印，pkg/log/options.go:44 确认默认级别为 LevelInfo（默认开启）；URI 内嵌用户凭据（vless/tuic UUID、trojan/hy2/anytls password、ss 凭据）由 converter 代码可证（surge.go 直接从 spec 取 Password 等）。同文件 maskToken(313-321) 及同模块 redactURI(uri_convert.go:163)、SanitizeURL(http.go:119) 证明脱敏是既有约定，此处明显违反。触发路径为每次 /sub 请求必经（line 163-173），无条件开关。P2 恰当（日志读者通常是运维，已有更高权限，非 P1）。

#### 11. [P2·uncertain] hy2 userinfo 含未转义冒号时密码被静默截断（userpass auth 场景取不到完整 auth）

- **位置**：`pkg/proxy/core/subscription/codec/hysteria2.go:92` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：DecodeHysteria2 用 `password = u.User.Username()` 取密码。Hysteria2 上游 URI 规范里 userinfo 是完整 auth 字符串，userpass 鉴权模式下形如 `user:pass`（官方客户端与 mihomo converter 都取整个 userinfo）。本实现遇到 `hysteria2://user:pass@h:443` 时 Username() 只返回 `user`，`pass` 被静默丢弃——实测 Decode 得到 Password="user" 且无任何错误，转换出的 clash/surge 节点鉴权必然失败且极难排查。trojan.go:112 是同一模式（trojan 规范要求百分号转义冒号，截断至少也应报错而非吞掉）。建议 hy2 侧改为拼接 Username+":"+Password（存在 Password 时）。
- **证据**：hysteria2.go:90-93 `password := ""; if u.User != nil { password = u.User.Username() }`。实测 `DecodeHysteria2("hysteria2://user:pass@h.com:443/?sni=x#n")` 返回 Password="user"、无 error。
- **核实**：代码行为已实测确证：hysteria2.go:90-93 只取 u.User.Username()，DecodeHysteria2("hysteria2://user:pass@h.com:443/?sni=x#n") 返回 Password="user" 且无 error，截断是静默的。但该条的缺陷定性完全依赖上游协议前提——'hy2 URI userinfo 是完整 auth 字符串、userpass 模式形如 user:pass、官方客户端与 mihomo 取整个 userinfo'。逐一检索仓库材料（wiki/knowledge/mihomo-container/{index,details,edge-cases}.md、docs/mihomo-container-implementation-plan.md:1054、docs/http-api-reference.md:712），全部只记载 hy2 URI 的 5 个 query key（obfs/obfs-password/sni/insecure/pinSHA256），无一处记载 userinfo/auth 段的格式语义；wiki 中唯一的 userinfo 冒号语义记载是 anytls（[user:]password），不适用于 hy2。且仓库自产链路不触发：本系统 hy2 密码是单段 AuthToken（无冒号），自身 Encode 用 url.User() 会把密码中的 ':' 转义为 %3A，round-trip 安全；触发面仅限 sub_handler.go:178 ext_sub 合入的外部 URI。按规则 3，第三方行为无仓库材料佐证，最高 uncertain。

#### 12. [P2·uncertain] 订阅节点无去重：cluster + ext_sub 合并后重名节点产生重复 Clash proxy name，mihomo 整份配置拒绝加载

- **位置**：`pkg/proxy/core/subscription/converter/clash.go:168` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：sub_handler 把多个 end node 返回的 URI（GetSubscription 又遍历所有 container）与 FetchAndMergeExtSubs 的 ext_sub URI 直接 append 合并（sub_handler.go:167/182），全链路无任何按 NodeName/URI 去重。ClashConverter.ConvertWithOptions 逐 spec 生成 ClashProxy，name 取自 spec.NodeName（nodeName():686），injectProxiesToTemplate 直接 yaml.Marshal proxies，也不做唯一化。Clash/mihomo 要求 proxies[].name 全局唯一，出现两个同名 proxy 会导致整份配置解析失败（而非跳过单个节点）。当同一节点在多个 container、多个 end node、或 ext_sub 与集群订阅中重复出现（很常见）时，用户会拿到一份完全无法加载的订阅。common/qv2ray 输出因逐行拼 URI 不受影响，故问题只在 clash/surge 客户端暴露且难排查。
- **证据**：clash.go:159-171 收集 proxies/nodeNames 无去重；injectProxiesToTemplate:738-758 直接 marshal；nodeName:686-691 name 仅由 NodeName/InboundTag 决定；sub_handler.go:167,182 合并处无 dedup。
- **核实**：代码侧全链路核实属实：manager.go:51-61（遍历 container 直接 append）、sub_handler.go:167/182（多节点+ext_sub 合并）、clash.go:159-171（收集 proxies/nodeNames）、injectProxiesToTemplate:738-758（直接 marshal）确无任何按 name/URI 去重；nodeName(686-691) 仅由 NodeName/InboundTag 决定，重复 URI 必产生同名 proxy。但结论的关键一环——'Clash/mihomo 对重名 proxies 拒绝加载整份配置'——是第三方行为断言，仓库内 wiki（mihomo-container）、docs、systemtest 均无材料佐证，按 protocolRelated 规则不得凭记忆确认。另外'很常见'的重复来源中，跨 end node 重名取决于各节点 URI fragment 生成规则（未证实会撞名），最现实的来源只有 ext_sub 与集群订阅重叠。去重缺失本身确凿，最终影响待证。

#### 13. [P3·confirmed] Load 对单行 expiry_time 解析失败即整体返回错误，一行坏数据导致节点启动直接退出

- **位置**：`pkg/store/user_store.go:87` · 维度：错误处理 · 单元：STO
- **问题**：Load/Get/ListByGroup 对 expiry_time 只接受 RFC3339 和 '2006-01-02 15:04:05' 两种格式，任何其他变体（如带小数秒 '2026-01-01 00:00:00.123'、带偏移 '...+08:00'——后者正是 modernc 驱动对直接绑定 time.Time 参数的缺省存储格式，任何历史版本或旁路写入都可能留下）都会让整个 Load 返回错误。调用链 usermanager.go:184-187 NewUserManagerWithStore → cmd/server.go:130-133 os.Exit(1)：一行不可解析的 expiry_time 会让节点永久无法启动、所有用户下线，只能手工改库恢复。与同函数内 port_mappings 损坏仅 log.Printf 容错（user_store.go:94-98）的降级策略完全相反。建议对无法解析的 expiry_time 记警告并按零值（永不过期）或跳过该行处理，而非让全量加载失败。
- **证据**：user_store.go:82-88: `t, parseErr = time.Parse("2006-01-02 15:04:05", expiryStr.String); if parseErr != nil { return nil, fmt.Errorf("SQLiteUserStore.Load parse expiry_time %q: %w", ...) }`；usermanager.go:185-187 将该错误原样上抛；cmd/server.go:131-133 `log.Error("init user manager failed"...); os.Exit(1)`。
- **核实**：代码路径完全属实：user_store.go:82-88 对 expiry_time 双格式解析失败即整体返回错误，usermanager.go:184-186 原样上抛，cmd/server.go:130-133 os.Exit(1)，一行坏数据确实导致节点无法启动；且与同函数 port_mappings 仅 log.Printf 容错（94-98 行）的策略不一致属实。但需降级严重度：git 历史（-S expiryArg）显示本仓库自 SQLite 持久化引入以来只用 RFC3339 写入 expiry_time，Save 从未直接绑定 time.Time，schema 也无 datetime 默认值，即不存在任何仓库内写入路径能产生第三种格式；触发只能来自旁路手工写库或外部工具。'modernc 驱动直接绑定 time.Time 的缺省格式' 一说无仓库内写入点支撑。另外对不可解析的过期时间按零值（永不过期）降级本身有安全风险（过期用户复活），fail-fast 有其合理性。综合定为出界数据才可触发的健壮性问题，P3 更合适。

#### 14. [P3·confirmed] Commit 失败时 defer 会对已完结事务再调 Rollback，必然产生一条误导性的 'rollback failed' 日志

- **位置**：`pkg/store/tx.go:18` · 维度：错误处理 · 单元：STO
- **问题**：WithTx 的 defer 只判断 retErr != nil 就调用 tx.Rollback()。database/sql 的 Tx.Commit 在进入驱动提交前就原子置位 done 标志，无论提交成败事务都已完结；因此当 Commit 返回错误（如提交时 SQLITE_BUSY/IO 错误）时，deferred Rollback 必定返回 sql.ErrTxDone，日志会打出 'store.WithTx: rollback failed: sql: transaction has already been committed or rolled back'，把排障注意力引向不存在的回滚问题而掩盖真实的提交失败。应在 defer 中对 rbErr == sql.ErrTxDone 静默，或改为 fn 成功后单独处理 Commit 错误不再触发 Rollback。
- **证据**：tx.go:16-26: defer 块 `if retErr != nil { if rbErr := tx.Rollback(); rbErr != nil { log.Printf(...) } }` 与 `return tx.Commit()` 组合；Go 标准库 Tx.Commit 先 CompareAndSwap done=1 再调驱动，失败后 Rollback 固定返回 ErrTxDone。
- **核实**：机制核实为真：tx.go:16-26 的 defer 仅判断 retErr != nil 即调 Rollback；Go 1.24.4 标准库 database/sql/sql.go:2299 显示 Tx.Commit 在调用驱动提交前先 tx.done.CompareAndSwap(false,true)，驱动提交失败后 done 仍为 true，随后 Rollback（sql.go:2327）CAS 失败固定返回 ErrTxDone。因此 Commit 阶段任何错误（SQLITE_BUSY/IO）都会额外打出 'rollback failed: sql: transaction has already been committed or rolled back' 的误导日志。影响仅限日志误导（原始 Commit 错误仍被正确返回给调用方，数据不受影响），P3 恰当。

#### 15. [P3·confirmed] InitLoginPasswords 启动期对全量缺口令用户串行 bcrypt，无进度日志与超时控制，升级后首次启动可阻塞数分钟

- **位置**：`pkg/store/manager.go:94` · 维度：资源 · 单元：STO
- **问题**：v6 迁移给所有存量用户置 login_password=''，升级后首次启动 InitLoginPasswords 会对每个用户串行执行 bcrypt.GenerateFromPassword(DefaultCost=10，单次约 50-100ms）再逐条 UPDATE。数千用户规模下这是数分钟的无输出阻塞（cmd/server.go:123-129 注释明确要求它先于 HTTP server），期间进程看起来像挂死且 HTTP/gRPC 全部不可用；任何一个用户 hash/UPDATE 失败即 os.Exit(1)，下次启动从头再来（已完成的部分幂等保留，但失败原因若持续存在则节点永远起不来）。建议至少输出待处理数量与进度日志，并考虑失败单用户降级跳过而非整体失败。
- **证据**：manager.go:94-102: `for _, r := range pending { hash, err := hashFn(r.authToken); if err != nil { return ... } ... }` 串行且任一错误即整体返回；password.go:28 使用 bcrypt.DefaultCost；cmd/server.go:126-129 失败即 os.Exit(1)。
- **核实**：各项事实核实为真：manager.go:94-102 对 pending 串行 hashFn + 逐条 UPDATE，无任何进度/数量日志；password.go:28 用 bcrypt.DefaultCost(10)，单次数十毫秒量级；migrations.go v6 给全部存量用户置 login_password=''；cmd/server.go:126-129 失败即 os.Exit(1) 且注释要求先于 HTTP server 阻塞执行。千级用户首次升级启动阻塞分钟级、无输出、任一用户失败整体退出（已完成部分因逐条 UPDATE 幂等保留）均与代码一致。P3 恰当。

#### 16. [P3·confirmed] Migrate 无跨进程互斥且事务为缺省 DEFERRED，双进程并发迁移时后者以混乱的 DDL 错误失败

- **位置**：`pkg/store/migrate.go:33` · 维度：并发 · 单元：STO
- **问题**：版本检查（SELECT MAX(version)）与迁移应用不在同一事务/锁内，WithTx 用 BeginTx(ctx, nil) 即 SQLite DEFERRED 事务。当两个进程共用同一 DSN（误双开实例、运维工具与服务并行）同时启动：两者都读到 maxVersion=N 并尝试应用 N+1；后完成锁升级的进程不会在 schema_migrations 冲突处得到清晰错误，而是先在 DDL 本身失败（如 v14 重复执行报 'no such column: password'、v3 报 'duplicate column name'），错误信息完全无法指向真实原因（并发迁移竞争）。虽然事务回滚保证了不损坏数据，但故障表现极具误导性。建议迁移前执行 BEGIN IMMEDIATE 或对版本检查+应用整体串行化，并把 DDL 失败时的当前 schema 版本带进错误信息。
- **证据**：migrate.go:33-36 版本查询在任何事务之外；migrate.go:59 applyMigration → WithTx → tx.go:12 `db.BeginTx(ctx, nil)` 缺省 DEFERRED；SetMaxOpenConns(1) 只串行化单进程内连接，对第二个进程无效。
- **核实**：代码事实全部核实：migrate.go:32-36 版本查询在任何事务/锁之外；applyMigration→WithTx→tx.go:12 BeginTx(ctx,nil)；db.go Open 的 DSN 无 _txlock 参数，go.mod 锁定 modernc.org/sqlite v1.46.1，其 driver.go:76 文档明确 _txlock 缺省为 deferred；SetMaxOpenConns(1) 与 busy_timeout=5000 均为进程内/连接级，对第二进程的版本检查竞争无效。双进程同读 maxVersion=N 后，后者在 DDL 处失败（ALTER 重复列/RENAME 不存在列）而非在 schema_migrations 主键冲突处失败，错误信息确实无法指向并发竞争根因；WithTx 回滚保证不损坏数据也属实。双开实例属运维误操作场景且仅误导排障，P3 恰当。

#### 17. [P3·confirmed] UserStore 接口的 Get/ListByGroup/Count 全仓库无生产消费方，且 Count/ListByGroup 对墓碑与空 target_group 的语义未定义

- **位置**：`pkg/store/user_store.go:22` · 维度：架构 · 单元：STO
- **问题**：grep 全仓库，Get/ListByGroup/Count 只被 pkg/store 自身测试和 usermanager 的一个测试引用，生产代码（usermanager.go）只使用 Load/Save/Delete。这三个方法是为集群同步预留但同步实际走内存 map + Save 的死接口面。同时其语义有坑：Count 统计包含 deletion_state='deleting' 的墓碑行；ListByGroup 按 target_group 精确匹配，而 v12 给存量用户的默认值是 ''（v7 死表 cluster_users 用的却是 'default'），未来消费方拿 'default' 查询将取不到任何 legacy 用户。建议要么删掉未用方法收窄接口，要么在启用前明确定义墓碑过滤与默认组语义并补测试。
- **证据**：grep 'ListByGroup|\.Count()' 非测试代码仅命中 pkg/store 内部与接口别名（usermanager/store.go 只是类型别名转发）；migrations.go:124 `target_group TEXT NOT NULL DEFAULT ''` vs migrations.go:93 cluster_users `DEFAULT 'default'`；user_store.go:293-299 Count 无 deletion_state 过滤。
- **核实**：grep 全仓库核实：Get/ListByGroup/Count 在非测试代码零消费方（生产侧仅 usermanager.go:184 调 UserStore().Load 与 mutateUser/AddUser 等处调 store.Save，甚至 Delete 也无生产调用，墓碑走 Save，接口死面比 finding 所述更大）；usermanager/store.go 仅为类型别名转发。语义坑核实：user_store.go:295 Count 为 SELECT COUNT(*) 无 deletion_state 过滤；ListByGroup（:239）按 target_group 精确匹配，migrations.go:124 v12 存量默认 '' 与 :93 v7 cluster_users 默认 'default' 不一致；cluster_users 表在 Go 代码中无任何查询引用，确为死表。属接口收窄/语义预定义的架构卫生问题，P3 恰当。

#### 18. [P3·confirmed] v12-v14 三个迁移零断言：v14 RENAME COLUMN 的数据保留、v12 存量行默认值、v13 deletion_state 均未验证，且升级测试注释与实际行为不符

- **位置**：`pkg/store/migrations/migrations_test.go:218` · 维度：测试缺口 · 单元：STO · ⚠️协议相关（需人工核对上游）
- **问题**：migrations_test 对 v5-v11 有专项断言，但 v12（users 加 target_group/updated_at_us/origin_node/hash + 索引）、v13（deletion_state）、v14（password RENAME 为 auth_token）没有任何字段级断言。TestClusterUserMigrations_UpgradeFromLegacyDB 注释写 'Upgrade to full v11' 实际 Migrate(migrations.All) 已跑到 v14，升级后只断言 username 可读，未验证 legacy 用户的 password 值确实保留在 auth_token 列、target_group 默认为 ''、idx_users_target_group 存在。v14 依赖 SQLite 3.25+ 的 RENAME COLUMN 语义（modernc 驱动实现），恰是最需要数据保留断言的一条。这是本模块迁移链上最后三个、也是与集群同步正确性直接相关的迁移。
- **证据**：migrations_test.go:216-217 注释 'then upgrade to full v11'，:238 `store.Migrate(db, migrations.All)` 实际到 v14；:243-245 仅 `SELECT username FROM users WHERE username='legacy-user'`；全文件 grep 'auth_token|v12|v14|RENAME' 无命中（仅 cluster_users 的 target_group 断言）。
- **核实**：migrations.go:122-137 确认 v12/v13/v14 内容与 finding 描述一致（v14 为 RENAME COLUMN password→auth_token）。migrations_test.go 全文无任何 v12-v14 字段级断言；:216-217 注释确写 'upgrade to full v11' 而 :238 Migrate(migrations.All) 实跑到 v14（TestAll_Count=14），:243-245 升级测试仅断言 username 可读，未验证 password 值保留到 auth_token、target_group 默认值、idx_users_target_group 存在。pkg/store 测试目录 grep auth_token 仅 manager_test.go 用作 INSERT 列名，非迁移保留断言。protocolRelated 部分由仓库内材料确证：go.mod:25 modernc.org/sqlite v1.46.1，且现有测试已证明 RENAME COLUMN 语句在该驱动下可执行——缺失的恰是数据保留断言本身，无需借助仓库外记忆。

#### 19. [P3·confirmed] InitLoginPasswords 直接 UPDATE login_password 绕过 Save 与集群 hash 协议，导致 users.hash/updated_at 与行内容失配

- **位置**：`pkg/store/manager.go:99` · 维度：正确性 · 单元：STO
- **问题**：InitLoginPasswords 用裸 SQL `UPDATE users SET login_password = ? WHERE username = ?` 修改 login_password，既不刷新 updated_at（Save 每次写 datetime('now')），也不重算 hash/updated_at_us。而 pkg/proxy/usermanager/sync/hash.go 的 ComputeHash 在 LoginPassword 非空时将其纳入 SHA-256 摘要（hash.go:58-62 附近的注释明确说明）。混合版本集群场景下，从旧节点同步来的用户 login_password 为空但 UpdatedAtUs != 0，本节点重启时 InitLoginPasswords 会将其 bcrypt 填充，但 BackfillClusterFields 只对 UpdatedAtUs==0 的用户重新 stampVersion（usermanager.go:1673-1681），于是 DB 中存储的 hash 是按空 login_password 算的旧值，与行内容永久失配：本地变更对 digest 比对不可见、不会向集群传播，违反了 hash=规范字段摘要 的同步不变量。修复应改走 UserStore.Save 或在 UPDATE 后按 sync.ComputeHash 重算并回写 hash。
- **证据**：manager.go:94-102 仅 Exec `UPDATE users SET login_password = ?`；sync/hash.go 中 writeField 序列包含非空 LoginPassword；usermanager.go BackfillClusterFields 对 `if u.UpdatedAtUs != 0 { continue }` 直接跳过
- **核实**：manager.go:99 确为裸 UPDATE users SET login_password，不刷新 updated_at 也不重算 hash/updated_at_us。sync/hash.go:57-62 确认 ComputeHash 在 LoginPassword 非空时将其纳入 SHA-256 摘要；usermanager.go:1674 BackfillClusterFields 对 UpdatedAtUs!=0 直接 continue。触发链真实可达：SyncUpsertUser（usermanager.go:1569 及 :1605-1608 注释'old nodes send empty'）会把旧节点同步来的 login_password 为空但 UpdatedAtUs!=0 的用户 Save 入 users 表；cmd/server.go:126 InitLoginPasswords 在 :130 加载用户之前执行填充，:222 BackfillClusterFields 跳过，DB 中 hash 与行内容永久失配。仓库内其余写路径均经 stampVersion 维持 hash 不变量，此处为唯一例外且无设计注释豁免。P3 恰当：digest 比对在正常收敛场景不受影响，实际危害限于 login_password 变更不传播与 hash 失配。

#### 20. [P3·confirmed] UserStore 的 Delete/Get/ListByGroup/Count 四个方法全仓库零非测试消费者，tombstone 用户行没有任何硬删路径而无限累积

- **位置**：`pkg/store/user_store.go:21` · 维度：架构 · 单元：STO
- **问题**：usermanager 是 UserStore 的唯一消费者，但只调用 Load（usermanager.go:184）和 Save（13 处 m.store.Save）。grep 全仓库（pkg/、cmd/，排除测试）没有任何地方调用 store 层的 Delete/Get/ListByGroup/Count；用户删除全部走 MarkDeleting 打 tombstone（deletion_state='deleting'）后 Save 回写，从未有代码对 users 表执行 DELETE。后果：(1) 接口一半以上方法是仅由测试供养的死 API，三处 21 列 Scan 拷贝（Get/ListByGroup）纯粹为死代码维护；(2) 被删用户的行（含 auth_token 明文、port_mappings）永久留存在 DB 中无 GC/TTL，users 表与 Load 的启动内存占用单调增长，与集群 tombstone 传播需求之间缺一个『过期 tombstone 清理』机制来闭环。建议要么给 tombstone 加定期硬删（利用现成的 Delete），要么把死方法从接口裁掉。
- **证据**：grep 'ListByGroup|\.Count\(\)|store\.Delete' 在 pkg/、cmd/ 非测试代码零命中；usermanager.go 对 m.store 仅有 Save/Load 调用；cleanupExpiredUsers/同步路径只 MarkDeleting 不删行
- **核实**：user_store.go:15-29 接口确含 Delete/Get/ListByGroup/Count 六方法。全仓非测试代码核实：UserStore 唯一消费者为 usermanager（usermanager.go:178/184 经 storeMgr.UserStore()），其对 m.store 的全部 25 处调用只有 Save/Load；grep 全仓 .Delete(/.ListByGroup(/.Count( 非测试命中均为 InboundStore/map/clusterState，无 UserStore 调用；DELETE FROM users 仅存在于 user_store.go:164 的实现内部。usermanager.go:541-542 注释自证 'The user stays in memory and store — it is never physically deleted'，cleanupExpiredUsers(:1400) 走 RemoveUser→MarkDeleting 打 tombstone，全仓无过期 tombstone 硬删/GC 机制。死 API 与 tombstone 无限累积两点均成立。

#### 21. [P3·confirmed] WithTx 在 Commit 失败时 defer 仍对已终结事务调用 Rollback，必然产生 ErrTxDone 噪声日志掩盖真实提交错误

- **位置**：`pkg/store/tx.go:26` · 维度：错误处理 · 单元：STO
- **问题**：WithTx 的 defer 只判断 retErr != nil 就调 tx.Rollback()。当 fn 成功但 `return tx.Commit()` 返回错误时（磁盘满、WAL 写失败等），database/sql 的 Tx.Commit 在进入时已通过原子 CAS 把事务标记为 done，无论驱动层提交成败，事务对象都已终结；此时 defer 里的 tx.Rollback() 必然返回 sql.ErrTxDone，于是每次真实的提交失败都会额外打出一条误导性的 'store.WithTx: rollback failed: sql: transaction has already been committed or rolled back'，把排障注意力从真正的 Commit 错误引开。应在 defer 中排除 Commit 已执行的情形（如用布尔标记 committed），或忽略 ErrTxDone。
- **证据**：tx.go:16-22 defer 无条件 Rollback + tx.go:26 `return tx.Commit()`；database/sql Tx.Commit 先 CAS done 标志后才调驱动 Commit，失败也不可再 Rollback
- **核实**：tx.go:16-22 defer 仅以 retErr != nil 为条件调用 tx.Rollback()，:26 return tx.Commit()。已在本机 go1.24.4 GOROOT 源码核实 database/sql Tx.Commit：先 tx.done.CompareAndSwap(false, true) 置位，再调驱动 txi.Commit，驱动提交失败也不复位 done；此后 Rollback 必返回 ErrTxDone。WithTx 的 ctx 为调用方传入（仓库内两处均为 Background），排除了唯一不置 done 的 ctx.Done 分支。因此每次真实 Commit 失败都会额外打出误导性 'rollback failed: sql: transaction has already been committed or rolled back' 日志。P3 恰当（仅日志噪声，原始错误仍正确返回）。

#### 22. [P3·confirmed] store 层完全不透传 context：WithTx 调用方一律 context.Background()，全部 Query/Exec 无超时，MaxOpenConns(1) 下挂起操作使后续所有 DB 调用不可取消地排队

- **位置**：`pkg/store/node_groups_store.go:70` · 维度：并发 · 单元：STO
- **问题**：WithTx 虽然接收 ctx 参数，但仓库内仅有的两个调用方都硬编码 context.Background()（node_groups_store.go:70、migrate.go:59）；UserStore/InboundStore/StoreManager 的全部方法签名不带 context，内部用无 ctx 的 db.Query/Exec/QueryRow。在 db.go SetMaxOpenConns(1) 的单连接池下，任何一个慢操作/挂起的事务（例如 WithTx 回调阻塞、磁盘 IO 卡顿）都会让所有 HTTP/gRPC 请求路径上的 DB 调用在连接池等待队列里无限期阻塞，既无超时也无取消途径，服务表现为整体僵死而非局部报错。与 db.go:22 注释『reads can share the pool』的说法也不符——单连接下读同样被串行阻塞。建议接口下沉 context 参数（至少给 Save/Load 这类热路径），并让 WithTx 调用方接入请求级 ctx。
- **证据**：node_groups_store.go:70 与 migrate.go:59 均为 WithTx(context.Background(), ...)；user_store.go/inbound_store.go/manager.go 全部使用 Query/Exec 非 Context 变体；db.go:23 SetMaxOpenConns(1)
- **核实**：逐项核实属实：WithTx 仓库内仅有两个调用方 node_groups_store.go:70 与 migrate.go:59，均硬编码 context.Background()；pkg/store 全目录 grep QueryContext/ExecContext/QueryRowContext 零命中，UserStore/InboundStore/StoreManager 方法签名均不带 context；db.go:23 SetMaxOpenConns(1)，:22 注释确为 'reads can share the pool'——单连接下读写实际全部串行，注释与行为不符。database/sql 在 MaxOpenConns(1) 下连接等待走 ctx（此处均为 Background）无限期阻塞，busy_timeout pragma 只管 SQLITE_BUSY 不管池等待，故任一挂起事务会使所有 DB 调用不可取消地排队。作为设计级健壮性问题 P3 恰当。

#### 23. [P3·confirmed] 测试缺口：WithTx 的 panic 路径与 Commit 失败路径、Migrate 中途失败的恢复语义、单连接池并发行为均无任何用例

- **位置**：`pkg/store/db_test.go:113` · 维度：测试缺口 · 单元：STO
- **问题**：db_test.go 对 WithTx 只有 Commit/Rollback 两个 happy/error 用例，未覆盖：(1) fn panic 时事务泄漏、单连接池被永久占用的行为（pass1 已确认该缺陷为 P2，但没有测试钉住修复后的语义）；(2) Commit 失败时的返回值与日志行为；(3) Migrate 在第 N 个迁移失败后前 N-1 个已提交、失败版本已回滚、修复后可续跑的『部分应用可恢复』语义（现有测试只测 gap 与幂等）；(4) SetMaxOpenConns(1) 下多 goroutine 并发 Save/Load/Set 的基本无死锁冒烟（如 go test -race 下并发跑 UserStore.Save 与 NodeGroupsStore.Set）。这些是本模块最核心的并发/资源不变量，当前全部零覆盖，修 WithTx panic 缺陷时极易回归。
- **证据**：db_test.go 全文件仅 TestWithTx_Commit/TestWithTx_Rollback 与 4 个 Migrate 用例；grep 测试目录无 recover/panic/并发 goroutine 相关的 store 用例
- **核实**：db_test.go 全文核实：仅 TestOpenClose/TestDB_ExposesUnderlying、4 个 Migrate 用例（FirstRun/Idempotent/VersionGap/PartialThenExtend）与 TestWithTx_Commit/TestWithTx_Rollback，无 panic 恢复、Commit 失败、迁移中途失败可恢复、并发/race 用例（finding 称'仅 WithTx 两用例与 4 个 Migrate 用例'漏提了 2 个 Open 用例，属无关紧要的不精确）。所指缺陷路径读码可证：tx.go defer 仅判 retErr != nil，fn panic 时命名返回值为 nil → 不 Rollback，泄漏的事务持有 SetMaxOpenConns(1) 的唯一连接；Migrate（migrate.go:59 每版本独立 WithTx）的部分应用语义确实只有 gap/幂等测试覆盖。测试缺口成立。

#### 24. [P3·confirmed] requireUint32 的 uint32 分支缺 >65535 上界检查，与其余全部数值分支不一致

- **位置**：`pkg/proxy/core/params/protocolparams/parser.go:163` · 维度：正确性 · 单元：CTR
- **问题**：uint/int/int32/int64/float64 分支全部拒绝 >65535（167-190 行），唯独 `case uint32: return x, nil`（162-163 行）无任何范围检查。类型化调用方传入 uint32(70000) 会得到一个合法返回的非法端口，直到容器层才失败。当前生产入口（proto int32、JSON float64）不会命中该分支，故为一致性缺陷而非线上 bug，但该 helper 的文档声称 'Out-of-range values produce ErrMissingRequired-wrapped errors'，uint32 分支违反自述契约。
- **证据**：parser.go:162-163 直接 `return x, nil`；对比 165-190 行每个分支的 `> 65535` 检查与 153-155 行的文档承诺
- **核实**：parser.go:162-163 的 uint32 分支确实无任何范围检查直接返回，uint/int/int32/int64/float64 分支（166-190 行）全部有 >65535 拒绝，153-155 行文档明确承诺越界值产生 ErrMissingRequired 包装错误——uint32 分支违反自述契约属实。当前生产入口（HTTP JSON float64、RPC proto int32）均不命中该分支，仅类型化调用方可触发，一致性缺陷定性准确，P3 恰当。

#### 25. [P3·confirmed] h2 传输是全协议不可达的死路径：文档称'used by vmess'但 parseVMess 拒绝 h2，且 "h2" 无 contracts.Transport 常量、IsValid 为 false

- **位置**：`pkg/proxy/core/params/protocolparams/protocolparams.go:106` · 维度：架构 · 单元：CTR
- **问题**：三处互相矛盾：(1) TransportSpec 注释 'h2 (HTTP/2 inbound, used by vmess)'（106 行）及 HTTPPath/HTTPHost 字段；(2) parseTransport 把 http 归一化为 h2 并在 kind 门放行（transport.go:34-45），还有专门的字段读取分支（70-72 行）；(3) 但 parseVLESS（params_vless.go:53-55）、parseVMess（params_vmess.go:51-53）、parseTrojan（params_trojan.go:41-43）全部显式拒绝 h2，hy2/tuic/anytls 根本不走 parseTransport——即 Kind="h2" 的 TransportSpec 在任何协议下都无法从 Parse 存活，h2 字段读取与 HTTPPath/HTTPHost/KeyHTTPPath/KeyHTTPHost 是死代码。且 "h2" 是散落在 4 个文件的裸字符串字面量，contracts/protocol.go 没有 TransportH2 常量，contracts.Transport("h2").IsValid()==false——一旦未来某协议放行 h2，持久化到 InboundStore 的将是一个 contracts 层非法的枚举值。应要么补 contracts 常量并真正支持，要么删除死路径并修正注释。
- **证据**：protocolparams.go:106-108 vs params_vmess.go:51-53/params_vless.go:53-55/params_trojan.go:41-43 全拒绝；contracts/protocol.go:56-88 无 h2 常量，IsValid 不含 h2；transport.go:45,70-72 裸字面量
- **核实**：三处矛盾全部读码确证：protocolparams.go:106 注释称 h2 'used by vmess'，但 params_vmess.go:51-53 显式拒绝 h2；parseTransport 仅被 vless/vmess/trojan 三个 parser 调用（grep 确认），三者全拒 h2，hy2/tuic/anytls 不走该函数——Kind=h2 的 TransportSpec 无法从 Parse 存活，transport.go:70-72 的字段读取与 HTTPPath/HTTPHost 在该管线内为死代码。contracts/protocol.go 常量表/AllTransports/IsValid 均无 h2，Transport("h2").IsValid()==false。更甚：RPC handler（end_node_inbound.go:214-215）把 HttpBuilderType 映射为 "h2"、HTTP handler validTransports 放行 "h2"，入口层接受但 parse 层必拒，死路径确凿。

#### 26. [P3·confirmed] "h2" 是 contracts.Transport 枚举外的伪值，且 "http"→"h2" 归一化大小写敏感，与全解析链的大小写不敏感约定相悖

- **位置**：`pkg/proxy/core/params/protocolparams/transport.go:34` · 维度：正确性 · 单元：CTR
- **问题**：两个相互纠缠的枚举一致性缺陷：(1) transport.go:34 `if kindStr == "http"` 在 ToLower/TrimSpace 之前做精确匹配——caller 传 "HTTP" 或 " http" 不会被归一化为 h2，随后 lower 成 "http" 落入 gate 的 default 分支报 "not supported"，而小写 "http" 却能通过归一化；同一函数里 kind 本身是大小写不敏感解析的（line 37），行为分裂。(2) 归一化产物 contracts.Transport("h2") 不在 contracts.Transport 枚举中：protocol.go 的常量表、AllTransports、IsValid 都不认识 "h2"，gate（transport.go:45）和三个 parseXxx 的 reject 分支只能用裸字符串字面量 "h2"，TestProtocolAllTransportsMatchesIsValid 的锁步机制对它完全失效。当前 vless/vmess/trojan 全部显式拒绝 h2，hy2/tuic/anytls 不走 parseTransport，因此 h2 分支（line 70-72）及 TransportSpec.HTTPPath/HTTPHost 字段实际不可达，属于以 JSON schema 形式持久化预留的死路径。应要么在 contracts 增加 TransportH2 常量纳入锁步测试，要么删掉 h2 归一化让 http 走统一拒绝路径。
- **证据**：transport.go:34-37 归一化在 lower/trim 之前；transport.go:45 gate 列表含裸字符串 "h2"；contracts/protocol.go:56-86 无 h2 常量，IsValid("h2")==false；params_vless.go:53、params_vmess.go:51、params_trojan.go:41 全部 `case "h2": return ErrInvalidCombination`。
- **核实**：两点均读码确证：(1) transport.go:34 `if kindStr == "http"` 精确匹配在 line 37 的 ToLower/TrimSpace 之前，传 "HTTP"/" http" 不被归一化、lower 后落入 gate default 分支报错，而小写 "http" 成功归一为 h2——同函数内 kind 本身大小写不敏感，行为分裂属实且经 RPC GetTransport 可达（HTTP handler 的 validTransports 也是小写精确匹配，"HTTP" 在那层就 400，同样体现分裂）。(2) contracts/protocol.go 无 TransportH2 常量，IsValid("h2")==false，gate（transport.go:45）与三个 parseXxx 用裸字面量 "h2"；锁步测试实名为 TestTransportAllMatchesIsValid（finding 写作 TestProtocolAllTransportsMatchesIsValid，名字微偏但机制描述正确——h2 非常量故不受锁步保护）。h2 分支在 Parse 管线不可达的论证与 idx 1 一致成立。P3 恰当。

#### 27. [P3·confirmed] optionalInt 用 Sscanf("%d") 解析字符串，接受尾随垃圾——"30s" 被静默解析为 30，恰好击穿 anytls 苦心设计的单位混淆防护

- **位置**：`pkg/proxy/core/params/protocolparams/transport.go:164` · 维度：正确性 · 单元：CTR
- **问题**：optionalInt 的 string 分支（transport.go:163-167）用 fmt.Sscanf(x, "%d")，Sscanf 对尾随未匹配文本不报错："30s"→(30,true)、"12abc"→(12,true)、"1e3"→(1,true)。parseAnyTLS 专门为 idle_session_* 字段写了 86400 秒上限并在报错文案里提示 "likely unit confusion (value is seconds, not ms)"，但操作员真的传 "30s"/"5m" 这类带单位字符串时反而被静默截断为数字前缀接受，防线形同虚设；alter_id="1x" 同理。同包 security.go:152 的 parseSignedInt 对非数字字符严格报错，一个包里两套字符串→int 语义。应改用 strconv.Atoi 全串匹配。
- **证据**：transport.go:163-167 `case string: if _, err := fmt.Sscanf(x, "%d", &parsed); err == nil { return parsed, true }`；params_anytls.go:120 报错文案 "likely unit confusion (value is seconds, not ms)"；security.go:164-169 parseSignedInt 逐字符校验 `if c < '0' || c > '9' { return 0, err }`。
- **核实**：亲测确认：transport.go:163-167 的 string 分支用 fmt.Sscanf(x, "%d", &parsed)，在本机 go run 实证 "30s"→30、"5m"→5、"12abc"→12、"1e3"→1、"1x"→1 均 err=nil 被静默接受。parseAnyTLS（params_anytls.go:114-146）对 idle_session_check_interval/idle_session_timeout/min_idle_session 全部经 optionalInt 读取，且 params_anytls.go:120/131 的报错文案确实写着 "likely unit confusion (value is seconds, not ms)"——操作员传 "30s"/"5m" 带单位字符串时恰好被截断为数字前缀绕过该防线。同包 security.go:152-171 parseSignedInt 逐字符拒绝非数字，包内两套字符串→int 语义分裂属实。P3 恰当。

#### 28. [P3·confirmed] parseSSPluginOpts 的 plugin_tls 又一处手写布尔解析：大小写敏感且不认 "yes"，与 optionalBool 语义第三次分裂

- **位置**：`pkg/proxy/core/params/protocolparams/params_ss.go:85` · 维度：正确性 · 单元：CTR
- **问题**：parseSSPluginOpts（params_ss.go:85-94）对 KeyPluginTLS 手写 type switch：字符串仅精确匹配 "true"/"1"，"True"/"TRUE"/"yes" 被静默丢弃（不设置 tls），且 false 值无论何种形态都不落入 opts——与包内标准 optionalBool（大小写不敏感、trim、支持 yes/no）是第三套布尔语义（第一套 optionalBool，第二套 parseSS 的 udp 手写解析——后者 pass1 已报）。v2ray-plugin 的 tls 开关被静默吞掉的后果是订阅生成的客户端配置缺 tls=true，客户端连不上却无任何报错。三处应统一收敛到 optionalBool。
- **证据**：params_ss.go:85-94 `case string: if v == "true" || v == "1"`，无 ToLower/TrimSpace；对比 transport.go:123-140 optionalBool 的 `strings.ToLower(strings.TrimSpace(x))` + "yes"/"no" 支持。与 pass1 params_ss.go:42（udp 解析）同类但不同站点、不同字段。
- **核实**：params_ss.go:85-94 核实：KeyPluginTLS 的 string 分支仅精确匹配 "true"/"1"，无 ToLower/TrimSpace，"True"/"TRUE"/"yes" 被静默丢弃（tls 不写入 opts）。对比 transport.go:123-140 optionalBool 确实做 strings.ToLower(strings.TrimSpace(x)) 且支持 yes/no；parseSS 的 udp 手写解析（params_ss.go:42-55，同样大小写敏感）构成第二套；此处为第三套布尔语义。注释（params_ss.go:25）确认 v2ray-plugin 的 tls 走此字段进订阅，静默丢弃即客户端配置缺 tls=true。一处保留意见：false 形态不写入 opts 对 presence-only 的 plugin-opts 而言可能是有意为之，但大小写/别名不一致的核心主张成立。P3 恰当。

#### 29. [P3·confirmed] 三个 Validate（InboundSpec/Config/DefaultInbound）都不校验 Protocol.IsValid()，枚举校验器与枚举定义脱节

- **位置**：`pkg/proxy/core/contracts/inbound.go:26` · 维度：正确性 · 单元：CTR
- **问题**：contracts.Protocol 提供了 IsValid()（protocol.go:41）且有锁步测试保证枚举完整性，但模块内所有结构级校验器都不调用它：contracts.InboundSpec.Validate（inbound.go:26-34）只查 tag 和 port；inbound.Config.Validate（inbound/inbound.go:44-52）同样只查 tag/port；DefaultInbound.Validate（default.go:96-104）亦然。Protocol 为空串或任意垃圾值的 InboundSpec/Config 全部通过 Validate，直到容器层 dispatch 才以各自不同的错误形态失败（mihomo 报 ErrProtocolNotSupported，其他容器行为各异）。本 lens 的核心——AllXxx/IsValid 与校验器一致性——在这三处断裂：枚举有校验能力但没有任何 Validate 使用。三处各加一行 `if !Protocol.IsValid()` 即可让非法协议在契约层就近失败。
- **证据**：contracts/inbound.go:26-34 Validate 无 Protocol 检查；inbound/inbound.go:44-52 Config.Validate 同；inbound/default.go:96-104 DefaultInbound.Validate 同；而 protocol.go:41 IsValid 与 protocol_test.go 锁步测试专为此存在。
- **核实**：三处校验器核实无 Protocol 检查：contracts/inbound.go:26-34 只查 tag+port；inbound/inbound.go:44-52 同；inbound/default.go:96-104 同。protocol.go:41-49 IsValid 存在且 protocol.go:28-31 注释确认有锁步测试。垃圾 Protocol 的确穿透这三个 Validate：xray Adapter.ValidateInbound（inbound_adapter.go:44-46）直接返回 spec.Validate()，不会拦截；mihomo 在 MihomoInbound.Validate 的 switch default（inbound.go:207）才报 ErrProtocolNotSupported。需注明的缓解：FastAdd 主路径 protocolparams.Parse（parser.go:38-40）已在入口做 IsValid 门禁，故实际触发面限于绕过 Parse 直接构造 InboundSpec/Config 的路径（xray adapter、store 回放等），属防御纵深缺口而非主链路漏洞——与 P3 定级一致。

#### 30. [P3·confirmed] autofillReality 不校验 caller 提供的 Reality 公私钥是否配对，错配密钥对静默通过导致全体客户端握手失败且无处报错

- **位置**：`pkg/proxy/core/params/protocolparams/security.go:197` · 维度：正确性 · 单元：CTR · ⚠️协议相关（需人工核对上游）
- **问题**：autofillReality（security.go:196-202）对 caller 同时提供的 private_key/public_key 只做各自的格式校验（ValidateRealityBase64Key：base64 解码为 32 字节），不验证 public == ScalarBaseMult(private)。监听器用 PrivateKey 起 Reality，订阅层把 PublicKey 发给客户端；两者错配（复制粘贴串了两套 keypair 是常见操作事故）时 inbound 创建成功、监听器正常启动、订阅正常下发，但所有客户端 Reality 握手必然失败，故障面上没有任何一层报错，只能靠抓包排查。包内已有 curve25519 依赖和解码逻辑，补一次 ScalarBaseMult 对比约 10 行，且与本包 "strict-by-design、mistyped key fails loudly at parse time"（reality_helpers.go:94-95 注释）的自我定位一致。
- **证据**：security.go:196-202 仅 `ValidateRealityBase64Key(rc.PrivateKey)` + `ValidateRealityBase64Key(rc.PublicKey)`，无配对性检查；reality_helpers.go:50-68 GenerateRealityKeyPair 已具备 ScalarBaseMult 推导能力；reality_helpers.go:93-95 注释宣称格式错误应 "fails loudly at parse time instead of silently at reality handshake"——错配密钥恰是 silent-at-handshake 的情形。
- **核实**：security.go:191-210 核实：privPresent 分支仅对两个 key 各调 ValidateRealityBase64Key（格式：base64 解码为 32 字节），无 ScalarBaseMult 配对性检查。仓库内材料确证数据流与后果主张：监听器侧 native config 只用 privateKey（xray inbound_adapter.go:596 realitySettings["privateKey"]）；订阅侧只下发 PublicKey（mihomo subscription.go:248-250/396-397、xray vless_profiles.go:439-440 reality_public_key）——两者错配时确无任何一层交叉验证。X25519 DH 要求 client 持有的 pub 必须等于 server priv 的 ScalarBaseMult 结果，否则共享密钥不一致握手必败，这是密码学定义而非第三方实现细节；且仓库自证材料齐备：reality_helpers.go:93-95 注释明言设计目标是 "fails loudly at parse time instead of silently at reality handshake"，reality_helpers.go:62-63 与 xray inbound_adapter.go:712-714（DeriveRealityPublicKey）证明包内已有由 priv 推导 pub 的能力，补配对检查成本极低。P3 恰当（错误配置类，非代码在正确输入下出错）。

#### 31. [P3·confirmed] Reality 公私钥对只校验各自格式，不校验公钥与私钥的数学一致性

- **位置**：`pkg/proxy/core/params/protocolparams/security.go:196` · 维度：正确性 · 单元：CTR · ⚠️协议相关（需人工核对上游）
- **问题**：autofillReality 对 caller 同时提供的 reality_private_key/reality_public_key 只调用 ValidateRealityBase64Key 分别检查'base64 且 32 字节'（196-202 行），不验证 PublicKey == ScalarBaseMult(PrivateKey)。监听器使用 PrivateKey 工作，订阅层向所有客户端下发 PublicKey；一旦运维拷错其中一个（例如把另一个节点的 public key 粘过来），inbound 正常创建、监听器正常启动，但所有客户端 Reality 握手全部失败，故障面在客户端侧，极难排查。本包已 import curve25519，用 ScalarBaseMult 复算一次比对成本可忽略，可在 parse 时把这种错配变成即时 400。
- **证据**：security.go:196-202 仅逐个 ValidateRealityBase64Key；reality_helpers.go:50-68 已具备 ScalarBaseMult 能力却未用于一致性校验
- **核实**：security.go:196-202 对成对提供的 key 仅逐个调用 ValidateRealityBase64Key（只查 base64 + 32 字节，reality_helpers.go:98-114），无 pub==ScalarBaseMult(priv) 一致性比对，代码事实确凿。公私钥的数学配对关系由仓库自身代码确证：reality_helpers.go:50-68 GenerateRealityKeyPair 正是用 curve25519.ScalarBaseMult 从私钥导出公钥（包已 import curve25519）。故障面主张也有仓库内证据：mihomo/profilegen.go:574-575 监听器配置只写 private-key，subscription/converter/clash.go:416-427 向客户端下发 PublicKey——两者错配时监听器正常启动而所有客户端握手失败，与 finding 描述一致。P3 恰当（需运维粘错键才触发）。

#### 32. [P3·confirmed] parseSignedInt 接受负值且无溢出保护：reality_max_time_diff=-5 或 20 位大数都能通过'必须为整数秒'校验

- **位置**：`pkg/proxy/core/params/protocolparams/security.go:152` · 维度：正确性 · 单元：CTR · ⚠️协议相关（需人工核对上游）
- **问题**：parseReality 在 141-145 行用 parseSignedInt 校验 reality_max_time_diff'必须是整数'，但 parseSignedInt 显式支持负号（155-159 行），而 Reality 的 max time difference 语义上是非负时长（xray 侧为 uint64 毫秒），负值应被拒绝却被放行；同时 168 行 n = n*10 + int(c-'0') 无溢出检查，超长数字串在 int64 上静默回绕，可能'校验通过'一个实际为负或无意义的值，最终由 profilegen 写进 mihomo/xray 配置产生下游不可预期行为。校验器本意是'把无效时钟偏移配置在 parse 期变为可见错误'（139-140 行注释），负数与溢出恰是最典型的无效输入。
- **证据**：security.go:155-159 显式接受 '-' 前缀；168 行累乘无溢出检查；141-144 行仅以 parseSignedInt 是否报错为准
- **核实**：代码事实全部核实：security.go:155-159 parseSignedInt 显式接受 '-' 前缀，168 行 n=n*10+int(c-'0') 无溢出检查。负值路径在仓库内可完整追踪：parseReality:141-145 放行 "-5"，mihomo/profilegen.go:580-583 strconv.Atoi("-5") 成功后把负数写进 max-time-difference 推给 mihomo。溢出路径更能在仓库内闭环：20 位数字串在 parseSignedInt 静默回绕通过校验，但 profilegen.go:581 的 strconv.Atoi 对超范围返回错误、err==nil 分支静默丢弃——这恰好复活了 139-140 行注释声称要消除的'silently drop'行为，校验器承诺与实际行为自相矛盾。小瑕疵：finding 称 xray 侧为 uint64 毫秒，仓库内 xray/inbound_adapter.go:616 实际按 int64 读取，该细节不准确但不影响结论。P3 恰当。

#### 33. [P3·confirmed] optionalStringSlice 的 []string 分支不过滤空串且直接别名调用方切片，与 []any/逗号分隔分支语义不一致

- **位置**：`pkg/proxy/core/params/protocolparams/transport.go:93` · 维度：正确性 · 单元：CTR
- **问题**：[]any 分支（96-99 行）和 string 分支（106-113 行）都会剔除空串，唯独 []string 分支直接 `return x`：一不过滤空元素，二返回对调用方底层数组的别名。RPC 路径恰好命中此分支——end_node_inbound.go:270-276 把 alpn/reality_server_names/http_host 用 strings.Split 得到 []string 放进 raw map，`"reality_server_names": " ,"` 这类输入经 Split 产生 [" ",""]，绕过 parseReality 的 len(ServerNames)==0 必填校验（security.go:124-126），把空 SNI 写进 RealitySpec 并最终进入 mihomo 配置；alpn 同理可携带空串。别名问题则让后续对 raw map 的修改隐式改动已解析的 spec，违反 parser.go:18-19 'Parse is read-only' 的注释契约精神。
- **证据**：transport.go:92-93 `case []string: return x` vs 96-99/106-113 行的空串过滤；pkg/rpc/server/end_node_inbound.go:275 `reqParams[k] = strings.Split(v, ",")`；security.go:124-126 仅检查 len==0
- **核实**：transport.go:91-93 `case []string: return x` 直接返回，与 []any 分支（96-101 过滤空串）和 string 分支（106-113 TrimSpace+过滤）语义确实不一致。RPC 触发路径真实存在：end_node_inbound.go:276-277 对 extra_params 中的 reality_server_names/alpn/http_host 等做 strings.Split(v, ",") 得 []string 放入 reqParams（260 行的 proto repeated string 直传也是 []string），" ," 经 Split 产生 [" ",""]，len==2 绕过 security.go:124-126 的 len(ServerNames)==0 必填校验，空白 SNI 经 profilegen.go:568-569 原样写进 mihomo server-names。别名问题同样属实：返回调用方切片的引用，与 parser.go:18 'Parse is read-only: it does not mutate raw' 的契约存在张力（反向：改 raw 底层数组会隐式改 spec）。P3 恰当。

#### 34. [P3·confirmed] Restart() 无条件补调 MarkRunning，可把仍在执行 runFunc 的 Starting 容器提前标为 Running，随后 Stop 会跳过停止钩子造成孤儿进程

- **位置**：`pkg/proxy/core/container/base.go:297` · 维度：并发 · 单元：CON
- **问题**：Restart() 在 b.Start() 返回 nil 后再调一次 b.MarkRunning()（base.go:297）。正常路径这是冗余（Start 成功内部已 MarkRunning），但 Start() 对 Starting 状态是幂等返回 nil（base.go:149-152）——并不代表启动完成。交错场景：goroutine A 调 Start()，状态置 Starting，解锁后正在执行 runFunc；goroutine B 调 Restart()，其 Stop() 把 Starting 打成 Stopped（Starting 分支），A 的 runFunc 仍在跑；B 的 Start() 从 Stopped 再次进入……或更直接地：B 的 Start() 撞上 A 的 Starting 幂等返回 nil，B 随即 MarkRunning() 把状态从 Starting 提为 Running，破坏了『只有 runFunc 完成后才 Running』的不变量。此刻若有 Stop()，走 Running 分支但 stopFunc 尚未缓存（b.stopFunc==nil，base.go:228/259），停止钩子被静默跳过并置 Stopped；随后 A 的 runFunc 完成又缓存 stopFunc（base.go:192）——真实进程已启动却无人能停。该路径独立于第一轮报的 Stop 晚完成覆盖 Running（#153/#270）：这里是 Restart 主动伪造 Running。修复方向：删掉 Restart 里的 MarkRunning，或让 Start 对 Starting 状态阻塞等待而非幂等返回。
- **证据**：base.go:293-298 `if err := b.Start(); err != nil { return err }\n// Mark as running after restart\nb.MarkRunning()`；base.go:149-152 Starting 幂等返回 nil；base.go:203-205 MarkRunning 将 Starting→Running
- **核实**：代码逻辑核实成立：base.go:293-297 Restart 在 Start 返回 nil 后无条件 MarkRunning；base.go:149-152 Start 对 Starting 态幂等返回 nil（不代表 runFunc 完成）；base.go:200-206 MarkRunning 将 Starting→Running。交错路径可复现：B.Restart 的 Stop 完成后 A.Start 进入 Starting 执行 runFunc，B.Start 撞 Starting 幂等返回 nil，B 的 MarkRunning 把状态伪造成 Running；此时 Stop 走 Running 分支但 stopFunc 尚未缓存(仅在 runFunc 成功后 base.go:192 缓存)，停止钩子被跳过并置 Stopped；A 的 runFunc 完成后进程已启动而状态 Stopped，后续 Stop 幂等短路，孤儿进程成立。但可达性受限：BaseContainer.Restart 全仓唯一入口是 xray Reload(exec_runner.go:463)，而 .Reload() 无任何生产调用者；snell/hysteria/mihomo 的 Container.Restart 继承自基类但同样 grep 不到调用点。缺陷真实存在于导出 API 契约，但当前仓库无触发路径且需特定并发交错，降为 P3。

#### 35. [P3·confirmed] StopChan 是零消费方的死 API，且语义残缺：Start 失败路径与 Stopped 态 Stop 永不 close 该 channel，按文档使用必然泄漏等待者

- **位置**：`pkg/proxy/core/container/base.go:280` · 维度：架构 · 单元：CON
- **问题**：全仓 grep `StopChan` 仅命中 base.go 定义和 base_test.go 自测，四个具体容器（xray/hysteria/snell/mihomo）没有任何一处用它协调停止——各自都实现了自己的 stop channel（如 snell 的 reconcileStopCh）。同时它的契约（'The channel is closed when Stop() is called'）在多条路径上不成立：(1) Start() 在 Stopped/Stopping 分支新建 stopChan（base.go:156/162），若 runFunc 失败，状态回 Stopped 但该 channel 无人 close，后续 Stop() 因 Stopped 幂等短路（base.go:231-234）也不 close，下次 Start 直接替换引用——在这个 channel 上等待的 goroutine 永久阻塞；(2) 重启后旧 channel 引用与新一代生命周期脱节，持有旧引用者观察到的是上一代的关闭事件。作为对外导出的协调原语，这是一个引导 embedder 写出泄漏代码的陷阱 API，应当删除或修复失败路径 close 语义并配文档。
- **证据**：grep 全仓 StopChan 仅 base.go:278-284 与 base_test.go；base.go:182-188 失败路径无 close(b.stopChan)；base.go:162 下次 Start `b.stopChan = make(chan struct{})` 直接替换
- **核实**：核实成立：全仓 grep StopChan 仅命中 base.go:278-284 定义与 base_test.go，四个容器实现均未使用（各自维护 reconcileStopCh 等私有 channel），确为零消费方的导出 API。契约缺口属实：文档(base.go:279)称 'closed when Stop() is called'，但 (1) Start 失败路径(base.go:182-188)状态回 Stopped 而 stopChan 不 close，后续 Stop 因 Stopped 幂等短路也不 close，下次 Start(base.go:156/162)直接 `b.stopChan = make(chan struct{})` 替换引用，等待旧 channel 的 goroutine 永久阻塞；(2) 重启后旧引用与新生命周期脱节。按文档使用必然踩坑，作为诱导性陷阱 API 报 P3 恰当。

#### 36. [P3·confirmed] LoadFromConfig 对同类型重复 entry（或重复调用）静默覆盖旧实例且不 Stop：被覆盖实例在 factory.New 时已订阅 UserManager 并启动 goroutine，直接泄漏且事件双份消费

- **位置**：`pkg/proxy/core/container/manager.go:81` · 维度：资源 · 单元：CON
- **问题**：LoadFromConfig 的写入是 `m.instances[entry.Type] = instance`，既不检测配置里同一 type 出现两次，也不处理二次 LoadFromConfig 时 map 中已有实例的情形——旧实例被静默丢弃，无 Stop、无任何日志。而实例在 factory.New 阶段就有持久副作用：xray 的 NewExecutor 在 UserManager 非空时立即 `go e.forwardUserEvents(cfg.UserManager.Subscribe())`（exec_runner.go:163-166 一带），即每个被丢弃的实例都在 UserManager 上留下一个永久订阅者和一条 goroutine；snell 等实现的 New 也可能落盘/建 channel。后果：配置里误写两条 `type: xray` 时第二条静默胜出，第一条的订阅者继续消费用户事件（与新实例双份处理 add/delete，可能触发重复 GetBindPort），无从发现。建议 LoadFromConfig 对重复 type 返回错误（与 fail-fast 风格一致），至少打 Warn。
- **证据**：manager.go:58-82 循环内直接 `m.instances[entry.Type] = instance` 无 exists 检查；exec_runner.go NewExecutor：`if cfg.UserManager != nil { ... go e.forwardUserEvents(cfg.UserManager.Subscribe()) }` 在 New 时即产生订阅与 goroutine
- **核实**：代码事实核实成立：manager.go:58-82 循环内直接 `m.instances[entry.Type] = instance`，无重复 type 检测、无对旧实例的 Stop、无日志；NewExecutor(exec_runner.go:161-165)在 UserManager 非空时于 New 阶段即 `go e.forwardUserEvents(cfg.UserManager.Subscribe())`，snell NewSnellContainer(container.go:~110)同样 New 时订阅，被覆盖实例确实留下永久订阅者+goroutine。触发路径真实：LoadFromConfig 由 cmd/server.go:175 生产调用，yaml 中重复 type 条目即触发。一处推断过强：被丢弃实例从未 Start，其事件处理 goroutine(startUserEventHandler 仅在 Start 中启动)不会运行，forwardUserEvents 只是填满 100 缓冲后静默丢弃事件，不会'双份处理 add/delete/重复 GetBindPort'——实际后果是静默配置遮蔽+订阅/goroutine 泄漏。核心问题成立，P3 恰当。

#### 37. [P3·confirmed] RegisterFactory 对同类型重复注册静默覆盖，与 legacy RegisterContainer 的重复即报错语义相反，init() 注册冲突完全不可见

- **位置**：`pkg/proxy/core/container/registry.go:188` · 维度：正确性 · 单元：CON
- **问题**：新注册体系 RegisterFactory 的实现是无条件 `factoryMap[kind] = f`，注释明确 'Overwrites any previously registered factory'；而同文件的 legacy RegisterContainer（registry.go:33-42）对重复注册返回 error、RegisterSingleton 同样查重。两套体系语义相反，且新体系恰恰是各容器包 init() 里实际使用的那套（xray/hysteria/snell/mihomo register.go）——init 执行顺序由 import 决定，若两个包（或同一包新旧两个 register 路径）误用同一 ContainerType，后 init 者静默胜出，没有 panic、没有日志，问题只会表现为『启动的是另一个容器实现』这类难排查的现象。init 期重复注册是典型编程错误，标准库惯例（如 database/sql.Register、gob.Register）是 panic 暴露。建议改为重复即 panic 或至少记录覆盖日志。
- **证据**：registry.go:185-189 `func RegisterFactory(...) { factoryMapMu.Lock(); defer ...; factoryMap[kind] = f }` 无查重；对照 registry.go:37-39 legacy 版本 `if _, exists := ...; exists { return fmt.Errorf("already registered") }`
- **核实**：核实成立：registry.go:185-189 RegisterFactory 无条件 `factoryMap[kind] = f`，注释明言 'Overwrites any previously registered factory'；同文件 legacy RegisterContainer(registry.go:37-39)对重复返回 error，RegisterSingleton(57-62)双重查重，两套语义确实相反。新体系确为实际使用路径：xray/snell/mihomo/hysteria 四个包的 register.go init() 均调用 RegisterFactory（已逐一核实），LoadFromConfig/Create 经 GetFactory 走 factoryMap。init 期类型冲突静默后者胜出、无 panic 无日志属实，与 database/sql.Register 等标准库 panic 惯例对照成立。当前四个 ContainerType 常量互异，冲突属编程错误场景，防御性缺陷报 P3 恰当。

#### 38. [P3·confirmed] Container.Init(config any) 的接口文档直接点名 *xray.ExecutorConfig，且 hotreload/hotupdate 确实借此以 xray 原生配置跨层调用，L1 抽象为具体实现留了类型后门

- **位置**：`pkg/proxy/core/container/interface.go:21` · 维度：架构 · 单元：CON
- **问题**：core/container 是 L1 抽象层，架构约束要求它不耦合具体业务、上层只经 contracts 领域模型交互。但 Container.Init 签名是 `Init(config any) error`，接口注释（interface.go:16-18）明文写出 '- xray: *xray.ExecutorConfig'——L1 接口文档引用具体容器包的导出类型；调用侧也确实这么用：pkg/xrayapi/hotreload/manager.go:268、:607 与 pkg/xrayapi/hotupdate/update.go:286 都是 `executor.Init(&xray.ExecutorConfig{...})`，即上层通过这个 any 通道传递 xray 原生类型，绕过了 contracts/Extensions 的约定。这与 BuildOptions.CertManager 的 any 注入是同一类问题（第一轮已报 CertReader，但 Init 这条通道未被覆盖）：类型错误只能在具体容器的 type assertion 处运行时爆出。建议：Init 收敛为接受 ContainerConfig（factory.go 已有该抽象）或干脆从接口移除（工厂模式下 New(opts) 已完成初始化），文档不得引用具体实现类型。
- **证据**：interface.go:14-21 注释 `// - xray: *xray.ExecutorConfig` + `Init(config any) error`；hotreload/manager.go:268 `executor.Init(&xray.ExecutorConfig{...})`；hotupdate/update.go:286 同样调用
- **核实**：亲读代码确证：interface.go:14-21 注释明写 '- xray: *xray.ExecutorConfig' 且签名为 Init(config any) error，L1 接口文档直接引用具体容器包导出类型；PROJECT_GUIDE.md 第 68 行确有'上层只用 core/contracts 领域模型,不允许跨层使用 xray 原生配置类型'的架构约束。三处调用点属实：hotreload/manager.go:268、:607 与 hotupdate/update.go:286 均为 executor.Init(&xray.ExecutorConfig{...})。exec_runner.go:1570-1578 的实现靠运行时 type assertion（config.(*ExecutorConfig)）兜底，类型错误确实只能运行时爆出。factory.go:9-11 已有 ContainerConfig 抽象，且三处调用点在 Init 前刚以同一 ExecutorConfig 调过 NewExecutor，Init 在这些路径近乎冗余，建议可行。两点细微修正不影响结论：(1) 三处调用者持有的是具体类型 *xray.Executor 而非接口值，'跨层借接口通道'的表述略强，但 any 签名正是为满足 L1 接口而生，后门在接口层成立；(2) pkg/xrayapi 是 xray 专属工具层，其用 xray 类型未必违反约束本意，但接口文档点名实现类型这一核心问题独立成立。P3 恰当。

#### 39. [P3·confirmed] decodeSSUserInfo 用 u.User.String() 取 base64，'/' 被重转义为 %2F 导致 Std 变体解码必失败

- **位置**：`pkg/proxy/core/subscription/codec/shadowsocks.go:122` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：SIP002 兼容分支用 `raw := u.User.String()` 作为 base64 输入。url.Userinfo.String() 会按 RFC 3986 重新转义 userinfo 中的 '/'、':'、'?'，因此对方以 StdEncoding 生成且规范地把 '/' 百分号转义为 %2F 的 ss URI（Go 系工具用 url.User(b64).String() 生成即是此形态），Parse 先解出 '/'、String() 又转回 %2F，四种 base64 变体全部失败，报 "cannot extract cipher method"。代码注释明确声称容忍 StdEncoding 变体（line 124），但含 '/' 的 std base64（随机凭据下概率很高）实际不可用。应改用 u.User.Username()（已完成百分号解码，base64 字符集不含 ':' 不会被截断）。实测 `ss://YWVzLTI1Ni1nY206cGE%2Fc3N3b3Jk@1.2.3.4:8388#n` 解码失败，而 Username() 返回的正是可解码的原文。
- **证据**：shadowsocks.go:122 `raw := u.User.String()`；实测该 URI 报 `ss: cannot extract cipher method from userinfo`，u.User.Username()="YWVzLTI1Ni1nY206cGE/c3N3b3Jk" 本可正常 StdEncoding 解码。
- **核实**：实测复现：DecodeShadowsocks("ss://YWVzLTI1Ni1nY206cGE%2Fc3N3b3Jk@1.2.3.4:8388#n") 报 'ss: cannot extract cipher method from userinfo'，同时验证 u.User.String() 重转义回 "YWVzLTI1Ni1nY206cGE%2Fc3N3b3Jk"（含 %2F，四种 base64 变体全部失败）而 u.User.Username() 返回可 RawStdEncoding 解码的原文。缺陷定性不依赖外部记忆：shadowsocks.go:17 与 :124 的注释本身就是仓库内材料，声明 SIP002 canonical 是 base64url、其余变体（shadowrocket/outline quirks）被容忍，而含 '/' 的 std base64 只能以 %2F 形式合法出现在 userinfo（裸 '/' 会终结 authority 使 userinfo 根本解析不出），此形态下代码自身声明的容忍失效——属代码与自身文档意图的矛盾。触发面真实存在：sub_handler.go:178 ext_sub 允许外部订阅 URI 进入该解码链。canonical RawURL 形态不受影响（字符集无 '/'），限于兼容分支，P3 恰当。修复建议（改用 Username()）也核实无副作用：base64 字符集不含 ':' 不会被 Username 截断。

#### 40. [P3·confirmed] DecodeVMess 的 TrimPrefix 大小写敏感，与 node.go 大小写不敏感的 scheme 分派不一致

- **位置**：`pkg/proxy/core/subscription/codec/vmess.go:143` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：node.go:48 用 `strings.ToLower(uri[:idx])` 分派，因此 `VMESS://...` 会被路由到 DecodeVMess；但 DecodeVMess 用 `strings.TrimPrefix(uri, "vmess://")` 剥前缀，大小写不匹配时前缀原样保留，base64 解码必失败，返回误导性的 "vmess: base64 decode failed"（实际是 scheme 大小写问题）。其余协议走 url.Parse，Go 会把 Scheme 归一为小写所以不受影响——只有 vmess 这一个解码器与分派层的大小写约定不一致。RFC 3986 规定 scheme 大小写不敏感。实测 `Decode("VMESS://eyJ...")` 报 base64 decode failed，小写同一 URI 成功。
- **证据**：vmess.go:143 `body := strings.TrimPrefix(uri, "vmess://")`；node.go:48 `scheme := strings.ToLower(uri[:idx])`。实测大写 scheme 失败、小写成功。
- **核实**：实测复现：对同一 vmess URI，小写 scheme Decode 成功，改成 VMESS:// 后 codec.Decode 报 'vmess: base64 decode failed'。不一致性完全由仓库内代码构成，无需第三方佐证：node.go:48 用 strings.ToLower(uri[:idx]) 分派（因此大写 VMESS:// 会命中 decoders["vmess"] 路由到 DecodeVMess），而 vmess.go:143 用大小写敏感的 strings.TrimPrefix(uri, "vmess://")，前缀剥不掉导致 base64 必失败且错误信息误导。其余协议（vless/trojan/ss/hy2/tuic/anytls/snell）均走 url.Parse，Go 将 Scheme 归一为小写后与各 decoder 的小写比较一致，已抽查 hysteria2.go:83 确认。分派层接受、解码层拒绝同一输入即是仓库内自洽性缺陷，RFC 大小写论据只是佐证而非必要前提。P3 恰当。

#### 41. [P3·confirmed] ssNodeToSpec 不投影 plugin_password/plugin_version，clash buildPluginOpts 对应读取为死代码（shadow-tls 类插件断裂）

- **位置**：`pkg/proxy/core/subscription/uri_convert.go:361` · 维度：正确性 · 单元：SUB
- **问题**：clash.go buildPluginOpts（1118-1146）读取 plugin_mode/plugin_host/plugin_path/plugin_tls/plugin_password/plugin_version 六个键，但 ssNodeToSpec（uri_convert.go:354-390）只写前四个：PluginOpts 中的 `password`、`version`（shadow-tls 插件必需字段）以及其他任何键都被静默丢弃。带 `plugin=shadow-tls;password=x;version=3` 的 ss URI 转 clash 后 plugin 名保留、关键 opts 缺失，产出必然连不上的半截配置；比完全跳过更糟，因为表面看配置存在。要么在 ssNodeToSpec 补投影 PluginOpts["password"]/["version"]，要么把 PluginOpts 整体透传（如 plugin_opts map）。
- **证据**：clash.go:1135-1142 读 `plugin_password`/`plugin_version`；uri_convert.go:359-380 仅写 plugin/plugin_mode/plugin_host/plugin_path/plugin_tls，无任何路径写 plugin_password/plugin_version（grep 全仓库无写入点）。
- **核实**：核实成立且生产触发链完整。uri_convert.go:354-390 ssNodeToSpec 逐行核对：仅写 method/plugin/plugin_mode/plugin_host/plugin_path/plugin_tls，PluginOpts 中 password/version 无任何投影；converter/clash.go:1135-1142 buildPluginOpts 确实读 plugin_password/plugin_version（shadow-tls 必需）。全仓 grep 确认 plugin_password/plugin_version 的另一写入点只有 mihomo/subscription.go:606-610（容器直写 Extensions），但生产链路上该 Extensions 在 RPC 边界被丢弃——end_node_user.go:318-330 只回传 spec.URI，sub_handler.go:199 再经 ConvertURIs 重解码，故 clash 侧读取在生产订阅链上确为死代码。触发真实：mihomo fillSSSubscriptionSpecPP（subscription.go:576-588）把 shadow-tls 的 password/version 放进 node.PluginOpts 并 Encode 进 URI query（plugin=shadow-tls;password=..;version=..），DecodeShadowsocks 能解析回 PluginOpts（shadowsocks.go:102-106 + parsePluginSpec），随后在 ssNodeToSpec 处丢失，产出 plugin 名保留但缺 password/version 的半截 clash 配置。P3 可接受（shadow-tls 使用面窄于 vmess+tls，但同为必然不可用的配置）。

#### 42. [P3·confirmed] hy2/tuic 的 alpn 逗号拆分不过滤空串，尾随逗号产生空 ALPN 条目并进入 Extensions

- **位置**：`pkg/proxy/core/subscription/codec/hysteria2.go:106` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：DecodeHysteria2（hysteria2.go:105-107）与 DecodeTuic（tuic.go:119-121）用 `strings.Split(raw, ",")` 拆 alpn，不 trim 也不滤空：`alpn=h3,` 实测得到 ["h3", ""]，`alpn=h3, h2` 得到 ["h3", " h2"]。空串/带空格条目经 hysteria2NodeToSpec/tuicNodeToSpec 写入 ext["alpn"]，再 Encode 回 URI 时产出 "h3,"，往返不收敛；若未来 clash/surge 层开始消费 alpn（目前注释说依赖 mihomo 默认 h3 而不读取），空条目会直接进 yaml 生成非法 ALPN。拆分后应 TrimSpace 并跳过空串。
- **证据**：实测 `DecodeHysteria2("hysteria2://p@h.com:443/?alpn=h3,#n")` 返回 ALPN=["h3" ""]；tuic.go:119-121 同一写法。
- **核实**：代码属实：hysteria2.go:105-107 与 tuic.go:119-121 均用 strings.Split(raw, ",") 拆 alpn，无 TrimSpace 也不滤空串（Go 语义下 "h3," 必得 ["h3",""]）；空条目经 uri_convert.go:406-407/430-431 写入 ext["alpn"]，Encode（hysteria2.go:58-59、tuic.go:62-63）用逗号 Join 会再产出 "h3,"。clash.go:599-600 注释确证当前 clash 层不消费 alpn，故现实影响仅限 Extensions/URI 往返携带垃圾条目，'未来消费方产生非法 ALPN' 是假设性外推。唯一小瑕疵：'往返不收敛'说法不准——一轮后即稳定为 "h3,"，只是不清洗。P3 恰当。

#### 43. [P3·confirmed] Surge 行格式转换器未对 NodeName 做转义，来自不可信 ext_sub 的 fragment 可注入任意 Surge 配置行

- **位置**：`pkg/proxy/core/subscription/converter/surge.go:249` · 维度：安全 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：SurgeConverter 用 strings.Join(parts, ", ") 逐行拼接，proxy 名 name = "🌿 X_" + spec.NodeName。NodeName 来自 codec 解码的 URI fragment（u.Fragment 已做百分号解码），对 ext_sub 这类外部来源完全不可信，可包含 ", " 或换行(%0A)。例如 ext_sub 返回 vmess://...#a%0A%5BProxy%5Devil%3Ddirect 会在 Surge 输出中插入额外的 [Proxy]/规则行，或用逗号伪造出多余的 Surge 参数字段。Clash 走 yaml.Marshal 天然转义故安全，但 surge（及潜在的 common/qv2ray 拼接）无防护。虽然 ext_sub 通常是订阅者自填（自伤面为主），但仍是一个未过滤的配置注入点，且 NodeName 也可能来自其他节点提供的 URI。
- **证据**：surge.go:249-254 nodeName 直接 Sprintf 拼 NodeName；convertVMess:69-74 等把 name 放入 parts 后 Join(", ")；uri_convert.go 各 nodeToSpec 将 codec fragment 原样填入 NodeName。
- **核实**：代码核实属实：surge.go:249-254 nodeName 直接 Sprintf 拼 spec.NodeName 无任何转义，各 convertXxx 把 name 放入 parts 后 strings.Join(parts, ", ")，Convert(28) 再以 "\n" 拼行——含换行或 ", " 的 NodeName 必然打断行结构、注入独立配置行/伪造字段。NodeName 来源确证：codec 各 Decode 取 u.Fragment（Go url.Parse 对 fragment 做百分号解码，%0A 变真实换行），经 nodeToSpec 原样入 spec.NodeName；ext_sub URI 经 sub_handler.go:182 → ConvertURIsWithOptions → urisToSpecs 抵达 SurgeConverter（client=surge 即触发），路径完整。注入原语（插入任意行）由仓库代码自证，无需依赖 Surge 解析细节；'[Proxy] 节头生效'属上游语义但不影响缺陷成立。ext_sub 主要自伤+跨来源 URI 面，P3 恰当。

#### 44. [P3·confirmed] MatchProxies 丢弃 regexp 编译错误：用户 proxy_group 中写错正则会静默生成空 group，无日志无报错

- **位置**：`pkg/proxy/core/subscription/converter/clash.go:133` · 维度：错误处理 · 单元：SUB
- **问题**：MatchProxies 对非 all/内置策略/已知 group 的条目走 regexp.MatchString(p, name)，返回值中的 error 被 `_` 丢弃。用户在 proxy_group 参数里写了非法正则（如未配对的 '(' 或 '[' ）时，每次匹配都返回 err!=nil、matched=false，结果该自定义 group 的 proxies 为空——既不报错也不告警，用户拿到一个空 group 且完全不知道原因。相比 rule 的 ValidatePolicy 会 5xx，这里的失败是纯静默。此外正则对每个 nodeName 重新编译一次（未预编译），节点多时是无谓开销。建议先 regexp.Compile 一次、失败时 log.Warn 或回退为字面匹配。
- **证据**：clash.go:132-136 `if matched, _ := regexp.MatchString(p, name); matched`，error 被忽略且每 name 编译一次；对照 ValidatePolicy:142 对非法 policy 会返回错误。
- **核实**：clash.go:133 确实以 `if matched, _ := regexp.MatchString(p, name)` 丢弃编译错误，MatchProxies 全函数无日志；非法正则时每个 name 都返回 err+false，该 group proxies 为空且完全静默。调用链真实：用户 HTTP 参数 → BuildConvertOptions(uri_convert.go:56)/http.go:178 → ParseProxyGroupParam（无正则校验）→ clash.go:951/1047 MatchProxies。对照 ValidatePolicy(clash.go:142-150) 对非法 rule policy 会返回错误，此处失败确为纯静默。每 name 重新编译也属实（regexp.MatchString 每次调用都 Compile）。P3 恰当。

#### 45. [P3·confirmed] RemoteFetcher.Fetch 产出仅含 URI、Protocol 为空的 spec，经 Clash/Surge 转换会被整体丢弃（且当前无生产调用方）

- **位置**：`pkg/proxy/core/subscription/remote.go:35` · 维度：架构 · 单元：SUB
- **问题**：RemoteFetcher.Fetch 为每个远程 URI 生成 contracts.SubscriptionSpec{URI: uri}，不填 Protocol/Host/Port/Extensions。ClashConverter.convertSpec 与 SurgeConverter.convertSpec 都以 spec.Protocol 分派，Protocol 为空时落到 default 返回 nil/false——即这些 spec 在 clash/surge 输出中会被静默全部丢弃，只有 common/qv2ray（读 spec.URI）能用。若未来把 RemoteFetcher 接到 clash/surge 链路，会得到空节点列表而无任何提示。grep 显示除自身与测试外无调用方，属未接线的死代码，其 Protocol-less 契约是个埋点隐患；应在 Fetch 内先 codec.Decode 投影（复用 urisToSpecs）或在文档中明确只适配 common 格式。
- **证据**：remote.go:34-38 只填 URI；clash.go:244-265 convertSpec / surge.go:32-64 convertSpec 均按 Protocol 分派，空 Protocol 命中 default 返回 nil；全仓 grep 无生产调用方。
- **核实**：核心事实全部成立：remote.go:34-38 只填 URI 不填 Protocol/Host/Port/Extensions；ClashConverter.convertSpec(clash.go:244-266) 按 spec.Protocol 分派、空 Protocol 落 default 返回 nil，即接入 clash 链路会静默产出空节点列表；全仓 grep 确认 RemoteFetcher 除 remote_test.go 外无任何生产调用方，属未接线代码。一处不精确：SurgeConverter.convertSpec(surge.go:58-63) 的 default 分支有 hysteria2:// URI 前缀回退，Surge 侧并非全部丢弃（hysteria2 节点能存活），且 Fetch 的 Godoc(remote.go:23) 已声明协议字段由调用方判断的契约，略削弱严重度但不改变 Clash 链路静默清空的隐患。P3 恰当。

#### 46. [P3·uncertain] parseVMess 静默吞掉非法 alter_id（"abc"→0）且不做 0-65535 范围校验，负值可入库

- **位置**：`pkg/proxy/core/params/protocolparams/params_vmess.go:61` · 维度：错误处理 · 单元：CTR · ⚠️协议相关（需人工核对上游）
- **问题**：`alterID, _ := optionalInt(raw, KeyAlterID)` 丢弃 ok 返回值：caller 传 alter_id="abc" 或 12.5 时静默落为 0，与本包对其他字段'loud failure'的风格（如 reality_max_time_diff 非整数即报错、tuic congestion_controller 白名单拒绝）不一致；同时无范围检查，alter_id=-1 或 999999 原样写入 VMessParams.AlterID（int）并持久化到 InboundStore，最终进入 mihomo VmessUser/xray clients 配置。vmess AlterID 协议语义为 uint16（0-65535），应拒绝负值/超界并对非整数报 ErrMissingRequired。
- **证据**：params_vmess.go:61 忽略 optionalInt 的 ok；transport.go:147-170 optionalInt 对不可解析值返回 (0,false)；对比 security.go:141-144 对非整数的显式报错
- **核实**：代码侧事实全部确证：params_vmess.go:61 丢弃 optionalInt 的 ok（"abc"/12.5 静默落 0，transport.go:159-168 确认），无任何范围检查，负值经 profilegen.go:314 写入 alterId 并持久化，与 security.go:141-144 对 max_time_diff 的 loud failure 风格不一致。但按核实规则，protocolRelated 条目的上游断言（'AlterID 协议语义为 uint16 0-65535'、mihomo VmessUser/xray 对越界值的行为）在仓库材料中检索无果——wiki/docs 仅有 'AlterID=0 默认 AEAD-only'（docs/mihomo-container-design.md:242），mihomo 未 vendor，uint16 范围语义与越界后果无法从仓库内确证，故最高 uncertain。代码侧一致性问题本身成立。

#### 47. [P3·uncertain] vless/trojan Decode 只识别 insecure=1，不识别生态常用的 allowInsecure=1，skip-cert-verify 丢失

- **位置**：`pkg/proxy/core/subscription/codec/vless.go:158` · 维度：正确性 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：DecodeVLess（vless.go:158）与 DecodeTrojan（trojan.go:145）只认 `q.Get("insecure") == "1"`。实际生态中 v2rayN/NekoBox 等生成的 vless/trojan 分享链接普遍使用 `allowInsecure=1`（或 =true），这类 URI 解码后 SkipCertVerify=false，经 nodeToSpec → clash/surge 输出后自签证书节点无法建连。与之对照，tuic.go:136 专门兼容了 dae 系的 `allow_insecure=1`，clash 转换层也为 hy2/tuic/anytls 留了 URI 子串 fallback（clash.go:557/617/671），唯独 vless/trojan 两个最常见协议既不认 allowInsecure 也无 fallback。建议 Decode 同时接受 insecure/allowInsecure 的 "1"/"true"。
- **证据**：vless.go:158 `if q.Get("insecure") == "1"`；trojan.go:145 同；tuic.go:136 已示范 allow_insecure 兼容写法。
- **核实**：代码事实全部核实：vless.go:158 与 trojan.go:145 均仅 q.Get("insecure") == "1"；tuic.go:136 确实兼容 allow_insecure=1（注释明言 NekoBox/Hiddify 互通）；clash 转换层仅为 hy2/tuic/anytls 留了 URI 子串 fallback（clash.go:557/617/671），vless/trojan 无。但危害成立的前提是第三方生态（v2rayN/NekoBox 等）为 vless/trojan 分享链接普遍生成 allowInsecure=1——检索仓库材料：wiki 只记载了 tuic 的 allow_insecure（dae 草案语义）与 hy2/anytls 的 insecure，没有任何 wiki/docs/config/测试记载 vless/trojan URI 的 allowInsecure 生态惯例；implementation-plan.md:287 的 'URI 加 allowInsecure' 只是作者的候选方案备忘，不构成对上游行为的核实。仓库自产 URI（xray/subscription.go:549-551）恰恰发的是 insecure=1，自有链路不受影响，触发面仅限 ext_sub 外部 URI。按规则 3 第三方断言无仓库佐证，最高 uncertain。

#### 48. [P3·uncertain] RemoteFetcher.Fetch 错误串内嵌完整 URL（不走 SanitizeURL），且部分失败时错误被完全静默

- **位置**：`pkg/proxy/core/subscription/remote.go:31` · 维度：错误处理 · 单元：SUB
- **问题**：两个问题：(1) `errs = append(errs, fmt.Sprintf("[%s]: %v", url, err))` 把完整订阅 URL（订阅源 URL 的 path/query 常含 token 凭据）拼进 error，最终 error 会被上层日志打出——与同包 http.go:146 特意用 SanitizeURL(subURL) 脱敏的策略自相矛盾；(2) 只有全部 URL 失败才返回错误（line 41），部分失败时 errs 被直接丢弃且无任何日志（FetchExternalSub 内部也不打日志），远程源静默变少与 manager.go 静默吞 container 错误（pass1 已报）是同一类可观测性缺陷但在不同文件、pass1 未覆盖。建议部分失败时逐条 log.Warn 并用 SanitizeURL 脱敏。
- **证据**：remote.go:29-43：errs 仅在 `len(errs) > 0 && len(specs) == 0` 时进入返回值，其余路径无日志无返回；对比 http.go:146 `log.Warn(..., "host", SanitizeURL(subURL), ...)`。
- **核实**：代码事实全部属实：remote.go:31 把完整 URL 拼进 error，line 41 仅在全部失败时返回错误，部分失败的 errs 无日志无返回，FetchExternalSub 内部也确无日志。但全仓库 grep 显示 NewRemoteFetcher/RemoteFetcher 除 remote_test.go 外没有任何生产调用方——finding 声称的触发路径（'最终 error 会被上层日志打出'、'远程源静默变少'）在当前代码中不存在，这是未接线的 API 的潜在卫生问题而非可触发缺陷。若该 fetcher 未来接入生产则两点均成立，建议作为预防性修复保留，P3 不变。

#### 49. [P3·uncertain] ParseRuleProviderParam 用未清洗的 name 拼 Path，可在用户端 Clash 配置里产生路径穿越

- **位置**：`pkg/proxy/core/subscription/converter.go:148` · 维度：安全 · 单元：SUB · ⚠️协议相关（需人工核对上游）
- **问题**：ParseRuleProviderParam 把用户传入的 rule_provider name（parts[0]，可含 '../'、绝对路径、斜杠）直接格式化进 Path: `./rule-provider/%s.yaml`，该 Path 原样写入下发给客户端的 rule-providers 配置（clash.go patchTemplateWithOptions:1088 / buildLocalConfig:991）。mihomo 客户端在拉取 http 型 rule-provider 时会把内容写到此 Path，name=`../../foo` 或 `/etc/xxx` 会导致客户端把文件写到预期目录之外。虽是客户端侧写入，但服务端生成了这个危险路径且完全无校验；同时对 name 没有任何字符白名单。建议对 name 做 basename/字符白名单清洗。
- **证据**：converter.go:136-149 SplitN 后 Path: fmt.Sprintf("./rule-provider/%s.yaml", parts[0])，name 未做任何校验；clash.go:1088、991 将 Path 原样写入下发配置。
- **核实**：服务端事实确认：converter.go:137-148 把 SplitN 出的 parts[0]（可含 ../ 、/、绝对路径）无任何清洗直接拼进 Path，clash.go:991/1088 原样写入下发的 rule-providers 配置。但危害成立与否取决于客户端行为：仓库内 wiki（wiki/knowledge/mihomo-container/details.md:85）明确记载 mihomo 对 config 引用的文件路径做 SAFE_PATHS 检查（路径必须位于 home directory 之下，否则报 path is not subpath of home directory），提示现代 mihomo 会拒绝穿越路径而非写出目录外；老版 clash 或其他客户端是否有同等防护仓库内无材料可证。按 protocolRelated 规则，上游写文件行为无法由仓库材料确证（现有材料反而部分削弱该断言），最高只能 uncertain。加 name 白名单清洗作为纵深防御仍属合理建议，P3 不改。

## 附录：verifier 判定 refuted（不计入）

| 单元 | 位置 | 标题 | refuted 理由 |
|------|------|------|-------------|
| STO | `pkg/store/manager.go:75` | InitLoginPasswords 不过滤墓碑用户与空/legacy auth_token，会为已删除用户和空 token 生成可用的登录口令 | 查询确实无过滤（manager.go:75），但核心安全断言不成立：(1) login_password 的全部三个消费点——login_handler.go:24、change_password_handler.go:42、sub_handler.go:102-108——都先经 GetUser/FindUserByToken 取用户，而 usermanager.go:605-612/627 明确过滤 IsDeleting() 与 IsExpired()，墓碑用户即使被回填 login_password 也无法通过任何登录/订阅路径认证，'给标记删除的账号建立可登录口令' 的实际危害不存在；(2) auth_token='' 场景无仓库内产生路径：AddUser 对空 token 自动生成唯一 token（usermanager.go:500 setAuthTokenLocked），schema NOT NULL，只能靠旁路写库构造，纯假设；(3) 执行时序先于 legacy token 重置是刻意设计——cmd/server.go:123-125 注释明确要求 InitLoginPasswords 先跑以便'existing users can log in with their current auth token'，legacy 用户的登录口令等于其升级前手里的旧 token 正是让其能立即登录的预期行为，非缺陷。加 deletion_state 过滤仅属防御性卫生改进，不构成实际缺陷。 |
