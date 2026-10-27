package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
)

const csvHeader = "npi,first_name,last_name,license_number,license_state,license_expiration\n"

// sampleCSV is the content of input.csv.
const sampleCSV = csvHeader + `1487203355,Adaeze,Okonkwo,NY-118402,NY,2027-05-31
1592038847,Rowan,Whitfield,NY-118455,NY,2027-05-30
1730915562,Priya,Ramanathan,IL-447120,IL,2027-05-29
1846620391,Devon,Achterberg,PA-330871,PA,2027-04-30
1955130284,Mercedes,Villalobos,TX-902244,TX,2027-04-29
1204487719,Tobias,Lindqvist,OH-551903,OH,2027-03-31
1318802446,Nia,Carrington,GA-664012,GA,2027-03-30
1427719035,Emeka,Balogun,MI-773218,MI,2027-03-02
1536604128,Saoirse,Mulvaney,MA-880134,MA,2027-03-01
1645593317,Hiroshi,Takenaka,WA-991027,WA,2027-02-28
1754482206,Camille,Dubois,NY-118903,NY,2027-03-20
1754482206,Camille,Dubois,NJ-207744,NJ,2027-11-01
1863371195,Yusuf,Demirkan,FL-445019,FL,2026-12-01
1972260084,Anneliese,Brandtner,CO-118277,CO,2028-02-29
1081159973,Marcus,Oyelaran,NY-119544,NY,2027-06-15
1190048862,Sunita,Bhattarai,AZ-773905,AZ,2028-01-01
`

// Two valid rows that go around the row that a test is about.
const (
	goodRow1 = "1487203355,Adaeze,Okonkwo,NY-118402,NY,2027-05-31\n"
	goodRow2 = "1592038847,Rowan,Whitfield,NY-118455,NY,2027-05-30\n"
)

func TestParseInput_SampleFile(t *testing.T) {
	t.Parallel()

	in, err := parseInput(strings.NewReader(sampleCSV))

	require.NoError(t, err)
	want := []license.Record{
		newRecord(t, "1487203355", "Adaeze", "Okonkwo", "NY-118402", "NY", "2027-05-31"),
		newRecord(t, "1592038847", "Rowan", "Whitfield", "NY-118455", "NY", "2027-05-30"),
		newRecord(t, "1730915562", "Priya", "Ramanathan", "IL-447120", "IL", "2027-05-29"),
		newRecord(t, "1846620391", "Devon", "Achterberg", "PA-330871", "PA", "2027-04-30"),
		newRecord(t, "1955130284", "Mercedes", "Villalobos", "TX-902244", "TX", "2027-04-29"),
		newRecord(t, "1204487719", "Tobias", "Lindqvist", "OH-551903", "OH", "2027-03-31"),
		newRecord(t, "1318802446", "Nia", "Carrington", "GA-664012", "GA", "2027-03-30"),
		newRecord(t, "1427719035", "Emeka", "Balogun", "MI-773218", "MI", "2027-03-02"),
		newRecord(t, "1536604128", "Saoirse", "Mulvaney", "MA-880134", "MA", "2027-03-01"),
		newRecord(t, "1645593317", "Hiroshi", "Takenaka", "WA-991027", "WA", "2027-02-28"),
		newRecord(t, "1754482206", "Camille", "Dubois", "NY-118903", "NY", "2027-03-20"),
		newRecord(t, "1754482206", "Camille", "Dubois", "NJ-207744", "NJ", "2027-11-01"),
		newRecord(t, "1863371195", "Yusuf", "Demirkan", "FL-445019", "FL", "2026-12-01"),
		newRecord(t, "1972260084", "Anneliese", "Brandtner", "CO-118277", "CO", "2028-02-29"),
		newRecord(t, "1081159973", "Marcus", "Oyelaran", "NY-119544", "NY", "2027-06-15"),
		newRecord(t, "1190048862", "Sunita", "Bhattarai", "AZ-773905", "AZ", "2028-01-01"),
	}
	assert.Equal(t, want, in.records)
	wantLines := make([]int, len(want))
	for i := range wantLines {
		wantLines[i] = i + 2 // The header is line 1.
	}
	assert.Equal(t, wantLines, in.lines)
	assert.Empty(t, in.rejected)
	assert.Empty(t, in.extraColumns)
	assert.Equal(t, civil.LayoutISO, in.layout)
}

