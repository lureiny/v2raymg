# TASKS.md — 任务列表

## Phase 1 — 领域模型 + 统一错误码

### T-001: 定义领域模型
- 状态: DONE
- 优先级: P0
- 描述: 定义 InboundSpec, UserSpec, ContainerModel, Protocol/Transport/Security 枚举
- 文件: `pkg/proxyrefactor/domain/`
- 验收标准:
  - InboundSpec 包含 ID/Tag/Port/Protocol/Transport/Security/Policy 等字段
  - UserSpec 包含 Email/UUID/Level/Protocol 等字段
  - 统一枚举类型可在 domain 层独立编译
  - 字段覆盖现有 proxy/manager 和 proxy/config 的业务语义
- 测试: domain_test.go 验证模型创建/验证/枚举 String()

### T-002: 定义统一错误码
- 状态: DONE
- 优先级: P0
- 描述: 定义 pkg/proxyrefactor/errors 统一错误类型
- 文件: `pkg/proxyrefactor/errors/`
- 验收标准:
  - ErrInvalidInboundSpec / ErrFeatureNotSupported / ErrNativeRenderFailed / ErrContainerNotRunning 等
  - 支持 Wrap/Unwrap/Is
  - 不依赖外部包
- 测试: errors_test.go 验证 Is/Wrap/Error()

### T-003: 定义核心接口 (Container/InboundAdapter/StatsClient)
- 状态: DONE
- 优先级: P0
- 描述: 定义 Container、InboundAdapter、StatsClient 接口
- 文件: `pkg/proxyrefactor/container/interface.go`, `pkg/proxyrefactor/provider/interface.go`
- 验收标准:
  - Container 接口: Start/Stop/Restart/Reload/IsRunning/Version
  - InboundAdapter 接口: MapInbound/ValidateInbound
  - StatsClient 接口: QueryStats
  - 接口只依赖 domain 层类型
- 测试: 接口定义无需单独测试（实现时测试）

## Phase 2 — Container 层

### T-004: 实现 ExecRunner
- 状态: DONE
- 优先级: P0
- 描述: 实现基于 os/exec 的进程管理器
- 文件: `pkg/proxyrefactor/container/exec_runner.go`
- 验收标准:
  - 支持 xray/v2ray/hysteria 进程启停
  - 版本检测 (解析 stdout)
  - 优雅停止 (signal)
  - 配置文件路径管理
- 测试: exec_runner_test.go (mock exec)

### T-005: 实现 ConfigWriter
- 状态: DONE
- 优先级: P1
- 描述: 将渲染后的 JSON 配置写入文件 + 触发 reload
- 文件: `pkg/proxyrefactor/container/config_writer.go`
- 验收标准:
  - 原子写入 (temp + rename)
  - 自动刷新机制 (周期/脏标记)
- 测试: config_writer_test.go (文件系统操作)

## Phase 3 — Provider 层

### T-006: 实现 xray InboundAdapter
- 状态: DONE
- 优先级: P0
- 描述: 将 InboundSpec 映射为 xray 原生 inbound JSON
- 文件: `pkg/proxyrefactor/provider/xray/inbound_adapter.go`
- 验收标准:
  - 支持 vmess/vless/trojan/shadowsocks 协议映射
  - 支持 tcp/ws/grpc/mkcp/quic/http 传输映射
  - 支持 tls/xtls/reality 安全配置映射
  - 不支持项返回 ErrFeatureNotSupported
- 测试: inbound_adapter_test.go

### T-007: 实现 v2ray InboundAdapter
- 状态: 取消（按需求）
- 优先级: P1
- 描述: 将 InboundSpec 映射为 v2ray 原生 inbound JSON
- 文件: `pkg/proxyrefactor/provider/v2ray/inbound_adapter.go`

### T-008: 实现 ConfigRenderer (xray)
- 状态: DONE
- 优先级: P0
- 描述: 将 ContainerModel(inbounds+routing+policy) 渲染为完整 JSON 配置
- 文件: `pkg/proxyrefactor/provider/xray/config_renderer.go` (+ v2ray)
- 验收标准:
  - 输入 ContainerModel，输出完整可用 JSON
  - 包含 API inbound 自动注入
  - 包含 routing/policy/stats 默认配置
