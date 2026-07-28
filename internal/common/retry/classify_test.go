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

func TestSleepRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Sleep(ctx, time.Hour) {
		t.Error("Sleep must return false when context is already cancelled")
	}
}
