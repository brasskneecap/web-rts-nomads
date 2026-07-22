# Tileset Editor SP1 (Data-driven tilesets + slice editor) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn hardcoded terrain sheets into an authored server-catalog of `TilesetDef`s with a visual slice editor, so terrain PNGs can be uploaded and sliced (tile size / grid / offset / spacing) without code changes.

**Architecture:** A new `tilesets` catalog on the Go server (embedded baseline + writable overlay + CRUD + PNG upload), mirroring the existing item/campaign catalogs. The client's `terrainTileset.ts` becomes data-driven (loads defs over HTTP instead of bundling PNGs). Placed tiles switch from pixel coords `{sheet,sx,sy}` to grid indices `{tileset,col,row}` with a load-time migration shim. A new `TilesetEditorPanel.vue` (on the shared `EditorShell`) uploads an image and dials in the slice grid with a live overlay.

**Tech Stack:** Go (server, `net/http`, `//go:embed`), Vue 3 + TypeScript (client), Vite. Reference editors to mirror: **Campaign** (catalog persistence + endpoints — the newest, cleanest example) and **Item** (image upload + editor panel).

## Global Constraints

- **Determinism:** No `math/rand`/wall-clock in simulation code. Editor-time randomness (already used) is fine; pathing must be deterministic. (`.claude/rules/AI_RULES.md`)
- **Client mirrors server:** walkability logic exists in BOTH `client/.../terrainTileset.ts` and `server/internal/game/pathing.go` and MUST stay identical. (existing invariant)
- **Cursor CSS:** component CSS must not set literal `cursor:` values; the global rules own it. `cursor: not-allowed` is the only allowed per-state exception. (`CLAUDE.md`)
- **No auto-commit beyond plan steps:** each task ends with an explicit commit; do not amend.
- **Commit trailers:** end commit messages with the two trailers from the repo's git guidance (Co-Authored-By + Claude-Session).
- **Server dev port is 8137** (not 8080) — set via `server/dev.bat`; the Vite proxy targets it.
- **Reference paths (verbatim):** logical tile size today is `32`; the 5 sheets and their sources are: `tileset`→`grass-dirt-elevation-25.png` (4×4, 32px), `grass-grass-25`→`new-grass-grass-elavation-640.png` (4×4, 160px), `dirt-dirt-25`→`new-dirt-dirt-elavation-640.png` (4×4, 160px), `grass-dirt-0`→`grass-dirt-elevation-0.png` (4×4, 32px), `grass-grass-8x8`→`8x8-grass-grass-1024.png` (8×8, 128px).

---

## File structure

**Server (new):**
- `server/internal/game/tileset_defs.go` — `TilesetDef` type, embedded baseline load, `currentTilesetDefs()`.
- `server/internal/game/tileset_persistence.go` — overlay, `SaveTilesetDef`, `DeleteTilesetDef`, `LoadPersistedTilesetsIntoOverlay`, image write.
- `server/internal/game/catalog/tilesets/*.json` — 5 embedded defs.
- `server/internal/game/catalog/tilesets/images/*.png` — copies of the 5 source PNGs.
- `server/internal/http/tileset_handlers.go` — `GET /catalog/tilesets`, `POST /tilesets`, `DELETE /tilesets/{id}`, `POST /tilesets/{id}/image`, `GET /tilesets/images/{key}`.

**Server (modify):**
- `server/pkg/protocol/messages.go` — `TileCoord` fields (`Tileset,Col,Row`), migration on unmarshal.
- `server/internal/game/pathing.go` — `isWalkableGroundTile` re-keyed to col/row.
- `server/internal/game/map_terrain_tile_groups.go` — group by `(tileset,col,row)` + legacy read.
- `server/cmd/api/main.go` — `LoadPersistedTilesetsIntoOverlay()` at startup.
- `server/internal/http/router.go` + `server/internal/embedded/handler.go` — register routes + `/tilesets` prefix.

