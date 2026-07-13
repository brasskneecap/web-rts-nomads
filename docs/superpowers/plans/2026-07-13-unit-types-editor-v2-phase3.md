# Unit-Types Editor v2 — Phase 3: Browser Packer & Art Ingest

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An author drops a PixelLab export folder onto the Unit Types panel, sees it packed and animating in the preview, and on Save it persists to the writable art dir and renders in a playtest — with no `npm run pack:sprites` and no rebuild.

**Architecture:** A pure TypeScript layout core (`spritePacking.ts`) reproduces the CLI packer's sheet math (columns = frames, rows = directions; rotations = 1-col vertical strip). A browser rasterizer executes that layout with canvas → PNG blobs; those blobs feed straight into Phase 2's runtime overlay for instant preview-before-persist. A new `POST /unit-art` write endpoint persists the packed sheets + manifest under `UNIT_ASSETS_DIR`; the client then re-runs `loadRuntimeSpriteSets()` so the art appears live.

**Tech Stack:** Go 1.22 (`server/`), Vue 3 + TS SPA (`client/src/game-portal`, vitest + happy-dom, `pngjs` available as a devDependency).

**Spec:** `docs/superpowers/specs/2026-07-13-unit-types-editor-v2-design.md` §5.2, §5.3, plus the `/units/{type}/art` row of §8.

**Phase 3 of 5.** Phase 2 (runtime sprite overlay + preview) is complete and is the read/preview substrate this phase writes into. Phase 4 (per-facing attack origins) and Phase 5 (path entities) follow.

---

## Three findings from reading `pack-unit-sprites.mjs` — read before starting

**1. "Byte-for-byte" conformance is against DECODED RGBA + the manifest JSON, NOT the encoded PNG file.**
The spec (§5.2) says "sheet pixel buffers match byte-for-byte." That is only true at the *decoded-RGBA* level. The CLI encodes PNGs with `pngjs`; the browser encodes with `canvas.toBlob`/`OffscreenCanvas.convertToBlob`. Those produce **different PNG container bytes** (different filters, compression, chunk order) for identical pixels. So:
- The **manifest JSON** must be structurally identical (both produce plain JSON — assert deep-equal, ignoring `packedAt`).
- The **sheet pixels** must be identical when decoded to RGBA (assert the decoded buffers are equal).
- The **encoded PNG bytes** are NOT required to match and MUST NOT be asserted. Editor-ingested art and CLI-committed art are never required to be the same file, only to decode to the same pixels.

**2. The conformance test runs entirely on `pngjs`, not canvas — so it works in vitest/happy-dom.**
happy-dom has no working canvas. But `pngjs` is a client devDependency (used by the CLI packer) and is pure JS. The pure layout core emits a *blit plan* (which source frame goes at which sheet coordinate); a `pngjs`-based test rasterizer executes that plan and the test compares its RGBA against a committed golden sheet that the CLI produced. **The browser canvas rasterizer is validated by the E2E only** (Task 5), exactly like Phase 2's image-decode path — because canvas doesn't run in the test env. This is an accepted, stated limit, not a gap.

**3. Two layout details that a naive packer gets wrong — both must be reproduced exactly:**
- **Uneven frame counts per direction.** `packAnimation` sets `frameCount = max(frames across directions)` and allocates `sheetW = frameWidth × frameCount`, but writes only each direction's *actual* frame count into its row. A direction with fewer frames leaves the trailing columns **transparent** (the sheet is zero-initialized). Do NOT clamp, pad, or repeat frames — allocate the full sheet and blit only the frames that exist. A fresh canvas is transparent black, matching `pngjs`'s zero-init, so this falls out naturally *if you don't fill it*.
- **`DIRECTION_ORDER` and the slug rule are load-bearing.** Rows are ordered by `['north','south','east','west','north-east','south-east','south-west','north-west']` **filtered to directions present** — cardinals first. `rowOrder` in the manifest records the actual filtered order, and the runtime reads it, so the order isn't visually load-bearing — but it IS conformance-load-bearing (the golden was packed this way). The animation slug is `hashedName.split('-')[0].toLowerCase()` (`"Walking-1656a518"` → `walking`). `normalizeExportShape` takes `states[0]` for the new export shape.

---

## Two decisions taken (settled; the plan assumes them)

| # | Decision | Rationale |
|---|---|---|
| P3-A | **Two implementations (CLI `.mjs` + new TS core) kept in sync by a golden-fixture conformance test — NOT a shared module.** | A shared pure-JS core imported by both would be more DRY, but the CLI runs as a bare `node` script (plain JS, `pngjs`) while the TS core is bundled by Vite — sharing one module across that runtime boundary is awkward (module resolution, `allowJs`, no `pngjs` in the browser). The layout math is ~40 lines and stable; a golden test that fails loudly on divergence is the cheaper, cleaner guard. `pack-unit-sprites.mjs` is **not modified**. |
| P3-B | **Preview-before-persist reuses the Phase 2 overlay.** After packing in-browser, wrap the sheet blobs in `object URLs`, build a `UnitSpriteSet` via the existing `buildSpriteSet`, and `registerRuntimeSpriteSet` it — the Phase 2 preview then plays the freshly-packed art from memory. Save uploads; a re-drop or cancel revokes the object URLs and clears. | No server round-trip to preview. The overlay already exists and already shadows bundled art; ingest is just another producer of runtime sets. |

