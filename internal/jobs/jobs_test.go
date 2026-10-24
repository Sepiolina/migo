package jobs

import (
	"context"
	"testing"
	"time"
)

func TestGateLimitsAndHonoursCancel(t *testing.T) {
	g := NewGate(1)
	if err := g.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := g.Acquire(ctx); err == nil {
		t.Error("second Acquire should wait until cancelled")
	}
	g.Release()
	if err := g.Acquire(context.Background()); err != nil {
		t.Error("slot should be free again")
	}
}