func TestParseInput_Header(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		csv       string
		wantExtra []string
	}{
		{
			name: "any order, any case, spaces, and a byte order mark",
			csv:  "\ufeffLicense_Expiration, NPI ,license_state,LICENSE_NUMBER,Last_Name,first_name\n2027-05-31,1487203355,NY,NY-118402,Okonkwo,Adaeze\n",
		},
		{
			name:      "an extra column is ignored and reported",
			csv:       "npi,first_name,Notes,last_name,license_number,license_state,license_expiration\n1487203355,Adaeze,call back,Okonkwo,NY-118402,NY,2027-05-31\n",
			wantExtra: []string{"Notes"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in, err := parseInput(strings.NewReader(tc.csv))

			require.NoError(t, err)
			assert.Equal(t, []license.Record{newRecord(t, "1487203355", "Adaeze", "Okonkwo", "NY-118402", "NY", "2027-05-31")}, in.records)
			assert.Equal(t, []int{2}, in.lines)
			assert.Empty(t, in.rejected)
			assert.Equal(t, tc.wantExtra, in.extraColumns)
		})
	}
}

func TestParseInput_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input io.Reader
		want  string
	}{
		{
			name:  "empty file",
			input: strings.NewReader(""),
			want:  "the file is empty",
		},
		{
			name:  "a missing column",
			input: strings.NewReader("npi,first_name,last_name,license_number,license_expiration\n"),
			want:  "missing columns: license_state",
		},
		{
			name:  "all the missing columns are named",
			input: strings.NewReader("npi,first_name\n"),
			want:  "missing columns: last_name, license_number, license_state, license_expiration",
		},
		{
			name:  "a duplicate column, in another case",
			input: strings.NewReader("npi,first_name,last_name,license_number,license_state,license_expiration,NPI\n"),
			want:  `column "npi" is listed twice`,
		},
		{
			name:  "dates with no common layout",
			input: strings.NewReader(csvHeader + goodRow1 + "1592038847,Rowan,Whitfield,NY-118455,NY,5/30/2027\n"),
			want:  "infer date layout",
		},
		{
			name:  "a date that is not a date",
			input: strings.NewReader(csvHeader + goodRow1 + "1592038847,Rowan,Whitfield,NY-118455,NY,soon\n"),
			want:  "infer date layout",
		},
		{
			name:  "a read error",
			input: iotest.ErrReader(errors.New("disk failure")),
			want:  "disk failure",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseInput(tc.input)

			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestParseInput_DateLayouts(t *testing.T) {
	t.Parallel()

	t.Run("US dates use the layout that the whole file shares", func(t *testing.T) {
		t.Parallel()
		csv := csvHeader +
			"1487203355,Adaeze,Okonkwo,NY-118402,NY,5/30/2027\n" +
			"1592038847,Rowan,Whitfield,NY-118455,NY,12/1/2026\n"

		in, err := parseInput(strings.NewReader(csv))

		require.NoError(t, err)
		assert.Equal(t, "1/2/2006", in.layout)
		require.Len(t, in.records, 2)
		assert.Equal(t, mustDate(t, "2027-05-30"), in.records[0].LicenseExpiration)
		assert.Equal(t, mustDate(t, "2026-12-01"), in.records[1].LicenseExpiration)
	})

	t.Run("a header with no rows has no records and no layout", func(t *testing.T) {
		t.Parallel()

		in, err := parseInput(strings.NewReader(csvHeader))

		require.NoError(t, err)
		assert.Empty(t, in.records)
		assert.Empty(t, in.rejected)
		assert.Empty(t, in.layout)
	})
}

func TestParseInput_Normalizes(t *testing.T) {
	t.Parallel()

	in, err := parseInput(strings.NewReader(csvHeader + "1487203355, Adaeze ,Okonkwo, ny-118402 , ny ,2027-05-31\n"))

	require.NoError(t, err)
	assert.Equal(t, []license.Record{newRecord(t, "1487203355", "Adaeze", "Okonkwo", "NY-118402", "NY", "2027-05-31")}, in.records)
}

func TestParseInput_RejectsBadRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		row  string
		want string // A part of the reason.
	}{
		{name: "an NPI with 9 digits", row: "148720335,Ada,Lovelace,NY-1,NY,2027-05-31", want: "npi must be exactly 10 digits"},
		{name: "an NPI with a letter", row: "14872033x5,Ada,Lovelace,NY-1,NY,2027-05-31", want: "npi must be exactly 10 digits"},
		{name: "an NPI with digits that are not ASCII", row: "１２３４５６７８９０,Ada,Lovelace,NY-1,NY,2027-05-31", want: "npi must be exactly 10 digits"},
		{name: "an empty first name", row: "1487203355,  ,Lovelace,NY-1,NY,2027-05-31", want: "first_name is empty"},
		{name: "an empty last name", row: "1487203355,Ada,,NY-1,NY,2027-05-31", want: "last_name is empty"},
		{name: "a first name with 101 characters", row: "1487203355," + strings.Repeat("é", 101) + ",Lovelace,NY-1,NY,2027-05-31", want: "first_name is longer than 100 characters"},
		{name: "an empty license number", row: "1487203355,Ada,Lovelace, ,NY,2027-05-31", want: "license_number is empty"},
		{name: "a license number with 51 characters", row: "1487203355,Ada,Lovelace," + strings.Repeat("N", 51) + ",NY,2027-05-31", want: "license_number is longer than 50 characters"},
		{name: "a state that is not a USPS code", row: "1487203355,Ada,Lovelace,NY-1,XX,2027-05-31", want: "license_state must be the USPS code"},
		{name: "a state with three letters", row: "1487203355,Ada,Lovelace,NY-1,NYC,2027-05-31", want: "license_state must be the USPS code"},
		{name: "an empty expiration", row: "1487203355,Ada,Lovelace,NY-1,NY, ", want: "license_expiration is empty"},
		{name: "too few fields", row: "1487203355,Ada,Lovelace", want: "the row has 3 fields, and the header has 6"},
		{name: "too many fields", row: "1487203355,Ada,Lovelace,NY-1,NY,2027-05-31,extra", want: "the row has 7 fields, and the header has 6"},
		{name: "a quote in the middle of a field", row: `1487203355,Ad"a,Lovelace,NY-1,NY,2027-05-31`, want: "bare"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			in, err := parseInput(strings.NewReader(csvHeader + goodRow1 + tc.row + "\n" + goodRow2))

			require.NoError(t, err)
			require.Len(t, in.rejected, 1)
			assert.Equal(t, 3, in.rejected[0].line)
			require.ErrorContains(t, in.rejected[0].err, tc.want)
			assert.Len(t, in.records, 2, "the other rows load")
			assert.Equal(t, []int{2, 4}, in.lines)
		})
	}

	t.Run("a reason does not repeat a value", func(t *testing.T) {
		t.Parallel()
		// The columns are shifted, so the NPI column holds a name.
		in, err := parseInput(strings.NewReader(csvHeader + "Adaeze,1487203355,Okonkwo,NY-1,NY,2027-05-31\n"))

		require.NoError(t, err)
		require.Len(t, in.rejected, 1)
		assert.NotContains(t, in.rejected[0].err.Error(), "Adaeze")
	})

	t.Run("rejected rows are in the order of the file", func(t *testing.T) {
		t.Parallel()
		// The CSV reader rejects line 4, and the format check rejects line 3.
		csv := csvHeader + goodRow1 + "x,Ada,Lovelace,NY-1,NY,2027-05-31\n" + "1,2\n"

		in, err := parseInput(strings.NewReader(csv))

		require.NoError(t, err)
		require.Len(t, in.rejected, 2)
		assert.Equal(t, 3, in.rejected[0].line)
		assert.Equal(t, 4, in.rejected[1].line)
	})
}