**Client (new):**
- `client/.../services/tilesetEditorApi.ts` — save/delete/upload.
- `client/.../components/TilesetEditorPanel.vue` — the editor.

**Client (modify):**
- `client/.../game/network/protocol.ts` — `TileCoord`/`TileInstance` → `{tileset,col,row}`; add `TilesetDef`.
- `client/.../game/rendering/terrainTileset.ts` — data-driven (fetch defs, slice from def, col/row).
- `client/.../game/maps/catalog.ts` — `fetchTilesetDefs`; legacy tile migration on map load.
- `client/.../game/maps/terrainTileGroups.ts` — group/expand by `(tileset,col,row)` + legacy read.
- `client/.../components/world-editor/WorldEditorPanel.vue` — picker + paint store col/row; wire tileset load.
- `client/.../components/world-editor/WorldEditorToolbar.vue` — add `tilesets` tab.
- `client/.../game/core/GameClient.ts` — fetch tileset defs at start.
- `client/.../game/rendering/CanvasRenderer.ts` — in-game terrain bake uses def slice (via terrainTileset).
- `client/src/game-portal/vite.config.ts` — proxy `/tilesets`.

---

## Task 1: `TilesetDef` type + embedded baseline (server, read-only)

**Files:**
- Create: `server/internal/game/tileset_defs.go`
- Create: `server/internal/game/catalog/tilesets/{tileset,grass-grass-25,dirt-dirt-25,grass-dirt-0,grass-grass-8x8}.json`
- Create: `server/internal/game/catalog/tilesets/images/*.png` (copy the 5 sources)
- Test: `server/internal/game/tileset_defs_test.go`

**Interfaces:**
- Produces: `type TilesetDef struct { ID, Name, Image string; Cols, Rows, OffsetX, OffsetY, TileWidth, TileHeight, SpacingX, SpacingY int }`; `func ListTilesetDefs() []TilesetDef`; `func GetTilesetDef(id string) (TilesetDef, bool)`.

- [ ] **Step 1: Copy the 5 source PNGs into the embed dir.**

```bash
mkdir -p server/internal/game/catalog/tilesets/images
cp client/src/game-portal/src/assets/terrain/grass-dirt-elevation-25.png     server/internal/game/catalog/tilesets/images/tileset.png
cp client/src/game-portal/src/assets/terrain/new-grass-grass-elavation-640.png server/internal/game/catalog/tilesets/images/grass-grass-25.png
cp client/src/game-portal/src/assets/terrain/new-dirt-dirt-elavation-640.png  server/internal/game/catalog/tilesets/images/dirt-dirt-25.png
cp client/src/game-portal/src/assets/terrain/grass-dirt-elevation-0.png       server/internal/game/catalog/tilesets/images/grass-dirt-0.png
cp client/src/game-portal/src/assets/terrain/8x8-grass-grass-1024.png         server/internal/game/catalog/tilesets/images/grass-grass-8x8.png
```

- [ ] **Step 2: Write the 5 def JSON files.** Example `catalog/tilesets/grass-grass-8x8.json`:

```json
{ "id": "grass-grass-8x8", "name": "Grass (8×8 variants)", "image": "grass-grass-8x8.png",
  "cols": 8, "rows": 8, "offsetX": 0, "offsetY": 0, "tileWidth": 128, "tileHeight": 128, "spacingX": 0, "spacingY": 0 }
```
`tileset`: cols/rows 4, tileWidth/Height 32, image `tileset.png`, name "Grass ↔ Dirt (Wang)". `grass-grass-25`/`dirt-dirt-25`: 4×4, tileWidth 160. `grass-dirt-0`: 4×4, tileWidth 32, name "Grass → Dirt (flat)".

- [ ] **Step 3: Write the failing test** in `tileset_defs_test.go`:

