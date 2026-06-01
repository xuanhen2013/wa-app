# WA n8n 工作流

本目录保存 WA CTF 链路的 n8n 编排定义。编排只串联原子动作，不把 Python 脚本作为运行时桥接；设备指纹、注册请求、登录态等具体能力由 `wa-app` 的原子服务或其 HTTP action gateway 提供。

## 工作流

| 文件 | 触发方式 | 职责 |
| --- | --- | --- |
| `proxy/wa-us-dynamic-ip.workflow.json` | Execute Workflow | 从 proxy-runtime `/leases/acquire` 申请美国随机动态 IP lease，返回工作流内部使用的代理 URL 和对外摘要；不做预检。 |
| `registration/wa-register.workflow.json` | Webhook `POST /wa/register` | 申请动态 IP，生成并提交注册用随机设备指纹，发起 SMS OTP，等待 OTP 回调，提交验证码并持久化登录态；号码探测不进入 n8n。 |

## 运行环境变量

- `WA_ACTION_API_BASE_URL`：WA 原子动作 HTTP 入口，默认指向 `wa-app-service:8080/api/wa/actions`；该入口薄封装 `wa-app` gRPC/服务动作，并负责 PG/Redis 持久化动作。
- `PROXY_RUNTIME_API_BASE_URL`：proxy-runtime HTTP API，动态代理工作流通过 `/leases/acquire` / `/leases/release` 申请和释放美国随机动态 IP lease。

## 输入

### 号码/SMS 检测

号码探测不属于 n8n 编排。前端工具箱对外入口是 `POST /api/wa/phone/sms-probe`，由 wa-app BFF 直连原子能力；该入口要求手机号和国家拨号码，每次探测生成随机设备指纹但不持久化，使用 1 分钟 proxy-runtime 美国随机动态 IP 短租约，用完立即释放。输出包含 `phone_status.account_status`、`phone_status.registered`、`phone_status.blocked`、`phone_status.sms_available`、`phone_status.sms_wait_seconds` 和动态代理摘要，`fingerprint_persistence` 固定为 `RANDOM_NOT_COMMITTED`。

### 注册

`POST /wa/register`：

```json
{
  "workspace_id": "default",
  "phone": "81234567890",
  "country_calling_code": "62"
}
```

注册工作流不执行号码探测。它在动态 IP 可用后调用 `/fingerprints/random` 生成注册用随机设备指纹，再调用 `/fingerprints/commit` 固化到该号码的 `wa_account_id` / `client_profile_id`。随后请求 SMS OTP，并把 `$execution.resumeUrl` 注册到 `/registration/await-otp`。

代理策略细节：

- 号码探测由 wa-app BFF 直连原子能力；若上游传入 `proxy_state_json`，注册工作流复用该动态 IP，否则自行申请独立 proxy-runtime 美国随机动态 IP lease。`wa-app` action gateway 只在内部使用 `proxy_url` 发起 WA 请求。
- 动态 IP 只按 `country_code: "US"` 申请；不做出口 IP、风控、CF 或目标连通性预检，不按 workspace、号码、账号、号码国家或地区绑定代理。
- 终态分支会调用 `/leases/release` 释放本次 lease。
- 最终响应只返回 `{ "proxy_mode": "US_RANDOM_DYNAMIC_IP", "country_code": "US" }` 等非敏感摘要，不返回具体代理 URL 或凭据。

OTP 回调向 n8n wait 节点的 resume URL 发送：

```json
{
  "otp": "123456"
}
```

也兼容 `code` 或 `verification_code` 字段。OTP 是敏感值，action gateway 和 n8n 日志都不得输出明文。

## Action API 约定

工作流当前使用以下动作端点，端点内部可以映射到 `wa-app` gRPC 原子接口：

| 端点 | 用途 |
| --- | --- |
| `POST /proxy-settings` | 返回 WA 美国随机动态 IP 模式摘要；具体代理 URL 只在工作流内部流转。 |
| `POST /fingerprints/random` | 生成随机模拟设备指纹；`persist:false` 时只返回临时引用。 |
| `POST /fingerprints/commit` | 将注册阶段生成的临时指纹提交为号码设备指纹，并返回 `wa_account_id`、`client_profile_id`、`protocol_profile_id`。 |
| `POST /registration/request-sms-otp` | 发起 SMS OTP 请求。 |
| `POST /registration/await-otp` | 记录 n8n resume URL 与等待窗口，便于外部 OTP 采集侧回调。 |
| `POST /registration/resume-otp` | 前端兜底一次性提交 OTP 到当前等待中的注册流程；不写 OTP 历史。 |
| `POST /registration/submit-otp` | 提交 OTP 完成注册。 |
| `POST /registration/persist-login-state` | 注册成功后幂等确认登录态投影，并返回脱敏登录态摘要。 |
| `POST /registration/check-login-state` | 使用原生 chatd 被动短连接握手检测账号远端登录态；可传 `login_state_id`、`registered_identity_id` 或 `wa_account_id`/`client_profile_id`。 |

## 状态边界

- 检测：只使用随机 fingerprint 发起一次探测，返回号码状态，不写长期 profile，不产生可提交的 fingerprint 引用。
- 注册：注册工作流自行生成并 commit 注册用 fingerprint，并把登录态作为长期事实保存；登录态未激活时注册工作流返回失败。登录态激活后由 wa-app 自动启动消息长连接，不需要额外 workflow。
- 登录态检测：远端握手成功会刷新 `last_verified_at` 并触发长连接恢复；明确失效会把登录态置为 `INVALID`；代理/网络不可达只返回 `UNREACHABLE` 检测结果，不把账号直接判失效。
- 等待 OTP、幂等窗口属于短期运行态，应设置 TTL。
- 响应中避免返回 token、authkey、cookie、OTP、可复用请求体、代理凭据等敏感材料。
