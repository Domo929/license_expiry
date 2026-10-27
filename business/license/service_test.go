package license_test

import (
	"context"
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/clock"
	"github.com/Domo929/license_expiry/foundation/errs"
)

func TestNewServicePanicsWithoutWiring(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)
	clk := clock.NewSimulated(time.Time{})

	tests := []struct {
		name string
		cfg  license.Config
	}{
		{name: "empty config", cfg: license.Config{}},
		{name: "no policy", cfg: license.Config{Clock: clk, Location: time.UTC}},
		{name: "no clock", cfg: license.Config{Policy: policy, Location: time.UTC}},
		{name: "no location", cfg: license.Config{Policy: policy, Clock: clk}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Panics(t, func() { license.NewService(tc.cfg) })
		})
	}
}

func TestToday(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		now  string // RFC 3339.
		zone string
		want string
	}{
		{name: "Denver is still on the day before", now: "2026-11-01T05:30:00Z", zone: "America/Denver", want: "2026-10-31"},
		{name: "Denver after midnight", now: "2026-11-01T06:30:00Z", zone: "America/Denver", want: "2026-11-01"},
		{name: "Denver when the clocks go back", now: "2026-11-01T08:30:00Z", zone: "America/Denver", want: "2026-11-01"},
		{name: "UTC", now: "2026-11-01T05:30:00Z", zone: "UTC", want: "2026-11-01"},
		{name: "Tokyo is ahead of UTC", now: "2026-10-31T16:00:00Z", zone: "Asia/Tokyo", want: "2026-11-01"},
		{name: "the zone of the reading does not matter", now: "2026-11-01T14:30:00+09:00", zone: "America/Denver", want: "2026-10-31"},
		{name: "last second of a leap day in Denver", now: "2028-03-01T06:59:59Z", zone: "America/Denver", want: "2028-02-29"},
		{name: "first second after a leap day in Denver", now: "2028-03-01T07:00:00Z", zone: "America/Denver", want: "2028-03-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now, err := time.Parse(time.RFC3339, tc.now)
			require.NoError(t, err)
			loc, err := time.LoadLocation(tc.zone)
			require.NoError(t, err)
			svc, _ := newService(t, now, loc)

			got := svc.Today()

			assert.Equal(t, mustDate(t, tc.want), got)
		})
	}
}

func TestTodayFollowsTheClock(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, clk := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)

	assert.Equal(t, mustDate(t, "2027-03-01"), svc.Today())

	clk.AddDays(30)
	assert.Equal(t, mustDate(t, "2027-03-31"), svc.Today())

	clk.Set(noon(mustDate(t, "2026-12-25"), loc))
	assert.Equal(t, mustDate(t, "2026-12-25"), svc.Today())
}

func TestLicense(t *testing.T) {
	t.Parallel()
	svc, _ := newFixtureService(t)

	tests := []struct {
		name       string
		asOf       string
		wantWindow license.Window
	}{
		{name: "the day before the first warning", asOf: "2026-12-19", wantWindow: license.WindowOK},
		{name: "the day it enters the first window", asOf: "2026-12-20", wantWindow: expiring90},
		{name: "the day before the second window", asOf: "2027-01-18", wantWindow: expiring90},
		{name: "the day it enters the second window", asOf: "2027-01-19", wantWindow: expiring60},
		{name: "the day before the third window", asOf: "2027-02-17", wantWindow: expiring60},
		{name: "the day it enters the third window", asOf: "2027-02-18", wantWindow: expiring30},
		{name: "the day before it expires", asOf: "2027-03-19", wantWindow: expiring30},
		{name: "the day it expires", asOf: "2027-03-20", wantWindow: license.WindowExpired},
		{name: "long after it expires", asOf: "2030-01-01", wantWindow: license.WindowExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := svc.License(t.Context(), "NY", "NY-118903", mustDate(t, tc.asOf))

			require.NoError(t, err)
			assert.Equal(t, tc.wantWindow, got.Window)
			assert.Equal(t, "1754482206", got.NPI)
			assert.Equal(t, mustDate(t, "2027-03-20"), got.LicenseExpiration)
		})
	}
}

