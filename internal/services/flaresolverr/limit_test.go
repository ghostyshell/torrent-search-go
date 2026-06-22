package flaresolverr

import (
	"context"
	"testing"
	"time"
)

func TestAcquireRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Acquire(ctx); err != nil {
		t.Fatal(err)
	}
	Release()
}

func TestAcquireRespectsCancel(t *testing.T) {
	if err := Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer Release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := Acquire(ctx); err == nil {
		t.Fatal("expected timeout waiting for slot")
	}
}
