# Ability Icon Picker + Upload Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Abilities editor an icon gallery (pick from bundled ability icons) plus a custom PNG upload served at runtime, mirroring the item-editor icon flow, and make the game honor the chosen icon.

**Architecture:** `AbilityDef.Icon` (currently dead placeholder data) becomes a resolution *key* like `ItemDef.IconKey`. Server gains `SaveAbilityIcon`/`ReadAbilityIcon` (an `ABILITY_CATALOG_DIR/_icons` store) + `POST /abilities/{id}/image` + `GET /catalog/abilities/{id}/image`, mirroring the item icon endpoints. Client `abilityAssets.ts` gains a bundled→server keyed resolver; `ActionIcon.vue` resolves ability icons by the key (from the already-on-wire `AbilitySnapshot.icon`) then falls back to ability-id; and `AbilityEditorPanel.vue` gets the item-style gallery + upload.

**Tech Stack:** Go (`internal/game`, `internal/http`), TypeScript / Vue 3 (`game-portal`), Vitest, `go test`.

## Global Constraints

- Branch: `ability-icon-editor` (off `main`, base `1324461`).
- The icon is pure presentation; the client stays a view; server-authoritative sim unchanged. No `game`→`profile` write.
- Icon storage is the `ABILITY_CATALOG_DIR` overlay `_icons` subdir (authoring-time). Ability id regex `^[a-z0-9_]+$` (`abilityIDPattern`) is the path-traversal gate on every id entry point (save-icon, read-icon).
- Icon size cap **256 * 1024 bytes**; uploads must be valid PNGs (`png.DecodeConfig`). Raw PNG bytes in the request body (NOT multipart/FormData) — matches the item endpoint.
- `AbilityDef.Icon` semantics: empty ⇒ resolve by ability id (unchanged default); a bundled ability-icon folder name ⇒ that bundled icon; the ability id after an upload ⇒ the uploaded custom icon. `SaveAbilityIcon` forces `def.Icon = id`.
- The keyed resolver only treats an icon key that matches `^[a-z0-9_]+$` as a real key — placeholder paths like `"TODO/abilities/fireball.png"` (shipped in the catalog today) are ignored so they never trigger a spurious server fetch; those abilities fall back to resolve-by-id (their real bundled art). Behavior for every shipped ability is unchanged.
- No literal `cursor:` in new/changed component CSS except `cursor: not-allowed` on forbidden states.
- Build gates: server `go build ./...` + `go vet ./...` + `go test ./...` (NOT gofmt — CRLF repo). Client `npm run build` (`vue-tsc -b`) + `npm run test`, run from `client/src/game-portal`.
- Per-task commits with explicit `git add <files>` (NEVER `git add -A`/`.` — the spec/plan docs and other files may be untracked and must not be swept in). Do not push.

## File Structure

**Server (package `game` / `httpserver`):**
- `server/internal/game/ability_persistence.go` — MODIFY: `SaveAbilityIcon`, `ReadAbilityIcon`, `_icons` const, delete cleanup, walk-skip.
- `server/internal/http/editor_handlers.go` — MODIFY: `POST /abilities/{id}/image` branch in the `/abilities/` handler.
- `server/internal/http/router.go` — MODIFY: `GET /catalog/abilities/{id}/image` in `registerAbilityCatalogRoutes`.

**Client (`client/src/game-portal/src`):**
- `game/rendering/abilityAssets.ts` — MODIFY: URL map, keyed server-fallback resolver, `listAbilityIconKeys`, `getAbilityIconSourceUrl`.
- `game/core/GameState.ts` — MODIFY: ability `iconDef` type gains `iconKey?`; the 4 construction sites pass `iconKey: <snapshot>.icon`.
- `components/ActionIcon.vue` — MODIFY: resolve ability icon by key first.
- `game/abilities/abilityEditorApi.ts` — MODIFY: `uploadAbilityIcon`, `abilityIconUrl`.
- `components/AbilityEditorPanel.vue` — MODIFY: replace the `icon` text input with the gallery + upload section.

---

## Task 1: Server — `SaveAbilityIcon` / `ReadAbilityIcon` + delete cleanup

**Files:**
- Modify: `server/internal/game/ability_persistence.go`
- Test: `server/internal/game/ability_persistence_test.go` (append)

