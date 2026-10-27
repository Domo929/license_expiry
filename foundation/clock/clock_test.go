package clock_test

import (
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // Embeds the time zone database, so the tests do not depend on the host.

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/foundation/clock"
)

// Both clocks fit the interface that code which needs the time defines for itself.
var (
	_ interface{ Now() time.Time } = clock.Real{}
	_ interface{ Now() time.Time } = (*clock.Simulated)(nil)
)

// loadDenver loads the America/Denver time zone, and stops the test if that fails.
// Denver has a daylight saving change on 2027-03-14 (forward) and 2026-11-01 (back).
func loadDenver(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Denver")
	require.NoError(t, err)
	return loc
}

func TestRealNow(t *testing.T) {
	t.Parallel()

	before := time.Now()
	got := clock.Real{}.Now()
	after := time.Now()

	assert.False(t, got.Before(before), "Now returned %v, which is before the call started at %v", got, before)
	assert.False(t, got.After(after), "Now returned %v, which is after the call ended at %v", got, after)
	assert.Same(t, time.Local, got.Location())
}

func TestSimulatedNow(t *testing.T) {
	t.Parallel()
	denver := loadDenver(t)

	tests := []struct {
		name  string
		start time.Time
		want  string // RFC 3339 with nanoseconds.
	}{
		{name: "UTC", start: time.Date(2027, time.March, 1, 9, 0, 0, 0, time.UTC), want: "2027-03-01T09:00:00Z"},
		{name: "Denver", start: time.Date(2027, time.May, 30, 8, 30, 0, 0, denver), want: "2027-05-30T08:30:00-06:00"},
		{name: "nanoseconds", start: time.Date(2027, time.March, 1, 9, 0, 0, 123_456_789, time.UTC), want: "2027-03-01T09:00:00.123456789Z"},
		{name: "zero time", start: time.Time{}, want: "0001-01-01T00:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clock.NewSimulated(tc.start)

			got := clk.Now()

			assert.Equal(t, tc.want, got.Format(time.RFC3339Nano))
			assert.Same(t, tc.start.Location(), got.Location(), "the clock keeps the time zone it was given")
			assert.Equal(t, tc.want, clk.Now().Format(time.RFC3339Nano), "reading the clock does not move it")
		})
	}
}

func TestSimulatedSet(t *testing.T) {
	t.Parallel()
	denver := loadDenver(t)
	start := time.Date(2027, time.March, 1, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		set  time.Time
		want string // RFC 3339 with nanoseconds.
	}{
		{name: "later", set: time.Date(2027, time.May, 30, 8, 30, 0, 0, denver), want: "2027-05-30T08:30:00-06:00"},
		{name: "earlier", set: time.Date(2026, time.December, 31, 23, 59, 59, 999_999_999, time.UTC), want: "2026-12-31T23:59:59.999999999Z"},
		{name: "same time", set: start, want: "2027-03-01T09:00:00Z"},
		{name: "zero time", set: time.Time{}, want: "0001-01-01T00:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clock.NewSimulated(start)

			clk.Set(tc.set)

			got := clk.Now()
			assert.Equal(t, tc.want, got.Format(time.RFC3339Nano))
			assert.Same(t, tc.set.Location(), got.Location(), "the clock keeps the time zone it was given")
		})
	}
}

