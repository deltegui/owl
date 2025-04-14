package owl

import (
	"context"
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/deltegui/owl/core"
	"github.com/deltegui/owl/localizer"
	"github.com/deltegui/valtruc"

	"github.com/julienschmidt/httprouter"
)

type Handler func(ctx Ctx) error

type Middleware func(next Handler) Handler

type Mux struct {
	router   *httprouter.Router
	cypher   core.Cypher
	locStore *localizer.Store

	middlewares []Middleware
}

func New(cy core.Cypher) *Mux {
	return &Mux{
		router:   httprouter.New(),
		locStore: nil,
		cypher:   cy,
	}
}

func (mux *Mux) createContext(w http.ResponseWriter, req *http.Request, params httprouter.Params) Ctx {
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

func (mux *Mux) Handle(method, pattern string, handler Handler, middlewares ...Middleware) {
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

func (mux *Mux) Get(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodGet, pattern, handler, middlewares...)
}

func (mux *Mux) Post(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodPost, pattern, handler, middlewares...)
}

func (mux *Mux) Patch(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodPatch, pattern, handler, middlewares...)
}

func (mux *Mux) Delete(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodDelete, pattern, handler, middlewares...)
}

func (mux *Mux) Head(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodHead, pattern, handler, middlewares...)
}

func (mux *Mux) Options(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodOptions, pattern, handler, middlewares...)
}

func (mux *Mux) Put(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodPut, pattern, handler, middlewares...)
}

func (mux *Mux) Trace(pattern string, handler Handler, middlewares ...Middleware) {
	mux.Handle(http.MethodTrace, pattern, handler, middlewares...)
}

func (mux *Mux) Static(path string) {
	mux.router.NotFound = http.FileServer(http.Dir(path))
}

func (mux *Mux) StaticEmbedded(fs embed.FS) {
	mux.router.NotFound = http.FileServer(http.FS(fs))
}

func (mux *Mux) StaticMount(url, path string) {
	mux.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.Dir(path))
}

func (mux *Mux) StaticMountEmbedded(url string, fs embed.FS) {
	mux.router.ServeFiles(fmt.Sprintf("%s/*filepath", url), http.FS(fs))
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

func (mux Mux) Listen(address string) {
	server := http.Server{
		Addr:    address,
		Handler: mux.router,
	}
	go startServer(&server)
	waitAndStopServer(&server)
}

func (mux *Mux) AddLocalization(fs embed.FS, sharedKey, errorKey string) {
	store := localizer.NewLocalizerStore(fs, sharedKey, errorKey, mux.cypher)
	mux.locStore = &store
}

func (mux *Mux) Use(middleware Middleware) {
	mux.middlewares = append(mux.middlewares, middleware)
}

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
