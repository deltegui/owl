// Package pdf is the mechanism to make PDF files: a Renderer turns a payload
// into HTML and the options of the document, a Driver turns that HTML into
// the bytes of the PDF (a headless Chrome, for example) and the Service
// puts both together and writes the result in a files.Temp directory, to hand
// the relative path to whoever has to deliver the file.
//
// There is no driver here: the one with Chrome lives in the application, or in
// its own package, so this one does not import anything but files. The
// Renderer written with Hix is in package hxpdf.
package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/deltegui/owl/v3/files"
)

// ErrNoPayload is returned when there is nothing to render.
var ErrNoPayload = errors.New("pdf: nil payload")

// Options are how the document is made. The zero value is a PDF with the
// default of the driver for everything, with backgrounds, and that waits for
// the network to be idle.
type Options struct {
	// Format is the paper: "A3", "A4", "A5", "Letter"... Empty is the one of the driver.
	Format    string
	Landscape bool
	// Margins are CSS lengths ("1cm", "10mm").
	MarginTop, MarginBottom, MarginLeft, MarginRight string
	DisplayHeaderFooter                              bool
	// NoBackground does not print the backgrounds (colors and images).
	NoBackground bool
	// WaitForSelector makes the driver wait until an element matches, for
	// content that a script builds (a chart). Empty does not wait.
	WaitForSelector string
	// WaitSelectorTimeout is how long to wait for the selector, in milliseconds.
	// Zero is for the driver to choose. If it is not there in time the
	// driver still renders the page.
	WaitSelectorTimeout int
	// NoWaitForNetworkIdle renders without waiting for the network to be idle.
	NoWaitForNetworkIdle bool
}

// Rendered is the content of a document, ready to become a PDF.
type Rendered struct {
	HTML    string
	Options Options
	// Driver makes this document instead of the one of the Service, if not nil.
	Driver Driver
}

// Renderer turns a payload into the HTML of the document. hxpdf.Renderer is
// one; another could use html/template. It does not make the PDF. lang can be empty.
type Renderer interface {
	Render(ctx context.Context, payload any, lang string) (Rendered, error)
}

// Request is what a Driver gets.
type Request struct {
	HTML    string
	Options Options
	Payload any
}

// Driver makes the PDF from the HTML and returns its bytes. If it needs the
// HTML in a file (Chrome opens it with file://), it writes it in tmp: see
// StageHTML and LocalPath. It must respect ctx.
type Driver interface {
	Render(ctx context.Context, req Request, tmp files.Temp) ([]byte, error)
}

// Service renders payloads to PDF and stores them. It is safe for concurrent
// use if the Renderer and the Driver are.
type Service struct {
	renderer Renderer
	driver   Driver
	tmp      files.Temp
}

// NewService creates a Service. tmp is where Save writes the documents, and
// where the driver can leave its files. It panics if the renderer or the
// driver is nil.
func NewService(renderer Renderer, driver Driver, tmp files.Temp) *Service {
	if renderer == nil || driver == nil {
		panic("pdf: NewService with a nil renderer or driver")
	}
	return &Service{renderer: renderer, driver: driver, tmp: tmp}
}

// Render makes the PDF and returns its bytes. lang can be empty.
func (s *Service) Render(ctx context.Context, payload any, lang string) ([]byte, error) {
	if payload == nil {
		return nil, ErrNoPayload
	}
	r, err := s.renderer.Render(ctx, payload, lang)
	if err != nil {
		return nil, err
	}
	driver := s.driver
	if r.Driver != nil {
		driver = r.Driver
	}
	b, err := driver.Render(ctx, Request{HTML: r.HTML, Options: r.Options, Payload: payload}, s.tmp)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("pdf: the driver made an empty document for %T", payload)
	}
	return b, nil
}

// Save makes the PDF and writes it in the Temp directory of the service. It
// returns the relative path where it was stored, which can be different from
// path (the directory may rename it): that is the one to give to whoever
// delivers the file.
func (s *Service) Save(ctx context.Context, payload any, path, lang string) (string, error) {
	return s.SaveTo(ctx, s.tmp, payload, path, lang)
}

// SaveTo is like Save, but writes in other directory.
func (s *Service) SaveTo(ctx context.Context, dir files.Directory, payload any, path, lang string) (string, error) {
	b, err := s.Render(ctx, payload, lang)
	if err != nil {
		return "", err
	}
	return dir.Write(ctx, path, bytes.NewReader(b))
}
