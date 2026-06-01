package app

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (e *NativeEngine) existParams(phone *waappv1.PhoneTarget, state nativeState) (map[string]string, map[string]struct{}) {
	params := map[string]string{
		"cc":                phoneCC(phone),
		"in":                phoneNational(phone),
		"lg":                "en",
		"lc":                "US",
		"fdid":              state.Profile.FDID,
		"expid":             state.Profile.ExpID,
		"access_session_id": state.Profile.AccessSessionID,
		"id":                state.Profile.ID,
		"backup_token":      state.Profile.BackupToken,
		"authkey":           state.AuthKey,
		"e_ident":           state.KeyBundle.IdentityPublic,
		"e_keytype":         state.KeyBundle.KeyType,
		"e_regid":           state.KeyBundle.RegID,
		"e_skey_id":         state.KeyBundle.SignedKeyID,
		"e_skey_val":        state.KeyBundle.SignedKeyValue,
		"e_skey_sig":        state.KeyBundle.SignedKeySig,
	}
	if token := e.registrationToken(phone, state); token != "" {
		params["token"] = token
	}
	raw := map[string]struct{}{"id": {}, "backup_token": {}}
	for key, value := range existDeviceMap(state) {
		params[key] = pctBytes([]byte(value))
		raw[key] = struct{}{}
	}
	return params, raw
}

func (e *NativeEngine) registrationToken(phone *waappv1.PhoneTarget, state nativeState) string {
	if token := state.LastCodeParams["token"]; token != "" {
		return token
	}
	return deriveDefaultRegistrationToken(phoneNational(phone))
}

const defaultRegistrationTokenHMACKeyHex = "44539b934347b6f12609296e69145b58309df94ed0a8a5a2d94078a8eaff87013e3d95a69644aa1b924646532c279f8bcd2855ab55f2c8bc1693adb7800c88ff"

const defaultRegistrationTokenMessagePrefixHex = "" +
	"30820332308202f0a00302010202044c2536a4300b06072a8648ce3804030500307c310b3009060355040613025553311330110603550408130a43616c69666f726e6961311430120603550407130b53616e746120436c61726131163014060355040a130d576861747341707020496e632e31143012060355040b130b456e67696e656572696e67311430120603550403130b427269616e204163746f6e301e170d3130303632353233303731365a170d3434303231353233303731365a307c310b3009060355040613025553311330110603550408130a43616c69666f726e6961311430120603550407130b53616e746120436c61726131163014060355040a130d576861747341707020496e632e31143012060355040b130b456e67696e656572696e67311430120603550403130b427269616e204163746f6e308201b83082012c06072a8648ce3804013082011f02818100fd7f53811d75122952df4a9c2eece4e7f611b7523cef4400c31e3f80b6512669455d402251fb593d8d58fabfc5f5ba30f6cb9b556cd7813b801d346ff26660b76b9950a5a49f9fe8047b1022c24fbba9d7feb7c61bf83b57e7c6a8a6150f04fb83f6d3c51ec3023554135a169132f675f3ae2b61d72aeff22203199dd14801c70215009760508f15230bccb292b982a2eb840bf0581cf502818100f7e1a085d69b3ddecbbcab5c36b857b97994afbbfa3aea82f9574c0b3d0782675159578ebad4594fe67107108180b449167123e84c281613b7cf09328cc8a6e13c167a8b547c8d28e0a3ae1e2bb3a675916ea37f0bfa213562f1fb627a01243bcca4f1bea8519089a883dfe15ae59f06928b665e807b552564014c3bfecf492a0381850002818100d1198b4b81687bcf246d41a8a725f0a989a51bce326e84c828e1f556648bd71da487054d6de70fff4b49432b6862aa48fc2a93161b2c15a2ff5e671672dfb576e9d12aaff7369b9a99d04fb29d2bbbb2a503ee41b1ff37887064f41fe2805609063500a8e547349282d15981cdb58a08bede51dd7e9867295b3dfb45ffc6b259300b06072a8648ce3804030500032f00302c021400a602a7477acf841077237be090df436582ca2f0214350ce0268d07e71e55774ab4eacd4d071cd1efad" +
	"55223ce7f9c00cb0117ca0af7f84f825"

