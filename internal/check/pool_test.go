package check

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunPoolPreservesOrder(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	got := RunPool(context.Background(), 3, items, func(_ context.Context, n int) int {
		return n * n
	})

	want := []int{1, 4, 9, 16, 25}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestRunPoolBoundsConcurrency tracks how many workers are ever in fn at once
// and checks it never exceeds the configured limit.
func TestRunPoolBoundsConcurrency(t *testing.T) {
	const workers = 4
	var inFlight, highWater atomic.Int32

	RunPool(context.Background(), workers, make([]int, 64), func(_ context.Context, _ int) struct{} {
		n := inFlight.Add(1)
		for { // bump the high-water mark if we're a new peak
			old := highWater.Load()
			if n <= old || highWater.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		inFlight.Add(-1)
		return struct{}{}
	})

	if peak := highWater.Load(); peak > workers {
		t.Fatalf("saw %d concurrent workers, limit was %d", peak, workers)
	}
}

func TestRunPoolStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before we start

	var calls atomic.Int32
	RunPool(ctx, 2, make([]int, 100), func(_ context.Context, _ int) int {
		calls.Add(1)
		return 0
	})

	// We can't assert zero (a worker may grab one item before noticing the
	// cancellation), but we shouldn't have churned through all 100.
	if calls.Load() == 100 {
		t.Fatalf("expected cancellation to skip work, ran all %d", calls.Load())
	}
}