- 测试: config_renderer_test.go

## Phase 4 — gRPC API 客户端

### T-009: 实现 xray gRPC 客户端
- 状态: 取消（按需求）
- 优先级: P0
- 描述: 独立实现 xray HandlerService + StatsService gRPC 客户端
- 文件: `pkg/proxyrefactor/container/grpc_client.go`
- 验收标准:
  - AddInbound/RemoveInbound/AddUser/RemoveUser
  - QueryStats (user/inbound/outbound)
  - 连接管理 (lazy connect/reconnect)
- 测试: grpc_client_test.go (mock server)

### T-010: 实现 stats 查询统一接口
- 状态: 取消（按需求）
- 优先级: P1
- 描述: 统一 xray + hysteria stats 查询
- 文件: `pkg/proxyrefactor/container/stats.go`

## Phase 5 — 用户管理 + Reconcile

### T-011: 实现 UserManager
- 状态: 取消（按需求）
- 优先级: P0
- 描述: 用户 CRUD (基于 InboundSpec + gRPC runtime)
- 文件: `pkg/proxyrefactor/usermanager/`

### T-012: 实现 InboundService
- 状态: 取消（按需求）
- 优先级: P0
- 描述: Inbound CRUD (基于 InboundSpec → adapter → container)
- 文件: `pkg/proxyrefactor/inboundservice/`

### T-013: 实现 Reconcile 编排
- 状态: 取消（按需求）
- 优先级: P1
- 描述: 配置变更 → diff → apply 的编排逻辑
- 文件: `pkg/proxyrefactor/reconcile/`

## Phase 6 — 端口转发层

### T-014: 转发层领域模型 + ForwardManager 接口
- 状态: DONE
- 优先级: P0
- 描述: 定义 ForwardRule / ForwardManager 接口 / TrafficStats 模型
- 文件: `pkg/proxyrefactor/forward/types.go`, `pkg/proxyrefactor/forward/manager.go`
- 验收标准:
  - ForwardRule: UserEmail + InboundTag + ListenPort + TargetAddr + RateLimit 配置
  - ForwardManager 接口: AddRule/RemoveRule/GetRule/GetRulesByInbound/GetRulesByUser/Stats/Close
  - 接口只依赖 domain 层类型 + forward 内部类型
- 测试: types_test.go

### T-015: 端口分配器 (PortAllocator)
- 状态: DONE
- 优先级: P0
- 描述: 实现范围端口分配器，支持分配/回收/冲突检测
- 文件: `pkg/proxyrefactor/forward/port_allocator.go`
- 验收标准:
  - 支持指定端口范围 (min-max)
  - 排除已占用端口集合 (reserved)
  - Allocate() 返回可用端口或 error
  - Release() 回收端口
  - IsAllocated() 查询
  - 并发安全 (sync.Mutex)
- 测试: port_allocator_test.go（分配/回收/耗尽/并发）

### T-016: TCP Relay 核心实现
- 状态: DONE
- 优先级: P0
- 描述: 实现 TCP 双向转发，支持优雅关闭 + 流量计数钩子
- 文件: `pkg/proxyrefactor/forward/relay.go`
- 验收标准:
  - 监听指定端口，接受连接后 dial 目标地址
  - 双向 io.Copy + 流量字节计数
  - 支持 context 取消 / 优雅关闭（drain 现有连接）
  - 连接数上限保护
- 测试: relay_test.go（本地 loopback 端到端）

### T-017: 流量统计 (TrafficCounter)
- 状态: DONE
- 优先级: P0
- 描述: 原子流量计数器，按 user + inbound 聚合
- 文件: `pkg/proxyrefactor/forward/traffic.go`
- 验收标准:
  - 原子 Add/Get/Reset（sync/atomic）
  - 按 UserEmail 查询 uplink/downlink
  - 按 InboundTag 聚合
  - Snapshot() 返回当前快照 + 可选重置
- 测试: traffic_test.go（并发写入一致性）

