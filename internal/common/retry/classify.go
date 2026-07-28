// Package retry separates failures that will resolve on their own from
// failures that will not.
//
// Without this distinction a thirty-minute Discord outage exhausts the
// ten-attempt budget and drops messages into the failed table for good,
// even though nothing was wrong with the messages themselves.
package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type Kind int

const (
	// Permanent is the zero value on purpose: an unrecognised error is
	// counted as an attempt, so a genuinely broken message still reaches
	// the failed table instead of retrying forever.
	Permanent Kind = iota
	Transient
)

func (k Kind) String() string {
	if k == Transient {
		return "transient"
	}
	return "permanent"
}

func Classify(err error) Kind {
	if err == nil {
		return Permanent
	}

	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return Transient
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Transient
	}

	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) && restErr.Response != nil {
		code := restErr.Response.StatusCode
		if code == 429 || code >= 500 {
			return Transient
		}
		return Permanent
	}

	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection refused",
		"connection reset",
		"no such host",
		"i/o timeout",
		"client.timeout",
		"context deadline exceeded",
		"unexpected eof",
	} {
		if strings.Contains(msg, marker) {
			return Transient
		}
	}

	return Permanent
}

func IsTransient(err error) bool {
	return Classify(err) == Transient
}
