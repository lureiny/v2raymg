# 2026-04-09 review 问题现状核查（2026-07-10）

> 对旧 review 全部 finding 逐条核对当前代码的结果。行号为**当前代码**位置。


## 汇总

| 现状 | 条数 |
|------|------|
| still-present | 84 |
| partially-fixed | 7 |
| cannot-determine | 0 |
| fixed | 22 |
| not-applicable | 2 |
| **合计** | **115** |

### 仍然存在（still-present）+ 部分修复（partially-fixed）清单

| Section | ID | 旧优先级 | 当前位置 |
|---------|----|---------|----------|
| S1 | E-01 | P1 | pkg/proxy/errors/errors.go:132-142 |
| S1 | E-02 | P1 | pkg/proxy/errors/errors.go:220-222 |
| S1 | E-03 | P2 | pkg/proxy/errors/errors.go:213-216 |
| S1 | E-05 | P3 | pkg/proxy/errors/errors.go:225-292 |
| S1 | P-01 | P1 | pkg/proxy/tools/process/runner.go:147-151 |
| S1 | P-02 | P1 | pkg/proxy/tools/process/runner.go:120-123 |
| S1 | P-03 | P2 | pkg/proxy/tools/process/runner.go:111-116 |
| S1 | P-04 | P2 | pkg/proxy/tools/process/runner.go:139-144 |
| S1 | P-05 | P2 | pkg/proxy/tools/process/runner.go:26-29,74-83 |
| S1 | P-06 | P3 | pkg/proxy/tools/process/runner.go:66-70 |
| S1 | P-07 | P3 | pkg/proxy/tools/process/runner.go:163-169 |
| S2 | C-04 | P1 | pkg/proxy/core/contracts/inbound.go:10-23 |
| S2 | C-05 | P3 |  |
| S2 | C-06 | P2 |  |
| S2 | C-07 | P2 |  |
| S2 | C-08 | P3 |  |
| S2 | C-09 | P1 |  |
| S2 | C-10 | P3 |  |
| S2 | C-11 | P3 |  |
| S2 | C-13 | P3 |  |
| S2 | C-14 | P1 |  |
| S3 | I-01 | P1 | pkg/proxy/core/inbound/default.go:64 |
| S3 | I-02 | P2 | pkg/proxy/core/inbound/inbound.go:70 |
| S3 | I-03 | P2 | pkg/proxy/core/inbound/inbound.go:72-74 |
| S3 | I-04 | P2 | pkg/proxy/core/inbound/inbound.go:99-123 |
| S3 | I-05 | P3 | pkg/proxy/core/inbound/default.go:23-28 |
| S3 | CT-01 | P2 | pkg/proxy/core/container/interface.go:21 |
| S3 | CT-02 | P2 | pkg/proxy/core/container/interface.go:37 |
| S3 | CT-03 | P2 | pkg/proxy/core/container/interface.go:65 |
| S3 | CT-04 | P2 | pkg/proxy/core/container/interface.go:55 |
| S3 | CT-05 | P1 | pkg/proxy/core/container/base.go:149-152,179-194 |
| S3 | CT-06 | P1 | pkg/proxy/core/container/base.go:239-248 |
| S3 | CT-07 | P3 | pkg/proxy/core/container/base.go:297 |
| S3 | CT-08 | P3 | pkg/proxy/core/container/base.go:156,162 |
| S3 | CT-09 | P1 | pkg/proxy/core/container/registry.go:22-47,175-189 |
| S3 | CT-10 | P3 | pkg/proxy/core/container/registry.go:121-131 |
| S3 | CT-11 | P2 | pkg/proxy/core/container/manager.go:81 |
| S3 | CT-12 | P3 | pkg/proxy/core/container/factory.go:22 |
| S3 | CT-13 | P3 | pkg/proxy/core/container/factory.go:19-21 |
| S4a | X-02 | P1 | pkg/proxy/containers/xray/exec_runner.go:374-405 |
| S4a | X-03 | P1 | pkg/proxy/tools/process/runner.go:147-151 |
| S4a | X-04 | P2 | pkg/proxy/containers/xray/exec_runner.go:462-482 |
| S4a | X-05 | P2 | pkg/proxy/containers/xray/exec_runner.go:842-881 |
| S4a | X-06 | P3 | pkg/proxy/containers/xray/exec_runner.go:501-508 |
| S4a | X-07 | P0 | pkg/proxy/containers/xray/grpc_client.go:372-430 |
| S4a | X-08 | P2 | pkg/proxy/containers/xray/grpc_client.go:169,256,280,320,352,373 |
| S4a | X-09 | P3 | pkg/proxy/containers/xray/grpc_client.go:116 |
| S4a | X-10 | P3 | pkg/proxy/containers/xray/grpc_client.go:295-306 |
| S4a | X-11 | P3 | pkg/proxy/containers/xray/config_renderer.go:156 |
| S4a | X-12 | P2 | pkg/proxy/containers/xray/updater.go:186,192 |
| S4a | X-13 | P3 | pkg/proxy/containers/xray/updater.go:213-261 |
| S4a | X-14 | P3 | pkg/proxy/containers/xray/updater.go:174-199 |
| S4a | X-15 | P3 | pkg/proxy/containers/xray/register.go:41 |
| S4b | A-04 | P2 | pkg/proxy/containers/xray/inbound_adapter.go:163-176 |
| S4b | A-05 | P2 | pkg/proxy/containers/xray/profilegen/shadowsocks.go:250 |
| S4b | V-04 | P2 | pkg/proxy/containers/xray/profilegen/vless_profiles.go:183-187 |
| S4b | S-03 | P3 | pkg/proxy/containers/xray/subscription.go:82 |
| S4b | SS-01 | P2 | pkg/proxy/containers/xray/profilegen/shadowsocks.go:88-90 |
| S4b | SS-02 | P2 | pkg/proxy/containers/xray/profilegen/shadowsocks.go:199 |
| S4b | T-01 | P3 | pkg/proxy/containers/xray/profilegen/trojan_tls.go:84-87 |
| S5 | H-01 | P2 | pkg/proxy/containers/hysteria/container.go:157, pkg/proxy/containers/snell/container.go:112 |
| S5 | H-02 | P2 | pkg/proxy/containers/hysteria/container.go:450-460, pkg/proxy/containers/snell/container.go:356-365 |
| S5 | H-04 | P0 | pkg/proxy/containers/hysteria/container.go:51,74,225,241-249 |
| S5 | H-05 | P1 | pkg/proxy/containers/hysteria/container.go:60-80,164-168,229-232 |
| S5 | H-06 | P3 | pkg/proxy/containers/hysteria/process.go:130 |
| S5 | H-07 | P3 | pkg/proxy/containers/hysteria/container.go:271-274 |
| S5 | SN-03 | P2 | pkg/proxy/containers/snell/container.go:557 |
| S5 | H-08 | P2 | pkg/proxy/core/container/interface.go:44-55 |
| S6 | U-02 | P1 | pkg/proxy/usermanager/usermanager.go:2233-2246 |
| S6 | U-03 | P1 | pkg/proxy/usermanager/usermanager.go:1360-1362 |
| S6 | U-04 | P2 | pkg/proxy/usermanager/usermanager.go:352-374, 1311-1372 |
| S6 | U-05 | P2 | pkg/proxy/usermanager/usermanager.go:867-875 |
| S6 | U-06 | P2 | pkg/proxy/usermanager/usermanager.go:975-978, 1332-1335 |
| S6 | U-08 | P3 | pkg/proxy/usermanager/usermanager.go:604-607 |
| S6 | U-10 | P3 | pkg/proxy/usermanager/sync/hash.go:51 |
| S6 | U-11 | P3 | pkg/proxy/usermanager/sync/hash.go:57-62 |
| S7 | SUB-01 | P2 | pkg/proxy/core/subscription/manager.go:57-60 |
| S7 | SUB-02 | P1 | pkg/proxy/core/subscription/converter/clash.go:47-52, 177-180, 944 |
| S7 | SUB-04 | P1 | pkg/proxy/core/subscription/converter/http.go:25-40 |
| S7 | SUB-09 | P3 | pkg/proxy/core/subscription/fake.go:29,38,43 |
| S7 | SUB-10 | P3 | pkg/proxy/core/subscription/converter/registry.go:9-22 |
| S7 | APP-01 | P2 | pkg/proxy/appconfig/loader.go:247-284 |
| S7 | APP-02 | P3 | pkg/proxy/appconfig/loader.go:199-206 |
| S7 | APP-03 | P3 | pkg/proxy/appconfig/loader.go:252-255 |
| S2 | C-03 | P3 |  |
| S2 | C-15 | P2 |  |
| S2 | C-16 | P2 |  |
| S4b | A-02 | P1 | pkg/proxy/containers/xray/inbound_adapter.go:544-559 |
| S4b | A-06 | P3 | pkg/proxy/core/params/protocolparams/keys.go:15-79 |
| S5 | H-03 | P3 | pkg/proxy/containers/hysteria/container.go:430-432, pkg/proxy/containers/snell/container.go:336-338 |
| S5 | SN-02 | P2 | pkg/proxy/containers/snell/container.go:522-524 |

