---
title: mihomo 容器
layer: details
---

## 生命周期

### Start(hook: GetRunFunc 返回的 runFunc)

顺序(container.go `mihomoHooks.GetRunFunc` 内):

1. `startUserEventHandler` — spawn goroutine 读 `userEventCh`,分发到 `handleUserEvent`
2. `startReconcileLoop` — spawn goroutine 每 30s 跑 `reconcileDrift`
3. `startProcess` — 若 `BinaryPath` 不存在且 `AutoDownload=true`,先调 `updater.Update(ReleaseTag, RestartPolicyNever)` 下载;然后 fork+exec mihomo(`-d <DataDir> -f <ConfigFilePath>`),等 `GET /version` 200(readinessTimeout=10s)
4. `restoreAndPushInbounds` — 从 `InboundStore` 加载所有 `ContainerType=mihomo` 记录,填回 `c.inbounds` map,再 `PUT /configs` 下发整份 listeners
5. `reconcileUsers` — 对每 (可见用户 × inbound) 调 `GetBindPort`,幂等补齐 forward rule

### Stop(hook: stopFunc)

顺序:

1. `stopReconcileLoop` — close stopCh,等 goroutine 退出
2. `stopProcess` — `runner.Stop()`(SIGINT 5s + SIGKILL 兜底);`cachedVersion` **保留**(用户可看最后运行的版本)

**注意**:Stop 不释放 forward rule。设计上 restart 期间转发端口侦听仍在但 relay 后端不可达;用户连接瞬断,下次 Start 的 `reconcileUsers` 复用既有 rule。

### Restore(Restorable 接口)

`ContainerMgr.StartAll` 的二次调用。语义 = `restoreAndPushInbounds` + `reconcileUsers`,幂等。Start hook 已做过一次,Restore 再跑一次只是 1 次 REST PUT(name-keyed diff,无 listener 重建)+ 一次 user 对账(addedUsers fast-path skip)。

### Reload / reconcileDrift

- **Reload**:等价于 `pushConfig()`,重推整份 listener 配置。证书 / 共享凭据变化后,同名 listener 的 Close+Listen 会断开该 inbound 上所有现有连接;跨 inbound 不受影响。
- **reconcileDrift**:周期 30s,先 `pushConfig()`(map → mihomo 漂移修正)再 `reconcileUsers()`(user → forward 漂移修正)。push 失败仅 log,不阻断 user 对账。

## User 事件管线

`usermanager.UserManager.Subscribe()` 返回的 channel 是事件源。container 在 `NewMihomoContainer`(有 `WithUserManager` option 时)构造期就 subscribe,避免 Start 前丢事件。

- `forwardUserEvents` — 拷贝事件到内部 `userEventCh`,隔离订阅层生命周期
- `startUserEventHandler` / `handleUserEvent` — 按 `UserEventAdd/Remove/Update` 分发
- `syncUserToInbound(user)` — 对 `c.inbounds` 每个 inbound 调 `inb.AddUser(username, user)`(即 `GetBindPort`)
- `removeUserFromInbounds(username)` — 对每个 inbound 调 `inb.RemoveUser`

`addedUsers` 追踪挂在 **inbound 上**(不在 container 层),与 xray 同构;锁粒度天然分片。

## 共享凭据 listener 布局

listener 模型仍是一条 inbound 一把共享凭据,但新协议扩展使用 `ProtocolParams` 保存协议/传输/安全层的结构化配置。旧 `SharedCred` 继续用于 shadowsocks 以及历史 vmess/trojan 持久化记录的兼容读取。

`MihomoInbound.SharedCred` legacy 字段按协议填不同字段:

| 协议 | 字段 |
|------|------|
| vmess | UUID(历史记录兼容;Phase 2+ 新记录走 ProtocolParams.VMess) |
| trojan | Password + CertFile + KeyFile(历史记录兼容;Phase 3+ 新记录走 ProtocolParams.Trojan + SecuritySpec) |
| shadowsocks | Password + Cipher |

`ProtocolParams` 当前已落地:

| 协议 | 支持范围 |
|------|----------|
| vless | tcp/ws/grpc/xhttp/splithttp + tls/reality |
| vmess | tcp/ws/grpc + none/tls/reality |
| trojan | tcp/ws/grpc + tls/reality |

**trojan 必须带 TLS 或 Reality**(stage 11+ E2E 确认):mihomo Alpha 的 trojan listener 运行时拒绝没有 `certificate + private-key` / `reality-config` / ss config 的配置,报 `disallow using Trojan without both certificates/reality/ss config`。Phase 3 后 FastAdd 的 trojan 新记录走 ProtocolParams:`security=tls` 输出 `certificate` / `private-key`;`security=reality` 输出 `reality-config`;transport 支持 tcp/ws/grpc。未显式传 `security` 时 parseTrojan 默认 TLS,生产 RPC/HTTP 路径由 `FillDefaults` 物化 cert_file/key_file。

**SAFE_PATHS 约束**:mihomo 对 config 里引用的文件路径做安全检查 —— 路径必须位于 `-d` 参数指定的 home directory(即 `MihomoConfig.DataDir`)之下,否则报 `path is not subpath of home directory`。生产部署要把 trojan cert 写到 `DataDir` 或其子目录。

