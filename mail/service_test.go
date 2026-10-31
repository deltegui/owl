package mail

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/deltegui/owl/v3/files"
)

// fakeRenderer gives what it is told and keeps the messages it gets.
type fakeRenderer struct {
	mu       sync.Mutex
	rendered Rendered
	err      error
	got      []Message
}

func (r *fakeRenderer) Render(_ context.Context, m Message) (Rendered, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, m)
	return r.rendered, r.err
}

// spyDriver keeps the requests and reads the resources from the Temp it is given.
type spyDriver struct {
	mu        sync.Mutex
	got       []Request
	resources []map[string]string // the content of the cids of each request
}

func (d *spyDriver) Send(ctx context.Context, req Request, tmp files.Temp) error {
	res := map[string]string{}
	for id, p := range req.CIDs {
		if tmp.Directory != nil {
			if s, err := files.ReadString(ctx, tmp, p); err == nil {
				res[id] = s
			}
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got = append(d.got, req)
	d.resources = append(d.resources, res)
	return nil
}

func (d *spyDriver) last(t *testing.T) (Request, map[string]string) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.got) == 0 {
		t.Fatal("the driver received nothing")
	}
	return d.got[len(d.got)-1], d.resources[len(d.resources)-1]
}

func okMessage() Message {
	return Message{
		Envelope: Envelope{To: []string{"ana@example.com"}, Attachments: []string{"tmp/report.pdf"}},
		Lang:     "en",
		Payload:  userConfirmation{Link: "https://example.com"},
	}
}

func TestServiceRendersAndGivesTheResultToTheDriver(t *testing.T) {
	r := &fakeRenderer{rendered: Rendered{Subject: "Hi", HTML: "<p>Hi</p>"}}
	d := &spyDriver{}
	var s Sender = NewService(r, d, files.Temp{})
	m := okMessage()
	if err := s.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	req, _ := d.last(t)
	if req.Subject != "Hi" || req.HTML != "<p>Hi</p>" || req.CIDs != nil {
		t.Errorf("%+v", req)
	}
	if req.Envelope.To[0] != "ana@example.com" || req.Envelope.Attachments[0] != "tmp/report.pdf" {
		t.Errorf("envelope %+v", req.Envelope)
	}
	if p, ok := req.Payload.(userConfirmation); !ok || p.Link == "" {
		t.Errorf("payload %#v", req.Payload)
	}
	if len(r.got) != 1 || r.got[0].Lang != "en" {
		t.Errorf("the renderer got %+v", r.got)
	}
}

func TestServiceStopsBeforeTheDriverOnErrors(t *testing.T) {
	boom := errors.New("no template")
	r := &fakeRenderer{err: boom}
	d := &spyDriver{}
	s := NewService(r, d, files.Temp{})
	ctx := context.Background()

	if err := s.Send(ctx, okMessage()); !errors.Is(err, boom) {
		t.Errorf("renderer error: %v", err)
	}
	// A message that is not valid does not even get to the renderer.
	before := len(r.got)
	if err := s.Send(ctx, Message{Payload: 1}); !errors.Is(err, ErrInvalid) || len(r.got) != before {
		t.Errorf("invalid: %v (renderer called: %v)", err, len(r.got) != before)
	}
	if len(d.got) != 0 {
		t.Fatal("the driver was called")
	}
}

