package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Domo929/license_expiry/business/license"
)

//go:generate go tool mockgen -typed -source=scheduler.go -destination=mock_scheduler_test.go -package=main

// scanner runs one scan. *license.Service implements it.
type scanner interface {
	Scan(ctx context.Context) (license.ScanResult, error)
}

// runScheduler scans at once, and then every interval, until ctx ends.
//
// The first scan is the check at start-up. The data is loaded again at each start, so the scan sets the windows of it at once.
// A scan can run twice and do no harm. So if a scan fails, the scheduler logs it, and the next tick tries again.
// An interval of 0 turns the scans after the first one off.
func runScheduler(ctx context.Context, log *slog.Logger, svc scanner, interval time.Duration) {
	scan := func() {
		if _, err := runScan(ctx, log, svc); err != nil {
			log.Error("scan failed", "error", err)
		}
	}

	scan()
	if interval <= 0 {
		log.Info("scans at an interval are off, so the scan at start-up is the only one")
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
		// A tick and the end of ctx can arrive together, and select then picks one at random.
		// Check ctx first, so that a shutdown never starts a scan that can only fail.
		if ctx.Err() != nil {
			log.Info("scheduler stopped")
			return
		}
		scan()
	}
}

// runScan runs one scan, and logs what it found. The caller handles the error, so it is not logged here.
func runScan(ctx context.Context, log *slog.Logger, svc scanner) (license.ScanResult, error) {
	result, err := svc.Scan(ctx)
	if err != nil {
		return license.ScanResult{}, fmt.Errorf("run scan: %w", err)
	}

	log.Info("scan finished", "as_of", result.AsOf, "checked", result.Checked, "hits", len(result.Hits))
	for _, hit := range result.Hits {
		hitLog := log.With("npi", hit.NPI, "license_state", hit.LicenseState, "license_number", hit.LicenseNumber, "window", hit.Window)
		hitLog.Info("license entered window", "previous_window", hit.Previous)
		// A skipped window means that a scan did not run, or that the data came late.
		if len(hit.Skipped) > 0 {
			hitLog.Warn("license skipped windows", "previous_window", hit.Previous, "skipped_windows", hit.Skipped)
		}
	}
	return result, nil
}