## S1 — errors + tools/process

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| E-01 | P1 | **still-present** | pkg/proxy/errors/errors.go:132-142 | pkg/proxy/errors/errors.go:138 still has `if se, ok := any(target).(ErrorCode); ok` inside ProxyError.Is; ErrorCode has no Error() method so a value passed as `error` can never assert to ErrorCode — the backward-compatibility branch remains dead code. |
| E-02 | P1 | **still-present** | pkg/proxy/errors/errors.go:220-222 | pkg/proxy/errors/errors.go:221 `ToError` still returns `errors.New(code.Message())`, a plain stdlib error; ProxyError.Is (errors.go:132-142) only matches *ProxyError or ErrorCode targets, so errors.Is(proxyErr, ToError(code)) still never matches. |
| E-03 | P2 | **still-present** | pkg/proxy/errors/errors.go:213-216 | pkg/proxy/errors/errors.go:214-216 `HasCode` is still `return Code(err) == code`; Code() (errors.go:197-211) returns only the first ProxyError found via errors.As, so a matching code deeper in the cause chain (behind another ProxyError with a different code) is missed — still inconsistent with errors.Is chain semantics. |
| E-04 | P3 | **fixed** | pkg/proxy/errors/errors.go:324-327 | NewErrAt still uses two %w (errors.go:326), but go.mod:3 now declares `go 1.24.0`, so multi-%w (Go 1.20+) is guaranteed by the module's toolchain requirement; the 1.18/1.19 silent-degradation risk no longer exists. |
| E-05 | P3 | **still-present** | pkg/proxy/errors/errors.go:225-292 | Message() switch (errors.go:226-291) still lacks cases for exactly 9 defined codes: ErrContainerStartFailed/StopFailed/RestartFailed/ReloadFailed (errors.go:57-60), ErrForwardRuleConflict (errors.go:78), ErrUserPortBindFail (errors.go:88), ErrGRPCRequestFailed (errors.go:98), ErrFileNotFound and ErrPermission (errors.go:106-107); default at errors.go:290 returns the raw code string. |
| P-01 | P1 | **still-present** | pkg/proxy/tools/process/runner.go:147-151 | runner.go:150 `IsRunning` is still `return p.cmd != nil && p.cmd.Process != nil`; nothing calls Wait/checks ProcessState outside Stop, so a process that exits on its own still reports running. |
| P-02 | P1 | **still-present** | pkg/proxy/tools/process/runner.go:120-123 | runner.go:121-123 still spawns `go func() { done <- p.cmd.Wait() }()` — the goroutine reads the p.cmd field without holding p.mu, the exact pattern flagged in the review; the code is unchanged. |
| P-03 | P2 | **still-present** | pkg/proxy/tools/process/runner.go:111-116 | runner.go:111-116: when Signal(os.Interrupt) fails, the code Kill()s, Wait()s, sets p.cmd=nil (process fully cleaned up) and still `return err` — callers continue to see a spurious stop failure. |
| P-04 | P2 | **still-present** | pkg/proxy/tools/process/runner.go:139-144 | runner.go:139-144 `Restart` is still `if err := p.Stop(); err != nil { return err }; return p.Start()` — Stop and Start each take p.mu separately, leaving an unlocked window where concurrent Restart/Start can double-start. |
| P-05 | P2 | **still-present** | pkg/proxy/tools/process/runner.go:26-29,74-83 | RunnerConfig.Stdout/Stderr are still `interface{}` (runner.go:26,29) and Start still does unchecked assertions `p.config.Stdout.(interface{ Write([]byte) (int, error) })` at runner.go:75 and runner.go:80 — a non-Writer value panics. |
| P-06 | P3 | **still-present** | pkg/proxy/tools/process/runner.go:66-70 | runner.go:67-70: Start still hardcodes `args = append(args, "-c", p.config.ConfigFile)` when ConfigFile is set — the xray-style -c convention remains baked into the "generic" runner (the RunnerConfig comment at runner.go:19 even says such flags should go in Args). |
| P-07 | P3 | **still-present** | pkg/proxy/tools/process/runner.go:163-169 | runner.go:165-169 `Process()` still returns the internal `*exec.Cmd` (`return p.cmd`), letting callers manipulate the process outside the mutex. |

