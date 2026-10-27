package main

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Domo929/license_expiry/business/license"
	"github.com/Domo929/license_expiry/foundation/civil"
)

// The columns of the input file. Each one is required, and the file can list them in any order.
const (
	colNPI        = "npi"
	colFirstName  = "first_name"
	colLastName   = "last_name"
	colNumber     = "license_number"
	colState      = "license_state"
	colExpiration = "license_expiration"
)

var requiredColumns = []string{colNPI, colFirstName, colLastName, colNumber, colState, colExpiration}

const (
	maxNameRunes   = 100
	maxNumberRunes = 50
)

// uspsCodes are the codes of the 50 states, the District of Columbia, and five territories.
var uspsCodes = strings.Fields(`
	AL AK AZ AR CA CO CT DE FL GA HI ID IL IN IA KS KY LA ME MD MA MI MN MS MO
	MT NE NV NH NJ NM NY NC ND OH OK OR PA RI SC SD TN TX UT VT VA WA WV WI WY
	DC PR GU VI AS MP`)

// normalize returns the form in which the service keeps a license number or a state: trimmed, in upper case.
// The loader and the routes both use it, so a lookup finds what the loader stored.
func normalize(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// parsedInput is an input file after the checks of the format.
type parsedInput struct {
	records []license.Record
	lines   []int // lines[i] is the line of records[i] in the file.

	rejected     []rowIssue // The rows that break a format rule, in the order of the file.
	extraColumns []string   // The columns that the service does not use.
	layout       string     // The layout of the dates. It is empty if no row has a date.
}

// rowIssue is a problem with one row of the file.
type rowIssue struct {
	line int
	err  error
}

// loadInput loads the licenses in the CSV file at path into svc, and logs what happened.
// A row with a problem is logged and skipped.
// It returns an error only if the file cannot be read, its header is wrong, or its dates have no common layout.
func loadInput(ctx context.Context, log *slog.Logger, svc *license.Service, path string) error {
	in, err := readInputFile(path)
	if err != nil {
		return err
	}

	log = log.With("file", path)
	if len(in.extraColumns) > 0 {
		log.Warn("input has columns that the service ignores", "columns", in.extraColumns)
	}
	for _, row := range in.rejected {
		log.Error("input row rejected", "line", row.line, "reason", row.err)
	}

	result, err := svc.Load(ctx, in.records)
	if err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	// The business package reports the position of a record. The log needs the line of the file.
	for _, issue := range result.Rejected {
		log.Error("input row rejected", "line", in.lines[issue.Index], "reason", issue.Err)
	}
	for _, issue := range result.Warnings {
		log.Warn("input row warning", "line", in.lines[issue.Index], "warning", issue.Err)
	}

	log.Info("input loaded",
		"rows", len(in.records)+len(in.rejected),
		"licenses_added", result.LicensesAdded,
		"providers_added", result.ProvidersAdded,
		"duplicates", result.Duplicates,
		"rejected", len(in.rejected)+len(result.Rejected),
		"warnings", len(result.Warnings),
		"date_layout", in.layout)
	return nil
}

// readInputFile parses the CSV file at path.
func readInputFile(path string) (parsedInput, error) {
	f, err := os.Open(path) //nolint:gosec // The operator chooses the file with the --input-file flag.
	if err != nil {
		return parsedInput{}, fmt.Errorf("input file: %w", err)
	}
	defer func() { _ = f.Close() }() // The file was only read, so a failed close loses nothing.

	in, err := parseInput(f)
	if err != nil {
		return parsedInput{}, fmt.Errorf("input file %s: %w", path, err)
	}
	return in, nil
}

// parseInput reads CSV from r and checks the format of each row. A row that breaks a rule is rejected.
// It returns an error if there is no header, if the header is wrong, or if the dates do not share one layout.
func parseInput(r io.Reader) (parsedInput, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // readRows counts the fields, so that a short row is rejected with its line.

	headerRow, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return parsedInput{}, errors.New("the file is empty, and the first row must be the header")
	}
	if err != nil {
		return parsedInput{}, fmt.Errorf("read header: %w", err)
	}
	pos, extra, err := parseHeader(headerRow)
	if err != nil {
		return parsedInput{}, err
	}
	rows, rejected, err := readRows(cr, len(headerRow))
	if err != nil {
		return parsedInput{}, err
	}
	layout, err := inferLayout(rows, pos[colExpiration])
	if err != nil {
		return parsedInput{}, err
	}

	in := parsedInput{rejected: rejected, extraColumns: extra, layout: layout}
	for _, row := range rows {
		record, err := parseRecord(pos, layout, row.fields)
		if err != nil {
			in.rejected = append(in.rejected, rowIssue{line: row.line, err: err})
			continue
		}
		in.records = append(in.records, record)
		in.lines = append(in.lines, row.line)
	}
	slices.SortFunc(in.rejected, func(a, b rowIssue) int { return cmp.Compare(a.line, b.line) })
	return in, nil
}

