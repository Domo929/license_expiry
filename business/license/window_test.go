package license_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/errs"
)

func TestNewPolicyRejectsBadThresholds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		thresholds []int
		wantErr    string
	}{
		{name: "nil", thresholds: nil, wantErr: "at least one threshold"},
		{name: "empty", thresholds: []int{}, wantErr: "at least one threshold"},
		{name: "zero", thresholds: []int{90, 0}, wantErr: "threshold 0 is not above 0"},
		{name: "negative", thresholds: []int{90, -5, 30}, wantErr: "threshold -5 is not above 0"},
		{name: "duplicate", thresholds: []int{90, 30, 90}, wantErr: "threshold 90 is listed twice"},
		{name: "duplicate in the only pair", thresholds: []int{7, 7}, wantErr: "threshold 7 is listed twice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := license.NewPolicy(tc.thresholds)

			require.ErrorContains(t, err, tc.wantErr)
			// A bad threshold is a mistake in the configuration. It is not input from a client.
			assert.NotErrorIs(t, err, errs.ErrInvalid)
		})
	}
}

func TestNewPolicyBuildsTheWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		thresholds []int
		want       []license.WindowRow
	}{
		{
			name:       "the current thresholds",
			thresholds: []int{90, 60, 30},
			want: []license.WindowRow{
				{Name: license.WindowOK, Severity: 0, Unbounded: true},
				{Name: expiring90, Severity: 1, MaxDaysLeft: 90},
				{Name: expiring60, Severity: 2, MaxDaysLeft: 60},
				{Name: expiring30, Severity: 3, MaxDaysLeft: 30},
				{Name: license.WindowExpired, Severity: 4, MaxDaysLeft: 0},
			},
		},
		{
			name:       "unsorted input",
			thresholds: []int{30, 90, 60},
			want: []license.WindowRow{
				{Name: license.WindowOK, Severity: 0, Unbounded: true},
				{Name: expiring90, Severity: 1, MaxDaysLeft: 90},
				{Name: expiring60, Severity: 2, MaxDaysLeft: 60},
				{Name: expiring30, Severity: 3, MaxDaysLeft: 30},
				{Name: license.WindowExpired, Severity: 4, MaxDaysLeft: 0},
			},
		},
		{
			name:       "one threshold",
			thresholds: []int{7},
			want: []license.WindowRow{
				{Name: license.WindowOK, Severity: 0, Unbounded: true},
				{Name: "EXPIRING_7", Severity: 1, MaxDaysLeft: 7},
				{Name: license.WindowExpired, Severity: 2, MaxDaysLeft: 0},
			},
		},
		{
			name:       "other thresholds",
			thresholds: []int{14, 120, 45},
			want: []license.WindowRow{
				{Name: license.WindowOK, Severity: 0, Unbounded: true},
				{Name: "EXPIRING_120", Severity: 1, MaxDaysLeft: 120},
				{Name: "EXPIRING_45", Severity: 2, MaxDaysLeft: 45},
				{Name: "EXPIRING_14", Severity: 3, MaxDaysLeft: 14},
				{Name: license.WindowExpired, Severity: 4, MaxDaysLeft: 0},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := slices.Clone(tc.thresholds)

			policy, err := license.NewPolicy(tc.thresholds)

			require.NoError(t, err)
			assert.Equal(t, tc.want, policy.Windows())
			assert.Equal(t, input, tc.thresholds, "NewPolicy does not change the slice that it gets")
		})
	}
}

func TestPolicyWindowsReturnsACopy(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)

	rows := policy.Windows()
	rows[0].Name = "CHANGED"
	rows[1].MaxDaysLeft = 1

	got := policy.Windows()
	assert.Equal(t, license.WindowOK, got[0].Name)
	assert.Equal(t, 90, got[1].MaxDaysLeft)
}

func TestPolicyLookup(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)

	tests := []struct {
		name         string
		window       license.Window
		wantSeverity int
		wantFound    bool
	}{
		{name: "OK", window: license.WindowOK, wantSeverity: 0, wantFound: true},
		{name: "the middle", window: expiring60, wantSeverity: 2, wantFound: true},
		{name: "EXPIRED", window: license.WindowExpired, wantSeverity: 4, wantFound: true},
		{name: "unknown", window: "EXPIRING_45", wantFound: false},
		{name: "empty", window: "", wantFound: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			row, found := policy.Lookup(tc.window)

			assert.Equal(t, tc.wantFound, found)
			if found {
				assert.Equal(t, tc.window, row.Name)
				assert.Equal(t, tc.wantSeverity, row.Severity)
			}
		})
	}
}

func TestPolicyNext(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)

	tests := []struct {
		name      string
		window    license.Window
		want      license.Window
		wantFound bool
	}{
		{name: "after OK", window: license.WindowOK, want: expiring90, wantFound: true},
		{name: "after the first", window: expiring90, want: expiring60, wantFound: true},
		{name: "after the second", window: expiring60, want: expiring30, wantFound: true},
		{name: "after the last threshold", window: expiring30, want: license.WindowExpired, wantFound: true},
		{name: "after EXPIRED", window: license.WindowExpired, wantFound: false},
		{name: "unknown", window: "EXPIRING_45", wantFound: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			row, found := policy.Next(tc.window)

			assert.Equal(t, tc.wantFound, found)
			assert.Equal(t, tc.want, row.Name)
		})
	}
}

