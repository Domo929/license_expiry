package license_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
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

// newPolicy returns the policy for the thresholds, and stops the test if that fails.
func newPolicy(t *testing.T, thresholds ...int) license.Policy {
	t.Helper()
	policy, err := license.NewPolicy(thresholds)
	require.NoError(t, err)
	return policy
}
