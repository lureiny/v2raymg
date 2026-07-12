# PROJECT_GUIDE.md — v2raymg 共享项目指南

> 本文件是 Claude 与 Codex 共享的项目事实来源。长期有效的项目背景、架构约束、构建测试入口都放在这里；Claude/Codex 专属工作方式分别放在 `CLAUDE.md` 和 `AGENTS.md`。

## 项目概述

v2raymg 是一个 Go 语言编写的**多节点代理管理服务**,底层支持 xray/hysteria/snell/mihomo 等代理内核。采用 center/end 双角色集群架构:

- **Center Node** — 集群协调层,负责节点发现、状态同步、跨节点用户编排
- **End Node** — 实际运行代理容器(xray/hysteria/snell/mihomo),对外暴露 HTTP 管理 API 和 gRPC 集群通信

核心能力:用户/inbound CRUD、订阅生成与多格式转换、证书自动签发(ACME)、端口转发与流量统计、节点集群、配置热更新、自动升级二进制。

## 重要历史:重构已完成并合入主干

> **注意**:老文档(`PROJECT.md`/`STATUS.md`/`TASKS.md`/`DECISIONS.md`)里频繁出现的 `pkg/proxyrefactor/` **已经不存在了**。那轮重构在 `f3ebd40 refactor!: complete rewrite` 之后整体迁移为当前的主干 `pkg/proxy/`,旧 `proxy/manager` 模块已被替换。读到这些老文档时请以当前代码为准。

当前 `pkg/proxy/` 继承了原重构的分层设计:`core/contracts` 领域模型 + `core/container` 容器抽象 + `containers/{xray,hysteria,snell,mihomo}` 具体实现 + `forward/` 端口转发 + `usermanager/` 用户管理。

## 仓库结构

```
cmd/                       # CLI 命令(server / cli / 版本等)
  server.go                # 端节点入口(main → cmd.Execute)
  cli/                     # 交互式管理 CLI(cobra + go-prompt)
main.go                    # 程序入口
pkg/
  proxy/                   # 代理核心(原 proxyrefactor,现为主干)
    core/
      contracts/           # 领域模型(InboundSpec/UserSpec/ContainerModel/Protocol)
      container/           # Container 抽象(Manager/Registry/Factory)
      inbound/             # Inbound 抽象
      subscription/        # 订阅核心(codec + converter:clash/surge/qv2ray)
    containers/
      xray/                # xray 实现(exec + grpc + adapter + profilegen + updater)
      hysteria/            # hysteria 实现
      snell/               # snell 实现
    forward/               # TCP/UDP 转发层(relay + port_allocator + traffic + ratelimit + clientlimit)
    usermanager/           # 用户管理(CRUD + 带宽统计 + 端口轮转 + sync 一致性)
    systemtest/            # 系统测试(xray 协议矩阵 / e2e / 降级链)
    appconfig/             # 应用配置加载与全局实例
    errors/                # 统一错误码 + ProxyError
    tools/                 # 通用工具(process/binary_swapper/checksum/downloader/github_release_client)
  rpc/                     # 集群 gRPC
    proto/                 # rpc_server.proto + 生成的 pb
    server/                # end/center node server + 各业务 RPC handler
    client/                # end_node rpc client
  http/                    # HTTP API(gin)与各路 handler(user/inbound/cert/sub/bound/node/rotate...)
  cluster/                 # 节点管理、发现、cluster_manager
  store/                   # SQLite 持久化(user_store/inbound_store/node_groups_store + migrations)
  xrayapi/                 # xray 特定 API 封装(grpc/hotreload/hotupdate/internalproto/reality/types)
  certmgmt/                # 证书管理(domain + lego 封装 + service)
  collecter/               # 节点信息/ping 采集
  common/                  # 常量与 rpc 辅助
  log/                     # slog 封装
  buildinfo/               # 构建信息注入点
config/                    # 示例配置(config.example.yaml)
docs/                      # 设计文档与 code review 产物(见下)
wiki/knowledge/            # LLM-first 知识库
scripts/                   # proto 生成、ping 测试等脚本
data/, bin/, tmp/          # 运行时数据 / 构建输出 / 临时
Makefile                   # build / build-full (full_dns tag) / proto
```

## 核心设计决策(仍然有效)

