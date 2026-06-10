package app

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

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
	applyNativeRawParamMap(params, raw, existDeviceMap(state), true)
	return params, raw
}

func (e *NativeEngine) registrationToken(phone *waappv1.PhoneTarget, state nativeState) string {
	if token := state.LastCodeParams["token"]; token != "" {
		return token
	}
	return deriveDefaultRegistrationToken(phoneNational(phone))
}

func registrationMethodName(method waappv1.VerificationDeliveryMethod, fallback string) string {
	switch method {
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS:
		return "sms"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE:
		return "voice"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_IN_APP_MESSAGE,
		waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD:
		return "wa_old"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH:
		return "flash"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP:
		return "email_otp"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY:
		return "passkey"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH:
		return "silent_auth"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43:
		return "silent_auth_ts_43"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA:
		return "recaptcha"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL:
		return "oauth_email"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL:
		return "discoverable_credential"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER:
		return "acc_tr"
	case waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP:
		return "standalone"
	default:
		return fallback
	}
}

func registrationMethodFromName(name string) waappv1.VerificationDeliveryMethod {
	switch verificationMethodCode(name) {
	case "send_sms", "sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	case "voice":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE
	case "flash":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH
	case "wa_old":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
	case "email_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP
	case "passkey":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY
	case "silent_auth":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH
	case "silent_auth_ts_43":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43
	case "recaptcha":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA
	case "oauth_email":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL
	case "discoverable_credential":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL
	case "acc_tr":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	case "standalone":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP
	default:
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED
	}
}

func applyNativeRawParamMap(params map[string]string, raw map[string]struct{}, values map[string]string, omitEmptyOperator bool) {
	for key, value := range values {
		if isOpaqueWamsysMapKey(key) {
			continue
		}
		if omitEmptyOperator && omitEmptyNativeOperatorField(key, value) {
			continue
		}
		if key == "token" {
			if value != "" {
				params[key] = value
			}
			continue
		}
		params[key] = pctBytes([]byte(value))
		raw[key] = struct{}{}
	}
}

func codeDeviceMap(method string, state nativeState) map[string]string {
	fields := nativeDeviceMapFields(state)
	out := map[string]string{
		"mistyped":                   "7",
		"reason":                     "",
		"hasav":                      "2",
		"client_metrics":             nativeCodeClientMetrics(),
		"education_screen_displayed": "false",
		"prefer_sms_over_flash":      "false",
		"_ge":                        `{"sb":false,"sv":false}`,
		"network_radio_type":         fields["network_radio_type"],
		"simnum":                     fields["simnum"],
		"hasinrc":                    fields["hasinrc"],
		"pid":                        fields["pid"],
		"rc":                         fields["rc"],
		"device_ram":                 fields["device_ram"],
		"db":                         fields["db"],
		"recaptcha":                  fields["recaptcha"],
		"feo2_query_status":          fields["feo2_query_status"],
		"mcc":                        fields["mcc"],
		"mnc":                        fields["mnc"],
		"sim_mcc":                    fields["sim_mcc"],
		"sim_mnc":                    fields["sim_mnc"],
	}
	if method == "flash" {
		out["prefer_sms_over_flash"] = "false"
	}
	return out
}

func registerDeviceMap(method string, state nativeState) map[string]string {
	fields := nativeDeviceMapFields(state)
	return map[string]string{
		"mistyped":              "7",
		"client_metrics":        nativeRegisterClientMetrics(method),
		"entered":               nativeCodeEntryMethod(method),
		"mcc":                   fields["mcc"],
		"mnc":                   fields["mnc"],
		"sim_mcc":               fields["sim_mcc"],
		"sim_mnc":               fields["sim_mnc"],
		"network_operator_name": fields["network_operator_name"],
		"sim_operator_name":     fields["sim_operator_name"],
		"network_radio_type":    fields["network_radio_type"],
		"simnum":                fields["simnum"],
		"hasinrc":               fields["hasinrc"],
		"pid":                   fields["pid"],
		"rc":                    fields["rc"],
	}
}

