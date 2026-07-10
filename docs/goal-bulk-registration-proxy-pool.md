# Goal: 批量注册代理候选池

日期：2026-07-11

## 目标

为批量 WhatsApp 注册建立 1024proxy 注册专用代理候选池，降低边缘国家因单个接入节点、认证链路或出口检测失败而导致整条注册链路提前失败的概率。

完成后，目标数量为 `N` 的批量任务会在开始时获取 `N * 6` 条候选接入节点；单次向 1024proxy 提取最多 `60` 条，超过时分批提取。候选在真正需要时进行出口校验，失败后切换至下一个候选。只有取得通过目标国家校验的代理后，才允许申请 HeroSMS 号码。

## 已知问题

当前实现每个条目在购号前临时创建 resolver，以 `num=1` 请求一个节点并预检；随后 `startWARegistration` 会创建新的 resolver，再解析一次代理。预检通过的路线没有进入实际 WA 请求，既不能复用，也不能在代理失败时从一组候选中顺序切换。

`registration proxy source request failed` 是 source 提取、代理连接、出口检测或出口国家不一致经过现有重试后汇总的安全错误。它发生在 HeroSMS 购号前，但现有事件没有足够的脱敏分类帮助定位失败比例。

## 范围

包含：

- 仅用于批量注册的 1024proxy 候选节点池。
- 按任务和目标国家隔离候选池；并发条目安全领取候选。
- 通过出口预检的代理路线交给实际 WA Probe/OTP 请求。
- 成功请求 OTP 后，继续使用既有 `RegistrationProxyWait` 保存的 sticky 路线提交 OTP。
- 不暴露节点地址、代理 URL、用户名、密码、session id、OTP 或 HeroSMS activation id。
- 为候选池耗尽和被淘汰原因增加聚合、脱敏诊断。

不包含：

- 更改 proto 契约、Dashboard 请求/响应中暴露代理实现信息。
- 将 `WA_COMMON_PROXY` 作为静默 fallback。
- 对 WA `blocked`、号码无效或 OTP 失败时切换代理后重试同一号码。
- 自动购号补偿或真实付费注册验证。

## 设计

### 1. 候选池

- 池键：`task_id + country_iso2`。
- 候选数：`task.target_count * 6`；1024proxy 的 `num` 以 `60` 为单批上限分批获取。
- pool 只保存服务进程内的节点地址和领取状态，不写入 Dashboard、日志、proto 或跨仓模型。
- 以 `host:port` 去重；返回不足时保留可用候选，不因不足补发无限请求。
- 服务重启后未领取的池可以重新创建；已成功请求 OTP 的条目仍从已有 `RegistrationProxyWait` 复用原 sticky 路线。

### 2. 路由分配

1. 条目进入 `QUEUED` 时，从对应池按顺序领取候选。
2. 为该候选生成与 `task_id + item_id + candidate_index` 绑定的独立 8 位 sticky session id，渲染 1024proxy 用户名。
3. 用该候选验证实际出口国家。
4. 预检失败：计入匿名淘汰计数，领取下一候选；不申请 HeroSMS 号码。
5. 预检成功：暂存此条路线并用它执行同一条目的 WA Probe 和 OTP 请求。
6. OTP 请求成功：由现有逻辑将此路线写入服务内 OTP wait；后续提交 OTP 必须继续使用它。

路线只会在“尚未向 WA 请求 OTP”的代理候选阶段轮换。WA 返回 `blocked`、请求已发送后短信超时或 OTP 提交失败时，不能更换代理重试同一号码；维持现有取消 HeroSMS 激活的安全策略。

### 3. 路由复用与重启

- 批量 worker 需要将预检结果传入实际注册入口，不能重新调用 resolver。
- 若服务恰好在购号完成、OTP 请求前重启，条目恢复时重新从候选池取得并预检一条路线后再发起 WA 请求；此时没有 OTP wait，不需要复用旧路线。
- 已有 OTP wait 的条目继续走 `RegistrationProxyWait`，不会触碰候选池。

### 4. 可观测性

仅记录以下脱敏信息：

- 池计划数、实际候选数、去重数。
- 已分配数、可用数、耗尽数。
- 聚合淘汰原因：`source_request_failed`、`invalid_node`、`egress_request_failed`、`egress_invalid_response`、`egress_country_mismatch`。

当池耗尽时，条目错误应明确为“注册代理候选池已耗尽”，并附脱敏计数；不输出地址、账号、密码、session 或原始响应。

## 实现步骤

1. 扩展 1024proxy source，使其可批量提取和渲染单个候选路线；保留单条 resolve 供非批量注册使用。
2. 在 bulk manager 引入按任务/国家隔离、互斥安全的候选池和路线租约。
3. 将 bulk 预检结果传递给 WA 注册入口，确保同一条已验证路线实际用于 Probe、请求 OTP，并由 OTP wait 持久化。
4. 在候选耗尽、预检失败和路线选中处增加聚合脱敏事件。
5. 更新 Dashboard 的失败文案，区分候选池耗尽与普通 source 失败。
6. 更新专用代理与批量注册计划文档。

## 测试与验收

- 目标 `10` 的任务请求 `60` 个候选；目标大于 `10` 时按每批 `60` 分段请求并汇总。
- 并发条目不会领取同一个候选；不同条目使用独立 sticky session id。
- 前置候选出口失败时会使用下一候选；预检失败期间 HeroSMS `AcquireNumber` 调用次数为 `0`。
- 池耗尽时条目失败且错误包含脱敏聚合原因，不购号。
- 预检通过的路线就是实际 WA OTP 请求使用的路线；OTP wait 可在后续 OTP 提交时复用。
- 原有专用代理、取消短信激活、HeroSMS、bulk 并发测试全部通过。
- `go test ./...`、`npm run lint`、Vite build、Docker build 通过。
- 生产发布只做健康检查、认证后的只读 bulk API 和代理配置/容器验证；不创建任务、不购号、不请求 OTP。

## 发布

- 发布到独立容器 `wa-app-whats`，域名 `whats.example.invalid`，保留原 `wa-app` 容器不变。
- 使用带提交短 hash 的 `wa-app:whats-<sha>` 镜像、校验和传输、健康检查与自动回滚。
- 发布完成后删除本地和服务器镜像归档，记录镜像、验证结果和提交。
