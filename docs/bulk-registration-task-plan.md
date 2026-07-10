# Bulk Registration Task Plan

## 背景

当前 dashboard 支持单个 WA 账号添加：输入手机号、检测验证通道、请求短信验证码、等待 OTP、提交 OTP、注册成功后账号进入 ACTIVE。

下一步需要支持批量添加账号。例如用户选择菲律宾地区、目标数量 10，然后系统从短信平台自动申请号码，走项目内已有 WA 注册链路，自动等待短信验证码并提交 OTP，最终批量添加成功。

第一版只接入 HeroSMS。SMSBower 暂不实现，后续如需要再作为新的 SMS provider 扩展。

## 目标

- 在“添加账号”入口旁增加“批量添加”入口。
- 支持选择地区、HeroSMS 价格档和数量。
- 支持查看 HeroSMS 的 WhatsApp 号码价格和库存数量。
- 支持自动选择最低价号码，也允许用户手动选择价格档和数量。
- 支持提交一个批量添加任务，后端自动执行：
  - 申请短信平台号码。
  - 使用项目现有注册链路请求 WA 短信验证码。
  - 等待短信平台收到验证码。
  - 自动提交 OTP。
  - 注册成功后完成短信平台激活。
- 支持查看任务进度、条目状态、错误原因。
- 支持取消任务。
- 全局同一时间只允许存在一个未结束批量任务；再次打开批量添加入口时显示当前任务。
- 遇到不可注册、封禁、限流、无路由、短信超时等异常时尽早取消短信平台号码，减少扣费风险。

## 国家选择与代理地区

批量添加的国家下拉不使用前端硬编码，也不直接暴露任一供应商的完整响应。运行时由服务端完成以下步骤：

1. 使用服务端 HeroSMS API key 调用 `getCountries`，只保留 `visible=1` 的国家记录。
2. 用 HeroSMS 返回的英文国家名称解析 ISO2，并与 1024proxy 的国家级出口目录取交集。
3. 将脱敏后的 `{ country_iso2, name }` 列表返回 Dashboard；HeroSMS 内部 country id 和 API key 不进入浏览器。
4. 选中国家后才查询 WhatsApp 报价和库存；无报价、无库存或不在交集内时不能提交任务。

`Rand` 是 1024proxy 的接入节点提取模式，不是注册目标国家，必须排除。1024proxy 返回的州/省信息也不进入第一版 UI：短信号码、HeroSMS country id 和注册代理用户名均以国家级 ISO2 为边界。国家目录在 provider 侧缓存 5 分钟，避免反复请求供应商。

## 非目标

- 不直接在前端循环调用单个添加流程。
- 第一版不实现 SMSBower；后续需要时再新增 provider。
- 不把 OTP 明文落库或写日志。
- 不在 proto 契约中暴露短信平台 API key、供应商 endpoint、代理地址、数据库表名或 Redis key。
- 不保证平台一定退款；系统只能根据平台规则尽早调用取消，并记录取消结果。
- 不把短信平台 API 请求混入 WA 注册专用代理。短信平台 API 和 WA 注册出口代理是两个独立边界。

## 当前代码入口

单个添加相关入口：

- 前端单个添加组件：`webui/src/dashboard/wa-account-add.tsx`
- 添加页面 route：`webui/src/dashboard/wa-page.tsx`
- 前端 API 封装：`webui/src/dashboard/wa-api.ts`
- 后端注册入口：`cmd/wa-app-service/dashboard_http.go` 的 `/api/wa/register`
- BFF 注册编排：`internal/waapp/bff/registration_orchestrator.go` 的 `StartRegistration`
- 提交 OTP：`internal/waapp/bff/action_gateway.go` 的 `submitOTP` / `resumeOTP`
- 清理失败注册账号：`internal/waapp/bff/action_gateway.go` 的 `cleanupFailedRegistration`

WA 状态模型：

- `AccountProbeStatus`
  - `UNKNOWN`
  - `REACHABLE`
  - `UNREACHABLE`
  - `REJECTED`
- `VerificationRequestStatus`
  - `REQUESTED`
  - `SENT`
  - `WAITING`
  - `REJECTED`
  - `EXPIRED`
- `RegistrationStatus`
  - `PENDING_CODE`
  - `SUBMITTED`
  - `REGISTERED`
  - `REJECTED`
  - `EXPIRED`
