package civil_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"testing"
	"time"
	_ "time/tzdata" // Embeds the time zone database, so the tests do not depend on the host.

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/foundation/civil"
)

// mustParse parses a date, and stops the test if that fails.
func mustParse(t *testing.T, layout, value string) civil.Date {
	t.Helper()
	d, err := civil.Parse(layout, value)
	require.NoError(t, err)
	return d
}

// mustISO parses an ISO date, and stops the test if that fails.
func mustISO(t *testing.T, value string) civil.Date {
	t.Helper()
	return mustParse(t, civil.LayoutISO, value)
}

// loadLocation loads a time zone, and stops the test if that fails.
func loadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

// assertSameDate compares the text first, because it gives a readable failure message.
// Then it compares the values, because Date promises that == works.
func assertSameDate(t *testing.T, want, got civil.Date) {
	t.Helper()
	assert.Equal(t, want.String(), got.String())
	assert.Equal(t, want, got)
}

func TestParseISO(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    civil.Date
		wantErr bool
	}{
		{name: "valid", value: "2027-05-30", want: civil.New(2027, time.May, 30)},
		{name: "first day of the year", value: "2027-01-01", want: civil.New(2027, time.January, 1)},
		{name: "last day of the year", value: "2027-12-31", want: civil.New(2027, time.December, 31)},
		{name: "leap day", value: "2028-02-29", want: civil.New(2028, time.February, 29)},
		{name: "zero date", value: "0001-01-01", want: civil.Date{}},
		{name: "day out of range", value: "2027-02-30", wantErr: true},
		{name: "leap day in a common year", value: "2027-02-29", wantErr: true},
		{name: "month out of range", value: "2027-13-01", wantErr: true},
		{name: "month zero", value: "2027-00-10", wantErr: true},
		{name: "day zero", value: "2027-05-00", wantErr: true},
		{name: "no zero padding", value: "2027-2-3", wantErr: true},
		{name: "empty", value: "", wantErr: true},
		{name: "blank", value: " ", wantErr: true},
		{name: "surrounding spaces", value: " 2027-05-30 ", wantErr: true},
		{name: "time of day", value: "2027-05-30T10:00:00Z", wantErr: true},
		{name: "US order", value: "05/30/2027", wantErr: true},
		{name: "two-digit year", value: "27-05-30", wantErr: true},
		{name: "text", value: "not a date", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := civil.ParseISO(tc.value)

			if tc.wantErr {
				require.ErrorContains(t, err, "parse date")
				_, ok := errors.AsType[*time.ParseError](err)
				assert.True(t, ok, "the error wraps the *time.ParseError")
				assert.True(t, got.IsZero(), "a failed parse returns the zero date")
				return
			}
			require.NoError(t, err)
			assertSameDate(t, tc.want, got)
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		layout  string
		value   string
		want    civil.Date
		wantErr bool
	}{
		{name: "US", layout: "1/2/2006", value: "3/1/2027", want: civil.New(2027, time.March, 1)},
		{name: "US zero padded", layout: "1/2/2006", value: "03/01/2027", want: civil.New(2027, time.March, 1)},
		{name: "US last day of the year", layout: "1/2/2006", value: "12/31/2027", want: civil.New(2027, time.December, 31)},
		{name: "US leap day", layout: "1/2/2006", value: "2/29/2028", want: civil.New(2028, time.February, 29)},
		{name: "US leap day in a common year", layout: "1/2/2006", value: "2/29/2027", wantErr: true},
		{name: "US month out of range", layout: "1/2/2006", value: "13/1/2027", wantErr: true},
		{name: "US day out of range", layout: "1/2/2006", value: "2/30/2027", wantErr: true},
		{name: "year first with slashes", layout: "2006/1/2", value: "2027/3/1", want: civil.New(2027, time.March, 1)},
		{name: "day first layout", layout: "2/1/2006", value: "31/12/2027", want: civil.New(2027, time.December, 31)},
		{name: "month name", layout: "Jan 2, 2006", value: "Mar 1, 2027", want: civil.New(2027, time.March, 1)},
		{name: "layout does not match", layout: "1/2/2006", value: "2027-03-01", wantErr: true},
		{name: "empty", layout: "1/2/2006", value: "", wantErr: true},
		// The date is the one that is written in the value, not the date in UTC.
		{name: "offset behind UTC", layout: time.RFC3339, value: "2027-05-30T23:30:00-06:00", want: civil.New(2027, time.May, 30)},
		{name: "offset ahead of UTC", layout: time.RFC3339, value: "2027-05-30T01:00:00+14:00", want: civil.New(2027, time.May, 30)},
		{name: "UTC", layout: time.RFC3339, value: "2027-05-30T23:30:00Z", want: civil.New(2027, time.May, 30)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := civil.Parse(tc.layout, tc.value)

			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, got.IsZero(), "a failed parse returns the zero date")
				return
			}
			require.NoError(t, err)
			assertSameDate(t, tc.want, got)
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		year  int
		month time.Month
		day   int
		want  string
	}{
		{name: "in range", year: 2027, month: time.May, day: 30, want: "2027-05-30"},
		{name: "leap day", year: 2028, month: time.February, day: 29, want: "2028-02-29"},
		{name: "day carries over", year: 2027, month: time.February, day: 30, want: "2027-03-02"},
		{name: "leap day in a common year carries over", year: 2027, month: time.February, day: 29, want: "2027-03-01"},
		{name: "day zero is the last day of the month before", year: 2027, month: time.March, day: 0, want: "2027-02-28"},
		{name: "negative day", year: 2027, month: time.March, day: -1, want: "2027-02-27"},
		{name: "month carries over", year: 2027, month: time.Month(14), day: 1, want: "2028-02-01"},
		{name: "month zero is December of the year before", year: 2027, month: time.Month(0), day: 1, want: "2026-12-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := civil.New(tc.year, tc.month, tc.day)

			assert.Equal(t, tc.want, got.String())
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		date civil.Date
		want string
	}{
		{name: "zero date", date: civil.Date{}, want: "0001-01-01"},
		{name: "zero padded", date: civil.New(2027, time.May, 5), want: "2027-05-05"},
		{name: "two-digit month and day", date: civil.New(2027, time.December, 31), want: "2027-12-31"},
		{name: "year zero", date: civil.New(0, time.January, 1), want: "0000-01-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.date.String())
			assert.Equal(t, tc.want, fmt.Sprint(tc.date), "Date is a fmt.Stringer")
		})
	}
}

