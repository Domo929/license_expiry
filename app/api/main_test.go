package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
)

func TestHealthz(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()

	healthz(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestReadyz(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ready bool
		want  int
	}{
		{name: "during shutdown", want: http.StatusServiceUnavailable},
		{name: "ready", ready: true, want: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var ready atomic.Bool
			ready.Store(tc.ready)
			w := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)

			readyz(&ready).ServeHTTP(w, req)

			assert.Equal(t, tc.want, w.Code)
		})
	}
}

func TestInstrument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
		want   string // The http_requests_total series after one request.
	}{
		{name: "route pattern", target: "/items/7", want: `http_requests_total{code="201",pattern="GET /items/{id}"} 1`},
		{name: "unmatched", target: "/missing", want: `http_requests_total{code="404",pattern="unmatched"} 1`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := prometheus.NewRegistry()
			mux := http.NewServeMux()
			mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusCreated)
			})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.target, nil)

			instrument(reg, mux).ServeHTTP(httptest.NewRecorder(), req)

			want := "# HELP http_requests_total Number of HTTP requests, by route pattern and status code.\n" +
				"# TYPE http_requests_total counter\n" + tc.want + "\n"
			require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "http_requests_total"))
		})
	}
}

func TestParsePolicy(t *testing.T) {
	t.Parallel()

	defaultWindows := []license.Window{license.WindowOK, "EXPIRING_90", "EXPIRING_60", "EXPIRING_30", license.WindowExpired}
	tests := []struct {
		name        string
		thresholds  string
		wantWindows []license.Window
		wantErr     string
	}{
		{name: "the default", thresholds: "90,60,30", wantWindows: defaultWindows},
		{name: "spaces and any order", thresholds: " 30, 90 ,60", wantWindows: defaultWindows},
		{name: "other thresholds", thresholds: "14,7", wantWindows: []license.Window{license.WindowOK, "EXPIRING_14", "EXPIRING_7", license.WindowExpired}},
		{name: "one threshold", thresholds: "30", wantWindows: []license.Window{license.WindowOK, "EXPIRING_30", license.WindowExpired}},
		{name: "empty", thresholds: "", wantErr: `"" is not a number of days`},
		{name: "an empty item", thresholds: "90,,30", wantErr: `"" is not a number of days`},
		{name: "not a number", thresholds: "90,soon", wantErr: `"soon" is not a number of days`},
		{name: "not a whole number", thresholds: "7.5", wantErr: `"7.5" is not a number of days`},
		{name: "zero", thresholds: "90,0", wantErr: "threshold 0 is not above 0"},
		{name: "negative", thresholds: "-5", wantErr: "threshold -5 is not above 0"},
		{name: "a threshold twice", thresholds: "30,60,30", wantErr: "threshold 30 is listed twice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			policy, err := parsePolicy(tc.thresholds)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			var got []license.Window
			for _, row := range policy.Windows() {
				got = append(got, row.Name)
			}
			assert.Equal(t, tc.wantWindows, got)
		})
	}
}
