# P1-B1 集群 RPC 加密 + 鉴权 —— 需要部署决策（未自动修复）

**状态**：3 条 finding 全部核实为真，但修复是**集群线格式破坏性变更**，无向后兼容路径。
因此**未随 P1-B 一起落地**，需用户就部署方式拍板后再实施。

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