## S2 — core/contracts

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| C-01 | P1 | **fixed** |  | pkg/proxy/core/contracts/protocol.go:28-49 — AllProtocols() now returns all 10 protocols, identical to IsValid()'s set, with an explanatory comment; drift is guarded by TestProtocolAllProtocolsMatchesIsValid (pkg/proxy/core/contracts/protocol_test.go:10) |
| C-02 | P1 | **fixed** | pkg/proxy/core/params/protocolparams/params_*.go | pkg/proxy/core/contracts/protocol.go:136-137 now documents the design intent ('Provider-specific restrictions are handled by InboundAdapter'), and cross-field combination validation is centralized in pkg/proxy/core/params/protocolparams per-protocol validators (e.g. params_anytls.go:83-84, params_hysteria2.go:59-60, params_tuic.go:79-80 reject reality; params_trojan.go:61-64 requires tls/reality) instead of being scattered in the xray adapter |
| C-03 | P3 | **partially-fixed** |  | pkg/proxy/core/contracts/protocol.go:56-75 — xhttp/splithttp/http are now enum values included in AllTransports(); however 'h2' is only handled as an alias outside the enum (pkg/proxy/core/params/protocolparams/transport.go:29-45) and 'h3' is used as a live transport string in pkg/proxy/containers/xray/subscription.go:255,393 without an enum constant |
| C-04 | P1 | **still-present** | pkg/proxy/core/contracts/inbound.go:10-23 | contracts.InboundSpec (pkg/proxy/core/contracts/inbound.go:10-23) still has no ListenAddr field; it is still passed via Extensions["listen_addr"] — pkg/proxy/containers/xray/inbound_adapter.go:130,287 and profilegen (e.g. pkg/proxy/containers/xray/profilegen/vless_profiles.go:389). Note the parallel type inbound.Config does have a typed ListenAddr (pkg/proxy/core/inbound/inbound.go:18), but the InboundSpec path the finding targets is unchanged |
| C-05 | P3 | **still-present** |  | Lower bound 100 is still a bare literal with no named constant, now in even more sites: pkg/proxy/core/contracts/inbound.go:30, pkg/proxy/core/inbound/default.go:100, pkg/proxy/containers/xray/exec_runner.go:1281,1678, pkg/proxy/containers/xray/profilegen/shadowsocks.go:93, pkg/proxy/containers/xray/profilegen/vless_profiles.go:151 |
| C-06 | P2 | **still-present** |  | pkg/proxy/core/contracts/user.go:14-103 — UserSpec still mixes identity/quota/rate-limit/connection-limit/port/cluster/frontend-auth concerns: BindPorts (user.go:65), PortMappings (user.go:71), UpdatedAtUs (user.go:94), OriginNode (user.go:98), Hash (user.go:101), Role/LoginPassword (user.go:75,79) all remain on the core model |
| C-07 | P2 | **still-present** |  | pkg/proxy/core/contracts/user.go:21 — AuthToken still serializes as json:"auth_token,omitempty" (a doc comment was added but exposure is unchanged), while LoginPassword remains json:"-" at user.go:79; the directly-usable credential is still the one exposed in JSON |
| C-08 | P3 | **still-present** |  | pkg/proxy/core/contracts/user.go:85 — DeletionState is still a bare string field; the DeletionStateActive/DeletionStateDeleting constants (user.go:127-132) are untyped string constants, so any string still compiles |
| C-09 | P1 | **still-present** |  | pkg/proxy/core/contracts/subscription.go:32-34 — transport/security still live in Extensions via magic-string keys; readers use e.g. extString(spec.Extensions, "ws_path") in pkg/proxy/containers/xray/subscription.go:351 and transport lookup at subscription.go:255 |
| C-10 | P3 | **still-present** |  | pkg/proxy/core/contracts/subscription.go:53 — ExcludeProtocols is still []string, not []Protocol |
| C-11 | P3 | **still-present** |  | pkg/proxy/core/contracts/subscription.go:28-30 — URI is still a field on SubscriptionSpec described as 'Populated by the provider-specific generator', so the same struct still plays both input and output roles |
| C-12 | P2 | **fixed** |  | ContainerModel now has real consumers: pkg/proxy/containers/xray/config_renderer.go:27 (Renderer.ToProvider(model contracts.ContainerModel)) and :222 (FromProvider returning contracts.ContainerModel), plus pkg/proxy/containers/xray/exec_runner.go:380 constructing one — the 'no actual consumer' defect no longer holds |
| C-13 | P3 | **still-present** |  | pkg/proxy/core/contracts/container.go:33-37 — Stats.Type and Stats.Proxy are still bare string fields with comment-only value documentation, no named enum types |
| C-14 | P1 | **still-present** |  | Both types still coexist with near-identical shape and no explicit conversion: contracts.InboundSpec (pkg/proxy/core/contracts/inbound.go:10-23, Tag/Port/Protocol/Extensions) vs inbound.Config (pkg/proxy/core/inbound/inbound.go:13-30, Tag/ListenAddr/Port/Protocol/Extensions); grep for ToInboundSpec/ToConfig-style converters found none; both are constructed independently in pkg/proxy/containers/xray/exec_runner.go:1250 (inbound.Config) and :1857/2118 (contracts.InboundSpec) |
| C-15 | P2 | **partially-fixed** |  | Constants now exist in pkg/proxy/core/params/protocolparams/keys.go (KeyWSPath keys.go:35, KeyRealityPrivateKey keys.go:78, ~60 keys total with a MUST-use comment), but many writers/readers still use raw literals: pkg/proxy/containers/xray/subscription.go:351,496, pkg/proxy/containers/xray/exec_runner.go:752,828,1740, pkg/proxy/core/subscription/uri_convert.go:231,277,335, pkg/proxy/containers/mihomo/subscription.go:277,419,520, pkg/proxy/core/subscription/converter/surge.go:87 |
| C-16 | P2 | **partially-fixed** |  | The http↔h2 normalization is now centralized and documented in pkg/proxy/core/params/protocolparams/transport.go:29-45 ('xray treats http and h2 interchangeably'), and TransportHTTP is annotated legacy at pkg/proxy/core/contracts/protocol.go:65; but mapping logic still also lives in pkg/proxy/containers/xray/subscription.go:658 ('return "h2" // legacy compat') and pkg/proxy/containers/xray/profilegen/vmess_tls.go:65-96 (its own http→h2 alias normalization), so it remains multi-site |