**Interfaces:**
- Consumes: `abilityIDPattern`, `getAbilityDef`, `SaveAbilityDef`, `resolveAbilitiesDir`, `DeleteAbilityOverride` (all exist).
- Produces: `func SaveAbilityIcon(id string, data []byte) error`, `func ReadAbilityIcon(id string) ([]byte, bool)`, `const abilityIconsSubdirName = "_icons"`.

- [ ] **Step 1: Write the failing test**

Append to `server/internal/game/ability_persistence_test.go`:

```go
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestSaveAndReadAbilityIcon(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	if err := SaveAbilityDef(&AbilityDef{ID: "icon_bolt", DamageAmount: 5}); err != nil {
		t.Fatalf("SaveAbilityDef: %v", err)
	}
	png := tinyPNG(t)
	if err := SaveAbilityIcon("icon_bolt", png); err != nil {
		t.Fatalf("SaveAbilityIcon: %v", err)
	}
	// def.Icon forced to the id.
	if def, ok := getAbilityDef("icon_bolt"); !ok || def.Icon != "icon_bolt" {
		t.Fatalf("Icon not forced to id: ok=%v icon=%q", ok, def.Icon)
	}
	got, ok := ReadAbilityIcon("icon_bolt")
	if !ok || len(got) != len(png) {
		t.Fatalf("ReadAbilityIcon ok=%v len=%d want %d", ok, len(got), len(png))
	}
	// Delete removes the icon too.
	if _, err := DeleteAbilityOverride("icon_bolt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := ReadAbilityIcon("icon_bolt"); ok {
		t.Fatal("icon still present after delete")
	}
}

func TestSaveAbilityIconRejects(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	// def must exist first
	if err := SaveAbilityIcon("nope", tinyPNG(t)); err == nil {
		t.Fatal("expected rejection when def missing")
	}
	_ = SaveAbilityDef(&AbilityDef{ID: "x1"})
	// not a PNG
	if err := SaveAbilityIcon("x1", []byte("not a png")); err == nil {
		t.Fatal("expected rejection for non-PNG")
	}
	// oversize
	if err := SaveAbilityIcon("x1", make([]byte, maxAbilityIconBytes+1)); err == nil {
		t.Fatal("expected rejection for oversize")
	}
	// bad id (path traversal)
	if err := SaveAbilityIcon("../x", tinyPNG(t)); err == nil {
		t.Fatal("expected rejection for bad id")
	}
}
```

Ensure the test file imports `"bytes"`, `"image"`, `"image/png"`, `"testing"` (add any missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/game/ -run 'TestSaveAndReadAbilityIcon|TestSaveAbilityIconRejects'`
Expected: FAIL — `SaveAbilityIcon`/`ReadAbilityIcon`/`maxAbilityIconBytes` undefined.

- [ ] **Step 3: Implement in `ability_persistence.go`**

Add the `_icons` const near `abilityIDPattern`, and the size cap + functions (mirrors `item_persistence.go`). Ensure the file imports `"bytes"` and `"image/png"` (add to the import block):

```go
// maxAbilityIconBytes caps uploaded icon size (ability icons are small sprites).
const maxAbilityIconBytes = 256 * 1024

// abilityIconsSubdirName holds uploaded ability icons; skipped by the def walk.
const abilityIconsSubdirName = "_icons"

// SaveAbilityIcon validates and stores an uploaded PNG for the ability, and
// forces the ability's Icon key to its id so the client's server-URL fallback
// resolves unambiguously.
func SaveAbilityIcon(id string, data []byte) error {
	if !abilityIDPattern.MatchString(id) {
		return fmt.Errorf("ability id %q must match %s", id, abilityIDPattern)
	}
	def, ok := getAbilityDef(id)
	if !ok {
		return fmt.Errorf("ability %q not found", id)
	}
	if len(data) > maxAbilityIconBytes {
		return fmt.Errorf("icon exceeds %d bytes", maxAbilityIconBytes)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("icon is not a valid PNG: %w", err)
	}
	dir, err := resolveAbilitiesDir()
	if err != nil {
		return err
	}
	iconDir := filepath.Join(dir, abilityIconsSubdirName)
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(iconDir, id+".png"), data, 0o644); err != nil {
		return err
	}
	if def.Icon != id {
		updated := def
		updated.Icon = id
		return SaveAbilityDef(&updated)
	}
	return nil
}

