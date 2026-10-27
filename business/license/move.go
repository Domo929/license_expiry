package license

import (
	"context"
	"fmt"

	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/errs"
)

// Move shows the next window that a license enters, and the first day on which it is in that window.
type Move struct {
	// License is the license with the window that it is in today.
	License License

	// From is the window of the license today. It is the same as License.Window.
	From Window

	// To is the next more severe window.
	To Window

	// On is the first day on which the license is in To. It is always after today.
	On civil.Date
}

// NextWindow returns the next window that the license enters. Today is the date of the clock.
//
// A demo uses it to move a license through the windows. Set the clock to the day On, then run a scan.
// NextWindow changes no data, and it does not move the clock.
//
// It wraps errs.ErrNotFound if there is no such license.
// It wraps errs.ErrConflict if the license is EXPIRED already, because no window follows EXPIRED.
func (s *Service) NextWindow(ctx context.Context, state, number string) (Move, error) {
	if err := ctx.Err(); err != nil {
		return Move{}, fmt.Errorf("next window: %w", err)
	}

	st := s.store
	st.mu.RLock()
	defer st.mu.RUnlock()

	row, ok := st.findLicense(state, number)
	if !ok {
		return Move{}, licenseNotFound(state, number)
	}

	from := st.windows.Classify(row.expiration, s.Today())
	next, ok := st.windows.Next(from)
	if !ok {
		return Move{}, fmt.Errorf("license %q in state %q is %s already: %w", number, state, from, errs.ErrConflict)
	}
	// Next never returns OK, the only window with no entry day, so the day exists.
	on, _ := next.EntersOn(row.expiration)
	return Move{License: row.inWindow(from), From: from, To: next.Name, On: on}, nil
}
