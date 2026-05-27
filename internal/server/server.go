// Package server exposes the checker over a small HTTP API.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/alizoj/pulse/internal/check"
)

type Server struct {
	checker check.Checker
	log     *slog.Logger
}

func New(c check.Checker, log *slog.Logger) *Server {
	return &Server{checker: c, log: log}
}

// Handler wires up the routes and middleware and returns the root handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /check", s.handleCheck)
	return s.withLogging(mux)
}

type checkRequest struct {
	Targets []string `json:"targets"`
}

// resultDTO is the wire shape of a result. It's separate from check.Result so
// the JSON contract doesn't drift every time the internal type changes, and so
// we control how an error renders.
type resultDTO struct {
	Target    string `json:"target"`
	Status    int    `json:"status,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(req.Targets) == 0 {
		http.Error(w, "no targets given", http.StatusBadRequest)
		return
	}

	// Carry the request context through so a client disconnect cancels the
	// in-flight probes instead of leaking goroutines.
	results := s.checker.Run(r.Context(), req.Targets)

	out := make([]resultDTO, len(results))
	for i, res := range results {
		out[i] = resultDTO{
			Target:    res.Target,
			Status:    res.Status,
			LatencyMS: res.Latency.Milliseconds(),
			OK:        res.OK(),
		}
		if res.Err != nil {
			out[i].Error = res.Err.Error()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.log.Error("encode response", "err", err)
	}
}

// ListenAndServe serves until ctx is cancelled, then drains in-flight requests
// within a fixed grace period before returning.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh: // failed to bind, or crashed before shutdown
		return err
	case <-ctx.Done():
		s.log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}

// withLogging emits one structured line per request once it completes.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"dur", time.Since(start).Round(time.Microsecond),
		)
	})
}

// statusRecorder remembers the status code so the logging middleware can report
// it; net/http gives no way to read it back otherwise.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
