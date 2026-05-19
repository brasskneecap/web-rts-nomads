//go:build !embed_spa

package main

import "net/http"

// newSPAHandler returns (nil, nil) in non-embed_spa builds so the router stays
// API-only and the air dev workflow is preserved unchanged.
func newSPAHandler() (http.Handler, error) {
	return nil, nil
}
