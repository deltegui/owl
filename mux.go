package owl

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/deltegui/owl/v3/core"
)

// SubMux represents a subrouter within the Owl framework.
//
// A SubMux is a limited, controlled view of a Mux (the main router).
// Internally, a SubMux is simply a *Mux with its own route prefix and middlewares,
// but its public interface restricts access to only the operations needed for a subrouter.
//
// In practice, CreateSubMux returns a SubMux which is a *Mux with the
// accumulated routePrefix and a copy of middlewares.
//
// Example usage:
//
//	api := mux.CreateSubMux("/api")
//	v1 := api.CreateSubMux("/v1")
//	v1.Get("/ping", handler)
//
// Here, v1 is a SubMux that only allows registering routes and middlewares,
// but does not expose global Mux methods such as starting the server.
type SubMux interface {
	Handle(method, pattern string, handler Handler, middlewares ...Middleware)
	Get(pattern string, handler Handler, middlewares ...Middleware)
	Post(pattern string, handler Handler, middlewares ...Middleware)
	Patch(pattern string, handler Handler, middlewares ...Middleware)
	Delete(pattern string, handler Handler, middlewares ...Middleware)
	Head(pattern string, handler Handler, middlewares ...Middleware)
	Options(pattern string, handler Handler, middlewares ...Middleware)
	Put(pattern string, handler Handler, middlewares ...Middleware)
	Trace(pattern string, handler Handler, middlewares ...Middleware)
	Use(middleware Middleware)
}

// Handler is a function that handles HTTP requests. Example:
//
//	func showIndex(ctx owl.Ctx) error {
//		return ctx.Ok("Hello")
//	}
//
// This handler just prints the text 'Hello' in the browser. Typically you will
// want to do more things than printing a text, like calling a service. So, you will
// want to provide a service implementation. You can do it this way:
//
//	func showIndex(service MyService) owl.Handler {
//		return func(ctx owl.Ctx) error {
//			res := service.DoSomething()
//			return ctx.Ok(res)
//		}
//	}
//
// Owl offers you two ways to poppulate the dependencies of this handler:
//
// - Manually. See Mux implementation.
// - Using a dependency injection container. See Muxi implementation.
type Handler func(ctx Ctx) error

// Middleware is a function that is executed in every request and before or after
// any handler. Lets you do many things, like authentication, logging, ...
type Middleware func(next Handler) Handler

// Mux is a HTTP multiplexer (router).
type Mux struct {
	base
}

// Creates a new multiplexer. Needs a core.Cypher
// implementation to automatically do some encryptation like cookies security.
func New(cy core.Cypher) *Mux {
	return &Mux{base: newBase(cy)}
}

func (mux *Mux) CreateSubMux(prefix string) SubMux {
	return &Mux{base: mux.base.sub(prefix)}
}

// Handle registers a http Handle to a particular HTTP method and pattern.
// A list of Middlewares can be optionally added. For example, register a handle this way:
//
//	func showIndex(ctx owl.Ctx) error {
//		return ctx.Ok("Hello")
//	}
//
//	mux.Handle(http.MethodGet, "/index", indexHandler)
func (mux *Mux) Handle(method, pattern string, handler Handler, middlewares ...Middleware) {
	mux.register(method, pattern, handler, middlewares)
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	path = strings.ReplaceAll(path, "//", "/")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	return path
}

// Get registers a handler to a particular pattern and HTTP Get method. See Handle method.
func (mux *Mux) Get(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodGet, pattern, handler, middlewares...)
}

// Post registers a handler to a particular pattern and HTTP Post method. See Handle method.
func (mux *Mux) Post(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodPost, pattern, handler, middlewares...)
}

// Patch registers a handler to a particular pattern and HTTP Patch method. See Handle method.
func (mux *Mux) Patch(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodPatch, pattern, handler, middlewares...)
}

// Delete registers a handler to a particular pattern and HTTP Delete method. See Handle method.
func (mux *Mux) Delete(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodDelete, pattern, handler, middlewares...)
}

// Head registers a handler to a particular pattern and HTTP Head method. See Handle method.
func (mux *Mux) Head(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodHead, pattern, handler, middlewares...)
}

// Options registers a handler to a particular pattern and HTTP Options method. See Handle method.
func (mux *Mux) Options(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodOptions, pattern, handler, middlewares...)
}

// Put registers a handler to a particular pattern and HTTP Put method. See Handle method.
func (mux *Mux) Put(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodPut, pattern, handler, middlewares...)
}

// Trace registers a handler to a particular pattern and HTTP Trace method. See Handle method.
func (mux *Mux) Trace(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodTrace, pattern, handler, middlewares...)
}

func startServer(server *http.Server) {
	log.Println("Listening on address: ", server.Addr)
	log.Println("You are ready to GO!")
	err := server.ListenAndServe()
	if err != nil {
		log.Fatalln("Error while listening: ", err)
	}
}

func waitAndStopServer(server *http.Server) {
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-done

	log.Print("Server Stopped")
	const maxTiemout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), maxTiemout)

	defer func() {
		cancel()
	}()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed:%+v", err)
	}

	log.Print("Server exited properly")
}

// Redirects request to URL.
func Redirect(to string) Handler {
	return func(c Ctx) error {
		http.RedirectHandler(to, http.StatusTemporaryRedirect).ServeHTTP(c.Res, c.Req)
		return nil
	}
}

// PrintLogo takes a file path and prints your fancy ascii logo.
// It will fail if your file is not found.
func PrintLogo(logoFile string) {
	logo, err := os.ReadFile(logoFile)
	if err != nil {
		log.Fatalf("Cannot read logo file: %s\n", err)
	}
	fmt.Println(string(logo))
}

// PrintLogo takes a embedded filesystem and file path and prints your fancy ascii logo.
// It will fail if your file is not found.
func PrintLogoEmbedded(fs embed.FS, path string) {
	logo, err := fs.ReadFile(path)
	if err != nil {
		log.Fatalf("Cannot read logo file: %s\n", err)
	}
	fmt.Println(string(logo))
}
