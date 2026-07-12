# L0 基础层 review — log / common / errors / tools / appconfig

> Finder → 对抗性 verifier 两级流程产出。下表为**保留**（confirmed + uncertain）的 findings，已剔除 verifier 判定 refuted 的 0 条。uncertain 多为依赖第三方协议/上游行为、无法在仓库内确证的条目，处理前需人工核对。

## 统计

| 维度 | 数量 |
|------|------|
| 保留条目 | 30 |
| — confirmed | 30 |
| — uncertain | 0 |
| — 其它(unverified) | 0 |
| refuted(已剔除) | 0 |

| 优先级 | P0 | P1 | P2 | P3 |
|--------|----|----|----|----|
| 保留条目 | 0 | 5 | 11 | 14 |

## 速查表

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P1 | ✓ | 安全 | LOG | `pkg/common/rpc/encrypt_codec.go:46` | RPC 密钥派生无 KDF：短 token 低熵、空 token 产生公开常量密钥 |
| 2 | P1 | ✓⚠️协议 | 安全 | LOG | `pkg/common/rpc/crypto.go:37` | 加密层无重放防护、无 AAD 上下文绑定，密文可整帧重放或跨方法剪接 |
| 3 | P1 | ✓ | 正确性 | ERR | `pkg/proxy/tools/binary_swapper.go:25` | SwapAtomic 在目标二进制不存在时返回并不存在的 backupPath，后续 Rollback 会把新二进制挪走导致二进制彻底丢失 |
| 4 | P1 | ✓ | 并发 | ERR | `pkg/proxy/tools/process/runner.go:150` | IsRunning 对自行退出的进程恒报 true，且 Start 后无人 Wait 导致崩溃进程成为僵尸（旧 review 已知，确认仍在） (P-01) |
| 5 | P1 | ✓ | 正确性 | CFG | `pkg/proxy/appconfig/loader.go:169` | legacy 迁移路径提前 return，绕过 jwt_secret 自动生成与 NodeSources 默认填充，迁移后 /login 直接不可用 |
| 6 | P2 | ✓ | 并发 | LOG | `pkg/log/logger.go:45` | 全局 logger 变量无同步，SetDefault 与并发日志调用构成数据竞争 |
| 7 | P2 | ✓ | 资源 | LOG | `pkg/log/slog.go:223` | PrefixWriter 行缓冲无上限，子进程持续无换行输出会内存无限增长 |
| 8 | P2 | ✓ | 测试缺口 | LOG | `pkg/common/rpc/crypto_test.go:173` | 测试缺口：codec Marshal/Unmarshal 无 roundtrip 测试，空 token 与 caller 归因深度也未覆盖 |
| 9 | P2 | ✓ | 错误处理 | ERR | `pkg/proxy/tools/binary_swapper.go:30` | SwapAtomic 并非原子：两次 rename 之间存在无二进制窗口，且失败恢复的 rename 错误被忽略 |
| 10 | P2 | ✓ | 资源 | ERR | `pkg/proxy/tools/downloader.go:19` | Downloader 与 GitHubReleaseClient 的默认 http.Client 均无超时，配合调用方传 context.Background() 会造成下载永久挂起 |
| 11 | P2 | ✓⚠️协议 | 错误处理 | CFG | `pkg/proxy/appconfig/loader.go:177` | YAML/JSON 解码均为非严格模式，拼写错误或大小写不符的配置键被静默丢弃 |
| 12 | P2 | ✓ | 安全 | CFG | `pkg/proxy/appconfig/loader.go:226` | SaveToFile 以 0644 权限明文落盘含 cluster token、http token、jwt_secret、DNS API 凭据的完整配置，且非原子写 |
| 13 | P2 | ✓ | 错误处理 | CFG | `pkg/proxy/appconfig/config.go:54` | ApplyToLogger 打开日志文件失败时静默降级到默认输出，无任何告警 |
| 14 | P2 | ✓ | 正确性 | CFG | `pkg/proxy/appconfig/loader.go:60` | isLegacyConfig 启发式可把手工半迁移的新格式配置整份静默丢弃 |
| 15 | P2 | ✓ | 正确性 | CFG | `pkg/proxy/appconfig/loader.go:247` | Validate 校验缺口仍在：端口范围/上限、node_type 枚举、log.level 枚举、cluster.token 均不校验（旧问题确认仍存在） (APP-01) |
| 16 | P2 | ✓ | 测试缺口 | CFG | `pkg/proxy/appconfig/loader.go:68` | 测试缺口：legacy 检测/迁移、SaveToFile、global.go、ApplyToLogger 全部无测试覆盖 |
| 17 | P3 | ✓ | 错误处理 | LOG | `pkg/common/rpc/encrypt_codec.go:20` | Marshal 对 v 做无检查类型断言，非 proto.Message 直接 panic |
| 18 | P3 | ✓ | 正确性 | LOG | `pkg/log/slog.go:230` | PrefixWriter.Write 出错时返回 (0, err) 违反 io.Writer 契约且可能残留孤儿前缀 |
| 19 | P3 | ✓ | 正确性 | LOG | `pkg/log/logger.go:69` | logSkip 对非 slogLogger 实现的 fallback 会使 caller 归因指向 logger.go 自身 |
| 20 | P3 | ✓ | 安全 | LOG | `pkg/log/slog.go:176` | quoteIfNeeded 不处理 \r 等控制字符，攻击者可控字段可伪造日志行 |
| 21 | P3 | ✓ | 架构 | LOG | `pkg/log/slog.go:124` | v2rayTextHandler.Handle 内残留死代码（frames/f 变量与两处空丢弃） |
| 22 | P3 | ✓ | 正确性 | LOG | `pkg/log/logger.go:7` | 包文档声称默认输出 stderr，实际 defaultOptions 是 os.Stdout |
| 23 | P3 | ✓⚠️协议 | 正确性 | ERR | `pkg/proxy/errors/errors.go:145` | ProxyError.As 经 errors.As 永远不会被调用，"按 Code 过滤"特性完全失效 |
| 24 | P3 | ✓ | 架构 | ERR | `pkg/proxy/errors/errors.go:220` | errors 包 legacy 桥接层（ToError/NewErr/NewErrAt/ErrorCode.Is）全仓库零调用方，且自带 nil panic 与字符串误匹配陷阱，应整体删除 (E-02) |
| 25 | P3 | ✓⚠️协议 | 正确性 | ERR | `pkg/proxy/tools/github_release_client.go:85` | NormalizeTag 全仓库无调用方，且其剥 v 前缀的语义与 FetchRelease 的 GitHub API 路径要求相矛盾 |
| 26 | P3 | ✓ | 架构 | ERR | `pkg/proxy/tools/binary_swapper.go:51` | BinarySwapperImpl 是全仓库无引用的死代码，与 BinarySwapper 完全重复 |
| 27 | P3 | ✓ | 资源 | ERR | `pkg/proxy/tools/downloader.go:39` | DownloadToFile 直接写最终路径且失败时不清理，io.Copy 中断会在目标位置留下截断文件；out.Close 错误被忽略 |
| 28 | P3 | ✓ | 测试缺口 | ERR | `pkg/proxy/tools/binary_swapper.go:1` | binary_swapper.go 与 downloader.go 完全没有单元测试，SwapAtomic 首装/失败恢复/Rollback 路径零覆盖 |
| 29 | P3 | ✓ | 正确性 | CFG | `pkg/proxy/appconfig/config.go:134` | PingNodeSource.UpdateInterval 注释宣称默认 300s，实际无任何默认填充，remote 源不配置则永不刷新 |
| 30 | P3 | ✓ | 架构 | CFG | `pkg/proxy/appconfig/global.go:13` | global.go 全局单例 API（Init/InitWithValidate/Get/MustGet/Set）在生产代码中零调用，属死代码且开放了无保护的共享可变指针 |

## 详细条目

### 1. [P1·confirmed] RPC 密钥派生无 KDF：短 token 低熵、空 token 产生公开常量密钥

- **位置**：`pkg/common/rpc/encrypt_codec.go:46` · 维度：安全 · 单元：LOG
- **问题**：GetRpcKeyByToken 直接把 cluster token 变成 AES-256 key：长度 >=32 时截断前 32 字节（第 33 字节起的差异被忽略），不足 32 时用 PKCS7Padding 填充。填充字节是确定性的（byte(paddingNum) 重复），所以 key 的熵完全等于 token 本身的熵——8 字符 token 就是约 8 字符口令强度的 key，可离线暴力枚举（GCM tag 可作为口令验证 oracle）。极端情况：token 为空字符串时 paddingNum=32，key 恒为 32 个 0x20，是任何读过源码的人都知道的常量；而配置层（pkg/proxy/appconfig/loader.go 只搬运 Cluster.Token，未见非空校验）不阻止空 token。由于集群 gRPC 走 insecure 明文传输（pkg/cluster/node.go:41 grpc.WithTransportCredentials(insecure.NewCredentials())），该 codec 是 center↔end 之间唯一的机密性/完整性层，弱/空 token 下 AddUsers/TransferCert（含证书私钥）等 RPC 等价于明文。建议改为 SHA-256(token) 派生（需两端同步升级）并在启动时强制 token 最小长度。
- **证据**：encrypt_codec.go:46-53 `if len(token) >= rpcServerKeyLen { return []byte(token)[:32] } else { return PKCS7Padding([]byte(token), rpcServerKeyLen) }`；crypto.go:13-17 PKCS7Padding 确定性填充；pkg/cluster/node.go:41 insecure transport；代码自注 encrypt_codec.go:50 "如果密码为空, 则同样不具有安全性"
- **核实**：代码逐行核实：encrypt_codec.go:46-53 GetRpcKeyByToken 对 token 长度 >=32 直接截前 32 字节（第 33 字节起被忽略），否则用 crypto.go:13-17 的 PKCS7Padding 确定性填充（bytes.Repeat(byte(paddingNum)))。空 token → paddingNum=32 → key 恒为 32 个 0x20，常量密钥属实。key 熵完全等于 token 熵，短 token 可离线暴力。cluster/node.go:41 确认 insecure.NewCredentials() 明文传输，该 codec 经 end_node_server.go:227 全局 RegisterCodec 是 center↔end 唯一机密层；client 各 RPC（AddUsers/TransferCert 等，end_node_rpc.go:151/360）都 ForceCodec 用同一 key。威胁链成立。注：空 token 一节与旧 review APP-01（loader.go Validate 未校验 cluster.token 为空）部分重叠，但本条的 KDF 弱化/短 token 低熵是独立的密码学缺陷，finder 未标 legacyId 可接受。P1 合理。

