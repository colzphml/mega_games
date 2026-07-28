// Package queue holds the retry-eligibility rules shared by every
// service that owns a status table.
//
// The rule used to be "retry anything whose last attempt is older than
// PROCESS_RETRY_INTERVAL", with the same value driving the ticker and
// a non-strict <= in SQL. A Telegram send that took exactly one
// interval was therefore picked up a second time and the post went out
// twice. The threshold is now a multiple of the ticker interval.
package queue

import "time"

const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusProcessed  = "processed"
)

// staleFactor keeps the eligibility threshold clear of the ticker
// interval. Telegram's HTTP client allows two minutes per request, so
// with a one-minute ticker the threshold must exceed that.
const staleFactor = 3

func StaleThreshold(retryInterval time.Duration) time.Duration {
	if retryInterval <= 0 {
		return 0
	}
	return retryInterval * staleFactor
}

func StaleCutoff(now time.Time, retryInterval time.Duration) time.Time {
	return now.Add(-StaleThreshold(retryInterval))
}
