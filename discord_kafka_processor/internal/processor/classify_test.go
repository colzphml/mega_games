package processor

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/colzphml/mega_games/internal/common/retry"
)

// fakeAttemptRecorder counts how many times a failure was charged
// against the retry budget.
type fakeAttemptRecorder struct {
	attempts int
}

func (f *fakeAttemptRecorder) record() { f.attempts++ }

func TestTransientFailuresDoNotConsumeAttempts(t *testing.T) {
	rec := &fakeAttemptRecorder{}
	outage := &discordgo.RESTError{Response: &http.Response{StatusCode: 503}}

	for i := 0; i < 20; i++ {
		if retry.Classify(outage) != retry.Transient {
			t.Fatal("a 503 from Discord must be transient")
		}
		// shouldCharge mirrors the decision processMessage makes.
		if shouldChargeAttempt(outage) {
			rec.record()
		}
	}

	if rec.attempts != 0 {
		t.Errorf("charged %d attempts during an outage, want 0: a 30-minute "+
			"Discord outage must not exhaust the budget and drop messages", rec.attempts)
	}
}

func TestPermanentFailureConsumesAttempt(t *testing.T) {
	rec := &fakeAttemptRecorder{}
	gone := &discordgo.RESTError{Response: &http.Response{StatusCode: 404}}

	if shouldChargeAttempt(gone) {
		rec.record()
	}
	if rec.attempts != 1 {
		t.Errorf("charged %d attempts for a 404, want 1", rec.attempts)
	}
}

func TestParseFailureConsumesAttempt(t *testing.T) {
	if !shouldChargeAttempt(errors.New("message has no embeds")) {
		t.Error("an unparseable message must consume an attempt")
	}
}

var _ = context.Background
