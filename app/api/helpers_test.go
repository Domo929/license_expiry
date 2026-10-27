package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/clock"
)

// logBuffer collects log output. It is safe for concurrent use, because the scheduler logs from its own goroutine.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, _ := b.buf.Write(p) // bytes.Buffer.Write always returns a nil error.
	return n, nil
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newTestLogger returns a logger that writes one text line for each record, with no time, and the buffer that holds the lines.
func newTestLogger() (*slog.Logger, *logBuffer) {
	logs := &logBuffer{}
	handler := slog.NewTextHandler(logs, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	return slog.New(handler), logs
}

func mustDate(t *testing.T, value string) civil.Date {
	t.Helper()
	day, err := civil.ParseISO(value)
	require.NoError(t, err)
	return day
}

func denver(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Denver")
	require.NoError(t, err)
	return loc
}

// defaultPolicy returns the policy with the thresholds 90, 60, and 30. They are the default of the --thresholds flag.
func defaultPolicy(t *testing.T) license.Policy {
	t.Helper()
	policy, err := parsePolicy("90,60,30")
	require.NoError(t, err)
	return policy
}

// newRecord returns a record. The arguments are in the order of the columns of input.csv.
func newRecord(t *testing.T, npi, first, last, number, state, expiration string) license.Record {
	t.Helper()
	return license.Record{
		NPI:               npi,
		FirstName:         first,
		LastName:          last,
		LicenseNumber:     number,
		LicenseState:      state,
		LicenseExpiration: mustDate(t, expiration),
	}
}

// newService returns a service with the default policy, the time zone of Denver, and the clock.
func newService(t *testing.T, clk license.Clock) *license.Service {
	t.Helper()
	return license.NewService(license.Config{Policy: defaultPolicy(t), Clock: clk, Location: denver(t)})
}

// newTestClock returns a simulated clock that shows noon in Denver on 2027-03-01.
func newTestClock(t *testing.T) *clock.Simulated {
	t.Helper()
	return clock.NewSimulated(noonOn(civil.New(2027, time.March, 1), denver(t)))
}

// newTestMux returns the routes of the API, and the buffer that holds the log of the handlers.
// sim is the simulated clock, or nil if the clock is the real one.
func newTestMux(t *testing.T, svc licenseService, clk license.Clock, sim *clock.Simulated) (*http.ServeMux, *logBuffer) {
	t.Helper()
	log, logs := newTestLogger()
	mux := http.NewServeMux()
	(&handlers{log: log, svc: svc, policy: defaultPolicy(t), loc: denver(t), clk: clk, sim: sim}).routes(mux)
	return mux, logs
}
