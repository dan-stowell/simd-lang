package grind_test

import (
	"testing"

	"fclang/grind"
)

func TestPick(t *testing.T) {
	if got := grind.Pick(); got != 9 {
		t.Fatalf("got %d, want 9", got)
	}
	if got := grind.RandN(); got >= 10 {
		t.Fatalf("got %d, want < 10", got)
	}
}

func TestFirst(t *testing.T) {
	if a, b, ok := grind.First(); a != 1 || b != 2 || !ok {
		t.Fatalf("got %d %d %v, want 1 2 true", a, b, ok)
	}
}

func TestRead(t *testing.T) {
	if got, err := grind.Read(); err != nil || got != "hello\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}
