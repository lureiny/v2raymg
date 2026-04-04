# Cluster User API 与 Schema 草案

## 目标

补充 `ClusterUser` 方案的实现细节，覆盖：

- 配置结构
- 本地表结构
- HTTP 接口草案
- gRPC 接口草案
- digest 同步协议
- reconcile 规则表

本文件仅描述设计，不代表已实现。

## 配置草案

建议新增配置块：

```yaml
cluster_user:
  enabled: false
  sync_interval_sec: 5
  bootstrap_from_local: true
  default_group: default
```

字段说明：

- `enabled`
  - 是否启用 `ClusterUser` 同步层与 placement controller
  - 默认 `false`
- `sync_interval_sec`
  - 心跳差量同步周期
  - 默认 `5`
- `bootstrap_from_local`
  - 首次发现本地 `cluster_users` 为空时，是否从本地 `usermgr` 导入旧 user
  - 默认 `true`
- `default_group`
  - 默认 group 名称
  - 默认 `default`

## 表结构草案

### 表：cluster_users

用途：

- 持久化本地已知的全局 user 元数据
- 支持心跳差量同步
- 支持 tombstone

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| username | TEXT PRIMARY KEY | 用户唯一标识 |
| password | TEXT NOT NULL | 全局同步的代理密码 |
| expire | INTEGER NOT NULL DEFAULT 0 | 绝对过期时间戳，0 表示永不过期 |
| role | TEXT NOT NULL DEFAULT 'normal' | 登录角色 |
| target_group | TEXT NOT NULL DEFAULT 'default' | 目标 group |
| deleted | INTEGER NOT NULL DEFAULT 0 | tombstone 标记，0/1 |
| updated_at_us | INTEGER NOT NULL | 版本时间戳（微秒） |
| origin_node | TEXT NOT NULL | 版本来源节点 |
| hash | TEXT NOT NULL | 当前全局字段摘要 |
| created_at | INTEGER NOT NULL | 本地创建时间 |
| updated_at | INTEGER NOT NULL | 本地更新时间 |

建议索引：

- `idx_cluster_users_target_group(target_group)`
- `idx_cluster_users_updated_at_us(updated_at_us)`
- `idx_cluster_users_group_deleted(target_group, deleted)`

约束：

- `username` 全局唯一
- `target_group` 为空时写入前补默认值
- `origin_node` 不可为空

说明：

- 同步层统一只存 `expire`
- 旧接口中的 `ttl` 在进入同步层前统一换算成 `expire`
- 同步层内部不再保留独立 `ttl` 字段

### 表：local_node_groups

用途：

- 存储当前 node 的本地 groups

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| group_name | TEXT PRIMARY KEY | group 名称 |
| created_at | INTEGER NOT NULL | 创建时间 |
| updated_at | INTEGER NOT NULL | 更新时间 |

说明：

- 当前设计只存“本 node 的 groups”
- 因此不需要 `node_name` 字段
- 当表为空时，读取层返回 `[default]`

## 版本与 Hash 规则

### 版本字段

采用：

- `updated_at_us`
- `origin_node`

比较规则：

1. 先比较 `updated_at_us`
2. 若相等，比较 `origin_node`

### Hash 输入字段

`hash` 必须只覆盖全局字段：

- `username`
- `password`
- `expire`
- `role`
- `target_group`
- `deleted`
- `updated_at_us`
- `origin_node`

不包含：

- 流量统计
- 本地端口
- forward 规则
- 任何 runtime 字段

## HTTP 接口草案

### 旧接口

旧接口继续保留，默认行为不变：

- `GET /api/user`
- `POST /api/user`
- `PUT /api/user`
- `DELETE /api/user`
- `POST /api/user/reset`

语义：

- 这些接口继续表示“直接操作本地 usermgr”
- 在 `cluster_user.enabled=false` 时，行为完全不变

### 删除接口

建议移除：

- `DELETE /api/users`（对应 `ClearUsers`）

原因：

- 与 `DeleteUsers` 语义重叠
- 当前实际实现未形成真正不同的删除层级

### 新接口：Node Groups

#### `GET /api/node/:name/groups`

说明：

- 获取目标 node 的本地 groups

返回：

```json
{
  "groups": ["default", "hk", "premium"]
}
```

#### `PUT /api/node/:name/groups`

说明：

- 整体替换目标 node 的本地 groups

请求：

```json
{
  "groups": ["default", "hk"]
}
```

规则：

- 若传空数组，则本地最终仍应回填 `default`

### 新接口：ClusterUser

这些接口仅在 `cluster_user.enabled=true` 时启用。

#### `GET /api/cluster-users`

参数：

- `group` 可选，仅筛选某个 target_group

返回：

```json
{
  "users": [
    {
      "username": "alice",
      "password": "xxx",
      "expire": 0,
      "role": "normal",
      "target_group": "default",
      "deleted": false,
      "updated_at_us": 123456789,
      "origin_node": "node-a"
    }
  ]
}
```

#### `POST /api/cluster-users`

说明：

- 新增或覆盖 `ClusterUser`

请求：

