package license_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/errs"
)

// indexes returns the record positions of the issues.
func indexes(issues []license.RecordIssue) []int {
	positions := []int{}
	for _, issue := range issues {
		positions = append(positions, issue.Index)
	}
	return positions
}

func TestLoadSampleData(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)
	asOf := mustDate(t, "2027-03-01")

	result, err := svc.Load(ctx, fixture(t))

	require.NoError(t, err)
	assert.Equal(t, 15, result.ProvidersAdded, "Camille Dubois has two licenses and one NPI")
	assert.Equal(t, 16, result.LicensesAdded)
	assert.Zero(t, result.Duplicates)
	assert.Empty(t, result.Rejected)
	assert.Empty(t, result.Warnings)

	// The two licenses of Camille Dubois have their own expiration dates and windows.
	ny, err := svc.License(ctx, "NY", "NY-118903", asOf)
	require.NoError(t, err)
	assert.Equal(t, license.License{
		NPI:               "1754482206",
		LicenseNumber:     "NY-118903",
		LicenseState:      "NY",
		LicenseExpiration: mustDate(t, "2027-03-20"),
		Window:            expiring30,
	}, ny)

	nj, err := svc.License(ctx, "NJ", "NJ-207744", asOf)
	require.NoError(t, err)
	assert.Equal(t, license.License{
		NPI:               "1754482206",
		LicenseNumber:     "NJ-207744",
		LicenseState:      "NJ",
		LicenseExpiration: mustDate(t, "2027-11-01"),
		Window:            license.WindowOK,
	}, nj)
}

func TestLoadRules(t *testing.T) {
	t.Parallel()

	// Each case loads base first, then batch. The results are the results of the second call.
	base := newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", "2027-06-01")

	tests := []struct {
		name           string
		batch          []license.Record
		wantProviders  int
		wantLicenses   int
		wantDuplicates int
		wantRejected   []int  // The record positions.
		wantWarnings   []int  // The record positions.
		wantWarning    string // The text of the only warning.
		wantExpiration string // The expiration date that NY-1 has after the load.
	}{
		{
			name:           "empty batch",
			batch:          nil,
			wantRejected:   []int{},
			wantWarnings:   []int{},
			wantExpiration: "2027-06-01",
		},
		{
			name:           "new provider with a new license",
			batch:          []license.Record{newRecord(t, "2222222222", "Grace", "Hopper", "NJ-2", "NJ", "2027-06-01")},
			wantProviders:  1,
			wantLicenses:   1,
			wantRejected:   []int{},
			wantWarnings:   []int{},
			wantExpiration: "2027-06-01",
		},
		{
			name:           "known provider with a second license",
			batch:          []license.Record{newRecord(t, "1111111111", "Ada", "Lovelace", "NJ-2", "NJ", "2027-06-01")},
			wantLicenses:   1,
			wantRejected:   []int{},
			wantWarnings:   []int{},
			wantExpiration: "2027-06-01",
		},
		{
			name:           "same license with the same expiration date",
			batch:          []license.Record{base},
			wantDuplicates: 1,
			wantRejected:   []int{},
			wantWarnings:   []int{},
			wantExpiration: "2027-06-01",
		},
		{
			name:           "license of another provider",
			batch:          []license.Record{newRecord(t, "2222222222", "Grace", "Hopper", "NY-1", "NY", "2027-06-01")},
			wantRejected:   []int{0},
			wantWarnings:   []int{},
			wantExpiration: "2027-06-01",
		},
		{
			name:           "earlier expiration date wins",
			batch:          []license.Record{newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", "2027-05-01")},
			wantDuplicates: 1,
			wantRejected:   []int{},
			wantWarnings:   []int{0},
			wantWarning:    `license "NY-1" in state "NY": the earlier expiration date 2027-05-01 replaces 2027-06-01`,
			wantExpiration: "2027-05-01",
		},
		{
			name:           "later expiration date is ignored",
			batch:          []license.Record{newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", "2027-07-01")},
			wantDuplicates: 1,
			wantRejected:   []int{},
			wantWarnings:   []int{0},
			wantWarning:    `license "NY-1" in state "NY": the later expiration date 2027-07-01 is ignored, and the license keeps 2027-06-01`,
			wantExpiration: "2027-06-01",
		},
		{
			name:           "known provider with another name",
			batch:          []license.Record{newRecord(t, "1111111111", "Grace", "Hopper", "NJ-2", "NJ", "2027-06-01")},
			wantLicenses:   1,
			wantRejected:   []int{},
			wantWarnings:   []int{0},
			wantWarning:    "provider 1111111111 is Ada Lovelace, but this record says Grace Hopper; the first name stays",
			wantExpiration: "2027-06-01",
		},
		{
			name: "a rejected record adds no provider",
			batch: []license.Record{
				newRecord(t, "3333333333", "Alan", "Turing", "NY-1", "NY", "2027-06-01"),
				newRecord(t, "3333333333", "Alan", "Turing", "NY-1", "NY", "2027-06-01"),
			},
			wantRejected:   []int{0, 1},
			wantWarnings:   []int{},
			wantExpiration: "2027-06-01",
		},
		{
			name: "records of all kinds keep their positions",
			batch: []license.Record{
				newRecord(t, "2222222222", "Grace", "Hopper", "NJ-2", "NJ", "2027-06-01"), // New.
				base, // Duplicate.
				newRecord(t, "2222222222", "Grace", "Hopper", "NY-1", "NY", "2027-06-01"), // Rejected.
				newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", "2027-05-01"), // Duplicate with a warning.
			},
			wantProviders:  1,
			wantLicenses:   1,
			wantDuplicates: 2,
			wantRejected:   []int{2},
			wantWarnings:   []int{3},
			wantExpiration: "2027-05-01",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			loc := loadDenver(t)
			svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)
			_, err := svc.Load(ctx, []license.Record{base})
			require.NoError(t, err)

			result, err := svc.Load(ctx, tc.batch)

			require.NoError(t, err)
			assert.Equal(t, tc.wantProviders, result.ProvidersAdded)
			assert.Equal(t, tc.wantLicenses, result.LicensesAdded)
			assert.Equal(t, tc.wantDuplicates, result.Duplicates)
			assert.Equal(t, len(tc.batch), result.LicensesAdded+result.Duplicates+len(result.Rejected), "each record is counted once")
			assert.Equal(t, tc.wantRejected, indexes(result.Rejected))
			assert.Equal(t, tc.wantWarnings, indexes(result.Warnings))
			for _, issue := range result.Rejected {
				require.ErrorIs(t, issue.Err, errs.ErrConflict)
			}
			if tc.wantWarning != "" {
				require.Len(t, result.Warnings, 1)
				require.EqualError(t, result.Warnings[0].Err, tc.wantWarning)
			}

			// The first owner keeps the license, and the earlier expiration date wins.
			got, err := svc.License(ctx, "NY", "NY-1", mustDate(t, "2027-03-01"))
			require.NoError(t, err)
			assert.Equal(t, base.NPI, got.NPI)
			assert.Equal(t, mustDate(t, tc.wantExpiration), got.LicenseExpiration)
		})
	}
}

