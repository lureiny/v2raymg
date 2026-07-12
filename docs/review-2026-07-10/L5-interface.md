# L5 接口层 review — http / cmd / cli

> Finder → 对抗性 verifier 两级流程产出。下表为**保留**（confirmed + uncertain）的 findings，已剔除 verifier 判定 refuted 的 0 条。uncertain 多为依赖第三方协议/上游行为、无法在仓库内确证的条目，处理前需人工核对。

## 统计

| 维度 | 数量 |
|------|------|
| 保留条目 | 46 |
| — confirmed | 46 |
| — uncertain | 0 |
| — 其它(unverified) | 0 |
| refuted(已剔除) | 0 |

| 优先级 | P0 | P1 | P2 | P3 |
|--------|----|----|----|----|
| 保留条目 | 0 | 3 | 22 | 21 |

## 速查表

| # | 优先级 | 判定 | 维度 | 单元 | 位置 | 标题 |
|---|--------|------|------|------|------|------|
| 1 | P1 | ✓ | 安全 | HTTP | `pkg/http/sub_handler.go:177` | /sub 公共端点透传任意 URL 触发 SSRF：ext_sub 与 proxy_groups_url/rule_providers_url/rules_url 无 host/scheme/内网校验 |
| 2 | P1 | ✓ | 错误处理 | CMD | `cmd/server.go:229` | RPC/HTTP server 在 goroutine 中启动，绑定失败或地址非法时静默失败，节点降级为无管理面僵尸而进程不退出 |
| 3 | P1 | ✓ | 错误处理 | CMD | `cmd/cli/client/http_client.go:110` | 约 30 个 API 封装函数完全忽略 HTTP 状态码，4xx/5xx 失败被当作成功结果打印 |
| 4 | P2 | ✓ | 安全 | HTTP | `pkg/http/sub_handler.go:166` | SubHandler 以 Info 级别打印完整节点订阅 URI，泄漏用户代理凭据到日志 |
| 5 | P2 | ✓ | 安全 | HTTP | `pkg/http/auth_with_token_handler.go:11` | /api/metrics 遗留鉴权在 token 未配置时 fail-open，且绕过 AuthMiddleware/JWT/黑名单体系 |
| 6 | P2 | ✓⚠️协议 | 安全 | HTTP | `pkg/http/auth_hysteria.go:50` | AuthHysteria2 公共端点是未认证密码预言机：可枚举用户、非常量时间比较、无限速 |
| 7 | P2 | ✓ | 正确性 | HTTP | `pkg/http/sub_handler.go:131` | 无可用节点时 /sub 返回 200 空订阅而非报错：nil 守卫是死代码（GetNodesWithFilter 永不返回 nil） |
| 8 | P2 | ✓ | 错误处理 | HTTP | `pkg/http/copy_user_between_nodes_handler.go:60` | copyUser 对 RPC 结果做无 ok 校验的类型断言，nil 或异常类型直接 panic |
| 9 | P2 | ✓ | 资源 | HTTP | `pkg/http/prometheus_desc/node_collector_desc.go:68` | nodeMetricDesc.Collect 忽略 Unmarshal 错误并用 MustNewConstMetric，畸形远端指标可 panic 掉 /api/metrics |
| 10 | P2 | ✓ | 错误处理 | HTTP | `pkg/http/prometheus_desc/node_collector_desc.go:47` | nodeMetricDesc.Collect 忽略 pb.Unmarshal 错误，空/损坏指标 blob 触发 MustNewConstMetric panic 中断整个抓取 |
| 11 | P2 | ✓ | 错误处理 | HTTP | `pkg/http/user_handler.go:91` | target=all 写操作中任一节点失败即返回 500，掩盖部分成功导致重试产生副作用 |
| 12 | P2 | ✓ | 正确性 | HTTP | `pkg/http/prometheus_desc/v2raymg_traffic_desc.go:70` | /api/metrics 每次抓取对全集群做破坏性 DrainStats，多 scraper/多入口并存时互相清零流量增量 |
| 13 | P2 | ✓ | 错误处理 | HTTP | `pkg/http/http_server.go:94` | HttpServer.Start 忽略 gin Run 的返回错误，端口被占用时管理 API 静默失效 |
| 14 | P2 | ✓ | 架构 | HTTP | `pkg/http/metric_handler.go:87` | /api/metrics 接口契约三处不一致：help 声称支持 JWT 但实现仅 X-Token、http-api-reference.md 完全未收录、README 声称 /api 下统一 JWT/X-Token |
| 15 | P2 | ✓ | 架构 | HTTP | `pkg/http/set_user_bandwidth_handler.go:67` | 错误响应契约混杂：部分 handler 用 HTTP 200 + body code=300/404 表达失败，与 jsonErr 5xx 系及文档约定冲突 |
| 16 | P2 | ✓ | 正确性 | CMD | `cmd/server.go:271` | 优雅关闭缺失：SIGTERM 时不做最终流量落盘（StopTrafficStats/ForceCollect 存在但未接线），每次重启平均丢失约半个采集周期的计费流量 |
| 17 | P2 | ✓ | 安全 | CMD | `cmd/server.go:84` | 启动校验未强制 cluster.token 非空/最小强度，空或弱 token 配置直接落入下层已确认的 RPC 常量密钥缺陷，本层是唯一 fail-fast 拦截点 |
| 18 | P2 | ✓ | 正确性 | CMD | `cmd/server.go:217` | node groups 播种把 List 查询错误与空结果混同：DB 瞬时错误时会 DELETE 全表并只写回 default group，覆盖已配置的节点分组 |
| 19 | P2 | ✓ | 错误处理 | CMD | `cmd/cli/client/http_client.go:362` | 约 30 个 CLI API 封装完全不检查 HTTP 状态码，401/500 的错误响应体被当成功结果打印，DeleteUser 等对失败操作报 succ |
| 20 | P2 | ✓ | 安全 | CMD | `cmd/cli/config.go:55` | CLI 把管理员密码/静态 token 明文写入 0666 世界可读可写的配置文件，且每次启动无条件重写 |
| 21 | P2 | ✓ | 正确性 | CMD | `cmd/server.go:75` | --migrate 对 .json 配置执行时用 yaml.Marshal 写回 JSON 文件，直接破坏配置导致后续无法启动 |
| 22 | P2 | ✓ | 错误处理 | CMD | `cmd/cli/config.go:116` | getAuthToken 对任何登录失败静默回退静态 token（可能为空串），密码错误永远不被提示 |
| 23 | P2 | ✓ | 正确性 | CMD | `cmd/cli/client/http_client.go:743` | Profile 命令请求 GET /api/user 而非 /api/profile，管理员执行 Profile 会拿到全集群用户列表（含全部 auth_token）而非个人信息 |
| 24 | P2 | ✓ | 错误处理 | CMD | `cmd/cli/command.go:18` | REPL 启动前串行预热三张缓存，目标不可达时启动阻塞可达数分钟且失败被静默丢弃 |
| 25 | P2 | ✓ | 资源 | CMD | `cmd/server.go:274` | 关停序列只停容器：traffic-stats/maintenance 循环未停止就解构 Store，最后一个采样窗口的流量丢失且存在对已关闭 DB 的写入竞态 |
| 26 | P3 | ✓ | 错误处理 | HTTP | `pkg/http/metric_handler.go:46` | metrics 抓取丢弃 collector Update 错误，固定 1s 超时下静默下发陈旧/缺失指标 |
| 27 | P3 | ✓ | 架构 | HTTP | `pkg/http/change_password_handler.go:51` | 写 handler 的 HTTP 错误契约不一致：部分故障返回 200+code:300，部分返回真实 5xx/502 |
| 28 | P3 | ✓ | 架构 | HTTP | `pkg/http/change_password_handler.go:59` | 写 handler 错误响应契约不一致：部分返回 200+code:300/404，部分返回真实 5xx |
| 29 | P3 | ✓ | 错误处理 | HTTP | `pkg/http/get_certs_handler.go:36` | log.Error 被当作 printf 使用，格式串未展开生成乱码日志 |
| 30 | P3 | ✓ | 错误处理 | HTTP | `pkg/http/user_handler.go:394` | UserListHandler.serveAdmin 静默丢弃 failedList，管理员列表可能缺节点且无提示 |
| 31 | P3 | ✓ | 并发 | HTTP | `pkg/http/metric_handler.go:20` | prometheus 收集器在注册闭包内单例共享，并发抓取相互覆盖 Update 数据 |
| 32 | P3 | ✓ | 测试缺口 | HTTP | `pkg/http/sub_handler.go:45` | SubHandler（最复杂 handler）零测试覆盖：双认证、ext_sub 合并、空订阅、userinfo 二次扇出均未测 |
| 33 | P3 | ✓ | 安全 | HTTP | `pkg/http/auth/middleware.go:24` | 管理员静态 token 比较未用常数时间比较，存在计时侧信道 |
| 34 | P3 | ✓ | 资源 | HTTP | `pkg/http/auth/blacklist.go:15` | JWT 黑名单只增不减且仅内存：无过期清理造成无界增长，重启后已注销 token 复活 |
| 35 | P3 | ✓ | 测试缺口 | HTTP | `pkg/http/prometheus_desc/common.go:1` | pkg/http/prometheus_desc 整包无任何测试，违反「每个可测模块应有 *_test.go」约束 |
| 36 | P3 | ✓ | 并发 | CMD | `cmd/cli/info.go:35` | getNode 无锁读取 localNodeList，与 listNode/updateLocalNodeList 持 nodeMutex 整体重赋值的写路径不一致 |
| 37 | P3 | ✓ | 正确性 | CMD | `cmd/cli/command.go:405` | fastAddInbound 随机端口取值区间 10000-49999 与 forward 用户转发端口默认段（10000-60000）完全重叠，易与已分配 bind port 冲突 |
| 38 | P3 | ✓ | 正确性 | CMD | `cmd/cli/client/base_client.go:40` | 用 filepath.Clean 清理 URL path：Windows 下路径分隔符变反斜杠导致全部 API 请求 404，并吞掉尾部斜杠 |
| 39 | P3 | ✓ | 资源 | CMD | `cmd/cli/client/http_client.go:22` | Login 非 200 分支在读取 body 前直接返回错误，resp.Body 未关闭造成连接泄漏 |
| 40 | P3 | ✓ | 错误处理 | CMD | `cmd/cli.go:22` | cliFunc 用 panic 处理配置加载与 REPL 错误，用户看到 goroutine 堆栈而非错误信息，且绕过 main.go 的统一错误出口 |
| 41 | P3 | ✓ | 正确性 | CMD | `cmd/cli/info.go:26` | REPL 三张本地缓存只在启动时预热一次，updateCycle 已声明但从未接线，长会话中节点/用户/inbound 补全与 getNode 域名回填持续陈旧 |
| 42 | P3 | ✓ | 测试缺口 | CMD | `cmd/server.go:105` | 测试缺口：runEndNode/startServer 的装配顺序契约与 --migrate 分支零测试覆盖 |
| 43 | P3 | ✓ | 安全 | CMD | `cmd/cli/config.go:66` | inputConfig 用 fmt.Scanln 明文回显读取密码，且 token/密码含空格时被截断 |
| 44 | P3 | ✓ | 并发 | CMD | `cmd/cli/info.go:34` | getNode 无锁读取 localNodeList，与 listNode/updateLocalNodeList 持锁整体重赋值构成不一致的锁协议 |
| 45 | P3 | ✓ | 正确性 | CMD | `cmd/cli/command.go:404` | fastAddInbound 随机端口生成无冲突规避且与提示文案不符，tag 生成用 sha256(时间戳) 冗余 |
| 46 | P3 | ✓ | 测试缺口 | CMD | `cmd/cli/suggest.go:340` | 测试缺口：REPL 输入行解析（GetSuggest/getParamValue）、LoadConfig 落盘行为、command.go 全部 handler 均无测试；ListAllUsers 与 ListUser 完全重复 |

## 详细条目

### 1. [P1·confirmed] /sub 公共端点透传任意 URL 触发 SSRF：ext_sub 与 proxy_groups_url/rule_providers_url/rules_url 无 host/scheme/内网校验

- **位置**：`pkg/http/sub_handler.go:177` · 维度：安全 · 单元：HTTP
- **问题**：SubHandler 是 public 路由，任意持有效 token 或 user+pwd 的低权限用户即可访问。handler 把用户提供的 ext_sub（QueryArray，行 64）直接传给 subscription.FetchAndMergeExtSubs（行 178），把 proxy_groups_url/rule_providers_url/rules_url（行 32-34）传给 BuildConvertOptions/ConvertURIsWithOptions（行 189-199）。这些值最终被服务端 httpGet 拉取（pkg/proxy/core/subscription/http.go 已加 10s 超时/4MB 上限，但 converter/http.go 仍无超时——见下层 digest SUB/converter/http.go:25），且全程无 scheme 限制（可 file://? 实际 http.NewRequest 仅 http/https，但可指向 http://169.254.169.254、http://127.0.0.1:内部端口、http://<内网服务>）。handler 层对 URL 主机没有任何白名单/私网地址拒绝，构成经典 SSRF：低权限订阅用户可让 center/end 节点向内网发起 GET 并（部分）回显响应体到订阅结果。下层已确认 SSRF，本层是可拦截而未拦截的输入点。
- **证据**：extSubs := c.QueryArray("ext_sub") (行64); extURIs, truncated := subscription.FetchAndMergeExtSubs(extSubs) (行178); opts = subscription.BuildConvertOptions(..., parasMap["proxy_groups_url"], parasMap["rule_providers_url"], parasMap["rules_url"]) (行189-196)——无任何 host/私网校验
- **核实**：SubHandler 注册在 public 组 (init.go:25)，仅要求 token 或 user+pwd 的低权限认证。用户传入的 ext_sub (line 64→178) 与 proxy_groups_url/rule_providers_url/rules_url (32-34→189-196) 最终进入 subscription/http.go 的 httpGet(line 26)，该函数用 http.NewRequest+默认 Client，仅设 10s 超时/4MB 上限，无任何 host/scheme/私网地址白名单校验。handler 层同样零校验。低权限用户可诱导服务端向 127.0.0.1/169.254.169.254/内网服务发起 GET，ext_sub 返回体经解码后并入订阅结果部分回显。SSRF 输入点真实存在且可达，P1 合理。

