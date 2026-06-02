package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const numberProbeProxyLeaseTTL = time.Minute

func (s *Server) ProbeNumberSMS(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	ctxData := actionContext(payload)
	phone := normalizePhone(phoneFromAction(payload))
	if phone.GetE164Number() == "" {
		err := NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone is required", false)
		result := numberProbeError(payload, err)
		logNumberProbeResult(ctxData, phone, DynamicProxyLease{}, result)
		return result, nil
	}
	engine, ok := s.runner.(*NativeEngine)
	if !ok {
		err := NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "native engine is required", false)
		result := numberProbeError(payload, err)
		logNumberProbeResult(ctxData, phone, DynamicProxyLease{}, result)
		return result, nil
	}
	lease, proxyURL, proxy, releaseProxy, err := s.numberProbeProxy(ctx, payload, ctxData.GetCorrelationId())
	if err != nil {
		result := numberProbeProxyFailure(payload, err)
		logNumberProbeResult(ctxData, phone, DynamicProxyLease{}, result)
		return result, nil
	}
	defer releaseProxy()

	probeEngine, err := engine.WithProxyURL(proxyURL)
	if err != nil {
		result := numberProbeError(payload, err)
		logNumberProbeResult(ctxData, phone, lease, result)
		return result, nil
	}
	state, err := probeEngine.newState(phone)
	if err != nil {
		result := numberProbeError(payload, err)
		logNumberProbeResult(ctxData, phone, lease, result)
		return result, nil
	}
	fingerprint := map[string]any{
		"fingerprint_persistence": "RANDOM_NOT_COMMITTED",
		"fingerprint":             fingerprintSummary(phoneProfileToProto(phone, state.Profile)),
	}
	account := probeResultMap(probeEngine.probeAccountWithState(ctx, EngineRegistrationInput{WorkspaceID: ctxData.GetWorkspaceId(), Phone: phone}, state))
	sms := smsProbeMap(account)
	result := buildNumberProbeResult(payload, proxy, fingerprint, account, sms)
	logNumberProbeResult(ctxData, phone, lease, result)
	return result, nil
}

func (s *Server) acquireNumberProbeProxy(ctx context.Context, correlationID string) (DynamicProxyLease, error) {
	if s == nil || s.proxyRuntime == nil {
		return DynamicProxyLease{}, fmt.Errorf("PROXY_RUNTIME_API_BASE_URL is required")
	}
	return s.proxyRuntime.AcquireUSDynamic(ctx, "WA_NUMBER_PROBE", correlationID, numberProbeProxyLeaseTTL)
}

func (s *Server) numberProbeProxy(ctx context.Context, payload map[string]any, correlationID string) (DynamicProxyLease, string, map[string]any, func(), error) {
	if proxyURL, lease, countryCode := sharedNumberProbeProxy(payload); proxyURL != "" {
		proxy := map[string]any{"success": true, "accepted": true, "proxy_mode": "SHARED_DYNAMIC_IP", "country_code": firstNonEmpty(countryCode, "UNKNOWN"), "account_id": lease.AccountID, "lease_id": lease.LeaseID}
		return lease, proxyURL, proxy, func() {}, nil
	}
	lease, err := s.acquireNumberProbeProxy(ctx, correlationID)
	if err != nil {
		return DynamicProxyLease{}, "", nil, func() {}, err
	}
	proxy := map[string]any{"success": true, "accepted": true, "proxy_mode": "US_RANDOM_DYNAMIC_IP", "country_code": "US", "account_id": lease.AccountID, "lease_id": lease.LeaseID}
	return lease, lease.ProxyURL, proxy, func() { s.proxyRuntime.Release(context.Background(), lease.AccountID) }, nil
}

func sharedNumberProbeProxy(payload map[string]any) (string, DynamicProxyLease, string) {
	proxyURL := firstNonEmpty(textField(payload, "proxy_url"), textField(objectField(payload, "proxy"), "proxy_url"))
	state := map[string]any{}
	rawState := firstNonEmpty(textField(payload, "proxy_state_json"), textField(payload, "state_json"), textField(objectField(payload, "proxy"), "state_json"))
	if rawState != "" {
		_ = json.Unmarshal([]byte(rawState), &state)
	}
	if proxyURL == "" {
		proxyURL = textField(state, "_gopay_proxy")
	}
	return proxyURL, DynamicProxyLease{
		AccountID: firstNonEmpty(textField(payload, "proxy_account_id"), textField(state, "_proxy_runtime_account_id")),
		LeaseID:   firstNonEmpty(textField(payload, "proxy_lease_id"), textField(state, "_proxy_runtime_lease_id")),
		ProxyURL:  proxyURL,
	}, firstNonEmpty(textField(payload, "proxy_country_code"), textField(state, "_gopay_country_code"), textField(state, "_proxy_runtime_preflight_country_code"))
}

