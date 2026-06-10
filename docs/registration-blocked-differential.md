# Registration blocked 差异排查

## 当前结论

同一号码在本协议链路返回 `blocked`、但 iOS 设备可注册，不能直接判定为号码封禁。更可能是当前 Android-like 注册链路缺少 App 运行态可信上下文，或某一步参数形态与 App 不一致。

## 已确认差异

| 链路 | 现状 | App/样本证据 | 风险 |
| --- | --- | --- | --- |
| WAMSYS opaque map | 运行态 `/v2/exist`、`/v2/code` 已接入 App/JNI capture 同形态的精准伪造 WAMSYS material provider；`/v2/register` 默认不额外注入 | `docs/registration-wamsys-re.md` 记录 `gpia/_gi/_gg/_gp/_ga/aid` 由 App/JNI/Play Integrity 链路生成 | 已补；仍需 blocked 样本回归验证 |
| `/v2/register` map | 曾复用较完整 device map | `X.C27428CHd.A0F` 的 verify map 只包含 `mistyped/client_metrics/entered/mcc/mnc/sim_mcc/sim_mnc/network_operator_name/sim_operator_name/network_radio_type/simnum/hasinrc/pid/rc` 及可选扩展 | 中；register 阶段过量字段会放大异常指纹 |
| `/v2/code` map | 缺 `pid`，且 WAMSYS 字段只能通过 tooling 手动构造 | App `/v2/code` capture 中含 `pid` 与 WAMSYS opaque map | 中高 |
| `/v2/exist` 预检 | `StartRegistration` 当前直接进入 `/v2/code` | App after-next 阶段先发 `/v2/exist` / same-device check | 中；是否必须依赖服务端策略和号码状态 |
| 登录闭环 | 注册成功后会创建 login state 并拉起 chatd，但没有注册后 account/bootstrap 辅助请求 | 纯脚本样本证明 `/v2/register` 后 chatd 可登录；更完整 App 仍有 client_log / pre-chatd AB / push 等边带 | 低到中；主要影响后续稳定性，不是 OTP blocked 首因 |

## 本轮对齐

- `/v2/register` 附加 map 改为 App `A0F(msys/verify)` 形态，移除 register 阶段不应携带的 `hasav/reason/device_ram/db/recaptcha/education_screen_displayed/prefer_sms_over_flash/feo2_query_status`。
- `nativePhoneProfile` 增加 profile 级 `pid`，旧 profile 缺失时使用 App capture 中同形态的默认 PID。
- `/v2/code` map 补 `pid`，避免字段集少于 App capture。
- `/v2/exist` map 补 `pid`。
- 运行态 `/v2/exist`、`/v2/code` 自动注入 `gpia/_gi/_gg/_gp/_ga/aid`：`gpia/_gi/_gg` fresh，`_gp/_ga/aid` profile-stable，长度和编码对齐 App capture。

## 下一步

1. 对 blocked 样本保留脱敏响应元数据：阶段、status、reason、param、HTTP code、是否 iOS 同号成功、同出口是否成功。
2. 回归 fresh WAMSYS 后的 `/v2/code` blocked 命中率。
3. 若仍 blocked，再把 `StartRegistration` 改成 `exist -> code -> register` 并补 App 边带 client_log / pre-chatd AB。
