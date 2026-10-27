package license

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/Domo929/license_expiry/foundation/civil"
)

// Window is the name of a window. A window is a range of days that a license has left before it expires.
type Window string

const (
	// WindowOK is the window of a license that has more days left than the largest threshold.
	WindowOK Window = "OK"

	// WindowExpired is the window of a license that has no days left. A license is expired on its expiration date.
	WindowExpired Window = "EXPIRED"
)

// expiringPrefix starts the name of each window between OK and EXPIRED. The threshold follows it.
const expiringPrefix = "EXPIRING_"

// WindowRow is one row of the windows table.
type WindowRow struct {
	// Name is the primary key: OK, EXPIRING_n for a threshold n, or EXPIRED.
	Name Window

	// Severity puts the windows in order. OK has 0, and EXPIRED has the highest value. Each window has its own value.
	Severity int

	// MaxDaysLeft is the largest number of days left that belongs to the window.
	// EXPIRING_n has n, and EXPIRED has 0. It has no meaning if Unbounded is true.
	MaxDaysLeft int

	// Unbounded is true for a window with no limit on the days left. Only OK is unbounded.
	// The table has NULL in max_days_left for it. A flag is safe: a huge number in its place could overflow date math.
	Unbounded bool
}

// reaches reports whether a license with daysLeft days left is in the window, or past it.
func (r WindowRow) reaches(daysLeft int) bool {
	return r.Unbounded || daysLeft <= r.MaxDaysLeft
}

// EntersOn returns the first day on which a license that expires on expiration is in the window.
// That day is the expiration date minus MaxDaysLeft. The second result is false for OK, which has no such day.
func (r WindowRow) EntersOn(expiration civil.Date) (civil.Date, bool) {
	if r.Unbounded {
		return civil.Date{}, false
	}
	return expiration.AddDays(-r.MaxDaysLeft), true
}

// Policy is the list of windows that the thresholds define. It is the windows table of the store.
// A Policy never changes after NewPolicy returns it. Use NewPolicy to make one: the zero Policy has no windows.
type Policy struct {
	windows []WindowRow // In severity order. The position of a row is its severity.
}

// NewPolicy returns the policy for the thresholds. A threshold is a number of days before the expiration date.
//
// The thresholds can be in any order. Each must be above 0, and no two can be the same.
// The policy needs at least one threshold.
//
// The windows are OK, then EXPIRING_n for each threshold n with the largest first, then EXPIRED.
// For the thresholds 14 and 7, the windows are OK, EXPIRING_14, EXPIRING_7, and EXPIRED.
func NewPolicy(thresholds []int) (Policy, error) {
	if len(thresholds) == 0 {
		return Policy{}, errors.New("new policy: the policy needs at least one threshold")
	}

	// A license meets the largest threshold first.
	descending := slices.Clone(thresholds)
	slices.SortFunc(descending, func(a, b int) int { return cmp.Compare(b, a) })
	for i, n := range descending {
		if n <= 0 {
			return Policy{}, fmt.Errorf("new policy: threshold %d is not above 0", n)
		}
		if i > 0 && n == descending[i-1] {
			return Policy{}, fmt.Errorf("new policy: threshold %d is listed twice", n)
		}
	}

	windows := make([]WindowRow, 0, len(descending)+2)
	windows = append(windows, WindowRow{Name: WindowOK, Unbounded: true})
	for _, n := range descending {
		windows = append(windows, WindowRow{Name: Window(expiringPrefix + strconv.Itoa(n)), MaxDaysLeft: n})
	}
	windows = append(windows, WindowRow{Name: WindowExpired, MaxDaysLeft: 0})
	for i := range windows {
		windows[i].Severity = i
	}
	return Policy{windows: windows}, nil
}

// Windows returns the rows of the windows table in severity order: OK first, and EXPIRED last.
// The slice is a copy. A change to it does not change the policy.
func (p Policy) Windows() []WindowRow {
	return slices.Clone(p.windows)
}

// Lookup returns the row of the window with the name. The second result is false if the policy has no such window.
func (p Policy) Lookup(name Window) (WindowRow, bool) {
	i := p.indexOf(name)
	if i < 0 {
		return WindowRow{}, false
	}
	return p.windows[i], true
}

// Next returns the row of the window after the named window in severity order.
// The second result is false after EXPIRED, which is the last window, and for an unknown name.
func (p Policy) Next(name Window) (WindowRow, bool) {
	i := p.indexOf(name)
	if i < 0 || i == len(p.windows)-1 {
		return WindowRow{}, false
	}
	return p.windows[i+1], true
}

// Between returns the names of the windows that are more severe than from and less severe than to, in severity order.
// The slice is empty, never nil, if no window is between them, or if a name is unknown.
func (p Policy) Between(from, to Window) []Window {
	between := []Window{}
	i, j := p.indexOf(from), p.indexOf(to)
	if i < 0 || j < 0 {
		return between
	}
	for k := i + 1; k < j; k++ {
		between = append(between, p.windows[k].Name)
	}
	return between
}

// Classify returns the window of a license that expires on expiration, as it is on the day today.
//
// The days left are the days from today to the expiration date. Past dates give negative numbers.
// The window is the most severe one whose bound is at or above the days left.
// OK has no bound, so it is the window when no other window fits.
// With the thresholds 10 and 5, a license with 7 days left is in EXPIRING_10, and one with 0 days left is EXPIRED.
func (p Policy) Classify(expiration, today civil.Date) Window {
	daysLeft := today.DaysUntil(expiration)
	for _, row := range slices.Backward(p.windows) {
		if row.reaches(daysLeft) {
			return row.Name
		}
	}
	return WindowOK
}

// moreSevere reports whether window a is more severe than window b. Both must be windows of the policy.
func (p Policy) moreSevere(a, b Window) bool {
	return p.indexOf(a) > p.indexOf(b)
}

// indexOf returns the position of the named window, which is its severity. It returns -1 for an unknown name.
func (p Policy) indexOf(name Window) int {
	return slices.IndexFunc(p.windows, func(r WindowRow) bool { return r.Name == name })
}

// isZero reports whether the policy is the zero Policy, which has no windows.
func (p Policy) isZero() bool {
	return len(p.windows) == 0
}