```go
func TestListTilesetDefs(t *testing.T) {
	defs := ListTilesetDefs()
	byID := map[string]TilesetDef{}
	for _, d := range defs { byID[d.ID] = d }
	g, ok := byID["grass-grass-8x8"]
	if !ok { t.Fatal("grass-grass-8x8 missing") }
	if g.Cols != 8 || g.TileWidth != 128 || g.Image != "grass-grass-8x8.png" {
		t.Fatalf("bad def: %+v", g)
	}
	if _, ok := byID["tileset"]; !ok { t.Fatal("tileset missing") }
}
```

- [ ] **Step 4: Run it (fails — undefined).** `cd server && go test ./internal/game/ -run TestListTilesetDefs` → FAIL (undefined `ListTilesetDefs`).

- [ ] **Step 5: Implement `tileset_defs.go`** — mirror the embed + load pattern in `campaign_defs.go` (the `//go:embed catalog/campaigns/*.json` block and `loadCampaignHeaders`). Embed `catalog/tilesets/*.json`, unmarshal into `[]TilesetDef` keyed by id in a package var `tilesetDefsByID` (panic on dup id / missing image / non-positive cols/rows/tileWidth). Add `ListTilesetDefs()` (sorted by id) and `GetTilesetDef(id)`.

- [ ] **Step 6: Run test → PASS.** `go build ./... && go test ./internal/game/ -run TestListTilesetDefs`.

- [ ] **Step 7: Commit.** `git add server/internal/game/tileset_defs.go server/internal/game/tileset_defs_test.go server/internal/game/catalog/tilesets/ && git commit` (message: `feat(tilesets): embedded TilesetDef catalog baseline` + trailers).

---

## Task 2: Tileset persistence overlay (server)

**Files:**
- Create: `server/internal/game/tileset_persistence.go`
- Modify: `server/internal/game/tileset_defs.go` (add `currentTilesetDefs()` merging embedded + overlay; make `ListTilesetDefs`/`GetTilesetDef` use it)
- Modify: `server/cmd/api/main.go` (call loader at startup)
- Test: `server/internal/game/tileset_persistence_test.go`

**Interfaces:**
- Produces: `func SaveTilesetDef(def TilesetDef) error`; `func DeleteTilesetDef(id string) (bool, error)`; `func SaveTilesetImage(id string, data []byte) (string, error)`; `func LoadPersistedTilesetsIntoOverlay()`; `func IsTilesetValidationError(err error) bool`; `func TilesetImagePath(key string) (string, bool)`.

Mirror `campaign_persistence.go` exactly (overlay map + `sync.RWMutex`, `resolveTilesetsDir()` via `TILESET_CATALOG_DIR` env, `currentTilesetDefs()` merge, validation → `errTilesetSave`, delete-guard). Delete guard: refuse if any map in `currentMapCatalogSnapshot()` has a `tiles[]` entry whose `Tileset == id` (see Task 6 for `TileCoord.Tileset`). Image storage: write PNG bytes to `<dir>/images/<id>.png` (validate `png.DecodeConfig`, cap 4 MB), return the key `"<id>.png"`.

- [ ] **Step 1: Write failing test** (`tileset_persistence_test.go`) — set `TILESET_CATALOG_DIR` to `t.TempDir()`, `SaveTilesetDef` a def, assert `GetTilesetDef` returns it; `DeleteTilesetDef` removes it; a bad id (`"Bad Id"`) returns `IsTilesetValidationError(err)==true`.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement `tileset_persistence.go`** mirroring `campaign_persistence.go`; add `currentTilesetDefs()` + repoint `ListTilesetDefs`/`GetTilesetDef` in `tileset_defs.go`.
- [ ] **Step 4: Add `game.LoadPersistedTilesetsIntoOverlay()` in `main.go`** next to `LoadPersistedCampaignsIntoOverlay()`.
- [ ] **Step 5: Run → PASS** (`go test ./internal/game/ -run Tileset`), `go build ./...`.
- [ ] **Step 6: Commit** (`feat(tilesets): writable overlay + persistence`).

