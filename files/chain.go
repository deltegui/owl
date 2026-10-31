package files

import (
	"context"
	"io"
)

// Chain builds a Directory that applies policies over other Directory.
type Chain struct {
	policies []Policy
	naming   NamingPolicy
	under    Directory
}

// Of starts a Chain over a Directory:
//
//	files.Of(local).Add(files.Folder("avatars")).Naming(files.GUID()).Build()
func Of(under Directory) *Chain {
	return &Chain{under: under}
}

// Add appends a policy. Policies run in the order they were added.
func (c *Chain) Add(p Policy) *Chain {
	c.policies = append(c.policies, p)
	return c
}

// Naming sets the naming policy, replacing the previous one.
func (c *Chain) Naming(n NamingPolicy) *Chain {
	c.naming = n
	return c
}

// Build returns the Directory. Later changes to the Chain do not affect it.
func (c *Chain) Build() Directory {
	return &built{
		policies: append([]Policy(nil), c.policies...),
		naming:   c.naming,
		under:    c.under,
	}
}

type built struct {
	policies []Policy
	naming   NamingPolicy
	under    Directory
}

func (b *built) Write(ctx context.Context, path string, content io.Reader) (string, error) {
	full, err := b.writePath(path)
	if err != nil {
		return "", err
	}
	return b.under.Write(ctx, full, content)
}

func (b *built) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	full, err := b.readPath(path)
	if err != nil {
		return nil, err
	}
	return b.under.Open(ctx, full)
}

func (b *built) Stat(ctx context.Context, path string) (Info, error) {
	full, err := b.readPath(path)
	if err != nil {
		return Info{}, err
	}
	return b.under.Stat(ctx, full)
}

func (b *built) Exists(ctx context.Context, path string) bool {
	full, err := b.readPath(path)
	return err == nil && b.under.Exists(ctx, full)
}

func (b *built) Delete(ctx context.Context, path string) error {
	full, err := b.readPath(path)
	if err != nil {
		return err
	}
	return b.under.Delete(ctx, full)
}

func (b *built) CopyTo(ctx context.Context, path, destiny string, other Directory) (string, error) {
	full, err := b.readPath(path)
	if err != nil {
		return "", err
	}
	return b.under.CopyTo(ctx, full, destiny, other)
}

// writePath asks every policy for its section, then adds the file path
// (renamed by the naming policy, if any). The result is checked as a whole,
// so a policy or a naming policy cannot make a path that climbs out.
func (b *built) writePath(path string) (string, error) {
	if _, err := split(path); err != nil {
		return "", err
	}
	p := forWriting()
	for _, policy := range b.policies {
		if err := policy.OnWrite(p); err != nil {
			return "", err
		}
	}
	if b.naming != nil {
		path = b.naming.Rename(path)
	}
	p.Enqueue(path)
	full := p.String()
	if _, err := split(full); err != nil {
		return "", err
	}
	return full, nil
}

// readPath checks the path with every policy. The path must still have
// something left after them: the file.
func (b *built) readPath(path string) (string, error) {
	p, err := forReading(path)
	if err != nil {
		return "", err
	}
	for _, policy := range b.policies {
		if err := policy.OnRead(p); err != nil {
			return "", err
		}
	}
	if p.Len() == 0 {
		return "", ErrNotFound
	}
	return path, nil
}
