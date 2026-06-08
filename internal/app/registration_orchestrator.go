package app

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *Server) StartRegistration(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if s == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "wa-app service is not configured", false)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	gateway := &actionGateway{server: s}
	basePayload := cloneActionPayload(payload)
	basePayload["purpose"] = firstNonEmpty(textField(basePayload, "purpose"), "WA_REGISTRATION")
	basePayload["proxy_session_mode"] = firstNonEmpty(textField(basePayload, "proxy_session_mode"), "STICKY")

	fingerprint, err := gateway.generateTransientFingerprint(ctx, basePayload)
	if err != nil {
		return nil, err
	}
	fingerprintRef := firstNonEmpty(textField(fingerprint, "fingerprint_ref"), textField(fingerprint, "transient_fingerprint_ref"))
	state, err := gateway.loadTransientState(ctx, fingerprintRef)
	if err != nil {
		return nil, err
	}
	runner, route, managedRoute, err := gateway.registrationRequestRunner(ctx, basePayload)
	if err != nil {
		return nil, err
	}
	phone := normalizePhone(phoneFromAction(basePayload))
	codeResult, updatedState := runner.requestVerificationCodeWithState(ctx, EngineRegistrationInput{Phone: phone}, state)
	runner.CloseIdleConnections()
	_ = gateway.server.runtime.DeleteTransientState(context.Background(), fingerprintRef)
	if !verificationCodeRequestAccepted(codeResult) {
		return rejectedRegistrationResult(basePayload, registrationRequestFailureMap(codeResult, route, managedRoute)), nil
	}
	account, profile, protocol, err := gateway.server.commitNativeState(ctx, phone, updatedState)
	if err != nil {
		return nil, err
	}
	record := gateway.server.newVerificationCodeRequestRecord(account, profile, waappv1.VerificationDeliveryMethod_VERIFICATION_DELIVERY_METHOD_SMS, codeResult)
	if err := gateway.server.store.SaveVerificationRequest(ctx, record); err != nil {
		_ = gateway.discardRejectedRegistration(context.Background(), basePayload, waAccountID(account), record.GetVerificationRequestId())
		return nil, err
	}
	verificationRequestID := record.GetVerificationRequestId()
	if managedRoute {
		if err := gateway.saveRegistrationProxyRoute(ctx, verificationRequestID, route); err != nil {
			_ = gateway.discardRejectedRegistration(context.Background(), basePayload, waAccountID(account), verificationRequestID)
			return nil, err
		}
	}
	wait := registrationOTPWait{
		WAAccountID:           waAccountID(account),
		VerificationRequestID: verificationRequestID,
		CreatedAtUnix:         time.Now().UTC().Unix(),
	}
	if err := gateway.saveRegistrationOTPWait(ctx, wait, registrationOTPWaitDefaultTTL); err != nil {
		_ = gateway.releaseRegistrationProxyRoute(context.Background(), wait.VerificationRequestID)
		_ = gateway.discardRejectedRegistration(context.Background(), basePayload, waAccountID(account), verificationRequestID)
		return nil, err
	}
	return map[string]any{
		"success":                 true,
		"status":                  record.GetStatus().String(),
		"error_message":           "",
		"phone":                   objectField(basePayload, "phone"),
		"wa_account_id":           waAccountID(account),
		"client_profile_id":       profile.GetClientProfileId(),
		"protocol_profile_id":     protocol.GetProtocolProfileId(),
		"verification_request_id": verificationRequestID,
		"verification_request":    protoMap(record),
		"registration_phase":      registrationPhase(true, verificationRequestID),
		"fingerprint_persistence": "COMMITTED",
		"persisted":               true,
		"proxy":                   registrationOrchestratorProxySummary(registrationProxyRouteMap(route, managedRoute)),
	}, nil
}

func cloneActionPayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func registrationPhase(success bool, verificationRequestID string) string {
	if success && strings.TrimSpace(verificationRequestID) != "" {
		return "OTP_WAITING"
	}
	return "OTP_REQUEST_FAILED"
}

func verificationCodeRequestAccepted(result EngineCodeResult) bool {
	return result.Err == nil && (result.Status == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT || result.Status == waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING)
}

func registrationRequestFailureMap(result EngineCodeResult, route DynamicProxyRoute, managedRoute bool) map[string]any {
	protoErr := ToProtoError(result.Err)
	return map[string]any{
		"success":       false,
		"status":        firstNonEmpty(result.Status.String(), "VERIFICATION_REQUEST_STATUS_REJECTED"),
		"error":         protoMap(protoErr),
		"error_message": protoErr.GetMessage(),
		"proxy":         registrationProxyRouteMap(route, managedRoute),
	}
}

func (g *actionGateway) discardRejectedRegistration(ctx context.Context, basePayload map[string]any, waAccountID string, verificationRequestID string) error {
	if strings.TrimSpace(verificationRequestID) != "" {
		_ = g.releaseRegistrationProxyRoute(context.Background(), verificationRequestID)
	}
	if strings.TrimSpace(waAccountID) == "" {
		return nil
	}
	result, err := g.cleanupFailedRegistration(ctx, map[string]any{
		"wa_account_id":           waAccountID,
		"verification_request_id": verificationRequestID,
	})
	if err != nil {
		return err
	}
	if boolField(result, "success") {
		return nil
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, firstNonEmpty(textField(result, "error_message"), "discard rejected WA registration failed"), false)
}

func rejectedRegistrationResult(basePayload map[string]any, requested map[string]any) map[string]any {
	errorMessage := firstNonEmpty(textField(requested, "error_message"), textField(objectField(requested, "error"), "message"), "WA registration request was rejected")
	return map[string]any{
		"success":                 false,
		"status":                  firstNonEmpty(textField(requested, "status"), "OTP_REQUEST_REJECTED"),
		"error":                   objectField(requested, "error"),
		"error_message":           errorMessage,
		"reject_reason":           registrationRejectReason(errorMessage),
		"phone":                   objectField(basePayload, "phone"),
		"registration_phase":      "OTP_REQUEST_REJECTED",
		"fingerprint_persistence": "DISCARDED",
		"persisted":               false,
		"proxy":                   registrationOrchestratorProxySummary(objectField(requested, "proxy")),
	}
}

func registrationRejectReason(errorMessage string) string {
	normalized := strings.ToLower(errorMessage)
	if strings.Contains(normalized, "no_routes") {
		return "no_routes"
	}
	if strings.Contains(normalized, "blocked") {
		return "blocked"
	}
	return "rejected"
}

func registrationOrchestratorProxySummary(proxy map[string]any) map[string]any {
	mode := firstNonEmpty(textField(proxy, "proxy_mode"), "DIRECT")
	countryCode := firstNonEmpty(textField(proxy, "country_code"), "LOCAL")
	return map[string]any{"proxy_mode": mode, "country_code": countryCode}
}
