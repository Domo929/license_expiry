package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
	"github.com/Domo929/license_expiry/foundation/clock"
	"github.com/Domo929/license_expiry/foundation/errs"
)

//go:generate go tool mockgen -typed -source=handlers.go -destination=mock_handlers_test.go -package=main

// licenseService is the part of the business service that the handlers call. *license.Service implements it.
type licenseService interface {
	Today() civil.Date
	License(ctx context.Context, state, number string, asOf civil.Date) (license.License, error)
	WindowGroups(ctx context.Context, asOf civil.Date) ([]license.WindowGroup, error)
	Audit(ctx context.Context, state, number string) ([]license.AuditEntry, error)
	Scan(ctx context.Context) (license.ScanResult, error)
	NextWindow(ctx context.Context, state, number string) (license.Move, error)
}

// handlers are the HTTP handlers of the API. The handlers check the format of the request and call the business service.
type handlers struct {
	log    *slog.Logger
	svc    licenseService
	policy license.Policy
	loc    *time.Location
	clk    license.Clock
	sim    *clock.Simulated // It is nil when clk is the real clock.
}

// routes adds the routes of the API to mux.
//
// A state or a license number in a path or a query is trimmed and put in upper case, as the loader does it.
// as_of is a date in the form YYYY-MM-DD. If it is missing, it is today.
//
//	GET  /licenses/{state}/{number}?as_of=        a license and its window
//	GET  /windows?as_of=&window=                  the licenses of each window, or of one window
//	GET  /audit?state=&number=                    the warnings of a license, or of all licenses
//	GET  /clock                                   the time that the service uses
//	POST /scans                                   a scan, as the scheduler runs it
//	POST /licenses/{state}/{number}/next-window   moves the clock to the day that the license enters its next window
//
// The last route exists only when the clock is simulated.
func (h *handlers) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /licenses/{state}/{number}", h.handle(h.getLicense))
	mux.HandleFunc("GET /windows", h.handle(h.getWindows))
	mux.HandleFunc("GET /audit", h.handle(h.getAudit))
	mux.HandleFunc("GET /clock", h.handle(h.getClock))
	mux.HandleFunc("POST /scans", h.handle(h.postScan))
	if h.sim != nil {
		mux.HandleFunc("POST /licenses/{state}/{number}/next-window", h.handle(h.postNextWindow))
	}
}

func (h *handlers) getLicense(r *http.Request) (any, error) {
	asOf, err := h.asOf(r)
	if err != nil {
		return nil, err
	}
	return h.svc.License(r.Context(), normalize(r.PathValue("state")), normalize(r.PathValue("number")), asOf)
}

func (h *handlers) getWindows(r *http.Request) (any, error) {
	asOf, err := h.asOf(r)
	if err != nil {
		return nil, err
	}
	requested := r.URL.Query().Get("window")
	only := license.Window(normalize(requested))
	if only != "" {
		if _, ok := h.policy.Lookup(only); !ok {
			return nil, fmt.Errorf("window %q must be one of %s: %w", requested, h.windowNames(), errs.ErrInvalid)
		}
	}

	groups, err := h.svc.WindowGroups(r.Context(), asOf)
	if err != nil {
		return nil, err
	}
	response := windowsResponse{AsOf: asOf, Windows: make([]windowGroup, 0, len(groups))}
	for _, group := range groups {
		if only == "" || group.Window == only {
			response.Windows = append(response.Windows, windowGroup{Window: group.Window, Count: len(group.Licenses), Licenses: group.Licenses})
		}
	}
	return response, nil
}

func (h *handlers) getAudit(r *http.Request) (any, error) {
	query := r.URL.Query()
	entries, err := h.svc.Audit(r.Context(), normalize(query.Get("state")), normalize(query.Get("number")))
	if err != nil {
		return nil, err
	}
	return newAuditResponse(entries, h.loc), nil
}

func (h *handlers) getClock(*http.Request) (any, error) {
	return clockResponse{
		Now:       h.clk.Now().In(h.loc),
		Today:     h.svc.Today(),
		TimeZone:  h.loc.String(),
		Simulated: h.sim != nil,
	}, nil
}

func (h *handlers) postScan(r *http.Request) (any, error) {
	result, err := runScan(r.Context(), h.log, h.svc)
	if err != nil {
		return nil, err
	}
	return newScanResponse(result, h.loc), nil
}

// postNextWindow moves the simulated clock to noon on the day that the license enters its next window.
// It does not scan. A demo calls POST /scans next, and the scan warns about the license.
func (h *handlers) postNextWindow(r *http.Request) (any, error) {
	ctx := r.Context()
	state, number := normalize(r.PathValue("state")), normalize(r.PathValue("number"))

	move, err := h.svc.NextWindow(ctx, state, number)
	if err != nil {
		return nil, err
	}
	h.sim.Set(noonOn(move.On, h.loc))

	today := h.svc.Today()
	moved, err := h.svc.License(ctx, state, number, today)
	if err != nil {
		return nil, err
	}
	h.log.Info("simulated clock moved", "today", today, "license_state", state, "license_number", number, "window", moved.Window)
	return nextWindowResponse{PreviousWindow: move.From, Today: today, License: moved}, nil
}

// asOf returns the as_of query value as a date. It is today if the value is missing.
func (h *handlers) asOf(r *http.Request) (civil.Date, error) {
	value := strings.TrimSpace(r.URL.Query().Get("as_of"))
	if value == "" {
		return h.svc.Today(), nil
	}
	day, err := civil.ParseISO(value)
	if err != nil {
		return civil.Date{}, fmt.Errorf("as_of %q must be a date like 2027-03-01: %w", value, errs.ErrInvalid)
	}
	return day, nil
}

// windowNames returns the names of the windows of the policy, in severity order, as one text.
func (h *handlers) windowNames() string {
	var names []string
	for _, row := range h.policy.Windows() {
		names = append(names, string(row.Name))
	}
	return strings.Join(names, ", ")
}

// noonOn returns noon on the day in loc. The simulated clock shows it.
func noonOn(day civil.Date, loc *time.Location) time.Time {
	return day.In(loc).Add(12 * time.Hour)
}
