# wa-app

`wa-app` 是 WA 应用链路的应用层服务。它把 WA 应用链路实现为 Go 原生原子 gRPC 接口，并优先用 PG/Redis 管理服务状态；未配置时降级到本服务 SQLite。`wa-re` / `app-release-re` 仅作为已验证参考材料，不作为运行时桥接脚本。

> [!CAUTION]
> 使用本项目即表示你同意 [NOTICE](./NOTICE) 的全部条款。本项目仅限协议建模、教学演示、授权安全研究和内部非商业验证；禁止用于任何商业用途、盈利服务、客户交付、SaaS/托管服务、代运营、账号注册/交易/代收码、营销自动化、灰产/黑产服务、未授权目标或违反第三方服务条款的场景。一切法律、合规和安全责任由使用者自行承担。

## 边界

- 不修改 `wa-re/` 和 `app-release-re/` 下的脚本与材料，也不通过进程桥接这些脚本。
- Proto 是 RPC、状态、错误码和事件语义的真源。
- PG 存长期事实：WAAccount、client profile、注册记录、消息元数据、解密/候选值投影。
- 注册成功后的登录态以 `LoginState` 投影保存；敏感登录材料只保存在受控 profile/state 工作区或 TTL 运行态。
- Redis 存 TTL 运行态：幂等窗口、消息会话 lease；未配置 Redis 时使用本服务 SQLite runtime。
- OTP、Flag、token、authkey、key/session 等敏感值不写普通日志；PG 中只保存引用或脱敏值。

## 目录

- `proto/byte/v/forge/waapp/v1/`：服务契约。
- `internal/app/`：服务编排、PG store、Redis runtime、Go 原生协议 engine、detached tooling。
- `cmd/wa-app-service/`：gRPC 服务入口。
- `migrations/001_init.sql`：PG 表结构。
- `docs/modeling.md`：动作拆分和状态边界说明。
- `webui/`：WA 管理 dashboard 远程模块。
- `NOTICE`：非商业使用须知与免责声明。

Go/TypeScript proto 生成物不入库；需要由 `scripts/generate-proto.sh` 和 `webui` 的 `npm run proto` 从 `proto/` 真源生成。

## 运行配置

运行配置均为可选；未配置 PG/Redis/proxy-runtime 时服务仍可用，分别降级到 SQLite durable store、SQLite runtime 和直连网络。

可选：

- `WA_APP_LISTEN_ADDR`，默认 `:50091`
- `WA_APP_DASHBOARD_HTTP_ADDR`，默认 `:8080`
- `WA_APP_DASHBOARD_STATIC_DIR`，默认 `/app/dashboard/wa`
- `WA_APP_PG_DSN`，未配置时使用 `WA_APP_DATA_DIR` 下的 SQLite durable store
- `WA_APP_REDIS_URL`，未配置时使用 `WA_APP_DATA_DIR` 下的 SQLite runtime
- `WA_APP_DATA_DIR`，默认 `/var/lib/wa-app`
- `WA_APP_PROXY_RUNTIME_API_BASE_URL`，配置后读取 proxy-runtime 固定网关 IN-USER 规则并拼装业务侧代理 URL；未配置或不可用时走直连降级
- `WA_APP_PROXY_RUNTIME_LOCAL_PROTOCOL`，本地固定网关协议，默认 `socks5`
- `WA_LONG_CONNECTION_PROXY_USERNAME`，长连接使用的固定网关用户名，默认 `whatsapp`
- `WA_NUMBER_PROBE_PROXY_USERNAME`，号码/SMS 检测使用的固定网关用户名，默认 `whatsapp-probe`
- `WA_REGISTRATION_PROXY_USERNAME`，注册 OTP 流程使用的固定网关用户名，默认 `whatsapp-reg`
- `WA_ACCOUNT_SETTINGS_PROXY_USERNAME`，账号设置动作使用的固定网关用户名，默认 `whatsapp-reg`
- `WA_LOGIN_STATE_CHECK_PROXY_USERNAME`，登录态检测使用的固定网关用户名，默认 `whatsapp-reg`

