// Command pulse checks the health of HTTP targets, either as a one-shot CLI
// run or as a long-lived HTTP service.
//
//	pulse check https://example.com https://go.dev
//	cat targets.txt | pulse check -c 16
//	pulse serve -addr :9000
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/alizoj/pulse/internal/check"
	"github.com/alizoj/pulse/internal/server"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pulse:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pulse <check|serve> [flags]")
	}

	// First interrupt cancels the root context so work can unwind; a second one
	// hits the default handler and kills the process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "check":
		return runCheck(ctx, rest)
	case "serve":
		return runServe(ctx, rest)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func runCheck(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	concurrency := fs.Int("c", 8, "max concurrent probes")
	timeout := fs.Duration("timeout", 5*time.Second, "per-probe timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	targets := fs.Args()
	if len(targets) == 0 {
		var err error
		if targets, err = readTargets(os.Stdin); err != nil {
			return err
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("no targets: pass URLs as arguments or on stdin")
	}

	checker := check.Checker{
		Prober:      check.NewHTTPProber(*timeout),
		Concurrency: *concurrency,
	}

	var failed int
	for _, r := range checker.Run(ctx, targets) {
		if r.OK() {
			fmt.Printf("OK    %-40s %3d  %s\n", r.Target, r.Status, r.Latency.Round(time.Millisecond))
			continue
		}
		failed++
		fmt.Printf("FAIL  %-40s %s\n", r.Target, r.Err)
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d targets unhealthy", failed, len(targets))
	}
	return nil
}

func runServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "listen address")
	concurrency := fs.Int("c", 8, "max concurrent probes per request")
	timeout := fs.Duration("timeout", 5*time.Second, "per-probe timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	checker := check.Checker{
		Prober:      check.NewHTTPProber(*timeout),
		Concurrency: *concurrency,
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	return server.New(checker, log).ListenAndServe(ctx, *addr)
}

// readTargets reads one target per line, ignoring blank lines and # comments.
func readTargets(r io.Reader) ([]string, error) {
	var targets []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets = append(targets, line)
	}
	return targets, sc.Err()
}