func deriveDefaultRegistrationToken(phone string) string {
	key, err := hex.DecodeString(defaultRegistrationTokenHMACKeyHex)
	if err != nil {
		return ""
	}
	prefix, err := hex.DecodeString(defaultRegistrationTokenMessagePrefixHex)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(prefix)
	_, _ = mac.Write([]byte(phone))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func existDeviceMap(state nativeState) map[string]string {
	fields := state.Profile.AdditionalMapFields
	return map[string]string{
		"mistyped":                        "7",
		"offline_ab":                      `{"exposure":[],"exp_hash":[],"metrics":{}}`,
		"client_metrics":                  `{"attempts":1,"app_campaign_download_source":"google-play|unknown","was_activated_from_stub":false}`,
		"read_phone_permission_granted":   "0",
		"sim_state":                       "1",
		"network_operator_name":           fields["network_operator_name"],
		"sim_operator_name":               fields["sim_operator_name"],
		"device_name":                     "HWTRT-Q",
		"feo2_query_status":               "error_security_exception",
		"is_foa_fdid_app_installed":       "false",
		"device_ram":                      "3.53",
		"language_selector_time_spent":    "0",
		"language_selector_clicked_count": "0",
		"db":                              "1",
		"recaptcha":                       `{"stage":"ABPROP_DISABLED"}`,
		"network_radio_type":              "1",
		"simnum":                          "0",
		"hasinrc":                         "1",
		"rc":                              "0",
		"_ge":                             `{"sb":false,"sv":false}`,
	}
}

func registerVerificationMap(state nativeState, method string) map[string]string {
	fields := state.Profile.AdditionalMapFields
	clientMetrics, _ := json.Marshal(struct {
		Attempts             int    `json:"attempts"`
		VerifyMethod         string `json:"verify_method"`
		WasActivatedFromStub bool   `json:"was_activated_from_stub"`
	}{
		Attempts:             1,
		VerifyMethod:         firstNonEmpty(method, "sms"),
		WasActivatedFromStub: false,
	})
	values := map[string]string{
		"mistyped":                   "7",
		"reason":                     "",
		"hasav":                      "2",
		"client_metrics":             string(clientMetrics),
		"entered":                    "2",
		"mcc":                        "",
		"mnc":                        "",
		"sim_mcc":                    "",
		"sim_mnc":                    "",
		"network_operator_name":      "",
		"sim_operator_name":          "",
		"network_radio_type":         "1",
		"simnum":                     "0",
		"hasinrc":                    "1",
		"rc":                         "0",
		"db":                         "1",
		"device_ram":                 "3.53",
		"education_screen_displayed": "false",
		"prefer_sms_over_flash":      "false",
		"recaptcha":                  `{"stage":"ABPROP_DISABLED"}`,
	}
	for key, value := range fields {
		values[key] = value
	}
	return values
}

func parseExistProbeResult(data map[string]any) EngineProbeResult {
	status := responseStatus(data)
	reason := responseReason(data)
	methods := availableVerificationMethods(data)
	methodStatuses := verificationMethodStatuses(data, methods)
	smsWait := methodCooldownSeconds(methodStatuses, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS)
	blocked := status == "blocked" || reason == "blocked"
	baseProtocolRejected := existProtocolRejected(status, reason)
	invalidNumber := existInvalidNumberReason(reason)
	rateLimited := existRateLimitedReason(reason)
	registered := !baseProtocolRejected && !blocked && !invalidNumber && !rateLimited && existRegisteredSignal(status, reason, data)
	protocolRejected := baseProtocolRejected || (!registered && existRequestMaterialReason(reason))
	notRegistered := !registered && !protocolRejected && !blocked && !invalidNumber && !rateLimited && existNotRegisteredSignal(status, reason, methods)
	registeredKnown := registered || notRegistered || invalidNumber
	canSendSMS := methodOffered(methodStatuses, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS) && smsWait <= 0 && !blocked && !protocolRejected && !invalidNumber && !rateLimited
	reachable := !protocolRejected && !blocked && !invalidNumber && !rateLimited && (existReachableStatus(status) || registered || len(methods) > 0 || notRegistered)
	result := EngineProbeResult{
		Status:           waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNKNOWN,
		AccountFlow:      existAccountFlow(protocolRejected, registered, notRegistered, blocked, invalidNumber, rateLimited),
		RawStatus:        status,
		RawReason:        reason,
		RegisteredKnown:  registeredKnown,
		Registered:       registered,
		Blocked:          blocked,
		SMSWaitSeconds:   smsWait,
		CanSendSMS:       canSendSMS,
		SupportedMethods: methods,
		MethodStatuses:   methodStatuses,
	}
	switch {
	case protocolRejected:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED
		result.Err = existProtocolError(data)
	case blocked:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNREACHABLE
	case invalidNumber || rateLimited:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNREACHABLE
	case reachable:
		result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE
	}
	return result
}

func responseReason(data map[string]any) string {
	if value, ok := data["reason"].(string); ok {
		return strings.ToLower(value)
	}
	if value, ok := data["failure_reason"].(string); ok {
		return strings.ToLower(value)
	}
	return ""
}

func existReachableStatus(status string) bool {
	switch status {
	case "ok", "sent", "valid", "exists", "registered":
		return true
	default:
		return false
	}
}

func existRegisteredStatus(status string) bool {
	switch status {
	case "exists", "registered":
		return true
	default:
		return false
	}
}

func existProtocolRejected(status string, reason string) bool {
	if status == "" && reason == "" {
		return false
	}
	switch reason {
	case "missing_param", "bad_param", "bad_token", "old_version", "invalid_skey":
		return true
	default:
		return false
	}
}

func existRequestMaterialReason(reason string) bool {
	return reason == "incorrect"
}

func existInvalidNumberReason(reason string) bool {
	switch reason {
	case "format_wrong", "length_short", "length_long":
		return true
	default:
		return false
	}
}

func existRateLimitedReason(reason string) bool {
	switch reason {
	case "too_recent", "too_many", "temporarily_unavailable":
		return true
	default:
		return false
	}
}

func existRegisteredSignal(status string, reason string, data map[string]any) bool {
	if existRegisteredReason(reason) {
		return true
	}
	if existExistingAccountSignal(data) {
		return true
	}
	if jsonString(data["login"]) != "" {
		return true
	}
	if existRegisteredStatus(status) {
		return true
	}
	return firstNonEmpty(jsonString(data["new_jid"]), jsonString(data["jid"]), jsonString(data["registration_jid"])) != ""
}

func existAppVerificationSignal(data map[string]any) bool {
	if jsonNumber(data["wa_old_eligible"]) > 0 ||
		jsonNumber(data["email_otp_eligible"]) > 0 {
		return true
	}
	return firstNonEmpty(
		jsonString(data["passkey_auth_challenge"]),
		jsonString(data["passkey_credential"]),
		jsonString(data["wa_old_device_name"]),
	) != ""
}

func existExistingAccountSignal(data map[string]any) bool {
	return existAppVerificationSignal(data) || jsonNumber(data["acc_tr_eligible"]) > 0
}

func existRegisteredReason(reason string) bool {
	switch reason {
	case "security_code", "second_code", "device_confirm_or_second_code", "consent", "consent_parent_linking_already_registered":
		return true
	default:
		return false
	}
}

func existNotRegisteredSignal(status string, reason string, methods []waappv1.VerificationDeliveryMethod) bool {
	return status == "ok" && reason == "" && len(methods) > 0
}

func existAccountFlow(protocolRejected bool, registered bool, notRegistered bool, blocked bool, invalidNumber bool, rateLimited bool) string {
	switch {
	case protocolRejected:
		return accountProbeFlowProbeFailed
	case registered:
		return accountProbeFlowRegistered
	case notRegistered:
		return accountProbeFlowNotRegistered
	case blocked:
		return accountProbeFlowBlocked
	case invalidNumber:
		return accountProbeFlowInvalidNumber
	case rateLimited:
		return accountProbeFlowRateLimited
	default:
		return accountProbeFlowUnknown
	}
}

func existProtocolError(data map[string]any) error {
	return waProtocolError(data, "WA exist probe rejected")
}

func waProtocolError(data map[string]any, fallback string) error {
	reason := responseReason(data)
	param := jsonString(data["param"])
	message := fallback
	if reason != "" {
		message += ": reason=" + reason
	}
	if param != "" {
		message += " param=" + param
	}
	code := waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED
	retryable := false
	switch reason {
	case "too_recent", "too_many", "temporarily_unavailable":
		code = waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED
		retryable = true
	case "no_routes":
		code = waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE
	}
	return NewError(code, message, retryable)
}

func availableVerificationMethods(data map[string]any) []waappv1.VerificationDeliveryMethod {
	candidates := verificationMethods(data["fallback_methods"])
	out := []waappv1.VerificationDeliveryMethod{}
	if containsDeliveryMethod(candidates, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS) ||
		jsonNumber(data["sms_length"]) > 0 ||
		jsonNumber(data["send_sms_eligible"]) > 0 {
		out = append(out, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS)
	}
	if containsDeliveryMethod(candidates, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE) ||
		jsonNumber(data["voice_length"]) > 0 {
		out = append(out, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE)
	}
	if containsDeliveryMethod(candidates, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE) || existAppVerificationSignal(data) {
		out = append(out, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE)
	}
	return out
}

func verificationMethods(value any) []waappv1.VerificationDeliveryMethod {
	seen := map[waappv1.VerificationDeliveryMethod]struct{}{}
	out := []waappv1.VerificationDeliveryMethod{}
	for _, name := range stringList(value) {
		method := verificationMethod(name)
		if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
			continue
		}
		if _, ok := seen[method]; ok {
			continue
		}
		seen[method] = struct{}{}
		out = append(out, method)
	}
	return out
}

