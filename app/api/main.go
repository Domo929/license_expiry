// Command api serves the HTTP API of license_expiry.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata" // Embeds the time zone database, because container images have no zone files.

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/clock"
)

// main does only the setup and the wiring. Add application routes in handlers.go and keep request handling outside main.
// main has no defer statements, because os.Exit skips them. The shutdown code closes the resources.
func main() {
	// Config. Settings come from flags. Secrets come from environment variables.
	var (
		httpAddr, debugAddr, logFormat string
		shutdownDelay, shutdownTimeout time.Duration
		inputFile, thresholdList       string
		timeZone, simulatedToday       string
		scanInterval                   time.Duration
	)
	flag.StringVar(&httpAddr, "http-addr", "localhost:8080", "address of the API server")
	flag.StringVar(&debugAddr, "debug-addr", "localhost:8081", "address of the debug server for metrics and pprof. Never expose it to the internet")
	flag.StringVar(&logFormat, "log-format", "text", "log format: text or json")
	flag.DurationVar(&shutdownDelay, "shutdown-delay", 0, "time between failing /readyz and stopping the servers. Set it above the readiness probe period when deployed")
	flag.DurationVar(&shutdownTimeout, "shutdown-timeout", 20*time.Second, "time for in-flight requests to finish at shutdown")
	flag.StringVar(&inputFile, "input-file", "input.csv", "CSV file with the licenses. The service loads it at each start, because the storage is in memory")
	flag.StringVar(&thresholdList, "thresholds", "90,60,30", "days before the expiration date at which a license enters a window, separated by commas")
	flag.StringVar(&timeZone, "timezone", "America/Denver", "time zone that decides what day it is")
	flag.DurationVar(&scanInterval, "scan-interval", time.Hour, "time between scans. 0 means that the scan at start-up is the only one")
	flag.StringVar(&simulatedToday, "simulated-today", "", "date, like 2027-03-01, at which a simulated clock starts. It replaces the real clock and turns on POST /licenses/{state}/{number}/next-window, a route for demos")
	flag.Parse() // A bad flag exits with code 2. --help exits with code 0.

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "unexpected arguments: %q\n", flag.Args())
		flag.Usage()
		os.Exit(2)
	}

	// Logger.
	var log *slog.Logger
	switch logFormat {
	case "text":
		log = slog.New(slog.NewTextHandler(os.Stdout, nil))
	case "json":
		log = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	default:
		fmt.Fprintf(os.Stderr, "--log-format %q: use text or json\n", logFormat)
		os.Exit(2)
	}

	// Policy, time zone, and clock. The clock is the real one, or a simulated one that starts at noon on the given day.
	policy, err := parsePolicy(thresholdList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "--thresholds %q: %v\n", thresholdList, err)
		os.Exit(2)
	}
	loc, err := time.LoadLocation(timeZone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "--timezone %q: %v\n", timeZone, err)
		os.Exit(2)
	}
	if scanInterval < 0 {
		fmt.Fprintf(os.Stderr, "--scan-interval %s: use 0 or a positive duration\n", scanInterval)
		os.Exit(2)
	}
	var clk license.Clock = clock.Real{}
	var sim *clock.Simulated // It stays nil with the real clock.
	if simulatedToday != "" {
		day, err := civil.ParseISO(simulatedToday)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--simulated-today %q: use a date like 2027-03-01\n", simulatedToday)
			os.Exit(2)
		}
		sim = clock.NewSimulated(noonOn(day, loc))
		clk = sim
	}

	// Metrics.
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	// SIGINT and SIGTERM cancel ctx, and that starts the shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// Data. The service holds it in memory, so the input file is loaded again at each start.
	// The servers and the scheduler start after the load.
	svc := license.NewService(license.Config{Policy: policy, Clock: clk, Location: loc})
	if err := loadInput(ctx, log, svc, inputFile); err != nil {
		log.Error("load input", "error", err)
		os.Exit(1)
	}
	log.Info("config",
		"thresholds", thresholdList,
		"time_zone", loc.String(),
		"today", svc.Today(),
		"simulated", sim != nil,
		"scan_interval", scanInterval.String(),
		"input_file", inputFile)

	// Routes. The main port serves the API and the probes. The debug port serves metrics and pprof.
	var ready atomic.Bool
	ready.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(&ready))
	h := &handlers{log: log, svc: svc, policy: policy, loc: loc, clk: clk, sim: sim}
	h.routes(mux)
	debugMux := http.NewServeMux()
	debugMux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	debugMux.HandleFunc("/debug/pprof/", pprof.Index)
	debugMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	debugMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	debugMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	debugMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Servers.
	// workCtx is the parent of the request and worker contexts. The signal does not cancel it,
	// so in-flight requests can finish. The shutdown cancels it after the servers stop.
	workCtx, stopWork := context.WithCancel(context.WithoutCancel(ctx))
	baseContext := func(net.Listener) context.Context { return workCtx }

	// Start background workers with workers.Go. Each worker must return when workCtx ends.
	var workers sync.WaitGroup
	workers.Go(func() { runScheduler(workCtx, log, svc, scanInterval) })

	api := &http.Server{
		Addr:              httpAddr,
		Handler:           instrument(reg, mux),
		BaseContext:       baseContext,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	debug := &http.Server{
		Addr:              debugAddr,
		Handler:           debugMux,
		BaseContext:       baseContext,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      time.Minute, // CPU profiles and traces take 30 seconds by default.
		IdleTimeout:       2 * time.Minute,
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return serve(log, "api", api) })
	g.Go(func() error { return serve(log, "debug", debug) })

	// Shutdown. It starts after a signal, or after a server fails.
	g.Go(func() error {
		<-gctx.Done()
		stop() // A second signal stops the process at once.
		log.Info("shutdown started")

		// 1. Fail /readyz, then wait, so that load balancers stop sending requests.
		ready.Store(false)
		time.Sleep(shutdownDelay)

		// 2. Stop the servers. In-flight requests get the shutdown timeout to finish.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(gctx), shutdownTimeout)
		defer cancel()
		serversErr := errors.Join(api.Shutdown(shutdownCtx), debug.Shutdown(shutdownCtx))

		// 3. Stop the background workers, and wait for them.
		stopWork()
		workers.Wait()

		// 4. Return the shutdown errors.
		if serversErr != nil {
			return fmt.Errorf("shutdown: %w", serversErr)
		}
		log.Info("shutdown finished")
		return nil
	})
	if err := g.Wait(); err != nil {
		log.Error("service failed", "error", err)
		os.Exit(1)
	}
}

