# P1-B1 集群 RPC 加密 + 鉴权 —— 已实施（方案 A，commit `61adb83`）

**状态（2026-07-12 更新）**：用户选 **方案 A（一步到位协调式破坏性升级）**，已实施并测试。
下面保留原始决策分析供参考；**实际落地方案见文末「已实施」一节**。

**原始状态**：3 条 finding 全部核实为真，但修复是**集群线格式破坏性变更**，无向后兼容路径。

设计 agent 因 API 中断未产出完整方案，但侦察 + 红队均已完成并给出一致结论（见
`docs/review-2026-07-10/L0-foundation.md` / `L4-cluster.md` 对应条目，及本轮 workflow 记录）。

## 三条 finding（均 confirmed）

1. **无 KDF，空/短 token → 弱/常量密钥**（`pkg/common/rpc/encrypt_codec.go:46`）
   `GetRpcKeyByToken`：token ≥ 32 字节直接取前 32 字节做 AES-256 密钥；< 32 用 PKCS7 填充。
   **空 token → 32×`0x20` 全集群公开常量密钥**；短 token 熵极低。且 appconfig 对
   `Cluster.Token` 无任何非空/最小长度校验，弱 token 是可部署的真实配置。

2. **nil AAD、无重放防护**（`pkg/common/rpc/crypto.go:37`）
   `gcm.Seal(nonce, nonce, plaintext, nil)`：密文不绑定方法名/身份/方向/时间。唯一时序
   防护是 HeartBeat 的 ±30s 漂移检查，且 `ts==0` 直接跳过；`AddUsers/DeleteUsers/
   ResetAuthToken/RotateAllPorts/UpdateProxy/AddInbound` 等**全部写方法无时间戳、无 nonce
   去重** → 捕获即可在窗口内整帧重放。

3. **CenterNodeServer 完全无鉴权且明文**（`pkg/rpc/server/center_node_server.go:119`）
   `grpc.NewServer()` 无拦截器、无加密 codec；`HeartBeat` 从不校验 `NodeAuthInfo.Token`；
   end→center 的 `heartbeatToCenterNode` 也是明文（`Token:""`、无 ForceCodec）。任何能连到
   center RpcPort 的攻击者可**枚举全集群节点目录**并**投毒**（注入任意 host:port 节点，被
   end 节点下轮心跳拉回并主动 Dial → 反射式连接/DoS）。

## 为什么不能默默修

- 加密帧 / `NodeAuthInfo` / `HeartBeatReq` **无任何 crypto/wire 版本协商字段**；`ts==0`
  向后兼容分支等证明当前**允许混合版本滚动升级**。
- 改 KDF / 改帧 / 加 AAD / 给 center 加密鉴权，任一都会让**升级节点与未升级节点
  `gcm.Open` 直接失败**，无回退路径 → 滚动升级期间整条集群 RPC 链路中断（用户掉线，
  直到所有节点升级完毕）。
- `encoding.RegisterCodec` 是进程级全局注册，center 是独立进程从不注册该 codec；给
  center 加密必须同时改 center `Start` 与 end 的 `heartbeatToCenterNode`，任一单边改动即断连。

## 可选方案（请用户选择）

- **A. 协调式破坏性升级**：一次性实现 KDF（sha256/HKDF）+ 方法名 AAD + center 鉴权加密，
  明确要求**全集群同时升级、不得混跑旧版本**。实现量中等，风险在部署窗口。
- **B. 先加协商再改**：给加密帧/心跳引入 crypto 版本字段，保留旧路径做过渡，之后分阶段
  切换。最稳但工作量最大（超出单条 P1 的最小化范围）。
- **C. 仅做无线格式影响的部分**：在 appconfig 加 `Cluster.Token` 最小长度/非空校验（挡掉
  #1 最弱的空/常量密钥）。注意：这仍是行为变化——现网用短 token 的节点升级后会因
  校验失败拒绝启动（比线格式破坏轻，且有明确报错）。不触碰 #2/#3。

**当前建议**：先落地 C（低风险缓解 #1 最严重面），#2/#3 的完整加固按 A 或 B 排期，由用户
定部署窗口。P1-B 的其余两单元（SSRF/模板注入、xray security=none）已在
`e0e0170` / `c425504` 落地，与本条无部署耦合。

---

## 已实施（方案 A，commit `61adb83`）

用户确认部署拓扑为 **多 cluster 共用一个 center（不同 token）**，据此调整了 center 侧设计。

- **#1 KDF**：`GetRpcKeyByToken` 改 HKDF-SHA256（`golang.org/x/crypto/hkdf`），任意 token → 32 字节强密钥，空/短 token 不再退化为常量。`appconfig.Validate` 要求 cluster token ≥ 16 字符（end 开 RPC 时 / center 每个 cluster_token）。
- **#2a AAD**：GCM 加 AAD = `协议版本 | proto 消息类型名`（跨类型重放被 GCM 拒）。
- **#2b 重放**：`NodeAuthInfo` 加 `timestamp_us / nonce / dest_node`（proto regen）；新增 end 拦截器 `checkReplay`（拒过期时间戳、重复 nonce〔进程级 TTL 缓存〕、发往其它节点的帧〔dest 绑定挡跨节点重放〕）；所有 ~5 个构造点统一走 `NewNodeAuthInfo`（每调用新 nonce，含修掉 upsert 复用 heartbeat nonce 的自伤断连）。
- **#3 center**：因多 cluster，改**app 层多 token 鉴权**——`CenterNodeConfig.cluster_tokens`（cluster→token）；heartbeat 仅当 cluster 已配置且 token 常量时间相等才接受；end→center 心跳改带 cluster token + 防重放戳。center 通道保持明文（多 cluster，只传节点目录不含用户凭据）。token 比对是**承重主鉴权**（gRPC 默认 proto codec 无法注销，明文旁路真实存在）。

**部署硬要求 / 残留（务必知会运维）**：
1. **全集群 + center 必须同时升级**并配强 token；混合版本互不解密即断连（预期，无回退）。
2. center 的 `cluster_tokens` 必须列全所有它服务的 cluster；未列的 cluster 其 end 会被拒（这是修复本身，非回归）。
3. 所有写方法现在都要带 ±30s 内的时间戳 → **时钟偏移 >30s 的节点会被隔离**（NTP 依赖从"仅心跳"扩到全链路）。
4. token 静态、**无密钥轮换路径**（后续可加）。
5. codec 层真·方法名绑定未做（5 个 UserOp 方法共享同一 proto 类型，仅做到消息类型绑定）——后续。
