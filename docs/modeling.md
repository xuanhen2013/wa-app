# wa-app 建模说明

## 1. 目标

把现有 `wa-re` 脚本能力拆成可组合的原子 RPC。RPC 层只表达“要做什么”和“状态如何变化”，不表达“具体如何请求、如何走代理、如何解包 APK、如何落库”。

## 2. 脚本动作到 RPC 的映射

| 现有脚本/动作 | 原子 RPC | 说明 |
| --- | --- | --- |
| `standalone_state.py` 生成号码状态 | `WaProfileService.PrepareClientProfile` | 为目标号码准备稳定客户端身份和消息状态；不暴露密钥材料。 |
| `fresh_v2_exist.py` 探测账号/路由 | `WaRegistrationService.ProbeAccount` | 只返回业务探测结果和可用投递方式。 |
| `fresh_v2_code.py` 请求 OTP | `WaRegistrationService.RequestVerificationCode` | 触发验证码投递，记录请求状态。 |
| `fresh_v2_register.py` 提交 OTP | `WaRegistrationService.SubmitVerificationCode` | 使用验证码完成注册，产出后续消息登录身份。 |
| `chatd_pure_client.py` 打开长连接 | `WaMessagingService.OpenMessageSession` | 打开一个可接收消息的运行态会话。 |
| `chatd_pure_client.py` 接收消息 | `WaMessagingService.ReceiveMessageBatch` | 拉取一批已接收消息元数据。 |
| `chatd_pure_client.py` ack | `WaMessagingService.AcknowledgeMessage` | 对单条消息做确认。 |
| `wa_signal_decrypt.py` 解 `<enc>` | `WaExtractionService.DecryptMessage` | 对消息密文做解密，必要时提交学习到的会话状态。 |
| OTP/Flag 文本提取 | `WaExtractionService.ExtractCandidates` | 从明文或消息内容中提取 OTP/Flag 候选值。 |
| APK/协议差异分析 | `WaDiscoveryService.RegisterAppArtifact` / `RecordProtocolProfile` | 记录一次应用/协议观察结果，只保留能力级模型。 |
| `phone_profile.py` 生成/应用 fingerprint | `WaToolingService.GeneratePhoneFingerprintProfile` / `BuildRegistrationRequest` | 用 Go 原生复刻 detached profile 字段、base/raw/map 参数注入。 |
| `fresh_v2_code.py build_plain` / `fresh_v2_exist.py build_plain` | `WaToolingService.BuildRegistrationRequest` | 只构造参数、plaintext、ENC body 和 header，不负责业务编排。 |
| `wasafe_enc.py` | `WaToolingService.EncryptWASafeEnvelope` | 对任意 plaintext 生成 WASafe ENC。 |
| WAMSYS plaintext JSON 导入 | `WaToolingService.ImportWamsysCapture` | 把 scalar/map/byte fields 转成 typed proto，后续构造请求时可选择应用。 |
| APK token 派生 / keystore authkey 派生 | `WaToolingService.DeriveRegistrationToken` / `DeriveAuthKey` | 迁移 PBKDF2/HMAC/APK cert/classes.dex/about_logo 和 keystore AES/OFB 逻辑。 |

## 3. 状态边界

长期事实：

- 目标号码与归属 workspace。
- 客户端 profile 的生命周期状态。
- 验证码请求与注册尝试。
- 注册成功后的服务端账号身份引用。
- 注册成功后的活动登录态投影，供后续消息会话校验与管理页展示。
- 消息元数据、ack 状态、解密状态、候选值元数据。
- 协议能力观察结果。

短期运行态：

- 幂等请求窗口。
- 长连接 lease、chatd ping heartbeat、监听缓冲。
- 操作锁和速率窗口。
- 等待消息的短期 batch cursor。

## 4. 存储约束

后续实现如需存储：

- PG：长期事实、用户数据、审计投影、消息元数据、注册记录、profile 状态。
- Redis：TTL 运行态、连接租约、锁、短期消息缓冲、幂等窗口、速率控制。

实现层已在 `migrations/001_init.sql` 定义服务自有 PG schema，在 Redis runtime 中定义服务自有 TTL key；这些实现细节不进入 proto 契约。

