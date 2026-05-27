package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alizoj/pulse/internal/check"
)

// proberFunc adapts an ordinary function to check.Prober, the same trick the
// stdlib uses with http.HandlerFunc. It lets tests stub out the network.
type proberFunc func(context.Context, string) check.Result

func (f proberFunc) Probe(ctx context.Context, target string) check.Result { return f(ctx, target) }

func newTestServer(p check.Prober) *httptest.Server {
	c := check.Checker{Prober: p, Concurrency: 2}
	s := New(c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return httptest.NewServer(s.Handler())
}

func TestHandleCheck(t *testing.T) {
	ts := newTestServer(proberFunc(func(_ context.Context, target string) check.Result {
		return check.Result{Target: target, Status: http.StatusOK}
	}))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/check", "application/json",
		strings.NewReader(`{"targets":["http://a","http://b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got []resultDTO
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if !got[0].OK || got[0].Target != "http://a" {
		t.Errorf("unexpected first result: %+v", got[0])
	}
}

func TestHandleCheckRejectsEmpty(t *testing.T) {
	ts := newTestServer(proberFunc(func(context.Context, string) check.Result {
		t.Fatal("prober should not run for an empty target list")
		return check.Result{}
	}))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/check", "application/json", strings.NewReader(`{"targets":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
