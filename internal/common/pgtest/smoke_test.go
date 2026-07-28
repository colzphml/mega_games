package pgtest

import (
	"context"
	"testing"
)

func TestPostgresStarts(t *testing.T) {
	pool := NewPostgres(t)
	var one int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("query: %v", err)
	}
	if one != 1 {
		t.Errorf("got %d, want 1", one)
	}
}
