// Package embedded serves the bundled Vue SPA from an embed.FS provided by
// the caller. Used by the embed_spa-tagged build of cmd/api; the no-tag build
// passes a nil handler and the router degrades to API-only.
package embedded

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

// apiPrefixes lists top-level path prefixes that MUST NOT be shadowed by the
// SPA fallthrough. The mux normally catches these first; this list is defense
// in depth so a missing route registration cannot accidentally serve index.html
// in place of an API 404.
var apiPrefixes = []string{"/ws", "/health", "/api", "/catalog", "/maps", "/matches", "/lobbies", "/tilesets"}

// Handler returns an http.Handler that serves the SPA from distFS. distFS must
// be rooted at the SPA's dist/ directory (i.e. index.html sits at the root of
// distFS). The handler:
//   - serves index.html for "/"
//   - serves any regular file present in distFS with class-appropriate cache headers
//   - returns index.html for unknown paths (Vue Router fallthrough), with no-cache
//   - returns 404 for paths under one of apiPrefixes (defense in depth)
//   - rejects methods other than GET and HEAD
func Handler(distFS fs.FS) (http.Handler, error) {
	if distFS == nil {
		return nil, errors.New("embedded: distFS is nil")
	}
	if _, err := fs.Stat(distFS, "index.html"); err != nil {
		return nil, errors.New("embedded: index.html missing from distFS")
	}
	fileServer := http.FileServer(http.FS(distFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		urlPath := r.URL.Path
		if isAPIPath(urlPath) {
			http.NotFound(w, r)
			return
		}
		if urlPath == "/" {
			serveIndex(w, distFS)
			return
		}
		clean := strings.TrimPrefix(urlPath, "/")
		if info, err := fs.Stat(distFS, clean); err == nil && !info.IsDir() {
			setCacheHeader(w, clean)
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, distFS)
	}), nil
}

func isAPIPath(p string) bool {
	for _, prefix := range apiPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

func serveIndex(w http.ResponseWriter, distFS fs.FS) {
	data, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func setCacheHeader(w http.ResponseWriter, cleanPath string) {
	if strings.HasPrefix(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
}