- `WAAccountStatus`
  - `PENDING_REGISTRATION`
  - `ACTIVE`
  - `PAUSED`
  - `ARCHIVED`
  - `TRANSFERRED_OUT`

批量任务不要直接复用这些状态作为任务状态。任务需要自己的状态机，然后映射 WA 和短信平台状态。

## 短信平台协议

### SMSBower

暂不实现。以下内容仅作为后续扩展参考，不进入 Goal 1 范围。

文档地址：`https://smsbower.app/cn/api?page=client`

基础 endpoint：

```text
https://smsbower.page/stubs/handler_api.php
```

关键接口：

- `getBalance`
- `getCountries`
- `getServicesList`
- `getPrices`
- `getPricesV2`
- `getPricesV3`
- `getNumber`
- `getNumberV2`
- `getStatus`
- `setStatus`

号码申请：

```text
action=getNumberV2&service=<service>&country=<country>&maxPrice=<maxPrice>&providerIds=<providerIds>
```

价格：

```text
action=getPricesV3&service=<service>&country=<country>
```

等待短信：

```text
action=getStatus&id=<activation_id>
```

常见返回：

- `STATUS_WAIT_CODE`：等待短信。
- `STATUS_WAIT_RETRY:<lastCode>`：等待下一条短信。
- `STATUS_CANCEL`：激活被取消。
- `STATUS_OK:<code>`：收到 OTP。

修改状态：

- `setStatus=1`：号码准备好接收短信。
- `setStatus=3`：请求另一条短信。
- `setStatus=6`：完成激活。
- `setStatus=8`：取消激活。

取消可能失败：

- `EARLY_CANCEL_DENIED`：购买后短时间内不能取消，需要延迟重试。
- `NO_ACTIVATION`：平台找不到激活 ID，按不可恢复错误处理。

### HeroSMS

文档来源：

- `https://hero-sms.com/api`
- `https://hero-sms.com/cn/api`
- 本地 OpenAPI：`C:/Users/admin/Downloads/api___cn.json`

HeroSMS OpenAPI 明确提供两个 server：

```text
https://hero-sms.com/api/v1
https://hero-sms.com/stubs/handler_api.php
```

鉴权方式：

- 新版 `/api/v1` 使用 header：`Authorization: ApiKey <token>`。
- SMS-Activate 兼容接口使用 query：`api_key=<token>`。

建议接入策略：

- 报价优先使用新版 `GET /activations/offers`，因为它直接返回按服务和国家分组的报价、库存和价格档。
- 号码生命周期仍优先使用 SMS-Activate 兼容接口。
- HeroSMS 新增的 `cancelActivation` / `finishActivation` 可作为取消和完成的优先接口；兼容的 `setStatus=8/6` 作为 fallback。
- OpenAPI 中 `getNumberV2`、`getStatus`、`cancelActivation`、`finishActivation` 等 action 操作带有 operation-level server，明确指向 `https://hero-sms.com/stubs/handler_api.php`。除非 HeroSMS 后续提供完整的 V1 购买/状态/取消/完成接口，否则不能只保留 `/api/v1` base。

连通性验证：

- 2026-07-09 手动请求 `https://hero-sms.com/stubs/handler_api.php`，无 `action` 返回 HTTP `422`，错误字段为 `action` required，说明入口可达且在做参数校验。
- 请求 `?action=getBalance` 在未传 API key 时返回 HTTP `422`，错误字段为 `api_key` required，说明 action 路由可用，凭证校验生效。
- 请求 `?action=getCountries` 返回 HTTP `200` 和国家列表；其中 Philippines / 菲律宾返回的 country id 为 `4`，可作为人工覆盖或启动期自动发现校验值。
- 这只能证明兼容接口连通和无凭证公开查询可用；真实的 `getNumberV2`、`getStatus`、`cancelActivation`、`finishActivation` 还需要 HeroSMS API key 和可扣费测试余额做完整链路验收。

新版报价接口：

```text
GET https://hero-sms.com/api/v1/activations/offers?services=<service>&countries=<country>
Authorization: ApiKey <token>
```

参数：

- `services`：服务代码。
- `countries`：国家 ID。

返回结构：