func TestLicenseNotFound(t *testing.T) {
	t.Parallel()
	svc, _ := newFixtureService(t)
	asOf := mustDate(t, sampleDay)

	tests := []struct {
		name   string
		state  string
		number string
	}{
		{name: "unknown number", state: "NY", number: "NY-000000"},
		{name: "number in another state", state: "NJ", number: "NY-118903"},
		{name: "unknown state", state: "ZZ", number: "NY-118903"},
		{name: "empty", state: "", number: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := svc.License(t.Context(), tc.state, tc.number, asOf)

			require.ErrorIs(t, err, errs.ErrNotFound)
			require.ErrorContains(t, err, tc.state)
			require.ErrorContains(t, err, tc.number)
		})
	}
}

func TestLicenseJSON(t *testing.T) {
	t.Parallel()
	value := license.License{
		NPI:               "1234567890",
		LicenseNumber:     "NY-44821",
		LicenseState:      "NY",
		LicenseExpiration: mustDate(t, "2027-05-30"),
		Window:            expiring90,
	}

	got, err := json.Marshal(value)

	require.NoError(t, err)
	assert.JSONEq(t, `{"npi":"1234567890","license_number":"NY-44821","license_state":"NY","license_expiration":"2027-05-30","window":"EXPIRING_90"}`, string(got))
}

// groupView is a window group in short form: the window and the numbers of its licenses.
type groupView struct {
	Window  license.Window
	Numbers []string
}

func viewGroups(groups []license.WindowGroup) []groupView {
	views := make([]groupView, 0, len(groups))
	for _, group := range groups {
		numbers := []string{}
		for _, l := range group.Licenses {
			numbers = append(numbers, l.LicenseNumber)
		}
		views = append(views, groupView{Window: group.Window, Numbers: numbers})
	}
	return views
}

func TestWindowGroups(t *testing.T) {
	t.Parallel()
	svc, _ := newFixtureService(t)

	// All 16 licenses, in the order of their expiration dates.
	byExpiration := []string{
		"FL-445019", "WA-991027", "MA-880134", "MI-773218", "NY-118903", "GA-664012", "OH-551903", "TX-902244",
		"PA-330871", "IL-447120", "NY-118455", "NY-118402", "NY-119544", "NJ-207744", "AZ-773905", "CO-118277",
	}

	tests := []struct {
		name string
		asOf string
		want []groupView
	}{
		{
			name: "the day of the sample data",
			asOf: "2027-03-01",
			want: []groupView{
				{Window: license.WindowOK, Numbers: []string{"NY-118402", "NY-119544", "NJ-207744", "AZ-773905", "CO-118277"}},
				{Window: expiring90, Numbers: []string{"IL-447120", "NY-118455"}},
				{Window: expiring60, Numbers: []string{"TX-902244", "PA-330871"}},
				{Window: expiring30, Numbers: []string{"MI-773218", "NY-118903", "GA-664012", "OH-551903"}},
				{Window: license.WindowExpired, Numbers: []string{"FL-445019", "WA-991027", "MA-880134"}},
			},
		},
		{
			name: "a day before every warning",
			asOf: "2026-01-01",
			want: []groupView{
				{Window: license.WindowOK, Numbers: byExpiration},
				{Window: expiring90, Numbers: []string{}},
				{Window: expiring60, Numbers: []string{}},
				{Window: expiring30, Numbers: []string{}},
				{Window: license.WindowExpired, Numbers: []string{}},
			},
		},
		{
			name: "a day after every expiration date",
			asOf: "2031-01-01",
			want: []groupView{
				{Window: license.WindowOK, Numbers: []string{}},
				{Window: expiring90, Numbers: []string{}},
				{Window: expiring60, Numbers: []string{}},
				{Window: expiring30, Numbers: []string{}},
				{Window: license.WindowExpired, Numbers: byExpiration},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			groups, err := svc.WindowGroups(t.Context(), mustDate(t, tc.asOf))

			require.NoError(t, err)
			assert.Equal(t, tc.want, viewGroups(groups))
			for _, group := range groups {
				for _, l := range group.Licenses {
					assert.Equal(t, group.Window, l.Window, "%s is in the group of its window", l.LicenseNumber)
				}
			}
		})
	}
}

func TestWindowGroupsOfAnEmptyService(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)

	groups, err := svc.WindowGroups(t.Context(), mustDate(t, "2027-03-01"))

	require.NoError(t, err)
	require.Len(t, groups, 5)
	for i, window := range []license.Window{license.WindowOK, expiring90, expiring60, expiring30, license.WindowExpired} {
		assert.Equal(t, window, groups[i].Window)
		assert.NotNil(t, groups[i].Licenses, "a window with no licenses has an empty slice, not nil")
		assert.Empty(t, groups[i].Licenses)
	}
}

