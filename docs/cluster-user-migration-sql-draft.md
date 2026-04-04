# Cluster User Migration SQL 草案

## 目标

提供 `ClusterUser` 方案第一版所需的数据库 migration SQL 草案，覆盖：

- `cluster_users`
- `local_node_groups`
- 相关索引

本文件只描述设计草案，不代表已实现。

## 设计原则

- 不修改现有 `users` 表语义
- 新功能使用独立表，避免影响旧版路径
- 默认 group 通过初始化逻辑保证，不依赖数据库触发器

## Migration 草案

### 创建表：cluster_users

```sql
CREATE TABLE IF NOT EXISTS cluster_users (
    username TEXT PRIMARY KEY,
    password TEXT NOT NULL,
    expire INTEGER NOT NULL DEFAULT 0,
    role TEXT NOT NULL DEFAULT 'normal',
    target_group TEXT NOT NULL DEFAULT 'default',
    deleted INTEGER NOT NULL DEFAULT 0,
    updated_at_us INTEGER NOT NULL,
    origin_node TEXT NOT NULL,
    hash TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

### 索引：cluster_users

```sql
CREATE INDEX IF NOT EXISTS idx_cluster_users_target_group
ON cluster_users(target_group);

CREATE INDEX IF NOT EXISTS idx_cluster_users_updated_at_us
ON cluster_users(updated_at_us);

CREATE INDEX IF NOT EXISTS idx_cluster_users_group_deleted
ON cluster_users(target_group, deleted);
```

### 创建表：local_node_groups

```sql
CREATE TABLE IF NOT EXISTS local_node_groups (
    group_name TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

## 字段说明

### cluster_users

- `username`
  - 全局唯一 user 标识
- `password`
  - 全局同步的代理密码
- `expire`
  - 绝对过期时间戳，0 表示永不过期
- `role`
  - 登录角色
- `target_group`
  - 目标 group，写入前必须补默认值
- `deleted`
  - tombstone 标记，0/1
- `updated_at_us`
  - 版本时间戳（微秒）
- `origin_node`
  - 版本来源 node
- `hash`
  - 当前全局字段摘要
- `created_at`
  - 本地创建时间
- `updated_at`
  - 本地更新时间

### local_node_groups

- `group_name`
  - 当前 node 所属 group
- `created_at`
  - 本地创建时间
- `updated_at`
  - 本地更新时间

## 初始化与默认值说明

### local_node_groups

数据库层不强制插入默认 group。

原因：

- 默认 group 是运行时语义
- 应由启动逻辑或写入逻辑保证
- 避免 migration 在不启用新功能时产生副作用

运行时规则：

- 若 `local_node_groups` 表为空，则读取层返回 `[default]`

### cluster_users

migration 不负责导入旧 user。

导入旧版 user 的逻辑由 bootstrap 完成：

- 仅在 `cluster_user.enabled=true`
- 且 `cluster_users` 为空时执行

## 回滚策略

若需要回滚 migration，理论上可执行：

```sql
DROP TABLE IF EXISTS local_node_groups;
DROP TABLE IF EXISTS cluster_users;
```

但建议：

- 第一版不要自动提供 destructive rollback
- 保留表，以便配置开关关闭后未来重新启用

## 与现有 users 表的关系

第一版中：

- `users` 表继续服务现有 `usermgr`
- `cluster_users` 表服务同步层
- 二者并存

这意味着：

- 不修改 `users` 表结构
- 不把同步层字段混入现有 `users` 表
- 降低对旧版链路的影响

## 未来扩展预留

如后续需要，可在 `cluster_users` 增加：

- `payload_version`
- `tombstone_expire_at`
- `last_sync_at`
- `notes` 或扩展字段

第一版不建议预留过多字段，保持最小可用结构即可。

## 总结

这份 SQL 草案的核心是：

- 以独立表方式引入 `ClusterUser` 能力
- 不影响现有 `users` 表与 `usermgr` 路径
- 把升级兼容交给 bootstrap，不放到 migration 里处理