```json
{
  "data": {
    "<service>": {
      "<country_id>": {
        "prices": {
          "default": 0.15,
          "retail": 0.15,
          "min": 0.15
        },
        "counts": {
          "total": 22598,
          "physical": 12352,
          "defaultPrice": 4787
        },
        "map": {
          "0.1500": 286896,
          "0.1765": 327454
        }
      }
    }
  }
}
```

`map` 是价格到数量的映射，可直接用于批量报价表的“价格档 + 数量”选择。

SMS-Activate 兼容 endpoint：

```text
https://hero-sms.com/stubs/handler_api.php
```

关键接口：

- `getBalance`
- `getCountries`
- `getServicesList`
- `getPrices`
- `getOperators`
- `getNumber`
- `getNumberV2`
- `getStatus`
- `getStatusV2`
- `getAllSms`
- `setStatus`
- `cancelActivation`
- `finishActivation`

号码申请：

```text
action=getNumberV2&service=<service>&country=<country>&operator=<operator>&maxPrice=<maxPrice>&fixedPrice=<0|1>
```

参数：

- `service`：必填，服务代码。
- `country`：必填，国家 ID。
- `operator`：可选，运营商列表，逗号分隔。
- `maxPrice`：可选，最高价格。
- `fixedPrice`：可选，和 `maxPrice` 搭配使用，要求严格按指定最高价购买。
- `ref`：可选，推荐标识。
- `phoneException`：可选，排除号码前缀，最多 20 个。

成功返回字段：

```json
{
  "activationId": "635468024",
  "phoneNumber": "79584******",
  "activationCost": 12.5,
  "currency": 840,
  "countryCode": 6,
  "countryPhoneCode": 62,
  "canGetAnotherSms": true,
  "activationTime": "2026-02-19T00:11:33+08:00",
  "activationEndTime": "2026-02-19T02:11:23+08:00",
  "activationOperator": "any"
}
```

注意：

- `phoneNumber` 不带 `+`。
- `currency` 是 ISO 数字货币代码，文档枚举包含 `840`、`978`、`156`。
- `canGetAnotherSms=true` 时，OTP 提交失败可考虑请求下一条短信。

等待短信：

```text
action=getStatus&id=<activation_id>
```

常见返回：

- `STATUS_WAIT_CODE`：等待短信。
- `STATUS_WAIT_RETRY:<lastCode>`：等待验证码确认或下一步。
- `STATUS_WAIT_RESEND`：等待重新发送短信。
- `STATUS_CANCEL`：激活已取消。
- `STATUS_OK:<code>`：收到 OTP。

结构化状态：

```text
action=getStatusV2&id=<activation_id>
```

返回可能包含：

- `sms.code`
- `sms.text`
- `call.code`
- `call.text`

`getStatusV2` 和 `getAllSms` 只作为兜底诊断或重发场景使用；仍不得保存短信全文或 OTP 明文。

修改状态：

```text
action=setStatus&id=<activation_id>&status=<status>
```

状态：

- `1`：号码准备好接收短信。
- `3`：请求另一条短信。
- `6`：完成激活。
- `8`：取消激活。

成功返回：

- `ACCESS_READY`
- `ACCESS_RETRY_GET`
- `ACCESS_ACTIVATION`
- `ACCESS_CANCEL`

新版完成/取消接口：

```text
action=finishActivation&id=<activation_id>
action=cancelActivation&id=<activation_id>
```

成功时返回 HTTP `204`。建议优先使用这两个接口，失败时根据错误决定是否 fallback 到 `setStatus=6/8`。

重要错误：

- `NO_NUMBERS`：无可售号码。
- `WRONG_MAX_PRICE` 或 JSON `title=WRONG_MAX_PRICE`：最高价过低，JSON `info.min` 给出最低价。
- `NO_BALANCE`：余额不足。
- `BAD_KEY` / `NO_KEY`：鉴权失败。
- `BANNED`：账号被临时封锁，JSON 可能包含 `scope`、`retry_after_seconds`、`readable_date`。
- `CHANNELS_LIMIT`：账号达到通道限制。
- `SERVICE_NOT_AVAILABLE`：该地区/服务不可售。
- `SIM_OFFLINE`：号码不再可用。
- `SIM_TEMPORARY_OFFLINE`：号码暂时不可用，可稍后重试。
- `EARLY_CANCEL_DENIED`：前 2 分钟无法取消号码，需要延迟重试。
- `FREE_CANCELLATION_EXPIRED`：免费取消时间已过。
- `OTP_RECEIVED`：已收到 OTP，无法取消。
- `NEW_OTP_RECEIVED`：已有新 OTP，需要确认后才能终止或完成。
- `ACTIVATION_NOT_ACTIVE`：激活已经终止或退款。
- `NOT_FOUND` / `NO_ACTIVATION`：激活不存在。