### 2. [P1·confirmed] 加密层无重放防护、无 AAD 上下文绑定，密文可整帧重放或跨方法剪接

- **位置**：`pkg/common/rpc/crypto.go:37` · 维度：安全 · 单元：LOG · ⚠️协议相关（需人工核对上游）
- **问题**：EncryptWithAES 调 gcm.Seal(nonce, nonce, plaintext, nil)，additionalData 恒为 nil；消息体内也没有时间戳/序号，接收端不做任何新鲜性校验。集群 RPC 连接是 insecure 明文 HTTP/2（pkg/cluster/node.go:41），gRPC 的 :path（方法名）等头部均为明文且不受该 codec 保护。后果：(1) 能观察网络的攻击者可把捕获到的加密请求帧原样重放——例如重放一次合法的 DeleteUsers/ResetUserTraffic/UpsertClusterUsers 请求，GCM 校验通过、内嵌 NodeAuthInfo.Token 也是旧的合法值，服务端会再次执行；(2) 由于密文未绑定 RPC 方法名，可将一个方法的密文剪接到 proto 结构兼容的另一方法上（active MITM 改 :path 即可）。建议：AAD 至少绑定 method 全名；消息内加时间戳+随机数并在 server 侧做窗口去重，或直接改用 TLS 传输让消息层加密只作纵深防御。（判断依据 gRPC over HTTP/2 明文头部与 codec 按 content-subtype 路由的上游行为。）
- **证据**：crypto.go:37 `return gcm.Seal(nonce, nonce, plaintext, nil), nil`（AAD=nil）；crypto.go:62 `gcm.Open(nil, nonce, ciphertext, nil)`；pkg/cluster/node.go:41 `grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))`；pkg/rpc/server/end_node_server.go:227 全局 RegisterCodec 使所有方法共用同一 key 无方法绑定
- **核实**：crypto.go:37 gcm.Seal(nonce, nonce, plaintext, nil) 与 :62 gcm.Open(nil, nonce, ciphertext, nil) 确认 AAD 恒为 nil；消息层无时间戳/序号绑定到密文。cluster/node.go:41 确认 insecure 明文传输。核实各方法服务端新鲜性校验：仅 HeartBeat（end_node_cluster.go:106-120、center_node_server.go:34）对 TimestampUs 做 30s drift 校验，且 ts==0 时跳过（向后兼容）；AddUsers/DeleteUsers/ResetUserTraffic/UpsertClusterUsers 等在 authRemoteNode（end_node_server.go:120-143）仅校验内嵌 token，无任何去重/时戳，捕获帧原样重放会被服务端再次执行——重放风险在仓库内证据即可确证成立。protocolRelated=true：'跨方法剪接需改 :path' 与 'HTTP/2 头部明文' 依赖 gRPC 上游行为，但主危害（整帧重放）不依赖第三方记忆，由 insecure 传输+无新鲜性校验+全局单 key 直接推出。综合确认，P1 合理。

### 3. [P1·confirmed] SwapAtomic 在目标二进制不存在时返回并不存在的 backupPath，后续 Rollback 会把新二进制挪走导致二进制彻底丢失

- **位置**：`pkg/proxy/tools/binary_swapper.go:25` · 维度：正确性 · 单元：ERR
- **问题**：SwapAtomic 对 os.Rename(binaryPath, backupPath) 的失败做了 IsNotExist 豁免（首次安装场景 binaryPath 不存在时继续执行），但仍然把 backupPath = binaryPath+".bak" 返回给调用方——此时磁盘上根本没有 .bak 文件。调用方（xray/mihomo updater）在后续 restart 失败时拿这个虚假 backupPath 调 Rollback：Rollback 先无条件把当前（刚换上的新）二进制 rename 成 binaryPath+".new"（且该 rename 的错误被完全忽略），再 os.Rename(backupPath, binaryPath) 因 .bak 不存在而失败返回错误——但此刻新二进制已被挪到 .new，binaryPath 处什么都没有。结果是"回滚"把系统从"有一个可能有问题的新二进制"变成"完全没有二进制"，容器进程再也起不来，且 xray updater 在 updater.go:186/192 还忽略了 Rollback 的返回错误，故障完全静默。修复方向：backupPath 只在备份实际创建成功时返回非空；Rollback 应先确认 backup 存在，第一步 rename 失败或第二步失败时恢复原状。
- **证据**：binary_swapper.go:25-27 `if err := os.Rename(binaryPath, backupPath); err != nil && !os.IsNotExist(err) { return "", ... }` 后无条件 `return backupPath, nil`（:36）；Rollback binary_swapper.go:45-47 `currentPath := binaryPath + ".new"; os.Rename(binaryPath, currentPath); return os.Rename(backupPath, binaryPath)` 第一次 rename 错误被丢弃且失败后不恢复；调用方 pkg/proxy/containers/xray/updater.go:186,192 `u.swapper.Rollback(...)` 忽略返回值
- **核实**：读 binary_swapper.go 核实：:22 先删旧 .bak，:25-27 对 os.Rename(binaryPath, backupPath) 做了 IsNotExist 豁免后，:36 无条件返回 backupPath——首次安装（binaryPath 不存在）时磁盘上确实没有 .bak 文件却返回了非空路径。Rollback（:45-47）第一步 os.Rename(binaryPath, binaryPath+".new") 错误被丢弃，第二步 os.Rename(backupPath, binaryPath) 因 .bak 不存在必然失败，此时刚换上的新二进制已被挪到 .new，binaryPath 为空。调用链核实：pkg/proxy/containers/xray/updater.go:174 SwapAtomic → :186/:192 在 Stop/Start 失败时调 Rollback 且返回值被忽略（对比 mihomo updater.go:341 有捕获 rbErr），故障静默。触发路径真实存在（binaryPath 缺失 + restart 失败），无上层防护。注：调用方忽略 Rollback 错误这一半与旧 review X-12（updater.go 187-193）重复，但 SwapAtomic 返回虚假 backupPath + Rollback 自毁语义是新发现。

### 4. [P1·confirmed] IsRunning 对自行退出的进程恒报 true，且 Start 后无人 Wait 导致崩溃进程成为僵尸（旧 review 已知，确认仍在）

- **位置**：`pkg/proxy/tools/process/runner.go:150` · 维度：并发 · 单元：ERR · 旧 review: P-01
- **问题**：IsRunning 仅检查 p.cmd != nil && p.cmd.Process != nil，Start 成功后除 Stop 外没有任何路径调用 Wait 或检查 ProcessState，所以 xray/mihomo/snell/hysteria 进程崩溃或自行退出后：(1) IsRunning 永远返回 true，上层健康检查/updater 的 processCtrl.IsRunning() 判断全部失真；(2) 退出的子进程在 Stop 被调用前一直是僵尸进程（未被 reap）。这是 2026-04-09 review 的 P-01，代码未变，本次确认仍然存在，并补充僵尸进程这一资源层面后果。修复方向：Start 后起一条 Wait 监视 goroutine 记录退出状态，IsRunning 基于 ProcessState 判断。
- **证据**：runner.go:147-151 `func (p *Runner) IsRunning() bool { ... return p.cmd != nil && p.cmd.Process != nil }`；全文件唯一的 cmd.Wait() 调用在 Stop 内（runner.go:114 与 :122），Start（:57-99）成功返回后没有任何 goroutine reap 子进程
- **核实**：读 runner.go 全文核实：IsRunning（:147-151）仅判断 p.cmd != nil && p.cmd.Process != nil；全文件 cmd.Wait() 只出现在 Stop 内部（:114、:122），Start（:57-99）成功后没有任何 goroutine reap 子进程，也没有任何路径检查 ProcessState。进程自行退出后 IsRunning 恒 true、子进程成僵尸的推理成立；且 Start（:61-63）因 cmd 未清空会拒绝重启（"process already running"），加重影响。与旧 review P-01 重复（docs/review-2026-04-09.md:31，旧行号 132-136 因文件演进变为 147-151），finder 已在标题注明；关联条目 X-03/H-05 也依赖此缺陷。僵尸进程后果为本次新补充，成立。

### 5. [P1·confirmed] legacy 迁移路径提前 return，绕过 jwt_secret 自动生成与 NodeSources 默认填充，迁移后 /login 直接不可用