### 2. [P1·confirmed] RPC/HTTP server 在 goroutine 中启动，绑定失败或地址非法时静默失败，节点降级为无管理面僵尸而进程不退出

- **位置**：`cmd/server.go:229` · 维度：错误处理 · 单元：CMD
- **问题**：runEndNode 用 `go rpcServer.Start()`（cmd/server.go:229）和 `go httpServer.Start()`（:257）启动两个管理面服务，但两者的失败都不会传播回主流程：EndNodeServer.Start 在 isAddrValid 失败（host 为空或 rpc port < 1000）时直接 `return`，一行日志都没有；net.Listen 失败仅 log.Error 后 goroutine 退出。HttpServer.Start 里 gin 的 `Run()` 返回值被直接丢弃，HTTP 端口被占用时无任何错误可见。结果是：配置错误或端口冲突时，代理容器照常启动服务用户，但节点既不能被集群发现（无 RPC 心跳注册）也不能被管理（无 HTTP API），运维只能从『节点消失』倒查。启动装配层应该对这两个关键服务做 fail-fast（同步 Listen 后再 go Serve，或通过 error channel 汇聚到主 goroutine 后 os.Exit），并且 appconfig.Validate 应校验 EndNode.ProxyHost/RpcPort/HttpPort（当前 Validate 完全不检查这些字段）。
- **证据**：cmd/server.go:229 `go rpcServer.Start()`、:257 `go httpServer.Start()`；pkg/rpc/server/end_node_server.go:218-226 `func (s *EndNodeServer) Start() { if !isAddrValid(s.Host, s.Port) { return } ... }`（isAddrValid 要求 port >= 1000，失败无日志）；pkg/http/http_server.go:92-95 `s.RestfulServer.Run(fmt.Sprintf(...))` 返回的 error 被丢弃；pkg/proxy/appconfig/loader.go:247 起的 Validate 不含任何 RpcPort/HttpPort/ProxyHost 校验。
- **核实**：逐条核实成立：cmd/server.go:229/:257 两个管理面服务均在裸 goroutine 中启动且无错误回传通道；pkg/rpc/server/end_node_server.go:214-226 isAddrValid（host 非空且 port>=1000）失败时 Start 无任何日志直接 return，net.Listen 失败仅 log.Error 后 goroutine 退出；pkg/http/http_server.go:92-95 gin Run() 的 error 被丢弃；pkg/proxy/appconfig/loader.go:247-284 Validate 确实不校验 ProxyHost/RpcPort/HttpPort。配置错误或端口占用时进程照常运行但管理面静默缺失，P1 合理。

### 3. [P1·confirmed] 约 30 个 API 封装函数完全忽略 HTTP 状态码，4xx/5xx 失败被当作成功结果打印

- **位置**：`cmd/cli/client/http_client.go:110` · 维度：错误处理 · 单元：CMD
- **问题**：http_client.go 中除 Login/ListNode/ListCert 三个函数外，其余全部封装（SetGatewayModel、ApplyCert、FastAddInbound、AddUser、UpdateUser、DeleteUser、ResetUser、UpdateProxy、RotateInboundPort、RotateAllPorts、SetUserRole、SetUserBandwidth、SetUserClientLimit、Logout、ChangePassword、TransferCert、GetStatus 等）的回调都是直接 readBody 后 `result = string(d); return nil`，不检查 resp.StatusCode。服务端鉴权失败返回 401 纯文本 "Unauthorized"（pkg/http/auth/middleware.go:30），业务失败返回 4xx/5xx JSON，此时 CLI 端 err==nil，command.go 里 deleteUser 会打印 `delete user[x] succ: Unauthorized`，addUser 打印 `add user[x] with result: {"code":401,...}`——操作实际失败却被明确报告为成功。而 ListUser/ListInboundsStructured 则把错误 JSON 塞进 json.Unmarshal，给用户抛出 "cannot unmarshal ..." 之类与真实原因无关的报错。应统一走 unexpectedHTTPStatus（该函数已存在于 http_client.go:54 但只有 2 个调用方）。
- **证据**：http_client.go:110-117 `cb := func(resp *http.Response) error { d, err := readBody(resp); if err != nil { return err }; result = string(d); return nil }`（该模式在文件内重复约 30 次，均无 StatusCode 检查）；对比 ListNode cb（70-72 行）有 `if resp.StatusCode != http.StatusOK { return unexpectedHTTPStatus(resp, d) }`；command.go:552 `fmt.Printf("delete user[%s] succ: %v\n", user, result)`
- **核实**：逐个函数核实：http_client.go 全文件中仅 Login（:22 检查 StatusCode!=200）、ListNode（:70 unexpectedHTTPStatus）、ListCert（:95 unexpectedHTTPStatus）三处检查状态码；其余约 27 个封装（SetGatewayModel、ApplyCert、FastAddInbound、CopyUserBetweenNodes、AddUser、UpdateUser、DeleteUser、ResetUser、AddInBound、GetInBound、UpdateProxy、SetPingCheck、ListInbounds、DeleteInboundByName、RotateInboundPort、RotateAllPorts、SetUserRole、SetUserBandwidth、SetUserClientLimit、Logout、Profile、ChangePassword、TransferCert、GetStatus 等）均为 readBody 后 `result = string(d); return nil`。服务端 AuthMiddleware（pkg/http/auth/middleware.go:30,50）确实对鉴权失败返回 401 纯文本 'Unauthorized'，command.go:552 确实打印 `delete user[%s] succ: %v`，失败被标注为成功属实；ListUser/ListInboundsStructured 把错误体喂给 json.Unmarshal 产生误导性报错也属实。unexpectedHTTPStatus（:54）已存在但只有 2 个调用方。触发路径真实（任何 401/4xx/5xx 响应即触发），P1 合理：管理员会误信删除/封禁等操作已生效。

### 4. [P2·confirmed] SubHandler 以 Info 级别打印完整节点订阅 URI，泄漏用户代理凭据到日志

- **位置**：`pkg/http/sub_handler.go:166` · 维度：安全 · 单元：HTTP
- **问题**：行 166 `log.Info("[SubHandler] node URIs", "node", n, "count", len(v), "uris", v)` 把每个节点返回的完整订阅 URI 列表原样写入 Info 日志。vmess/vless/trojan/ss URI 内嵌用户 UUID、proxy password（trojan/ss 明文密码、hysteria2/tuic 口令等），Info 是默认开启级别。任何能读日志的运维/日志采集/被拖库的日志文件都会拿到全量用户代理凭据，等价于订阅泄漏。同文件对 token 已用 maskToken 脱敏（行 88/127），却在 URI 处反而全量输出，脱敏策略不自洽。应降级到 Debug 或对 URI 做脱敏。
- **证据**：log.Info("[SubHandler] node URIs", "node", n, "count", len(v), "uris", v) — v 为 []string 完整 URI，含 uuid/password
- **核实**：sub_handler.go:166 确为 log.Info("[SubHandler] node URIs", ... "uris", v)，v 是完整 []string URI，含 vmess/vless/trojan/ss 的 uuid/明文密码等代理凭据；Info 为默认开启级别，且同文件对 token 已 maskToken 脱敏(88/127)，此处却全量输出，脱敏策略确实不自洽。问题真实。但该泄漏需具备日志读取/采集能力方可利用，非直接远程可利用，按常见评级更接近 P2/中危，故给 severityOverride=P2。

### 5. [P2·confirmed] /api/metrics 遗留鉴权在 token 未配置时 fail-open，且绕过 AuthMiddleware/JWT/黑名单体系

- **位置**：`pkg/http/auth_with_token_handler.go:11` · 维度：安全 · 单元：HTTP
- **问题**：MetricHandler 用的是 getAuthHandlerFunc（metric_handler.go:74），与 userGroup/adminGroup 的 auth.AuthMiddleware 完全不同的一套。校验逻辑 `token := c.GetHeader("X-Token"); if token != httpServer.token`：当 httpServer.token 为空串（未配置 token 的部署，如仅用 JWT 的单机/未设 cluster token 场景）时，缺省不带 X-Token 头的请求 token=""，"" != "" 为 false → 校验通过，/api/metrics 完全开放。metrics 暴露每用户上下行流量、节点 mem/cpu/版本/拓扑等敏感信息。AuthMiddleware 对空 xtoken 有 `if xtoken != ""` 保护（middleware.go:23），此遗留函数没有，形成 fail-open 分叉。
- **证据**：token := c.GetHeader("X-Token"); if token != httpServer.token { ...401 } — 无 token=="" 空值保护；httpServer.token 为空时空头请求通过
- **核实**：MetricHandler 经 RegisterPrometheus(cmd/server.go:255)→RegisterHandler 直接注册在根引擎 RestfulServer.GET 上，不在 auth.AuthMiddleware 的 /api 组内，鉴权唯一来源是 getHandlers 里的 getAuthHandlerFunc。auth_with_token_handler.go:10-11 逻辑 token:=GetHeader("X-Token"); if token != httpServer.token，当 httpServer.token 为空串且请求不带 X-Token 时，""!="" 为 false → 放行，端点完全开放并回显每用户流量/节点 mem/cpu/版本等敏感指标。AuthMiddleware 有 if xtoken!="" 保护 fail-closed，此遗留函数无，fail-open 分叉真实存在。P2 合理。

### 6. [P2·confirmed] AuthHysteria2 公共端点是未认证密码预言机：可枚举用户、非常量时间比较、无限速

- **位置**：`pkg/http/auth_hysteria.go:50` · 维度：安全 · 单元：HTTP · ⚠️协议相关（需人工核对上游）
- **问题**：AuthHysteria2 注册在 public 组（init.go:27），无任何调用方鉴权（本应只允许本机 hysteria2 进程回调，但监听在 HTTP 端口对外可达）。handlerFunc 遍历 ul.ListUsersWithPasswd()，用 `passwd == parasMap["auth"]` 及其 base64 变体逐个比对（行 51-52），命中即返回 `{ok:true, id:<username>}`（行 53-56）。三重问题：(1) 未认证的攻击者可提交任意 auth 猜测代理口令，命中即回显对应用户名——口令爆破 + 用户枚举预言机；(2) `==` 是非常量时间比较，存在计时侧信道；(3) 无速率限制。代理口令若较短/可预测，可被离线或在线爆破。建议限定回调来源（本机/单独内网监听）并加限速。
- **证据**：for name, passwd := range ul.ListUsersWithPasswd() { if passwd == parasMap["auth"] || base64...==parasMap["auth"] { c.JSON(200, {ok:true, id:name}) } } — public 路由，非常量比较，无限速
- **核实**：AuthHysteria2 注册在 public 组(init.go:27)，无任何调用方鉴权且监听在对外 HTTP 端口。handlerFunc(50-58) 遍历 ListUsersWithPasswd()，用 passwd==parasMap["auth"] 及其 base64 变体逐一比对，命中即返回 {ok:true,id:name} 回显用户名。构成未认证口令爆破+用户枚举预言机；== 为非常量时间比较存在计时侧信道；无任何限速。虽标记 protocolRelated，但该漏洞的成立(公共路由/明文比较/回显用户名/无限速)完全由仓库内代码证实，不依赖对 hysteria2 上游行为的记忆断言。P2 合理。

### 7. [P2·confirmed] 无可用节点时 /sub 返回 200 空订阅而非报错：nil 守卫是死代码（GetNodesWithFilter 永不返回 nil）

- **位置**：`pkg/http/sub_handler.go:131` · 维度：正确性 · 单元：HTTP
- **问题**：行 131 `if nodes == nil { c.String(200, "no avaliable node"); return }`。但 GetTargetNodes → cluster.GetNodesWithFilter 始终返回 `[]*Node{}`（node_manager.go:110 初始化非 nil 切片，无匹配时返回空非 nil 切片），该守卫永不触发。因此当 target 无有效节点时，代码继续以 0 个节点 fan-out：succList/failedList 皆空，allURIs 为空，ConvertURIsWithOptions 对空列表返回结果，最终 c.String(200, result) 下发空/无效订阅。用户静默拿到空订阅而非明确错误。同样的死守卫存在于 get_certs_handler.go:22 与 metric_handler.go:36。其余写 handler 用的是 `len(nodes)==0` 才正确返回 502，本 handler 应统一改为 len 判断。
- **证据**：nodes := ...GetTargetNodes(...); if nodes == nil { return } — 但 GetNodesWithFilter: nodes := []*Node{}; ...; return nodes 永不为 nil；空节点时继续 fan-out 返回 200 空订阅
- **核实**：cluster/node_manager.go:110 GetNodesWithFilter 以 nodes:=[]*Node{} 初始化并始终 return 非 nil 切片，无匹配时返回空非 nil 切片。故 sub_handler.go:131 `if nodes == nil` 为死代码，永不触发。target 无有效节点时以 0 节点继续 fan-out：succList/allURIs 皆空，最终 c.String(200,result) 下发空/无效订阅而非报错。metric_handler.go:36 存在同样死守卫；写 handler 用 len(nodes)==0 才正确返回 502。问题真实，P2 合理。

