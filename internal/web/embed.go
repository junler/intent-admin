// Package web embeds the built frontend into the binary and serves it as a
// single-page application.
//
// `make build` builds web/ and copies the output into dist/ here. A
// placeholder.html is committed so the backend always compiles on
// its own — a contributor working only on Go code never has to install Node.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist placeholder.html
var embedded embed.FS

// startedAt is used as the modification time for the SPA shell. Embedded
// files all report a zero mtime, which makes conditional requests useless;
// the process start time is stable within a deployment and changes on deploy,
// which is exactly the cache behaviour wanted here.
var startedAt = time.Now()

// Handler serves the SPA. Requests under apiPrefix are never handled here —
// an unmatched API path must return a JSON 404, not the HTML shell, or every
// frontend typo looks like a working page that renders nothing.
func Handler(apiPrefix string) http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("web: dist 目录缺失，请执行 make build-web")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiPrefix != "" && strings.HasPrefix(r.URL.Path, apiPrefix) {
			http.NotFound(w, r)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			serveIndex(w, r, sub)
			return
		}

		data, err := fs.ReadFile(sub, name)
		if err != nil {
			// Unknown path: hand it to the client-side router.
			serveIndex(w, r, sub)
			return
		}

		// Build output is content-hashed, so it can be cached indefinitely.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeContent(w, r, path.Base(name), startedAt, bytes.NewReader(data))
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		data, err = embedded.ReadFile("placeholder.html")
		if err != nil {
			http.Error(w, "前端资源未构建，请执行 make build-web", http.StatusInternalServerError)
			return
		}
	}
	// The shell must never be cached: it references hashed asset names that
	// change on every deploy.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", startedAt, bytes.NewReader(data))
}