// ReadAbilityIcon returns the uploaded PNG for id, if any.
func ReadAbilityIcon(id string) ([]byte, bool) {
	if !abilityIDPattern.MatchString(id) {
		return nil, false // also blocks path traversal
	}
	dir, err := resolveAbilitiesDir()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, abilityIconsSubdirName, id+".png"))
	if err != nil {
		return nil, false
	}
	return data, true
}
```

NOTE: `getAbilityDef` returns `(AbilityDef, bool)` by value (not a pointer), so `updated := def` copies it — correct here.

- [ ] **Step 4: Add delete cleanup + walk-skip**

In `DeleteAbilityOverride`, after removing the def file(s) and before/with the overlay-map deletion, also remove the icon (mirror the item cleanup). Add this line where the function removes the def file(s):

```go
	// Remove the uploaded icon too, if any.
	_ = os.Remove(filepath.Join(dir, abilityIconsSubdirName, id+".png"))
```

(`dir` is the resolved abilities dir already available in `DeleteAbilityOverride`; if the function doesn't currently resolve `dir`, add `dir, _ := resolveAbilitiesDir()` guarded like the existing code — match the existing structure of the function.)

In `loadPersistedAbilitiesFromDir` (the startup walk), skip the `_icons` subdir so an icon PNG is never parsed as a def JSON. In the `WalkDir` callback, add near the top (mirror how `unit_persistence.go` skips its subdir):

```go
		if d.IsDir() && d.Name() == abilityIconsSubdirName {
			return filepath.SkipDir
		}
```

- [ ] **Step 5: Run tests + build**

Run: `cd server && go test ./internal/game/ -run 'Ability' && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/game/ability_persistence.go server/internal/game/ability_persistence_test.go
git commit -m "feat(ability-icons): SaveAbilityIcon/ReadAbilityIcon overlay store + delete cleanup"
```

---

## Task 2: Server — icon HTTP endpoints

**Files:**
- Modify: `server/internal/http/editor_handlers.go` (the `/abilities/` handler — add a POST-image branch)
- Modify: `server/internal/http/router.go` (`registerAbilityCatalogRoutes` — add `GET /catalog/abilities/{id}/image`)
- Test: `server/internal/http/editor_handlers_ability_icon_test.go`

**Interfaces:**
- Consumes: `game.SaveAbilityIcon`, `game.ReadAbilityIcon`, `game.SaveEditorAbility` (for the test to create a def), `writeJSONError`, `writeJSON`.

- [ ] **Step 1: Write the failing test**

Create `server/internal/http/editor_handlers_ability_icon_test.go`:

```go
package httpserver

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func abIconPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestAbilityIconUploadThenServe(t *testing.T) {
	t.Setenv("ABILITY_CATALOG_DIR", t.TempDir())
	mux := http.NewServeMux()
	registerEditorRoutes(mux)
	registerAbilityCatalogRoutes(mux)

	// create the ability def first
	save := httptest.NewRequest(http.MethodPost, "/abilities", strings.NewReader(`{"ability":{"id":"pic_bolt","damageAmount":3}}`))
	srec := httptest.NewRecorder()
	mux.ServeHTTP(srec, save)
	if srec.Code != http.StatusCreated {
		t.Fatalf("save def: %d %s", srec.Code, srec.Body.String())
	}

	// upload the icon (raw PNG body)
	up := httptest.NewRequest(http.MethodPost, "/abilities/pic_bolt/image", bytes.NewReader(abIconPNG(t)))
	urec := httptest.NewRecorder()
	mux.ServeHTTP(urec, up)
	if urec.Code != http.StatusCreated || !strings.Contains(urec.Body.String(), "icon_saved") {
		t.Fatalf("upload: %d %s", urec.Code, urec.Body.String())
	}

	// serve it back
	get := httptest.NewRequest(http.MethodGet, "/catalog/abilities/pic_bolt/image", nil)
	grec := httptest.NewRecorder()
	mux.ServeHTTP(grec, get)
	if grec.Code != http.StatusOK || grec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("serve: %d ct=%q", grec.Code, grec.Header().Get("Content-Type"))
	}

	// 404 for an unknown icon
	miss := httptest.NewRequest(http.MethodGet, "/catalog/abilities/unknown_x/image", nil)
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, miss)
	if mrec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing icon, got %d", mrec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/http/ -run TestAbilityIconUploadThenServe`
