package app

import (
	"context"
	"fmt"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListAccountMessages(ctx context.Context, waAccountIDValue string, contactRef string, cursorValue string, limit int, includeSensitiveText bool) ([]*waappv1.AccountMessage, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	rows, err := s.queryAccountMessagePage(ctx, waAccountIDValue, strings.TrimSpace(contactRef), cursor, keysetLookaheadLimit(limit))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []*waappv1.AccountMessage{}
	for rows.Next() {
		item, err := scanAccountMessage(rows, includeSensitiveText)
		if err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	items, nextCursor := newKeysetPage(items, limit, func(message *waappv1.AccountMessage) keysetCursor {
		return keysetCursorValue(timeFromProto(message.GetReceivedAt()), message.GetAccountMessageId())
	})
	return items, nextCursor, nil
}

func (s *PostgresStore) queryAccountMessagePage(ctx context.Context, waAccountIDValue string, contactRef string, cursor keysetCursor, limit int) (pgx.Rows, error) {
	query := `SELECT m.message_id,ms.wa_account_id,m.message_session_id,m.kind,m.encryption_state,m.ack_status,m.contact_ref,m.sender_ref,m.payload_ref,COALESCE(d.plaintext_value,''),COALESCE(d.plaintext_redacted,''),COALESCE(d.plaintext_secret_ref,''),m.last_error_code,m.last_error_message,m.last_error_retryable,m.received_at
FROM wa_inbound_messages m
JOIN wa_message_sessions ms ON ms.message_session_id=m.message_session_id
LEFT JOIN LATERAL (
  SELECT plaintext_value,plaintext_redacted,plaintext_secret_ref
  FROM wa_decrypted_messages
  WHERE message_id=m.message_id
  ORDER BY decrypted_at DESC, decrypted_message_id DESC
  LIMIT 1
) d ON true
WHERE ms.wa_account_id=$1 AND m.kind=$2`
	args := []any{waAccountIDValue, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE.String()}
	nextArg := 3
	if contactRef != "" {
		query += fmt.Sprintf(" AND COALESCE(NULLIF(m.contact_ref,''), m.sender_ref)=$%d", nextArg)
		args = append(args, contactRef)
		nextArg++
	}
	if hasKeysetCursor(cursor) {
		query += fmt.Sprintf(" AND (m.received_at, m.message_id) < ($%d, $%d)", nextArg, nextArg+1)
		args = append(args, cursor.UpdatedAt, cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" ORDER BY m.received_at DESC, m.message_id DESC LIMIT $%d", nextArg)
	args = append(args, limit)
	return s.pool.Query(ctx, query, args...)
}

func scanAccountMessage(rows pgx.Rows, includeSensitiveText bool) (*waappv1.AccountMessage, error) {
	var parts accountMessageParts
	var kind string
	var encryptionState string
	var ackStatus string
	var errCode string
	var errMessage string
	var errRetryable bool
	if err := rows.Scan(&parts.messageID, &parts.accountID, &parts.sessionID, &kind, &encryptionState, &ackStatus, &parts.contactRef, &parts.senderRef, &parts.payloadRef, &parts.plaintext, &parts.redacted, &parts.secretRef, &errCode, &errMessage, &errRetryable, &parts.receivedAt); err != nil {
		return nil, err
	}
	parts.kind = waappv1.InboundMessageKind(waappv1.InboundMessageKind_value[kind])
	parts.encryptionState = waappv1.MessageEncryptionState(waappv1.MessageEncryptionState_value[encryptionState])
	parts.ackStatus = waappv1.MessageAckStatus(waappv1.MessageAckStatus_value[ackStatus])
	parts.lastError = protoError(errCode, errMessage, errRetryable)
	return newAccountMessage(parts, includeSensitiveText), nil
}