func TestUSPSCodes(t *testing.T) {
	t.Parallel()

	assert.Len(t, uspsCodes, 56, "50 states, DC, and 5 territories")
	assert.Len(t, slices.Compact(slices.Sorted(slices.Values(uspsCodes))), 56, "no code is listed twice")
	for _, code := range []string{"AK", "HI", "NY", "WY", "DC", "PR", "GU", "VI", "AS", "MP"} {
		assert.Contains(t, uspsCodes, code)
	}
}

func TestLoadInput(t *testing.T) {
	t.Parallel()

	csv := csvHeader +
		"1111111111,Ada,Lovelace,NY-1,NY,2027-05-31\n" + // Line 2: a new license.
		"bad,Grace,Hopper,NY-2,NY,2027-05-31\n" + // Line 3: the format check rejects it.
		"2222222222,Grace,Hopper,NY-1,NY,2027-05-31\n" + // Line 4: NY-1 belongs to another provider, so the business package rejects it.
		"1111111111,Ada,Lovelace,NY-1,NY,2027-05-31\n" + // Line 5: a duplicate that has the same date.
		"1111111111,Ada,Lovelace,NY-1,NY,2027-05-01\n" + // Line 6: a duplicate with an earlier date, which gets a warning.
		"3333333333,Linus,Torvalds,CA-1,ca,2027-05-31\n" // Line 7: a new license.
	path := filepath.Join(t.TempDir(), "input.csv")
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))
	log, logs := newTestLogger()
	svc := newService(t, newTestClock(t))

	err := loadInput(t.Context(), log, svc, path)

	require.NoError(t, err)
	out := logs.String()
	assert.Regexp(t, `level=ERROR msg="input row rejected" file=\S+ line=3 reason="npi must be exactly 10 digits"\n`, out)
	assert.Regexp(t, `level=ERROR msg="input row rejected" file=\S+ line=4 reason=".*belongs to provider 1111111111, not to provider 2222222222.*"\n`, out)
	assert.Regexp(t, `level=WARN msg="input row warning" file=\S+ line=6 warning=".*earlier expiration date 2027-05-01.*"\n`, out)
	assert.Regexp(t, `level=INFO msg="input loaded" file=\S+ rows=6 licenses_added=2 providers_added=2 duplicates=2 rejected=2 warnings=1 date_layout=2006-01-02\n`, out)

	got, err := svc.License(t.Context(), "NY", "NY-1", mustDate(t, "2027-03-01"))
	require.NoError(t, err)
	assert.Equal(t, mustDate(t, "2027-05-01"), got.LicenseExpiration)
	_, err = svc.License(t.Context(), "CA", "CA-1", mustDate(t, "2027-03-01"))
	require.NoError(t, err, "the state is in upper case")
}