func TestAddDays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		start string
		days  int
		want  string
	}{
		{name: "zero days", start: "2027-03-01", days: 0, want: "2027-03-01"},
		{name: "one day", start: "2027-03-01", days: 1, want: "2027-03-02"},
		{name: "month end", start: "2027-01-31", days: 1, want: "2027-02-01"},
		{name: "month end of a 30-day month", start: "2027-04-30", days: 1, want: "2027-05-01"},
		{name: "year end", start: "2027-12-31", days: 1, want: "2028-01-01"},
		{name: "to the leap day", start: "2028-02-28", days: 1, want: "2028-02-29"},
		{name: "past the leap day", start: "2028-02-29", days: 1, want: "2028-03-01"},
		{name: "no leap day in a common year", start: "2027-02-28", days: 1, want: "2027-03-01"},
		{name: "back over a month start", start: "2027-03-01", days: -1, want: "2027-02-28"},
		{name: "back over the leap day", start: "2028-03-01", days: -1, want: "2028-02-29"},
		{name: "back over a year start", start: "2028-01-01", days: -1, want: "2027-12-31"},
		{name: "90 days", start: "2027-03-01", days: 90, want: "2027-05-30"},
		{name: "365 days over a leap day", start: "2027-03-01", days: 365, want: "2028-02-29"},
		{name: "366 days over a leap day", start: "2027-03-01", days: 366, want: "2028-03-01"},
		{name: "100 years, with 2100 not a leap year", start: "2027-03-01", days: 36524, want: "2127-03-01"},
		{name: "from the zero date", start: "0001-01-01", days: 1, want: "0001-01-02"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start := mustISO(t, tc.start)

			got := start.AddDays(tc.days)

			assertSameDate(t, mustISO(t, tc.want), got)
		})
	}
}

