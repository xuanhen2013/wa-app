# Registration Dedicated Proxy Plan

## 背景

注册链路需要按号码归属地区选择出口代理。例如注册美国 WhatsApp 账号时，注册相关请求应使用美国地区代理。

这个能力不应复用 `WA_COMMON_PROXY`。`WA_COMMON_PROXY` 是服务级通用出口代理，而本方案只限定在注册动作内生效。

## 目标

- 仅对注册相关动作使用注册专用代理：
  - 请求 OTP，包括注册前 probe 和 request code。
  - 提交 OTP，包括普通验证码提交和账号迁移轮询提交。
- 根据注册号码或请求 payload 推导目标地区，优先使用 ISO 3166-1 alpha-2 国家码，例如 `US`。
- 注册请求和提交 OTP 使用同一个 sticky session，避免 OTP 请求和提交阶段出口 IP 不一致。
- 不修改 proto 契约，不在跨仓公共模型中暴露代理供应商、具体 endpoint、代理地址、账号密码或会话材料。
- 不在日志、文档、dashboard 响应中输出真实代理凭据、真实代理地址或可复用请求材料。

## 非目标

- 不把注册专用代理变成全局 WA 出口。
- 不影响登录态检查、长连接、消息收发、资料拉取等非注册链路，除非后续明确扩展。
- 不在 proto 中新增供应商字段或具体代理配置字段。
- 不依赖 sibling 仓库源码。

## 当前代码入口

- Dashboard 请求 OTP 入口：`internal/waapp/bff/action_gateway.go` 的 `requestSMSOTP`。
- Dashboard 提交 OTP 入口：`internal/waapp/bff/action_gateway.go` 的 `submitOTP`。
- 两者都会走 `registrationRunner`，这里是绑定注册 runner 和代理的最佳插入点。
- 当前代理解析入口在 `internal/waapp/bff/wa_proxy_resolver.go` 的 `resolveWAProxyRoute`。
- 当前地区推导逻辑在 `internal/waapp/bff/wa_proxy_route.go` 的 `proxyCountryCodeFromPayload`。
- 引擎已经支持 `engine.NativeEngine.WithProxyURL(...)`，并支持 `http`、`https`、`socks5`、`socks5h` 代理 URL。

## 上游 cliproxy/boltproxy 分支参考

`upstream/cliproxy-sticky-proxy` 是原仓库中的代理实验分支。它最初实现 cliproxy，后续提交迁移为 boltproxy。该分支和本计划方向接近，但供应商形态不同。

可借鉴的设计：

- 用手机号和 salt 派生稳定 sticky sid，避免请求 OTP 和提交 OTP 使用不同出口。
- 按号码国家推导代理地区，并扩展 calling code 到 ISO2 的映射。
- number probe 先选择并预检代理出口，再把验证过的出口以 `egress_pin` 形式带到注册请求中，确保 probe 和注册复用同一个出口。
- 对候选出口做 precheck，验证连通性和出口质量；失败时换 sid 轮转。
- dashboard 只返回脱敏的 route summary，不暴露完整代理 URL 或凭据。

不能直接套用的部分：

- boltproxy 使用固定 endpoint 和 username 规则；1024proxy 需要先调用 API 获取 `host` / `port`。
- boltproxy 分支包含注册参数、长连接、协议号导出、前端部署等非代理改动，直接 merge 会扩大冲突面。
- 本计划仍以 source adapter 为边界。1024proxy 作为一个 source；后续如果要兼容 boltproxy，可新增另一个 source，而不是把供应商规则写死到 resolver。

## 推荐方案

新增注册专用代理 resolver，优先级放在 `WA_COMMON_PROXY` 之前，并且只由 `registrationRunner` 调用。

解析顺序建议：

