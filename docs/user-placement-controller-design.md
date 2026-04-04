# User Placement Controller 设计文档

## 目标

实现一个独立于 `usermgr` 的 placement controller，根据：

- 本地 node groups
- 同步层中的 `ClusterUser`

自动决定当前 node 上哪些 user 应该存在，并通过现有 `usermgr` 执行 add/update/remove。

约束：

- 不改动现有 `usermgr` 内部逻辑
- controller 只调用 `usermgr` 提供的现有能力
- 允许最终一致
- controller 必须幂等
- 仅在 `cluster_user.enabled=true` 时启用

## 核心职责

placement controller 只负责“期望状态 -> 本地实际状态”的对账。

输入：

- 本地 `node groups`
- 本地同步层中的全部 `ClusterUser`
- 本地 `usermgr` 当前 user 列表

输出：

- `AddUser`
- `UpdateUser`
- `RemoveUser`

## 与旧接口并存

placement controller 仅在 `cluster_user.enabled=true` 时启动。

这意味着：

- 旧版直接操作本地 `usermgr` 的路径仍然存在
- 新版 controller 只在显式启用后介入
- 默认配置下不会改变现有系统行为

## 判定规则

对于每个 `ClusterUser`，controller 先算出该 user 在当前 node 上是否应该存在。

### 规则 1：删除优先

若：

- `deleted=true`

则该 user 在当前 node 上一定不应存在。

### 规则 2：group 命中

若：

- `deleted=false`
- `user.target_group` 命中本地 node groups

则该 user 在当前 node 上应存在。

### 规则 3：group 不命中

若：

- `deleted=false`
- `user.target_group` 不命中本地 node groups

则该 user 在当前 node 上不应存在。

## Reconcile 模型

controller 每次运行时，计算两组集合：

- `desired users`
- `actual users`

其中：

- `desired users` 来源于 `ClusterUser + local node groups`
- `actual users` 来源于 `usermgr.ListUsers()`

然后分三类处理。

### Add

条件：

- user 在 `desired` 中
- user 不在 `actual` 中

动作：

- 调用 `usermgr.AddUser`

### Update

条件：

- user 同时存在于 `desired` 和 `actual`
- 但全局字段不一致

需要比较的字段：

- `password`
- `ttl/expire`
- `role`（若本地 user 层承载）

动作：

- 调用 `usermgr.UpdateUser`

### Remove

条件：

- user 不在 `desired` 中
- 但存在于 `actual` 中

动作：

- 调用 `usermgr.RemoveUser`

## 幂等要求

controller 必须天然幂等。

要求：

- 重复执行同一轮 reconcile，不应产生额外副作用
- user 已存在时，不重复 add
- user 已删除时，不重复 remove
- user 未变化时，不重复 update

因此 controller 在调用 `usermgr` 前，必须先做本地差异判断。

## 调度方式

第一版建议采用：

- 周期轮询

例如：

- 每 5 秒或 10 秒执行一次 reconcile

原因：

- 实现简单
- 不依赖事件总线
- 对最终一致模型足够

后续可再补：

- user sync 更新后触发一次 reconcile
- node groups 变更后触发一次 reconcile
- 周期轮询继续作为兜底

## 本地 groups 读取

controller 每次运行前需要读取当前 node groups。

规则：

- 若本地 groups 为空，视为 `[default]`

这条规则必须和同步层中的默认 group 规则保持一致。

## 升级兼容

旧版升级到新版时，controller 不应误删所有 user。

依赖前提：

- 同步层已完成 bootstrap
- 本地 groups 若为空，已填充默认 group `default`
- bootstrap 导入的 user 若未设置 group，也已填充默认 group

在这三个条件成立后，controller 启动时会自然得出：

- 旧用户仍应存在于当前 node

从而避免升级后用户瞬时消失。

## 错误处理

controller 调用 `usermgr` 失败时，应：

- 记录错误日志
- 保留当前同步层状态不变
- 下一轮 reconcile 重试

不要因为某一次 reconcile 失败就回滚同步层数据。

## 与 usermgr 的边界

controller 只依赖 `usermgr` 公开能力，不进入其内部逻辑。

这意味着：

- user 的本地语义仍由 `usermgr` 决定
- controller 只负责决定“应不应该存在”和“是否需要更新”
- runtime 细节仍由 `usermgr` 与下层组件处理

## 第一版范围

包含：

- desired/actual 计算
- add/update/remove 决策
- 周期 reconcile
- 默认 group 兼容
- 幂等控制
- 仅在显式启用时启动

不包含：

- 更复杂的批量优化
- 更复杂的事件驱动机制
- 分阶段渐进迁移
- runtime 层细粒度修复

## 总结

placement controller 是一个位于：

- 用户同步层
- 现有 `usermgr`

之间的独立控制层。

它的职责不是管理 user 运行细节，而是根据：

- 同步得到的全局 user 元数据
- 本地 node group 配置

持续把本地 `usermgr` 收敛到正确状态。
