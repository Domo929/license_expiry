package license_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/clock"
	"github.com/Domo929/license_expiry/foundation/errs"
)

// demoExpiration is the expiration date of the license of newOneLicenseService.
const demoExpiration = "2027-02-04"

// atDaysLeft returns noon in loc on the day that is daysLeft days before demoExpiration.
func atDaysLeft(t *testing.T, loc *time.Location, daysLeft int) time.Time {
	t.Helper()
	return noon(mustDate(t, demoExpiration).AddDays(-daysLeft), loc)
}

// newOneLicenseService returns a service with one license: NY-1 in NY, which expires on demoExpiration.
// The clock shows noon in loc, daysLeft days before the expiration date.
func newOneLicenseService(t *testing.T, loc *time.Location, daysLeft int) (*license.Service, *clock.Simulated) {
	t.Helper()
	svc, clk := newService(t, atDaysLeft(t, loc, daysLeft), loc)
	_, err := svc.Load(t.Context(), []license.Record{newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", demoExpiration)})
	require.NoError(t, err)
	return svc, clk
}

// hitView is a hit in short form.
type hitView struct {
	Number   string
	Window   license.Window
	Previous license.Window
	Skipped  []license.Window
}

func viewHits(hits []license.Hit) []hitView {
	views := make([]hitView, 0, len(hits))
	for _, hit := range hits {
		views = append(views, hitView{Number: hit.LicenseNumber, Window: hit.Window, Previous: hit.Previous, Skipped: hit.Skipped})
	}
	return views
}

// countingClock counts the readings of the service.
type countingClock struct {
	now   time.Time
	reads atomic.Int32
}

func (c *countingClock) Now() time.Time {
	c.reads.Add(1)
	return c.now
}

// TestLicenseMovesThroughEveryWindow is the demo: a license moves from OK to EXPIRED without any real waiting.
func TestLicenseMovesThroughEveryWindow(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	// 100 days before the expiration date, at noon in Denver.
	svc, clk := newOneLicenseService(t, loc, 100)

	first, err := svc.Scan(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, first.Checked)
	assert.Empty(t, first.Hits, "100 days left is OK, and OK gets no warning")

	steps := []struct {
		to license.Window
		on string // The day of the move.
	}{
		{to: expiring90, on: "2026-11-06"},
		{to: expiring60, on: "2026-12-06"},
		{to: expiring30, on: "2027-01-05"},
		{to: license.WindowExpired, on: "2027-02-04"},
	}
	from := license.WindowOK
	scanTimes := make([]time.Time, 0, len(steps))
	for _, step := range steps {
		t.Run(string(step.to), func(t *testing.T) {
			ctx := t.Context()
			move, err := svc.NextWindow(ctx, "NY", "NY-1")
			require.NoError(t, err)
			assert.Equal(t, from, move.From)
			assert.Equal(t, from, move.License.Window)
			assert.Equal(t, step.to, move.To)
			assert.Equal(t, mustDate(t, step.on), move.On)
			assert.True(t, move.On.After(svc.Today()), "the move is in the future")

			clk.Set(move.On.In(loc).Add(12 * time.Hour))
			scanTimes = append(scanTimes, clk.Now())
			scan, err := svc.Scan(ctx)

			require.NoError(t, err)
			assert.Equal(t, move.On, scan.AsOf)
			require.Len(t, scan.Hits, 1)
			assert.Equal(t, step.to, scan.Hits[0].Window)
			assert.Equal(t, from, scan.Hits[0].Previous)
			assert.Empty(t, scan.Hits[0].Skipped)
			assert.True(t, scanTimes[len(scanTimes)-1].Equal(scan.Hits[0].HitAt), "the hit has the time of the scan")

			again, err := svc.Scan(ctx)
			require.NoError(t, err)
			assert.Empty(t, again.Hits, "a second scan on the same day has no hit")
		})
		from = step.to
	}

	_, err = svc.NextWindow(t.Context(), "NY", "NY-1")
	require.ErrorIs(t, err, errs.ErrConflict, "no window follows EXPIRED")

	entries, err := svc.Audit(t.Context(), "NY", "NY-1")
	require.NoError(t, err)
	require.Len(t, entries, len(steps))
	for i, step := range steps {
		assert.Equal(t, int64(i+1), entries[i].ID)
		assert.Equal(t, step.to, entries[i].Window)
		assert.True(t, scanTimes[i].Equal(entries[i].HitAt), "the audit row has the time of the scan")
	}
}

func TestScanAfterMissedRuns(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, clk := newOneLicenseService(t, loc, 100)

	// The scanner runs with 100 days left. Then it is down until 45 days are left.
	steps := []struct {
		name         string
		daysLeft     int
		wantWindow   license.Window // Empty if the scan has no hit.
		wantPrevious license.Window
		wantSkipped  []license.Window
	}{
		{name: "100 days left", daysLeft: 100},
		{name: "45 days left, after the scanner was down", daysLeft: 45, wantWindow: expiring60, wantPrevious: license.WindowOK, wantSkipped: []license.Window{expiring90}},
		{name: "30 days left", daysLeft: 30, wantWindow: expiring30, wantPrevious: expiring60, wantSkipped: []license.Window{}},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			clk.Set(atDaysLeft(t, loc, step.daysLeft))

			result, err := svc.Scan(t.Context())

			require.NoError(t, err)
			if step.wantWindow == "" {
				assert.Empty(t, result.Hits)
				return
			}
			require.Len(t, result.Hits, 1, "a late scan gives one hit for the window that the license is in")
			assert.Equal(t, step.wantWindow, result.Hits[0].Window)
			assert.Equal(t, step.wantPrevious, result.Hits[0].Previous)
			assert.Equal(t, step.wantSkipped, result.Hits[0].Skipped)
		})
	}
}

func TestFirstScanOfALicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		daysLeft    int
		wantWindow  license.Window // Empty if the scan has no hit.
		wantSkipped []license.Window
	}{
		{name: "OK", daysLeft: 100},
		{name: "the first window", daysLeft: 75, wantWindow: expiring90, wantSkipped: []license.Window{}},
		{name: "the second window", daysLeft: 45, wantWindow: expiring60, wantSkipped: []license.Window{expiring90}},
		{name: "the third window", daysLeft: 20, wantWindow: expiring30, wantSkipped: []license.Window{expiring90, expiring60}},
		{name: "the expiration date", daysLeft: 0, wantWindow: license.WindowExpired, wantSkipped: []license.Window{expiring90, expiring60, expiring30}},
		{name: "expired before the first scan", daysLeft: -25, wantWindow: license.WindowExpired, wantSkipped: []license.Window{expiring90, expiring60, expiring30}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newOneLicenseService(t, loadDenver(t), tc.daysLeft)

			first, err := svc.Scan(t.Context())
			require.NoError(t, err)
			second, err := svc.Scan(t.Context())
			require.NoError(t, err)

			assert.Equal(t, mustDate(t, demoExpiration).AddDays(-tc.daysLeft), first.AsOf)
			assert.Equal(t, 1, first.Checked)
			if tc.wantWindow == "" {
				assert.Empty(t, first.Hits)
			} else {
				require.Len(t, first.Hits, 1)
				assert.Equal(t, tc.wantWindow, first.Hits[0].Window)
				assert.Equal(t, license.WindowOK, first.Hits[0].Previous)
				assert.Equal(t, tc.wantSkipped, first.Hits[0].Skipped)
			}
			assert.Empty(t, second.Hits, "the second scan finds nothing new")
		})
	}
}