### 8. [P2·confirmed] copyUser 对 RPC 结果做无 ok 校验的类型断言，nil 或异常类型直接 panic

- **位置**：`pkg/http/copy_user_between_nodes_handler.go:60` · 维度：错误处理 · 单元：HTTP
- **问题**：行 59-60 `users := succList[req.SrcNode]; for _, u := range users.([]*proto.User)`。succList[req.SrcNode] 是 interface{}，若源节点在 succList 中不存在对应 key（例如 RPC 既未计入 succ 也未计入 fail，或节点名 key 与 req.SrcNode 大小写/别名不一致），users 为 nil interface，`nil.([]*proto.User)` 触发 `interface conversion: interface {} is nil` panic；若类型不是 []*proto.User 同样 panic。前面仅用 `len(failedList) > 0` 拦截失败（行 52），无法覆盖“既不 succ 也不 fail”的情形。应改为 `users, ok := succList[req.SrcNode].([]*proto.User); if !ok { ... }`。gin 会 recover 但该请求 500 且日志噪声，且属可预期输入路径。
- **证据**：users := succList[req.SrcNode]; for _, u := range users.([]*proto.User) { ... } — 无 ok 校验，nil/异型 panic
- **核实**：end_node_rpc.go 中 succList/failedList 均以 n.Name(单个节点名)为 key(488/510)，而 copy_user_between_nodes_handler.go:59 用 req.SrcNode 取值。req.SrcNode 可为 "all" 或组/别名(GetTargetNodes 支持 all→多节点)，此时 succList[req.SrcNode] 为 nil interface，line 60 与 67 的 users.([]*proto.User) 触发 interface conversion nil panic；len(failedList)>0 无法覆盖既不 succ 也不 fail(如 line 475 !RegisteredRemote 被 continue 跳过)的情形。属 admin 路由但输入可预期可达，gin recover 后 500+panic 日志噪声。应改 ok 校验。P2 合理。

### 9. [P2·confirmed] nodeMetricDesc.Collect 忽略 Unmarshal 错误并用 MustNewConstMetric，畸形远端指标可 panic 掉 /api/metrics

- **位置**：`pkg/http/prometheus_desc/node_collector_desc.go:68` · 维度：资源 · 单元：HTTP
- **问题**：Collect 中 `pb.Unmarshal(snf, metricFaimily)` 的错误被丢弃（行 47），随后对反序列化出的 MetricFamily 逐条 `prometheus.MustNewConstMetric(desc, ..., labelsValues...)`（行 68）。MustNewConstMetric 在 label 基数与 desc 不匹配、或指标名/label 名非法时 panic。desc 的 label 名与值均来自远端 end 节点上报的 SerializedMetrics（GenerateLabel 基于 remote metric），一个被攻破或有 bug 的 end 节点（持 cluster token 即可上报）即可构造使 label 数量/名称非法的 MetricFamily，在 center 抓取时 panic。prometheus Gather 不保证 recover 该 panic，可导致抓取请求崩溃甚至进程退出。应改用非 Must 的 NewConstMetric 并处理 error + 校验 Unmarshal。
- **证据**：pb.Unmarshal(snf, metricFaimily) // err 忽略 (行47); prometheus.MustNewConstMetric(desc, GetPrometheusValueType(...), ..., labelsValues...) (行68) — 远端可控 label
- **核实**：核实成立，且实际后果比描述更严重。node_collector_desc.go:47 确实丢弃 pb.Unmarshal 错误；损坏 blob 或远端构造的非法 MetricFamily 使 NewDesc 记录 err（client_golang v1.20.5 desc.go:98 校验 IsValidMetricName，label 名同样校验），MustNewConstMetric 对含 err 的 desc panic（value.go:106）。已核实 v1.20.5 Registry.Gather 在 `go collectWorker()` goroutine 中调用 Collect 且全库无 recover，panic 直接导致进程退出，'不保证 recover' 的判断正确。攻击面真实：Update 经 GetNodeMetricType RPC 拉取各 end 节点响应，被攻破节点完全控制字节内容。一处细节不准：GenerateLabel 对 keys/values 成对生成，'label 基数与 desc 不匹配'这一路径不存在；但非法/重复 label 名、空指标名均可 panic，且 common.go:43 对 nil Name 直接解引用还有额外 panic 路径。

### 10. [P2·confirmed] nodeMetricDesc.Collect 忽略 pb.Unmarshal 错误，空/损坏指标 blob 触发 MustNewConstMetric panic 中断整个抓取

- **位置**：`pkg/http/prometheus_desc/node_collector_desc.go:47` · 维度：错误处理 · 单元：HTTP
- **问题**：Collect 反序列化各节点上报的 SerializedMetrics 时 `pb.Unmarshal(snf, metricFaimily)` 丢弃错误。若某节点传来损坏或空 blob，metricFaimily 保持零值，GetName() 返回空串，prometheus.NewDesc("", ...) 会构造带 err 的 Desc，随后 MustNewConstMetric(desc, ...) 因 desc.err!=nil 而 panic。该 panic 发生在 Registry.Gather 的 Collect 内，会中断本次 /api/metrics 抓取（net/http 层 recover 后返回 500）。即任一半可信 end 节点的一个坏指标即可让集群指标端点整体不可用。Update() 里 `nms, _ := v.(*proto.NodeMetrics)` 同样吞断言失败追加 nil 元素（虽 proto getter nil 安全）。应校验 Unmarshal 错误并跳过、对空 name 跳过。
- **证据**：line 47: `pb.Unmarshal(snf, metricFaimily)`（返回值被丢弃）→ 空 MetricFamily → line 59 NewDesc(exMetricFamily.GetName()=="", ...) → line 68 MustNewConstMetric 对含 err 的 desc panic。
- **核实**：与 idx 0 同根因，机制链核实成立：line 47 丢弃 Unmarshal 错误 → 空/损坏 blob 得到零值 MetricFamily → NewDesc("") 因 IsValidMetricName 失败置 d.err（desc.go:98-100）→ MustNewConstMetric panic（value.go:106-107 先查 desc.err）。但影响描述被低估：panic 不是发生在 HTTP handler goroutine，而是 Registry.Gather 派生的 `go collectWorker()` goroutine（registry.go:452-467），client_golang v1.20.5 无任何 recover，net/http 的 conn.serve recover 覆盖不到该 goroutine，结果是整个 center 进程崩溃而非返回 500。Update 里吞类型断言失败追加 nil 的描述也属实（line 86，proto getter nil 安全所以本身无害）。P2 维持（考虑到需半可信集群节点参与，未升 P1）。

### 11. [P2·confirmed] target=all 写操作中任一节点失败即返回 500，掩盖部分成功导致重试产生副作用

- **位置**：`pkg/http/user_handler.go:91` · 维度：错误处理 · 单元：HTTP
- **问题**：UserAddHandler 及一众写 handler 的聚合逻辑为 `if len(failedList) != 0 { jsonErr(500) }`，即使 succList 中部分节点已成功执行也整体报 500。对 target="all" 的跨节点写（AddUser/Update/DeleteInbound/FastAddInbound/Cert 等），当集群中一节点不可达而其余节点已落库时，调用方收到 500 会重试；对非幂等操作（AddUsers 在已成功节点上会返回 user exists、FastAddInbound 端口/tag 冲突）造成二次副作用或永远无法成功。缺少区分 "全失败" 与 "部分成功" 的响应（对比 RotateAllPorts 用 code=301 表达部分成功）。建议对部分成功返回 2xx+partial 结构或明确 207 语义。
- **证据**：user_handler.go:90-96：`_, failedList, _ := ...ReqToMultiEndNodeServer(...); if len(failedList)!=0 { jsonErr(c,500,errMsg) }` —— 未检查 succList 是否有成功节点即整体 500；FastAdd/Cert/Inbound 等 handler 同构。
- **核实**：核实成立。user_handler.go:90-96 确为 `_, failedList, _ := ...; if len(failedList)!=0 { jsonErr(c,500) }`，succList 被下划线丢弃，任一节点失败即整体 500；同构模式在 cert_handler.go:31、fastAddInbound_handler.go:303、bound_handler.go:38 逐一确认。target="all" 路径真实存在（http_server.go:102 对 "all" 返回全部节点，UserAdd req 含 Target 字段），ReqToMultiEndNodeServer 按节点独立执行故部分成功可发生。对比项也属实：rotate_all_ports.go:75 明确用 code=301 表达部分成功，证明仓库已有该约定但这批写 handler 未采用。重试导致 user exists/端口冲突的二次副作用推论合理。P2 恰当。

### 12. [P2·confirmed] /api/metrics 每次抓取对全集群做破坏性 DrainStats，多 scraper/多入口并存时互相清零流量增量

- **位置**：`pkg/http/prometheus_desc/v2raymg_traffic_desc.go:70` · 维度：正确性 · 单元：HTTP
- **问题**：trafficDesc.Update 通过 GetBandWidthStats RPC 扇出（默认 target=all），而 end 节点实现是 statsCollector.DrainStats()→GetAllDeltaTraffic(reset=true)（end_node_user.go:337-339, bandwidth_stats_collector.go:35），即读取即清零的增量语义。后果：(1) 集群多个节点都开 enable_prometheus 且 Prometheus 抓取每个节点的 /api/metrics 时，抓 A 节点会把 B/C 节点的 delta 全部清零，各端点互相偷增量，任一端点的 v2raymg_traffic 都系统性低估；(2) 管理员手工 curl /api/metrics 同样消耗增量；(3) 增量值以 GaugeValue+时间戳导出，数值含义依赖抓取间隔，无法用 rate()/increase() 正确聚合。修复：导出累计计数器（CounterValue）走非破坏性读取，或明确单 scraper 拓扑约束并在文档声明。
- **证据**：v2raymg_traffic_desc.go:69-90 Update 调 GetBandWidthStatsReqType 后整体替换 d.stats；rpc/server/end_node_user.go:335-341 `getBandWidthStatsRsp.Stats = s.statsCollector.DrainStats()`；bandwidth_stats_collector.go:9-10 注释「Each DrainStats() call returns delta since the last call and resets」。
- **核实**：证据链全部核实：v2raymg_traffic_desc.go:69-90 Update 经 GetBandWidthStatsReqType 扇出并整体替换 d.stats；metric_handler.go:66 经 getTargetFromQuery 取 target，target.go:17 确认默认值为 "all"（与 help 文本一致）；rpc/server/end_node_user.go:335-341 `getBandWidthStatsRsp.Stats = s.statsCollector.DrainStats()`；bandwidth_stats_collector.go:20-35 注释与实现均为「返回上次调用以来的 delta 并 reset」（GetAllDeltaTraffic(true)）。因此任一开启 enable_prometheus 的节点被抓取（默认 target=all）都会清零全集群所有节点的增量，多 scraper/手工 curl 互相偷增量；且 delta 以 GaugeValue+时间戳导出，rate()/increase() 语义不成立。GetBandWidthStats 在 onlyGatewayMethods 列表中只是 OnlyGateway 模式下的白名单（end_node_server.go:148），不限制调用方，不排除该路径。P2 恰当。

### 13. [P2·confirmed] HttpServer.Start 忽略 gin Run 的返回错误，端口被占用时管理 API 静默失效

- **位置**：`pkg/http/http_server.go:94` · 维度：错误处理 · 单元：HTTP
- **问题**：`s.RestfulServer.Run(...)` 的 error 返回值被丢弃，而 cmd/server.go:257 以 `go httpServer.Start()` 启动。端口冲突、监听地址非法等启动失败只会让 goroutine 悄悄退出：进程继续运行、日志只有一条 "http server starting"，运维要等到调 API 连接被拒才发现管理面已死。end 节点的 /sub 订阅分发、登录、全部管理操作都在这个服务上。修复：捕获 Run 错误并 log.Error + 进程级失败（或至少显式告警）。
- **证据**：http_server.go:92-95 `func (s *HttpServer) Start() { log.Info("http server starting", ...); s.RestfulServer.Run(fmt.Sprintf("%s:%d", s.Host, s.Port)) }` —— error 未接收；cmd/server.go:257 `go httpServer.Start()`。
- **核实**：http_server.go:92-95 确认 Start() 中 `s.RestfulServer.Run(...)` 返回的 error 未接收，唯一输出是启动前的一条 Info 日志；cmd/server.go:257 确认以 `go httpServer.Start()` 启动，Run 失败（端口占用、地址非法）只会让该 goroutine 静默退出，进程继续运行，管理 API（含 /sub、登录、全部管理操作）静默失效。P2 恰当。

