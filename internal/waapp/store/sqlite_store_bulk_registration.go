package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/byte-v-forge/wa-app/internal/waapp/bulkregistration"
)

func (s *SQLiteStore) CreateTask(ctx context.Context, task bulkregistration.Task, items []bulkregistration.Item) (*bulkregistration.Task, bool, error) {
	task = normalizeBulkTask(task)
	payload, err := json.Marshal(task)
	if err != nil {
		return nil, false, err
	}
	itemPayloads := make([]string, len(items))
	for index := range items {
		items[index] = normalizeBulkItem(items[index])
		data, err := marshalBulkItem(items[index])
		if err != nil {
			return nil, false, err
		}
		itemPayloads[index] = string(data)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	active, err := sqliteActiveBulkTask(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if active != nil {
		return active, true, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO wa_sqlite_bulk_registration_tasks (id,status,created_at,updated_at,payload) VALUES (?,?,?,?,?)`, task.TaskID, task.Status, SQLiteTimeValue(task.CreatedAt), SQLiteTimeValue(task.UpdatedAt), string(payload)); err != nil {
		_ = tx.Rollback()
		if active, activeErr := s.GetActiveTask(ctx); activeErr == nil && active != nil {
			return active, true, nil
		}
		return nil, false, err
	}
	for index, item := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO wa_sqlite_bulk_registration_items (id,task_id,status,created_at,updated_at,payload) VALUES (?,?,?,?,?,?)`, item.ItemID, item.TaskID, item.Status, SQLiteTimeValue(item.CreatedAt), SQLiteTimeValue(item.UpdatedAt), itemPayloads[index]); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		if active, activeErr := s.GetActiveTask(ctx); activeErr == nil && active != nil {
			return active, true, nil
		}
		return nil, false, err
	}
	return &task, false, nil
}

func (s *SQLiteStore) GetActiveTask(ctx context.Context) (*bulkregistration.Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT payload FROM wa_sqlite_bulk_registration_tasks WHERE status IN ('DRAFT','RUNNING','CANCEL_REQUESTED','CANCELING','PAUSED') ORDER BY updated_at DESC, id DESC LIMIT 1`)
	return sqliteBulkTask(row)
}

func (s *SQLiteStore) GetTask(ctx context.Context, taskID string) (*bulkregistration.Task, error) {
	return sqliteBulkTask(s.db.QueryRowContext(ctx, `SELECT payload FROM wa_sqlite_bulk_registration_tasks WHERE id=?`, taskID))
}

func (s *SQLiteStore) ListItems(ctx context.Context, taskID string) ([]bulkregistration.Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM wa_sqlite_bulk_registration_items WHERE task_id=? ORDER BY created_at ASC, id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []bulkregistration.Item{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		item, err := unmarshalBulkItem([]byte(payload))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) SaveTask(ctx context.Context, task bulkregistration.Task) error {
	task = normalizeBulkTask(task)
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_bulk_registration_tasks (id,status,created_at,updated_at,payload) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`, task.TaskID, task.Status, SQLiteTimeValue(task.CreatedAt), SQLiteTimeValue(task.UpdatedAt), string(payload))
	return err
}

func (s *SQLiteStore) SaveItem(ctx context.Context, item bulkregistration.Item) error {
	item = normalizeBulkItem(item)
	payload, err := marshalBulkItem(item)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_bulk_registration_items (id,task_id,status,created_at,updated_at,payload) VALUES (?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status, updated_at=excluded.updated_at, payload=excluded.payload`, item.ItemID, item.TaskID, item.Status, SQLiteTimeValue(item.CreatedAt), SQLiteTimeValue(item.UpdatedAt), string(payload))
	return err
}

func (s *SQLiteStore) AppendEvent(ctx context.Context, event bulkregistration.Event) error {
	if event.EventID == "" || event.TaskID == "" {
		return fmt.Errorf("bulk registration event id and task id are required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	payload, err := marshalBulkEvent(event)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO wa_sqlite_sms_activation_events (id,task_id,item_id,created_at,payload) VALUES (?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, event.EventID, event.TaskID, event.ItemID, SQLiteTimeValue(event.CreatedAt), string(payload))
	return err
}

func sqliteActiveBulkTask(ctx context.Context, tx *sql.Tx) (*bulkregistration.Task, error) {
	return sqliteBulkTask(tx.QueryRowContext(ctx, `SELECT payload FROM wa_sqlite_bulk_registration_tasks WHERE status IN ('DRAFT','RUNNING','CANCEL_REQUESTED','CANCELING','PAUSED') ORDER BY updated_at DESC, id DESC LIMIT 1`))
}

type sqliteBulkTaskRow interface {
	Scan(...any) error
}

func sqliteBulkTask(row sqliteBulkTaskRow) (*bulkregistration.Task, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	task := &bulkregistration.Task{}
	if err := json.Unmarshal([]byte(payload), task); err != nil {
		return nil, err
	}
	return task, nil
}

func normalizeBulkTask(task bulkregistration.Task) bulkregistration.Task {
	now := time.Now().UTC()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = now
	}
	return task
}

func normalizeBulkItem(item bulkregistration.Item) bulkregistration.Item {
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	return item
}

// Bulk items hold supplier activation and phone routing data needed to resume
// a task after a restart. These fields are persisted in the store wrapper but
// remain absent from the dashboard Item JSON representation.
type bulkItemPayload struct {
	Item               bulkregistration.Item `json:"item"`
	PhoneE164          string                `json:"phone_e164"`
	CountryCallingCode string                `json:"country_calling_code"`
	CountryISO2        string                `json:"country_iso2"`
	ActivationID       string                `json:"activation_id"`
}

type bulkEventPayload struct {
	Event        bulkregistration.Event `json:"event"`
	ActivationID string                 `json:"activation_id"`
}

func marshalBulkItem(item bulkregistration.Item) ([]byte, error) {
	return json.Marshal(bulkItemPayload{
		Item:               item,
		PhoneE164:          item.PhoneE164,
		CountryCallingCode: item.CountryCallingCode,
		CountryISO2:        item.CountryISO2,
		ActivationID:       item.ActivationID,
	})
}

func unmarshalBulkItem(data []byte) (bulkregistration.Item, error) {
	payload := bulkItemPayload{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return bulkregistration.Item{}, err
	}
	if payload.Item.ItemID == "" {
		item := bulkregistration.Item{}
		if err := json.Unmarshal(data, &item); err != nil {
			return bulkregistration.Item{}, err
		}
		return item, nil
	}
	payload.Item.PhoneE164 = payload.PhoneE164
	payload.Item.CountryCallingCode = payload.CountryCallingCode
	payload.Item.CountryISO2 = payload.CountryISO2
	payload.Item.ActivationID = payload.ActivationID
	return payload.Item, nil
}

func marshalBulkEvent(event bulkregistration.Event) ([]byte, error) {
	return json.Marshal(bulkEventPayload{Event: event, ActivationID: event.ActivationID})
}