Expected: FAIL — the POST `/abilities/{id}/image` and GET `/catalog/abilities/{id}/image` don't exist yet (upload likely 405, serve 404-but-for-wrong-reason).

- [ ] **Step 3: Add the POST-image branch**

In `server/internal/http/editor_handlers.go`, find the `mux.HandleFunc("/abilities/", ...)` handler (currently DELETE-only). At the TOP of that handler, right after `id := strings.TrimPrefix(r.URL.Path, "/abilities/")`, add the image branch (mirrors the `/items/` image branch):

```go
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
```

Confirm `io` is imported in this file (the `/items/` image branch uses it — it is). The existing DELETE logic below is unchanged.

- [ ] **Step 4: Add the GET serve route**

In `server/internal/http/router.go`, inside `registerAbilityCatalogRoutes(mux)`, add (mirrors `/catalog/items/`):

```go
	mux.HandleFunc("/catalog/abilities/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/catalog/abilities/")
		id, suffix, ok := strings.Cut(rest, "/")
		if !ok || suffix != "image" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		data, found := game.ReadAbilityIcon(id)
		if !found {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	})
```

Confirm `strings` and `net/http` are imported in `router.go` (they are — used by the existing `/catalog/items/` handler). NOTE: `/catalog/abilities` (exact, the list route) and `/catalog/abilities/` (subtree, this image route) are distinct ServeMux patterns and do not collide.

- [ ] **Step 5: Run test + build**

Run: `cd server && go test ./internal/http/ -run TestAbilityIconUploadThenServe && go build ./... && go vet ./...`
Expected: PASS + clean.

- [ ] **Step 6: Commit**

```bash
git add server/internal/http/editor_handlers.go server/internal/http/router.go server/internal/http/editor_handlers_ability_icon_test.go
git commit -m "feat(ability-icons): POST /abilities/{id}/image + GET /catalog/abilities/{id}/image"
```

---

## Task 3: Client — keyed resolver + gallery list (`abilityAssets.ts`)

**Files:**
- Modify: `client/src/game-portal/src/game/rendering/abilityAssets.ts`
- Test: `client/src/game-portal/src/game/rendering/abilityAssets.test.ts` (create or append)

**Interfaces:**
- Produces: `getAbilityIconImageByKey(iconKey?: string): HTMLImageElement | null`, `resolveAbilityIconImageKeyed(iconKey: string | undefined, abilityId: string, projectileId?: string): HTMLImageElement | null`, `listAbilityIconKeys(): string[]`, `getAbilityIconSourceUrl(iconKey: string): string`.
- Consumes: existing `abilityImages`, `getAbilityAssetImage`, `getProjectileAssetImage`.

- [ ] **Step 1: Write the failing test**

Create/append `client/src/game-portal/src/game/rendering/abilityAssets.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import {
  listAbilityIconKeys,
  getAbilityIconImageByKey,
  getAbilityIconSourceUrl,
} from './abilityAssets'

describe('abilityAssets icon keys', () => {
  it('listAbilityIconKeys returns bundled ability folder names, sorted', () => {
    const keys = listAbilityIconKeys()
    expect(keys).toEqual([...keys].sort())
    // fireball ships as a bundled ability icon folder
    expect(keys).toContain('fireball')
  })

  it('ignores a non-id-pattern key (placeholder path) rather than fetching it', () => {
    // A placeholder path must NOT resolve as a key (no bundled, no server fetch).
    expect(getAbilityIconImageByKey('TODO/abilities/fireball.png')).toBeNull()
    expect(getAbilityIconImageByKey(undefined)).toBeNull()
    expect(getAbilityIconImageByKey('')).toBeNull()
  })

  it('resolves a bundled key to its image', () => {
    expect(getAbilityIconImageByKey('fireball')).not.toBeNull()
  })

  it('getAbilityIconSourceUrl returns the server route for an unbundled key', () => {
    expect(getAbilityIconSourceUrl('uploaded_only')).toContain('/catalog/abilities/uploaded_only/image')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/game/rendering/abilityAssets.test.ts`
Expected: FAIL — new exports undefined.

- [ ] **Step 3: Implement**

In `abilityAssets.ts`, add a URL map alongside `abilityImages` (in the existing glob loop that populates `abilityImages`, also capture the url):