### 14. [P2·confirmed] /api/metrics 接口契约三处不一致：help 声称支持 JWT 但实现仅 X-Token、http-api-reference.md 完全未收录、README 声称 /api 下统一 JWT/X-Token

- **位置**：`pkg/http/metric_handler.go:87` · 维度：架构 · 单元：HTTP
- **问题**：metric_handler.go help() 写「需要认证 (X-Token 或 JWT)」，README.md:57 说「All other routes are under /api and require JWT or X-Token」且 :128 列出 /api/metrics；但实现只挂 getAuthHandlerFunc（仅 X-Token），持 admin JWT 的调用方会得到 401 "invalide token"。同时 docs/http-api-reference.md（1278 行、自称 v4 全量端点表）没有 /api/metrics 任何条目，端点表与实际路由不一致。这是本层「与 docs/README API 列表一致性」的直接违反，也让双认证栈（auth/middleware.go vs auth_with_token_handler.go）继续并存。修复：/api/metrics 换用 AuthMiddleware（同时消除空 token 放行缺陷），并补文档。
- **证据**：metric_handler.go:83-88 help 文本「需要认证 (X-Token 或 JWT)」；:72-77 getHandlers 仅 getAuthHandlerFunc + prometheusHandler；grep docs/http-api-reference.md 'metrics|prometheus' 无端点条目；README.md:57,128。
- **核实**：亲自核实成立。/api/metrics 经 cmd/server.go:255 RegisterPrometheus → http_server.go:117 RegisterHandler 直接挂在 gin engine 根上，不在 init.go:30/42 带 auth.AuthMiddleware 的 /api group 内；handler 链仅 getAuthHandlerFunc（X-Token 精确比较，auth_with_token_handler.go:11）+ prometheusHandler，持 admin JWT 请求确实会 401 "invalide token"。help()（metric_handler.go:87）却写「需要认证 (X-Token 或 JWT)」。grep docs/http-api-reference.md 仅 :310 一处无关 Prometheus 提及，无 /api/metrics 端点条目。README.md:57 声称其余 /api 路由需 JWT/X-Token，:128 又把 GET /api/metrics 列在「Public (No Auth)」表中——README 自身还内部矛盾，实际不一致比 finding 描述更严重。P2 恰当。

### 15. [P2·confirmed] 错误响应契约混杂：部分 handler 用 HTTP 200 + body code=300/404 表达失败，与 jsonErr 5xx 系及文档约定冲突

- **位置**：`pkg/http/set_user_bandwidth_handler.go:67` · 维度：架构 · 单元：HTTP
- **问题**：同类失败在不同 handler 返回完全不同的线协议：SetUserBandwidth/SetUserClientLimit 对「no available node」返回 HTTP 200 + {code:404}（set_user_bandwidth_handler.go:67, set_user_client_limit_handler.go:69），SetUserRole 返回 200+{code:404}（set_user_role_handler.go:42），ChangePassword/ResetAuthToken 失败返回 200+{code:300}（change_password_handler.go:51,59; user_handler.go:336,348），而 UserAdd/Delete/Cert 等返回真实 502/500（jsonErr）。docs/http-api-reference.md:1235 声明统一错误格式 `{"code": <http_status>, ...}`，200+code300 系列直接违反。对不解析 body 的客户端/前端拦截器，失败被当成功，尤其 ChangePassword「更新失败但返回 200」可能让用户误以为密码已改。修复：统一走 jsonErr 语义，HTTP 状态码与 body code 对齐。
- **证据**：set_user_bandwidth_handler.go:66-69 `if len(nodes) == 0 { c.JSON(200, gin.H{"code": 404, ...}) }`；change_password_handler.go:57-60 `c.JSON(200, gin.H{"code": 300, "msg": joinFailedList(failedList)})`；对照 user_handler.go:85-87 `jsonErr(c, 502, ...)`；docs/http-api-reference.md:1235。
- **核实**：所有引用点逐一核实：set_user_bandwidth_handler.go:66-68 与 set_user_client_limit_handler.go（no available node）返回 200+{code:404}；set_user_role_handler.go 同模式 200+{code:404}；change_password_handler.go 两处（no available node、全部节点失败）返回 200+{code:300}；user_handler.go ResetAuthToken 两处 200+{code:300}。对照 user_handler.go:86 UserAdd 用 jsonErr(c,502,"no available node")——同一失败原因两种线协议。docs/http-api-reference.md:~1235 明确声明错误格式 {"code": <http_status>}，200+code300 直接违反。ChangePassword 失败返回 HTTP 200 对不解析 body 的客户端确实会被当成功。P2 恰当。

### 16. [P2·confirmed] 优雅关闭缺失：SIGTERM 时不做最终流量落盘（StopTrafficStats/ForceCollect 存在但未接线），每次重启平均丢失约半个采集周期的计费流量

- **位置**：`cmd/server.go:271` · 维度：正确性 · 单元：CMD
- **问题**：runEndNode 收到信号后只执行 `cancel()` + `containerMgr.StopAll()` 就返回（cmd/server.go:271-275）。流量统计由 StartTrafficStats(0) 以默认 1 分钟 tick 采集并在 onCollect 回调里 store.Save 持久化，UserManager 提供了 StopTrafficStats() 和 statsCollector.ForceCollect() 但本层都没有调用——自上次 tick 以来的用户流量增量在进程退出时直接丢失（架构约束 5：流量统计只信 forward 层 relay 计数，这是唯一计费数据源，每次发版/重启都系统性少计）。此外 deferred `storeMgr.Close()` 在 statsCollector goroutine 仍在运行时关闭 DB，最后一个 tick 的 `_ = m.store.Save(u)` 会静默失败；HTTP/gRPC server 也没有 GracefulStop，正在处理的管理请求被硬切。修复方向：信号后先 StopTrafficStats + ForceCollect（或增加显式 FlushTraffic），再 StopAll，最后关 store。
- **证据**：cmd/server.go:271-275 `sig := <-sigCh ... cancel(); containerMgr.StopAll()` 后直接返回；pkg/proxy/usermanager/usermanager.go:2092-2095 注释 'interval specifies the collection interval (default 1 minute if 0)'、:2163-2168 StopTrafficStats 与 :1991 ForceCollect 在 cmd 层无任何调用点（grep cmd/ 无引用）；onCollect 内 `_ = m.store.Save(u)` 错误被吞。
- **核实**：事实全部核实：cmd/server.go:271-275 信号处理只有 cancel()+containerMgr.StopAll()，grep 确认 cmd 层无 StopTrafficStats/ForceCollect/ForceTrafficStatsCollection 调用点；usermanager.go:2164 StopTrafficStats 与 :1991 ForceCollect 存在但未接线；onCollect 回调内 `_ = m.store.Save(u)` 吞错；deferred storeMgr.Close() 在 collector 仍运行时关库。但丢失量有硬上界：cmd 传 0 → 默认 1 分钟 tick，每次重启最多丢约 1 分钟（平均半分钟）流量增量，且仅发生于重启/发版时刻，非持续性少计。相对 P1（重大计费/可用性影响）量级偏小，建议降为 P2。

### 17. [P2·confirmed] 启动校验未强制 cluster.token 非空/最小强度，空或弱 token 配置直接落入下层已确认的 RPC 常量密钥缺陷，本层是唯一 fail-fast 拦截点

- **位置**：`cmd/server.go:84` · 维度：安全 · 单元：CMD
- **问题**：startServer 在启动子系统前调用 appconfig.Validate（cmd/server.go:84）作为 fail-fast 关口，但 Validate 对 end_node.cluster.token 没有任何检查；cluster.NewEndNodeClusterManagerFromConfig 也无条件接受空 token（end_node_init.go:44 直接赋值）。下层已确认（L4 digest，不重复计分）：encrypt codec 的密钥直接从 token 派生且无 KDF，空 token 产生公开常量密钥、短 token 低熵——即整个集群 gRPC 的 AES-GCM 加密和双向鉴权（架构约束 8）在默认/空配置下形同虚设，且 EndNodeServer.Start 里 `encoding.RegisterCodec(rpc.NewEncryptMessageCodec(s.cfg.Cluster.Token))` 照常注册。装配层应在 Validate 或 runEndNode 中要求：启用集群（配置了 static_nodes/center_node）时 token 必须非空且达到最小长度（如 >= 16 字节），否则拒绝启动。
- **证据**：cmd/server.go:84 `appconfig.Validate(cfg)`；pkg/proxy/appconfig/loader.go:247-284 Validate 全文只校验 store.dsn/forward 端口/email/ping source/容器 enabled，无 token 检查；pkg/cluster/end_node_init.go:44 `mgr.Token = clusterCfg.ClusterToken` 空值直通；下层已确认 pkg/common/rpc/encrypt_codec.go:46 无 KDF 弱密钥（L4 P1）。
- **核实**：仓库内证据链完整：loader.go Validate 全文无任何 cluster.token 检查；pkg/cluster/end_node_init.go:44 `mgr.Token = clusterCfg.ClusterToken` 空值直通无校验；pkg/common/rpc/encrypt_codec.go:44-53 GetRpcKeyByToken 无 KDF，短 token 用 PKCS7Padding 补齐、空 token 产生完全确定的公开常量密钥，代码注释自己承认『如果密码为空,则同样不具有安全性』；end_node_server.go:225 无条件 RegisterCodec。装配层作为唯一 fail-fast 拦截点缺失校验属实，P2（本层为缺失防御、根因已在下层计分）恰当。

### 18. [P2·confirmed] node groups 播种把 List 查询错误与空结果混同：DB 瞬时错误时会 DELETE 全表并只写回 default group，覆盖已配置的节点分组

- **位置**：`cmd/server.go:217` · 维度：正确性 · 单元：CMD
- **问题**：runEndNode 的 cluster sync 分支写作 `if groups, _ := ngStore.List(); len(groups) == 0 { _ = ngStore.Set([]string{cfg.ClusterUser.DefaultGroup}) }`（cmd/server.go:217-219）。List 返回 error 时 groups 为 nil，len==0 条件成立，于是走 Set——而 SQLiteNodeGroupsStore.Set 的语义是先 `DELETE FROM local_node_groups` 再插入（node_groups_store.go:71），即一次 SQLite 瞬时故障（锁冲突/IO 错误）就会把节点已有的多个 group 配置擦成只剩 default group，随后集群按 group 同步用户的行为全部漂移。Set 的返回错误也被 `_ =` 吞掉，失败无任何日志。应当区分 err != nil（记日志、跳过播种）与真正的空表，且 Set 失败至少要 log.Error。
- **证据**：cmd/server.go:217-219 `if groups, _ := ngStore.List(); len(groups) == 0 { _ = ngStore.Set([]string{cfg.ClusterUser.DefaultGroup}) }`；pkg/store/node_groups_store.go:34-52 List 出错返回 (nil, err)，:70-77 Set 在事务内 `DELETE FROM local_node_groups` 后重建。
- **核实**：代码核实属实：cmd/server.go:217-219 `if groups, _ := ngStore.List(); len(groups) == 0` 将 List 的 error（返回 nil slice，len==0 成立）与真空表混同，随后 Set 被 `_ =` 吞错；pkg/store/node_groups_store.go:34-53 List 出错返回 (nil, err)，:57-89 Set 在事务内先 DELETE FROM local_node_groups 再重建。缓解因素是 Set 本身在事务中，若 DB 故障持续则 Set 回滚不丢数据，但 List 瞬时失败（锁冲突）后 Set 成功的窗口真实存在，一旦命中即擦除已配置分组且无日志。P2 可接受。

### 19. [P2·confirmed] 约 30 个 CLI API 封装完全不检查 HTTP 状态码，401/500 的错误响应体被当成功结果打印，DeleteUser 等对失败操作报 succ

- **位置**：`cmd/cli/client/http_client.go:362` · 维度：错误处理 · 单元：CMD
- **问题**：http_client.go 中只有 Login（:22 检查 200）、ListNode（:70）和 ListCert（:95 用 unexpectedHTTPStatus）校验状态码；其余 SetGatewayModel/ApplyCert/FastAddInbound/AddUser/UpdateUser/DeleteUser/ResetUser/AddInBound/UpdateProxy/Rotate*/SetUser*/Logout/TransferCert 等约 30 个封装的回调都是『读 body → 存入 result → return nil』，任何非 2xx 响应（token 过期 401、admin-only 403、服务端 500）都以 err==nil 返回。后果直接可见：command.go 的 deleteUser 循环里对每个用户打印 `delete user[%s] succ: Unauthorized`；addUser 打印 `add user[x] with result: <错误文本>`；logout 在 401 时也会清空 JWT 缓存并显示服务端错误文本为『结果』。所有操作型命令的失败都被伪装成成功，操作员无法信任 CLI 输出。应统一在 doJSONRequest/回调层做 `resp.StatusCode >= 300 → unexpectedHTTPStatus` 处理（该 helper 已存在只是没人用）。
- **证据**：cmd/cli/client/http_client.go 全文 grep StatusCode 只命中 :22（Login）、:70（ListNode）、:95（ListCert）三处；对比 :362-380 DeleteUser 的 cb 只有 readBody+result 赋值；cmd/cli/command.go:545-556 deleteUser 对 err==nil 一律打印 `delete user[%s] succ`。
- **核实**：核实属实：cmd/cli/client/http_client.go 全文 StatusCode 检查仅三处（Login :22、ListNode :70、ListCert :95-96），unexpectedHTTPStatus helper（:54）无其他调用者；base_client.go doJSONRequest/DoGetRequest 均不检查状态码直接调回调；DeleteUser（:362 起）等其余封装的回调只做 readBody→result→return nil，401/403/500 均以 err==nil 返回错误响应体。command.go:552 对 err==nil 一律打印 `delete user[%s] succ: %v`，:530 addUser 同样把错误文本当结果打印。失败被伪装成成功属实，P2 恰当。DeleteUser 函数实际起始于 :362，:364 为其回调内部行，偏差可忽略。