func buildNumberProbeResult(input map[string]any, proxy map[string]any, fingerprint map[string]any, account map[string]any, sms map[string]any) map[string]any {
	accountStatus := firstNonEmpty(textField(account, "status"), textField(account, "account_status"), textField(objectField(account, "probe"), "status"), "UNKNOWN")
	accountRawStatus := firstNonEmpty(textField(account, "raw_status"), textField(account, "rawStatus"), textField(account, "status_text"))
	accountRawReason := firstNonEmpty(textField(account, "raw_reason"), textField(account, "reason"))
	accountError := firstNonEmpty(textField(account, "error_message"), textField(objectField(account, "error"), "message"))
	accountFlow := firstNonEmpty(textField(account, "account_flow"), accountProbeFlowUnknown)
	smsStatus := firstNonEmpty(textField(sms, "status"), textField(sms, "sms_status"), textField(sms, "route_status"), "UNKNOWN")
	methodStatuses := objectListField(account, "method_statuses")
	registered, registeredKnown := optionalBoolField(account, "registered")
	if statusIn(accountRawStatus, "exists", "registered", "account_exists") || statusIn(accountStatus, "registered", "exists") {
		registered = true
		registeredKnown = true
	}
	blocked := accountFlow == accountProbeFlowBlocked || boolField(account, "blocked") || statusIn(accountRawStatus, "blocked") || statusIn(accountRawReason, "blocked") || statusIn(accountStatus, "blocked")
	accountReachable := statusIn(accountStatus, "reachable", "account_probe_status_reachable", "ok", "sent", "valid", "exists") || statusIn(accountRawStatus, "ok", "sent", "valid", "exists") || accountFlow == accountProbeFlowRegistered || accountFlow == accountProbeFlowNotRegistered
	smsAvailable := boolField(sms, "can_send_sms") || boolField(sms, "sms_available") || statusIn(smsStatus, "available", "sms_available", "verification_request_status_sent", "sent", "waiting", "ok")
	smsWaitSeconds := firstNumberValue(sms, "sms_wait_seconds", "wait_seconds", "retry_after_seconds", "cooldown_seconds", "remaining_seconds", "retry_after", "wait")
	smsWaitUntil := firstNonEmpty(textField(sms, "sms_wait_until"), textField(sms, "wait_until"), textField(sms, "retry_after_at"), textField(sms, "cooldown_until"))
	proxyAccepted := boolField(proxy, "accepted")
	requestFailed := !proxyAccepted || accountProbeRequestFailed(accountFlow, accountStatus, accountRawStatus, accountRawReason, accountError)
	requestSucceeded := !requestFailed
	if requestFailed {
		accountFlow = accountProbeFlowProbeFailed
	}
	canRegister := canRegisterValue(requestSucceeded, accountReachable, smsAvailable, registeredKnown, registered, blocked, accountFlow)
	failureReason := ""
	if requestFailed {
		failureReason = numberProbeFailureReason(proxyAccepted, accountStatus, accountRawStatus, accountRawReason, accountError)
	}
	return map[string]any{
		"success":                 requestSucceeded,
		"passed":                  requestSucceeded,
		"request_failed":          requestFailed,
		"error_message":           failureReason,
		"reject_reason":           failureReason,
		"phone":                   objectField(input, "phone"),
		"proxy":                   map[string]any{"proxy_mode": firstNonEmpty(textField(proxy, "proxy_mode"), "US_RANDOM_DYNAMIC_IP"), "country_code": firstNonEmpty(textField(proxy, "country_code"), "US")},
		"fingerprint_persistence": firstNonEmpty(textField(fingerprint, "fingerprint_persistence"), "RANDOM_NOT_COMMITTED"),
		"fingerprint":             objectField(fingerprint, "fingerprint"),
		"account_probe":           account,
		"sms_probe":               sms,
		"phone_status": map[string]any{
			"account_status":     accountStatus,
			"account_flow":       accountFlow,
			"account_raw_status": accountRawStatus,
			"account_raw_reason": accountRawReason,
			"account_error":      accountError,
			"account_reachable":  accountReachable,
			"request_failed":     requestFailed,
			"registered":         optionalBoolValue(registered, registeredKnown),
			"blocked":            blocked,
			"sms_status":         smsStatus,
			"sms_available":      smsAvailable,
			"sms_wait_seconds":   smsWaitSeconds,
			"sms_wait_until":     smsWaitUntil,
			"method_statuses":    methodStatuses,
			"reject_reason":      failureReason,
			"can_register":       canRegister,
		},
	}
}