```ts
const abilityUrlsByKey = new Map<string, string>()
```

and inside the `for (const [path, url] of Object.entries(abilityGlob))` loop, after `abilityImages.set(...)`, add:

```ts
  abilityUrlsByKey.set(match[1].toLowerCase(), url)
```

Then append the keyed resolver + gallery helpers at the end of the file:

```ts
const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''
const ABILITY_ICON_KEY_RE = /^[a-z0-9_]+$/

// Server-served (editor-uploaded) ability icons, resolved lazily by key.
const serverAbilityIconCache = new Map<string, HTMLImageElement>()
const serverAbilityIconFailed = new Set<string>()

function getServerAbilityIcon(key: string): HTMLImageElement | null {
  if (serverAbilityIconFailed.has(key)) return null
  const cached = serverAbilityIconCache.get(key)
  if (cached) return cached
  const img = new Image()
  img.addEventListener('error', () => {
    serverAbilityIconFailed.add(key)
    serverAbilityIconCache.delete(key)
  })
  img.src = `${API_BASE}/catalog/abilities/${encodeURIComponent(key)}/image`
  serverAbilityIconCache.set(key, img)
  return img
}

// getAbilityIconImageByKey resolves a chosen icon key: bundled-by-key first,
// else the server-served uploaded icon. Only a key matching the ability-id
// pattern is treated as a real key — placeholder paths (e.g. "TODO/x.png")
// return null so they never trigger a spurious server fetch.
export function getAbilityIconImageByKey(iconKey?: string): HTMLImageElement | null {
  if (!iconKey) return null
  const key = iconKey.toLowerCase()
  if (!ABILITY_ICON_KEY_RE.test(key)) return null
  const bundled = abilityImages.get(key)
  if (bundled) return bundled
  return getServerAbilityIcon(key)
}

// resolveAbilityIconImageKeyed applies the full action-bar resolution order:
// chosen key (bundled-by-key → server) → bundled-by-ability-id → projectile.
export function resolveAbilityIconImageKeyed(
  iconKey: string | undefined,
  abilityId: string,
  projectileId?: string,
): HTMLImageElement | null {
  return (
    getAbilityIconImageByKey(iconKey) ??
    getAbilityAssetImage(abilityId) ??
    (projectileId ? getProjectileAssetImage(projectileId) : null)
  )
}

// listAbilityIconKeys returns the bundled ability-icon keys (folder names),
// sorted — for the editor gallery.
export function listAbilityIconKeys(): string[] {
  return [...abilityImages.keys()].sort()
}

// getAbilityIconSourceUrl resolves a key to an <img>/canvas source URL: the
// bundled url when present, else the server-served route. For the editor.
export function getAbilityIconSourceUrl(iconKey: string): string {
  const key = iconKey.toLowerCase()
  const bundled = abilityUrlsByKey.get(key)
  if (bundled) return bundled
  return `${API_BASE}/catalog/abilities/${encodeURIComponent(key)}/image`
}
```

- [ ] **Step 4: Run test + build**

Run: `cd client/src/game-portal && npx vitest run src/game/rendering/abilityAssets.test.ts && npm run build`
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/rendering/abilityAssets.ts client/src/game-portal/src/game/rendering/abilityAssets.test.ts
git commit -m "feat(ability-icons): keyed bundled→server resolver + gallery key list"
```

---

## Task 4: Client — render path honors the icon key

**Files:**
- Modify: `client/src/game-portal/src/game/core/GameState.ts` (ability `iconDef` type + 4 construction sites)
- Modify: `client/src/game-portal/src/components/ActionIcon.vue`

**Interfaces:**
- Consumes: `resolveAbilityIconImageKeyed`, `getAbilityIconImageByKey` (Task 3); `AbilitySnapshot.icon` (already exists, `protocol.ts:1002`).

- [ ] **Step 1: Widen the ability `iconDef` type**

In `GameState.ts` line ~257, the ability iconDef variant is:

```ts
    | { kind: 'ability'; type: string; projectile?: string }
```

Change it to:

```ts
    | { kind: 'ability'; type: string; projectile?: string; iconKey?: string }