### T-018: 限速器 (RateLimiter)
- 状态: DONE
- 优先级: P0
- 描述: 令牌桶限速器，应用于 relay 的 Read/Write 路径
- 文件: `pkg/proxyrefactor/forward/ratelimit.go`
- 验收标准:
  - 可配置 bytes/sec 速率
  - 支持突发容量 (burst)
  - LimitedReader / LimitedWriter 包装 io.Reader/io.Writer
  - 0 或负值表示不限速（透传）
  - 纯标准库实现
- 测试: ratelimit_test.go（速率验证 + 不限速透传）

### T-019: ForwardManager 完整实现
- 状态: DONE
- 优先级: P0
- 描述: 实现 ForwardManager（全局共享实例），管理所有转发规则生命周期
- 文件: `pkg/proxyrefactor/forward/forward_manager.go`
- 验收标准:
  - 组合 PortAllocator + Relay + TrafficCounter + RateLimiter
  - AddRule: 分配端口 → 启动 relay → 注册统计
  - RemoveRule: 停止 relay → 回收端口 → 清理统计
  - GetStats: 按 user/inbound 查询流量
  - Close: 停止所有 relay + 回收所有端口
  - 并发安全 (sync.RWMutex)
- 测试: forward_manager_test.go（完整 CRUD 生命周期 + 并发）

### T-020: 与 Container/InboundSpec 集成接口
- 状态: DONE
- 优先级: P1
- 描述: 定义 Container 和 InboundSpec 初始化时注入 ForwardManager 的接口
- 文件: `pkg/proxyrefactor/forward/integration.go`
- 验收标准:
  - ForwardAwareContainer 包装 Container + ForwardManager
  - InboundForwarder 为一个 inbound 管理其所有 user 的转发规则
  - 端到端测试：创建 inbound → 添加 user → 转发端口可连接 → 统计正确
- 测试: integration_test.go

## Phase U — Container Update 接口

### T-021: Update 抽象接口与类型
- 状态: DONE
- 优先级: P0
- 描述: 新增 UpdateRequest/UpdateResult/Updater 接口，并扩展 Container
- 文件: `pkg/proxyrefactor/container/interface.go`, `update_types.go`
- 验收标准:
  - 支持 TargetTag/RestartPolicy/DryRun/SourceConfig
  - 返回 FromVersion/ToVersion/Tag/ChecksumVerified/Restarted
- 测试: update_types_test.go

### T-022: GitHub Release 客户端与资产选择
- 状态: DONE
- 优先级: P0
- 描述: 实现 release 拉取、tag 标准化、xray 资产选择
- 文件: `pkg/proxyrefactor/container/github_release_client.go`
- 验收标准:
  - 支持 owner/repo 可配置
  - 支持按 tag 拉 release
  - 资产选择支持 xray linux amd64
- 测试: github_release_client_test.go

### T-023: 下载与 checksum 校验
- 状态: DONE
- 优先级: P0
- 描述: 下载 release 资产并进行 sha256 校验
- 文件: `pkg/proxyrefactor/container/downloader.go`, `checksum.go`
- 验收标准:
  - 下载失败可识别
  - checksum mismatch 返回错误
- 测试: checksum_test.go

### T-024: 原子替换与回滚
- 状态: DONE
- 优先级: P0
- 描述: 实现 binary 原子替换、失败回滚
- 文件: `pkg/proxyrefactor/container/swapper.go`, `rollback.go`
- 验收标准:
  - rename 原子替换
  - 替换失败可回滚
  - 回滚失败有清晰错误
- 测试: swapper_test.go

### T-025: ExecRunner.Update 集成
- 状态: DONE
- 优先级: P0
- 描述: 在 ExecRunner 集成 Update 流程与进程协同策略
- 文件: `pkg/proxyrefactor/container/updater.go`
- 验收标准:
  - 更新前后 stop/restart 策略正确
  - 成功后版本更新可见
  - 失败后回滚且可恢复
- 测试: updater_test.go

### T-026: Phase U 残留风险修复（QA FAIL 闭环）
- 状态: DONE
- 优先级: P0
- 描述: 按 QA FAIL 项做可测试性重构 + mock 全覆盖
- 文件: `pkg/proxyrefactor/container/{updater.go,github_release_client.go,*_test.go}`
- 验收标准:
  - ExecRunner.Update 依赖接口化：ReleaseClient/Downloader/Swapper/ProcessController
  - GitHubReleaseClient 支持 BaseURL（可指向 httptest）
  - 表驱动测试覆盖：下载失败/checksum pass-fail/swap fail/restart fail->rollback/rollback fail/restart policy
  - 版本变化断言 FromVersion/ToVersion
  - running/non-running 调用顺序断言
