package app

import (
	"context"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *SQLiteStore) ListAccountMessages(ctx context.Context, waAccountIDValue string, contactRef string, cursorValue string, limit int, includeSensitiveText bool) ([]*waappv1.AccountMessage, string, error) {
	cursor, err := decodeKeysetCursor(cursorValue)
	if err != nil {
		return nil, "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, err.Error(), false)
	}
	limit = normalizePageLimit(limit)
	rows, err := s.queryAccountMessages(ctx, waAccountIDValue, strings.TrimSpace(contactRef), cursor, keysetLookaheadLimit(limit))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := []*waappv1.AccountMessage{}
	for rows.Next() {
		message, decrypted, err := scanSQLiteAccountMessage(rows)
		if err != nil {
			return nil, "", err
		}
		item := newAccountMessageFromInbound(waAccountIDValue, message, decrypted, includeSensitiveText)
		if item != nil {
			items = append(items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	items, nextCursor := newKeysetPage(items, limit, func(message *waappv1.AccountMessage) keysetCursor {
		return keysetCursorValue(timeFromProto(message.GetReceivedAt()), message.GetAccountMessageId())
	})
	return items, nextCursor, nil
}

func (s *SQLiteStore) queryAccountMessages(ctx context.Context, waAccountIDValue string, contactRef string, cursor keysetCursor, limit int) (sqlRows, error) {
	query := `SELECT m.payload, COALESCE((
  SELECT d.payload
  FROM wa_sqlite_decrypted_messages d
  WHERE d.message_id=m.id
  ORDER BY d.decrypted_at DESC, d.id DESC
  LIMIT 1
), '')
FROM wa_sqlite_inbound_messages m
JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
WHERE s.wa_account_id=? AND json_extract(m.payload, '$.kind')=?`
	args := []any{waAccountIDValue, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE.String()}
	if contactRef != "" {
		query += ` AND COALESCE(NULLIF(json_extract(m.payload, '$.contact_ref'), ''), json_extract(m.payload, '$.sender_ref'))=?`
		args = append(args, contactRef)
	}
	if hasKeysetCursor(cursor) {
		value := sqliteTimeValue(cursor.UpdatedAt)
		query += ` AND (m.received_at < ? OR (m.received_at = ? AND m.id < ?))`
		args = append(args, value, value, cursor.ID)
	}
	query += ` ORDER BY m.received_at DESC, m.id DESC LIMIT ?`
	args = append(args, limit)
	return s.db.QueryContext(ctx, query, args...)
}

type sqlRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

func scanSQLiteAccountMessage(rows sqlRows) (*waappv1.InboundMessage, *waappv1.DecryptedMessage, error) {
	var messagePayload string
	var decryptedPayload string
	if err := rows.Scan(&messagePayload, &decryptedPayload); err != nil {
		return nil, nil, err
	}
	message := &waappv1.InboundMessage{}
	if err := sqliteUnmarshal([]byte(messagePayload), message); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(decryptedPayload) == "" {
		return message, nil, nil
	}
	decrypted := &waappv1.DecryptedMessage{}
	if err := sqliteUnmarshal([]byte(decryptedPayload), decrypted); err != nil {
		return nil, nil, err
	}
	return message, decrypted, nil
}
