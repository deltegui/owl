# OWL documentation

OWL is a web framework for server-rendered sites written in Go, with pages
built in Go with [Hix](https://github.com/deltegui/hx) and optional *islands*
that the browser hydrates (WASM). Module path: `github.com/deltegui/owl/v3`.

Start with [getting started](getting-started.md), then read what you need.

| Document | Packages | About |
|---|---|---|
| [Getting started](getting-started.md) | `owl` | Install, the smallest app, project layout, wiring order. |
| [Routing](routing.md) | `owl`, `owl/middleware` | `Mux`, `Muxi`, sub-routers, middleware, static files, dependency injection. |
| [Request context](context.md) | `owl` | `Ctx`: parameters, forms, JSON, validation, responses, cookies. |
| [Views](views.md) | `owl`, `owl/hxui`, `owl/pagination` | `ctx.View`, `hxui` pieces, forms and errors, pagination. |
| [Islands](islands.md) | `owl/island`, `owl/island/client`, `owl/hxui` | Server-rendered, browser-hydrated pieces; talking to the JSON API. |
| [Localization](localization.md) | `owl/localizer`, `owl/localizer/webstore` | Dictionaries in code, language cookie, translated errors. |
| [Security](security.md) | `owl/cipher`, `owl/csrf`, `owl/middleware` | Cipher, sealed values, CSRF, CORS. |
| [Authentication](authentication.md) | `owl/session`, `owl/hash`, `owl/middleware` | Sessions, route protection, passwords. |
| [Files](files.md) | `owl/files` | Directories, policies, naming, local and temporal storage. |
| [Mail](mail.md) | `owl/mail/...` | Sending email: renderer, SMTP, queue, outbox. |
| [PDF and Excel](pdf-excel.md) | `owl/pdf`, `owl/pdf/hxpdf`, `owl/excel` | Document generation contracts and services. |
| [Utilities](utilities.md) | `owl/core`, `owl/logx`, `owl/pagination` | Domain errors, tokens, logging, pagination data. |

## Conventions used in these documents

- Code samples omit imports and error handling that does not matter.
- "Server" code imports the `owl` root package. Packages that must also compile
  to WASM (`localizer`, `island`, `island/client`) never import it.
- A **builder** is a function whose return type is what it builds; the
  dependency injection container calls it to get that type.
- Most constructors that can be misconfigured **panic at startup** (for example
  `AddLocalization` with no languages), so the mistake shows when the program
  starts, not on the first request.
