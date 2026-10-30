package owl

import (
	"net/http"

	"github.com/deltegui/owl/v3/core"
)

type SubMuxi interface {
	Handle(method, pattern string, builder Builder, middlewares ...Middleware)
	Get(pattern string, builder Builder, middlewares ...Middleware)
	Post(pattern string, builder Builder, middlewares ...Middleware)
	Patch(pattern string, builder Builder, middlewares ...Middleware)
	Delete(pattern string, builder Builder, middlewares ...Middleware)
	Head(pattern string, builder Builder, middlewares ...Middleware)
	Options(pattern string, builder Builder, middlewares ...Middleware)
	Put(pattern string, builder Builder, middlewares ...Middleware)
	Trace(pattern string, builder Builder, middlewares ...Middleware)

	Use(middleware Middleware)

	Run(runner Runner)
	ShowAvailableBuilders()
	PopulateStruct(s any)
	Add(builder Builder)
}

// Muxi is a HTTP multiplexer (router) with a dependency injection container.
type Muxi struct {
	base
	injector *Injector
}

// Creates a new multiplexer with dependency injection container. Needs a core.Cypher
// implementation to automatically do some encryptation like cookies security.
func NewWithInjector(cy core.Cypher) *Muxi {
	return &Muxi{base: newBase(cy), injector: NewInjector()}
}

func (mux *Muxi) CreateSubMuxi(prefix string) SubMuxi {
	return &Muxi{base: mux.base.sub(prefix), injector: mux.injector.clone()}
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
	mux.register(method, pattern, mux.injector.ResolveHandler(builder), middlewares)
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