---

## Global Constraints

- **Do not run `git commit` or `git add`.** Each task ends with a **Checkpoint**.
- Go from `server/`; client from `client/src/game-portal`. Client typecheck is **`npx vue-tsc -b`**.
- `gofmt -l` flags the whole checkout (CRLF) — gates are `go vet` / `go build`.
- **No literal `cursor:` declarations** in component CSS (`cursor: not-allowed` on forbidden states only).
- **Do NOT modify `client/src/game-portal/scripts/pack-unit-sprites.mjs`.** It is the reference; the golden fixture is generated from it.
- **Every new server route the SPA calls MUST be added to the `proxy` block in `vite.config.ts`** — an unproxied route silently 404s in `npm run dev` (this bug shipped twice already: `/units`, `/factions`).
- The write endpoint reads `UNIT_ASSETS_DIR` and creates it if missing. It must NEVER write outside that root, and only `.png`/`.json` files with `unitIDPattern`-clean path segments.
- Do not modify the item editor or the old map editor.

---

### Task 1: Server — create-capable art dir + `POST /unit-art` write endpoint

**Files:**
- Modify: `server/internal/game/unit_art.go` (`resolveUnitAssetsDir` → create-capable; add `SaveUnitArt`)
- Modify: `server/internal/game/unit_art_test.go` (write + rejection tests)
- Modify: `server/internal/http/router.go` (add `POST /unit-art`)
- Modify: `server/internal/http/editor_routes_test.go` (HTTP-level write + rejection test)
- Modify: `client/src/game-portal/vite.config.ts` (proxy `/unit-art`)

**Interfaces:**
- Produces: `type UnitArtSaveRequest struct { Faction, Unit, Path string; Files []UnitArtFile }`, `type UnitArtFile struct { Name string; ContentBase64 string }`
- Produces: `func SaveUnitArt(req UnitArtSaveRequest) error`
- Changes: `resolveUnitAssetsDir()` returns the path even when it doesn't exist yet (drops the `os.Stat` gate); `ListUnitArt` already tolerates a missing dir (its `WalkDir` returns empty), so this is safe — confirm with the existing Task-1-of-Phase-2 tests still passing.

**The write security surface — this is the point of the task:**
- `Faction`, `Unit`, and (if set) `Path` must each match `unitIDPattern` (`^[a-z0-9_]+$`). Reject otherwise.
- Each `File.Name` is a forward-slash relative path under the unit dir. The ONLY allowed names are: `sprites.json`, `metadata.json`, `portrait.png`, and `packed/<slug>.png` where `<slug>` matches `unitIDPattern`. Anything else (any `..`, any other directory, any other extension) → reject. This is an allowlist, not a filter.
- Total request size and per-file size are capped (per-file 4 MB, total 32 MB — sheets are small; a runaway is a bug or an attack).
- Write path: `<UNIT_ASSETS_DIR>/<faction>/<unit>/<name>` for a base unit, `<UNIT_ASSETS_DIR>/<faction>/<unit>/paths/<path>/<name>` for a path. `MkdirAll` the parent. Reuse the resolved-absolute-path containment check from Phase 2's `ReadUnitArtFile` — the final write path must stay inside the root.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/unit_art_test.go` (the `writeArtFixture` helper already exists from Phase 2):

```go
import "encoding/base64" // add to the existing import block

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestSaveUnitArt_WritesBaseUnitFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)

	req := UnitArtSaveRequest{
		Faction: "human", Unit: "moon_dancer",
		Files: []UnitArtFile{
			{Name: "sprites.json", ContentBase64: b64(`{"key":"moon_dancer"}`)},
			{Name: "packed/walking.png", ContentBase64: b64("\x89PNG fake")},
			{Name: "packed/rotations.png", ContentBase64: b64("\x89PNG fake")},
		},
	}
	if err := SaveUnitArt(req); err != nil {
		t.Fatalf("SaveUnitArt: %v", err)
	}
	for _, rel := range []string{"human/moon_dancer/sprites.json", "human/moon_dancer/packed/walking.png"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("expected %s written: %v", rel, err)
		}
	}
	// The art must be immediately visible to the read side.
	var found bool
	for _, e := range ListUnitArt() {
		if e.Key == "moon_dancer" {
			found = true
		}
	}
	if !found {
		t.Fatal("saved art not visible to ListUnitArt")
	}
}

func TestSaveUnitArt_WritesPathFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	req := UnitArtSaveRequest{
		Faction: "human", Unit: "archer", Path: "moonshadow",
		Files: []UnitArtFile{{Name: "sprites.json", ContentBase64: b64(`{"key":"moonshadow"}`)}},
	}
	if err := SaveUnitArt(req); err != nil {
		t.Fatalf("SaveUnitArt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "human", "archer", "paths", "moonshadow", "sprites.json")); err != nil {
		t.Fatalf("expected path art written: %v", err)
	}
}

// THE security test. Every one of these must be REFUSED (SaveUnitArt returns an
// error and writes nothing outside the intended tree).
func TestSaveUnitArt_RejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	secret := filepath.Join(dir, "..", "secret.json")
	_ = os.WriteFile(secret, []byte("orig"), 0o644)
	t.Cleanup(func() { _ = os.Remove(secret) })

	cases := map[string]UnitArtSaveRequest{
		"bad faction":       {Faction: "../evil", Unit: "u", Files: []UnitArtFile{{Name: "sprites.json", ContentBase64: b64("{}")}}},
		"bad unit":          {Faction: "human", Unit: "../evil", Files: []UnitArtFile{{Name: "sprites.json", ContentBase64: b64("{}")}}},
		"bad path":          {Faction: "human", Unit: "archer", Path: "../evil", Files: []UnitArtFile{{Name: "sprites.json", ContentBase64: b64("{}")}}},
		"traversal name":    {Faction: "human", Unit: "u", Files: []UnitArtFile{{Name: "../../secret.json", ContentBase64: b64("x")}}},
		"disallowed subdir": {Faction: "human", Unit: "u", Files: []UnitArtFile{{Name: "raw/x.png", ContentBase64: b64("x")}}},
		"bad extension":     {Faction: "human", Unit: "u", Files: []UnitArtFile{{Name: "packed/x.exe", ContentBase64: b64("x")}}},
		"bad slug":          {Faction: "human", Unit: "u", Files: []UnitArtFile{{Name: "packed/../evil.png", ContentBase64: b64("x")}}},
		"no files":          {Faction: "human", Unit: "u", Files: nil},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if err := SaveUnitArt(req); err == nil {
				t.Fatalf("%s: expected rejection, got nil", name)
			}
		})
	}
	// The secret outside the root is untouched.
	if b, _ := os.ReadFile(secret); string(b) != "orig" {
		t.Fatal("a rejected write escaped the art root")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestSaveUnitArt -count=1`