协议 endpoint、WASafe 公钥、WA app 版本、User-Agent、ChatD host/TLS/timeout、请求 timeout、本地 profile 目录和设备指纹策略都内置在 wa-app 原生 engine 中，不再作为运行时环境变量暴露；号码探测/注册可通过 proxy-runtime 固定网关用户名选择出口，proxy-runtime 缺失时不阻塞服务。

## 原子 RPC

- Discovery：登记 app artifact、记录协议能力 profile。
- Profile：创建 WAAccount、准备/查询/退役客户端 profile。
- Registration：账号探测、请求验证码、提交验证码、查询注册记录。
- LoginState：查询注册成功后的活动登录态；`CheckLoginState` 会用原生 chatd 被动短连接握手检测远端登录态，并在消息会话打开前校验该登录态属于对应 WAAccount/client profile。
- Messaging：打开消息会话、批量接收消息、ack、关闭会话、查询长连接状态；注册/登录态检测成功后会自动恢复消息长连接。
- Extraction：解密消息、提取 OTP/Flag 候选值；长连接收到 OTP 后写入 wa-app 自有 OTP 历史。
- Tooling：生成 detached phone profile、导入 WAMSYS 捕获、构造注册请求材料、WASafe ENC、APK token/authkey 派生。

## 外部自动化边界

当前 WA 主链路不保留外部 workflow 定义，也不依赖外部编排器才能启动。号码探测、注册、OTP 等待、OTP 提交、登录态检测和长连接恢复均由 `wa-app-service` 原生 HTTP/gRPC 能力处理，不再通过 proxy-runtime lease API 申请代理。

登录态检测是 wa-app 直连接口：`POST /api/wa/login-state-check`。

## 前端管理页

`webui/` 构建为 WA 独立前端，由 `wa-app-service` 直接托管在独立域名根路径 `/`。页面包含：

- 账号管理：WAAccount 列表、分页和添加入口是默认首屏。
- 工具箱：输入手机号和国家拨号码执行轻量号码探测。
- 号码探测动作：调用 `/api/wa/phone/sms-probe`，由 wa-app dashboard BFF 直连 wa-app 原子能力；每次探测生成随机设备指纹但不持久化；配置 proxy-runtime 时通过固定网关用户名选择出口，未配置或不可用时走直连降级。
- 号码解析使用 libphonenumber 元数据，不维护固定国家码表，也不强解未规范化号码。
- action gateway：`/api/wa/actions/*` 由 wa-app 提供，负责指纹临时态、注册 OTP、登录态持久化和登录态远端检测动作；它是可被外部自动化调用的本服务 HTTP 边界，不要求任何外部编排器存在。
- 注册流程由 `POST /api/wa/register` 直连 wa-app 原生编排；OTP 请求成功进入等待态后才持久化账号。
- 后端自动补齐：`job_id`、`request_id`；检测号码和检测 SMS 可发送可使用 proxy-runtime 固定网关 IN-USER 规则；未配置时直连，不暴露也不持久化上游代理账号、代理国家/地区或具体代理绑定。
- 长连接状态：号码详情会展示当前长连接、chatd ping 心跳、最近心跳和最近消息时间。

前端展示会脱敏 token、OTP、cookie、session、auth/key 等字段。

## 本地验证

当前仅做非构建类验证：

```bash
wa-app/scripts/generate-proto.sh
protoc -I wa-app/proto --descriptor_set_out=/tmp/wa-app.pb $(find wa-app/proto -name '*.proto' | sort)
(cd wa-app && go list ./...)
(cd wa-app && go vet ./...)
(cd wa-app/webui && npm run proto && npm run lint)
```

不要在 Mac 本机执行业务构建、镜像构建或部署验证。
