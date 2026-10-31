package mail

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"path"

	"github.com/deltegui/owl/v3/files"
)

// Renderer turns a Message into the content of the email: the subject and the
// HTML. hxmail.Renderer is one, written with Hix; another could use html/template
// or give an HTML that is already made. It does not send anything.
type Renderer interface {
	Render(ctx context.Context, msg Message) (Rendered, error)
}

// Rendered is the content of an email, ready to be sent.
type Rendered struct {
	Subject string
	HTML    string
	// CIDs are the resources this email uses with "cid:", for the ones that are not
	// for all the emails: the key is the content id and the value is a path, with
	// the rules of WithCID. They win over the ones of the Service.
	CIDs map[string]string
	// Driver sends this email instead of the one of the Service, if it is not nil.
	Driver Driver
}

// Service is the Sender that sends for real: it asks a Renderer for the content
// of the message, adds the resources and gives the result to a Driver. It is
// the end of the chain: Sender wrappers (package queue and package outbox) end
// in one. It is safe for concurrent use if the Renderer and the Driver are.
type Service struct {
	renderer  Renderer
	driver    Driver
	tmp       files.Temp
	resources files.Directory
	cids      map[string]string
}

var _ Sender = (*Service)(nil)

// ServiceOption changes how a Service works.
type ServiceOption func(*Service)

// WithResources sets the directory where the resources of the emails (a logo...)
// are. See WithCID.
func WithResources(dir files.Directory) ServiceOption { return func(s *Service) { s.resources = dir } }

// WithCID adds a resource used by all the emails, a logo for example: the views
// write it as <img src="cid:id">. The path is relative to the resources
// directory (WithResources). Without one, it is a path in the Temp directory.
func WithCID(id, path string) ServiceOption { return func(s *Service) { s.cids[id] = path } }

// NewService creates a Service. tmp is where the driver finds the attachments
// and the resources. It panics if the renderer or the driver is nil.
func NewService(renderer Renderer, driver Driver, tmp files.Temp, opts ...ServiceOption) *Service {
	if renderer == nil || driver == nil {
		panic("mail: NewService with a nil renderer or driver")
	}
	s := &Service{renderer: renderer, driver: driver, tmp: tmp, cids: map[string]string{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Send checks the message, renders it and gives it to the driver. It returns
// what the driver returns, so a nil error means what it means for that driver.
func (s *Service) Send(ctx context.Context, msg Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	r, err := s.renderer.Render(ctx, msg)
	if err != nil {
		return err
	}
	cids, err := s.resolveCIDs(ctx, r)
	if err != nil {
		return err
	}
	driver := s.driver
	if r.Driver != nil {
		driver = r.Driver
	}
	return driver.Send(ctx, Request{
		Subject:  r.Subject,
		HTML:     r.HTML,
		Envelope: msg.Envelope,
		CIDs:     cids,
		Payload:  msg.Payload,
	}, s.tmp)
}

// resolveCIDs joins the resources of the service and the email. If there is a
// resources directory they are copied to the Temp directory, where the driver
// reads them: a Temp that expires (files.Temporal) would delete a logo that was
// put there once.
func (s *Service) resolveCIDs(ctx context.Context, r Rendered) (map[string]string, error) {
	if len(s.cids) == 0 && len(r.CIDs) == 0 {
		return nil, nil
	}
	cids := maps.Clone(s.cids)
	maps.Copy(cids, r.CIDs)
	if s.resources == nil {
		return cids, nil
	}
	for id, p := range cids {
		dest := rand.Text() + "/" + path.Base(p) // a folder of its own: sends do not share files
		stored, err := s.resources.CopyTo(ctx, p, dest, s.tmp)
		if err != nil {
			return nil, fmt.Errorf("mail: resource %q (cid %s): %w", p, id, err)
		}
		cids[id] = stored
	}
	return cids, nil
}
