package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"webrts/server/internal/game"
)

// registerTilesetRoutes wires the tileset-editor endpoints: the read-only
// catalog listing, def save, sheet-image upload/serve, and delete. No auth,
// matching the item/unit/ability editors (dev/desktop tool); server-side
// validation is the gate.
func registerTilesetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/catalog/tilesets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		writeJSON(w, map[string]any{"tilesets": game.ListTilesetDefs()})
	})

	mux.HandleFunc("/tilesets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var def game.TilesetDef
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveTilesetDef(def); err != nil {
			if game.IsTilesetValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": def.ID, "status": "saved"})
	})

	// /tilesets/ subtree handles three shapes:
	//   GET    /tilesets/images/{key}  -> serve an uploaded sheet PNG
	//   POST   /tilesets/{id}/image    -> upload/replace a sheet PNG
	//   DELETE /tilesets/{id}          -> remove an author-created def
	// The images/ prefix MUST be checked first — otherwise a request for
	// /tilesets/images/foo.png would be parsed as a delete/upload id of
	// "images/foo.png" by the branches below.
	mux.HandleFunc("/tilesets/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/tilesets/")

		if key, isImageGet := strings.CutPrefix(rest, "images/"); isImageGet {
			if r.Method != http.MethodGet {
				writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
				return
			}
			path, ok := game.TilesetImagePath(key)
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

		if id, isImageUpload := strings.CutSuffix(rest, "/image"); isImageUpload {
			if r.Method != http.MethodPost {
				writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
				return
			}
			if id == "" || strings.Contains(id, "/") {
				writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /tilesets/{id}/image")
				return
			}
			data, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 4*1024*1024+1))
			if rerr != nil {
				writeJSONError(w, http.StatusBadRequest, "read_failed", rerr.Error())
				return
			}
			key, err := game.SaveTilesetImage(id, data)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "image_rejected", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "image_saved", "image": key})
			return
		}

		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		id := rest
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /tilesets/{id}")
			return
		}
		if _, err := game.DeleteTilesetDef(id); err != nil {
			if game.IsTilesetValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		writeJSON(w, map[string]string{"id": id, "status": "deleted"})
	})
}
