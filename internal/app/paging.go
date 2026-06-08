package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	defaultPageLimit = 100
	maxPageLimit     = 500
)

type keysetCursor struct {
	UpdatedAt time.Time `json:"updated_at"`
	ID        string    `json:"id"`
}

func decodeKeysetCursor(value string) (keysetCursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return keysetCursor{}, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return keysetCursor{}, fmt.Errorf("invalid page cursor")
	}
	var cursor keysetCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return keysetCursor{}, fmt.Errorf("invalid page cursor")
	}
	if cursor.UpdatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return keysetCursor{}, fmt.Errorf("invalid page cursor")
	}
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	cursor.ID = strings.TrimSpace(cursor.ID)
	return cursor, nil
}

func encodeKeysetCursor(updatedAt time.Time, id string) string {
	id = strings.TrimSpace(id)
	if updatedAt.IsZero() || id == "" {
		return ""
	}
	data, err := json.Marshal(keysetCursor{UpdatedAt: updatedAt.UTC(), ID: id})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func hasKeysetCursor(cursor keysetCursor) bool {
	return !cursor.UpdatedAt.IsZero() && strings.TrimSpace(cursor.ID) != ""
}

func normalizePageLimit(limit int) int {
	if limit <= 0 {
		return defaultPageLimit
	}
	if limit > maxPageLimit {
		return maxPageLimit
	}
	return limit
}

func keysetLookaheadLimit(limit int) int {
	return normalizePageLimit(limit) + 1
}

func trimLimit[T any](rows []T, limit int) ([]T, bool) {
	if limit < 0 {
		limit = 0
	}
	if len(rows) <= limit {
		return rows, false
	}
	return rows[:limit], true
}

func newKeysetPage[T any](rows []T, limit int, cursorOf func(T) keysetCursor) ([]T, string) {
	limit = normalizePageLimit(limit)
	items, hasMore := trimLimit(rows, limit)
	if !hasMore || len(items) == 0 || cursorOf == nil {
		return items, ""
	}
	cursor := cursorOf(items[len(items)-1])
	return items, encodeKeysetCursor(cursor.UpdatedAt, cursor.ID)
}

func keysetCursorValue(updatedAt time.Time, id string) keysetCursor {
	id = strings.TrimSpace(id)
	if updatedAt.IsZero() || id == "" {
		return keysetCursor{}
	}
	return keysetCursor{UpdatedAt: updatedAt.UTC(), ID: id}
}