## S3 — core/container + core/inbound

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| I-01 | P1 | **still-present** | pkg/proxy/core/inbound/default.go:64 | default.go:64 `SetTag(tag string) { i.tag_ = tag }` only updates tag_; config_ (default.go:18, initialized at 35-41) is never synced, so Tag() and Config().Tag diverge after SetTag. Same for SetPort/SetProtocol/SetListenAddr (default.go:67-78). |
| I-02 | P2 | **still-present** | pkg/proxy/core/inbound/inbound.go:70 | inbound.go:70 interface method `Config() *Config` and default.go:58 `func (i *DefaultInbound) Config() *Config { return i.config_ }` still return the raw pointer; callers can mutate Extensions directly. |
| I-03 | P2 | **still-present** | pkg/proxy/core/inbound/inbound.go:72-74 | inbound.go:72-74 `Extra()` doc ('implementations can store their specific fields') and inbound.go:26-29 Config.Extensions doc ('provider-specific configuration') still describe overlapping semantics with no documentation of the difference or which to use. |
| I-04 | P2 | **still-present** | pkg/proxy/core/inbound/inbound.go:99-123 | InboundError (inbound.go:99-123) implements only Error(), Unwrap() and WithCause(); no `Is(target error) bool`, so errors.Is fails to match a WithCause-derived instance against the sentinel (e.g. ErrInboundTagRequired at inbound.go:90). |
| I-05 | P3 | **still-present** | pkg/proxy/core/inbound/default.go:23-28 | default.go:23-28 still silently substitutes tag="default-inbound" for empty tag and port=10000 for zero port, while NewConfig (inbound.go:33-41) takes values verbatim — behavioral inconsistency unchanged. |
| CT-01 | P2 | **still-present** | pkg/proxy/core/container/interface.go:21 | interface.go:21 `Init(config any) error` still in Container interface with implementation-specific `any` config, coexisting with Factory.New(BuildOptions) (factory.go:47). |
| CT-02 | P2 | **still-present** | pkg/proxy/core/container/interface.go:37 | interface.go:33-37 `GetRunFunc() (func() error, func() error)` still exposed on the public Container interface; it is the BaseContainer/Hooks internal mechanism (base.go:44-53, 109-117). |
| CT-03 | P2 | **still-present** | pkg/proxy/core/container/interface.go:65 | interface.go:62-65 `GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error)` is still a mandatory method of the Container interface, not an optional capability interface (contrast: Restorable in manager.go:30-32 which is done via type assertion). |
| CT-04 | P2 | **still-present** | pkg/proxy/core/container/interface.go:55 | interface.go:44-55 `FastAddInbound(tag string, params map[string]any) error` — required keys (protocol, method/password for ss, etc.) still only documented in comments, no params struct. |
| CT-05 | P1 | **still-present** | pkg/proxy/core/container/base.go:149-152,179-194 | base.go:179 unlocks before executing runFunc; base.go:149-152 a concurrent Start() seeing state==Starting returns nil ('Already starting, wait for it') without actually waiting — caller believes start succeeded while runFunc is still executing and may yet fail. |
| CT-06 | P1 | **still-present** | pkg/proxy/core/container/base.go:239-248 | base.go:239-248 Stop() in Starting state closes stopChan and immediately MarkStopped() without executing any stop function — stopFunc is nil at that point because Start() only caches it after runFunc returns (base.go:191-193); the in-flight runFunc/process is orphaned. |
| CT-07 | P3 | **still-present** | pkg/proxy/core/container/base.go:297 | base.go:288-299 Restart() still calls b.MarkRunning() at line 297 after Start(), which already ends with MarkRunning() at base.go:194 — redundant call unchanged. |
| CT-08 | P3 | **still-present** | pkg/proxy/core/container/base.go:156,162 | base.go:156 and base.go:162 still rebuild stopChan (`b.stopChan = make(chan struct{})`) on every Start; a caller holding the channel obtained via StopChan() (base.go:280-284) before a Restart keeps a stale reference and never sees subsequent closes. |
| CT-09 | P1 | **still-present** | pkg/proxy/core/container/registry.go:22-47,175-189 | Both mechanisms still coexist in registry.go: legacy globalRegistry/RegisterContainer (registry.go:22-47) and new factoryMap/RegisterFactory (registry.go:175-189). ContainerMgr.LoadFromConfig only consults GetFactory (manager.go:63), so legacy registrations remain invisible to ContainerMgr, and neither path emits any warning. |
| CT-10 | P3 | **still-present** | pkg/proxy/core/container/registry.go:121-131 | Inconsistent lock usage unchanged: SetContainer acquires globalRegistry.mu then nests singletonM inside it (registry.go:121, 129-131), while GetContainer/IsSingleton acquire singletonM alone (registry.go:106-108, 169-171) and IsRegistered takes mu then singletonM sequentially (registry.go:152-162) — the latent lock-ordering hazard flagged in the review remains. |
| CT-11 | P2 | **still-present** | pkg/proxy/core/container/manager.go:81 | manager.go:54-85 LoadFromConfig still does `m.instances[entry.Type] = instance` (line 81) with no check for an existing entry and no Stop() on the replaced instance — a second load of the same ContainerType silently leaks the running old instance. |
| CT-12 | P3 | **still-present** | pkg/proxy/core/container/factory.go:22 | factory.go:22 `Config any // container-specific config object (implements ContainerConfig)` — still typed as any despite the ContainerConfig interface existing at factory.go:11-13. |
| CT-13 | P3 | **still-present** | pkg/proxy/core/container/factory.go:19-21 | factory.go:19-21 `CertManager any` still relies on a comment ('concrete type: *certmgmtservice.Manager ... containers type-assert') instead of a dedicated interface; note the neighboring CertReader field (factory.go:26-28) shows the interface approach was adopted for cert file lookup but CertManager itself is unchanged. |

