package files

// Policy decides where files go and who can reach them. A Policy is
// symmetric: what OnWrite adds to the path, OnRead takes back.
//
// OnWrite adds its section to the path. OnRead takes its section from the
// start of the path and returns ErrAccessDenied if it is not valid for the
// caller. The sections are applied in the order the policies were added.
type Policy interface {
	OnWrite(path *Path) error
	OnRead(path *Path) error
}

// NamingPolicy changes the name of a file when it is written. It receives
// the path the caller gave (with its own subfolders, if any) and returns the
// path to use, in the same folder. It does not run when reading.
type NamingPolicy interface {
	Rename(original string) string
}
