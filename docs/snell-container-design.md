# Snell Container 设计文档

## 概述

实现 snell 协议作为新的 container 类型，与现有 xray container 并列。snell 本身不支持用户管理，用户隔离完全通过 forwardmgr 端口转发实现。

## 架构

```
客户端 → 外部端口(forwardmgr分配) → 127.0.0.1:内部端口(snell-server)
```

- 一个 snell-server 进程，监听 `127.0.0.1:内部端口`
- 一个占位 inbound（不可删除），保持 container 架构一致性
- AddUser/RemoveUser 走 usermanager → forwardmgr 分配/回收外部端口
- forwardmgr 将每个用户的外部端口转发到 snell-server 内部端口
- 所有用户共享 snell-server 的 psk

## 文件结构

```
pkg/proxyrefactor/containers/snell/
├── factory.go        # Factory 注册 + 构建
├── container.go      # Container 接口实现
├── config.go         # SnellConfig + 配置文件生成
├── downloader.go     # 自动下载 snell-server
└── process.go        # 进程管理（启动/停止/健康检查）

pkg/proxyrefactor/core/subscription/converter/
└── snell_surge.go    # Surge converter（仅 surge 客户端输出 snell）
```

## Container 接口实现

| 方法 | 行为 |
|------|------|
| Start() | 检查二进制→不存在则下载→生成配置→启动进程 |
| Stop() | kill 进程 |
| AddUser() | 无操作（用户管理由 usermanager + forwardmgr 处理） |
| RemoveUser() | 无操作 |
| AddInbound() | 无操作（占位 inbound 在 Start 时创建） |
| RemoveInbound() | 拒绝（占位 inbound 不可删除） |
| FastAddInbound() | 无操作 |
| GetInboundConfig(tag) | 返回占位 inbound 配置 |
| ListInboundConfigs() | 返回 [占位 inbound] |
| Version() | 返回 snell-server 版本号 |
| Update() | 下载新版本二进制，重启进程 |

## 配置

### AppConfig yaml

```yaml
containers:
  containers:
    - type: snell
      enabled: true
      config:
        binary_path: /usr/local/bin/snell-server  # 不存在则自动下载
        config_file_path: /etc/v2raymg/snell.conf
        listen: 127.0.0.1
        port: 16160                               # snell-server 内部监听端口
        psk: "auto"                                # "auto" 则自动生成随机 psk
        version: "v5.0.1"                          # 下载版本，硬编码
        # obfs: ""                                 # 预留，暂不实现
```

### 生成的 snell.conf

```ini
[snell-server]
listen = 127.0.0.1:16160
psk = <generated_or_configured_psk>
ipv6 = false
# obfs = http    # 预留
```

## 自动下载

URL 模板：`https://dl.nssurge.com/snell/snell-server-{version}-linux-{arch}.zip`

GOARCH 映射：
- amd64 → amd64
- 386 → i386
- arm64 → aarch64
- arm → armv7l

流程：
1. 检查 binary_path 是否存在
2. 不存在 → 根据 version + runtime.GOARCH 构造 URL
3. 下载 zip → 解压 → 放到 binary_path
4. chmod +x

## 占位 Inbound

```go
type SnellInbound struct {
    tag      string   // "snell-default"
    listen   string   // "127.0.0.1"
    port     int      // snell-server 内部端口
    psk      string   // snell-server psk
    version  int      // 5
}
```

- Tag: "snell-default"
- 不可删除
- GetInboundConfig 返回此对象

## Subscription Converter

### Surge（唯一输出 snell 的 client）

```
ProxyName = snell, SERVER_HOST, USER_PORT, psk=SNELL_PSK, version=5
```

- SERVER_HOST: 节点的 proxy_host
- USER_PORT: forwardmgr 分配给该用户的外部端口
- SNELL_PSK: snell-server 的 psk

### 其他 client（clash/v2ray/通用）

converter 过滤掉 snell 协议，不输出。

## 开发步骤

### Step 1: 基础框架
- contracts 中注册 ContainerSnell 类型
- 创建 snell/factory.go + container.go 骨架
- 实现 Container 接口所有方法（大部分空操作）
- 注册 factory

### Step 2: 配置 + 进程管理
- config.go: SnellConfig 结构体 + 生成 snell.conf
- process.go: 启动/停止 snell-server 进程
- downloader.go: 自动下载逻辑

### Step 3: Subscription Converter
- snell_surge.go: Surge converter
- 其他 converter 过滤 snell

### Step 4: 集成测试
- 启动 endnode + snell container
- AddUser → 验证 forwardmgr 分配端口
- 请求 /sub?target=surge 验证输出
- 停止 → 验证进程清理

## 预留

- obfs 支持：config 中预留字段，converter 预留参数位
- QUIC Proxy Mode：config 中可扩展 udp_port 字段
