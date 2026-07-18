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

	// GET /items/{id}/availability is gone: "where is this item available" is now
	// answered by list membership, which the Lists tab owns.
	mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/items/")
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
		// status is "deleted" (author-created item removed), "reverted" (shipped
		// item taken back to the state before the last save) or "reset" (shipped
		// item taken back to the catalog default) — DeleteEditorItem decides.
		status, existed, err := game.DeleteEditorItem(id)
		if err != nil {
			// An item still referenced by a list, another item's recipe, an
			// upgrade, or a map is a validation error (author-fixable — the
			// message names every referencing site), not an infrastructure
			// failure. Mirrors /factions/ DELETE's still-has-units 400.
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
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	// Lists: the grouping primitive shops, recipe shops, artificers and camps all
	// bind to. Authorable end-to-end from the editor's Lists tab — before this
	// there was no write route at all, and no loader, so an authored list could
	// not even survive a restart.
	mux.HandleFunc("/lists", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorListSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorList(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.List.ID, "status": "saved"})
	})

	mux.HandleFunc("/lists/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/lists/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /lists/{id}")
			return
		}
		existed, err := game.DeleteEditorList(id)
		if err != nil {
			// A list still referenced by a table, a map building, or a
			// neutral group is a validation error (author-fixable — the
			// message names every referencing site), not an infrastructure
			// failure. Mirrors /factions/ DELETE's still-has-units 400.
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
		writeJSON(w, map[string]string{"id": id, "status": "deleted"})
	})

	mux.HandleFunc("/tables", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorTableSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorTable(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Table.ID, "status": "saved"})
	})

	mux.HandleFunc("/tables/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/tables/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /tables/{id}")
			return
		}
		existed, err := game.DeleteEditorTable(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		writeJSON(w, map[string]string{"id": id, "status": "deleted"})
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

	// /abilities/validate is a dry-run: it decodes the same body shape as POST
	// /abilities but never saves and never 400s on content — it always returns
	// 200 with a (possibly empty) structured issues list so the editor can
	// annotate cards without a save round-trip. Registered as its own EXACT
	// pattern (not folded into the "/abilities/" catch-all below): net/http's
	// ServeMux prefers the longer exact match "/abilities/validate" over the
	// "/abilities/" subtree pattern, so this route wins for that one path and
	// every other "/abilities/{id}..." request still falls through to the
	// catch-all unaffected.
	mux.HandleFunc("/abilities/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorAbilitySaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		writeJSON(w, map[string]any{"issues": game.EditorAbilityIssues(req.Ability)})
	})

	// /abilities/preview wraps the (Phase 6a, Task 2) deterministic preview
	// harness, game.RunAbilityPreview: the editor posts a candidate ability
	// def + a scene (caster position, scene units, cast target/point,
	// simulated duration) and gets back the full execution trace plus a
	// per-unit HP-before/after summary, without touching a live match.
	// Registered as its own EXACT pattern (not folded into the
	// "/abilities/" catch-all below), mirroring "/abilities/validate" just
	// above: net/http's ServeMux prefers the longer exact match
	// "/abilities/preview" over the "/abilities/" subtree pattern, so this
	// route wins for that one path and every other "/abilities/{id}..."
	// request still falls through to the catch-all unaffected (see
	// TestPreviewEndpoint_DoesNotShadowExistingRoutes).
	mux.HandleFunc("/abilities/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		// The request body carries a full AbilityDef (incl. its composable
		// Program) — 512KB is generous headroom over any realistic ability.
		// A MaxBytesReader trip is a distinct failure mode from a decode
		// error (the body may be perfectly well-formed JSON, just too big),
		// so it gets its own code rather than being folded into
		// invalid_json — mirrors the /items/{id}/image and
		// /abilities/{id}/image routes' own read_failed.
		data, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, 512*1024))
		if rerr != nil {
			writeJSONError(w, http.StatusBadRequest, "body_too_large", rerr.Error())
			return
		}
		var req game.PreviewRequest
		if err := json.Unmarshal(data, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		// Sane defaults so a bare/near-empty request from the editor's first
		// "Run" click still previews something instead of failing:
		//  - DurationSeconds<=0 -> 2s of simulated time.
		//  - No scene units supplied -> one enemy 40px from the caster, and
		//    Target defaults to it. This covers an offensive ability's
		//    default preview; a heal/buff author still needs to place an
		//    ally scene unit explicitly (RunAbilityPreview's own target
		//    fallback then does something reasonable with it — see its doc
		//    comment). A point-target ability ignores Target and casts at
		//    CastX/CastY instead — if the caller didn't specify one either
		//    (both still zero), aim the cast at the SAME spot as the
		//    injected enemy so a bare point-AoE preview actually hits it
		//    rather than landing on the caster's own feet.
		if req.DurationSeconds <= 0 {
			req.DurationSeconds = 2.0
		}
		if len(req.Units) == 0 {
			enemyX, enemyY := req.CasterX+40, req.CasterY
			req.Units = []game.PreviewSceneUnit{{Team: "enemy", X: enemyX, Y: enemyY, HP: 200, MaxHP: 200}}
			req.Target = 0
			if req.CastX == 0 && req.CastY == 0 {
				req.CastX, req.CastY = enemyX, enemyY
			}
		}
		res, err := game.RunAbilityPreview(req)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "preview_failed", err.Error())
			return
		}
		writeJSON(w, res)
	})

	mux.HandleFunc("/abilities/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/abilities/")
		if rest, isConvert := strings.CutSuffix(id, "/convert"); isConvert {
			if r.Method != http.MethodPost {
				writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
				return
			}
			conv, warnings, err := game.ConvertLegacyAbility(rest)
			if err != nil {
				writeJSONError(w, http.StatusNotFound, "convert_failed", err.Error())
				return
			}
			writeJSON(w, map[string]any{
				"ability":  conv,
				"warnings": warnings,
				"runnable": game.AbilityProgramRunnable(conv.Program),
			})
			return
		}
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
		// status is "deleted" (author-created ability removed), "reverted"
		// (shipped ability taken back to the state before the last save) or
		// "reset" (shipped ability taken back to the catalog default) —
		// DeleteEditorAbility decides. Mirrors DELETE /items/{id}.
		status, existed, err := game.DeleteEditorAbility(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	mux.HandleFunc("/effects", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorEffectSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorEffect(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Effect.ID, "status": "saved"})
	})

	mux.HandleFunc("/effects/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/effects/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /effects/{id}")
			return
		}
		existed, err := game.DeleteEditorEffect(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.EffectIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})

	mux.HandleFunc("/projectiles", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorProjectileSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorProjectile(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Projectile.ID, "status": "saved"})
	})

	mux.HandleFunc("/projectiles/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/projectiles/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /projectiles/{id}")
			return
		}
		existed, err := game.DeleteEditorProjectile(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.ProjectileIsEmbedded(id) {
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

	// POST /perks body: { "perk": <PerkDef JSON> }.
	mux.HandleFunc("/perks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		var req game.EditorPerkSaveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveEditorPerk(req); err != nil {
			if game.IsEditorValidationError(err) {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": req.Perk.ID, "status": "saved"})
	})

	// DELETE /perks/{id}.
	mux.HandleFunc("/perks/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/perks/")
		if r.Method != http.MethodDelete {
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "DELETE only")
			return
		}
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", "expected /perks/{id}")
			return
		}
		existed, err := game.DeleteEditorPerk(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if !existed {
			writeJSONError(w, http.StatusNotFound, "not_found", "no editor override for "+id)
			return
		}
		status := "deleted"
		if game.PerkIsEmbedded(id) {
			status = "reset"
		}
		writeJSON(w, map[string]string{"id": id, "status": status})
	})
}
