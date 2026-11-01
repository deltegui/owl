# Files

Package `github.com/deltegui/owl/v3/files`. Standard library only.

It stores and reads files by **relative path**, with *policies* that decide where
files go and who can reach them (a folder per user, per date, a random name...).
It is a port of the virtual directories of Kura.

A `Directory` knows nothing about URLs or about where files really are. The same
code works over a local folder, and could work over object storage.

## Directory

```go
type Directory interface {
	Write(ctx context.Context, path string, content io.Reader) (string, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Stat(ctx context.Context, path string) (Info, error)
	Exists(ctx context.Context, path string) bool
	Delete(ctx context.Context, path string) error
	CopyTo(ctx context.Context, path, destiny string, other Directory) (string, error)
}

type Info struct { Name string; Size int64 }
```

- Paths are relative and use `/` as separator.
- **`Write` returns the path where the content was really stored**, which can
  differ from the one requested when the directory has policies. That returned
  path is the one to keep and to read with later.
- `Open` returns a `ReadCloser`: close it.
- `CopyTo` copies a file to `destiny` in another `Directory` and returns the path
  where that one stored it.

Helpers for small files:

```go
files.WriteBytes(ctx, dir, "a/b.txt", []byte("..."))   // returns the stored path
files.WriteString(ctx, dir, "a/b.txt", "...")
files.ReadBytes(ctx, dir, path)
files.ReadString(ctx, dir, path)
```

Errors: `files.ErrNotFound` (no such file, or the path does not point to a file) and
`files.ErrAccessDenied` (a policy rejected the path, or the path is not valid).

### Valid paths

Every path, for reading and writing, is checked **before** the policies run, and
the final path is checked again after them. A path is rejected
(`ErrAccessDenied`) if it has empty sections, `.` or `..` sections, a backslash, a
NUL byte, or a `/` inside a section. So neither a caller nor a policy or naming
policy can take a path out of its place.

## Backends

### Local

```go
dir, err := files.NewLocal("/var/app/uploads") // creates the folder if missing
defer dir.Close()
```

Stores files in a folder of the file system. It uses `os.Root`, so it cannot read
or write outside that folder **even through symbolic links**.

- `Write` writes into a temporary file (`.tmp-...`) and renames it, so a failed
  write or a cancelled context never leaves a half-written file.
- `Delete` only deletes files, not folders.
- Permissions: folders `0750`, files `0640`.
- `Close()` releases the folder; do not use the `Local` after it.

### Temporal

```go
tmp, err := files.NewTemporal("/var/app/tmp", 24*time.Hour,
	files.WithCleanEvery(10*time.Minute),
)
```

A `Local` whose files **expire**: a file whose modification time is older than
`maxAge` is deleted, and so are folders left empty. Cleanup happens when the
directory is used, at most once per interval (a minute by default): there are no
goroutines to stop. `maxAge` must be positive.

| Option | Meaning |
|---|---|
| `WithCleanEvery(d)` | Minimum time between two cleanups. |
| `WithClock(now)` | Clock used to decide if a file is old (for tests). |
| `WithShouldDelete(func(name, info, now) bool)` | Replace the age rule. `maxAge` is still required. Empty folders are removed anyway. |

## Policies

A **policy** decides where files go and who can reach them. Policies are
**symmetric**: what `OnWrite` adds to the path, `OnRead` takes back and checks.

```go
secure := files.Of(local).
	Add(files.Folder("avatars")).
	Add(files.User(currentUserID)).
	Naming(files.GUID()).
	Build()

stored, _ := secure.Write(ctx, "photo.png", upload)
// stored == "avatars/42/0b8c2f4e-....png"
f, err := secure.Open(ctx, stored)       // checks "avatars", then that "42" is the current user
```

- `files.Of(dir)` starts a chain; `Add(policy)` appends (they run in the order
  added); `Naming(n)` sets the naming policy (replacing a previous one); `Build()`
  returns the `Directory`. Changing the chain after `Build` does not affect it.
- On **write**, each policy adds its section. On **read**, the path must start with
  those sections in the same order, or the answer is `ErrAccessDenied`. A path
  shorter than the policies is denied; if nothing is left after them, `ErrNotFound`.

### Ready-made policies

| Policy | Write | Read |
|---|---|---|
| `Folder(sections...)` | Adds the folders, nested in that order. | The path must start with them. |
| `SystemUser()` | Adds folder `0` (files that belong to no user). | Must start with `0`. |
| `User(func() ID)` | Adds the folder of the current user's id. The function runs on every operation, so it can read the id from the request. An id that is not a safe folder name (empty, with `/`) is `ErrAccessDenied`. | The path must start with the **current user's** folder: nobody reaches another user's files. |
| `Date(now)` | Adds a `yyyyMMdd` folder of the write date (`now == nil` uses `time.Now`). | Any valid date is accepted, not only today. |

`User` is generic: `files.User(func() int64 { return userID(ctx) })`.

### Naming policies

A `NamingPolicy` changes the name of the file on **write only**. It receives the
path the caller gave and returns the path to use, in the same folder.

| Naming | `a/b/photo.png` becomes |
|---|---|
| `GUID()` | `a/b/0b8c2f4e-....png` (random UUID v4, keeps folder and extension) |
| `TimeStamp(now, layout)` | `a/b/photo_20261031153045.123.png` (adds the time to the name) |
| `NameWithTimeStamp(now, "invoice", layout)` | `a/b/invoice_20261031153045.123.png` (ignores the name) |

An empty `layout` uses `files.DefaultStampLayout` (`20060102150405.000`: the
milliseconds come after a dot, because Go only supports them that way); a nil
`now` uses `time.Now`.

### Writing your own policy

```go
type Policy interface {
	OnWrite(path *Path) error
	OnRead(path *Path) error
}
```

`Path` is a queue of sections:

```go
func (p *Path) Enqueue(section string)        // OnWrite: add a section
func (p *Path) Dequeue() (string, bool)       // OnRead: take the first; false if none left
func (p *Path) Len() int
func (p *Path) String() string
```

```go
type tenant struct{ id func() string }

func (t tenant) OnWrite(p *files.Path) error { p.Enqueue("tenant-" + t.id()); return nil }
func (t tenant) OnRead(p *files.Path) error {
	section, ok := p.Dequeue()
	if !ok || section != "tenant-"+t.id() {
		return files.ErrAccessDenied
	}
	return nil
}
```

## Temp: handing files between subsystems

```go
type Temp struct{ Directory }
func AsTemp(d Directory) Temp // panics if d is nil
```

`Temp` is the contract for the directory where subsystems pass files to each
other: whoever **produces** a file writes it in the `Temp` and passes the relative
path that `Write` returned; whoever **consumes** it opens that path in the same
`Temp`. Nobody needs to know where files really are. This is how the mail, PDF and
Excel services work: an Excel service writes a report into the `Temp` and the mail
service attaches it by that path.

`Temp` is its own type (not an alias) so a function can ask for it in its
signature, and a dependency container can tell it apart from other directories:

```go
func NewReportService(tmp files.Temp) *ReportService
tmp := files.AsTemp(temporal)
```

It does not have to be a `Temporal`; use one when the files must expire. The
**lifetime of a `Temporal` must exceed the whole time a file may be needed**:
a mail outbox retries over about two and a half hours, so a one-hour `Temporal`
would delete attachments before the last attempt.

When files are served to users, wrap the `Temporal` with a `User` policy
(`files.AsTemp(files.Of(temporal).Add(files.User(currentID)).Build())`), so nobody can
read someone else's documents.
