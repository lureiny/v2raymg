# Cluster User 接口迁移清单

## 目标

整理现有 HTTP/gRPC 接口在 `ClusterUser` 方案下的去向，明确：

- 哪些接口继续保留
- 哪些接口需要删除
- 哪些接口需要新增
- 哪些接口受 `cluster_user.enabled` 控制

本文件只描述迁移设计，不代表已实现。

## 迁移原则

- 默认不破坏旧版行为
- 旧接口优先保留，作为本地 `usermgr` 路径
- 新能力通过新增接口接入
- `cluster_user.enabled=false` 时，系统行为应尽量等同旧版

## HTTP 接口迁移

### 保留：本地 user 管理路径

以下接口继续保留，语义不变：

| 接口 | 方法 | 现有语义 | 新方案定位 |
|---|---|---|---|
| `/api/user` | GET | 查询目标 node 的本地 users | 保留为本地 user 查询接口 |
| `/api/user` | POST | 向目标 node 添加本地 user | 保留为本地 user 直连接口 |
| `/api/user` | PUT | 更新目标 node 本地 user | 保留为本地 user 直连接口 |
| `/api/user` | DELETE | 删除目标 node 本地 user | 保留为本地 user 直连接口 |
| `/api/user/reset` | POST | 重置/轮换本地 user 端口 | 保留 |
| `/api/profile` | GET | 查询当前登录用户本地信息 | 保留 |
| `/api/profile/password` | PUT | 修改当前 node 上的本地 user 密码 | 保留 |

说明：

- 这些接口继续绑定本地 `usermgr`
- 在 `cluster_user.enabled=false` 时保持现有语义
- 在 `cluster_user.enabled=true` 时仍可存在，但定位为“本地运维接口”

### 删除

| 接口 | 方法 | 原因 |
|---|---|---|
| `/api/users` | DELETE | 对应 `ClearUsers`，与 `DeleteUsers` 语义重叠 |

### 新增：node group 管理接口

以下接口仅在设计方案中新增：

| 接口 | 方法 | 语义 |
|---|---|---|
| `/api/node/:name/groups` | GET | 获取目标 node 的本地 groups |
| `/api/node/:name/groups` | PUT | 整体替换目标 node 的本地 groups |

说明：

- 这两条接口操作的是目标 node 本地持久化的 groups
- 不通过心跳传播
- 建议仅在 `cluster_user.enabled=true` 时注册

### 新增：ClusterUser 管理接口

以下接口用于操作同步层的 `ClusterUser`：

| 接口 | 方法 | 语义 |
|---|---|---|
| `/api/cluster-users` | GET | 查询 `ClusterUser` 列表，可按 group 过滤 |
| `/api/cluster-users` | POST | 新增 `ClusterUser` |
| `/api/cluster-users/:name` | PUT | 更新指定 `ClusterUser` |
| `/api/cluster-users/:name` | DELETE | 逻辑删除指定 `ClusterUser` |

说明：

- 这些接口仅在 `cluster_user.enabled=true` 时注册
- 它们不直接操作本地 `usermgr`
- 它们操作的是同步层 store，再由 placement controller 收敛到本地执行层

## gRPC 接口迁移

### 保留：本地 user 接口

以下 gRPC 接口继续保留，语义保持本地执行：

| 接口 | 现有语义 | 新方案定位 |
|---|---|---|
| `GetUsers` | 获取本地 users | 保留为本地查询接口 |
| `AddUsers` | 添加本地 users | 保留为本地执行接口 |
| `DeleteUsers` | 删除本地 users | 保留为本地执行接口 |
| `UpdateUsers` | 更新本地 users | 保留为本地执行接口 |
| `ResetUser` | 轮换本地 user 端口 | 保留 |
| `GetSub` | 生成本地订阅 | 保留 |
| `GetBandWidthStats` | 查询本地带宽统计 | 保留 |

说明：

- 这些接口继续直接绑定 `usermgr`
- 新方案不替换这些接口
- 它们仍是旧版兼容路径和本地执行能力

### 删除

| 接口 | 原因 |
|---|---|
| `ClearUsers` | 与 `DeleteUsers` 语义重叠，建议移除 |

### 新增：Node Groups

| 接口 | 语义 |
|---|---|
| `GetNodeGroups` | 获取目标 node 的本地 groups |
| `SetNodeGroups` | 整体替换目标 node 的本地 groups |

### 新增：ClusterUser

| 接口 | 语义 |
|---|---|
| `ListClusterUsers` | 列出 `ClusterUser`，可按 group 过滤 |
| `GetClusterUsersByName` | 按用户名获取完整 `ClusterUser` |
| `UpsertClusterUsers` | 新增或覆盖 `ClusterUser` |
| `DeleteClusterUsers` | 对 `ClusterUser` 打 tombstone |

## 开关控制矩阵

### 当 `cluster_user.enabled=false`

| 接口类别 | 行为 |
|---|---|
| 旧 HTTP user 接口 | 全部保留，行为不变 |
| 旧 gRPC user 接口 | 全部保留，行为不变 |
| node group 接口 | 不注册或返回 disabled |
| `ClusterUser` 接口 | 不注册或返回 disabled |
| placement controller | 不启动 |
| user sync | 不启动 |

### 当 `cluster_user.enabled=true`

| 接口类别 | 行为 |
|---|---|
| 旧 HTTP user 接口 | 保留，定位为本地运维接口 |
| 旧 gRPC user 接口 | 保留，定位为本地执行接口 |
| node group 接口 | 注册并启用 |
| `ClusterUser` 接口 | 注册并启用 |
| placement controller | 启动 |
| user sync | 启动 |

## 迁移顺序建议

建议按照以下顺序整理接口：

1. 先保留所有旧接口，不改行为
2. 先在 proto 和 HTTP 层标记 `ClearUsers` 为待移除
3. 增加 `cluster_user.enabled` 配置开关
4. 新增 node group 接口
5. 新增 `ClusterUser` 接口
6. 新增 placement controller 和 user sync
7. 最后移除 `ClearUsers`

这样可以保证：

- 旧版链路始终可用
- 新功能逐步接入
- 每一步都可以独立验证

## 与代码模块的映射建议

### 旧接口

继续映射到：

- 现有 `http/user_handler.go`
- 现有 `rpc/server/end_node_user.go`
- 现有 `usermgr`

### 新接口

建议新增：

- `http/node_groups_handler.go`
- `http/cluster_user_handler.go`
- `rpc/server/end_node_node_groups.go`
- `rpc/server/end_node_cluster_user.go`

说明：

- 新文件只承载新能力
- 避免把旧接口逻辑和新接口逻辑混在一起
- `usermgr` 仍只做本地执行层，新同步层由新 store / controller 模块承载

## 总结

最终接口迁移原则非常明确：

- 旧接口不推翻
- `ClearUsers` 删除
- 新方案通过新增接口接入
- 默认关闭新方案
- 启用后新旧接口并存，但职责分开
