package license

import (
	"sync"
	"time"

	"github.com/Domo929/license_expiry/foundation/civil"
)

// store holds the data of the service in memory. Its tables and indexes follow this Postgres schema:
//
//	CREATE TABLE providers (
//	    npi        char(10) PRIMARY KEY,
//	    first_name text     NOT NULL,
//	    last_name  text     NOT NULL
//	);
//
//	-- Built from the thresholds in the configuration. Nobody edits it.
//	CREATE TABLE windows (
//	    name          text PRIMARY KEY,     -- OK, EXPIRING_n, or EXPIRED
//	    max_days_left int  NULL,            -- NULL for OK, which has no bound
//	    severity      int  NOT NULL UNIQUE  -- 0 for OK, the highest value for EXPIRED
//	);
//
//	-- The window of a license is not a column. The code computes it from license_expiration, a day, and windows.
//	CREATE TABLE licenses (
//	    id                 bigint   PRIMARY KEY,
//	    npi                char(10) NOT NULL REFERENCES providers (npi),
//	    license_state      char(2)  NOT NULL,
//	    license_number     text     NOT NULL,
//	    license_expiration date     NOT NULL,
//	    UNIQUE (license_state, license_number)
//	);
//
//	-- One row says that a scan warned about the license when it entered the window.
//	CREATE TABLE license_window_audit (
//	    id          bigint      PRIMARY KEY,
//	    license_id  bigint      NOT NULL REFERENCES licenses (id),
//	    window_name text        NOT NULL REFERENCES windows (name),
//	    hit_at      timestamptz NOT NULL,
//	    UNIQUE (license_id, window_name)
//	);
//
// A scan needs the most severe window in the audit rows of each license.
// Postgres answers that with an index on the audit table. Here the mostSevere map is that index.
//
// One lock guards all the tables. A method of Service takes it once for a whole operation, as a transaction would.
// The methods of store expect the caller to hold the lock.
type store struct {
	mu sync.RWMutex // Guards every other field.

	// windows is the windows table. NewService fills it, and nothing changes it.
	windows Policy

	// providers is the providers table. The key is the npi.
	providers map[string]provider

	// licenses is the licenses table, in id order.
	// The ids start at 1 and nothing deletes a row, so the row with the id n is at position n-1.
	licenses []licenseRow

	// byKey is the unique index on (license_state, license_number). The value is the id of the license.
	byKey map[licenseKey]int64

	// audit is the audit table, in id order. The position of a row follows the same rule as in licenses.
	audit []auditRow

	// mostSevere is the index on the audit table. For a license id, it has the most severe window that has an audit row.
	// A license with no audit row has no key, and its most severe window is OK.
	mostSevere map[int64]Window
}

type provider struct {
	npi       string
	firstName string
	lastName  string
}

type licenseKey struct {
	state  string
	number string
}

type licenseRow struct {
	id         int64
	npi        string
	state      string
	number     string
	expiration civil.Date
}

type auditRow struct {
	id        int64
	licenseID int64
	window    Window
	hitAt     time.Time
}

// newStore returns an empty store. The windows table has the windows of the policy.
func newStore(windows Policy) *store {
	return &store{
		windows:    windows,
		providers:  map[string]provider{},
		byKey:      map[licenseKey]int64{},
		mostSevere: map[int64]Window{},
	}
}

// findLicense returns the license with the state and number. The second result is false if there is none.
func (s *store) findLicense(state, number string) (licenseRow, bool) {
	id, ok := s.byKey[licenseKey{state: state, number: number}]
	if !ok {
		return licenseRow{}, false
	}
	return s.licenses[id-1], true
}

// insertLicense adds a license, and gives it the next id.
func (s *store) insertLicense(npi, state, number string, expiration civil.Date) {
	row := licenseRow{id: int64(len(s.licenses)) + 1, npi: npi, state: state, number: number, expiration: expiration}
	s.licenses = append(s.licenses, row)
	s.byKey[licenseKey{state: state, number: number}] = row.id
}

// setExpiration changes the expiration date of the license with the id.
func (s *store) setExpiration(id int64, expiration civil.Date) {
	s.licenses[id-1].expiration = expiration
}

// insertAudit adds an audit row for the license, and gives it the next id. It returns the row with its license.
// The caller makes sure that the license has no row for the window yet. Scan does that.
func (s *store) insertAudit(licenseID int64, window Window, hitAt time.Time) AuditEntry {
	row := auditRow{id: int64(len(s.audit)) + 1, licenseID: licenseID, window: window, hitAt: hitAt}
	s.audit = append(s.audit, row)
	if s.windows.moreSevere(window, s.mostSevereAudited(licenseID)) {
		s.mostSevere[licenseID] = window
	}
	return s.entry(row)
}

// mostSevereAudited returns the most severe window that the license has an audit row for, or OK if it has none.
func (s *store) mostSevereAudited(licenseID int64) Window {
	if window, ok := s.mostSevere[licenseID]; ok {
		return window
	}
	return WindowOK
}

// entry joins an audit row with its license.
func (s *store) entry(row auditRow) AuditEntry {
	owner := s.licenses[row.licenseID-1]
	return AuditEntry{
		ID:                row.id,
		NPI:               owner.npi,
		LicenseNumber:     owner.number,
		LicenseState:      owner.state,
		LicenseExpiration: owner.expiration,
		Window:            row.window,
		HitAt:             row.hitAt,
	}
}

// inWindow returns the row as a License that is in the window.
func (r licenseRow) inWindow(window Window) License {
	return License{
		NPI:               r.npi,
		LicenseNumber:     r.number,
		LicenseState:      r.state,
		LicenseExpiration: r.expiration,
		Window:            window,
	}
}