```

- [ ] **Step 2: Thread the key at every construction site**

In `GameState.ts`, every ability `iconDef` is built as
`iconDef: { kind: 'ability' as const, type: <x>.id, projectile: <x>.projectile }`.
There are 4 such sites (around lines 3800, 3832, 3897, 4157). At EACH, add `iconKey: <x>.icon` using the SAME snapshot variable that supplies `.id`/`.projectile` at that site (it's `a` at three sites and `ability` at one). Example (site at ~3800, snapshot var `a`):

```ts
        iconDef: { kind: 'ability' as const, type: a.id, projectile: a.projectile, iconKey: a.icon },
```

Use Grep for `iconDef: { kind: 'ability' as const` to find all four; update each, matching the local snapshot variable name. (`AbilitySnapshot.icon` is already a typed optional field, so `a.icon` / `ability.icon` type-checks.)

- [ ] **Step 3: Resolve by key in `ActionIcon.vue`**

Update the import (line 19) to add the keyed helpers:

```ts
import { getAbilityAssetImage, getProjectileAssetImage, getAbilityIconImageByKey, resolveAbilityIconImageKeyed } from '@/game/rendering/abilityAssets'
```

(Drop `resolveAbilityIconImage` from the import if it is no longer referenced after this change — `vue-tsc -b` fails on unused imports.)

In `useCanvas` (the `iconDef?.kind === 'ability'` branch, ~line 171), change:

```ts
    return !!resolveAbilityIconImageKeyed(iconDef.iconKey, iconDef.type, iconDef.projectile) || !!getActionIconImage(lookupId)
```

In `draw()` (the `props.action.iconDef?.kind === 'ability'` branch, ~line 191), change the destructure + the first image lookup so the chosen key wins:

```ts
    const { type, projectile, iconKey } = props.action.iconDef
    const abilityImg = getAbilityIconImageByKey(iconKey) ?? getAbilityAssetImage(type)
```

Leave the rest of that branch (the `abilityImg` load/draw logic and the projectile fallback) unchanged — `drawActionSpriteFirstFrame` already handles both bundled strips and single-frame uploaded PNGs (frames === 1 draws whole).

- [ ] **Step 4: Build gate**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean (watch for the dropped-import unused-local check), full suite green.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/core/GameState.ts client/src/game-portal/src/components/ActionIcon.vue
git commit -m "feat(ability-icons): action bar resolves ability icon by chosen key"
```

---

## Task 5: Client — editor API upload

**Files:**
- Modify: `client/src/game-portal/src/game/abilities/abilityEditorApi.ts`
- Test: `client/src/game-portal/src/game/abilities/abilityEditorApi.test.ts` (append)

**Interfaces:**
- Produces: `uploadAbilityIcon(id: string, file: Blob): Promise<void>`, `abilityIconUrl(id: string): string`.

- [ ] **Step 1: Write the failing test**

Append to `abilityEditorApi.test.ts`:

```ts
import { uploadAbilityIcon, abilityIconUrl } from './abilityEditorApi'

describe('ability icon upload', () => {
  it('POSTs the raw blob to /abilities/{id}/image', async () => {
    const calls: { url: string; init?: RequestInit }[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url: String(url), init })
      return { ok: true, status: 201, json: async () => ({ status: 'icon_saved' }) }
    }) as unknown as typeof fetch)
    const blob = new Blob([new Uint8Array([1, 2, 3])], { type: 'image/png' })
    await uploadAbilityIcon('pic_bolt', blob)
    expect(calls[0].url).toContain('/abilities/pic_bolt/image')
    expect(calls[0].init?.method).toBe('POST')
    vi.restoreAllMocks()
  })

  it('abilityIconUrl points at the serve route', () => {
    expect(abilityIconUrl('pic_bolt')).toContain('/catalog/abilities/pic_bolt/image')
  })
})
```

(Ensure `describe/it/expect/vi` are imported at the top of the file — they already are for the existing tests.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/game/abilities/abilityEditorApi.test.ts`
Expected: FAIL — exports undefined.

- [ ] **Step 3: Implement**

Append to `abilityEditorApi.ts` (mirrors `uploadItemIcon`/`itemIconUrl`):

```ts
// uploadAbilityIcon posts a raw PNG blob for the ability; the server stores it
// and forces the ability's Icon key to its id. Save the ability def first.
export async function uploadAbilityIcon(id: string, file: Blob): Promise<void> {
  const res = await fetch(`${API_BASE}/abilities/${encodeURIComponent(id)}/image`, {
    method: 'POST',
    headers: { 'Content-Type': 'image/png' },
    body: file,
  })
  if (!res.ok) throw new Error(`Failed to upload ability icon: ${res.status}`)
}

export function abilityIconUrl(id: string): string {
  return `${API_BASE}/catalog/abilities/${encodeURIComponent(id)}/image`
}
```

(`API_BASE` is already defined at the top of this module.)

- [ ] **Step 4: Run test + build**

Run: `cd client/src/game-portal && npx vitest run src/game/abilities/abilityEditorApi.test.ts && npm run build`
Expected: PASS + clean.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/game/abilities/abilityEditorApi.ts client/src/game-portal/src/game/abilities/abilityEditorApi.test.ts
git commit -m "feat(ability-icons): client uploadAbilityIcon + abilityIconUrl"
```

---

## Task 6: Client — editor panel icon section (gallery + upload)

**Files:**
- Modify: `client/src/game-portal/src/components/AbilityEditorPanel.vue`
- Test: `client/src/game-portal/src/components/AbilityEditorPanel.test.ts` (append)
- Reference (read first): `client/src/game-portal/src/components/ItemEditorPanel.vue` (its icon section: preview `<img>`, "Choose from gallery", file input, gallery overlay, `onIconFileChosen`, `pickGalleryIcon`).

**Interfaces:**
- Consumes: `listAbilityIconKeys`, `getAbilityIconSourceUrl` (Task 3); `uploadAbilityIcon` (Task 5); `drawActionSpriteFirstFrame` behavior (render first frame of a sprite strip).

**Panel structure (build to this):**
- Replace the current plain `icon` text input (`AbilityEditorPanel.vue:116`, `<label>Icon <input v-model="form.icon" /></label>`) with an icon section modeled on `ItemEditorPanel`'s:
  - **Preview:** a small `<canvas>` that draws the FIRST frame of the icon at `getAbilityIconSourceUrl(form.icon || form.id)`. Because bundled ability icons are horizontal multi-frame sprite strips, use a first-frame draw (not `<img>`). Implement a small local helper that loads the URL into an `Image` and draws its first frame to the canvas: infer frame count the same way the game does — reuse `inferProjectileFrameCount(img.naturalWidth, img.naturalHeight)` from `@/game/rendering/projectileSprites`, source width = `naturalWidth / frames`, draw `drawImage(img, 0, 0, sw, sh, 0, 0, canvas.width, canvas.height)`. Redraw on image load and whenever `form.icon`/`form.id` changes.
  - **"Choose from gallery"** button → toggles a gallery overlay: a grid over `listAbilityIconKeys()`, each cell a small canvas rendering that key's first frame (same helper, source `getAbilityIconSourceUrl(key)`), click → `form.icon = key`, close overlay.
  - **File upload:** `<input type="file" accept="image/png" @change="onIconFileChosen">`. `onIconFileChosen`: if the ability hasn't been saved yet (it's a brand-new blank form whose id isn't persisted), show an inline message "Save the ability before uploading an icon" and return (mirror the item guard); else `await uploadAbilityIcon(form.id, file)`, set `form.icon = form.id`, and force the preview to re-resolve (bump a reactive `iconCacheBust` ref used in the preview `key`/redraw, since the server URL is unchanged but its bytes changed).
  - No literal `cursor:` declarations. Reuse the panel's existing class idioms.
- Keep everything else in the panel unchanged. `form.icon` is already a modeled field (Task 7 of the abilities editor), so gallery-pick and upload both just set `form.icon`, and it saves via the existing `saveRequestFromForm`.

- [ ] **Step 1: Write the failing test**

Append to `AbilityEditorPanel.test.ts` (the existing test already stubs the on-mount catalog fetches — reuse that stub helper; if it's local to the existing test, factor it so this test can call it, or duplicate the stub):

```ts
it('lists bundled ability icons in the gallery and picks one', async () => {
  stubCatalogFetch() // the existing on-mount fetch stub used by the mount test
  const wrapper = mount(AbilityEditorPanel)
  await flushPromises()
  // open an ability to edit, then open the gallery
  // (select the first listed ability, then click "Choose from gallery")
  await wrapper.find('[data-test="ability-row"]').trigger('click')
  await wrapper.find('[data-test="icon-gallery-open"]').trigger('click')
  await flushPromises()
  const cells = wrapper.findAll('[data-test="icon-gallery-cell"]')
  expect(cells.length).toBeGreaterThan(0)
})
```

Add the `data-test` attributes (`ability-row` on the list rows if not already present, `icon-gallery-open` on the gallery button, `icon-gallery-cell` on each gallery cell) as you build the section so the test can target them. If the existing mount test's `stubCatalogFetch` isn't exported/shared, lift it to a shared helper at the top of the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd client/src/game-portal && npx vitest run src/components/AbilityEditorPanel.test.ts`
Expected: FAIL — no gallery button/cells yet.

- [ ] **Step 3: Read the reference, then build the section**

Read `ItemEditorPanel.vue`'s icon section in full and mirror its shape (preview + gallery overlay + file input + handlers), adapted per "Panel structure" above (canvas first-frame preview instead of `<img>`; no group chips — abilities have no groups). Wire `onIconFileChosen`, `pickGalleryIcon(key)`, and the preview redraw.

- [ ] **Step 4: Run test + full client gates**

Run: `cd client/src/game-portal && npx vitest run src/components/AbilityEditorPanel.test.ts && npm run build && npm run test`
Expected: PASS, build clean, full suite green.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/components/AbilityEditorPanel.vue client/src/game-portal/src/components/AbilityEditorPanel.test.ts
git commit -m "feat(ability-icons): editor icon gallery + upload section"
```

---

## Task 7: Final verification

**Files:** none (verification only).

- [ ] **Step 1: Full server suite**

Run: `cd server && go build ./... && go vet ./... && go test ./...`
Expected: builds, vet clean, all tests pass. (Two repo-wide pre-existing failures may exist — `internal/ws TestSPBaseline_StructuralShape`, `cmd/api TestServerReadyLineAndStdinShutdown` — treat as expected if they fail identically on `main`; `internal/game` + `internal/http` MUST pass.)

- [ ] **Step 2: Full client suite**

Run: `cd client/src/game-portal && npm run build && npm run test`
Expected: build clean, all tests pass.

- [ ] **Step 3: Manual E2E (hard gate — requires a running server)**

1. World editor → **Abilities** → edit an ability → **Choose from gallery** → pick `fireball` → Save → **Play** → confirm the ability's action-bar icon is fireball's art.
2. Edit an ability → **Upload** a custom PNG → confirm the preview updates → Save → **Play** → confirm the custom icon renders on the action bar.
3. Upload before saving a brand-new ability → confirm the inline "save first" guard.
4. Delete/Reset an ability with an uploaded icon → confirm it reverts and the icon is gone.
5. Confirm a shipped ability (e.g. fireball/meteor) still shows its normal icon (placeholder `icon` path → resolve-by-id, unchanged).

- [ ] **Step 4: Confirm clean tree**

Run: `git status` (only intended files committed; spec/plan docs may remain untracked) and `git log --oneline` for the task commits.

---

## Self-Review Notes (for the executor)

- **Spec coverage:** §1 (Icon as key) → Task 4 + the resolver guard in Task 3; §2 (persistence) → Task 1; §3 (endpoints) → Task 2; §4 (client resolver + gallery list) → Task 3; §5 (render honors key) → Task 4; §6 (api + panel) → Tasks 5-6; testing → per-task + Task 7.
- **Type consistency:** the ability `iconDef` gains `iconKey?: string` (Task 4 step 1) and is populated from `AbilitySnapshot.icon` (already typed, `protocol.ts:1002`); `ActionIcon.vue` reads `iconDef.iconKey` (Task 4 step 3). Server `SaveAbilityIcon` forces `def.Icon = id` (Task 1) which is what the upload path relies on client-side (`form.icon = form.id`, Task 6). Client fetch keys (`/abilities/{id}/image`, `/catalog/abilities/{id}/image`) match the server routes (Task 2).
- **Watch item:** Task 4 drops `resolveAbilityIconImage` from the ActionIcon import if unused — `vue-tsc -b` (`noUnusedLocals`) will fail otherwise; the executor must check.
- **Placeholder-path safety:** the resolver's `^[a-z0-9_]+$` guard (Task 3) means shipped abilities' placeholder `icon` paths never fetch/​resolve as keys — they fall back to resolve-by-id, so no behavior change and no 404 spam.
