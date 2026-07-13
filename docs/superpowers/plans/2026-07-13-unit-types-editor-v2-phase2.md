# Unit-Types Editor v2 — Phase 2: Runtime Sprite Overlay & Animation Viewer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unit art can be served from a writable directory at runtime and shadows the build-time bundled art, and the editor can play back any unit's rotations and animations — so that Phase 3's ingested art can be previewed and playtested with no rebuild.

**Architecture:** The Go server gains a read-only art surface (`GET /catalog/unit-art` to enumerate manifests, `GET /assets/units/...` to serve the sheets) over a new `UNIT_ASSETS_DIR`. The client's `unitSprites.ts` gains a runtime overlay map consulted *before* the `import.meta.glob` bundled map — mirroring the server's existing overlay-over-embed model for `UnitDef`. A new `UnitSpritePreview.vue` plays the resulting sprite set back on a canvas.

**Tech Stack:** Go 1.22 (`server/`), Vue 3 + TypeScript SPA (`client/src/game-portal`, vitest + happy-dom).

**Spec:** `docs/superpowers/specs/2026-07-13-unit-types-editor-v2-design.md` §4, §5.1

**Phase 2 of 5.** Phase 1 (factions, archetypes, stat floors) is complete. Phase 3 (browser packer + art ingest) depends on this and is where art gets *written*; Phase 2 is read-only.

---

## Two corrections to the spec — read before starting

The spec was written before the code was checked. Two of its claims are wrong, and both make this phase **smaller**:

**1. The rotations row-offset refactor is NOT needed. Do not do it.**
Spec §4.2 says the canvas-slicing in `loadPackedRotations` must be replaced because an HTTP-served sheet would taint the canvas and make `toDataURL` throw. That is false here: `vite.config.ts` proxies API routes to the Go server, and in the packaged build the Go binary serves the SPA. **Art served from `/assets/...` is same-origin in both cases**, so there is no taint. `loadPackedRotations` is reused as-is for runtime sets.

This matters: that refactor would have changed `UnitSpriteSet.rotations` from `DirectionMap<HTMLImageElement>` to a row-offset shape, and `getUnitPortraitUrl` (`unitSprites.ts:321-332`) returns `rotations.south.src` **straight into an `<img>` tag**. A row-offset source's `.src` is the whole 8-row vertical strip — every portrait would have rendered as a column of all eight facings. Leaving rotations alone avoids this entirely.

