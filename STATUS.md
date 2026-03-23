# STATUS.md — 当前状态

## 当前阶段: Phase S+ — Start 自动拉取二进制 ✅ 完成

## 阶段计划

### Phase 1 — 领域模型 + 统一错误码（✅ 完成）
- 目标：定义 InboundSpec / UserSpec / ContainerModel 领域模型 + 统一错误码 + 基础接口
- 任务：T-001, T-002, T-003
- 前置依赖：无

### Phase 2 — Container 层（exec 进程管理）（✅ 完成）
- 目标：实现 Container 接口 + ExecRunner (xray/v2ray 进程启停/重载/版本管理)
- 任务：T-004, T-005
- 前置依赖：Phase 1 完成

### Phase 3 — Provider 层（InboundAdapter + ConfigRenderer）（⚠️ xray 完成, v2ray 取消（按需求））
- 目标：实现 xray/v2ray InboundAdapter + 配置渲染
- 任务：T-006, T-007, T-008
- 前置依赖：Phase 1 完成

### Phase 4 — gRPC API 客户端（独立实现）（🚫 取消（按需求））
- 目标：独立实现 xray/v2ray gRPC stats/handler 客户端
- 任务：T-009, T-010
- 前置依赖：Phase 2 完成

### Phase 5 — 用户管理 + Reconcile 编排（🚫 取消（按需求））
- 目标：实现用户管理、inbound 服务编排、reconcile 机制
- 任务：T-011, T-012, T-013
- 前置依赖：Phase 3, Phase 4 完成

### Phase 6 — 端口转发层（✅ 完成）
- 目标：实现 TCP 双向转发 + 流量统计 + 限速，每 user 独立转发端口
- 任务：T-014 ~ T-020
- 前置依赖：Phase 1 domain 模型

#### Phase 6A — 数据结构 + 接口 + 端口分配（✅）
- T-014: 转发层领域模型 + ForwardManager 接口
- T-015: 端口分配器（PortAllocator）

#### Phase 6B — TCP Relay 核心实现（✅）
- T-016: TCP Relay（双向转发 + 优雅关闭）
- T-017: 流量统计（TrafficCounter，原子计数，按 user/inbound 聚合）

#### Phase 6C — 限速 + ForwardManager 实现（✅）
- T-018: 限速器（RateLimiter，令牌桶）
- T-019: ForwardManager 完整实现（全局单例 + CRUD + 生命周期）

#### Phase 6D — 集成接口 + 收尾测试（✅）
- T-020: 与 Container/InboundSpec 集成接口 + 端到端测试

### Phase U — Container Update 接口（✅ 完成）
- 目标：实现 xray 指定版本更新（GitHub Releases）+ checksum + 原子替换 + 回滚 + 进程协同
- 任务：T-021 ~ T-025

#### Phase U1 — 抽象接口与类型（✅）
- T-021: UpdateRequest/Result + Updater 接口 + Container 扩展

#### Phase U2 — Release 拉取与下载校验（✅）
- T-022: GitHub Releases 客户端 + 资产选择
- T-023: 下载 + checksum 校验

#### Phase U3 — 原子替换与回滚（✅）
- T-024: BinarySwapper + rollback manager

#### Phase U4 — ExecRunner 集成与测试（✅）
- T-025: ExecRunner.Update(ctx, req) + 单测

### Phase S — 简单系统测试（✅ 完成）
- 目标：验证重构框架可用性（xray container + socks5 inbound + 代理访问）
- 任务：T-027
- 交付：真实集成测试 + 环境受限降级测试 + 成功/失败判据文档

## 最近更新
- 2026-02-22: T-032 完成（目录重组与引用迁移，QA=PASS_WITH_RISK，已汇报）
- 2026-02-22: T-031 完成（domain 解耦第一阶段，QA=PASS_WITH_RISK）
- 2026-02-21: T-030 完成（注释增强任务闭环：接口/流程/并发/回滚语义补充 + 回归测试通过）
- 2026-02-21: T-029 完成（重新review：8项清单逐项结论 + 全量测试通过）
- 2026-02-21: T-028 完成（xray Start 自动拉取二进制 + 单测）
- 2026-02-21: Phase S 完成（T-027 系统测试：真实+降级双方案）
- 2026-02-21: QA 复测（mock 全覆盖）PASS
- 2026-02-21: T-026 完成（Phase U QA FAIL 闭环，mock 覆盖补齐）
- 2026-02-20: Phase U 完成（T-021~T-025）
- 2026-02-20: Ryan 确认 container Update 方案，进入 Phase U
- 2026-02-20: D-008 Container option 注入约束落地（global.go + integration.go 重写 + 测试补齐）
- 2026-02-20: Phase 6 全部完成 (T-014 ~ T-020)
- 2026-02-20: 新需求：端口转发层，开始 Phase 6
- 2026-02-20: Ryan 决定取消 Phase 3(v2ray) / Phase 4 / Phase 5，项目收尾
- 2026-02-20: Phase 2+3(xray) 完成，进入 Phase 3(v2ray) + Phase 4
- 2026-02-20: Phase 1 完成（domain/errors/interfaces），进入 Phase 2+3 并行
- 2026-02-20: 项目启动，开始 Phase 1