func accountProbeRequestFailed(accountFlow string, accountStatus string, accountRawStatus string, accountRawReason string, accountError string) bool {
	if strings.TrimSpace(accountError) != "" {
		return true
	}
	if accountFlow == accountProbeFlowRegistered {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(accountStatus))
	raw := strings.ToLower(strings.TrimSpace(accountRawStatus + " " + accountRawReason))
	if status == "" || status == "unknown" || status == "account_probe_status_rejected" || status == "rejected" || status == "error" {
		return true
	}
	return strings.Contains(raw, "invalid_skey") || strings.Contains(raw, "bad_token") || strings.Contains(raw, "missing_param") || strings.Contains(raw, "bad_param") || strings.Contains(raw, "old_version")
}

func numberProbeFailureReason(proxyAccepted bool, accountStatus string, accountRawStatus string, accountRawReason string, accountError string) string {
	if !proxyAccepted {
		return "dynamic IP lease unavailable"
	}
	if strings.TrimSpace(accountError) != "" {
		return "account probe request failed: " + accountError
	}
	if accountStatus == "ACCOUNT_PROBE_STATUS_REJECTED" {
		return "account probe request rejected"
	}
	return "account probe request failed: " + firstNonEmpty(accountRawReason, accountRawStatus, accountStatus, "UNKNOWN")
}

func canRegisterValue(requestSucceeded bool, accountReachable bool, smsAvailable bool, registeredKnown bool, registered bool, blocked bool, accountFlow string) any {
	if !requestSucceeded || !accountReachable || !smsAvailable || blocked {
		return false
	}
	if !registeredKnown {
		if accountFlow == accountProbeFlowNotRegistered {
			return true
		}
		return nil
	}
	return !registered
}

func optionalBoolValue(value bool, known bool) any {
	if !known {
		return nil
	}
	return value
}

func numberProbeProxyFailure(payload map[string]any, err error) map[string]any {
	return map[string]any{
		"success":                 false,
		"passed":                  false,
		"request_failed":          true,
		"error_message":           err.Error(),
		"reject_reason":           err.Error(),
		"phone":                   objectField(payload, "phone"),
		"proxy":                   map[string]any{"proxy_mode": "US_RANDOM_DYNAMIC_IP", "country_code": "US"},
		"fingerprint_persistence": "NOT_CREATED",
		"phone_status": map[string]any{
			"account_status":    "UNKNOWN",
			"account_flow":      accountProbeFlowProbeFailed,
			"account_reachable": false,
			"request_failed":    true,
			"registered":        nil,
			"blocked":           nil,
			"sms_status":        "UNKNOWN",
			"sms_available":     false,
			"sms_wait_seconds":  nil,
			"sms_wait_until":    "",
			"method_statuses":   []map[string]any{},
			"can_register":      false,
		},
	}
}

func numberProbeError(payload map[string]any, err error) map[string]any {
	result := numberProbeProxyFailure(payload, err)
	result["fingerprint_persistence"] = "RANDOM_NOT_COMMITTED"
	return result
}