---

## Task 3: Tileset HTTP endpoints (server)

**Files:**
- Create: `server/internal/http/tileset_handlers.go`
- Modify: `server/internal/http/router.go` (register), `server/internal/http/profile_handlers.go` (add `GET /catalog/tilesets` next to campaigns) OR put GET in the new handler file
- Modify: `server/internal/embedded/handler.go` (add `/tilesets` to `apiPrefixes`)
- Test: `server/internal/http/tileset_handlers_test.go` (or an httptest in game package)

**Interfaces:**
- Produces routes: `GET /catalog/tilesets` → `{ "tilesets": ListTilesetDefs() }`; `POST /tilesets` (decode `game.TilesetDef`, `SaveTilesetDef`, 201/400); `DELETE /tilesets/{id}` (guarded, 200/400); `POST /tilesets/{id}/image` (read body ≤4 MB, `SaveTilesetImage`, 201 `{id,status:"image_saved",image}`); `GET /tilesets/images/{key}` (serve PNG, `Cache-Control: no-cache`).

Mirror the campaign endpoints in `profile_handlers.go` (`/api/catalog/campaigns` GET+POST switch, `/api/catalog/campaigns/` DELETE) and the item image upload/serve in `editor_handlers.go` (`/items/{id}/image`) + `router.go` asset serving.

- [ ] **Step 1: Write failing httptest** — start the mux, `POST /tilesets` a def → 201; `GET /catalog/tilesets` includes it; `POST /tilesets/x/image` with a tiny valid PNG → 201; `GET /tilesets/images/x.png` → 200 image/png.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement handlers + register in `router.go`; add `/tilesets` to `apiPrefixes`.**
- [ ] **Step 4: Add `/tilesets` to the Vite proxy** (`vite.config.ts` proxy block, target `GO_SERVER`).
- [ ] **Step 5: Run → PASS**, `go vet ./internal/http/`.
- [ ] **Step 6: Commit** (`feat(tilesets): CRUD + image upload endpoints`).

---

## Task 4: `TileCoord` → col/row on the wire (server) + migration

**Files:**
- Modify: `server/pkg/protocol/messages.go` (TileCoord fields + `UnmarshalJSON` shim)
- Modify: `server/internal/game/map_terrain_tile_groups.go` (group/expand by tileset/col/row + legacy read)
- Modify: `server/internal/game/pathing.go` (`isWalkableGroundTile` re-keyed)
- Test: `server/internal/game/tile_migration_test.go`, extend pathing test

**Interfaces:**
- Produces: `TileCoord{ Tileset string \`json:"tileset"\`; Col int \`json:"col"\`; Row int \`json:"row"\` }` with an `UnmarshalJSON` that reads legacy `{sheet,sx,sy}` → `{tileset:sheet, col:sx/32, row:sy/32}`.
- Consumes: `GetTilesetDef` (Task 1) — not strictly needed here (legacy divisor is the fixed `32`).

- [ ] **Step 1: Write failing test** (`tile_migration_test.go`):

```go
func TestTileCoordLegacyUnmarshal(t *testing.T) {
	var c protocol.TileCoord
	if err := json.Unmarshal([]byte(`{"sheet":"grass-grass-8x8","sx":96,"sy":0}`), &c); err != nil { t.Fatal(err) }
	if c.Tileset != "grass-grass-8x8" || c.Col != 3 || c.Row != 0 {
		t.Fatalf("legacy migrate wrong: %+v", c)
	}
	var c2 protocol.TileCoord
	json.Unmarshal([]byte(`{"tileset":"t","col":2,"row":1}`), &c2)
	if c2.Col != 2 || c2.Row != 1 { t.Fatalf("new shape wrong: %+v", c2) }
}
```

- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement.** Give `TileCoord` the new fields and:

