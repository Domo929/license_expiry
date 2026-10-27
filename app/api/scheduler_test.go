package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
)

// The tests of the scheduler run in a synctest bubble. Time in the bubble moves only when every goroutine waits,
// so a test that sleeps for hours takes no real time.

func TestRunScheduler_ScansAtStartAndAtEachInterval(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		svc := NewMockscanner(gomock.NewController(t))
		start := time.Now()
		var scans []time.Duration // The time of each scan, counted from the start.
		svc.EXPECT().Scan(gomock.Any()).DoAndReturn(func(context.Context) (license.ScanResult, error) {
			scans = append(scans, time.Since(start))
			return license.ScanResult{}, nil
		}).AnyTimes()
		log, logs := newTestLogger()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			runScheduler(ctx, log, svc, time.Hour)
		}()

		synctest.Wait()
		assert.Equal(t, []time.Duration{0}, scans, "the scan at start-up runs at once")

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, []time.Duration{0, time.Hour}, scans)

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, []time.Duration{0, time.Hour, 2 * time.Hour}, scans)

		cancel()
		<-done
		time.Sleep(10 * time.Hour)
		assert.Len(t, scans, 3, "no scan runs after the context ends")
		assert.Contains(t, logs.String(), `level=INFO msg="scheduler stopped"`)
	})
}

func TestRunScheduler_AScanThatFailsDoesNotStopIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		svc := NewMockscanner(gomock.NewController(t))
		// The first scan is the one at start-up. The mock matches the calls to these two expectations in order.
		svc.EXPECT().Scan(gomock.Any()).Return(license.ScanResult{}, errors.New("memory is full")).Times(1)
		svc.EXPECT().Scan(gomock.Any()).Return(license.ScanResult{}, nil).Times(2)
		log, logs := newTestLogger()
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			runScheduler(ctx, log, svc, time.Hour)
		}()

		synctest.Wait()
		assert.Contains(t, logs.String(), `level=ERROR msg="scan failed" error="run scan: memory is full"`)

		time.Sleep(2 * time.Hour) // Two ticks. The mock fails the test if a scan does not run, or if one runs too often.
		synctest.Wait()
		cancel()
		<-done
	})
}

func TestRunScheduler_AnIntervalThatIsNotPositiveIsOneScan(t *testing.T) {
	t.Parallel()

	for _, interval := range []time.Duration{0, -time.Second} {
		t.Run(interval.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				svc := NewMockscanner(gomock.NewController(t))
				svc.EXPECT().Scan(gomock.Any()).Return(license.ScanResult{}, nil).Times(1)
				log, logs := newTestLogger()

				runScheduler(t.Context(), log, svc, interval) // It returns at once. A ticker would panic with this interval.

				time.Sleep(24 * time.Hour) // The mock fails the test if a second scan runs.
				assert.Contains(t, logs.String(), `level=INFO msg="scans at an interval are off, so the scan at start-up is the only one"`)
			})
		})
	}
}

func TestRunScan(t *testing.T) {
	t.Parallel()

	hitAt := time.Date(2027, time.March, 1, 19, 0, 0, 0, time.UTC)
	onTime := license.Hit{
		ID: 1, NPI: "1592038847", LicenseNumber: "NY-118455", LicenseState: "NY", Window: "EXPIRING_90", HitAt: hitAt,
		Previous: license.WindowOK,
		Skipped:  []license.Window{},
	}
	late := license.Hit{
		ID: 2, NPI: "1204487719", LicenseNumber: "OH-551903", LicenseState: "OH", Window: "EXPIRING_30", HitAt: hitAt,
		Previous: license.WindowOK,
		Skipped:  []license.Window{"EXPIRING_90", "EXPIRING_60"},
	}

	t.Run("it logs the summary, each hit, and each hit that skipped windows", func(t *testing.T) {
		t.Parallel()
		want := license.ScanResult{AsOf: civil.New(2027, time.March, 1), Checked: 16, Hits: []license.Hit{onTime, late}}
		svc := NewMockscanner(gomock.NewController(t))
		svc.EXPECT().Scan(gomock.Any()).Return(want, nil)
		log, logs := newTestLogger()

		got, err := runScan(t.Context(), log, svc)

		require.NoError(t, err)
		assert.Equal(t, want, got)
		lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
		assert.Equal(t, []string{
			`level=INFO msg="scan finished" as_of=2027-03-01 checked=16 hits=2`,
			`level=INFO msg="license entered window" npi=1592038847 license_state=NY license_number=NY-118455 window=EXPIRING_90 previous_window=OK`,
			`level=INFO msg="license entered window" npi=1204487719 license_state=OH license_number=OH-551903 window=EXPIRING_30 previous_window=OK`,
			`level=WARN msg="license skipped windows" npi=1204487719 license_state=OH license_number=OH-551903 window=EXPIRING_30 previous_window=OK skipped_windows="[EXPIRING_90 EXPIRING_60]"`,
		}, lines)
	})

	t.Run("it returns the error and does not log it", func(t *testing.T) {
		t.Parallel()
		svc := NewMockscanner(gomock.NewController(t))
		svc.EXPECT().Scan(gomock.Any()).Return(license.ScanResult{}, errors.New("memory is full"))
		log, logs := newTestLogger()

		_, err := runScan(t.Context(), log, svc)

		require.ErrorContains(t, err, "memory is full")
		assert.Empty(t, logs.String(), "the caller handles the error")
	})
}
