package license

import (
	"context"
	"fmt"

	"github.com/Domo929/license_expiry/foundation/civil"
)

// Hit is a license that a scan warned about, because the license entered a window with no audit row.
type Hit struct {
	// AuditEntry is the audit row that the scan wrote, with its license.
	AuditEntry

	// Previous is the most severe window that the license had an audit row for before this scan.
	// It is OK if the license had none.
	Previous Window

	// Skipped has the windows between Previous and the window of the hit. The license has no audit row for them.
	// A scan that runs late shows here what it missed. The scan works it out for the result.
	// The audit table does not keep it. The slice is empty, never nil, if no window was skipped.
	Skipped []Window
}

// ScanResult is what a scan found.
type ScanResult struct {
	// AsOf is the day of the scan.
	AsOf civil.Date

	// Checked is the number of licenses that the scan looked at.
	Checked int

	// Hits has one hit for each warning, in the order that the licenses were loaded.
	// It is empty, never nil, if no license entered a window.
	Hits []Hit
}

// Scan warns about the licenses that entered a window. It returns one hit for each warning.
//
// A scan reads the clock once. The day of the scan is the date of that reading, in the time zone of the service.
// The scan visits the licenses in the order that they were loaded. For each license it does these steps:
//
//  1. Find the window of the license on the day of the scan.
//  2. Find the most severe window that the license has an audit row for. It is OK if there is none.
//  3. If the window is not more severe, do nothing. That skips a license that is OK, and a license that was warned already.
//  4. Write an audit row for the window, and add a hit.
//
// So a license never gets a second warning for a window.
// It also never gets a warning for a less severe window than before, even if the clock moves back.
//
// A scan that runs late can find a license that skipped a window. The scan writes one row, for the window that the license is in.
// The hit lists the skipped windows, so a missed window is not silent.
//
// Scan holds the write lock, so scans that run at the same time take turns.
// Scan checks the context once, before it starts. After that nothing in it can fail.
// So a scan writes all of its audit rows, or none.
func (s *Service) Scan(ctx context.Context) (ScanResult, error) {
	if err := ctx.Err(); err != nil {
		return ScanResult{}, fmt.Errorf("scan: %w", err)
	}

	st := s.store
	st.mu.Lock()
	defer st.mu.Unlock()

	// The clock is read under the lock. Then the ids and the times of the audit rows grow together.
	now := s.clock.Now()
	today := s.dateOf(now)

	result := ScanResult{AsOf: today, Checked: len(st.licenses), Hits: []Hit{}}
	for _, row := range st.licenses {
		window := st.windows.Classify(row.expiration, today)
		previous := st.mostSevereAudited(row.id)
		// OK is the least severe window, so this test also skips each license that is OK.
		if !st.windows.moreSevere(window, previous) {
			continue
		}
		result.Hits = append(result.Hits, Hit{
			AuditEntry: st.insertAudit(row.id, window, now),
			Previous:   previous,
			Skipped:    st.windows.Between(previous, window),
		})
	}
	return result, nil
}
