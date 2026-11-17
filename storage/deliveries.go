package storage

import (
	"database/sql"
	"errors"
	"time"
)

// Delivery statuses.
//
// Статусы доставок вебхуков.
const (
	DeliveryQueued    = "queued"    // its messages wait in the outbox
	DeliveryDelivered = "delivered" // every message was sent
	DeliveryIgnored   = "ignored"   // nothing to send: unsupported/filtered event or no chat
	DeliveryRejected  = "rejected"  // signature or token did not match
	DeliveryFailed    = "failed"    // some message could not be sent and none waits
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

// DeliveryState returns the status and row ID of the latest queued,
// delivered or failed delivery with this ID ("" when there is none).
// An empty ID never matches.
//
// Состояние последней доставки с этим ID.
func (s *Store) DeliveryState(platform, deliveryID string) (status string, rowID int64, err error) {
	if deliveryID == "" {
		return "", 0, nil
	}
	err = s.db.QueryRow(`
		SELECT status, id FROM deliveries
		WHERE platform = ? AND delivery_id = ? AND status IN (?, ?, ?)
		ORDER BY id DESC LIMIT 1
	`, platform, deliveryID, DeliveryQueued, DeliveryDelivered, DeliveryFailed).Scan(&status, &rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return status, rowID, err
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

// RepoCount is a repository URL with a number of deliveries.
type RepoCount struct {
	URL   string
	Count int
}

// UnconnectedRepos counts, per repository, the webhooks received since a
// time that nobody was subscribed to, the most frequent first.
//
// Репозитории, чьи вебхуки приходят, но никуда не отправляются.
func (s *Store) UnconnectedRepos(since time.Time) ([]RepoCount, error) {
	rows, err := s.db.Query(`
		SELECT repo_url, COUNT(*) AS n FROM deliveries
		WHERE status = ? AND error = 'not connected' AND received_at >= ? AND repo_url != ''
		GROUP BY repo_url ORDER BY n DESC, repo_url
	`, DeliveryIgnored, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []RepoCount
	for rows.Next() {
		var rc RepoCount
		if err := rows.Scan(&rc.URL, &rc.Count); err != nil {
			return nil, err
		}
		list = append(list, rc)
	}
	return list, rows.Err()
}
