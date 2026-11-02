package excel

import (
	"math"
	"testing"
	"time"
)

func TestDecimalStringsAreExact(t *testing.T) {
	for _, tc := range []struct {
		unscaled int64
		scale    int
		want     string
	}{
		{0, 2, "0.00"},
		{0, 0, "0"},
		{5, 2, "0.05"},
		{12, 2, "0.12"}, // as many digits as decimals: the zero before the point is needed
		{123, 2, "1.23"},
		{-1, 2, "-0.01"},
		{-12, 2, "-0.12"},
		{-123, 2, "-1.23"},
		{125, 1, "12.5"},
		{7, 1, "0.7"},
		{7, 0, "7"},
		{1234567, 0, "1234567"},
		{5, -3, "5"}, // a negative scale is zero
		{math.MaxInt64, 2, "92233720368547758.07"},
		{math.MinInt64, 2, "-92233720368547758.08"},
		{math.MinInt64, 0, "-9223372036854775808"},
	} {
		if got := Decimal(tc.unscaled, tc.scale).String(); got != tc.want {
			t.Errorf("Decimal(%d, %d) = %q, want %q", tc.unscaled, tc.scale, got, tc.want)
		}
	}
	if got := Cents(-1999).String(); got != "-19.99" {
		t.Errorf("Cents = %q", got)
	}
}

func TestDecimalScaleIsNeverNegative(t *testing.T) {
	for _, scale := range []int{-1, -3, -100} {
		if _, got := Decimal(5, scale).Decimal(); got != 0 {
			t.Errorf("Decimal(5, %d) has scale %d", scale, got)
		}
	}
	if u, s := Decimal(1234, 2).Decimal(); u != 1234 || s != 2 {
		t.Errorf("Decimal() = %d, %d", u, s)
	}
}

func TestNumberAsAFloat(t *testing.T) {
	if got := Number(1.5).Number(); got != 1.5 {
		t.Errorf("Number(1.5).Number() = %v", got)
	}
	if got := Decimal(1234, 2).Number(); math.Abs(got-12.34) > 1e-12 {
		t.Errorf("Decimal(1234, 2).Number() = %v", got)
	}
	for name, c := range map[string]Cell{
		"text": Text("7"), "bool": Boolean(true), "time": Time(time.Now()), "blank": Blank(),
	} {
		if got := c.Number(); got != 0 {
			t.Errorf("%s: Number() = %v, want 0", name, got)
		}
	}
}

func TestNumberStrings(t *testing.T) {
	for n, want := range map[float64]string{0: "0", 3: "3", 0.1: "0.1", -2.5: "-2.5", 1e21: "1000000000000000000000"} {
		if got := Number(n).String(); got != want {
			t.Errorf("Number(%v) = %q, want %q", n, got, want)
		}
	}
}