**证书清理**:`RemoveInboundConfig` 会从 legacy SharedCred 或 ProtocolParams TLS block 读取 `cert_source`,仅清理 v2raymg 自己写入的 `pem` / `self_signed` 证书;`file` / `domain` 来源不动。

`BuildListener(inb)` 输出的 map 字段严格匹配 mihomo Alpha `listener/inbound/*.go` 的 `inbound:"..."` struct tag。`name=inb.Tag()`,`type=string(inb.Protocol())`,`listen=inb.ListenAddr()`(默认 127.0.0.1),`port=strconv.FormatUint(inb.Port(),10)`(mihomo `BaseOption.Port` 是字符串,支持范围)。

## REST 客户端

`rest_client.go::RESTClient` 封装了对 `external-controller` 的调用:

| 方法 | 端点 | 用途 |
|------|------|------|
| `GetVersion` | `GET /version` | readiness 探测 + cachedVersion 刷新 |
| `GetConfigs` | `GET /configs` | 读当前配置(少用) |
| `PutConfigs` | `PUT /configs` | 下发整份 yaml(listener CRUD 走这里) |
| `PatchConfigs` | `PATCH /configs` | 局部字段热改(未在主流程使用) |
| `PostConfigsGeo` | `POST /configs/geo` | 触发 geo 数据重载 |

所有请求走 `do(ctx, method, url, op, body)` 统一构造:Content-Type `application/json`,Secret 非空时加 `Authorization: Bearer <secret>`,错误 body 限 1024 字节,解析 `{"message":"..."}` JSON,解析失败 fallback 到 trimmed raw(256 字节)。

## Updater 完整 7 步(stage 9 + 10a)

`updater.go::Update(ctx, req)`:

1. **fetch** — `releaseClient.FetchRelease(owner="MetaCubeX", repo="mihomo", tag=req.TargetTag)`。`tag==""` / `"latest"` → `/releases/latest`(只返 stable);其他 → `/releases/tags/<tag>`。
2. **pick asset** — `pickLinuxAmd64V1Asset(release)`:
   - Alpha(`TagName=="Prerelease-Alpha"`)→ 前缀 `mihomo-linux-amd64-v1-alpha-` + 后缀 `.gz`;第一条匹配
   - stable → 精确匹配 `mihomo-linux-amd64-v1-<TagName>.gz`(排除 `-go120-/go123-` toolchain 变体)
3. **download** — 下载 `.gz` 到 `DownloadDir`(NewUpdater 已建目录,不重复 MkdirAll)
4. **verify(可选)** — 找 `checksums.txt` 资产;存在则下载 + `extractChecksumFor`(剥掉文件名里 `./` 前缀)+ `tools.VerifyChecksum`(TrimSpace + ToLower 后严格相等)。不存在则 `log.Debug` 跳过
5. **extract** — `gunzipToTemp(gzPath, DownloadDir)` → chmod 0755
6. **swap** — `BinarySwapper.SwapAtomic(BinaryPath, extractedPath)`,返回 backupPath(`<BinaryPath>.bak`)
7. **restart**(仅 `RestartPolicy != Never` 且 `processCtrl != nil`):
   - `IsRunning()` → `Stop()` 若在跑
   - `Start()` 拉新进程
   - `WaitReady(ctx, bound by ReadinessTimeout=10s)`(stage 10a)—— 失败则 `Stop` + 走 `restartFailure` 做 `Rollback`
   - 任何阶段失败 → `ErrUpdateFailed{Stage:"...", Cause:..., Wrapped:ErrRestartFailed 若在 restart 阶段}`;Cause + Wrapped 都能通过 `errors.Is` 可达

### 错误链

`ErrUpdateFailed.Unwrap() []error` 返回 `[Cause, Wrapped]`(nil 过滤)。`errors.Is(err, ErrRestartFailed)` 判断是不是 restart 阶段;`errors.Is(err, context.Canceled)` 判断是不是 ctx 被 cancel。`errors.Join` 用于"双失败"(e.g. WaitReady 失败 + 清理 Stop 失败 → 两 cause 同时可达)。

## 用户订阅生成

`subscription.go::GetUserSubscriptions(req)`:

1. `snapshotInbounds()` 取所有 inbound 的浅拷贝
2. 对每个 inbound 查 `userMgr.GetUserPortByDst(req.Username, inb.Port())`,没映射则跳过(debug log)
3. `buildSubscriptionSpec(inb, req, port)` 构造 `SubscriptionSpec`:
   - vless → `VLessNode{UUID, Port=forwardPort, Host=req.Host, ...}` → `Encode()`
   - vmess → `VMessNode{UUID, Port=forwardPort, Host=req.Host, ...}` → `Encode()`
   - trojan → `TrojanNode{Password, Port=forwardPort, ...}` → `Encode()`;Phase 3 会透传 `transport` / `security` / reality / skip-cert-verify 到 URI 和 Extensions
   - ss → `ShadowsocksNode{Method, Password, Port=forwardPort, ...}`;cipher 为空时 fallback `aes-256-gcm`(与 clash converter 的 `defaultSSMethod` 对齐)→ `Encode()`

URI 组装全部在 `core/subscription/codec` 层完成,本包零字符串拼接。协议 filter 不在本层处理(交给上层 HTTP handler 做)。
