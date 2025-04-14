package owl

import (
	"context"
	"embed"
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/localizer"
	"github.com/deltegui/valtruc"
)

type Muxi struct {
	router   *httprouter.Router
	cypher   core.Cypher
	locStore *localizer.Store

	injector *Injector

	middlewares []Middleware
}

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
		ctx:       context.Background(),
		locstore:  mux.locStore,
		validator: valtruc.New(),
		cypher:    mux.cypher,
	}
}

func (mux *Muxi) Run(runner Runner) {
	mux.injector.Run(runner)
}

func (mux *Muxi) ShowAvailableBuilders() {
	mux.injector.ShowAvailableBuilders()
}

func (mux *Muxi) PopulateStruct(s any) {
	mux.injector.PopulateStruct(s)
}

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

func (mux *Muxi) Get(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodGet, pattern, builder, middlewares...)
}

func (mux *Muxi) Post(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodPost, pattern, builder, middlewares...)
}

func (mux *Muxi) Patch(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodPatch, pattern, builder, middlewares...)
}

func (mux *Muxi) Delete(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodDelete, pattern, builder, middlewares...)
}

func (mux *Muxi) Head(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodHead, pattern, builder, middlewares...)
}

func (mux *Muxi) Options(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodOptions, pattern, builder, middlewares...)
}

func (mux *Muxi) Put(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodPut, pattern, builder, middlewares...)
}

func (mux *Muxi) Trace(pattern string, builder Builder, middlewares ...Middleware) {
	mux.Handle(http.MethodTrace, pattern, builder, middlewares...)
}

func (mux *Muxi) Static(path string) {
	mux.router.NotFound = http.FileServer(http.Dir(path))
}

func (mux *Muxi) StaticEmbedded(fs embed.FS) {
	mux.router.NotFound = http.FileServer(http.FS(fs))
}

func (mux *Muxi) StaticMount(url, path string) {
	mux.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.Dir(path))
}

func (mux *Muxi) StaticMountEmbedded(url string, fs embed.FS) {
	mux.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.FS(fs))
}

func (mux Muxi) Listen(address string) {
	server := http.Server{
		Addr:    address,
		Handler: mux.router,
	}
	go startServer(&server)
	waitAndStopServer(&server)
}

func (mux *Muxi) AddLocalization(fs embed.FS, sharedKey, errorKey string) {
	store := localizer.NewLocalizerStore(fs, sharedKey, errorKey, mux.cypher)
	mux.locStore = &store
}

func (mux *Muxi) Use(middleware Middleware) {
	mux.middlewares = append(mux.middlewares, middleware)
}
