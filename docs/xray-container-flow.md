# Xray Container 流程图详解

本文档提供 xray container 的详细流程图解，是对架构文档的补充。

---

## 一、容器生命周期流程

### 1.1 启动流程 (Startup)

```mermaid
flowchart TD
    A[Caller: Startup] --> B{二进制存在?}
    B -->|否| C{AutoDownload?}
    C -->|是| D[调用 Updater]
    C -->|否| E[返回错误]
    D --> F{下载成功?}
    F -->|否| E
    F -->|是| G[继续]
    B -->|是| G

    G --> H{配置文件存在?}
    H -->|否| I[生成默认配置]
    H -->|是| J[继续]
    I --> J

    J --> K[BaseContainer.Start]
    K --> L[executorHooks.GetRunFunc]
    L --> M[process.Runner.Start]
    M --> N[exec.Command xray run -c config.json]
    N --> O{进程启动成功?}
    O -->|否| P[返回错误]
    O -->|是| Q[MarkRunning]
    Q --> R[返回成功]
```

**关键代码**: `containers/xray/exec_runner.go:215-232`

### 1.2 停止流程 (Stop)

```mermaid
flowchart TD
    A[Caller: Stop] --> B[BaseContainer.Stop]
    B --> C{当前状态}
    C -->|Stopped| D[直接返回]
    C -->|Running| E[切换到 Stopping]
    E --> F[关闭 stopChan]
    F --> G{stopFunc 存在?}
    G -->|是| H[调用 stopFunc]
    G -->|否| I[跳过]
    H --> I
    I --> J[MarkStopped]
    J --> K[返回成功]
```

---

## 二、Inbound 管理流程

### 2.1 添加 Inbound

```mermaid
flowchart TD
    A[AddInboundConfig] --> B{Validate?}
    B -->|否| C[返回错误]
    B -->|是| D{Tag 已存在?}
    D -->|是| E[返回错误]
    D -->|否| F[转换为 XrayInbound]
    F --> G[从 Inbound.ToNative 获取原生 JSON]
    G --> H[存入 inbounds map]
    H --> I[返回成功]
```

**关键代码**: `containers/xray/exec_runner.go:294-341`

### 2.2 运行时添加 Inbound (gRPC)

```mermaid
sequenceDiagram
    participant Caller
    participant Executor
    participant XrayAPI
    participant Xray

    Caller->>Executor: AddInboundNative(JSON)
    Executor->>XrayAPI: AddInbound(nativeJSON)
    XrayAPI->>Xray: gRPC AddInbound
    Xray-->>XrayAPI: 响应
    XrayAPI-->>Executor: 成功/错误
    Executor-->>Caller: 成功/错误
```

---

## 三、用户管理流程

### 3.1 添加用户并绑定端口

```mermaid
sequenceDiagram
    participant Caller
    participant UM as UserManager
    participant FM as ForwardManager
    participant PA as PortAllocator

    Caller->>UM: AddUser + GetBindPort

    UM->>UM: AddUser() 创建用户

    UM->>FM: AddRule(ForwardRule)

    FM->>PA: Allocate()
    PA-->>FM: 端口 20001

    FM->>FM: 创建 Relay 监听 20001

    FM-->>UM: ForwardRule{ListenPort: 20001}

    UM-->>Caller: 返回 BindPort=20001
```

**关键代码**: `usermanager/usermanager.go:128-160, 300-352`

### 3.2 用户事件处理

```mermaid
flowchart TD
    A[UserManager 内部] --> B{用户操作}
    B -->|AddUser| C[emitEvent: UserEventAdd]
    B -->|RemoveUser| D[emitEvent: UserEventRemove]
    B -->|UpdatePassword| E[emitEvent: UserEventUpdate]
    B -->|过期| F[emitEvent: UserEventExpire]
    B -->|GetBindPort| G[emitEvent: UserEventPortBind]

    C --> H[写入 eventCh]
    D --> H
    E --> H
    F --> H
    G --> H

    H --> I{Container 订阅}
    I -->|是| J[Executor.forwardUserEvents]
    J --> K[待实现: 分发到 Inbound]
```

**注意**: `Executor.forwardUserEvents` 已存在但未完整实现事件分发逻辑

---

## 四、订阅生成流程

### 4.1 完整订阅生成

```mermaid
flowchart TD
    A[GetUserSubscriptions] --> B{参数检查}
    B -->|失败| C[返回错误]
    B -->|成功| D[获取读锁]

    D --> E[遍历 inbounds]
    E --> F{每个 inbound}
    F --> G[generateSubscriptionSpec]

    G --> H{端口优先级}
    H -->|req.Port 显式| I[使用 req.Port]
    H -->|forwardPort 设置| J[使用 inbound.forwardPort]
    H -->|UserManager 存在| K[调用 GetUserPort]
    K --> L{找到?}
    L -->|是| M[使用 forwardPort]
    L -->|否| N[使用 inbound.port]
    H -->|默认| N

    I --> O[提取凭证]
    J --> O
    M --> O
    N --> O

    O --> P[buildSubscriptionExtensions]
    P --> Q[generateURI]
    Q --> R[返回 SubscriptionSpec]

    E -->|结束| S[释放锁]
    S --> T[返回所有 specs]
```

**关键代码**: `containers/xray/subscription.go:27-110`

### 4.2 协议 URI 生成

