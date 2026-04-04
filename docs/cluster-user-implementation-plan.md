# Cluster User 开发规划文档

## 目标

分阶段为 v2raymg 引入：

- 最终一致的 `ClusterUser` 同步层
- node 本地 group 管理
- placement controller

同时满足：

- 不改动现有 `usermgr` 内部逻辑
- 旧版路径默认保持不变
- 新功能通过配置显式启用
- 支持旧版平滑升级

## 总体策略

采用渐进式开发，分 7 个阶段完成。

原则：

- 先建模，再落存储
- 先做兼容，再做新能力
- 先保证旧版不受影响，再逐步启用新路径

## 阶段 0：接口与边界收敛

目标：

- 定死术语和职责边界
- 明确旧接口保留、新接口新增

产出：

- `ClusterUser` 模型
- node group 本地配置模型
- placement controller 边界
- 配置开关设计

约束：

- 现有 `usermgr` 不改语义
- 删除 `ClearUsers` 设计入口

## 阶段 1：配置与开关

目标：

- 引入 `cluster_user.enabled` 配置
- 默认关闭，确保旧版兼容

建议配置：

```yaml
cluster_user:
  enabled: false
  sync_interval_sec: 5
  bootstrap_from_local: true
  default_group: default
```

任务：

- 扩展配置结构体
- 配置加载与默认值
- server 启动时根据开关决定是否初始化新模块

验收：

- `enabled=false` 时系统行为与当前版本一致

## 阶段 2：本地存储层

目标：

- 为同步层和 node groups 提供本地持久化

新增存储对象：

- `cluster_users`
- `local_node_groups`

任务：

- 设计表结构
- 增加 migration
- 增加 store 接口
- 为 tombstone 预留字段

`cluster_users` 最低字段：

- `username`
- `password`
- `ttl`
- `expire`
- `role`
- `target_group`
- `deleted`
- `updated_at_us`
- `origin_node`
- `hash`

`local_node_groups` 最低字段：

- `group_name`

验收：

- store 可读写
- node 重启后数据可恢复

## 阶段 3：旧版升级兼容与 bootstrap

目标：

- 从旧版平滑迁移，不导致 user 丢失

任务：

- 启动时检查 `cluster_users` 是否为空
- 为空时调用 `usermgr.ListUsers()` 导入本地 user
- 导入时填充默认 group
- 初始化 `updated_at_us` 与 `origin_node`
- node groups 为空时写入 `default`

规则：

- 仅第一次 bootstrap
- 后续重启不再从 `usermgr` 反向覆盖同步层

验收：

- 旧版升级后，未开启新功能也不受影响
- 开启新功能后，旧 user 不会被立即清空

## 阶段 4：Node Group 管理接口

目标：

- 通过现有 `http -> grpc -> target node` 链路管理 node 本地 groups

任务：

- 新增 HTTP 接口：`PUT /api/node/:name/groups`
- 新增 HTTP 接口：`GET /api/node/:name/groups`
- 新增 gRPC 接口：`SetNodeGroups`
- 新增 gRPC 接口：`GetNodeGroups`
- 持久化到 `local_node_groups`

语义：

- 使用“整体替换”而不是 patch
- groups 为空时自动回填 `default`

验收：

- 任意管理节点可远程修改目标 node 的 groups
- node 重启后 groups 不丢失

## 阶段 5：ClusterUser 同步层

目标：

- 基于现有心跳机制实现最终一致同步

任务：

- 扩展心跳 payload，加入 `UserDigest`
- 增加摘要对比逻辑
- 增加按需拉取完整 user 的接口
- 增加 digest hash 计算
- 增加版本裁决：`updated_at_us + origin_node`
- 增加 tombstone 传播

建议新增 gRPC 接口：

- 在心跳中直接附带 `UserDigest`
- `GetClusterUsersByName`
- `UpsertClusterUsers`
- `DeleteClusterUsers`

注意：

- 旧的本地 user 接口继续保留
- 新接口只在 `cluster_user.enabled=true` 时启用

验收：

- 节点间可自动发现 user 差异
- 掉线节点恢复后可自动补齐
- 删除能通过 tombstone 收敛

## 阶段 6：Placement Controller

目标：

- 根据本地 node groups 和 `ClusterUser`，自动驱动本地 `usermgr`

任务：

- 实现 `desired users` 计算
- 实现 `actual users` 读取
- 实现 add/update/remove 决策
- 周期 reconcile
- 错误重试
- 幂等控制

规则：

- `deleted=true` 一定删除
- `target_group` 命中本地 groups 则存在
- 不命中则移除

验收：

- 修改 node groups 后，本地 user 集合会自动收敛
- 修改 `ClusterUser.target_group` 后，本地 user 自动迁移
- 不需要改动 `usermgr` 内部逻辑

## 阶段 7：旧接口整理

目标：

- 梳理旧接口与新接口的并存关系

任务：

- 删除 `ClearUsers`
- 保留现有本地 user 接口
- 明确文档：旧接口为本地直连路径
- 文档化新接口为 cluster user 路径

验收：

- 接口职责清晰
- 不再存在 `ClearUsers`/`DeleteUsers` 的语义重叠

## 测试规划

### 单元测试

覆盖：

- `ClusterUser` store
- node groups store
- digest 比较
- hash 计算
- 版本裁决
- bootstrap 兼容逻辑
- placement controller 的 desired/actual 计算

### 集成测试

覆盖：

- 修改 node groups 后，本地 user 自动增删
- 添加/更新/删除 `ClusterUser` 后，多个 node 最终收敛
- node 掉线恢复后补齐 user
- 默认 group 升级兼容

### 回归测试

覆盖：

- `cluster_user.enabled=false` 时旧接口行为不变
- 现有本地 user 管理链路不受影响

## 实施顺序建议

推荐顺序：

1. 配置开关
2. store + migration
3. bootstrap 兼容
4. node group 管理接口
5. `ClusterUser` 同步层
6. placement controller
7. 旧接口清理
8. 完整测试和文档补齐

## 风险点

- 旧版不同 node 上存在同名 user 且数据不一致
- 未启用新功能时不能影响旧版路径
- 升级后默认 group 处理不当会导致误删 user
- reconcile 若不幂等，会对 `usermgr` 造成重复操作

## 总结

整个开发规划的核心是：

- 先把 `ClusterUser` 作为基础设施做出来
- 再把 placement controller 叠加在上面
- 旧版路径默认不动
- 新功能显式启用
- `usermgr` 继续只做本地执行层