### 20. [P2·confirmed] CLI 把管理员密码/静态 token 明文写入 0666 世界可读可写的配置文件，且每次启动无条件重写

- **位置**：`cmd/cli/config.go:55` · 维度：安全 · 单元：CMD
- **问题**：LoadConfig 结尾 `return os.WriteFile(config, d, 0666)`（cmd/cli/config.go:55）：.v2raymg-tools.yaml 含集群管理入口 host、静态管理 token、用户名和明文密码，以 0666 权限落盘（umask 前世界可读可写）。同机低权限用户可以直接读走管理员凭据；因为文件还可写，攻击者也可改写 host 字段把 CLI 指向自己控制的服务器，下次运维执行任意命令时 getAuthToken 会把 username/password POST 到攻击者的 /api/login 完成凭据收割。另外 inputConfig（:58-69）用 fmt.Scanln 明文回显密码，且即使文件读取成功也会在每次启动重写文件。至少应改为 0600、密码输入用 term.ReadPassword、只在内容变化时写回。
- **证据**：cmd/cli/config.go:55 `return os.WriteFile(config, d, 0666)`；:16-21 config 结构含 Token/Username/Password 明文字段；:103-125 getAuthToken 会用文件中的 host+username+password 发起 Login。
- **核实**：核实属实：cmd/cli/config.go:55 `os.WriteFile(config, d, 0666)`，且 LoadConfig 在文件读取成功时也无条件重写（:46-55）；:16-21 config 结构含明文 Token/Username/Password；:58-68 inputConfig 用 fmt.Scanln 明文回显密码；:103-125 getAuthToken 会用文件中的 host+username+password 发起 Login，host 被篡改即可收割凭据。注意 os.WriteFile 受 umask 约束，典型环境实际落盘 0644（世界可读、非世界可写），『可写』攻击面依赖 umask=0 的少见配置，但世界可读已足以泄露管理员明文密码，P2 恰当。

### 21. [P2·confirmed] --migrate 对 .json 配置执行时用 yaml.Marshal 写回 JSON 文件，直接破坏配置导致后续无法启动

- **位置**：`cmd/server.go:75` · 维度：正确性 · 单元：CMD
- **问题**：startServer 的 --migrate 分支对任意扩展名的配置都调用 appconfig.SaveToFile（cmd/server.go:75），而 SaveToFile 无条件 `yaml.Marshal` 后写回原路径（loader.go:222-226）。LoadFromFile 是按扩展名选择解码器的：对 config.json 执行 `v2raymg server --conf config.json --migrate` 会把 YAML 文本写进 .json 文件，下次正常启动走 `case ".json": json.Unmarshal(yaml 字节)` 必然失败，服务器无法启动且用户的原始配置已被覆盖（无备份）。--migrate 本身还有意跳过 Validate，用户不会得到任何预警。应在 migrate 分支限制扩展名为 yaml/yml，或让 SaveToFile 按扩展名序列化，且写回前先备份原文件。
- **证据**：cmd/server.go:74-81 migrate 分支直接 `appconfig.SaveToFile(cfg, serverConfig)`；pkg/proxy/appconfig/loader.go:221-229 SaveToFile 固定 `yaml.Marshal`；loader.go LoadFromFile 中 `case ".json": if err := json.Unmarshal(data, cfg)` 将对 YAML 内容报 decode 错误。
- **核实**：亲读 cmd/server.go:74-81：migrate 分支对任意扩展名配置无条件调用 appconfig.SaveToFile(cfg, serverConfig) 且无备份。pkg/proxy/appconfig/loader.go:220-230 SaveToFile 固定 yaml.Marshal 后 os.WriteFile 覆盖原路径；loader.go:175-186 LoadFromFile 按扩展名分派，case ".json" 走 json.Unmarshal。对 config.json 执行 --migrate 会把 YAML 字节写入 .json 文件，下次启动 json.Unmarshal 必然失败且原配置已被覆盖。migrate 分支还明确跳过 Validate（代码注释自证）。触发路径真实、无任何扩展名守卫。P2 恰当（配置数据丢失但需用户主动执行 --migrate 且配置为 .json）。

### 22. [P2·confirmed] getAuthToken 对任何登录失败静默回退静态 token（可能为空串），密码错误永远不被提示

- **位置**：`cmd/cli/config.go:116` · 维度：错误处理 · 单元：CMD
- **问题**：getAuthToken 在 client.Login 出错时不区分错误类型直接 `return globalConfig.Token`：密码错误(401)、服务不可达、超时都被吞掉。若用户只配置了 username/password（Token 为空，这正是 inputConfig 引导的路径：输入了 token 就不再问用户名密码，反之 Token==""），回退结果是空串，setAuthHeader 对空 token 不设任何鉴权头（base_client.go:19-25），请求裸发到服务端得到 401；叠加封装函数不检查状态码的问题，用户看到的是 "succ: Unauthorized" 或 unmarshal 报错，从头到尾没有任何 "用户名或密码错误" 的提示。且失败不缓存，之后每条命令都重复一次最长 30 秒的登录尝试（unreachable host 时每条命令卡 60 秒：登录 30s + 业务请求 30s，期间还持有 jwtCache.mu）。登录失败应把 err 向上传播或至少打印告警，并区分 "凭证错误" 与 "网络错误"。
- **证据**：config.go:116-120 `token, expire, err := client.Login(...); if err != nil { // Login failed — fall back to static token.\n return globalConfig.Token }`；config.go:63-68 inputConfig 中 token 与 username/password 互斥输入；base_client.go:19-25 空 token 不设任何 header
- **核实**：config.go:116-120 核实属实：client.Login 任何错误（401、超时、拒连）均不区分直接 `return globalConfig.Token`，无日志无告警。inputConfig（config.go:61-68）确认 token 与 username/password 互斥输入路径，走用户名密码路径时 Token==""。base_client.go:19-25 确认 setAuthHeader 对空 token 既不设 Authorization 也不设 X-Token，请求裸发必得 401；叠加 idx 2 的状态码不检查问题，'succ: Unauthorized' 链路完整可达。失败不写 jwtCache（122-123 仅成功时写），每条命令重复登录尝试属实；jwtCache.mu 在 defer Unlock 下于整个 Login 期间持有（:108-109 与 :116）也属实。每业务调用最长 30s（Login）+30s（业务请求，base_client.go Timeout 30s）成立。P2 恰当。

### 23. [P2·confirmed] Profile 命令请求 GET /api/user 而非 /api/profile，管理员执行 Profile 会拿到全集群用户列表（含全部 auth_token）而非个人信息

- **位置**：`cmd/cli/client/http_client.go:743` · 维度：正确性 · 单元：CMD
- **问题**：client.Profile 用 common.User("api/user") 拼 URL，但服务端 GET /api/user（UserListHandler）按角色分流：admin（含 X-Token 持有者——AuthMiddleware 将其直接置为 admin）走 serveAdmin 返回所有节点全部用户及其 auth_token；只有 normal 角色才返回本人 profile。服务端已注册专门的 GET /api/profile（ProfileHandler，不区分角色始终返回本人完整 profile），且 common/const.go 中只有 ChangePassword="api/profile/password" 而缺少 api/profile 常量——CLI 的 Profile 语义（"prints the current user's profile"）对最常见的 admin 使用场景完全失效，还顺带把全部用户凭证刷屏输出。应改为请求 api/profile。
- **证据**：http_client.go:731-744 注释 "Profile returns the current user's profile from GET /api/user" 且 `reqUrl := fmt.Sprintf("%s/%s", host, common.User)`；pkg/http/user_handler.go:375-381 `if role == "admin" { handler.serveAdmin(c) }`（serveAdmin 返回含 auth_token 的全量用户）；pkg/http/init.go:33 已注册 `&ProfileHandler{}`（路径 /api/profile）
- **核实**：全链路核实成立：http_client.go:743 `reqUrl := fmt.Sprintf("%s/%s", host, common.User)`，common.User="api/user"（const.go:17），且 const.go 确无 api/profile 常量（仅 ChangePassword="api/profile/password"）。服务端 user_handler.go:376-382 GET /api/user 按 role 分流，role=="admin" 走 serveAdmin（:386-422）经 GetUsersReq{IncludeAll:true} 返回全部节点全部用户且响应体包含 auth_token（:406）；AuthMiddleware（middleware.go:24-27）确认 X-Token 持有者直接置 role=admin。init.go:33 确认 ProfileHandler 已注册于 userGroup（路径 /api/profile，user_handler.go:514），其 handlerFunc 不分角色走 serveUserProfile。即 CLI Profile 命令对 admin JWT/X-Token 用户返回的是全集群用户凭证清单而非个人 profile，与函数文档语义相悖。注意 X-Token 场景下 ContextKeyUsername 未设置，即使改走 serveNormal 也会 401——但这不影响本条结论（应改请求 api/profile）。P2 恰当：调用方本就是 admin，属功能语义错误而非越权。

### 24. [P2·confirmed] REPL 启动前串行预热三张缓存，目标不可达时启动阻塞可达数分钟且失败被静默丢弃

- **位置**：`cmd/cli/command.go:18` · 维度：错误处理 · 单元：CMD
- **问题**：InitPromptAndRegister 在注册 handler 前同步执行 updateLocalNodeList/updateLocalUserList/updateLocalInboundList，每个都要先走 getAuthToken（配置了用户名密码时含一次最长 30s 的 Login）再发 30s 超时的业务请求。host 配错或节点宕机时，用户敲下 `v2raymg cli` 后终端无任何输出地卡最长约 3 分钟才出现提示符；三处错误全部用 `_` 丢弃（info.go:31/41/47-49），用户不知道自动补全数据为空的原因。且这是三张缓存唯一的刷新时机（info.go:26 定义的 updateCycle=5s 在全仓库无引用，周期刷新从未接线），REPL 长会话中节点/用户/inbound 变更后补全与 fastAddInbound 的 getNode(target) 域名回填全部基于陈旧数据。建议：预热并行化+失败打印告警，并把 updateCycle 接线或删除。
- **证据**：command.go:18-20 `updateLocalNodeList(); updateLocalUserList(); updateLocalInboundList()`（prompt.Run 之前同步执行）；info.go:31 `localNodeList, _ = client.ListNode(getHost(), getAuthToken())`；info.go:26 `var updateCycle = 5 * time.Second`（grep 全仓库仅此一处出现）；base_client.go:29 Timeout 30s
- **核实**：核实成立：cmd/cli.go:24-25 确认 InitPromptAndRegister（内含 command.go:18-20 三个串行预热）在 prompt.Run() 之前同步执行，期间终端无输出。info.go:31/41 用 `_` 丢弃 ListNode/ListUser 错误，47-49 对 ListInboundsStructured 错误静默 return，三处失败均无提示属实。每个预热先走 getAuthToken（配置用户名密码时含最长 30s 的 Login，config.go:116 + base_client 30s Timeout），再发 30s 超时业务请求；登录失败不缓存，三次预热各自重试登录，最坏 3×(30+30)s≈3 分钟阻塞成立（'最长'表述准确；拒连场景会快速失败，但黑洞/丢包场景确实撞满超时）。updateCycle 无引用已在 idx 0 核实。与 idx 0 部分重叠但侧重点不同（启动阻塞+静默失败 vs 缓存陈旧），P2 合理。

### 25. [P2·confirmed] 关停序列只停容器：traffic-stats/maintenance 循环未停止就解构 Store，最后一个采样窗口的流量丢失且存在对已关闭 DB 的写入竞态

