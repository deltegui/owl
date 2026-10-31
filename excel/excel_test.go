package excel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/deltegui/owl/v3/files"
)

type entry struct {
	At       time.Time
	Concept  string
	Cents    int64
	Done     bool
	Internal string
}

func columns(isAdmin bool) []Column[entry] {
	cols := []Column[entry]{
		Date("Date", "dd/mm/yyyy", func(e entry) time.Time { return e.At }),
		Str("Concept", func(e entry) string { return e.Concept }),
		Money("Amount", func(e entry) int64 { return e.Cents }),
		YesNo("Done", "Sí", "No", func(e entry) bool { return e.Done }),
	}
	if isAdmin {
		cols = append(cols, Str("Internal", func(e entry) string { return e.Internal }))
	}
	return cols
}

// csvGenerator is the kind of generator an application writes: titles, then rows.
type csvGenerator struct{}

func (csvGenerator) Generate(ctx context.Context, w io.Writer, wb Workbook) error {
	for _, s := range wb.Sheets {
		fmt.Fprintf(w, "# %s\n", s.Name)
		heads := make([]string, len(s.Columns))
		for i, c := range s.Columns {
			heads[i] = c.Header
		}
		fmt.Fprintln(w, strings.Join(heads, ","))
		for cells, err := range s.Rows {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			parts := make([]string, len(cells))
			for i, c := range cells {
				parts[i] = c.String()
			}
			fmt.Fprintln(w, strings.Join(parts, ","))
		}
	}
	return nil
}

