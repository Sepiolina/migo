package httpx

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"miservice/internal/logx"
)

// Handler is an HTTP handler that reports failure by returning an error.
type Handler func(w http.ResponseWriter, r *http.Request) error

// Route describes one registered endpoint (used for the startup banner).
type Route struct{ Method, Path string }

type endpoint struct {
	path     string // as registered, for display
	byMethod map[string]Handler
	any      Handler
}

// Router matches requests the way the original Express app did: paths are
// case-insensitive and tolerate a trailing slash, and anything unmatched is
// a plain-text "Cannot GET /x" 404.
type Router struct {
	log        logx.Logger
	endpoints  map[string]*endpoint // key: normalised path
	routes     []Route
	middleware []Middleware
}

// NewRouter creates an empty router. log receives handler errors.
func NewRouter(log logx.Logger) *Router {
	return &Router{log: log, endpoints: map[string]*endpoint{}}
}

func normalise(path string) string { return strings.ToLower(strings.TrimRight(path, "/")) }

// GET registers h for GET (and HEAD, like Express' app.get).
func (rt *Router) GET(path string, h Handler) {
	rt.add(path, h, http.MethodGet, http.MethodHead)
	rt.routes = append(rt.routes, Route{http.MethodGet, path})
}

// POST registers h for POST.
func (rt *Router) POST(path string, h Handler) {
	rt.add(path, h, http.MethodPost)
	rt.routes = append(rt.routes, Route{http.MethodPost, path})
}

// Any registers h for every method on path.
func (rt *Router) Any(path string, h Handler) {
	rt.add(path, h)
	rt.routes = append(rt.routes, Route{"ANY", path})
}

// Use adds middleware; the first one added is the outermost.
func (rt *Router) Use(mw ...Middleware) { rt.middleware = append(rt.middleware, mw...) }

// Routes lists every registered endpoint, sorted by path.
func (rt *Router) Routes() []Route {
	out := append([]Route(nil), rt.routes...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// add panics on a duplicate: two modules claiming the same URL is a
// programming error that must surface at startup, not as a silent override.
func (rt *Router) add(path string, h Handler, methods ...string) {
	key := normalise(path)
	ep := rt.endpoints[key]
	if ep == nil {
		ep = &endpoint{path: path, byMethod: map[string]Handler{}}
		rt.endpoints[key] = ep
	}
	if len(methods) == 0 {
		if ep.any != nil || len(ep.byMethod) > 0 {
			panic(fmt.Sprintf("httpx: route %s is already registered", path))
		}
		ep.any = h
		return
	}
	for _, m := range methods {
		if _, dup := ep.byMethod[m]; dup || ep.any != nil {
			panic(fmt.Sprintf("httpx: route %s %s is already registered", m, path))
		}
		ep.byMethod[m] = h
	}
}

// Handler returns the http.Handler to serve: middleware wrapped around the
// route dispatcher.
func (rt *Router) Handler() http.Handler {
	var h http.Handler = http.HandlerFunc(rt.dispatch)
	for i := len(rt.middleware) - 1; i >= 0; i-- {
		h = rt.middleware[i](h)
	}
	return h
}

func (rt *Router) dispatch(w http.ResponseWriter, r *http.Request) {
	if ep := rt.endpoints[normalise(r.URL.Path)]; ep != nil {
		h := ep.byMethod[r.Method]
		if h == nil {
			h = ep.any
		}
		if h != nil {
			if err := h(w, r); err != nil {
				rt.respondError(w, err)
			}
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, "Cannot %s %s", r.Method, r.URL.Path)
}
