# CLAUDE.md — v2raymg 项目指南

## 项目概述

v2raymg 是一个 Go 语言的代理管理工具。当前核心目标是**重构 proxy 模块**。

## 重构核心目标

### 目标 1：抽象 Container 和 Inbound

- **Container（容器/进程）和 Inbound（入站规则）必须彻底抽象化**，内部不耦合任何业务逻辑
- 这两个抽象的配置需要**尽量低耦合**：
  - 配置对象包含基础配置项
  - 外部进程相关的配置（路径、参数、环境变量等）尽量压缩到一个**通用的配置项**中，而非为每种进程类型定义特殊字段
- 目标是：换一个代理内核（如 xray → sing-box），只需新增一个实现，不改抽象层

### 目标 2：先以 xray 为参考实现

- 重构期间，先参考已有代码，实现 xray 的 Container 与 Inbound
- xray 实现作为第一个具体实现，验证抽象层的设计是否合理

### 目标 3：聚焦三大核心功能

当前阶段只关注以下三个功能的重构，其他功能暂不处理：

1. **Container（进程管理）**：启动、停止、重启、重载、状态检测、版本查询
2. **Inbound（入站管理）**：增删改查入站规则、配置渲染、配置验证
3. **用户管理**：增删改查用户、用户与 Inbound 的关联

端口转发、流量统计、限速、自动更新等功能暂不在本阶段范围内。

## 已有重构成果

重构代码位于 `pkg/proxyrefactor/`，当前目录结构：

```
pkg/proxyrefactor/
├── core/
│   ├── contracts/     # 接口定义（Container/Inbound/User/Subscription）
│   ├── container/    # Container 抽象（BaseContainer + Registry）
│   └── inbound/     # Inbound 抽象
├── containers/xray/ # Xray 具体实现（exec 进程管理 + 配置渲染 + inbound adapter）
│   └── profilegen/  # 协议配置生成器（VMess/VLESS/Trojan/SS）
├── errors/          # 统一错误码
├── forward/         # TCP 端口转发层（暂不在本阶段范围）
├── systemtest/      # 集成测试
├── tools/           # 通用工具
│   └── process/    # 进程执行器
├── usermanager/    # 用户管理
└── examples/       # 示例代码
```

### 已完成的部分（约 90+ Go 文件）

- **Phase 1** ✅ 领域模型 + 统一错误码 + 核心接口（Container/InboundAdapter/StatsClient）
- **Phase 2** ✅ ExecRunner（exec 进程管理）+ ConfigWriter（原子写入配置文件）
- **Phase 3 (xray)** ✅ xray InboundAdapter（InboundSpec → xray 原生 JSON）+ ConfigRenderer
- **Phase 6** ✅ 端口转发层（relay/限速/统计/端口分配/ForwardManager）—— *暂不在本阶段范围*
- **Phase U** ✅ Container 自动更新（GitHub Releases + checksum + 原子替换 + 回滚）—— *暂不在本阶段范围*
- **Phase S** ✅ 系统测试 + Start 自动拉取二进制
- **T-031** ✅ Domain 解耦（denylist gate + InboundSpecV2 + TargetRef）
- **T-032** ✅ 目录重组（core/container 抽象 + containers/xray 实现）

### 已取消的部分

- v2ray InboundAdapter（暂不需要）
- gRPC API 客户端（暂不需要）
- Reconcile 编排（暂不需要）

### 遗留风险

- domain 逆向语义需补强（T-031 QA 标记）
- Update 失败路径完整性需强化（T-032 QA 标记）

## 关键设计决策（必读）

1. **exec 管理进程** — 不嵌入 xray-core Go 库，通过 os/exec 启停外部二进制
2. **InboundSpec 封装** — 上层统一用领域模型，禁止直接使用 xray 原生 inbound 配置
3. **不改动旧代码** — 重构完全独立于 `proxy/` 主干，需复用的逻辑复制后修改
4. **所有代码放 `pkg/` 下** — 多级目录，保持清晰的包结构
5. **单元测试必须补全** — 每个模块对应 `*_test.go`
6. **Forward 规则收敛** — forward 规则创建/删除必须通过 usermanager（GetBindPort/ReleaseBindPort），inbound/container 内部禁止直接调用 forwardManager.AddRule 创建业务规则
7. **统计链路** — 仅依赖 forward 层统计，不依赖 container.QueryStats

## 技术栈

- Go 1.18+（与 go.mod 对齐）
- 标准库为主；仅依赖 `google.golang.org/grpc`（xray gRPC API 交互）
- 测试：`testing` + `testify`
- 不依赖 xray-core / v2ray-core Go 库

## 参考文件

- `PROJECT.md` — 项目目标与架构概览
- `STATUS.md` — 当前阶段状态
- `TASKS.md` — 完整任务列表
- `DECISIONS.md` — 所有架构/技术决策记录
- `proxy/` — 现有代码（重构参考，不直接修改）
- `cmd/xraydemo/` — 手动系统测试 Demo（用于验证完整功能流程）
