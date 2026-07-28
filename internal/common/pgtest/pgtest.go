// Package pgtest starts a throwaway PostgreSQL for integration tests.
//
// The status tables carry the correctness of the whole pipeline —
// idempotent inserts, attempt counting, the transaction behind
// MoveToFailed. Those are SQL behaviours; a fake would only assert
// what the author already believed.
package pgtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	ctx := context.Background()
	container, err := postgres.Run(ctx,
		"postgres:16.4-alpine",
		postgres.WithDatabase("megagames_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate container: %v", err)
		}
	})

	return pool
}

func ApplySchema(t *testing.T, pool *pgxpool.Pool, ddl ...string) {
	t.Helper()
	for _, stmt := range ddl {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			t.Fatalf("apply schema: %v\nstatement: %s", err, stmt)
		}
	}
}
