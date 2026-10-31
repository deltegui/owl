package excel

import (
	"errors"
	"fmt"
	"iter"
	"strings"
	"time"
)

// ErrInvalid is wrapped by the errors of Workbook.Validate.
var ErrInvalid = errors.New("excel: invalid workbook")

// Column describes one column for the rows of type T: its title and how to
// read it from a row. Which columns there are, and in which order, is decided
// by the code that builds the slice, so hiding a column (for who cannot see
// it) is not adding it.
type Column[T any] struct {
	// Header is the title, already translated.
	Header string
	// Value reads the cell of a row.
	Value func(T) Cell
	// Format is the format of the cells, in the notation of spreadsheets:
	// "dd/mm/yyyy hh:mm", "#,##0.00". Empty is the general format.
	Format string
	// Width is the width of the column in characters. Zero lets the generator
	// choose (to fit the content, for example).
	Width float64
}

// ColumnInfo is what a Generator knows of a column.
type ColumnInfo struct {
	Header string
	Format string
	Width  float64
}

// Str is a column of text.
func Str[T any](header string, f func(T) string) Column[T] {
	return Column[T]{Header: header, Value: func(r T) Cell { return Text(f(r)) }}
}

// Num is a column of floating point numbers.
func Num[T any](header string, f func(T) float64) Column[T] {
	return Column[T]{Header: header, Value: func(r T) Cell { return Number(f(r)) }}
}

// Int is a column of integers, written exactly.
func Int[T any](header string, f func(T) int64) Column[T] {
	return Column[T]{Header: header, Value: func(r T) Cell { return Decimal(f(r), 0) }}
}

// Money is a column of amounts in cents, written exactly with two decimals.
func Money[T any](header string, f func(T) int64) Column[T] {
	return Column[T]{Header: header, Format: "#,##0.00", Value: func(r T) Cell { return Cents(f(r)) }}
}

// Bool is a column of booleans.
func Bool[T any](header string, f func(T) bool) Column[T] {
	return Column[T]{Header: header, Value: func(r T) Cell { return Boolean(f(r)) }}
}

// YesNo is a column of booleans written as text, with the words already
// translated.
func YesNo[T any](header, yes, no string, f func(T) bool) Column[T] {
	return Column[T]{Header: header, Value: func(r T) Cell {
		if f(r) {
			return Text(yes)
		}
		return Text(no)
	}}
}

// Date is a column of dates: real date cells, not text, so they can be sorted
// and filtered. The zero time is an empty cell.
func Date[T any](header, format string, f func(T) time.Time) Column[T] {
	return Column[T]{Header: header, Format: format, Value: func(r T) Cell { return Time(f(r)) }}
}

// Sheet is a sheet as the Generator sees it: with no type parameter and with
// the rows in a sequence, so a big table is not loaded in memory.
type Sheet struct {
	// Name is the name of the sheet. The generator may change it if the format
	// asks for it (a spreadsheet limits the length and some characters).
	Name    string
	Columns []ColumnInfo
	// Rows gives the cells of each row, one per column. The slice is valid
	// until the next iteration: copy it to keep it. An error stops the
	// sequence and the generation fails with it.
	Rows iter.Seq2[[]Cell, error]
}

// NewSheet makes a Sheet from columns and rows of type T. Rows can be nil (a
// sheet with only the titles); the titles are written even if there are no rows. It
// panics if a column has no Value.
func NewSheet[T any](name string, cols []Column[T], rows iter.Seq2[T, error]) Sheet {
	infos := make([]ColumnInfo, len(cols))
	for i, c := range cols {
		if c.Value == nil {
			panic(fmt.Sprintf("excel: column %q has no Value", c.Header))
		}
		infos[i] = ColumnInfo{Header: c.Header, Format: c.Format, Width: c.Width}
	}
	return Sheet{
		Name:    name,
		Columns: infos,
		Rows: func(yield func([]Cell, error) bool) {
			if rows == nil {
				return
			}
			buf := make([]Cell, len(cols))
			for r, err := range rows {
				if err != nil {
					yield(nil, err)
					return
				}
				for i, c := range cols {
					buf[i] = c.Value(r)
				}
				if !yield(buf, nil) {
					return
				}
			}
		},
	}
}

// Slice is the sequence of the items of a slice, for NewSheet.
func Slice[T any](items []T) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for _, it := range items {
			if !yield(it, nil) {
				return
			}
		}
	}
}

// Workbook is a file with one or more sheets.
type Workbook struct {
	Sheets []Sheet
}

// Validate checks that there is a sheet, that each one has a name (different
// from the others, ignoring case) and at least one column.
func (wb Workbook) Validate() error {
	if len(wb.Sheets) == 0 {
		return fmt.Errorf("%w: no sheets", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, s := range wb.Sheets {
		name := strings.ToLower(strings.TrimSpace(s.Name))
		switch {
		case name == "":
			return fmt.Errorf("%w: a sheet has no name", ErrInvalid)
		case seen[name]:
			return fmt.Errorf("%w: two sheets are named %q", ErrInvalid, s.Name)
		case len(s.Columns) == 0:
			return fmt.Errorf("%w: sheet %q has no columns", ErrInvalid, s.Name)
		}
		seen[name] = true
	}
	return nil
}
