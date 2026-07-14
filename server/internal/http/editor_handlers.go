package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"webrts/server/internal/game"
)

// registerEditorRoutes wires the item-editor endpoints. No auth, matching the
// map editor (dev/desktop tool); server-side validation is the gate.
func registerEditorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorItemSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorItem(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Item.ID, "status": "saved"})
	})

	mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/items/")
		if rest, isAvail := strings.CutSuffix(id, "/availability"); isAvail && r.Method == http.MethodGet {
			av, found := game.GetItemAvailability(rest)
			if !found {
				writeJSONError(w, http.StatusNotFound, "not_found", "no item "+rest)
				return
			}
			writeJSON(w, av)
			return
		}
		if rest, isImage := strings.CutSuffix(id, "/image"); isImage && r.Method == http.MethodPost {
			data, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024+1))
			if rerr != nil {
				writeJSONError(w, http.StatusBadRequest, "read_failed", rerr.Error())
				return
			}
			if err := game.SaveItemIcon(rest, data); err != nil {
				writeJSONError(w, http.StatusBadRequest, "icon_rejected", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": rest, "status": "icon_saved"})
			return
		}
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /items/{id}")
			return
		}
		existed, err := game.DeleteEditorItem(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.ItemIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	mux.HandleFunc("/units", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorUnitSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorUnit(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Unit.Type, "status": "saved"})
	})

	mux.HandleFunc("/units/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/units/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /units/{id}")
			return
		}
		existed, err := game.DeleteEditorUnit(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.UnitIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	mux.HandleFunc("/factions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorFactionSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorFaction(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Faction.ID, "status": "saved"})
	})

	mux.HandleFunc("/factions/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/factions/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /factions/{id}")
			return
		}
		existed, err := game.DeleteEditorFaction(id)
		if err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no faction record for "+id)
			return
		}
		status := "deleted"
		if game.FactionIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	mux.HandleFunc("/abilities", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorAbilitySaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorAbility(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Ability.ID, "status": "saved"})
	})

	mux.HandleFunc("/abilities/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/abilities/")
		if rest, isImage := strings.CutSuffix(id, "/image"); isImage && r.Method == http.MethodPost {
			data, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 256*1024+1))
			if rerr != nil {
				writeJSONError(w, http.StatusBadRequest, "read_failed", rerr.Error())
				return
			}
			if err := game.SaveAbilityIcon(rest, data); err != nil {
				writeJSONError(w, http.StatusBadRequest, "icon_rejected", err.Error())
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": rest, "status": "icon_saved"})
			return
		}
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /abilities/{id}")
			return
		}
		existed, err := game.DeleteEditorAbility(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.AbilityIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	// POST /paths body: { "unit": string, "path": <raw pathCatalogFile JSON> }.
	// Routes MUST go through the SaveEditorPath* (not SavePathDef) — that's
	// where the single-owner guard and per-file validation live.
	mux.HandleFunc("/paths", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req struct {
			Unit string          `json:"unit"`
			Path json.RawMessage `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorPathFromJSON(req.Unit, req.Path); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		// The path id lives inside the raw path JSON body (pathCatalogFile's
		// own "path" field) — pull it back out for the response.
		var pathID struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(req.Path, &pathID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": pathID.Path, "status": "saved"})
	})

	mux.HandleFunc("/paths/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/paths/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /paths/{id}")
			return
		}
		existed, err := game.DeleteEditorPath(id)
		if err != nil {
			// A path still referenced by a unit's pathChances is a
			// validation error (author-fixable — the message names the
			// referencing unit), not an infrastructure failure. Mirrors
			// /factions/ DELETE's still-has-units 400.
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.PathIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	// POST /perks body: { "unit": string, "path": string, "rank": string,
	// "perks": <raw perk-entry array JSON> }.
	mux.HandleFunc("/perks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req struct {
			Unit  string          `json:"unit"`
			Path  string          `json:"path"`
			Rank  string          `json:"rank"`
			Perks json.RawMessage `json:"perks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorPerkPoolFromJSON(req.Unit, req.Path, req.Rank, req.Perks); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"unit": req.Unit, "path": req.Path, "rank": req.Rank, "status": "saved",
		})
	})

	// DELETE /perks/{unit}/{path}/{rank} — exactly 3 non-empty segments.
	mux.HandleFunc("/perks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/perks/")
		segs := strings.Split(rest, "/")
		if len(segs) != 3 || segs[0] == "" || segs[1] == "" || segs[2] == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /perks/{unit}/{path}/{rank}")
			return
		}
		unitType, pathName, rank := segs[0], segs[1], segs[2]
		existed, err := game.DeleteEditorPerkPool(unitType, pathName, rank)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+rest)
			return
		}
		status := "deleted"
		if game.PerkPoolIsEmbedded(unitType, pathName, rank) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"unit": unitType, "path": pathName, "rank": rank, "status": status})
	})
}