func TestServiceNeedsARendererAndADriver(t *testing.T) {
	for name, f := range map[string]func(){
		"no renderer": func() { NewService(nil, &spyDriver{}, files.Temp{}) },
		"no driver":   func() { NewService(&fakeRenderer{}, nil, files.Temp{}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			f()
		}()
	}
}

func TestTheDriverOfTheRenderedEmailWins(t *testing.T) {
	own, def := &spyDriver{}, &spyDriver{}
	r := &fakeRenderer{rendered: Rendered{Subject: "s", HTML: "h", Driver: own}}
	if err := NewService(r, def, files.Temp{}).Send(context.Background(), okMessage()); err != nil {
		t.Fatal(err)
	}
	if len(own.got) != 1 || len(def.got) != 0 {
		t.Fatalf("own %d, default %d", len(own.got), len(def.got))
	}
}

func newLocal(t *testing.T) *files.Local {
	t.Helper()
	l, err := files.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestResourcesAreCopiedToTemp(t *testing.T) {
	ctx := context.Background()
	resources, tmpDir := newLocal(t), newLocal(t)
	files.WriteString(ctx, resources, "img/logo.png", "LOGO")
	files.WriteString(ctx, resources, "img/other.png", "OTHER")
	files.WriteString(ctx, resources, "img/special.png", "SPECIAL")
	// The Temp renames what it stores: the service must use the path it gives back.
	tmp := files.AsTemp(files.Of(tmpDir).Add(files.Folder("mail")).Naming(files.GUID()).Build())

	// The email asks for its own "logo": it wins over the one of the service.
	r := &fakeRenderer{rendered: Rendered{Subject: "s", HTML: "h", CIDs: map[string]string{"logo": "img/special.png"}}}
	d := &spyDriver{}
	s := NewService(r, d, tmp, WithResources(resources), WithCID("logo", "img/logo.png"), WithCID("other", "img/other.png"))
	if err := s.Send(ctx, okMessage()); err != nil {
		t.Fatal(err)
	}
	req, content := d.last(t)
	if len(req.CIDs) != 2 || !strings.HasPrefix(req.CIDs["logo"], "mail/") || strings.Contains(req.CIDs["logo"], "special") {
		t.Fatalf("cids %v", req.CIDs)
	}
	if content["logo"] != "SPECIAL" || content["other"] != "OTHER" {
		t.Fatalf("content in the Temp: %v", content)
	}
	// The next email gets its own copy.
	s.Send(ctx, okMessage())
	if again, _ := d.last(t); again.CIDs["other"] == req.CIDs["other"] {
		t.Error("two emails share a file in the Temp")
	}
	// What the renderer gave is not changed by the copy.
	if r.rendered.CIDs["logo"] != "img/special.png" {
		t.Error("the Rendered of the renderer was changed")
	}

	// A resource that does not exist is an error and nothing is sent.
	n := len(d.got)
	bad := NewService(&fakeRenderer{}, d, tmp, WithResources(resources), WithCID("x", "img/nope.png"))
	if err := bad.Send(ctx, okMessage()); !errors.Is(err, files.ErrNotFound) || len(d.got) != n {
		t.Fatalf("got %v", err)
	}
}

func TestCIDsAreTempPathsWithoutResources(t *testing.T) {
	ctx := context.Background()
	tmpDir := newLocal(t)
	files.WriteString(ctx, tmpDir, "logo.png", "LOGO")
	d := &spyDriver{}
	s := NewService(&fakeRenderer{}, d, files.AsTemp(tmpDir), WithCID("logo", "logo.png"))
	if err := s.Send(ctx, okMessage()); err != nil {
		t.Fatal(err)
	}
	req, content := d.last(t)
	if len(req.CIDs) != 1 || req.CIDs["logo"] != "logo.png" || content["logo"] != "LOGO" {
		t.Fatalf("cids %v, %v", req.CIDs, content)
	}
}

func TestServiceIsSafeForConcurrentUse(t *testing.T) {
	ctx := context.Background()
	resources, tmpDir := newLocal(t), newLocal(t)
	files.WriteString(ctx, resources, "logo.png", "LOGO")
	d := &spyDriver{}
	s := NewService(&fakeRenderer{rendered: Rendered{Subject: "s", HTML: "h"}}, d, files.AsTemp(tmpDir),
		WithResources(resources), WithCID("logo", "logo.png"))
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Send(ctx, okMessage()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(d.got) != 30 {
		t.Fatalf("%d emails", len(d.got))
	}
	seen := map[string]bool{}
	for i, req := range d.got {
		if d.resources[i]["logo"] != "LOGO" || seen[req.CIDs["logo"]] {
			t.Fatalf("email %d: %v %v", i, req.CIDs, d.resources[i])
		}
		seen[req.CIDs["logo"]] = true
	}
}
