package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"webrts/server/internal/game"
)

// registerTileLibraryRoutes wires the tile-library endpoints: the read-only
// catalog listing, image upload, image serve, and delete. Mirrors
// registerTilesetRoutes. No auth, matching the item/unit/ability/tileset
// editors (dev/desktop tool); server-side validation is the gate.
func registerTileLibraryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/catalog/tiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		writeJSON(w, map[string]any{"tiles": game.ListTileAssets()})
	})

	// /tiles/ subtree handles two shapes:
	//   GET            /tiles/images/{key} -> serve an uploaded tile PNG
	//   POST/DELETE    /tiles/{id}         -> upload / remove a tile image
	// The images/ prefix MUST be checked first — otherwise a request for
	// /tiles/images/foo.png would be parsed as an id of "images/foo.png" by
	// the branch below.
	mux.HandleFunc("/tiles/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/tiles/")

		if key, isImageGet := strings.CutPrefix(rest, "images/"); isImageGet {
			if r.Method != http.MethodGet {
				writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
				return
			}
			path, ok := game.TileImagePath(key)
			if !ok {
				http.NotFound(w, r)
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(data)
			return
		}

		id := rest
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /tiles/{id}")
			return
		}

		switch r.Method {
		case http.MethodPost:
			data, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 2*1024*1024+1))
			if rerr != nil {
				writeJSONError(w, http.StatusBadRequest, "read_failed", rerr.Error())
				return
			}
			key, err := game.SaveTileImage(id, data)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "image_rejected", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "saved", "image": key})

		case http.MethodDelete:
			if _, err := game.DeleteTileImage(id); err != nil {
				if game.IsTilesetValidationError(err) {
					writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
					return
				}
				writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
				return
			}
			writeJSON(w, map[string]string{"id": id, "status": "deleted"})

		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST or DELETE only")
		}
	})
}
