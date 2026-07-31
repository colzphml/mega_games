package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

func TestClassifyTransient(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"deadline", context.DeadlineExceeded},
		{"eof", io.ErrUnexpectedEOF},
		{"net timeout", &net.DNSError{IsTimeout: true}},
		{"discord 500", &discordgo.RESTError{Response: &http.Response{StatusCode: 500}}},
		{"discord 429", &discordgo.RESTError{Response: &http.Response{StatusCode: 429}}},
		{"wrapped", fmt.Errorf("fetch discord message: %w", context.DeadlineExceeded)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != Transient {
				t.Errorf("Classify(%v) = %v, want Transient", tc.err, got)
			}
		})
	}
}

func TestClassifyPermanent(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"discord 404", &discordgo.RESTError{Response: &http.Response{StatusCode: 404}}},
		{"discord 403", &discordgo.RESTError{Response: &http.Response{StatusCode: 403}}},
		{"parse failure", errors.New("message has no embeds")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != Permanent {
				t.Errorf("Classify(%v) = %v, want Permanent", tc.err, got)
			}
		})
	}
}

func TestClassifyNil(t *testing.T) {
	if got := Classify(nil); got != Permanent {
		t.Errorf("Classify(nil) = %v, want Permanent (nil must never be retried)", got)
	}
}

// TestClassifyTransientRawNetworkErrors covers the substring fallback in
// Classify -- the branch that exists specifically for raw network failures
// that reach this package as plain, untyped errors: a dial failure or a
// dropped connection to Discord does not implement net.Error and is not a
// discordgo.RESTError, so it can only be recognised by the text of
// err.Error(). Getting this branch backwards is exactly the failure this
// package was written to prevent (see the package doc comment): a
// half-hour Discord outage burns the whole retry budget in minutes and
// every message in flight lands in the failed table for good.
func TestClassifyTransientRawNetworkErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"connection refused", errors.New("dial tcp 127.0.0.1:443: connect: connection refused")},
		{"connection reset", errors.New("read tcp 10.0.0.1:53512->1.2.3.4:443: read: connection reset by peer")},
		{"no such host", errors.New("dial tcp: lookup discord.com: no such host")},
		{"i/o timeout", errors.New("dial tcp 1.2.3.4:443: i/o timeout")},
		{"client timeout", errors.New(`Get "https://discord.com/api": net/http: request canceled (Client.Timeout exceeded while awaiting headers)`)},
		// Text that merely mentions the deadline, as opposed to wrapping
		// the context.DeadlineExceeded sentinel -- that typed case is
		// already covered by TestClassifyTransient and would never reach
		// the substring loop at all.
		{"context deadline exceeded text", errors.New(`Get "https://discord.com/api": context deadline exceeded`)},
		{"unexpected eof text", errors.New("unexpected EOF")},
		{"marker is case-insensitive", errors.New("Dial Failed: Connection Refused")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != Transient {
				t.Errorf("Classify(%v) = %v, want Transient: this is exactly the "+
					"shape of raw network error the substring fallback in "+
					"Classify exists to catch", tc.err, got)
			}
		})
	}
}

// TestClassifyPermanentRawNonNetworkErrors is the other half of the same
// gap: an untyped error whose text does not contain any network marker
// must fall through to Permanent, or a genuinely broken message (bad
// payload, parse failure) would retry forever instead of reaching the
// failed table.
func TestClassifyPermanentRawNonNetworkErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"validation error", errors.New("invalid game payload: missing game_id")},
		{"permission text", errors.New("permission denied writing to disk")},
		{"generic wrapped", fmt.Errorf("decode message: %w", errors.New("unexpected token"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != Permanent {
				t.Errorf("Classify(%v) = %v, want Permanent: an error with no "+
					"network marker and no recognised type must not be retried "+
					"forever", tc.err, got)
			}
		})
	}
}

func TestSleepRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, time.Hour) {
		t.Error("Sleep must return false when context is already cancelled")
	}
}
