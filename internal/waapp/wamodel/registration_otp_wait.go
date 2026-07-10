package wamodel

// RegistrationOTPWait is the transient registration state persisted while the
// dashboard waits for the user to submit the OTP. It is keyed both by
// verification request and by WA account so either side can look it up.
type RegistrationOTPWait struct {
	WAAccountID           string                 `json:"wa_account_id"`
	VerificationRequestID string                 `json:"verification_request_id"`
	ResumeURL             string                 `json:"resume_url"`
	CreatedAtUnix         int64                  `json:"created_at_unix"`
	RegistrationProxy     *RegistrationProxyWait `json:"registration_proxy,omitempty"`
}

// RegistrationProxyWait is transient-only registration egress state. ProxyURL
// is sensitive and must never be copied into dashboard responses or durable
// task records.
type RegistrationProxyWait struct {
	ProxyURL      string `json:"proxy_url"`
	ProxyMode     string `json:"proxy_mode"`
	CountryCode   string `json:"country_code"`
	Source        string `json:"source"`
	RouteID       string `json:"route_id"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

func RegistrationOTPWaitKey(verificationRequestID string) string {
	return "wa-registration-otp-wait:verification:" + verificationRequestID
}

func RegistrationOTPWaitAccountKey(waAccountIDValue string) string {
	return "wa-registration-otp-wait:account:" + waAccountIDValue
}