1. **exec 管理外部进程** — 不嵌入 xray-core/sing-box Go 库,统一通过 `pkg/proxy/tools/process` 启停外部二进制
2. **InboundSpec 封装层** — 上层只用 `core/contracts` 领域模型,不允许跨层使用 xray 原生配置类型;provider-specific 字段走 `Extensions map[string]any`
3. **Container 抽象低耦合** — `core/container` 不耦合任何业务;加一个代理内核只需新增 `containers/<name>/` 子目录实现
4. **转发层统一收口** — forward 规则创建/释放必须走 `usermanager`(`GetBindPort`/`ReleaseBindPort`),inbound/container 内部**禁止**直接调用 `forwardManager.AddRule` 创建业务规则
5. **流量统计只信 forward 层** — 不依赖 `container.QueryStats`,统计全部源自 relay 中间计数
6. **ForwardManager 全局单例** — container 通过 `WithForwardManager(fm)` 显式注入,缺省回退到进程级单例;inbound 继承 container 实例,不单独注入
   - **转发监听默认双栈** — 转发规则未指定时默认同时监听 IPv4(`0.0.0.0`)+ IPv6(`[::]`,`tcp6/udp6` 即 `IPV6_V6ONLY`)。全局默认由 `forward.listen_stack`(`dual`/`ipv4`/`ipv6`,默认 `dual`)控制;单规则可用 `ForwardRule.ListenAddr`(具体 IP 字面量,IPv6 自动加方括号)或 `ForwardRule.ListenStack` 覆盖。双栈下**两半都是 best-effort**:纯 IPv4 主机跳过 `[::]`、纯 IPv6 主机跳过 `0.0.0.0`,只要有一个协议族能绑就照常启动,两个都绑不上才失败(见 `pkg/proxy/forward/listen.go` + `relay_multi.go`)
7. **持久化走 SQLite** — user/inbound/node_groups 全部落 SQLite,DB 操作通过 `pkg/store/manager` 的 `Provider` + `Tx` 抽象
8. **集群通信走 gRPC** — center/end 节点间协议定义在 `pkg/rpc/proto/rpc_server.proto`,运行时通过 cobra 启动的 server 暴露;默认加密已切到 GCM(老 CBC 回退已移除)
9. **单元测试必须补全** — 每个可测模块对应 `*_test.go`;系统测试在 `pkg/proxy/systemtest/`(部分需 `-tags=integration` + 真实 xray 二进制)

> **新增 container 必读** — `docs/container-design-principles.md` 把上述 #1/#3/#4/#5 落成三条可操作的 container 开发原则,并给出"底层是否支持多 inbound / 是否有运行时 API"的适配模式矩阵(A/B/C)。接入新代理内核前必须读完并在设计文档里声明模式。

## 技术栈

- **Go 1.24**(`go.mod` 声明 `go 1.24.0`, toolchain `go1.24.4`) — 旧文档写的 1.18 已过时
- Web:`gin-gonic/gin`
- CLI:`spf13/cobra` + `lureiny/go-prompt`
- gRPC:`google.golang.org/grpc`
- 证书:`go-acme/lego/v4`(支持多 DNS provider,full_dns build tag 开启完整 provider 集)
- DB:`modernc.org/sqlite`(纯 Go,无 CGO)
- 指标:`prometheus/client_golang` + `node_exporter`
- 测试:`stretchr/testify` + `agiledragon/gomonkey/v2`(monkey patch,用于 mock 难以抽象的外部依赖)
- UUID:`google/uuid`

**不依赖 xray-core / v2ray-core / sing-box 的 Go 库** — xray gRPC proto 通过 `scripts/sync-xray-proto.sh` + `scripts/gen-xray-proto.sh` 同步到 `pkg/xrayapi/internalproto/` 里单独生成。

## 当前工作焦点

**全仓分层 code review 已完成,待处理**(见 `docs/review-2026-07-10/`,入口 `99-summary.md`):