## S4a — containers/xray 核心（进程管理+配置渲染）

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| X-01 | ✅ 已修复(原 data race) | **fixed** | pkg/proxy/containers/xray/exec_runner.go:1170-1183 | pkg/proxy/containers/xray/exec_runner.go:1170-1183 — addedUsers 带 addedUsersMu sync.Mutex，注释强制通过 hasAddedUser/listAddedUsers/markAddedUser/unmarkAddedUser 访问；调用点 1044/1056/1086 均走封装方法 |
| X-02 | P1 | **still-present** | pkg/proxy/containers/xray/exec_runner.go:374-405 | pkg/proxy/containers/xray/exec_runner.go:382 generateDefaultConfig 仍硬编码 APIPort: 62789（注释称'与 GRPCAPIAddress 默认值保持一致'），但 NewExecutor (exec_runner.go:128-136) 在 GRPCAPIAddress 为空时用 net.Listen("127.0.0.1:0") 动态分配端口，此路径下配置文件端口与 grpcAPIAddress 仍不匹配；generateDefaultConfig 未调用 parsePortFromAddr(e.grpcAPIAddress)。仅 register.go:40 Decode 显式设 62789 的路径巧合一致 |
| X-03 | P1 | **still-present** | pkg/proxy/tools/process/runner.go:147-151 | pkg/proxy/tools/process/runner.go:147-151 IsRunning 仅检查 p.cmd != nil && p.cmd.Process != nil，Start (runner.go:57-99) 未启动 Wait/watchdog goroutine，子进程崩溃后 cmd 不会被置 nil，IsRunning 仍返回 true；Executor.IsRunning (exec_runner.go:468-470) 直接委托，无自愈机制 |
| X-04 | P2 | **still-present** | pkg/proxy/containers/xray/exec_runner.go:462-482 | pkg/proxy/containers/xray/exec_runner.go:480-482 Restart() 仍直接调 e.Runner.Restart()，绕过 BaseContainer 状态机且不重启 reconcileLoop；Reload() (exec_runner.go:462-464) 走 BaseContainer.Restart()，但 executorHooks.GetRunFunc (exec_runner.go:107-117) 的 run/stop 只调 Runner.Start/Stop，同样不触碰 reconcileLoop（startReconcileLoop 只在 Executor.Start 里调，exec_runner.go:437） |
| X-05 | P2 | **still-present** | pkg/proxy/containers/xray/exec_runner.go:842-881 | RemoveInboundConfig 删 store（pkg/proxy/containers/xray/exec_runner.go:540-544 storeMgr.InboundStore().Delete(tag)），而 RemoveInboundNative (exec_runner.go:842-881) 全程不触碰 storeMgr；对应的 AddInboundNative 却会 Save 到 store（exec_runner.go:704-711），不一致仍在 |
| X-06 | P3 | **still-present** | pkg/proxy/containers/xray/exec_runner.go:501-508 | pkg/proxy/containers/xray/exec_runner.go:501-508 Version() 每次调用 exec.Command(e.config.BinaryPath, "version") 启动子进程，无缓存（仅新增 BaseContainer.IsRunning() 前置检查） |
| X-07 | P0 | **still-present** | pkg/proxy/containers/xray/grpc_client.go:372-430 | pkg/proxy/containers/xray/grpc_client.go:381-388 QueryStats 仍把 json.Marshal(QueryStatsRequest{...}) 的 []byte 直接传给 conn.Invoke(ctx, "/xray.app.stats.command.HandlerService/QueryStats", reqData, &response)，把 JSON 当 protobuf wire format 发送，响应也按 JSON 解析（grpc_client.go:401-403） |
| X-08 | P2 | **still-present** | pkg/proxy/containers/xray/grpc_client.go:169,256,280,320,352,373 | 每次调用新建 gRPC 连接：grpc.Dial 出现在 pkg/proxy/containers/xray/grpc_client.go:256, 280, 320, 352, 373（ListInbounds/RemoveInbound/AddUser/RemoveUser/QueryStats 各自 Dial+defer Close）；AddInbound 仍有硬编码 time.Sleep(500 * time.Millisecond)（grpc_client.go:169） |
| X-09 | P3 | **still-present** | pkg/proxy/containers/xray/grpc_client.go:116 | debug 分支仍用 fmt.Printf 而非结构化日志：pkg/proxy/containers/xray/grpc_client.go:116,119,124,127,151,172,181,211,217 及 debugDump* 系列（530,534,549,557,564,568,640,645,656,677,704,720） |
| X-10 | P3 | **still-present** | pkg/proxy/containers/xray/grpc_client.go:295-306 | pkg/proxy/containers/xray/grpc_client.go:295-306 AddUser 仍硬编码 accountType = "xray.proxy.trojan.Account" + trojan.Account{Password}，注释原文仍写 'This method is currently unused in the production path' |
| X-11 | P3 | **still-present** | pkg/proxy/containers/xray/config_renderer.go:156 | pkg/proxy/containers/xray/config_renderer.go:156-161 buildAPI(port int) 函数体只返回 tag/services，port 参数完全未使用（调用点 config_renderer.go:34 传入 model.APIPort） |
| X-12 | P2 | **still-present** | pkg/proxy/containers/xray/updater.go:186,192 | pkg/proxy/containers/xray/updater.go:186 与 192 两处 u.swapper.Rollback(u.config.BinaryPath, backupPath) 的返回错误均被丢弃（无赋值、无日志），rollback 失败时仍可能留下不一致状态 |
| X-13 | P3 | **still-present** | pkg/proxy/containers/xray/updater.go:213-261 | pkg/proxy/containers/xray/updater.go:248 extractBinary 中 io.Copy(tmpFile, rc) 对 zip entry 无大小上限（未用 io.LimitReader / 未校验 UncompressedSize），zip-bomb 风险仍在 |
| X-14 | P3 | **still-present** | pkg/proxy/containers/xray/updater.go:174-199 | pkg/proxy/containers/xray/updater.go:174-199 更新成功路径（result.Restarted = restarted 后直接 return）没有任何 os.Remove(backupPath)；文件中 backupPath 仅用于 rollback（186/192），成功后旧备份文件保留 |
| X-15 | P3 | **still-present** | pkg/proxy/containers/xray/register.go:41 | pkg/proxy/containers/xray/register.go:41 Decode 默认 c.AutoDownload = true（注释'默认自动下载 xray 二进制'），仅当配置显式给出 auto_download 时才覆盖（register.go:66-68） |

## S4b — containers/xray InboundAdapter + profilegen

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| A-01 | 误报 (P3 撤销) | **not-applicable** |  | 旧 review 已自行标注为误报（xray port 字段支持字符串 port range），无缺陷需要跟踪 |
| A-02 | P1 | **partially-fixed** | pkg/proxy/containers/xray/inbound_adapter.go:544-559 | 主路径已修复：reality_short_ids 列表写入 realitySettings["shortIds"]（inbound_adapter.go:546），自动生成路径也写 shortIds（:555）。但 backward-compat 分支在只有单数 reality_short_id 扩展时仍写入 xray 不识别的单数 key：realitySettings["shortId"] = realityShortID（inbound_adapter.go:551） |
| A-03 | P1 | **fixed** | pkg/proxy/containers/xray/exec_runner.go:826-833 | extractNativeExtra 从渲染后的 realitySettings.privateKey 用 DeriveRealityPublicKey 推导公钥并写入 extra["reality_public_key"]（exec_runner.go:826-833，被 :678 在构建 XrayInbound 时调用）；buildSubscriptionExtensions 拷贝 reality_public_key（exec_runner.go:1545），订阅 URI 输出 pbk=（subscription.go:583）。profilegen 路径也回写 extensions["reality_public_key"]（profilegen/vless_profiles.go:440） |
| A-04 | P2 | **still-present** | pkg/proxy/containers/xray/inbound_adapter.go:163-176 | Shadowsocks 分支仍只取 users[0] 的 method/password（inbound_adapter.go:167,169），注释写明 "single-user mode" 且 "Do NOT add clients"（:176），对多用户输入既无 clients 展开也无任何警告日志，静默截断行为未变 |
| A-05 | P2 | **still-present** | pkg/proxy/containers/xray/profilegen/shadowsocks.go:250 | 各 profilegen 仍各自硬编码 email："auto@ss.local"（shadowsocks.go:250）、"auto@trojan.local"（trojan_tls.go:132）、"auto@vless.local"（vless_profiles.go:334）、"auto@vmess.local"（vmess_tls.go:140），无统一常量/格式策略 |
| A-06 | P3 | **partially-fixed** | pkg/proxy/core/params/protocolparams/keys.go:15-79 | 已新建规范常量文件 pkg/proxy/core/params/protocolparams/keys.go（含 KeyRealityPublicKey = "reality_public_key" 等 60+ 常量,keys.go:79），供 parser/RPC 侧使用；但 containers/xray 目录内没有任何文件 import protocolparams（grep 无命中），inbound_adapter.go:567、subscription.go:583 等处仍直接使用字符串字面量 |
| V-01 | P0 | **fixed** | pkg/proxy/containers/xray/profilegen/vless_profiles.go:273-293 | 旧的 generateRandomKeyPair 已删除（grep 全仓库无命中），替换为 GenerateRealityKeyPairWithPublic：X25519 clamping 后用 curve25519.ScalarBaseMult 从私钥正确推导公钥并分别返回（vless_profiles.go:286-292）；applyDefaults 中分别赋给 p.RealityPrivateKey / p.RealityPublicKey（:251-258） |
| V-02 | P2 | **fixed** | pkg/proxy/containers/xray/profilegen/vless_profiles.go:117-126 | Validate 的 validTransports 白名单只含 tcp/ws/grpc/httpupgrade/xhttp，不含 h3，非法 transport 直接返回 error（vless_profiles.go:117-126），h3 无法再写入 xray 配置 |
| V-03 | P3 | **fixed** | pkg/proxy/containers/xray/profilegen/vless_profiles.go:110-113 | 空操作函数 normalizeTransport 已删除（grep pkg/proxy/ 全目录无命中）；仅存的 transport 归一化是 Validate 中 splithttp→xhttp 的有效映射（vless_profiles.go:110-113） |
| V-04 | P2 | **still-present** | pkg/proxy/containers/xray/profilegen/vless_profiles.go:183-187 | Validate 仍允许空 security（validSecurities 含 ""，vless_profiles.go:141-145），applyDefaults 仍将空 security 静默改为 "tls"（vless_profiles.go:186），对调用方仍不透明 |
| S-01 | 误报 (撤销) | **not-applicable** |  | 旧 review 已自行标注为误报（VMess URI 用 RawURLEncoding 原写法正确），无缺陷需要跟踪 |
| S-02 | P0 | **fixed** | pkg/proxy/containers/xray/subscription.go:266-272 | 新增 extStringOrJoinSlice 同时支持 string 和 []string 两种存储形态（subscription.go:638-653），Reality URI 的 serverNames=/sid= 参数改用该 helper 读取 reality_server_names/reality_short_ids（subscription.go:267-271），[]string 类型不再丢失 |
| S-03 | P3 | **still-present** | pkg/proxy/containers/xray/subscription.go:82 | generateSubscriptionSpec（subscription.go:82）在生产代码中仍无调用者（grep 全仓库仅有定义本身）；实际订阅路径走 in.GetSub（subscription.go:61 → exec_runner.go:1441），后者用的是 exec_runner.go:1521 的另一个同名方法 buildSubscriptionExtensions。subscription.go:189 的包级 buildSubscriptionExtensions 仅被 generateSubscriptionSpec 和测试调用，死代码链仍在 |
| SS-01 | P2 | **still-present** | pkg/proxy/containers/xray/profilegen/shadowsocks.go:88-90 | Validate 仍把 "reality" 列为 Shadowsocks 合法 security 值（shadowsocks.go:88: `if p.Security != "" && ... && p.Security != "reality"` 才报错），SS+Reality 非法组合仍未拦截 |
| SS-02 | P2 | **still-present** | pkg/proxy/containers/xray/profilegen/shadowsocks.go:199 | validate2022Password 仍只用 base64.StdEncoding.DecodeString（shadowsocks.go:199），带 padding 要求，no-padding 的合法 2022 密钥仍会被误拒 |
| T-01 | P3 | **still-present** | pkg/proxy/containers/xray/profilegen/trojan_tls.go:84-87 | generateRandomPassword 仍用 base64.StdEncoding.EncodeToString（trojan_tls.go:87），输出可含 +/=/ 字符；注释（:83）声称 "random 32-byte hex string" 但实现是 base64，与建议的 hex 改法不符 |