- **位置**：`pkg/proxy/appconfig/loader.go:169` · 维度：正确性 · 单元：CFG
- **问题**：LoadFromFile 检测到 legacy 格式后在 loader.go:167-169 直接 `return cfg, nil`，跳过了后面两段兜底逻辑：(1) NodeSources 为空时填默认 file 源（loader.go:189-193）；(2) 非 center 节点 JWTSecret 为空时自动生成随机 secret（loader.go:199-206）。migrateLegacyConfig 调用的 defaultAppConfig 也不设这两项。结果是：老配置（必然没有 jwt_secret）迁移启动后 EndNode.JWTSecret=="" 被传入 http server（cmd/server.go:243），POST /login 在 pkg/http/auth/jwt.go:28-30 因 `jwt: secret must not be empty` 恒定失败——这与 loader.go:198 注释 "This allows old configs to upgrade without modification" 的设计意图直接矛盾；同时 Ping.NodeSources 为空，ping 采集无节点源。附带：old.Proxy.HysteriaConfigFile 和 old.Proxy.Port 在迁移中被静默丢弃，仅有代码注释、无 stderr 提示。修复方式建议把默认填充/jwt 生成下沉为公共 finalize 步骤，legacy 分支迁移后也走同一段。
- **证据**：loader.go:167-169 `cfg := migrateLegacyConfig(&old); ...; return cfg, nil`（早退）；loader.go:199 jwt 生成只在非 legacy 路径执行；pkg/http/auth/jwt.go:28-30 `if cfg.Secret == "" { return "", 0, fmt.Errorf("jwt: secret must not be empty") }`；cmd/server.go:243 `JWTSecret: cfg.EndNode.JWTSecret`
- **核实**：亲读 loader.go:159-171：legacy 命中后 `cfg := migrateLegacyConfig(&old)` 随即 `return cfg, nil`（167-169），确实跳过 189-193 的 NodeSources 默认填充与 199-206 的 jwt_secret 自动生成。migrateLegacyConfig（68-125）与 defaultAppConfig（129-144）均不设置 JWTSecret 和 Ping.NodeSources。Validate（247-284）不检查 jwt_secret，所以直接用 legacy 配置启动（loader 文档自称 'migrates it transparently'）不会被拦截：cmd/server.go 把空 cfg.EndNode.JWTSecret 传入 httpServer.Init，pkg/http/login_handler.go:36 调用 auth.GenerateJWT，pkg/http/auth/jwt.go:28-30 对空 Secret 恒定返回 'jwt: secret must not be empty'，/login 返回 500，与 loader.go:198 注释的升级不修改配置意图直接矛盾。附带项也属实：migrateLegacyConfig 从不读取 old.Proxy.Port 与 old.Proxy.HysteriaConfigFile（仅 87 行注释），静默丢弃。触发路径真实存在（不加 --migrate 直接 server 启动即触发；--migrate 保存后二次加载走新格式路径才会兜底，但那不是唯一路径）。P1 恰当。注意与旧 review APP-02（secret 每次重启重新生成、不持久化）同域但不同缺陷，非重复。

### 6. [P2·confirmed] 全局 logger 变量无同步，SetDefault 与并发日志调用构成数据竞争

- **位置**：`pkg/log/logger.go:45` · 维度：并发 · 单元：LOG
- **问题**：包级 `var global Logger`（logger.go:41）是普通接口变量，SetDefault 直接赋值、Debug/Info/... 与 With 直接读取，均无 atomic/锁。接口值在 Go 中是两个机器字，无同步的并发读写属于数据竞争，撕裂读可导致调用到错误的 itab/data 组合而 panic。当前唯一写点是 cmd/server.go:90 启动早期，靠"启动时单次设置"的约定成立，但该约定没有任何机制保证：init 阶段或 SetDefault 之前启动的任何 goroutine 打日志即触发 race（go test -race 可检出）。作为全仓约 62 个文件依赖的最底层模块，建议用 atomic.Pointer[Logger]（参考标准库 slog.SetDefault 内部用 atomic.Value）消除该窗口。
- **证据**：logger.go:41 `var global Logger = New()`；logger.go:44-46 `func SetDefault(l Logger) { global = l }`；logger.go:56-59 包级函数直接读 global；写点 cmd/server.go:90
- **核实**：logger.go:41 `var global Logger = New()` 为普通接口变量；:44-46 SetDefault 直接赋值；:56-64 包级 Debug/Info/... 与 :94 With 直接读 global，均无 atomic/锁。接口值双字，无同步并发读写按 Go 内存模型属数据竞争，go test -race 可检出。唯一写点确认在 cmd/server.go:90 启动早期，靠单次设置约定成立，但 init 阶段或 SetDefault 前起的 goroutine 打日志即触发窗口。作为底层模块，判定成立。触发窗口窄、实际撕裂罕见，P2 可接受，不强制下调。

### 7. [P2·confirmed] PrefixWriter 行缓冲无上限，子进程持续无换行输出会内存无限增长

- **位置**：`pkg/log/slog.go:223` · 维度：资源 · 单元：LOG
- **问题**：PrefixWriter.Write 把输入 append 进 pw.buf，只有遇到 '\n' 才消费；buf 没有任何长度上限。该 writer 被接到 xray/hysteria/mihomo/snell 四类外部子进程的 stdout/stderr（如 pkg/proxy/containers/xray/exec_runner.go:144-145），子进程行为不受本进程控制：若其输出用 '\r' 刷进度、输出超长单行或异常吐二进制流，buf 会随子进程存活时间无限增长直至 OOM，且四个容器长期运行放大风险。建议设上限（如 64KB）：超限时把当前 buf 作为一行强制刷出（加前缀）后清空，既保内存又不丢日志。
- **证据**：slog.go:223 `pw.buf = append(pw.buf, p...)` 后仅在 slog.go:225 `bytes.IndexByte(pw.buf, '\n')` 命中时消费；无任何 len(pw.buf) 上限检查；挂载点 pkg/proxy/containers/xray/exec_runner.go:144、hysteria/process.go:85、mihomo/process.go:88、snell/process.go:33
- **核实**：slog.go:219-239 核实：Write 无条件 `pw.buf = append(pw.buf, p...)`（:223），仅在 bytes.IndexByte(pw.buf,'\n') 命中时消费（:225-236），buf 无任何长度上限。挂载点确认接到外部子进程 stdout/stderr：xray exec_runner.go:144-145、hysteria process.go:85-86、mihomo process.go:88-89、snell process.go:33-34。子进程用 \r 刷进度/超长单行/二进制流且无换行时 buf 随存活时间无限增长，OOM 风险属实。P2 合理。

### 8. [P2·confirmed] 测试缺口：codec Marshal/Unmarshal 无 roundtrip 测试，空 token 与 caller 归因深度也未覆盖

- **位置**：`pkg/common/rpc/crypto_test.go:173` · 维度：测试缺口 · 单元：LOG
- **问题**：该单元三处关键行为没有测试保护：(1) EncryptMessageCodec 只测了 Name()（crypto_test.go:173-178），Marshal→Unmarshal 用真实 proto.Message 的 roundtrip、Marshal 传非 proto 类型、Unmarshal 传错 key 的行为均无覆盖——而这是 center/end 全部 RPC 的编解码路径（架构约束 9 要求可测模块有测试）；(2) GetRpcKeyByToken 只测了长/短 token，未测空 token（当前会返回 32×0x20 常量 key，一旦修复派生算法该用例是兼容性回归的哨兵）；(3) pkg/log 的 logSkip/slogLogger.log 硬编码 runtime.Callers(3)（logger.go:87、117），source 归因正确性完全无测试——任何人给包级函数或 slogLogger 方法加一层包装，source 会静默错位到 logger.go 自身，logger_test.go 现有用例只断言不 panic 和字段存在，不校验 source 指向调用方文件。
- **证据**：crypto_test.go:173-178 仅 TestEncryptMessageCodec_Name；grep 无 codec.Marshal/Unmarshal 测试调用；logger.go:87/117 `runtime.Callers(3, pcs[:])` 硬编码深度；logger_test.go 15 个用例无 source 断言
- **核实**：逐项核实成立。(1) crypto_test.go:173-178 确实只有 TestEncryptMessageCodec_Name；全仓 grep 'EncryptMessageCodec' 无任何 Marshal/Unmarshal 测试调用，而该 codec 通过 grpc.ForceCodec 用于 pkg/rpc/client/end_node_rpc.go 全部 end-node RPC（RegisterNode/HeartBeat/AddUsers/GetSub 等十余处），是核心编解码路径。encrypt_codec.go:20 的 `m := v.(proto.Message)` 是无保护断言，Marshal 传非 proto 类型会 panic（Unmarshal 侧 28-37 行有 ok 检查，两侧不对称），恰是 roundtrip/异常测试应覆盖的行为。(2) GetRpcKeyByToken 测试只有 Long(crypto_test.go:151)/Short(:162)，无空 token 用例；实测逻辑 PKCS7Padding([]byte{}, 32) 确为 32 个 0x20（byte(32)），finder 描述准确。(3) logger.go:87 与 :117 均硬编码 runtime.Callers(3)，logger_test.go 全部 16 个用例中唯一 source 相关断言是 TestWithAddSource 的 strings.Contains(out, "source=")，不校验指向调用方文件，任何包装层引入都会静默错位。与 docs/review-2026-04-09.md 无重复（该文档不覆盖 pkg/log 与 pkg/common/rpc）。

### 9. [P2·confirmed] SwapAtomic 并非原子：两次 rename 之间存在无二进制窗口，且失败恢复的 rename 错误被忽略