func TestScanNeverWarnsBackwards(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, clk := newOneLicenseService(t, loc, 100)

	steps := []struct {
		name         string
		daysLeft     int
		wantWindow   license.Window // Empty if the scan has no hit.
		wantPrevious license.Window
	}{
		{name: "the license is in the third window", daysLeft: 20, wantWindow: expiring30, wantPrevious: license.WindowOK},
		{name: "the clock goes back to the second window", daysLeft: 45},
		{name: "the clock goes back to OK", daysLeft: 100},
		{name: "the clock goes forward to the third window again", daysLeft: 20},
		{name: "the license expires", daysLeft: 0, wantWindow: license.WindowExpired, wantPrevious: expiring30},
		{name: "the clock goes back to the second window after EXPIRED", daysLeft: 45},
		{name: "the clock goes forward to EXPIRED again", daysLeft: -5},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			clk.Set(atDaysLeft(t, loc, step.daysLeft))

			result, err := svc.Scan(t.Context())

			require.NoError(t, err)
			if step.wantWindow == "" {
				assert.Empty(t, result.Hits)
				return
			}
			require.Len(t, result.Hits, 1)
			assert.Equal(t, step.wantWindow, result.Hits[0].Window)
			assert.Equal(t, step.wantPrevious, result.Hits[0].Previous)
		})
	}

	entries, err := svc.Audit(t.Context(), "NY", "NY-1")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, expiring30, entries[0].Window)
	assert.Equal(t, license.WindowExpired, entries[1].Window)
}

func TestScanSampleData(t *testing.T) {
	t.Parallel()
	svc, clk := newFixtureService(t)
	skipAll := []license.Window{expiring90, expiring60, expiring30}

	first, err := svc.Scan(t.Context())
	require.NoError(t, err)

	assert.Equal(t, mustDate(t, "2027-03-01"), first.AsOf)
	assert.Equal(t, 16, first.Checked)
	// One hit for each license that is not OK, in the order of the file. It is the first scan, so each hit skips
	// the windows between OK and its own window.
	assert.Equal(t, []hitView{
		{Number: "NY-118455", Window: expiring90, Previous: license.WindowOK, Skipped: []license.Window{}},
		{Number: "IL-447120", Window: expiring90, Previous: license.WindowOK, Skipped: []license.Window{}},
		{Number: "PA-330871", Window: expiring60, Previous: license.WindowOK, Skipped: []license.Window{expiring90}},
		{Number: "TX-902244", Window: expiring60, Previous: license.WindowOK, Skipped: []license.Window{expiring90}},
		{Number: "OH-551903", Window: expiring30, Previous: license.WindowOK, Skipped: []license.Window{expiring90, expiring60}},
		{Number: "GA-664012", Window: expiring30, Previous: license.WindowOK, Skipped: []license.Window{expiring90, expiring60}},
		{Number: "MI-773218", Window: expiring30, Previous: license.WindowOK, Skipped: []license.Window{expiring90, expiring60}},
		{Number: "MA-880134", Window: license.WindowExpired, Previous: license.WindowOK, Skipped: skipAll},
		{Number: "WA-991027", Window: license.WindowExpired, Previous: license.WindowOK, Skipped: skipAll},
		{Number: "NY-118903", Window: expiring30, Previous: license.WindowOK, Skipped: []license.Window{expiring90, expiring60}},
		{Number: "FL-445019", Window: license.WindowExpired, Previous: license.WindowOK, Skipped: skipAll},
	}, viewHits(first.Hits))
	for i, hit := range first.Hits {
		assert.Equal(t, int64(i+1), hit.ID)
		assert.True(t, clk.Now().Equal(hit.HitAt))
	}

	second, err := svc.Scan(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 16, second.Checked)
	assert.Empty(t, second.Hits, "the second scan finds nothing new")

	// Camille Dubois holds two licenses. Only the one in a window got a warning.
	ny, err := svc.Audit(t.Context(), "NY", "NY-118903")
	require.NoError(t, err)
	assert.Len(t, ny, 1)
	nj, err := svc.Audit(t.Context(), "NJ", "NJ-207744")
	require.NoError(t, err)
	assert.Empty(t, nj)
}

func TestScanOfAnEmptyService(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)

	result, err := svc.Scan(t.Context())

	require.NoError(t, err)
	assert.Equal(t, mustDate(t, "2027-03-01"), result.AsOf)
	assert.Zero(t, result.Checked)
	assert.NotNil(t, result.Hits)
	assert.Empty(t, result.Hits)
}

