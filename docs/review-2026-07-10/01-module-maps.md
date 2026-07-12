# 模块地图（Phase 0 产出，2026-07-10）

> 21 个模块单元的结构化地图，由并行 map agent 生成，作为分层 review 的输入。机器可读版本在 `tmp/review-maps/<ID>.json`。


## [L0] LOG — pkg/log + pkg/common + pkg/buildinfo

**职责**：项目基础层的三个小模块。pkg/log 是全项目统一的结构化日志封装：基于 slog 提供 Logger 接口、包级全局实例（log.Info 等）、自定义 text handler（"[v2raymg] 时间 level=... source=... msg=..." 格式）以及给子进程（xray/hysteria 等）输出加行前缀的 PrefixWriter。pkg/common 只有节点类型常量（End/Center）。pkg/common/rpc 提供 gRPC 消息的 AES-256-GCM 加密编解码器（token 派生 key + proto marshal 后加密），是 center/end 节点间 RPC 通信的加密层。pkg/buildinfo 是三个 -ldflags 注入的构建元数据变量。


**文件角色**：

- `pkg/log/logger.go` — Logger 接口、包级全局 logger（global/SetDefault/Default）、包级 Debug/Info/... 与 *f 变体、logSkip 调整 caller 深度、slogLogger 实现（New 构造）
- `pkg/log/options.go` — Options（Level/Format/Output/AddSource/Prefix，默认 INFO/text/os.Stdout/AddSource=true/前缀 v2raymg）与函数式 Option（WithLevel 等）
- `pkg/log/slog.go` — buildSlogLogger 按 Options 构建 slog.Logger（JSON handler 或自定义 v2rayTextHandler）；shortSource/sourceFromPC 截短 caller 路径为 pkg/... 或 cmd/...；PrefixWriter 按行给子进程 stdout/stderr 加 "[name] " 前缀（含 Flush）
- `pkg/log/logger_test.go` — 测试：默认配置、各 Option、JSON/text 格式输出、SetDefault/Default、包级函数不 panic、With 链、prefix 默认/自定义/空、PrefixWriter 分行与 Flush（不逐行读）
- `pkg/common/const.go` — 常量 EndNodeType="End"、CenterNodeType="Center"
- `pkg/common/const_test.go` — 测试两个常量字面值
- `pkg/common/rpc/crypto.go` — PKCS7Padding、EncryptWithAES（AES-256-GCM，随机 12 字节 nonce 前置）、DecryptWithAES（长度校验 nonce+tag）
- `pkg/common/rpc/encrypt_codec.go` — EncryptMessageCodec：gRPC codec，proto.Marshal 后 AES 加密 / 解密后 proto.Unmarshal；GetRpcKeyByToken 由 token 截断或 PKCS7 填充成 32 字节 key
- `pkg/common/rpc/crypto_test.go` — 测试：padding 各长度、加解密 roundtrip、随机 nonce、篡改/过短/空数据/错 key/非法 key 长度、GetRpcKeyByToken 长短 token、codec Name（不逐行读）
- `pkg/buildinfo/buildinfo.go` — Version/Commit/BuildTime 三个包级变量，构建时 -ldflags -X 注入，默认 dev/unknown/unknown

**关键流程**：
- 全局日志路径：log.Info(msg, kv...) → logSkip(global, slog.LevelInfo, ...) → 类型断言 *slogLogger → Enabled 检查 → runtime.Callers(3) 取 caller PC → slog.NewRecord → Handler().Handle。非 slogLogger 实现退化为直接调 l.Info（丢失 caller 修正）。
- 实例日志路径：New(opts...) → defaultOptions + Option 应用 → buildSlogLogger(o)：FormatJSON 走 slog.NewJSONHandler（ReplaceAttr 用 shortSource 截短 source，prefix 变成固定 JSON 字段）；默认走 newV2rayTextHandler。之后 slogLogger.Debug/Info/... → l.log(level,...) → runtime.Callers(3) → Handle。
- text handler 输出：v2rayTextHandler.Handle 组装 "[prefix] " + 时间 + level= + source=（sourceFromPC 解析 PC 再按 /pkg/ /cmd/ 截短）+ msg= + preAttrs + record attrs（writeAttr 加 group 点号前缀，quoteIfNeeded 引号），加锁后一次性 Write。
- 子进程输出捕获：NewPrefixWriter(os.Stdout, "xray") → runner.Stdout；PrefixWriter.Write 缓冲到换行，逐完整行写 prefix+line；进程退出后调 Flush 写残余不完整行。
- RPC 加密生命周期：NewEncryptMessageCodec(token) → GetRpcKeyByToken 得 32 字节 key；gRPC 每次调用经 Marshal（proto.Marshal → EncryptWithAES，输出 nonce||ciphertext||tag）与 Unmarshal（DecryptWithAES → proto.Unmarshal）。被 pkg/rpc/client/end_node_rpc.go 与 pkg/rpc/server/end_node_server.go 用作 codec。
- 版本信息：编译时 -ldflags -X 注入 buildinfo.Version/Commit/BuildTime，cmd/version.go、cmd/server.go、pkg/rpc/server/end_node_status.go 读取。

**依赖**：
- pkg/log：无仓库内导入（仅标准库）；被约 62 个非测试 .go 文件导入，全仓最基础的依赖
- pkg/common：无导入；被 pkg/rpc/server/{end_node_server,center_node_server}.go 导入
- pkg/common/rpc：仅导入 google.golang.org/protobuf/proto；被 pkg/rpc/client/end_node_rpc.go 与 pkg/rpc/server/end_node_server.go 导入
- pkg/buildinfo：无导入；被 cmd/version.go、cmd/server.go、pkg/rpc/server/end_node_status.go 导入

**并发模型**：无 goroutine/channel。锁有两处：v2rayTextHandler.mu（slog.go）只保护最终 w.Write，保证多 goroutine 日志行不交叉（buf 组装在锁外，每次 Handle 独立 bytes.Buffer）；PrefixWriter.mu 保护内部行缓冲 buf 及写出。注意 logger.go 的 global 变量本身是无锁的普通变量，SetDefault 与并发 log.Info 之间无同步（约定只在启动时替换）。EncryptMessageCodec 无状态字段仅 key，只读，天然并发安全。

**外部交互**：
- pkg/log 写 io.Writer（默认 os.Stdout；PrefixWriter 用于外部进程 xray/hysteria 等 stdout/stderr 管道输出的落地）
- pkg/common/rpc 使用 crypto/rand 读随机 nonce；本身不做网络 IO，但作为 gRPC codec 处于 center↔end 网络加密链路上
- pkg/buildinfo 无 IO，仅链接期 -ldflags 注入

**风险关注点（reviewer 重点）**：
- pkg/log/logger.go：runtime.Callers(3) 的 skip 深度在 logSkip 与 slogLogger.log 中硬编码，任何包装层级变动都会导致 source 归因错位；logSkip 对非 *slogLogger 的 fallback 分支 caller 会指向 logSkip 内部。
- pkg/log/logger.go: 全局 global 变量无原子/锁保护，SetDefault 与并发日志调用存在数据竞争窗口（依赖启动时单次设置的约定）。
- pkg/log/slog.go: v2rayTextHandler.Handle 中有死代码残留（frames/f 变量与 _ = 丢弃），且 text 输出格式为手工拼接（quoteIfNeeded 仅按空白/引号/= 判断），reviewer 可核对与 slog 语义（如 Group 展开、Attr Resolve）的一致性。
- pkg/common/rpc/encrypt_codec.go: GetRpcKeyByToken 用 PKCS7Padding 填充短 token 做密钥（代码注释自认弱安全）；token 长度不足时密钥熵极低。Marshal 中 v.(proto.Message) 是无检查断言，非 proto 消息会 panic（对比 Unmarshal 有 ok 检查）。
- pkg/common/rpc: 该 codec 是 center/end RPC 唯一加密层，无消息认证之外的重放/版本协商机制，review RPC 层时需结合 pkg/rpc/{client,server} 看 codec 注册与 token 一致性。
- pkg/log/slog.go: PrefixWriter.Write 出错时返回 (0, err) 而非已消费字节数，且 buf 无上限（子进程持续无换行输出会无限增长）——事实陈述供 reviewer 关注。


## [L0] ERR — pkg/proxy/errors + pkg/proxy/tools

**职责**：Two small leaf/infrastructure packages for the proxy refactor. pkg/proxy/errors defines the unified error model: string-based ErrorCode constants (format PRX-MOD-XXX across Domain/Container/Forward/User/Xray/Sys modules), a ProxyError wrapper type with errors.Is/As support, and constructor/inspection helpers (New/Newf/Wrap/Wrapf/Code/HasCode). pkg/proxy/tools provides binary self-update primitives — GitHub release lookup, HTTP download, SHA256 checksum verification, atomic binary swap — used by the xray/mihomo/hysteria updaters. pkg/proxy/tools/process is a generic external-process lifecycle Runner (start/stop/restart/PID) used by all container process wrappers (xray, mihomo, snell, hysteria).


**文件角色**：

- `pkg/proxy/errors/errors.go` — Entire errors package: ErrorCode constants per module prefix, ProxyError type (Error/Unwrap/Is/As), constructors New/Newf/Wrap/Wrapf, extractors Code/HasCode, legacy bridges ToError/NewErr/NewErrAt, ErrorCode.Message() switch mapping codes to human text, ErrorCode.Is() with substring fallback
- `pkg/proxy/errors/errors_test.go` — Tests: sentinel distinctness, New/Newf/Wrap, Code/HasCode extraction, ToError, ErrorCode.Is matching
- `pkg/proxy/tools/checksum.go` — ComputeChecksum (SHA256 of a file) and VerifyChecksum (strict case-insensitive equality after TrimSpace; comment documents removal of former strings.Contains loose match)
- `pkg/proxy/tools/checksum_test.go` — Tests compute, valid/invalid verify, substring rejection, case-insensitivity
- `pkg/proxy/tools/github_release_client.go` — GitHubReleaseClient.FetchRelease queries api.github.com /releases/tags/<tag> or /releases/latest, decodes tag_name + asset name/browser_download_url; NormalizeTag trims and strips leading 'v', empty→"latest"
- `pkg/proxy/tools/github_release_client_test.go` — Tests client constructor defaults and NormalizeTag
- `pkg/proxy/tools/downloader.go` — Downloader.DownloadToFile: HTTP GET with context, requires 200, io.Copy body to os.Create'd output file
- `pkg/proxy/tools/binary_swapper.go` — BinarySwapper.SwapAtomic (rename current→.bak, rename new→current, restore .bak on failure) and Rollback (rename current→.new, restore backup); BinarySwapperImpl duplicates both as value-receiver delegating methods
- `pkg/proxy/tools/process/runner.go` — Runner: mutex-guarded exec.Cmd lifecycle — NewRunner, Start (builds args, appends -c ConfigFile, sets Stdout/Stderr via interface{} type-assert, appends Env onto os.Environ), Stop (SIGINT then 5s timeout force-Kill), Restart, IsRunning, PID, Process, Set/GetConfig
- `pkg/proxy/tools/process/runner_test.go` — Tests runner constructor, start/stop/restart with real processes, already-running/not-running edge cases, SetConfig, PID, version-style invocation

**关键流程**：
- Error creation/propagation: lower layers call errors.New/Newf/Wrap/Wrapf(code, ...) producing *ProxyError; upper layers branch via errors.Code(err), errors.HasCode(err, code), or std errors.Is against another *ProxyError (matches on Code equality in ProxyError.Is).
- Legacy sentinel path: ToError(code) builds a plain errors.New from ErrorCode.Message(); NewErr/NewErrAt wrap sentinels with fmt.Errorf(%w); ErrorCode.Is(err) matches either extracted Code or by strings.Contains(err.Error(), c.Message()) — string-based matching.
- Binary update pipeline (driven by containers/{xray,mihomo,hysteria} updater/downloader files): NormalizeTag → GitHubReleaseClient.FetchRelease(ctx, owner, repo, tag) → pick asset URL → Downloader.DownloadToFile → VerifyChecksum(path, expected) → BinarySwapper.SwapAtomic(binaryPath, newBinaryPath), with Rollback on failure.
- Process start: container process wrappers construct process.NewRunner(RunnerConfig{BinaryPath, Args, ConfigFile, Env, Stdout/Stderr}) then Runner.Start(), which execs the binary with -c <config> appended and env = os.Environ()+cfg.Env.
- Process stop: Runner.Stop() sends SIGINT, spawns a goroutine doing cmd.Wait() into a channel, selects with a 5s time.After, force-Kills on timeout, then nils p.cmd.
- Config change + restart: Runner.SetConfig(newCfg) then Runner.Restart() (Stop then Start); Start uses the updated config.

**依赖**：
- Both packages import only stdlib — no intra-repo imports (leaf packages).
- pkg/proxy/errors imported by (non-test): pkg/proxy/core/container/base.go, pkg/proxy/usermanager/usermanager.go, pkg/proxy/containers/xray/{subscription,exec_runner}.go.
- pkg/proxy/tools imported by: pkg/proxy/containers/{xray,mihomo}/updater.go, pkg/proxy/containers/{hysteria,mihomo}/downloader.go.
- pkg/proxy/tools/process imported by: pkg/proxy/core/container/process.go and pkg/proxy/containers/{xray/exec_runner,mihomo,snell,hysteria}/{container,process}.go.

**并发模型**：All concurrency lives in pkg/proxy/tools/process/runner.go: Runner.mu (sync.Mutex) guards config and cmd across Start/Stop/IsRunning/PID/Process/Set-GetConfig. Stop spawns one goroutine that does p.cmd.Wait() into a buffered chan error(1), and selects it against time.After(5s); on timeout it Kills and drains the channel. Note Restart calls Stop then Start without holding the lock across both (each locks individually). pkg/proxy/errors and the rest of pkg/proxy/tools have no goroutines, locks, or channels.

**外部交互**：
- Network: GitHubReleaseClient.FetchRelease hits https://api.github.com REST (releases/tags, releases/latest); Downloader.DownloadToFile does HTTP GET of arbitrary URLs (both use a default http.Client with no timeout set).
- Disk: ComputeChecksum reads files; DownloadToFile creates/writes output file; BinarySwapper renames binaries and .bak/.new backups (os.Rename, same-filesystem assumption).
- Exec: process.Runner launches external binaries (xray/mihomo/snell/hysteria) via exec.Command, sends SIGINT/Kill signals, inherits or extends the parent environment.

**风险关注点（reviewer 重点）**：
- errors.go ProxyError.Is line 138: `any(target).(ErrorCode)` on an error-typed target — ErrorCode does not implement error at that point in the type switch; reviewers should check whether this branch is reachable. Also ErrorCode.Is (line 296-303) matches by strings.Contains of the message text — fragile string-based error matching.
- errors.go ErrorCode.Message() is a large manual switch; several codes (e.g. ErrContainerStartFailed, ErrPermission, ErrForwardRuleConflict) fall through to default returning the raw code string — asymmetric coverage worth checking against callers of ToError.
- runner.go Start: Stdout/Stderr are typed interface{} and asserted to a Write-method interface without ok-check (lines 75, 80) — a non-writer value panics. ConfigFile appends to p.config.Args via append (line 69), potentially mutating the shared backing array across restarts.
- runner.go Stop/Restart: Restart is not atomic (lock released between Stop and Start); Stop's 5s hard-coded timeout and the fact that a crashed process leaves p.cmd non-nil (IsRunning only checks cmd.Process != nil, never Wait state) mean IsRunning can report true for a dead process.
- binary_swapper.go: SwapAtomic's rollback path ignores the restore os.Rename error; Rollback renames current binary to .new without cleanup; BinarySwapperImpl is a duplicated wrapper type — reviewers should confirm which is actually used.
- downloader.go / github_release_client.go: default http.Client has no Timeout; DownloadToFile writes directly to the final path (no temp file + rename), so a partial download leaves a truncated file at the target.


## [L0] CFG — pkg/proxy/appconfig

**职责**：顶层应用配置模块：定义 v2raymg proxy 重构栈的 AppConfig 结构树（日志/存储/端口转发/证书/容器/订阅/EndNode/CenterNode/ClusterUser），从 YAML 或 JSON 文件加载并填默认值、校验合法性，且能自动检测并迁移旧版（legacy）配置格式。同时提供带 RWMutex 保护的全局配置单例（Init/Get/MustGet/Set），供 cmd/server 和各子系统（rpc server、collecter/ping 等）读取。


**文件角色**：

- `pkg/proxy/appconfig/config.go` — AppConfig 及全部子配置结构体定义（LogConfig/StoreConfig/SubscriptionConfig/ClusterConfig/NodeConfig/PingConfig/EndNodeConfig/CenterNodeConfig/ClusterUserConfig）；LogConfig.ApplyToLogger 把配置转成 pkg/log Logger；DefaultClusterUserConfig 默认值
- `pkg/proxy/appconfig/global.go` — 全局单例：globalCfg + sync.RWMutex，Init/InitWithValidate 加载并存全局，Get/MustGet/Set 读写
- `pkg/proxy/appconfig/loader.go` — 加载/迁移/校验：LoadFromFile（按扩展名选 YAML/JSON 解码、legacy 检测迁移、缺省填充、自动生成 jwt_secret）、SaveToFile、Validate、defaultAppConfig、migrateLegacyConfig、generateRandomSecret
- `pkg/proxy/appconfig/config_test.go` — 测试（未逐行读）：覆盖 YAML/YML/JSON 加载、不支持扩展名/读错误、Validate 各分支（DSN 空、端口区间、无 enabled 容器、非法 email、center 节点豁免、JWT 空/过短现已允许）、默认值应用与用户值覆盖、LoadFromFileWithValidate、HttpListen 各取值场景

**关键流程**：
- 启动加载路径: Init(path)/InitWithValidate(path) (global.go) → LoadFromFile/LoadFromFileWithValidate (loader.go) → os.ReadFile → 若 .yaml/.yml 先 yaml.Unmarshal 到 map 做 isLegacyConfig 检测 → 命中则 migrateLegacyConfig 返回；否则 defaultAppConfig() 再按扩展名 yaml/json Unmarshal 覆盖 → 填 NodeSources 默认 → 非 center 且 JWTSecret 空时 generateRandomSecret(32)（crypto/rand）→ 存入 globalCfg（加锁）
- 校验路径: LoadFromFileWithValidate → Validate(cfg)：检查 Store.DSN 非空、Forward.MinPort<MaxPort、CertMgmt.Email 含 @、ping NodeSources.Type ∈ {file,remote}、非 center 节点至少一个 enabled 容器
- legacy 迁移: isLegacyConfig（有 server+proxy 无 node_type 键）→ migrateLegacyConfig：把旧 server/proxy/cluster/cert 字段映射到 NodeType、CertMgmt、Containers（仅 xray，binary_path 硬编码 /usr/local/bin/xray）、EndNode/Cluster，并向 stderr 打印迁移提示
- 运行期读取: 各子系统调用 appconfig.Get()/MustGet()（RLock）取全局配置；MustGet 未初始化时 panic
- 日志初始化: LogConfig.ApplyToLogger()（config.go）→ 按 level/format/output 组装 log.Option（output 为文件路径时 os.OpenFile 追加打开）→ log.New
- 配置回写: SaveToFile(cfg, path) → yaml.Marshal → os.WriteFile 0644（覆盖写）

**依赖**：
- 导入仓库内包: pkg/certmgmt/service (certmgmtservice.Config)、pkg/log、pkg/proxy/core/container (ContainerMgrConfig/ContainerEntry)、pkg/proxy/core/contracts (ContainerXray)、pkg/proxy/forward (PortAllocatorConfig)
- 第三方: gopkg.in/yaml.v3
- 被导入方（grep 直接引用者）: cmd/server.go、pkg/rpc/server/end_node_server.go、pkg/rpc/server/center_node_server.go、pkg/collecter/ping_collector.go、pkg/collecter/ping/node_manager.go

**并发模型**：仅 global.go：包级 globalCfg *AppConfig 由 sync.RWMutex globalMu 保护，Init/InitWithValidate/Set 写锁，Get 读锁。没有 goroutine 和 channel。注意保护的只是指针本身——Get 返回的 *AppConfig 内部字段没有任何锁保护，调用方共享同一可变对象。

**外部交互**：
- 磁盘读: os.ReadFile 加载配置文件 (loader.go LoadFromFile)
- 磁盘写: os.WriteFile 保存配置 (SaveToFile)；LogConfig.ApplyToLogger 在 Output 为路径时 os.OpenFile 追加打开日志文件
- crypto/rand 生成 jwt_secret
- stderr 直写两条提示信息（legacy 迁移、自动 jwt_secret）
- 无网络/exec/DB 交互（DSN 仅是字符串配置，由别处使用）

**风险关注点（reviewer 重点）**：
- loader.go isLegacyConfig 的启发式检测（server+proxy 且无 node_type）：新格式配置若恰好含这两个顶级键会被误判为 legacy 并静默迁移，reviewer 应关注检测边界
- loader.go LoadFromFile 中 legacy 分支绕过了后续默认填充/jwt_secret 生成/Validate 的部分路径（migrateLegacyConfig 直接 return），迁移出的配置与正常路径的缺省一致性值得核对（如 NodeSources 默认只在非 legacy 路径设置）
- loader.go 非 center 节点 JWTSecret 自动随机生成：每次重启会话失效属于设计行为，但 Init（不带 Validate 的版本）也会触发生成，两个入口行为差异只有 Validate
- global.go Get 返回可变共享指针，AppConfig 深层字段无并发保护；若有运行期改配置（Set/SaveToFile 组合），存在读写竞争面
- config.go ApplyToLogger 文件打开失败时静默忽略（err != nil 分支无处理），输出会 fallback 到 log.New 默认，无告警
- loader.go migrateLegacyConfig 硬编码 /usr/local/bin/xray 与 ./certs、./v2raymg.db 等路径，迁移语义正确性依赖部署约定


## [L1] STO — pkg/store + migrations

**职责**：SQLite 持久层。封装 modernc.org/sqlite（纯 Go 驱动）连接、PRAGMA 初始化和顺序版本迁移框架，并在其上提供三类存储：用户（UserStore，读写 contracts.User，含集群同步字段和 tombstone deletion_state）、inbound 配置（InboundStore，按 tag 存原生 JSON）、本地节点分组（NodeGroupsStore）。StoreManager 是一站式入口：建目录、开库、跑迁移、构造各 store，另带一次性的 InitLoginPasswords 密码补齐。migrations 子包只声明 14 个版本的 schema DDL（migrations.All）。


**文件角色**：

- `pkg/store/db.go` — DB 包装 *sql.DB；Open 打开 SQLite 并设 MaxOpenConns(1) + WAL/busy_timeout/foreign_keys/cache_size PRAGMA；Close/DB 访问器
- `pkg/store/migrate.go` — Migration{Version,SQL} 定义与 Migrate 引擎：建 schema_migrations 表、排序副本、拒绝版本 gap、每个迁移在 WithTx 事务中执行并记录版本
- `pkg/store/tx.go` — WithTx 事务助手：BeginTx→fn→Commit，出错 Rollback（rollback 失败仅 log.Printf）
- `pkg/store/user_store.go` — UserStore 接口 + SQLiteUserStore：Load/Save/Delete/Get/ListByGroup/Count；expiry_time 双格式解析（RFC3339 回退 SQLite datetime），port_mappings JSON 序列化（解析失败仅告警），Save 时空 Role 归一为 normal
- `pkg/store/inbound_store.go` — InboundStore 接口 + SQLiteInboundStore：Save 前 prettyPrintJSON 两空格缩进 native_json，INSERT OR REPLACE；Load/Delete
- `pkg/store/node_groups_store.go` — NodeGroupsStore 接口 + SQLite 实现：List 返回非 nil 空 slice；Set 去重后在单事务内 DELETE 全表 + 逐条 INSERT；Close 为 no-op
- `pkg/store/manager.go` — StoreManager：NewStoreManager(dsn, migrations) 建目录→Open→Migrate→构造 user/inbound store；InitLoginPasswords 用 hashFn(auth_token) 填充空 login_password（阻塞，需在 HTTP server 启动前调用）
- `pkg/store/provider.go` — Provider 接口 + SQLiteProvider：共享 DB 的 UserStore/InboundStore 工厂（每次调用新建实例，实例无状态）
- `pkg/store/migrations/migrations.go` — migrations.All：v1 users 表、v2 inbounds 表、v3 port_mappings 列、v4 重建 users 删 enable 列、v5 累计流量列、v6 role/login_password、v7-10 cluster_users 表+三个索引、v11 local_node_groups 表、v12 集群同步字段(target_group/updated_at_us/origin_node/hash)、v13 deletion_state、v14 password→auth_token 改名
- `pkg/store/db_test.go` — 测试：Open/Close、Migrate 首跑/幂等/版本 gap/增量扩展、WithTx commit/rollback
- `pkg/store/user_store_test.go` — 测试：role/login_password 往返与默认值、更新不干扰其他字段、ListByGroup
- `pkg/store/node_groups_store_test.go` — 测试：List 空、Set/List 往返、Set 替换语义、空 slice 清空、Close
- `pkg/store/manager_test.go` — 测试：InitLoginPasswords 空密码补哈希、非空跳过、幂等、原 token 可验证
- `pkg/store/migrations/migrations_test.go` — 测试：版本连续/SQL 非空/数量、v5-v11 各迁移的 DDL 与默认值、从 legacy DB 全链升级

**关键流程**：
- 启动初始化：NewStoreManager(dsn, migrations.All)（manager.go）→ os.MkdirAll → Open(db.go, 设 PRAGMA + MaxOpenConns(1)) → Migrate(migrate.go) → NewSQLiteUserStore/NewSQLiteInboundStore；失败时关库返回。cmd/server.go 是主要调用方。
- 迁移生命周期：Migrate → 建 schema_migrations → 查 MAX(version) → 排序副本逐个检查 gap（要求恰好 maxApplied+1）→ applyMigration 在 WithTx 事务内执行 m.SQL 并 INSERT 版本记录。
- 用户读写：SQLiteUserStore.Load/Get/ListByGroup 扫 21 列，expiry_time 先 RFC3339 后 '2006-01-02 15:04:05' 双格式解析，port_mappings JSON 解析失败仅 log 不报错；Save 走 INSERT OR REPLACE，空 Role 归一为 'normal'，零值 ExpiryTime 存 NULL。usermanager 通过 UserStore 接口消费。
- inbound 持久化：SQLiteInboundStore.Save → prettyPrintJSON（Unmarshal+MarshalIndent）→ INSERT OR REPLACE inbounds；Load 全表读回 NativeJSON 字节。xray/hysteria/snell/mihomo container 层消费。
- 节点分组替换：SQLiteNodeGroupsStore.Set → 内存去重 → WithTx 内 DELETE 全表 + 逐条 INSERT（原子替换语义）；List 返回非 nil 空 slice。
- 登录密码补齐：StoreManager.InitLoginPasswords(hashFn) → 查 login_password='' 的 (username, auth_token) → 逐用户 hashFn（bcrypt）→ UPDATE；非事务、逐条提交，设计为启动阻塞步骤。