- **位置**：`pkg/proxy/tools/binary_swapper.go:30` · 维度：错误处理 · 单元：ERR
- **问题**：SwapAtomic 的实现是"rename 当前→.bak，再 rename 新→当前"两步，两步之间 binaryPath 上没有任何文件（若此刻进程重启/宿主崩溃则二进制缺失）；真正的原子替换应是把新文件直接 os.Rename 覆盖到 binaryPath（POSIX rename 对已存在目标是原子替换），备份可通过先 link/copy 实现。更实际的问题是第二步失败时的恢复路径 `os.Rename(backupPath, binaryPath)` 的返回错误被完全忽略：若恢复也失败（例如 .bak 被并发清理、目录权限变化），函数返回 ("", err)，调用方拿到空 backupPath 无法再 Rollback，而磁盘上二进制留在 .bak 位置，binaryPath 为空——错误信息里完全看不出这一点。至少应把恢复失败并入返回错误并告知当前磁盘状态。
- **证据**：binary_swapper.go:30-34 `if err := os.Rename(newBinaryPath, binaryPath); err != nil { os.Rename(backupPath, binaryPath); return "", fmt.Errorf("swap failed: %w", err) }` — 恢复 rename 无错误检查；函数名 SwapAtomic 与两步 rename 的非原子语义不符
- **核实**：读 binary_swapper.go:25-36 核实：实现为两步 rename（当前→.bak，新→当前），两步之间 binaryPath 上确实无文件，函数名 SwapAtomic 与实际语义不符；第二步失败时的恢复 `os.Rename(backupPath, binaryPath)`（:32）返回值被完全忽略，若恢复也失败则返回 ("", err)，调用方（xray updater.go:175-177 直接 return，不 Rollback）拿到空 backupPath 无从恢复，二进制留在 .bak 而 binaryPath 为空，错误信息不反映磁盘状态。POSIX rename 对已存在目标是原子替换这一点属常识级标准行为，且修复方向合理。P2 恰当（窗口极窄，主危害在恢复路径错误吞没）。

### 10. [P2·confirmed] Downloader 与 GitHubReleaseClient 的默认 http.Client 均无超时，配合调用方传 context.Background() 会造成下载永久挂起

- **位置**：`pkg/proxy/tools/downloader.go:19` · 维度：资源 · 单元：ERR
- **问题**：NewDownloader() 和 NewGitHubReleaseClient() 都使用零值 &http.Client{}（无 Timeout，无 Transport 层超时），完全依赖调用方通过 ctx 控制生命周期。但实际调用方并不都传可取消的 ctx：pkg/proxy/containers/hysteria/downloader.go:20 用 context.Background() 调 DownloadToFile，下载 GitHub release 二进制时若对端挂起（TCP 建连成功但不发数据、或传输中途停滞），该调用会无限期阻塞，拖住 hysteria 容器的启动/更新路径且无任何日志。二进制下载动辄数十 MB、走公网 GitHub，是最容易出现长尾挂起的场景。建议给两个构造函数的 client 设置合理 Timeout（或至少 Transport 的 ResponseHeaderTimeout + 整体 deadline），不要把兜底责任全部推给调用方。
- **证据**：downloader.go:19 `return &Downloader{HTTPClient: &http.Client{}}`；github_release_client.go:33 `HTTPClient: &http.Client{}`；调用方 pkg/proxy/containers/hysteria/downloader.go:20 `tools.NewDownloader().DownloadToFile(context.Background(), url, tmpPath)`
- **核实**：逐一核实三处代码：downloader.go:19 `&Downloader{HTTPClient: &http.Client{}}` 零值 client 无 Timeout；github_release_client.go:33 同样 `HTTPClient: &http.Client{}`；调用方 pkg/proxy/containers/hysteria/downloader.go:20 确实用 context.Background() 调 DownloadToFile，且 downloadHysteria 被 hysteria/process.go:46（进程启动前确保二进制存在）和 container.go:304（更新路径）调用——对端停滞时启动/更新路径会无限期阻塞的推理成立。零值 http.Client 无任何超时是 Go 标准行为。P2 恰当。

### 11. [P2·confirmed] YAML/JSON 解码均为非严格模式，拼写错误或大小写不符的配置键被静默丢弃

- **位置**：`pkg/proxy/appconfig/loader.go:177` · 维度：错误处理 · 单元：CFG · ⚠️协议相关（需人工核对上游）
- **问题**：loader.go:177 用 yaml.Unmarshal、loader.go:181 用 json.Unmarshal 解码整棵 AppConfig，两者都不拒绝未知键：yaml.v3 只有通过 Decoder.KnownFields(true) 才会对未知字段报错（依据 yaml.v3 文档/源码，Unmarshal 包级函数无此开关），encoding/json 同理需要 Decoder.DisallowUnknownFields。用户把 `end_node` 写成 `endnode`、`jwt_secret` 写成 `jwt-secret`、或沿用旧格式键名时，整段配置被静默忽略并落回默认值，无任何告警——这正是此前 cert_mgmt 大小写键静默丢弃 bug 的同类根因，本模块是全项目配置入口，应在此处统一收紧。建议改用 yaml.NewDecoder(bytes.NewReader(data)) + KnownFields(true)（legacy 探测分支已有独立的宽松解析，不受影响），JSON 路径用 DisallowUnknownFields。
- **证据**：loader.go:177 `if err := yaml.Unmarshal(data, cfg); err != nil`；loader.go:181 `if err := json.Unmarshal(data, cfg); err != nil`——均无 KnownFields/DisallowUnknownFields；对照 gopkg.in/yaml.v3 Decoder.KnownFields 文档
- **核实**：loader.go:177 用包级 yaml.Unmarshal、:181 用包级 json.Unmarshal 解码 AppConfig，均无未知键拒绝。第三方行为未凭记忆：读了仓库 go.mod 钉死版本的实际依赖源码 ~/go/pkg/mod/gopkg.in/yaml.v3@v3.0.1/yaml.go——`func Unmarshal(in, out)` 直接调用 `unmarshal(in, out, false)`（strict=false），KnownFields 仅是 Decoder 方法（yaml.go:110），包级 Unmarshal 无法开启；encoding/json 为 Go 标准库，Unmarshal 默认忽略未知字段、DisallowUnknownFields 仅在 Decoder 上可用，是语言标准行为。故拼错键（endnode/jwt-secret 等，字段均带显式 tag，键名不匹配即整段忽略）静默落回默认值且无告警，成立。本模块是全项目配置入口（cmd/server.go LoadFromFile 唯一入口），P2 恰当。非旧 review 重复（APP-01/APP-02 均不覆盖）。

### 12. [P2·confirmed] SaveToFile 以 0644 权限明文落盘含 cluster token、http token、jwt_secret、DNS API 凭据的完整配置，且非原子写

- **位置**：`pkg/proxy/appconfig/loader.go:226` · 维度：安全 · 单元：CFG
- **问题**：SaveToFile（loader.go:221-230）把整个 AppConfig yaml.Marshal 后 os.WriteFile(path, data, 0644) 覆盖写。AppConfig 序列化内容包含 EndNode.Cluster.Token（集群共享密钥，即 AES-GCM 加密口令）、HttpToken、JWTSecret、CertMgmt.Challenge.DNS.Credentials（DNS 服务商 API key 明文 map）。0644 使同机任意本地用户可读全部密钥；`v2raymg server --migrate`（cmd/server.go:75）就走这条路径，且对新格式配置还会把内存中刚随机生成的 jwt_secret 一并固化进去。另外写入非原子（无 tmp 文件 + rename），进程在 WriteFile 中途被杀会截断唯一的配置文件。建议改 0600 + 临时文件 rename。
- **证据**：loader.go:226 `os.WriteFile(path, data, 0644)`；config.go:103 ClusterConfig.Token、config.go:174/177 HttpToken/JWTSecret、pkg/certmgmt/service/manager.go:55 `Credentials map[string]string yaml:"credentials"` 全部会被 yaml.Marshal 输出；cmd/server.go:75 `--migrate` 调用点
- **核实**：loader.go:221-230 亲核：yaml.Marshal 整个 AppConfig 后 os.WriteFile(path, data, 0644)，无临时文件+rename，非原子且 world-readable。序列化内容含密钥字段属实：config.go:103 ClusterConfig.Token、:174 HttpToken、:177 JWTSecret 均有 yaml tag 且非 omitempty；pkg/certmgmt/service/manager.go DNSConfig.Credentials map[string]string 带 yaml:"credentials" 同样全量输出。调用点属实：cmd/server.go:74-80 --migrate 分支调用 SaveToFile 覆盖写用户配置文件；且 --migrate 前 LoadFromFile 对新格式配置会先随机生成 jwt_secret（loader.go:199-205），确实会被固化进文件。0644 落盘集群共享密钥+DNS API 凭据，P2 恰当。行号 226 准确。非旧 review 重复。

### 13. [P2·confirmed] ApplyToLogger 打开日志文件失败时静默降级到默认输出，无任何告警

