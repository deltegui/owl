package files

// Temp is the contract for the directory where subsystems hand files to
// each other: the one that produces a file writes it here and passes the
// relative path that Write returned; the one that consumes it opens that
// path in the same Temp. Nobody needs to know where the files really are.
//
// It is a type of its own, not an alias, so a function can ask for it in its
// signature (and a dependency container can tell it apart from other
// directories). It does not need to be a Temporal: use one when the files
// must expire.
//
//	func NewPdfService(tmp files.Temp) *PdfService
//	tmp := files.AsTemp(temporal)
type Temp struct {
	Directory
}

// AsTemp marks a Directory as the temporal one. It panics if d is nil.
func AsTemp(d Directory) Temp {
	if d == nil {
		panic("files: AsTemp with a nil Directory")
	}
	return Temp{d}
}