func verificationMethod(name string) waappv1.VerificationDeliveryMethod {
	switch verificationMethodCode(name) {
	case "sms", "send_sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	case "voice":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE
	case "in_app", "in_app_message", "wa_old", "email_otp", "passkey", "flash":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE
	default:
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED
	}
}

func verificationMethodStatuses(data map[string]any, methods []waappv1.VerificationDeliveryMethod) []VerificationMethodStatus {
	seen := map[string]VerificationMethodStatus{}
	order := []string{}
	add := func(code string, method waappv1.VerificationDeliveryMethod, available bool, cooldown int64) {
		if code == "" || method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
			return
		}
		if !available && cooldown <= 0 {
			return
		}
		if _, ok := seen[code]; !ok {
			order = append(order, code)
		}
		previous := seen[code]
		previous.Code = code
		previous.Method = method
		previous.Available = previous.Available || available
		if cooldown <= 0 {
			cooldown = previous.CooldownSeconds
		}
		previous.CooldownSeconds = cooldown
		seen[code] = previous
	}
	for _, raw := range stringList(data["fallback_methods"]) {
		code := verificationMethodCode(raw)
		add(code, verificationMethod(code), true, verificationCodeCooldownSeconds(data, code))
	}
	for _, method := range methods {
		code := defaultVerificationMethodCode(method)
		if method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE && hasSpecificInAppMethod(seen) {
			continue
		}
		add(code, method, true, verificationCooldownSeconds(data, method))
	}
	add("sms", waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, false, verificationCodeCooldownSeconds(data, "sms"))
	add("voice", waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE, false, verificationCodeCooldownSeconds(data, "voice"))
	add("wa_old", waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE, false, verificationCodeCooldownSeconds(data, "wa_old"))
	add("email_otp", waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE, false, verificationCodeCooldownSeconds(data, "email_otp"))
	add("flash", waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE, false, verificationCodeCooldownSeconds(data, "flash"))
	add("passkey", waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE, false, verificationCodeCooldownSeconds(data, "passkey"))
	out := make([]VerificationMethodStatus, 0, len(seen))
	for _, code := range order {
		out = append(out, seen[code])
	}
	return out
}

