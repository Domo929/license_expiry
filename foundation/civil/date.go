// Package civil provides calendar dates with no time of day and no time zone.
//
// A license expires on a day, not at an instant. A Date holds only the day,
// so a time zone or a daylight saving change cannot move it.
package civil

import (
	"fmt"
	"time"
)

// LayoutISO is the layout of an ISO 8601 calendar date, such as 2027-05-30.
const LayoutISO = "2006-01-02"

const secondsPerDay = 24 * 60 * 60

// Date is a calendar date.
//
// Inside, a Date is a time.Time at midnight UTC, so daylight saving time does not affect date math.
// The zero value is the zero date, 0001-01-01. IsZero reports it.
// Dates can be compared with ==, and used as map keys.
type Date struct{ t time.Time }

// New returns the date of year, month, and day.
// Values that are out of range carry over to the next unit, as in time.Date:
// New(2027, time.February, 30) is 2027-03-02.
func New(year int, month time.Month, day int) Date {
	return Date{t: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// Parse returns the date in value. The value must match layout, which uses the syntax of time.Parse.
// The result is the date as written in value, even if layout has a time zone.
func Parse(layout, value string) (Date, error) {
	t, err := time.Parse(layout, value)
	if err != nil {
		return Date{}, fmt.Errorf("parse date: %w", err)
	}
	return DateOf(t), nil
}

// ParseISO returns the date in value, which must match LayoutISO.
func ParseISO(value string) (Date, error) {
	return Parse(LayoutISO, value)
}

// DateOf returns the calendar date of t in the time zone of t.
// One instant can be on different dates in different time zones.
func DateOf(t time.Time) Date {
	year, month, day := t.Date()
	return New(year, month, day)
}

// String returns d in LayoutISO.
func (d Date) String() string {
	return d.t.Format(LayoutISO)
}

// MarshalText implements encoding.TextMarshaler, so JSON shows d in LayoutISO.
// Date has no UnmarshalText: callers parse input with ParseISO or Parse, which return an error to check.
func (d Date) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// AddDays returns the date n days after d. A negative n gives an earlier date.
func (d Date) AddDays(n int) Date {
	return Date{t: d.t.AddDate(0, 0, n)}
}

// DaysUntil returns the number of whole days from d to later.
// It is negative if later is before d.
func (d Date) DaysUntil(later Date) int {
	// Both dates are midnight UTC, so the difference is an exact number of days.
	// Unix seconds avoid time.Duration, which saturates after about 292 years.
	return int((later.t.Unix() - d.t.Unix()) / secondsPerDay)
}

// Compare returns -1 if d is before other, 0 if the dates are equal, and +1 if d is after other.
func (d Date) Compare(other Date) int {
	return d.t.Compare(other.t)
}

// Before reports whether d is before other.
func (d Date) Before(other Date) bool {
	return d.t.Before(other.t)
}

// After reports whether d is after other.
func (d Date) After(other Date) bool {
	return d.t.After(other.t)
}

// IsZero reports whether d is the zero date.
func (d Date) IsZero() bool {
	return d.t.IsZero()
}

// In returns the start of d in loc. That is midnight, or the first instant of the day
// when a clock change in loc skips midnight. It panics if loc is nil, as time.Date does.
func (d Date) In(loc *time.Location) time.Time {
	year, month, day := d.t.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, loc)
	if DateOf(start) == d {
		return start
	}
	// When the clocks go forward over midnight, time.Date can return a time on the previous day.
	// The day then starts when the time zone period of that time ends.
	if _, end := start.ZoneBounds(); !end.IsZero() && DateOf(end) == d {
		return end
	}
	return start
}
