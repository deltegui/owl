# Mail

Packages under `github.com/deltegui/owl/v3/mail`:

| Package | Role | Imports |
|---|---|---|
| `mail` | The contract: `Message`, `Sender`, `Renderer`, `Driver`, and `Service` (renders + sends). | stdlib, `files` |
| `mail/hxmail` | A `Renderer` written with Hix and `localizer.Dict`. | `hx/ssr` |
| `mail/smtp` | A `Driver` over SMTP, standard library only. | stdlib |
| `mail/queue` | A `Sender` that sends in the background from memory. | |
| `mail/outbox` | A `Sender` that saves emails in a store and a `Worker` that sends them, with retries. | |

They are separate so a program that only sends emails does not pull what it does
not use.

## The idea

```
your code ──Send(Message)──▶ Sender ─────────────────────────────────────────────┐
                              │                                                   │
          (optional wrappers) │  queue.Queue   in memory, background              │
                              │  outbox.Sender saved in a Store, sent by a Worker │
                              ▼                                                   │
                         mail.Service  ── Renderer (hxmail) ─▶ subject + HTML    │
                              │                                                   │
                              └────────── Driver (smtp) ─▶ the mail server ◀──────┘
```

Your service only knows `mail.Sender`:

```go
type Sender interface {
	Send(ctx context.Context, msg Message) error
}
```

What a `nil` error means depends on the implementation: *sent* (`mail.Service`),
*queued* (`queue`) or *saved* (`outbox`).

## Message

```go
type Message struct {
	Envelope Envelope
	Lang     string   // "es", "en"...; empty is the default language
	Payload  Payload  // any struct or pointer; its TYPE decides the template and subject
}

type Envelope struct {
	To, Cc, Bcc, ReplyTo []string
	Attachments          []string // relative paths in the files.Temp directory
}
```

- The **payload type** is the identity of an email: `UserConfirmation{Name, Link}`
  is one email, `PasswordReset{Link}` another. It must not be `nil`.