接入前仍需用真实 API key 做一次联调，确认：

- WhatsApp 服务 code。
- 菲律宾 country id。
- `GET /api/v1/activations/offers` 在 `services=wa,countries=<PH id>` 下的真实返回。
- `cancelActivation` / `finishActivation` 是否可覆盖所有成功和失败场景。

## 服务 code 与国家映射

不要硬编码 WhatsApp 服务 code 或菲律宾 country id。

启动或首次查询时：

1. 调用每个平台 `getServicesList`。
2. 根据服务名匹配 WhatsApp，得到平台 service code。
3. 调用 `getCountries`。
4. 根据 `eng`、`chn`、ISO 映射匹配 Philippines / 菲律宾，得到平台 country id。
5. HeroSMS 报价可用 `GET /api/v1/activations/offers` 校验 service/country 组合是否真实有库存。
6. 缓存映射，保留人工配置覆盖。

第一版不建议把 service code、country id 做成默认环境变量。它们应由 provider adapter 在启动或首次查询时自动发现并缓存；如果平台规则变化，优先修正 adapter 或测试 harness，而不是增加运维配置面。

## 配置建议

短信供应商 API key 只放服务内环境变量，不进入 proto。

```env
WA_BULK_REGISTRATION_ENABLED=false
WA_BULK_REGISTRATION_MAX_ITEMS=20
WA_BULK_REGISTRATION_CONCURRENCY=20

WA_HERO_SMS_API_KEY=
```

说明：

- `WA_BULK_REGISTRATION_ENABLED` 是总开关。
- `WA_BULK_REGISTRATION_MAX_ITEMS` 是单个任务目标数量的部署上限，最大为 `20`；`WA_BULK_REGISTRATION_CONCURRENCY` 是用户可选并发的部署上限，默认同为 `20`。
- HeroSMS 是否启用由 API key 是否存在决定，不再单独暴露 provider enabled 开关。
- HeroSMS `/api/v1` 报价 endpoint、HeroSMS `handler_api.php` 生命周期 endpoint 都由 provider adapter 内置。
- WhatsApp service code、country id、报价/库存字段映射由 provider adapter 自动发现和缓存。
- 短信等待超时、轮询间隔、取消重试次数先使用代码内默认值：等待 `1200s`、轮询 `5s`、取消重试 `5` 次；只有真实运营中需要调参时再增加配置。

每个任务都显式保存并发数。表单默认使用 `max(1, floor(target_count / 3))`，用户可在 `1` 到目标数量之间调整，并同时受部署并发上限约束。历史任务未保存并发数时按 `1` 恢复执行。

## UI 设计

### 入口

左侧栏只保留“添加账号”按钮，进入 `/accounts/new`。单个添加和批量添加通过添加页面内的 Tab 切换：

- `/accounts/new` 或 `/accounts/new?mode=single`：单个添加。
- `/accounts/new?mode=bulk`：批量添加。

### 页面

当前右侧页面：

```text
WaCreateAccountRoute -> PageShell(title="添加账号") -> WaAccountAdd
```

建议扩展为：

- 页面顶部使用 tabs：
  - 单个添加
  - 批量添加
- 单个添加仍渲染 `WaAccountAdd`。
- 批量添加渲染 `WaBulkAccountAdd`。

### 批量添加表单

无运行中任务时显示：

- 国家选择：默认菲律宾。
- 供应商：第一版固定为 HeroSMS，不做供应商多选。
- 服务：WhatsApp，内部用服务 code。
- 数量：默认 10，上限 20（仍可由部署配置下调）。
- 并发数：默认目标数量的三分之一，最少 1；不得超过目标数量和部署并发上限。
- 验证方式：SMS。
- Integrity mode：复用当前单个添加的选择逻辑。
- 刷新报价按钮。
- 报价表。

