package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/byte-v-forge/wa-app/internal/waapp/bulkregistration"
	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) CreateTask(ctx context.Context, task bulkregistration.Task, items []bulkregistration.Item) (*bulkregistration.Task, bool, error) {
	task = normalizeBulkTask(task)
	taskPayload, err := json.Marshal(task)
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	active, err := postgresActiveBulkTask(ctx, tx)
	if err != nil {
		return nil, false, err
	}
	if active != nil {
		return active, true, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wa_bulk_registration_tasks (task_id,status,created_at,updated_at,payload) VALUES ($1,$2,$3,$4,$5::jsonb)`, task.TaskID, task.Status, task.CreatedAt, task.UpdatedAt, string(taskPayload)); err != nil {
		_ = tx.Rollback(ctx)
		if active, activeErr := s.GetActiveTask(ctx); activeErr == nil && active != nil {
			return active, true, nil
		}
		return nil, false, err
	}
	for index, item := range items {
		if _, err := tx.Exec(ctx, `INSERT INTO wa_bulk_registration_items (item_id,task_id,status,created_at,updated_at,payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb)`, item.ItemID, item.TaskID, item.Status, item.CreatedAt, item.UpdatedAt, itemPayloads[index]); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		if active, activeErr := s.GetActiveTask(ctx); activeErr == nil && active != nil {
			return active, true, nil
		}
		return nil, false, err
	}
	return &task, false, nil
}

func (s *PostgresStore) GetActiveTask(ctx context.Context) (*bulkregistration.Task, error) {
	return postgresBulkTask(s.pool.QueryRow(ctx, `SELECT payload FROM wa_bulk_registration_tasks WHERE status IN ('DRAFT','RUNNING','CANCEL_REQUESTED','CANCELING','PAUSED') ORDER BY updated_at DESC, task_id DESC LIMIT 1`))
}

func (s *PostgresStore) GetTask(ctx context.Context, taskID string) (*bulkregistration.Task, error) {
	return postgresBulkTask(s.pool.QueryRow(ctx, `SELECT payload FROM wa_bulk_registration_tasks WHERE task_id=$1`, taskID))
}

func (s *PostgresStore) ListItems(ctx context.Context, taskID string) ([]bulkregistration.Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT payload FROM wa_bulk_registration_items WHERE task_id=$1 ORDER BY created_at ASC, item_id ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []bulkregistration.Item{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		item, err := unmarshalBulkItem(payload)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) SaveTask(ctx context.Context, task bulkregistration.Task) error {
	task = normalizeBulkTask(task)
	payload, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO wa_bulk_registration_tasks (task_id,status,created_at,updated_at,payload) VALUES ($1,$2,$3,$4,$5::jsonb)
ON CONFLICT (task_id) DO UPDATE SET status=EXCLUDED.status, updated_at=EXCLUDED.updated_at, payload=EXCLUDED.payload`, task.TaskID, task.Status, task.CreatedAt, task.UpdatedAt, string(payload))
	return err
}

func (s *PostgresStore) SaveItem(ctx context.Context, item bulkregistration.Item) error {
	item = normalizeBulkItem(item)
	payload, err := marshalBulkItem(item)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO wa_bulk_registration_items (item_id,task_id,status,created_at,updated_at,payload) VALUES ($1,$2,$3,$4,$5,$6::jsonb)
ON CONFLICT (item_id) DO UPDATE SET status=EXCLUDED.status, updated_at=EXCLUDED.updated_at, payload=EXCLUDED.payload`, item.ItemID, item.TaskID, item.Status, item.CreatedAt, item.UpdatedAt, string(payload))
	return err
}

func (s *PostgresStore) AppendEvent(ctx context.Context, event bulkregistration.Event) error {
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
	_, err = s.pool.Exec(ctx, `INSERT INTO wa_sms_activation_events (event_id,task_id,item_id,created_at,payload) VALUES ($1,$2,$3,$4,$5::jsonb)
ON CONFLICT (event_id) DO UPDATE SET payload=EXCLUDED.payload`, event.EventID, event.TaskID, event.ItemID, event.CreatedAt, string(payload))
	return err
}

type postgresBulkTaskRow interface {
	Scan(...any) error
}

func postgresBulkTask(row postgresBulkTaskRow) (*bulkregistration.Task, error) {
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	task := &bulkregistration.Task{}
	if err := json.Unmarshal(payload, task); err != nil {
		return nil, err
	}
	return task, nil
}

func postgresActiveBulkTask(ctx context.Context, tx pgx.Tx) (*bulkregistration.Task, error) {
	return postgresBulkTask(tx.QueryRow(ctx, `SELECT payload FROM wa_bulk_registration_tasks WHERE status IN ('DRAFT','RUNNING','CANCEL_REQUESTED','CANCELING','PAUSED') ORDER BY updated_at DESC, task_id DESC LIMIT 1`))
}