- **位置**：`pkg/proxy/appconfig/config.go:54` · 维度：错误处理 · 单元：CFG
- **问题**：LogConfig.ApplyToLogger 在 Output 为文件路径时 os.OpenFile(..., O_CREATE|O_WRONLY|O_APPEND, 0644)，err != nil 分支什么都不做（config.go:54-56）：既不返回错误、不写 stderr，也不追加 WithOutput 选项，日志静默落到 log.New 的默认输出。典型触发场景：日志目录不存在（O_CREATE 不建目录）、权限不足、路径拼写错误——运维配置了 `log.output: /var/log/v2raymg.log` 却发现文件永远为空，且没有任何线索。cmd/server.go:89 在启动早期调用此函数，此时完全可以向 stderr 打一条 warning 或直接让启动失败。另外该方法签名无 error 返回，调用方无从感知，建议改成返回 (log.Logger, error) 或至少 fmt.Fprintf(os.Stderr, ...)。
- **证据**：config.go:54-56 `if f, err := os.OpenFile(c.Output, ...); err == nil { opts = append(opts, log.WithOutput(f)) }` —— 无 else 分支；cmd/server.go:89 `logger := cfg.Log.ApplyToLogger()`
- **核实**：config.go:52-57 亲核：default 分支 `if f, err := os.OpenFile(...); err == nil { opts = append(...) }`，确无 else——打开失败既不报错、不写 stderr，也不 append 任何 WithOutput，日志静默落到 log.New 默认输出。方法签名 `func (c LogConfig) ApplyToLogger() log.Logger` 无 error 返回，调用方 cmd/server.go:89 `logger := cfg.Log.ApplyToLogger()` 无从感知。O_CREATE 不建父目录、权限不足等触发场景真实，运维配置文件输出后文件永远为空且无线索，成立。P2（静默运维故障、日志丢失）合理。

### 14. [P2·confirmed] isLegacyConfig 启发式可把手工半迁移的新格式配置整份静默丢弃

- **位置**：`pkg/proxy/appconfig/loader.go:60` · 维度：正确性 · 单元：CFG
- **问题**：legacy 判定条件是「存在 server 与 proxy 顶级键且无 node_type」（loader.go:60-65）。node_type 缺省为 end 是文档允许的省略写法（config.go:220 'Default: "end"'），因此一份已经写好 store/forward/containers/end_node 等新键、但残留了旧 `server:`/`proxy:` 段（或注释掉不彻底）且没写 node_type 的配置，会被整体判为 legacy：所有新格式键被丢弃，配置从 migrateLegacyConfig 硬编码模板重建（DSN 强制 ./v2raymg.db、binary_path 强制 /usr/local/bin/xray），仅 stderr 一行提示。叠加非严格解码（未知键本来就不报错），用户几乎无法发现自己精心写的新配置根本没生效。建议：判定改为「无任何新格式专有顶级键（store/containers/end_node 等）」或命中 legacy 后将迁移结果与新键解析结果做冲突检测并报错。
- **证据**：loader.go:60-65 `return hasServer && hasProxy && !hasNodeType`；loader.go:161-169 命中即走 migrateLegacyConfig 并 return，新格式键全部不解析；migrateLegacyConfig 硬编码 loader.go:78 `cfg.Store.DSN = "./v2raymg.db"`、loader.go:89 `"binary_path": "/usr/local/bin/xray"`
- **核实**：loader.go:60-65 亲核：判定确为 `hasServer && hasProxy && !hasNodeType`，且 config.go:220 注释明示 node_type 'Default: "end"' 即文档允许省略。loader.go:159-169：只要 YAML 顶层同时含 server 与 proxy 键且无 node_type，即整体走 legacyConfig 解析（只识别 legacy 键集）+migrateLegacyConfig 硬编码重建（78 行 DSN 强制 ./v2raymg.db、89 行 binary_path 强制 /usr/local/bin/xray）并 return，store/containers/end_node 等新格式键全部不解析，仅 stderr 一行提示。叠加 idx 1 的非严格解码，半迁移配置被整份静默丢弃的场景成立且用户难以察觉。触发前提（残留旧 server:/proxy: 段且省略 node_type）虽需一定巧合，但属文档允许写法，P2 合理。

### 15. [P2·confirmed] Validate 校验缺口仍在：端口范围/上限、node_type 枚举、log.level 枚举、cluster.token 均不校验（旧问题确认仍存在）

- **位置**：`pkg/proxy/appconfig/loader.go:247` · 维度：正确性 · 单元：CFG · 旧 review: APP-01
- **问题**：2026-04-09 review 的 APP-01 仍然成立：Validate（loader.go:247-284）只查 store.dsn 非空、forward min<max、cert email 含 @、ping source type 枚举、非 center 至少一个 enabled 容器。仍缺：rpc_port/http_port 的 1-65535 区间；forward.max_port ≤ 65535（MinPort/MaxPort 是 uint32，写 70000 能通过校验、到分配器 bind 时才失败）；NodeType 枚举——拼错成 "centre"/"центр" 会被静默当 end 节点处理（loader.go:271 与 cmd/server.go:92 都只做 EqualFold("center") 判断，无第三分支报错）；log.level/format 非法值静默回落 info/text（config.go:36,43）；cluster.token 为空时集群 AES-GCM 加密强度问题也无提示。
- **证据**：loader.go:247-284 Validate 全文仅 5 项检查；config.go:119 `RpcPort int`、config.go:168 `HttpPort int` 无处校验；pkg/proxy/forward/port_allocator.go:25-27 MinPort/MaxPort uint32 无 65535 上限；cmd/server.go:92 `if strings.EqualFold(cfg.NodeType, "center")` else 一律按 end 运行
- **核实**：核实成立，但一处细节不准。Validate（loader.go:247-284）确实只有 5 项检查：store.dsn 非空、min_port<max_port、cert email 含 @、ping source type 枚举、非 center 至少一个 enabled 容器。缺口逐条核实：(1) RpcPort（config.go:119）/HttpPort（config.go:168）全程无 1-65535 校验；(2) NodeType 无枚举校验——cmd/server.go:92 仅 EqualFold("center")，else 一律 runEndNode，拼写错误静默按 end 节点运行，无第三分支报错；(3) log.level/format 非法值在 config.go:36/43 的 switch default 静默回落 info/text；(4) cluster.token 为空无任何提示。细节修正：forward.max_port=70000 并非'到分配器 bind 时才失败'——pkg/proxy/forward/port_allocator.go:40-42 会把 MaxPort>65535 静默钳制为 65535（静默钳制本身仍属校验缺口，但无运行时失败）。与旧 review APP-01 重复（docs/review-2026-04-09.md:338，finder 标题已承认但未标 legacyId）。

### 16. [P2·confirmed] 测试缺口：legacy 检测/迁移、SaveToFile、global.go、ApplyToLogger 全部无测试覆盖

- **位置**：`pkg/proxy/appconfig/loader.go:68` · 维度：测试缺口 · 单元：CFG
- **问题**：config_test.go 的 27 个用例覆盖了加载、默认值、Validate 分支和 HttpListen，但本模块风险最高的几条路径零覆盖：(1) isLegacyConfig/migrateLegacyConfig——字段映射最多（约 20 个字段）、且已被发现存在 jwt_secret/NodeSources 缺失问题（见本次 P1），无任何用例构造旧格式 YAML 验证迁移结果；(2) SaveToFile——无 round-trip 测试，yaml 序列化后再 LoadFromFile 是否等价（--migrate 的正确性依赖它）未验证；(3) global.go 的 Init/Get/MustGet/Set（含 MustGet panic 语义）；(4) LogConfig.ApplyToLogger 的 level/format/output 分支与文件打开失败路径。违反 PROJECT_GUIDE 第 9 条「每个可测模块应有测试」的精神——文件存在但关键流程缺位。
- **证据**：config_test.go 测试函数清单（TestLoadFromFile_*/TestValidate_*/TestLoadFromFileWithValidate_*/TestLoadFromFile_HttpListen_*）中无一匹配 legacy/migrate/Save/Global/Logger；loader.go:68-125 migrateLegacyConfig、loader.go:221-230 SaveToFile、global.go 全文、config.go:26-64 ApplyToLogger 均无对应用例
- **核实**：核实成立。config_test.go 全部 26 个测试函数（TestLoadFromFile_*/TestValidate_*/TestLoadFromFileWithValidate_*/TestLoadFromFile_HttpListen_*）中无一覆盖 isLegacyConfig/migrateLegacyConfig（loader.go:58-125）、SaveToFile（loader.go:221-230）、global.go 全文、LogConfig.ApplyToLogger（config.go:26-64）。且该缺口有实际后果佐证：legacy 迁移路径在 loader.go:169 提前 return，跳过了 loader.go:188-206 的 NodeSources 默认值填充与 jwt_secret 自动生成（迁移后的 end 节点 JWTSecret 为空串、NodeSources 为空）——任何一条构造旧格式 YAML 的迁移测试都能捕获此问题，印证'风险最高路径零覆盖'的判断。lineCorrection 无需（loader.go:68 即 migrateLegacyConfig）。鉴于迁移路径存在已证实的实际 bug 且 --migrate 正确性依赖 SaveToFile round-trip，P2 可接受。

### 17. [P3·confirmed] Marshal 对 v 做无检查类型断言，非 proto.Message 直接 panic

- **位置**：`pkg/common/rpc/encrypt_codec.go:20` · 维度：错误处理 · 单元：LOG
- **问题**：EncryptMessageCodec.Marshal 第一行 `m := v.(proto.Message)` 无 ok 检查，而同文件 Unmarshal（第 33-36 行）对同样的断言做了 ok 检查并返回 error。gRPC codec 的 Marshal 在 client 调用路径和 server 发送响应路径上被 grpc-go 直接调用，一旦某个调用点把非 proto 消息交给该 codec（例如未来接入使用该 codec 的新 service 时传错类型），会在 grpc-go 内部 goroutine 里 panic；server 侧 grpc-go 默认没有 recovery interceptor，整个 end 节点进程会崩溃。当前所有调用点（pkg/rpc/client/end_node_rpc.go 各 ForceCodec 调用）都是 proto 消息，属于潜在 panic 面而非现行 bug，但与 Unmarshal 的防御不一致。改成与 Unmarshal 相同的 ok 检查即可。
- **证据**：encrypt_codec.go:20 `m := v.(proto.Message)`（无 ok）对比 encrypt_codec.go:33-36 `vv, ok := v.(proto.Message); if !ok { return fmt.Errorf(...) }`
- **核实**：encrypt_codec.go:20 `m := v.(proto.Message)` 无 ok 检查，与 :33-36 Unmarshal 的 `vv, ok := ...; if !ok { return err }` 防御不一致，属实。所有现行调用点（end_node_rpc.go 各 ForceCodec、server:227 RegisterCodec）传的都是 proto 消息，属潜在 panic 面非现行 bug，finder 亦如实说明。严重度下调：无现行触发路径、纯防御一致性问题，P2 偏高，调为 P3。