1. 从 payload 或 phone 推导 `country_code`。
2. 如果启用注册专用代理，生成或复用该注册流程的 sticky session。
3. 按 source 顺序调用注册代理源适配器，由适配器负责提取节点、拼接鉴权材料和生成可用代理 URL。
4. 使用最终代理 URL 访问内置的非敏感出口检测目标，确认出口 ISO2 与 `country_code` 一致；检测失败、不可达或地区不匹配时不允许进入 WA 注册。
5. 调用 `engine.WithProxyURL(proxyURL)` 创建只用于本次注册动作的 runner；同一个已验证 route 用于 probe、请求 OTP 和后续提交 OTP。
6. 返回脱敏后的 `WAProxyRoute` 元数据给 dashboard，例如 `proxy_mode`、`country_code`、`source`、`policy_mode`、`route_id`，不返回 `proxy_url`。
7. 如果注册专用代理未启用或解析失败，根据配置决定 fail closed 或 fallback direct/common proxy。

注册代理 resolver 不关心某个供应商如何取 IP、如何映射地区、如何拼接 username，也不持有静态 host/port。每个 source 适配器只需要统一产出候选代理列表：

```json
{
  "source": "1024proxy",
  "scheme": "http",
  "host": "<provider-returned-host>",
  "port": "<provider-returned-port>",
  "username": "<runtime-built-username>",
  "password": "<runtime-secret>",
  "expires_at_unix": 0
}
```

正式运行时只暴露一个最终可用的代理 URL 给 `engine.NativeEngine.WithProxyURL(...)`。链式代理只属于本地调试手段，不进入服务配置和 source 输出模型。

## 1024proxy 接入形态

1024proxy 的接入节点提取与出口选择是两层独立机制：

- 节点提取：调用 `white` 提取 API，并固定使用 `region=Rand`，返回 HTTP 接入节点的 `host` 和 `port`。
- 地区：不由提取 API 决定；最终 HTTP 代理用户名中的 `region-{country}` 才选择出口国家，`{country}` 为 ISO 两位国家码，例如 `US`。
- Sticky session：最终用户名中的 `sid-{session_id}-t-{sticky_minutes}` 选择粘性会话；`sid` 为 8 位 token，`t` 为分钟数。
- 最终拨号形态是 `http://<username>-region-<country>-sid-<session_id>-t-<minutes>:<password>@<host>:<port>`；不得把完整 URL、用户名、密码或节点地址写入日志、文档示例或 dashboard。
- 协议：当前手工验证返回节点按 HTTP 代理可用，按 SOCKS5 不可用；默认 source scheme 建议为 `http`。
- Proxy Host/Port：不作为静态配置保存；每次由提取 API 返回。

注册链路建议使用 sticky session，而不是 rotating session。OTP 请求和 OTP 提交之间需要尽量保持同一个出口 IP。

### 批量注册国家目录

1024proxy 提供的 `Rand` 和州/省数据不等同于可注册国家：`Rand` 只用于提取接入节点，注册出口仍必须是明确的 ISO2 国家码。批量注册通过服务端将 HeroSMS `getCountries` 的可见国家解析为 ISO2 后，再与 1024proxy 的国家级支持目录取交集；因此不会在 Dashboard 暴露供应商 country id、代理凭据、`Rand` 或州/省选择。对非交集国家的报价查询和任务创建必须拒绝。

手工验证记录：

- 2026-07-09 在本地测试环境中，因为 1024proxy 返回节点限制中国 IP，使用 `socks5://127.0.0.1:9981` 作为测试用上一跳，请求 1024proxy 提取 API 可返回 1 个节点。
- 使用测试链路 `local socks -> 1024proxy returned HTTP node -> HTTPS target` 可成功访问 HTTPS 目标，并可观察到 1024proxy 出口国家。
- 使用测试链路 `local socks -> 1024proxy returned SOCKS5 node -> target` 不可用，因此当前不要把 1024proxy 返回节点默认当 SOCKS5 使用。
- 正式海外环境不需要链式代理；服务配置只保存 1024proxy source 本身所需参数。
- 1024proxy 的 `white` 提取 API 要求将正式服务器的实际公网出站地址加入供应商白名单。上线前必须从该服务器直连验证提取接口返回 HTTP `200` 和非空节点；`403` 表示白名单或其生效范围仍不正确，不能开始注册或购号。
- 提取 API 偶发 TLS 握手失败，需要 source 适配器内置有限重试；重试后仍无节点时，本次注册应按 `fallback` 策略处理，默认拒绝。
- 2026-07-10 使用本地 SOCKS5 实测：`region=Rand` 的提取 API 和返回 HTTP 节点链路可用；该测试不输出节点或凭据。
- 2026-07-10 生产服务器实测确认：提取 API 的地区参数只用于获取随机接入节点，固定为 `Rand`；1024proxy 的真实出口地区与 sticky session 必须由最终代理用户名控制。使用 `PH`、8 位 sid 和 30 分钟时，接入节点提取成功且真实出口国家为 `PH`。不要记录节点地址、完整用户名、密码或出口 IP。