func TestDaysUntil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		from  string
		later string
		want  int
	}{
		{name: "91 days", from: "2027-03-01", later: "2027-05-31", want: 91},
		{name: "90 days", from: "2027-03-01", later: "2027-05-30", want: 90},
		{name: "60 days", from: "2027-03-01", later: "2027-04-30", want: 60},
		{name: "30 days", from: "2027-03-01", later: "2027-03-31", want: 30},
		{name: "1 day", from: "2027-03-01", later: "2027-03-02", want: 1},
		{name: "same day", from: "2027-03-01", later: "2027-03-01", want: 0},
		{name: "1 day ago", from: "2027-03-01", later: "2027-02-28", want: -1},
		{name: "over a leap day", from: "2028-02-28", later: "2028-03-01", want: 2},
		{name: "no leap day in a common year", from: "2027-02-28", later: "2027-03-01", want: 1},
		{name: "a year that has a leap day", from: "2027-03-01", later: "2028-03-01", want: 366},
		{name: "a year that has no leap day", from: "2026-03-01", later: "2027-03-01", want: 365},
		{name: "over a year end", from: "2027-12-31", later: "2028-01-01", want: 1},
		// time.Duration saturates after about 292 years, so these need the exact day count.
		{name: "from the zero date", from: "0001-01-01", later: "2027-03-01", want: 740040},
		{name: "to the zero date", from: "2027-03-01", later: "0001-01-01", want: -740040},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			from, later := mustISO(t, tc.from), mustISO(t, tc.later)

			assert.Equal(t, tc.want, from.DaysUntil(later))
			assert.Equal(t, -tc.want, later.DaysUntil(from), "the reverse count is the negative")
		})
	}
}

func TestDaysUntilInvertsAddDays(t *testing.T) {
	t.Parallel()

	for _, start := range []string{"0001-01-01", "2026-12-31", "2027-03-01", "2028-02-29", "9999-12-31"} {
		t.Run(start, func(t *testing.T) {
			t.Parallel()
			from := mustISO(t, start)

			for n := -800; n <= 800; n++ {
				assert.Equal(t, n, from.DaysUntil(from.AddDays(n)), "n=%d", n)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want int
	}{
		{name: "equal", a: "2027-03-01", b: "2027-03-01", want: 0},
		{name: "earlier day", a: "2027-03-01", b: "2027-03-02", want: -1},
		{name: "later day", a: "2027-03-02", b: "2027-03-01", want: 1},
		{name: "earlier month", a: "2027-02-28", b: "2027-03-01", want: -1},
		{name: "earlier year", a: "2026-12-31", b: "2027-01-01", want: -1},
		{name: "zero date is the earliest", a: "0001-01-01", b: "2027-03-01", want: -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b := mustISO(t, tc.a), mustISO(t, tc.b)

			assert.Equal(t, tc.want, a.Compare(b))
			assert.Equal(t, -tc.want, b.Compare(a))
			assert.Equal(t, tc.want < 0, a.Before(b))
			assert.Equal(t, tc.want > 0, a.After(b))
			assert.Equal(t, tc.want > 0, b.Before(a))
			assert.Equal(t, tc.want < 0, b.After(a))
		})
	}
}

func TestIsZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		date civil.Date
		want bool
	}{
		{name: "zero value", date: civil.Date{}, want: true},
		{name: "first day of year 1", date: civil.New(1, time.January, 1), want: true},
		{name: "parsed first day of year 1", date: mustISO(t, "0001-01-01"), want: true},
		{name: "second day of year 1", date: civil.New(1, time.January, 2), want: false},
		{name: "year zero", date: civil.New(0, time.December, 31), want: false},
		{name: "Unix epoch", date: civil.New(1970, time.January, 1), want: false},
		{name: "ordinary date", date: civil.New(2027, time.March, 1), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.date.IsZero())
		})
	}
}