- 2026-07-10 对 `pkg/`+`cmd/` 全量做了**自底向上 7 层递进 review**(L0 基础→L1 契约/持久→L2 核心域→L3 容器→L4 集群/RPC→L5 HTTP/CLI→L6 测试),finder→对抗性 verifier 两级流程,L1/L2 另跑了独立第二轮。全程 Fable 5。
- **规模**:保留 475 条(447 confirmed / 28 uncertain,剔除 refuted 7 条),其中 **P0 2 条 / P1 57 条 / P2 181 / P3 235**。
- **2 P0 全修 + P1 43/52 已修 —— 分支 `fix/p0-concurrency`,13 `fix(` commit(2026-07-13 按 finding 逐条核对修正)**。每簇走 scout→design→红队对抗验证 + "修前必炸/修后必绿" 的 -race 回归。commit:P0-1 NodeManager `38a6620`、P0-2 hysteria `f4445c9`、A1 集群节点并发 `7a5ffbe`、A2 容器状态机+snell `13e5c25`、B2 SSRF/模板注入 `e0e0170`、B3 xray security=none `c425504`、C 转发数据面 `a2c3c08`、D 证书原子性 `571a7dd`、E 集群同步一致性 `ddffbe7`、F 进程收尸/二进制交换 `77106f6`、G 启动静默失败 `3851c81`、H 探测采集 `a93f985`、**B1 集群 RPC 加密/鉴权 `61adb83`**。
  - ⚠️ **P1 未全闭合(9 条)**:此前"8 簇全完成"是按簇报、非按 finding 报。**仍为真 P1、待补修的 usermanager 计费并发子簇**(本属最大并发簇,被 E 提交漏掉):`usermanager.go:2239` GetStats 共享 map 并发崩溃(**P0 级 fatal**,bandwidth 采集 goroutine 锁外 range `ByUser` vs collect ticker 写)、`:2237` GetAllDeltaTraffic 两段锁丢流量(计费少算)、`:2233` 二者零回归测试。**其余(非回归/已降级)**:VLESS 缺 subscription_chain 测试、CI `-race` 仅覆盖 pkg/cluster+hysteria 两包(应扩面)、HC `run()` 假 Running(降 P2)、drainEnd 慢泄漏(降 P3)、clash 外部 sub-converter 依赖(遗留)、pkg/rpc/client 零测试、rotate 测试泄漏 listener。逐条核对结果见 `99-summary.md` §4。
  - **P1-B1(方案 A,一步到位破坏性升级)**:HKDF KDF + 消息类型 AAD + NodeAuthInfo 加 ts/nonce/dest 防重放 + center 改 app 层多 token 鉴权(拓扑=多 cluster 共 center)。**部署硬要求**:全集群+center 同时升级、配强 token(≥16)、center 的 `cluster_tokens` 列全所有 cluster、时钟偏移 >30s 会被隔离(NTP 依赖扩面)、token 静态无轮换。详见 `docs/review-2026-07-10/B1-rpc-crypto-DECISION-NEEDED.md`。
  - **各簇明确划出的残留/未修项**(见对应 commit message):Node 字段外的 gRPC 连接泄漏与 CenterNodeServer 鉴权(→B1)、hysteria run() 假 Running、BaseContainer Starting 态 Stop 窗口、snell reconcileStopCh 跨代 race、drainEnd map 慢泄漏、Clash 模板 rule-providers 仍信任第三方、xray 已落库坏 inbound 需运维重建、SetPingCheck 运行期重启路径缺失。多为 P2 级或需更大结构改动,后续处理。
- **最大主题**:并发/数据竞争(~18 条,集群节点状态 / 事件 channel / 容器状态机三处缺同步),且 **CI 未开 `-race`、systemtest 矩阵未在 CI 跑**(L6),故这些长期在盲区。
- **处理规则(用户明确要求,仍然有效)**:
  1. 先描述清楚问题本质和修改方案,等用户确认才能动手
  2. review agent 对涉及第三方协议/规范的问题误报率较高,不确定时必须明确标注(本轮 `protocolRelated`/uncertain 已标)
  3. 按 P0 → P1 → P2 → P3 优先级顺序处理;P1 建议按 `99-summary.md` §4 的主题成簇修
- **旧 review**:2026-04-09 的 `pkg/proxy/` 全量 review(124 条,见 `docs/review-2026-04-09.md` 与用户级 memory `project_review_status.md`)已被本轮覆盖并核对遗留状态(见 `docs/review-2026-07-10/10-legacy-status.md`)。
- 延后事项:`Inbound 用户追踪架构统一`(见 `docs/inbound-user-tracker-refactor.md`),P0/P1 清空后再评估

**近期主题**(git log 可查):集群用户同步、reset traffic/auth token API、node containers RPC、hysteria UDP 转发、订阅协议统一转换层、FastAdd 对齐最新 xray、cluster 心跳与 RPC 加固;mihomo 协议扩展(Phase 0-5 完成 vless/vmess+/trojan+/ss+/hysteria2,Phase 6-7 待开工 tuic/anytls)。

