// Package jobs holds the helpers for running heavy work in a small service
// (a few users, a few requests a week, tight RAM budget).
package jobs

import (
	"context"
	"runtime"
	"runtime/debug"
)

// Gate bounds how many heavy jobs run at once across ALL modules, so one
// busy endpoint cannot exhaust the machine's memory. Extra callers wait.
type Gate struct{ slots chan struct{} }

// NewGate allows at most n concurrent jobs.
func NewGate(n int) *Gate { return &Gate{slots: make(chan struct{}, n)} }

// Acquire waits for a free slot (or ctx being cancelled, e.g. the client
// hung up while queued). Every successful Acquire needs a Release.
func (g *Gate) Acquire(ctx context.Context) error {
	select {
	case g.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees the slot taken by Acquire.
func (g *Gate) Release() { <-g.slots }

// FreeMemory hands memory back to the OS right after a heavy job (parsing
// and writing a BOM is the one big allocation in this service's life).
func FreeMemory() {
	runtime.GC()
	debug.FreeOSMemory()
}
