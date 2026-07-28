package config

import (
	"testing"
	"time"
)

func TestRequired(t *testing.T) {
	t.Setenv("TEST_KEY", "  value  ")
	got, err := Required("TEST_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "value" {
		t.Errorf("got %q, want %q (must trim)", got, "value")
	}

	t.Setenv("TEST_KEY", "   ")
	if _, err := Required("TEST_KEY"); err == nil {
		t.Error("whitespace-only value must be treated as missing")
	}
}

func TestPositiveIntRejectsZero(t *testing.T) {
	t.Setenv("N", "0")
	if _, err := PositiveInt("N", 5); err == nil {
		t.Error("PositiveInt must reject 0")
	}
}

func TestNonNegativeIntAcceptsZero(t *testing.T) {
	t.Setenv("N", "0")
	got, err := NonNegativeInt("N", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestDurationDefault(t *testing.T) {
	got, err := Duration("MISSING_KEY", 3*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 3*time.Second {
		t.Errorf("got %v, want 3s", got)
	}
}

func TestList(t *testing.T) {
	got := List(" a , ,b ")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %#v, want [a b] with blanks dropped", got)
	}
}
