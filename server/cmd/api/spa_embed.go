//go:build embed_spa

package main

import (
	"embed"
	"io/fs"
	"net/http"

	"webrts/server/internal/embedded"
)

// spaDistFS holds the Vue SPA's built output. The Makefile (or equivalent
// packaging script) stages client/src/game-portal/dist/ into ./dist/ before
// invoking `go build -tags embed_spa`; Go's //go:embed cannot reach files
// outside the module so the staging step is non-optional.
//
//go:embed all:dist
var spaDistFS embed.FS

func newSPAHandler() (http.Handler, error) {
	sub, err := fs.Sub(spaDistFS, "dist")
	if err != nil {
		return nil, err
	}
	return embedded.Handler(sub)
}
