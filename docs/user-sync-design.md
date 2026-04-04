# User Sync 设计文档

## 目标

实现一层独立于 `usermgr` 的集群用户同步能力，作为后续“按 node 自适应装载 user”的基础设施。

约束：

- 不改动现有 `usermgr` 内部逻辑
- 用户同步层完全位于 `usermgr` 之上
- 允许最终一致，不追求强一致
- 默认基于现有心跳链路扩展，不引入 raft

## 核心思路

把用户信息拆成两层：

- 全局同步层：集群内需要传播和最终一致的 user 元数据
- 本地运行层：节点本地运行态，由现有 `usermgr` 继续负责

同步层只负责：

- 存储和传播全局 user 元数据
- 发现差异
- 拉取缺失或更新的数据
- 把最终结果投影给本地控制器

同步层不负责：

- 端口分配
- forward rule 管理
- 流量统计
- 本地 container/runtime 细节

这些仍由现有 `usermgr` 和其他本地组件负责。

## User 数据分层

### 集群级字段

以下字段属于全局 user 元数据，需要同步：

- `username`
- `password`
- `ttl`
- `expire`
- `role`
- `target_group`
- `deleted`
- `updated_at_us`
- `origin_node`

说明：

- `ttl` 和 `expire` 在同步层视为同一类“用户有效期配置”
- `target_group` 用于后续 node 自适应装载 user
- `deleted` 用于 tombstone 删除传播
- `updated_at_us + origin_node` 用于版本裁决

### 本地字段

以下字段不参与同步，只保留在本地运行层：

- 流量统计
- 转发端口
- 端口映射
- forward rules
- 本地装载状态
- 其他 runtime/cache 类字段

## ClusterUser 模型

建议在同步层新增独立模型：

```go
type ClusterUser struct {
    Username    string
    Password    string
    TTL         int64
    Expire      int64
    Role        string
    TargetGroup string
    Deleted     bool
    UpdatedAtUs int64
    OriginNode  string
}
```

说明：

- 这是同步层对象，不等同于现有 `usermgr` 内部 user 对象
- 同步层可以落到单独 store/table
- 后续由控制器把 `ClusterUser` 投影到本地 `usermgr`

## Node Group 管理

第一版不再引入 `ClusterGroup` 全局同步对象。

采用方案：

- 每个 node 只维护自己的本地 `groups`
- group 配置不通过心跳传播
- 管理面通过统一 `http -> grpc -> target node` 链路，直接写入目标 node 的本地 group 配置

这样做的结果：

- user 是全局同步对象
- node groups 是本地持久化配置
- 控制器通过“本地 groups + 全局 users”来决定本地应装载哪些 user

### 本地持久化

node 的 group 配置必须持久化到 store。

建议新增本地持久化对象：

- `local_node_groups`

要求：

- node 重启后 groups 可恢复
- controller 启动时可立即读取
- 不依赖外部重新下发

### 远程管理接口

建议通过现有全链路新增 node group 管理接口：

- HTTP：`PUT /api/node/:name/groups`
- gRPC：`SetNodeGroups(node, groups)`
- gRPC：`GetNodeGroups(node)`

第一版建议采用“整体替换”语义，不做 add/remove patch：

- 简单
- 幂等
- 容易对账

## 默认 Group 规则

必须定义默认 group，避免旧版升级到新版后出现“所有 user 都不匹配任何 node”的情况。

建议默认值：

- `default`

规则：

- 当 node 的 groups 为空时，自动填充 `default`
- 当 user 的 `target_group` 为空时，自动填充 `default`

填充时机：

- add user 时
- bootstrap 老用户时
- node groups 加载为空时

这样可以保证：

- 新版 controller 启动后不会把旧用户全部清掉
- 旧版升级到新版后行为保持连续

## 与现有 User 接口的兼容策略

新方案上线后，现有 user 相关 HTTP/gRPC 接口默认继续保留，作为“直接操作本地 usermgr”的旧路径。

