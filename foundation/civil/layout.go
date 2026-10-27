package civil

import (
	"errors"
	"fmt"
	"strings"
)

const (
	layoutUS       = "1/2/2006"
	layoutSlashISO = "2006/1/2"
)

// DefaultLayouts returns the layouts that InferLayout tries when it gets none, in this order:
// LayoutISO, "1/2/2006", and "2006/1/2". The second layout is the US month/day/year order.
// It also accepts zero-padded values, such as 03/01/2027.
//
// Day-first layouts are not in the list on purpose, because they are ambiguous with US dates.
func DefaultLayouts() []string {
	return []string{LayoutISO, layoutUS, layoutSlashISO}
}

// InferLayout returns the first layout in layouts that parses every non-empty value in values.
// It trims the spaces around each value, and it ignores the values that are empty after that.
// If layouts is empty, it tries DefaultLayouts.
//
// InferLayout picks one layout for a whole data set, so a file never mixes month/day orders.
// It returns an error if there is no non-empty value, or if no layout parses all of them.
// In the second case, the error names a value that failed.
func InferLayout(values []string, layouts ...string) (string, error) {
	if len(layouts) == 0 {
		layouts = DefaultLayouts()
	}

	present := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			present = append(present, v)
		}
	}
	if len(present) == 0 {
		return "", errors.New("infer date layout: no non-empty values")
	}

	// If no layout fits, report the failure of the layout that parsed the most values first.
	// It is the one that is closest to fitting, so its failed value is the most likely typo.
	var (
		closestErr    error
		closestParsed = -1
	)
	for _, layout := range layouts {
		parsed, err := parseUntilFailure(present, layout)
		if err == nil {
			return layout, nil
		}
		if parsed > closestParsed {
			closestParsed, closestErr = parsed, err
		}
	}
	return "", fmt.Errorf("infer date layout: no layout parses every value: %w", closestErr)
}

// parseUntilFailure parses values in order. It returns the number of values that parsed,
// and the error of the first value that did not.
func parseUntilFailure(values []string, layout string) (int, error) {
	for i, v := range values {
		if _, err := Parse(layout, v); err != nil {
			return i, err
		}
	}
	return len(values), nil
}
