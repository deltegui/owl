package files

import (
	"fmt"
	"time"
)

// Folder is a policy that puts the files inside a folder. With several
// sections, they are nested in that order. When reading, the path must start
// with them.
func Folder(sections ...string) Policy { return folder(sections) }

// SystemUser is a policy for files that do not belong to any user: they go
// in the folder "0", the same as User does for the user with id 0.
func SystemUser() Policy { return folder{"0"} }

type folder []string

func (f folder) OnWrite(p *Path) error {
	for _, s := range f {
		p.Enqueue(s)
	}
	return nil
}

func (f folder) OnRead(p *Path) error {
	for _, s := range f {
		if got, ok := p.Dequeue(); !ok || got != s {
			return ErrAccessDenied
		}
	}
	return nil
}

// User is a policy that gives every user its own folder, named after the
// id that current returns (it is called on every operation, so it can read it
// from the request). When reading, the path must start with the folder of the
// current user, otherwise it is ErrAccessDenied: nobody can reach the files
// of another user. An id that is not a safe folder name (empty, with '/') is
// ErrAccessDenied when writing.
func User[ID any](current func() ID) Policy { return user[ID]{current} }

type user[ID any] struct{ current func() ID }

func (u user[ID]) OnWrite(p *Path) error {
	id := fmt.Sprint(u.current())
	if !validSection(id) { // it could not be read back
		return ErrAccessDenied
	}
	p.Enqueue(id)
	return nil
}

func (u user[ID]) OnRead(p *Path) error {
	if got, ok := p.Dequeue(); !ok || got != fmt.Sprint(u.current()) {
		return ErrAccessDenied
	}
	return nil
}

const dateLayout = "20060102"

// Date is a policy that puts the files in a folder with the date of the
// write, like 20261031. When reading, the path must have a valid date there:
// it can be any day, not only today.
func Date(now func() time.Time) Policy { return date{clock(now)} }

type date struct{ now func() time.Time }

func (d date) OnWrite(p *Path) error {
	p.Enqueue(d.now().Format(dateLayout))
	return nil
}

func (d date) OnRead(p *Path) error {
	got, ok := p.Dequeue()
	if !ok {
		return ErrAccessDenied
	}
	if _, err := time.Parse(dateLayout, got); err != nil {
		return ErrAccessDenied
	}
	return nil
}

// clock returns time.Now when now is nil.
func clock(now func() time.Time) func() time.Time {
	if now == nil {
		return time.Now
	}
	return now
}
