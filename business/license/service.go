package license

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/errs"
)

// Clock tells the current time. The clocks in the clock package fit it.
type Clock interface {
	Now() time.Time
}

// Config holds what NewService needs. Each field is required.
type Config struct {
	// Policy defines the windows. Make it with NewPolicy.
	Policy Policy

	// Clock tells the time. A simulated clock lets a test or a demo move time.
	Clock Clock

	// Location is the time zone that decides what day it is. A reading of the clock becomes a date in this zone.
	Location *time.Location
}

// Service tracks licenses and their windows. It is safe for concurrent use.
type Service struct {
	clock Clock
	loc   *time.Location
	store *store
}

// NewService returns a service with an empty store. The windows table of the store comes from cfg.Policy.
//
// It panics if a field of cfg is missing. A missing field is a mistake in the wiring that shows at start-up.
// Without a policy, every license would be OK.
func NewService(cfg Config) *Service {
	if cfg.Policy.isZero() || cfg.Clock == nil || cfg.Location == nil {
		panic("license: NewService needs a policy from NewPolicy, a clock, and a location")
	}
	return &Service{clock: cfg.Clock, loc: cfg.Location, store: newStore(cfg.Policy)}
}

// Today returns the date of the clock, in the time zone of the service.
func (s *Service) Today() civil.Date {
	return s.dateOf(s.clock.Now())
}

// dateOf returns the date of the instant t in the time zone of the service.
func (s *Service) dateOf(t time.Time) civil.Date {
	return civil.DateOf(t.In(s.loc))
}

// License is a license and the window that it is in on one day.
// Its JSON form is the output format that the requirements prescribe.
type License struct {
	NPI               string     `json:"npi"`
	LicenseNumber     string     `json:"license_number"`
	LicenseState      string     `json:"license_state"`
	LicenseExpiration civil.Date `json:"license_expiration"`
	Window            Window     `json:"window"`
}

// License returns the license with the state and number, and the window that it is in on the day asOf.
// It wraps errs.ErrNotFound if there is no such license.
func (s *Service) License(ctx context.Context, state, number string, asOf civil.Date) (License, error) {
	if err := ctx.Err(); err != nil {
		return License{}, fmt.Errorf("license: %w", err)
	}

	s.store.mu.RLock()
	defer s.store.mu.RUnlock()

	row, ok := s.store.findLicense(state, number)
	if !ok {
		return License{}, licenseNotFound(state, number)
	}
	return row.inWindow(s.store.windows.Classify(row.expiration, asOf)), nil
}

// WindowGroup is a window and the licenses that are in it.
type WindowGroup struct {
	Window   Window
	Licenses []License
}

// WindowGroups returns each window of the policy, with the licenses that are in it on the day asOf.
// The groups are in severity order: OK first, and EXPIRED last.
// The licenses of a group are in the order of expiration date, then state, then number.
// A window with no licenses is in the result, with an empty slice.
func (s *Service) WindowGroups(ctx context.Context, asOf civil.Date) ([]WindowGroup, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("window groups: %w", err)
	}

	s.store.mu.RLock()
	defer s.store.mu.RUnlock()

	windows := s.store.windows
	byWindow := map[Window][]License{}
	for _, row := range s.store.licenses {
		license := row.inWindow(windows.Classify(row.expiration, asOf))
		byWindow[license.Window] = append(byWindow[license.Window], license)
	}

	rows := windows.Windows()
	groups := make([]WindowGroup, 0, len(rows))
	for _, row := range rows {
		licenses := byWindow[row.Name]
		if licenses == nil {
			licenses = []License{}
		}
		slices.SortFunc(licenses, compareLicenses)
		groups = append(groups, WindowGroup{Window: row.Name, Licenses: licenses})
	}
	return groups, nil
}

// compareLicenses puts licenses in the order of expiration date, then state, then number.
func compareLicenses(a, b License) int {
	return cmp.Or(
		a.LicenseExpiration.Compare(b.LicenseExpiration),
		strings.Compare(a.LicenseState, b.LicenseState),
		strings.Compare(a.LicenseNumber, b.LicenseNumber),
	)
}

// AuditEntry is a row of the audit table with its license. The row says that a scan warned about the license.
type AuditEntry struct {
	// ID is the primary key of the audit row. A higher ID means a later warning.
	ID int64

	NPI           string
	LicenseNumber string
	LicenseState  string

	// LicenseExpiration is the expiration date that the license has now. The audit row does not keep the date of the warning.
	LicenseExpiration civil.Date

	// Window is the window that the license entered.
	Window Window

	// HitAt is the time of the clock reading of the scan that wrote the row.
	HitAt time.Time
}

// Audit returns the audit entries of the license with the state and number, from the first to the last.
// If the state and the number are both empty, it returns the entries of all licenses.
// A license with no warnings gives an empty slice.
//
// It wraps errs.ErrNotFound if there is no such license.
// It wraps errs.ErrInvalid if only one of the state and the number is empty, because a license needs both.
func (s *Service) Audit(ctx context.Context, state, number string) ([]AuditEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("audit: %w", err)
	}
	if (state == "") != (number == "") {
		return nil, fmt.Errorf("audit: a license needs both a state and a number: %w", errs.ErrInvalid)
	}

	s.store.mu.RLock()
	defer s.store.mu.RUnlock()

	all := state == "" && number == ""
	var licenseID int64
	if !all {
		row, ok := s.store.findLicense(state, number)
		if !ok {
			return nil, licenseNotFound(state, number)
		}
		licenseID = row.id
	}

	entries := []AuditEntry{}
	for _, row := range s.store.audit {
		if all || row.licenseID == licenseID {
			entries = append(entries, s.store.entry(row))
		}
	}
	return entries, nil
}

// licenseNotFound returns the error for a license that does not exist.
func licenseNotFound(state, number string) error {
	return fmt.Errorf("license %q in state %q: %w", number, state, errs.ErrNotFound)
}