**依赖**：
- 导入仓库内包: pkg/store 仅 user_store.go 导入 github.com/lureiny/v2raymg/pkg/proxy/core/contracts（User 结构体）；migrations 子包导入 pkg/store（Migration 类型）
- 被导入方: cmd/server.go、pkg/proxy/usermanager（usermanager.go/store.go）、pkg/proxy/containers/{xray,hysteria,snell,mihomo}、pkg/proxy/core/container/{factory,manager}.go，以及多处测试（cmd/server_cluster_user_test.go、pkg/proxy/systemtest 等）

**并发模型**：模块本身无 goroutine/channel/自有锁。并发安全完全依赖两点：db.go 的 SetMaxOpenConns(1) 把连接池限成单连接串行化所有读写（配 WAL + busy_timeout=5000 PRAGMA 避免 SQLITE_BUSY），以及 database/sql 自身的池锁。tx.go WithTx 提供事务边界（migrate 与 node_groups Set 使用）；user/inbound store 的单条 Exec 无显式事务。各 store 结构体只含 *DB 指针、无可变状态，可跨 goroutine 共享。

**外部交互**：
- SQLite 数据库文件读写（磁盘），驱动为 modernc.org/sqlite 纯 Go 实现（无 CGO、无外部二进制），WAL 模式会产生 -wal/-shm 伴随文件
- NewStoreManager 用 os.MkdirAll 创建 DSN 所在目录（0755）
- 无网络、exec、其他第三方进程交互

**风险关注点（reviewer 重点）**：
- pkg/store/user_store.go: Load/Get/ListByGroup 三处 21 列 Scan + expiry 双格式解析 + port_mappings 容错解析是手工三份拷贝，列序必须与 Save 的 INSERT 列序及 migrations v1/v3/v5/v6/v12/v13/v14 累积 schema 严格对齐——加字段时最容易漏改的地方
- pkg/store/migrations/migrations.go: v4 用建新表+INSERT SELECT+DROP+RENAME 重建 users；v14 将 password 改名 auth_token（依赖 SQLite RENAME COLUMN 支持）；多语句 SQL（v4/v5/v6/v12）依赖驱动单次 Exec 执行多语句的行为
- pkg/store/migrate.go: gap 检查基于 maxApplied+1，跨分支/回滚场景下版本编排是脆弱点；schema_migrations 建表本身不在迁移事务内
- pkg/store/manager.go InitLoginPasswords: 读-改分离且逐条 UPDATE 非事务，bcrypt 逐用户哈希在大用户量下是启动阻塞热点
- pkg/store/user_store.go Save: port_mappings Marshal 失败被静默吞掉（err 被忽略，写入 '{}'）——reviewer 应确认这是有意为之
- pkg/store/db.go: SetMaxOpenConns(1) 使全部存储串行化，任何长事务（如 node_groups Set 或迁移）会阻塞所有其他查询


## [L1] CTR — core/contracts + core/params + core/inbound

**职责**：Container-agnostic foundation layer of the proxy refactor. contracts defines the shared vocabulary (Protocol/Transport/Security/ContainerType enums, InboundSpec, UserSpec, SubscriptionSpec, ContainerModel, Stats). params normalises the raw map[string]any FastAddInbound payload — fills missing credentials per protocol and materialises TLS cert material to disk from four possible sources. params/protocolparams then promotes that filled map into a strongly-typed *ProtocolParams (one non-nil per-protocol slot, optional TransportSpec/SecuritySpec) plus shared Reality key/shortId helpers. inbound provides a minimal generic Inbound interface + Config + DefaultInbound base implementation that concrete containers (xray/mihomo/hysteria/snell) build on.


**文件角色**：

- `pkg/proxy/core/contracts/protocol.go` — Protocol/Transport/Security/ContainerType string enums with IsValid/AllXxx sets, ValidateCombination, ValidationError type
- `pkg/proxy/core/contracts/inbound.go` — InboundSpec (tag, port, protocol, provider-keyed Extensions map) with Validate (tag non-empty, port 100-65535)
- `pkg/proxy/core/contracts/container.go` — ContainerModel (full desired container state: inbounds, UserBindings, paths, APIPort) and Stats (uplink/downlink per user/inbound)
- `pkg/proxy/core/contracts/user.go` — UserSpec (alias User): credentials (AuthToken vs never-serialised LoginPassword), traffic/bandwidth/client limits, PortMappings, cluster-sync fields (TargetGroup, UpdatedAtUs, OriginNode, Hash), deletion-state helpers IsDeleting/MarkDeleting/IsExpired
- `pkg/proxy/core/contracts/subscription.go` — SubscriptionSpec (per-link generic fields + Extensions + generated URI) and SubscriptionRequest (user, host, port override, ExcludeProtocols)
- `pkg/proxy/core/contracts/protocol_test.go` — Test: enforces AllProtocols/AllTransports/AllSecurityModes stay in lockstep with IsValid; ValidateCombination cases
- `pkg/proxy/core/contracts/user_test.go` — Test: UserSpec expiry, validation, deletion-state transitions
- `pkg/proxy/core/params/defaults.go` — FillDefaults entry: fillCredentials (per-protocol uuid/password/cipher generation incl. SIP022 base64 SS keys), needsTLS gate, resolveCertSource (file/PEM/domain-via-CertManager/self-signed → cert_file+key_file+cert_source), self-signed RSA cert generation, idempotent writeFileIfChanged, FNV-1a hashShort for PEM file naming
- `pkg/proxy/core/params/defaults_test.go` — Test: credential fill per protocol, cert-source resolution matrix, SS password key lengths, scratch-dir file materialisation
- `pkg/proxy/core/params/protocolparams/protocolparams.go` — ProtocolParams struct (dispatch by Protocol; exactly one of VLESS/VMess/Trojan/SS/Hysteria2/TUIC/AnyTLS non-nil), TransportSpec, SecuritySpec/TLSSpec/RealitySpec, per-protocol param structs, sentinel errors ErrProtocolNotSupported/ErrMissingRequired/ErrInvalidCombination; JSON tags are persisted on-disk schema
- `pkg/proxy/core/params/protocolparams/parser.go` — Parse entry: reads protocol+port+tag+listen_addr (default 127.0.0.1), dispatches to parseXxx; helpers requireString/optionalString/requireUint32 (accepts int/int32/int64/float64/uint shapes); normaliseProtocolString folds "ss" alias
- `pkg/proxy/core/params/protocolparams/keys.go` — Canonical string-key constants for the raw request map (transport/TLS/reality/plugin/hy2/tuic/anytls keys); contract with pkg/http/fastAddInbound_handler.go
- `pkg/proxy/core/params/protocolparams/transport.go` — parseTransport (kind gate mirroring FastAdd-supported set, http→h2 normalisation, per-kind sub-field reads) plus optionalStringSlice/optionalBool/optionalInt multi-shape readers
- `pkg/proxy/core/params/protocolparams/security.go` — parseSecurity/parseTLS (cert_file+key_file both-or-neither rule)/parseReality (required target+server_names, autofillReality generates shortId and X25519 pair, validates MaxTimeDiff integer)
- `pkg/proxy/core/params/protocolparams/reality_helpers.go` — GenerateRealityShortID (8 bytes→16 hex), GenerateRealityKeyPair (clamped X25519 → base64url), ValidateRealityShortID, ValidateRealityBase64Key (stricter than xray: enforces 32-byte decode)
- `pkg/proxy/core/params/protocolparams/params_vless.go` — parseVLESS: requires uuid; rejects httpupgrade/h2 transports (mihomo-specific gate); decryption defaults to "none"
- `pkg/proxy/core/params/protocolparams/params_vmess.go` — parseVMess: requires uuid, alter_id default 0, cipher deliberately never defaulted (xray parity)
- `pkg/proxy/core/params/protocolparams/params_trojan.go` — parseTrojan: requires password; TLS/Reality lives on parent SecuritySpec
- `pkg/proxy/core/params/protocolparams/params_ss.go` — parseSS: password+cipher required, udp/plugin/plugin_opts (shadow-tls, obfs-local, v2ray-plugin flat keys)
- `pkg/proxy/core/params/protocolparams/params_hysteria2.go` — parseHysteria2: password required; obfs/up/down/masquerade/ignore_client_bandwidth; transport-fixed (no TransportSpec)
- `pkg/proxy/core/params/protocolparams/params_tuic.go` — parseTUIC: uuid+password (v5), congestion controller / udp relay mode / heartbeat / zero-rtt fields
- `pkg/proxy/core/params/protocolparams/params_anytls.go` — parseAnyTLS: password + padding_scheme + client-side idle-session knobs (int seconds, mihomo Alpha schema)
- `pkg/proxy/core/params/protocolparams/*_test.go (9 files)` — Tests: per-protocol parse matrices (params_*_test.go), parser helpers (parser_test.go), transport×security combos (transport_security_test.go), reality helper round-trips (reality_helpers_test.go), FillDefaults→Parse pipeline (integration_test.go), JSON-tag on-disk schema lock (json_schema_test.go, TestProtocolParamsJSONTagSchema)
- `pkg/proxy/core/inbound/inbound.go` — Generic inbound Config (tag/listen/port/protocol/Extensions) + Inbound interface (Tag/Protocol/Port/Config/Extra/ToNative/Validate), InboundValidator, InboundError with Code/Cause/WithCause and sentinel errors
- `pkg/proxy/core/inbound/default.go` — DefaultInbound base implementation: getters/setters, defaults (tag "default-inbound", port 10000, listen 0.0.0.0), ToNative returns ErrInboundToNativeNotImplemented for containers to override

