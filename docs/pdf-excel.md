# PDF and Excel

Packages: `owl/pdf`, `owl/pdf/hxpdf`, `owl/excel`.

Both are **mechanisms with a contract**: they define how a document is described,
how it is saved and how the file is handed over, but the library that writes the
bytes is **yours** (headless Chrome for PDF, an xlsx library or a CSV writer for
Excel). That keeps OWL free of those dependencies.

The result of both is a **relative path in a [`files.Temp`](files.md#temp-handing-files-between-subsystems)**:
the code that makes the file returns the path, and whoever delivers it (a
download handler, a mail attachment) reads it from the same `Temp`.

## PDF

```
payload ──Renderer──▶ HTML + Options ──Driver──▶ PDF bytes ──Service──▶ path in Temp
          (hxpdf)                      (yours)
```

### Contracts

```go
type Renderer interface {
	Render(ctx context.Context, payload any, lang string) (Rendered, error)
}
type Driver interface {
	Render(ctx context.Context, req Request, tmp files.Temp) ([]byte, error)
}

type Rendered struct { HTML string; Options Options; Driver Driver }
type Request  struct { HTML string; Options Options; Payload any }
```

- A `Renderer` turns a payload into HTML and options. It does not make the PDF.
  `lang` can be empty.
- A `Driver` turns HTML into the bytes of a PDF. It must respect `ctx`.
  `Rendered.Driver`, when set by a template, wins over the service's driver.

### Options

```go
type Options struct {
	Format string // "A4", "A3", "Letter"... empty is the driver's default
	Landscape bool
	MarginTop, MarginBottom, MarginLeft, MarginRight string // CSS lengths: "1cm", "10mm"
	DisplayHeaderFooter bool
	NoBackground bool          // do not print backgrounds
	WaitForSelector string     // wait for an element (content a script builds, a chart)
	WaitSelectorTimeout int    // ms; zero lets the driver choose; if it is not there in time, it still renders
	NoWaitForNetworkIdle bool  // do not wait for the network to be idle
}
```

The zero value is a PDF with the driver's defaults for everything, with
backgrounds, that waits for the network to be idle (the booleans are inverted
from the usual so that zero works).

### Service

```go
svc := pdf.NewService(renderer, driver, tmp) // panics if renderer or driver is nil

bytes, err := svc.Render(ctx, payload, lang)          // the PDF bytes
rel, err := svc.Save(ctx, payload, "reports/r.pdf", lang) // writes in the Temp, returns the stored path
rel, err := svc.SaveTo(ctx, otherDir, payload, "r.pdf", lang)
```

- A `nil` payload is `pdf.ErrNoPayload`; an empty PDF is an error.
- `Save` returns the path **that the `Temp` gave**, which may differ from the one
  you asked for (a policy may rename it). That is the one to deliver.
- `Service` is safe for concurrent use if the renderer and driver are.

### hxpdf: the Hix renderer

```go
renderer := hxpdf.New(hxpdf.WithDefaultLang("es"))

hxpdf.Register(renderer, hxpdf.Template[ReportPdf]{
	Texts:   reportTexts, // localizer.Dict
	View:    func(r ReportPdf, t localizer.Localizer) hx.INode { return ... },
	Options: pdf.Options{Format: "A4", MarginTop: "1.5cm"},
})
```

```go
type Template[P any] struct {
	Texts   localizer.Source
	View    func(payload P, texts localizer.Localizer) hx.INode
	Options pdf.Options
	Driver  pdf.Driver // this document with another driver
}
```

- The template is chosen by the **type** of the payload (`P` or `*P`).
  `Register` panics if `View` is nil or the type already has a template.
- The language is `localizer.Pick(Texts, lang, defaultLang)`; with no `Texts` it is
  empty (keys show as themselves).
- A view that builds an `<html>` element is preceded by `<!doctype html>`. A panic
  or an empty view is an error. A payload with no template is `hxpdf.ErrNotRegistered`.
- Unlike `hxmail` there is no subject, no names for an outbox and no `cid:`.

### Writing a driver

A Chrome driver opens the HTML as `file://`, so it needs the HTML in a file.
`pdf.StageHTML` and `pdf.LocalPath` are for that:

```go
type chromeDriver struct{ base string } // the folder of the files.Local behind the Temp

func (d chromeDriver) Render(ctx context.Context, req pdf.Request, tmp files.Temp) ([]byte, error) {
	rel, cleanup, err := pdf.StageHTML(ctx, tmp, req.HTML) // writes tmp/<random>/view.html
	if err != nil {
		return nil, err
	}
	defer cleanup() // deletes it, even if ctx was cancelled

	path, err := pdf.LocalPath(d.base, rel) // absolute path on disk
	if err != nil {
		return nil, err
	}
	return printWithChrome(ctx, "file://"+path, req.Options)
}
```

A `files.Directory` does not say where its files are, so **the base folder is
given to the driver separately** (the `basePath` of the original). `LocalPath`
joins it with the relative path and returns `files.ErrAccessDenied` if the result
would leave the base (`..`, absolute or empty paths).

### Delivering the PDF

```go
func handleReport(svc *pdf.Service, tmp files.Temp) owl.Handler {
	return func(ctx owl.Ctx) error {
		rel, err := svc.Save(ctx.Context(), ReportPdf{...}, "report.pdf", ctx.GetCurrentLanguage())
		if err != nil {
			return err
		}
		f, err := tmp.Open(ctx.Context(), rel)
		if err != nil {
			return err
		}
		defer f.Close()
		ctx.Res.Header().Set("Content-Type", "application/pdf")
		_, err = io.Copy(ctx.Res, f)
		return err
	}
}
```

Make the `Temp` a `Temporal` wrapped with a `files.User` policy so nobody can read
someone else's PDFs.

## Excel

```
rows ──Columns──▶ Sheet ──▶ Workbook ──Generator──▶ .xlsx / .csv ──Service──▶ path in Temp
     (functions)                      (yours)
```

The package only depends on `files` and the standard library. The columns are
**explicit and functions**, not struct tags or reflection: which columns exist,
and in which order, is decided by the code that builds the slice. Hiding a column
from someone who cannot see it is simply **not adding it**; you need no DTO just to
export.

### Columns

```go
type Column[T any] struct {
	Header string              // already translated
	Value  func(T) Cell        // reads one cell from a row
	Format string              // spreadsheet notation: "dd/mm/yyyy hh:mm", "#,##0.00"; empty is general
	Width  float64             // in characters; zero lets the generator choose
}
```

One-liners for the common cases:

| Constructor | Column of | Notes |
|---|---|---|
| `excel.Str(header, f)` | text | |
| `excel.Num(header, f)` | `float64` | Not exact: do not use for money. |
| `excel.Int(header, f)` | `int64` | Written exactly. |
| `excel.Money(header, f)` | cents as `int64` | Exact, format `#,##0.00`. |
| `excel.Bool(header, f)` | `bool` | |
| `excel.YesNo(header, yes, no, f)` | `bool` as text | Words already translated. |
| `excel.Date(header, format, f)` | `time.Time` | A **real date cell**, not text, so it sorts and filters. The zero time is an empty cell. |

For anything else write the column yourself:
`excel.Column[E]{Header: "...", Value: func(e E) excel.Cell { ... }}`.

### Cells

`Cell` is a closed set so a `Generator` can handle every kind:

```go
excel.Blank()  excel.Text(s)  excel.Number(f)  excel.Decimal(unscaled, scale)
excel.Cents(n) excel.Boolean(b)  excel.Time(t)

c.Kind() // KindBlank, KindText, KindNumber, KindDecimal, KindBool, KindTime
c.Text(); c.Number(); c.Decimal() (unscaled, scale); c.Bool(); c.Time()
c.String() // no format, exact decimals ("12.34"), RFC 3339 times: what a CSV writes
```

`Decimal(1234, 2)` is `12.34` exactly; `Cents(1234)` is the same. A `Text` that
begins with `=` is **text**, never a formula (a generator must not interpret it).

### Sheets and workbooks

```go
cols := []excel.Column[Entry]{
	excel.Date(loc.Get("Date"), "dd/mm/yyyy hh:mm", func(e Entry) time.Time { return e.At }),
	excel.Str(loc.Get("Concept"), func(e Entry) string { return e.Concept }),
	excel.Money(loc.Get("Amount"), func(e Entry) int64 { return e.Cents }),
}
sheet := excel.NewSheet("Entries", cols, excel.Slice(entries))
wb := excel.Workbook{Sheets: []excel.Sheet{sheet}}
```

- `NewSheet(name, cols, rows iter.Seq2[T, error])` returns the `Sheet` the
  generator sees, with no type parameter. It **panics** if a column has no `Value`.
  `rows` may be nil: the sheet has only the titles (an empty export does not
  fail).
- `excel.Slice(items)` turns a slice into that sequence. A DAO with a cursor can
  pass its own sequence and never load everything in memory; an error in the
  sequence stops and fails the generation.
- The `[]Cell` of each row is **reused**: valid only until the next iteration. Copy
  it to keep it.
- `wb.Validate()` checks there is a sheet, names are non-empty and different
  (ignoring case) and each sheet has a column; errors wrap `excel.ErrInvalid`.

### Generator

```go
type Generator interface {
	Generate(ctx context.Context, w io.Writer, wb Workbook) error
}
```

You implement it. It must read rows **one by one** (do not load them all),
respect `ctx`, not interpret cells, and when a sheet exceeds the format's row limit
(about a million in `.xlsx`) continue in `Name_2`, `Name_3`... The sheet name may
be changed to fit the format's limits.

### Service

```go
svc := excel.NewService(generator, tmp) // panics if generator is nil
rel, err := svc.Save(ctx, wb, "entries/entries.xlsx")
rel, err = svc.SaveTo(ctx, otherDir, wb, "entries.xlsx")
```

`Save` validates, **generates through an `io.Pipe` while the `Temp` reads** (the
file is never fully in memory) and returns the path that the `Temp` stored, which
may differ from the one requested: deliver that one. If generation fails, no file
is left (`files.Local` writes to a temporary and renames). A panic in the
generator or in a column function is turned into an error (they run in another
goroutine), and if the `Temp` fails to write, the pipe is closed so the generator
is not left blocked.

### A CSV generator, as a starting point

```go
type csvGenerator struct{}

func (csvGenerator) Generate(ctx context.Context, w io.Writer, wb excel.Workbook) error {
	sheet := wb.Sheets[0]
	out := csv.NewWriter(w)
	header := make([]string, len(sheet.Columns))
	for i, c := range sheet.Columns {
		header[i] = c.Header
	}
	out.Write(header)
	for cells, err := range sheet.Rows {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		row := make([]string, len(cells))
		for i, c := range cells {
			row[i] = c.String()
		}
		out.Write(row)
	}
	out.Flush()
	return out.Error()
}
```

(A real generator must also guard text that starts with `=`, `+`, `-` or `@`
against spreadsheet formula injection when it targets CSV.)