```mermaid
flowchart LR
    A[SubscriptionSpec] --> B{Protocol}
    B -->|VLESS| C[generateVLESSURI]
    B -->|VMess| D[generateVMessURI]
    B -->|Trojan| E[generateTrojanURI]
    B -->|Shadowsocks| F[generateShadowsocksURI]
    B -->|SOCKS5| G[generateSOCKS5URI]

    C --> H[vless://UUID@host:port?params#node]
    D --> I[vmess://base64(JSON)]
    E --> J[trojan://pass@host:port?params#node]
    F --> K[ss://base64(method:pass)@host:port]
    G --> L[socks://base64(user:pass)@host:port]
```

---

## 五、端口转发流程

### 5.1 添加转发规则

```mermaid
sequenceDiagram
    participant Caller
    participant FM as ForwardManager
    participant PA as PortAllocator
    participant Relay as Relay
    participant Counter as TrafficCounter

    Caller->>FM: AddRule(ForwardRule)

    FM->>FM: 验证规则
    FM->>FM: 检查规则是否存在

    FM->>PA{端口分配}
    PA-->|ListenPort=0| PA1[Allocate]
    PA-->|ListenPort>0| PA2[AllocateSpecific]

    PA-->>FM: 分配的端口

    FM->>Counter: GetOrCreate

    FM->>Relay: NewRelay + Start
    Relay->>Relay: net.Listen(listenAddr)
    Relay->>Relay: go handleConn()

    FM->>FM: 存入 rules map

    FM-->>Caller: ForwardRule with ListenPort
```

**关键代码**: `forward/forward_manager.go:90-172`

### 5.2 流量转发

```mermaid
sequenceDiagram
    participant Client
    participant Relay
    participant Target as TargetAddr
    participant Counter as TrafficCounter

    Client->>Relay: TCP 连接

    Relay->>Relay: Accept()

    rect rgb(200, 230, 255)
    note right of Relay: 上行: Client -> Target
    Relay->>Target: Dial + 复制数据
    Target-->>Relay: 响应数据
    Relay->>Counter: AddUpload(bytes)
    end

    rect rgb(230, 200, 255)
    note right of Relay: 下行: Target -> Client
    Relay->>Client: 复制数据
    Relay->>Counter: AddDownload(bytes)
    end

    Client->>Relay: 关闭连接
    Relay->>Target: 关闭
    Relay->>Counter: Close()
```

---

## 六、自动更新流程

### 6.1 二进制更新

```mermaid
flowchart TD
    A[Update] --> B[检查当前版本]
    B --> C[获取 Release 信息]
    C --> D{版本匹配?}
    D -->|已是最新| E[返回]
    D -->|需要更新| F[下载新版本]

    F --> G{下载成功?}
    G -->|否| H[返回错误]
    G -->|是| I[校验 Checksum]

    I --> J{校验通过?}
    J -->|否| K[删除下载文件<br/>返回错误]
    J -->|是| L[原子替换二进制]

    L --> M{RestartPolicy}
    M -->|always| N[重启容器]
    M -->|on-failure| O{替换成功?}
    O -->|是| N
    O -->|否| P[回滚]
    M -->|never| Q[完成]

    N --> R[返回 UpdateResult]
    P --> R
    Q --> R
```

**关键代码**: `containers/xray/updater.go`

---

## 七、组件关系总览

```mermaid
classDiagram
    class Container {
        <<interface>>
        +Start() error
        +Stop() error
        +Restart() error
        +Reload() error
        +IsRunning() bool
        +Version() string
        +Type() ContainerType
        +ConfigFile() string
        +Update() *UpdateResult
        +AddInboundConfig() error
        +RemoveInboundConfig() error
        +GetInboundConfig() Inbound
        +ListInboundConfigs() []Inbound
        +SetUserManager()
        +UserEventChannel()
        +StartUserSync()
        +StopUserSync()
    }

    class BaseContainer {
        +Start() error
        +Stop() error
        +Restart() error
        +IsRunning() bool
        +State() ContainerState
    }

    class Executor {
        +Start() error
        +Stop() error
        +Restart() error
        +AddInboundConfig() error
        +GetUserSubscriptions() []SubscriptionSpec
        -inbounds map[string]*XrayInbound
        -userPorts map[string]uint32
    }

    class XrayInbound {
        +Tag() string
        +Protocol() Protocol
        +Port() uint32
        +ListenAddr() string
        +ToNative() []byte
        +Validate() error
    }

    class Inbound {
        <<interface>>
        +Tag() string
        +Protocol() Protocol
        +Port() uint32
        +ListenAddr() string
        +Config() *Config
        +Extra() map[string]any
        +ToNative() []byte
        +Validate() error
    }

    class UserManager {
        +AddUser() error
        +RemoveUser() error
        +GetUser() *User
        +ListUsers() []*User
        +GetBindPort() uint32
        +ReleaseBindPort() error
        +UserEventChannel() chan UserEvent
    }

    class ForwardManager {
        +AddRule() *ForwardRule
        +RemoveRule() error
        +GetRulesByUser() []*ForwardRule
        +GetTraffic() *TrafficSnapshot
    }

    class SubscriptionProvider {
        <<interface>>
        +GetUserSubscriptions() []SubscriptionSpec
    }

    Container <|.. BaseContainer
    Container <|.. Executor
    BaseContainer --> Executor : 嵌入
    Executor ..> XrayInbound : 管理
    Inbound <|.. XrayInbound
    Executor --> UserManager : 集成
    UserManager --> ForwardManager : 集成
    Container ..|> SubscriptionProvider
    Executor ..|> SubscriptionProvider
```