func TestWindowGroupsOrder(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)
	_, err := svc.Load(t.Context(), []license.Record{
		newRecord(t, "1111111111", "Ada", "Lovelace", "TX-2", "TX", "2027-06-01"),
		newRecord(t, "2222222222", "Grace", "Hopper", "AZ-9", "AZ", "2027-06-01"),
		newRecord(t, "3333333333", "Alan", "Turing", "AZ-10", "AZ", "2027-06-01"),
		newRecord(t, "4444444444", "Edsger", "Dijkstra", "NY-5", "NY", "2027-05-01"),
	})
	require.NoError(t, err)

	groups, err := svc.WindowGroups(t.Context(), mustDate(t, "2027-01-01"))

	require.NoError(t, err)
	// The date comes first. For the same date, the state comes next, and then the number as text.
	assert.Equal(t, []string{"NY-5", "AZ-10", "AZ-9", "TX-2"}, viewGroups(groups)[0].Numbers)
}

func TestAudit(t *testing.T) {
	t.Parallel()
	svc, _ := newFixtureService(t)
	scan, err := svc.Scan(t.Context())
	require.NoError(t, err)
	require.Len(t, scan.Hits, 11)

	t.Run("one license", func(t *testing.T) {
		t.Parallel()

		entries, err := svc.Audit(t.Context(), "NY", "NY-118903")

		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "1754482206", entries[0].NPI)
		assert.Equal(t, "NY-118903", entries[0].LicenseNumber)
		assert.Equal(t, "NY", entries[0].LicenseState)
		assert.Equal(t, mustDate(t, "2027-03-20"), entries[0].LicenseExpiration)
		assert.Equal(t, expiring30, entries[0].Window)
	})

	t.Run("a license with no warning", func(t *testing.T) {
		t.Parallel()

		entries, err := svc.Audit(t.Context(), "NJ", "NJ-207744")

		require.NoError(t, err)
		assert.NotNil(t, entries)
		assert.Empty(t, entries)
	})

	t.Run("all licenses", func(t *testing.T) {
		t.Parallel()

		entries, err := svc.Audit(t.Context(), "", "")

		require.NoError(t, err)
		require.Len(t, entries, 11)
		for i, entry := range entries {
			assert.Equal(t, int64(i+1), entry.ID, "the entries are in the order that the scan wrote them")
			assert.Equal(t, scan.Hits[i].AuditEntry, entry)
		}
	})

	t.Run("unknown license", func(t *testing.T) {
		t.Parallel()

		_, err := svc.Audit(t.Context(), "NY", "NY-000000")

		require.ErrorIs(t, err, errs.ErrNotFound)
		assert.ErrorContains(t, err, "NY-000000")
	})

	t.Run("half a license", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct{ state, number string }{{"NY", ""}, {"", "NY-118903"}} {
			_, err := svc.Audit(t.Context(), tc.state, tc.number)

			require.ErrorIs(t, err, errs.ErrInvalid)
		}
	})
}

func TestCanceledContext(t *testing.T) {
	t.Parallel()
	svc, _ := newFixtureService(t)
	asOf := mustDate(t, sampleDay)

	tests := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{name: "Load", call: func(ctx context.Context) error {
			_, err := svc.Load(ctx, nil)
			return err
		}},
		{name: "License", call: func(ctx context.Context) error {
			_, err := svc.License(ctx, "NY", "NY-118903", asOf)
			return err
		}},
		{name: "WindowGroups", call: func(ctx context.Context) error {
			_, err := svc.WindowGroups(ctx, asOf)
			return err
		}},
		{name: "Audit", call: func(ctx context.Context) error {
			_, err := svc.Audit(ctx, "", "")
			return err
		}},
		{name: "Scan", call: func(ctx context.Context) error {
			_, err := svc.Scan(ctx)
			return err
		}},
		{name: "NextWindow", call: func(ctx context.Context) error {
			_, err := svc.NextWindow(ctx, "NY", "NY-118903")
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			err := tc.call(ctx)

			require.ErrorIs(t, err, context.Canceled)
		})
	}

	t.Run("a canceled scan writes nothing", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := svc.Scan(ctx)
		require.ErrorIs(t, err, context.Canceled)

		entries, err := svc.Audit(t.Context(), "", "")
		require.NoError(t, err)
		assert.Empty(t, entries)
	})
}