func TestPolicyBetween(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)

	tests := []struct {
		name string
		from license.Window
		to   license.Window
		want []license.Window
	}{
		{name: "all the windows in the middle", from: license.WindowOK, to: license.WindowExpired, want: []license.Window{expiring90, expiring60, expiring30}},
		{name: "one window", from: license.WindowOK, to: expiring60, want: []license.Window{expiring90}},
		{name: "two windows", from: license.WindowOK, to: expiring30, want: []license.Window{expiring90, expiring60}},
		{name: "none, next to each other", from: expiring60, to: expiring30, want: []license.Window{}},
		{name: "none, the same window", from: expiring60, to: expiring60, want: []license.Window{}},
		{name: "none, wrong order", from: license.WindowExpired, to: license.WindowOK, want: []license.Window{}},
		{name: "none, unknown from", from: "EXPIRING_45", to: license.WindowExpired, want: []license.Window{}},
		{name: "none, unknown to", from: license.WindowOK, to: "EXPIRING_45", want: []license.Window{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := policy.Between(tc.from, tc.to)

			assert.Equal(t, tc.want, got, "the result is empty, not nil, when no window is between")
		})
	}
}

func TestWindowRowEntersOn(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)
	expiration := mustDate(t, "2027-05-30")
	rows := policy.Windows()

	t.Run("OK has no entry day", func(t *testing.T) {
		t.Parallel()

		_, ok := rows[0].EntersOn(expiration)

		assert.False(t, ok)
	})

	// The entry day is the first day that Classify gives the window. The day before, the license is in the window before.
	for i, row := range rows[1:] {
		t.Run(string(row.Name), func(t *testing.T) {
			t.Parallel()

			day, ok := row.EntersOn(expiration)

			require.True(t, ok)
			assert.Equal(t, expiration.AddDays(-row.MaxDaysLeft), day)
			assert.Equal(t, row.Name, policy.Classify(expiration, day))
			assert.Equal(t, rows[i].Name, policy.Classify(expiration, day.AddDays(-1)))
		})
	}
}

func TestClassifyBoundaries(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)
	today := mustDate(t, "2027-03-01")

	tests := []struct {
		expiration string
		want       license.Window
	}{
		{expiration: "2027-05-31", want: license.WindowOK},      // 91 days left.
		{expiration: "2027-05-30", want: expiring90},            // 90.
		{expiration: "2027-05-29", want: expiring90},            // 89.
		{expiration: "2027-05-01", want: expiring90},            // 61.
		{expiration: "2027-04-30", want: expiring60},            // 60.
		{expiration: "2027-04-29", want: expiring60},            // 59.
		{expiration: "2027-04-01", want: expiring60},            // 31.
		{expiration: "2027-03-31", want: expiring30},            // 30.
		{expiration: "2027-03-30", want: expiring30},            // 29.
		{expiration: "2027-03-02", want: expiring30},            // 1.
		{expiration: "2027-03-01", want: license.WindowExpired}, // 0. The expiration date counts as expired.
		{expiration: "2027-02-28", want: license.WindowExpired}, // -1.
		{expiration: "2026-12-01", want: license.WindowExpired}, // -90.
		{expiration: "2028-02-29", want: license.WindowOK},      // 364.
	}
	for _, tc := range tests {
		t.Run(tc.expiration, func(t *testing.T) {
			t.Parallel()

			got := policy.Classify(mustDate(t, tc.expiration), today)

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifySweep(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)
	today := mustDate(t, "2027-03-01")

	// As the days left fall from 120 to -10, each day has one known window, and the severity never falls.
	previous := -1
	for daysLeft := 120; daysLeft >= -10; daysLeft-- {
		window := policy.Classify(today.AddDays(daysLeft), today)

		row, known := policy.Lookup(window)
		require.True(t, known, "%d days left give the unknown window %q", daysLeft, window)
		assert.GreaterOrEqual(t, row.Severity, previous, "%d days left", daysLeft)
		previous = row.Severity
	}
	assert.Equal(t, 4, previous, "the sweep ends in EXPIRED")
}

func TestClassifyOtherThresholds(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, 120, 45, 14)
	today := mustDate(t, "2027-03-01")

	tests := []struct {
		daysLeft int
		want     license.Window
	}{
		{daysLeft: 121, want: license.WindowOK},
		{daysLeft: 120, want: "EXPIRING_120"},
		{daysLeft: 46, want: "EXPIRING_120"},
		{daysLeft: 45, want: "EXPIRING_45"},
		{daysLeft: 15, want: "EXPIRING_45"},
		{daysLeft: 14, want: "EXPIRING_14"},
		{daysLeft: 1, want: "EXPIRING_14"},
		{daysLeft: 0, want: license.WindowExpired},
		{daysLeft: -1, want: license.WindowExpired},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d days left", tc.daysLeft), func(t *testing.T) {
			t.Parallel()

			got := policy.Classify(today.AddDays(tc.daysLeft), today)

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyDoesNotOverflow(t *testing.T) {
	t.Parallel()
	policy := newPolicy(t, currentThresholds...)

	// OK has no number in place of a bound, so dates far from today cannot overflow it.
	assert.Equal(t, license.WindowOK, policy.Classify(civil.New(9999, time.December, 31), civil.New(1970, time.January, 1)))
	assert.Equal(t, license.WindowExpired, policy.Classify(civil.New(1970, time.January, 1), civil.New(9999, time.December, 31)))
}