Expected: FAIL — `undefined: UnitArtSaveRequest`.

- [ ] **Step 3: Make `resolveUnitAssetsDir` create-capable, add `SaveUnitArt`**

In `unit_art.go`, drop the `os.Stat` gate in `resolveUnitAssetsDir` so it returns the path even when absent (it's now used by both read, which tolerates a missing dir, and write, which creates it). Then add:

```go
// unitArtFileNamePattern matches an animation slug used in packed/<slug>.png.
var unitArtSlugPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// allowedUnitArtName reports whether a request file name is one the editor is
// permitted to write. Allowlist, not filter: sprites.json / metadata.json /
// portrait.png at the unit root, and packed/<slug>.png. Everything else — any
// other directory, any traversal, any other extension — is refused.
func allowedUnitArtName(name string) bool {
	switch name {
	case "sprites.json", "metadata.json", "portrait.png":
		return true
	}
	if rest, ok := strings.CutPrefix(name, "packed/"); ok {
		return strings.HasSuffix(rest, ".png") &&
			unitArtSlugPattern.MatchString(strings.TrimSuffix(rest, ".png"))
	}
	return false
}

const (
	maxUnitArtFileBytes  = 4 << 20  // 4 MB per file
	maxUnitArtTotalBytes = 32 << 20 // 32 MB per request
)

// UnitArtFile is one file in a SaveUnitArt request. Name is a forward-slash
// path relative to the unit's art directory; ContentBase64 is its bytes.
type UnitArtFile struct {
	Name          string `json:"name"`
	ContentBase64 string `json:"contentBase64"`
}

// UnitArtSaveRequest is the body of POST /unit-art.
type UnitArtSaveRequest struct {
	Faction string        `json:"faction"`
	Unit    string        `json:"unit"`
	Path    string        `json:"path,omitempty"`
	Files   []UnitArtFile `json:"files"`
}

// SaveUnitArt validates and writes a packed art set to the writable art dir.
// Base unit → <dir>/<faction>/<unit>/; promotion path → .../paths/<path>/.
func SaveUnitArt(req UnitArtSaveRequest) error {
	if !unitIDPattern.MatchString(req.Faction) {
		return fmt.Errorf("faction %q must match %s", req.Faction, unitIDPattern)
	}
	if !unitIDPattern.MatchString(req.Unit) {
		return fmt.Errorf("unit %q must match %s", req.Unit, unitIDPattern)
	}
	if req.Path != "" && !unitIDPattern.MatchString(req.Path) {
		return fmt.Errorf("path %q must match %s", req.Path, unitIDPattern)
	}
	if len(req.Files) == 0 {
		return fmt.Errorf("no files in art request")
	}

	root, err := resolveUnitAssetsDir()
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	unitDir := filepath.Join(absRoot, req.Faction, req.Unit)
	if req.Path != "" {
		unitDir = filepath.Join(unitDir, unitPathsSubdirName, req.Path)
	}

	// Decode + validate EVERYTHING before writing anything, so a bad file in the
	// set doesn't leave a half-written art dir.
	type decoded struct {
		abs  string
		data []byte
	}
	var out []decoded
	total := 0
	for _, f := range req.Files {
		if !allowedUnitArtName(f.Name) {
			return fmt.Errorf("file name %q is not an allowed art file", f.Name)
		}
		raw, derr := base64.StdEncoding.DecodeString(f.ContentBase64)
		if derr != nil {
			return fmt.Errorf("file %q: bad base64: %w", f.Name, derr)
		}
		if len(raw) > maxUnitArtFileBytes {
			return fmt.Errorf("file %q exceeds %d bytes", f.Name, maxUnitArtFileBytes)
		}
		total += len(raw)
		if total > maxUnitArtTotalBytes {
			return fmt.Errorf("art request exceeds %d bytes", maxUnitArtTotalBytes)
		}
		abs := filepath.Join(unitDir, filepath.FromSlash(f.Name))
		// Belt-and-braces containment: the resolved write path must stay under root.
		rel, rerr := filepath.Rel(absRoot, abs)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("file %q escapes the art root", f.Name)
		}
		out = append(out, decoded{abs: abs, data: raw})
	}

	for _, d := range out {
		if err := os.MkdirAll(filepath.Dir(d.abs), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(d.abs, d.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
```

Add `encoding/base64` and `regexp` to the imports if not present.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/game/ -run 'TestSaveUnitArt|TestListUnitArt|TestReadUnitArtFile' -count=1`
Expected: PASS — the new write tests AND the existing Phase-2 read tests (the `resolveUnitAssetsDir` change must not break them).

- [ ] **Step 5: Add the HTTP route**

In `server/internal/http/router.go`, near the editor write routes:

```go
	mux.HandleFunc("/unit-art", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req game.UnitArtSaveRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 48<<20)).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_json", err.Error())
			return
		}
		if err := game.SaveUnitArt(req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "art_rejected", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"unit": req.Unit, "status": "saved"})
	})