func logNumberProbeResult(ctxData *waappv1.RequestContext, phone *waappv1.PhoneTarget, lease DynamicProxyLease, result map[string]any) {
	phoneStatus := objectField(result, "phone_status")
	phoneHash := ""
	if phone != nil && phone.GetE164Number() != "" {
		phoneHash = stableID(phone.GetE164Number())
	}
	log.Printf(
		"wa_phone_probe_result workspace=%s correlation=%s phone_hash=%s proxy_account=%s lease_id=%s request_failed=%t success=%t account_flow=%s account_status=%s raw_status=%s raw_reason=%s sms_status=%s sms_available=%t sms_wait_seconds=%v error=%s",
		probeLogValue(ctxData.GetWorkspaceId()),
		probeLogValue(ctxData.GetCorrelationId()),
		phoneHash,
		probeLogValue(lease.AccountID),
		probeLogValue(lease.LeaseID),
		boolField(phoneStatus, "request_failed") || boolField(result, "request_failed"),
		boolField(result, "success"),
		probeLogValue(textField(phoneStatus, "account_flow")),
		probeLogValue(textField(phoneStatus, "account_status")),
		probeLogValue(textField(phoneStatus, "account_raw_status")),
		probeLogValue(textField(phoneStatus, "account_raw_reason")),
		probeLogValue(textField(phoneStatus, "sms_status")),
		boolField(phoneStatus, "sms_available"),
		firstNumberValue(phoneStatus, "sms_wait_seconds"),
		probeLogValue(firstNonEmpty(textField(result, "error_message"), textField(phoneStatus, "account_error"))),
	)
}

func probeLogValue(value string) string {
	value = strings.TrimSpace(strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(value))
	if len(value) <= 160 {
		return value
	}
	return value[:160]
}

func statusIn(value string, expected ...string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, item := range expected {
		if normalized == strings.ToLower(item) {
			return true
		}
	}
	return false
}

func probeResultMap(result EngineProbeResult) map[string]any {
	out := map[string]any{
		"success":           result.Status == waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE,
		"status":            result.Status.String(),
		"account_status":    result.Status.String(),
		"account_flow":      firstNonEmpty(result.AccountFlow, accountProbeFlowUnknown),
		"raw_status":        result.RawStatus,
		"raw_reason":        result.RawReason,
		"blocked":           result.Blocked,
		"sms_wait_seconds":  result.SMSWaitSeconds,
		"can_send_sms":      result.CanSendSMS,
		"supported_methods": enumNames(result.SupportedMethods),
		"method_statuses":   methodStatusMaps(result.MethodStatuses),
	}
	if result.RegisteredKnown {
		out["registered"] = result.Registered
	}
	if result.Err != nil {
		protoErr := ToProtoError(result.Err)
		out["success"] = false
		out["error"] = protoMap(protoErr)
		out["error_message"] = protoErr.GetMessage()
	}
	return out
}

func smsProbeMap(account map[string]any) map[string]any {
	status := firstNonEmpty(textField(account, "account_status"), textField(account, "status"))
	reachable := status == waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REACHABLE.String() || strings.EqualFold(status, "REACHABLE") || strings.EqualFold(status, "ok")
	waitSeconds := firstNumberValue(account, "sms_wait_seconds")
	if !reachable || !boolField(account, "can_send_sms") {
		return map[string]any{"success": false, "status": "UNAVAILABLE", "sms_status": "UNAVAILABLE", "can_send_sms": false, "sms_wait_seconds": waitSeconds}
	}
	return map[string]any{"success": true, "status": "AVAILABLE", "sms_status": "AVAILABLE", "can_send_sms": true, "sms_wait_seconds": waitSeconds}
}

func boolField(data map[string]any, key string) bool {
	value, ok := optionalBoolField(data, key)
	return ok && value
}

func objectListField(data map[string]any, key string) []map[string]any {
	values, ok := data[key].([]map[string]any)
	if ok {
		return values
	}
	raw, ok := data[key].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(map[string]any); ok {
			out = append(out, value)
		}
	}
	return out
}

func methodStatusMaps(statuses []VerificationMethodStatus) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		method := status.Code
		if method == "" {
			method = status.Method.String()
		}
		out = append(out, map[string]any{
			"method":           method,
			"delivery_method":  status.Method.String(),
			"available":        status.Available,
			"cooldown_seconds": status.CooldownSeconds,
		})
	}
	return out
}

func optionalBoolField(data map[string]any, key string) (bool, bool) {
	switch value := data[key].(type) {
	case bool:
		return value, true
	case string:
		if strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes") {
			return true, true
		}
		if strings.EqualFold(value, "false") || value == "0" || strings.EqualFold(value, "no") {
			return false, true
		}
		return false, false
	default:
		return false, false
	}
}

func firstNumberValue(data map[string]any, keys ...string) any {
	for _, key := range keys {
		value := data[key]
		switch typed := value.(type) {
		case int, int32, int64, float32, float64:
			return typed
		case string:
			if strings.TrimSpace(typed) != "" {
				return typed
			}
		}
	}
	return nil
}