func TestLoadInput_Warnings(t *testing.T) {
	t.Parallel()

	t.Run("an extra column is logged once", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "input.csv")
		csv := "npi,first_name,last_name,license_number,license_state,license_expiration,Notes,Source\n" +
			"1111111111,Ada,Lovelace,NY-1,NY,2027-05-31,a,b\n"
		require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))
		log, logs := newTestLogger()

		err := loadInput(t.Context(), log, newService(t, newTestClock(t)), path)

		require.NoError(t, err)
		assert.Equal(t, 1, strings.Count(logs.String(), "input has columns that the service ignores"))
		assert.Contains(t, logs.String(), `level=WARN msg="input has columns that the service ignores"`)
		assert.Contains(t, logs.String(), `columns="[Notes Source]"`)
	})
}

func TestLoadInput_Errors(t *testing.T) {
	t.Parallel()

	t.Run("a file that does not exist", func(t *testing.T) {
		t.Parallel()
		log, _ := newTestLogger()

		err := loadInput(t.Context(), log, newService(t, newTestClock(t)), filepath.Join(t.TempDir(), "missing.csv"))

		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("a bad header", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "input.csv")
		require.NoError(t, os.WriteFile(path, []byte("npi,name\n"), 0o600))
		log, _ := newTestLogger()

		err := loadInput(t.Context(), log, newService(t, newTestClock(t)), path)

		require.ErrorContains(t, err, "missing columns")
		require.ErrorContains(t, err, path)
	})
}