## S5 — containers/hysteria + containers/snell

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| H-01 | P2 | **still-present** | pkg/proxy/containers/hysteria/container.go:157, pkg/proxy/containers/snell/container.go:112 | Both constructors still call userMgr.Subscribe() (hysteria/container.go:157, snell/container.go:112) and neither package contains any Unsubscribe call (grep over pkg/proxy/containers/ finds Unsubscribe only in mihomo/container.go:278). stop closures only call closeUserEventCh (hysteria/container.go:87, snell/container.go:81), so UserManager keeps the dead subscriber channel forever. |
| H-02 | P2 | **still-present** | pkg/proxy/containers/hysteria/container.go:450-460, pkg/proxy/containers/snell/container.go:356-365 | forwardUserEvents still does `for event := range source` over the UserManager subscription channel (hysteria/container.go:450, snell/container.go:356); closeUserEventCh (hysteria/container.go:838-843, snell/container.go:698-703) only closes the local userEventCh and sets it nil. Since Unsubscribe is never called (H-01), source never closes and the goroutine leaks. |
| H-03 | P3 | **partially-fixed** | pkg/proxy/containers/hysteria/container.go:430-432, pkg/proxy/containers/snell/container.go:336-338 | FastAddInbound is no longer an unconditional 'not supported' stub — it now actually enables the default inbound (commit 061b7df, hysteria/container.go:430-442, snell/container.go:336-348). But the remaining unsupported case (tag != defaultInboundTag) still returns a bare fmt.Errorf custom string (`"hysteria: only the default inbound %q is supported"`), not wrapping container.ErrNotSupported (defined at pkg/proxy/core/container/base.go:314), so errors.Is still cannot detect it. |
| H-04 | P0 | **still-present** | pkg/proxy/containers/hysteria/container.go:51,74,225,241-249 | Code is byte-identical in structure to the reviewed commit f3ebd40: run() assigns hc.certWaitStopCh with no lock (container.go:74), the waitForCertAndStart goroutine reads the field in its select loop (container.go:225), and closeCertWaitStopCh in stop does an unguarded nil-check + select/close (container.go:241-249). No mutex was added for this field (field decl at :51); a restart can reassign the field in run() while the previous waitForCertAndStart goroutine (which stopProcess does not wait for) still reads it. |
| H-05 | P1 | **still-present** | pkg/proxy/containers/hysteria/container.go:60-80,164-168,229-232 | run() still returns nil immediately after `go h.c.waitForCertAndStart()` (container.go:77-79); BaseContainer.Start then calls MarkRunning right away (pkg/proxy/core/container/base.go:191-194). startProcess() failures in waitForCertAndStart are only logged: `slog.Error("hysteria: start process failed", ...)` at container.go:166-167 (direct cert path) and :230-232 (polling path), so IsRunning() stays true with no process. |
| H-06 | P3 | **still-present** | pkg/proxy/containers/hysteria/process.go:130 | generateConfigFile still builds `fmt.Sprintf("http://127.0.0.1:%d/api/authHysteria2", hc.httpPort)` at process.go:130 with no validation anywhere that httpPort != 0 — the only assignments are the field decl (container.go:31) and WithHTTPPort option (container.go:128), neither of which checks for zero. |
| H-07 | P3 | **still-present** | pkg/proxy/containers/hysteria/container.go:271-274 | `// Reload is a no-op for hysteria.` / `func (hc *HysteriaContainer) Reload() error { return nil }` at container.go:271-274 — still a silent no-op, no SIGHUP-based hot reload. |
| SN-01 | P0 | **fixed** | pkg/proxy/containers/snell/container.go:39-48 | A mutex was added: field comment at container.go:39-45 states "mu guards addedUsers and inboundEnabled against concurrent access between [event handler] and reconcile goroutine". handleUserEvent wraps every addedUsers read/write in sc.mu.Lock/Unlock (container.go:389-392, 407-409, 412-414, 430-432) and reconcileUsers does too (container.go:444-447, 462-464), with a documented TOCTOU rationale at :381-386. |
| SN-02 | P2 | **partially-fixed** | pkg/proxy/containers/snell/container.go:522-524 | The listen half is moot (listen is now hardcoded to 127.0.0.1 everywhere: generateConfigFile container.go:251, restore comment :522). But the port is still restored unconditionally: `if port, ok := data["port"].(float64); ok { sc.cfg.Port = int(port) }` at container.go:523-524, with no `if sc.cfg.Port == 0` guard — hysteria's counterpart has that guard (hysteria/container.go:625-631 "Only restore from store if the config field is at its zero/default value"). A user changing the configured snell port and restarting still gets the stale stored port. |
| SN-03 | P2 | **still-present** | pkg/proxy/containers/snell/container.go:557 | GetUserSubscriptions (container.go:550) still calls `sc.userMgr.GetUserPortByDst(req.User.Username, uint32(sc.cfg.Port))` at :557 with no nil check on sc.userMgr — other methods in the same file guard it (`if sc.userMgr == nil` at :281, :594, :665), but this one does not; nil userMgr panics. |
| SN-04 | P3 | **fixed** | pkg/proxy/containers/snell/container.go:106-107 | Design intent now documented at the point of use: `// snell binds loopback only; forward layer handles external access.` (container.go:106) before SetListenAddr("127.0.0.1"); restoreInboundConfig also notes "listen is always 127.0.0.1" (:522) and "listen is always loopback" (:535), and generateConfigFile hardcodes 127.0.0.1 (:251) so the cfg.Listen default no longer silently controls the bind address. |
| H-08 | P2 | **still-present** | pkg/proxy/core/container/interface.go:44-55 | The Container interface still has no IsSingleInbound()/capability metadata — FastAddInbound's doc (interface.go:44-55) describes only the generic multi-inbound contract, and grep for IsSingleInbound/SingleInbound across the repo returns nothing. The single-inbound constraint remains implicit, enforced only at runtime by a tag equality check returning an error (hysteria/container.go:430-432, snell/container.go:336-338). |