报价表字段：

```text
供应商 | 国家 | 服务 | 价格 | 可用数量 | 价格档/渠道 | 选择数量
```

支持：

- 自动选择最低价填满目标数量。
- 手动选择每个价格档数量。
- 价格档可按供应商、价格、库存排序。

### 任务详情

有 active task 时，批量添加页面显示：

- 总状态。
- 创建时间、更新时间。
- 目标数量、并发数、成功数量、失败数量、取消数量、等待数量。
- 取消任务按钮。
- 条目表。

条目表字段：

```text
# | 供应商 | 价格 | 号码 | 阶段 | WA 状态 | 短信状态 | 错误 | 更新时间
```

号码展示按现有敏感策略处理，可显示 E.164 或部分遮罩，具体按产品需求决定。

再次切换到批量添加 Tab 时：

- 如果有 `RUNNING`、`CANCEL_REQUESTED`、`CANCELING`、`PAUSED` 任务，显示该任务。
- 如果没有 active task，显示新建任务表单。

## Dashboard API

新增 dashboard JSON API，不进入 proto：

```text
GET  /api/wa/bulk-registration/offers?country_iso2=PH&service=whatsapp
GET  /api/wa/bulk-registration/task
POST /api/wa/bulk-registration/task
POST /api/wa/bulk-registration/task/cancel
```

`GET /offers` 返回统一报价：

```json
{
  "success": true,
  "country_iso2": "PH",
  "service": "whatsapp",
  "offers": [
    {
      "offer_id": "hero-sms:PH:wa:price:0.1500",
      "provider": "hero-sms",
      "country_iso2": "PH",
      "country_id": "4",
      "service_code": "wa",
      "price": 0.15,
      "currency": "USD",
      "available_count": 286896,
      "provider_id": "",
      "operator": "any",
      "price_tier": "0.1500"
    }
  ]
}
```

`POST /task` 输入：

```json
{
  "country_iso2": "PH",
  "target_count": 10,
  "concurrency": 3,
  "integrity_mode": "error_code",
  "offers": [
    {
      "offer_id": "hero-sms:PH:wa:price:0.50",
      "quantity": 10,
      "max_price": 0.50
    }
  ]
}
```

如果已有 active task，则返回当前 task，不创建第二个。

`POST /cancel`：

- 设置任务为 `CANCEL_REQUESTED`。
- worker 负责取消未完成号码。
- 已注册成功的条目不取消。

## 后端模块设计

建议新增包：

```text
internal/waapp/bulkregistration
internal/waapp/smsotp
```

`smsotp` 负责供应商适配：

```go
type Provider interface {
    Name() string
    ListOffers(ctx context.Context, input OfferQuery) ([]Offer, error)
    AcquireNumber(ctx context.Context, input AcquireNumberInput) (Activation, error)
    MarkReady(ctx context.Context, activationID string) error
    PollCode(ctx context.Context, activationID string) (ActivationStatus, error)
    RequestResend(ctx context.Context, activationID string) error
    Complete(ctx context.Context, activationID string) error
    Cancel(ctx context.Context, activationID string) error
}
```

`bulkregistration` 负责任务编排：

- 创建任务。
- 保证只有一个 active task。
- 按任务条目执行状态机。
- 调用短信平台 provider。
- 调用现有 BFF 注册链路。
- 清理失败 WA pending account。
- 记录任务事件。
- 处理取消、恢复、超时。

## 存储设计

需要落库，不能只放内存。服务重启后必须能恢复任务或继续取消号码。

建议新增表：

```text
wa_bulk_registration_tasks
wa_bulk_registration_items
wa_sms_activation_events
```

### wa_bulk_registration_tasks

字段建议：

```text
task_id
status
country_iso2
target_count
concurrency
requested_count
success_count
failed_count
canceled_count
provider_selection_json
created_at
updated_at
started_at
finished_at
cancel_requested_at
last_error
```

### wa_bulk_registration_items

字段建议：

```text
item_id
task_id
status
provider
offer_id
activation_id
phone_e164
country_calling_code
country_iso2
price
currency
wa_account_id
client_profile_id
verification_request_id
registration_id
login_state_id
sms_status
wa_probe_status
wa_verification_status
wa_registration_status
attempt_count
cancel_attempt_count
next_attempt_at
created_at
updated_at
finished_at
last_error
last_error_code
```

