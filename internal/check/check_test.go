package check

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPProber(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantErr    error // matched with errors.Is; nil means "no error"
	}{
		{
			name:       "healthy",
			handler:    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
			wantStatus: http.StatusOK,
		},
		{
			name:       "server error",
			handler:    func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) },
			wantStatus: http.StatusServiceUnavailable,
			wantErr:    ErrUnhealthy,
		},
		{
			name:       "redirect is not followed",
			handler:    func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/gone", http.StatusFound) },
			wantStatus: http.StatusFound,
			wantErr:    ErrUnhealthy,
		},
	}

	p := NewHTTPProber(2 * time.Second)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			got := p.Probe(context.Background(), srv.URL)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", got.Status, tt.wantStatus)
			}
			if !errors.Is(got.Err, tt.wantErr) {
				t.Errorf("err = %v, want match of %v", got.Err, tt.wantErr)
			}
		})
	}
}

func TestProbeRespectsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	got := NewHTTPProber(time.Second).Probe(ctx, srv.URL)
	if got.OK() {
		t.Fatal("expected the cancelled context to fail the probe")
	}
}
