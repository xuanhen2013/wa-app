# Goal Plan: Registration Proxy Then Bulk Registration

## 背景

本文件是给 `/goal` 使用的总控计划。它把两份细计划串成一个可执行目标：

- `docs/registration-dedicated-proxy-plan.md`
- `docs/bulk-registration-task-plan.md`

执行顺序必须是：先完成注册专用代理，再完成批量注册。代理能力是批量注册的前置底座，批量 worker 不应自己拼 WA 代理。

## Git 更新策略

开做前先检查 fork 和上游是否一致，再从新的 feature branch 开始实现：

```text
git fetch upstream origin --prune
git log --oneline --left-right --cherry-pick origin/main...upstream/main
```

如果上面没有输出，说明 fork 的 `main` 与上游 `main` 已一致，可以直接开分支：

```text
git checkout main
git pull --ff-only origin main
git checkout -b codex/registration-proxy-bulk-registration
```

如果上面显示 `upstream/main` 有新提交，再同步到 fork：

```text
git checkout main
git merge --ff-only upstream/main
git push origin main
git checkout -b codex/registration-proxy-bulk-registration
```

原因：

- 上游可能继续更新注册链路，开做前检查一次可减少后续冲突。
- 实现不直接压在 `main` 上，方便后续继续从 upstream 更新。
- 尽量把新增功能放在新模块和 dashboard BFF 边界内，少改 proto、少改 engine 公共注册参数逻辑。
- 如果必须改共享注册链路，保持改动小而集中，并增加测试，避免后续 upstream 注册逻辑更新时难以 rebase。

## 上游代理分支参考

原仓库存在 `upstream/cliproxy-sticky-proxy` 分支。该分支不是 main 的一部分，但它已经在探索和本计划类似的 WA 注册 sticky proxy 方向：

- 每个手机号生成稳定 sticky sid。
- 按号码国家推导代理地区。
- number probe 先验证出口，再把验证过的出口通过 `egress_pin` 复用到后续注册。
- 对代理出口做 precheck，尽量避开不可用或 datacenter 出口。
- 后续提交已从 cliproxy 迁移到 boltproxy。

执行 Goal 1 前应先阅读该分支相关文件，吸收实现思路：

- `internal/waapp/bff/boltproxy_proxy.go`
- `internal/waapp/bff/boltproxy_precheck.go`
- `internal/waapp/bff/wa_proxy_resolver.go`
- `internal/waapp/bff/number_probe.go`
- `internal/waapp/bff/registration_orchestrator.go`
- `internal/waapp/bff/wa_proxy_route.go`

不要直接 merge 该分支作为本计划实现方式。原因：

- 它是 boltproxy 专用配置和 username 拼接模式；本计划优先实现 1024proxy，节点由 API 动态返回 `host` / `port`。
- 它包含代理以外的注册、长连接、前端部署、安全输入等改动，直接 merge 会扩大冲突面。
- 更稳妥的方式是保留本计划的 source adapter 抽象，把 sticky sid、probe precheck、egress pin、route summary 等设计吸收进 1024proxy adapter 和注册 resolver。

## Goal 1: 实现和自测

目标：一次性完成注册专用代理和批量注册功能，并在本地完成可行的代码级、模拟级和连通性自测。

不要做：

- 不上线部署。
- 不连接生产服务器。
- 不读取或输出 `.env` 中的 SSH 私钥、密码、token、API key 等敏感值。
- 不把真实 OTP、代理凭据、短信平台 key、完整代理 URL 写入文档、日志或 dashboard 响应。
- 不修改 proto 来暴露供应商 endpoint、代理地址、数据库表名、Redis key 或其他实现细节。

### 执行顺序