原则：

- 旧接口保留，语义不变
- 新方案新增独立的 `ClusterUser` 接口
- 两套能力并存，由配置决定是否启用新方案

### 旧接口定位

现有 user 接口继续作为本地执行路径：

- 直接调用本地 `usermgr`
- 继续服务旧版流程
- 在 `cluster_user.enabled=false` 时保持原行为

这意味着：

- 旧接口不被新方案替代
- 旧接口是兼容层，也是本地运维入口

### ClearUsers 处理

当前 `ClearUsers` 与 `DeleteUsers` 语义重叠，实际底层都走本地删除逻辑。

因此设计上建议：

- 删除 `ClearUsers` 接口
- 保留 `DeleteUsers`
- 如果后续需要“真正物理清理同步层 tombstone 或 cluster user”的能力，再新增语义明确的新接口

## 配置开关

`ClusterUser` 方案必须通过显式配置启用，默认关闭。

建议配置：

```yaml
cluster_user:
  enabled: false
  sync_interval_sec: 5
  bootstrap_from_local: true
  default_group: default
```

说明：

- `enabled=false` 为默认值，保证旧版兼容
- `sync_interval_sec` 控制心跳差量同步周期
- `bootstrap_from_local` 控制首次从本地 `usermgr` 导入旧数据
- `default_group` 定义默认 group

### 关闭时行为

当 `cluster_user.enabled=false` 时：

- 不启动 user sync
- 不启动 placement controller
- 不启用新的 cluster user 管理接口
- 现有 user 相关 HTTP/gRPC 接口完全保持旧行为

### 开启时行为

当 `cluster_user.enabled=true` 时：

- 启动同步层
- 启动 placement controller
- 新增 `ClusterUser` 管理接口
- 旧接口仍可保留，但定位为“本地 usermgr 直连接口”

## 版本规则

本方案采用低复杂度版本裁决：

- `updated_at_us`
- `origin_node`

比较规则：

1. 先比较 `updated_at_us`
2. 若相等，再比较 `origin_node`

结论：

- 时间更新更晚的记录覆盖更早的记录
- 同一时间戳下，由 `origin_node` 作为稳定 tie-break

前提：

- 用户并发修改概率低
- 可接受最终一致

不引入：

- vector clock
- lamport clock
- hlc
- raft log version

## 删除语义

删除采用 tombstone，不做立即物理删除。

规则：

- 删除 user 时，写入 `deleted=true`
- 同时刷新 `updated_at_us` 和 `origin_node`
- 节点收到 tombstone 后，本地同步层保留该记录一段时间

这样可以避免：

- 掉线节点恢复后把旧 user 重新传播回来

物理清理策略：

- 后续可增加 tombstone 保留时间
- 超过保留期再做清理

第一版先只要求 tombstone 保留，不要求立刻实现 GC

## 同步传输分层

同步采用两层设计：

### 第一层：Digest

心跳默认只传摘要，不传全量 user 数据。

建议 digest 字段：

```go
type UserDigest struct {
    Username    string
    UpdatedAtUs int64
    OriginNode  string
    Deleted     bool
    Hash        string
}
```

其中：

- `Hash` 只覆盖全局字段
- 不包含任何本地 runtime 字段

推荐 hash 输入：

- `username`
- `password`
- `ttl`
- `expire`
- `role`
- `target_group`
- `deleted`
- `updated_at_us`
- `origin_node`

### 第二层：Payload Pull

只有在摘要不一致时才拉取完整 user。

触发条件：

- 本地没有该 user
- 远端版本更新
- 本地版本更新但需要对账
- 版本相同但 `Hash` 不同

拉取方式：

- `GetUser(username)`
- 或批量 `GetUsers([]username)`

## 同步流程

### 正常心跳

1. 节点 A 向节点 B 发送心跳
2. 心跳内带 `UserDigest`
3. 节点 B 对比本地摘要
4. 节点 B 返回需要补齐的对象列表
5. 节点 A 按需返回完整 payload
6. 节点 B 更新本地同步 store

