# Cluster User Proto 草案

## 目标

给 `ClusterUser` 方案提供一份可直接落地到 `rpc_server.proto` 的 proto 草案，覆盖：

- 新增 message
- 新增 rpc
- 对现有心跳的扩展建议

本文件只描述设计草案，不代表已实现。

## 设计原则

- 旧接口尽量不动
- 新能力通过新增 proto message 和 rpc 接入
- `UserDigest` 直接挂到现有心跳链路
- `ClearUsers` 作为待移除接口

## 新增 Message

### Node Groups

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

### ClusterUser

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
```

### ClusterUser 查询与写入

```proto
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

### 心跳差量请求/响应补充

当前建议直接扩展现有心跳消息：

```proto
message HeartBeatReq {
    NodeAuthInfo node_auth_info = 1;
    repeated UserDigest user_digests = 2;
}

message HeartBeatRsp {
    int32 code = 1;
    string msg = 2;
    map<string, Node> nodesMap = 3;
    repeated string need_cluster_users = 4;
}
```

说明：

- `user_digests`：本节点当前持有的摘要列表
- `need_cluster_users`：对端需要我补发完整 payload 的用户名列表

这样可复用原有心跳链路，不额外引入单独的 digest 对账 rpc。

## 新增 RPC 草案

建议在 `service EndNodeAccess` 中增加：

```proto
rpc GetNodeGroups(GetNodeGroupsReq) returns (GetNodeGroupsRsp) {}
rpc SetNodeGroups(SetNodeGroupsReq) returns (SetNodeGroupsRsp) {}

rpc ListClusterUsers(ListClusterUsersReq) returns (ListClusterUsersRsp) {}
rpc GetClusterUsersByName(GetClusterUsersByNameReq) returns (GetClusterUsersByNameRsp) {}
rpc UpsertClusterUsers(UpsertClusterUsersReq) returns (UpsertClusterUsersRsp) {}
rpc DeleteClusterUsers(DeleteClusterUsersReq) returns (DeleteClusterUsersRsp) {}
```

## 旧 RPC 的处理建议

### 保留

以下接口继续保留：

```proto
rpc GetUsers(GetUsersReq) returns (GetUsersRsp) {}
rpc AddUsers(UserOpReq) returns (UserOpRsp) {}
rpc DeleteUsers(UserOpReq) returns (UserOpRsp) {}
rpc UpdateUsers(UserOpReq) returns (UserOpRsp) {}
rpc ResetUser(UserOpReq) returns (UserOpRsp) {}
rpc GetSub(GetSubReq) returns (GetSubRsp) {}
rpc GetBandWidthStats(GetBandwidthStatsReq) returns (GetBandwidthStatsRsp) {}
```

语义：

- 它们继续表示“本地 usermgr 执行路径”

### 待移除

```proto
rpc ClearUsers(ClearUsersReq) returns (ClearUsersRsp) {}
```

原因：

- 与 `DeleteUsers` 语义重叠
- 当前设计中不再保留这一层删除语义

## Message 与 HTTP 的对应关系

### Node Groups

- HTTP `GET /api/node/:name/groups`
  - gRPC `GetNodeGroups`
- HTTP `PUT /api/node/:name/groups`
  - gRPC `SetNodeGroups`

### ClusterUser

- HTTP `GET /api/cluster-users`
  - gRPC `ListClusterUsers`
- HTTP `POST /api/cluster-users`
  - gRPC `UpsertClusterUsers`
- HTTP `PUT /api/cluster-users/:name`
  - gRPC `UpsertClusterUsers`
- HTTP `DELETE /api/cluster-users/:name`
  - gRPC `DeleteClusterUsers`

## 兼容性说明

- 当 `cluster_user.enabled=false` 时，新 rpc 即使已经定义，也可以不注册业务处理，或返回 disabled
- 旧 rpc 保持原有语义
- 这样 proto 可以先扩展，业务按开关逐步启用

## 总结

这份 proto 草案的核心是：

- 新增 `ClusterUser` 与 node group 相关的 message/rpc
- 复用心跳承载 `UserDigest`
- 保留旧的本地 user 接口
- 后续通过开关决定是否真正启用新路径
