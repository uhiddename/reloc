package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

const indexHTML = `<!doctype html><title>reloc</title><script type="module" src="/assets/index-3f9a1c.js"></script>`

// built is what npm run build leaves in dist.
var built = fstest.MapFS{
	"index.html":              {Data: []byte(indexHTML)},
	"favicon.svg":             {Data: []byte("<svg/>")},
	"assets/index-3f9a1c.js":  {Data: []byte("console.log(1)")},
	"assets/index-77b0e2.css": {Data: []byte("body{}")},
}

const (
	cacheForever = "public, max-age=31536000, immutable"
	cacheNever   = "no-cache"
)

func get(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// Every answer, a file or an error, carries the security headers.
func wantSecurityHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for name, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
	} {
		if got := w.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestServeFiles(t *testing.T) {
	h := newHandler(built)
	tests := []struct {
		path, body, contentType, cache string
	}{
		// hashed names change with the content: cache them for good
		{"/assets/index-3f9a1c.js", "console.log(1)", "text/javascript", cacheForever},
		{"/assets/index-77b0e2.css", "body{}", "text/css", cacheForever},
		// stable names: the browser must ask again every time
		{"/favicon.svg", "<svg/>", "image/svg+xml", cacheNever},
		{"/", indexHTML, "text/html", cacheNever},
		// no redirect to "/", unlike http.FileServer
		{"/index.html", indexHTML, "text/html", cacheNever},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			w := get(t, h, http.MethodGet, tt.path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if got := w.Body.String(); got != tt.body {
				t.Errorf("body = %q, want %q", got, tt.body)
			}
			if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, tt.contentType) {
				t.Errorf("Content-Type = %q, want %s", got, tt.contentType)
			}
			if got := w.Header().Get("Cache-Control"); got != tt.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.cache)
			}
			wantSecurityHeaders(t, w)
		})
	}
}

// Paths of the router in the browser have no file behind them: they get
// index.html, and the router shows the page.
func TestFallbackToIndex(t *testing.T) {
	h := newHandler(built)
	for _, path := range []string{
		"/servers",
		"/servers/7",
		"/servers/7/clients/",
		"/robots.txt",
		// a directory is not served as such, there is no listing
		"/assets",
	} {
		t.Run(path, func(t *testing.T) {
			w := get(t, h, http.MethodGet, path)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			if got := w.Body.String(); got != indexHTML {
				t.Errorf("body = %q, want index.html", got)
			}
			if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", got)
			}
			if got := w.Header().Get("Cache-Control"); got != cacheNever {
				t.Errorf("Cache-Control = %q, want %q", got, cacheNever)
			}
			wantSecurityHeaders(t, w)
		})
	}
}

// A missing file under assets/ is a 404, not index.html: a page loaded before
// a deploy asks for the old hashed names, and a 200 with HTML in place of a
// script fails in the browser in a confusing way.
func TestMissingAssetNotFound(t *testing.T) {
	h := newHandler(built)
	for _, path := range []string{
		"/assets/index-000000.js",
		"/assets/",
		"/assets/nested/x.js",
	} {
		t.Run(path, func(t *testing.T) {
			w := get(t, h, http.MethodGet, path)
			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", w.Code)
			}
			if strings.Contains(w.Body.String(), indexHTML) {
				t.Error("index.html served for a missing asset")
			}
			wantSecurityHeaders(t, w)
		})
	}
}

func TestHead(t *testing.T) {
	h := newHandler(built)
	w := get(t, h, http.MethodHead, "/servers/7")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("HEAD returned a body: %q", w.Body)
	}
}

// Requests are served in parallel, and each must get the whole index.html:
// nothing may be shared between them that has a read position. Run with
// -race, as CI does.
func TestConcurrentRequests(t *testing.T) {
	h := newHandler(built)
	var wg sync.WaitGroup
	for range 20 {
		for _, path := range []string{"/", "/servers/7"} {
			wg.Go(func() {
				w := get(t, h, http.MethodGet, path)
				if got := w.Body.String(); got != indexHTML {
					t.Errorf("%s: body = %q, want index.html", path, got)
				}
			})
		}
	}
	wg.Wait()
}

// Without npm run build dist holds only .gitkeep. Go still builds, and every
// page says what is missing instead of a blank screen.
func TestNotBuilt(t *testing.T) {
	h := newHandler(fstest.MapFS{".gitkeep": {}})
	for _, path := range []string{"/", "/index.html", "/servers/7", "/.gitkeep"} {
		t.Run(path, func(t *testing.T) {
			w := get(t, h, http.MethodGet, path)
			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", w.Code)
			}
			if !strings.Contains(w.Body.String(), "not built") {
				t.Errorf("body = %q, want it to say the UI is not built", w.Body)
			}
			wantSecurityHeaders(t, w)
		})
	}
}

// The embedded dist: built or not, depending on the machine, but it is
// served either way.
func TestHandler(t *testing.T) {
	w := get(t, Handler(), http.MethodGet, "/")
	switch w.Code {
	case http.StatusOK, http.StatusNotFound:
	default:
		t.Errorf("status = %d, want 200 or 404", w.Code)
	}
	wantSecurityHeaders(t, w)
}