### 节点恢复

1. 掉线节点恢复上线
2. 通过心跳拿到其他节点 digest
3. 对比后拉取落后的 user
4. 本地更新同步 store
5. 触发本地 reconcile

## 本地落盘

同步层数据必须本地落盘。

原因：

- 节点重启后可快速恢复同步视图
- 不必每次都依赖全量拉取
- 掉线恢复后只需补差量

建议新增持久化对象：

- `cluster_users`

本地 store 用于保存：

- 最新已知全局 user 元数据
- tombstone

## 旧版升级兼容

第一版必须考虑从旧版平滑升级。

### Bootstrap 原则

仅当本地 `cluster_users` 为空时，才允许从当前本地 user 数据初始化同步层。

这样可以避免：

- 每次重启都从 `usermgr` 反向覆盖同步层
- 已同步的数据被本地旧数据重复污染

### Bootstrap 数据来源

启动时：

1. 先初始化本地 node groups
2. 若 groups 为空，填充默认 group `default`
3. 检查本地 `cluster_users` 是否为空
4. 若为空，则调用 `usermgr.ListUsers()` 读取当前本地 user 列表
5. 为每个 user 生成一条 `ClusterUser`
6. 若 user 未设置 group，则填充默认 group `default`
7. `updated_at_us` 初始化为当前时间
8. `origin_node` 初始化为当前 node
9. 写入本地 `cluster_users`

### Bootstrap 时间戳

当一次性导入多个 user 时，建议保证 `updated_at_us` 单调递增。

简单做法：

- 以当前时间为基准
- 每导入一个 user，时间戳加 1

这样可以避免首次 bootstrap 时大量 user 拥有完全相同的版本。

### 兼容期冲突

如果旧版不同 node 上本来就存在同名 user 但内容不同，则升级后的首次同步会按：

- `updated_at_us`
- `origin_node`

自然收敛到一份最终记录。

这属于兼容期的预期行为。

## 与 usermgr 的边界

本方案明确要求不改动现有 `usermgr` 逻辑。

边界划分如下：

### 同步层职责

- 维护 `ClusterUser`
- 处理 digest 比较
- 处理差量拉取
- 本地持久化同步对象
- 处理 bootstrap 兼容逻辑

### 控制层职责

- 读取同步层状态
- 读取本地 node groups
- 判断当前 node 需要哪些 user
- 调用现有 `usermgr` 完成本地 add/update/remove

### usermgr 职责

- 保持现有本地用户管理语义
- 管理本地 runtime
- 管理本地端口/流量/forward 规则

换句话说：

- `usermgr` 仍然是本地用户执行层
- 新同步层只是它上方的“全局期望状态层”

## 第一版范围

第一版只做“用户同步基础层”，不做完整自动装载。

范围包含：

- `ClusterUser` 模型
- node group 本地持久化要求
- digest/hash 机制
- payload pull 机制
- 本地同步层持久化
- 版本裁决
- tombstone 传播
- 旧版升级 bootstrap
- 默认 group 规则
- 旧接口兼容策略
- 配置开关

第一版不包含：

- 基于 group 的自动 add/remove user 到 node 的控制器实现
- 更复杂的冲突合并策略
- tombstone GC
- 批量压缩同步优化

## 后续演进

在本方案完成后，下一步实现：

- node 自适应装载 user

思路是：

1. 每个 node 读取本地 groups
2. 遍历 `ClusterUser`
3. 若 `user.target_group` 命中本 node，则确保本地 user 存在
4. 若不命中，则确保本地 user 被移除
5. 整个过程通过控制层驱动现有 `usermgr`

## 总结

本方案的定位非常明确：

- 它不是新的 `usermgr`
- 它也不替代本地运行态管理
- 它只是为整个集群提供一个低复杂度、最终一致的 user 元数据同步层

这个同步层是后续“按 group 自适应把 user 装载到 node”的基础设施。
