package civil_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/foundation/civil"
)

func TestDefaultLayouts(t *testing.T) {
	t.Parallel()

	want := []string{"2006-01-02", "1/2/2006", "2006/1/2"}

	t.Run("order", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, want, civil.DefaultLayouts())
		assert.Equal(t, civil.LayoutISO, civil.DefaultLayouts()[0])
	})

	t.Run("callers get their own copy", func(t *testing.T) {
		t.Parallel()
		got := civil.DefaultLayouts()

		got[0] = "changed"

		assert.Equal(t, want, civil.DefaultLayouts())
	})

	t.Run("every layout parses a date it should", func(t *testing.T) {
		t.Parallel()
		examples := []string{"2027-03-01", "3/1/2027", "2027/3/1"}

		layouts := civil.DefaultLayouts()

		require.Len(t, layouts, len(examples))
		for i, layout := range layouts {
			got, err := civil.Parse(layout, examples[i])
			require.NoError(t, err)
			assert.Equal(t, "2027-03-01", got.String())
		}
	})
}

func TestInferLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  []string
		layouts []string
		want    string
		wantErr string // A text that the error must contain. Empty means no error.
	}{
		{name: "ISO", values: []string{"2027-05-30", "2028-02-29"}, want: civil.LayoutISO},
		{name: "one value", values: []string{"2027-05-30"}, want: civil.LayoutISO},
		{name: "US", values: []string{"3/1/2027", "12/31/2027"}, want: "1/2/2006"},
		{name: "US zero padded", values: []string{"03/01/2027", "12/31/2027"}, want: "1/2/2006"},
		{name: "US with values that day first would also parse", values: []string{"3/1/2027", "4/2/2027"}, want: "1/2/2006"},
		{name: "year first with slashes", values: []string{"2027/3/1", "2027/12/31"}, want: "2006/1/2"},
		{name: "blanks and spaces are ignored", values: []string{"", "  ", " 2027-05-30 ", "\t", "2028-02-29"}, want: civil.LayoutISO},
		{name: "spaces around US values", values: []string{" 3/1/2027", "12/31/2027 "}, want: "1/2/2006"},
		{name: "custom layout", values: []string{"31/12/2027", "1/2/2027"}, layouts: []string{"2/1/2006"}, want: "2/1/2006"},
		{name: "the first custom layout that fits wins", values: []string{"2027-05-30"}, layouts: []string{"2006-1-2", civil.LayoutISO}, want: "2006-1-2"},
		{name: "a later custom layout can fit", values: []string{"2027-05-30"}, layouts: []string{"2/1/2006", civil.LayoutISO}, want: civil.LayoutISO},
		{name: "custom layouts replace the defaults", values: []string{"2027-05-30"}, layouts: []string{"2/1/2006"}, wantErr: "2027-05-30"},
		{name: "no values", values: nil, wantErr: "no non-empty values"},
		{name: "empty list", values: []string{}, wantErr: "no non-empty values"},
		{name: "only blanks", values: []string{"", "   ", "\t"}, wantErr: "no non-empty values"},
		{name: "text", values: []string{"not a date"}, wantErr: "not a date"},
		{name: "impossible date", values: []string{"2027-02-30"}, wantErr: "2027-02-30"},
		{name: "ISO mixed with US", values: []string{"2027-05-30", "3/1/2027"}, wantErr: "3/1/2027"},
		{name: "US mixed with ISO", values: []string{"3/1/2027", "2027-05-30"}, wantErr: "2027-05-30"},
		{name: "day first is not guessed", values: []string{"31/12/2027"}, wantErr: "31/12/2027"},
		// The error names the value of the layout that is closest to fitting, which is the typo.
		{name: "one bad value among US values", values: []string{"3/1/2027", "13/45/2027", "12/31/2027"}, wantErr: "13/45/2027"},
		{name: "one bad value among ISO values", values: []string{"2027-05-30", "2027-13-01", "2028-02-29"}, wantErr: "2027-13-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := civil.InferLayout(tc.values, tc.layouts...)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.ErrorContains(t, err, "infer date layout")
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInferLayoutError(t *testing.T) {
	t.Parallel()

	t.Run("wraps the parse error of the failed value", func(t *testing.T) {
		t.Parallel()

		_, err := civil.InferLayout([]string{"3/1/2027", "13/45/2027"})

		parseErr, ok := errors.AsType[*time.ParseError](err)
		require.True(t, ok)
		assert.Equal(t, "13/45/2027", parseErr.Value)
	})

	t.Run("does not change the values", func(t *testing.T) {
		t.Parallel()
		values := []string{" 2027-05-30 ", "", "2028-02-29"}

		_, err := civil.InferLayout(values)

		require.NoError(t, err)
		assert.Equal(t, []string{" 2027-05-30 ", "", "2028-02-29"}, values)
	})
}