func TestLoadRejectionText(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)
	records := fixture(t)
	conflict := records[0]
	conflict.NPI = "9999999999"

	result, err := svc.Load(t.Context(), append(records, conflict))

	require.NoError(t, err)
	require.Len(t, result.Rejected, 1)
	assert.Equal(t, len(records), result.Rejected[0].Index)
	require.ErrorIs(t, result.Rejected[0].Err, errs.ErrConflict)
	assert.EqualError(t, result.Rejected[0].Err,
		`license "NY-118402" in state "NY" belongs to provider 1487203355, not to provider 9999999999: conflict`)
}

func TestLoadKeepsTheFirstName(t *testing.T) {
	t.Parallel()
	loc := loadDenver(t)
	svc, _ := newService(t, noon(mustDate(t, "2027-03-01"), loc), loc)

	// The second and the third record disagree with the first, and the third also disagrees with the second.
	// Both warnings name the first name, so the second name did not replace it.
	result, err := svc.Load(t.Context(), []license.Record{
		newRecord(t, "1111111111", "Ada", "Lovelace", "NY-1", "NY", "2027-06-01"),
		newRecord(t, "1111111111", "Grace", "Hopper", "NY-2", "NY", "2027-06-01"),
		newRecord(t, "1111111111", "Alan", "Turing", "NY-3", "NY", "2027-06-01"),
	})

	require.NoError(t, err)
	assert.Equal(t, 1, result.ProvidersAdded)
	assert.Equal(t, 3, result.LicensesAdded)
	require.Len(t, result.Warnings, 2)
	require.EqualError(t, result.Warnings[0].Err, "provider 1111111111 is Ada Lovelace, but this record says Grace Hopper; the first name stays")
	assert.EqualError(t, result.Warnings[1].Err, "provider 1111111111 is Ada Lovelace, but this record says Alan Turing; the first name stays")
}

func TestLoadTwiceChangesNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	svc, _ := newFixtureService(t)
	asOf := mustDate(t, sampleDay)
	before, err := svc.WindowGroups(ctx, asOf)
	require.NoError(t, err)

	result, err := svc.Load(ctx, fixture(t))

	require.NoError(t, err)
	assert.Zero(t, result.ProvidersAdded)
	assert.Zero(t, result.LicensesAdded)
	assert.Equal(t, 16, result.Duplicates)
	assert.Empty(t, result.Rejected)
	assert.Empty(t, result.Warnings, "a repeated load is quiet")
	after, err := svc.WindowGroups(ctx, asOf)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