- Attachments are **paths** in a [`files.Temp`](files.md#temp-handing-files-between-subsystems),
  not content: whoever sends only gets the paths.
- `msg.Validate()` checks a payload exists, there is at least one recipient, and the
  addresses are well formed (parsed with `net/mail` and free of line breaks, so
  nobody injects headers). Its errors wrap `mail.ErrInvalid`.

## hxmail: rendering with Hix

```go
renderer := hxmail.New(hxmail.WithDefaultLang("es"))

hxmail.Register(renderer, hxmail.Template[UserConfirmation]{
	Texts: confirmationTexts, // localizer.Dict, with the key "Subject"
	View: func(p UserConfirmation, t localizer.Localizer) hx.INode {
		return hx.Div().Body(
			hx.H1().Text(t.GetFormatted("Hello", p.Name)),
			hx.A().Href(p.Link).Text(t.Get("Confirm")),
		)
	},
})

var confirmationTexts = localizer.Dict{
	"es": {"Subject": "Confirma tu cuenta", "Hello": "Hola %s", "Confirm": "Confirmar"},
	"en": {"Subject": "Confirm your account", "Hello": "Hello %s", "Confirm": "Confirm"},
}
```

`Register` is a generic function that binds a payload type (`P` or `*P`) to its
template. Call it when the program starts: it **panics** if `View` is nil, or if
the type or the name already has a template, and it is not safe to register while
rendering.

```go
type Template[P any] struct {
	Texts   localizer.Source
	View    func(payload P, texts localizer.Localizer) hx.INode
	Subject func(payload P, texts localizer.Localizer) string // when the subject depends on the payload
	CIDs    map[string]string                                // resources of this email
	Driver  mail.Driver                                      // send this email with another driver
	Name    string                                           // identity when stored (outbox)
}
```

- **Subject**: the key `hxmail.SubjectKey` (`"Subject"`) in `Texts`, or
  `Template.Subject` if it depends on the payload. No subject is
  `hxmail.ErrNoSubject`.
- **Language**: `localizer.Pick(Texts, msg.Lang, defaultLang)`: the message's
  language, or the default of the renderer if the dictionary has nothing in it.
- **View**: text is escaped and URLs are checked by Hix, as in a page. If the view
  builds an `<html>` element, `<!doctype html>` is written first. Email clients
  support little CSS: write styles in the elements with `.Style(...)`. A view that
  panics or builds nothing is an error, it does not crash the program.
- **No shared texts between templates**: the common layout is a Go function and
  common texts are repeated in each `Dict`.
- A payload type with no template is `hxmail.ErrNotRegistered`. Both errors are
  *permanent* (see [outbox](#outbox-delayed-delivery-with-retries)).

`Renderer.Render(ctx, msg)` returns `mail.Rendered{Subject, HTML, CIDs, Driver}`.
It neither validates nor sends.

`hxmail` calls `hx.AllowURLScheme("cid")` at startup, because Hix replaces `cid:`
URLs with `about:invalid#hx-unsafe-url` and embedded images need them. It affects
the whole program; on a web page it is harmless.

### Embedded images (CID)

```go
hx.Img().Src("cid:logo")           // in the view
mail.WithCID("logo", "logo.png")   // an image all emails use (see Service options)
Template[P]{CIDs: map[string]string{"chart": "charts/x.png"}} // for one email
```

The `mail.Service` attaches each used resource with its `Content-ID`.

## Service: render and deliver

```go
svc := mail.NewService(renderer, smtpDriver, tmp,
	mail.WithResources(resourcesDir),         // where WithCID paths are
	mail.WithCID("logo", "logo.png"),
)
err := svc.Send(ctx, mail.Message{
	Envelope: mail.Envelope{To: []string{"ana@example.com"}},
	Lang:     "es",
	Payload:  UserConfirmation{Name: "Ana", Link: link},
})
```

`NewService(renderer, driver, tmp, opts...)` panics if the renderer or driver is
nil. `Send` validates the message, asks the renderer for the content, gathers the
resources and calls the driver; the `Rendered.Driver` of a template wins over the
service's. It is safe for concurrent use if the renderer and driver are.

**Resources.** With `WithResources(dir)`, `WithCID` paths are in that `Directory`
and the service **copies each resource into the `Temp` on every send** (a random
folder of its own), because a `files.Temporal` would delete a logo that had been
placed there once. Without `WithResources`, the paths are passed as they are, as
paths in the `Temp`. A template's own `CIDs` win over the global ones.

`Service` is where any chain of `Sender`s ends.

### Contracts

```go
type Renderer interface {
	Render(ctx context.Context, msg Message) (Rendered, error)
}
type Driver interface {
	Send(ctx context.Context, req Request, tmp files.Temp) error
}
type Request struct {
	Subject  string
	HTML     string
	Envelope Envelope
	CIDs     map[string]string // content id -> relative path in tmp
	Payload  Payload           // for drivers that need it; most do not
}
```

Implement `Renderer` to use `html/template` or a ready-made HTML, and `Driver` to
use a provider's HTTP API. A driver reads attachments and resources from `tmp` by
relative path and does not touch the file system itself.

## smtp: the SMTP driver

```go
driver, err := smtp.New(smtp.Options{
	Host:     "smtp.example.com",
	Sender:   "Little Cash <no-reply@example.com>",
	Username: "user",
	Password: "secret",
	// Security: smtp.StartTLS (default, port 587)
})
```

`New` does not connect. The driver opens **one connection per email** and is safe
for concurrent use.

| Option | Meaning |
|---|---|
| `Host` (required), `Port` | Port defaults to 587 (`StartTLS`), 465 (`ImplicitTLS`) or 25 (`NoTLS`). |
| `Sender` (required) | The `From` and the envelope sender, with or without name. |
| `Username`, `Password` | PLAIN login, or LOGIN if the server does not offer PLAIN (Office 365). No username means no authentication. |
| `Auth` | Any `net/smtp.Auth` instead (XOAUTH2...). |
| `Security` | `StartTLS` (default): upgrades with STARTTLS and **fails** if the server does not offer it, it never falls back to plain text. `ImplicitTLS`: encrypted from the first byte. `NoTLS`: never encrypts, only for a local server or development; the password is refused unencrypted unless the host is localhost. |
| `TLSConfig` | Optional; `ServerName` defaults to `Host`. |
| `Timeout` | Limit for sending one email, connection included. 30 s by default; the context can be shorter. |
| `Hostname` | The name given in HELO. `localhost` by default. |
| `Now` | Clock for the `Date` header. |

Behavior:

- Files are opened **before connecting**: a missing attachment fails without
  calling the server (`files.ErrNotFound` or `ErrAccessDenied`).
- The message is written to the connection **while reading the files**, so a big
  attachment is not loaded in memory. If writing fails midway the connection is
  closed without ending `DATA`, so the server never accepts a truncated email.
- MIME structure: `multipart/mixed` [ `multipart/related` [ html, inline
  resources ], attachments ]; each level exists only if needed. HTML in
  quoted-printable, files in base64, subject encoded, accented file names per RFC
  2231. `Bcc` goes only in the envelope. A random `Message-ID` with the domain of
  `Sender`.
- Line breaks in subject, addresses, `Sender` or content ids are rejected.
- Errors from the server wrap `*textproto.Error`: `errors.As` gives the code
  (4xx temporary, 5xx permanent), which the outbox uses.

`smtp.Security` values: `smtp.StartTLS`, `smtp.ImplicitTLS`, `smtp.NoTLS`.

## queue: background sending from memory

```go
q := queue.New(svc, queue.Options{Workers: 2})
defer q.Close(ctx)

err := q.Send(ctx, msg) // nil means "queued"
```

A `Queue` is a `mail.Sender` that puts the email in a bounded queue and lets
goroutines give it to another sender (usually `mail.Service`), so the caller does
not wait for the mail server.

| Option | Default | Meaning |
|---|---|---|
| `Buffer` | 100 | How many emails can wait. When full, `Send` waits for room until its `ctx` is done: give it a context with a limit. |
| `Workers` | 1 | Emails sent at once. |
| `Timeout` | 1 min | Limit for one email. |
| `OnError` | logs | Called from a worker for each email that fails (also on a panic of the sender). The default logs the payload type and the *number* of recipients, not addresses. |
| `Logger` | `logx.Default{}` | Used by the default `OnError`. |

- `Send` validates first, so `mail.ErrInvalid` comes back to the caller.
- The message is **copied** (the envelope slices); the payload is not, so if it
  holds a pointer, do not change what it points to after `Send`.
- The `ctx` of `Send` only covers waiting for room. The email is sent with the
  queue's own context, because an HTTP request's context is cancelled when the
  response ends and the email would be lost.
- `Close(ctx)` stops accepting (`queue.ErrClosed`, also for senders waiting for room)
  and sends what is waiting until `ctx` ends. If time runs out it cancels the sends
  in progress, gives the unsent ones to `OnError` with `queue.ErrDiscarded` and returns
  `queue.ErrNotDrained` wrapped with how many were left. The sender must respect
  its context for those to stop. It can be called more than once.

**A `nil` from a queue only means "queued".** If the process dies, what waited is
lost and nobody retries failures. Use it for notices that may be lost. Do **not**
use it for emails that something depends on (confirm an account, new password,
verification code): use the outbox.

## outbox: delayed delivery with retries

An outbox saves the email in your database **in the same transaction** as what
caused it, and a worker sends it later, retrying on failure. It has two halves
that can run in different processes.

```go
renderer := hxmail.New(...)               // registers the payload types, once
store := myStore{db}                      // your implementation of outbox.Store
sender := outbox.NewSender(store, renderer, outbox.WithWake(worker.Wake))

worker, err := outbox.NewWorker(outbox.Options{
	Store:    store,
	Sender:   svc,       // a mail.Service
	Payloads: renderer,
})
go worker.Run(ctx)

err = sender.Send(txCtx, msg) // nil means "saved"
```

### Sender

`outbox.NewSender(store, payloads, opts...)` is a `mail.Sender` that validates the
message, finds the **name** of the payload type (`outbox.ErrUnknownPayload` if
unknown: it fails when saving, not later when nobody is looking), encodes the
payload and envelope as JSON and calls `store.Save`. Only **exported fields** of
the payload survive the JSON round trip. Attachments are kept as paths.

`WithWake(fn)` calls `fn` after saving (usually `worker.Wake` when the worker is in
the same program) so the email does not wait for the next poll.

The `ctx` passed to `Send` reaches `Store.Save`, so a store can use the transaction
the context carries: the email is saved, or not, together with what caused it.

### Payloads

```go
type Payloads interface {
	PayloadName(payload any) (string, bool)
	DecodePayload(name string, decode func(into any) error) (any, error)
}
```

`hxmail.Renderer` implements it, so payload types are registered once, with the
templates. The name is `Template.Name`, or the package-qualified Go type
(like `account.UserConfirmation`) if empty. Set `Name` explicitly if you want to
rename or move a type without leaving stored emails that can no longer be read.
Two templates with the same name panic.

### Store

You implement `outbox.Store` (a table, usually):

```go
type Store interface {
	Save(ctx context.Context, e Entry) error
	ResetStale(ctx context.Context, before time.Time) error
	Claim(ctx context.Context, worker string, limit int, now time.Time) ([]Item, error)
	MarkSent(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, cause string, next *time.Time) error
}
```

| Method | Contract |
|---|---|
| `Save` | Keep a new email, `Pending` and due now. |
| `ResetStale(before)` | Make `Pending` again emails claimed before `before` and still `InProgress` (their worker died). |
| `Claim(worker, limit, now)` | Take up to `limit` `Pending` emails whose time has come, **oldest first**, and mark them `InProgress`. **Must be atomic**: in Postgres, `UPDATE … WHERE id IN (SELECT … FOR UPDATE SKIP LOCKED) … RETURNING`. Several workers in several processes share a store: each item goes to only one. |
| `MarkSent(id)` | Mark `Sent`. |
| `MarkFailed(id, cause, next)` | Count a failed attempt and keep the cause. `next == nil`: final failure (`Failed`); otherwise `Pending`, claimable from `next`. |

```go
type Entry struct { Type string; Payload, Envelope []byte; Lang string } // JSON written by this package
type Item  struct { ID int64; Attempts int; Entry }
type Status int // outbox.Pending, InProgress, Sent, Failed
```

`outbox.NewMemoryStore(now)` is a store for tests and development (nothing
survives; `Snapshot()` returns every email as `[]Record`). There is no SQL store
and no cleanup of sent rows: those are yours.

### Worker

```go
func NewWorker(o Options) (*Worker, error)
func (w *Worker) Run(ctx context.Context)              // blocks; run with `go`
func (w *Worker) RunOnce(ctx context.Context) (int, error) // for a scheduled job; returns how many it claimed
func (w *Worker) Wake()                                // look at the store now; never blocks
```

`Options` (`Store`, `Sender`, `Payloads` are required):

| Option | Default | Meaning |
|---|---|---|
| `PollInterval` | 30 s | Time between looks when nothing wakes the worker. |
| `BatchSize` | 50 | Emails claimed at once; if a look fills a batch, the next is immediate. |
| `StaleAfter` | 10 min | How long `InProgress` before its worker is thought dead. Longer than the time to send one email. |
| `MaxAttempts` | 5 | Attempts before `Failed` (the failing attempt counts). |
| `Backoff` | `DefaultBackoff` | Wait after each failed attempt: 30 s, 5 min, 30 min, then 2 h. |
| `Retry` | `DefaultRetry` | Whether an error is worth another attempt. |
| `Logger` | `logx.Default{}` | |
| `Now` | `time.Now` | |
| `ID` | `host:pid` | Names this worker in the store. |

Each cycle: `ResetStale`, `Claim`, then for each email `Send` and
`MarkSent`/`MarkFailed`.

**Delivery is at least once.** If the process dies after the server accepted the
email and before `MarkSent`, it is sent again.

**What is retried.** `DefaultRetry` does **not** retry:
errors marked with `outbox.Permanent(err)` or having a `Permanent() bool` method
that returns true (`hxmail.ErrNotRegistered` and `ErrNoSubject` do); SMTP `5xx`
answers except login ones (530, 534, 535, 538, which are configuration: the email
goes out once you fix it); `mail.ErrInvalid`; `files.ErrNotFound`/`ErrAccessDenied`;
and what cannot be read from the store (bad JSON, unknown type). It retries `4xx`,
timeouts and network failures.

**Stopping.** When `ctx` is cancelled the `Mark*` calls use a context without the
cancellation (10 s), so an email that was already sent is not lost. What was claimed
and not sent stays `InProgress` without counting an attempt until `ResetStale`
frees it after `StaleAfter`.

### Operational rules

- **Deploy workers before the programs that save a new kind of email**, or that
  email fails as an unknown type.
- **The `Temp` of an outbox must outlive the whole retry window** (about 2 h 35 min
  with the default backoff). A `Temporal` of 1 hour would delete attachments before
  the last attempt, and a missing attachment is a *final* failure.
- Keep the same payload names and exported fields compatible while old rows exist.

## Wiring, all together

```go
tmp := files.AsTemp(temporal)                     // lives longer than the retry window
renderer := hxmail.New(hxmail.WithDefaultLang("es"))
hxmail.Register(renderer, hxmail.Template[UserConfirmation]{...})

driver, _ := smtp.New(smtp.Options{Host: "...", Sender: "..."})
svc := mail.NewService(renderer, driver, tmp, mail.WithResources(res), mail.WithCID("logo", "logo.png"))

// critical emails: stored and retried
store := newPostgresStore(db)
worker, _ := outbox.NewWorker(outbox.Options{Store: store, Sender: svc, Payloads: renderer})
go worker.Run(ctx)
critical := outbox.NewSender(store, renderer, outbox.WithWake(worker.Wake))

// notices that can be lost: in memory
notices := queue.New(svc, queue.Options{Workers: 2})
defer notices.Close(shutdownCtx)
```

Services take a `mail.Sender`: give the critical flows `critical` and the rest
`notices`.