func TestSimulatedAddDays(t *testing.T) {
	t.Parallel()
	denver := loadDenver(t)

	tests := []struct {
		name        string
		start       time.Time
		days        int
		want        string        // RFC 3339.
		wantElapsed time.Duration // The real time between the start and the result.
	}{
		{
			name:  "UTC, one day",
			start: time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC), days: 1,
			want: "2027-03-02T12:00:00Z", wantElapsed: 24 * time.Hour,
		},
		{
			name:  "UTC, zero days",
			start: time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC), days: 0,
			want: "2027-03-01T12:00:00Z", wantElapsed: 0,
		},
		{
			name:  "UTC, back one day",
			start: time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC), days: -1,
			want: "2027-02-28T12:00:00Z", wantElapsed: -24 * time.Hour,
		},
		{
			name:  "month end",
			start: time.Date(2027, time.January, 31, 12, 0, 0, 0, time.UTC), days: 1,
			want: "2027-02-01T12:00:00Z", wantElapsed: 24 * time.Hour,
		},
		{
			name:  "year end",
			start: time.Date(2027, time.December, 31, 23, 30, 0, 0, time.UTC), days: 1,
			want: "2028-01-01T23:30:00Z", wantElapsed: 24 * time.Hour,
		},
		{
			name:  "leap day",
			start: time.Date(2028, time.February, 28, 12, 0, 0, 0, time.UTC), days: 2,
			want: "2028-03-01T12:00:00Z", wantElapsed: 48 * time.Hour,
		},
		{
			name:  "90 days",
			start: time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC), days: 90,
			want: "2027-05-30T12:00:00Z", wantElapsed: 90 * 24 * time.Hour,
		},
		{
			name:  "Denver, clocks go forward",
			start: time.Date(2027, time.March, 13, 12, 0, 0, 0, denver), days: 1,
			want: "2027-03-14T12:00:00-06:00", wantElapsed: 23 * time.Hour,
		},
		{
			name:  "Denver, clocks go back",
			start: time.Date(2026, time.October, 31, 12, 0, 0, 0, denver), days: 1,
			want: "2026-11-01T12:00:00-07:00", wantElapsed: 25 * time.Hour,
		},
		{
			name:  "Denver, back over the change",
			start: time.Date(2027, time.March, 14, 12, 0, 0, 0, denver), days: -1,
			want: "2027-03-13T12:00:00-07:00", wantElapsed: -23 * time.Hour,
		},
		{
			name:  "Denver, 30 days with a change in between",
			start: time.Date(2027, time.March, 1, 12, 0, 0, 0, denver), days: 30,
			want: "2027-03-31T12:00:00-06:00", wantElapsed: 30*24*time.Hour - time.Hour,
		},
		{
			name:  "Denver, a year with two changes in between",
			start: time.Date(2027, time.March, 1, 12, 0, 0, 0, denver), days: 365,
			want: "2028-02-29T12:00:00-07:00", wantElapsed: 365 * 24 * time.Hour,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clock.NewSimulated(tc.start)

			clk.AddDays(tc.days)

			got := clk.Now()
			assert.Equal(t, tc.want, got.Format(time.RFC3339))
			assert.Equal(t, tc.wantElapsed, got.Sub(tc.start))
			assert.Same(t, tc.start.Location(), got.Location(), "the clock keeps its time zone")
		})
	}

	t.Run("one day at a time over a change", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewSimulated(time.Date(2027, time.March, 12, 12, 0, 0, 0, denver))

		want := []string{"2027-03-13T12:00:00-07:00", "2027-03-14T12:00:00-06:00", "2027-03-15T12:00:00-06:00"}
		for _, w := range want {
			clk.AddDays(1)

			assert.Equal(t, w, clk.Now().Format(time.RFC3339))
		}
	})

	t.Run("uses the time zone of the last Set", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewSimulated(time.Date(2027, time.March, 13, 12, 0, 0, 0, time.UTC))
		clk.Set(time.Date(2027, time.March, 13, 12, 0, 0, 0, denver))

		clk.AddDays(1)

		assert.Equal(t, "2027-03-14T12:00:00-06:00", clk.Now().Format(time.RFC3339))
	})
}

func TestSimulatedConcurrentUse(t *testing.T) {
	t.Parallel()

	const (
		workers = 8
		steps   = 200
	)
	start := time.Date(2027, time.March, 1, 12, 0, 0, 0, time.UTC)

	t.Run("AddDays loses no step", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewSimulated(start)

		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				for range steps {
					clk.AddDays(1)
					_ = clk.Now()
				}
			})
		}
		wg.Wait()

		want := start.AddDate(0, 0, workers*steps)
		assert.Equal(t, want.Format(time.RFC3339), clk.Now().Format(time.RFC3339))
	})

	t.Run("Now sees only whole values", func(t *testing.T) {
		t.Parallel()
		denver := loadDenver(t)
		values := []time.Time{
			start,
			time.Date(2028, time.February, 29, 8, 30, 0, 0, denver),
			time.Date(2030, time.July, 4, 23, 59, 59, 999_999_999, time.UTC),
		}
		valid := make([]string, len(values))
		for i, v := range values {
			valid[i] = v.Format(time.RFC3339Nano)
		}
		clk := clock.NewSimulated(start)

		var wg sync.WaitGroup
		for i := range workers {
			wg.Go(func() {
				for range steps {
					clk.Set(values[i%len(values)])
				}
			})
			wg.Go(func() {
				for range steps {
					assert.Contains(t, valid, clk.Now().Format(time.RFC3339Nano))
				}
			})
		}
		wg.Wait()
	})

	t.Run("Set, AddDays, and Now together", func(t *testing.T) {
		t.Parallel()
		clk := clock.NewSimulated(start)

		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				for range steps {
					clk.Set(start)
				}
			})
			wg.Go(func() {
				for range steps {
					clk.AddDays(1)
				}
			})
			wg.Go(func() {
				for range steps {
					_ = clk.Now()
				}
			})
		}
		wg.Wait()

		// The final time depends on how the calls interleave. It is some whole number of days after start.
		after := clk.Now()
		assert.False(t, after.Before(start))
		assert.Zero(t, after.Sub(start)%(24*time.Hour), "only whole days were added")
	})
}
