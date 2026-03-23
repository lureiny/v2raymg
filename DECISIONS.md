# DECISIONS.md — 架构 / 技术决策记录

## D-001: 分层架构
- 日期: 2026-02-20
- 决策: 采用 domain → provider → container 三层架构
- 原因: 上层只看 InboundSpec，下层只看 NativeInbound，adapter 做转换
- 影响: 清晰隔离业务语义与容器实现

## D-002: Container 通过 exec 管理外部进程
- 日期: 2026-02-20
- 决策: 不在 Go 中嵌入 xray-core/v2ray-core，通过 os/exec 启停外部二进制
- 原因: Ryan 明确要求；降低编译依赖
- 影响: gRPC API 交互需独立实现（不依赖 xray-core Go 包的 command 类型）

## D-003: InboundSpec 封装层
- 日期: 2026-02-20
- 决策: 上层统一使用 InboundSpec 领域模型，禁止直接使用 container 原生 inbound 配置
- 原因: Ryan 明确要求
- 影响: 需新增 InboundAdapter 接口 + xray/v2ray 各一个实现

## D-005: 端口转发层架构
- 日期: 2026-02-20
- 决策: 在 container inbound 前增加 TCP 端口转发层；每个 user 分配独立的转发端口
- 原因: 不依赖 container 自带用户管理，在转发层实现流量统计和限速
- 影响:
  - 客户端连接用户独立端口 → 转发层 relay → container inbound 端口
  - 转发层在 relay 中间计数/限速
  - 全局共享一个 ForwardManager 实例，避免端口冲突
  - Container/InboundSpec 初始化时需注入 ForwardManager

## D-006: 端口分配策略
- 日期: 2026-02-20
- 决策: 使用范围端口分配器（PortAllocator），支持指定端口范围 + 冲突检测
- 原因: 避免与 container inbound 端口和系统端口冲突；支持集中管理
- 影响: PortAllocator 维护已分配端口集合，分配/回收 O(1)

## D-007: 限速采用令牌桶算法
- 日期: 2026-02-20
- 决策: 使用 token bucket 限速，每连接独立限速器，支持按 user 聚合
- 原因: 令牌桶允许突发流量，符合网络场景；纯标准库实现
- 影响: 每个 relay 连接持有限速器引用，Read/Write 过桶

## D-008: Container option 注入 ForwardManager
- 日期: 2026-02-20
- 决策:
  - NewContainer(...) 通过 ContainerOption 模式注入 ForwardManager
  - WithForwardManager(fm) 显式注入；缺省时使用进程级全局单例
  - Inbound 不再单独注入 FM，直接继承 container 持有的实例
- 原因: Ryan 要求统一注入点，简化 inbound 创建
- 影响:
  - 新增 global.go (GlobalForwardManager 单例 + SetGlobalConfig + Reset)
  - integration.go 重写：NewContainer 取代 NewForwardAwareContainer
  - InboundForwarder 构造不变但只由 container.RegisterInbound 内部调用

## D-009: Container Update 接口设计
- 日期: 2026-02-20
- 决策: 在 container 抽象层增加 Update(ctx, req) 能力，ExecRunner 实现 xray 优先更新
- 原因: 支持指定版本自动更新、回滚与可测试性
- 影响:
  - Container interface 增加 Update 方法
  - 新增 update_types/github_release_client/downloader/checksum/swapper/updater 模块
  - 更新流程中进程 stop/restart 策略受 RestartPolicy 控制

## D-010: 更新安全策略
- 日期: 2026-02-20
- 决策: 最低要求 checksum 校验；签名校验预留接口字段（本轮未强制）
- 原因: 满足安全底线并保持可演进
- 影响: UpdateResult 增加 ChecksumVerified/SignatureVerified

## D-011: 重构代码注释基线（长期约束）
- 日期: 2026-02-21
- 决策: 后续 `pkg/proxyrefactor` 新增/重构代码默认补充关键注释
- 注释重点:
  1) 对外接口职责与参数语义
  2) 核心流程与顺序约束
  3) 并发控制点（锁/atomic/生命周期）
  4) 回滚与异常分支意图
- 原因: 降低多轮迭代维护成本，提升 Dev/QA 复核效率

## D-004: 不改动现有代码
- 日期: 2026-02-20
- 决策: 如需复用已有逻辑且需改动，复制到 pkg/ 新目录后修改
- 原因: 降低回归风险，重构独立于主干
