package flaresolverr

import (
	"context"
	"os"
	"strconv"
	"sync"
)

var (
	once  sync.Once
	slots chan struct{}
)

func slotCount() int {
	n := 1
	if v, err := strconv.Atoi(os.Getenv("FLARE_SOLVERR_MAX_CONCURRENT")); err == nil && v > 0 {
		n = v
	}
	return n
}

func slotsCh() chan struct{} {
	once.Do(func() { slots = make(chan struct{}, slotCount()) })
	return slots
}

// Acquire waits for a FlareSolverr slot. ponytail: default 1; FS is single-browser.
func Acquire(ctx context.Context) error {
	select {
	case slotsCh() <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release frees a slot acquired with Acquire.
func Release() { <-slotsCh() }