func TestScanUsesTheDayInTheLocation(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)

	tests := []struct {
		name       string
		now        string // RFC 3339.
		wantAsOf   string
		wantWindow license.Window
	}{
		// Denver is on UTC-7 in March, so midnight in Denver is 07:00 in UTC.
		{name: "the last second before the day", now: "2027-03-01T06:59:59Z", wantAsOf: "2027-02-28", wantWindow: expiring30},
		{name: "the first second of the day", now: "2027-03-01T07:00:00Z", wantAsOf: "2027-03-01", wantWindow: license.WindowExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now, err := time.Parse(time.RFC3339, tc.now)
			require.NoError(t, err)
			svc, _ := newService(t, now, loc)
			_, err = svc.Load(t.Context(), []license.Record{newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", "2027-03-01")})
			require.NoError(t, err)

			result, err := svc.Scan(t.Context())

			require.NoError(t, err)
			assert.Equal(t, mustDate(t, tc.wantAsOf), result.AsOf)
			require.Len(t, result.Hits, 1)
			assert.Equal(t, tc.wantWindow, result.Hits[0].Window)
			assert.True(t, now.Equal(result.Hits[0].HitAt))
		})
	}
}

func TestScanReadsTheClockOnce(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	clk := &countingClock{now: noon(mustDate(t, "2027-03-01"), loc)}
	svc := license.NewService(license.Config{Policy: newPolicy(t, currentThresholds...), Clock: clk, Location: loc})
	_, err := svc.Load(t.Context(), fixture(t))
	require.NoError(t, err)
	before := clk.reads.Load()

	result, err := svc.Scan(t.Context())

	require.NoError(t, err)
	assert.Equal(t, int32(1), clk.reads.Load()-before, "one reading gives the day and the time of every audit row")
	for _, hit := range result.Hits {
		assert.True(t, clk.now.Equal(hit.HitAt))
	}
}

func TestScanSeesDataThatIsLoadedLater(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)

	// The licenses are expired. The ids of the licenses continue from one load to the next.
	_, err := svc.Load(ctx, []license.Record{newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", "2027-01-01")})
	require.NoError(t, err)
	first, err := svc.Scan(ctx)
	require.NoError(t, err)
	_, err = svc.Load(ctx, []license.Record{
		newRecord(t, "2222222222", "Grace", "Hopper", "NJ-2", "NJ", "2027-01-02"),
		newRecord(t, "3333333333", "Alan", "Turing", "TX-3", "TX", "2027-01-03"),
	})
	require.NoError(t, err)
	second, err := svc.Scan(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, first.Checked)
	assert.Equal(t, []string{"NY-1"}, hitNumbers(first.Hits))
	assert.Equal(t, 3, second.Checked)
	assert.Equal(t, []string{"NJ-2", "TX-3"}, hitNumbers(second.Hits), "the first license gets no second warning")
	assert.Equal(t, int64(2), second.Hits[0].ID)
	assert.Equal(t, int64(3), second.Hits[1].ID)
}

func hitNumbers(hits []license.Hit) []string {
	numbers := []string{}
	for _, hit := range hits {
		numbers = append(numbers, hit.LicenseNumber)
	}
	return numbers
}

func TestScanSeesAnExpirationDateThatMovesEarlier(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)

	steps := []struct {
		name       string
		expiration string // The date in the record that is loaded before the scan.
		wantWindow license.Window
		wantHit    bool
	}{
		{name: "92 days left", expiration: "2027-06-01", wantWindow: license.WindowOK},
		{name: "a record with an earlier date, 45 days left", expiration: "2027-04-15", wantWindow: expiring60, wantHit: true},
		{name: "a record with a later date has no effect", expiration: "2027-09-01", wantWindow: expiring60},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			_, err := svc.Load(ctx, []license.Record{newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", step.expiration)})
			require.NoError(t, err)

			result, err := svc.Scan(ctx)

			require.NoError(t, err)
			if step.wantHit {
				require.Len(t, result.Hits, 1)
				assert.Equal(t, step.wantWindow, result.Hits[0].Window)
			} else {
				assert.Empty(t, result.Hits)
			}
			got, err := svc.License(ctx, "NY", "NY-1", svc.Today())
			require.NoError(t, err)
			assert.Equal(t, step.wantWindow, got.Window)
		})
	}
}

