package main

import (
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/errs"
)

// handle returns the HTTP handler of fn, a function from a request to a response value.
// If fn returns an error, the response is the error. Otherwise it is the status 200 and the value as JSON.
func (h *handlers) handle(fn func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := fn(r)
		if err != nil {
			writeError(h.log, w, err)
			return
		}
		writeJSON(h.log, w, http.StatusOK, body)
	}
}

// writeJSON writes body as JSON with the status code.
func writeJSON(log *slog.Logger, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.MarshalWrite(w, body); err != nil {
		// The status is sent already, so the client can only see a response that stops early.
		log.Error("write response", "error", err)
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

// writeError is the only place that turns an error into a response.
//
// An error of one kind in the errs package is the fault of the client, and the client sees its text.
// Any other error is our fault. It is logged here, once, and the client sees a generic message.
func writeError(log *slog.Logger, w http.ResponseWriter, err error) {
	status := statusOf(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		log.Error("request failed", "error", err)
		message = "internal server error"
	}
	writeJSON(log, w, status, errorResponse{Error: message})
}

// statusOf returns the status code for err.
func statusOf(err error) int {
	switch {
	case errors.Is(err, errs.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, errs.ErrUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, errs.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, errs.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, errs.ErrConflict):
		return http.StatusConflict
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusInternalServerError
}

// The JSON shapes of the responses. A license.License is already in the shape that the requirements prescribe.

type windowsResponse struct {
	AsOf    civil.Date    `json:"as_of"`
	Windows []windowGroup `json:"windows"`
}

type windowGroup struct {
	Window   license.Window    `json:"window"`
	Count    int               `json:"count"`
	Licenses []license.License `json:"licenses"`
}

type auditResponse struct {
	Entries []auditEntryView `json:"entries"`
}

type auditEntryView struct {
	ID                int64          `json:"id"`
	NPI               string         `json:"npi"`
	LicenseNumber     string         `json:"license_number"`
	LicenseState      string         `json:"license_state"`
	LicenseExpiration civil.Date     `json:"license_expiration"`
	Window            license.Window `json:"window"`
	HitAt             time.Time      `json:"hit_at"`
}

type clockResponse struct {
	Now       time.Time  `json:"now"`
	Today     civil.Date `json:"today"`
	TimeZone  string     `json:"time_zone"`
	Simulated bool       `json:"simulated"`
}

type scanResponse struct {
	AsOf    civil.Date `json:"as_of"`
	Checked int        `json:"checked"`
	Hits    []hitView  `json:"hits"`
}

type hitView struct {
	AuditID           int64            `json:"audit_id"`
	NPI               string           `json:"npi"`
	LicenseNumber     string           `json:"license_number"`
	LicenseState      string           `json:"license_state"`
	LicenseExpiration civil.Date       `json:"license_expiration"`
	Window            license.Window   `json:"window"`
	PreviousWindow    license.Window   `json:"previous_window"`
	SkippedWindows    []license.Window `json:"skipped_windows"`
	HitAt             time.Time        `json:"hit_at"`
}

type nextWindowResponse struct {
	PreviousWindow license.Window  `json:"previous_window"`
	Today          civil.Date      `json:"today"`
	License        license.License `json:"license"`
}

// newAuditResponse shows the entries with their times in the time zone loc.
func newAuditResponse(entries []license.AuditEntry, loc *time.Location) auditResponse {
	views := make([]auditEntryView, 0, len(entries))
	for _, e := range entries {
		views = append(views, auditEntryView{
			ID:                e.ID,
			NPI:               e.NPI,
			LicenseNumber:     e.LicenseNumber,
			LicenseState:      e.LicenseState,
			LicenseExpiration: e.LicenseExpiration,
			Window:            e.Window,
			HitAt:             e.HitAt.In(loc),
		})
	}
	return auditResponse{Entries: views}
}

// newScanResponse shows the hits with their times in the time zone loc.
func newScanResponse(result license.ScanResult, loc *time.Location) scanResponse {
	views := make([]hitView, 0, len(result.Hits))
	for _, hit := range result.Hits {
		views = append(views, hitView{
			AuditID:           hit.ID,
			NPI:               hit.NPI,
			LicenseNumber:     hit.LicenseNumber,
			LicenseState:      hit.LicenseState,
			LicenseExpiration: hit.LicenseExpiration,
			Window:            hit.Window,
			PreviousWindow:    hit.Previous,
			SkippedWindows:    hit.Skipped,
			HitAt:             hit.HitAt.In(loc),
		})
	}
	return scanResponse{AsOf: result.AsOf, Checked: result.Checked, Hits: views}
}