```go
func (c *TileCoord) UnmarshalJSON(b []byte) error {
	var raw struct {
		Tileset string `json:"tileset"`
		Col     *int   `json:"col"`
		Row     *int   `json:"row"`
		Sheet   string `json:"sheet"`
		SX      *int   `json:"sx"`
		SY      *int   `json:"sy"`
	}
	if err := json.Unmarshal(b, &raw); err != nil { return err }
	if raw.Tileset != "" || raw.Col != nil || raw.Row != nil {
		c.Tileset = raw.Tileset
		if raw.Col != nil { c.Col = *raw.Col }
		if raw.Row != nil { c.Row = *raw.Row }
		return nil
	}
	// Legacy pixel form → grid indices (logical tile size was 32).
	c.Tileset = raw.Sheet
	if raw.SX != nil { c.Col = *raw.SX / 32 }
	if raw.SY != nil { c.Row = *raw.SY / 32 }
	return nil
}
```

- [ ] **Step 4: Re-key `isWalkableGroundTile` in `pathing.go`** to col/row:

```go
func isWalkableGroundTile(c protocol.TileCoord) bool {
	if c.Tileset == "grass-grass-8x8" {
		return c.Row == 0 && c.Col >= 0 && c.Col <= 3 // first-row grass variants
	}
	// Wang sheets: pure-interior slots (64,32)=col2,row1 grass; (0,96)=col0,row3 dirt.
	if c.Col == 2 && c.Row == 1 { return true }
	if c.Col == 0 && c.Row == 3 { return true }
	return false
}
```

