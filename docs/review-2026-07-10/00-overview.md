# Code Review — 2026-07-10 · 总览与方法论

> 本轮为**全仓递进式 review**（上一轮 2026-04-09 仅覆盖 `pkg/proxy/`）。目标：按依赖方向自底向上逐层审查所有生产代码，结论落盘，供后续逐条处理。

## 与 2026-04-09 review 的关系

- 旧 review 共 124 条问题（`docs/review-2026-04-09.md`），三个月来代码大量演进。
- 本轮启动时先对旧 review **逐条核查现状**（fixed / still-present / partially-fixed / not-applicable / cannot-determine），结果在 [`10-legacy-status.md`](10-legacy-status.md)。
- 新 review 中若再次发现与旧条目相同的问题，会标注 `legacyId`，不重复计数。

## 分层顺序（递进逻辑）

按依赖方向自底向上。**下层确认的 P0/P1 结论会作为摘要注入上层 reviewer 的上下文**——例如底层错误包装的缺陷会改变对上层错误处理代码的判断。

| 层 | 单元 | 范围 | 产出文件 |
|----|------|------|----------|
| L0 基础层 | LOG / ERR / CFG | `pkg/log` `pkg/common` `pkg/buildinfo`、`pkg/proxy/errors` `pkg/proxy/tools`、`pkg/proxy/appconfig` | `L0-foundation.md` |
| L1 契约与持久层 | STO / CTR / CON / SUB | `pkg/store`、`core/{contracts,params,inbound}`、`core/container`、`core/subscription` | `L1-core.md` |
| L2 核心域 | FWD / UM / XAPI / CERT | `pkg/proxy/forward`、`pkg/proxy/usermanager`、`pkg/xrayapi`、`pkg/certmgmt` | `L2-domain.md` |
| L3 容器层 | XC / MC / HC / SC | `containers/{xray,mihomo,hysteria,snell}` | `L3-containers.md` |
| L4 集群层 | CLU / RPC / COL | `pkg/cluster`、`pkg/rpc`、`pkg/collecter` | `L4-cluster.md` |
| L5 接口层 | HTTP / CMD | `pkg/http`、`cmd/` + `main.go` | `L5-interface.md` |
| L6 测试横切 | TEST | `pkg/proxy/systemtest` + 全仓测试缺口 | `L6-tests.md` |

生成代码（`*.pb.go`、`pkg/xrayapi/internalproto/`）不逐行 review；proto **定义**（`rpc_server.proto`）在 L4 审。

## 每个模块的 review 维度（7 维 checklist）

1. **正确性** — 逻辑错误、边界条件、协议/格式正确性
2. **并发安全** — data race、死锁、goroutine 泄漏、锁粒度
3. **资源管理** — fd/timer/goroutine 泄漏、错误路径上的清理
4. **错误处理** — 吞错、错误包装失效、ProxyError 约定一致性
5. **安全** — 鉴权、注入、密钥处理、加密实现
6. **架构约束符合性** — PROJECT_GUIDE.md 九条设计决策（重点：#2 上层不得使用 xray 原生类型、#4 转发规则必须走 usermanager 收口、#5 流量统计只信 forward 层、#6 ForwardManager 注入方式）
7. **测试缺口** — 关键路径无测试、空测试、掩盖缺陷的测试

## 验证流程（控制误报）

每条 finding 经过两级：

1. **Finder**：按模块 + lens 分工，必须给出 file:line 与代码证据；
2. **Verifier**：独立 agent 以"反驳优先"立场逐条核实，判定 confirmed / refuted / uncertain。

**第三方协议/规范类问题**（xray 配置字段、clash schema、TUIC/hysteria 协议、ACME 语义等）按项目既有经验误报率高：finder 必须标 `protocolRelated`，verifier 无法从仓库内证据（docs/、wiki/knowledge/、上游文档副本）确证时一律标 `uncertain`，**不得凭记忆断言上游行为**。落盘时 uncertain 条目单独标注，处理前需人工核对上游。

## 优先级定义（与旧 review 一致）

- **P0**：正确性/安全性错误，需立即修复
- **P1**：高优先级稳定性/设计缺陷
- **P2**：中优先级一致性/接口设计问题
- **P3**：低优先级清理/规范问题

## 文件索引

- `00-overview.md` — 本文件（方法论）
- `01-module-maps.md` — 21 个模块单元的结构化地图（熟悉阶段产出）
- `10-legacy-status.md` — 2026-04-09 review 124 条问题现状核查
- `L0-foundation.md` … `L6-tests.md` — 各层 findings
- `99-summary.md` — 跨层统计与 P0/P1 处理清单

## 模块规模基线（2026-07-10，含测试）

| 单元 | 目录 | LOC |
|------|------|-----|
| LOG | pkg/log + pkg/common(+rpc) + pkg/buildinfo | ~975 |
| ERR | pkg/proxy/errors + tools(+process) | ~1246 |
| CFG | pkg/proxy/appconfig | ~1090 |
| STO | pkg/store(+migrations) | ~1781 |
| CTR | core/contracts + params + inbound | ~1843 |
| CON | core/container | ~2200 |
| SUB | core/subscription | ~2236 |
| FWD | pkg/proxy/forward | ~7613 |
| UM | pkg/proxy/usermanager(+sync) | ~6044 |
| XAPI | pkg/xrayapi（不含生成代码） | ~4325 |
| CERT | pkg/certmgmt | ~1967 |
| XC | containers/xray | ~10921 |
| MC | containers/mihomo | ~12266 |
| HC | containers/hysteria | ~1344 |
| SC | containers/snell | ~959 |
| CLU | pkg/cluster | ~913 |
| RPC | pkg/rpc client+server（proto 定义另计） | ~4416 |
| COL | pkg/collecter(+ping) | ~1471 |
| HTTP | pkg/http(+auth+prometheus_desc) | ~6673 |
| CMD | cmd + cli + main.go | ~3697 |
| TEST | pkg/proxy/systemtest | ~9097 |