## S6 — usermanager

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| U-01 | P0 | **fixed** | pkg/proxy/usermanager/usermanager.go:2113-2145 | Race eliminated by redesign: onCollect callback is now invoked synchronously while sc.mu is held (usermanager.go:1926-1938, comment explains lock order sc.mu -> m.mu), the lastSeen map was replaced by m.lastSeenUserTotal which is only read/written under m.mu inside the callback (usermanager.go:2123-2129), and ForceCollect's `go sc.collect()` (usermanager.go:1991-1993) serializes on sc.mu.Lock inside collect() (usermanager.go:1858-1859). ResetUserTotalTraffic also acquires sc.mu before m.mu to match (usermanager.go:2264-2270). |
| U-02 | P1 | **still-present** | pkg/proxy/usermanager/usermanager.go:2233-2246 | GetAllDeltaTraffic still performs two independent lock acquisitions: m.statsCollector.GetStats() takes sc.mu.RLock and releases (usermanager.go:2238, GetStats at 1943-1947), then resetAllDeltas() takes sc.mu.Lock separately (usermanager.go:2243, resetAllDeltas at 2009-2011). A collect() cycle can run in the window between them, and its increments are zeroed without being returned. Same pattern in GetUserDeltaTraffic (usermanager.go:2221-2229). |
| U-03 | P1 | **still-present** | pkg/proxy/usermanager/usermanager.go:1360-1362 | ReleaseBindPort (usermanager.go:1311) still calls m.forwardMgr.RemoveRulesByUser(req.Username) at line 1361 instead of removing only the rule for the released port, so releasing one port tears down all of the user's forward relays across inbounds. RemoveRule(single) exists and is used elsewhere (e.g. RotateUserPortForInbound, usermanager.go:1052). |
| U-04 | P2 | **still-present** | pkg/proxy/usermanager/usermanager.go:352-374, 1311-1372 | mutateUser still does m.store.Save(user) while holding m.mu write lock (usermanager.go:353-374). ReleaseBindPort still holds m.mu (usermanager.go:1312-1313) across forwardMgr.RemoveRulesByUser (1361) and store.Save (1366); GetBindPort likewise holds m.mu across AddRule and store.Save (865, 929, 956). Some mitigation exists for runtime side effects (applyRuntimeSideEffects is called after releasing m.mu, usermanager.go:1176-1183 comment), but the core coarse-grained lock+IO pattern remains. |
| U-05 | P2 | **still-present** | pkg/proxy/usermanager/usermanager.go:867-875 | GetBindPort (usermanager.go:855) checks only existence (867-870) and user.IsExpired() (873-875); there is no user.IsDeleting() check anywhere in the function (grep of IsDeleting shows no hit between lines 855-966), so a tombstoned user can still be allocated a port and forward rule. |
| U-06 | P2 | **still-present** | pkg/proxy/usermanager/usermanager.go:975-978, 1332-1335 | The ReleaseBindPort doc comment still promises finalize: 'if the user is in "deleting" state and has no more forward rules, it will physically delete the user (finalize)' (usermanager.go:976-978), and inside the body the comment 'Still check for finalize if user is deleting' (1334) is followed by no finalize code — ReleaseBindPort never deletes tombstones. RemoveUser's doc even states the opposite: 'The user stays in memory and store — it is never physically deleted' (usermanager.go:541-542). Tombstones remain in memory permanently (only replaced on re-AddUser, usermanager.go:480-481). |
| U-07 | P2 | **fixed** | pkg/proxy/usermanager/usermanager.go:999-1126 | RotateUserPort was rewritten as RotateUserPortForInbound (commit 6ec8694 'refactor port rotation APIs') using make-before-break: new relay is created under a temp rule key first (usermanager.go:1029-1050), old rule removed only after the new one is confirmed (1052-1058), and user.BindPorts/PortMappings are mutated and persisted via store.Save only in Step 5 after the final relay is up (1091-1114). A crash mid-rotation leaves the DB with the previous consistent mapping, and GetBindPort falls back to auto-allocate if the preferred port is unavailable on restart (usermanager.go:929-940). Test coverage exists in pkg/proxy/usermanager/rotate_inbound_port_test.go. |
| U-08 | P3 | **still-present** | pkg/proxy/usermanager/usermanager.go:604-607 | GetUser still returns errors.ErrUserNotFound for deleting users: 'if user.IsDeleting() { return nil, errors.New(errors.ErrUserNotFound, ...) }' (usermanager.go:605-607). Callers still cannot distinguish 'not found' from 'deleting' via GetUser (a separate GetUserIncludingDeleting exists at 692, but the error code conflation itself is unchanged). |
| U-09 | P3 | **fixed** | pkg/proxy/usermanager/usermanager.go:2263-2293 | ResetUserTotalTraffic (commit b8c8b83 'feat(user): add reset traffic API') now persists the reset: zeroes u.TrafficTotalUplink/Downlink, drains collector state via drainForUserLocked, clears lastSeenUserTotal, then calls m.store.Save(u) returning an error on failure (usermanager.go:2287-2290). The TODO is gone and the doc comment states 'persists the reset to storage' (2248-2249). Covered by pkg/proxy/usermanager/reset_user_total_traffic_test.go. |
| U-10 | P3 | **still-present** | pkg/proxy/usermanager/sync/hash.go:51 | ComputeHash still writes UpdatedAtUs into the digest: writeField(strconv.FormatInt(u.UpdatedAtUs, 10)) at hash.go:51. Identical logical modifications made on nodes with clock skew still produce different hashes and trigger sync. The behavior is now documented as intentional for conflict detection (hash.go:11-16 comment), but the technical behavior flagged by the review is unchanged. |
| U-11 | P3 | **still-present** | pkg/proxy/usermanager/sync/hash.go:57-62 | ComputeHash still conditionally includes LoginPassword only when non-empty: 'if u.LoginPassword != "" { writeField(u.LoginPassword) }' at hash.go:60-61. A comment (hash.go:57-59) now explains this is deliberate for wire compatibility with old nodes, but the conditional-inclusion shape flagged by the review is unchanged. |
| U-12 | P3 | **fixed** | pkg/proxy/usermanager/sync/version.go:7-15 | The tie-break behavior itself is intentionally unchanged (IsNewer compares OriginNode lexicographically when UpdatedAtUs is equal, version.go:14), but the review's remediation was to record the decision — which is done: the code comment documents the stable tie-break semantics (version.go:7-9) and docs/user-sync-design.md:249-251 records '同一时间戳下，由 origin_node 作为稳定 tie-break' as a design decision (the repo has no DECISIONS.md; user-sync-design.md is the equivalent design record). |