1. 代理底座
   - 实现注册专用代理配置加载。
   - 实现注册代理 source adapter 抽象。
   - 实现 1024proxy source adapter。
    - 1024proxy 稳定参数内置到 adapter：endpoint、`num=1`、`format=1`、`type=json`、返回节点协议 `http`。
    - 提取 API 的 `region` 固定为 `Rand`；注册号码国家只写入最终代理用户名的 `region-{country}`，例如菲律宾号码使用 `region-PH`。
    - `time` 使用 `WA_REGISTRATION_PROXY_STICKY_MINUTES`。
    - 在发出任何 WA 请求前校验候选节点的出口国家；不匹配或不可用时默认拒绝。
    - 只把最终代理 URL 交给 `engine.NativeEngine.WithProxyURL(...)`。
   - 不把链式代理做成正式配置。

2. 注册链路接入
   - 在 `registrationRunner` 接入注册专用代理 resolver。
   - 请求 OTP 和提交 OTP 复用同一个 sticky session。
   - 注册专用代理仅作用于注册请求和提交 OTP。
   - 非注册链路继续保持现有 `WA_COMMON_PROXY` 行为。
   - dashboard 只返回脱敏 route summary，不返回真实代理 URL 或凭据。

3. 代理自测
   - 单元测试覆盖地区推导、1024proxy URL 拼接、脱敏输出、sticky session 复用。
   - 本地测试环境因 1024proxy ban 中国 IP，连通性探针可在测试命令层使用 `socks5://127.0.0.1:9981` 做链式代理。
   - 链式代理仅用于本地测试命令或 test harness，不进入正式服务配置。
   - 本地可验证：1024proxy API 能返回节点，返回节点按 HTTP proxy 可访问 HTTPS 测试目标。

4. 短信供应商底座
   - 实现 SMS provider 抽象。
   - 实现 HeroSMS provider。
   - HeroSMS `getCountries` 仅在服务端使用 API key 调用；将可见国家解析为 ISO2 并缓存，内部 country id 不进入 Dashboard。
   - HeroSMS 报价优先使用 `/api/v1/activations/offers`。
   - HeroSMS 号码生命周期使用 `https://hero-sms.com/stubs/handler_api.php` 兼容接口。
   - 短信平台 API 不走 WA 注册专用代理。

5. 批量任务后端
   - 实现全局最多一个 active bulk registration task。
   - 实现任务、条目、短信事件存储。
   - 实现国家列表、报价查询、提交任务、查询任务、取消任务 API。
   - 国家列表必须是 HeroSMS 可见国家和 1024proxy 国家级出口目录的交集；`Rand` 和州/省不作为可选注册地区，直接 API 请求非交集国家也必须拒绝。
   - worker 按每个任务保存的并发数执行；表单默认目标数量的三分之一，历史任务按 `1` 恢复。
   - 单条流程：报价选择、申请号码、WA probe、请求 OTP、轮询短信、提交 OTP、完成/取消短信激活。
   - 失败时尽早取消短信号码，降低扣费风险。

6. 批量任务 UI
   - 在添加账号页面通过“单个添加 / 批量添加”Tab 切换；左侧栏只保留添加账号入口。
   - 再次打开批量添加时，如果已有 active task，显示当前任务而不是新建表单。
   - 第一版供应商固定为 HeroSMS，支持选择服务端返回的国家、价格档、数量；切换国家后重新查询该国报价和库存。
   - 展示任务进度、条目状态、短信状态、WA 状态、错误原因和取消按钮。

7. 最终本地验证
   - `go test ./...`
   - `npm run lint`，工作目录 `webui`
   - `npm run build`，工作目录 `webui`
   - `docker build -t wa-app:goal-local .`
   - 本地启动服务并访问 `/healthz` 或 `/api/wa/health`。
   - 如供应商 key 可用，做短信平台报价/国家/服务 code 查询验证。
   - 如本地网络受限，真实 WA 注册冒烟留到 Goal 2 的海外部署环境完成。

### Goal 1 配置面

建议只暴露这些注册代理配置：

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

建议只暴露这些批量注册配置：

```env
WA_BULK_REGISTRATION_ENABLED=false
WA_BULK_REGISTRATION_MAX_ITEMS=100
WA_BULK_REGISTRATION_CONCURRENCY=100
WA_HERO_SMS_API_KEY=
```

批量注册其余稳定参数由 provider adapter 内置或自动发现：

