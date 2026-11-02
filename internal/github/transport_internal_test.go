// The one white-box test of the package: retryAfter is a pure function of a
// response's status and headers, and its bound is a constant no server can
// exercise under a minute.

package github

import (
	"net/http"
	"testing"
	"time"
)

// TestRetryAfterIsBounded: a Retry-After of an hour is waited for a minute.
func TestRetryAfterIsBounded(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("Retry-After", "3600")

	wait, retry := retryAfter(answer{status: http.StatusTooManyRequests, header: header})
	if !retry || wait != time.Minute {
		t.Errorf("wait %s retry %v, want %s once", wait, retry, time.Minute)
	}

	header.Set("Retry-After", "3")

	if wait, _ := retryAfter(answer{status: http.StatusTooManyRequests, header: header}); wait != 3*time.Second {
		t.Errorf("wait %s, want the three seconds GitHub named", wait)
	}
}