func TestScanConcurrently(t *testing.T) {
	t.Parallel()
	svc, _ := newFixtureService(t)
	asOf := mustDate(t, sampleDay)
	const workers = 8

	scans := make([]license.ScanResult, workers)
	scanErrs := make([]error, workers)
	readErrs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			scans[i], scanErrs[i] = svc.Scan(t.Context())
		})
		// Readers run at the same time as the scans.
		wg.Go(func() {
			_, readErrs[i] = svc.WindowGroups(t.Context(), asOf)
			if readErrs[i] == nil {
				_, readErrs[i] = svc.Audit(t.Context(), "", "")
			}
		})
	}
	wg.Wait()

	for i := range workers {
		require.NoError(t, scanErrs[i])
		require.NoError(t, readErrs[i])
	}

	// The scans take turns. The first one finds every warning, and the others find none.
	withHits := 0
	for _, scan := range scans {
		assert.Equal(t, 16, scan.Checked)
		if len(scan.Hits) > 0 {
			withHits++
			assert.Len(t, scan.Hits, 11)
		}
	}
	assert.Equal(t, 1, withHits)

	// Each license that is not OK has exactly one audit row.
	entries, err := svc.Audit(t.Context(), "", "")
	require.NoError(t, err)
	require.Len(t, entries, 11)
	seen := map[string]int{}
	for i, entry := range entries {
		assert.Equal(t, int64(i+1), entry.ID)
		seen[entry.LicenseState+" "+entry.LicenseNumber+" "+string(entry.Window)]++
	}
	assert.Len(t, seen, 11)
	for key, count := range seen {
		assert.Equal(t, 1, count, key)
	}
}

func TestNextWindow(t *testing.T) {
	t.Parallel()
	svc, _ := newFixtureService(t)
	today := mustDate(t, sampleDay)

	tests := []struct {
		name     string
		state    string
		number   string
		wantFrom license.Window
		wantTo   license.Window
		wantOn   string
		wantErr  error
	}{
		{name: "OK to the first window", state: "NY", number: "NY-118402", wantFrom: license.WindowOK, wantTo: expiring90, wantOn: "2027-03-02"},
		{name: "OK a long time before", state: "NJ", number: "NJ-207744", wantFrom: license.WindowOK, wantTo: expiring90, wantOn: "2027-08-03"},
		{name: "the first window to the second", state: "NY", number: "NY-118455", wantFrom: expiring90, wantTo: expiring60, wantOn: "2027-03-31"},
		{name: "the second window to the third", state: "PA", number: "PA-330871", wantFrom: expiring60, wantTo: expiring30, wantOn: "2027-03-31"},
		{name: "the third window to EXPIRED", state: "NY", number: "NY-118903", wantFrom: expiring30, wantTo: license.WindowExpired, wantOn: "2027-03-20"},
		{name: "expired today", state: "MA", number: "MA-880134", wantErr: errs.ErrConflict},
		{name: "expired long ago", state: "FL", number: "FL-445019", wantErr: errs.ErrConflict},
		{name: "unknown license", state: "NY", number: "NY-000000", wantErr: errs.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			move, err := svc.NextWindow(ctx, tc.state, tc.number)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.ErrorContains(t, err, tc.number)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantFrom, move.From)
			assert.Equal(t, tc.wantFrom, move.License.Window)
			assert.Equal(t, tc.wantTo, move.To)
			assert.Equal(t, mustDate(t, tc.wantOn), move.On)
			assert.Equal(t, tc.number, move.License.LicenseNumber)
			assert.True(t, move.On.After(today), "the day of the move is after today")

			// The day of the move is the first day of the next window.
			onTheDay, err := svc.License(ctx, tc.state, tc.number, move.On)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTo, onTheDay.Window)
			dayBefore, err := svc.License(ctx, tc.state, tc.number, move.On.AddDays(-1))
			require.NoError(t, err)
			assert.Equal(t, tc.wantFrom, dayBefore.Window)
		})
	}
}

func TestNextWindowChangesNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	svc, clk := newFixtureService(t)
	before := clk.Now()

	_, err := svc.NextWindow(ctx, "NY", "NY-118402")

	require.NoError(t, err)
	assert.True(t, before.Equal(clk.Now()), "the clock did not move")
	entries, err := svc.Audit(ctx, "", "")
	require.NoError(t, err)
	assert.Empty(t, entries, "no audit row was written")
}
