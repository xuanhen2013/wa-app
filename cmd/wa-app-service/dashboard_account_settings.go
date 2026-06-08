package main

import (
	"encoding/json"
	"io"
	"net/http"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *dashboardHTTP) handleSetTwoFactorAuthSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.SetTwoFactorAuthSettings(r.Context(), &waappv1.SetTwoFactorAuthSettingsRequest{
		Context:       accountSettingsRequestContext(payload, "wa-account-2fa"),
		Selector:      accountSettingsSelector(payload),
		Pin:           &waappv1.SensitiveText{Value: textField(payload, "pin")},
		RecoveryEmail: textField(payload, "recovery_email"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "set WA 2FA settings failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleSetAccountEmail(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.SetAccountEmail(r.Context(), &waappv1.SetAccountEmailRequest{
		Context:       accountSettingsRequestContext(payload, "wa-account-email"),
		Selector:      accountSettingsSelector(payload),
		EmailAddress:  textField(payload, "email_address"),
		GoogleIdToken: &waappv1.SensitiveText{Value: textField(payload, "google_id_token")},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "set WA account email failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleRequestAccountEmailOtp(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.RequestAccountEmailOtp(r.Context(), &waappv1.RequestAccountEmailOtpRequest{
		Context:        accountSettingsRequestContext(payload, "wa-account-email-otp"),
		Selector:       accountSettingsSelector(payload),
		LocaleLanguage: firstNonEmpty(textField(payload, "locale_language"), textField(payload, "language"), "en"),
		LocaleCountry:  firstNonEmpty(textField(payload, "locale_country"), textField(payload, "country"), "US"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "request WA account email OTP failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) handleVerifyAccountEmailOtp(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccountSettingsPost(w, r) {
		return
	}
	payload, ok := readAccountSettingsPayload(w, r)
	if !ok {
		return
	}
	resp, err := s.service.VerifyAccountEmailOtp(r.Context(), &waappv1.VerifyAccountEmailOtpRequest{
		Context:  accountSettingsRequestContext(payload, "wa-account-email-otp-verify"),
		Selector: accountSettingsSelector(payload),
		Code:     &waappv1.SensitiveText{Value: firstNonEmpty(textField(payload, "code"), textField(payload, "otp"))},
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "verify WA account email OTP failed"})
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

func (s *dashboardHTTP) requireAccountSettingsPost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return false
	}
	if s.service == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wa-app service is not configured"})
		return false
	}
	return true
}

func readAccountSettingsPayload(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return nil, false
	}
	payload := map[string]any{}
	if len(body) == 0 {
		return payload, true
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be json"})
		return nil, false
	}
	return payload, true
}

func accountSettingsRequestContext(payload map[string]any, prefix string) *waappv1.RequestContext {
	return &waappv1.RequestContext{
		RequestId: firstNonEmpty(textField(payload, "request_id"), newRequestID(prefix)),
	}
}

func accountSettingsSelector(payload map[string]any) *waappv1.AccountLoginSelector {
	selector := objectField(payload, "selector")
	return &waappv1.AccountLoginSelector{
		LoginStateId:         firstNonEmpty(textField(payload, "login_state_id"), textField(selector, "login_state_id")),
		RegisteredIdentityId: firstNonEmpty(textField(payload, "registered_identity_id"), textField(selector, "registered_identity_id")),
		WaAccountId:          firstNonEmpty(textField(payload, "wa_account_id"), textField(selector, "wa_account_id")),
		ClientProfileId:      firstNonEmpty(textField(payload, "client_profile_id"), textField(selector, "client_profile_id")),
	}
}