```json
{
  "username": "alice",
  "password": "xxx",
  "expire": 0,
  "role": "normal",
  "target_group": "default"
}
```

规则：

- `target_group` 为空时补默认值
- 自动写入 `updated_at_us` 与 `origin_node`

#### `PUT /api/cluster-users/:name`

说明：

- 更新指定 `ClusterUser`

#### `DELETE /api/cluster-users/:name`

说明：

- 逻辑删除，对应 tombstone
- 设置 `deleted=true`
- 更新时间戳和来源节点

## gRPC 接口草案

### 旧接口保留

现有本地 user 接口继续保留：

- `GetUsers`
- `AddUsers`
- `DeleteUsers`
- `UpdateUsers`
- `ResetUser`
- `GetSub`
- `GetBandWidthStats`

建议移除：

- `ClearUsers`

### 新接口：Node Groups

```proto
message GetNodeGroupsReq {
    NodeAuthInfo node_auth_info = 1;
}

message GetNodeGroupsRsp {
    int32 code = 1;
    string msg = 2;
    repeated string groups = 3;
}

message SetNodeGroupsReq {
    NodeAuthInfo node_auth_info = 1;
    repeated string groups = 2;
}

message SetNodeGroupsRsp {
    int32 code = 1;
    string msg = 2;
}
```

### 新接口：ClusterUser

```proto
message ClusterUserInfo {
    string username = 1;
    string password = 2;
    int64 expire = 3;
    string role = 4;
    string target_group = 5;
    bool deleted = 6;
    int64 updated_at_us = 7;
    string origin_node = 8;
    string hash = 9;
}

message ListClusterUsersReq {
    NodeAuthInfo node_auth_info = 1;
    string group = 2;
}

message ListClusterUsersRsp {
    int32 code = 1;
    string msg = 2;
    repeated ClusterUserInfo users = 3;
}

message GetClusterUsersByNameReq {
    NodeAuthInfo node_auth_info = 1;
    repeated string usernames = 2;
}

message GetClusterUsersByNameRsp {
    int32 code = 1;
    string msg = 2;
    repeated ClusterUserInfo users = 3;
}

message UpsertClusterUsersReq {
    NodeAuthInfo node_auth_info = 1;
    repeated ClusterUserInfo users = 2;
}

message UpsertClusterUsersRsp {
    int32 code = 1;
    string msg = 2;
}

message DeleteClusterUsersReq {
    NodeAuthInfo node_auth_info = 1;
    repeated string usernames = 2;
}

message DeleteClusterUsersRsp {
    int32 code = 1;
    string msg = 2;
}
```

## Digest 同步协议草案

### UserDigest

```proto
message UserDigest {
    string username = 1;
    int64 updated_at_us = 2;
    string origin_node = 3;
    bool deleted = 4;
    string hash = 5;
}
```

### 心跳扩展

当前方案已确定：

- `UserDigest` 直接挂到现有心跳消息里

目标：

- 复用现有节点发现/保活链路
- 不额外新增独立 digest 对账 RPC

### 对账流程

1. 节点 A 向节点 B 发送心跳，并附带本地 digest 列表
2. 节点 B 逐条比较本地 `cluster_users`
3. 节点 B 识别：
   - 本地缺失
   - 本地版本落后
   - 版本一致但 hash 不同
4. 节点 B 返回需要完整 payload 的用户名列表
5. 节点 A 通过 `GetClusterUsersByName` 或直接响应 payload，返回完整记录
6. 节点 B 应用记录并更新本地 store

## Reconcile 规则表

### 输入

- 本地 `ClusterUser`
- 本地 node groups
- 本地 `usermgr.ListUsers()`

### 输出

- `usermgr.AddUser`
- `usermgr.UpdateUser`
- `usermgr.RemoveUser`

### 规则

| ClusterUser 状态 | group 是否命中本地 | 本地 user 是否存在 | 动作 |
|---|---|---|---|
| deleted=true | 无关 | 存在 | Remove |
| deleted=true | 无关 | 不存在 | 无操作 |
| deleted=false | 命中 | 不存在 | Add |
| deleted=false | 命中 | 存在但密码/expire等不一致 | Update |
| deleted=false | 命中 | 存在且一致 | 无操作 |
| deleted=false | 不命中 | 存在 | Remove |
| deleted=false | 不命中 | 不存在 | 无操作 |

## 升级兼容规则

### 首次启用新功能

当：

- `cluster_user.enabled=true`
- 本地 `cluster_users` 为空

则：

1. 读取本地 `usermgr.ListUsers()`
2. 将旧 user 转换为 `ClusterUser`
3. 若旧输入来自 `ttl`，先统一换算成 `expire`
4. 空 group 写入 `default`
5. 空 groups 写入本地 `default`
6. 写入 `cluster_users`
7. 再启动同步层与 placement controller

### 关闭新功能

当：

- `cluster_user.enabled=false`

则：

- 不使用 `cluster_users`
- 不使用 placement controller
- 所有旧 user 接口继续按本地路径执行

## 已确认决策

以下两点已确认：

- `UserDigest` 直接挂到现有心跳消息里
- 同步层统一存 `expire`，不再保留独立 `ttl`