**关键流程**：
- FastAddInbound pipeline: RPC handler (pkg/rpc/server/end_node_inbound.go) → params.FillDefaults(params, protocol, certMgr, scratchDir) — fillCredentials generates missing uuid/password/cipher, needsTLS decides, resolveCertSource writes cert_file/key_file/cert_source into the map → container's FastAddInbound.
- Typed promotion: mihomo adapter (pkg/proxy/containers/mihomo/{inbound,adapter}.go) calls protocolparams.Parse(raw) → dispatch on contracts.Protocol → parseVLESS/parseVMess/parseTrojan/parseSS/parseHysteria2/parseTUIC/parseAnyTLS, each composing parseTransport + parseSecurity; result *ProtocolParams is persisted as MihomoInbound native JSON to InboundStore (JSON tags are the on-disk schema).
- Cert-source resolution: resolveCertSource priority order — explicit cert_file+key_file (source=file, untouched) → PEM content via writePEMToScratch (content-hash-named, idempotent writeFileIfChanged) → domain via CertManager.GetCert → self_signed via generateSelfSigned (fresh RSA-2048, random file suffix). cert_source tells inbound-removal whether files are deletable (pem/self_signed) or not (file/domain).
- Reality autofill: parseSecurity → parseReality → autofillReality: generates 16-hex shortId when absent (back-written so subscription layer sees the listener's value), generates clamped X25519 keypair via GenerateRealityKeyPair when neither key supplied, rejects half pairs; ValidateRealityBase64Key enforces 32-byte decode.
- Inbound abstraction lifecycle: containers construct inbound.NewDefaultInbound(tag, protocol, port) or embed DefaultInbound, override ToNative() to emit container-native config bytes; Validate() enforces tag non-empty and port in [100,65535]. Consumed via pkg/proxy/core/container/{interface,inbound}.go.
- contracts as shared vocabulary: UserSpec flows through user store, usermanager, cluster sync (Hash/UpdatedAtUs/OriginNode version arbitration), RPC end-node handlers; InboundSpec/ContainerModel describe desired container state; SubscriptionSpec/SubscriptionRequest feed provider-specific URI generators.

**依赖**：
- contracts: no repo-internal imports (stdlib only). Imported very widely (~73 non-test files): pkg/rpc/server/*, pkg/store/user_store.go, pkg/proxy/usermanager (+sync), pkg/xrayapi/hotreload, all containers (xray/mihomo/hysteria/snell), pkg/proxy/core/container, subscription converter.
- params (defaults.go): imports only github.com/google/uuid + stdlib; deliberately flat — CertManager/CertRecord are local shims for pkg/certmgmt. Imported by pkg/rpc/server/end_node_inbound.go.
- params/protocolparams: imports pkg/proxy/core/contracts + golang.org/x/crypto/curve25519. Imported by pkg/proxy/containers/mihomo/{inbound,adapter,profilegen,subscription}.go (mihomo is currently the only typed consumer).
- inbound: imports pkg/proxy/core/contracts. Imported by pkg/proxy/core/container/{interface,inbound}.go and containers hysteria/container.go, snell/container.go, xray/exec_runner.go, mihomo/{container,inbound}.go.

**并发模型**：None in-module: no goroutines, mutexes, or channels in any of the three packages. All state is either pure value types (contracts), caller-owned map[string]any mutated in place (params.FillDefaults writes cert_file/key_file/cert_source and generated credentials back into the caller's map), or per-instance unsynchronised structs (inbound.DefaultInbound getters/setters have no locking — safe only under external serialisation). Concurrency awareness appears only in generateSelfSigned's random file-name suffix, added so concurrent FastAdds for the same CN don't race on the same scratch file; writePEMToScratch by contrast uses deterministic content-hash names shared across inbounds.

**外部交互**：
- Disk: params/defaults.go writes cert/key PEM files under caller-supplied scratchDir (os.MkdirAll 0700, files 0600) — writePEMToScratch (idempotent via writeFileIfChanged read-compare) and generateSelfSigned (os.WriteFile).
- Crypto: crypto/rand for uuids/passwords/shortIds/X25519 scalars; rsa.GenerateKey(2048) + x509.CreateCertificate for self-signed certs (10-year validity, SANs {CN, localhost}); randomHex/randomSSPassword fall back to a time-seeded placeholder string if crypto/rand errors.
- Indirect: CertManager interface delegates domain→cert lookup to pkg/certmgmt (injected; nil disables domain source).
- No network, no exec, no DB in these packages; they only prepare state for containers that manage external xray/mihomo/etc. binaries.

**风险关注点（reviewer 重点）**：
- params/defaults.go resolveCertSource mutates the caller's map in place and its four-source priority order + cert_source tagging drives downstream deletion decisions (pem/self_signed deletable vs file/domain not) — mistakes here delete or orphan operator cert files; also hashShort is FNV-1a (comment says SHA-like) so PEM file-name collisions are theoretically possible across different certs sharing a scratch file name.
- randomHex/randomSSPassword fallback path returns a predictable time-seeded string as a secret if crypto/rand fails — silent weak-credential path worth a reviewer glance.
- protocolparams JSON tags are a persisted on-disk schema (MihomoInbound native JSON in InboundStore); any field/tag rename breaks deserialisation of existing records — json_schema_test.go locks it, so review renames against that test.
- keys.go must stay in sync with pkg/http/fastAddInbound_handler.go convenience-field keys and transport.go's kind gate mirrors handler lines 102-105 — cross-file contract maintained only by comments + tests.
- Mihomo-specific gates live in this nominally container-agnostic layer: parseVLESS rejects httpupgrade/h2, needsTLS encodes mihomo listener behaviour (trojan+reality exemption) — a second container consumer would need these moved.
- Inconsistent port validation floors: contracts.InboundSpec.Validate and inbound.Config/DefaultInbound.Validate enforce [100,65535], while protocolparams.requireUint32 accepts 0 ('allocate one') through 65535 — reviewers should check which validator each call path actually hits.
- reality autofill back-writes generated shortIds/keys into RealitySpec but parser.go states Parse is read-only w.r.t. the raw map — generated Reality material lives only in the returned struct, so subscription-layer agreement depends on the struct (not the map) being the source of truth downstream.
- inbound.DefaultInbound keeps duplicate state (tag_/port_ fields vs the config_ struct built at construction); SetTag/SetPort do not update config_, so Config() can diverge from the getters after mutation — worth verifying no consumer relies on both.


## [L1] CON — core/container

**职责**：Defines the Container abstraction layer of the proxy subsystem: the Container interface every proxy-process implementation (xray/hysteria/snell/mihomo) must satisfy, a BaseContainer with reusable start/stop/state lifecycle machinery, and two registration mechanisms (a legacy global registry with singleton support and a newer Factory-based registry). ContainerMgr consumes ContainerMgrConfig to instantiate all enabled containers via factories and drives StartAll/StopAll/RestoreAll. The package itself is pure orchestration/type-definition code; the actual process management lives in the concrete container packages.


**文件角色**：

- `pkg/proxy/core/container/base.go` — BaseContainer: embeddable lifecycle state machine (Stopped/Starting/Running/Stopping) driven by a Hooks interface providing run/stop funcs; Start/Stop/Restart/MarkRunning/MarkStopped/StopChan
- `pkg/proxy/core/container/interface.go` — Container interface (Init/Start/Stop/Reload/Update/inbound CRUD/FastAddInbound/UserEventChannel/GetUserSubscriptions), RuntimeAPI interface, UpdateRequest/UpdateResult/RestartPolicy/ReleaseSourceConfig types
- `pkg/proxy/core/container/factory.go` — Factory interface (NewConfigObj + New), ContainerConfig decode interface, BuildOptions dependency-injection struct (UserManager, StoreMgr, CertManager as any, CertReader, HTTPPort, ProxyHost)
- `pkg/proxy/core/container/manager.go` — ContainerMgr: LoadFromConfig (factory lookup + config decode + New, fail-fast), StartAll (with auto-Restore for Restorable containers), StopAll, RestoreAll, Get/Types; ContainerEntry/ContainerMgrConfig YAML types
- `pkg/proxy/core/container/registry.go` — Two registries: legacy global containerRegistry (RegisterContainer/RegisterSingleton/NewContainer/GetContainer/SetContainer) and new factoryMap (RegisterFactory/GetFactory/Create/RegisteredTypes)
- `pkg/proxy/core/container/inbound.go` — Inbound type alias to core/inbound.Inbound; InboundAdapter converting contracts.InboundSpec to Inbound (inboundAdapterImpl with spec-precedence overrides); deprecated legacy InboundConfig and error values
- `pkg/proxy/core/container/subscription.go` — SubscriptionProvider optional interface (GetUserSubscriptions)
- `pkg/proxy/core/container/types.go` — ContainerType alias to contracts.ContainerType plus constants (Xray/V2ray/Hysteria/Snell)
- `pkg/proxy/core/container/process.go` — Deprecated backward-compat aliases to tools/process (ProcessRunner, ProcessRunnerConfig, NewProcessRunner)
- `pkg/proxy/core/container/base_test.go` — Tests BaseContainer: nil-hooks panic, state transitions, idempotent Start/Stop, IsRunning, Type
- `pkg/proxy/core/container/manager_test.go` — Tests ContainerMgr.LoadFromConfig (disabled entries, unknown type, decode error) and lifecycle via fake factories
- `pkg/proxy/core/container/interface_test.go` — Tests interface constants/structs: RestartPolicy values, UpdateRequest/UpdateResult field passthrough
- `pkg/proxy/core/container/registry_test.go` — Tests legacy registry register/duplicate/unregister/singleton behavior and NewContainer/GetContainer

**关键流程**：
- Container bootstrap: cmd/server.go / appconfig/loader.go build BuildOptions, call NewContainerMgr then ContainerMgr.LoadFromConfig — for each enabled ContainerEntry: GetFactory(type) → factory.NewConfigObj().Decode(entry.Config) → factory.New(opts) → store in instances map.
- StartAll(ctx): iterates instances, calls Container.Start() on each; on success, if the container implements Restorable, immediately calls Restore(ctx); errors accumulated via errors.Join.
- BaseContainer.Start(): lock, state Stopped→Starting, fetch run/stop funcs from hooks.GetRunFunc(), unlock, execute runFunc() (concrete container spawns the external process here), on success cache stopFunc and MarkRunning(); on failure revert to Stopped with errors.Wrap(ErrContainerStartFailed).
- BaseContainer.Stop(): lock, Running→Stopping, clear cached stopFunc, close(stopChan), unlock, execute stopFunc(); marks Stopped even if stopFunc errors. Idempotent for Stopped/Stopping; Starting state short-circuits to Stopped without calling stopFunc.
- Restart(): BaseContainer default = Stop() then Start() then an extra MarkRunning().
- Factory registration: concrete packages (containers/xray|hysteria|snell|mihomo register.go) call RegisterFactory(kind, f) in init(); ContainerMgr.Create/GetFactory look up factoryMap.
- Legacy singleton path: RegisterSingleton/SetContainer put an instance in globalRegistry.instances; GetContainer(kind) returns singleton if present, else falls back to NewContainer(kind) via factories map (used by pkg/xrayapi hotreload/hotupdate and rpc/server).
- Subscription: rpc server calls Container.GetUserSubscriptions(contracts.SubscriptionRequest) (interface method; SubscriptionProvider is the optional-capability variant), implemented per-container e.g. containers/xray/subscription.go.

**依赖**：
- imports: pkg/proxy/core/contracts
- imports: pkg/proxy/core/inbound
- imports: pkg/proxy/errors
- imports: pkg/proxy/usermanager
- imports: pkg/proxy/tools/process (deprecated aliases)
- imports: pkg/store
- imported by: pkg/proxy/containers/{xray,hysteria,snell,mihomo}
- imported by: pkg/proxy/appconfig (config.go, loader.go)
- imported by: pkg/proxy/core/subscription/manager.go
- imported by: pkg/rpc/server (end_node_server.go, end_node_inbound.go)
- imported by: pkg/xrayapi/{hotreload,hotupdate}
- imported by: cmd/server.go, cmd/test_hotreload

**并发模型**：No goroutines or channels-as-queues in this package itself. BaseContainer (base.go): sync.RWMutex `mu` protects state/containerType/stopChan/hooks/stopFunc; Start/Stop deliberately unlock before invoking run/stop hooks; stopChan (chan struct{}) is closed on Stop as a broadcast signal to concrete containers, and recreated on Start from Stopped/Stopping. ContainerMgr (manager.go): sync.RWMutex protects the instances map; StartAll/StopAll/RestoreAll hold RLock while calling container Start/Stop/Restore. registry.go: globalRegistry uses two locks — `mu` (RWMutex) for factories map and `singletonM` (Mutex) for instances map; factoryMap has its own factoryMapMu RWMutex.

**外部交互**：
- None directly — no network, disk, exec, or DB calls in this package. External-process/exec interaction happens in the concrete container implementations invoked through the run/stop hooks and in tools/process (re-exported here as deprecated aliases).
- UpdateRequest/ReleaseSourceConfig model GitHub-release binary updates, but the download/exec logic lives in per-container updater.go files.

**风险关注点（reviewer 重点）**：
- base.go Start/Stop lock-drop windows: both release mu before running the hook funcs, so state can be observed as Starting/Stopping while hooks run, and concurrent Start+Stop interleavings (e.g. Stop during a long runFunc: Stop's Starting branch marks Stopped and closes stopChan while runFunc is still executing, then Start's success path caches stopFunc and MarkRunning is a no-op only because state left Starting — but stopFunc caching at line ~192 happens unconditionally). Reviewer should trace Start/Stop races carefully.
- base.go Stop() reads stopFunc before the state switch and clears it inside; a Stop racing a Start that hasn't cached stopFunc yet will skip the stop hook.
- registry.go dual-lock design: globalRegistry.mu vs singletonM guard overlapping data (instances map is written under mu in RegisterSingleton/UnregisterContainer but under singletonM in SetContainer/GetContainer/IsSingleton) — inconsistent lock discipline on the same map.
- Two parallel registration systems (legacy ContainerFactory registry + new Factory/factoryMap) plus RegisterFactory silently overwriting duplicates while RegisterContainer errors on duplicates — behavioral asymmetry worth checking at call sites.
- manager.go StartAll holds RLock across Start()+Restore(ctx), which can be long-running process launches; any container callback re-entering ContainerMgr with a write lock would deadlock.
- factory.go BuildOptions.CertManager typed as `any` with per-container type assertions — assertion failures surface at runtime in concrete containers.
- inbound.go carries a large deprecated surface (InboundConfig, error values) alongside the live Inbound alias/adapter; inboundAdapterImpl field-precedence overrides (spec value wins when non-zero) are subtle.


## [L1] SUB — core/subscription

**职责**：聚合并输出用户订阅：Manager 校验用户后向所有已加载 container 收集 SubscriptionSpec，按客户端格式（common/clash/surge/qv2ray）经 converter registry 转换为最终订阅文本。codec 子包提供各协议（vless/vmess/trojan/ss/hysteria2/tuic/anytls/snell）URI 的类型化 Decode/Encode；converter 子包实现具体格式转换器，其中 Clash 转换依赖外部 sub-converter 服务拉模板骨架。另支持拉取外部订阅链接（ext_sub）合并、以及从 URL/HTTP 参数解析 proxy-group/rule-provider/rule 自定义选项。


**文件角色**：

- `pkg/proxy/core/subscription/manager.go` — Manager：GetSubscription 校验用户（userGetter/IsDeleting）后遍历 containerSource 聚合 specs，支持 ExcludeProtocols 过滤；GetSubscriptionForClient[WithOptions] 再走 converter
- `pkg/proxy/core/subscription/converter.go` — Converter 接口 + 全局 registry（RegisterConverter/GetConverter/GetConverterOrDefault，RWMutex 保护）；ConvertOptions 及 ParseProxyGroupParam/ParseRuleProviderParam/ParseRuleParam 参数解析
- `pkg/proxy/core/subscription/format.go` — ClientFormat 常量定义（common/clash/surge/qv2ray）
- `pkg/proxy/core/subscription/http.go` — 外部 HTTP 层：httpGet（10s 超时、4MB 上限）、tryBase64Decode 多变体解码、FetchExternalSub、FetchAndMergeExtSubs（并发、MaxExtSubs=10 截断）、SanitizeURL、FetchProxyGroups/RuleProviders/RulesFromURL
- `pkg/proxy/core/subscription/remote.go` — RemoteFetcher.Fetch：从配置的远程订阅 URL 列表拉 URI 合并为仅含 URI 字段的 spec，全部失败才报错
- `pkg/proxy/core/subscription/uri_convert.go` — ConvertURIs[WithOptions]：按 User-Agent detectFormat，urisToSpecs 经 codec.Decode 把 URI 投影为带 Extensions 的 spec（8 个 xxxNodeToSpec 投影函数）；BuildConvertOptions 从 HTTP 参数构建选项；redactURI 日志脱敏
- `pkg/proxy/core/subscription/fake.go` — GenerateFakeSSSub：生成随机假 SS 订阅 URI（用于向外部 sub-converter 服务拉 Clash 模板）
- `pkg/proxy/core/subscription/codec/node.go` — Node 接口 + Decode 按 scheme 分派到 decoders map（vless/vmess/trojan/ss/hysteria2|hy2/tuic/anytls/snell）；ErrUnsupportedScheme 区分未知 scheme 与解码失败；hostPort IPv6 加括号
- `pkg/proxy/core/subscription/codec/vless.go` — VLessNode Decode/Encode，含 security/transport 归一化、reality/ws/grpc/xhttp 字段
- `pkg/proxy/core/subscription/codec/vmess.go` — VMessNode Decode/Encode（base64 JSON 体格式）
- `pkg/proxy/core/subscription/codec/trojan.go` — TrojanNode Decode/Encode
- `pkg/proxy/core/subscription/codec/shadowsocks.go` — ShadowsocksNode Decode/Encode，含 SIP002/legacy base64 形式与 plugin/PluginOpts 解析
- `pkg/proxy/core/subscription/codec/hysteria2.go` — Hysteria2Node Decode/Encode（obfs/alpn/insecure）
- `pkg/proxy/core/subscription/codec/tuic.go` — TuicNode Decode/Encode（uuid+password、congestion/udp-relay/heartbeat 等）
- `pkg/proxy/core/subscription/codec/anytls.go` — AnyTLSNode Decode/Encode（idle session 参数）
- `pkg/proxy/core/subscription/codec/snell.go` — SnellNode Decode/Encode（psk/version/obfs/obfs-host）
- `pkg/proxy/core/subscription/converter/registry.go` — 对父包 registry 的薄封装（Register/Get/GetOrDefault）
- `pkg/proxy/core/subscription/converter/common.go` — CommonConverter：URI 换行连接后 base64，init 自注册
- `pkg/proxy/core/subscription/converter/qv2ray.go` — Qv2rayConverter：URI 换行连接不编码，init 自注册
- `pkg/proxy/core/subscription/converter/surge.go` — SurgeConverter：逐 spec 输出 Surge 行格式（vmess/trojan/ss/snell/hy2/tuic/anytls，VLESS 跳过；vmess/trojan 仅 tcp/ws），init 自注册
- `pkg/proxy/core/subscription/converter/snell_surge.go` — SurgeConverter.convertSnell 单独文件（psk/version，obfs 暂留 TODO）
- `pkg/proxy/core/subscription/converter/clash.go` — ClashConverter（1416 行）：ConvertWithOptions 走 fetchClashTemplate（4 个外部 sub-converter URL 轮询+假节点触发）→ injectProxiesToTemplate → ensureTemplateGroups / patchTemplateWithOptions（yaml.Node 操作合并自定义 group/rule/provider）；各协议 convertXxx→ClashProxy；ConvertXxxForTest 测试缝；MatchProxies/ValidatePolicy 支持 regex 与内建策略
- `pkg/proxy/core/subscription/converter/ext.go` — Extensions map 读取 helper（extString/extInt/extStringOrJoinSlice）
- `pkg/proxy/core/subscription/converter/http.go` — converter 包内私有 httpGet（注意：无超时、无 body 上限，与父包 http.go 不同）
- `pkg/proxy/core/subscription/*_test.go 及 codec/converter 各 *_test.go` — 测试：manager_test（用户校验/聚合/协议过滤）、http_test（base64 变体、并发合并、截断）、remote_test、uri_convert_test（UA 检测、spec 投影）；codec 每协议 encode/decode 往返；converter 有 clash 协议/自定义 group/patch/template groups/snell surge 大量表驱动测试

**关键流程**：
- 订阅主链路：Manager.GetSubscriptionForClient(WithOptions) → GetSubscription（users.GetUser 校验 + 遍历 containers.Types/Get 调 c.GetUserSubscriptions，单 container 失败静默跳过）→ filterSpecsByProtocol → GetConverterOrDefault(format).Convert/ConvertWithOptions
- URI 转换链路（HTTP /sub 层使用）：ConvertURIs(WithOptions)(userAgent, uris) → detectFormat（UA 大小写不敏感 contains）→ urisToSpecs → codec.Decode 分派各协议解码 → nodeToSpec 投影 Extensions → converter.Convert
- Clash 转换生命周期：ClashConverter.ConvertWithOptions → convertSpec 逐协议构建 []*ClashProxy → fetchClashTemplate（依次请求 4 个外部 sub-converter URL，带 GenerateFakeSSSub 风格假节点，clearClashTemplateNoise 清理）→ injectProxiesToTemplate + ensureTemplateGroups → 有自定义选项时 patchTemplateWithOptions → yaml.Marshal；模板全部失败则整体报错（不静默降级）
- 外部订阅合并：FetchAndMergeExtSubs → 每 URL 一个 goroutine 调 FetchExternalSub（httpGet → tryBase64Decode 四种 base64 变体 + looksLikeURIList 启发式，失败按明文）→ 按序合并，>10 个截断
- 远程订阅源：RemoteFetcher.Fetch → 串行 FetchExternalSub，每 URI 生成仅 URI 字段的 spec，全部失败才返回错误
- 自定义选项构建：BuildConvertOptions(HTTP 参数) → ParseProxyGroupParam/ParseRuleProviderParam/ParseRuleParam + FetchXxxFromURL（逐行解析，坏行忽略）→ ConvertOptions
- converter 自注册：converter 包各文件 init() → Register → subscription.RegisterConverter 写全局 registry（调用方需 blank-import converter 包才有 clash/surge 等格式）

**依赖**：
- 导入: pkg/proxy/core/container (containerSource)
- 导入: pkg/proxy/core/contracts (SubscriptionSpec/Request、Protocol、User)
- 导入: pkg/proxy/usermanager (userGetter)
- 导入: pkg/log
- converter 子包导入父包 subscription 与 gopkg.in/yaml.v3；codec 子包仅依赖 contracts
- 被导入: cmd/server.go、pkg/http/sub_handler.go、pkg/proxy/containers/mihomo/subscription.go、pkg/proxy/core/params/defaults.go、pkg/rpc/server/end_node_server.go、systemtest 多个 matrix 测试

**并发模型**：converter registry 由 converter.go 中的 registryMu (sync.RWMutex) 保护，写入主要发生在 init() 自注册。FetchAndMergeExtSubs (http.go) 每个 ext_sub URL 起一个 goroutine，用 sync.WaitGroup 汇合，结果写入按索引预分配的 results [][]string（无共享写冲突）。Manager 本身无锁无状态，线程安全性依赖 containers/users 依赖方。fake.go 每次调用 rand.NewSource(time.Now().UnixNano()) 创建独立 rand 实例（非并发共享）。

**外部交互**：
- http.go httpGet: 出站 HTTP GET（10s 超时、4MB body 上限）——拉外部订阅 ext_sub、proxy_groups_url/rule_providers_url/rules_url 配置
- converter/http.go httpGet: 另一份出站 HTTP GET 实现，无超时、无 body 上限，用于 fetchClashTemplate
- converter/clash.go subConverterURLs: 依赖 4 个第三方公网 sub-converter 服务（sub.xeton.dev、api.dler.io、sub.maoxiongnet.com、sub.id9.cc）获取 Clash 配置模板，用假 SS 节点触发
- 无磁盘/exec/DB 交互（RuleProviderConfig.Path 只是写进 YAML 的路径字符串）

**风险关注点（reviewer 重点）**：
- converter/clash.go（1416 行）是复杂度中心：yaml.Node 级模板操作（injectProxiesToTemplate/ensureTemplateGroups/patchTemplateWithOptions/appendGroupNameToAllOtherGroups）、typed-vs-generic 两套 group 合并逻辑（applyCustomGroupMembership 与 ...Local）、clearClashTemplateNoise 用字符串替换修补模板（ReplaceAll ',,' / ',dns-failed'）
- Clash 输出对外部第三方 sub-converter 服务硬依赖（fetchClashTemplate，4 个 URL 全挂则订阅失败，ConvertWithOptions 明确不降级）；且 converter/http.go 的 httpGet 无超时、无响应大小限制，与父包 http.go 行为不一致
- manager.go GetSubscription 对单 container 错误静默 continue（无日志），排查订阅缺协议时不可见
- uri_convert.go 的 8 个 nodeToSpec 投影函数与 converter 层 extString key 是隐式字符串契约（如 server_name/utls_fingerprint/plugin_mode），改一侧需同步另一侧；ss plugin opts 做了 obfs/mode 双 schema 归一
- http.go tryBase64Decode 的启发式（UTF-8 + 含 '://'）决定明文/base64 判定，边界输入行为值得关注；FetchExternalSub 供外部不可信内容进入 codec.Decode
- 凭据泄漏面：redactURI（uri_convert.go）与 SanitizeURL（http.go）负责日志脱敏，surge.go/clash.go 输出中直接嵌 spec.Password，review 时关注新增日志点
- codec 各协议 Decode 处理不可信 URI 输入（base64、query 解析），shadowsocks.go 有 SIP002/legacy 双格式分支，vmess.go 是 base64-JSON 解析，是模糊输入面
- converter registry 依赖 init() 自注册，调用方若未 import converter 子包则 GetConverterOrDefault 返回 nil（manager.go 已处理但 ConvertURIs 返回空串 nil error）


## [L2] FWD — pkg/proxy/forward

**职责**：进程级 TCP/UDP 端口转发层：为每个 (container_type, inbound_tag, username) 规则分配一个用户面向监听端口，把流量中继到容器内部 inbound 端口（Client → UserPort → Relay → Container InboundPort）。内置流量计数（按规则）、用户级聚合带宽限速（token bucket，跨该用户所有规则共享）、用户级 remote-IP 客户端槽位限制、端口池分配。设计为进程内单例（GlobalForwardManager），上层 UserManager/rpc/xrayapi 通过 ForwardManager 接口使用它，不感知 inbound 语义。


**文件角色**：

- `pkg/proxy/forward/manager.go` — ForwardManager 接口定义（AddRule/RemoveRule*/GetTraffic*/SetUser*Limit/DropUser/AllocatePort/ReleasePort/Close），含每个方法的语义契约注释
- `pkg/proxy/forward/forward_manager.go` — DefaultForwardManager 实现：规则生命周期、端口分配回滚、用户级 bandwidth/client 限速器的创建与稳定引用管理、流量查询与 group-by 聚合
- `pkg/proxy/forward/relay.go` — Relay 与 Limiter 接口定义及契约（共享 token bucket、Stop 需 drain）
- `pkg/proxy/forward/relay_tcp.go` — TCPRelay：accept 循环、per-conn 双向 copy goroutine、限速 reader/counting writer、单向结束后的 drain-deadline 处理
- `pkg/proxy/forward/relay_udp.go` — UDPRelay：单 readLoop 按源地址 demux 到 per-client session（各自 upstream UDPConn + downstreamLoop），idle GC，reapSession 幂等回收
- `pkg/proxy/forward/ratelimit.go` — TokenBucketLimiter（LimitReader/LimitWriter/WaitN 共享一个桶，SetRate 原地更新）、NoopLimiter、userBandwidthLimiter（每用户上/下行稳定引用限速器）
- `pkg/proxy/forward/clientlimit.go` — ClientLimiter 接口 + remoteIPClientLimiter：按 remote IP 的槽位限制，Acquire/Confirm/Cancel/Release 状态机、recycle 定时器、drain deadline
- `pkg/proxy/forward/traffic.go` — TrafficCounter（全 atomic 的 up/down/activeConns，Snapshot 支持 swap-to-0 重置）与 TrafficRegistry（ruleKey → counter 映射）
- `pkg/proxy/forward/port_allocator.go` — PortAllocator：范围内随机（crypto/rand）或 round-robin 分配、reserved 集合、AllocateSpecific/Release
- `pkg/proxy/forward/types.go` — ForwardRule（含 Validate/RuleKey/ResolvedNetwork）、TrafficSnapshot/ForwardTrafficRecord/TrafficQuery(Result)、BandwidthLimitKind 等数据类型
- `pkg/proxy/forward/global.go` — 全局单例：SetGlobalConfig（初始化后再调 panic）、GlobalForwardManager 懒初始化、ResetGlobalForwardManager（测试用）
- `pkg/proxy/forward/forward_manager_test.go` — 测试：规则增删/去重/按用户查询、traffic 读取与 reset、指定端口、Close、用户带宽限制生效与动态更新、UDP dispatch、AllocatePort 与 AddRule 共池
- `pkg/proxy/forward/relay_test.go` — 测试：TCP 中继端到端转发、计数、限速、maxConns、client limiter 拒绝路径
- `pkg/proxy/forward/relay_udp_test.go` — 测试：UDP 基本转发、session 复用/多客户端/上限、client limiter 拒绝、idle GC、Stop drain、limiter 下的字节计数、并发客户端
- `pkg/proxy/forward/ratelimit_test.go` — 测试：token bucket 限速/burst/maxChunk/SetRate/WaitN(ctx、共桶、并发)、userBandwidthLimiter 引用稳定性
- `pkg/proxy/forward/integration_test.go` — 测试：TCP+UDP 共享用户带宽与 client slot、AddRule 后再设带宽对两种 relay 均生效、UDP 规则重启幂等
- `pkg/proxy/forward/port_allocator_test.go` — 测试：端口分配/释放/reserved/耗尽/随机模式
- `pkg/proxy/forward/traffic_test.go` — 测试：TrafficCounter/TrafficRegistry 计数与 reset
- `pkg/proxy/forward/types_test.go` — 测试：ForwardRule.Validate/RuleKey/ResolvedNetwork
- `pkg/proxy/forward/global_test.go` — 测试：全局单例初始化、SetGlobalConfig 顺序约束

**关键流程**：
- AddRule (forward_manager.go): Validate → 持 m.mu 检查重复 ruleKey → allocator.Allocate/AllocateSpecific → traffic.GetOrCreate → 确保 userBandwidth[username] 存在（首建时从 rule 级限速种子化）→ 决定 effective client-limit 配置（rule.MaxClients>0 > storedConfig > 无限制，rule 负值=passthrough 且不覆盖 storedConfig）→ 复用或新建用户级 remoteIPClientLimiter → 按 ResolvedNetwork 构造 NewTCPRelay/NewUDPRelay → relay.Start()；失败则回滚端口和 counter
- TCP 数据面: TCPRelay.acceptLoop → clientLimiter.Acquire(remoteIP) → maxConns 检查 → go handleConn：DialTimeout target → ConfirmAcquire + activeConns/counter.IncrConns → 两个 copyWithCount goroutine（limiter.LimitReader 包裹 + countingWriter 计数）→ select 循环等 uploadDone/downloadDone/100ms tick/ctx.Done，处理单向结束后的 drain deadline → cleanup（sync.Once：DecrConns + clientLimiter.Release）
- UDP 数据面: UDPRelay.readLoop（单 goroutine ReadFrom）→ sessions.Load 或 establishSession（7 步：session 上限 → Acquire → ResolveUDPAddr+DialUDP → LoadOrStore 注册 → ConfirmAcquire → 起 downstreamLoop）→ forwardUpstream（limiterUp.WaitN 10ms 超时否则丢包 → upstream.Write → AddUpload）；downstreamLoop：upstream.Read（100ms×2.5 deadline）→ limiterDown.WaitN → conn.WriteTo(client) → AddDownload；gcLoop 每 5s 按 lastActive reap 空闲 session；reapSession（closeOnce 幂等）：cancel+close upstream → sessions.Delete → 减 activeSessions/DecrConns/clientLimiter.Release
- RemoveRule / Close (forward_manager.go): 持锁 relay.Stop()（TCP: cancel+close listener+wg.Wait；UDP: stopOnce 内 cancel+close+reap 全部 session+wg.Wait）→ allocator.Release → traffic.Remove → delete(rules)。RemoveRulesByUser/ByInbound 先收集 key 再逐个 RemoveRule
- SetUserBandwidthLimit (forward_manager.go): 查/建 userBandwidth[username] → userBandwidthLimiter.UpdateRates → TokenBucketLimiter.SetRate 原地更新——已运行的 relay 持有同一 limiter 引用，立即生效；bytesPerSec=0 清除该方向的 set 标记
- SetUserClientLimitConfig / DropUser: 前者写 userClientLimitConfigs 并对已存在的 remoteIPClientLimiter.SetConfig 原地更新（MaxClients<=0 = passthrough，不换实例）；DropUser 删除三个 per-user map 条目，活跃 relay 仍持旧 limiter 指针（注释明确接受短暂 2x 配额窗口）
- AllocatePort/ReleasePort: 仅委托 PortAllocator，不建 relay/规则；供容器自开 127.0.0.1 监听时保证与 AddRule 共用一个端口唯一性表
- 全局单例: GlobalForwardManager() 懒初始化 DefaultForwardManager（globalConfig 默认 10000-65535）；SetGlobalConfig 必须在首次调用前，否则 panic

**依赖**：
- imports: pkg/log (forward_manager.go), pkg/proxy/core/contracts (types.go: ContainerType/Protocol); 其余为标准库
- imported by: pkg/proxy/usermanager (主要消费者), pkg/proxy/appconfig, pkg/rpc/server (end_node_user.go), pkg/xrayapi/hotreload + hotupdate, cmd/server.go, cmd/test_hotreload, pkg/proxy/systemtest 与 containers/{xray,mihomo} 的测试

**并发模型**：DefaultForwardManager.mu (RWMutex) 保护 rules/closed/userBandwidth/userClientLimiters/userClientLimitConfigs；数据面刻意不经该锁——TrafficCounter 全 atomic，limiter 引用在 AddRule 时交给 relay 后独立使用。TCPRelay：ctx+cancel 停机、wg 追踪 acceptLoop 和每连接 handleConn、每连接再起 2 个 copy goroutine + copyWg，drainMu 保护 drainDeadline/drainActive，activeConns 为 atomic。UDPRelay：单 readLoop goroutine（establishSession 的 maxSessions 检查依赖此单读者假设，代码注释明示）、每 session 一个 downstreamLoop、gcLoop、forwardUpstream 写失败时异步 spawn reap goroutine（wg 追踪）；sessions 用 sync.Map + LoadOrStore 防重建；session.closeOnce 幂等回收；relay.stopOnce 幂等 Stop。TokenBucketLimiter：mu 保护 tokens/rate/burst，unlimited 为 atomic.Bool 快路径（SetRate 与 waitForTokens 有 stale-false 双检窗口处理）；waitForTokens 中途 Unlock-Sleep-Lock。remoteIPClientLimiter：单 mu 保护 slots/drainEnd/config，slot 内计数为 atomic，Release 用 time.AfterFunc 定时回收槽位（回调再取 l.mu）。PortAllocator/TrafficRegistry 各自独立 mutex。global.go 用 globalMu 保护单例。

**外部交互**：
- 网络: net.Listen/Accept/DialTimeout (TCP relay), net.ListenPacket/ResolveUDPAddr/DialUDP/WriteTo (UDP relay)——是模块唯一的外部 I/O
- crypto/rand: 随机端口选择 (port_allocator.go Allocate)
- 无磁盘/exec/DB/第三方二进制交互；对容器进程的关系仅是向其监听端口转发字节

**风险关注点（reviewer 重点）**：
- relay_tcp.go handleConn 的 drain 状态机：drainMu 下的 drainDeadline/drainActive、uploadDone/downloadDone channel 与 100ms idleTick 的 select 循环、条件 `!time.Now().IsZero()`（恒 true）、drain 过期时对另一方向 done channel 的阻塞接收——本模块最复杂的并发交织点
- relay_udp.go establishSession 与 readLoop 的单读者假设：maxSessions 检查 + activeSessions 原子加的 race-free 性依赖 readLoop 单 goroutine（注释自述）；forwardUpstream 直接复用 buf[:n] 不拷贝，同样依赖此假设；同步 DialUDP 阻塞 readLoop 的注释中已知限制
- forward_manager.go AddRule 的 client-limit 优先级逻辑（rule>0 / rule<0 / rule==0 三分支 + ruleOwnsUserPolicy 决定是否覆盖 storedConfig）——语义细腻，reviewer 应对照 SetUserClientLimitConfig 与 DropUser 的注释契约核对
- clientlimit.go remoteIPClientLimiter：Acquire/ConfirmAcquire/CancelAcquire/Release 四段状态机 + recycling 标志 + time.AfterFunc 回调再取锁；Acquire 每次 O(slots) 遍历计数；recycling 槽位继续占配额的语义
- ratelimit.go TokenBucketLimiter.waitForTokens 中途释放锁 sleep 再取锁，以及 SetRate 与 unlimited atomic 的顺序约定（stale-false 窗口靠锁内 rate 复检兜底）
- forward_manager.go DropUser 与规则拆除并行的 '短暂 2x 配额' 已知窗口（注释明确接受）；UpdateRateLimit 只改 rule 字段、对已运行 relay 无效（注释自述 trade-off）
- port_allocator.go Allocate 每次构建全量 available 切片（范围 10000-65535 时约 5.5 万元素的 O(n) 分配路径）
- Close (forward_manager.go) 持 m.mu 期间逐个 relay.Stop() 并 wg.Wait()，规则多时全局锁持有时间长


## [L2] UM — pkg/proxy/usermanager (+sync)

**职责**：UserManager 是 end 节点的用户核心状态机：维护 users map（含 tombstone 软删除）、UUID v4 AuthToken、过期时间、角色/集群分组，并通过 pub/sub 事件通道 (UserEvent) 通知各协议容器（xray/hysteria/snell/mihomo）做 inbound/端口联动。它同时负责端口绑定生命周期（经 forward.ForwardManager 分配/轮换/释放转发端口）、按用户的带宽与客户端数限额下发、以及基于 forward 层计数器的流量统计聚合与持久化。子包 sync 提供集群同步原语：版本仲裁 (IsNewer)、SHA-256 内容哈希 (ComputeHash)、心跳 digest 差量比较 (CompareDigests)，供 UserManager 的 SyncUpsertUser/ListDigests 走多节点最终一致同步。


**文件角色**：

- `pkg/proxy/usermanager/usermanager.go` — 核心 2293 行大文件：UserManager 结构体、CRUD（AddUser/RemoveUser/UpdateUser*）、mutateUser 统一变更路径、事件 pub/sub（Subscribe/emitEvent）、端口分配与轮换（GetBindPort/RotateUserPortForInbound/RotateAllUserPorts/ReleaseBindPort）、集群同步入口（SyncUpsertUser/ListDigests/BackfillClusterFields）、statsCollector 流量采集器及 ResetUserTotalTraffic
- `pkg/proxy/usermanager/store.go` — 对 pkg/store 的类型别名转发（UserStore/SQLiteUserStore/NewSQLiteUserStore），纯向后兼容
- `pkg/proxy/usermanager/bandwidth_stats_collector.go` — BandwidthStatsCollector 适配器：DrainStats() 把 GetAllDeltaTraffic(reset=true) 转成 []*proto.Stats 供 pkg/rpc/server 上报（Prometheus/center 拉取模式）
- `pkg/proxy/usermanager/sync/version.go` — IsNewer(a,b)：UpdatedAtUs 优先，相等时 OriginNode 字典序 tie-break
- `pkg/proxy/usermanager/sync/hash.go` — ComputeHash：对 user 规范字段做长度前缀 SHA-256，与旧 pkg/cluster_user/hash 线上兼容；LoginPassword 仅非空时参与哈希
- `pkg/proxy/usermanager/sync/digest.go` — UserDigest 结构与 CompareDigests：心跳差量同步，决定哪些用户需要拉全量（不存在/远端更新/同版本哈希不一致/DB 错误自愈）
- `pkg/proxy/usermanager/usermanager_test.go` — 测试：CRUD、tombstone 语义、GetBindPort/ReleaseBindPort、group 过滤、AuthToken 生成/冲突、SyncUpsertUser token 仲裁、store 持久化（不逐行读）
- `pkg/proxy/usermanager/sync_runtime_effects_test.go` — 测试：SyncUpsertUser/RemoveUser/构造时的 forward 层运行时副作用（限速/限客户端）一致性与 race-free
- `pkg/proxy/usermanager/rotate_inbound_port_test.go` — 测试：RotateUserPortForInbound/RotateAllUserPorts make-before-break、订阅端口更新
- `pkg/proxy/usermanager/reset_user_total_traffic_test.go` — 测试：ResetUserTotalTraffic 持久化、防双计、与 collect 并发
- `pkg/proxy/usermanager/login_password_test.go` — 测试：loginPasswordHasher 注入路径（AddUser/UpdateUser*）与 hasher 失败处理
- `pkg/proxy/usermanager/store_test.go` — 测试：SQLiteUserStore save/load/upsert/delete/expiry
- `pkg/proxy/usermanager/bandwidth_stats_collector_test.go` — 测试：接口实现断言
- `pkg/proxy/usermanager/testhelper_test.go` — 测试辅助工具
- `pkg/proxy/usermanager/sync/sync_test.go` — 测试：IsNewer/ComputeHash 确定性与字段敏感度/CompareDigests 各分支

**关键流程**：
- 用户创建：AddUser(req) → validateUserCredentials → 锁内检查 tombstone 重建条件（forwardMgr.GetRulesByUser 必须为空）→ setAuthTokenLocked 自动生成 UUID → 赋默认 TargetGroup → stampVersion → loginPasswordHasher 生成登录密码哈希 → store.Save → emitEvent(UserEventAdd)，容器订阅者据此建 inbound。
- 统一变更路径：所有既有用户的同步字段修改（SetUserRole/SetUserGroup/UpdateUser/UpdateUserPassword/SetUserBandwidthLimit/SetUserClientLimit/ResetAuthToken/RemoveUser）走 mutateUser(username, eventType, closure)：lock → lookup → mutate → stampVersion → store.Save → emitEvent；errMutateSkip 哨兵支持幂等中止。SetUser{Bandwidth,Client}Limit 随后在 RLock 下快照字段并调 applyRuntimeSideEffects 下发到 forward 层。
- 删除生命周期（两阶段）：RemoveUser → mutateUser 标记 MarkDeleting()（tombstone，永不物理删除）→ emitEvent(UserEventRemove) → forwardMgr.DropUser 释放限速器状态；容器收到事件异步拆除 inbound 并回调 ReleaseBindPort/GetUserPortByDstForCleanup 完成端口清理。过期用户由 StartMaintenance goroutine 定期 cleanupExpiredUsers → RemoveUser 走同一路径。
- 端口绑定：GetBindPort(req) → 若 forwardMgr.GetRule(ruleKey) 已存在直接返回 → 否则用 user.PortMappings[dstPort] 作 preferredPort 调 forwardMgr.AddRule（失败退回自动分配）→ 更新 BindPorts/PortMappings → store.Save → emitEvent(UserEventPortBind)。
- 端口轮换（make-before-break）：RotateUserPortForInbound → 快照旧 rule → 用临时 tag AddRule 起新 relay → RemoveRule 旧 relay → 删临时并以正式 key 重建（端口被抢则自动分配）→ 锁内更新 BindPorts/PortMappings 持久化 → emitEvent(UserEventPortBind)。RotateAllUserPorts 逐 inbound 循环调用并收集首个错误。
- 集群同步（拉方向）：心跳交换 ListDigests() → 远端用 sync.CompareDigests(getLocal, remoteDigests) 决定需拉全量的用户名 → GetUserForSync 提供全量 → 对端 SyncUpsertUser(incoming)：sync.IsNewer 版本仲裁后直接采纳远端版本字段（不 re-stamp），分 tombstone/新用户/更新三个分支，各自 store.Save + applyRuntimeSideEffects/DropUser + emitEvent。
- 流量统计：StartTrafficStats(interval) 注册 onCollect 回调并启动 statsCollector.collectLoop goroutine；每 tick collect() 调 forwardMgr.GetAllTrafficRecords(false) 算增量累进 ByUser/ByInbound/ByContainer/Global，然后在 sc.mu 持有下同步调 onCollect：用 m.lastSeenUserTotal 算持久化增量写入 user.TrafficTotal* 并 store.Save。Prometheus/center 侧经 BandwidthStatsCollector.DrainStats → GetAllDeltaTraffic(reset=true) 排空 delta。
- 流量重置：ResetUserTotalTraffic(username) 按 sc.mu → m.mu 锁序（与 collect+onCollect 一致）清零 user.TrafficTotal*、sc.drainForUserLocked 对齐 prevCounters 基线、delete lastSeenUserTotal、store.Save。

**依赖**：
- 导入: pkg/log, pkg/proxy/core/contracts, pkg/proxy/errors, pkg/proxy/forward, pkg/proxy/usermanager/sync (usync), pkg/store, pkg/rpc/proto (仅 bandwidth_stats_collector.go), github.com/google/uuid
- 被导入: cmd/server.go; pkg/proxy/containers/{xray,hysteria,snell,mihomo}; pkg/proxy/core/container (factory/interface); pkg/proxy/core/subscription/manager.go; pkg/rpc/server/{end_node_server,end_node_user,end_node_cluster}.go

**并发模型**：三把锁 + 两类 goroutine + 事件 channel。(1) m.mu (sync.RWMutex)：保护 users map、lastSeenUserTotal、cachedNodeGroups；mutateUser/GetBindPort/SyncUpsertUser 等写路径持写锁，List*/Get* 持读锁。(2) m.eventChMutex：独立保护 eventCh(legacy, buffered 100)、subscribers 切片和 closed 标志——emitEvent 用它而非 m.mu 以避免持 m.mu 时死锁；所有发送均为非阻塞 select，channel 满即丢事件。(3) sc.mu (statsCollector)：保护 stats/prevCounters/running/stopCh。约定锁序 sc.mu → m.mu：collect() 持 sc.mu 同步调 onCollect（内部再取 m.mu），ResetUserTotalTraffic 也按 sc.mu → m.mu 取锁以与之匹配。goroutine：statsCollector.collectLoop（Start 时启动，stopCh 由 Start 捕获传入避免与 Stop 竞态）；StartMaintenance 的匿名 ticker goroutine（无停止机制）；ForceCollect 每次 spawn 一个 go sc.collect()。applyRuntimeSideEffects 依赖 forward 层独立互斥且不回调 UserManager，因而可在持 m.mu 时调用。SetUserBandwidthLimit/SetUserClientLimit 在 mutateUser 释放锁后重新 RLock 快照字段再下发，避免无锁读 *User。

**外部交互**：
- SQLite（经 pkg/store.UserStore.Save/Load）：mutateUser、AddUser、GetBindPort、SyncUpsertUser、onCollect 每采集周期逐用户 Save、ResetUserTotalTraffic 等均落盘
- forward.ForwardManager（进程内 TCP/UDP relay 层，实际监听本机端口）：AddRule/RemoveRule/RemoveRulesByUser/RemoveRulesByInbound/GetRule/GetRulesByUser/DropUser/SetUserBandwidthLimit/SetUserClientLimitConfig/GetAllTrafficRecords
- 无直接网络/exec；对 xray/hysteria 等外部二进制的交互全部经由订阅本模块事件的 container 层间接发生
- pkg/rpc/proto.Stats：DrainStats 输出给 gRPC 上报路径

**风险关注点（reviewer 重点）**：
- usermanager.go 的锁序约定 sc.mu → m.mu 仅靠注释维持（collect+onCollect vs ResetUserTotalTraffic）；onCollect 在持 sc.mu 下逐用户做 SQLite Save，1s tick 下会串行化所有 sc.mu 读者（文件内 TODO(perf) 已自述），reviewer 应确认没有第三条路径以相反顺序取这两把锁
- RotateUserPortForInbound (usermanager.go:999)：多步无事务的 make-before-break（临时 tag → 删旧 → 删临时 → 重建正式 rule），Step 3-4 之间临时端口释放窗口可被外部进程抢占，且整个流程不持 m.mu，与并发 GetBindPort/RemoveUser 交错时的状态一致性值得细看；Sscanf 解析 TargetAddr 也硬编码了 127.0.0.1 前缀
- SyncUpsertUser (usermanager.go:1488)：三分支各自维护采纳字段列表（新增 User 字段容易漏同步）、token 冲突时 re-stamp 会改变版本、'not newer 但采纳 LoginPassword' 的特例分支——集群一致性核心，且不走 mutateUser 统一路径
- emitEvent 非阻塞发送、channel 满即静默丢事件（usermanager.go:436）：容器侧依赖 UserEventRemove/PortBind 做端口清理，丢事件意味着泄漏 relay/inbound；订阅者消费速度是隐式契约
- GetUser/ListUsers 等返回 *contracts.User 指针而非拷贝，调用方在锁外读字段与 mutateUser 并发写存在数据竞态面（applyRuntimeSideEffects 的注释已明确这一坑）
- ReleaseBindPort (usermanager.go:1311) 释放单个端口却调用 forwardMgr.RemoveRulesByUser 移除该用户全部规则，与函数名/参数语义不对称，多 inbound 用户场景值得核对
- sync/hash.go ComputeHash 的 LoginPassword 仅非空才写入哈希以兼容旧节点——字段加减必须保持线上兼容，reviewer 检查任何 contracts.User 字段变更是否同步更新了 hash 与 SyncUpsertUser 的采纳列表
- StartMaintenance goroutine 无 stop channel，StartTrafficStats 可重复调用会重复注册 onCollect（statsCollector 为 nil 时才新建，但 onCollect 每次覆盖）——生命周期管理集中在 cmd/server.go 单次调用的假设上


## [L2] XAPI — pkg/xrayapi (grpc/builder/hotreload/hotupdate/reality/types)

**职责**：Provides the xray gRPC API surface without depending on xray-core: locally-generated protobuf types (internalproto/gen), JSON→protobuf conversion of inbound configs (types.ParseInboundConfig), pure-proto inbound builders (grpc/builder), a minimal raw gRPC client (grpc), Reality X25519 key generation/persistence (reality), and two overlapping hot-reload/hot-update orchestrators (hotreload, hotupdate) that start a second xray instance with port-offset config, switch forward rules, then stop the old process.


**文件角色**：

- `pkg/xrayapi/grpc/client.go` — Minimal gRPC Client (default 127.0.0.1:62789) with AddInboundWithProto/RemoveInbound; sends raw JSON bytes via conn.Invoke to xray HandlerService, no proto codec
- `pkg/xrayapi/grpc/builder/builder.go` — InboundBuilder: BuildVMess/BuildVMessWS/BuildVMessTCPWithTLS/BuildVLESSReality/BuildVLESSWSTLS/BuildTrojanTCPWithTLS/BuildShadowsocks producing types.InboundHandlerConfig; buildTLSSettings writes hard-coded placeholder cert/key to /tmp/xray-cert.pem//tmp/xray-key.pem; streamSettings maps are never wired into proto ReceiverConfig (TODO in buildReceiverSettings); many legacy BuildXxxNoTLS methods that only return errors
- `pkg/xrayapi/grpc/builder/builder_test.go` — Tests: covers each Build* variant, error rejections (no uuid/password/TLS) and proto serialization round-trip
- `pkg/xrayapi/hotreload/manager.go` — Manager (Config, sync.RWMutex): GetCurrentInbounds/PrepareNewConfig/GetPortMapping/SwitchForwardRules/HealthCheck/ExecuteHotReload/ExecuteHotUpdate/RollbackUpdate — file-based config merge with PortOffset, spawns second xray.Executor, rewrites forward rules by TargetAddr match
- `pkg/xrayapi/hotupdate/update.go` — Package-level functional twin of hotreload: Execute/Rollback plus prepareConfig/getInboundPorts/createNewExecutor/switchForwardRules/getVersion — same 7-stage flow, near-duplicate logic without the Manager/mutex
- `pkg/xrayapi/reality/keys.go` — GenerateKeyPair (curve25519 with RFC7748 clamping, base64 StdEncoding output) and KeyStore (JSON file, LoadOrGenerate/GetPublicKey/DeleteKey/ListTags, saveKeys 0600)
- `pkg/xrayapi/types/types.go` — Aliases InboundHandlerConfig/TypedMessage to generated pb types; NewTypedMessage/NewTypedMessageFromProto/NewTypedMessageRaw; ParseInboundConfig JSON→proto pipeline (buildReceiverConfigProto, buildProxySettingsProto for vmess/vless/trojan/ss2022/socks/http, buildStreamConfigProto for tls/xtls/reality + ws/grpc/h2/tcp/httpupgrade/splithttp); hand-rolled protobuf wire encoding for splithttp (encodeSplitHTTPAsProto/encodeVarint); FlexPort accepting string-or-number; NewAddUserOperation/NewRemoveUserOperation (JSON-encoded operation payloads)
- `pkg/xrayapi/types/types_test.go` — Tests: TypedMessage proto-not-JSON invariants, ParseInboundConfig for every protocol, Reality (default/explicit type, invalid hex shortId, key length), splithttp wire encoding incl. headers, XTLS→TLS mapping, network name normalization, protocol dispatch
- `pkg/xrayapi/internalproto/gen/**` — 生成代码 (protoc-gen-go output mirroring xray-core proto packages: proxyman, serial, net, protocol, proxies, transports) — skipped

**关键流程**：
- JSON inbound → gRPC add: types.ParseInboundConfig(jsonData) → buildReceiverConfigProto (port range, listen IP/domain, buildStreamConfigProto, buildSniffingConfigProtoDirect) + buildProxySettingsProto (per-protocol builders wrapping accounts in serial.TypedMessage) → *proxyman.InboundHandlerConfig, then consumed by pkg/proxy/containers/xray/grpc_client.go for the real gRPC call.
- Programmatic build path: builder.NewInboundBuilder().BuildVLESSReality/BuildVMess/... → types.NewTypedMessageFromProto for proxy settings + buildReceiverSettings (proto ReceiverConfig, stream settings NOT applied) → InboundHandlerConfig → grpc.Client.AddInboundWithProto which JSON-marshals it and conn.Invoke('s HandlerService/AddInbound with raw []byte codec.
- Hot-reload lifecycle: hotreload.Manager.ExecuteHotReload → PrepareNewConfig (read old JSON, applyPortOffset, adapter.ToProvider for new InboundSpecs, write NewConfigPath) → GetPortMapping → CreateNewExecutor (xray.NewExecutor+Init, gRPC on 62789+PortOffset) → Start → HealthCheck (500ms sleep + IsRunning only; gRPC check disabled) → SwitchForwardRules (RemoveRule/AddRule by TargetAddr '127.0.0.1:<port>') → oldExecutor.Stop.
- Hot-update (binary swap) via Manager: hotreload.Manager.ExecuteHotUpdate — stages prepare (prepareForUpdate copies current inbounds to new config) → start-new (createUpdateExecutor with new binary) → health-check → switch-forward → drain-old (500ms sleep) → stop-old; on switch-forward failure stops new executor and sets RollbackPerformed.
- Hot-update, package-level variant: hotupdate.Execute(ctx, cfg, forwardMgr, oldExecutor) — same 7 stages implemented independently (prepareConfig copies old config verbatim, getInboundPorts skips 'api' tag, mappings = oldPort+PortOffset); hotupdate.Rollback / hotreload.Manager.RollbackUpdate reverse the forward-rule swap and restart the old executor.
- Reality key lifecycle: reality.KeyStore.LoadOrGenerate(tag) → loadKeys (JSON file, missing = empty map) → GenerateKeyPair (crypto/rand + curve25519.ScalarBaseMult) → saveKeys (MkdirAll + WriteFile 0600; save failure only prints a warning and still returns the key).

**依赖**：
- imports (repo-internal): pkg/xrayapi/internalproto/gen/* (generated, used by types and builder), pkg/xrayapi/types (used by grpc and builder), pkg/proxy/containers/xray (Executor, Adapter — used by hotreload/hotupdate), pkg/proxy/core/container, pkg/proxy/core/contracts (InboundSpec), pkg/proxy/forward (DefaultForwardManager/ForwardRule)
- imported by: pkg/proxy/containers/xray/grpc_client.go (types + many internalproto/gen packages), cmd/test_hotreload/main.go
- third-party: google.golang.org/grpc, google.golang.org/protobuf/proto, golang.org/x/crypto/curve25519

**并发模型**：Nearly none. Only hotreload.Manager holds a sync.RWMutex (mu) guarding currentConfig; GetCurrentInbounds takes RLock but nothing in the package ever writes currentConfig or takes the write lock, so the cache stays nil and reads always go to disk. No goroutines or channels anywhere in the module; hotreload/hotupdate use time.Sleep(500ms) for startup wait and connection draining. grpc.Client is stateless (a new grpc.Dial per call).

**外部交互**：
- Network: grpc.Dial (insecure) to xray gRPC API at configurable address, default 127.0.0.1:62789; hot-reload spawns a second instance on 62789+PortOffset
- Disk: os.ReadFile/WriteFile of xray JSON configs (hotreload/hotupdate, 0644), Reality key JSON store (0600), TLS cert files read in types.buildStreamConfigProto, hard-coded placeholder cert/key written to /tmp/xray-cert.pem and /tmp/xray-key.pem by builder.generatePlaceholderCert
- Exec: exec.Command(binaryPath, "version") in hotreload.oldVersion and hotupdate.getVersion; xray process lifecycle indirectly via pkg/proxy/containers/xray Executor Start/Stop
- Crypto: crypto/rand + curve25519 for Reality keys

**风险关注点（reviewer 重点）**：
- hotreload/manager.go vs hotupdate/update.go are near-duplicate implementations of the same 7-stage flow (ExecuteHotUpdate vs Execute, RollbackUpdate vs Rollback, switchForwardRules duplicated); divergence risk — reviewers should check which one is actually called from production code (cmd/test_hotreload is the only caller found).
- grpc/client.go conn.Invoke passes JSON []byte where grpc-go expects proto messages with a codec — the whole raw-invoke path deserves scrutiny; note pkg/proxy/containers/xray/grpc_client.go is the real client using command.pb.go stubs.
- builder.go buildTLSSettings writes a hard-coded, syntactically bogus placeholder certificate to fixed /tmp paths (world-shared, TOCTOU on os.Stat) and sets allowInsecure:true; buildReceiverSettings has a TODO — streamSettings maps built by buildWebSocketTLSSettings/buildRealitySettings are silently dropped from the proto ReceiverConfig, so WS/TLS/Reality builder variants produce plain-TCP receivers.
- hotreload/manager.go HealthCheck is only sleep(500ms)+IsRunning (gRPC check disabled per xray 26.x comment); port switch depends on string-matching rule.TargetAddr == '127.0.0.1:<port>', fragile to any other target format.
- hotreload/manager.go buildConfig drops Outbounds/Routing of the original config (xrayConfig fields exist but are never copied), and hardcodes API port 62789 without PortOffset in the generated inbound list.
- types.go: encodeSplitHTTPAsProto hand-rolls protobuf wire format (map iteration order nondeterministic); buildReceiverConfigProto uses fmt.Sscanf on port so ranges like '1000-2000' silently become a single port; builder.buildReceiverSettings puts []byte(listen) (ASCII string, not parsed IP) into IPOrDomain_Ip, unlike types.buildReceiverConfigProto which parses with stdnet.ParseIP.
- types.go NewAddUserOperation/NewRemoveUserOperation JSON-encode the operation Value inside a TypedMessage whose Type names a protobuf message — inconsistent with the proto-everywhere invariant the tests assert elsewhere.
- reality/keys.go LoadOrGenerate swallows both loadKeys errors (treated as empty store, could regenerate keys over an unreadable existing file) and saveKeys errors (printf warning only); KeyStore has no file locking against concurrent writers.


## [L2] CERT — pkg/certmgmt (domain/lego/service)

**职责**：ACME 证书管理模块，基于 go-acme/lego 实现证书的签发、续期、吊销、导入与本地文件持久化。分三层：domain 定义纯类型/接口/哨兵错误；lego 实现 Issuer、账号与证书的磁盘存储、HTTP-01/DNS-01 challenge provider；service.Manager 是对外门面，负责配置解析、每域名互斥、自动续期调度，并通过 rpc_adapter 实现 pkg/rpc/server.CertManager 与 pkg/http.CertReader 接口，供 server 主流程和各代理容器（xray/hysteria/mihomo）取证书文件路径。


**文件角色**：

- `pkg/certmgmt/domain/types.go` — 核心类型：IssueRequest/CertificateRecord/AccountData/LegoResource、ChallengeConfig，以及 Issuer 与 Store 接口定义
- `pkg/certmgmt/domain/errors.go` — 10 个哨兵错误（ErrConfigInvalid、ErrStorageIO、ErrCertResourceMissing 等），供 errors.Is 分类
- `pkg/certmgmt/domain/errors_test.go` — 测试：所有哨兵错误经 fmt.Errorf %w 包装后 errors.Is 仍匹配
- `pkg/certmgmt/lego/account_store.go` — ACME 账号磁盘存储：SaveAccount/LoadAccount/GetOrCreateAccountKey，目录布局 <base>/accounts/<ca-host>/<email>/，兼容 lego 官方布局
- `pkg/certmgmt/lego/cert_store.go` — 证书磁盘存储：SaveCert（atomicWrite 临时文件+rename 写 .crt/.key/.json/.meta.json）、LoadCert、DeleteCert、ListCerts（扫描 .meta.json）
- `pkg/certmgmt/lego/client_factory.go` — NewClient：由 IssueRequest+私钥+registration 构造 lego.Client；acmeUser 实现 registration.User；keyTypeFromString 映射密钥类型
- `pkg/certmgmt/lego/issuer.go` — LegoIssuer 实现 domain.Issuer：Issue/Renew/Revoke 全流程编排（账号注册、challenge 设置、obtain/renew、持久化）；renewBeforeDuration 续期窗口解析；ParseCertNotAfter 导出给 service 用
- `pkg/certmgmt/lego/solver_dns.go` — WithDNSCredentials：全局 dnsGlobalMu 互斥下临时设置/恢复 DNS 凭证环境变量，并强制 LEGO_DISABLE_CNAME_SUPPORT=true
- `pkg/certmgmt/lego/solver_dns_full.go` — //go:build full_dns：NewDNS01Provider 委托 lego 全量 provider 注册表（50+ providers）
- `pkg/certmgmt/lego/solver_dns_slim.go` — //go:build !full_dns：slim 构建，仅内置 7 个常用 DNS provider（alidns/cloudflare/dnspod/route53/tencentcloud/namecheap/godaddy）
- `pkg/certmgmt/lego/solver_http.go` — NewHTTP01Provider：server（内置 HTTP server，支持 ProxyHeader）/webroot/memcached 三种模式；splitListenAddr 解析监听地址
- `pkg/certmgmt/lego/issuer_test.go` — 测试：renewBeforeDuration 优先级（RenewBefore > RenewBeforeDays > 30d 默认，含 >30d 不被截断的回归）
- `pkg/certmgmt/lego/solver_dns_test.go` — 测试：WithDNSCredentials 设置/恢复环境变量语义（含出错路径）
- `pkg/certmgmt/lego/solver_http_test.go` — 测试：NewHTTP01Provider 各模式构造与非法模式报错
- `pkg/certmgmt/service/manager.go` — Manager 门面：Config（yaml/json）、renewBeforeDuration（Hours > Days*24 > 24h 默认）、per-domain sync.Map 锁、Issue/RenewDomain/GetCert/ListCerts、buildIssueRequest 配置到 IssueRequest 转换
- `pkg/certmgmt/service/renew_scheduler.go` — StartAutoRenew 启动后台 goroutine，每 renewCheckInterval(1min，可被 cycleSeconds 覆盖) 跑 runRenewCycle：遍历 ListCerts、跳过 imported、逐域调 RenewDomain
- `pkg/certmgmt/service/rpc_adapter.go` — 适配层：GetCertFiles(pkg/http.CertReader)、ObtainNewCert/AddCertificates(导入外部证书)/DeleteCert/GetCertInfo/GetAllCert(pkg/rpc/server.CertManager，返回 proto.Cert)
- `pkg/certmgmt/service/manager_test.go` — 测试：mockIssuer 验证同域名 Issue/Renew 绝不并发（per-domain 锁）
- `pkg/certmgmt/service/renew_scheduler_test.go` — 测试：直接写 .meta.json 后跑 RunRenewCycleForTest，验证到期判断、imported 跳过等
- `pkg/certmgmt/service/renew_window_test.go` — 测试：Hours/Days 优先级与 24h 默认经 RunRenewCycle 端到端生效

**关键流程**：
- 签发：Manager.Issue(ctx, domains) → domainLock 加锁 → buildIssueRequest → LegoIssuer.Issue → validateIssueRequest → GetOrCreateAccountKey/LoadAccount → 未注册则 NewClient+Registration.Register+SaveAccount（保存错误被 _ 忽略）→ NewClient → DNS01 走 WithDNSCredentials{NewDNS01Provider+SetDNS01Provider+obtainCert}，否则 setupHTTPChallenge+obtainCert → saveCertResult(parseCertNotAfter+SaveCert)
- 自动续期：Manager.StartAutoRenew(ctx, cycleSeconds) 起 goroutine 循环 → runRenewCycle → ListCerts → 跳过 ObtainedBy==imported → RenewDomain(逐域，per-domain 锁) → LoadCert 检查 time.Until(NotAfter) > renewBeforeDuration 则跳过 → LegoIssuer.Renew（自己再做一次窗口判断 renewBeforeDuration(req)）→ LoadCert 取 LegoResource → 重建 certificate.Resource → client.Certificate.Renew → SaveCert 原路径覆盖（代理核心靠文件热加载感知）
- 吊销：LegoIssuer.Revoke(ctx, record) → LoadCert → 用证书自身私钥（解析失败则临时生成 EC256 密钥）→ NewClient(强制 LEDirectoryProduction) → client.Certificate.Revoke → os.Remove 四个本地文件
- 外部证书导入：Manager.AddCertificates(domain, keyData, certData)（rpc_adapter）→ ParseCertNotAfter → 构造 ObtainedBy=imported 的 record + LegoResource → SaveCert 写入与 ACME 证书相同的 {Path}/certificates/ 布局
- 取证书路径：Manager.GetCertFiles(domain)（实现 pkg/http.CertReader）→ GetCert → certmgmtlego.LoadCert 读 .meta.json → 返回 CertFile/KeyFile；GetAllCert 走 ListCerts 转 []*proto.Cert
- RPC 触发签发：Manager.ObtainNewCert(d)（实现 pkg/rpc/server.CertManager）→ Issue(context.Background(), [d])

**依赖**：
- 导入仓库内包: pkg/certmgmt/domain（被 lego、service 导入）、pkg/certmgmt/lego（被 service 导入）、pkg/log（service 的 manager.go/renew_scheduler.go）、pkg/rpc/proto（service/rpc_adapter.go）
- 被导入方（grep 结果）: cmd/server.go、pkg/rpc/server/{cert_manager,end_node_inbound}.go、pkg/proxy/containers/{xray/cert_helper,hysteria/register,mihomo/register,mihomo/container}.go、pkg/proxy/core/container/factory.go、pkg/proxy/core/params/defaults.go、pkg/proxy/appconfig/config.go
- 第三方: github.com/go-acme/lego/v4（certcrypto/certificate/lego/registration/challenge/dns01/http01/providers）

**并发模型**：两处锁 + 一个后台 goroutine。(1) service/manager.go: Manager.domainMu 是 sync.Map[domain]*sync.Mutex（domainLock 用 LoadOrStore 创建），Issue 和 RenewDomain 按 domains[0]/domain 加锁，保证同一域名的签发/续期串行；锁条目只增不删。(2) lego/solver_dns.go: 包级 dnsGlobalMu sync.Mutex，WithDNSCredentials 在整个 provider 构造+obtain/renew 期间持有，保护进程级环境变量（DNS 凭证 + LEGO_DISABLE_CNAME_SUPPORT）的设置/恢复——即所有 DNS-01 操作全进程串行。(3) service/renew_scheduler.go: StartAutoRenew 起一个匿名 goroutine，循环 runRenewCycle + time.After(interval)，通过 ctx.Done() 两处 select 退出；runRenewCycle 内每域也检查 ctx.Err()。

**外部交互**：
- 网络: ACME 出站 HTTPS（lego client 的 Register/Obtain/Renew/Revoke，默认 Let's Encrypt production）；DNS provider API 出站调用（slim 7 家 / full 全量）；memcached 模式向 memcached 写 token
- 入站网络: HTTP-01 server 模式在 ListenAddr（默认 :80）起临时 HTTP server（http01.NewProviderServer）
- 磁盘: {Path}/accounts/<ca-host>/<email>/{account.json,keys/<email>.key}（0600/0700），{Path}/certificates/{domain}.crt/.key/.json/.meta.json（cert_store.go atomicWrite 临时文件+rename）；webroot 模式向 WebRoot 目录写 token 文件
- 进程环境: WithDNSCredentials 通过 os.Setenv/恢复 修改进程级环境变量
- 无 exec、无 DB

**风险关注点（reviewer 重点）**：
- lego/solver_dns.go WithDNSCredentials: 用进程级 os.Setenv 传凭证，dnsGlobalMu 只能挡住本包内并发；进程内任何其它读同名 env 的代码在窗口期会看到凭证值，且恢复用 map 迭代（含出错路径的部分恢复逻辑）——reviewer 应确认所有 DNS 路径都经过该函数、无绕过
- lego/issuer.go Issue: 注册成功后 SaveAccount 错误被 `_ =` 丢弃——账号 KID 可能未持久化，下次运行会重复注册；Revoke 中签名密钥回退逻辑（证书私钥解析失败→临时 EC256 密钥）与注释自述的 'in production should use stored account key' 是重点审查区
- 续期窗口双重实现: service/manager.go renewBeforeDuration（Hours>Days>24h 默认）与 lego/issuer.go renewBeforeDuration（RenewBefore>Days>30d 默认）两套 gate 串联，默认值不同（24h vs 30d），靠 buildIssueRequest 填 RenewBefore 对齐——任何一侧改动都可能重新引入 '窗口被截断' 回归（renew_window_test.go 有覆盖）
- service/manager.go domainMu: Issue 只锁 domains[0]，多域名证书的其它 SAN 域不加锁；sync.Map 锁条目永不回收
- lego/cert_store.go: atomicWrite 的 tmp 文件固定为 path+".tmp"（同域并发写会互踩，目前靠上层 per-domain 锁保证）；SaveCert 四个文件非事务性写入，中途失败会留下不一致状态；LoadCert 对 meta 存在但 resource 缺失返回 (record,nil,nil)，调用方须区分
- lego/issuer.go Renew: 保存新记录时 ObtainedBy 沿用旧值、Domain 用 record.Domain（Issue 用 req.Domains[0]），两条路径构造 record 的字段来源不一致，review 时留意 CertFile/KeyFile 由 SaveCert 回填的耦合
- 构建标签双实现: solver_dns_full.go(full_dns)/solver_dns_slim.go(!full_dns) 的 NewDNS01Provider 选项组装逻辑重复，改一处易漏另一处


## [L3] XC — containers/xray

**职责**：xray-core 外部进程的容器实现：负责 xray 二进制的下载/更新/启停、默认配置生成、通过 gRPC API 动态增删 inbound 和查询流量统计。维护 in-memory inbound 表并持久化到 store，重启后 Restore 可重建（含证书重生成）。集成 usermanager：订阅用户事件 + 周期 reconcile，将用户映射为 forward 端口（不走 xray 的多用户机制），并为每个用户生成各协议订阅 URI。通过 init() 注册到 container 工厂，供 center/end 服务按类型创建。


**文件角色**：

- `pkg/proxy/containers/xray/exec_runner.go` — 核心：Executor（容器实现，2600 行）+ XrayInbound（inbound.Inbound 实现）+ FastAddInbound/Restore/用户事件与 reconcile 循环/证书 temp 文件处理/killProcessOnPort
- `pkg/proxy/containers/xray/register.go` — init() 注册 xrayFactory 到 container.RegisterFactory；ExecutorConfig.Decode 解析 map 配置（含 ~ 展开与默认值）
- `pkg/proxy/containers/xray/grpc_client.go` — XrayAPI：每次操作新建 gRPC 连接的客户端（AddInbound 带重试、ListInbounds、RemoveInbound、AddUser/RemoveUser via AlterInbound、QueryStats、HTTPStats 回退）+ 大量 debug dump/TLS 有效性断言
- `pkg/proxy/containers/xray/inbound_adapter.go` — Adapter：InboundSpec ↔ xray native JSON 双向映射（ToProvider/FromProvider/buildStreamSettings/MapUsers）+ 自签证书生成 GenerateSelfSignedCert + Reality 密钥（curve25519 派生、RealityKeyStore 文件持久化、shortId 校验）
- `pkg/proxy/containers/xray/config_renderer.go` — Renderer：ContainerModel → 完整 xray config.json（api/policy/stats/routing/dokodemo API inbound）及反向解析 FromProvider（带 UnmappedWarnings）
- `pkg/proxy/containers/xray/subscription.go` — GetUserSubscriptions 入口 + 各协议分享链接生成（generateVLESSURI/generateVMessURI/generateTrojanURI/generateShadowsocksURI SIP002/generateSOCKS5URI），对齐 Xray-core issue #91 语义
- `pkg/proxy/containers/xray/updater.go` — Updater：GitHub release fetch → 下载 → checksum 校验 → zip 解包 → 原子换二进制 → 重启（失败回滚），依赖 pkg/proxy/tools 的接口抽象
- `pkg/proxy/containers/xray/cert_helper.go` — CertManagerGetter/CertIssuer 接口定义 + AddInboundWithCert（查证书或签发后调 FastAddInbound）
- `pkg/proxy/containers/xray/inbound_store.go` — 对 pkg/store 类型的向后兼容别名（InboundRecord/InboundStore/SQLiteInboundStore）
- `pkg/proxy/containers/xray/native_types.go` — NativeInbound/NativeConfig（JSON 字节包装）与 UnmappedWarning 类型
- `pkg/proxy/containers/xray/profilegen/vless_profiles.go` — GenerateVLessInboundSpec：VLESS 各 transport/security（含 Reality、XHTTP、sniffing、TLS 高级项）的 InboundSpec 生成
- `pkg/proxy/containers/xray/profilegen/vmess_tls.go` — GenerateVMessTLSInboundSpec：VMess+TLS spec 生成
- `pkg/proxy/containers/xray/profilegen/trojan_tls.go` — GenerateTrojanTLSInboundSpec：Trojan+TLS spec 生成（密码可自动生成）
- `pkg/proxy/containers/xray/profilegen/shadowsocks.go` — GenerateShadowsocksInboundSpec：SS spec 生成，2022 系列密钥长度校验/自动生成
- `pkg/proxy/containers/xray/*_test.go + profilegen/*_test.go` — 测试覆盖：exec_runner_startup_test（Start 顺序/EnsureBinary/EnsureConfig）、fast_add_inbound_test（FastAddInbound 各协议/证书路径）、subscription_test（1868 行，各协议 URI 字段级断言含 Reality/SIP002/insecure）、inbound_adapter_test（ToProvider/FromProvider 往返）、config_renderer_test、updater_test（mock ReleaseClient/swapper 回滚）、inbound_store_test、testmain_test（共享测试基建）、profilegen 各文件参数校验/密钥长度/随机端口测试

**关键流程**：
- 启动生命周期：register.go init() 注册工厂 → xrayFactory.New 注入 UserManager/CertManager/StoreMgr → NewExecutor（建 process.Runner、订阅用户事件 goroutine forwardUserEvents、可选建 Updater）→ Executor.Start()：校验 userMgr → killProcessOnPort 清残留进程 → startUserEventHandler + startReconcileLoop → EnsureBinary（缺失时 updater.Update 自动下载）→ EnsureConfig（generateDefaultConfig 写临时 config.json）→ Runner.Start() + 500ms sleep。
- 动态加 inbound：Executor.FastAddInbound(tag, params) → 协议/端口/安全校验 → resolveFastAddCert（file/PEM temp/certManager domain/self-signed 四路）→ profilegen.GenerateXxxInboundSpec 或 buildFastAddSimpleSpec → Adapter.ToProvider 转 native JSON → XrayAPI.AddInbound gRPC（失败清 temp cert）→ 持久化 InboundStore.Save → 写 e.inbounds → reconcileUsersForInbound。
- 重启恢复：Executor.Restore(ctx) → InboundStore.Load → 按 cert_source 分支（self_signed 重新生成证书、domain 查 certManager、pem 从 native JSON 提取并检查过期）→ updateTLSCertPaths 改写 JSON → AddInboundNative → 重存原 record 保 cert_source → syncInboundsFromXray（gRPC ListInbounds 补注册静态配置 inbound）→ reconcileUsers。
- 用户同步：UserManager.Subscribe → forwardUserEvents goroutine 转发到 userEventCh（满则丢）→ handleUserEvent：Add/Update 走 syncUserToInbound → 每个 XrayInbound.AddUser → userMgr.GetBindPort 分配 forward 端口（不调 xray gRPC）→ markAddedUser；Remove 走 removeUserFromInbounds → RemoveUser → GetUserPortByDstForCleanup + ReleaseBindPort。
- 周期 reconcile：startReconcileLoop 每 30s（可配）调 reconcileUsers：对每个 inbound，listAddedUsers 中不再可见的用户 RemoveUser，可见但未 tracked 的 AddUser。
- 订阅生成：Executor.GetUserSubscriptions(req)（持 inboundsMu.RLock 全程）→ 每个 XrayInbound.GetSub → GetUserPort 查 forward 端口 → getCredentialForUser（VMess/VLESS 用 defaultClientUUID，Trojan/SS 用 defaultPassword，SOCKS5 用 AuthToken）→ buildSubscriptionExtensions → generateURI 按协议产出分享链接。
- 二进制更新：Executor.Update → Updater.Update：FetchRelease → DownloadToFile → VerifyChecksum（可选）→ extractBinary(zip) → SwapAtomic → 按 RestartPolicy Stop/Start（失败 Rollback）。
- 删除 inbound：RemoveInboundConfig(tag)：读 inbound → ReleaseAllUserPorts（锁外）→ InboundStore.Delete → gRPC RemoveInbound（失败仅告警）→ 二次加锁删 map + 清 tempCertFiles。RemoveInboundNative 类似但语义略不同（gRPC 失败也删本地）。

**依赖**：
- 导入仓库内: pkg/log
- pkg/proxy/core/container
- pkg/proxy/core/contracts
- pkg/proxy/core/inbound
- pkg/proxy/errors
- pkg/proxy/tools (GitHub release/downloader/swapper)
- pkg/proxy/tools/process (Runner)
- pkg/proxy/usermanager
- pkg/store (StoreManager/InboundStore)
- pkg/certmgmt/domain
- pkg/xrayapi/types + pkg/xrayapi/internalproto/gen/** (生成代码，proto 类型)
- 子包 profilegen 仅依赖 core/contracts
- 被导入方: cmd/server.go, cmd/test_hotreload, pkg/xrayapi/hotreload, pkg/xrayapi/hotupdate, pkg/proxy/systemtest/* (多个系统测试)

**并发模型**：Executor.inboundsMu (RWMutex) 保护 e.inbounds map；多处两阶段加锁模式（RemoveInboundConfig/RemoveInboundNative/FastAddInbound：读锁取引用→锁外做 gRPC/端口释放→再加锁删改），reconcileUsersForInbound 刻意不持锁由调用方保证顺序。XrayInbound.addedUsersMu (Mutex) 保护 addedUsers 集合，所有访问必须走 hasAddedUser/listAddedUsers/markAddedUser/unmarkAddedUser。Goroutine 有三个：forwardUserEvents（NewExecutor 起，UserManager.Subscribe → userEventCh 缓冲 100，满则静默丢事件）、startUserEventHandler（Start 起，串行消费 userEventCh）、startReconcileLoop（ticker + reconcileStopCh + reconcileWg，Stop 时 close+Wait）。GetUserSubscriptions 全程持 RLock 遍历并生成 URI。XrayInbound 的非 addedUsers 字段（forwardPort/userEmail/tempCertFiles 等 setter）无锁。

**外部交互**：
- exec: 启动 xray 二进制 (process.Runner, args=["run"])；exec.Command(binary, "version") 探测二进制存在/版本
- 网络 gRPC: XrayAPI 对 xray 进程 127.0.0.1:port 的 HandlerService（AddInbound/RemoveInbound/AlterInbound/ListInbounds）与 stats QueryStats（raw conn.Invoke JSON 编解码）；每次调用新建连接
- 网络 HTTP: HTTPStats 回退查询；Updater 经 pkg/proxy/tools 访问 GitHub release API 并下载 zip
- 磁盘: 写临时 config.json、cert/key temp 文件 (os.CreateTemp xray-fastadd-*)、zip 下载/解包/二进制原子替换、RealityKeyStore 密钥文件、读 /proc/net/tcp 与 /proc/*/fd 定位并 SIGKILL 占端口的残留进程
- DB: 经 store.StoreManager.InboundStore()（SQLite）持久化 InboundRecord
- 第三方二进制: xray-core 本体（自动从 XTLS/Xray-core release 下载 Xray-linux-64.zip）

**风险关注点（reviewer 重点）**：
- exec_runner.go: Restore 的 cert_source 四分支 + AddInboundNative 会以 cert_source=none 覆盖存储、随后再 re-save 原 record 补救——状态修复链路长且交叉，是最脆弱区
- exec_runner.go: inboundsMu 两阶段锁模式在 RemoveInboundConfig/RemoveInboundNative/FastAddInbound 各不相同（gRPC 失败时本地清理策略不一致），并发 add/remove 同 tag 的交错行为值得细看
- exec_runner.go: forwardUserEvents 在 channel 满时静默丢用户事件（default 分支），只靠 30s reconcile 兜底；startUserEventHandler goroutine 无停止机制（依赖 channel close）
- exec_runner.go: killProcessOnPort 解析 /proc 并对匹配 inode 的进程直接 SIGKILL，误杀面与平台假设（仅 Linux、hex 端口、pid>1）需要审视
- exec_runner.go: Start 与 grpc_client.go AddInbound 都靠固定 sleep（500ms）等待 xray 就绪，AddInbound 的重试判定基于错误字符串匹配
- grpc_client.go: QueryStats 用 conn.Invoke 传 JSON 而非 protobuf codec（注释称 StatsService 未生成），编解码正确性依赖 xray 侧行为
- subscription.go: buildShareLinkParams 中 Reality SNI 用 rand.Intn 随机选取（非确定输出）；extensions 值 string/[]string 双形态由 extStringOrJoinSlice 兜底，历史不一致易再引入
- exec_runner.go AddInboundNative 与 FastAddInbound 各自复制了一份从 native JSON 提取 defaultClientUUID/defaultPassword 的逻辑，两处需同步维护
- inbound_adapter.go: ~900 行的 ToProvider/buildStreamSettings 大量手写 map 组装 + Reality 密钥文件存取（RealityKeyStore 无文件锁），是字段遗漏/类型断言错误的高发区
- register.go 与 exec_runner.go 底部各有一个 init()（RegisterFactory 与 RegisterContainerFunc 双注册机制并存），后者默认路径失败会 panic


## [L3] MC — containers/mihomo

**职责**：Container 实现，让 v2raymg 以受管子进程方式运行 mihomo(Alpha)，仅用其 inbound listener 能力（outbound 侧保持 MATCH,DIRECT 惰性）。采用"一个 listener 一份共享凭证"模型：所有用户共用 UUID/password，按用户隔离完全交给 forward 层（通过 usermanager 的 GetBindPort/ReleaseBindPort）。支持 7 种协议（vless/vmess/trojan/ss/hysteria2/tuic/anytls），配置通过 REST PUT /configs 热推送（name-keyed diff），并内置 GitHub 下载/校验/原子替换/重启回滚的自更新器。同时实现 GetUserSubscriptions，把 inbound + forward 端口投影成订阅 URI/Extensions。


**文件角色**：

- `container.go` — 核心 MihomoContainer：生命周期 hooks（run/stop）、inbound CRUD（FastAddInbound/RemoveInboundConfig）、持久化 + REST 推送原子性、用户事件转发/处理、30s reconcile 循环、Close/Restore/Update/WaitReady
- `process.go` — startProcess/stopProcess：二进制缺失时自动下载、写初始 config.yaml（0600）、spawn 进程（-d/-f + SAFE_PATHS env）、waitForVersion 指数退避就绪探测
- `config.go` — MihomoConfig（binary/config/data 路径、external_controller、secret、release_tag、auto_download）的 Decode 默认值与 validate
- `config_builder.go` — BuildConfig：base 配置 + 按 tag 排序的 inbound 集合 → 完整 mihomo yaml（确定性输出）
- `adapter.go` — FromParams/FromProtocolParams/fastAddBuildInbound：把 FastAddInbound 的 map[string]any 参数解析成 MihomoInbound；requirePort 多数值类型兼容；非 loopback listen 警告
- `inbound.go` — MihomoInbound：SharedCred（MVP 三协议 legacy）与 ProtocolParams（Phase 1+）双轨、7 个 validateXxx、ToNative/FromNative 存储序列化、AddUser/RemoveUser/ReleaseAllUserPorts（forward 端口分配）、addedUsers 跟踪、cleanupCertFiles
- `profilegen.go` — BuildListener + fillXxxListener（7 协议）：MihomoInbound → mihomo Alpha listeners[] yaml map，键名严格对齐 mihomo inbound struct tag；buildRealityConfig
- `subscription.go` — GetUserSubscriptions + buildSubscriptionSpec + fillXxxSubscriptionSpec（7 协议）：inbound + forward 端口 → SubscriptionSpec（codec URI + clash converter 用的 Extensions）；shouldSkipCertVerifyForClient 集中门控
- `rest_client.go` — RESTClient：mihomo external-controller HTTP 客户端（GET /version、GET/PUT/PATCH /configs、POST /configs/geo），Bearer 鉴权、错误体解析、响应体大小上限
- `updater.go` — Updater：fetch release → 选 linux/amd64-v1 asset → 下载 → SHA256 校验（有 checksums.txt 时）→ gunzip → SwapAtomic → 按策略重启 + WaitReady，失败回滚；ErrUpdateFailed 分阶段错误；4 个可注入 seam 接口
- `downloader.go` — legacy Alpha-only 下载 helper（downloadMihomoWith/gunzipFile），生产走 updater.go；gunzipFile 被 updater 复用
- `register.go` — init() 注册 mihomoFactory 到 container.RegisterFactory；从 BuildOptions 装配 StoreMgr/UserManager/certmgr SAFE_PATHS root
- `*_test.go（16 个文件）` — 测试：container_test 覆盖 CRUD 原子性/回滚/用户事件/reconcile/Close；updater_test 覆盖各阶段失败与回滚；rest_client_test 用 httptest 覆盖 REST 契约；profilegen_/subscription_ 各协议一个文件做 yaml 输出与订阅投影黄金对比；adapter/config/config_builder/downloader/inbound_test 覆盖参数解析、默认值、序列化往返；integration_test 需真实二进制（跳过标记）

**关键流程**：
- 启动: BaseContainer.Start → mihomoHooks.GetRunFunc run → startUserEventHandler + startReconcileLoop → startProcess（缺二进制时 updater.Update(RestartPolicyNever) 自动下载；写 config；process.Runner.Start；waitForVersion 就绪探测）→ restoreAndPushInbounds（InboundStore.Load → FromNative → PUT /configs）→ reconcileUsers；任一步失败则 stopProcess
- 新增 inbound: FastAddInbound → fastAddBuildInbound（protocolparams.Parse→FromProtocolParams 或 legacy FromParams）→ addInboundLocked（持 inboundsMu：persistInbound 存储 → map 写入 → pushConfigLocked REST 推送，失败则 map+store 双回滚）→ reconcileUsersForInbound（锁外为现有用户 AddUser 分配 forward 端口）
- 删除 inbound: RemoveInboundConfig → inb.ReleaseAllUserPorts（forward 规则先撤）→ pushConfigWithSnapshot（不含该 tag 的集合）→ store delete → map delete → 清理 v2raymg 自建证书文件（CertSource pem/self_signed）
- 用户事件: UserManager.Subscribe →（构造时启动的）forwardUserEvents goroutine 转入 100 缓冲 userEventCh（满则丢弃）→ handleUserEvent → syncUserToInbound/removeUserFromInbounds → MihomoInbound.AddUser/RemoveUser → userMgr.GetBindPort/ReleaseBindPort
- 周期收敛: startReconcileLoop 每 30s reconcileDrift → pushConfig（重推 map 作为真值，覆盖 mihomo 侧漂移）+ reconcileUsers（addedUsers vs userMgr.ListUsers 双向收敛，兜底丢失的 UserEvent）
- 更新二进制: Container.Update → Updater.Update（mu 串行）：FetchRelease → pickLinuxAmd64V1Asset → DownloadToFile → checksums.txt SHA256 校验 → gunzipToTemp → SwapAtomic → Stop+Start+WaitReady（失败则 restartFailure：Rollback + ErrUpdateFailed{stage:restart}）→ refreshCachedVersionAfterRestart
- 订阅生成: GetUserSubscriptions → snapshotInbounds → 逐 inbound userMgr.GetUserPortByDst（无映射跳过）→ buildSubscriptionSpec → fillXxxSubscriptionSpec（codec.XxxNode.Encode 生成 URI + Extensions 供 clash converter）
- 热重载: Reload → pushConfig → RESTClient.PutConfigs(force=false)，mihomo PatchInboundListeners 按 name diff，仅重建变更 listener（证书/凭证热更新路径）

**依赖**：
- 导入: pkg/log
- 导入: pkg/proxy/core/container（BaseContainer、Factory 注册、UpdateRequest/Result、ErrNotSupported）
- 导入: pkg/proxy/core/contracts（Protocol/User/SubscriptionSpec/ContainerMihomo）
- 导入: pkg/proxy/core/inbound（DefaultInbound）
- 导入: pkg/proxy/core/params/protocolparams（Parse、ProtocolParams/SecuritySpec/RealitySpec）
- 导入: pkg/proxy/core/subscription/codec（各协议 Node.Encode）
- 导入: pkg/proxy/tools（GitHubReleaseClient/Downloader/BinarySwapper/VerifyChecksum）
- 导入: pkg/proxy/tools/process（Runner）
- 导入: pkg/proxy/usermanager（UserManager、UserEvent、GetBindPort/ReleaseBindPort）
- 导入: pkg/store（StoreManager/InboundRecord）
- 第三方: gopkg.in/yaml.v3
- 被导入: cmd/server.go（blank import 触发 register.go 工厂注册）、pkg/proxy/systemtest/mihomo_helpers_test.go

**并发模型**：container.go 是并发中心。锁：(1) MihomoContainer.mu (RWMutex) 保护 runner/restClient/cachedVersion/userEventHandlerStarted，只在字段赋值处持有、不跨 I/O；(2) inboundsMu (Mutex) 保护 inbounds map 且刻意跨 REST 推送持有（addInboundLocked/RemoveInboundConfig/pushConfig），使"改 map + PUT /configs"原子；(3) MihomoInbound.addedUsersMu 保护每 inbound 的 addedUsers set；(4) Updater.mu 串行化 Update 调用（防 DownloadDir 文件名冲突）。goroutine：forwardUserEvents（构造时启动，UserManager 订阅 → 100 缓冲 userEventCh，满则丢弃事件，由 Close 关 forwardStopCh 终止）；user-event handler（startUserEventHandler，range userEventCh，mu+bool 保证幂等，仅随 Close 退出，Stop 不停它）；reconcile 循环（30s ticker，reconcileStopCh + reconcileWg 停止，Stop hook 调 stopReconcileLoop 后置 nil 供重启）。closeOnce 保证 Close 并发安全。snapshotInbounds 模式：锁内拷贝指针切片、锁外做慢调用（GetBindPort）。

**外部交互**：
- exec: 通过 pkg/proxy/tools/process.Runner 启动 mihomo 二进制（args: -d DataDir -f ConfigFilePath；env SAFE_PATHS）；stdout/stderr 经 log.NewPrefixWriter 转发
- 网络(本机): RESTClient → mihomo external-controller HTTP API（默认 127.0.0.1:9090，Bearer secret）：GET /version、GET/PUT/PATCH /configs、POST /configs/geo
- 网络(外网): updater.go / downloader.go → GitHub Releases API（MetaCubeX/mihomo）+ asset 下载 + checksums.txt
- 磁盘: 写 ConfigFilePath (0600)、DataDir MkdirAll、DownloadDir 中 .gz/临时解压文件、SwapAtomic/Rollback 对 BinaryPath 的原子替换（.bak）、RemoveInboundConfig 删自建证书 PEM 文件
- DB/存储: storeMgr.InboundStore() Save/Load/Delete（inbound 持久化记录 NativeJSON）

**风险关注点（reviewer 重点）**：
- container.go 锁/回滚编排最密集：addInboundLocked 的 store→map→REST 三步回滚（含 errors.Join 双失败路径）、RemoveInboundConfig 五阶段各自的失败语义（phase 3 失败留下 released-but-listener-live 窗口）、Restore 与并发 snapshotInbounds 的 stale-pointer 说明——reviewer 应逐条核对注释声称的幂等性是否真实成立
- container.go 用户事件链路的丢事件设计：forwardUserEvents 满则 default 丢弃，依赖 30s reconcile 兜底；startUserEventHandler/startReconcileLoop 的 Start-retry 幂等（BaseContainer 失败不调 stop hook）是隐性契约
- inbound.go AddUser 的 stale-tracking 修复路径（hasAddedUser 但 GetUserPortByDst 失败时 unmark+重分配）以及 RemoveUser 用 GetUserPortByDstForCleanup 处理两阶段删除——与 usermanager 侧语义强耦合
- profilegen.go 键名必须与 mihomo Alpha 的 inbound struct tag 逐字对齐（拼错会被 decoder 静默丢弃）；tuic 的 congestion-controller 必须显式写 bbr（不走 mihomo ParseListener 默认注入）——第三方 schema 漂移风险点
- subscription.go Extensions 键名与 pkg/proxy/core/subscription/converter/clash.go 的读取端是隐式字符串协议（server_name/reality_short_ids/plugin_* 等），两侧无编译期约束
- updater.go Update 的 swap 后失败回滚（restartFailure：Stop 半死进程→Rollback→errors.Join）与 ErrUpdateFailed.Unwrap 返回双 error 的链路；extractChecksumFor 对 './' 前缀的剥离是曾经的静默失败点
- inbound.go FromNative 双轨反序列化：ProtocolParams 记录刻意丢弃残留 SharedCred，且用记录顶层 Tag/Protocol/Port 覆盖 pp 内字段——迁移中间态记录的正确性值得看
- cert 清理判定分散：cleanupCertFiles 优先 ProtocolParams.Security.TLS.CertSource、fallback SharedCred.CertSource，只删 pem/self_signed；误判会删外部证书或泄漏文件


## [L3] HC — containers/hysteria

**职责**：Container 实现，管理 hysteria2 服务器外部进程：生成 YAML 配置（loopback 监听 + HTTP auth 回调 + trafficStats API）、启动/停止/升级二进制。hysteria 进程只绑 127.0.0.1:cfg.Port，公网访问全部走 UserManager 分配的每用户 UDP forward 端口；容器通过用户事件 + 30s 周期 reconcile 维护 addedUsers 与 forward 规则一致。同时处理 TLS 证书轮询/触发签发、inbound 状态持久化（store）、订阅 URI 生成、traffic 查询与踢人 API。


**文件角色**：

- `pkg/proxy/containers/hysteria/container.go` — 核心 HysteriaContainer：hooks(run/stop)、证书等待 waitForCertAndStart、用户事件处理 handleUserEvent、reconcileUsers 循环、inbound enable/disable + store 持久化、GetUserSubscriptions、Update 二进制升级(带备份回滚)
- `pkg/proxy/containers/hysteria/config.go` — HysteriaConfig 结构与 Decode（map[string]any → 默认值填充，Port 默认 9443，TrafficStatsSecret 缺省随机生成，Domain 回退到 Host）
- `pkg/proxy/containers/hysteria/process.go` — startProcess/stopProcess/generateConfigFile：确保二进制存在（缺则下载）、三级证书来源（cfg 直配 → certReader → certMgr）、写 YAML 配置、经 process.Runner 启动 `hysteria server --config`
- `pkg/proxy/containers/hysteria/register.go` — init 注册 hysteriaFactory 到 container.RegisterFactory；New 做 Domain 回退链（cfg.Domain→Host→opts.ProxyHost）与无证书源 fail-fast；定义 certIssuer 接口
- `pkg/proxy/containers/hysteria/downloader.go` — downloadHysteria：从 GitHub release 下载 linux-amd64 二进制到 .tmp、chmod 755、rename 就位
- `pkg/proxy/containers/hysteria/traffic.go` — QueryTraffic/KickUser：调用 hysteria trafficStats HTTP API（127.0.0.1:TrafficStatsPort，Authorization=secret），rx/tx 语义换算为 Upload/Download
- `pkg/proxy/containers/hysteria/container_fastadd_test.go` — 测试：FastAddInbound 只接受 defaultInboundTag、params 被忽略、inboundEnabled 置 true 的回归契约（Phase 5 legacy path）

**关键流程**：
- 构建: cmd/server.go 空导入触发 register.go init → container.RegisterFactory；hysteriaFactory.New 解析 *HysteriaConfig、做 Domain 回退与 fail-fast 校验、按 BuildOptions 注入 StoreMgr/UserManager/CertReader/CertManager/HTTPPort，调 NewHysteriaContainer（同时 goroutine 订阅 userMgr.Subscribe → forwardUserEvents）
- 启动: BaseContainer.Start → hysteriaHooks.GetRunFunc 的 run：restoreInboundConfig（store 中零值字段回填 + enabled 状态）→ saveInboundConfig → startUserEventHandler + startReconcileLoop → goroutine waitForCertAndStart
- 证书等待: waitForCertAndStart —— cfg 直配证书则立即 startProcess；否则 hasCert()（certReader/certMgr.GetCertFiles）失败时 goroutine 调 certMgr.Issue，之后 30s ticker 轮询直到证书就绪或 certWaitStopCh 关闭，就绪后 startProcess
- 进程启动: startProcess —— 二进制缺失则 downloadHysteria；选证书来源；generateConfigFile 写 YAML（listen=127.0.0.1:Port，auth.http.url=http://127.0.0.1:{httpPort}/api/authHysteria2，trafficStats）；process.NewRunner + Start
- 用户事件: userMgr.Subscribe → forwardUserEvents（非阻塞转发进 100 缓冲 userEventCh，满则丢弃）→ startUserEventHandler goroutine → handleUserEvent：Add 时 GetBindPort(UDP) 分配公网转发口并记入 addedUsers；Remove/Update(不可见) 时 GetUserPortByDstForCleanup+ReleaseBindPort
- 周期对账: startReconcileLoop 每 30s 调 reconcileUsers（Restore/FastAddInbound 也调用）：ListUsers 与 addedUsers 对比，释放不可见用户端口、为可见未跟踪用户 GetBindPort（幂等）
- 订阅: GetUserSubscriptions —— inbound 启用且 GetUserPortByDst 有该用户的转发口才返回 hysteria2:// URI（端口是每用户公网 UDP 口，密码为 AuthToken；cfg.CertFile 非空视为自签 → insecure=1）
- 升级: Update —— downloadHysteria 到 .new → Stop → 原二进制 rename 为 .bak → rename .new 就位 → Start；任一步失败回滚 .bak 并尝试重启旧版

**依赖**：
- imports: pkg/proxy/core/container (BaseContainer/Hooks/Factory/BuildOptions)
- imports: pkg/proxy/core/contracts (ContainerHysteria/ProtocolHysteria2/SubscriptionSpec)
- imports: pkg/proxy/core/inbound (DefaultInbound)
- imports: pkg/proxy/tools/process (Runner)
- imports: pkg/proxy/tools (Downloader)
- imports: pkg/proxy/usermanager (UserManager/UserEvent/GetBindPort 等)
- imports: pkg/store (StoreManager/InboundRecord)
- imports: pkg/certmgmt/domain (CertificateRecord, certIssuer 接口)
- imports: pkg/log (Infof/PrefixWriter)
- imported-by: cmd/server.go (空导入注册 factory)
- imported-by: pkg/proxy/systemtest/hysteria_udp_forward_test.go

**并发模型**：goroutine 共 4 类：forwardUserEvents（构造时启动，非阻塞转发，channel 满即丢事件）、startUserEventHandler 消费 userEventCh、startReconcileLoop 30s ticker（reconcileWg + reconcileStopCh 优雅停止）、waitForCertAndStart（certWaitStopCh 停止；内部还可能 spawn certMgr.Issue goroutine）。hc.mu (sync.Mutex) 仅保护 addedUsers map 与 inboundEnabled 字段，注释明确不在持锁时调 userMgr 方法；handleUserEvent 与 reconcileUsers 并发访问这两个字段。closeUserEventCh 在 stop 时 close+置 nil；closeCertWaitStopCh 用 select-default 防重复 close。

**外部交互**：
- exec: 通过 process.Runner 启动 /usr/local/bin/hysteria server --config <path>（stdout/stderr 经 log.PrefixWriter）
- 网络: downloader.go 从 github.com/apernet/hysteria releases 下载二进制
- 网络: traffic.go 对 hysteria 本地 trafficStats API (127.0.0.1:TrafficStatsPort) 做 GET /traffic、POST /kick，Authorization 头带 secret
- 网络(间接): 生成配置让 hysteria 回调 v2raymg HTTP /api/authHysteria2 做用户鉴权
- 磁盘: 写 /etc/v2raymg/hysteria.yaml（0644，含 trafficStats secret）、二进制 .tmp/.new/.bak rename 操作
- DB/store: storeMgr.InboundStore() Save/Load 持久化 inbound 状态
- 第三方: certmgmt Issue 触发 ACME 签发（经 certIssuer 接口）

**风险关注点（reviewer 重点）**：
- container.go handleUserEvent vs reconcileUsers：check-then-act 模式（读 addedUsers 解锁后再调 GetBindPort 再回写），正确性依赖 GetBindPort 幂等这一注释约定，review 时值得对照 usermanager 实现
- container.go forwardUserEvents 非阻塞 send，channel 满（100）静默丢事件——一致性完全靠 30s reconcile 兜底
- container.go Update：Stop/Rename/Start 多步回滚路径（.bak/.new），失败分支组合多，且 hc.cfg.Version 在 Start 失败回滚后仍为 targetVersion
- container.go stop 顺序：closeUserEventCh 关闭 userEventCh 但 forwardUserEvents 仍可能对已 close 的 channel 发送（读到的是构造时的 hc.userEventCh 字段，close+置 nil 与非阻塞 send 的竞态）
- waitForCertAndStart 与 stopProcess：startProcess 在后台 goroutine 里可能在 Stop 之后才执行（stop 只 close certWaitStopCh，ticker 分支与 stop 分支的 select 竞态窗口）
- restoreInboundConfig 只在 cfg 字段为零值时回填，port 走 float64 断言——store 记录格式变化时静默失败（unmarshal 失败仅 Warn 且 return nil）
- process.go generateConfigFile 将 TrafficStatsSecret 以 0644 写入磁盘
- traffic.go QueryTraffic(clear=1) 读后清零语义——调用方失败时数据丢失，review L3 时结合调用方看


## [L3] SC — containers/snell

**职责**：Container 实现，负责管理 snell-server 外部进程的完整生命周期：自动下载二进制、生成 INI 配置、启停进程、版本升级（原子替换+回滚）。snell 只监听 127.0.0.1 单端口共享 PSK，因此模块通过 UserManager 的 bind-port/forward 层为每个可见用户分配外部转发端口，并据此生成 snell:// 订阅 URI。带事件驱动 + 30s 周期 reconcile 双通道的用户同步，以及经 StoreManager 的 inbound 配置持久化/恢复。


**文件角色**：

- `pkg/proxy/containers/snell/container.go` — 核心 703 行：SnellContainer 结构、hooks run/stop、Update 原子升级、inbound enable/disable、用户事件处理 handleUserEvent、reconcileUsers、store 持久化 save/restoreInboundConfig、GetUserSubscriptions
- `pkg/proxy/containers/snell/config.go` — SnellConfig 及 Decode（map[string]any → 结构体，带默认值：/usr/local/bin/snell-server、端口 16160、v5.0.1）；PSK 为空或 "auto" 时用 crypto/rand 生成 16 字节 hex PSK
- `pkg/proxy/containers/snell/downloader.go` — downloadSnellServer：按 GOARCH 映射拼 dl.nssurge.com 下载 URL，HTTP 下载 zip 到临时文件，解压出 snell-server 写到目标路径 (0755)
- `pkg/proxy/containers/snell/process.go` — startProcess（缺二进制则自动下载 → generateConfigFile → process.NewRunner + Start，stdout/stderr 走 log.NewPrefixWriter）和 stopProcess
- `pkg/proxy/containers/snell/register.go` — init() 向 container.RegisterFactory 注册 snellFactory；New 从 BuildOptions 装配 StoreMgr/UserManager 选项并调 NewSnellContainer

**关键流程**：
- 创建: cmd/server.go 空导入触发 register.go init() → RegisterFactory；框架调 snellFactory.NewConfigObj + SnellConfig.Decode（默认值/PSK 生成）→ snellFactory.New → NewSnellContainer（建 BaseContainer、loopback DefaultInbound；若有 userMgr 则订阅并起 forwardUserEvents goroutine）。
- 启动: BaseContainer.Start → snellHooks.GetRunFunc 的 run：restoreInboundConfig（从 store 恢复 port/psk/enabled）→ saveInboundConfig → startUserEventHandler → startReconcileLoop → startProcess（stat 二进制缺则 downloadSnellServer → generateConfigFile → process.Runner.Start）。
- 停止: hooks stop：stopReconcileLoop（close reconcileStopCh + wg.Wait）→ closeUserEventCh（终止 forwardUserEvents 和事件处理 goroutine）→ stopProcess（runner.Stop）。
- 用户事件: userMgr.Subscribe → forwardUserEvents（非阻塞转发到容量 100 的 userEventCh，满则丢弃）→ handleUserEvent：Add 时 userMgr.GetBindPort 建转发并记入 addedUsers；Remove/不可见 Update 时 GetUserPortByDstForCleanup + ReleaseBindPort。
- 周期对账: startReconcileLoop 每 30s（及 Restore()、FastAddInbound）调 reconcileUsers：ListUsers 与 addedUsers 快照对比，缺的 GetBindPort 补建、多的 ReleaseBindPort 清理。
- 订阅: GetUserSubscriptions → 检查 inboundEnabled → userMgr.GetUserPortByDst 取用户外部端口 → 拼 snell://psk@host:port?version=5 URI 的 SubscriptionSpec。
- 升级: Update → downloadSnellServer 到 .new → Stop → 原二进制 rename 为 .bak → .new rename 就位 → Start；任一步失败按 hasBackup 回滚并尝试重启旧版本。
- inbound 开关: FastAddInbound 置 inboundEnabled=true + saveInboundConfig + reconcileUsers；RemoveInboundConfig 置 false + releaseAllForwardRules（进程不停）。

**依赖**：
- imports: pkg/log
- imports: pkg/proxy/core/container (BaseContainer/Factory/UpdateRequest)
- imports: pkg/proxy/core/contracts (ContainerSnell/ProtocolSnell/SubscriptionSpec)
- imports: pkg/proxy/core/inbound (DefaultInbound)
- imports: pkg/proxy/tools/process (Runner)
- imports: pkg/proxy/usermanager (UserManager/UserEvent/GetBindPort/ReleaseBindPort)
- imports: pkg/store (StoreManager/InboundRecord)
- imported by: cmd/server.go（空导入注册工厂）；pkg/proxy/containers/hysteria/container.go 注释引用其 handleUserEvent 模式（非代码依赖）

**并发模型**：3 类 goroutine：forwardUserEvents（NewSnellContainer 起，非阻塞 select 转发，channel 满丢事件，userEventCh close 后退出）；startUserEventHandler 的事件消费 goroutine；startReconcileLoop 的 30s ticker goroutine（reconcileStopCh + reconcileWg 优雅停止）。sc.mu (sync.Mutex) 保护 addedUsers map 和 inboundEnabled，注释明确锁只包住 map/字段读写、绝不持锁调用 userMgr.GetBindPort/ReleaseBindPort；handleUserEvent 与 reconcileUsers 可并发，依赖 GetBindPort 幂等性容忍 TOCTOU。closeUserEventCh 将 sc.userEventCh 置 nil，而 forwardUserEvents 对同字段做无锁读。

**外部交互**：
- 网络: downloader.go 从 https://dl.nssurge.com/snell/snell-server-{ver}-linux-{arch}.zip HTTP 下载
- exec: 通过 pkg/proxy/tools/process.Runner 启停 snell-server 第三方二进制
- 磁盘: 二进制写入 cfg.BinaryPath（默认 /usr/local/bin/snell-server），升级时 .new/.bak rename 交换；INI 配置写 cfg.ConfigFilePath（默认 /etc/v2raymg/snell.conf，0644，含明文 PSK）；zip 临时文件
- 持久化: storeMgr.InboundStore().Save/Load，NativeJSON 里含 psk

**风险关注点（reviewer 重点）**：
- container.go Update：Stop/rename/Start 多步回滚路径（约 L174-247），各失败分支忽略 rollback 的 Start/Rename 错误，且升级期间 sc.cfg.Version 已改写——升级流程是最复杂的状态机
- container.go closeUserEventCh 无锁置 userEventCh=nil，与 forwardUserEvents 中对 sc.userEventCh 的读存在数据竞争窗口；forwardUserEvents 满 channel 丢事件依赖 30s reconcile 兜底
- container.go handleUserEvent 与 reconcileUsers 的锁分段（先读后写、中间不持锁调 userMgr）——正确性依赖 GetBindPort 幂等的约定，reviewer 应对照 usermanager 实现核实
- container.go restoreInboundConfig 会用 store 记录覆盖 Decode 生成的 port/psk；PSK 明文出现在 store JSON、配置文件和订阅 URI 三处
- downloader.go 下载无校验（无 checksum/签名），直接写入 0755 可执行路径
- process.go startProcess 失败路径与 stopProcess 中 sc.runner 的置 nil 无锁保护（依赖 BaseContainer 串行化 Start/Stop）


## [L4] CLU — pkg/cluster

**职责**：维护集群成员表：以 Node（proto.Node + 双向 token + 心跳时间戳 + gRPC 连接缓存）为单位，跟踪每个对端节点的注册/心跳/有效性状态。提供两种视角的管理器：EndNodeClusterManager（end 节点视角，单个 Cluster，内嵌 NodeManager + WrongTokenNode 列表）和 CenterClusterManager（center 视角，clusterName→Cluster 多集群 map）。同时持有全局 LocalNode 单例（本节点身份与本地 RPC token），并支持从配置加载静态对端节点以支持无中心节点部署。


**文件角色**：

- `pkg/cluster/node.go` — Node/StaticNode/LocalNode 类型：token 与心跳字段、有效性判断（IsValid/IsCompleteRegister/RegisteredLocal/RegisteredRemote）、GetGrpcClientConn 惰性 gRPC 连接缓存、全局 globalLocalNode 单例
- `pkg/cluster/node_manager.go` — NodeManager：带 RWMutex 的 name→*Node map（指针到 map），Add/Delete/Get/Filter/LoadStaticNode/Clear
- `pkg/cluster/cluster.go` — Cluster：单集群封装（Name/Token/NodeManager/WrongTokenNode），节点鉴权 AuthRemoteNode、IsSameCluster、FilterTimeoutNode、按 NodeFilter 取节点集合
- `pkg/cluster/cluster_manager.go` — EndNodeClusterManager（内嵌 Cluster）与 CenterClusterManager（RWMutex 保护的 clusters map，Add/GetCluster/DeleteNode/DeleteCluster/Filter）
- `pkg/cluster/end_node_init.go` — NewEndNodeClusterManagerFromConfig：从 ClusterInitConfig/NodeInitConfig 初始化本地节点（uuid token、MaxInt64 永久心跳）、注册进集群、加载静态节点；GetClusterToken/IsSameCluster 薄封装
- `pkg/cluster/cluster_test.go` — 测试：Node 有效性/注册状态判定、IsSameCluster、AuthRemoteNode 四种分支、LoadStaticNode 过滤规则、StaticNode.IsValide、FromConfig 构造

**关键流程**：
- End 节点启动: NewEndNodeClusterManagerFromConfig（end_node_init.go）→ GetLocalNode 填充全局 LocalNode + uuid Token → mgr.Init/Add 以 math.MaxInt64-NodeTimeOut 心跳注册本地节点 → mgr.LoadStaticNode 载入静态对端（NodeManager.LoadStaticNode 用 StaticNode.IsValide 过滤掉与本地同 host:port 或同名节点，标记 isLocal=true）。
- 远端节点鉴权（每次 RPC 心跳/请求）: Cluster.AuthRemoteNode(**Node)（cluster.go）→ NodeManager.Get 查本地记录 → Compare 校验 host+port+cluster+name 四元组 → 比对 InToken → 检查 GetHeartBeatTime 是否超 HeartBeatTimeout(60s) → 通过则刷新 GetHeartBeatTime 并把 *node 替换为本地记录指针。
- 超时清理: Cluster.FilterTimeoutNode / Cluster.Filter（cluster.go）→ NodeManager.Filter 重建 map 丢弃 IsValid()==false 的节点；WrongTokenNode 按 CreateTime+NodeTimeOut 过期。由外层（rpc server 的周期任务）驱动。
- 出站 gRPC 连接: Node.GetGrpcClientConn（node.go）→ 若连接为 nil 或状态非 Ready 则 grpc.Dial(host:port, insecure) 并缓存在 node.grpcClientConn；被 pkg/rpc/client/end_node_rpc.go 消费。
- Center 侧注册: CenterClusterManager.Add(clusterName, node)（cluster_manager.go）→ 写锁下查/建 Cluster 并 NodeManager.Add；周期性 CenterClusterManager.Filter 在 RLock 下对每个 cluster 跑 NodeManager.Filter 清理失效节点。
- 错误 token 节点旁路: Cluster.AddToWrongNodeList / GetNodeFromWrongNodeList / DeleteFromWrongTokenNodeList → 独立 WrongTokenNode NodeManager，key=node_name:token，由 Cluster.Filter 按创建时间清理。

**依赖**：
- 导入仓库内: pkg/rpc/proto（proto.Node）、pkg/log（node_manager.go 日志）
- 被导入方: cmd/server.go、cmd/cli/info.go、cmd/cli/client/http_client.go、pkg/rpc/server/{end_node_server,center_node_server,end_node_cluster,cluster_state}.go、pkg/rpc/client/end_node_rpc.go、pkg/http/{http_server,node_handler,sub_handler}.go 及 http 测试

**并发模型**：本包不启 goroutine。两把 sync.RWMutex：NodeManager.lock 保护 nodes（注意 nodes 是 *map[string]*Node，Filter 通过 RLock 下复制到新 map、再 Lock 下换指针实现原子替换；但 HaveNode、Get、GetAllNode 完全不加锁直接读 map，GetAllNode 还把内部 map 直接返回给调用方遍历）；CenterClusterManager.lock 保护 clusters map（DeleteNode 和 Filter 只取 RLock，内层写由 NodeManager 自己的锁承担）。Node 本身无锁：GetHeartBeatTime/ReportHeartBeatTime/grpcClientConn 字段由多个 RPC 路径直接读写（AuthRemoteNode 写 GetHeartBeatTime，GetGrpcClientConn 惰性写连接字段）。globalLocalNode 是包级单例，Init 阶段写、之后各处读。

**外部交互**：
- Node.GetGrpcClientConn: grpc.Dial 到对端节点 host:port，使用 insecure.NewCredentials()（明文传输凭据，实际加密在上层实现），连接缓存于 Node 内
- 无磁盘/exec/DB 操作；静态节点数据由上层从配置文件解析后以 []StaticNode 传入 LoadStaticNode

**风险关注点（reviewer 重点）**：
- node_manager.go: HaveNode/Get/GetAllNode 无锁读 map，且 GetAllNode 直接返回内部 map（cluster.go 的 GetProtoNodesWithFilter/GetNodeNameList 在其上无锁遍历），与 Add/Delete/Filter 的写并发——reviewer 应重点评估数据竞争面
- node.go: Node 的心跳时间戳与 grpcClientConn 字段无任何同步，AuthRemoteNode（写 GetHeartBeatTime）与 IsValid/Filter（读）跨 goroutine 交叉
- node_manager.go NodeManager.Filter: RLock 复制 + Lock 换指针的两段式操作，两段之间其他写入（Add/Delete）会丢失
- cluster.go AuthRemoteNode 的 **Node 双重指针替换语义（调用方持有的指针被换成本地记录），调用侧行为依赖此副作用
- cluster.go IsValid/RegisteredLocal/RegisteredRemote 直接对 NodeManager.Get 返回值调方法，Get 返回 nil 时是对 nil receiver 的方法调用（IsValid 等会解引用字段）
- cluster_manager.go: NodeManager 是值类型但内部持 *map 与 lock，EndNodeClusterManager 内嵌 Cluster 值传递时 lock 复制的语义值得确认
- end_node_init.go: 本地节点用 math.MaxInt64-NodeTimeOut 心跳时间避免被过滤，IsValid 中 +NodeTimeOut 的溢出边界
- node.go GetGrpcClientConn: 连接非 Ready 时直接重 Dial 覆盖旧连接，旧 ClientConn 不 Close


## [L4] RPC — pkg/rpc (client/server/proto定义)

**职责**：节点间 gRPC 通信层。proto 定义 EndNodeAccess 与 CenterNodeAccess 两个 gRPC 服务（生成代码）。server 实现 EndNodeServer（end 节点上的全部 RPC handler：用户/入站/证书/订阅/指标/集群成员管理/心跳，以及主动向 center 和 peer end 节点发心跳/注册的后台循环）和 CenterNodeServer（center 侧仅 HeartBeat + 节点目录维护）。client 提供 EndNodeClient，把 HTTP 层的操作以类型注册表方式并发扇出到多个 end 节点，请求体经 token 加密 codec（pkg/common/rpc EncryptMessageCodec）传输。


**文件角色**：

- `pkg/rpc/client/end_node_rpc.go` — EndNodeClient 核心：reqFuncMap 注册表（init 注册 ~30 种 ReqXxx 请求函数，每个 = unmarshal + 注入 NodeAuthInfo + 带加密 codec 调 gRPC + 业务 code 检查）；ReqToMultiEndNodeServer 并发扇出入口
- `pkg/rpc/client/end_node_rpc_type.go` — ReqToEndNodeType iota 枚举（33 个请求类型常量）
- `pkg/rpc/client/context.go` — NewContext：固定 1 秒超时的 RPC context（RpcTimeOut=1）
- `pkg/rpc/server/end_node_server.go` — EndNodeServer 结构体（全局单例 endNodeServer）、Init 依赖注入、Start（监听+注册加密 codec+启动心跳/filter goroutine）、unaryServerInterceptor（反射式 NodeAuthInfo 鉴权 + OnlyGateway 方法过滤 + methodRspMap 反射构造错误响应）
- `pkg/rpc/server/center_node_server.go` — CenterNodeServer：HeartBeat handler（时钟漂移校验、集群/节点目录增删）、filter 定期清理无效节点、Init/Start
- `pkg/rpc/server/end_node_cluster.go` — 端节点集群逻辑：RegisterNode/HeartBeat handler（含用户 digest 对比）、registerToEndNode/heartbeatToEndNode 出站调用、heartBeatAndRegisterToNodeOrCenterNode 后台循环、addRemoteNode 节点发现、proto<->contracts.User 转换助手
- `pkg/rpc/server/end_node_user.go` — 用户类 handler：GetProfile/GetUsers/AddUsers/DeleteUsers/UpdateUsers/ResetAuthToken/ResetUser/ResetUserTraffic/GetSub/GetBandWidthStats/RotateInboundPort/RotateAllPorts，全部委托 userMgr/subMgr/statsCollector
- `pkg/rpc/server/end_node_inbound.go` — 入站类 handler：AddInbound/GetInbound/UpdateProxy/SetGatewayModel/FastAddInbound（协议/transport/security 解析、证书获取、extra_params 类型转换、params.FillDefaults）/ListInbound/DeleteInboundByName；certMgrAdapter 适配 params.CertManager
- `pkg/rpc/server/end_node_status.go` — GetStatus handler + 一组懒启动的系统指标采样器（CPU/内存/网速/TCP 连接数，读 /proc），InitNetSpeedMonitor/detectExternalInterface 接口选择
- `pkg/rpc/server/end_node_metric.go` — SetPingCheck/GetPingMetric/GetNodeMetric handler，委托 pingCollector/nodeMetricCol
- `pkg/rpc/server/end_node_cert.go` — ObtainNewCert/TransferCert/GetCerts/DeleteCert handler，委托 certManager
- `pkg/rpc/server/end_node_cluster_user.go` — UpsertClusterUsers handler：集群用户全量同步写入（userMgr.SyncUpsertUser 版本仲裁）
- `pkg/rpc/server/end_node_node_groups.go` — GetNodeGroups/SetNodeGroups handler（空组回退 ["default"]）
- `pkg/rpc/server/end_node_containers.go` — GetContainers handler：返回 containerMgr.TypeStrings()
- `pkg/rpc/server/cert_manager.go` — CertManager 接口定义（供 lego / pkg/certmgmt 适配器实现）
- `pkg/rpc/server/cluster_state.go` — ClusterState 接口定义（集群成员/token/wrong-node 列表操作）
- `pkg/rpc/server/metrics.go` — BandwidthStatsCollector/PingCollector/NodeMetricCollector 三个注入接口定义
- `pkg/rpc/proto/rpc_server.proto` — proto 源：EndNodeAccess/CenterNodeAccess 服务与全部消息定义
- `pkg/rpc/proto/rpc_server.pb.go + rpc_server_grpc.pb.go` — 生成代码
- `pkg/rpc/server/*_test.go` — 7 个测试文件：getContainerByType 解析、ListInbound 多容器/排序、FastAddInbound 参数组合矩阵/mihomo 路由/legacy stream 回退/reality 不触发证书、ResetAuthToken/ResetUserTraffic/Rotate{InboundPort,AllPorts} 成功+失败+部分成功分支、UpsertClusterUsers 同步语义（新用户/旧版本跳过/墓碑）、NodeGroups、集群开关 wiring

**关键流程**：
- HTTP 层扇出请求: EndNodeClient.ReqToMultiEndNodeServer (client/end_node_rpc.go) — pb.Marshal 请求 → 按 reqType 查 reqFuncMap → 对每个已注册节点起 goroutine（全局 ch 限流 64）→ node.GetGrpcClientConn → ReqXxx（注入 NodeAuthInfo{node.OutToken}，grpc.ForceCodec(EncryptMessageCodec(token))）→ 结果按节点名汇入 succList/failedList（mutex 保护）
- 入站 RPC 鉴权: EndNodeServer.unaryServerInterceptor (end_node_server.go) — OnlyGateway 时非白名单方法返回 methodRspMap 空响应 → authRemoteNode 用反射取 req.NodeAuthInfo → clusterState.AuthRemoteNode 校验 InToken（或等于本地 Token 放行）→ 失败时反射往对应 Rsp 类型写 Code=400/Msg → 通过则调 handler 并打 delay 日志
- 心跳/注册循环: EndNodeServer.Start → go heartBeatAndRegisterToNodeOrCenterNode (end_node_cluster.go) — 每 10s: heartbeatToCenterNode（明文 gRPC，无 token）+ registerOrHeartBeatToEndNode（并发上限 10，对未注册节点 registerToEndNode 取 OutToken，对已注册节点 heartbeatToEndNode 附带 TimestampUs 和用户 digest；心跳失败即清空 node.OutToken）；heartBeatRsp.NodesMap 经 addRemoteNode 做节点发现
- 节点注册（server 侧）: EndNodeServer.RegisterNode (end_node_cluster.go) — 校验 host/port/重名 → clusterState.IsSameCluster(clusterToken) → wrong-node 列表处理 → 新节点生成 uuid InToken 存入 clusterState，Rsp.Data 返回 token
- 集群用户同步: HeartBeat handler 用 usync.CompareDigests 对比请求 digest，Rsp.NeedClusterUsers 列出缺的用户名 → 对端 heartbeatToEndNode 收到后组 UpsertClusterUsersReq 推送 → UpsertClusterUsers handler (end_node_cluster_user.go) 经 userMgr.SyncUpsertUser 版本仲裁写入
- FastAddInbound: handler (end_node_inbound.go) — getContainerByType（空串默认 xray，hysteria2 归一化）→ 协议/transport/security 解析（QUIC/TCP-native 协议不设 transport）→ 需要 tls/xtls 且有 domain 时 certManager.ObtainNewCert → extra_params 逐 key 类型转换 → params.FillDefaults（certMgrAdapter + scratchDir）→ c.FastAddInbound
- GetStatus 指标: EndNodeServer.GetStatus (end_node_status.go) — sync.Once 懒启动 4 个采样 goroutine（每秒读 /proc/stat、/proc/net/dev、/proc/meminfo、/proc/net/tcp[6]，结果存 atomic）→ 组 NodeStatus 返回
- Center 心跳: CenterNodeServer.HeartBeat (center_node_server.go) — 时间戳漂移>30s 拒绝（Code 105）→ node.IsComplete 校验 → clusters.GetCluster 更新心跳时间/加入新节点/新建集群 → 返回集群有效节点表；CenterNodeServer.Start → go filter 每 10s 清理

**依赖**：
- 导入(server): pkg/cluster, pkg/common, pkg/common/rpc(加密codec), pkg/log, pkg/buildinfo, pkg/proxy/appconfig, pkg/proxy/core/{container,contracts,params,subscription}, pkg/proxy/usermanager(+usermanager/sync), pkg/proxy/forward, pkg/rpc/client(server 复用 ReqRegisterNode/ReqHeartBeat/ReqUpsertClusterUsers/NewContext), pkg/rpc/proto
- 导入(client): pkg/cluster, pkg/log, pkg/common/rpc, pkg/rpc/proto
- 被导入: cmd/server.go, pkg/http/* 大量 handler(经 rpc/client 扇出), pkg/certmgmt/service/rpc_adapter.go, pkg/collecter/ping_collector.go

**并发模型**：client: 全局 chan struct{} 容量 64 (MaxConcurrencyClientNum) 限流所有 ReqToMultiEndNodeServer 调用；每节点一个 goroutine，succList/failedList 由局部 sync.Mutex 保护，sync.WaitGroup 汇合。server(end): Start 起两个长期 goroutine —— heartBeatAndRegisterToNodeOrCenterNode（10s ticker）和 filter（20s ticker 清理无效节点）；registerOrHeartBeatToEndNode 内部再用容量 10 的 chan + WaitGroup 并发出站；end_node_status.go 用 4 个 sync.Once 懒启动 4 个每秒采样 goroutine，结果全部存 atomic.Value/atomic.Uint64/atomic.Int32，netMonitorIfaceSet 由 Init 阶段一次性写入后只读。server(center): Start 起 filter goroutine（10s ticker）。cluster.Node 字段（OutToken/GetHeartBeatTime/ReportHeartBeatTime）在心跳 goroutine 与 handler 间直接读写，依赖 pkg/cluster 内部保护（或无保护）。

**外部交互**：
- gRPC 网络: end/center 两个 grpc.Server 监听 TCP；client 与心跳循环向 peer/center 发起 gRPC 连接（node.GetGrpcClientConn）
- 请求体加密: pkg/common/rpc.NewEncryptMessageCodec(token) 作为 grpc ForceCodec / 注册 codec（center 心跳不加密）
- 磁盘/procfs: end_node_status.go 每秒读 /proc/stat、/proc/net/dev、/proc/meminfo、/proc/net/tcp、/proc/net/tcp6、/proc/net/route（仅 Linux，出错返回 -1/0）
- 间接外部进程: handler 委托 containerMgr/certManager/userMgr，最终触达 xray/hysteria/snell/mihomo 二进制与 ACME 签证书（不在本模块内）

**风险关注点（reviewer 重点）**：
- end_node_server.go authRemoteNode/unaryServerInterceptor：反射取 NodeAuthInfo（FieldByName("NodeAuthInfo").Elem()）和反射写 methodRspMap 的 Code/Msg——依赖每个 req/rsp 类型都有这些字段且 methodRspMap 覆盖全部方法；methodRspMap 是共享单例对象，错误分支返回的是同一个 rsp 实例并被并发复用/修改
- end_node_server.go authRemoteNode 中 `err != nil && localNode.Token != node.InToken` 的短路放行逻辑（本地 token 等价旁路）是鉴权关键路径
- end_node_cluster.go：node.OutToken/ReportHeartBeatTime 在多个心跳 goroutine 与 RegisterNode handler 间无锁读写 cluster.Node 字段；registerOrHeartBeatToEndNode 对 invalid 节点用 `return` 而非 `continue`（会跳过其余全部节点且已 wg.Add 的计数路径需要核对——实际 return 在 wg.Add 之前）
- client/context.go RpcTimeOut=1 秒的固定超时对 UpdateProxy/ObtainNewCert 等慢操作是全局约束；ReqToMultiEndNodeServer 传入 nil ctx 时才用它，ctx 语义分叉
- client/end_node_rpc.go 全局 ch(64) 在 wg.Add 之前 `ch <- struct{}{}`，扇出与其他并发调用共享限流，节点多时可能阻塞持有 caller
- end_node_inbound.go FastAddInbound：协议/transport/security/extra_params 的手工类型转换矩阵和证书触发条件分支多，是历史回归高发区（已有 fastadd_params_test.go 组合矩阵覆盖）
- end_node_status.go 采样器 goroutine 永不退出、每秒读 procfs；netMonitorIfaceSet 必须在首次 GetStatus 前由 InitNetSpeedMonitor 设置（初始化顺序耦合）
- server 包依赖全局单例 endNodeServer 与 localNode = cluster.GetLocalNode()（包 init 时取），测试与多实例场景受限
- heartbeat 时钟漂移校验（30s，heartbeatMaxDriftUs）在 end 和 center 两处重复实现（end_node_cluster.go HeartBeat 与 center_node_server.go HeartBeat）


## [L4] COL — pkg/collecter (+ping)

**职责**：Metric collection for a v2raymg node. `pkg/collecter` exposes two collectors: NodeCollector, a thin wrapper around prometheus node_exporter for system metrics, and PingCollector, which orchestrates network-latency probing to a configurable set of target nodes. `pkg/collecter/ping` implements the probing machinery: ICMP (via pro-bing) and TCP-connect checkers, a NodeManager that loads/reloads target nodes from local YAML files or remote HTTP URLs, and PingResult, a ring-buffer of the last 100 delays with avg/max/min/stdev/loss aggregation exported as proto.PingResult for the RPC layer.


**文件角色**：

- `pkg/collecter/node_collector.go` — NodeCollector: wraps prometheus node_exporter in a private Registry; sync.Once singleton via DefaultNodeCollector(); panics if node_exporter init fails
- `pkg/collecter/ping_collector.go` — PingCollector: builds NodeManager + enabled checkers (ICMP/TCP) from PingCollectorConfig, spawns one aggregator goroutine per checker (startChecker), merges results into pingResultMap keyed by checker/geo_isp_ip, exposes GetPingResults/StopPing for pkg/rpc/server; cleanupStaleResults + extractHostFromNodeName prune results on node reload
- `pkg/collecter/ping/ping.go` — Core types: PingResult (100-slot ring buffer, RWMutex, Update/GetAvg/GetMax/GetMin/GetStDev/GetLoss/GetLatestDelay/ConvertToProtoPingResult; -1=no result, -2=loss sentinels), PingChecker interface, basePingChecker (buffered resultChan size 1000, ctx/cancel, atomic isRunning)
- `pkg/collecter/ping/icmp_ping_checker.go` — IcmpPingChecker: Start() runs a 1s reconcile loop swapping pingerMap against NodeManager.ListByUsage("icmp"); each pingerInfo runs pro-bing privileged Pinger with OnSend/OnRecv channels, a 500ms cycle ticker declares packets lost after pingTimeout; package globals pingerIndex/pingInterval/pingTimeout
- `pkg/collecter/ping/tcp_ping_checker.go` — TcpPingChecker: 1s reconcile loop (updatePingers, mutex-protected pingerMap keyed host:port); per-node tcpPingerInfo goroutine does net.DialTimeout on interval, RTT in ms or -2 loss, WaitGroup-based stop
- `pkg/collecter/ping/node_manager.go` — NodeManager interface + nodeManagerImpl: LoadFromSources builds file/remote loaders from appconfig.PingNodeSource, merges nodes by host, wires StartReload for remote sources with UpdateInterval; updateNodes diff-applies reloads, notifyChange fires OnChange callbacks, logStatsLocked logs per-usage counts
- `pkg/collecter/ping/node_loader.go` — NodeLoader and ReloadableLoader interfaces (Load/Name, StartReload)
- `pkg/collecter/ping/file_loader.go` — FileLoader: reads YAML nodes file from disk, defaults Usage=[icmp,tcp] and Port=80
- `pkg/collecter/ping/remote_loader.go` — RemoteLoader: HTTP GET YAML (10s timeout), same defaults as FileLoader; StartReload spawns ticker goroutine calling Load and onChange, stop func closes stopCh under mutex
- `pkg/collecter/ping/ping_node_manager.go` — PingNodeInfo struct (Host/Port/ISP/Geo/Usage) plus legacy PingNodeManager (AddNode requires parseable IP, ListNode); PingNodeManager appears unused by the new NodeManager path
- `pkg/collecter/ping/options.go` — OptionFunc functional options: WithNodeManager/WithGetNodeFunc/With{ICMP,TCP}Ping{Interval,Timeout}, applied via type-assertion on the concrete checker

**关键流程**：
- Startup: NewPingCollector(cfg) (ping_collector.go) → ping.NewNodeManager() → NodeManager.LoadFromSources(cfg.NodeSources) (creates FileLoader/RemoteLoader, starts remote reload goroutines) → nm.OnChange(cleanupStaleResults) → pc.init() constructs NewIcmpPingChecker/NewTcpPingChecker, applies options, and per checker calls startChecker (aggregator goroutine + checker.Start()).
- ICMP probing: IcmpPingChecker.Start() runs a 1s ticker goroutine reconciling pingerMap with nodeManager.ListByUsage("icmp"); new nodes get pingerInfo.start → newPinger (pro-bing, privileged, SetID from global pingerIndex) → pingLoop goroutine bridges OnSend/OnRecv callbacks through sendCh/recvCh, a 500ms ticker marks packets older than pingTimeout as loss (-2), each event pushes a PingResult to ipc.resultChan.
- TCP probing: TcpPingChecker.Start() → 1s ticker → updatePingers() diffs pingerMap against ListByUsage("tcp"); each tcpPingerInfo.pingLoop ticks at interval → doPing() net.DialTimeout, Update(rtt ms) or Update(-2), sends to resultChan.
- Aggregation: PingCollector.startChecker's goroutine reads checker.GetChan(); per result builds key "{geo}_{isp}_{ip}" and under pc.mu either stores the PingResult or calls existing.Update(result.GetLatestDelay()), maintaining the 100-sample ring per node.
- Query: PingCollector.GetPingResults() (called by pkg/rpc/server) RLocks pingResultMap and converts every PingResult via ConvertToProtoPingResult to []*proto.PingResult.
- Remote reload: RemoteLoader.StartReload ticker → Load() HTTP GET → onChange closure in LoadFromSources → nm.updateNodes (diff add/remove by host) → nm.notifyChange → PingCollector.cleanupStaleResults deletes results whose host (parsed back out of the composite node name by extractHostFromNodeName) is gone; checkers pick up node changes on their own next 1s reconcile tick.
- Shutdown: PingCollector.StopPing() → each checker.Stop() (basePingChecker.cancel for ICMP; TcpPingChecker.Stop cancels then stopAllPingers via ctx.Done branch) → nodeManager.Stop() closes remote-reload goroutines.
- System metrics: DefaultNodeCollector() (sync.Once) → NewNodeCollector (node_exporter into fresh Registry) → Collect() = registry.Gather().

**依赖**：
- imports: github.com/lureiny/v2raymg/pkg/log
- imports: github.com/lureiny/v2raymg/pkg/proxy/appconfig (PingNodeSource config type)
- imports: github.com/lureiny/v2raymg/pkg/rpc/proto (proto.PingResult)
- imported by: cmd/server.go (only in-repo importer; wires PingCollector/NodeCollector into pkg/rpc/server)

**并发模型**：Heavy goroutine fan-out. Per PingCollector: one aggregator goroutine per checker (startChecker) consuming the checker's buffered resultChan (size 1000); pc.mu (RWMutex) protects pingResultMap (written by aggregators, read by GetPingResults, write-locked by cleanupStaleResults from the reload callback goroutine). Per checker: a 1s reconcile goroutine; ICMP additionally runs, per node, a pro-bing pinger goroutine plus a pingLoop goroutine bridging unbuffered sendCh/recvCh with a 500ms timeout ticker; TCP runs one pingLoop goroutine per node with WaitGroup-based stop and tpc.mu guarding pingerMap. Notably IcmpPingChecker.pingerMap is swapped/rebuilt in the reconcile loop with no lock (unlike TCP), and package globals pingerIndex/pingInterval/pingTimeout in icmp_ping_checker.go are shared unguarded. nodeManagerImpl.mu (RWMutex) guards nodes/stopFuncs/callbacks; RemoteLoader has its own mutex + stopCh for reload lifecycle. basePingChecker uses atomic.Bool isRunning and context cancel; PingResult has its own RWMutex around the ring buffer. NodeCollector uses sync.Once for the singleton.

**外部交互**：
- Raw ICMP echo via github.com/prometheus-community/pro-bing with SetPrivileged(true) — requires raw-socket capability/root
- TCP connect probes via net.DialTimeout to configured host:port (default port 80)
- HTTP GET of YAML node lists (RemoteLoader, 10s client timeout)
- Disk read of local YAML node file (FileLoader, os.ReadFile)
- System metrics scraped from /proc, /sys etc. via prometheus node_exporter library (NodeCollector)

**风险关注点（reviewer 重点）**：
- icmp_ping_checker.go: IcmpPingChecker.pingerMap is read/replaced inside the reconcile goroutine without any mutex (TcpPingChecker uses tpc.mu for the equivalent map); Init/Start ordering is the only thing keeping this single-threaded — reviewers should verify no other goroutine touches it
- icmp_ping_checker.go: package-level mutable globals pingerIndex, pingInterval, pingTimeout; pingLoop uses the globals for pinger.Interval and loss timeout rather than the per-pingerInfo interval/timeout fields set from options, and pi.interval/pi.timeout are stored but appear unread
- icmp_ping_checker.go Start(): the ctx.Done check sits inside a select default inside the ticker loop — the select's <-ipc.ctx.Done() case only wins if checked before default; the goroutine structure (for range ticker.C with inner select) is worth close reading for shutdown behavior; also isRunning.Store(true) happens after go func, and basePingChecker.Stop() sets isRunning to true (ping.go line ~1074 of concat; basePingChecker.Stop stores true not false)
- ping.go basePingChecker.Stop(): `bpc.isRunning.Store(true)` after cancel — TcpPingChecker overrides Stop with Store(false), but IcmpPingChecker inherits the base version; affects restart/IsRunning semantics
- ping_collector.go startChecker: `ctx, _ := context.WithCancel(checker.GetContext())` discards the cancel func (vet-flagged pattern); aggregator lifetime is tied solely to the checker's own ctx
- ping_collector.go extractHostFromNodeName: reverse-parses host out of "{geo}_{isp}_{host}" by splitting on "_" — breaks if geo/isp/host themselves contain underscores; also for ICMP the map key uses the resolved pinger IP (pinger.IPAddr()) while cleanupStaleResults compares against configured node.Host, so hostname-configured ICMP nodes may never match the host set
- icmp_ping_checker.go pingLoop: sendCh/recvCh are unbuffered and fed from pro-bing callbacks; if pingLoop blocks (e.g. on `ch <- result` into a full resultChan, size 1000) the pinger's callback goroutine blocks too — backpressure chain worth reviewing
- node_manager.go LoadFromSources vs Stop/updateNodes: LoadFromSources stops and clears stopFuncs while holding mu, and remote onChange callbacks call nm.updateNodes (Lock) — reentrancy between reload goroutines and a concurrent LoadFromSources call deserves attention
- ping_node_manager.go: legacy PingNodeManager (AddNode/ListNode) coexists with the newer NodeManager interface in node_manager.go — duplicate concepts, the legacy one seems unused within the module
- ping.go calculateStDev returns variance (sumSquaredDiff/n without sqrt) despite the StDev name; GetMin returns math.MaxFloat64 when no valid sample (findMin has no invalidResult fallback, unlike findMax returning -1) — behavioral facts a reviewer should check against consumer expectations


## [L5] HTTP — pkg/http (+auth +prometheus_desc)

**职责**：Gin-based REST management API of v2raymg. It exposes login/JWT auth, user CRUD, inbound management, cert management, subscription delivery (/sub), Hysteria2 external auth, port rotation, node/cluster inspection and a Prometheus /api/metrics endpoint. Nearly every handler follows the same pattern: parse request → resolve target node(s) via HttpServer.GetTargetNodes → fan out a gRPC request through client.NewEndNodeClient(...).ReqToMultiEndNodeServer → aggregate succ/failed lists into JSON. pkg/http/auth provides JWT issue/parse, bcrypt(SHA256) password handling, JTI blacklist and gin middleware; pkg/http/prometheus_desc provides custom prometheus Collectors fed by cluster RPC.


**文件角色**：

- `pkg/http/init.go` — NewHttpServer + registerRoutes: route table split into public / userGroup (AuthMiddleware) / adminGroup (AuthMiddleware+AdminOnly); node-groups routes only when clusterEnabled
- `pkg/http/http_server.go` — HttpServer struct + HttpServerConfig; Init() wires deps (localNode, ClusterNodes, CertReader, UserLister), Start() runs gin, GetTargetNodes() resolves target param ('all'/name) to cluster nodes; also defines GlobalHttpServer singleton and RegisterHandler
- `pkg/http/http_handler.go` — HttpHandlerInterface + HttpHandlerImp base (parseParam/handlerFunc/getHandlers/getRelativePath/help), embedded by every handler
- `pkg/http/common.go` — jsonOK/jsonErr helpers, joinFailedList (node→err map to string), splitAndFilter
- `pkg/http/target.go` — getTargetFromQuery (default 'all') and resolveTarget (empty → local node name, used for write ops)
- `pkg/http/auth_with_token_handler.go` — getAuthHandlerFunc: legacy X-Token-only middleware, used only by MetricHandler
- `pkg/http/auth_hysteria.go` — AuthHysteria2 POST /api/authHysteria2 (public): matches auth against ListUsersWithPasswd (plain or base64 password); defines the UserLister interface
- `pkg/http/login_handler.go` — LoginHandler POST /api/login: GetUser + VerifyLoginPassword → GenerateJWT; unified 401
- `pkg/http/logout_handler.go` — LogoutHandler POST /api/logout: re-parses Bearer JWT, BlacklistAdd(jti)
- `pkg/http/change_password_handler.go` — ChangePasswordHandler PUT /api/profile/password: verify old password locally, push new via UpdateUsers RPC
- `pkg/http/user_handler.go` — User CRUD: UserAdd/Update/Delete/Reset/ResetTraffic/ResetAuthToken handlers, UserListHandler (admin=all users, normal=own profile), ProfileHandler, shared serveUserProfile; all via UserOpReq/GetProfile RPC
- `pkg/http/set_user_role_handler.go` — SetUserRoleHandler PUT /api/user/:name/role (X-Token break-glass or admin JWT) via UpdateUsers RPC
- `pkg/http/set_user_bandwidth_handler.go` — PUT /user/:name/bandwidth; maps JSON 0→proto -1 sentinel (remove limit)
- `pkg/http/set_user_client_limit_handler.go` — PUT /user/:name/client-limit; same sentinel mapping for max_clients
- `pkg/http/sub_handler.go` — SubHandler GET /sub (public): token or user+pwd auth at HTTP layer, GetSub fan-out, ext_sub merge, format conversion (clash detection), optional Subscription-Userinfo header via extra GetProfile fan-out (writeSubUserInfoHeader); maskToken for audit logging
- `pkg/http/sub_userinfo_format.go` — Subscription-Userinfo header templating: ${var} table (upload/download/total/expire + unit variants), -1 sentinels, resolveSubUserInfoFormat override chain, stripClashEmptyFields
- `pkg/http/rotate_inbound_port.go` — POST /rotateInboundPort: self-service port rotation with X-Token/adminJWT/normalJWT permission matrix
- `pkg/http/rotate_all_ports.go` — POST /rotateAllPorts: rotate all of a user's forward ports, same permission matrix
- `pkg/http/fastAddInbound_handler.go` — POST /inbound/fast: largest handler; protocol whitelist (fastAddValidProtocols), transport normalization/aliases, convenience-field → extra_params merging for ss-plugin/hysteria2/tuic/anytls, FastAddInboundReq RPC
- `pkg/http/bound_handler.go` — InboundAddHandler POST /inbound (raw base64 xray JSON, xray-only) and InboundGetHandler GET /inbound
- `pkg/http/inbound_list_handler.go` — GET /inbounds (cross-container list) and DELETE /inbounds by container+name
- `pkg/http/cert_handler.go` — POST /cert: obtain new cert via ObtainNewCert RPC
- `pkg/http/get_certs_handler.go` — GET /getCerts: cert list fan-out
- `pkg/http/delete_cert_handler.go` — DELETE /cert via DeleteCert RPC
- `pkg/http/tranfer_cert_handler.go` — TransferCertHandler POST /transferCert: reads local cert/key files via CertReader + os.ReadFile and pushes to other nodes (filters out self)
- `pkg/http/update_handler.go` — POST /update: trigger proxy binary update (UpdateProxyReq, version_tag default 'latest')
- `pkg/http/gateway_handler.go` — PUT /gateway: toggle gateway model via RPC
- `pkg/http/check_server_handler.go` — PingCheckHandler PUT /pingCheck: toggle ping checking via SetPingCheckReq
- `pkg/http/node_handler.go` — NodeHandler GET /node: lists cluster nodes, fetches each node's groups in parallel goroutines
- `pkg/http/node_containers_handler.go` — GET /containers: per-node enabled container types
- `pkg/http/node_groups_handler.go` — GET/PUT /node/:name/groups (registered only when clusterEnabled)
- `pkg/http/status_handler.go` — GET /status: per-node NodeStatus (mem/cpu/net/goroutines/version) aggregation
- `pkg/http/copy_user_between_nodes_handler.go` — POST /copyUser: GetUsers from src node, AddUsers to dst node
- `pkg/http/help_handler.go` — GET /help/*relativePath: renders help() strings from handlersMap
- `pkg/http/metric_handler.go` — MetricHandler GET /api/metrics + RegisterPrometheus: per-route prometheus.NewRegistry with 3 collectors; on each scrape spawns 3 goroutines to Update collectors via RPC (1s timeout) then serves promhttp
- `pkg/http/auth/jwt.go` — GenerateJWT (HS256, uuid JTI, default 24h) / ParseJWT (rejects non-HMAC alg)
- `pkg/http/auth/middleware.go` — AuthMiddleware (X-Token→admin priority, else Bearer JWT + blacklist check; empty jwtSecret disables JWT path) and AdminOnly
- `pkg/http/auth/blacklist.go` — in-memory global JTI revocation set (sync.RWMutex), cleared on restart
- `pkg/http/auth/password.go` — HashLoginPassword/VerifyLoginPassword: bcrypt over SHA256-hex normalization (accepts plaintext or pre-hashed sha256 hex)
- `pkg/http/prometheus_desc/common.go` — dto.MetricType→prometheus value type/value mapping, label generation with host/source extra labels
- `pkg/http/prometheus_desc/node_collector_desc.go` — nodeMetricDesc Collector: unmarshals serialized dto.MetricFamily blobs from GetNodeMetric RPC and re-emits with host/source labels
- `pkg/http/prometheus_desc/ping_desc.go` — pingDesc Collector: 6 gauges (max/min/avg/stdev/loss/latest) from GetPingMetric RPC
- `pkg/http/prometheus_desc/v2raymg_traffic_desc.go` — v2raymgTrafficDesc Collector: per-user/per-inbound up/downlink gauges from GetBandwidthStats RPC, node label from RPC response key
- `pkg/http/*_test.go + auth/*_test.go` — tests (not read line-by-line): login/logout/JWT/blacklist/password round-trips, middleware auth matrix, rotate handlers permission matrix, sub_userinfo formatting/sentinels/clash stripping, fastAddInbound protocol/builder alignment, node_groups/set_user_role/user handlers, cluster_user routing; testhelper_test.go provides shared fixtures

**关键流程**：
- Server lifecycle: NewHttpServer() → HttpServer.Init(cfg, localNode, clusterNodes, certReader, userLister, clusterEnabled) → registerRoutes() (public/user/admin groups) → Start() (gin Run, blocking). Caller is cmd/server.go via GlobalHttpServer; RegisterPrometheus adds /api/metrics separately.
- Authenticated request: AuthMiddleware(token, jwtSecret) — X-Token match → role=admin; else Bearer JWT ParseJWT + BlacklistContains check → sets ContextKeyUsername/Role; AdminOnly() gates the admin group. Handlers read identity from gin context.
- Login/logout: LoginHandler.handlerFunc → userLister.GetUser + auth.VerifyLoginPassword → auth.GenerateJWT; LogoutHandler.handlerFunc re-parses the Bearer token and auth.BlacklistAdd(claims.JTI).
- Generic admin op (UserAddHandler.handlerFunc and ~20 siblings): bind JSON → resolveTarget → GetTargetNodes → client.NewEndNodeClient(...).ReqToMultiEndNodeServer(reqType, protoReq, clusterToken) → joinFailedList on error → jsonOK.
- Subscription: SubHandler.handlerFunc — credential resolution (FindUserByToken via tokenFinder interface assertion on userLister, or GetUser+VerifyLoginPassword) → GetSub fan-out to target nodes → merge URIs (+FetchAndMergeExtSubs for ext_sub) → subscription.ConvertURIsWithOptions by user-agent/client hint → optional writeSubUserInfoHeader (second GetProfile fan-out, renderSubUserInfoFormat, stripClashEmptyFields for clash).
- Hysteria2 external auth: AuthHysteria2.handlerFunc (public POST) iterates ListUsersWithPasswd comparing raw and base64(passwd) to the auth field, returns {ok,id}.
- Metrics scrape: MetricHandler.getHandlers → getAuthHandlerFunc (X-Token) then prometheusHandler closure — 3 goroutines call trafficDesc/pingDesc/nodeMetricDesc.Update (RPC fan-out, 1s context timeout), wg.Wait, then promhttp Collect() reads cached slices under RWMutex.
- Port rotation: RotateInboundPortHandler/RotateAllPortsHandler.handlerFunc — three-way caller matrix (X-Token needs body username; normal JWT self-only; admin JWT may target others) → RotateInboundPort/RotateAllPorts RPC.

**依赖**：
- imports: pkg/http/auth, pkg/http/prometheus_desc, pkg/cluster (Node/LocalNode/NodeFilter), pkg/log, pkg/rpc/client (EndNodeClient), pkg/rpc/proto, pkg/proxy/core/contracts (User), pkg/proxy/core/subscription (+blank-import of subscription/converter for registration)
- pkg/http/auth imports only third-party (golang-jwt/v4, google/uuid, x/crypto/bcrypt, gin)
- pkg/http/prometheus_desc imports pkg/rpc/client, pkg/rpc/proto, prometheus client_golang, client_model, protobuf
- imported by: cmd/server.go (only in-repo importer of pkg/http; uses GlobalHttpServer/Init/RegisterPrometheus)

**并发模型**：Mostly request-scoped and delegated to gin. Explicit concurrency: (1) auth/blacklist.go — global JTI set guarded by sync.RWMutex (BlacklistAdd/BlacklistContains). (2) metric_handler.go prometheusHandler — per-scrape 3 goroutines + sync.WaitGroup updating the three collectors with a shared 1s context. (3) Each prometheus_desc collector (nodeMetricDesc, pingDesc, v2raymgTrafficDesc) holds a sync.RWMutex protecting its cached metrics slice: Update() write-locks and swaps, Collect()/Describe() read-lock. (4) node_handler.go — per-node goroutines fetching groups, results into groupsMap under a sync.Mutex + WaitGroup. Shared mutable server state: HttpServer fields are written only in Init() before Start(); handlersMap is not locked (populated at registration time). The fan-out parallelism itself lives in pkg/rpc/client.ReqToMultiEndNodeServer, outside this module.

**外部交互**：
- Network (serve): gin HTTP server on Host:Port (HttpServer.Start)
- Network (client): gRPC fan-out to end nodes via pkg/rpc/client.EndNodeClient in nearly every handler and in prometheus_desc Update()s
- Network (client): SubHandler fetches external subscription URLs via subscription.FetchAndMergeExtSubs (ext_sub param, max 10) and remote Clash templates via the converter
- Disk: tranfer_cert_handler.go os.ReadFile of cert/key paths returned by CertReader.GetCertFiles
- No direct exec/DB; xray/hysteria/snell/mihomo binaries are managed by end nodes reached over RPC, not by this module

**风险关注点（reviewer 重点）**：
- sub_handler.go — the most complex handler: dual auth paths (token vs user+pwd, tokenFinder interface assertion on userLister), always-200 error responses ('invalid user' text with HTTP 200), two RPC fan-outs per request when userinfo header enabled, ext_sub fetching of arbitrary user-supplied URLs, and Info-level logging of full node URIs
- fastAddInbound_handler.go — three-layer protocol/transport alignment (fastAddValidProtocols ↔ getBuilderType ↔ end-node InboundBuilderType) plus a large convenience-field → extra_params merge with per-key overwrite rules; easy to break parity when adding a protocol
- auth/middleware.go + auth_with_token_handler.go — two separate auth mechanisms (new AuthMiddleware vs legacy getAuthHandlerFunc for /api/metrics); X-Token silently grants admin; empty jwtSecret silently disables the JWT path
- auth/blacklist.go — logout revocation is in-memory only and unbounded (no expiry pruning); lost on restart
- metric_handler.go — per-scrape goroutine fan-out with a fixed 1s timeout; Update errors are discarded (succList only), so stale cached metrics are served silently; prometheus registry built per-handler-closure
- node_collector_desc.go Collect — pb.Unmarshal error ignored, and prometheus.NewDesc built per metric from remote-supplied names/labels
- http_server.go GetTargetNodes — 'all' vs name semantics and the nil-vs-empty distinction handlers check inconsistently (some `nodes == nil`, some `len(nodes) == 0`)
- user_handler.go / set_user_*_handler.go — sentinel mapping conventions (JSON 0 → proto -1 remove-limit, absent → 0 no-change) spread across handlers; getExpireTime vs calcExpire duplication
- tranfer_cert_handler.go — reads local key material and ships it to other nodes; self-node filtering via slice reuse nodes[:0]
- Several write handlers return HTTP 200 with code 300 in body on failure (ChangePassword, ResetAuthToken) while others use real 5xx — inconsistent error contract for reviewers to track


## [L5] CMD — cmd + cmd/cli + main.go

**职责**：v2raymg 二进制的入口层。main.go 调用 cmd.Execute() 运行 cobra 根命令，下挂两个子命令：`server`（按 node_type 启动 center 或 end 节点，end 节点在 startServer/runEndNode 中完成全部子系统的依赖装配与生命周期管理）和 `cli`（基于 lureiny/go-prompt 的交互式 REPL，通过 HTTP API 管理整个集群：节点/用户/inbound/证书/端口轮换/会话等）。cmd/cli/client 是纯 HTTP 客户端封装，cmd/cli/common 是 API 路径常量。


**文件角色**：

- `main.go` — 程序入口，调用 cmd.Execute()，出错打印 stderr 并 exit(1)
- `cmd/root.go` — cobra rootCmd 定义与 Execute()；init 注册 serverCmd、cliCmd
- `cmd/version.go` — buildinfo 的向后兼容 accessor（GetVersion/GetCommit/GetBuildTime）
- `cmd/server.go` — server 子命令：--conf/--migrate flag、printBanner、startServer 分流 runCenterNode/runEndNode；runEndNode 是 end 节点全部子系统的装配与启动序列（约 200 行）
- `cmd/cli.go` — cli 子命令：LoadConfig → InitPromptAndRegister → prompt.Run()，出错 panic
- `cmd/cli/config.go` — CLI 配置（host/token/username/password，YAML，默认 .v2raymg-tools.yaml）；交互式 inputConfig；getHost URL 归一化；jwtCache + getAuthToken（JWT 登录、60 秒提前刷新、失败回退静态 token）、ClearJWTCache
- `cmd/cli/info.go` — REPL 本地缓存：localNodeList/localUserList/localInboundList 三个 map + 各自 mutex，updateLocalXxx 拉取刷新；updateCycle 变量已声明但仓库内无引用
- `cmd/cli/command.go` — InitPromptAndRegister 注册 ~30 个 REPL handler（ListNode/ApplyCert/FastAddInbound/AddUser/RotateAllPorts/GetStatus/Logout 等），每个 handler 是薄封装：getHost+getAuthToken → client.Xxx → 打印；含 formatBytes/formatSpeed 及 fastAddInbound 的 tag/port 默认值生成
- `cmd/cli/suggest.go` — REPL 自动补全：几十个 prompt.Suggest 定义、GetSuggest 解析输入行，按最后一个 flag 分派到 getNodeSuggest/getUserSuggest/getContainerSuggest/getInboundNameSuggest（读 info.go 缓存）；knownContainers 硬编码 [xray snell hysteria mihomo]
- `cmd/cli/client/base_client.go` — 通用 HTTP 层：DoGet/Post/Put/DeleteRequest（30s 超时、filepath.Clean 路径）、setAuthHeader（Bearer→Authorization，否则 X-Token）、HttpCallback 类型
- `cmd/cli/client/http_client.go` — 约 35 个 API 封装函数（Login/ListNode/ListCert/FastAddInbound/AddUser/UpdateUser/RotateInboundPort/SetUserBandwidth/Logout/GetStatus 等），每个拼 URL+body 并解析响应；FastAddInboundRequest 大结构体
- `cmd/cli/common/const.go` — HTTP API 路径常量（api/login、api/inbound/fast、api/user 等）
- `cmd/helloworld/main.go` — 7 行 hello-world 占位程序，与业务无关
- `cmd/test_hotreload/main.go` — //go:build ignore 手工测试脚本：起两个 xray 实例验证 add-inbound 热重载，非编译单元
- `cmd/server_cluster_user_test.go` — 测试：cluster_user 启动装配（enable/disable、node groups seed、BackfillClusterFields）
- `cmd/cli/config_test.go` — 测试：getAuthToken JWT 缓存命中/临期刷新/登录失败回退/ClearJWTCache
- `cmd/cli/client/http_client_test.go` — 测试：setAuthHeader 三分支、Rotate*/SetUser* 请求体与方法、Logout/Profile Bearer、非 200 错误传播（httptest）

**关键流程**：
- server 启动: main → cmd.Execute → startServer(cmd/server.go) → appconfig.LoadFromFile → (--migrate 时 SaveToFile 后直接返回) → appconfig.Validate → log.SetDefault → 按 cfg.NodeType 分流 runCenterNode 或 runEndNode
- runCenterNode: server.CenterNodeServer{}.Init(cfg.CenterNode) → Start()（阻塞，center 分支到此为止）
- runEndNode 装配序列(顺序敏感): store.NewStoreManager → forward.NewDefaultForwardManager → storeMgr.InitLoginPasswords(auth.HashLoginPassword，必须先于 UserManager) → usermanager.NewUserManagerWithStore → cluster.NewEndNodeClusterManagerFromConfig → certmgmtservice.NewManager + certMgr.StartAutoRenew(ctx) → container.NewContainerMgr + LoadFromConfig → subscription.NewManager → 两个 collector → rpcServer.Init；若 cfg.ClusterUser.Enabled 则 EnableClusterSync + node groups seed + BackfillClusterFields
- runEndNode 运行期: go rpcServer.Start() → go httpServer.Start()（含可选 RegisterPrometheus）→ userMgr.StartTrafficStats(0)/StartMaintenance(0) → containerMgr.StartAll(ctx) → 阻塞等 SIGTERM/SIGINT → cancel() → containerMgr.StopAll()
- cli 启动: cliFunc(cmd/cli.go) → clipkg.LoadConfig（读/写 .v2raymg-tools.yaml，缺失时 inputConfig 交互输入并回写）→ InitPromptAndRegister（先 updateLocalNodeList/UserList/InboundList 预热缓存，再注册 ~30 handler）→ prompt.Run() REPL 循环
- REPL 命令执行: 用户输入 → go-prompt 反射调用 handler（如 fastAddInbound/addUser）→ getAuthToken()（jwtCache 命中或 client.Login 换 JWT，失败回退静态 token）→ client.DoXxxRequest（base_client.go，30s 超时）→ 打印结果；错误经 handlerCallback 反射打印
- 补全流: GetSuggest(suggest.go) → prompt.DefaultGetHandlerSuggests 无结果时解析输入行最后一个 flag → getNodeSuggest/getUserSuggest/getInboundNameSuggest 读 info.go 本地缓存（缓存只在 REPL 启动时预热一次）
- 登出流: logout(command.go) → client.Logout → 成功后 ClearJWTCache() 使下一条命令重新登录

**依赖**：
- cmd 导入: pkg/buildinfo, pkg/appconfig(=pkg/proxy/appconfig), pkg/certmgmt/service, pkg/cluster, pkg/collecter, pkg/http, pkg/http/auth, pkg/log, pkg/proxy/core/{container,subscription}, pkg/proxy/forward, pkg/proxy/usermanager, pkg/rpc/server, pkg/store, pkg/store/migrations, cmd/cli；blank import 注册: pkg/proxy/containers/{xray,snell,hysteria,mihomo}, pkg/proxy/core/subscription/converter
- cmd/cli 导入: cmd/cli/client, pkg/cluster, pkg/rpc/proto, github.com/lureiny/go-prompt, gopkg.in/yaml.v3
- cmd/cli/client 导入: cmd/cli/common, pkg/cluster, pkg/rpc/proto
- 被谁导入: 只有 main.go 导入 cmd；仓库内其他包不导入 cmd/*（helloworld/test_hotreload 是独立 main）

**并发模型**：runEndNode: rpcServer.Start 和 httpServer.Start 各跑在一个 goroutine，主 goroutine 阻塞在 sigCh channel（signal.Notify SIGTERM/SIGINT）；context.WithCancel 贯穿 certMgr.StartAutoRenew 与 containerMgr.StartAll，收到信号后 cancel()+StopAll()。cmd/cli: info.go 三个 sync.Mutex（nodeMutex/userMutex/inboundMutex）分别保护 localNodeList/localUserList/localInboundList 缓存 map（写在 updateLocalXxx 和 listNode，读在 suggest.go 的 getXxxSuggest）；config.go 的 jwtCache.mu 保护 JWT token+expire 缓存（getAuthToken 在持锁状态下发起 Login HTTP 请求）。getNode(info.go) 读 localNodeList 不加锁。CLI 本身单线程 REPL，无后台刷新 goroutine（updateCycle 变量未被使用）。

**外部交互**：
- cmd/server.go(runEndNode): SQLite（store.NewStoreManager, cfg.Store.DSN）、磁盘配置文件读写（appconfig.LoadFromFile/SaveToFile）、通过 containerMgr 间接管理 xray/snell/hysteria/mihomo 外部二进制进程、gRPC server、HTTP server、可选 Prometheus、ICMP/TCP ping collector、ACME 证书自动续期（certMgr.StartAutoRenew）
- cmd/cli: 全部经 HTTP(S) 调用 v2raymg 节点的 REST API（base_client.go，30s 超时）；.v2raymg-tools.yaml 配置读写（0666）；stdin 交互输入（inputConfig，密码明文回显并明文落盘）
- cmd/test_hotreload: 直接 exec /tmp/xray-bin/xray 并连其 gRPC API（手工脚本）

**风险关注点（reviewer 重点）**：
- cmd/server.go runEndNode 的初始化顺序是显式契约（注释标 1-13 步）：InitLoginPasswords 必须在 NewUserManagerWithStore 之前、containerMgr 必须在 certMgr 之后；reviewer 改动装配顺序需逐条核对
- cmd/server.go --migrate 分支跳过 appconfig.Validate 写回配置文件（注释解释是有意的），是配置格式变更时的敏感路径
- cmd/cli/config.go getAuthToken 在持 jwtCache.mu 期间做 30s 超时的 HTTP Login；以及 LoadConfig/inputConfig 把密码明文写入 0666 权限的 yaml
- cmd/cli/info.go getNode 读 localNodeList 无锁，而 listNode(command.go) 持 nodeMutex 重新赋值该 map；缓存三张表只在 REPL 启动时预热一次，updateCycle 未接线
- cmd/cli/suggest.go GetSuggest 的输入行手工解析（空格 split、lastFlag 匹配、getParamValue）状态多且脆弱，与 go-prompt 的 SuggestPrefix 约定强耦合
- cmd/cli/command.go fastAddInbound 与 client.FastAddInbound 的 ~40 个参数手工一一映射（位置参数→结构体→map key），新增协议字段（hysteria2/tuic/anytls）时容易漏字段或 key 拼错；tag 用时间戳 sha256、端口用 rand.Intn(40000)+10000 随机生成
- cmd/cli/suggest.go knownContainers 硬编码，注释要求与 pkg/proxy/core/contracts 常量手工保持同步
- cmd/cli/client/base_client.go 对 URL path 用 filepath.Clean（OS 路径语义清理 URL 路径），GET/JSON 两条路径都经过它


## [L6] TEST — pkg/proxy/systemtest

**职责**：Test-only package (no non-test .go files; almost everything behind the `integration` build tag) that validates the refactored proxy framework end-to-end against real external kernels: xray, mihomo, hysteria, snell, plus the compiled v2raymg binary itself. Typical flow: boot a real container (or the full v2raymg server subprocess), FastAdd a protocol inbound, add a user through the production UserEvent→ForwardManager pipeline, then drive real traffic (Go http.Client via SOCKS5, or a second mihomo process as protocol client) through the forward port to a local httptest origin or a public upstream, and assert connectivity plus negative controls (stop server → traffic must fail). It also exercises restart/restore, port allocation/release, and the real subscription pipeline (GET /sub → codec.Decode → clash proxy map → mihomo client).


**文件角色**：

- `pkg/proxy/systemtest/README.md` — Test package doc: purpose, env vars (XRAY_BIN/MIHOMO_BIN), run commands, test→example mapping
- `pkg/proxy/systemtest/helpers_test.go` — Shared helpers: noopForwardManager stub, newTestUserManager, freeTCPPort, waitPort, httpClientViaSocks5, writeTempCert (self-signed via xray.GenerateSelfSignedCert), checkNetwork
- `pkg/proxy/systemtest/mihomo_helpers_test.go` — Mihomo rig: ensureMihomoBin (MIHOMO_BIN env or Updater downloads Alpha release), startMihomoContainer (real ForwardManager+UserManager+SQLite store), addMihomoUserAndWaitForPort / removeMihomoUserAndWaitForPortRelease polling helpers
- `pkg/proxy/systemtest/public_upstream_test.go` — canReachPublicUpstream probe (google generate_204 + fallbacks, sync.Once cached) and spawnMihomoClient — the single place that runs a mihomo process as a protocol client; plus TestPublicUpstreamFrameworkSmoke
- `pkg/proxy/systemtest/e2e_server_helpers_test.go` — Full-server E2E infra: ensureV2raymgBin (go build once), generateE2EConfig (all-containers YAML), e2eServer subprocess lifecycle + HTTP API wrappers (addUser, fastAddInbound, getRawSub), uriToClashProxy converting codec nodes (vless/vmess/trojan/ss/hy2/tuic/anytls/snell) to clash proxy maps
- `pkg/proxy/systemtest/e2e_server_test.go` — TestE2EAllContainersProtocolMatrix: compile v2raymg → start server → HTTP addUser/fastAdd → GET /sub → codec parse → mihomo client → traffic → stop server negative control
- `pkg/proxy/systemtest/degraded_socks5_chain_test.go` — Only test without integration tag: TestDegradedLocalSocks5ProxyChain with a hand-written minimal in-process SOCKS5 relay, no binaries/network needed
- `pkg/proxy/systemtest/hysteria_udp_forward_test.go` — TestHysteriaUDPForwardIntegration: hysteria client → forward.UDPRelay → hysteria server → internet, incl. stop-container negative control; needs HYSTERIA_BIN; duplicates hysteria's unexported default inbound tag
- `pkg/proxy/systemtest/mihomo_e2e_test.go` — TestMihomoE2E_RealInternet (vmess/trojan/ss cases, server via FastAdd, client via hand-written mihomo yaml, credential-change step) and TestMihomoE2E_FillDefaults_TrojanSelfSigned; uses fillparams package
- `pkg/proxy/systemtest/mihomo_protocol_matrix_test.go` — TestMihomoProtocolMatrix (MVP D4): vmess/trojan/ss FastAdd + user handshake + port-release negative control, xray as protocol client
- `pkg/proxy/systemtest/mihomo_restore_test.go` — TestMihomoRestore_RecoversInboundAndUser: Stop→Start cycle, listener/forward-port/connectivity recovery (Restore + reconcileUsers against real wiring)
- `pkg/proxy/systemtest/mihomo_vless_matrix_test.go` — TestMihomoVLESSMatrix Phase 1: vless transport×security matrix, mihomo client→forward→mihomo server→public upstream
- `pkg/proxy/systemtest/mihomo_vmess_matrix_test.go` — TestMihomoVMessMatrix Phase 2: 7-case vmess matrix (tcp/ws/grpc × none/tls/reality)
- `pkg/proxy/systemtest/mihomo_trojan_matrix_test.go` — TestMihomoTrojanMatrix Phase 3: trojan transport/security matrix
- `pkg/proxy/systemtest/mihomo_ss_matrix_test.go` — TestMihomoSSMatrix Phase 4: shadowsocks cipher matrix incl. SIP022 2022-blake3 keys (randBase64Key)
- `pkg/proxy/systemtest/mihomo_hy2_matrix_test.go` — TestMihomoHy2Matrix Phase 5: hysteria2 matrix; waitUDPListener readiness helper for QUIC/UDP ports
- `pkg/proxy/systemtest/mihomo_tuic_matrix_test.go` — TestMihomoTuicMatrix Phase 6: TUIC (bbr/cubic, reduce-rtt) + mandatory subscription_chain subtest via GetUserSubscriptions→ClashConverter.convertTuic
- `pkg/proxy/systemtest/mihomo_anytls_matrix_test.go` — TestMihomoAnyTLSMatrix Phase 7: anytls end-to-end matrix
- `pkg/proxy/systemtest/xray_dynamic_inbound_system_test.go` — Xray container tests: TestXrayDynamicSocks5Inbound, _VerifyProcessRunning, _MultipleInbounds, _RemoveInbound, TestXrayInboundConfig_GenericAPI, TestXraySocks5ToLocalhost
- `pkg/proxy/systemtest/xray_fastadd_connectivity_test.go` — TestFastAddConnectivity: 14 xray FastAdd protocol combos (vless/vmess/trojan… incl. TLS and Reality cases) with real connectivity — the current authoritative xray coverage
- `pkg/proxy/systemtest/xray_e2e_protocol_test.go` — FIXME/stale: uses removed contracts.UserSpec fields (Email/Protocol/Extensions/Level/Password); superseded by xray_fastadd_connectivity_test.go
- `pkg/proxy/systemtest/xray_protocol_connectivity_test.go` — FIXME/stale: same stale UserSpec/InboundSpec fields; superseded by xray_fastadd_connectivity_test.go
- `pkg/proxy/systemtest/xray_protocol_matrix_test.go` — FIXME/stale: protocol matrix + TestXrayDynamicInbound_Concurrent (10 concurrent inbound adds via goroutines/errCh) + add/remove cycle; superseded per FIXME header

**关键流程**：
- Mihomo matrix lifecycle (all TestMihomo*Matrix): ensureMihomoBin → startMihomoContainer (real ForwardManager+UserManager+StoreManager+MihomoContainer.Start, waits /version) → FastAddInbound → addMihomoUserAndWaitForPort (AddUser → UserEventAdd → container handler → GetBindPort, polled via GetUserPortByDst) → spawnMihomoClient (second mihomo process, mixed-port inbound) → httpClientViaSocks5 → public upstream; teardown via t.Cleanup (Stop/Close container, forwardMgr.Close, store Close).
- Full-server E2E (TestE2EAllContainersProtocolMatrix, e2e_server_test.go): ensureV2raymgBin (go build once, sync.Once) → generateE2EConfig (xray+mihomo+optional hysteria/snell YAML) → startE2EServer (exec v2raymg server, waitReady polls /api/status) → addUser + fastAddInbound over HTTP → getRawSub/getNewURI (base64 /sub) → uriToClashProxy (codec.Decode) → spawnMihomoClient → traffic → srv.shutdown (SIGTERM) → negative-control curl must fail.
- Public upstream gating: canReachPublicUpstream (public_upstream_test.go) probes https://www.google.com/generate_204 with fallbacks (cloudflare, apple), result cached in sync.Once; matrix tests t.Skip when unreachable instead of falsely passing.
- User port release (removeMihomoUserAndWaitForPortRelease): RemoveUser → UserEventRemove → container ReleaseBindPort → poll forwardMgr.GetRule("mihomo:<tag>:<user>") == nil within 5s.
- Restart restore (TestMihomoRestore_RecoversInboundAndUser): container.Stop → Start → verify listener reappears, user forward port stable, connectivity restored via xray client.
- Hysteria UDP chain (TestHysteriaUDPForwardIntegration): HysteriaContainer.Start → AddUser → forward.UDPRelay on public port → real hysteria2 client SOCKS5 → internet; container stop must break the chain.
- Xray dynamic inbound (TestXrayDynamicSocks5Inbound etc.): start XrayContainer → dynamically add socks5/other inbound → connect through it; TestFastAddConnectivity runs the 14-combo FastAdd matrix incl. Reality.
- Degraded fallback (TestDegradedLocalSocks5ProxyChain, no build tag): in-process minimal SOCKS5 server (startMinimalSocks5Server, per-connection goroutines) → httptest origin, runs in restricted envs.

**依赖**：
- imports: pkg/proxy/containers/xray
- imports: pkg/proxy/containers/mihomo
- imports: pkg/proxy/containers/hysteria
- imports: pkg/proxy/core/container (UpdateRequest/RestartPolicy)
- imports: pkg/proxy/core/contracts
- imports: pkg/proxy/core/params (fill defaults)
- imports: pkg/proxy/core/subscription/codec (URI decode)
- imports: pkg/proxy/core/subscription/converter (ClashConverter subscription-chain subtests)
- imports: pkg/proxy/forward
- imports: pkg/proxy/usermanager
- imports: pkg/store + pkg/store/migrations
- third-party: golang.org/x/net/proxy, gopkg.in/yaml.v3, github.com/google/uuid
- imported-by: nothing — test-only package; converter/clash.go only mentions it in a comment

**并发模型**：Light and test-local. e2e_server_helpers_test.go: sync.Once (v2raymgBinOnce) guards the one-time `go build`; e2eServer.mu (sync.Mutex) makes shutdown/SIGTERM idempotent across t.Cleanup and explicit stop-server negative controls. public_upstream_test.go: sync.Once (upstreamOnce) caches the public-upstream reachability probe process-wide. xray_protocol_matrix_test.go TestXrayDynamicInbound_Concurrent: 10 goroutines adding inbounds concurrently, results collected via buffered errCh (chan error). degraded_socks5_chain_test.go and hysteria_udp_forward_test.go spawn per-listener/serve goroutines (go srv.Serve(ln), go func accept loop). Everything else is polling loops (waitPort, waitUDPListener, addMihomoUserAndWaitForPort) with time.Sleep, no shared mutable state between tests beyond the sync.Once caches.

**外部交互**：
- exec of external binaries: xray (XRAY_BIN, as server and as protocol client), mihomo (MIHOMO_BIN or downloaded, both server container and spawnMihomoClient client process), hysteria2 (HYSTERIA_BIN), snell-server, and the compiled v2raymg binary itself (go build + exec, SIGTERM shutdown)
- network egress: mihomo Updater downloads Alpha prerelease + checksums.txt from GitHub when MIHOMO_BIN unset (3 min budget); public upstream probes to google.com/generate_204, cloudflare.com, apple.com; checkNetwork GETs google.com; matrix traffic exits to real internet
- disk: t.TempDir configs (YAML/JSON), self-signed TLS certs via writeTempCert, SQLite store DB files (store.NewStoreManager with migrations.All), mihomo dataDir (SAFE_PATHS constraint: referenced files must live under -d dir)
- local sockets: freeTCPPort/freeUDPPort bind-and-release allocation; forward-port pool 40000-50000; httptest origins; v2raymg HTTP API (X-Token auth) and mihomo external-controller REST

**风险关注点（reviewer 重点）**：
- Three stale files with FIXME headers referencing removed contracts.UserSpec/InboundSpec fields — xray_e2e_protocol_test.go, xray_protocol_connectivity_test.go, xray_protocol_matrix_test.go — declared superseded by xray_fastadd_connectivity_test.go; reviewers should check whether they even compile under -tags=integration and whether the concurrent test's coverage was really replaced.
- freeTCPPort/freeUDPPort bind-then-close allocation is inherently racy (port can be reclaimed before the process binds it) and is used everywhere, incl. e2e_server_helpers_test.go allocating 4+ ports per server; matters for CI flakiness.
- Network-dependent gates: ensureMihomoBin's GitHub download path (mihomo_helpers_test.go) and canReachPublicUpstream fallback logic (public_upstream_test.go) decide skip-vs-fail; the skip semantics differ between files (mihomo_e2e_test.go deliberately fails fast on unreachable MIHOMO_E2E_TARGET while matrix tests skip) — easy to regress.
- Duplicated private constants: hysteria_udp_forward_test.go re-declares hysteria's unexported default inbound tag; mihomo helpers hard-code the forward rule key format "mihomo:<tag>:<user>" — both break silently if the source package renames.
- e2e_server_helpers_test.go generateE2EConfig hand-mirrors the production YAML schema (container config keys, end_node fields); schema drift in config parsing won't be caught at compile time.
- uriToClashProxy + eight *NodeToProxy converters (e2e_server_helpers_test.go:616-938) hand-map codec fields to clash options per protocol — the most detail-dense, drift-prone code in the package; must stay byte-consistent with converter/clash.go and mihomo schemas.
- Polling/timeout constants (5s port waits, 60s server ready, 3 min download) tuned for CI; the sync.Once-cached go build and upstream probe share state across parallel tests in one process.