### wa_sms_activation_events

用于审计和排障：

```text
event_id
task_id
item_id
provider
activation_id
event_type
provider_status
wa_status
message
created_at
```

注意：

- 不存 OTP 明文。
- 不存短信全文。
- 不存 API key。
- 不存完整代理 URL。
- 不存完整可复用认证材料。
- Dashboard 读取最近 `100` 条事件，用于展示每个条目的阶段、短信状态、WA 状态和脱敏根因；任务结束后仍显示最近一次终态任务的日志，便于复盘失败。
- 容器日志对有根因的事件记录 task/item、阶段、短信/WA 状态和脱敏失败分类；不记录 OTP、号码、activation ID、代理或凭据。

## 单任务唯一性

只允许一个 active task。

Active 状态集合：

```text
DRAFT
RUNNING
CANCEL_REQUESTED
CANCELING
PAUSED
```

创建任务时：

1. 查询 active task。
2. 如果存在，返回当前任务。
3. 如果不存在，在事务中创建新任务。
4. PG 模式可使用部分唯一索引保证 active task 唯一。
5. SQLite 模式用事务和状态检查兜底。

## 任务状态机

任务状态：

```text
DRAFT
RUNNING
CANCEL_REQUESTED
CANCELING
COMPLETED
PARTIAL_COMPLETED
FAILED
CANCELED
PAUSED
```

条目状态：

```text
QUEUED
ACQUIRING_NUMBER
NUMBER_ACQUIRED
WA_PROBING
WA_REQUESTING_OTP
WAITING_SMS
SMS_RECEIVED
SUBMITTING_OTP
REGISTERED
CANCELING_NUMBER
NUMBER_CANCELED
CANCEL_PENDING
FAILED
EXPIRED
```

状态流：

```text
QUEUED
  -> ACQUIRING_NUMBER
  -> NUMBER_ACQUIRED
  -> WA_PROBING
  -> WA_REQUESTING_OTP
  -> WAITING_SMS
  -> SMS_RECEIVED
  -> SUBMITTING_OTP
  -> REGISTERED
```

失败流：

```text
NUMBER_ACQUIRED / WA_PROBING / WA_REQUESTING_OTP / WAITING_SMS
  -> CANCELING_NUMBER
  -> NUMBER_CANCELED
  -> FAILED
```

取消流：

```text
QUEUED -> CANCELED
ACQUIRING_NUMBER -> CANCEL_REQUESTED 等待返回
NUMBER_ACQUIRED / WAITING_SMS -> CANCELING_NUMBER -> NUMBER_CANCELED
REGISTERED -> 保持 REGISTERED，不取消
```

## 单条执行流程

每个 item 的标准流程：

1. 从报价选择中取一个 offer。
2. 调用 `AcquireNumber`。
3. 得到 `activation_id`、`phone_number`、价格、国家信息。
4. 将号码规范化为 `PhoneTarget`。
5. 调用现有 `StartRegistration`，payload 包含：
   - `e164_number`
   - `country_calling_code`
   - `country_iso2`
   - `delivery_method=sms`
   - `integrity_mode`
   - `bulk_task_id`
   - `bulk_item_id`
6. 如果 WA 注册请求被拒绝，立即取消短信号码。
7. 如果 `verification_request_id` 成功生成，保存 `wa_account_id` 和 `verification_request_id`。
8. 调用短信平台 `MarkReady`。
9. 轮询 `PollCode`。
10. 收到 OTP 后调用现有 `resumeOTP` 或内部 `SubmitVerificationCodeWithRunner`。
11. 注册成功后调用短信平台 `Complete`。
12. 更新 item 为 `REGISTERED`。

## 异常兜底策略

### WA probe 阶段失败

需要取消号码：

- `AccountProbeStatus=REJECTED`
- `AccountProbeStatus=UNREACHABLE`
- `blocked=true`
- `account_flow=invalid_number`
- `account_flow=consent_required`
- `account_flow=challenge_required`
- 网络错误且不可快速恢复

### WA 请求 OTP 失败

需要取消号码：