## 5. 敏感数据处理

以下内容视为敏感：

- OTP / Flag。
- token、authkey、request token、backup token。
- identity key、prekey、signed prekey、Signal session。
- 可复用请求体、cookie、proxy credential。

RPC 可在授权场景返回敏感候选值，但日志、错误、索引和普通列表接口应使用引用或脱敏值。

## 6. 当前实现边界

当前实现已经落地以下运行时组件：

- gRPC 服务入口：`cmd/wa-app-service`。
- PG store：`internal/app/postgres_store.go`，只保存长期事实和脱敏/引用字段。
- Redis runtime：`internal/app/redis_runtime.go`，保存幂等窗口和消息会话 lease。
- HTTP action gateway：`internal/app/action_gateway.go`，承接外部自动化可选调用的 `/api/wa/actions/*` 动作，管理临时指纹 TTL、OTP 提交和登录态投影。
- Go 原生协议引擎：`internal/app/native_engine.go`，不通过外部脚本桥接。
- WASafe ENC/HTTP：`internal/app/native_http.go`。
- chatd Noise XX 握手、帧层、二进制节点 codec：`internal/app/chatd_*.go`。
- Signal pkmsg/msg 解析、X3DH/root ratchet、AES-CBC 解密：`internal/app/signal_decrypt.go`。
- Detached profile/WAMSYS/APK-token/keystore-authkey/request-material tooling：`internal/app/tooling.go`。
- 本地 profile/state：`internal/app/native_state.go`。
- 原生注册编排：`registration_orchestrator.go` 与 action gateway 负责号码/SMS 检测、注册 OTP、登录态持久化等原子动作串联。
- Dashboard BFF 与独立前端：`cmd/wa-app-service/dashboard_http.go` 与 `webui/`，负责静态 SPA、WA 管理页和长连接状态查询。
- 长连接管理器：服务启动后扫描 ACTIVE 登录态，先做原生 chatd 被动短连接检测，再恢复消息接收循环；注册成功或登录态检测成功后会自动启动对应长连接。
- OTP 历史：解密得到 OTP 候选值时只写入 wa-app 自有 OTP 历史；当前不依赖平台 MQ 或跨仓公共事件。

Go 原生实现关系：

| RPC | 当前 native 行为 |
| --- | --- |
| `PrepareClientProfile` | 直接用 Go 生成稳定号码 profile、X25519 authkey 与注册 key bundle 状态。 |
| `ProbeAccount` | 直接用 Go 构造 WASafe ENC 请求并调用账号探测接口。 |
| `RequestVerificationCode` | 直接用 Go 构造 `/v2/code` 参数、WASafe 加密、HTTP 请求，并把敏感运行态写入服务工作区。 |
| `SubmitVerificationCode` | 直接用 Go 复用上一步参数构造 `/v2/register` 请求并解析注册身份。 |
| `ReceiveMessageBatch` | Go 原生打开 chatd 连接，执行 WA Noise XX 握手，发送 chatd ping 作为心跳探测，解密二进制节点流，抽取 `<enc>` payload 引用并自动发送基础 ack。 |
| `DecryptMessage` / `ExtractCandidates` | Go 原生支持 inline/plaintext 与 Signal `pkmsg`/`msg` 解密，并可按 `SessionCommitPolicy` 提交学习到的接收链状态。 |
| `WaToolingService.*` | Go 原生迁移 detached Python helper：profile、WAMSYS typed import、registration plaintext/ENC 构造、APK token、keystore authkey、WASafe ENC。 |

注意：profile 状态目录属于 Go native engine 工作区，默认 `var/wa-app/profiles`；PG 只保存其业务投影，Redis 只保存 TTL 运行态。


## 7. 当前非编排实现边界

