# CLAUDE.md — v2raymg 项目指南

## 项目概述

v2raymg 是一个 Go 语言编写的**多节点代理管理服务**,底层支持 xray/hysteria/snell 三种代理内核。采用 center/end 双角色集群架构:

- **Center Node** — 集群协调层,负责节点发现、状态同步、跨节点用户编排
- **End Node** — 实际运行代理容器(xray/hysteria/snell),对外暴露 HTTP 管理 API 和 gRPC 集群通信

核心能力:用户/inbound CRUD、订阅生成与多格式转换、证书自动签发(ACME)、端口转发与流量统计、节点集群、配置热更新、自动升级二进制。

## 重要历史:重构已完成并合入主干

> **注意**:老文档(`PROJECT.md`/`STATUS.md`/`TASKS.md`/`DECISIONS.md`)里频繁出现的 `pkg/proxyrefactor/` **已经不存在了**。那轮重构在 `f3ebd40 refactor!: complete rewrite` 之后整体迁移为当前的主干 `pkg/proxy/`,旧 `proxy/manager` 模块已被替换。读到这些老文档时请以当前代码为准。

当前 `pkg/proxy/` 继承了原重构的分层设计:`core/contracts` 领域模型 + `core/container` 容器抽象 + `containers/{xray,hysteria,snell}` 具体实现 + `forward/` 端口转发 + `usermanager/` 用户管理。

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
wiki/knowledge/            # LLM-first 知识库(走 wiki skill,见全局 CLAUDE.md)
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

**proxy 模块全量 code review 处理中**(见 `docs/review-2026-04-09.md`):

- 2026-04-09 对 `pkg/proxy/` 做了全量 7-session code review,共发现 124 条问题
- 截至 memory 最近一次记录(2026-04-10 左右)剩余若干 P0 未处理,详情与进度在用户级 memory `project_review_status.md`
- **处理规则(用户明确要求)**:
  1. 先描述清楚问题本质和修改方案,等用户确认才能动手
  2. review agent 对涉及第三方协议/规范的问题误报率较高,不确定时必须明确标注
  3. 按 P0 → P1 → P2 → P3 优先级顺序处理
- 延后事项:`Inbound 用户追踪架构统一`(见 `docs/inbound-user-tracker-refactor.md`),P0/P1 清空后再评估

**近期主题**(git log 可查):集群用户同步、reset traffic/auth token API、node containers RPC、hysteria UDP 转发、订阅协议统一转换层、FastAdd 对齐最新 xray、cluster 心跳与 RPC 加固。

## 知识库(wiki)

项目内维护了 LLM-first 知识库 `wiki/knowledge/`,符合全局 `CLAUDE.md` 里的 wiki 路由规则。回答**"是什么 / 怎么工作 / 为什么"**类问题时应当**先查 wiki 再读代码**。

- 已登记概念(`wiki/knowledge/_manifest.json`):
  - `port-management` — 端口管理与流量统计(端口池、转发规则、`GetBindPort`/`ReleaseBindPort` 入口、累计/增量流量等)
- 路由工具:`/home/node/.claude/wiki-tools/wiki-locate --all`
- 新建/更新/读取一律走相应 skill(`write-wiki-page` / `update-wiki-page` / `read-wiki-page`)

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
  - `review-2026-04-09.md` — 当前 review 问题全量清单
  - `xray-container-architecture.md` / `xray-container-flow.md` — xray container 设计
  - `snell-container-design.md` — snell container 设计
  - `mihomo-container-design.md` — mihomo container 设计(模式 B + per-user listener 变种)
  - `cluster-user-*` — 集群用户体系各阶段设计
  - `inbound-user-tracker-refactor.md` — 延后的用户追踪架构统一方案
  - `http-api-reference.md` — HTTP API 参考
  - `certmgmt-design.md` — 证书管理设计
  - `user-placement-controller-design.md` / `user-sync-design.md` — 多节点用户编排
- `wiki/knowledge/` — LLM 友好知识库(优先于代码查阅)

## 过时/历史文档(仅做背景参考)

以下文档基本是 2026-02 阶段重构期间留下的,内容指向已不存在的 `pkg/proxyrefactor/`,**不再反映当前代码结构**,改动这些文件时需先甄别:

- `PROJECT.md` — 仅对重构阶段的目标描述
- `STATUS.md` — 停在 `T-032` / 2026-02-22
- `TASKS.md` — 重构阶段 T-001 ~ T-032 任务清单,全部 DONE 或取消
- `DECISIONS.md` — D-001 ~ D-011 架构决策;核心思想仍有效,但文件路径全指向旧目录

如需更新项目状态/决策,建议**改写或废弃**这些文件,不要继续在上面累加新内容。
