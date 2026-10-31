package files

import "strings"

const separator = "/"

// Path is the path being built (when writing) or checked (when reading)
// by the policies of a directory. It is a queue of sections: on write each
// policy enqueues its section, and on read each policy dequeues its own.
type Path struct {
	sections []string
}

func forWriting() *Path { return &Path{} }

// forReading parses a path given by the user. It fails with ErrAccessDenied
// if it is not a clean relative path, so a policy that checks the first
// sections can trust that the rest cannot climb out of them.
func forReading(path string) (*Path, error) {
	sections, err := split(path)
	if err != nil {
		return nil, err
	}
	return &Path{sections}, nil
}

func split(path string) ([]string, error) {
	if path == "" {
		return nil, ErrAccessDenied
	}
	sections := strings.Split(path, separator)
	for _, s := range sections {
		if !validSection(s) {
			return nil, ErrAccessDenied
		}
	}
	return sections, nil
}

func validSection(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "/\\\x00")
}

// Enqueue adds a section at the end of the path. The section is checked
// when the path is built, not here.
func (p *Path) Enqueue(section string) {
	p.sections = append(p.sections, section)
}

// Dequeue removes and returns the first section. The bool is false if
// there are no sections left (the path is shorter than the policies).
func (p *Path) Dequeue() (string, bool) {
	if len(p.sections) == 0 {
		return "", false
	}
	s := p.sections[0]
	p.sections = p.sections[1:]
	return s, true
}

// Len returns the number of sections left.
func (p *Path) Len() int { return len(p.sections) }

func (p *Path) String() string { return strings.Join(p.sections, separator) }