- Go profile 已生成可验证的 Signal/X25519 key bundle：`e_skey_sig` 使用 identity private key 对带 Curve25519 type 前缀的 signed prekey public key 做 XEdDSA 签名，并在写入 state 前自校验。
- 原生注册 HTTP 参数覆盖实际 App 主干形态：`/v2/exist`、`/v2/code`、`/v2/register` 统一复用稳定 profile/key bundle，发送 code/register 默认设备 map，并保留 SMS、voice、flash、WA old、email OTP、passkey、silent auth、recaptcha 等投递/挑战方式的协议枚举与等待状态解析。
- WAMSYS 对齐记录见 `docs/registration-wamsys-re.md`。App 侧硬件/反滥用生成的 opaque WAMSYS 字段按逆向确认的字段集、长度、编码和生命周期精准伪造；profile/default map 不承载 `gpia/_gi/_gg/_gp/_ga/aid`，这些字段只能由明确的 WAMSYS material source 注入。运行态 `/v2/exist`、`/v2/code` 使用 Pure-Go precision provider，授权分析场景可通过 `ImportWamsysCapture` / `BuildRegistrationRequest` 显式覆盖。
- chatd token dictionary 目前使用最小 fallback 表，覆盖当前收消息与 ack 所需节点；如需更完整词表，应迁移为 Go 资源或生成物，而不是运行时读取 `wa-re` 脚本。
- 跨步骤业务编排不依赖外部编排器；`StartRegistration` 提供本服务原生 HTTP 编排入口，外部自动化如需接入只能调用 wa-app 自有 HTTP/gRPC 原子能力。`BuildRegistrationRequest` 只产出请求材料，不替代 `RequestVerificationCode` / `SubmitVerificationCode` 的业务记录。

## 8. 外部自动化边界

当前 WA 主链路不保留外部 workflow。号码探测、注册、OTP 等待、OTP 提交、登录态检测和长连接恢复均由 wa-app BFF/原生服务直连原子能力完成；外部自动化只作为可选调用方，不作为运行时依赖。

注册入口 `POST /api/wa/register` 入参为号码国家/拨号码和号码；生成注册用随机指纹并提交为该号码设备指纹，再发起 SMS OTP，后续 OTP 提交和登录态持久化仍由 wa-app 自有 action gateway / gRPC 服务负责。

`CheckLoginState` 由 wa-app 原生服务直接处理。根据 `app-release-re` 中 chatd login payload 的 `passive` / `short_connect` 以及 `last_heartbeat_login`、`wamo_heartbeat` 逆向线索，用原生 chatd 被动短连接握手检测远端登录态。成功刷新 `last_verified_at` 并触发长连接恢复；明确失效置为 `INVALID`；代理/网络不可达只返回 `UNREACHABLE`，不直接吊销本地登录态。

号码探测路径每次生成随机设备指纹但不持久化；配置 `WA_COMMON_PROXY` 时所有 WA 出站链路统一使用该代理，未配置时直连降级。代理始终是可选增强；不做出口 IP、风控、CF 或目标连通性预检，不按 workspace、号码、账号、号码国家或地区直接持有上游代理 lease。

PG/Redis/proxy 都不是 wa-app 启动必需依赖；PG/Redis 缺省使用 `WA_APP_DATA_DIR` 下的 SQLite durable/runtime，proxy 缺省直连，外部自动化仅能作为调用方存在。

## 9. 前端管理边界

WA 管理页是 `wa-app` 自有独立前端，由 `wa-app-service` 在独立域名根路径直接托管，不再通过 dashboard shell 或 module federation 装载。账号管理是默认首屏；零散诊断动作收敛到工具箱，当前只维护单个手机号/SMS 探测输入，不再保留号码池、批量导入或注册前准备页面。前端和 BFF 使用 libphonenumber 元数据解析号码，所有号码输入都要求显式国家拨号码，不按固定国家码表强解。页面不暴露 `job_id`、`request_id`、代理账号、代理地区、动态代理 URL 或具体代理输入，不直接持久化业务状态，只提交号码状态检测请求并展示直连探测返回的必要状态摘要；敏感字段在展示前脱敏。dashboard BFF 的 `/api/wa/phone/sms-probe` 直连 wa-app 原子能力；配置 proxy-runtime 时可通过固定网关用户名选择出口，未配置或不可用时直连。注册能力由 `/api/wa/register` 直连 wa-app 原生编排，不属于工具箱号码池语义；`/api/wa/login-state-check` 和 `/api/wa/long-connections` 为 wa-app 直连接口。
