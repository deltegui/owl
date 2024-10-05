package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/deltegui/owl"
)

const (
	CorsAny    string = "*"
	corsMaxAge int    = 864000
)

var corsDefaultOptions = CorsOptions{
	AllowOrigin:  CorsAny,
	AllowMethods: []string{CorsAny},
	AllowHeaders: []string{CorsAny},
	MaxAge:       corsMaxAge,
}

type CorsOptions struct {
	AllowOrigin  string
	AllowMethods []string
	AllowHeaders []string
	MaxAge       int
}

func CorsDefault() owl.Middleware {
	return Cors(corsDefaultOptions)
}

func (opt CorsOptions) isOriginAllowed(origin string) bool {
	if len(opt.AllowOrigin) == 0 || opt.AllowOrigin == CorsAny {
		return true
	}
	reqOrigin := origin
	if len(reqOrigin) == 0 || reqOrigin != opt.AllowOrigin {
		return false
	}
	return true
}

func (opt CorsOptions) isAllHeadersAllowed(headers []string) bool {
	if len(opt.AllowHeaders) == 0 || len(opt.AllowHeaders) == 1 && opt.AllowHeaders[0] == CorsAny {
		return true
	}
next:
	for _, rh := range headers {
		for _, ah := range opt.AllowHeaders {
			if rh == ah {
				continue next
			}
		}
		return false
	}
	return true
}

func (opt CorsOptions) isMethodAllowed(method string) bool {
	if len(opt.AllowMethods) == 0 || (len(opt.AllowMethods) == 1 && opt.AllowMethods[0] == CorsAny) {
		return true
	}
	for _, allowed := range opt.AllowMethods {
		if allowed == method {
			return true
		}
	}
	return false
}

func getHeadersNames(ctx owl.Ctx) []string {
	flatten := make([]string, len(ctx.Req.Header))
	i := 0
	for key := range ctx.Req.Header {
		flatten[i] = key
		i++
	}
	return flatten
}

func Cors(opt CorsOptions) owl.Middleware {
	return func(next owl.Handler) owl.Handler {
		return func(ctx owl.Ctx) error {
			if ctx.Req.Method == http.MethodOptions {
				ctx.Res.Header().Add("Access-Control-Allow-Origin", opt.AllowOrigin)
				ctx.Res.Header().Add("Access-Control-Allow-Methods", strings.Join(opt.AllowMethods, ", "))
				ctx.Res.Header().Add("Access-Control-Max-Age", strconv.Itoa(opt.MaxAge))
				ctx.Res.Header().Add("Access-Control-Allow-Credentials", "true")

				reqMethod := ctx.Req.Header.Get("Access-Control-Request-Method")
				if len(reqMethod) > 0 && !opt.isMethodAllowed(reqMethod) {
					return ctx.Forbidden("Method not allowed by CORS preflight: %s", reqMethod)
				}

				reqHeaders := ctx.Req.Header.Get("Access-Control-Request-Headers")
				if len(reqHeaders) > 0 && !opt.isAllHeadersAllowed(strings.Split(reqHeaders, ", ")) {
					return ctx.Forbidden("Request headers not allowed")
				}

				return ctx.NotContent()
			}
			if !opt.isMethodAllowed(ctx.Req.Method) {
				return ctx.Forbidden("Method not allowed by CORS: %s", ctx.Req.Method)
			}
			origin := ctx.Req.Header.Get("Origin")
			if len(origin) != 0 && !opt.isOriginAllowed(origin) {
				return ctx.Forbidden("Origin not allowed by CORS")
			}
			if !opt.isAllHeadersAllowed(getHeadersNames(ctx)) {
				return ctx.Forbidden("Request headers not allowed")
			}
			err := next(ctx)
			ctx.Res.Header().Add("Access-Control-Allow-Origin", opt.AllowOrigin)
			return err
		}
	}
}