- **位置**：`cmd/server.go:274` · 维度：资源 · 单元：CMD
- **问题**：收到 SIGTERM 后 runEndNode 只做 cancel() 和 containerMgr.StopAll()，随后函数返回触发 deferred forwardMgr.Close() 与 storeMgr.Close()。userMgr.StartTrafficStats(0)/StartMaintenance(0)（server.go:260-261）启动的后台循环从未被停止——usermanager 提供了 StopTrafficStats（usermanager.go:2163）但装配层不调用；rpcServer/httpServer 也没有 GracefulStop/Shutdown。后果：(1) 自上次统计 tick 以来累计的用户流量增量未落库即丢失（对按量计费/限额场景是真实数据损失）；(2) 统计/维护 goroutine 在 storeMgr.Close() 之后仍可能触发 DB 写，与关闭中的 SQLite 连接竞态，关停期产生噪音错误甚至挂起。应在 StopAll 前依次 StopTrafficStats + 最终 flush、停 maintenance、GracefulStop 两个 server。
- **证据**：server.go:271-276 `sig := <-sigCh; ...; cancel(); containerMgr.StopAll()`（无 StopTrafficStats/rpc/http 停止调用）+ server.go:112 `defer storeMgr.Close()`；server.go:260-261 `userMgr.StartTrafficStats(0); userMgr.StartMaintenance(0)`；pkg/proxy/usermanager/usermanager.go:2163-2164 存在未被调用的 StopTrafficStats
- **核实**：server.go:260-261 确实调用 StartTrafficStats(0)/StartMaintenance(0)；:271-275 关停路径仅 cancel()+containerMgr.StopAll()。StartTrafficStats（usermanager.go:2095-2146）不接收 ctx，statsCollector 用自己的 stopCh（Start/Stop 在 :1788/:1807），cancel() 对其无效；StartMaintenance（:2150-2161）的 goroutine 是 `for range ticker.C` 无任何退出通道，根本无法停止。StopTrafficStats 存在于 usermanager.go:2164 但 grep 全 cmd/ 无任何调用者；rpcServer/httpServer 也无 GracefulStop/Shutdown 调用。流量落库仅在每个 collect tick 的 onCollect 回调里发生（:2139-2141 m.store.Save），无最终 flush，故最后一个采样窗口（默认最长 1 分钟）的增量确实丢失；且 runEndNode 返回后 deferred storeMgr.Close() 与仍在运行的 stats goroutine 的 store.Save 存在真实竞态窗口。P2 恰当。

### 26. [P3·confirmed] metrics 抓取丢弃 collector Update 错误，固定 1s 超时下静默下发陈旧/缺失指标

- **位置**：`pkg/http/metric_handler.go:46` · 维度：错误处理 · 单元：HTTP
- **问题**：prometheusHandler 每次抓取起 3 个 goroutine 调用 trafficDesc/pingDesc/nodeMetricDesc.Update（行 46-57），三者返回的 error 全部被忽略（Update 内部亦仅取 succList，丢 failedList/err，见 node_collector_desc.go:79）。共享 1s 超时（行 42），任一节点 RPC 慢/失败时，对应 collector 保留上一次缓存切片，Collect 时照常输出旧值，且无任何指标标注新鲜度或失败节点。监控消费者会把陈旧数据当作当前值（例如流量停止增长被误读为无流量），运维无法从 /metrics 侧感知采集失败。建议至少暴露 scrape_error / 采集时间戳指标。
- **证据**：go func(){ defer wg.Done(); trafficDesc.Update(ctx, rpcClient, token) }() ... — 三处 Update 返回值全丢；ctx 为 1s 超时；Collect 输出缓存快照无失败标记
- **核实**：核心成立但机制描述有一处错误。metric_handler.go:42 确为 1s 共享超时，46-57 三个 goroutine 的 Update 返回值全部丢弃，Update 内部也只取 succList（三个 desc 均如此），无 scrape_error/时间戳指标，采集失败对 /metrics 消费者完全不可见——这部分确认。但'保留上一次缓存切片、输出旧值/陈旧数据'是错的：三个 Update 都用本轮 succList 结果整体覆盖缓存（v2raymg_traffic_desc.go:89、ping_desc.go:163、node_collector_desc.go:93），失败节点的指标是静默消失而非陈旧。表现为时间序列断点/归零而非停滞，运维仍无法区分'节点无数据'与'采集失败'，P3 恰当。

### 27. [P3·confirmed] 写 handler 的 HTTP 错误契约不一致：部分故障返回 200+code:300，部分返回真实 5xx/502

- **位置**：`pkg/http/change_password_handler.go:51` · 维度：架构 · 单元：HTTP
- **问题**：同为写操作，错误响应约定分裂：ChangePassword 无可用节点返回 `c.JSON(200, {code:300})`（行 51）、更新失败同样 200+code:300（行 59）；UserResetAuthToken 同样 200+code:300（user_handler.go:336/348）；SetUserRole 无节点返回 `c.JSON(200, {code:404})`（set_user_role_handler.go:42）；SetUserBandwidth/ClientLimit 无节点返回 `c.JSON(200,{code:404})`。而 UserAdd/Update/Delete/Cert/Inbound 等用 jsonErr 返回真实 502/500。客户端无法用 HTTP 状态码统一判断成败，必须逐 endpoint 解析 body 内 code，易漏判失败为成功。建议统一错误码语义（HTTP 状态码与 body code 对齐）。
- **证据**：ChangePassword: c.JSON(200, gin.H{"code": 300, "msg": "no available node"}) (行51) vs UserAdd: jsonErr(c, 502, "no available node") — 同类写操作两种错误契约
- **核实**：逐处核实属实：change_password_handler.go:51 与 59 返回 200+code:300（59 行仅在全部节点失败时触发）；user_handler.go:336/348 同为 200+code:300；set_user_role_handler.go:42、set_user_bandwidth_handler.go:67、set_user_client_limit_handler.go:69 返回 200+code:404；而 user_handler.go:86 用 jsonErr(c,502,...)，common.go:14 确认 jsonErr 写真实 HTTP 状态码。同类写操作三种错误契约并存，客户端按 HTTP 状态码判断会把失败当成功。P3 恰当。注意与 idx 5 为同一问题的重复项。

### 28. [P3·confirmed] 写 handler 错误响应契约不一致：部分返回 200+code:300/404，部分返回真实 5xx

- **位置**：`pkg/http/change_password_handler.go:59` · 维度：架构 · 单元：HTTP
- **问题**：同为写操作，错误响应码约定分裂：ChangePassword（line 51、59）和 ResetAuthToken（user_handler.go:336、348）在无节点/RPC 失败时返回 HTTP 200 且 body code=300；SetUserRole（set_user_role_handler.go:42）、SetUserBandwidth（65-69）、SetUserClientLimit（68）在无节点时返回 HTTP 200 code=404；而 UserAdd/Update/Delete/Cert/Inbound 等用真实的 502/500。前端/网关按 HTTP 状态码判断成败时会把这些 200 当成功。建议统一错误映射（要么全走 HTTP 状态码，要么全走 body code）。
- **证据**：change_password_handler.go:51 `c.JSON(200, gin.H{"code":300,...})` 与 user_handler.go:86 `jsonErr(c,502,...)` 对同类失败给出不同 HTTP 语义；set_user_role_handler.go:42 又用 `c.JSON(200, {code:404})`。
- **核实**：与 idx 2 为同一问题（重复项），证据逐处核实属实：change_password_handler.go:51/59 返回 200+code:300（59 行条件为 failedList 非空且 succList 为空，即全失败仍 200）；user_handler.go:336/348 同；set_user_role_handler.go:42 为 200+code:404；set_user_bandwidth_handler.go 实际在 67 行（finding 写 65-69 区间，可接受）；set_user_client_limit_handler.go 实际在 69 行（finding 写 68，偏差 1 行）；user_handler.go:86 的 jsonErr(c,502) 经 common.go:14 确认写真实 HTTP 状态。契约分裂确凿，P3 恰当。

### 29. [P3·confirmed] log.Error 被当作 printf 使用，格式串未展开生成乱码日志

- **位置**：`pkg/http/get_certs_handler.go:36` · 维度：错误处理 · 单元：HTTP
- **问题**：log.Error 是结构化日志（func Error(msg string, args ...any)，args 为 key-value 对），但这里传入 `log.Error("Err=%s|Target=%s", errMsg, target)`。结果是 msg 原样保留 "Err=%s|Target=%s"，errMsg/target 被当成一对 key/value 附加，%s 占位符不会替换，运维排障时日志无法阅读。应改用 log.Errorf 或改成结构化 key-value（同仓库已有 Errorf 变体）。
- **证据**：get_certs_handler.go:36-40 `log.Error("Err=%s|Target=%s", errMsg, parasMap["target"])`；log/logger.go:59 Error 非格式化函数，Errorf 才做 fmt.Sprintf。
- **核实**：get_certs_handler.go:36-40 确实调用 log.Error("Err=%s|Target=%s", errMsg, parasMap["target"])。pkg/log/logger.go:59 的 Error 是 slog 风格结构化接口（msg + key-value args），不做 fmt.Sprintf（只有 :64 的 Errorf 才展开格式串）。实际输出 msg 原样为 "Err=%s|Target=%s"，errMsg 被当作 key、target 被当作 value 附加，%s 不会替换。触发路径真实（failedList 非空即触发）。P3 恰当。

### 30. [P3·confirmed] UserListHandler.serveAdmin 静默丢弃 failedList，管理员列表可能缺节点且无提示

- **位置**：`pkg/http/user_handler.go:394` · 维度：错误处理 · 单元：HTTP
- **问题**：serveAdmin 用 `succList, _, _ := ...` 直接忽略失败节点列表。当 target=all 且部分节点 RPC 失败时，返回的用户全量视图缺少这些节点的数据，且响应里没有任何 failed 字段告知管理员结果不完整，可能被误读为该节点无用户/用户已删。对比 StatusHandler、ContainersHandler 都返回了 failed 映射。建议同样在响应中带上 failed。
- **证据**：user_handler.go:394 `succList, _, _ := rpcClient.ReqToMultiEndNodeServer(..., &proto.GetUsersReq{IncludeAll: true}, ...)` —— 失败节点被丢弃，line 421 `c.JSON(200, result)` 无 failed 字段。
- **核实**：user_handler.go:394 确认 `succList, _, _ := rpcClient.ReqToMultiEndNodeServer(...)` 丢弃 failedList，且 :396-421 构造的 result 仅含成功节点，c.JSON(200, result) 无任何 failed/错误提示字段。target 默认 all（target.go:17 defaultValue="all"），部分节点 RPC 失败时管理员视图静默缺节点。对比同包其他 handler（如 get_certs_handler 至少记日志）此处连日志都没有。P3 恰当。

### 31. [P3·confirmed] prometheus 收集器在注册闭包内单例共享，并发抓取相互覆盖 Update 数据

- **位置**：`pkg/http/metric_handler.go:20` · 维度：并发 · 单元：HTTP
- **问题**：prometheusHandler 在注册时（一次）创建 reg 与 trafficDesc/pingDesc/nodeMetricDesc 三个收集器，之后所有 /api/metrics 请求共用同一组收集器。每次抓取 goroutine 调 Update() 写锁 swap 整个 metrics 切片。两个并发抓取时，A.Update → B.Update（覆盖）→ A 调 promHandler.ServeHTTP 读到 B 的数据，抓取结果串味/张冠李戴（node 标签虽区分，但同一抓取里混入另一请求 target 的数据）。有 RWMutex 保护不会崩溃，但指标正确性在并发抓取下不可靠。应每次请求新建收集器或在单请求内串行 update+serve。
- **证据**：metric_handler.go:19-27 收集器与 reg 在 prometheusHandler 首次调用（注册）时创建并被返回的闭包长期持有；line 33-61 的请求闭包对同一 trafficDesc.Update/ServeHTTP 复用，无 per-request 隔离。
- **核实**：metric_handler.go:18-32 确认 reg 与三个收集器在 prometheusHandler 注册时创建一次，被返回的闭包（:33-61）长期共享；每个请求并发调共享 trafficDesc/pingDesc/nodeMetricDesc 的 Update()（v2raymg_traffic_desc.go:87-89 写锁整体替换 d.stats）后再 ServeHTTP 读 Collect。两个并发抓取（尤其 target 不同）时后到的 Update 覆盖先到的，先到请求 serve 出的是对方 target 的数据。RWMutex 只保证不崩溃不保证请求间隔离。该路由有 getAuthHandlerFunc 保护但不影响并发正确性问题。P3 恰当。

### 32. [P3·confirmed] SubHandler（最复杂 handler）零测试覆盖：双认证、ext_sub 合并、空订阅、userinfo 二次扇出均未测

