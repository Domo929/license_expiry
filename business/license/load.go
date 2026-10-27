package license

import (
	"context"
	"fmt"

	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/errs"
)

// Record is one row of input: a provider and one license of the provider.
// The caller has checked the format of the fields. Load checks what they mean.
type Record struct {
	NPI               string
	FirstName         string
	LastName          string
	LicenseNumber     string
	LicenseState      string
	LicenseExpiration civil.Date
}

// RecordIssue is a problem with one record of a Load call.
type RecordIssue struct {
	// Index is the position of the record in the slice that was given to Load.
	// The caller can map it to a line of the input file.
	Index int

	// Err says what is wrong. For a rejected record, it wraps one of the error kinds in the errs package.
	Err error
}

// LoadResult reports what a Load call did.
// Each record is a new license, a duplicate, or rejected.
// So LicensesAdded, Duplicates, and the length of Rejected add up to the number of records.
type LoadResult struct {
	// ProvidersAdded is the number of NPIs that were new.
	ProvidersAdded int

	// LicensesAdded is the number of licenses that were new.
	LicensesAdded int

	// Duplicates is the number of records for a license that the provider already held.
	// A duplicate adds nothing. It can only move the expiration date of the license to an earlier day.
	Duplicates int

	// Rejected has the records that Load did not use. Each Err wraps errs.ErrConflict.
	Rejected []RecordIssue

	// Warnings has the records that disagree with the stored data, but that Load accepted.
	// The text of Err says which value Load kept: the name, or the expiration date.
	Warnings []RecordIssue
}

func (r *LoadResult) warn(index int, format string, args ...any) {
	r.Warnings = append(r.Warnings, RecordIssue{Index: index, Err: fmt.Errorf(format, args...)})
}

// Load adds the records to the store, in order. New licenses get the next ids in the order of the records.
// A second call adds to the data of the first call.
//
// A record adds its provider if the NPI is new, and its license if the state and number are new.
// A provider keeps the name of its first record. A later record with a different name gets a warning.
//
// A license belongs to the provider of its first record.
// A record that gives the license to a different NPI is rejected, and its error wraps errs.ErrConflict.
// A rejected record adds nothing, not even its provider.
//
// A record for a license that the provider already holds is a duplicate. It adds nothing.
// A duplicate with the same expiration date gets no warning.
// So a load of the same data twice changes nothing, and it does not fill the log.
// A duplicate with a different date keeps the earlier date and gets a warning.
// An earlier date makes the license warn sooner, so the choice is the safe one.
//
// Load holds the write lock, so a reader sees all the records or none of them.
// Load returns an error only when the context ends. The problems with records are in the result.
func (s *Service) Load(ctx context.Context, records []Record) (LoadResult, error) {
	if err := ctx.Err(); err != nil {
		return LoadResult{}, fmt.Errorf("load: %w", err)
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	result := LoadResult{Rejected: []RecordIssue{}, Warnings: []RecordIssue{}}
	for i, record := range records {
		s.loadRecord(i, record, &result)
	}
	return result, nil
}

// loadRecord applies one record. The caller holds the write lock.
func (s *Service) loadRecord(index int, record Record, result *LoadResult) {
	st := s.store

	// Check the license first. Then a rejected record leaves no provider behind.
	existing, found := st.findLicense(record.LicenseState, record.LicenseNumber)
	if found && existing.npi != record.NPI {
		err := fmt.Errorf("license %q in state %q belongs to provider %s, not to provider %s: %w",
			record.LicenseNumber, record.LicenseState, existing.npi, record.NPI, errs.ErrConflict)
		result.Rejected = append(result.Rejected, RecordIssue{Index: index, Err: err})
		return
	}

	known, ok := st.providers[record.NPI]
	switch {
	case !ok:
		st.providers[record.NPI] = provider{npi: record.NPI, firstName: record.FirstName, lastName: record.LastName}
		result.ProvidersAdded++
	case known.firstName != record.FirstName || known.lastName != record.LastName:
		// The text names only the NPI, because the caller logs it, and logs must not hold names.
		result.warn(index, "provider %s has a different name in this record; the name of the first record stays", record.NPI)
	}

	if !found {
		st.insertLicense(record.NPI, record.LicenseState, record.LicenseNumber, record.LicenseExpiration)
		result.LicensesAdded++
		return
	}

	result.Duplicates++
	switch {
	case record.LicenseExpiration.Before(existing.expiration):
		st.setExpiration(existing.id, record.LicenseExpiration)
		result.warn(index, "license %q in state %q: the earlier expiration date %s replaces %s",
			record.LicenseNumber, record.LicenseState, record.LicenseExpiration, existing.expiration)
	case record.LicenseExpiration.After(existing.expiration):
		result.warn(index, "license %q in state %q: the later expiration date %s is ignored, and the license keeps %s",
			record.LicenseNumber, record.LicenseState, record.LicenseExpiration, existing.expiration)
	}
}
