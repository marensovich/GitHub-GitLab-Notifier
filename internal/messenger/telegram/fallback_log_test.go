package telegram

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/mymmrac/telego/telegoapi"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// TestFallbackLoggedOncePerKind checks that rejections that differ only in
// numbers (byte offsets, entity positions) are logged once.
func TestFallbackLoggedOncePerKind(t *testing.T) {
	buf := captureLog(t)
	for _, offset := range []int{12, 57, 3012} {
		logFallbackOnce(&telegoapi.Error{ErrorCode: 400,
			Description: fmt.Sprintf("Bad Request: can't parse rich message: unexpected end tag at byte offset %d", offset)})
	}
	if c := strings.Count(buf.String(), "unexpected end tag"); c != 1 {
		t.Errorf("logged %d times, want once:\n%s", c, buf.String())
	}
}

// TestFallbackLogIsBounded checks that the set of logged rejections does not
// grow without limit.
func TestFallbackLogIsBounded(t *testing.T) {
	captureLog(t)
	for i := 0; i < 5*maxLoggedFallbacks; i++ {
		logFallbackOnce(&telegoapi.Error{ErrorCode: 400,
			Description: fmt.Sprintf("Bad Request: rejection kind %c%c", 'a'+i%26, 'a'+i/26%26)})
	}
	if n := loggedFallbackCount(); n > maxLoggedFallbacks {
		t.Errorf("remembered %d rejections, limit %d", n, maxLoggedFallbacks)
	}
}