### 18. [P3·confirmed] PrefixWriter.Write 出错时返回 (0, err) 违反 io.Writer 契约且可能残留孤儿前缀

- **位置**：`pkg/log/slog.go:230` · 维度：正确性 · 单元：LOG
- **问题**：Write 先把 p 全量 append 进内部 buf（数据实际已被消费），随后逐行写出；一旦底层 w.Write 出错即 `return 0, err`。问题有三：(1) 违反 io.Writer 契约——返回的 n 应是已消费的字节数，此处数据已进 buf 却报 0，若上层按契约重试未写部分会导致日志重复；(2) 循环中前面若干行已成功写出后才失败，返回值同样是 0，信息完全失真；(3) slog.go:230 写 prefix 成功而 slog.go:233 写 line 失败时，输出流里残留一个没有内容的 "[xray] " 孤儿前缀，且该行仍留在 buf 中，下次 Flush/Write 会再带一次前缀重复输出。建议：成功路径已缓冲即返回 len(p)；错误时也返回 len(p)（数据已接收）或严格跟踪已写字节。
- **证据**：slog.go:223 `pw.buf = append(pw.buf, p...)`；slog.go:230-234 两处 `return 0, err`；行从 buf 移除发生在写出之后（slog.go:236），失败行会被 Flush 重发
- **核实**：slog.go:230-234 两处 `return 0, err`，而数据已在 :223 全量 append 进 buf；行从 buf 移除在 :236（写出成功后）。核实三点均成立：(1) 数据已消费却返回 n=0 违反 io.Writer 契约；(2) 循环中前几行已成功写出后失败仍返回 0，信息失真；(3) :230 写 prefix 成功、:233 写 line 失败时输出流残留孤儿 prefix，且失败行仍在 buf，下次 Write/Flush 会再带一次 prefix 重发。行为属实。但仅当底层 w.Write 出错时触发（os.Stdout/Stderr 罕见），且 io.Copy 从子进程 pipe 出错即停不重试，实际影响有限，P2 偏高，调为 P3。

### 19. [P3·confirmed] logSkip 对非 slogLogger 实现的 fallback 会使 caller 归因指向 logger.go 自身

- **位置**：`pkg/log/logger.go:69` · 维度：正确性 · 单元：LOG
- **问题**：logSkip 依赖类型断言 `l.(*slogLogger)` 走 caller 深度修正路径；当 SetDefault 注入的是其他 Logger 实现（接口是公开的，测试里就常注入 fake）时退化为直接调 l.Debug/l.Info——此时该实现若自行取 caller，看到的是 logger.go:73 一带的 logSkip 内部帧而非业务调用方，source 字段系统性错位且无任何提示。同时 skip=3 的深度与 log.Debug→logSkip 的调用层级强耦合（logger.go:87 注释已说明），但没有编译期或测试保护（见测试缺口条目）。建议 Logger 接口增加带 skip 的日志方法或在文档中明确第三方实现的归因约定。
- **证据**：logger.go:68-81 fallback 分支直接 `l.Debug(msg, args...)` 等；logger.go:87 `runtime.Callers(3, pcs[:]) // skip: runtime.Callers, logSkip, log.{Debug,Info,...}`
- **核实**：代码证据确证：logger.go:67-81 中 logSkip 对 `l.(*slogLogger)` 断言失败时直接调 l.Debug/Info/Warn/Error，非 slogLogger 实现若自行解析 caller，看到的最近帧是 logSkip（logger.go:73 一带）而非业务调用方；且 skip=3 深度与调用层级强耦合（logger.go:87 注释自认），无测试保护（呼应 idx 0）。需注明该问题当前为潜在风险：grep 显示生产代码仅 cmd/server.go 与 pkg/proxy/appconfig/config.go 通过 log.New 注入，均为 *slogLogger，现无第三方实现被 SetDefault；但 Logger 接口公开、SetDefault 接受任意实现，测试注入 fake 即触发。P3 定级恰当。

### 20. [P3·confirmed] quoteIfNeeded 不处理 \r 等控制字符，攻击者可控字段可伪造日志行

