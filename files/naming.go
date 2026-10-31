package files

import (
	"crypto/rand"
	"fmt"
	"path"
	"strings"
	"time"
)

// DefaultStampLayout is the layout of the time stamps of TimeStamp and
// NameWithTimeStamp: 20261031153045.123 (the last digits are milliseconds).
const DefaultStampLayout = "20060102150405.000"

// GUID is a naming policy that names the file with a random GUID (UUID v4)
// and keeps the folder and the extension: "a/b/photo.png" becomes
// "a/b/0b8c2f4e-....png".
func GUID() NamingPolicy { return guid{} }

type guid struct{}

func (guid) Rename(original string) string {
	dir, _, ext := splitName(original)
	return joinName(dir, newGUID()+ext)
}

func newGUID() string {
	var b [16]byte
	rand.Read(b[:]) // never fails (Go 1.24)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// TimeStamp is a naming policy that adds the time to the name of the file:
// "a/report.pdf" becomes "a/report_20261031153045.123.pdf". If layout is
// empty, DefaultStampLayout is used. If now is nil, time.Now is used.
func TimeStamp(now func() time.Time, layout string) NamingPolicy {
	return stamped{now: clock(now), layout: stampLayout(layout)}
}

// NameWithTimeStamp is a naming policy that ignores the name of the file and
// uses name plus the time: with "invoice", "a/x.pdf" becomes
// "a/invoice_20261031153045.123.pdf". If layout is empty, DefaultStampLayout
// is used. If now is nil, time.Now is used.
func NameWithTimeStamp(now func() time.Time, name, layout string) NamingPolicy {
	return stamped{now: clock(now), layout: stampLayout(layout), name: &name}
}

type stamped struct {
	now    func() time.Time
	layout string
	name   *string // nil: keep the name of the file
}

func (s stamped) Rename(original string) string {
	dir, base, ext := splitName(original)
	if s.name != nil {
		base = *s.name
	}
	return joinName(dir, base+"_"+s.now().Format(s.layout)+ext)
}

func stampLayout(layout string) string {
	if layout == "" {
		return DefaultStampLayout
	}
	return layout
}

// splitName splits "a/b/photo.png" in "a/b", "photo" and ".png".
func splitName(original string) (dir, base, ext string) {
	dir, file := path.Split(original)
	ext = path.Ext(file)
	return strings.TrimSuffix(dir, "/"), strings.TrimSuffix(file, ext), ext
}

func joinName(dir, file string) string {
	if dir == "" {
		return file
	}
	return dir + "/" + file
}