func newTmp(t *testing.T) (files.Temp, string) {
	t.Helper()
	base := t.TempDir()
	l, err := files.NewLocal(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return files.AsTemp(l), base
}

func TestCellStrings(t *testing.T) {
	at := time.Date(2026, 10, 31, 15, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		c    Cell
		kind Kind
		want string
	}{
		{Blank(), KindBlank, ""},
		{Text("=1+1"), KindText, "=1+1"},
		{Number(1.5), KindNumber, "1.5"},
		{Cents(1234), KindDecimal, "12.34"},
		{Cents(5), KindDecimal, "0.05"},
		{Cents(-5), KindDecimal, "-0.05"},
		{Cents(-123456), KindDecimal, "-1234.56"},
		{Decimal(7, 0), KindDecimal, "7"},
		{Decimal(7, -3), KindDecimal, "7"},
		{Decimal(-1<<63, 2), KindDecimal, "-92233720368547758.08"},
		{Boolean(true), KindBool, "true"},
		{Time(at), KindTime, "2026-10-31T15:30:00Z"},
		{Time(time.Time{}), KindBlank, ""},
	} {
		if tc.c.Kind() != tc.kind || tc.c.String() != tc.want {
			t.Errorf("%#v: kind %v, %q; want %v, %q", tc.c, tc.c.Kind(), tc.c.String(), tc.kind, tc.want)
		}
	}
	if got := Cents(1234).Number(); got != 12.34 {
		t.Errorf("Number of a decimal: %v", got)
	}
	if u, s := Cents(1234).Decimal(); u != 1234 || s != 2 {
		t.Errorf("Decimal: %d, %d", u, s)
	}
}

func TestSaveWritesTitlesAndRows(t *testing.T) {
	ctx := context.Background()
	tmp, _ := newTmp(t)
	svc := NewService(csvGenerator{}, tmp)
	rows := []entry{
		{At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Concept: "Café", Cents: 250, Done: true, Internal: "x"},
		{Concept: "Sin fecha", Cents: -100, Internal: "y"},
	}
	sheet := NewSheet("Entries", columns(false), Slice(rows))
	rel, err := svc.Save(ctx, Workbook{Sheets: []Sheet{sheet}}, "entries/e.csv")
	if err != nil {
		t.Fatal(err)
	}
	got, err := files.ReadString(ctx, tmp, rel)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Entries\nDate,Concept,Amount,Done\n2026-01-02T00:00:00Z,Café,2.50,Sí\n,Sin fecha,-1.00,No\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestHiddenColumnIsNotAdded(t *testing.T) {
	rows := []entry{{Concept: "a", Internal: "secret"}}
	for _, admin := range []bool{false, true} {
		s := NewSheet("S", columns(admin), Slice(rows))
		var cells []Cell
		for c := range s.Rows {
			cells = append(cells, c...)
		}
		if has := len(s.Columns) == 5; has != admin || (len(cells) == 5) != admin {
			t.Errorf("admin=%v: %d columns, %d cells", admin, len(s.Columns), len(cells))
		}
	}
}

func TestEmptyRowsKeepTitles(t *testing.T) {
	ctx := context.Background()
	tmp, _ := newTmp(t)
	svc := NewService(csvGenerator{}, tmp)
	for _, rows := range []func(yield func(entry, error) bool){nil, Slice([]entry(nil))} {
		rel, err := svc.Save(ctx, Workbook{Sheets: []Sheet{NewSheet("S", columns(false), rows)}}, "e.csv")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := files.ReadString(ctx, tmp, rel); got != "# S\nDate,Concept,Amount,Done\n" {
			t.Fatalf("%q", got)
		}
	}
}

func TestRowsError(t *testing.T) {
	ctx := context.Background()
	tmp, _ := newTmp(t)
	svc := NewService(csvGenerator{}, tmp)
	boom := errors.New("cursor died")
	rows := func(yield func(entry, error) bool) {
		if !yield(entry{Concept: "ok"}, nil) {
			return
		}
		yield(entry{}, boom)
	}
	_, err := svc.Save(ctx, Workbook{Sheets: []Sheet{NewSheet("S", columns(false), rows)}}, "e.csv")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if tmp.Exists(ctx, "e.csv") {
		t.Fatal("a failed generation left a file")
	}
}

type panicGenerator struct{}

func (panicGenerator) Generate(context.Context, io.Writer, Workbook) error { panic("bad") }

type blockedGenerator struct{ stopped chan error }

// writes forever: only a closed pipe stops it
func (g blockedGenerator) Generate(_ context.Context, w io.Writer, _ Workbook) error {
	buf := make([]byte, 1<<16)
	for {
		if _, err := w.Write(buf); err != nil {
			g.stopped <- err
			return err
		}
	}
}

func TestGeneratorPanicIsAnError(t *testing.T) {
	tmp, _ := newTmp(t)
	svc := NewService(panicGenerator{}, tmp)
	_, err := svc.Save(context.Background(), Workbook{Sheets: []Sheet{NewSheet("S", columns(false), nil)}}, "e.csv")
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteFailureFreesTheGenerator(t *testing.T) {
	g := blockedGenerator{stopped: make(chan error, 1)}
	svc := NewService(g, files.AsTemp(failingDir{}))
	_, err := svc.Save(context.Background(), Workbook{Sheets: []Sheet{NewSheet("S", columns(false), nil)}}, "e.csv")
	if err == nil {
		t.Fatal("no error")
	}
	select {
	case <-g.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the generator is still blocked")
	}
}

// failingDir reads a little and fails, like a full disk.
type failingDir struct{ files.Directory }

func (failingDir) Write(_ context.Context, _ string, r io.Reader) (string, error) {
	_, _ = r.Read(make([]byte, 10))
	return "", errors.New("disk full")
}

func TestValidate(t *testing.T) {
	good := NewSheet("A", columns(false), nil)
	for name, wb := range map[string]Workbook{
		"no sheets":  {},
		"no name":    {Sheets: []Sheet{{Columns: good.Columns}}},
		"repeated":   {Sheets: []Sheet{good, NewSheet("a", columns(false), nil)}},
		"no columns": {Sheets: []Sheet{{Name: "B"}}},
		"blank name": {Sheets: []Sheet{{Name: "  ", Columns: good.Columns}}},
	} {
		if err := wb.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := (Workbook{Sheets: []Sheet{good}}).Validate(); err != nil {
		t.Fatal(err)
	}
	svc := NewService(csvGenerator{}, files.Temp{})
	if _, err := svc.Save(context.Background(), Workbook{}, "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Save with an invalid workbook: %v", err)
	}
}

func TestConstructorsPanic(t *testing.T) {
	for name, fn := range map[string]func(){
		"nil Value":     func() { NewSheet("S", []Column[int]{{Header: "x"}}, nil) },
		"nil generator": func() { NewService(nil, files.Temp{}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			fn()
		}()
	}
}
