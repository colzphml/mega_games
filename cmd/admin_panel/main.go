package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/colzphml/mega_games/internal/admin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Setup zerolog
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	// Load config
	cfg, err := admin.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Setup signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Setup pgxpool
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(dbCtx, cfg.PostgresDSN())
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to postgres")
	}
	defer pool.Close()

	if err := pool.Ping(dbCtx); err != nil {
		log.Fatal().Err(err).Msg("failed to ping postgres")
	}
	log.Info().Msg("connected to postgres")

	// Setup HTTP server
	store := admin.NewStore(pool)
	router, err := newRouter(cfg, store)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to build router")
	}

	srv := &http.Server{
		Addr:              cfg.HealthAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Info().Str("addr", cfg.HealthAddr).Msg("starting server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	log.Info().Msg("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("server forced to shutdown")
	}

	log.Info().Msg("server exited")
}