- `VerificationRequestStatus=REJECTED`
- `raw_reason=blocked`
- `raw_reason=format_wrong`
- `raw_reason=length_short`
- `raw_reason=length_long`
- `raw_reason=no_routes`
- `raw_reason=bad_param`
- `raw_reason=missing_param`
- `raw_reason=bad_token`
- `raw_reason=old_version`
- `raw_reason=invalid_skey`
- `raw_reason=consent`
- `raw_reason=challenge`
- `retry_after > 0`

说明：批量任务不建议等待 cooldown，因为号码是租用资源，等待会增加扣费风险。

### 等待短信超时

如果超过代码默认短信等待超时（第一版建议 `1200s`）仍未收到 OTP：

1. 调用短信平台取消。
2. 清理 pending registration WA account。
3. item 标记 `EXPIRED` 或 `FAILED`。

### 收到 OTP 但 WA 提交失败

根据错误处理：

- `bad_code` / mismatch：如果平台支持 `setStatus=3`，可请求重发一次。
- `blocked` / terminal rejected：标记失败；此时可能已经扣费，记录为 `FAILED_AFTER_SMS_RECEIVED`。
- 网络错误：短重试，保持同一 WA 注册专用代理 sticky session。

### 用户取消任务

- `QUEUED`：直接取消。
- 未申请号码：直接取消。
- 已申请但未注册成功：调用短信平台取消。
- 已注册成功：保持成功，不能取消。
- 取消失败：进入 `CANCEL_PENDING`，延迟重试。

### 平台取消失败

常见情况：

- `EARLY_CANCEL_DENIED`：延迟重试。
- `FREE_CANCELLATION_EXPIRED`：免费取消窗口已过，停止自动退款假设，标记需要人工确认。
- `OTP_RECEIVED`：号码已收到 OTP，不能取消；进入 OTP 提交流程或标记 `FAILED_AFTER_SMS_RECEIVED`。
- `NEW_OTP_RECEIVED`：有新 OTP 到达；先拉取最新 OTP，再决定完成或失败。
- `ACTIVATION_NOT_ACTIVE`：激活已终止或退款，可停止取消重试。
- `NO_ACTIVATION`：标记取消不可确认，停止重试。
- `SIM_OFFLINE`：号码不再可用，标记失败并尝试取消。
- `SIM_TEMPORARY_OFFLINE`：短延迟重试；超过短信等待窗口后取消。
- 网络超时：指数退避重试。

取消重试次数达到上限后：

- item 标记 `CANCEL_PENDING` 或 `FAILED_CANCEL`.
- task 标记 `PARTIAL_COMPLETED` 或 `FAILED`，并在 UI 显示需要人工确认。

## 计费策略

系统只负责尽早调用平台取消或完成：

- WA 未成功请求 OTP：取消号码。
- WA 已请求 OTP 但短信未到：超时取消。
- WA 已收到 OTP 且提交成功：完成激活。
- WA 已收到 OTP 但提交失败：可能已扣费，记录状态并尽量按平台规则处理。

不要在 UI 中承诺“失败一定不扣费”。

## 与注册专用代理的关系

批量任务申请到菲律宾号码后，注册 payload 会带：

```json
{
  "country_iso2": "PH",
  "country_calling_code": "63"
}
```

WA 注册阶段应复用 `docs/registration-dedicated-proxy-plan.md` 的注册专用代理 resolver，由注册链路根据 `PH` 选择菲律宾地区代理。

短信平台 API 请求本身不走 WA 注册代理。它只使用普通服务端 HTTP client，并可按运维需要单独配置出站代理。

## Worker 与恢复

需要后端 worker：

- 服务启动后扫描 active task。
- 对 `RUNNING` 任务继续推进。
- 对 `CANCEL_REQUESTED` 任务继续取消。
- 对 `CANCEL_PENDING` item 定时重试取消。
- 对长时间未更新 item 做超时判断。

并发控制：

- 任务创建时保存用户选择的并发数；默认 `max(1, floor(target_count / 3))`。
- 用户可选范围为 `1` 到目标数量，并受 `WA_BULK_REGISTRATION_CONCURRENCY` 部署上限限制。
- worker 使用受限并行执行条目；历史任务没有并发字段时按 `1` 恢复。
- 每个 item 状态更新要幂等。
- 外部 API 调用前后记录 event，便于恢复时判断下一步。

