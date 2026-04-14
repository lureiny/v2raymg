# Inbound 用户追踪架构统一重构方案

**状态**：待实施
**创建时间**：2026-04-10
**触发来源**：处理 review 问题 X-01 / SN-01 时发现三个容器的用户追踪架构不一致

## 背景

当前三个容器对"inbound 级别用户追踪"的处理方式完全不同：

| 容器 | Inbound 类型 | 用户追踪位置 | 数据结构 |
|---|---|---|---|
| **Xray** | `XrayInbound`（独立 struct） | `XrayInbound.addedUsers` | `map[string]struct{}` |
| **Hysteria** | 直接用 `inbound.DefaultInbound` | **不追踪**（走 HTTP 回调认证） | 无 |
| **Snell** | 直接用 `inbound.DefaultInbound` | `SnellContainer.addedUsers`（**容器级别**） | `map[string]struct{}` |

问题：
1. Xray 和 Snell 的追踪位置不一致（inbound 级 vs 容器级）
2. 三者没有共享的抽象，重复实现且都存在并发安全问题
3. 无法通过 `Inbound` 接口统一查询"这个 inbound 上有哪些用户"

## 核心设计思路

统一架构，用 `map[string]interface{}` 让 value 类型可变：
- xray/snell：value 是 `struct{}{}`，只追踪存在性
- hysteria：value 是密码，供 HTTP 回调查询使用

## 设计方案

### 1. 新增 `UserTracker` 类型

文件：`pkg/proxy/core/inbound/user_tracker.go`

```go
type UserTracker struct {
    mu    sync.Mutex
    users map[string]interface{}
}

func NewUserTracker() *UserTracker
func (t *UserTracker) Add(username string, data interface{})
func (t *UserTracker) Remove(username string)
func (t *UserTracker) Get(username string) (interface{}, bool)
func (t *UserTracker) Has(username string) bool
func (t *UserTracker) List() []string  // 返回 key 的快照
func (t *UserTracker) Len() int
```

### 2. `DefaultInbound` 内置 `*UserTracker`

文件：`pkg/proxy/core/inbound/default.go`

```go
type DefaultInbound struct {
    ...
    users *UserTracker  // 新增
}

func NewDefaultInbound(...) *DefaultInbound {
    return &DefaultInbound{
        ...,
        users: NewUserTracker(),
    }
}

func (i *DefaultInbound) Users() *UserTracker { return i.users }
```

### 3. `Inbound` 接口新增 `Users()` 方法

文件：`pkg/proxy/core/inbound/inbound.go`

```go
type Inbound interface {
    ...
    Users() *UserTracker  // 新增
}
```

### 4. XrayInbound 改造

两个选项：

**方案 A：嵌入 DefaultInbound**
- 优点：结构统一
- 缺点：大范围改动，字段/方法迁移风险高

**方案 B（推荐）：只加 `users *UserTracker` 字段，实现 `Users()` 方法**
- 优点：最小改动
- 缺点：XrayInbound 仍保持独立 struct 形态

### 5. Snell 改造

- 删除 `SnellContainer.addedUsers` 字段
- 所有 `sc.addedUsers` 访问替换为 `sc.inbound.Users().Add/Has/List/Remove`
- 通过 `sc.inbound.(*inbound.DefaultInbound).Users()` 访问，或直接通过接口 `sc.inbound.Users()`

### 6. Hysteria 改造

**需要先调研的问题**：
- Hysteria 当前的 HTTP 回调是直接查 `usermanager` 验证用户，还是查 inbound 本地缓存？
- 把用户信息放到 inbound 本地，是为了减少 usermanager 查询，还是有其他考虑？

**可能的改造方向**：
- 在 Hysteria inbound 的 UserTracker 中存密码（value 为 password string）
- HTTP 回调优先查本地 tracker，未命中再查 usermanager
- 这是一个行为变更，需要评估对现有认证流程的影响

## 影响范围

| 文件 | 改动 |
|---|---|
| `pkg/proxy/core/inbound/user_tracker.go` | 新建 |
| `pkg/proxy/core/inbound/inbound.go` | 接口加 `Users()` |
| `pkg/proxy/core/inbound/default.go` | 加 `users` 字段和方法 |
| `pkg/proxy/containers/xray/exec_runner.go` | XrayInbound 加字段和方法，所有 `addedUsers` 访问替换 |
| `pkg/proxy/containers/snell/container.go` | 删 `addedUsers`，替换为 `sc.inbound.Users()` |
| `pkg/proxy/containers/hysteria/container.go` | 视调研结果决定是否改造 |

## 与 review 问题的关系

完成此重构可一次性关闭：
- **X-01**：`XrayInbound.addedUsers` 并发读写 data race
- **SN-01**：`SnellContainer.addedUsers` 并发读写 data race

## 当前处理策略

**暂时不实施此重构**。先用最小化改动（给各自的 map 加独立锁）分别解决 X-01 和 SN-01 的 data race，避免一次动太多文件。

此文档作为后续架构统一的设计参考，在所有 P0/P1 review 问题处理完后再评估是否实施。