```

(Confirm `writeJSONError` exists in this file/package — it's used by the `/units`, `/factions` handlers. Match its signature.)

Add an HTTP-level test in `editor_routes_test.go` (use the `newTestRouter(t)` helper): POST a valid one-file body with `UNIT_ASSETS_DIR` set to a temp dir → 201, file on disk; POST a traversal `name` → 400.

- [ ] **Step 6: Add the Vite proxy entry**

In `client/src/game-portal/vite.config.ts` proxy block:

```ts
      '/unit-art': { target: GO_SERVER, changeOrigin: true },
```

- [ ] **Step 7: Gates**

- `cd server && go test ./internal/game/ ./internal/http/ -count=1` → ok
- `cd server && go build ./... && go vet ./...` → clean
- `cd client/src/game-portal && npm run build` → clean (vite.config change compiles)

- [ ] **Step 8: Checkpoint (do not commit)**

---

### Task 2: Client — the pure sprite-packing layout core + golden conformance

**This is the crux. It must reproduce the CLI packer's layout exactly.**

**Files:**
- Create: `client/src/game-portal/src/game/units/spritePacking.ts`
- Create: `client/src/game-portal/src/game/units/spritePacking.test.ts`
- Create (fixture): `client/src/game-portal/src/game/units/__fixtures__/pack/` — a tiny synthetic export + its CLI-generated golden output (see Step 5 for how to generate).

**Interfaces:**
- `interface FrameRef { key: string; w: number; h: number }` — an opaque handle to one decoded frame plus its dimensions. `key` is how the rasterizer later fetches the pixels; the pure core never touches pixels.
- `interface SheetPlan { name: string; width: number; height: number; blits: Blit[] }`
- `interface Blit { srcKey: string; dstX: number; dstY: number; w: number; h: number }`
- `interface PackPlan { manifest: SpriteManifestJSON; sheets: SheetPlan[] }`
- `function planSpriteSheets(meta: PixelLabMeta, frameDims: Record<string, { w: number; h: number }>): PackPlan` — pure. `meta` is the (already `normalizeExportShape`-d) metadata; `frameDims` maps each referenced frame path to its decoded dimensions. Returns the manifest (minus `packedAt`) and the per-sheet blit plans.
- Also export the small pure helpers so they're unit-testable: `normalizeExportShape`, `animSlugFromHashedName`, `DIRECTION_ORDER`.

**Reproduce the CLI EXACTLY** (`pack-unit-sprites.mjs`, verified against source):
- `DIRECTION_ORDER = ['north','south','east','west','north-east','south-east','south-west','north-west']`.
- `animSlugFromHashedName(name) = name.split('-')[0].toLowerCase()`.
- `normalizeExportShape(meta)`: if `!meta.frames && Array.isArray(meta.states) && meta.states[0]` → `meta.states[0]`, else `meta`.
- `size = { width: meta.character?.size?.width ?? 64, height: meta.character?.size?.height ?? 64 }`.
- **Rotations** (`packRotationsSheet`): `dirs = DIRECTION_ORDER.filter(d => typeof rotations[d] === 'string')`; if empty → no rotations. `frameWidth/Height` from the first dir's frame; **throw on any size mismatch**. Sheet is `frameWidth` wide × `frameHeight × dirs.length` tall (1 column, N rows). Row `r` gets `rotations[dirs[r]]` at `dstY = r × frameHeight`. Manifest entry: `{ sheet: 'packed/rotations.png', rowOrder: dirs, frameWidth, frameHeight }`.
- **Animations** (`packAnimation`, per `frames.animations[hashedName]`): `slug = animSlugFromHashedName(hashedName)`; `byDir = frames.animations[hashedName]`; `dirs = DIRECTION_ORDER.filter(d => Array.isArray(byDir[d]) && byDir[d].length > 0)`; if empty → skip that animation. `frameCount = max over dirs of byDir[d].length`; `frameWidth/Height` from the first dir's first frame; **throw on any size mismatch across all frames**. Sheet is `frameWidth × frameCount` wide × `frameHeight × dirs.length` tall (columns = frames, rows = directions). Row `r`, frame `f`: `byDir[dirs[r]][f]` at `dstX = f × frameWidth`, `dstY = r × frameHeight` — **but only for `f < byDir[dirs[r]].length`** (a shorter row leaves trailing columns unblitted/transparent — DO NOT pad). Manifest entry: `{ frameCount, frameWidth, frameHeight, sheet: 'packed/<slug>.png', rowOrder: dirs }`.
- Manifest object: `{ key, size, ...(rotations ? { rotations } : {}), animations }`. (The core omits `packedAt` — the caller/server owns timestamps. The runtime doesn't read `packedAt`.)

- [ ] **Step 1: Write the pure-logic tests first** (these don't need the golden — they pin the math)

Create `spritePacking.test.ts` with cases that would catch the classic bugs:

```ts
import { describe, expect, it } from 'vitest'
import {
  animSlugFromHashedName, DIRECTION_ORDER, normalizeExportShape, planSpriteSheets,
} from './spritePacking'

