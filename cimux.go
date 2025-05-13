package owl

import (
	"embed"
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/localizer"
	"github.com/deltegui/valtruc"
)

// Muxi is a HTTP multiplexer (router) with a dependency injection container.
type Muxi struct {
	router   *httprouter.Router
	cypher   core.Cypher
	locStore *localizer.WebStore

	injector *Injector

	middlewares []Middleware
}

// Creates a new multiplexer with dependency injection container. Needs a core.Cypher
// implementation to automatically do some encryptation like cookies security.
func NewWithInjector(cy core.Cypher) *Muxi {
	return &Muxi{
		router:   httprouter.New(),
		locStore: nil,
		cypher:   cy,
		injector: NewInjector(),
	}
}

func (mux *Muxi) createContext(w http.ResponseWriter, req *http.Request, params httprouter.Params) Ctx {
	return Ctx{
		Req:       req,
		Res:       w,
		params:    params,
		ctx:       req.Context(),
		locstore:  mux.locStore,
		validator: valtruc.New(),
		cypher:    mux.cypher,
	}
}

// Run is a function that runs a Runner. Show Runner type for more information.
func (mux *Muxi) Run(runner Runner) {
	mux.injector.Run(runner)
}

// ShowAvailableBuilders prints all registered builders.
func (mux *Muxi) ShowAvailableBuilders() {
	mux.injector.ShowAvailableBuilders()
}

// PopulateStruct fills a struct with the implementations
// that the injector can create. Make sure you pass a reference and
// not a value.
func (mux *Muxi) PopulateStruct(s any) {
	mux.injector.PopulateStruct(s)
}

// Add registers new builders to the dependency injection container.
func (mux *Muxi) Add(builder Builder) {
	mux.injector.Add(builder)
}

// Handle registers a http Handle to a particular HTTP method and pattern. The handler must
// be created using a builder. A list of Middlewares can be optionally added.
// For example, create a builder for a handler:
//
//	func indexHandler(d dependency) owl.Handler {
//		return func(ctx owl.Ctx) error {
//			return ctx.Ok("Hello")
//		}
//	}
//
// Then, register it using handle:
//
//	mux.Handle(http.MethodGet, "/index", indexHandler)
//
// Here, indexHandler builder function will be called with the requested dependencies automatically.
// In this case, the dependency should have been provided. In this case could be something like this:
//
//	mux.Add(NewDependency)
//
// Where NewDependecy is a builder that produces the type 'dependency'.
func (mux *Muxi) Handle(method, pattern string, builder Builder, middlewares ...Middleware) {
	handler := mux.injector.ResolveHandler(builder)
	for _, m := range middlewares {
		handler = m(handler)
	}
	for _, m := range mux.middlewares {
		handler = m(handler)
	}
	mux.router.Handle(method, pattern, func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		ctx := mux.createContext(w, req, params)
		handler(ctx)
	})
}

// Get registers a handler to a particular pattern and HTTP Get method. See Handle method.
func (mux *Muxi) Get(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodGet, pattern, builder, middlewares...)
}

// Post registers a handler to a particular pattern and HTTP Post method. See Handle method.
func (mux *Muxi) Post(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodPost, pattern, builder, middlewares...)
}

// Patch registers a handler to a particular pattern and HTTP Patch method. See Handle method.
func (mux *Muxi) Patch(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodPatch, pattern, builder, middlewares...)
}

// Delete registers a handler to a particular pattern and HTTP Delete method. See Handle method.
func (mux *Muxi) Delete(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodDelete, pattern, builder, middlewares...)
}

// Head registers a handler to a particular pattern and HTTP Head method. See Handle method.
func (mux *Muxi) Head(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodHead, pattern, builder, middlewares...)
}

// Options registers a handler to a particular pattern and HTTP Options method. See Handle method.
func (mux *Muxi) Options(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodOptions, pattern, builder, middlewares...)
}

// Put registers a handler to a particular pattern and HTTP Put method. See Handle method.
func (mux *Muxi) Put(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodPut, pattern, builder, middlewares...)
}

// Trace registers a handler to a particular pattern and HTTP Trace method. See Handle method.
func (mux *Muxi) Trace(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodTrace, pattern, builder, middlewares...)
}

// Creates a static file server in the requested dir path.
func (mux *Muxi) Static(path string) {
	mux.router.NotFound = http.FileServer(http.Dir(path))
}

// Creates a static file server with the requested embedded file system.
func (mux *Muxi) StaticEmbedded(fs embed.FS) {
	mux.router.NotFound = http.FileServer(http.FS(fs))
}

// Creates a static file server in the requested dir URL and path.
func (mux *Muxi) StaticMount(url, path string) {
	mux.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.Dir(path))
}

// Creates a static file server in the requested dir URL and embedded file system.
func (mux *Muxi) StaticMountEmbedded(url string, fs embed.FS) {
	mux.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.FS(fs))
}

// Listen starts owl's server
func (mux Muxi) Listen(address string) {
	server := http.Server{
		Addr:    address,
		Handler: mux.router,
	}
	go startServer(&server)
	waitAndStopServer(&server)
}

// AddLocalization creates a new WebLocalizerStore using the provided parameters.
func (mux *Muxi) AddLocalization(fs embed.FS, sharedKey, errorKey string) {
	store := localizer.NewWebLocalizerStore(fs, sharedKey, errorKey, mux.cypher)
	mux.locStore = &store
}

// Use middleware globally.
func (mux *Muxi) Use(middleware Middleware) {
	mux.middlewares = append(mux.middlewares, middleware)
}
