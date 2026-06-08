package app

import (
	"context"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (e *NativeEngine) ApplyAccountSettings(ctx context.Context, input EngineAccountSettingsInput) EngineAccountSettingsResult {
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = ensureChatStatic(state.ChatStatic)
		_ = e.saveState(ctx, input.ClientProfileID, state)
	}
	proxyURL, err := e.proxyURL()
	if err != nil {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	request := buildAccountSettingsIQ(e.ids.NewID("waiq_"), input)
	if request.Tag == "" {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "account settings operation is not supported", false)}
	}
	client := newChatdClient(chatdConfigForState(proxyURL, state, defaultAccountIQTimeout))
	response, update, err := client.sendAccountIQ(ctx, state, input, defaultWAAppVersion, request)
	if applyChatdConnectionState(&state, update) {
		_ = e.saveState(ctx, input.ClientProfileID, state)
	}
	if err != nil {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "native account settings request failed", accountSettingsRetryableError(err))}
	}
	return accountSettingsResultFromIQ(input.Kind, response)
}

func buildAccountSettingsIQ(id string, input EngineAccountSettingsInput) chatdNode {
	switch input.Kind {
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_TWO_FACTOR_AUTH_SETTINGS:
		return buildTwoFactorAuthSettingsIQ(id, input.Pin, input.RecoveryEmail)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_SET:
		return buildSetAccountEmailIQ(id, input.EmailAddress, input.GoogleIDToken)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST:
		return buildRequestAccountEmailOtpIQ(id, firstNonEmpty(input.LocaleLanguage, "en"), firstNonEmpty(input.LocaleCountry, "US"))
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY:
		return buildVerifyAccountEmailOtpIQ(id, input.Code)
	default:
		return chatdNode{}
	}
}

func buildAccountIQ(id string, iqType string, children []chatdNode) chatdNode {
	return chatdNode{Tag: "iq", Attrs: map[string]string{"to": "s.whatsapp.net", "id": id, "xmlns": "urn:xmpp:whatsapp:account", "type": iqType}, Content: children}
}

func buildTwoFactorAuthSettingsIQ(id string, pin string, recoveryEmail string) chatdNode {
	children := []chatdNode{{Tag: "code", Content: pin}}
	if strings.TrimSpace(recoveryEmail) != "" {
		children = append(children, chatdNode{Tag: "email", Content: recoveryEmail})
	}
	return buildAccountIQ(id, "set", []chatdNode{{Tag: "2fa", Content: children}})
}

func buildSetAccountEmailIQ(id string, emailAddress string, googleIDToken string) chatdNode {
	children := []chatdNode{}
	if strings.TrimSpace(googleIDToken) != "" {
		children = append(children, chatdNode{Tag: "id_token", Content: googleIDToken})
	}
	children = append(children, chatdNode{Tag: "email_address", Content: emailAddress})
	return buildAccountIQ(id, "set", []chatdNode{{Tag: "email", Content: children}})
}

func buildRequestAccountEmailOtpIQ(id string, language string, country string) chatdNode {
	return buildAccountIQ(id, "set", []chatdNode{{Tag: "verify_email", Content: []chatdNode{{Tag: "lg", Content: language}, {Tag: "lc", Content: country}}}})
}

func buildVerifyAccountEmailOtpIQ(id string, code string) chatdNode {
	return buildAccountIQ(id, "get", []chatdNode{{Tag: "verify_email", Content: []chatdNode{{Tag: "code", Content: code}}}})
}

func accountSettingsResultFromIQ(kind waappv1.AccountSettingsOperationKind, node chatdNode) EngineAccountSettingsResult {
	if err := chatdIQError(node); err != nil {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_REJECTED, Err: err}
	}
	switch kind {
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_SET:
		return emailSetResultFromIQ(node)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_REQUEST:
		return emailOtpRequestResultFromIQ(node)
	case waappv1.AccountSettingsOperationKind_ACCOUNT_SETTINGS_OPERATION_KIND_ACCOUNT_EMAIL_OTP_VERIFY:
		return emailOtpVerifyResultFromIQ(node)
	default:
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
	}
}

func emailSetResultFromIQ(node chatdNode) EngineAccountSettingsResult {
	emailNode, ok := chatdChild(node, "email")
	if !ok {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
	}
	if chatdNodeBool(emailNode, "auto_verify") || chatdNodeBool(emailNode, "verified") || chatdNodeBool(emailNode, "confirmed") {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED}
	}
	if chatdNodeBool(emailNode, "do_verify") {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_NEEDS_VERIFICATION}
	}
	return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
}

func emailOtpRequestResultFromIQ(node chatdNode) EngineAccountSettingsResult {
	verifyNode, ok := chatdChild(node, "verify_email")
	if !ok {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING}
	}
	return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_WAITING, WaitTime: chatdNodeDuration(verifyNode, "wait_time")}
}

func emailOtpVerifyResultFromIQ(node chatdNode) EngineAccountSettingsResult {
	verifyNode, ok := chatdChild(node, "verify_email")
	if !ok {
		return EngineAccountSettingsResult{Status: waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_ACCEPTED}
	}
	status := waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_CODE_MISMATCH
	if chatdNodeBool(verifyNode, "code_match") {
		status = waappv1.AccountSettingsOperationStatus_ACCOUNT_SETTINGS_OPERATION_STATUS_VERIFIED
	}
	return EngineAccountSettingsResult{Status: status, WaitTime: chatdNodeDuration(verifyNode, "wait_time")}
}

func accountSettingsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "deadline", "proxy", "dial", "connect", "network", "tls", "no such host", "connection refused", "temporary"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
