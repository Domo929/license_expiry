# license_expiry

A service that tracks the expiration dates of clinician licenses and surfaces the licenses that approach expiry.
Storage is in memory. On each start, the service loads `input.csv` again.

## Run

```sh
go run ./app/api                                          # real clock
go run ./app/api --simulated-today=2027-03-01 --scan-interval=0   # demo mode (see below)
```

| Flag | Default | Meaning |
|---|---|---|
| `--input-file` | `input.csv` | The CSV file to load at start-up. The header is `npi,first_name,last_name,license_number,license_state,license_expiration`. |
| `--thresholds` | `90,60,30` | The window thresholds in days. Compliance changes them here, not in the code. |
| `--timezone` | `America/Denver` | The time zone that decides what day "today" is. |
| `--scan-interval` | `1h` | The time between scans. `0` means that only the start-up scan runs. |
| `--simulated-today` | (empty) | Starts a simulated clock on this date, and turns on the next-window route. |
| `--http-addr`, `--debug-addr` | `localhost:8080`, `localhost:8081` | The API port, and the port for metrics and pprof. Never expose the debug port. |

Checks: `task check` formats, vets, lints, runs `go test -race`, and runs govulncheck.

## API

There is no authentication. The service is assumed to run behind an internal gateway.
`as_of` is a date such as `2027-03-01`. When it is missing, the service uses today.

| Route | Purpose |
|---|---|
| `GET /licenses/{state}/{number}?as_of=` | One license and its window, in the prescribed shape. |
| `GET /windows?as_of=&window=` | The licenses in each window (requirement 1). `window` limits the result to one window. |
| `GET /audit?state=&number=` | The audit table: the warnings that were sent. |
| `GET /clock` | The time and the date that the service uses, and whether the clock is simulated. |
| `POST /scans` | Runs a scan now, the same scan that the scheduler runs. |
| `POST /licenses/{state}/{number}/next-window` | Demo mode only. Moves the clock to the day that the license enters its next window. |

## Windows

`days_left = license_expiration − today`. Today is the current date in `--timezone`.
A license is in the most severe window whose bound it meets. With the default thresholds:

| Window | Days left |
|---|---|
| `OK` | more than 90 |
| `EXPIRING_90` | 61–90 |
| `EXPIRING_60` | 31–60 |
| `EXPIRING_30` | 1–30 |
| `EXPIRED` | 0 or fewer. The expiration date itself counts as expired. |

The window is never stored. The service computes it from the expiration date, the day, and the windows table.
A stored window would go stale at midnight.

## Schema

The in-memory store follows the tables below. The DDL is also in the doc comment of `business/license/store.go`.

```sql
CREATE TABLE providers (npi char(10) PRIMARY KEY, first_name text NOT NULL, last_name text NOT NULL);

CREATE TABLE windows (                       -- built from --thresholds
    name          text PRIMARY KEY,          -- OK, EXPIRING_90, EXPIRING_60, EXPIRING_30, EXPIRED
    max_days_left int,                       -- NULL for OK
    severity      int NOT NULL UNIQUE);

CREATE TABLE licenses (
    id                 bigint PRIMARY KEY,
    npi                char(10) NOT NULL REFERENCES providers (npi),
    license_state      char(2)  NOT NULL,
    license_number     text     NOT NULL,
    license_expiration date     NOT NULL,
    UNIQUE (license_state, license_number));

CREATE TABLE license_window_audit (          -- "the provider was warned about this window"
    id          bigint PRIMARY KEY,
    license_id  bigint      NOT NULL REFERENCES licenses (id),
    window_name text        NOT NULL REFERENCES windows (name),
    hit_at      timestamptz NOT NULL,
    UNIQUE (license_id, window_name));
```

- One provider can hold many licenses. Each license is tracked on its own.
- `(license_state, license_number)` is the natural key. A row that gives a known license to a different NPI is rejected.
- The unique key on the audit table is requirement 4: no repeats.

## Scans

A scan runs at start-up, then every `--scan-interval`, and on `POST /scans`. For each license, it does these steps:

1. Compute the license's window today.
2. Find the most severe window in the audit table for the license. If there is none, use `OK`.
3. If today's window is more severe, write one audit row.

The scan is level-triggered. It compares states, and never asks "is today exactly 90 days out?".
So a scan after days of downtime still catches every license (requirement 3).
The license gets one row for the window it is in now. The scan result and the log list the windows it skipped (`skipped_windows`, and a WARN line), so a skipped window is never silent.
A second scan writes nothing (requirement 4). A warning never goes back to a less severe window, even if the clock moves back.

## Demo (Requirement 5)

```sh
go run ./app/api --simulated-today=2027-03-01 --scan-interval=0
curl localhost:8080/licenses/NY/NY-118402                          # 2027-05-31: OK
curl -X POST localhost:8080/licenses/NY/NY-118402/next-window      # clock → 2027-03-02: EXPIRING_90
curl -X POST localhost:8080/scans                                  # audit row for EXPIRING_90
curl -X POST localhost:8080/scans                                  # no hits: no repeats
# Repeat next-window and scans: EXPIRING_60 (2027-04-01), EXPIRING_30 (2027-05-01), EXPIRED (2027-05-31).
curl 'localhost:8080/audit?state=NY&number=NY-118402'              # 4 rows
```

To demonstrate missed runs, call `next-window` twice with no scan in between, then scan once.
Here is the result for `AZ-773905`: one row for `EXPIRING_60`, with `skipped_windows: ["EXPIRING_90"]`.
Use `--scan-interval=0` in demos. Otherwise a timed scan can record the hit before you call `POST /scans`.

## Assumptions and Known Gaps

- **Restarts.** Storage is in memory. A restart forgets the audit table, so the next scan warns again about every license that is not OK. Postgres removes this gap.
- **Renewals.** An audit row does not keep the expiration date it was for. If a renewal arrived later through an update (not built), the old rows would block new warnings. The fix is to add `license_expiration` to the audit key.
- **Duplicate rows.** Rows for the same license with different dates keep the earlier date, which warns sooner.
- **Date format.** The loader infers one date format for the whole file. It tries ISO first, then US month/day/year, then year/month/day. A date that fits no format stops the load.
- **NPI check.** The loader checks that an NPI is 10 digits. It does not run the Luhn check-digit test.
- **Time zone.** One time zone decides today for every license. A license in another zone can change window a few hours earlier or later than its local midnight.

## Layout

```
app/api/           HTTP handlers, CSV input, scheduler, main
business/license/  windows policy, store, load, scan, next-window
foundation/        civil (dates), clock (real and simulated), errs (error kinds)
```
