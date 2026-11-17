package telegram

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTelegramHealth(t *testing.T) {
	now := time.Now()
	if err := telegramHealth(now.Add(-time.Minute), errors.New("blip"), now); err != nil {
		t.Errorf("recent contact: %v", err)
	}
	err := telegramHealth(now.Add(-10*time.Minute), errors.New("dial tcp: timeout"), now)
	if err == nil || !strings.Contains(err.Error(), "10m0s") || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("stale with error: %v", err)
	}
	if err := telegramHealth(now.Add(-10*time.Minute), nil, now); err == nil {
		t.Error("stale without error: want error")
	}
}