**2. The Vite proxy gap is already fixed** (`/units`, `/factions` were missing and their writes 404'd in dev). You still must add `/assets`.

---

## Global Constraints

- **Do not run `git commit` or `git add`.** The user handles staging. Each task ends with a **Checkpoint**.
- Go from `server/`; client from `client/src/game-portal`. Client typecheck is **`npx vue-tsc -b`** (build mode).
- `gofmt -l` flags the whole checkout (CRLF) — gates are `go vet` / `go build`.
- **No literal `cursor:` declarations** in component CSS (`cursor: not-allowed` on forbidden states only). Global rules already paint the game cursor.
- Do not modify the item editor or the old map editor.
- **Every new server route the SPA calls MUST be added to the `proxy` block in `vite.config.ts`**, or it silently 404s in `npm run dev`. This exact bug shipped once already.
- Server must degrade gracefully: if `UNIT_ASSETS_DIR` does not resolve, `/catalog/unit-art` returns an empty list and the client falls back to bundled art. A missing art dir is NOT an error.

---

### Task 1: Server — `UNIT_ASSETS_DIR` + `GET /catalog/unit-art`

**Files:**
- Create: `server/internal/game/unit_art.go`
- Create: `server/internal/game/unit_art_test.go`
- Modify: `server/internal/http/router.go` (add the route near the other `/catalog/*` handlers)
- Modify: `server/internal/http/editor_routes_test.go` (add `/catalog/unit-art` to the existing table-driven route test — note it may legitimately return an EMPTY array in CI, so assert 200 + the key exists, NOT non-empty)

**Interfaces:**
- Produces: `type UnitArtEntry struct { Key, Faction, Unit, Path, BaseURL string; Manifest json.RawMessage }`
- Produces: `func resolveUnitAssetsDir() (string, error)`, `func ListUnitArt() []UnitArtEntry`
- Consumed by: the client overlay (Task 3) and `GET /assets/units/...` (Task 2).

**Layout being walked** (mirrors the client's asset tree exactly):
```
<UNIT_ASSETS_DIR>/<faction>/<unit>/sprites.json                  → key = <unit>
<UNIT_ASSETS_DIR>/<faction>/<unit>/paths/<path>/sprites.json     → key = <path>
```
The key is the directory immediately containing `sprites.json` — the same rule `unitSprites.ts:232` uses. `Manifest` is passed through as raw JSON: the client already knows the shape, and re-modeling it in Go would be a second source of truth that can drift.

- [ ] **Step 1: Write the failing test**

Create `server/internal/game/unit_art_test.go`:

```go
package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeArtFixture(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListUnitArt_FindsBaseUnitsAndPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)

	writeArtFixture(t, dir, "human/archer/sprites.json", `{"key":"archer","size":{"width":104,"height":104}}`)
	writeArtFixture(t, dir, "human/archer/paths/marksman/sprites.json", `{"key":"marksman"}`)

	byKey := map[string]UnitArtEntry{}
	for _, e := range ListUnitArt() {
		byKey[e.Key] = e
	}

	archer, ok := byKey["archer"]
	if !ok {
		t.Fatal("base unit art not found")
	}
	if archer.Faction != "human" || archer.Unit != "archer" || archer.Path != "" {
		t.Fatalf("archer entry wrong: %+v", archer)
	}
	if archer.BaseURL != "/assets/units/human/archer" {
		t.Fatalf("archer BaseURL = %q, want /assets/units/human/archer", archer.BaseURL)
	}
	// The manifest must round-trip verbatim — the client parses it, not the server.
	var manifest map[string]any
	if err := json.Unmarshal(archer.Manifest, &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if manifest["key"] != "archer" {
		t.Fatalf("manifest did not round-trip: %v", manifest)
	}

	marksman, ok := byKey["marksman"]
	if !ok {
		t.Fatal("promotion-path art not found")
	}
	if marksman.Path != "marksman" || marksman.Unit != "archer" {
		t.Fatalf("marksman entry wrong: %+v", marksman)
	}
	if marksman.BaseURL != "/assets/units/human/archer/paths/marksman" {
		t.Fatalf("marksman BaseURL = %q", marksman.BaseURL)
	}
}

// A missing art dir is not an error — the client just falls back to bundled art.
func TestListUnitArt_MissingDirIsEmptyNotFatal(t *testing.T) {
	t.Setenv("UNIT_ASSETS_DIR", filepath.Join(t.TempDir(), "does_not_exist"))
	if got := ListUnitArt(); len(got) != 0 {
		t.Fatalf("want empty, got %d entries", len(got))
	}
}

// A malformed sprites.json is skipped, not fatal — one bad unit must not take
// the whole art catalog down.
func TestListUnitArt_SkipsMalformedManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	writeArtFixture(t, dir, "human/good/sprites.json", `{"key":"good"}`)
	writeArtFixture(t, dir, "human/bad/sprites.json", `{ NOT JSON`)

	keys := map[string]bool{}
	for _, e := range ListUnitArt() {
		keys[e.Key] = true
	}
	if !keys["good"] {
		t.Fatal("the valid manifest was dropped along with the bad one")
	}
	if keys["bad"] {
		t.Fatal("a malformed manifest was served")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestListUnitArt -count=1`
Expected: FAIL — `undefined: UnitArtEntry`.

- [ ] **Step 3: Create `unit_art.go`**

```go
package game

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// spriteManifestFileName is the generated per-unit sprite manifest, emitted by
// the sprite packer beside the packed/ sheets it describes.
const spriteManifestFileName = "sprites.json"

// unitArtURLPrefix is the public mount point for the writable art dir. Kept in
// one place because the client resolves every sheet relative to BaseURL.
const unitArtURLPrefix = "/assets/units"

// UnitArtEntry describes one unit's (or promotion path's) packed art on disk.
//
// Manifest is passed through as RAW JSON on purpose: the client already owns
// the sprite-manifest shape (unitSprites.ts), and re-modeling it in Go would
// create a second source of truth that silently drifts from the packer.
type UnitArtEntry struct {
	Key      string          `json:"key"`
	Faction  string          `json:"faction"`
	Unit     string          `json:"unit"`
	Path     string          `json:"path,omitempty"`
	BaseURL  string          `json:"baseUrl"`
	Manifest json.RawMessage `json:"manifest"`
}

// resolveUnitAssetsDir returns the writable unit-art dir: UNIT_ASSETS_DIR if
// set, else the SPA's asset tree in the dev checkout (the server runs from
// server/, so the client tree is one level up).
func resolveUnitAssetsDir() (string, error) {
	if dir := os.Getenv("UNIT_ASSETS_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, "..", "client", "src", "game-portal", "src", "assets", "units")
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	return "", fmt.Errorf("unit assets directory not found at %s; set UNIT_ASSETS_DIR to override", dir)
}

// ListUnitArt enumerates every packed sprite manifest under the writable art
// dir, for both base units and promotion paths.
//
// A missing dir yields an empty list, NOT an error: the client falls back to
// its build-time bundled art, which is the correct degraded state.
func ListUnitArt() []UnitArtEntry {
	dir, err := resolveUnitAssetsDir()
	if err != nil {
		return nil
	}
	var out []UnitArtEntry
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || d.Name() != spriteManifestFileName {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return nil
		}
		entry, ok := parseUnitArtPath(filepath.ToSlash(rel))
		if !ok {
			return nil
		}
		raw, rferr := os.ReadFile(p)
		if rferr != nil || !json.Valid(raw) {
			// One malformed manifest must not take down the whole art catalog.
			return nil
		}
		entry.Manifest = json.RawMessage(raw)
		out = append(out, entry)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// parseUnitArtPath maps a slash-separated path relative to the art root onto an
// entry. Accepts exactly the two shapes the packer emits:
//
//	<faction>/<unit>/sprites.json
//	<faction>/<unit>/paths/<path>/sprites.json
//
// The key is the directory immediately containing the manifest — the same rule
// the client's sprite loader uses, so keys line up on both sides.
func parseUnitArtPath(rel string) (UnitArtEntry, bool) {
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) == 3 && parts[2] == spriteManifestFileName:
		return UnitArtEntry{
			Key:     parts[1],
			Faction: parts[0],
			Unit:    parts[1],
			BaseURL: path.Join(unitArtURLPrefix, parts[0], parts[1]),
		}, true
	case len(parts) == 5 && parts[2] == unitPathsSubdirName && parts[4] == spriteManifestFileName:
		return UnitArtEntry{
			Key:     parts[3],
			Faction: parts[0],
			Unit:    parts[1],
			Path:    parts[3],
			BaseURL: path.Join(unitArtURLPrefix, parts[0], parts[1], unitPathsSubdirName, parts[3]),
		}, true
	}
	return UnitArtEntry{}, false
}
```

Note `path.Join` (not `filepath.Join`) for `BaseURL` — it is a URL, and on Windows `filepath.Join` would emit backslashes.

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestListUnitArt -count=1`
Expected: PASS, all three.

- [ ] **Step 5: Add the route**

In `server/internal/http/router.go`, alongside the other `/catalog/*` handlers:

```go
	mux.HandleFunc("/catalog/unit-art", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"art": game.ListUnitArt(),
		})
	})
```

- [ ] **Step 6: Extend the route test**

`server/internal/http/editor_routes_test.go` has a table-driven test asserting each catalog route returns 200 with a **non-empty** array under its key. `/catalog/unit-art` can legitimately be EMPTY (no art dir in a CI checkout), so do NOT add it to that table. Add a separate case asserting 200 and that the `art` key is present (array may be empty).

- [ ] **Step 7: Gates**

- `cd server && go test ./internal/game/ ./internal/http/ -count=1` → ok
- `cd server && go build ./... && go vet ./internal/game/ ./internal/http/` → clean

- [ ] **Step 8: Checkpoint (do not commit)**

---

### Task 2: Server — `GET /assets/units/...` static art serving

**Files:**
- Modify: `server/internal/game/unit_art.go` (add `ReadUnitArtFile`)
- Modify: `server/internal/game/unit_art_test.go` (add traversal + type tests)
- Modify: `server/internal/http/router.go` (add the `/assets/units/` handler)
- Modify: `client/src/game-portal/vite.config.ts` (proxy `/assets`)

**Interfaces:**
- Produces: `func ReadUnitArtFile(rel string) (data []byte, contentType string, ok bool)`

**Security — this serves files off disk, so it is the highest-risk surface in Phase 2:**
- Only `.png` and `.json` are servable. Anything else → not found.
- The resolved absolute path MUST stay inside the art root. Reject `..`, absolute paths, and anything that escapes after `filepath.Clean`.
- No directory listing.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/unit_art_test.go`:

```go
func TestReadUnitArtFile_ServesPNGAndJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	writeArtFixture(t, dir, "human/archer/sprites.json", `{"key":"archer"}`)
	writeArtFixture(t, dir, "human/archer/packed/walking.png", "\x89PNG\r\n\x1a\n fake")

	if _, ct, ok := ReadUnitArtFile("human/archer/sprites.json"); !ok || ct != "application/json" {
		t.Fatalf("sprites.json: ok=%v ct=%q", ok, ct)
	}
	if _, ct, ok := ReadUnitArtFile("human/archer/packed/walking.png"); !ok || ct != "image/png" {
		t.Fatalf("walking.png: ok=%v ct=%q", ok, ct)
	}
}

// THE security test. Every one of these must be refused.
func TestReadUnitArtFile_RejectsTraversalAndOtherTypes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	writeArtFixture(t, dir, "human/archer/sprites.json", `{"key":"archer"}`)
	// A secret sitting next to the art root — the classic traversal target.
	if err := os.WriteFile(filepath.Join(dir, "..", "secret.txt"), []byte("shh"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{
		"../secret.txt",
		"../../secret.txt",
		"human/../../secret.txt",
		"human/archer/../../../secret.txt",
		"/etc/passwd",
		"human/archer/notes.txt",   // wrong extension
		"human/archer/script.js",   // wrong extension
		"",
	} {
		if _, _, ok := ReadUnitArtFile(bad); ok {
			t.Fatalf("ReadUnitArtFile(%q) was SERVED — it must be refused", bad)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd server && go test ./internal/game/ -run TestReadUnitArtFile -count=1`
Expected: FAIL — `undefined: ReadUnitArtFile`.

- [ ] **Step 3: Implement `ReadUnitArtFile`**

Append to `unit_art.go`:

```go
// unitArtContentTypes is the ALLOWLIST of servable art file types. Anything not
// in this map is not served — an allowlist, never a denylist, because this
// handler reads straight off the filesystem.
var unitArtContentTypes = map[string]string{
	".png":  "image/png",
	".json": "application/json",
}

// ReadUnitArtFile reads one file from the writable art dir, given a path
// relative to that dir (as it appears after the /assets/units/ URL prefix).
//
// Refuses anything that is not an allowlisted type, and anything that escapes
// the art root after cleaning — the escape check compares the RESOLVED absolute
// path against the root, so it holds regardless of how the traversal is spelled.
func ReadUnitArtFile(rel string) (data []byte, contentType string, ok bool) {
	if rel == "" || strings.Contains(rel, "\x00") {
		return nil, "", false
	}
	ct, allowed := unitArtContentTypes[strings.ToLower(path.Ext(rel))]
	if !allowed {
		return nil, "", false
	}
	root, err := resolveUnitAssetsDir()
	if err != nil {
		return nil, "", false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, "", false
	}
	target := filepath.Join(absRoot, filepath.FromSlash(rel))
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, "", false
	}
	// Containment check on the resolved paths. filepath.Rel gives a path
	// starting with ".." exactly when absTarget is outside absRoot.
	relCheck, err := filepath.Rel(absRoot, absTarget)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return nil, "", false
	}
	info, err := os.Stat(absTarget)
	if err != nil || info.IsDir() {
		return nil, "", false
	}
	raw, err := os.ReadFile(absTarget)
	if err != nil {
		return nil, "", false
	}
	return raw, ct, true
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd server && go test ./internal/game/ -run TestReadUnitArtFile -count=1`
Expected: PASS — including every traversal case.

- [ ] **Step 5: Add the HTTP handler**

In `server/internal/http/router.go`:

```go
	mux.HandleFunc("/assets/units/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/assets/units/")
		data, contentType, ok := game.ReadUnitArtFile(rel)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		// Art is content-addressed by the editor's save cycle, not by URL, so it
		// must NOT be cached long — an edited sheet reuses the same URL.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
```

- [ ] **Step 6: Add the Vite proxy entry — REQUIRED**

In `client/src/game-portal/vite.config.ts`, add to the `proxy` block (this is the bug that already shipped once — a server route the SPA calls that is not proxied silently 404s in dev):

```ts
      '/assets': { target: GO_SERVER, changeOrigin: true },
```

> Note: Vite serves its OWN `/src/assets/...` bundled URLs with hashed paths under `/@fs` or `/src/`, not under `/assets/`, so this prefix does not collide with the bundler in dev. In the production build, static assets land in `/assets/*.js|css` with hashed names — but the Go binary serves those from the embedded SPA, and this route only matches `/assets/units/`, which the bundler never emits. **Verify both in Task 7** (dev: art loads AND the app boots; prod build: `npm run build` output still loads).

- [ ] **Step 7: Gates**

- `cd server && go test ./internal/game/ ./internal/http/ -count=1` → ok
- `cd server && go build ./... && go vet ./...` → clean

- [ ] **Step 8: Checkpoint (do not commit)**

---

### Task 3: Client — the runtime sprite overlay

**This is the core of Phase 2.** Everything downstream depends on it.

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/unitSprites.ts`
- Create: `client/src/game-portal/src/game/rendering/unitSprites.overlay.test.ts`

**Interfaces:**
- Produces: `registerRuntimeSpriteSet(set: UnitSpriteSet): void`, `clearRuntimeSpriteSets(): void`, `loadRuntimeSpriteSets(): Promise<number>`, `buildSpriteSet(key, manifest, resolveUrl): UnitSpriteSet | null`
- Changes: `getUnitSpriteSet` consults the runtime map FIRST; `getUnitPortraitImage` consults a runtime portrait map first.

**Two design points that are load-bearing:**

1. **Register only AFTER the images decode.** `getUnitFrame` returns `null` when an image isn't ready, and the renderer then draws a procedural placeholder. If a runtime set is registered while its PNGs are still in flight, it *shadows* the perfectly-good bundled set and every unit flashes as a placeholder for a few hundred ms at boot. So `loadRuntimeSpriteSets` must await decode, *then* register. This is the difference between a seamless overlay and a visible regression.

2. **Reuse the existing manifest→set builder.** The module currently inlines that logic in a top-level `for` loop over `manifestGlob` (`unitSprites.ts:227-297`). Extract it to `buildSpriteSet(key, manifest, resolveUrl)` where `resolveUrl(relative) → string | undefined`, and have BOTH the bundled loop (resolving via `stripGlob`) and the runtime loader (resolving via `${baseUrl}/${rel}`) call it. Two copies of this logic will drift.

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/game/rendering/unitSprites.overlay.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  buildSpriteSet,
  clearRuntimeSpriteSets,
  getUnitSpriteSet,
  loadRuntimeSpriteSets,
  registerRuntimeSpriteSet,
  type UnitSpriteSet,
} from './unitSprites'

afterEach(() => {
  clearRuntimeSpriteSets()
  vi.restoreAllMocks()
})

const MANIFEST = {
  key: 'overlay_unit',
  size: { width: 104, height: 104 },
  animations: {
    walking: {
      frameCount: 4,
      frameWidth: 104,
      frameHeight: 104,
      sheet: 'packed/walking.png',
      rowOrder: ['north', 'south', 'east', 'west'],
    },
  },
}

describe('buildSpriteSet', () => {
  it('resolves sheet urls through the injected resolver', () => {
    const seen: string[] = []
    const set = buildSpriteSet('overlay_unit', MANIFEST, (rel) => {
      seen.push(rel)
      return `https://example.test/${rel}`
    })
    expect(seen).toContain('packed/walking.png')
    expect(set).not.toBeNull()
    expect(set!.size).toEqual({ width: 104, height: 104 })

    const walking = set!.animations.get('walking')
    expect(walking?.frameCount).toBe(4)
    // rowOrder[i] must become the row index for that direction — this is what
    // getUnitFrame uses as srcY, so an off-by-one here draws the wrong facing.
    expect(walking?.directions.north?.row).toBe(0)
    expect(walking?.directions.west?.row).toBe(3)
  })

  it('returns a set with no animations when no sheet resolves', () => {
    const set = buildSpriteSet('missing', MANIFEST, () => undefined)
    expect(set!.animations.size).toBe(0)
  })
})

describe('runtime overlay', () => {
  it('a runtime set shadows a bundled set with the same key', () => {
    // 'archer' is bundled (real art in src/assets). Confirm it resolves first…
    const bundled = getUnitSpriteSet('archer')
    expect(bundled).not.toBeNull()

    const fake: UnitSpriteSet = {
      key: 'archer',
      size: { width: 1, height: 1 },
      rotations: {},
      animations: new Map(),
      beamOrigin: { x: 0, y: 0 },
    }
    registerRuntimeSpriteSet(fake)

    // …and now the overlay wins.
    expect(getUnitSpriteSet('archer')!.size).toEqual({ width: 1, height: 1 })

    clearRuntimeSpriteSets()
    expect(getUnitSpriteSet('archer')!.size).not.toEqual({ width: 1, height: 1 })
  })

  it('still prefers the path key over the unit type', () => {
    const fake: UnitSpriteSet = {
      key: 'marksman',
      size: { width: 7, height: 7 },
      rotations: {},
      animations: new Map(),
      beamOrigin: { x: 0, y: 0 },
    }
    registerRuntimeSpriteSet(fake)
    expect(getUnitSpriteSet('marksman', 'archer')!.size).toEqual({ width: 7, height: 7 })
  })
})

describe('loadRuntimeSpriteSets', () => {
  it('maps the {art:[...]} envelope and registers each entry by key', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        art: [{ key: 'overlay_unit', baseUrl: '/assets/units/human/overlay_unit', manifest: MANIFEST }],
      }),
    })) as unknown as typeof fetch)

    const count = await loadRuntimeSpriteSets()
    expect(count).toBe(1)
    expect(getUnitSpriteSet('overlay_unit')).not.toBeNull()
  })

  it('a failed fetch registers nothing and does not throw', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 500 })) as unknown as typeof fetch)
    await expect(loadRuntimeSpriteSets()).resolves.toBe(0)
  })
})
```

> **happy-dom note:** images never actually decode in the test env, so do NOT assert on `getUnitFrame` returning a drawable frame here — assert on *structure* (`animations`, `frameCount`, `row`). Frame drawing is verified in the manual E2E (Task 7).

- [ ] **Step 2: Run to verify it fails**

Run: `cd client/src/game-portal && npm run test -- unitSprites.overlay`
Expected: FAIL — `buildSpriteSet` / `registerRuntimeSpriteSet` are not exported.

- [ ] **Step 3: Extract `buildSpriteSet` from the bundled loop**

In `unitSprites.ts`, replace the body of the top-level `for (const [manifestPath, manifest] of Object.entries(manifestGlob))` loop (lines ~227-297) so the manifest→set logic lives in a reusable function. The loop keeps computing `key` and `unitFolder`, then delegates:

```ts
// Builds a UnitSpriteSet from a manifest. `resolveUrl` maps a manifest-relative
// path (e.g. "packed/walking.png") to a loadable URL, which is the ONLY thing
// that differs between bundled art (resolved through Vite's stripGlob) and
// runtime art (resolved against the server's /assets/units/... base URL).
// Both callers share this so the two paths cannot drift.
export function buildSpriteSet(
  key: string,
  manifest: SpriteManifest,
  resolveUrl: (relative: string) => string | undefined,
): UnitSpriteSet | null {
  if (!key) return null
  const size = {
    width: manifest.size?.width ?? 64,
    height: manifest.size?.height ?? 64,
  }

  let rotations: DirectionMap<HTMLImageElement> = {}
  if (isPackedRotations(manifest.rotations)) {
    const sheetUrl = resolveUrl(manifest.rotations.sheet)
    if (sheetUrl) rotations = loadPackedRotations(sheetUrl, manifest.rotations)
  } else if (manifest.rotations) {
    console.warn(
      `[unitSprites] ${key}: sprites.json uses the legacy per-direction rotation shape; ` +
      `re-run \`npm run pack:sprites\` to migrate to the packed-sheet layout.`,
    )
  }

  const animations = new Map<string, StripAnimation>()
  for (const [animKey, anim] of Object.entries(manifest.animations ?? {})) {
    const directions: DirectionMap<DirectionSource> = {}

    if (anim.sheet && anim.rowOrder) {
      const url = resolveUrl(anim.sheet)
      if (url) {
        const image = loadImage(url)
        for (let row = 0; row < anim.rowOrder.length; row++) {
          const dir = anim.rowOrder[row]
          if (!dir) continue
          directions[dir] = { image, row }
        }
      }
    } else if (anim.strips) {
      for (const [dir, rel] of Object.entries(anim.strips)) {
        if (!rel) continue
        const url = resolveUrl(rel)
        if (!url) continue
        directions[dir as UnitDirection] = { image: loadImage(url), row: 0 }
      }
    }

    if (Object.keys(directions).length === 0) continue
    animations.set(animKey.toLowerCase(), {
      frameCount: anim.frameCount ?? 1,
      frameWidth: anim.frameWidth ?? size.width,
      frameHeight: anim.frameHeight ?? size.height,
      directions,
    })
  }

  return {
    key,
    size,
    rotations,
    animations,
    beamOrigin: { x: manifest.beamOrigin?.x ?? 0, y: manifest.beamOrigin?.y ?? 0 },
  }
}
```

and the bundled loop becomes:

```ts
for (const [manifestPath, manifest] of Object.entries(manifestGlob)) {
  const match = manifestPath.match(/\/([^/]+)\/sprites\.json$/)
  if (!match) continue
  const key = match[1].toLowerCase()
  const unitFolder = manifestPath.slice(0, manifestPath.lastIndexOf('/'))
  const set = buildSpriteSet(key, manifest, (rel) => stripGlob[`${unitFolder}/${rel}`])
  if (set) sprites.set(key, set)
}
```

- [ ] **Step 4: Add the runtime overlay**

Also in `unitSprites.ts`:

```ts
// Runtime sprite overlay — art served by the server from the writable art dir,
// which SHADOWS the build-time bundled art above. This is the client-side twin
// of the server's runtimeUnits-over-embedded-catalog model, and it is what lets
// newly-authored art appear without a rebuild.
const runtimeSprites = new Map<string, UnitSpriteSet>()
const runtimePortraits = new Map<string, HTMLImageElement>()

export function registerRuntimeSpriteSet(set: UnitSpriteSet): void {
  runtimeSprites.set(set.key.toLowerCase(), set)
}

export function clearRuntimeSpriteSets(): void {
  runtimeSprites.clear()
  runtimePortraits.clear()
}

interface UnitArtEntry {
  key: string
  baseUrl: string
  manifest: SpriteManifest
}

// Waits for every image in a set to finish decoding. We register a runtime set
// only AFTER this resolves: getUnitFrame returns null for an undecoded image and
// the renderer falls back to a procedural placeholder, so registering early would
// let a half-loaded overlay shadow perfectly-good bundled art and flash every
// unit as a placeholder. Never rejects — a broken sheet just stays not-ready.
function whenSetDecoded(set: UnitSpriteSet): Promise<void> {
  const images = new Set<HTMLImageElement>()
  for (const img of Object.values(set.rotations)) if (img) images.add(img)
  for (const anim of set.animations.values()) {
    for (const src of Object.values(anim.directions)) if (src) images.add(src.image)
  }
  return Promise.all(
    [...images].map((img) =>
      img.complete
        ? Promise.resolve()
        : new Promise<void>((resolve) => {
            img.onload = () => resolve()
            img.onerror = () => resolve()
          }),
    ),
  ).then(() => undefined)
}

// Fetches the server's writable art catalog and registers each entry into the
// runtime overlay. Returns how many sets were registered. Safe to call more than
// once (re-registering a key replaces it) — the editor calls it again after
// saving new art. Never throws: art is an enhancement, and a failure here must
// degrade to bundled art rather than break the app.
export async function loadRuntimeSpriteSets(): Promise<number> {
  const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''
  let entries: UnitArtEntry[]
  try {
    const res = await fetch(`${API_BASE}/catalog/unit-art`)
    if (!res.ok) return 0
    const body = (await res.json()) as { art?: UnitArtEntry[] }
    entries = body.art ?? []
  } catch {
    return 0
  }

  const built = entries
    .map((entry) => {
      const set = buildSpriteSet(
        entry.key.toLowerCase(),
        entry.manifest,
        (rel) => `${API_BASE}${entry.baseUrl}/${rel}`,
      )
      if (set) {
        const portrait = new Image()
        portrait.src = `${API_BASE}${entry.baseUrl}/portrait.png`
        runtimePortraits.set(entry.key.toLowerCase(), portrait)
      }
      return set
    })
    .filter((s): s is UnitSpriteSet => s !== null)

  await Promise.all(built.map(whenSetDecoded))
  for (const set of built) registerRuntimeSpriteSet(set)
  return built.length
}
```

> The portrait `Image` is created optimistically; a unit with no `portrait.png` gets a 404 and the image simply never becomes ready. `getUnitPortraitImage` must therefore check `imageReady`, not mere presence — see Step 5.

- [ ] **Step 5: Make the lookups overlay-aware**

`getUnitSpriteSet` — runtime wins:

```ts
export function getUnitSpriteSet(...keys: Array<string | undefined | null>): UnitSpriteSet | null {
  for (const k of keys) {
    if (!k || k === 'none') continue
    const lower = k.toLowerCase()
    const runtime = runtimeSprites.get(lower)
    if (runtime) return runtime
    const bundled = sprites.get(lower)
    if (bundled) return bundled
  }
  return null
}
```

`getUnitPortraitImage` — runtime wins, but only if the portrait actually decoded (a 404'd optimistic portrait must not shadow a real bundled one):

```ts
export function getUnitPortraitImage(path?: string, unitType?: string): HTMLImageElement | null {
  for (const k of [path, unitType]) {
    if (!k || k === 'none') continue
    const lower = k.toLowerCase()
    const runtime = runtimePortraits.get(lower)
    if (imageReady(runtime)) return runtime
    const bundled = portraitImagesByKey.get(lower)
    if (bundled) return bundled
  }
  return null
}
```

Note this moves `imageReady` above its first use, or keep it where it is — it is a function declaration and hoists. Verify with the typecheck.

- [ ] **Step 6: Run tests + typecheck**

- `cd client/src/game-portal && npm run test -- unitSprites` → PASS
- `cd client/src/game-portal && npm run test` → no NEW failures (baseline 45 files / 225+ tests)
- `cd client/src/game-portal && npx vue-tsc -b` → clean

- [ ] **Step 7: Checkpoint (do not commit)**

---

### Task 4: Client — load the overlay at boot

**Files:**
- Modify: `client/src/game-portal/src/main.ts`

- [ ] **Step 1: Wire it**

In `main.ts`, before `app.use(router).mount('#app')`:

```ts
import { loadRuntimeSpriteSets } from './game/rendering/unitSprites'

// Overlay server-served unit art on top of the bundled art. Fire-and-forget: it
// resolves after the app has mounted, and getUnitSpriteSet is called per-frame
// by the renderer, so a set registered late is picked up on the next frame with
// no invalidation needed. Never throws — a failure leaves bundled art in place.
void loadRuntimeSpriteSets()
```

- [ ] **Step 2: Verify no boot regression**

- `cd client/src/game-portal && npm run build` → clean
- `cd client/src/game-portal && npm run test` → no new failures

- [ ] **Step 3: Checkpoint (do not commit)**

---

### Task 5: Client — `UnitSpritePreview.vue` (the animation & rotation viewer)

**Files:**
- Create: `client/src/game-portal/src/components/UnitSpritePreview.vue`

**Interfaces:**
- Props: `unitKey?: string` (the unit type), `pathKey?: string` (promotion path; wins over unitKey — same precedence as the renderer).
- No emits. Self-contained; safe to mount with an unknown key (renders an empty state).

**What it must show** (spec §5.1):
- A **rotation strip**: all 8 facings side by side, drawn from `set.rotations`.
- An **animation player**: a `<select>` of the animations the manifest ACTUALLY contains, a facing selector, play/pause, a frame scrubber, and an FPS readout.
- **Honesty about fallbacks.** If the selected animation is missing from the set, say which animation it actually falls back to (`ANIMATION_FALLBACK`: `carrying_gold → walking`, `casting → attacking`). No unit currently ships a `casting.png`, so this is not hypothetical — a caster will silently play its attack swing, and the author must be told that rather than left to wonder.
- An empty state when the unit has no art at all ("No packed art for `<key>` — it renders as a placeholder").

**Implementation notes:**
- Draw on a `<canvas>` with `ctx.imageSmoothingEnabled = false` (pixel art) at `UNIT_SPRITE_SCALE`.
- Drive with `requestAnimationFrame`; cancel on unmount (`onBeforeUnmount`) — a leaked rAF loop in a modal that reopens is a real bug.
- Use `getUnitSpriteSet(pathKey, unitKey)` and `getUnitFrame(set, anim, dir, frameIndex)`. Do NOT reimplement frame math — `getUnitFrame` already owns `srcX = frame × frameWidth`, `srcY = row × frameHeight`.
- Re-resolve the sprite set when props change AND when the runtime overlay reloads (Phase 3 will re-call `loadRuntimeSpriteSets` after an art save) — expose a `refresh()` via `defineExpose` so the panel can force it.
- Frame timing: default 8 fps (125 ms/frame, matching `DEFAULT_FRAME_MS` in `unitAnimation.ts`). Do not import the combat-derived attack timing — this is a preview, not a simulation.
- **No literal `cursor:` declarations.**

- [ ] **Step 1: Build the component**

Sketch of the script (fill in the template + scoped styles in the panel's existing dark aesthetic):

```vue
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  getUnitFrame, getUnitSpriteSet, UNIT_DIRECTIONS, UNIT_SPRITE_SCALE,
  type UnitDirection, type UnitSpriteSet,
} from '@/game/rendering/unitSprites'

const props = defineProps<{ unitKey?: string; pathKey?: string }>()

const set = ref<UnitSpriteSet | null>(null)
const animation = ref('walking')
const direction = ref<UnitDirection>('south')
const frame = ref(0)
const playing = ref(true)
const fps = ref(8)

const canvas = ref<HTMLCanvasElement | null>(null)
const strip = ref<HTMLCanvasElement | null>(null)

const animationNames = computed(() => (set.value ? [...set.value.animations.keys()].sort() : []))
const frameCount = computed(() => set.value?.animations.get(animation.value)?.frameCount ?? 1)

// The manifest may not contain the selected animation; getUnitFrame silently
// falls back (carrying_gold → walking, casting → attacking) or lands on the
// idle rotation. Say so out loud rather than letting the author wonder why
// their caster is swinging a sword.
const fallbackNote = computed(() => {
  if (!set.value || !animation.value) return ''
  if (set.value.animations.has(animation.value)) return ''
  return `No "${animation.value}" sheet — falling back to the idle rotation or a substitute animation.`
})

function refresh() {
  set.value = getUnitSpriteSet(props.pathKey, props.unitKey)
  frame.value = 0
  const names = set.value ? [...set.value.animations.keys()] : []
  if (names.length && !names.includes(animation.value)) animation.value = names[0]
}
defineExpose({ refresh })
watch(() => [props.unitKey, props.pathKey], refresh, { immediate: true })

let raf = 0
let lastStep = 0
function tick(now: number) {
  raf = requestAnimationFrame(tick)
  if (playing.value && now - lastStep >= 1000 / Math.max(1, fps.value)) {
    lastStep = now
    frame.value = (frame.value + 1) % Math.max(1, frameCount.value)
  }
  draw()
}
raf = requestAnimationFrame(tick)
onBeforeUnmount(() => cancelAnimationFrame(raf))

function draw() { /* main canvas: getUnitFrame(set, animation, direction, frame) → drawImage */ }
function drawStrip() { /* rotation strip: one cell per UNIT_DIRECTIONS entry */ }
</script>
```

- [ ] **Step 2: Verify by eye**

- `cd client/src/game-portal && npx vue-tsc -b && npm run build` → clean
- Component is not yet mounted anywhere; the real check is Task 6.

- [ ] **Step 3: Checkpoint (do not commit)**

---

### Task 6: Client — wire the preview into the Unit Types panel

**Files:**
- Modify: `client/src/game-portal/src/components/UnitTypeEditorPanel.vue`

- [ ] **Step 1: Add a Preview section**

Add `<UnitSpritePreview :unit-key="form.type" />` as a new collapsible section at the TOP of the form (before Identity), open by default — the author should see the unit they selected without hunting for it.

Bind it to `form.type` (not `selectedType`) so it updates as soon as a different unit is selected. Grab a template ref and call `.refresh()` inside `selectUnit()` and after a successful `save()`, so the preview always reflects the selected unit.

Keep `openSections` behavior consistent with the existing sections. **No literal `cursor:` declarations.**

- [ ] **Step 2: Gates**

- `cd client/src/game-portal && npx vue-tsc -b && npm run build && npm run test` → clean, no new failures
- `grep -n "cursor:" src/components/UnitTypeEditorPanel.vue src/components/UnitSpritePreview.vue` → only `not-allowed` permitted

- [ ] **Step 3: Checkpoint (do not commit)**

---

### Task 7: Verification sweep + END-TO-END proof

**The unit tests cannot prove this phase works** — happy-dom never decodes an image, so "the overlay wins" and "the animation plays" are only truly verified in a browser. Do the E2E.

- [ ] **Step 1: Full gates**

- `cd server && go vet ./... && go build ./... && go test ./... -count=1` → all ok
- `cd client/src/game-portal && npm run test && npx vue-tsc -b && npm run build` → green + clean

- [ ] **Step 2: Prove the server art surface works (real HTTP)**

Start the Go server on a spare port against an ISOLATED art dir (never the source tree):
```
UNIT_ASSETS_DIR=<tmp> ./api -port 8099
```
- `GET /catalog/unit-art` on an empty dir → `{"art":[]}` (empty, not an error)
- Copy one real unit's folder (e.g. `client/src/game-portal/src/assets/units/human/archer/`) into `<tmp>/human/archer/`, re-request → the archer entry appears with a `baseUrl` and a manifest
- `GET /assets/units/human/archer/packed/walking.png` → 200, `Content-Type: image/png`
- `GET /assets/units/human/archer/../../../../etc/passwd` → 404 (traversal refused)
- `GET /assets/units/human/archer/sprites.json` → 200 `application/json`

- [ ] **Step 3: Prove the overlay actually overlays (the milestone)**

With the real dev stack (`npm run dev` + the Go server on 8080):
1. Open `/unit-type-editor`, select `archer` → **the preview plays its walking and attacking animations and shows all 8 rotations.**
2. Take a real unit's art folder, copy it into the writable art dir under a DIFFERENT unit's key (e.g. put `soldier`'s packed art at `<art dir>/human/archer/`), and reload.
3. **The archer must now render with the soldier's art — in the editor preview AND in a playtest — with no rebuild.** That is the overlay winning over the bundled art, and it is the entire point of Phase 2.
4. Remove the override, reload → archer is back to its own art.
5. Confirm no placeholder flash at boot (the decode-then-register rule in Task 3).

- [ ] **Step 4: Prove the production build still works**

`/assets` is now proxied in dev, and the Vite production build emits its own hashed `/assets/*.js|css`. Confirm they do not collide:
- `cd client/src/game-portal && npm run build` → clean
- Serve the built SPA via the Go binary (the packaged path) and confirm the app boots and its JS/CSS load. If `/assets/units/` shadowed a bundler asset path, the app would white-screen — this check is not optional.

- [ ] **Step 5: Catalog + art hygiene**

`git status` must show no stray art or catalog files created during testing. The only intended new catalog file from Phase 1 remains `catalog/units/human/faction.json`.

- [ ] **Step 6: Report**

Gates, the E2E result, and anything that had to deviate from this plan.