- 测试: updater_test.go（table-driven + mocks）

## Phase S — 简单系统测试（框架可用性）

### T-027: xray+socks5 系统测试与降级方案
- 状态: DONE
- 优先级: P0
- 描述: 编写可重复执行系统测试（真实集成）+ 环境受限降级验证
- 文件: `pkg/proxyrefactor/systemtest/`
- 验收标准:
  - 真实方案：启动 xray container，使用 socks5 inbound 访问网站
  - 降级方案：本地最小 socks5 链路验证（无 xray/无外网）
  - 明确成功/失败判据
  - 不改旧代码
- 测试:
  - `TestXrayContainerSocks5WebsiteAccess`（integration tag + XRAY_BIN）
  - `TestDegradedLocalSocks5ProxyChain`（默认可运行）

### T-028: Start 自动拉取 xray 二进制
- 状态: DONE
- 优先级: P0
- 描述: xray container Start 时若 binary 缺失/不可执行，自动 Update(latest) 后继续启动
- 文件: `pkg/proxyrefactor/container/{exec_runner.go,updater.go,*_test.go}`
- 验收标准:
  - 仅 xray 生效
  - Start 流程包含：检测 -> 自动 update -> 启动
  - auto-update 可配置（默认开启）
  - update 失败错误包含阶段信息
  - 手动 Update 能力不受影响
- 测试:
  - binary 存在不触发 update
  - binary 缺失触发 update 并启动成功
  - update 失败时 start 失败且错误可判定
  - auto-update disabled 时缺失直接失败

### T-029: 重新 Review（Dev/QA 复核）
- 状态: DONE
- 优先级: P0
- 描述: 按 Ryan 检查清单对重构代码做重新核查并复测
- 文件: 全量 `pkg/proxyrefactor/**`
- 验收标准:
  - 8 项检查清单逐项给结论
  - `go test ./pkg/proxyrefactor/...` 通过
  - 输出残留风险结论
- 测试: go test 全量通过

### T-030: 注释增强与可读性回归
- 状态: DONE
- 优先级: P0
- 描述: 在 `pkg/proxyrefactor` 补充注释（接口职责、关键流程、并发控制、回滚分支）并完成回归
- 文件: `container/{interface.go,updater.go,exec_runner.go}` `forward/{forward_manager.go,relay.go}`
- 验收标准:
  - 不改变业务语义，仅注释增强与轻微重排
  - 关键流程（Start/Update/Forward）注释可读
  - 并发/回滚意图可直接从代码理解
  - 回归测试通过
- 测试: `go test ./pkg/proxyrefactor/... -count=1` 通过

### T-031: Domain 解耦第一阶段（8点方案-A）
- 状态: DONE
- 优先级: P0
- 描述: 落地 domain 禁止项清单+gate、InboundSpecV2 最小字段集、TargetRef 契约、mapper 分级契约
- 文件: `domain/*` `tools/denylist_gate*` `provider/interface.go`（后续清理）
- 验收标准:
  - 禁止项可自动检查
  - InboundSpecV2 收敛（Mode/Entry/TargetRef/PolicyRef/Meta）
  - TargetRef 可判定错误码
  - 契约分级可表达 To/From + warnings
- QA结论: PASS_WITH_RISK（逆向语义后续补强）
- 测试: `go test ./pkg/proxyrefactor/... -count=1` 通过

### T-032: 目录重组与引用迁移（container 体系）
- 状态: DONE
- 优先级: P0
- 描述: 重组为 `core/container` 抽象 + `containers/xray` 实现 + `tools` 通用工具，并修复引用
- 文件: `pkg/proxyrefactor/{core/container,containers/xray,tools}`
- 验收标准:
  - 去 provider 概念残留
  - 目录与引用迁移完成
  - 不改变既有业务行为（结构重组为主）
- QA结论: PASS_WITH_RISK（Update 失败路径完整性需继续强化）
- 测试: `go test ./pkg/proxyrefactor/... -count=1` 通过
