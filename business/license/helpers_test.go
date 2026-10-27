package license_test

import (
	"testing"
	"time"
	_ "time/tzdata" // Embeds the time zone database, so the tests do not depend on the host.

	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/clock"
)

// The windows of the policy with the thresholds 90, 60, and 30. These are the current thresholds of the company.
const (
	expiring90 = license.Window("EXPIRING_90")
	expiring60 = license.Window("EXPIRING_60")
	expiring30 = license.Window("EXPIRING_30")
)

// currentThresholds are the thresholds of the current policy, in the order that the requirements list them.
var currentThresholds = []int{90, 60, 30}

// mustDate parses an ISO date, and stops the test if that fails.
func mustDate(t *testing.T, value string) civil.Date {
	t.Helper()
	d, err := civil.ParseISO(value)
	require.NoError(t, err)
	return d
}

// loadDenver loads the America/Denver time zone, and stops the test if that fails.
func loadDenver(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Denver")
	require.NoError(t, err)
	return loc
}

// noon returns 12:00 on the day in loc. No day in the tests has a clock change before noon.
func noon(day civil.Date, loc *time.Location) time.Time {
	return day.In(loc).Add(12 * time.Hour)
}

// newPolicy returns the policy for the thresholds, and stops the test if that fails.
func newPolicy(t *testing.T, thresholds ...int) license.Policy {
	t.Helper()
	policy, err := license.NewPolicy(thresholds)
	require.NoError(t, err)
	return policy
}

// newService returns a service with the current thresholds, and the simulated clock that it uses.
func newService(t *testing.T, now time.Time, loc *time.Location) (*license.Service, *clock.Simulated) {
	t.Helper()
	clk := clock.NewSimulated(now)
	svc := license.NewService(license.Config{Policy: newPolicy(t, currentThresholds...), Clock: clk, Location: loc})
	return svc, clk
}

// sampleDay is the day of the sample data tests. It is the day that the requirements use for the windows.
const sampleDay = "2027-03-01"

// newFixtureService returns a service that holds the sample data. The clock shows noon in Denver on sampleDay.
func newFixtureService(t *testing.T) (*license.Service, *clock.Simulated) {
	t.Helper()
	loc := loadDenver(t)
	svc, clk := newService(t, noon(mustDate(t, sampleDay), loc), loc)

	result, err := svc.Load(t.Context(), fixture(t))
	require.NoError(t, err)
	require.Empty(t, result.Rejected)
	require.Empty(t, result.Warnings)
	return svc, clk
}

// newRecord returns a record. The arguments are in the order of the columns of input.csv.
func newRecord(t *testing.T, npi, first, last, number, state, expiration string) license.Record {
	t.Helper()
	return license.Record{
		NPI:               npi,
		FirstName:         first,
		LastName:          last,
		LicenseNumber:     number,
		LicenseState:      state,
		LicenseExpiration: mustDate(t, expiration),
	}
}

// fixture returns the 16 records of input.csv, in the same order and with the same columns.
func fixture(t *testing.T) []license.Record {
	t.Helper()
	return []license.Record{
		newRecord(t, "1487203355", "Adaeze", "Okonkwo", "NY-118402", "NY", "2027-05-31"),
		newRecord(t, "1592038847", "Rowan", "Whitfield", "NY-118455", "NY", "2027-05-30"),
		newRecord(t, "1730915562", "Priya", "Ramanathan", "IL-447120", "IL", "2027-05-29"),
		newRecord(t, "1846620391", "Devon", "Achterberg", "PA-330871", "PA", "2027-04-30"),
		newRecord(t, "1955130284", "Mercedes", "Villalobos", "TX-902244", "TX", "2027-04-29"),
		newRecord(t, "1204487719", "Tobias", "Lindqvist", "OH-551903", "OH", "2027-03-31"),
		newRecord(t, "1318802446", "Nia", "Carrington", "GA-664012", "GA", "2027-03-30"),
		newRecord(t, "1427719035", "Emeka", "Balogun", "MI-773218", "MI", "2027-03-02"),
		newRecord(t, "1536604128", "Saoirse", "Mulvaney", "MA-880134", "MA", "2027-03-01"),
		newRecord(t, "1645593317", "Hiroshi", "Takenaka", "WA-991027", "WA", "2027-02-28"),
		newRecord(t, "1754482206", "Camille", "Dubois", "NY-118903", "NY", "2027-03-20"),
		newRecord(t, "1754482206", "Camille", "Dubois", "NJ-207744", "NJ", "2027-11-01"),
		newRecord(t, "1863371195", "Yusuf", "Demirkan", "FL-445019", "FL", "2026-12-01"),
		newRecord(t, "1972260084", "Anneliese", "Brandtner", "CO-118277", "CO", "2028-02-29"),
		newRecord(t, "1081159973", "Marcus", "Oyelaran", "NY-119544", "NY", "2027-06-15"),
		newRecord(t, "1190048862", "Sunita", "Bhattarai", "AZ-773905", "AZ", "2028-01-01"),
	}
}