- [ ] **Step 5: Update `map_terrain_tile_groups.go`** to serialize/group by `(tileset,col,row)` (the wire grouped form's per-tile key). Keep reading legacy grouped entries via the same divisor if present.
- [ ] **Step 6: Run pathing + migration tests → PASS**, `go build ./...`, run the full `go test ./internal/game/ -run 'Tile|Path|Map'`.
- [ ] **Step 7: Commit** (`feat(tilesets): TileCoord col/row + legacy migration + walkability re-key`).

---

## Task 5: Client protocol + tileset fetch + migration shim

**Files:**
- Modify: `client/.../game/network/protocol.ts` (`TileCoord`/`TileInstance` → `{tileset,col,row}`; add `TilesetDef` + `TileSheet` deprecation note)
- Modify: `client/.../game/maps/catalog.ts` (`fetchTilesetDefs()`; migrate `tiles[]` on `fetchMapCatalogFile`)
- Modify: `client/.../game/maps/terrainTileGroups.ts` (group/expand by tileset/col/row + legacy read)
- Test: `client/.../game/maps/terrainTileGroups.test.ts` (extend), a small migration test

**Interfaces:**
- Produces: `type TileCoord = { tileset: string; col: number; row: number }`; `type TileInstance = GridCoord & TileCoord`; `interface TilesetDef { id; name; image; cols; rows; offsetX; offsetY; tileWidth; tileHeight; spacingX; spacingY }`; `fetchTilesetDefs(): Promise<TilesetDef[]>` (GET `/catalog/tilesets`).

- [ ] **Step 1: Write failing test** — `expandTileGroups` on a legacy grouped payload `{sheet:"grass-grass-8x8", tiles:[{sx:96,sy:0,...}]}` yields `{tileset:"grass-grass-8x8", col:3, row:0}`. And a new-shape payload round-trips.
- [ ] **Step 2: Run → FAIL** (`npx vitest run terrainTileGroups`).
- [ ] **Step 3: Implement** the protocol type change, `fetchTilesetDefs`, the grouping/migration (divide by 32 for legacy). Add a `migrateLegacyTile(t)` helper used by both `fetchMapCatalogFile` and `expandTileGroups`.
- [ ] **Step 4: Run → PASS.**
- [ ] **Step 5: `npx vue-tsc -b`** — expect errors at all `sx/sy`/`sheet` tile usages; those are fixed in Tasks 6–7 (do NOT fix rendering/paint yet if splitting, but this task must at least compile the changed modules). If tsc is red only in `terrainTileset.ts`/`WorldEditorPanel.vue`, proceed — they're the next tasks; otherwise fix stragglers here.
- [ ] **Step 6: Commit** (`feat(tilesets): client TileCoord col/row + fetchTilesetDefs + migration`).

---

## Task 6: Data-driven `terrainTileset.ts`

**Files:**
- Modify: `client/.../game/rendering/terrainTileset.ts`
- Modify: `client/.../game/core/GameClient.ts` (fetch + init tileset defs in `start()`, alongside the other catalogs added earlier)
- Test: `client/.../game/rendering/terrainTileset.test.ts`

**Interfaces:**
- Produces: `initTilesetDefs(defs: TilesetDef[])`; `getTilesetDef(id)`; `tilesetImageUrl(def)` (→ `${API_BASE}/tilesets/images/${def.image}`); `drawTerrainTile(ctx, coord: TileCoord, destX, destY, destSize)` now slices via the def; `getWangGrassDirtCoord(mask): TileCoord` (returns `{tileset:'tileset',col,row}`); `getSheetVariantPool(id): TileCoord[] | null`; `isWalkableGroundTile(coord): boolean`.

- [ ] **Step 1: Write failing test** — `initTilesetDefs([grass8x8Def]); const {col,row}=...;` assert `drawTerrainTile` computes `srcX = offsetX + col*(tileWidth+spacingX)` (test via a fake ctx capturing `drawImage` args), and `isWalkableGroundTile({tileset:'grass-grass-8x8',col:3,row:0})===true`, `col:2,row:2 ===false`.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement.** Replace the hardcoded `sheetUrls/SHEET_TILE_SIZE/SHEET_GRID/SHEET_VARIANT_POOLS/WANG_GRASS_DIRT_COORDS` with a runtime map seeded by `initTilesetDefs`. Images load lazily per def from `tilesetImageUrl` (keep the existing `onSheetReady`/`getSheetImage` async pattern, keyed by tileset id). `drawTerrainTile`:

```ts
export function drawTerrainTile(ctx, coord, destX, destY, destSize) {
  const def = getTilesetDef(coord.tileset); const img = getTilesetImage(coord.tileset)
  if (!def || !img) return
  const sx = def.offsetX + coord.col * (def.tileWidth + def.spacingX)
  const sy = def.offsetY + coord.row * (def.tileHeight + def.spacingY)
  const hiRes = def.tileWidth > destSize
  const inset = hiRes ? 0.5 : 0
  ctx.imageSmoothingEnabled = hiRes
  ctx.drawImage(img, sx + inset, sy + inset, def.tileWidth - inset*2, def.tileHeight - inset*2, destX, destY, destSize, destSize)
}
```
Convert `WANG_GRASS_DIRT_COORDS` (16 entries) from pixel to col/row (`sx/32, sy/32`) and `getWangGrassDirtCoord` to return `{tileset:'tileset',col,row}`. Convert `SHEET_VARIANT_POOLS` to `{tileset:'grass-grass-8x8', col, row}` for the 4 grass variants. Re-key `isWalkableGroundTile` identically to the server (Task 4). `isTerrainTilesetReady()` gates on `getTilesetDef('tileset')` + its image.

- [ ] **Step 4: In `GameClient.start`**, add `fetchTilesetDefs().then(initTilesetDefs)` into the concurrent catalog block (it's needed before terrain renders — put `initTilesetDefs` in the `catalogsReady` `.then`).
- [ ] **Step 5: Run test + `vue-tsc -b` → PASS/clean.**
- [ ] **Step 6: Commit** (`feat(tilesets): data-driven terrainTileset slice math`).

---

## Task 7: Painting + picker store col/row

**Files:**
- Modify: `client/.../components/world-editor/WorldEditorPanel.vue` (paint tile branch, tile picker render/click, tile refs, tileset load in `onMounted`)
- Test: manual (editor) + `vue-tsc`/`vite build`

**Interfaces:**
- Consumes: `getTilesetDef`, `drawTerrainTile`, `getSheetVariantPool`, `initTilesetDefs`, `fetchTilesetDefs`.

- [ ] **Step 1:** In `onMounted`, add `void fetchTilesetDefs().then(initTilesetDefs).catch(()=>{})` (alongside the other catalog fetches).
- [ ] **Step 2:** Change the tile-brush state: `selectedTileCoord` becomes `{ col: number; row: number } | null`; the paint tile branch writes `{ tileset: selectedTileSheet.value, col, row }`. The variant pool + `cellVariant` logic now yields `{col,row}`.
- [ ] **Step 3:** `renderTilePicker` sizes the picker to `def.cols*tileW × def.rows*tileH` scaled to fit a fixed display box (e.g. 256px wide); `onTilePickerClick` maps click → `col = floor(px/displayTileW)`, `row = floor(py/displayTileH)`. Sheet dropdown lists tileset ids from the loaded defs (replace `TILE_SHEET_NAMES`).
- [ ] **Step 4:** Run `vue-tsc -b` (clean) + `vite build` (green).
- [ ] **Step 5:** Manual: paint a `grass-grass-8x8` tile, save the map, reload — tile persists as col/row; an OLD map still renders (migration). Verify units walk on grass, not on a painted cliff tile.
- [ ] **Step 6: Commit** (`feat(tilesets): paint + picker in col/row grid space`).

---

## Task 8: `tilesetEditorApi.ts` (client)

**Files:**
- Create: `client/.../services/tilesetEditorApi.ts`
- Test: none (thin fetch wrappers; covered by the panel's manual E2E)

**Interfaces:**
- Produces: `saveTileset(def: TilesetDef): Promise<void>` (POST `/tilesets`, throw `TilesetSaveError` on 400); `deleteTileset(id): Promise<void>` (DELETE `/tilesets/{id}`); `uploadTilesetImage(id, file: File): Promise<{image:string}>` (POST `/tilesets/{id}/image`, body = raw bytes).

- [ ] **Step 1:** Write the module mirroring `campaignEditorApi.ts` (save/delete) + the item icon upload (`uploadItemIcon` in `itemEditorApi.ts`) for the image POST.
- [ ] **Step 2:** `vue-tsc -b` clean.
- [ ] **Step 3: Commit** (`feat(tilesets): client editor API`).

---

## Task 9: `TilesetEditorPanel.vue` + toolbar wiring

**Files:**
- Create: `client/.../components/TilesetEditorPanel.vue`
- Modify: `client/.../components/world-editor/WorldEditorToolbar.vue` (add `{ id:'tilesets', label:'Tilesets', enabled:true }`)
- Modify: `client/.../components/world-editor/WorldEditorPanel.vue` (import + render `<TilesetEditorPanel v-else-if="activeScreen==='tilesets'"/>`; add `'tilesets'` to the `EditorScreen` type + `onToolbarSelect` case)
- Test: manual E2E + `vue-tsc`/`vite build`

**Interfaces:**
- Consumes: `fetchTilesetDefs`, `saveTileset`, `deleteTileset`, `uploadTilesetImage`, `tilesetImageUrl`.

Build on `EditorShell`/`EditorSidebar`/`EditorHeader`/`SectionCard`/`EditorField` (mirror `CampaignEditorPanel.vue` for the shell + list + header + save/delete; mirror the Item editor for the image upload control). Sections: **Image** (upload button + current image preview), **Slice** (`cols,rows,offsetX,offsetY,tileWidth,tileHeight,spacingX,spacingY` number fields), **Preview** (a `<canvas>` drawing the image with a grid overlay computed from the slice fields — draw a rect per `(col,row)` using the same srcX/srcY formula; redraw on any field change; smoothing off for the grid lines, on for the image if downscaled).

- [ ] **Step 1:** Scaffold the panel on `EditorShell` with the sidebar list + header (copy structure from `CampaignEditorPanel.vue`), form state = a working `TilesetDef`, id auto-slug while new.
- [ ] **Step 2:** Image upload: file input → `uploadTilesetImage(form.id, file)` → set `form.image` → reload the preview image from `tilesetImageUrl`.
- [ ] **Step 3:** Slice fields (EditorField number inputs) bound to the working def; the overlay canvas redraws on change:

```ts
function drawOverlay() {
  // draw img at fit-scale; for col in 0..cols, row in 0..rows: strokeRect at
  // (offsetX+col*(tileWidth+spacingX))*scale, ... , tileWidth*scale, tileHeight*scale
}
```

- [ ] **Step 4:** Save → `saveTileset(form)`; Delete → `deleteTileset(id)` (guarded server-side; surface 400 message). Reload list after.
- [ ] **Step 5:** Toolbar + screen wiring; `vue-tsc -b` clean + `vite build` green.
- [ ] **Step 6: Manual E2E:** new tileset → upload PNG → adjust offset/tileWidth and watch the grid snap → save → switch to Map → Tile brush → the new tileset appears → paint from it.
- [ ] **Step 7: Commit** (`feat(tilesets): tileset editor panel + slice overlay + toolbar tab`).

---

## Task 10: Cleanup + full verification

**Files:**
- Modify: remove now-dead hardcoded imports/consts from `terrainTileset.ts`; drop the `?url` PNG imports and the `8x8-grass-grass-1024.png` etc. imports if fully replaced by server-served images (keep the source PNGs in `assets/terrain/` only if still referenced elsewhere — grep first).
- Modify: `client/.../game/rendering/CanvasRenderer.ts` — confirm the in-game terrain bake still renders (it calls `drawAutoTiledTerrain` → `drawTerrainTile`, now def-driven; ensure `initTilesetDefs` runs before the first bake — it does via `GameClient.start`).

- [ ] **Step 1:** `grep` for `sx`/`sy`/`sheet`/`TILE_SHEET_NAMES`/`SHEET_GRID` in client terrain code; remove dead references.
- [ ] **Step 2:** Full green gate: `cd server && go build ./... && go vet ./... && go test ./internal/game/ ./internal/http/`; `cd client/src/game-portal && npx vue-tsc -b && npx vite build && npx vitest run`.
- [ ] **Step 3:** Manual E2E in a running match/playtest: an existing campaign map renders terrain identically to before (migration correct); paint a new hi-res tileset tile; units path around a cliff tile, walk on grass.
- [ ] **Step 4: Commit** (`chore(tilesets): remove dead hardcoded sheet config; SP1 complete`).

---

## Self-review

**Spec coverage:** TilesetDef catalog (T1–T2), CRUD+upload endpoints (T3), col/row + migration (T4–T5), data-driven terrainTileset (T6), painting col/row (T7), editor panel + slice overlay (T8–T9), walkability re-key client+server (T4+T6), embedded-def migration (T1+T4+T5), cleanup/verify (T10). Non-goals (SP2 tile-gen, SP3 walkability authoring) correctly excluded. ✓

**Placeholder scan:** Boilerplate tasks (T2, T8, T9) reference exact mirror files (`campaign_persistence.go`, `campaignEditorApi.ts`, `CampaignEditorPanel.vue`, item upload) rather than repeating hundreds of lines — an intentional adaptation for a repo with four working editors; the novel code (migration `UnmarshalJSON`, `drawTerrainTile`, walkability, overlay) is shown in full. ✓

**Type consistency:** `TileCoord{tileset,col,row}` used identically in server (T4) and client (T5–T7); `isWalkableGroundTile` col/row rule identical in T4 (Go) and T6 (TS); `drawTerrainTile` signature stable. ✓
