package storage

import (
	"time"
)

// Delivery statuses.
//
// Статусы доставок вебхуков.
const (
	DeliveryDelivered = "delivered" // at least one message was sent
	DeliveryIgnored   = "ignored"   // nothing to send: unsupported/filtered event or no chat
	DeliveryRejected  = "rejected"  // signature or token did not match
	DeliveryFailed    = "failed"    // there was something to send, but every send failed
	DeliveryDuplicate = "duplicate" // the same delivery ID was already delivered
)

// Delivery is one incoming webhook request.
//
// Одна входящая доставка вебхука (только метаданные, без тела).
type Delivery struct {
	ID         int64
	DeliveryID string
	Platform   string
	Event      string
	RepoURL    string
	ReceivedAt time.Time
	Status     string
	HTTPStatus int
	Error      string
	Duration   time.Duration
}

// RecordDelivery appends a delivery to the log.
//
// Записывает доставку в журнал.
func (s *Store) RecordDelivery(d Delivery) error {
	_, err := s.db.Exec(`
		INSERT INTO deliveries (delivery_id, platform, event, repo_url, received_at, status, http_status, error, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, d.DeliveryID, d.Platform, d.Event, d.RepoURL, d.ReceivedAt.UnixMilli(), d.Status, d.HTTPStatus, d.Error, d.Duration.Milliseconds())
	return err
}

// WasDelivered reports whether a delivery with this ID was already delivered.
// An empty ID is never a duplicate.
//
// Проверяет, была ли доставка с этим ID уже отправлена.
func (s *Store) WasDelivered(platform, deliveryID string) (bool, error) {
	if deliveryID == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM deliveries
		WHERE platform = ? AND delivery_id = ? AND delivery_id != '' AND status = ?
	`, platform, deliveryID, DeliveryDelivered).Scan(&n)
	return n > 0, err
}

// RecentDeliveries returns the newest deliveries first.
//
// Возвращает последние доставки, новые первыми.
func (s *Store) RecentDeliveries(limit int) ([]Delivery, error) {
	rows, err := s.db.Query(`
		SELECT id, delivery_id, platform, event, repo_url, received_at, status, http_status, error, duration_ms
		FROM deliveries ORDER BY received_at DESC, id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Delivery
	for rows.Next() {
		var d Delivery
		var received, duration int64
		if err := rows.Scan(&d.ID, &d.DeliveryID, &d.Platform, &d.Event, &d.RepoURL,
			&received, &d.Status, &d.HTTPStatus, &d.Error, &duration); err != nil {
			return nil, err
		}
		d.ReceivedAt = time.UnixMilli(received).UTC()
		d.Duration = time.Duration(duration) * time.Millisecond
		list = append(list, d)
	}
	return list, rows.Err()
}

// PruneDeliveries deletes deliveries received before the given time and
// returns how many were removed.
//
// Удаляет доставки старше указанного времени.
func (s *Store) PruneDeliveries(before time.Time) (int64, error) {
	res, err := s.db.Exec("DELETE FROM deliveries WHERE received_at < ?", before.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