## 配置建议

新增服务内环境变量，避免进入 proto 契约：

```env
WA_REGISTRATION_PROXY_ENABLED=false
WA_REGISTRATION_PROXY_SOURCE_ORDER=1024proxy
WA_REGISTRATION_PROXY_FALLBACK=reject
WA_REGISTRATION_PROXY_STICKY_MINUTES=30
WA_REGISTRATION_PROXY_SOURCE_RETRY_MAX=3

WA_REGISTRATION_PROXY_SOURCE_1024_ENABLED=false
WA_REGISTRATION_PROXY_SOURCE_1024_USERNAME_TEMPLATE=
WA_REGISTRATION_PROXY_SOURCE_1024_PASSWORD=
```

说明：

- `WA_REGISTRATION_PROXY_SOURCE_ORDER` 定义 source 尝试顺序；后续接入多个供应商时，每个 source 有自己的配置前缀和适配器。
- 1024proxy 的提取 API endpoint、`format=1`、`type=json`、`num=1`、返回节点协议 `http` 都由 1024proxy source 适配器内置，不作为环境变量暴露。
- 1024proxy 提取 API 的 `region=Rand` 由 source adapter 固定；注册号码推导出的 ISO 国家码只填入最终代理用户名的 `{country}`。
- 1024proxy 的 `time` 参数默认使用 `WA_REGISTRATION_PROXY_STICKY_MINUTES`，避免注册 sticky session 和提取 IP 会话时长不一致。
- 出口检测 URL、响应格式与超时属于服务内 resolver 实现，不额外增加环境变量；检测只比较国家码，不把出口 IP、完整代理 URL 或响应体写入日志和 dashboard。
- `WA_REGISTRATION_PROXY_SOURCE_1024_USERNAME_TEMPLATE` 在运行时填充 `{country}`、`{session_id}`、`{sticky_minutes}`。1024proxy sticky 模板必须符合 `<username>-region-{country}-sid-{session_id}-t-{sticky_minutes}`；sid 使用 8 位稳定 token，sticky 时长限制为 1–120 分钟。模板本身不应写入真实用户名样例。
- `WA_REGISTRATION_PROXY_SOURCE_1024_PASSWORD` 属于敏感数据，只从环境变量读取，不输出日志。
- 本地调试时，如果 API 和返回节点都要求非中国出口，可在测试命令或测试 harness 层使用 `socks5://127.0.0.1:9981` 链式验证；不要把该上一跳写进正式服务配置。
- `WA_REGISTRATION_PROXY_FALLBACK=reject` 表示代理不可用时拒绝注册请求；后续可支持 `direct` 或 `common_proxy`，但默认不建议静默降级。

## Session 设计

注册请求和提交 OTP 需要复用同一个 sticky session。

建议生成 session id 的时机：

- 第一次请求 OTP 时生成。
- 和 `verification_request_id`、`wa_account_id` 或注册临时状态关联。
- 保存到短期运行态，TTL 应比 sticky session 时长多至少 5 分钟，但不长期保留。

建议保存内容：

```json
{
  "proxy_mode": "REGISTRATION_DEDICATED",
  "country_code": "US",
  "source": "REGISTRATION_1024PROXY",
  "route_id": "<redacted-route-id>",
  "expires_at_unix": 0
}
```

