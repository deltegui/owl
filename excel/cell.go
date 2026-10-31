package excel

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Kind is the type of the value of a Cell.
type Kind uint8

// The kinds of cell. The zero value of Cell is KindBlank.
const (
	KindBlank Kind = iota
	KindText
	KindNumber
	KindDecimal
	KindBool
	KindTime
)

// Cell is the value of a cell of a sheet. The kinds are a closed set, so a
// Generator can handle all of them. Make one with Text, Number, Decimal, Cents,
// Boolean, Time or Blank.
type Cell struct {
	kind Kind
	s    string
	n    float64
	i    int64
	scl  int
	b    bool
	t    time.Time
}

// Blank is an empty cell.
func Blank() Cell { return Cell{} }

// Text is a cell with text. The generator must not interpret it (a text that
// begins with "=" is text, not a formula).
func Text(s string) Cell { return Cell{kind: KindText, s: s} }

// Number is a cell with a floating point number. For money or anything that
// has to be exact, use Decimal.
func Number(n float64) Cell { return Cell{kind: KindNumber, n: n} }

// Decimal is an exact number: unscaled / 10^scale, so Decimal(1234, 2) is 12.34.
// It is how an amount in cents is written without losing precision. A
// negative scale is zero.
func Decimal(unscaled int64, scale int) Cell {
	return Cell{kind: KindDecimal, i: unscaled, scl: max(scale, 0)}
}

// Cents is an amount in cents: Decimal with two decimals.
func Cents(cents int64) Cell { return Decimal(cents, 2) }

// Boolean is a cell with a boolean.
func Boolean(b bool) Cell { return Cell{kind: KindBool, b: b} }

// Time is a cell with a date and time. The zero time is a Blank cell.
func Time(t time.Time) Cell {
	if t.IsZero() {
		return Blank()
	}
	return Cell{kind: KindTime, t: t}
}

// Kind says which kind of value the cell has.
func (c Cell) Kind() Kind { return c.kind }

// Text is the text of a KindText cell, and "" for the other kinds. See String.
func (c Cell) Text() string { return c.s }

// Number is the value of a KindNumber cell, or the closest float of a
// KindDecimal one. It is zero for the other kinds.
func (c Cell) Number() float64 {
	switch c.kind {
	case KindNumber:
		return c.n
	case KindDecimal:
		return float64(c.i) / math.Pow10(c.scl)
	}
	return 0
}

// Decimal is the unscaled value and the scale of a KindDecimal cell.
func (c Cell) Decimal() (unscaled int64, scale int) { return c.i, c.scl }

// Bool is the value of a KindBool cell.
func (c Cell) Bool() bool { return c.b }

// Time is the value of a KindTime cell.
func (c Cell) Time() time.Time { return c.t }

// String is the value as text, with no format: what a CSV generator writes.
// Decimals are exact ("12.34"), booleans are "true" or "false" and times are
// RFC 3339.
func (c Cell) String() string {
	switch c.kind {
	case KindText:
		return c.s
	case KindNumber:
		return strconv.FormatFloat(c.n, 'f', -1, 64)
	case KindDecimal:
		return formatDecimal(c.i, c.scl)
	case KindBool:
		return strconv.FormatBool(c.b)
	case KindTime:
		return c.t.Format(time.RFC3339)
	}
	return ""
}

func formatDecimal(unscaled int64, scale int) string {
	neg := unscaled < 0
	u := uint64(unscaled)
	if neg {
		u = -u // right even for the minimum int64
	}
	digits := strconv.FormatUint(u, 10)
	if scale > 0 {
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	if neg {
		return "-" + digits
	}
	return digits
}
