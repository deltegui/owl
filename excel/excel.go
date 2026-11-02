// Package excel is the contract to make spreadsheet files (.xlsx, or a CSV):
// what goes in them is described here, and writing the file is the job of a
// Generator, which the application provides (a library, a zip of XMLs...). This
// package only depends on files and the standard library.
//
// The columns of a sheet are explicit. Each Column[T] has a title and a function
// that reads one cell from a row of type T, so there is no reflection, no DTO
// only for the export and no flag to hide a column: the code that builds the
// slice of columns leaves out the ones that must not be there.
//
//	cols := []excel.Column[Entry]{
//		excel.Date(loc["Date"], "dd/mm/yyyy hh:mm", func(e Entry) time.Time { return e.At }),
//		excel.Str(loc["Concept"], func(e Entry) string { return e.Concept }),
//		excel.Money(loc["Amount"], func(e Entry) int64 { return e.Cents }),
//	}
//	sheet := excel.NewSheet("Entries", cols, excel.Slice(entries))
//	rel, err := svc.Save(ctx, excel.Workbook{Sheets: []excel.Sheet{sheet}}, "entries/entries.xlsx")
package excel

import (
	"context"
	"fmt"
	"io"

	"github.com/deltegui/owl/v3/files"
)

// Generator writes a Workbook in a format. The application implements it. It
// must read the rows one by one, so it does not load them all in memory, respect
// ctx, and not interpret the cells (a text that begins with "=" is text).
// When a sheet has more rows than the format allows (about a million for
// .xlsx) it continues in another one named "Name_2", "Name_3"...
type Generator interface {
	Generate(ctx context.Context, w io.Writer, wb Workbook) error
}

// Service writes the workbooks with a Generator in a files.Temp directory,
// and gives back the relative path to deliver the file. It is safe for
// concurrent use if the Generator is.
type Service struct {
	gen Generator
	tmp files.Temp
}

// NewService creates a Service. It panics if the generator is nil.
func NewService(gen Generator, tmp files.Temp) *Service {
	if gen == nil {
		panic("excel: NewService with a nil generator")
	}
	return &Service{gen: gen, tmp: tmp}
}

// Save generates the workbook and writes it in the Temp directory of the
// service. It returns the relative path where it was stored, which can be
// different from path (the directory may rename it): that is the one to give to
// whoever delivers the file. If the generation fails nothing is left.
func (s *Service) Save(ctx context.Context, wb Workbook, path string) (string, error) {
	return s.SaveTo(ctx, s.tmp, wb, path)
}

// SaveTo is like Save, but writes in other directory.
func (s *Service) SaveTo(ctx context.Context, dir files.Directory, wb Workbook, path string) (string, error) {
	if err := wb.Validate(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The generator writes in one end of a pipe while the directory reads the
	// other, so the file is never whole in memory.
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pw.CloseWithError(s.generate(ctx, pw, wb)) // nil closes with io.EOF
	}()
	rel, err := dir.Write(ctx, path, pr)
	_ = pr.CloseWithError(err) // if Write stopped early, this frees a generator blocked in a write
	<-done
	return rel, err
}

// generate calls the generator; a panic (in it or in a function of a column)
// is an error, because this runs in a goroutine of its own.
func (s *Service) generate(ctx context.Context, w io.Writer, wb Workbook) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("excel: the generator panicked: %v", r)
		}
	}()
	return s.gen.Generate(ctx, w, wb)
}
