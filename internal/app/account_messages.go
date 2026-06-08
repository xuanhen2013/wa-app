package app

import (
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type accountMessageParts struct {
	accountID       string
	sessionID       string
	messageID       string
	kind            waappv1.InboundMessageKind
	encryptionState waappv1.MessageEncryptionState
	ackStatus       waappv1.MessageAckStatus
	contactRef      string
	senderRef       string
	payloadRef      string
	plaintext       string
	redacted        string
	secretRef       string
	lastError       *waappv1.WaError
	receivedAt      time.Time
}

func newAccountMessage(parts accountMessageParts, includeSensitiveText bool) *waappv1.AccountMessage {
	displayPlaintext := accountMessageDisplayText(parts.plaintext)
	displayRedacted := accountMessageDisplayText(parts.redacted)
	text := &waappv1.SensitiveText{
		RedactedValue: firstNonEmpty(displayRedacted, redacted(displayPlaintext), payloadTextSummary(parts.payloadRef)),
		SecretRef:     parts.secretRef,
	}
	if includeSensitiveText {
		text.Value = displayPlaintext
	}
	return &waappv1.AccountMessage{
		AccountMessageId: parts.messageID,
		WaAccountId:      parts.accountID,
		MessageSessionId: parts.sessionID,
		ContactRef:       contactRefForMessage(parts.contactRef, parts.senderRef),
		SenderRef:        parts.senderRef,
		Direction:        accountMessageDirection(parts.kind),
		Source:           waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION,
		Kind:             parts.kind,
		EncryptionState:  parts.encryptionState,
		AckStatus:        parts.ackStatus,
		Text:             text,
		ReceivedAt:       timestamp(parts.receivedAt),
		LastError:        parts.lastError,
	}
}

func newAccountMessageFromInbound(accountID string, msg *waappv1.InboundMessage, decrypted *waappv1.DecryptedMessage, includeSensitiveText bool) *waappv1.AccountMessage {
	if msg == nil {
		return nil
	}
	text := decrypted.GetPlaintextText()
	return newAccountMessage(accountMessageParts{
		accountID:       accountID,
		sessionID:       msg.GetMessageSessionId(),
		messageID:       msg.GetMessageId(),
		kind:            msg.GetKind(),
		encryptionState: msg.GetEncryptionState(),
		ackStatus:       msg.GetAckStatus(),
		contactRef:      msg.GetContactRef(),
		senderRef:       msg.GetSenderRef(),
		payloadRef:      msg.GetPayloadRef(),
		plaintext:       text.GetValue(),
		redacted:        text.GetRedactedValue(),
		secretRef:       text.GetSecretRef(),
		lastError:       msg.GetLastError(),
		receivedAt:      timeFromProto(msg.GetReceivedAt()),
	}, includeSensitiveText)
}

func contactRefForMessage(contactRef string, sender string) string {
	value := strings.TrimSpace(firstNonEmpty(contactRef, sender))
	if value == "" {
		return "unknown"
	}
	return value
}

func accountMessageDirection(kind waappv1.InboundMessageKind) waappv1.AccountMessageDirection {
	if kind == waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_SYSTEM
	}
	return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND
}

func accountMessageDisplayText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if value := waJSONDisplayText(text); value != "" {
		return value
	}
	return text
}

func payloadTextSummary(payloadRef string) string {
	payloadRef = strings.TrimSpace(payloadRef)
	if strings.HasPrefix(payloadRef, "node:") {
		return strings.TrimPrefix(payloadRef, "node:")
	}
	return ""
}