短期运行态还需要保存最终代理 URL，以便提交 OTP 时精确复用已请求验证码的出口。该 URL 含凭据，必须只存在于 runtime（Redis 或 SQLite runtime TTL），并且绝不进入 dashboard 响应、日志、PG 任务记录或文档示例。

不保存到长期记录或 dashboard 的内容：

- 代理密码。
- 完整代理 URL。
- 可直接复用的完整用户名。
- 真实代理出口 IP 或供应商返回节点。

## 推荐默认值

- Sticky session 时长：`30` 分钟。
- 运行态 TTL：`35` 分钟。
- 代理不可用策略：`reject`。
- 代理模式名：`REGISTRATION_DEDICATED_PROXY`。
- 来源名：`REGISTRATION_PROXY_PROVIDER` 或 `REGISTRATION_1024PROXY`。

`t=5` 不建议作为默认值，因为当前 OTP 等待默认是 20 分钟，5 分钟可能导致提交 OTP 时 sticky session 已过期。

## 错误处理

注册专用代理 resolver 应区分以下错误：

- 配置缺失：缺 source、提取 API、username template、password 等必要配置。
- 地区缺失：无法从手机号或 payload 推导国家码。
- 提取失败：API 不可达、TLS 握手失败、返回空列表或返回格式不符合预期。
- 代理 URL 无效：协议或 host/port 不合法。
- 代理连接失败：`WithProxyURL` 成功但实际请求失败。
- Sticky session 过期：提交 OTP 时发现 session 不存在或已过期。

建议 dashboard 返回脱敏错误，例如：

```json
{
  "success": false,
  "proxy": {
    "proxy_mode": "REGISTRATION_DEDICATED_PROXY",
    "country_code": "US",
    "source": "REGISTRATION_PROXY_PROVIDER"
  },
  "error_message": "registration proxy is unavailable"
}
```

## 测试计划

单元测试：

- `country_calling_code=1` 推导为 `US`。
- payload 显式 `proxy_country_code` 优先级最高。
- 注册专用代理启用时，`registrationRunner` 不使用 `WA_COMMON_PROXY`。
- 生成的 route map 不包含 `proxy_url`、username、password。
- sticky session 的 dashboard summary 不包含完整代理 URL 或密码；runtime 中的敏感 URL 只用于 OTP 提交复用。
- 1024proxy source 可把提取 API 返回的 `host`、`port` 转成候选代理。
- 出口检测返回的国家与目标地区不一致时，注册专用代理按 `fallback=reject` 拒绝，不触发 WA 请求。

集成测试：

- 用本地可控 HTTP/SOCKS5 代理验证 `requestSMSOTP` 和 `submitOTP` 都进入同一个代理 runner。
- 验证代理不可用时按 `fallback=reject` 返回可理解错误。
- 验证 `WA_COMMON_PROXY` 仍只影响非注册专用代理场景。

手工验证：

- 使用 1024proxy 提取 API 以 `region=Rand` 获取接入节点，不依赖静态 Proxy Host/Port；出口地区只通过最终代理用户名的 `region-{country}` 选择。
- 本地测试机如果被供应商 IP 限制，可在测试命令层使用 `local socks -> source returned node -> target` 验证，不纳入正式运行时配置。
- 先用非敏感目标验证 `region-US` 的出口地区。
- 再验证同一 `sid` 在 sticky session 时长内出口保持稳定。
- 在批量注册前执行同一 resolver 的代理预检；预检失败时必须在申请短信号码之前终止条目，避免代理故障消耗短信激活费用。

## 开工决策

- 1024proxy 第一版统一使用 HTTP proxy；当前手工验证 HTTP 可用、SOCKS5 不可用。
- sticky session 默认时长使用 `30` 分钟。
- 代理不可用时默认 `fallback=reject`，不静默 fallback direct。
- OTP 提交超出 sticky session 后直接拒绝并提示重新请求 OTP，不自动新建 session 后重试。
- 正式环境不配置链式代理；本地测试才在测试命令或 test harness 层使用 `socks5://127.0.0.1:9981`。
