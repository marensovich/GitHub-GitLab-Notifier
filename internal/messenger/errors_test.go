package messenger

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("send: %w", err) }
	cases := []struct {
		name string
		err  error
		want Class
	}{
		{"nil", nil, ClassOK},
		{"plain", errors.New("connection reset"), ClassTemporary},
		{"rate limited", wrap(&RateLimited{RetryAfter: 3 * time.Second}), ClassRateLimited},
		{"thread not found", wrap(fmt.Errorf("%w: topic 5", ErrThreadNotFound)), ClassThreadNotFound},
		{"chat gone", wrap(fmt.Errorf("%w: kicked", ErrChatGone)), ClassChatGone},
		{"rejected", wrap(fmt.Errorf("%w: bad html", ErrRejected)), ClassRejected},
	}
	for _, c := range cases {
		if got := Classify(c.err); got != c.want {
			t.Errorf("%s: Classify = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRetryAfter(t *testing.T) {
	err := fmt.Errorf("send: %w", &RateLimited{RetryAfter: 7 * time.Second, Err: errors.New("429")})
	if got := RetryAfter(err); got != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", got)
	}
	if got := RetryAfter(errors.New("x")); got != 0 {
		t.Errorf("RetryAfter of a plain error = %v, want 0", got)
	}
	if !errors.Is(err, err) || errors.Unwrap(&RateLimited{Err: ErrRejected}) != ErrRejected {
		t.Error("RateLimited must unwrap to its cause")
	}
}
