package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

func requireToken(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		// constant time: == would leak the token through response timing
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusRecorder remembers the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the original writer.
func (w *statusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// logRequests logs every request after it is served. It does not log headers
// or the query string: they may hold the token and other secrets.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// 200 is what net/http sends if the handler never calls WriteHeader
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).Round(time.Microsecond),
		)
	})
}

type trackingWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *trackingWriter) WriteHeader(status int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *trackingWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

func (w *trackingWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// recoverPanics turns a panic in a handler into a logged error and a JSON
// 500, instead of the dropped connection net/http leaves by default.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := &trackingWriter{ResponseWriter: w}
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			// a deliberate abort: net/http handles it and stays silent
			if v == http.ErrAbortHandler {
				panic(v)
			}
			// no headers or query string, as in logRequests; the stack is
			// still that of the panic, the defer runs before unwinding
			slog.Error("panic",
				"method", r.Method,
				"path", r.URL.Path,
				"panic", v,
				"stack", string(debug.Stack()),
			)
			// The status line is out: a 500 can't follow it. Abort, so the
			// client sees a broken response, not a cut one that looks whole.
			if tw.wroteHeader {
				panic(http.ErrAbortHandler)
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse{"internal error"})
		}()
		next.ServeHTTP(tw, r)
	})
}