- **位置**：`pkg/http/sub_handler.go:45` · 维度：测试缺口 · 单元：HTTP
- **问题**：pkg/http 下 16 个测试文件覆盖了 login/logout/rotate/fastAdd/user 等 handler，但 /sub 这个面向终端用户、公开、最复杂的 handler 没有任何 handler 级测试：token 认证 vs user+pwd 认证分支、tokenFinder 接口断言失败路径、invalid user 响应、ext_sub 合并与截断、clash 格式检测、writeSubUserInfoHeader 的二次 GetProfile 扇出与 header 组装均无覆盖（sub_userinfo_format_test.go 只测格式化纯函数）。本报告发现的 P1「空订阅 200」和 P2「URI 日志泄漏」都属于有测试即可拦住的回归面。违反架构约束 9（每个可测模块应有 *_test.go——按 handler 粒度看该文件是缺口）。
- **证据**：grep -l SubHandler pkg/http/*_test.go 无结果；ls pkg/http 显示 sub_handler.go 16KB 无对应 sub_handler_test.go，仅有 sub_userinfo_format_test.go 覆盖纯函数。
- **核实**：核实属实：pkg/http 下 14 个 *_test.go（finding 称 16，计数小误但不影响结论），grep SubHandler|/sub 在所有测试文件中零命中；sub_handler.go 16KB，handlerFunc(:45-216) 含 token/user+pwd 双认证分支、tokenFinder 接口断言失败路径(:80-85)、invalid user 200 响应、ext_sub 合并截断(:177-184)、clash 检测、writeSubUserInfoHeader 二次 GetProfile 扇出(:233-288)，均无 handler 级测试；sub_userinfo_format_test.go 只测纯函数。PROJECT_GUIDE.md:76 约束 9「每个可测模块对应 *_test.go」确实存在，:166 也确实在 Info 级别打印完整 URI（佐证其引用的回归面）。但这是测试缺口/流程类问题而非运行时缺陷，本身不直接造成线上故障，与同批 P2（管理面静默失效、指标数据被清零）不在同一严重度层级，建议降为 P3。

### 33. [P3·confirmed] 管理员静态 token 比较未用常数时间比较，存在计时侧信道

- **位置**：`pkg/http/auth/middleware.go:24` · 维度：安全 · 单元：HTTP
- **问题**：AuthMiddleware 的 `xtoken == staticToken` 与 auth_with_token_handler.go:11 的 `token != httpServer.token` 都是普通字符串比较，Go 的字符串相等在首个不匹配字节即返回。X-Token 是全权限 admin 凭据且这两个端点无失败限速，理论上可被网络计时逐字节猜解（实际受网络抖动限制，难度高）。属纵深防御问题。修复：crypto/subtle.ConstantTimeCompare，一行改动。
- **证据**：auth/middleware.go:23-27 `if xtoken := c.GetHeader("X-Token"); xtoken != "" { if xtoken == staticToken { c.Set(ContextKeyRole, "admin") ... } }`；auth_with_token_handler.go:11 同模式。
- **核实**：auth/middleware.go:24 `xtoken == staticToken` 与 auth_with_token_handler.go:11 `token != httpServer.token` 均为普通字符串比较，且两条鉴权路径上无失败限速（gin.Default 仅 logger/recovery，未见 rate limiter）。X-Token 为全权限 admin 凭据，属实的纵深防御缺口；finding 已自认网络抖动下实际利用难度高，P3 定级恰当。修复确为 subtle.ConstantTimeCompare 一行改动。

### 34. [P3·confirmed] JWT 黑名单只增不减且仅内存：无过期清理造成无界增长，重启后已注销 token 复活

- **位置**：`pkg/http/auth/blacklist.go:15` · 维度：资源 · 单元：HTTP
- **问题**：BlacklistAdd 只往全局 map 写入，没有随 JWT 过期时间的清理逻辑（JTI 在 token 过期后本可安全移除），长期运行的高频 logout 场景内存无界增长；且黑名单不持久化、不跨节点同步——进程重启后所有已注销但未过期的 JWT 重新有效（默认 24h 有效期窗口内），文件头注释已自认「Cleared on process restart」。作为管理面凭据撤销机制这是弱保证。修复：记录 jti→expiry 并周期清理；如需要重启后仍然生效可落地到 SQLite（架构约束 7 已有 Provider 抽象）。
- **证据**：auth/blacklist.go:12-19 `var globalBlacklist = &tokenBlacklist{set: make(map[string]struct{})}; func BlacklistAdd(jti string) { ...set[jti] = struct{}{} }` —— 全文件无删除/过期路径。
- **核实**：auth/blacklist.go 全文 27 行核实：globalBlacklist 只有 BlacklistAdd 写入与 BlacklistContains 查询，无任何删除/过期清理路径；文件头注释自认「Cleared on process restart」。jwt.go:31-33 确认默认有效期 24h，即重启后已 logout 但未过期的 JWT 在最长 24h 窗口内重新有效。内存无界增长需高频 logout 长期运行才显著，撤销弱保证是管理面纵深防御问题，P3 恰当。

### 35. [P3·confirmed] pkg/http/prometheus_desc 整包无任何测试，违反「每个可测模块应有 *_test.go」约束

- **位置**：`pkg/http/prometheus_desc/common.go:1` · 维度：测试缺口 · 单元：HTTP
- **问题**：该包 4 个文件（common.go/node_collector_desc.go/ping_desc.go/v2raymg_traffic_desc.go）承担 /api/metrics 的全部指标转换逻辑——dto 类型映射、远端 MetricFamily 反序列化、标签拼装、并发 Update/Collect——却没有一个 *_test.go（ls 确认目录仅 4 个源文件）。这些都是纯函数或可用 fake RPC 数据驱动的逻辑，极易测试；本报告发现的 Unmarshal 忽略错误/Must panic 问题（node_collector_desc.go:47）恰在无测试保护的路径上。修复：至少为 GetMetricValue/GenerateLabel/各 Collect 增加损坏输入与并发 Update+Collect 的表驱动测试。
- **证据**：ls pkg/http/prometheus_desc → common.go, node_collector_desc.go, ping_desc.go, v2raymg_traffic_desc.go，无任何 _test.go；架构约束 9 要求每个可测模块有测试。
- **核实**：ls 核实 pkg/http/prometheus_desc/ 仅 common.go、node_collector_desc.go、ping_desc.go、v2raymg_traffic_desc.go 四个源文件，无任何 *_test.go。PROJECT_GUIDE.md:76 架构约束 9 明确要求「每个可测模块对应 *_test.go」。node_collector_desc.go:47 `pb.Unmarshal(snf, metricFaimily)` 忽略错误返回值属实，正处于无测试保护路径。该包逻辑（标签拼装、MetricFamily 反序列化、并发 Update/Collect）确实可用 fake 数据表驱动测试。P3 恰当。

### 36. [P3·confirmed] getNode 无锁读取 localNodeList，与 listNode/updateLocalNodeList 持 nodeMutex 整体重赋值的写路径不一致

- **位置**：`cmd/cli/info.go:35` · 维度：并发 · 单元：CMD
- **问题**：info.go 为 localNodeList 定义了 nodeMutex，updateLocalNodeList（:29-32）和 command.go 的 listNode（:326-328）都在持锁状态下对 map 整体重赋值，suggest.go 的 getNodeSuggest 也持锁遍历；唯独 getNode（info.go:34-36）裸读 `return localNodeList[nodeName]`，被 fastAddInbound（command.go:395）调用。go-prompt REPL 当前基本单线程执行 handler，所以实际竞态窗口取决于补全回调与 handler 是否并发，但这套『同一份数据三处加锁一处不加』的写法意味着锁要么是必要的（那 getNode 是数据竞态，map 读与重赋值并发是未定义行为）要么是全部多余的。按最小修复给 getNode 补 nodeMutex 即可。
- **证据**：cmd/cli/info.go:34-36 `func getNode(nodeName string) *cluster.Node { return localNodeList[nodeName] }` 无锁；:28-32 updateLocalNodeList 持 nodeMutex 重赋值；cmd/cli/command.go:326-328 listNode 同样持锁写 `localNodeList, err = client.ListNode(...)`。
- **核实**：亲读 cmd/cli/info.go:34-36：getNode 裸读 localNodeList[nodeName]，无锁；info.go:28-32 updateLocalNodeList、command.go:326-328 listNode 均持 nodeMutex 对 map 整体重赋值，suggest.go:471-479 getNodeSuggest 持锁遍历。getNode 被 command.go:395 fastAddInbound 调用。核实并发上下文：updateLocalNodeList 仅在 InitPromptAndRegister（command.go:18，prompt.Run 之前）调用一次，无后台定时 goroutine（updateCycle 变量未被使用），实际竞态取决于 go-prompt 补全回调与 handler 是否并发——finding 对此已如实说明。『三处加锁一处不加』的不一致本身属实，P3 恰当。

### 37. [P3·confirmed] fastAddInbound 随机端口取值区间 10000-49999 与 forward 用户转发端口默认段（10000-60000）完全重叠，易与已分配 bind port 冲突

- **位置**：`cmd/cli/command.go:405` · 维度：正确性 · 单元：CMD
- **问题**：port==0 时 CLI 在本地生成 `port = 10000 + rand.Intn(40000)`（command.go:405），而服务端 forward 层的用户端口分配默认区间是 min_port=10000..max_port=60000（appconfig defaultAppConfig）。CLI 端的随机数对服务端已被 GetBindPort 分配出去的端口一无所知，撞上时新 inbound 会在容器启动/热加载阶段 bind 失败，错误又经由上一条 finding（状态码不检查）被打印成正常结果。端口分配本应由服务端统一决策（架构约束 4 的端口口径都在 usermanager/forward 层）：CLI 更合理的做法是把 port=0 原样传给服务端让其在可用区间外分配，或至少避开 forward 段。另外 tag 的时间戳 sha256 前 8 位也无碰撞检查，rand.Seed 在 Go 1.20+ 已废弃。
- **证据**：cmd/cli/command.go:403-406 `if port == 0 { rand.Seed(time.Now().UnixNano()); port = 10000 + rand.Intn(40000) }`；pkg/proxy/appconfig/loader.go:131-132（defaultAppConfig）`cfg.Forward.MinPort = 10000; cfg.Forward.MaxPort = 60000`。
- **核实**：亲读 cmd/cli/command.go:403-406：port==0 时 rand.Seed(time.Now().UnixNano()); port = 10000 + rand.Intn(40000)，取值 [10000,49999]。pkg/proxy/appconfig/loader.go:132-133 defaultAppConfig 中 cfg.Forward.MinPort=10000; cfg.Forward.MaxPort=60000，区间完全覆盖 CLI 随机段。CLI 侧无任何对服务端已分配端口的查询。另核实 http_client.go FastAddInbound 的回调确实不检查 StatusCode（:196-203 直接把 body 当 result），错误会被打印成正常输出。rand.Seed 弃用与 tag 8 位 sha256 前缀无碰撞检查也属实。P3 恰当。

### 38. [P3·confirmed] 用 filepath.Clean 清理 URL path：Windows 下路径分隔符变反斜杠导致全部 API 请求 404，并吞掉尾部斜杠

- **位置**：`cmd/cli/client/base_client.go:40` · 维度：正确性 · 单元：CMD
- **问题**：DoGetRequest（base_client.go:40）和 doJSONRequest（:73）都执行 `parsedUrl.Path = filepath.Clean(parsedUrl.Path)`。filepath 是 OS 路径语义：在 Windows 上 Clean 会把 "/api/inbound/fast" 规范成 "\\api\\inbound\\fast"，反斜杠在 url.String() 中被转义成 %5C，服务端路由全部 404——即 CLI 在 Windows 上完全不可用。即便在 Unix 上，Clean 也会移除尾斜杠和空 path 变 "."，与服务端路由的精确匹配存在隐性耦合。清理 URL 路径应使用 path.Clean（斜杠语义），或者干脆不清理（路径全部来自 common/const.go 常量拼接，本就无需规范化）。
- **证据**：cmd/cli/client/base_client.go:40 与 :73 `parsedUrl.Path = filepath.Clean(parsedUrl.Path)`，import 的是 "path/filepath" 而非 "path"。
- **核实**：亲读 cmd/cli/client/base_client.go：import 的是 "path/filepath"（:9），:40（DoGetRequest）与 :73（doJSONRequest）均执行 parsedUrl.Path = filepath.Clean(parsedUrl.Path)。filepath.Clean 是 OS 语义：Windows 下把 / 替换为 \，url.String() 会将其转义为 %5C，服务端路由匹配失败——GOOS=windows 构建的 CLI 全部 API 请求失效。Unix 下 Clean 去尾斜杠/空 path 变 "." 的行为也属实。路径全部来自 common 常量拼接，确实无需规范化。P3 恰当（Windows 构建是否为支持目标未在仓库中明示，但代码缺陷确凿）。

### 39. [P3·confirmed] Login 非 200 分支在读取 body 前直接返回错误，resp.Body 未关闭造成连接泄漏

- **位置**：`cmd/cli/client/http_client.go:22` · 维度：资源 · 单元：CMD
- **问题**：Login 的回调（http_client.go:21-30）在 `resp.StatusCode != 200` 时立即 `return fmt.Errorf(...)`，此路径既没有 io 读尽也没有 Close body——readBody（含 defer Close）只在 200 路径调用。CLI 每条命令都可能触发 getAuthToken→Login，当服务端持续返回 401（密码改了/token 失效）时每次命令都泄漏一个 TCP 连接直到进程退出。同时错误信息也丢弃了服务端 body 里的具体原因。应改为先 readBody（统一关闭），非 200 再用 body 构造错误（unexpectedHTTPStatus 正是为此存在）。
- **证据**：cmd/cli/client/http_client.go:21-30 `cb := func(resp *http.Response) error { if resp.StatusCode != 200 { return fmt.Errorf("login failed: HTTP %d", resp.StatusCode) } d, e := readBody(resp) ... }`，readBody（:49-52）才含 `defer resp.Body.Close()`。
- **核实**：亲读 cmd/cli/client/http_client.go:21-30：Login 回调在 StatusCode!=200 时直接 return fmt.Errorf，未读 body 也未 Close；含 defer resp.Body.Close() 的 readBody（:49-52）只在 200 路径调用，连接泄漏属实。触发频率也核实：cmd/cli/config.go:103-125 getAuthToken 在配置了用户名密码且 JWT 缓存失效时每次命令都调 client.Login，Login 持续 401 时每条命令泄漏一个连接（且静默回退到静态 token，用户无感知）。unexpectedHTTPStatus（:54-60）确实存在且未被 Login 使用，服务端 body 错误原因被丢弃。P3 恰当。

### 40. [P3·confirmed] cliFunc 用 panic 处理配置加载与 REPL 错误，用户看到 goroutine 堆栈而非错误信息，且绕过 main.go 的统一错误出口

- **位置**：`cmd/cli.go:22` · 维度：错误处理 · 单元：CMD
- **问题**：cliFunc（cmd/cli.go:20-28）对 LoadConfig 和 prompt.Run 的错误都直接 panic(err)。main.go 专门实现了 `if err := cmd.Execute(); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }` 的统一错误出口，但 cobra 的 Run（非 RunE）签名让错误无法走这条路径，最终用户因为配置文件权限问题或 yaml marshal 失败看到的是完整 panic 堆栈。应改用 RunE 返回 error，让 main.go 打印一行可读错误并以非零码退出。
- **证据**：cmd/cli.go:20-28 `if err := clipkg.LoadConfig(cliConfig); err != nil { panic(err) } ... if err := prompt.Run(); err != nil { panic(err) }`；main.go:10-14 已有 stderr+exit(1) 的错误出口未被利用。
- **核实**：亲读 cmd/cli.go:8-28：cliCmd 使用 Run（非 RunE），cliFunc 对 LoadConfig 和 prompt.Run 的错误均直接 panic(err)。main.go:10-15 确有 cmd.Execute() 错误经 stderr+os.Exit(1) 的统一出口，但 Run 签名不返回 error，panic 会绕过该出口并向用户输出完整 goroutine 堆栈。触发路径真实（配置文件不存在/权限错误即触发 LoadConfig 失败）。P3 恰当。

### 41. [P3·confirmed] REPL 三张本地缓存只在启动时预热一次，updateCycle 已声明但从未接线，长会话中节点/用户/inbound 补全与 getNode 域名回填持续陈旧

- **位置**：`cmd/cli/info.go:26` · 维度：正确性 · 单元：CMD
- **问题**：localNodeList/localUserList/localInboundList 仅在 InitPromptAndRegister 启动时各拉取一次（command.go:18-20），此后只有显式执行 ListNode/ListUser 才会部分刷新（listUser 只更新命中的 node key，删除的用户残留），ListInbounds 则根本不回写 localInboundList。info.go:26 的 `updateCycle = 5 * time.Second` 在仓库内无任何引用，说明周期刷新是计划中但从未实现的。后果：长会话里 AddUser/DeleteUsers/FastAddInbound 之后补全列表不更新；更实质的是 fastAddInbound 依赖 getNode(target) 回填 domain（command.go:395-398），新加入集群的节点查不到，domain 静默取空串传给服务端。要么接线周期刷新，要么在各写操作成功后主动失效对应缓存。
- **证据**：cmd/cli/info.go:26 `var updateCycle = 5 * time.Second`（grep 全仓库无使用点）；cmd/cli/command.go:18-20 三个 updateLocalXxx 仅在 InitPromptAndRegister 调一次；listInbounds（command.go:489-501）不写 localInboundList。
- **核实**：逐点核实成立：info.go:26 `var updateCycle = 5 * time.Second` 经全仓库 grep 仅有声明处一个匹配，无任何使用点；三个 updateLocalXxx 仅在 command.go:18-20（InitPromptAndRegister 开头）各调用一次，无 goroutine/ticker 接线。listUser（command.go:567-591）只对返回结果中命中的 node key 做 `localUserList[node] = users`，未返回的 node 上已删除用户确实残留；listInbounds（command.go:489-501）只打印 result，完全不回写 localInboundList。fastAddInbound（command.go:395-398）在 domain 为空时依赖 getNode(target) 回填，getNode 直接读 localNodeList map，新节点查不到时 node==nil，domain 保持空串直接传给服务端，无告警。P3 合理（有 ListNode 手动刷新的 workaround）。

### 42. [P3·confirmed] 测试缺口：runEndNode/startServer 的装配顺序契约与 --migrate 分支零测试覆盖

- **位置**：`cmd/server.go:105` · 维度：测试缺口 · 单元：CMD
- **问题**：cmd 包唯一的测试 server_cluster_user_test.go 只覆盖 cluster_user 装配的 4 个场景。runEndNode 中被注释标注为显式契约的初始化顺序（InitLoginPasswords 必须先于 NewUserManagerWithStore、certMgr 必须先于 containerMgr）、startServer 的 center/end 分流、--migrate 的读写回路（包括上一条 .json 破坏性 bug）、以及信号触发的关停序列均无任何测试。这些顺序契约恰恰是最容易被后续重构无声破坏的（违反架构约束 9：每个可测模块应有 *_test.go 覆盖其核心行为）。建议至少为 startServer 的 migrate 分支（可用临时目录内 yaml/json 各一份配置做回环断言）和装配顺序（以可注入的构造函数桩验证调用次序）补测试。
- **证据**：cmd/server_cluster_user_test.go 仅 4 个 Test 函数且 grep 无 runEndNode/startServer 引用；cmd/server.go:123-125 注释 'Must run BEFORE NewUserManagerWithStore'、:168 'needs certMgr and HTTPPort' 等顺序契约无测试保护。
- **核实**：cmd 包（package cmd）唯一测试文件为 server_cluster_user_test.go，仅含 4 个 Test 函数（TestStartupWiring_ClusterDisabled/ClusterEnabled/NodeGroupsSeed/BackfillClusterFields），grep 确认无任何测试引用 runEndNode/startServer。server.go:123-126 注释明确声明顺序契约 'Must run BEFORE NewUserManagerWithStore'（InitLoginPasswords）、:168 'needs certMgr and HTTPPort'（containerMgr 依赖 certMgr），:74-81 的 --migrate 分支（load→SaveToFile→exit）均无测试保护。cmd/cli 与 cmd/cli/client 各有一个测试文件但与 server 装配无关。测试缺口属实，P3 恰当。runEndNode 实际起始于 server.go:105，行号准确。

### 43. [P3·confirmed] inputConfig 用 fmt.Scanln 明文回显读取密码，且 token/密码含空格时被截断

- **位置**：`cmd/cli/config.go:66` · 维度：安全 · 单元：CMD
- **问题**：首次运行的交互式配置直接 `fmt.Scanln(&globalConfig.Password)`：密码在终端明文回显（进入 shell 滚动缓冲与可能的终端日志），且 Scanln 按空白分词，含空格的密码只会读到第一段、剩余部分残留在 stdin 缓冲里错位填充后续字段，用户得到一个悄悄错误的配置并被回写落盘。读密码应使用 golang.org/x/term 的 ReadPassword，并对整行输入使用 bufio 读行。
- **证据**：config.go:66-67 `fmt.Printf("please input password: "); fmt.Scanln(&(globalConfig.Password))`；同模式的 :62 token 输入亦然
- **核实**：config.go:66-67 逐字核实为 `fmt.Printf("please input password: "); fmt.Scanln(&(globalConfig.Password))`，:62 token 同模式。未使用 x/term ReadPassword，密码明文回显属实；fmt.Scanln 按空白分词，输入含空格时只取第一段且剩余 token 残留 stdin 缓冲、错位填充后续 Scanln，随后 LoadConfig(:51-55) 无条件 Marshal 回写落盘（且权限为 0666，比 finding 说的还多一个问题），错误配置被静默持久化。P3 恰当。

### 44. [P3·confirmed] getNode 无锁读取 localNodeList，与 listNode/updateLocalNodeList 持锁整体重赋值构成不一致的锁协议

- **位置**：`cmd/cli/info.go:34` · 维度：并发 · 单元：CMD
- **问题**：localNodeList 的所有写方（updateLocalNodeList info.go:29-31、listNode command.go:326-328）都持 nodeMutex 并整体替换 map，suggest.go:471 的 getNodeSuggest 读取时也加锁，唯独 getNode（被 fastAddInbound 用来回填 domain）裸读。当前 go-prompt REPL 下 handler 与 suggest 回调若在不同 goroutine 触发（补全在输入线程、handler 在执行线程），存在 map 读与替换写的竞态窗口；即使当前实现是单线程，同一数据三处加锁一处不加也属于协议不一致，后续接线 updateCycle 周期刷新（原设计意图）后会立即变成真实 data race。加锁成本为零，应补齐。
- **证据**：info.go:34-36 `func getNode(nodeName string) *cluster.Node { return localNodeList[nodeName] }`（无锁）；对比 info.go:29-31 与 command.go:326-328 写路径均 `nodeMutex.Lock()` 后重赋值 `localNodeList, err = client.ListNode(...)`
- **核实**：info.go:34-36 getNode 裸读 localNodeList 属实；三处对照方全部核实持锁：updateLocalNodeList（info.go:29-31）、listNode（command.go:326-328）均 nodeMutex.Lock 后整体重赋值 map，getNodeSuggest（suggest.go:470-471）读取时也加锁。getNode 被 fastAddInbound（command.go:395）调用。锁协议不一致确凿；是否构成当前运行时的真实 data race 取决于 lureiny/go-prompt 的 handler/suggest 调度线程模型（未逐行核实），但 finding 本身已如实限定该不确定性，且 updateCycle（info.go:26）确实定义未接线，佐证周期刷新的原设计意图。作为 P3 协议一致性问题成立。

### 45. [P3·confirmed] fastAddInbound 随机端口生成无冲突规避且与提示文案不符，tag 生成用 sha256(时间戳) 冗余

- **位置**：`cmd/cli/command.go:404` · 维度：正确性 · 单元：CMD
- **问题**：port==0 时 `rand.Seed(time.Now().UnixNano()); port = 10000 + rand.Intn(40000)`：(1) 生成区间是 [10000,49999]，而 portSuggest 文案承诺 "10000-50000"；(2) 每次调用重新 Seed 是 Go1.20+ 已弃用的反模式（go.mod 声明 go 1.24），并且会把全局随机源重置；(3) 随机端口不与目标节点既有 inbound、forward 用户端口段做任何冲突检查，撞上已占用端口时依赖服务端报错，而 FastAddInbound 封装又不检查状态码（见另一 finding），失败会被打印成成功。tag 生成用 sha256(纳秒时间戳) 截 8 位 hex，等价于直接取随机 hex 却引入碰撞不必要的哈希步骤。建议 port 交由服务端自动分配（与 RotateInboundPort 的 port=0 语义对齐）或至少提示区间一致。
- **证据**：command.go:403-406 `if port == 0 { rand.Seed(time.Now().UnixNano()); port = 10000 + rand.Intn(40000) }`；suggest.go:92-95 portSuggest Description "will generate a random port in 10000-50000"
- **核实**：command.go:403-406 逐字核实 `rand.Seed(time.Now().UnixNano()); port = 10000 + rand.Intn(40000)`，区间为 [10000,49999]；suggest.go:92-95 portSuggest 文案为 "random port in 10000-50000"（该子项属措辞级差异，较弱）。command.go:8 import math/rand 而 go.mod 声明 go 1.24.0，每次调用重置全局种子的 rand.Seed 反模式属实。tag 生成用 sha256(纳秒时间戳) 截 8 hex（:400-401）属实。生成端口后直接发请求（:425+），本地无任何与既有 inbound/forward 端口的冲突检查，属实。P3 恰当。

### 46. [P3·confirmed] 测试缺口：REPL 输入行解析（GetSuggest/getParamValue）、LoadConfig 落盘行为、command.go 全部 handler 均无测试；ListAllUsers 与 ListUser 完全重复

- **位置**：`cmd/cli/suggest.go:340` · 维度：测试缺口 · 单元：CMD
- **问题**：cmd/cli 现有测试只覆盖 config.go 的 getAuthToken 缓存逻辑与 client 层部分请求编码。缺口：(1) suggest.go 的 GetSuggest/getParamValue/isInputXxx 是手工字符串状态机（空格分词、lastFlag 匹配、SuggestPrefix 拼接），是模块地图明示的高脆弱点，0 测试；(2) LoadConfig 的三条路径（文件缺失→交互输入、yaml 损坏→覆盖重写、成功→回写与权限）0 测试；(3) command.go 约 30 个 handler 的参数映射（尤其 fastAddInbound 40 个位置参数→结构体，新增协议字段时最易漏）0 测试。另外 http_client.go:422-440 的 ListAllUsers 与 402-420 的 ListUser 请求 URL、参数、解析完全相同，是复制粘贴产物，REPL 同时暴露 ListUser/ListAllUsers 两个行为一致的命令，应删一保一。违反架构约束第 9 条（每个可测模块应有 *_test.go：suggest.go/command.go/info.go 无对应测试）。
- **证据**：cmd/cli 下仅存在 config_test.go（6 个 Test 均针对 getAuthToken/ClearJWTCache）与 client/http_client_test.go；suggest.go:340-435 GetSuggest/getParamValue 无任何测试引用；http_client.go:402 与 :422 两函数逐行相同（仅函数名不同）
- **核实**：cmd/cli 下仅 config_test.go 与 client/http_client_test.go 两个测试文件，config_test.go 的 6 个 Test 全部针对 getAuthToken/ClearJWTCache；suggest.go:340 GetSuggest、:420 getParamValue、command.go 全部 handler、LoadConfig 均无测试，属实。http_client.go:402 ListUser 与 :422 ListAllUsers 逐行相同（URL 同为 common.User、参数同为 target、解析相同），且 command.go:145/:152 在 REPL 同时注册 ListUser 与 ListAllUsers 两个行为一致的命令，复制粘贴冗余属实。PROJECT_GUIDE.md:76 确有约束第 9 条'每个可测模块对应 *_test.go'。P3 恰当。