- HeroSMS endpoint 不作为默认环境变量暴露。
- WhatsApp service code、country id、可见国家目录、报价/库存字段映射自动发现并缓存；国家下拉只返回 HeroSMS 与 1024proxy 的 ISO2 交集。
- provider 是否启用由对应 API key 是否存在决定。
- 短信等待超时、轮询间隔、取消重试次数先使用代码默认值；真实运营需要调参时再加配置。
- 短信平台 API key 只放环境变量或部署 secret，不进入 proto 和前端 bundle。

### Goal 1 完成标准

- 注册专用代理和批量注册代码已实现。
- 单元测试和本地构建通过，lint 为 0 error / 0 warning。
- 关键异常路径有测试：代理不可用、无号码、请求 OTP 失败、短信超时、用户取消、取消短信平台号码失败。
- dashboard 不展示真实代理 URL、代理密码、短信平台 API key、OTP 明文。
- 本地测试记录说明哪些是真实供应商连通性验证，哪些是 fake provider / fake WA 注册链路验证。
- 未执行线上部署。

## Goal 2: Docker 上线部署

目标：在 Goal 1 完成后，用新的 `/goal` 单独部署到线上。

已知部署条件：

- 新域名：`whats.example.invalid`。
- 域名解析和 nginx 反向代理已完成。
- nginx 已反向代理到宿主机 `127.0.0.1:4399`。
- 使用 Docker 部署。
- `.env` 下有服务器 SSH 配置信息，可在部署 goal 中读取使用，但不得输出敏感值。

部署建议：

- Docker 容器内部继续使用默认 dashboard 端口 `8080`。
- 宿主机只绑定本地地址：`127.0.0.1:4399:8080`。
- 不把 dashboard 端口绑定到 `0.0.0.0`。
- 除非明确需要远程 gRPC，不映射 `50091` 到公网。
- 持久化 `/var/lib/wa-app`。
- 通过 env file 或 Docker secret 注入生产配置。

Goal 2 验收：

- 容器健康运行。
- `https://whats.example.invalid/healthz` 或 `https://whats.example.invalid/api/wa/health` 返回健康结果。
- dashboard 可打开。
- 生产环境没有测试链式代理配置。
- 1024proxy 的 `white` 节点提取 API 从生产服务器返回 HTTP `200` 和非空节点；在此之前批量 worker 的代理预检必须拒绝购号。
- 在海外服务器上做 1 个小流量真实注册代理冒烟。
- 再做批量任务 1 个号码的小批量冒烟。
- 冒烟成功后，再允许尝试 10 个号码任务。

### Goal 2 实测记录（2026-07-10）

- `wa-app-whats` 独立容器已部署到宿主机 `127.0.0.1:4399`，由 `whats.example.invalid` 反向代理；原 `wa-app` 实例、端口和数据卷未改动。
- 生产环境无测试链式代理配置；公开健康检查通过，Dashboard 登录保护与 HeroSMS 菲律宾报价可用。
- 1024proxy 固定以 `region=Rand` 提取接入节点，再以 `<username>-region-{country}-sid-{session_id}-t-{sticky_minutes}` 认证模板完成真实 `PH` 出口预检；worker 在购号前先做预检，预检失败不会申请 HeroSMS 号码。
- 已完成一个菲律宾单号真实冒烟：HeroSMS 成功分配号码，WA 验证请求经专用 PH 出口发出，WA 返回 `blocked`；worker 已将 HeroSMS 激活取消，最终状态为 `STATUS_CANCEL`。未收到 OTP，未产生注册成功账号。
- 因本次真实号码被 WA 风控拒绝，后续 10 号码任务须在人工确认成本与接受率后另行发起。
- 同日新增批量国家目录验证：服务端以 HeroSMS `getCountries` 的可见国家和 1024proxy 国家级目录取交集，线上接口返回 178 个当前可选国家；确认包含 `PH`、`US`，不包含 `Rand` 和州/省。该验证未购号、未请求 OTP、未发起 WA 注册。

### Goal 2 并发发布记录（2026-07-10）