func verificationMethodCode(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sms", "send_sms":
		return "sms"
	case "voice", "send_voice":
		return "voice"
	case "wa_old", "wa-old", "old_wa":
		return "wa_old"
	case "email", "email_otp", "email-otp":
		return "email_otp"
	case "flash":
		return "flash"
	case "passkey":
		return "passkey"
	case "in_app", "in_app_message":
		return "in_app"
	default:
		return ""
	}
}

func defaultVerificationMethodCode(method waappv1.VerificationDeliveryMethod) string {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS:
		return "sms"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE:
		return "voice"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE:
		return "in_app"
	default:
		return ""
	}
}

func hasSpecificInAppMethod(seen map[string]VerificationMethodStatus) bool {
	for _, code := range []string{"passkey", "wa_old", "email_otp", "flash"} {
		if _, ok := seen[code]; ok {
			return true
		}
	}
	return false
}

func verificationCooldownSeconds(data map[string]any, method waappv1.VerificationDeliveryMethod) int64 {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS:
		return firstJSONInt64(data["sms_wait"], data["send_sms_wait"], data["sms_retry_after"], data["send_sms_retry_after"])
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE:
		return firstJSONInt64(data["voice_wait"], data["send_voice_wait"], data["voice_retry_after"], data["send_voice_retry_after"])
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE:
		return firstJSONInt64(data["wa_old_wait"], data["email_otp_wait"], data["in_app_wait"], data["device_confirm_wait"], data["passkey_wait"])
	default:
		return 0
	}
}

