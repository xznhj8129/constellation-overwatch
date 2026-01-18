package metrics

import (
	"net/http"
	"net/http/pprof"
)

// RegisterPProf registers pprof handlers on the given mux
// The protect function wraps handlers with authentication if needed
func RegisterPProf(mux *http.ServeMux, protect func(http.HandlerFunc) http.Handler) {
	// Index
	mux.Handle("/debug/pprof/", protect(pprof.Index))
	mux.Handle("/debug/pprof/cmdline", protect(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", protect(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", protect(pprof.Symbol))
	mux.Handle("/debug/pprof/trace", protect(pprof.Trace))

	// Individual profiles
	mux.Handle("/debug/pprof/goroutine", protect(pprof.Handler("goroutine").ServeHTTP))
	mux.Handle("/debug/pprof/heap", protect(pprof.Handler("heap").ServeHTTP))
	mux.Handle("/debug/pprof/allocs", protect(pprof.Handler("allocs").ServeHTTP))
	mux.Handle("/debug/pprof/block", protect(pprof.Handler("block").ServeHTTP))
	mux.Handle("/debug/pprof/mutex", protect(pprof.Handler("mutex").ServeHTTP))
	mux.Handle("/debug/pprof/threadcreate", protect(pprof.Handler("threadcreate").ServeHTTP))
}

// RegisterPProfUnprotected registers pprof handlers without authentication
// Use with caution - only for development environments
func RegisterPProfUnprotected(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Individual profiles
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
}
