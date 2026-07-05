package app

import (
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
)

const contactMessagePreviewLimit = 96

func contactMessagePreview(plaintext string, redacted string, payloadRef string, state waappv1.MessageEncryptionState) string {
	value := shared.FirstNonEmpty(accountMessageDisplayText(plaintext), accountMessageDisplayText(redacted), payloadTextSummary(payloadRef), messageStatePreview(state))
	return truncatePreview(value, contactMessagePreviewLimit)
}

func truncatePreview(value string, limit int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if value == "" || limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func messageStatePreview(state waappv1.MessageEncryptionState) string {
	switch state {
	case waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_ENCRYPTED:
		return "消息待解密"
	case waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_DECRYPTION_FAILED:
		return "消息解密失败"
	default:
		return ""
	}
}