- 已发布提交 `5ad52b4` 到独立容器 `wa-app-whats`，运行镜像为 `wa-app:whats-5ad52b4`；旧 `wa-app` 容器和数据卷未变更。
- 生产 `wa-app.env` 已设置 `WA_BULK_REGISTRATION_MAX_ITEMS=20` 与 `WA_BULK_REGISTRATION_CONCURRENCY=20`。
- 宿主机 `127.0.0.1:4399/healthz`、公网 `https://whats.example.invalid/healthz`、认证后的 Dashboard 和批量任务只读 API 均返回成功；API 确认 `max_items=20`、`max_concurrency=20`，且没有 active task。
- 本次仅进行部署和只读验证，未创建批量任务、未购号、未请求 OTP。

### Goal 2 失败日志发布记录（2026-07-10）

- 已发布提交 `6c4032e` 到独立容器 `wa-app-whats`，运行镜像为 `wa-app:whats-6c4032e`。
- 批量任务 API 新增 active task 的 `events`，以及终态任务的 `last_task`、`last_items`、`last_events`；Dashboard 会在新建表单下展示最近完成任务的事件日志。
- 线上持久化库验证到最近终态任务的 `10` 个条目和最近 `100` 条事件，说明旧失败任务的记录可被新版本直接回放；其中 `71` 条带根因的事件均归类为 `wa_blocked`，取消事件是 WA 拒绝后的清理状态，不是首要失败根因。
- 本次仅读取既有任务和事件，未创建新任务、未购号、未请求 OTP。

### Goal 2 批量容量与运营商标识发布记录（2026-07-10）

- 已发布提交 `538de8f` 到独立容器 `wa-app-whats`，运行镜像为 `wa-app:whats-538de8f`；原 `wa-app` 服务保持运行原有 `ghcr.io` 镜像。
- 批量任务的目标数量和部署并发上限均调整为 `100`；表单仍默认按目标数量的三分之一设置并发，用户可以在部署上限内调整。
- Dashboard 报价列表和任务条目列表显示“供应商 - 运营商”；运营商会从 HeroSMS 报价保留到申请号码时使用的任务条目。
- 报价列表下方的“提交任务”按钮固定在批量添加表单底部，长列表滚动时仍可直接提交。
- 宿主机与公网 `https://whats.example.invalid/healthz` 均通过；认证后的只读批量任务 API 确认 `max_items=100`、`max_concurrency=100`。本次未创建任务、未购号、未请求 OTP。

### Goal 2 HeroSMS 运营商报价修正（2026-07-10）

- 初版 HeroSMS adapter 将 V1 报价的运营商硬编码为 `any`，且申请号码没有传递 `operator`；因此 Dashboard 无法区分运营商，供应商也不会收到筛选条件。
- 修正后通过 `getOperators` 读取国家级运营商列表，为每个运营商生成可选报价，并使用 `getNumberV2&operator=<code>` 申请号码；菲律宾只读验证到 `tm`、`globe_telecom`、`smart`、`dito`。
- 同一价格档的库存按共享总量进行前后端校验，不会因运营商展开而重复计算库存。实际分配的 `activationOperator` 会写回任务条目。
- 已发布提交 `505f423` 到独立容器 `wa-app-whats`，运行镜像为 `wa-app:whats-505f423`；公网健康检查和认证后的菲律宾报价接口均通过，返回 `104` 条按价格档和运营商展开的报价。本次未创建任务、未购号、未请求 OTP。

## 推荐 /goal 文案

Goal 1:

```text
按 docs/goal-registration-proxy-bulk-registration.md 的 Goal 1 完成注册专用代理和批量注册实现，并完成本地自测。不要上线部署，不要读取或输出 .env 中的 SSH 和密钥敏感值。
```

Goal 2:

```text
在 Goal 1 已完成且自测通过后，按 docs/goal-registration-proxy-bulk-registration.md 的 Goal 2 使用 Docker 部署 wa-app 到 whats.example.invalid。nginx 已反向代理 127.0.0.1:4399，可读取 .env 中的服务器 SSH 配置，但不得输出任何敏感值。
```
