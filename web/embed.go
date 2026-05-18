// Package web embeds the built dashboard SPA and serves it with SPA-routing
// fallback. The dist/ tree is produced by `npm run build` (committed so the
// Go module builds without a node toolchain).
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler serving the embedded SPA: real files are
// served from dist/ (hashed assets cached immutably); any other path returns
// dist/index.html (200) so client-side routing works.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p != "" {
			if f, err := sub.Open(p); err == nil {
				if st, e := f.Stat(); e == nil && !st.IsDir() {
					_ = f.Close()
					if strings.HasPrefix(p, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					fileServer.ServeHTTP(w, r)
					return
				}
				_ = f.Close()
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	}), nil
}
