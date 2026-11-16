// Package web serves the browser UI: the files that npm run build leaves in
// web/dist, embedded into the binary at compile time.
//
// Paths without a file behind them get index.html, so that the router in the
// browser can show the page; only a missing file under assets/ is a 404.
package web

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

const (
	// scripts, styles and images only from this origin, and no framing by
	// other sites
	contentSecurityPolicy = "default-src 'self'; frame-ancestors 'none'"
	// the browser must trust Content-Type, not guess it from the content
	xContentTypeOptions = "nosniff"
	// Vite puts a hash of the content into the names under assets/, so a
	// name always has the same content; a year in seconds is the usual "forever"
	cacheControlForever = "public, max-age=31536000, immutable"
	// may be stored, but must be checked with the server before each use:
	// index.html names the current assets
	cacheControlNever = "no-cache"

	indexName    = "index.html"
	assetsPrefix = "assets/"
)

// all: keeps dist/.gitkeep, so that the package builds before npm run build
//
//go:embed all:dist
var dist embed.FS

// Handler serves the embedded UI and refuses methods other than GET and HEAD.
// The caller routes the API elsewhere.
func Handler() http.Handler {
	fsys, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return newHandler(fsys)
}

// newHandler serves the UI from files: the embedded dist or a test fixture.
// Without index.html every request is a 404 that says the UI is not built.
func newHandler(files fs.FS) http.Handler {
	// Read once into bytes, not kept open: an open file has one read position,
	// and requests are served in parallel.
	index, err := fs.ReadFile(files, indexName)
	if err != nil {
		return allowGetHead(notBuilt())
	}
	return allowGetHead(&builtUI{
		files: files,
		index: index,
	})
}

func notBuilt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		setSecurityHeaders(h)
		http.Error(w,
			"UI is not built: run npm run build in web/",
			http.StatusNotFound,
		)
	}
}

type builtUI struct {
	files fs.FS
	index []byte
}

func (u *builtUI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	setSecurityHeaders(h)
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = indexName
	}
	f, err := openFile(u.files, name)
	var content io.ReadSeeker
	switch {
	case err == nil:
		// a file, including index.html itself, served as it is opened,
		// without a copy
		defer func() { _ = f.Close() }()
		// ServeContent needs Seek; files of embed.FS and fstest.MapFS have it,
		// but fs.File does not promise it
		rs, ok := f.(io.ReadSeeker)
		if !ok {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		content = rs
		if strings.HasPrefix(name, assetsPrefix) {
			h.Set("Cache-Control", cacheControlForever)
		} else {
			h.Set("Cache-Control", cacheControlNever)
		}
	case strings.HasPrefix(name, assetsPrefix):
		// Not index.html: a page loaded before a deploy asks for old asset
		// names, and HTML in place of a script fails in a confusing way.
		http.NotFound(w, r)
		return
	default:
		// a page of the router; the name decides Content-Type in ServeContent
		h.Set("Cache-Control", cacheControlNever)
		name, content = indexName, bytes.NewReader(u.index)
	}
	// no modification time: embedded files have none, and a made-up one
	// would let the browser get 304 for a changed index.html
	http.ServeContent(w, r, name, time.Time{}, content)
}

// allowGetHead refuses every method but GET and HEAD: the UI only reads.
func allowGetHead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			next.ServeHTTP(w, r)
		default:
			setSecurityHeaders(w.Header())
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func setSecurityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", xContentTypeOptions)
}

// openFile opens a regular file. A directory counts as missing, as it does
// for fs.ReadFile: there is nothing in it to serve.
func openFile(files fs.FS, name string) (fs.File, error) {
	f, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && info.IsDir() {
		err = fs.ErrNotExist
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
