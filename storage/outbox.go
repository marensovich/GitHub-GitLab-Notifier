package storage

import (
	"database/sql"
	"errors"
	"time"
)

// Outbox row states.
const (
	OutboxPending = "pending"
	OutboxSending = "sending"
	OutboxDone    = "done"
	OutboxDead    = "dead"
)

// How long finished rows are kept.
const (
	keepDone = 7 * 24 * time.Hour
	keepDead = 30 * 24 * time.Hour
)

// OutboxItem is one message waiting to be sent to one destination.
type OutboxItem struct {
	ID, DeliveryID, DestinationID int64
	ThreadID, EventType           string
	Payload                       []byte
	Attempts                      int
	NextAttemptAt                 time.Time
	LastError, State              string
}

const outboxColumns = "id, COALESCE(delivery_id, 0), destination_id, thread_id, event_type, payload, attempts, next_attempt_at, last_error, state"

func scanOutbox(row interface{ Scan(...any) error }) (*OutboxItem, error) {
	var it OutboxItem
	var payload string
	var next int64
	err := row.Scan(&it.ID, &it.DeliveryID, &it.DestinationID, &it.ThreadID, &it.EventType, &payload,
		&it.Attempts, &next, &it.LastError, &it.State)
	if err != nil {
		return nil, err
	}
	it.Payload = []byte(payload)
	it.NextAttemptAt = time.UnixMilli(next).UTC()
	return &it, nil
}

