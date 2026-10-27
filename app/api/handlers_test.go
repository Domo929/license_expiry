package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/clock"
	"github.com/Domo929/license_expiry/foundation/errs"
)

var (
	march1   = civil.New(2027, time.March, 1)
	ny118402 = license.License{NPI: "1487203355", LicenseNumber: "NY-118402", LicenseState: "NY", LicenseExpiration: civil.New(2027, time.May, 31), Window: license.WindowOK}
	ny118455 = license.License{NPI: "1592038847", LicenseNumber: "NY-118455", LicenseState: "NY", LicenseExpiration: civil.New(2027, time.May, 30), Window: "EXPIRING_90"}
)

// The JSON of the licenses above, in the shape that the requirements prescribe.
const (
	ny118402JSON = `{"npi":"1487203355","license_number":"NY-118402","license_state":"NY","license_expiration":"2027-05-31","window":"OK"}`
	ny118455JSON = `{"npi":"1592038847","license_number":"NY-118455","license_state":"NY","license_expiration":"2027-05-30","window":"EXPIRING_90"}`
)

func errorJSON(message string) string {
	return fmt.Sprintf(`{"error":%q}`, message)
}

// routesTest is the routes of the API over a mock of the business service.
// The clock is simulated, and it shows noon in Denver on 2027-03-01.
type routesTest struct {
	mux  *http.ServeMux
	svc  *MocklicenseService
	logs *logBuffer
}

func newRoutesTest(t *testing.T) *routesTest {
	t.Helper()
	svc := NewMocklicenseService(gomock.NewController(t))
	mux, logs := newTestMux(t, svc, newTestClock(t), nil)
	return &routesTest{mux: mux, svc: svc, logs: logs}
}

func (rt *routesTest) do(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, rt.mux, method, target)
}

func request(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), method, target, nil))
	return w
}

func TestGetLicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		target     string
		expect     func(svc *MocklicenseService)
		wantStatus int
		wantBody   string
	}{
		{
			name:   "a license in the window EXPIRING_90",
			target: "/licenses/NY/NY-118455?as_of=2027-03-01",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().License(gomock.Any(), "NY", "NY-118455", march1).Return(ny118455, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   ny118455JSON,
		},
		{
			name:   "as_of is today if it is missing",
			target: "/licenses/NY/NY-118455",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().Today().Return(march1)
				svc.EXPECT().License(gomock.Any(), "NY", "NY-118455", march1).Return(ny118455, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   ny118455JSON,
		},
		{
			name:   "the state and the number are in upper case",
			target: "/licenses/ny/%20ny-118455?as_of=2027-03-01",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().License(gomock.Any(), "NY", "NY-118455", march1).Return(ny118455, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   ny118455JSON,
		},
		{
			name:       "as_of is not an ISO date",
			target:     "/licenses/NY/NY-118455?as_of=03/01/2027",
			wantStatus: http.StatusBadRequest,
			wantBody:   errorJSON(`as_of "03/01/2027" must be a date like 2027-03-01: invalid`),
		},
		{
			name:   "a license that does not exist",
			target: "/licenses/NY/NY-999?as_of=2027-03-01",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().License(gomock.Any(), "NY", "NY-999", march1).
					Return(license.License{}, fmt.Errorf(`license "NY-999" in state "NY": %w`, errs.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
			wantBody:   errorJSON(`license "NY-999" in state "NY": not found`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rt := newRoutesTest(t)
			if tc.expect != nil {
				tc.expect(rt.svc)
			}

			w := rt.do(t, http.MethodGet, tc.target)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.JSONEq(t, tc.wantBody, w.Body.String())
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		})
	}
}

// TestGetLicense_PrescribedShape compares the bytes, so it checks the names, the order, and the format of the fields.
func TestGetLicense_PrescribedShape(t *testing.T) {
	t.Parallel()
	rt := newRoutesTest(t)
	rt.svc.EXPECT().License(gomock.Any(), "NY", "NY-118455", march1).Return(ny118455, nil)

	w := rt.do(t, http.MethodGet, "/licenses/NY/NY-118455?as_of=2027-03-01")

	assert.Equal(t, `{"npi":"1592038847","license_number":"NY-118455","license_state":"NY","license_expiration":"2027-05-30","window":"EXPIRING_90"}`, w.Body.String()) //nolint:testifylint // The test compares the bytes on purpose.
}

func TestGetWindows(t *testing.T) {
	t.Parallel()

	groups := []license.WindowGroup{
		{Window: license.WindowOK, Licenses: []license.License{ny118402}},
		{Window: "EXPIRING_90", Licenses: []license.License{ny118455}},
		{Window: "EXPIRING_60", Licenses: []license.License{}},
		{Window: "EXPIRING_30", Licenses: []license.License{}},
		{Window: license.WindowExpired, Licenses: []license.License{}},
	}
	groupsJSON := map[string]string{
		"OK":          `{"window":"OK","count":1,"licenses":[` + ny118402JSON + `]}`,
		"EXPIRING_90": `{"window":"EXPIRING_90","count":1,"licenses":[` + ny118455JSON + `]}`,
		"EXPIRING_60": `{"window":"EXPIRING_60","count":0,"licenses":[]}`,
		"EXPIRING_30": `{"window":"EXPIRING_30","count":0,"licenses":[]}`,
		"EXPIRED":     `{"window":"EXPIRED","count":0,"licenses":[]}`,
	}
	expectGroups := func(svc *MocklicenseService) {
		svc.EXPECT().WindowGroups(gomock.Any(), march1).Return(groups, nil)
	}

	tests := []struct {
		name       string
		target     string
		expect     func(svc *MocklicenseService)
		wantStatus int
		wantBody   string
	}{
		{
			name:       "every window, from the least to the most severe",
			target:     "/windows?as_of=2027-03-01",
			expect:     expectGroups,
			wantStatus: http.StatusOK,
			wantBody: `{"as_of":"2027-03-01","windows":[` +
				strings.Join([]string{groupsJSON["OK"], groupsJSON["EXPIRING_90"], groupsJSON["EXPIRING_60"], groupsJSON["EXPIRING_30"], groupsJSON["EXPIRED"]}, ",") + `]}`,
		},
		{
			name:       "one window",
			target:     "/windows?as_of=2027-03-01&window=expiring_90",
			expect:     expectGroups,
			wantStatus: http.StatusOK,
			wantBody:   `{"as_of":"2027-03-01","windows":[` + groupsJSON["EXPIRING_90"] + `]}`,
		},
		{
			name:   "as_of is today if it is missing",
			target: "/windows?window=EXPIRED",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().Today().Return(march1)
				expectGroups(svc)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"as_of":"2027-03-01","windows":[` + groupsJSON["EXPIRED"] + `]}`,
		},
		{
			name:       "a window that is not in the policy",
			target:     "/windows?as_of=2027-03-01&window=expiring_120",
			wantStatus: http.StatusBadRequest,
			wantBody:   errorJSON(`window "expiring_120" must be one of OK, EXPIRING_90, EXPIRING_60, EXPIRING_30, EXPIRED: invalid`),
		},
		{
			name:       "as_of is not a date",
			target:     "/windows?as_of=tomorrow",
			wantStatus: http.StatusBadRequest,
			wantBody:   errorJSON(`as_of "tomorrow" must be a date like 2027-03-01: invalid`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rt := newRoutesTest(t)
			if tc.expect != nil {
				tc.expect(rt.svc)
			}

			w := rt.do(t, http.MethodGet, tc.target)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.JSONEq(t, tc.wantBody, w.Body.String())
		})
	}
}

func TestGetAudit(t *testing.T) {
	t.Parallel()

	// The time of the entry is in UTC. The response shows it in the time zone of the service.
	entry := license.AuditEntry{
		ID: 1, NPI: "1487203355", LicenseNumber: "NY-118402", LicenseState: "NY", LicenseExpiration: civil.New(2027, time.May, 31),
		Window: "EXPIRING_90", HitAt: time.Date(2027, time.March, 2, 19, 0, 0, 0, time.UTC),
	}
	const entryJSON = `{"id":1,"npi":"1487203355","license_number":"NY-118402","license_state":"NY","license_expiration":"2027-05-31","window":"EXPIRING_90","hit_at":"2027-03-02T12:00:00-07:00"}`

	tests := []struct {
		name       string
		target     string
		expect     func(svc *MocklicenseService)
		wantStatus int
		wantBody   string
	}{
		{
			name:   "the entries of one license",
			target: "/audit?state=ny&number=ny-118402",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().Audit(gomock.Any(), "NY", "NY-118402").Return([]license.AuditEntry{entry}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"entries":[` + entryJSON + `]}`,
		},
		{
			name:   "the entries of all licenses",
			target: "/audit",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().Audit(gomock.Any(), "", "").Return([]license.AuditEntry{entry}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"entries":[` + entryJSON + `]}`,
		},
		{
			name:   "a license with no entries",
			target: "/audit?state=NY&number=NY-118455",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().Audit(gomock.Any(), "NY", "NY-118455").Return(nil, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"entries":[]}`,
		},
		{
			name:   "only the state",
			target: "/audit?state=NY",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().Audit(gomock.Any(), "NY", "").
					Return(nil, fmt.Errorf("audit: a license needs both a state and a number: %w", errs.ErrInvalid))
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   errorJSON("audit: a license needs both a state and a number: invalid"),
		},
		{
			name:   "a license that does not exist",
			target: "/audit?state=NY&number=NY-999",
			expect: func(svc *MocklicenseService) {
				svc.EXPECT().Audit(gomock.Any(), "NY", "NY-999").
					Return(nil, fmt.Errorf(`license "NY-999" in state "NY": %w`, errs.ErrNotFound))
			},
			wantStatus: http.StatusNotFound,
			wantBody:   errorJSON(`license "NY-999" in state "NY": not found`),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rt := newRoutesTest(t)
			tc.expect(rt.svc)

			w := rt.do(t, http.MethodGet, tc.target)

			assert.Equal(t, tc.wantStatus, w.Code)
			assert.JSONEq(t, tc.wantBody, w.Body.String())
		})
	}
}

func TestGetClock(t *testing.T) {
	t.Parallel()

	// The clock shows 19:00 UTC, which is noon in Denver.
	now := time.Date(2027, time.March, 1, 19, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		simulated bool
		want      string
	}{
		{name: "a simulated clock", simulated: true, want: `{"now":"2027-03-01T12:00:00-07:00","today":"2027-03-01","time_zone":"America/Denver","simulated":true}`},
		{name: "the real clock", simulated: false, want: `{"now":"2027-03-01T12:00:00-07:00","today":"2027-03-01","time_zone":"America/Denver","simulated":false}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := NewMocklicenseService(gomock.NewController(t))
			svc.EXPECT().Today().Return(march1)
			clk := clock.NewSimulated(now) // With simulated false, it stands in for the real clock.
			var sim *clock.Simulated
			if tc.simulated {
				sim = clk
			}
			mux, _ := newTestMux(t, svc, clk, sim)

			w := request(t, mux, http.MethodGet, "/clock")

			assert.Equal(t, http.StatusOK, w.Code)
			assert.JSONEq(t, tc.want, w.Body.String())
		})
	}
}

func TestPostScan(t *testing.T) {
	t.Parallel()

	// The times of the entries are in UTC. The response shows them in the time zone of the service.
	hitAt := time.Date(2027, time.March, 1, 19, 0, 0, 0, time.UTC)
	onTime := license.Hit{
		ID: 7, NPI: "1592038847", LicenseNumber: "NY-118455", LicenseState: "NY", LicenseExpiration: civil.New(2027, time.May, 30),
		Window: "EXPIRING_90", HitAt: hitAt,
		Previous: license.WindowOK,
		Skipped:  nil, // The response has an empty array for it, and not null.
	}
	late := license.Hit{
		ID: 8, NPI: "1204487719", LicenseNumber: "OH-551903", LicenseState: "OH", LicenseExpiration: civil.New(2027, time.March, 31),
		Window: "EXPIRING_30", HitAt: hitAt,
		Previous: license.WindowOK,
		Skipped:  []license.Window{"EXPIRING_90", "EXPIRING_60"},
	}

	t.Run("the hits", func(t *testing.T) {
		t.Parallel()
		rt := newRoutesTest(t)
		rt.svc.EXPECT().Scan(gomock.Any()).Return(license.ScanResult{AsOf: march1, Checked: 16, Hits: []license.Hit{onTime, late}}, nil)

		w := rt.do(t, http.MethodPost, "/scans")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"as_of":"2027-03-01","checked":16,"hits":[
			{"audit_id":7,"npi":"1592038847","license_number":"NY-118455","license_state":"NY","license_expiration":"2027-05-30",
			 "window":"EXPIRING_90","previous_window":"OK","skipped_windows":[],"hit_at":"2027-03-01T12:00:00-07:00"},
			{"audit_id":8,"npi":"1204487719","license_number":"OH-551903","license_state":"OH","license_expiration":"2027-03-31",
			 "window":"EXPIRING_30","previous_window":"OK","skipped_windows":["EXPIRING_90","EXPIRING_60"],"hit_at":"2027-03-01T12:00:00-07:00"}]}`, w.Body.String())
		assert.Contains(t, w.Body.String(), `"skipped_windows":[]`, "an empty array, not null")

		// The route runs the same code as the scheduler, so it logs the same lines.
		logs := rt.logs.String()
		assert.Contains(t, logs, `level=INFO msg="scan finished" as_of=2027-03-01 checked=16 hits=2`)
		assert.Contains(t, logs, `level=INFO msg="license entered window" npi=1204487719 license_state=OH license_number=OH-551903 window=EXPIRING_30 previous_window=OK`)
		assert.Contains(t, logs, `level=WARN msg="license skipped windows" npi=1204487719 license_state=OH license_number=OH-551903 window=EXPIRING_30 previous_window=OK skipped_windows="[EXPIRING_90 EXPIRING_60]"`)
	})

	t.Run("no hits", func(t *testing.T) {
		t.Parallel()
		rt := newRoutesTest(t)
		rt.svc.EXPECT().Scan(gomock.Any()).Return(license.ScanResult{AsOf: march1, Checked: 16}, nil)

		w := rt.do(t, http.MethodPost, "/scans")

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"as_of":"2027-03-01","checked":16,"hits":[]}`, w.Body.String())
	})

	t.Run("a scan that fails", func(t *testing.T) {
		t.Parallel()
		rt := newRoutesTest(t)
		rt.svc.EXPECT().Scan(gomock.Any()).Return(license.ScanResult{}, errors.New("memory is full"))

		w := rt.do(t, http.MethodPost, "/scans")

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.JSONEq(t, errorJSON("internal server error"), w.Body.String())
	})
}

func TestUnexpectedErrorGetsAGenericResponse(t *testing.T) {
	t.Parallel()
	rt := newRoutesTest(t)
	rt.svc.EXPECT().Audit(gomock.Any(), "", "").Return(nil, errors.New("disk on fire"))

	w := rt.do(t, http.MethodGet, "/audit")

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.JSONEq(t, errorJSON("internal server error"), w.Body.String())
	assert.NotContains(t, w.Body.String(), "disk", "the client does not see the error")
	assert.Equal(t, 1, strings.Count(rt.logs.String(), `level=ERROR msg="request failed" error="disk on fire"`), "the error is logged once")
}

func TestStatusOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid", err: errs.ErrInvalid, want: http.StatusBadRequest},
		{name: "unauthenticated", err: errs.ErrUnauthenticated, want: http.StatusUnauthorized},
		{name: "forbidden", err: errs.ErrForbidden, want: http.StatusForbidden},
		{name: "not found", err: errs.ErrNotFound, want: http.StatusNotFound},
		{name: "conflict", err: errs.ErrConflict, want: http.StatusConflict},
		{name: "a kind with context", err: fmt.Errorf("license %q: %w", "NY-1", errs.ErrNotFound), want: http.StatusNotFound},
		{name: "a body that is too large", err: &http.MaxBytesError{Limit: 1024}, want: http.StatusRequestEntityTooLarge},
		{name: "a body that is too large, with context", err: fmt.Errorf("read body: %w", &http.MaxBytesError{Limit: 1024}), want: http.StatusRequestEntityTooLarge},
		{name: "any other error", err: errors.New("boom"), want: http.StatusInternalServerError},
		{name: "a canceled request", err: context.Canceled, want: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, statusOf(tc.err))
		})
	}
}

// newDemo returns the routes over a real service, and the simulated clock that the service uses.
// The service holds one license, NY-118402, which expires on 2027-05-31. The clock shows noon in Denver on 2027-03-01.
func newDemo(t *testing.T) (*http.ServeMux, *clock.Simulated) {
	t.Helper()
	sim := newTestClock(t)
	svc := newService(t, sim)
	_, err := svc.Load(t.Context(), []license.Record{newRecord(t, "1487203355", "Adaeze", "Okonkwo", "NY-118402", "NY", "2027-05-31")})
	require.NoError(t, err)

	mux, _ := newTestMux(t, svc, sim, sim)
	return mux, sim
}

// TestNextWindow_Demo is the demo of the requirements: one license moves through every window, and no one sets the clock by hand.
func TestNextWindow_Demo(t *testing.T) {
	t.Parallel()
	mux, sim := newDemo(t)

	steps := []struct {
		previous, window, today, hitAt string
	}{
		{previous: "OK", window: "EXPIRING_90", today: "2027-03-02", hitAt: "2027-03-02T12:00:00-07:00"},
		{previous: "EXPIRING_90", window: "EXPIRING_60", today: "2027-04-01", hitAt: "2027-04-01T12:00:00-06:00"},
		{previous: "EXPIRING_60", window: "EXPIRING_30", today: "2027-05-01", hitAt: "2027-05-01T12:00:00-06:00"},
		{previous: "EXPIRING_30", window: "EXPIRED", today: "2027-05-31", hitAt: "2027-05-31T12:00:00-06:00"},
	}
	for i, step := range steps {
		moved := request(t, mux, http.MethodPost, "/licenses/NY/NY-118402/next-window")

		assert.Equal(t, http.StatusOK, moved.Code)
		assert.JSONEq(t, fmt.Sprintf(
			`{"previous_window":%q,"today":%q,"license":{"npi":"1487203355","license_number":"NY-118402","license_state":"NY","license_expiration":"2027-05-31","window":%q}}`,
			step.previous, step.today, step.window), moved.Body.String())
		assert.True(t, sim.Now().Equal(noonOn(mustDate(t, step.today), denver(t))), "the clock shows noon on %s", step.today)

		scanned := request(t, mux, http.MethodPost, "/scans")

		assert.Equal(t, http.StatusOK, scanned.Code)
		assert.JSONEq(t, fmt.Sprintf(
			`{"as_of":%q,"checked":1,"hits":[{"audit_id":%d,"npi":"1487203355","license_number":"NY-118402","license_state":"NY","license_expiration":"2027-05-31","window":%q,"previous_window":%q,"skipped_windows":[],"hit_at":%q}]}`,
			step.today, i+1, step.window, step.previous, step.hitAt), scanned.Body.String())

		again := request(t, mux, http.MethodPost, "/scans")

		assert.JSONEq(t, fmt.Sprintf(`{"as_of":%q,"checked":1,"hits":[]}`, step.today), again.Body.String(), "no repeats")
	}

	expired := request(t, mux, http.MethodPost, "/licenses/NY/NY-118402/next-window")

	assert.Equal(t, http.StatusConflict, expired.Code)
	assert.JSONEq(t, errorJSON(`license "NY-118402" in state "NY" is EXPIRED already: conflict`), expired.Body.String())

	audit := request(t, mux, http.MethodGet, "/audit?state=NY&number=NY-118402")

	assert.Equal(t, http.StatusOK, audit.Code)
	assert.Equal(t, 4, strings.Count(audit.Body.String(), `"hit_at"`), "one entry for each window")
}

func TestNextWindow_Errors(t *testing.T) {
	t.Parallel()

	t.Run("a license that does not exist", func(t *testing.T) {
		t.Parallel()
		mux, sim := newDemo(t)
		before := sim.Now()

		w := request(t, mux, http.MethodPost, "/licenses/NY/NY-999/next-window")

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.JSONEq(t, errorJSON(`license "NY-999" in state "NY": not found`), w.Body.String())
		assert.True(t, sim.Now().Equal(before), "the clock does not move")
	})

	t.Run("the state and the number are in upper case", func(t *testing.T) {
		t.Parallel()
		mux, _ := newDemo(t)

		w := request(t, mux, http.MethodPost, "/licenses/ny/ny-118402/next-window")

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("the route does not exist with the real clock", func(t *testing.T) {
		t.Parallel()
		mux, _ := newTestMux(t, newService(t, clock.Real{}), clock.Real{}, nil)

		w := request(t, mux, http.MethodPost, "/licenses/NY/NY-118402/next-window")

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestNoonOn(t *testing.T) {
	t.Parallel()
	loc := denver(t)

	tests := []struct {
		name string
		day  string
	}{
		{name: "a day with no clock change", day: "2027-03-02"},
		{name: "the day that the clocks go forward", day: "2027-03-14"},
		{name: "the day that the clocks go back", day: "2027-11-07"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			day := mustDate(t, tc.day)

			got := noonOn(day, loc)

			assert.Equal(t, day, civil.DateOf(got), "it is on the same day, in the time zone")
			assert.InDelta(t, 12, got.Hour(), 1, "it is within one hour of noon")
		})
	}
}
