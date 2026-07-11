# SMSBower API Research

日期：2026-07-11

## 结论

SMSBower 可以接入批量注册链路。它提供与现有 `smsotp.Provider` 对应的号码申请、激活、轮询、完成和取消接口，且 `getNumberV2` 会在成功购号后返回实际运营商。

不过不能只新增一个 adapter 就发布。当前 bulk manager 只持有一个固定的 HeroSMS provider，报价查询、国家目录、购号、轮询、完成与取消都直接调用它。SMSBower 上线需要先改为按用户所选 provider 和任务 provider 路由，才能让 HeroSMS 与 SMSBower 同时可用。

产品决策：批量添加表单提供供应商下拉框，每个任务只允许选择一个供应商。切换供应商时重新加载该供应商的国家和报价，并清空原有报价选择；不同供应商的报价不混排、不比较、不允许组成同一个任务。

本次只执行了不产生购号或状态变更的目录和报价查询；没有调用 `getNumberV2`、`getStatus`、`setStatus`，因此没有消费余额、创建激活或取消号码。

## 官方契约

- 文档：<https://smsbower.app/cn/api?page=client>
- 生命周期 endpoint：`https://smsbower.page/stubs/handler_api.php`
- 所有请求使用 query 参数 `api_key`；支持 GET 或 POST。
- HTTP `200` 不能单独视为成功，平台业务错误会作为字符串响应返回。

| Provider 方法 | SMSBower 请求 | 成功结果 | 处理规则 |
| --- | --- | --- | --- |
| `ListCountries` | `action=getCountries` | 国家目录含 `id`、`eng`、`chn` | 以英文国家名映射 ISO2，再与 1024proxy 支持国家取交集。 |
| `ListOffers` | `action=getServicesList`、`action=getCountries`、`action=getPricesV3&service=wa&country=<id>` | `count`、`price`、`provider_id` | 每个 `provider_id + price` 形成一条报价；内部 country/service/provider 映射缓存。 |
| `AcquireNumber` | `action=getNumberV2&service=wa&country=<id>&maxPrice=<price>&providerIds=<id>&minPrice=<price>` | `activationId`、`phoneNumber`、`activationCost`、`countryCode`、`activationTime`、`activationOperator` | 必须锁定用户选择的报价上游和价格，返回的实际价格不得高于选中的上限。 |
| `MarkReady` | `action=setStatus&status=1&id=<activation>` | `ACCESS_READY` | 短信已准备接收。 |
| `PollCode` | `action=getStatus&id=<activation>` | `STATUS_WAIT_CODE`、`STATUS_WAIT_RETRY`、`STATUS_CANCEL`、`STATUS_OK:<code>` | 仅 `STATUS_OK` 解析验证码；取消状态必须停止 WA 流程。 |
| `Complete` | `action=setStatus&status=6&id=<activation>` | `ACCESS_ACTIVATION` | 仅 WA 注册成功后调用。 |
| `Cancel` | `action=setStatus&status=8&id=<activation>` | `ACCESS_CANCEL` | 仅未完成的激活调用，保留现有 cancel-pending 重试和事件记录。 |

文档还定义 `setStatus=3` 为请求下一条短信。当前批量状态机不支持在同一号码上重新请求 OTP，本次接入不应调用该状态。

## 只读实测

使用运行时环境中的 SMSBower key，仅调用目录和报价接口：

- `getServicesList` 返回成功，服务目录中 WhatsApp code 为 `wa`。
- `getCountries` 返回国家目录；菲律宾解析到内部 country id `4`。
- `getPricesV3(service=wa,country=4)` 返回 `20` 条报价，包含 `count`、`price`、`provider_id`；最低价为 `0.17`，合计可用量为 `39111`。
- 未将 API key、完整请求 URL、余额、手机号、activation id 或原始响应写入日志、计划或本文件。

## 供应商、运营商与报价限制

`getPricesV3` 的 `provider_id` 是 SMSBower 上游渠道标识，不是移动运营商名称。文档没有提供在购号前按真实运营商筛选或将 `provider_id` 映射为运营商的接口。