**证书自动续期接线(2026-06)**:此前 `certmgmt` 的 `StartAutoRenew` 实现完整但**全仓无人调用**,证书过期从不自动更新。已修复:
- `cmd/server.go` 启动时拉起 `certMgr.StartAutoRenew(ctx, 0)`(随服务生命周期 ctx 启停);巡检间隔硬编码 1 分钟(本地判断,不可配)。
- 新增配置项 `cert_mgmt.renew_before_hours`(单位**小时**,默认 24h,优先于旧的 `renew_before_days`);service 与 lego 两层用同一续期窗口优先级。
- **零重启热重载**:续期就地覆盖 `certificates/<domain>.crt|.key`,xray(~1h ticker)/mihomo(fswatch,需 ≥v1.19.18)/hysteria(每握手比 mtime)各核自行从文件热重载,certmgmt **不**主动重启 container。详见 `docs/certmgmt-design.md` §6.4。
- 顺带修两个邻近 bug:`config.example.yaml` 的 `cert_mgmt` key 大小写不匹配(snake_case 未对上、被 yaml 静默丢弃)、`rpc_adapter.AddCertificates` 导入证书路径分叉(副本被 SaveCert 忽略)。

## 知识库(wiki)

项目内维护了 LLM-first 知识库 `wiki/knowledge/`。回答**"是什么 / 怎么工作 / 为什么"**类问题时,应当在相关时**先查 wiki 再读代码**。

- 已登记概念(`wiki/knowledge/_manifest.json`):
  - `port-management` — 端口管理与流量统计(端口池、转发规则、`GetBindPort`/`ReleaseBindPort` 入口、累计/增量流量等)
- Claude 或 Codex 的专属 wiki 工具使用方式写在各自入口文件中,不要写入本共享文件。

## 构建与测试

```bash
# 常规构建(精简 DNS provider,默认)
make build

# 完整 DNS provider
make build-full

# 生成 proto
make proto

# 全量测试(含 race)
go test ./... -race -count=1

# 系统测试(降级版,无外网/无 xray 可跑)
go test ./pkg/proxy/systemtest -run TestDegradedLocalSocks5ProxyChain -v

# 系统测试(真实 xray,需 XRAY_BIN 环境变量)
XRAY_BIN=/path/to/xray go test ./pkg/proxy/systemtest -tags=integration -v
```

`pkg/proxy/systemtest/README.md` 里仍写的是 `./pkg/proxyrefactor/...`,是历史残留,实际路径应当是 `./pkg/proxy/...`。

## 参考文件(当前有效的)

- `README.md` — 项目对外说明 + HTTP API 列表(权威)
- `CHANGELOG.md` — 逐版本变更
- `Makefile` — 构建入口
- `config/config.example.yaml` — 配置模板(包含 log/store/forward/cert_mgmt/subscription/containers 等完整段落)
- `docs/`
  - `container-design-principles.md` — **Container 开发三原则 + 适配模式矩阵(新增内核必读)**
  - `review-2026-07-10/` — **当前权威 review**：全仓分层 7 层 review 全量结果（入口 `99-summary.md`）
  - `review-2026-04-09.md` — 上一轮 `pkg/proxy/` review 全量清单（已被 2026-07-10 覆盖，遗留状态见 `review-2026-07-10/10-legacy-status.md`）
  - `xray-container-architecture.md` / `xray-container-flow.md` — xray container 设计
  - `snell-container-design.md` — snell container 设计
  - `mihomo-container-design.md` — mihomo container 设计(模式 B + per-user listener 变种)
  - `cluster-user-*` — 集群用户体系各阶段设计
  - `inbound-user-tracker-refactor.md` — 延后的用户追踪架构统一方案
  - `http-api-reference.md` — HTTP API 参考
  - `certmgmt-design.md` — 证书管理设计
  - `user-placement-controller-design.md` / `user-sync-design.md` — 多节点用户编排
  - `heartbeat-optimization-design.md` — **集群心跳流量优化(有序聚合 hash + 不一致才拉全量,兼容旧版)设计稿,待评审**
- `wiki/knowledge/` — LLM 友好知识库(优先于代码查阅)

## 过时/历史文档(仅做背景参考)

以下文档基本是 2026-02 阶段重构期间留下的,内容指向已不存在的 `pkg/proxyrefactor/`,**不再反映当前代码结构**,改动这些文件时需先甄别:

- `PROJECT.md` — 仅对重构阶段的目标描述
- `STATUS.md` — 停在 `T-032` / 2026-02-22
- `TASKS.md` — 重构阶段 T-001 ~ T-032 任务清单,全部 DONE 或取消
- `DECISIONS.md` — D-001 ~ D-011 架构决策;核心思想仍有效,但文件路径全指向旧目录

如需更新项目状态/决策,建议**改写或废弃**这些文件,不要继续在上面累加新内容。
