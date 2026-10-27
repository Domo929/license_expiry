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