func nativeDeviceMapFields(state nativeState) map[string]string {
	fields := map[string]string{}
	for key, value := range state.Profile.AdditionalMapFields {
		if isOpaqueWamsysMapKey(key) {
			continue
		}
		fields[key] = value
	}
	defaults := map[string]string{
		"network_radio_type":    "1",
		"pid":                   "29418",
		"simnum":                "0",
		"hasinrc":               "1",
		"rc":                    "0",
		"device_ram":            "3.53",
		"db":                    "1",
		"recaptcha":             `{"stage":"ABPROP_DISABLED"}`,
		"feo2_query_status":     "error_security_exception",
		"network_operator_name": "",
		"sim_operator_name":     "",
		"mcc":                   "",
		"mnc":                   "",
		"sim_mcc":               "",
		"sim_mnc":               "",
	}
	for key, value := range defaults {
		fields[key] = firstNonEmpty(fields[key], value)
	}
	return fields
}

func nativeCodeClientMetrics() string {
	return `{"attempts":1,"app_campaign_download_source":"google-play|unknown"}`
}

func nativeRegisterClientMetrics(method string) string {
	body, err := json.Marshal(struct {
		Attempts             int    `json:"attempts"`
		VerifyMethod         string `json:"verify_method"`
		WasActivatedFromStub bool   `json:"was_activated_from_stub"`
	}{Attempts: 1, VerifyMethod: firstNonEmpty(method, "sms"), WasActivatedFromStub: false})
	if err != nil {
		return `{"attempts":1,"verify_method":"sms","was_activated_from_stub":false}`
	}
	return string(body)
}

func nativeCodeEntryMethod(method string) string {
	switch method {
	case "voice", "email_otp":
		return "1"
	default:
		return "2"
	}
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
	fields := nativeDeviceMapFields(state)
	return map[string]string{
		"mistyped":                        "7",
		"offline_ab":                      `{"exposure":[],"exp_hash":[],"metrics":{}}`,
		"client_metrics":                  `{"attempts":1,"app_campaign_download_source":"google-play|unknown","was_activated_from_stub":false}`,
		"read_phone_permission_granted":   "0",
		"sim_state":                       "1",
		"network_operator_name":           fields["network_operator_name"],
		"sim_operator_name":               fields["sim_operator_name"],
		"device_name":                     "HWTRT-Q",
		"feo2_query_status":               fields["feo2_query_status"],
		"is_foa_fdid_app_installed":       "false",
		"device_ram":                      fields["device_ram"],
		"language_selector_time_spent":    "0",
		"language_selector_clicked_count": "0",
		"db":                              fields["db"],
		"recaptcha":                       fields["recaptcha"],
		"network_radio_type":              fields["network_radio_type"],
		"simnum":                          fields["simnum"],
		"hasinrc":                         fields["hasinrc"],
		"pid":                             fields["pid"],
		"rc":                              fields["rc"],
		"_ge":                             `{"sb":false,"sv":false}`,
		"mcc":                             fields["mcc"],
		"mnc":                             fields["mnc"],
		"sim_mcc":                         fields["sim_mcc"],
		"sim_mnc":                         fields["sim_mnc"],
	}
}

