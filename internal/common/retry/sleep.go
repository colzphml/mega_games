package retry

import (
	"context"
	"time"
)

// Sleep waits for d and reports whether the wait completed. It returns
// false as soon as ctx is done. Replaces six copies of sleepWithContext.
func Sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