- **位置**：`pkg/log/slog.go:176` · 维度：安全 · 单元：LOG
- **问题**：quoteIfNeeded 只在字符串含空白 " \t\n\"=" 时才用 %q 引用，其余原样输出。'\r' 及其他控制字符（ANSI 转义 \x1b 等）不在判定集合里：一个含 "abc\r[v2raymg] 2026/07/10 level=INFO msg=fake" 的值（如订阅请求里的用户名、外部传入的节点名——这些在 http/rpc 层常被打进日志）会在终端上用回车覆盖当前行、伪造出一条看似合法的日志，干扰审计排查；ANSI 序列还可污染终端。修复：判定集合改为“含任意 unicode.IsControl 或空白/引号/= 即 %q 引用”，%q 会转义所有控制字符。
- **证据**：slog.go:175-180 `if strings.ContainsAny(s, " \t\n\"=") { return fmt.Sprintf("%q", s) } return s` —— '\r'、'\x1b' 不触发引用，经 writeAttr（slog.go:172）与 msg（slog.go:143）两条路径直出
- **核实**：核实成立，但 finder 的示例 payload 有瑕疵。代码证据：slog.go:175-180 判定集合仅 " \t\n\"="，'\r' 与 '\x1b' 不触发 %q 引用，经 writeAttr(slog.go:172) 与 msg(slog.go:143) 直出；触发路径真实存在——pkg/http/sub_handler.go:88/98/128 将攻击者可控的 query 参数（target 来自 getTargetFromQuery、user 来自 c.Query，gin 会做 URL 解码故 %0D→\r 可注入）打进 log.Error/Debug，且 :88/:98 是 Error 级默认输出。瑕疵：finder 示例 "abc\r[v2raymg] 2026/07/10 level=INFO msg=fake" 含空格和 '='，实际会被 %q 引用；真正穿透的是不含空白/=/引号的 payload，如纯 ANSI 转义序列（\x1b[2K\x1b[1A 污染终端）或 \r 加无空格文本做部分行覆盖。核心缺陷（控制字符不转义）与修复建议（unicode.IsControl 即引用）均正确，P3 恰当。

### 21. [P3·confirmed] v2rayTextHandler.Handle 内残留死代码（frames/f 变量与两处空丢弃）

- **位置**：`pkg/log/slog.go:124` · 维度：架构 · 单元：LOG
- **问题**：Handle 的 source 段里 `frames := slog.Source{}`、`f := r.PC; _ = f`、`_ = frames` 均为无效残留（slog.go:124-138），真正生效的只有 sourceFromPC(r.PC)。另外 `src := &slog.Source{}; *src = sourceFromPC(r.PC)` 也可简化为直接赋值。属清理项，但位于每条 text 日志的热路径上，且会误导后续 reviewer 以为有未完成逻辑。
- **证据**：slog.go:124 `frames := slog.Source{}`、slog.go:126-127 `f := r.PC; _ = f`、slog.go:138 `_ = frames`
- **核实**：逐行核实：slog.go:124 `frames := slog.Source{}`、:126-127 `f := r.PC; _ = f`、:138 `_ = frames` 与描述完全一致，均为无效残留；实际生效逻辑只有 :129-130 的 sourceFromPC(r.PC)（且 `src := &slog.Source{}; *src = ...` 可简化为直接赋值）。该段位于 v2rayTextHandler.Handle 内，text 是默认格式（options.go:45），确在每条日志热路径上。纯清理项，P3 恰当。

### 22. [P3·confirmed] 包文档声称默认输出 stderr，实际 defaultOptions 是 os.Stdout

- **位置**：`pkg/log/logger.go:7` · 维度：正确性 · 单元：LOG
- **问题**：logger.go 包注释第 7 行 "(defaults to INFO level, text format, stderr)" 和 New 的注释第 102 行 "defaults are used (INFO level, text format, stderr)" 都写默认输出 stderr，但 options.go:46 defaultOptions 实际为 `Output: os.Stdout`。依据注释做日志重定向（如 2>file 收集日志）的运维/调用者会把日志重定向丢失。二选一：改注释为 stdout，或按惯例（日志走 stderr）改默认值——后者会改变现有部署行为，需评估。
- **证据**：logger.go:7 与 logger.go:102 均写 "stderr"；options.go:42-50 `Output: os.Stdout`
- **核实**：代码证据确证：logger.go:7 包注释 "(defaults to INFO level, text format, stderr)" 与 logger.go:102 New 注释 "defaults are used (INFO level, text format, stderr)" 均声称 stderr，而 options.go:46 defaultOptions 实际为 `Output: os.Stdout`（options.go:32 字段注释也写 "默认 os.Stdout"，与实现一致，仅 logger.go 两处注释错）。cmd/server.go 走 SetDefault 注入 appconfig 构建的 logger，但 appconfig 未显式 WithOutput 时同样落到 stdout 默认值，注释误导真实存在。文档与实现不符，P3 恰当。

### 23. [P3·confirmed] ProxyError.As 经 errors.As 永远不会被调用，"按 Code 过滤"特性完全失效

- **位置**：`pkg/proxy/errors/errors.go:145` · 维度：正确性 · 单元：ERR · ⚠️协议相关（需人工核对上游）
- **问题**：Go 标准库 errors.As 的实现（src/errors/wrap.go）在遍历错误链时先做类型可赋值性检查：当 err 的具体类型 *ProxyError 可直接赋给 target 指向的类型（**ProxyError 场景）时，直接反射赋值并返回 true，根本不会调用自定义 As 方法；而对其它 target 类型，本方法的 `target.(**ProxyError)` 断言失败直接返回 false。因此 errors.go:151-155 注释宣称的"Allow matching by code"（用预置 Code 的非 nil *ProxyError 做过滤匹配）经 errors.As 调用时永远不生效——errors.As 会无条件覆盖 target，忽略过滤逻辑。这段代码是无效特性 + 误导性文档，若未来有人依赖"按 code 过滤"语义写查询代码会得到与预期相反的结果。建议直接删除自定义 As（保留默认可赋值性行为即可），按 code 匹配统一走 HasCode。
- **证据**：errors.go:145-158 `func (e *ProxyError) As(target interface{}) bool { if te, ok := target.(**ProxyError); ok { if *te == nil {...} if (*te).Code == "" || (*te).Code == e.Code {...} } return false }`；依据 Go 标准库 errors.As 实现：`if reflectlite.TypeOf(err).AssignableTo(targetType) { targetVal.Elem().Set(...); return true }` 先于 `err.(interface{ As(any) bool })` 分支执行
- **核实**：未凭记忆——在本环境用两种方式实证：(1) 读本机 Go 工具链源码 src/errors/wrap.go 的 as() 实现，确认 `reflectlite.TypeOf(err).AssignableTo(targetType)` 的直接赋值分支先于 `err.(interface{ As(any) bool })` 分支执行；(2) 写最小复现程序 link 本仓库 pkg/proxy/errors 实跑：构造 Code=PRX-USR-001 的 ProxyError，用预置 Code=PRX-FWD-001 的 filter 调 errors.As，输出 `filter matched=true, filter.Code now=PRX-USR-001`——errors.As 无视过滤语义直接覆盖 target，errors.go:151-155 注释宣称的"Allow matching by code"经 errors.As 永不生效；对非 **ProxyError target 该方法只会返回 false。属无效特性+误导性文档，删除自定义 As、按 code 匹配走 HasCode 的建议合理。P3 恰当（当前仓库无人依赖该过滤语义）。

### 24. [P3·confirmed] errors 包 legacy 桥接层（ToError/NewErr/NewErrAt/ErrorCode.Is）全仓库零调用方，且自带 nil panic 与字符串误匹配陷阱，应整体删除

- **位置**：`pkg/proxy/errors/errors.go:220` · 维度：架构 · 单元：ERR · 旧 review: E-02
- **问题**：grep 全仓库（含 cmd/），ToError、NewErr、NewErrAt、ErrorCode.Is 除 errors_test.go 外没有任何调用方——实际业务代码只使用 New/Newf/Wrap/HasCode/Code。这层死代码不仅无用，还各自带陷阱：ToError 返回 plain errors.New，与 ProxyError.Is 永远不匹配（旧 review E-02）；ErrorCode.Is(err) 在 err==nil 时 `err.Error()` 直接 panic（errors.go:302），且 strings.Contains(err.Error(), c.Message()) 的子串匹配会把任何消息里碰巧含有 "user not found" 之类文案的无关错误误判为对应 code；Code() 里 errors.As 失败后的手动 Unwrap 递归（errors.go:207-209)是死代码——errors.As 本身已遍历整条 Unwrap 链（含 Go 1.20 多错误分支，覆盖面比手动递归更广）。趁无调用方直接删除整个桥接层（连带 ErrorCode.Message 中仅为 ToError 服务的部分），可同时消灭旧 review E-01/E-02 两条挂账。
- **证据**：errors.go:220-222 ToError、:296-303 ErrorCode.Is（:302 `strings.Contains(err.Error(), c.Message())` 对 nil err panic）、:317-327 NewErr/NewErrAt、:207-209 Code 的死递归；`grep -rn "ToError\|NewErrAt\|NewErr(" --include=*.go .` 排除 errors.go 与测试后无任何结果；实际用法样本 pkg/proxy/usermanager/usermanager.go:358,557 只用 New/Wrap/HasCode
- **核实**：grep 全仓库核实：ToError/NewErr/NewErrAt/ErrorCode.Is 除 pkg/proxy/errors/errors_test.go 外零调用方（NewErr/NewErrAt 连测试都没有引用）。各陷阱逐一验证：(1) ErrorCode.Is 的 nil panic——实跑复现程序调 pe.ErrUserNotFound.Is(nil)，确实 panic（errors.go:302 `err.Error()` 对 nil 解引用）；(2) strings.Contains 子串误匹配风险由 :302 代码直接可见；(3) ToError（:220-222）返回 plain errors.New，与 ProxyError.Is 不匹配——与旧 review E-02（docs/review-2026-04-09.md:22）重复，finder 已注明；(4) Code() 的 :207-209 手动 Unwrap 递归确为死代码：errors.As 已遍历整条 Unwrap 链（含 Go 1.20 的 Unwrap() []error 分支，已从本机工具链 wrap.go 源码确认），手动递归只覆盖其严格子集，As 失败则递归必然也失败。整体删除建议成立，可连带清掉旧 review E-01/E-02。P3 恰当（死代码，当前无触发路径）。

### 25. [P3·confirmed] NormalizeTag 全仓库无调用方，且其剥 v 前缀的语义与 FetchRelease 的 GitHub API 路径要求相矛盾

- **位置**：`pkg/proxy/tools/github_release_client.go:85` · 维度：正确性 · 单元：ERR · ⚠️协议相关（需人工核对上游）
- **问题**：NormalizeTag 把 "v1.19.2" 规整成 "1.19.2"，但 GitHub REST 的 /repos/{owner}/{repo}/releases/tags/{tag} 要求传仓库中真实的 tag 名——xray-core 与 mihomo 的 release tag 均带 v 前缀（如 v1.8.24 / v1.19.2），剥掉 v 后 FetchRelease 会得到 404。目前 grep 全仓库该函数除自身测试外无任何调用方（xray/mihomo updater 都直接把用户传入的 tag 原样交给 FetchRelease），所以尚未造成故障；但这个函数作为公开 API 留在包里，语义上是给 FetchRelease 之前做规整用的，一旦未来被接上就是必现 404。建议删除，或改为保证输出是合法 GitHub tag（例如反向：为纯版本号补 v 前缀，且此逻辑应属于各 repo 的 updater 而非通用 client）。
- **证据**：github_release_client.go:85-91 `return strings.TrimPrefix(t, "v")`；FetchRelease github_release_client.go:45 `url := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", ...)` 需要真实 tag 名（文件头注释 :40-43 自己也说明了 /tags/ 语义）；`grep -rn NormalizeTag --include=*.go .` 仅命中定义与测试
- **核实**：核实成立。(1) 死代码：grep 全仓库 NormalizeTag 仅命中 github_release_client.go:84-91 定义与 github_release_client_test.go 的测试，无任何生产调用方；xray updater (pkg/proxy/containers/xray/updater.go:119) 与 mihomo updater (pkg/proxy/containers/mihomo/updater.go:199 起，TrimSpace 后原样传入) 都把 req.TargetTag 直接交给 FetchRelease。(2) 语义矛盾：FetchRelease (github_release_client.go:45) 构造 /repos/{owner}/{repo}/releases/tags/{tag}，需要真实 tag 名；NormalizeTag 却剥掉 v 前缀。上游 tag 带 v 前缀这一点无需依赖记忆，仓库内材料可确证：docs/mihomo-container-implementation-plan.md:314 明确记录正式 release tag 为 v1.19.24（asset 名 mihomo-linux-amd64-v1-v1.19.24.gz 精确嵌 tag），wiki/knowledge/mihomo-container/details.md:139-142 同样记录 /releases/tags/<tag> 与 <TagName> 精确匹配，xray/mihomo updater_test.go 全部用 v 前缀 tag（v1.8.4 / v1.19.24）作为合法输入。因此一旦 NormalizeTag 被接到 FetchRelease 之前，剥 v 后必得 404。protocolRelated 的上游行为已由仓库内文档/wiki/测试确证。当前无调用方、无实际故障，P3 恰当。与旧 review 无重复（S1 未覆盖 github_release_client.go）。

### 26. [P3·confirmed] BinarySwapperImpl 是全仓库无引用的死代码，与 BinarySwapper 完全重复

- **位置**：`pkg/proxy/tools/binary_swapper.go:51` · 维度：架构 · 单元：ERR
- **问题**：BinarySwapperImpl 的 SwapAtomic/Rollback 只是内部 new 一个 BinarySwapper 再委托调用，与 BinarySwapper 功能完全相同；grep 全仓库（含测试）除定义处外没有任何引用，xray/mihomo updater 均通过 tools.NewBinarySwapper() 使用指针类型。两套等价类型并存会让后续修改者困惑该改哪一个（例如修复本单元的 Rollback 缺陷时漏改一处）。直接删除 BinarySwapperImpl。
- **证据**：binary_swapper.go:50-63 定义 BinarySwapperImpl 及两个委托方法；`grep -rn BinarySwapperImpl --include=*.go .` 仅命中 binary_swapper.go 自身；调用方 pkg/proxy/containers/xray/updater.go:101、mihomo/updater.go 均用 tools.NewBinarySwapper()
- **核实**：核实成立。binary_swapper.go:50-63 定义 BinarySwapperImpl，其 SwapAtomic/Rollback 方法体均为 new 一个 BinarySwapper 再委托，功能与 BinarySwapper 完全等价。grep 全仓库 BinarySwapperImpl（含测试）仅命中 binary_swapper.go 自身四处（定义与两个方法）；实际调用方 pkg/proxy/containers/xray/updater.go:101 与 pkg/proxy/containers/mihomo/updater.go:147 均使用 tools.NewBinarySwapper() 返回的 *BinarySwapper，updater 测试用的是各自的 mockSwapper/fakeSwapper 而非该类型。确属全仓库无引用的重复死代码，P3 恰当。与旧 review 无重复。

### 27. [P3·confirmed] DownloadToFile 直接写最终路径且失败时不清理，io.Copy 中断会在目标位置留下截断文件；out.Close 错误被忽略

- **位置**：`pkg/proxy/tools/downloader.go:39` · 维度：资源 · 单元：ERR
- **问题**：DownloadToFile 用 os.Create(output) 直接在调用方指定的最终路径写入，io.Copy 因网络中断/ctx 取消失败时函数返回错误但截断文件留在原地不删除；同时 `defer out.Close()` 丢弃 Close 的返回错误（磁盘满/延迟写回失败时错误只在 Close 暴露），可能出现"函数返回 nil 但文件不完整"的情况。当前调用方风险被部分缓解——hysteria/mihomo downloader 自己传 .tmp 路径再 rename，xray updater 对 zipPath 有 defer os.Remove 且下游 checksum/unzip 会拦截坏文件——但作为通用工具，正确做法是内部落 tmp 文件、Close 检错、成功后 rename 到 output、失败时清理，避免每个调用方各自重复这套防御（snell downloader 就没有走 tools 层，见其自写实现）。
- **证据**：downloader.go:39-46 `out, err := os.Create(output); ...; defer out.Close(); _, err = io.Copy(out, resp.Body); return err` — 无 tmp+rename、无失败清理、Close 错误被 defer 丢弃
- **核实**：核实成立。downloader.go:39-46：os.Create(output) 直接在最终路径创建文件，defer out.Close() 丢弃 Close 返回错误，io.Copy 失败时直接 return err 而不删除已截断的 output 文件——三个代码事实全部属实。特别是 Close 错误被丢弃意味着 io.Copy 成功但延迟写回/磁盘满在 Close 才暴露时函数返回 nil。finder 对调用方缓解的描述也逐一核实：mihomo/downloader.go:48-50 用 destPath+".gz.tmp" 且 defer os.Remove；hysteria/downloader.go:17-18 用 destPath+".tmp" 且 defer os.Remove；xray/updater.go:133-137 对 zipPath 有 defer os.Remove 且下游有 checksum/unzip 拦截；mihomo/updater.go:240 同类。因此当前无实际故障路径，作为通用工具的健壮性缺陷定 P3 恰当。与旧 review 无重复（S1 未覆盖 downloader.go）。

### 28. [P3·confirmed] binary_swapper.go 与 downloader.go 完全没有单元测试，SwapAtomic 首装/失败恢复/Rollback 路径零覆盖

- **位置**：`pkg/proxy/tools/binary_swapper.go:1` · 维度：测试缺口 · 单元：ERR
- **问题**：pkg/proxy/tools 下只有 checksum_test.go 和 github_release_client_test.go；binary_swapper.go（二进制自更新的核心磁盘操作）与 downloader.go 没有任何 *_test.go，违反 PROJECT_GUIDE 约束 9"每个可测模块应有 *_test.go"。SwapAtomic/Rollback 全是纯文件系统操作，用 t.TempDir() 极易测试，而本单元发现的 P1（首装场景虚假 backupPath + Rollback 丢失二进制）恰恰位于这些无覆盖的分支：正常 swap、binaryPath 不存在、newBinaryPath 不存在、Rollback 无 backup 四个用例即可锁住行为。DownloadToFile 也可用 httptest.Server 覆盖非 200、中途断连残留文件等分支。
- **证据**：`ls pkg/proxy/tools/` 仅有 checksum_test.go、github_release_client_test.go 两个测试文件；binary_swapper.go、downloader.go 无对应测试；本单元 P1 缺陷所在的 binary_swapper.go:25-27/:45-47 分支无任何回归保护
- **核实**：核实成立。ls pkg/proxy/tools/ 确认仅有 checksum_test.go 和 github_release_client_test.go 两个测试文件，binary_swapper.go 与 downloader.go 无任何对应测试。PROJECT_GUIDE.md:76 约束 9 明确要求'每个可测模块对应 *_test.go'。finder 引用的无覆盖高危分支真实存在：binary_swapper.go:25-27 首装场景（binaryPath 不存在时 os.Rename 的 IsNotExist 被吞，仍返回指向不存在文件的 backupPath）与 :45-47（Rollback 先把 binaryPath 挪到 .new，若 backupPath 不存在则第二个 Rename 失败，二进制从原位置丢失），这些纯文件系统分支确实可用 t.TempDir() 低成本锁定。测试缺口类问题定 P3 恰当。旧 review S1 的测试缺口条目仅针对 process/runner.go，与本条不重复。

### 29. [P3·confirmed] PingNodeSource.UpdateInterval 注释宣称默认 300s，实际无任何默认填充，remote 源不配置则永不刷新

- **位置**：`pkg/proxy/appconfig/config.go:134` · 维度：正确性 · 单元：CFG
- **问题**：config.go:133 注释写明 'Default: 300 (5 minutes)'，但 defaultAppConfig、LoadFromFile 都不给 UpdateInterval 填默认值，唯一消费方 pkg/collecter/ping/node_manager.go:69 只在 `src.UpdateInterval > 0` 时才启动周期 reload。结果：用户按文档理解省略 update_interval 的 remote 源加载一次后永不刷新，与注释语义相反，且无告警。要么在 LoadFromFile/Validate 阶段为 type==remote 且 UpdateInterval<=0 的源填 300，要么修正注释为「0 表示不自动刷新」。
- **证据**：config.go:132-134 `// UpdateInterval is the reload interval in seconds for "remote" sources. // Default: 300 (5 minutes).`；pkg/collecter/ping/node_manager.go:69 `if src.UpdateInterval > 0 {` 之外无任何 300 兜底（grep 300 仅此一处相关）
- **核实**：config.go:132-134 亲核：注释确写 'Default: 300 (5 minutes)'，但 defaultAppConfig（loader.go:129-144）、LoadFromFile、Validate 均不为 UpdateInterval 填任何默认值（全仓 grep UpdateInterval 仅 config.go 定义与 node_manager.go 两处消费）。唯一消费方 pkg/collecter/ping/node_manager.go:69 `if src.UpdateInterval > 0` 才 StartReload，省略该字段的 remote 源 Load 一次后永不刷新，与注释语义相反且无告警。注释与实现不一致成立，影响面小（仅 remote ping 源刷新），P3 恰当。行号 134 为字段定义行、注释在 132-133，无需修正。

### 30. [P3·confirmed] global.go 全局单例 API（Init/InitWithValidate/Get/MustGet/Set）在生产代码中零调用，属死代码且开放了无保护的共享可变指针

- **位置**：`pkg/proxy/appconfig/global.go:13` · 维度：架构 · 单元：CFG
- **问题**：grep 全仓库（排除测试与本包）：appconfig 的外部引用只有类型（EndNodeConfig/CenterNodeConfig/PingNodeSource/StaticNodeConfig）和 LoadFromFile/Validate/SaveToFile（均在 cmd/server.go 显式传参），没有任何生产代码调用 Init/InitWithValidate/Get/MustGet/Set——模块地图声称 rpc server/collecter 走 Get() 的说法与现状不符。这套 API 保留着两个问题：(1) 与 cmd/server.go 的显式注入形成两套并存的配置访问模式，后续开发者可能误用；(2) Get 返回共享 *AppConfig，RWMutex 只保护指针本身，深层字段（slice/map，如 Containers、Credentials）无并发保护，一旦有人开始用 Set 做热更新就有数据竞争面。建议删除 global.go 或在注释中明确弃用并约束只读语义。
- **证据**：global.go:13-60 定义 Init/InitWithValidate/Get/MustGet/Set；`grep -rn 'appconfig\.' --include=*.go`（排除本包与 _test）命中仅 cmd/server.go:66/75/84/99/105/278 与 rpc/collecter 的类型引用，无 Get/MustGet/Set/Init 调用
- **核实**：核实成立。global.go:13-60 确实定义了 Init/InitWithValidate/Get/MustGet/Set。全仓库 grep `appconfig\.(Init|InitWithValidate|Get|MustGet|Set)\b`（排除本包）零命中；外部对 appconfig 的全部引用为：cmd/server.go:66/75/84（LoadFromFile/SaveToFile/Validate 显式传参）、cmd/server.go:99/105/278 与 pkg/rpc/server/{center,end}_node_server.go、pkg/collecter/{ping/node_manager.go,ping_collector.go}（均只用类型 AppConfig/EndNodeConfig/CenterNodeConfig/PingNodeSource/StaticNodeConfig）。包内测试 config_test.go 也不覆盖 global.go。生产路径全部走 cmd/server.go 的显式注入，全局单例 API 属死代码；Get 返回共享 *AppConfig、RWMutex 仅保护指针不保护深层 slice/map 的描述与代码一致（global.go:39-43 直接返回指针）。P3 恰当。