func parseExistProbeResult(data map[string]any) EngineProbeResult {
	status := responseStatus(data)
	reason := responseReason(data)
	methodStatuses := verificationMethodStatuses(data, nil)
	smsWait := verificationSMSCooldownSeconds(data)
	smsWaitExhausted := verificationSMSWaitExhausted(data)
	blocked := status == "blocked" || reason == "blocked"
	baseProtocolRejected := existProtocolRejected(status, reason)
	invalidNumber := existInvalidNumberReason(reason)
	rateLimited := existRateLimitedReason(reason)
	registered := !baseProtocolRejected && !blocked && !invalidNumber && !rateLimited && (waOldFallbackEligible(data) || existRegisteredSignal(status, reason, data))
	protocolRejected := baseProtocolRejected
	notRegistered := false
	registeredKnown := registered || invalidNumber
	smsRouteUnavailable := existRouteUnavailableReason(reason)
	canSendSMS := smsProbeAvailableByCooldownOnly(smsWait, smsWaitExhausted, blocked, protocolRejected, invalidNumber, rateLimited, smsRouteUnavailable)
	methodStatuses = ensureSendSMSMethodStatus(methodStatuses, smsWait > 0 || smsWaitExhausted || canSendSMS, canSendSMS, smsWait)
	methods := methodsFromStatuses(methodStatuses)
	reachable := !protocolRejected && !blocked && !invalidNumber && !rateLimited && (existReachableStatus(status) || registered || notRegistered || status != "" || reason != "")
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

func existRouteUnavailableReason(reason string) bool {
	switch reason {
	case "no_routes", "route_not_found", "route_unavailable":
		return true
	default:
		return false
	}
}

func existRegisteredSignal(status string, reason string, data map[string]any) bool {
	if existRegisteredReason(reason) {
		return true
	}
	if existRegisteredStatus(status) {
		return true
	}
	return firstNonEmpty(jsonString(data["new_jid"]), jsonString(data["jid"]), jsonString(data["registration_jid"])) != ""
}

func existRegisteredReason(reason string) bool {
	switch reason {
	case "security_code", "second_code", "device_confirm_or_second_code", "consent", "consent_parent_linking_already_registered":
		return true
	default:
		return false
	}
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

func methodsFromStatuses(statuses []VerificationMethodStatus) []waappv1.VerificationDeliveryMethod {
	seen := map[waappv1.VerificationDeliveryMethod]struct{}{}
	out := make([]waappv1.VerificationDeliveryMethod, 0, len(statuses))
	for _, status := range statuses {
		if status.Method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED {
			continue
		}
		if _, ok := seen[status.Method]; ok {
			continue
		}
		seen[status.Method] = struct{}{}
		out = append(out, status.Method)
	}
	return out
}

func verificationMethod(name string) waappv1.VerificationDeliveryMethod {
	switch verificationMethodCode(name) {
	case "send_sms", "sms":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
	case "voice":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_VOICE
	case "flash":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_FLASH
	case "wa_old":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_WA_OLD
	case "email_otp":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_EMAIL_OTP
	case "passkey":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_PASSKEY
	case "silent_auth":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH
	case "silent_auth_ts_43":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SILENT_AUTH_TS43
	case "recaptcha":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_RECAPTCHA
	case "oauth_email":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_OAUTH_EMAIL
	case "discoverable_credential":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_DISCOVERABLE_CREDENTIAL
	case "acc_tr":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER
	case "standalone":
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_STANDALONE_APP
	default:
		return waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_UNSPECIFIED
	}
}

func verificationMethodStatuses(data map[string]any, methods []waappv1.VerificationDeliveryMethod) []VerificationMethodStatus {
	_ = methods
	out := []VerificationMethodStatus{}
	for _, code := range fallbackVerificationMethodCodes(data) {
		if !verificationMethodVisibleForProbe(data, code) {
			continue
		}
		cooldown := verificationCodeCooldownSeconds(data, code)
		out = append(out, VerificationMethodStatus{
			Method:          verificationMethod(code),
			Code:            code,
			Available:       cooldown <= 0 && !verificationCodeWaitExhausted(data, code),
			CooldownSeconds: cooldown,
		})
	}
	return out
}

func verificationMethodCode(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "send_sms", "sms":
		return "send_sms"
	case "voice", "call", "phone_call":
		return "voice"
	case "flash":
		return "flash"
	case "wa_old", "wa-old", "old_wa":
		return "wa_old"
	case "email", "email_otp", "email-otp":
		return "email_otp"
	case "passkey":
		return "passkey"
	case "silent_auth", "silent-auth":
		return "silent_auth"
	case "silent_auth_ts_43", "silent-auth-ts-43", "silent_auth_ts43":
		return "silent_auth_ts_43"
	case "recaptcha":
		return "recaptcha"
	case "oauth_email", "oauth-email":
		return "oauth_email"
	case "discoverable_credential", "discoverable-credential":
		return "discoverable_credential"
	case "acc_tr", "account_transfer", "account-transfer":
		return "acc_tr"
	case "standalone", "acverify", "app":
		return "standalone"
	default:
		return ""
	}
}

func fallbackVerificationMethodCodes(data map[string]any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, raw := range stringList(data["fallback_methods"]) {
		code := verificationMethodCode(raw)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func waOldFallbackEligible(data map[string]any) bool {
	for _, code := range fallbackVerificationMethodCodes(data) {
		if code == "wa_old" {
			return verificationMethodVisibleForProbe(data, code)
		}
	}
	return false
}

func verificationMethodVisibleForProbe(data map[string]any, code string) bool {
	switch code {
	case "wa_old":
		eligibility, ok := firstPresentJSONInt64(data["pref_wa_old_eligibility"], data["wa_old_eligible"])
		if !ok {
			return false
		}
		return eligibility != 0 && eligibility != 4
	case "send_sms":
		return true
	case "voice", "flash", "passkey", "silent_auth", "silent_auth_ts_43", "recaptcha", "oauth_email", "discoverable_credential", "acc_tr", "standalone":
		return true
	case "email_otp":
		eligibility, ok := firstPresentJSONInt64(data["pref_email_otp_eligibility"], data["email_otp_eligible"])
		return !ok || eligibility == 1
	default:
		return false
	}
}

func verificationCodeCooldownSeconds(data map[string]any, code string) int64 {
	switch code {
	case "send_sms":
		return firstJSONWaitSeconds(data["send_sms_wait"], data["send_sms_retry_after"], data["send_sms_retry_time"], data["pref_send_sms_wait_time"], data["EXTRA_SEND_SMS_RETRY_TIME"])
	case "voice":
		return firstJSONWaitSeconds(data["voice_wait"], data["voice_retry_after"], data["voice_retry_time"], data["pref_voice_wait_time"], data["EXTRA_VOICE_RETRY_TIME"])
	case "flash":
		return firstJSONWaitSeconds(data["flash_wait"], data["flash_retry_after"], data["flash_retry_time"], data["pref_flash_wait_time"], data["EXTRA_FLASH_RETRY_TIME"])
	case "wa_old":
		return firstJSONWaitSeconds(data["wa_old_wait"], data["wa_old_retry_time"], data["EXTRA_WA_OLD_RETRY_TIME"], data["pref_wa_old_wait_time"])
	case "email_otp":
		return firstJSONWaitSeconds(data["email_otp_wait"], data["email_otp_retry_time"], data["EXTRA_EMAIL_OTP_RETRY_TIME"], data["pref_email_otp_wait_time"])
	case "silent_auth":
		return firstJSONWaitSeconds(data["silent_auth_wait"], data["silent_auth_retry_time"], data["EXTRA_SILENT_AUTH_RETRY_TIME"], data["pref_silent_auth_wait_time"])
	case "silent_auth_ts_43":
		return firstJSONWaitSeconds(data["silent_auth_ts_43_wait"], data["silent_auth_ts_43_retry_time"], data["EXTRA_SILENT_AUTH_TS_43_RETRY_TIME"], data["pref_silent_auth_ts_43_wait_time"])
	case "passkey":
		return firstJSONWaitSeconds(data["passkey_wait"], data["passkey_retry_time"], data["EXTRA_PASSKEY_RETRY_TIME"], data["pref_passkey_wait_time"])
	case "recaptcha":
		return firstJSONWaitSeconds(data["recaptcha_wait"], data["recaptcha_retry_time"], data["EXTRA_RECAPTCHA_RETRY_TIME"], data["pref_recaptcha_wait_time"])
	default:
		return 0
	}
}

func verificationCodeWaitExhausted(data map[string]any, code string) bool {
	switch code {
	case "send_sms":
		return firstJSONWaitExhausted(data["send_sms_wait"], data["send_sms_retry_after"], data["send_sms_retry_time"], data["pref_send_sms_wait_time"], data["EXTRA_SEND_SMS_RETRY_TIME"])
	case "voice":
		return firstJSONWaitExhausted(data["voice_wait"], data["voice_retry_after"], data["voice_retry_time"], data["pref_voice_wait_time"], data["EXTRA_VOICE_RETRY_TIME"])
	case "flash":
		return firstJSONWaitExhausted(data["flash_wait"], data["flash_retry_after"], data["flash_retry_time"], data["pref_flash_wait_time"], data["EXTRA_FLASH_RETRY_TIME"])
	case "wa_old":
		return firstJSONWaitExhausted(data["wa_old_wait"], data["wa_old_retry_time"], data["EXTRA_WA_OLD_RETRY_TIME"], data["pref_wa_old_wait_time"])
	case "email_otp":
		return firstJSONWaitExhausted(data["email_otp_wait"], data["email_otp_retry_time"], data["EXTRA_EMAIL_OTP_RETRY_TIME"], data["pref_email_otp_wait_time"])
	case "silent_auth":
		return firstJSONWaitExhausted(data["silent_auth_wait"], data["silent_auth_retry_time"], data["EXTRA_SILENT_AUTH_RETRY_TIME"], data["pref_silent_auth_wait_time"])
	case "silent_auth_ts_43":
		return firstJSONWaitExhausted(data["silent_auth_ts_43_wait"], data["silent_auth_ts_43_retry_time"], data["EXTRA_SILENT_AUTH_TS_43_RETRY_TIME"], data["pref_silent_auth_ts_43_wait_time"])
	case "passkey":
		return firstJSONWaitExhausted(data["passkey_wait"], data["passkey_retry_time"], data["EXTRA_PASSKEY_RETRY_TIME"], data["pref_passkey_wait_time"])
	case "recaptcha":
		return firstJSONWaitExhausted(data["recaptcha_wait"], data["recaptcha_retry_time"], data["EXTRA_RECAPTCHA_RETRY_TIME"], data["pref_recaptcha_wait_time"])
	default:
		return false
	}
}

func verificationSMSCooldownSeconds(data map[string]any) int64 {
	return firstJSONWaitSeconds(
		data["sms_wait"],
		data["sms_wait_time"],
		data["sms_retry_after"],
		data["sms_retry_time"],
		data["pref_sms_wait_time"],
		data["EXTRA_SMS_RETRY_TIME"],
		data["retry_after"],
		data["send_sms_wait"],
		data["send_sms_retry_after"],
		data["send_sms_retry_time"],
		data["pref_send_sms_wait_time"],
		data["EXTRA_SEND_SMS_RETRY_TIME"],
	)
}

func verificationSMSWaitExhausted(data map[string]any) bool {
	return firstJSONWaitExhausted(
		data["sms_wait"],
		data["sms_wait_time"],
		data["sms_retry_after"],
		data["sms_retry_time"],
		data["pref_sms_wait_time"],
		data["EXTRA_SMS_RETRY_TIME"],
		data["retry_after"],
	)
}

func smsProbeAvailableByCooldownOnly(smsWait int64, smsWaitExhausted bool, blocked bool, protocolRejected bool, invalidNumber bool, rateLimited bool, routeUnavailable bool) bool {
	return smsWait <= 0 && !smsWaitExhausted && !blocked && !protocolRejected && !invalidNumber && !rateLimited && !routeUnavailable
}

func ensureSendSMSMethodStatus(statuses []VerificationMethodStatus, visible bool, available bool, cooldownSeconds int64) []VerificationMethodStatus {
	if !visible {
		return statuses
	}
	for i := range statuses {
		if statuses[i].Code == "send_sms" || statuses[i].Method == waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS {
			statuses[i].Code = "send_sms"
			statuses[i].Method = waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS
			statuses[i].Available = available
			statuses[i].CooldownSeconds = cooldownSeconds
			return statuses
		}
	}
	return append(statuses, VerificationMethodStatus{
		Method:          waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS,
		Code:            "send_sms",
		Available:       available,
		CooldownSeconds: cooldownSeconds,
	})
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

func firstPresentJSONInt64(values ...any) (int64, bool) {
	for _, value := range values {
		if jsonValuePresent(value) {
			return jsonInt64(value), true
		}
	}
	return 0, false
}

func firstJSONWaitSeconds(values ...any) int64 {
	for _, value := range values {
		if result := normalizeWaitSeconds(jsonInt64(value)); result > 0 {
			return result
		}
	}
	return 0
}

func firstJSONWaitExhausted(values ...any) bool {
	for _, value := range values {
		if jsonValuePresent(value) {
			return jsonInt64(value) == -1
		}
	}
	return false
}

func normalizeWaitSeconds(value int64) int64 {
	if value <= 0 {
		return 0
	}
	now := time.Now()
	nowMS := now.UnixMilli()
	if value >= 1_000_000_000_000 {
		if value <= nowMS {
			return 0
		}
		return (value - nowMS + 999) / 1000
	}
	nowSeconds := now.Unix()
	if value >= 1_000_000_000 {
		if value <= nowSeconds {
			return 0
		}
		return value - nowSeconds
	}
	return value
}

func jsonValuePresent(value any) bool {
	if value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}