## S7 — core/subscription + converter + appconfig + COMPAT-01

| ID | 旧优先级 | 现状 | 当前位置 | 证据 |
|----|---------|------|----------|------|
| SUB-01 | P2 | **still-present** | pkg/proxy/core/subscription/manager.go:57-60 | pkg/proxy/core/subscription/manager.go:57-60 — `subs, err := c.GetUserSubscriptions(req); if err != nil { continue }` per-container error still silently dropped with no logging; the doc comment at manager.go:40 now explicitly says 'failed containers are silently skipped', i.e. the behavior is unchanged (only documented). |
| SUB-02 | P1 | **still-present** | pkg/proxy/core/subscription/converter/clash.go:47-52, 177-180, 944 | clash.go:47-52 still hardcodes the same 4 third-party subconverter URLs; ConvertWithOptions at clash.go:177-180 returns an error when fetchClashTemplate fails ('fetch clash template failed'), so Clash subscription still completely fails when the network is unavailable; buildLocalConfig still exists at clash.go:944 and grep shows zero callers. Note: the code now documents fail-fast as intentional (clash.go:156 '(fail fast; no silent fallback)'), but the defect as described (no local degradation path used) remains. |
| SUB-03 | P1 | **fixed** | pkg/proxy/core/subscription/converter/clash.go:56, 700-717, 738-759 | fetchClashTemplate (clash.go:700-703) now sends only a hardcoded fake node (clashFakeSub = 'ss://...@192.168.100.1:8888#FakeSub', clash.go:56) to the external service to pull the template skeleton; real user proxies are injected locally via injectProxiesToTemplate (clash.go:738-759). User node URIs are never sent to third parties. |
| SUB-04 | P1 | **still-present** | pkg/proxy/core/subscription/converter/http.go:25-40 | converter/http.go:25 `client := &http.Client{}` — no timeout; converter/http.go:36 `io.ReadAll(resp.Body)` — no response size limit. The parallel subscription/http.go implementation still exists separately, so the duplication/inconsistency also remains. |
| SUB-05 | P1 | **fixed** | pkg/proxy/core/subscription/codec/vmess.go:142-205 | parseVMessURI no longer exists; the refactored codec.DecodeVMess (codec/vmess.go:142-205) decodes the base64 body with 4 encoding variants, unmarshals the JSON, and populates Host (add), Port, UUID (id), transport etc., with validation errors on missing add/id/port. Wired into the conversion chain via codec.Decode → nodeToSpec → vmessNodeToSpec (uri_convert.go:145, 186, 259). |
| SUB-06 | P2 | **fixed** | pkg/proxy/core/subscription/codec/node.go:65, uri_convert.go:197-198 | Old parseSnellURI was removed in the codec refactor; its replacement DecodeSnell is registered in the scheme decoder map (codec/node.go:65 `"snell": func(u string) (Node, error) { return DecodeSnell(u) }`) and dispatched at uri_convert.go:197-198 (case *codec.SnellNode → snellNodeToSpec at uri_convert.go:487). No longer dead code. |
| SUB-07 | P3 | **fixed** | pkg/proxy/core/subscription/codec/shadowsocks.go:111-144 | decodeSSUserInfo (codec/shadowsocks.go:111-144) now takes the full userinfo via u.User.String() (line 122) instead of relying on url.Parse's user/password split, tries 4 base64 alphabets, and handles SIP022 ('2022-' prefixed) plaintext method:password explicitly (lines 117-120). The %3A split-ambiguity path is gone; file header (lines 17-18) documents the SIP002/SIP022 formats. |
| SUB-08 | P2 | **fixed** | pkg/http/sub_handler.go:48 | GenerateFakeSSSub (pkg/proxy/core/subscription/fake.go:19) is now called at pkg/http/sub_handler.go:48 `c.String(200, subscription.GenerateFakeSSSub())` — no longer dead code. |
| SUB-09 | P3 | **still-present** | pkg/proxy/core/subscription/fake.go:29,38,43 | generateRandomIP/generateRandomPort/generateRandomString still each use `rand.New(rand.NewSource(time.Now().UnixNano()))` (fake.go:29, 38, 43) — non-cryptographic seeding unchanged (impact remains low since the output is an intentionally fake node). |
| SUB-10 | P3 | **still-present** | pkg/proxy/core/subscription/converter/registry.go:9-22 | registry.go:9-22 Register/Get/GetOrDefault are still pure forwarders to subscription.RegisterConverter/GetConverter/GetConverterOrDefault. Only remaining callers are the package's own tests (converter_test.go:326,333,340,350); production code (e.g. manager.go:99) calls subscription.GetConverterOrDefault directly, so the redundant indirection persists. |
| APP-01 | P2 | **still-present** | pkg/proxy/appconfig/loader.go:247-284 | Validate (loader.go:247-284) checks only store.dsn, forward min/max port ordering, cert_mgmt.email, ping node source types, and enabled-container count. grep confirms no validation of rpc_port, http_port, proxy_host, or cluster.token anywhere in the function. |
| APP-02 | P3 | **still-present** | pkg/proxy/appconfig/loader.go:199-206 | loader.go:199-206 — when end_node.jwt_secret is empty, a random secret is generated on every load and only a fmt.Fprintf(os.Stderr, ...) warning is emitted ('sessions will be invalidated on restart'); the secret is not persisted back to the config. Behavior unchanged, now documented as an accepted trade-off in the comment at lines 195-198. |
| APP-03 | P3 | **still-present** | pkg/proxy/appconfig/loader.go:252-255 | Validate only checks forward.min_port < forward.max_port (loader.go:252-255); grep for 'conflict'/'overlap' in pkg/proxy/appconfig/ returns nothing — there is still no cross-check between the forward port range (forward.PortAllocatorConfig, config.go:224) and inbound/container port ranges. |
| COMPAT-01 | Feature | **fixed** | pkg/proxy/containers/xray/inbound_adapter.go:437-445, pkg/proxy/containers/xray/profilegen/vless_profiles.go:22-66 | Commit e185b33 (2026-04-16, 'feat: align FastAdd with latest xray-core, add httpupgrade/xhttp transport and e2e tests') reworked exactly the files named in the finding: removed xtls (mapped to tls+flow), removed h2/h3/http transports, added httpupgrade/xhttp (inbound_adapter.go:437-445 httpupgradeSettings; vless_profiles.go:22-24 valid transports 'tcp/ws/grpc/httpupgrade/xhttp' with splithttp→xhttp note), added sniffing/TLS advanced options, and shipped a 13-combination e2e system test against an auto-downloaded real xray binary — verifying the generated native JSON loads in current xray. |