## 安全与日志

敏感数据：

- OTP。
- 短信全文。
- API key。
- 平台账号凭据。
- 代理 URL。
- 可复用注册请求材料。

日志规则：

- 不记录 OTP。
- 不记录短信全文。
- 不记录 API key。
- 不记录完整手机号时，优先记录 hash 或尾号。
- 错误信息要脱敏。

## 前端刷新策略

- 任务详情 `refetchInterval=2000-5000ms`。
- `RUNNING` 时自动刷新。
- 终态后停止高频刷新。
- 成功注册 item 后 invalidate `waKeys.accounts()`。
- 页面重新打开时先 `GET /task`。

## 测试计划

### 单元测试

- 服务 code / country id 映射。
- 价格表合并和最低价选择。
- 单任务唯一性。
- 状态机合法迁移。
- WA 失败原因到取消策略的映射。
- 短信平台状态解析。
- 敏感字段不会进入 JSON 响应和日志字段。

### 集成测试

使用 fake provider：

- 申请号码成功 -> WA 成功 -> 收到 OTP -> 注册成功 -> complete。
- WA probe blocked -> cancel。
- WA request OTP no_routes -> cancel。
- 短信超时 -> cancel。
- 取消任务 -> 未完成 item 全部 cancel。
- complete 失败 -> 重试。
- cancel `EARLY_CANCEL_DENIED` -> 延迟重试。
- 服务重启后恢复 RUNNING task。

### 手工联调

1. 配置 HeroSMS API key。
2. 调 HeroSMS `getCountries` 验证菲律宾 country id。
3. 调 HeroSMS `getServicesList` 验证 WhatsApp service code。
4. 调 HeroSMS `GET /api/v1/activations/offers?services=<wa>&countries=<PH id>` 验证价格档、库存、`map` 结构。
5. 调 HeroSMS `getNumberV2` 验证申请号码返回字段。
6. 调 HeroSMS `cancelActivation` 验证未收到 OTP 时取消返回 HTTP 204。
7. 小批量 1 个号码试跑。
8. 再扩大到 3 个。
9. 最后开放 10 个。

## 分阶段落地

### Phase 1: 供应商适配与报价

- 新增 `smsotp` provider 接口。
- 实现 HeroSMS provider。
- 新增报价 API。
- 前端展示报价表。

### Phase 2: 任务模型与唯一任务

- 新增 task/item 存储。
- 新增 `GET task` / `POST task` / `POST cancel`。
- 前端显示任务详情。
- 保证只能有一个 active task。

### Phase 3: Worker 编排

- 实现 item 状态机。
- 接入 `StartRegistration`。
- 接入 OTP 轮询和提交。
- 接入 complete/cancel。
- 支持取消任务。

### Phase 4: 恢复与兜底

- 服务启动恢复 active task。
- cancel retry。
- timeout scanner。
- 完整事件审计。

### Phase 5: UI 打磨与验收

- 批量入口。
- 当前任务可视化。
- 错误和费用风险提示。
- 成功后账号列表刷新。

## 开工决策

- 第一版只实现 HeroSMS，不实现 SMSBower。
- HeroSMS API key 是运行时输入，不阻塞代码开工。
- HeroSMS WhatsApp service code、国家 id 和可见国家目录由 provider adapter 自动发现并缓存，不作为默认环境变量暴露。
- Dashboard 国家下拉只显示 HeroSMS 可见国家与 1024proxy 国家级出口目录的交集；`Rand` 和州/省不显示。
- HeroSMS 报价优先使用新版 `/api/v1/activations/offers`。
- HeroSMS 完成/取消优先使用 `finishActivation` / `cancelActivation`；兼容 `setStatus=6/8` 作为 fallback。
- 默认目标数量为 `10`，上限为 `20`，并受 `WA_BULK_REGISTRATION_MAX_ITEMS` 进一步限制；默认并发为目标数量的三分之一（向下取整，最少 `1`）。
- 失败后第一版不自动补号，避免扣费风险扩大。
- 单批任务超时时间第一版使用代码默认值；建议按 `target_count * 25min` 设软超时，上限 `4h`。
- dashboard 默认展示脱敏手机号，调试详情只显示必要后四位或后六位。
- 第一版使用轮询，不做 webhook。
