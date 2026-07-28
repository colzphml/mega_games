// Package health separates liveness from readiness.
//
// The processor used to report unhealthy whenever the Discord API was
// unreachable, so autoheal restarted a container that had nothing wrong
// with it. Liveness now covers only what a restart could actually fix.
package health

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

type Check func(ctx context.Context) error

type namedCheck struct {
	name  string
	check Check
}

type Server struct {
	addr      string
	log       zerolog.Logger
	liveness  []namedCheck
	readiness []namedCheck
	srv       *http.Server
}

func NewServer(addr string, log zerolog.Logger) *Server {
	return &Server{addr: addr, log: log}
}

func (s *Server) AddLiveness(name string, c Check) {
	s.liveness = append(s.liveness, namedCheck{name, c})
}

func (s *Server) AddReadiness(name string, c Check) {
	s.readiness = append(s.readiness, namedCheck{name, c})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		s.run(w, r, s.liveness)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		s.run(w, r, append(append([]namedCheck{}, s.liveness...), s.readiness...))
	})
	return mux
}

func (s *Server) run(w http.ResponseWriter, r *http.Request, checks []namedCheck) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	for _, c := range checks {
		if err := c.check(ctx); err != nil {
			s.log.Warn().Err(err).Str("check", c.name).Msg("health check failed")
			http.Error(w, fmt.Sprintf("%s not healthy", c.name), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) Start(ctx context.Context) {
	s.srv = &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.log.Info().Str("addr", s.addr).Msg("health server listening")

	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error().Err(err).Msg("health server stopped")
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			s.log.Error().Err(err).Msg("health server shutdown error")
		}
	}()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// WaitFor blocks until the check succeeds or attempts run out.
// Replaces waitForPostgres, waitForMongo and waitForMinio.
func WaitFor(ctx context.Context, name string, attempts int, delay time.Duration, log zerolog.Logger, c Check) error {
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		checkCtx, cancel := context.WithTimeout(ctx, delay+5*time.Second)
		lastErr = c(checkCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt == attempts {
			break
		}
		log.Warn().Err(lastErr).Str("dependency", name).
			Int("attempt", attempt).Int("max_attempts", attempts).
			Msg("dependency not ready, retrying")
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("%s not ready: %w", name, lastErr)
}