func TestDateOf(t *testing.T) {
	t.Parallel()

	denver := loadLocation(t, "America/Denver")
	tokyo := loadLocation(t, "Asia/Tokyo")
	instant := time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC)

	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "UTC midnight", at: time.Date(2027, time.May, 30, 0, 0, 0, 0, time.UTC), want: "2027-05-30"},
		{name: "UTC last nanosecond", at: time.Date(2027, time.May, 30, 23, 59, 59, 999_999_999, time.UTC), want: "2027-05-30"},
		{name: "the same instant in UTC", at: instant, want: "2026-11-01"},
		{name: "the same instant in Denver is the day before", at: instant.In(denver), want: "2026-10-31"},
		{name: "the same instant in Tokyo", at: instant.In(tokyo), want: "2026-11-01"},
		{name: "Tokyo early morning is the day before in UTC", at: time.Date(2027, time.May, 30, 1, 0, 0, 0, tokyo), want: "2027-05-30"},
		{name: "Denver midnight", at: time.Date(2027, time.May, 30, 0, 0, 0, 0, denver), want: "2027-05-30"},
		{name: "Denver last nanosecond", at: time.Date(2027, time.May, 30, 23, 59, 59, 999_999_999, denver), want: "2027-05-30"},
		// The clocks in Denver go forward on 2027-03-14 and go back on 2026-11-01.
		{name: "clocks go forward, before the change", at: time.Date(2027, time.March, 14, 1, 59, 59, 0, denver), want: "2027-03-14"},
		{name: "clocks go forward, after the change", at: time.Date(2027, time.March, 14, 3, 0, 0, 0, denver), want: "2027-03-14"},
		{name: "clocks go forward, end of the day", at: time.Date(2027, time.March, 14, 23, 59, 59, 0, denver), want: "2027-03-14"},
		{name: "clocks go back, first 01:30", at: time.Date(2026, time.November, 1, 7, 30, 0, 0, time.UTC).In(denver), want: "2026-11-01"},
		{name: "clocks go back, second 01:30", at: time.Date(2026, time.November, 1, 8, 30, 0, 0, time.UTC).In(denver), want: "2026-11-01"},
		{name: "clocks go back, end of the day", at: time.Date(2026, time.November, 1, 23, 59, 59, 0, denver), want: "2026-11-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := civil.DateOf(tc.at)

			assertSameDate(t, mustISO(t, tc.want), got)
		})
	}
}

func TestIn(t *testing.T) {
	t.Parallel()

	denver := loadLocation(t, "America/Denver")
	tokyo := loadLocation(t, "Asia/Tokyo")
	saoPaulo := loadLocation(t, "America/Sao_Paulo")
	havana := loadLocation(t, "America/Havana")
	beirut := loadLocation(t, "Asia/Beirut")
	offset := time.FixedZone("UTC+5:30", 5*60*60+30*60)

	tests := []struct {
		name string
		date string
		loc  *time.Location
		want string // RFC 3339.
	}{
		{name: "UTC", date: "2027-05-30", loc: time.UTC, want: "2027-05-30T00:00:00Z"},
		{name: "fixed offset", date: "2027-05-30", loc: offset, want: "2027-05-30T00:00:00+05:30"},
		{name: "Tokyo", date: "2027-05-30", loc: tokyo, want: "2027-05-30T00:00:00+09:00"},
		{name: "Denver in summer", date: "2027-07-01", loc: denver, want: "2027-07-01T00:00:00-06:00"},
		{name: "Denver in winter", date: "2027-01-15", loc: denver, want: "2027-01-15T00:00:00-07:00"},
		{name: "Denver, clocks go forward", date: "2027-03-14", loc: denver, want: "2027-03-14T00:00:00-07:00"},
		{name: "Denver, clocks go back", date: "2026-11-01", loc: denver, want: "2026-11-01T00:00:00-06:00"},
		// In these zones the clocks go forward over midnight, so midnight does not exist on that day.
		// Alone, time.Date returns the previous day in some of them.
		{name: "Sao Paulo, midnight skipped", date: "2018-11-04", loc: saoPaulo, want: "2018-11-04T01:00:00-02:00"},
		{name: "Sao Paulo, day before", date: "2018-11-03", loc: saoPaulo, want: "2018-11-03T00:00:00-03:00"},
		{name: "Sao Paulo, day after", date: "2018-11-05", loc: saoPaulo, want: "2018-11-05T00:00:00-02:00"},
		{name: "Havana, midnight skipped", date: "2026-03-08", loc: havana, want: "2026-03-08T01:00:00-04:00"},
		{name: "Beirut, midnight skipped", date: "2023-03-26", loc: beirut, want: "2023-03-26T01:00:00+03:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			date := mustISO(t, tc.date)

			got := date.In(tc.loc)

			assert.Equal(t, tc.want, got.Format(time.RFC3339))
			assert.Same(t, tc.loc, got.Location())
			assertSameDate(t, date, civil.DateOf(got))
		})
	}

	t.Run("nil location", func(t *testing.T) {
		t.Parallel()

		assert.Panics(t, func() { civil.New(2027, time.May, 30).In(nil) })
	})
}

func TestMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		date civil.Date
		want string
	}{
		{name: "ordinary date", date: civil.New(2027, time.May, 30), want: "2027-05-30"},
		{name: "leap day", date: civil.New(2028, time.February, 29), want: "2028-02-29"},
		{name: "zero date", date: civil.Date{}, want: "0001-01-01"},
		{name: "year zero", date: civil.New(0, time.January, 1), want: "0000-01-01"},
		{name: "last four-digit year", date: civil.New(9999, time.December, 31), want: "9999-12-31"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := tc.date.MarshalText()

			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

type licenseJSON struct {
	Expiration civil.Date `json:"license_expiration"`
}

type optionalJSON struct {
	Expiration civil.Date `json:"license_expiration,omitzero"`
}

func TestJSON(t *testing.T) {
	t.Parallel()

	t.Run("field", func(t *testing.T) {
		t.Parallel()

		data, err := json.Marshal(licenseJSON{Expiration: civil.New(2027, time.May, 30)})

		require.NoError(t, err)
		assert.JSONEq(t, `{"license_expiration":"2027-05-30"}`, string(data))
	})

	t.Run("slice with a leap day", func(t *testing.T) {
		t.Parallel()

		data, err := json.Marshal([]civil.Date{civil.New(2028, time.February, 29), civil.New(2027, time.March, 1)})

		require.NoError(t, err)
		assert.Equal(t, `["2028-02-29","2027-03-01"]`, string(data))
	})

	t.Run("omitzero omits the zero date", func(t *testing.T) {
		t.Parallel()

		zero, err := json.Marshal(optionalJSON{})
		require.NoError(t, err)
		assert.Equal(t, `{}`, string(zero))

		set, err := json.Marshal(optionalJSON{Expiration: civil.New(2027, time.May, 30)})
		require.NoError(t, err)
		assert.JSONEq(t, `{"license_expiration":"2027-05-30"}`, string(set))
	})
}

func TestDateIsComparable(t *testing.T) {
	t.Parallel()

	denver := loadLocation(t, "America/Denver")
	want := civil.New(2027, time.March, 1)
	// Every way to make the same date gives a value that == treats as equal.
	same := map[string]civil.Date{
		"New":                  civil.New(2027, time.March, 1),
		"New with carry":       civil.New(2027, time.February, 29),
		"ParseISO":             mustISO(t, "2027-03-01"),
		"Parse":                mustParse(t, "1/2/2006", "3/1/2027"),
		"Parse with an offset": mustParse(t, time.RFC3339, "2027-03-01T23:00:00-07:00"),
		"DateOf in UTC":        civil.DateOf(time.Date(2027, time.March, 1, 15, 0, 0, 0, time.UTC)),
		"DateOf in Denver":     civil.DateOf(time.Date(2027, time.March, 1, 15, 0, 0, 0, denver)),
		"AddDays":              civil.New(2027, time.February, 28).AddDays(1),
		"AddDays back":         civil.New(2027, time.March, 2).AddDays(-1),
	}
	for name, got := range same {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertSameDate(t, want, got)
		})
	}

	t.Run("map key", func(t *testing.T) {
		t.Parallel()
		counts := make(map[civil.Date]int)

		for _, d := range same {
			counts[d]++
		}

		assert.Equal(t, map[civil.Date]int{want: len(same)}, counts)
	})
}
