package app

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

var waAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_-]{0,127}$`)

func newWAAccount(id string, phone *waappv1.PhoneTarget, status waappv1.WAAccountStatus, audit *waappv1.AuditStamp) *waappv1.WAAccount {
	phone = normalizePhone(phone)
	return &waappv1.WAAccount{
		WaAccountId: strings.TrimSpace(id),
		Phone:       phone,
		Status:      normalizeWAAccountStatus(status),
		Audit:       audit,
	}
}

func withWAAccountStatus(account *waappv1.WAAccount, status waappv1.WAAccountStatus, updatedAt time.Time) *waappv1.WAAccount {
	createdAt := waAccountCreatedAt(account)
	if createdAt.IsZero() {
		createdAt = updatedAt
	}
	return newWAAccount(waAccountID(account), account.GetPhone(), status, audit(createdAt, updatedAt))
}

func waAccountID(account *waappv1.WAAccount) string {
	return strings.TrimSpace(account.GetWaAccountId())
}

func waAccountStatus(account *waappv1.WAAccount) waappv1.WAAccountStatus {
	if account == nil {
		return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED
	}
	return normalizeWAAccountStatus(account.GetStatus())
}

func normalizeWAAccountStatus(status waappv1.WAAccountStatus) waappv1.WAAccountStatus {
	if status != waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED {
		return status
	}
	return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION
}

func parseWAAccountStatus(value string) waappv1.WAAccountStatus {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_UNSPECIFIED
	}
	if !strings.HasPrefix(value, "WA_ACCOUNT_STATUS_") {
		value = "WA_ACCOUNT_STATUS_" + value
	}
	return waappv1.WAAccountStatus(waappv1.WAAccountStatus_value[value])
}

func waAccountStatusStorageValue(account *waappv1.WAAccount) string {
	return waAccountStatus(account).String()
}

func waAccountCreatedAt(account *waappv1.WAAccount) time.Time {
	return timeFromProto(account.GetAudit().GetCreatedAt())
}

func waAccountUpdatedAt(account *waappv1.WAAccount) time.Time {
	return timeFromProto(account.GetAudit().GetUpdatedAt())
}

func requireWAAccountID(value string) (string, error) {
	accountID := strings.TrimSpace(value)
	if accountID == "" {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "wa_account_id is required", false)
	}
	if !waAccountIDPattern.MatchString(accountID) {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "wa_account_id must use letters, digits, colon, underscore or dash", false)
	}
	return accountID, nil
}

func requireWAAccountIDValue(value string) (string, error) {
	accountID, err := requireWAAccountID(value)
	if err != nil {
		return "", fmt.Errorf("%w", err)
	}
	return accountID, nil
}
