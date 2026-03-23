# PROJECT.md — v2raymg Proxy 层重构

## 项目目标
将 v2raymg 现有 `proxy/manager` 包中 xray/v2ray 进程管理、inbound 管理、用户管理、stats 查询等功能，
在 `pkg/proxyrefactor/` 下独立重构，实现清晰分层（domain → provider → container），
并增加 inbound 封装层（InboundSpec），最终具备独立编译与测试能力。

## 技术栈
- Go 1.18+（与现有 go.mod 对齐）
- 标准库为主；仅依赖 `google.golang.org/grpc`（xray/v2ray gRPC API 交互）
- 不引入 xray-core / v2ray-core Go 库（container 通过 exec 管理外部进程）
- 测试：`testing` + `testify`（按需引入）

## 架构概览
```
pkg/proxyrefactor/
├── domain/            # 领域模型 (InboundSpec, UserSpec, ContainerModel 等)
├── errors/            # 统一错误码
├── container/         # Container 接口 + exec-based 实现 (进程生命周期)
├── provider/          # InboundAdapter 接口 + xray/v2ray 实现
│   ├── xray/
│   └── v2ray/
└── (后续: reconcile/, usermanager/, configrender/)
```

## 硬约束
1. 所有代码放 `pkg/` 多级目录
2. 除 log 等基础库外，重新实现
3. container 通过 exec 管理外部 xray/v2ray 进程
4. 不改动现有代码
5. 补全单元测试
6. 先不集成到主流程
7. inbound 配置必须封装一层（InboundSpec），不直接暴露原生 inbound
