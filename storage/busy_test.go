package storage

import (
	"testing"
	"time"
)

// A write while another connection holds the write lock waits for it
// instead of failing with SQLITE_BUSY.
func TestWriteWaitsForLock(t *testing.T) {
	s := openTest(t)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE TABLE lock_holder (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = tx.Commit()
	}()
	if err := s.RecordDelivery(Delivery{Platform: "GitHub", Event: "push", RepoURL: "https://github.com/a/b", ReceivedAt: time.Now(), Status: DeliveryIgnored}); err != nil {
		t.Fatalf("write during another transaction: %v", err)
	}
}
