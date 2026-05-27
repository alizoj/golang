package check

import (
	"context"
	"sync"
)

// RunPool applies fn to every item using at most `workers` goroutines and
// returns the results in the same order as items. A workers value <= 0 (or one
// larger than len(items)) is treated as "one worker per item".
//
// Each worker writes to a distinct index of the output slice, so no locking is
// needed around the results. If ctx is cancelled, RunPool stops dispatching new
// work and returns what's been collected; entries for undispatched items keep
// their zero value. fn is responsible for honouring ctx itself.
func RunPool[In, Out any](ctx context.Context, workers int, items []In, fn func(context.Context, In) Out) []Out {
	out := make([]Out, len(items))
	if len(items) == 0 {
		return out
	}
	if workers <= 0 || workers > len(items) {
		workers = len(items)
	}

	type job struct {
		idx  int
		item In
	}
	jobs := make(chan job)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for j := range jobs {
				out[j.idx] = fn(ctx, j.item)
			}
		}()
	}

feed:
	for i, it := range items {
		select {
		case jobs <- job{i, it}:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	return out
}