describe('pure helpers', () => {
  it('slug is the pre-dash segment, lowercased', () => {
    expect(animSlugFromHashedName('Walking-1656a518')).toBe('walking')
    expect(animSlugFromHashedName('Attacking')).toBe('attacking')
  })
  it('normalizeExportShape unwraps states[0] only when frames is absent', () => {
    expect(normalizeExportShape({ states: [{ frames: { rotations: {} } }] })).toEqual({ frames: { rotations: {} } })
    const flat = { frames: { rotations: {} } }
    expect(normalizeExportShape(flat)).toBe(flat)
  })
})

describe('planSpriteSheets layout', () => {
  const dims = (w: number, h: number) => ({ w, h })

  it('animation sheet is frames-wide by directions-tall, rowOrder cardinals first', () => {
    const meta = {
      character: { size: { width: 8, height: 8 } },
      frames: {
        rotations: {},
        animations: {
          'Walking-abc': {
            north: ['a0', 'a1'],
            south: ['b0', 'b1'],
          },
        },
      },
    }
    const frameDims = { a0: dims(8, 8), a1: dims(8, 8), b0: dims(8, 8), b1: dims(8, 8) }
    const plan = planSpriteSheets(meta, frameDims)
    const walking = plan.manifest.animations!.walking
    expect(walking.frameCount).toBe(2)
    expect(walking.rowOrder).toEqual(['north', 'south'])
    const sheet = plan.sheets.find((s) => s.name === 'packed/walking.png')!
    expect(sheet.width).toBe(16)  // 2 frames × 8
    expect(sheet.height).toBe(16) // 2 dirs × 8
    // north row 0, south row 1; frame 1 of south at (8, 8)
    expect(sheet.blits).toContainEqual({ srcKey: 'b1', dstX: 8, dstY: 8, w: 8, h: 8 })
  })

  it('a direction with fewer frames leaves trailing columns unblitted (no padding)', () => {
    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: { rotations: {}, animations: { 'Cast-x': { north: ['n0', 'n1', 'n2'], south: ['s0'] } } },
    }
    const frameDims = { n0: dims(4, 4), n1: dims(4, 4), n2: dims(4, 4), s0: dims(4, 4) }
    const plan = planSpriteSheets(meta, frameDims)
    const sheet = plan.sheets.find((s) => s.name === 'packed/cast.png')!
    expect(sheet.width).toBe(12) // frameCount 3 × 4
    // south (row 1) has ONE frame; columns 1 and 2 must have NO blit.
    const southBlits = sheet.blits.filter((b) => b.dstY === 4)
    expect(southBlits).toHaveLength(1)
    expect(southBlits[0]).toMatchObject({ dstX: 0 })
  })

  it('rotations sheet is a 1-column vertical strip', () => {
    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: { rotations: { north: 'rn', south: 'rs' }, animations: {} },
    }
    const plan = planSpriteSheets(meta, { rn: dims(4, 4), rs: dims(4, 4) })
    const rot = plan.manifest.rotations!
    expect(rot).toMatchObject({ sheet: 'packed/rotations.png', rowOrder: ['north', 'south'], frameWidth: 4, frameHeight: 4 })
    const sheet = plan.sheets.find((s) => s.name === 'packed/rotations.png')!
    expect(sheet.width).toBe(4)
    expect(sheet.height).toBe(8)
    expect(sheet.blits).toContainEqual({ srcKey: 'rs', dstX: 0, dstY: 4, w: 4, h: 4 })
  })

  it('throws on a frame-size mismatch (the CLI does)', () => {
    const meta = {
      character: { size: { width: 4, height: 4 } },
      frames: { rotations: {}, animations: { 'Walk-x': { north: ['n0', 'n1'] } } },
    }
    expect(() => planSpriteSheets(meta, { n0: dims(4, 4), n1: dims(4, 5) })).toThrow(/mismatch/i)
  })
})
```

- [ ] **Step 2: Run to verify they fail** — `cd client/src/game-portal && npm run test -- spritePacking` → FAIL (module missing).

- [ ] **Step 3: Implement `spritePacking.ts`** — the pure core reproducing the algorithm above. No canvas, no DOM, no `pngjs` import. `SpriteManifestJSON` is the plain-object manifest shape (`key`, `size`, optional `rotations`, `animations`) — you may import the `SpriteManifest` type from `unitSprites.ts` or declare a local structural type; do NOT add `packedAt` (the core is timestamp-free).

- [ ] **Step 4: Run to verify they pass** — `npm run test -- spritePacking` → PASS.

- [ ] **Step 5: Generate the golden fixture, then write the conformance test**

Generate the golden ONCE from the real CLI (this proves the TS core matches actual CLI output):
1. Create a tiny synthetic export at `src/assets/units/human/__packtest__/` with a `metadata.json` referencing 2 rotations (north, south) and one 2-direction × 2-frame animation, plus the raw 4×4 PNGs it names, each frame a DISTINCT solid color (so a wrong row/column placement produces different pixels). Use a scratch Node snippet with `pngjs` to emit the colored PNGs — or hand-place them; they're 4×4.
2. Run `npm run pack:sprites`. It writes `packed/*.png` + `sprites.json` under `__packtest__/`.
3. Copy `__packtest__/metadata.json`, its raw frame PNGs, its `packed/`, and `sprites.json` into `src/game/units/__fixtures__/pack/`.
4. **Revert the asset-tree pollution**: `git checkout -- src/assets/units && rm -rf src/assets/units/human/__packtest__` — the fixture lives ONLY under `__fixtures__/`, never in the shipped asset tree.
5. Add a comment at the top of the fixture dir (a `README.md`) with this exact regen recipe, so a future CLI change can be re-goldened.

Then the conformance test (`spritePacking.test.ts`, appended) — uses `pngjs` (a devDependency, pure JS, works in vitest) as the rasterizer for BOTH sides, so PNG encoding is apples-to-apples and the comparison is at the pixel level:

```ts
import { PNG } from 'pngjs'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const fixture = (rel: string) =>
  fileURLToPath(new URL(`./__fixtures__/pack/${rel}`, import.meta.url))

describe('conformance with the CLI packer (golden fixture)', () => {
  const meta = JSON.parse(readFileSync(fixture('metadata.json'), 'utf8'))
  const golden = JSON.parse(readFileSync(fixture('sprites.json'), 'utf8'))

  // Decode every referenced frame via pngjs so the core gets real dimensions,
  // and so we can rasterize its blit plan the same way the CLI did.
  const frameDims: Record<string, { w: number; h: number }> = {}
  const framePixels: Record<string, PNG> = {}
  for (const rel of collectFramePaths(meta)) {
    const png = PNG.sync.read(readFileSync(fixture(rel)))
    frameDims[rel] = { w: png.width, h: png.height }
    framePixels[rel] = png
  }

  const plan = planSpriteSheets(normalizeExportShape(meta), frameDims)

  it('manifest matches the CLI output (ignoring packedAt)', () => {
    const { packedAt: _drop, ...goldenNoTs } = golden
    expect(plan.manifest).toEqual(goldenNoTs)
  })

  it('every sheet decodes to the same RGBA pixels as the CLI sheet', () => {
    for (const sheet of plan.sheets) {
      const ours = rasterizeWithPngjs(sheet, framePixels) // execute blits into a PNG
      const cli = PNG.sync.read(readFileSync(fixture(sheet.name)))
      expect(ours.width).toBe(cli.width)
      expect(ours.height).toBe(cli.height)
      expect(Buffer.compare(ours.data, cli.data)).toBe(0) // RGBA byte-identical
    }
  })
})
```

Write the two helpers used above (`collectFramePaths(meta)`, `rasterizeWithPngjs(sheet, framePixels)`) inside the test file — `rasterizeWithPngjs` allocates a zero-init `new PNG({width, height})` and copies each blit's source rows in, exactly like the CLI's copy loop.

> If the manifest test fails, the TS core's layout diverges from the CLI — fix the CORE, not the golden. Only regen the golden when `pack-unit-sprites.mjs` itself changed.

- [ ] **Step 6: Gates** — `npm run test -- spritePacking` → PASS; `npx vue-tsc -b` → clean; confirm `git status src/assets/units/` shows NO new files (the fixture is under `__fixtures__/` only).

- [ ] **Step 7: Checkpoint (do not commit)**

---

### Task 3: Client — the browser rasterizer + export-folder reader

**Files:**
- Create: `client/src/game-portal/src/game/units/spriteIngest.ts`
- Create: `client/src/game-portal/src/game/units/spriteIngest.test.ts`

**Interfaces:**
- `interface DroppedFile { path: string; blob: Blob }` — one file from the dropped folder, `path` relative to the folder root (from `webkitRelativePath` minus its first segment).
- `interface IngestResult { manifest: SpriteManifestJSON; sheets: { name: string; blob: Blob }[]; portrait?: Blob; warnings: string[] }`
- `async function ingestExportFolder(files: DroppedFile[]): Promise<IngestResult>` — finds `metadata.json`, decodes the referenced frames (`createImageBitmap`), calls `planSpriteSheets`, rasterizes each `SheetPlan` to a PNG `Blob` via canvas, and returns the packable set. Surfaces the CLI's validation failures as thrown errors or `warnings` (see below).
- `function packedSheetToObjectUrls(result: IngestResult): { urls: Record<string, string>; revoke: () => void }` — turns the sheet blobs into object URLs keyed by manifest-relative path (`packed/walking.png`), for feeding `buildSpriteSet` in Task 4. `revoke()` frees them.

**Validation to surface (matches the CLI's failure modes + the spec's ingest requirements):**
- No `metadata.json` in the drop → throw with a clear message.
- A frame referenced by metadata but absent from the drop → throw naming the missing file.
- Frame-size mismatch across a row → `planSpriteSheets` throws; surface it verbatim.
- Zero animations AND zero rotations → throw ("nothing to pack").
- `meta.states.length > 1` → push a `warning` ("Multi-state export: only the first state was packed" — the CLI takes `states[0]`, and this editor inherits that limit; the author must be told, not silently truncated).

**Canvas rasterization** (browser-only, E2E-validated — happy-dom can't run it, so the unit tests cover the READER + validation, not the pixel output):
- For each `SheetPlan`: `const canvas = new OffscreenCanvas(width, height)` (or a detached `<canvas>` fallback), `ctx.imageSmoothingEnabled = false`, `ctx.clearRect(...)` (transparent — matches the CLI zero-init and the uneven-frame trailing-transparency), then `ctx.drawImage(bitmap, blit.dstX, blit.dstY)` per blit, then `canvas.convertToBlob({ type: 'image/png' })`.

- [ ] **Step 1: Write the failing tests** (reader + validation logic — NOT canvas pixels)

Create `spriteIngest.test.ts`. Mock `DroppedFile`s with real tiny PNG `Blob`s built from `pngjs` buffers (or `Uint8Array`s) — the test exercises path-matching, missing-frame detection, the multi-state warning, and the "no metadata" / "nothing to pack" throws. Stub `createImageBitmap` (happy-dom lacks it) to resolve a `{ width, height }`-bearing object so the reader can obtain dims; assert `ingestExportFolder` throws/warns correctly. Do NOT assert on rasterized pixels here.

Key cases:
```ts
it('throws when metadata.json is missing', async () => { /* files without metadata.json */ })
it('throws naming a frame referenced by metadata but absent from the drop', async () => { /* metadata references rotations/south.png, not provided */ })
it('warns (does not throw) on a multi-state export and packs states[0]', async () => { /* meta.states.length === 2 → result.warnings has the multi-state note */ })
it('throws when there is nothing to pack (no animations, no rotations)', async () => {})
it('produces one sheet per animation plus rotations, keyed by manifest path', async () => { /* result.sheets names include packed/walking.png, packed/rotations.png */ })
```

- [ ] **Step 2: Run to verify they fail** — `npm run test -- spriteIngest` → FAIL.

- [ ] **Step 3: Implement `spriteIngest.ts`** — the reader + validation + canvas rasterizer, delegating ALL layout to `planSpriteSheets`. The reader owns: path normalization, frame decode (`createImageBitmap`), missing-frame detection, multi-state warning; the rasterizer owns only the mechanical `drawImage` per blit. Guard `OffscreenCanvas` absence with a `document.createElement('canvas')` fallback so it works in the Tauri webview and older engines.

- [ ] **Step 4: Run to verify they pass** — `npm run test -- spriteIngest` → PASS. `npx vue-tsc -b` → clean.

- [ ] **Step 5: Checkpoint (do not commit)**

---

### Task 4: Client — API + drop zone + preview-before-persist wiring

**Files:**
- Modify: `client/src/game-portal/src/game/units/editorCatalogApi.ts` (add `saveUnitArt`)
- Modify: `client/src/game-portal/src/components/UnitSpritePreview.vue` OR `UnitTypeEditorPanel.vue` (drop zone + ingest flow — see note)
- Modify: `client/src/game-portal/src/game/rendering/unitSprites.ts` IF a portrait-from-blob hook is needed (likely not — reuse `buildSpriteSet` + `registerRuntimeSpriteSet` as-is)

**Interfaces:**
- `saveUnitArt(payload: { faction: string; unit: string; path?: string; files: { name: string; contentBase64: string }[] }): Promise<void>` — POSTs `/unit-art`, surfaces the 400 `art_rejected` message as an error.

**The flow (preview-before-persist, per decision P3-B):**
1. A drop zone (a `<input type="file" webkitdirectory>` + a drag target) in the Preview section accepts a folder. Read its `FileList` into `DroppedFile[]` (`file.webkitRelativePath` minus the leading folder segment).
2. `ingestExportFolder(files)` → `IngestResult`. Show any `warnings` inline; on throw, show the error and stop.
3. `packedSheetToObjectUrls(result)` → object URLs. `buildSpriteSet(form.type, result.manifest, rel => urls[rel])` → a `UnitSpriteSet`; `registerRuntimeSpriteSet(it)`; `preview.refresh()`. **The author now sees the freshly-packed art animating, before anything is saved.**
4. A **Save Art** button: base64-encode each sheet blob + the `sprites.json` (serialize `result.manifest` + `packedAt`) + `metadata.json` + optional `portrait`, `saveUnitArt({ faction: form.faction, unit: form.type, path: <if a path unit>, files })`, then `await loadRuntimeSpriteSets()` (re-fetches from the server, replacing the in-memory object-URL set with the persisted one) and `preview.refresh()`. Then `revoke()` the object URLs.
5. A **Discard** action (or re-drop): `revoke()` the URLs, `clearRuntimeSpriteSets()`-of-just-this-key is not available, so instead call `loadRuntimeSpriteSets()` to reset to the server truth, and `preview.refresh()`.

**Object-URL hygiene:** revoke on save, on discard, on a new drop replacing the old, and on component unmount. A leaked object URL per drop is a real memory bug in a modal the author opens repeatedly.

**Faction/path context:** the payload's `faction` comes from `form.faction`; `unit` from `form.type`. Phase 3 targets **base units** (path art ingest is natural once Phase 5 adds path entities — a base-unit drop with `path` omitted is the whole scope here). If `form.type`/`form.faction` is blank, disable the drop zone with a hint ("set the unit's type and faction first").

- [ ] **Step 1: Add `saveUnitArt`** to `editorCatalogApi.ts`, mirroring the existing `saveFaction` 400-handling idiom (parse `{error:"art_rejected", message}` → throw `EditorValidationError`). Add a focused test (stub `fetch`): asserts the POST body shape `{faction, unit, files}` and that a 400 surfaces the message.

- [ ] **Step 2: Build the drop zone + ingest flow** in the Preview section. Reuse the Phase 2 `preview` ref's `refresh()`. No literal `cursor:` declarations. Keep the drag/drop consistent with the app's global drag rules (see `main.ts` — a deliberate drag target needs to not be swallowed by the global `dragstart` preventDefault; use `@drop.prevent` / `@dragover.prevent` on the zone).

- [ ] **Step 3: Gates**

- `cd client/src/game-portal && npx vue-tsc -b && npm run build && npm run test` → clean, no new failures
- `grep -n "cursor:"` on the touched components → only `not-allowed` permitted

- [ ] **Step 4: Checkpoint (do not commit)**

---

### Task 5: Verification sweep + END-TO-END proof

**The unit tests prove the layout math and the reader; they CANNOT prove the canvas rasterizer or the ingest-to-screen path (happy-dom has no canvas / no real image decode). The E2E is the real proof.**

- [ ] **Step 1: Full gates**

- `cd server && go vet ./... && go build ./... && go test ./... -count=1` → all ok
- `cd client/src/game-portal && npm run test && npx vue-tsc -b && npm run build` → green + clean

- [ ] **Step 2: Server write E2E (real HTTP, isolated dir)**

Start the Go server on a spare port with `UNIT_ASSETS_DIR=<tmp>`:
- `POST /unit-art` a valid base64 set (one `sprites.json` + one `packed/walking.png`) → 201; files on disk under `<tmp>/human/<unit>/`.
- `GET /catalog/unit-art` → the new unit appears. `GET /assets/units/human/<unit>/packed/walking.png` → 200 `image/png`.
- `POST /unit-art` with a `name` of `../../secret.png` → 400, nothing written outside the root.

- [ ] **Step 3: The ingest milestone (real dev stack)**

With `npm run dev` + the Go server on 8080 (point its `UNIT_ASSETS_DIR` at an isolated scratch dir, NOT the source tree, so the test doesn't pollute committed art):
1. Open `/unit-type-editor`, create a new unit (type + faction), or select one.
2. Drop a **real** PixelLab export folder (copy one of the shipped units' source export — `src/assets/units/human/archer/` has `metadata.json` + raw frames; if the raw frames were gitignored/pruned, regenerate a minimal export or use the Task-2 fixture scaled up).
3. **The preview plays the freshly-packed animation and rotations, before saving.** This is the preview-before-persist proof.
4. Click **Save Art** → confirm the files land in the isolated `UNIT_ASSETS_DIR`, and after the `loadRuntimeSpriteSets()` refresh the preview still shows the art (now from the server, not the object URLs).
5. Place that unit and **playtest** → it renders with the ingested art, no rebuild.
6. Drop a mismatched/broken export (a frame of the wrong size) → the panel shows the validation error and does NOT upload.
7. Drop a multi-state export (or fake `states: [..,..]`) → the multi-state warning shows and it packs the first state.

- [ ] **Step 4: Object-URL leak check**

In devtools, drop → discard → drop → save several times; confirm object URLs are revoked (no unbounded growth in `performance.memory` / no leaked blob: URLs). This is the one hygiene bug most likely to slip through.

- [ ] **Step 5: Hygiene**

- `git status` shows NO stray art under `src/assets/units/` and NO `__packtest__` residue; the ONLY new committed fixture is under `src/game/units/__fixtures__/pack/`.
- The isolated scratch `UNIT_ASSETS_DIR` used for testing is outside the repo (or its contents reverted).
- Confirm `pack-unit-sprites.mjs` is unchanged (`git diff --stat` on it is empty).

- [ ] **Step 6: Report** — gates, the E2E result (with emphasis on the preview-before-persist and playtest-with-no-rebuild proofs), and anything that deviated.
