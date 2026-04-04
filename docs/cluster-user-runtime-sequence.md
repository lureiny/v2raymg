# Cluster User 启动与运行时序文档

## 目标

补充 `ClusterUser` 方案在运行期的关键时序，明确：

- server 启动顺序
- `cluster_user.enabled=false` 时的行为
- `cluster_user.enabled=true` 时的行为
- bootstrap、sync、placement controller 的依赖关系

本文件只描述设计，不代表已实现。

## 模式划分

系统分为两种运行模式：

- 模式 A：`cluster_user.enabled=false`
- 模式 B：`cluster_user.enabled=true`

这两种模式的核心区别在于：

- 是否启动同步层
- 是否启动 placement controller
- 是否启用新的 `ClusterUser`/node group 管理接口

## 模式 A：关闭 ClusterUser

### 目标

保持与旧版完全一致的运行行为。

### 启动顺序

1. 加载配置
2. 初始化 store
3. 初始化 `forwardMgr`
4. 初始化本地 `usermgr`
5. 初始化 cluster/node/rpc/http 等原有模块
6. 不初始化 `cluster_users` 逻辑
7. 不初始化 `local_node_groups` 逻辑
8. 不启动 user sync
9. 不启动 placement controller

### 行为特征

- 现有 `GET/POST/PUT/DELETE /api/user` 继续直接操作本地 `usermgr`
- 现有 gRPC user 接口继续直接操作本地 `usermgr`
- 没有 `ClusterUser` 最终一致同步
- 没有基于 group 的自适应装载

### 兼容性目标

- 旧配置不需要修改即可启动
- 旧接口返回结果不发生语义变化
- 旧流程的运行时行为不受新方案影响

## 模式 B：开启 ClusterUser

### 目标

在不修改 `usermgr` 内部逻辑的前提下，增加：

- `ClusterUser` 同步层
- node 本地 group 配置
- placement controller

### 启动顺序

建议顺序如下：

1. 加载配置
2. 初始化基础 store
3. 初始化 `forwardMgr`
4. 初始化本地 `usermgr`
5. 初始化 `cluster_users` store
6. 初始化 `local_node_groups` store
7. 读取本地 node groups
8. 若 groups 为空，则填充默认 group `default`
9. 检查本地 `cluster_users` 是否为空
10. 若为空且 `bootstrap_from_local=true`，从 `usermgr.ListUsers()` 导入旧 user
11. 初始化 cluster/node/rpc/http 等原有模块
12. 启动 user sync
13. 启动 placement controller
14. 开放 `ClusterUser`/node group 新接口

### 依赖关系

必须满足：

- `usermgr` 先于 bootstrap 初始化
- `cluster_users` store 先于 user sync 初始化
- `local_node_groups` 先于 placement controller 初始化
- user sync 与 placement controller 都只能在 `enabled=true` 时启动

## Bootstrap 时序

### 触发条件

同时满足以下条件时触发 bootstrap：

- `cluster_user.enabled=true`
- `cluster_users` 本地表为空
- `bootstrap_from_local=true`

### 执行流程

1. 调用本地 `usermgr.ListUsers()`
2. 遍历本地 user 列表
3. 将每个 user 转为 `ClusterUser`
4. 空 group 填默认 group `default`
5. 若旧输入只提供 `ttl`，则换算成 `expire`
6. 生成 `updated_at_us`
7. 写入 `origin_node`
8. 落盘到 `cluster_users`
9. bootstrap 完成后，再启动 user sync 和 placement controller

### 目的

- 保证旧版升级到新版时，本地已有 user 不会因为同步层为空而丢失
- 保证 controller 第一次运行时能得到正确的 `desired users`

## User Sync 时序

### 正常周期

每个同步周期：

1. 节点构造本地 `UserDigest` 列表
2. 将 `UserDigest` 直接挂到现有心跳中
3. 对端接收心跳并比较本地摘要
4. 对端找出：
   - 本地缺失的 user
   - 版本落后的 user
   - 版本一致但 hash 不同的 user
5. 对端返回差异请求
6. 发送方返回完整 `ClusterUser`
7. 接收方更新本地 `cluster_users`

### 节点恢复

1. 节点重新上线
2. 通过心跳拿到其他节点 digest
3. 识别缺失/落后 user
4. 拉取完整 payload
5. 更新本地 `cluster_users`
6. 触发下一轮 reconcile

## Placement Controller 时序

### 输入准备

每轮 reconcile 前，controller 读取：

- 本地 `ClusterUser` 列表
- 本地 node groups
- 本地 `usermgr.ListUsers()`

### 运行流程

1. 读取 node groups
2. 若为空，按规则视为 `[default]`
3. 遍历 `ClusterUser`
4. 计算 `desired users`
5. 读取本地 `actual users`
6. 比较 `desired` 与 `actual`
7. 逐个执行：
   - Add
   - Update
   - Remove
8. 记录日志
9. 等待下一轮调度

### 调度方式

第一版建议：

- 周期轮询

例如：

- 每 5 秒执行一次

后续可扩展：

- user sync 更新后触发
- node groups 变更后触发
- 周期轮询保底

## 运行时场景

### 场景 1：新增 ClusterUser

1. 管理接口写入新的 `ClusterUser`
2. 本地更新 `cluster_users`
3. 通过心跳同步到其他节点
4. 各节点 placement controller 判断 group 是否命中
5. 命中的节点调用本地 `usermgr.AddUser`

### 场景 2：修改 user group

1. 管理接口更新 `target_group`
2. 通过心跳同步到其他节点
3. 原命中节点在下一轮 reconcile 中移除该 user
4. 新命中节点在下一轮 reconcile 中添加该 user

### 场景 3：删除 user

1. 管理接口将 user 标记为 `deleted=true`
2. 通过心跳传播 tombstone
3. 所有节点在 reconcile 中将本地 user 删除
4. tombstone 暂时保留，避免旧数据回流

### 场景 4：修改 node groups

1. 管理节点通过 HTTP/gRPC 修改目标 node 的本地 groups
2. 目标 node 将 groups 落盘
3. placement controller 下一轮读取新 groups
4. 根据新的 group 匹配关系调整本地 user 集合

## 失败与重试

### user sync 失败

- 本轮差量同步失败只影响本轮
- 下一轮心跳继续重试
- 不回滚本地已确认的 `cluster_users`

### placement reconcile 失败

- 某个 user 的 add/update/remove 失败后，只记录错误
- 不修改同步层状态
- 下一轮继续重试

### node groups 读取为空

- 按规则视为 `[default]`
- 避免因为本地 groups 为空而误删所有 user

## 模式切换注意点

### 从 false 切到 true

顺序建议：

1. 修改配置为 `enabled=true`
2. 重启节点
3. 执行 bootstrap
4. 启动 sync 与 controller

### 从 true 切到 false

顺序建议：

1. 修改配置为 `enabled=false`
2. 重启节点
3. 停止 sync 与 controller
4. 回到纯本地 `usermgr` 模式

说明：

- 关闭后不再以 `cluster_users` 驱动本地状态
- 但本地持久化数据可以保留，以便后续重新启用

## 总结

最终时序关系可以概括为：

- `usermgr` 始终是本地执行层
- `ClusterUser` 是上层同步状态
- node groups 是本地持久化配置
- placement controller 负责把同步状态收敛到本地 `usermgr`

并且：

- `enabled=false` 时，系统行为退化为旧版
- `enabled=true` 时，系统进入“最终一致同步 + 本地收敛”模式