// EnqueueDelivery logs a webhook as queued and stores its messages, in one
// transaction. It returns the row ID of the delivery.
func (s *Store) EnqueueDelivery(d Delivery, items []OutboxItem) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO deliveries (delivery_id, platform, event, repo_url, received_at, status, http_status, error, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.DeliveryID, d.Platform, d.Event, d.RepoURL, d.ReceivedAt.UnixMilli(), DeliveryQueued, d.HTTPStatus, d.Error, d.Duration.Milliseconds())
	if err != nil {
		return 0, err
	}
	rowID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	created := d.ReceivedAt.UnixMilli()
	for _, it := range items {
		if _, err := tx.Exec(`INSERT INTO outbox (delivery_id, destination_id, thread_id, event_type, payload, next_attempt_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			rowID, it.DestinationID, it.ThreadID, it.EventType, string(it.Payload), it.NextAttemptAt.UnixMilli(), created); err != nil {
			return 0, err
		}
	}
	return rowID, tx.Commit()
}

// ClaimNext marks the next row of a messenger as sending and returns it, or
// nil when nothing is due. It keeps each destination in order: only the
// oldest pending row of a destination may go, and only while no other row
// of it is being sent. Destinations in busy are skipped.
func (s *Store) ClaimNext(messenger string, now time.Time, busy map[int64]bool) (*OutboxItem, error) {
	rows, err := s.db.Query(`SELECT o.id, o.destination_id FROM outbox o
		JOIN destinations d ON d.id = o.destination_id
		WHERE o.state = 'pending' AND d.messenger = ? AND o.next_attempt_at <= ?
		AND NOT EXISTS (SELECT 1 FROM outbox x WHERE x.destination_id = o.destination_id
			AND (x.state = 'sending' OR (x.state = 'pending' AND x.id < o.id)))
		ORDER BY o.id`, messenger, now.UnixMilli())
	if err != nil {
		return nil, err
	}
	var candidates []int64
	for rows.Next() {
		var id, dest int64
		if err := rows.Scan(&id, &dest); err != nil {
			rows.Close()
			return nil, err
		}
		if !busy[dest] {
			candidates = append(candidates, id)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, id := range candidates {
		// The state check makes the claim safe against another worker.
		it, err := scanOutbox(s.db.QueryRow(`UPDATE outbox SET state = 'sending'
			WHERE id = ? AND state = 'pending' RETURNING `+outboxColumns, id))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		return it, err
	}
	return nil, nil
}

// MarkDone records a sent row.
func (s *Store) MarkDone(id int64) error {
	return s.finish(id, OutboxDone, "")
}

// MarkDead gives up on a row.
func (s *Store) MarkDead(id int64, errText string) error {
	return s.finish(id, OutboxDead, errText)
}

func (s *Store) finish(id int64, state, errText string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var delivery sql.NullInt64
	err = tx.QueryRow(`UPDATE outbox SET state = ?, last_error = CASE WHEN ? != '' THEN ? ELSE last_error END, finished_at = ?
		WHERE id = ? RETURNING delivery_id`, state, errText, errText, time.Now().UnixMilli(), id).Scan(&delivery)
	if err != nil {
		return err
	}
	if delivery.Valid {
		if err := refreshDelivery(tx, delivery.Int64); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// refreshDelivery sets the status of a queued delivery from its rows:
// delivered when all are done, failed when some are dead and none waits.
func refreshDelivery(tx *sql.Tx, deliveryRowID int64) error {
	var waiting, dead int
	var lastErr string
	err := tx.QueryRow(`SELECT
		COALESCE(SUM(state IN ('pending', 'sending')), 0),
		COALESCE(SUM(state = 'dead'), 0),
		COALESCE((SELECT last_error FROM outbox WHERE delivery_id = ? AND state = 'dead' ORDER BY id DESC LIMIT 1), '')
		FROM outbox WHERE delivery_id = ?`, deliveryRowID, deliveryRowID).Scan(&waiting, &dead, &lastErr)
	if err != nil || waiting > 0 {
		return err
	}
	status, errText := DeliveryDelivered, ""
	if dead > 0 {
		status, errText = DeliveryFailed, lastErr
	}
	_, err = tx.Exec("UPDATE deliveries SET status = ?, error = ? WHERE id = ?", status, errText, deliveryRowID)
	return err
}

// MarkRetry puts a row back to wait until next, with its attempt count.
func (s *Store) MarkRetry(id int64, attempts int, next time.Time, errText string) error {
	_, err := s.db.Exec(`UPDATE outbox SET state = 'pending', attempts = ?, next_attempt_at = ?, last_error = ?
		WHERE id = ?`, attempts, next.UnixMilli(), errText, id)
	return err
}

// ClearOutboxThread makes a row go to the main chat from now on.
func (s *Store) ClearOutboxThread(id int64) error {
	_, err := s.db.Exec("UPDATE outbox SET thread_id = '' WHERE id = ?", id)
	return err
}

// ResetSending returns rows left sending by a stopped process to pending.
func (s *Store) ResetSending() (int64, error) {
	res, err := s.db.Exec("UPDATE outbox SET state = 'pending' WHERE state = 'sending'")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PruneOutbox deletes done rows finished more than 7 days before now and
// dead rows finished more than 30 days before now.
func (s *Store) PruneOutbox(now time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM outbox WHERE (state = 'done' AND finished_at < ?) OR (state = 'dead' AND finished_at < ?)`,
		now.Add(-keepDone).UnixMilli(), now.Add(-keepDead).UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// OutboxCounts returns how many rows wait to be sent and how many are dead.
func (s *Store) OutboxCounts() (pending, dead int, err error) {
	err = s.db.QueryRow(`SELECT COALESCE(SUM(state IN ('pending', 'sending')), 0), COALESCE(SUM(state = 'dead'), 0)
		FROM outbox`).Scan(&pending, &dead)
	return pending, dead, err
}

// RequeueDead sends the dead rows of a delivery again from scratch and marks
// the delivery queued. It returns how many rows were requeued.
func (s *Store) RequeueDead(deliveryRowID int64) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE outbox SET state = 'pending', attempts = 0, next_attempt_at = ?, last_error = '', finished_at = 0
		WHERE delivery_id = ? AND state = 'dead'`, time.Now().UnixMilli(), deliveryRowID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return 0, err
	}
	if _, err := tx.Exec("UPDATE deliveries SET status = ?, error = '' WHERE id = ?", DeliveryQueued, deliveryRowID); err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}
