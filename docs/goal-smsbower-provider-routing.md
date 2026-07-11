# Goal: SMSBower Provider And Single-Provider Tasks

日期：2026-07-11

## 目标

在不改变 WA 注册专用代理边界的前提下，支持 HeroSMS 与 SMSBower 两个可选短信供应商。批量添加表单先选择供应商，再从该供应商可用且 1024proxy 支持出口的国家中搜索并选择地区，最后加载该供应商的 WhatsApp 报价。

每个批量任务固定一个供应商；报价、号码申请、短信轮询、完成和取消均通过该供应商执行。不同供应商的地区、报价和任务选择不得混合。

## 范围

包含：

- `WA_SMS_BOWER_API_KEY` 的运行时配置和 SMSBower adapter。
- SMSBower 国家、服务、V3 报价、V2 购号、状态、完成与取消映射。
- 单供应商任务持久化和 provider registry 路由。
- Dashboard 的供应商优先选择、地区搜索和切换重置逻辑。
- 单元测试、全量构建、提交和 `whats.example.invalid` 独立容器发布。

不包含：

- 同一个任务混合多个供应商报价。
- 自动跨供应商比价、补号或故障切换。
- SMSBower 购号前按真实移动运营商筛选；其 API 仅在购号后返回实际运营商。
- 调整 WA 注册代理、proto 契约或旧 `wa-app` 容器。

## 设计

### 供应商固定路由

- provider registry 仅注册拥有有效 API key 的 provider。
- `GET /countries?provider=<name>` 和 `GET /offers?provider=<name>&country_iso2=<iso2>` 只查询指定 provider。
- 创建任务请求包含 `provider`；服务端重新读取该 provider 的报价并拒绝任何不属于它的 offer。
- `bulkregistration.Task` 持久化 `provider`，同时保持每条 item 的 `provider` 用于恢复和审计。
- worker 的购号、ready、poll、complete、cancel 都从 item provider 解析 adapter；配置缺失时安全失败，不换到其他供应商。

### SMSBower

- endpoint、服务 code 和国家 id 都在 adapter 内动态发现、缓存；不作为环境变量。
- WhatsApp 服务 code 由 `getServicesList` 发现；国家由 `getCountries` 映射 ISO2；报价用 `getPricesV3`。
- 每个 `provider_id + price` 生成稳定 offer id。`getNumberV2` 同时传 `providerIds`、`maxPrice` 和 `minPrice` 锁定渠道和价格。
- `activationOperator` 只在成功购号后写入任务 item；报价阶段展示渠道 ID 和“运营商待分配”。
- HTTP 200 的业务错误字符串仍按错误处理；日志和 Dashboard 不暴露 API key、完整 URL、activation id、手机号或验证码。

### 表单

1. 供应商下拉：HeroSMS、SMSBower（仅显示配置完成的供应商）。
2. 地区为可搜索下拉，只显示所选供应商可用且 1024proxy 支持的国家。
3. 切换供应商：清空地区、报价、已选数量；重新加载地区。
4. 切换地区：清空报价、已选数量；重新加载报价。
5. 每个任务详情显示固定供应商。SMSBower 报价显示渠道，号码分配后显示实际运营商。

## 测试与验收

- SMSBower adapter 覆盖目录、V3 报价、V2 activation、`setStatus=1/6/8`、状态解析与错误脱敏。
- HeroSMS 回归测试保持通过。
- manager 覆盖 provider 可用性、地区/报价按 provider 隔离、拒绝混合 offer、任务和恢复流程按 item provider 路由。
- 前端覆盖或验证 provider 切换会清空地区、报价与数量，地区搜索可用。
- `go test ./...`、`npm run lint`、Docker Linux build 通过。
- 生产发布只验证健康检查、认证后的只读供应商/地区/报价 API 与任务 API；不创建任务、不购号、不请求 OTP。

## 发布

- 使用提交短 hash 构建 `wa-app:whats-<sha>`，发布到 `wa-app-whats`，保留旧 `wa-app`。
- 服务端 `wa-app.env` 配置 `WA_SMS_BOWER_API_KEY`，仅验证其存在，不输出值。
- 上传镜像前后做 SHA-256 校验，失败自动回滚至之前镜像。