// parseHeader returns the position of each required column. Names are not case sensitive, and spaces around them do not count.
// It also returns the names of the other columns, as written in the file.
func parseHeader(row []string) (map[string]int, []string, error) {
	pos := map[string]int{}
	var extra []string
	for i, cell := range row {
		// Excel puts a byte order mark before the first name.
		name := strings.TrimSpace(strings.TrimPrefix(cell, "\ufeff"))
		column := strings.ToLower(name)
		if !slices.Contains(requiredColumns, column) {
			extra = append(extra, name)
			continue
		}
		if _, listed := pos[column]; listed {
			return nil, nil, fmt.Errorf("header: column %q is listed twice", column)
		}
		pos[column] = i
	}

	var missing []string
	for _, column := range requiredColumns {
		if _, ok := pos[column]; !ok {
			missing = append(missing, column)
		}
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("header: missing columns: %s", strings.Join(missing, ", "))
	}
	return pos, extra, nil
}

// csvRow is a row of the file that has the same number of fields as the header.
type csvRow struct {
	line   int
	fields []string
}

// readRows reads the rows after the header. A row that is not valid CSV, or that has too many or too few fields, is rejected.
// It returns an error only if the reader fails.
func readRows(cr *csv.Reader, width int) ([]csvRow, []rowIssue, error) {
	var (
		rows     []csvRow
		rejected []rowIssue
	)
	for {
		fields, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return rows, rejected, nil
		}
		if parseErr, ok := errors.AsType[*csv.ParseError](err); ok {
			rejected = append(rejected, rowIssue{line: parseErr.StartLine, err: parseErr.Err})
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read rows: %w", err)
		}

		line, _ := cr.FieldPos(0)
		if len(fields) != width {
			rejected = append(rejected, rowIssue{line: line, err: fmt.Errorf("the row has %d fields, and the header has %d", len(fields), width)})
			continue
		}
		rows = append(rows, csvRow{line: line, fields: fields})
	}
}

// inferLayout returns the layout of the dates in the column at position col. It is empty if no row has a date.
// The whole file has one layout, so a file never mixes month/day orders.
// A date that fits no layout fails the load. It is not guessed.
func inferLayout(rows []csvRow, col int) (string, error) {
	var dates []string
	for _, row := range rows {
		if date := strings.TrimSpace(row.fields[col]); date != "" {
			dates = append(dates, date)
		}
	}
	if len(dates) == 0 {
		return "", nil
	}
	return civil.InferLayout(dates)
}

// parseRecord checks the format of one row, and returns its record. layout is the layout of the dates.
//
// A reason never repeats a value of the row. A value in the wrong column can be a name, and the log must not hold names.
func parseRecord(pos map[string]int, layout string, fields []string) (license.Record, error) {
	field := func(column string) string { return strings.TrimSpace(fields[pos[column]]) }
	record := license.Record{
		NPI:           field(colNPI),
		FirstName:     field(colFirstName),
		LastName:      field(colLastName),
		LicenseNumber: normalize(field(colNumber)),
		LicenseState:  normalize(field(colState)),
	}

	if !isNPI(record.NPI) {
		return license.Record{}, fmt.Errorf("%s must be exactly 10 digits", colNPI)
	}
	if err := checkText(colFirstName, record.FirstName, maxNameRunes); err != nil {
		return license.Record{}, err
	}
	if err := checkText(colLastName, record.LastName, maxNameRunes); err != nil {
		return license.Record{}, err
	}
	if err := checkText(colNumber, record.LicenseNumber, maxNumberRunes); err != nil {
		return license.Record{}, err
	}
	if !slices.Contains(uspsCodes, record.LicenseState) {
		return license.Record{}, fmt.Errorf("%s must be the USPS code of a state or territory", colState)
	}

	expiration := field(colExpiration)
	if expiration == "" {
		return license.Record{}, fmt.Errorf("%s is empty", colExpiration)
	}
	date, err := civil.Parse(layout, expiration)
	if err != nil {
		return license.Record{}, fmt.Errorf("%s: %w", colExpiration, err)
	}
	record.LicenseExpiration = date
	return record, nil
}

// isNPI reports whether s is exactly 10 ASCII digits.
func isNPI(s string) bool {
	return len(s) == 10 && !strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
}

// checkText returns an error if the value is empty or has more than maxRunes characters.
func checkText(column, value string, maxRunes int) error {
	switch {
	case value == "":
		return fmt.Errorf("%s is empty", column)
	case utf8.RuneCountInString(value) > maxRunes:
		return fmt.Errorf("%s is longer than %d characters", column, maxRunes)
	}
	return nil
}