// parsePolicy returns the policy for the --thresholds flag: whole numbers of days, separated by commas, such as 90,60,30.
func parsePolicy(list string) (license.Policy, error) {
	var thresholds []int
	for part := range strings.SplitSeq(list, ",") {
		days, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return license.Policy{}, fmt.Errorf("%q is not a number of days", part)
		}
		thresholds = append(thresholds, days)
	}
	return license.NewPolicy(thresholds)
}

// serve runs srv until Shutdown. It returns nil after a clean shutdown.
func serve(log *slog.Logger, name string, srv *http.Server) error {
	log.Info("server starting", "server", name, "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("%s server: %w", name, err)
	}
	return nil
}

// healthz reports that the process runs.
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// readyz reports whether the service can take requests. It fails during the shutdown.
func readyz(ready *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// instrument counts the requests to next, and measures their latency, by route pattern and status code.
func instrument(reg prometheus.Registerer, next http.Handler) http.Handler {
	labels := []string{"pattern", "code"}
	requests := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Number of HTTP requests, by route pattern and status code.",
	}, labels)
	latency := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Latency of HTTP requests, by route pattern and status code.",
		Buckets: prometheus.DefBuckets,
	}, labels)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		// ServeMux sets r.Pattern. Unmatched requests share one value, so the label set stays small.
		pattern := cmp.Or(r.Pattern, "unmatched")
		code := strconv.Itoa(sw.status)
		requests.WithLabelValues(pattern, code).Inc()
		latency.WithLabelValues(pattern, code).Observe(time.Since(start).Seconds())
	})
}

// statusWriter records the status code that a handler writes.
type statusWriter struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	// Informational codes (1xx) come before the final status code.
	if !w.wroteHeader && code >= http.StatusOK {
		w.status, w.wroteHeader = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the original writer.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
