// Package license tracks when the licenses of providers expire. It shows which licenses need attention.
//
// A license is in one window: OK, EXPIRING_n for each threshold n, or EXPIRED.
// A Policy defines the windows. It comes from the thresholds in the configuration.
// The package computes the window from the expiration date of the license and a day. It never stores the window.
// A stored window goes stale at midnight.
//
// A scan warns about a license when the license enters a window.
// The audit table holds one row for each warning.
// A scan compares the window of a license with the most severe window in its audit rows.
// It warns only if the window is more severe. So a license gets one warning for each window.
// A scan that runs late still warns, and it lists the windows that the license skipped.
// A scan never warns about a less severe window than before, even if the clock moves back.
//
// A day is a civil.Date in the time zone of the Service. A license expires on its expiration date.
// That date is the first day on which the license is EXPIRED.
//
// The service keeps its data in memory. The comment on the store type shows the Postgres schema that the data follows.
// Each method of Service takes the lock of the store once, as a transaction would.
package license
