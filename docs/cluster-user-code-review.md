# Cluster User Code Review

## Findings

### 1. HTTP 写接口集成测试断言还不够强
- 严重级别：低危
- 相关文件：
  - `/home/node/.openclaw/projects/v2raymg/pkg/http/cluster_user_grpc_integration_test.go`
- 问题描述：
  - 当前 HTTP 写接口的集成测试已经补上：
    - `POST /api/cluster-users`
    - `PUT /api/cluster-users/:name`
    - `DELETE /api/cluster-users/:name`
    - `PUT /api/node/:name/groups`
  - 但这些测试目前主要只断言：
    - HTTP 200
    - body 包含 `Succ`
  - 测试文件里的 gRPC stub 已经保留了：
    - `lastUpsertReq`
    - `lastDeleteReq`
    - `lastSetGroups`
    但当前成功路径测试没有断言这些字段。
- 风险：
  - 仍可能漏掉以下类型的问题：
    - `PUT /cluster-users/:name` 没把 path 中的用户名正确写入请求
    - `POST /cluster-users` 字段映射错误
    - `PUT /node/:name/groups` 没有原样透传 groups
    - delete 写到了错误用户名
- 修改建议：
  - 在成功路径测试中显式断言 stub 记录到的请求内容。
  - 把 HTTP 层“请求被正确翻译成 gRPC 请求”也纳入测试目标，而不只是看返回码。
- 验收建议：
  - `TestClusterUserAddHandler_Success` 断言 `lastUpsertReq.Users[0]`
  - `TestClusterUserUpdateHandler_Success` 断言 path 用户名被正确带入
  - `TestClusterUserDeleteHandler_Success` 断言 `lastDeleteReq.Usernames`
  - `TestNodeGroupsSetHandler_Success` 断言 `lastSetGroups`

### 2. 下游失败聚合测试还没有覆盖全部写接口
- 严重级别：低危
- 相关文件：
  - `/home/node/.openclaw/projects/v2raymg/pkg/http/cluster_user_grpc_integration_test.go`
- 问题描述：
  - 当前已经补了两条下游失败聚合测试：
    - `GET /api/node/:name/groups`
    - `POST /api/cluster-users`
  - 但其余写接口在失败时走的是同类分支，目前还没有对应 case：
    - `PUT /api/cluster-users/:name`
    - `DELETE /api/cluster-users/:name`
    - `PUT /api/node/:name/groups`
- 风险：
  - 这已经不是结构性问题，只是剩余的覆盖缺口。
- 修改建议：
  - 为剩余三条写接口各补一条失败聚合测试。
- 验收建议：
  - 验证失败时不会返回 `Succ`
  - 验证失败时不会误返回正常 JSON 结构
  - 验证返回体确实进入错误聚合分支

## Summary
- 整体看，这套测试现在已经比较完整。
- 当前没有再看到中高危问题。
- 剩余问题主要是把已有集成测试做得更严一些：
  - 成功路径断言请求内容
  - 失败路径把其余写接口补齐

## Residual Risk
- 本次仍然是静态 review，没有实际执行测试。
