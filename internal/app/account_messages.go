package app

import (
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type accountMessageParts struct {
	accountID       string
	sessionID       string
	messageID       string
	kind            waappv1.InboundMessageKind
	encryptionState waappv1.MessageEncryptionState
	ackStatus       waappv1.MessageAckStatus
	direction       waappv1.AccountMessageDirection
	source          waappv1.AccountMessageSource
	contactRef      string
	senderRef       string
	payloadRef      string
	plaintext       string
	redacted        string
	secretRef       string
	lastError       *waappv1.WaError
	receivedAt      time.Time
	readAt          time.Time
	deleteStatus    waappv1.MessageDeleteStatus
	deletedAt       time.Time
}

func newAccountMessage(parts accountMessageParts, includeSensitiveText bool) *waappv1.AccountMessage {
	displayPlaintext := accountMessageDisplayText(parts.plaintext)
	displayRedacted := accountMessageDisplayText(parts.redacted)
	text := &waappv1.SensitiveText{
		RedactedValue: shared.FirstNonEmpty(displayRedacted, shared.Redacted(displayPlaintext), payloadTextSummary(parts.payloadRef)),
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
		Direction:        accountMessageDirection(parts.direction, parts.kind),
		Source:           accountMessageSource(parts.source),
		Kind:             parts.kind,
		EncryptionState:  parts.encryptionState,
		AckStatus:        parts.ackStatus,
		Text:             text,
		ReceivedAt:       shared.ProtoTimestamp(parts.receivedAt),
		LastError:        parts.lastError,
		ReadAt:           shared.ProtoTimestamp(parts.readAt),
		DeleteStatus:     accountMessageDeleteStatus(parts.deleteStatus),
		DeletedAt:        shared.ProtoTimestamp(parts.deletedAt),
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
		direction:       msg.GetDirection(),
		source:          msg.GetSource(),
		contactRef:      msg.GetContactRef(),
		senderRef:       msg.GetSenderRef(),
		payloadRef:      msg.GetPayloadRef(),
		plaintext:       text.GetValue(),
		redacted:        text.GetRedactedValue(),
		secretRef:       text.GetSecretRef(),
		lastError:       msg.GetLastError(),
		receivedAt:      timeFromProto(msg.GetReceivedAt()),
		readAt:          protoTimeOrZero(msg.GetReadAt()),
		deleteStatus:    msg.GetDeleteStatus(),
		deletedAt:       protoTimeOrZero(msg.GetDeletedAt()),
	}, includeSensitiveText)
}

func accountMessageDeleteStatus(status waappv1.MessageDeleteStatus) waappv1.MessageDeleteStatus {
	if status == waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_UNSPECIFIED {
		return waappv1.MessageDeleteStatus_MESSAGE_DELETE_STATUS_NOT_DELETED
	}
	return status
}

func protoTimeOrZero(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime().UTC()
}

func contactRefForMessage(contactRef string, sender string) string {
	value := strings.TrimSpace(shared.FirstNonEmpty(contactRef, sender))
	if value == "" {
		return "unknown"
	}
	return value
}

func accountMessageDirection(direction waappv1.AccountMessageDirection, kind waappv1.InboundMessageKind) waappv1.AccountMessageDirection {
	if direction != waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_UNSPECIFIED {
		return direction
	}
	if kind == waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM {
		return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_SYSTEM
	}
	return waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND
}

func accountMessageSource(source waappv1.AccountMessageSource) waappv1.AccountMessageSource {
	if source != waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_UNSPECIFIED {
		return source
	}
	return waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION
}

func accountMessageDisplayText(text string) string {
	text = normalizeWAFramedDisplayText(strings.TrimSpace(text))
	if text == "" {
		return ""
	}
	if value := waJSONDisplayText(text); value != "" {
		return value
	}
	if waJSONLikeText(text) {
		return ""
	}
	return text
}

func waJSONLikeText(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}")
}

func payloadTextSummary(payloadRef string) string {
	payloadRef = strings.TrimSpace(payloadRef)
	if strings.HasPrefix(payloadRef, "node:") {
		return strings.TrimPrefix(payloadRef, "node:")
	}
	return ""
}