func verificationCodeCooldownSeconds(data map[string]any, code string) int64 {
	switch code {
	case "sms":
		return firstJSONInt64(data["sms_wait"], data["send_sms_wait"], data["sms_retry_after"], data["send_sms_retry_after"])
	case "voice":
		return firstJSONInt64(data["voice_wait"], data["send_voice_wait"], data["voice_retry_after"], data["send_voice_retry_after"])
	case "wa_old":
		return firstJSONInt64(data["wa_old_wait"])
	case "email_otp":
		return firstJSONInt64(data["email_otp_wait"])
	case "flash":
		return firstJSONInt64(data["flash_wait"])
	case "passkey":
		return firstJSONInt64(data["passkey_wait"])
	case "in_app":
		return firstJSONInt64(data["wa_old_wait"], data["email_otp_wait"], data["flash_wait"], data["in_app_wait"], data["device_confirm_wait"], data["passkey_wait"])
	default:
		return 0
	}
}

func methodCooldownSeconds(statuses []VerificationMethodStatus, method waappv1.VerificationDeliveryMethod) int64 {
	for _, status := range statuses {
		if status.Method == method {
			return status.CooldownSeconds
		}
	}
	return 0
}

func methodOffered(statuses []VerificationMethodStatus, method waappv1.VerificationDeliveryMethod) bool {
	for _, status := range statuses {
		if status.Method == method && status.Available {
			return true
		}
	}
	return false
}

func stringList(value any) []string {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return v
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			out = append(out, strings.TrimSpace(part))
		}
		return out
	default:
		return nil
	}
}

func containsDeliveryMethod(methods []waappv1.VerificationDeliveryMethod, expected waappv1.VerificationDeliveryMethod) bool {
	for _, method := range methods {
		if method == expected {
			return true
		}
	}
	return false
}

func jsonInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func firstJSONInt64(values ...any) int64 {
	for _, value := range values {
		if result := jsonInt64(value); result > 0 {
			return result
		}
	}
	return 0
}