`getNumberV2` 的 `activationOperator` 才是实际分配后的运营商。因此：

- 用户先在下拉框中选择 `SMSBower`，再单独查看 SMSBower 的国家和报价；不与 HeroSMS 的报价混排。
- SMSBower 报价表应展示为 `渠道 #<provider_id>`，并明确运营商为“待分配”，不能伪装为真实运营商。
- 创建条目时保存报价的 `provider_id` 到 SMSBower 自有 offer id；购号成功后用 `activationOperator` 更新任务条目的 `operator`，让任务详情展示真实运营商。
- SMSBower 不支持当前 HeroSMS 那种在报价阶段选择 `TM`、`Smart` 等真实运营商。用户选择的是价格和 SMSBower 上游渠道。

## 错误与资金风险

- 文档列出 `BAD_KEY`、`BAD_ACTION`、`BAD_SERVICE`、`BAD_COUNTRY`、`NO_ACTIVATION`、`BAD_STATUS`。
- `EARLY_CANCEL_DENIED` 表示取消请求不被当前激活接受。文档的取消窗口描述不够精确，不能假设一定退款或可立即取消。
- adapter 必须把这些业务状态规范化为安全、可分类的错误。错误、日志和 Dashboard 不能包含 API key、完整 URL、activation id、手机号或验证码。
- 对 `EARLY_CANCEL_DENIED` 保留现有 `CANCEL_PENDING` 机制；首版不自动切换供应商或重新购号，避免扩大扣费。
- `activationCost` 必须与任务所选报价一起审计。若返回价格高于报价约束，立即进入取消流程，不向 WA 请求验证码。

## 需要的代码改造

1. 在 `internal/config`、`.env.example`、Docker compose 与服务启动配置中加入 `WA_SMS_BOWER_API_KEY`；不新增 endpoint、国家或 service 的环境变量。
2. 新增 `smsotp.SMSBowerProvider`，将 API 细节、动态目录发现、V3 报价解析、offer id 编解码和错误脱敏隔离在 adapter 内。
3. 将 `bulkRegistrationManager.provider` 改为按 provider 名称查找的注册表，并在 `bulkregistration.Task` 持久化选中的 provider。`ListCountries` 和 `ListOffers` 接收 provider 参数，只查询该供应商；`AcquireNumber`、`MarkReady`、`PollCode`、`Complete`、`Cancel` 始终按任务和条目的固定 provider 路由，不能在任务中途换供应商。
4. Dashboard API 的国家和报价查询增加 provider 参数；创建任务时校验所有报价都属于选中的 provider，拒绝混合选择。切换供应商后前端清空旧报价数量。
5. 国家下拉随所选 provider 单独加载，再与 1024proxy 国家目录求交集。某个供应商的国家或报价失败不影响另一个供应商的可用性。
6. 更新报价和任务界面：SMSBower 显示渠道 id 与“运营商待分配”，购号后显示真实 `activationOperator`；保留 HeroSMS 已有的运营商展示规则。
7. 保持 proto、Dashboard API 的既有响应外形和注册代理边界不变。SMSBower API 请求不经过 WA 注册专用代理。

## 验收与剩余验证

- 单元测试：目录解析、ISO2 映射、V3 报价、渠道锁定、V2 activation 解析、所有状态和错误脱敏。
- manager 测试：供应商切换清空选择、国家与报价不混排、创建任务拒绝混合 provider 选择、按固定 item provider 调度，以及任务恢复和取消不会错路由到另一个 provider。
- 非付费联调：`getServicesList`、`getCountries`、`getPricesV3`。
- 付费联调必须在单独确认后进行：只申请一个号码，验证 `getNumberV2 -> setStatus=1 -> getStatus -> setStatus=8/6`，并记录余额差异和取消结果。
- 上线前用 Docker 构建、`go test ./...`、前端 lint/build、独立生产容器的健康检查和只读 API 验证。
