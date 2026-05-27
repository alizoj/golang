// Package check probes network targets concurrently and reports their health.
package check

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrUnhealthy means a target responded, but with a status code outside the
// 2xx range. It's wrapped (not returned directly) so callers can tell apart a
// "bad status" from a transport failure with errors.Is.
var ErrUnhealthy = errors.New("unhealthy status")

// Result is the outcome of probing a single target.
type Result struct {
	Target  string
	Status  int           // HTTP status code; 0 if the request never completed.
	Latency time.Duration // Time until the response headers were read.
	Err     error
}

// OK reports whether the target was reachable and healthy.
func (r Result) OK() bool { return r.Err == nil }

// Prober probes one target. Implementations must be safe for concurrent use:
// the pool calls Probe from many goroutines at once.
type Prober interface {
	Probe(ctx context.Context, target string) Result
}

// HTTPProber probes targets over HTTP(S). Build one with NewHTTPProber; the
// zero value has no client and will panic.
type HTTPProber struct {
	client *http.Client
}

func NewHTTPProber(timeout time.Duration) *HTTPProber {
	return &HTTPProber{
		client: &http.Client{
			Timeout: timeout,
			// A redirect is a real answer about the target, so don't chase it
			// off to another host and report that one's health instead.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (p *HTTPProber) Probe(ctx context.Context, target string) Result {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Result{Target: target, Err: fmt.Errorf("build request: %w", err)}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return Result{Target: target, Latency: time.Since(start), Err: err}
	}
	defer resp.Body.Close()

	res := Result{Target: target, Status: resp.StatusCode, Latency: time.Since(start)}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		res.Err = fmt.Errorf("%w: %d", ErrUnhealthy, resp.StatusCode)
	}
	return res
}

// Checker runs a Prober against many targets at a bounded concurrency.
type Checker struct {
	Prober      Prober
	Concurrency int // <= 0 means one goroutine per target.
}

// Run probes every target and returns the results in input order. It returns
// early if ctx is cancelled; targets not yet probed get a zero Result.
func (c Checker) Run(ctx context.Context, targets []string) []Result {
	return RunPool(ctx, c.Concurrency, targets, c.Prober.Probe)
}
