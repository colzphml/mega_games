package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func testServer() *Server {
	return NewServer(":0", zerolog.New(os.Stdout))
}

func TestLivenessIgnoresReadinessFailure(t *testing.T) {
	s := testServer()
	s.AddLiveness("db", func(context.Context) error { return nil })
	s.AddReadiness("discord", func(context.Context) error {
		return errors.New("discord unreachable")
	})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/health = %d, want 200: a Discord outage must not restart the container", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/ready = %d, want 503", rec.Code)
	}
}

func TestLivenessFailsWhenOwnDependencyIsDown(t *testing.T) {
	s := testServer()
	s.AddLiveness("db", func(context.Context) error { return errors.New("postgres down") })

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/health = %d, want 503", rec.Code)
	}
}

func TestReadyIncludesLiveness(t *testing.T) {
	s := testServer()
	s.AddLiveness("db", func(context.Context) error { return errors.New("postgres down") })
	s.AddReadiness("discord", func(context.Context) error { return nil })

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/ready = %d, want 503 when a liveness check fails", rec.Code)
	}
}
