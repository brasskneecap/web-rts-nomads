package game

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
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
//
// The dir is NOT required to exist yet: ListUnitArt tolerates an absent dir
// (empty catalog) and SaveUnitArt creates it on first write, so this always
// returns the path rather than erroring when nothing has been written yet.
func resolveUnitAssetsDir() (string, error) {
	if dir := os.Getenv("UNIT_ASSETS_DIR"); dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "..", "client", "src", "game-portal", "src", "assets", "units"), nil
}

// ListUnitArt enumerates every packed sprite manifest under the writable art
// dir, for both base units and promotion paths.
//
// A missing dir yields an empty list, NOT an error: the client falls back to
// its build-time bundled art, which is the correct degraded state.
func ListUnitArt() []UnitArtEntry {
	dir, err := resolveUnitAssetsDir()
	if err != nil {
		return []UnitArtEntry{}
	}
	out := make([]UnitArtEntry, 0)
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
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		// Tiebreak on BaseURL (unique by construction) so two factions
		// shipping a same-named unit (human/archer, orc/archer) get a
		// deterministic order instead of leaking directory-walk order.
		return out[i].BaseURL < out[j].BaseURL
	})
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

// unitArtContentTypes is the ALLOWLIST of servable art file types. Anything not
// in this map is not served. An allowlist, never a denylist — this handler
// reads straight off the filesystem from a URL, so the default must be "no".
var unitArtContentTypes = map[string]string{
	".png":  "image/png",
	".json": "application/json",
}

// ReadUnitArtFile reads one file from the writable art dir, given a path
// relative to that dir (as it appears after the /assets/units/ URL prefix).
//
// Refuses anything that is not an allowlisted type, and anything that escapes
// the art root. The escape check runs on the RESOLVED absolute paths, not on the
// raw string: a string check for ".." is trivially bypassed (encoded separators,
// backslashes on Windows, symlinked segments), whereas comparing the cleaned
// absolute target against the cleaned absolute root holds no matter how the
// traversal is spelled.
func ReadUnitArtFile(rel string) (data []byte, contentType string, ok bool) {
	if rel == "" || strings.ContainsRune(rel, 0) {
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
	absTarget, err := filepath.Abs(filepath.Join(absRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, "", false
	}
	// Containment: filepath.Rel yields a path starting with ".." exactly when
	// absTarget lies outside absRoot.
	relCheck, err := filepath.Rel(absRoot, absTarget)
	if err != nil ||
		relCheck == ".." ||
		strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return nil, "", false
	}
	info, err := os.Stat(absTarget)
	if err != nil || info.IsDir() {
		return nil, "", false
	}
	// The lexical containment check above catches "../" traversal, but it does
	// NOT catch a symlink planted inside the art root that points outside it
	// (filepath.Join/Abs are purely lexical; os.Stat follows symlinks). Resolve
	// the real path and re-verify containment against it before reading bytes.
	realTarget, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		return nil, "", false
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, "", false
	}
	relReal, err := filepath.Rel(realRoot, realTarget)
	if err != nil ||
		relReal == ".." ||
		strings.HasPrefix(relReal, ".."+string(filepath.Separator)) {
		return nil, "", false
	}
	raw, err := os.ReadFile(absTarget)
	if err != nil {
		return nil, "", false
	}
	return raw, ct, true
}

// unitArtSlugPattern matches an animation slug used in packed/<slug>.png.
var unitArtSlugPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// allowedUnitArtName reports whether a request file name is one the editor may
// write. Allowlist, NOT a filter: sprites.json / metadata.json / portrait.png at
// the unit root, and packed/<slug>.png. Anything else — any other directory, any
// traversal, any other extension — is refused.
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

// UnitArtFile is one file in a SaveUnitArt request. Name is a forward-slash path
// relative to the unit's art directory; ContentBase64 is its bytes.
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
// Base unit -> <dir>/<faction>/<unit>/; promotion path -> .../paths/<path>/.
// Everything is validated BEFORE any file is written, so a bad file in the set
// never leaves a half-written art dir.
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

	// relName is kept alongside abs/data so errors after this point can name
	// the file the caller sent (e.g. "packed/walking.png") without ever
	// interpolating the server's absolute art-root path into a message that
	// reaches the HTTP client.
	type decodedFile struct {
		abs     string
		relName string
		data    []byte
	}
	var out []decodedFile
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
		rel, rerr := filepath.Rel(absRoot, abs)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("file %q escapes the art root", f.Name)
		}
		out = append(out, decodedFile{abs: abs, relName: f.Name, data: raw})
	}

	for _, d := range out {
		// Belt-and-suspenders symlink re-check, mirroring ReadUnitArtFile: the
		// lexical containment check above only catches "../"-style traversal.
		// It does not catch a symlinked ancestor directory planted inside the
		// art root that would redirect this write outside it once MkdirAll
		// walks through it.
		safe, serr := writeTargetContainedInRealRoot(absRoot, d.abs)
		if serr != nil {
			return fmt.Errorf("failed to verify art file %q", d.relName)
		}
		if !safe {
			return fmt.Errorf("file %q escapes the art root", d.relName)
		}
		if err := os.MkdirAll(filepath.Dir(d.abs), 0o755); err != nil {
			return fmt.Errorf("failed to write art file %q", d.relName)
		}
		if err := os.WriteFile(d.abs, d.data, 0o644); err != nil {
			return fmt.Errorf("failed to write art file %q", d.relName)
		}
	}
	return nil
}

// writeTargetContainedInRealRoot verifies that abs, once its deepest EXISTING
// ancestor directory is resolved through any symlinks, still lands inside
// absRoot (also symlink-resolved).
//
// This exists for the write path specifically: unlike ReadUnitArtFile, the
// target file (and often several of its parent directories) does not exist
// yet, so we can't just filepath.EvalSymlinks(absTarget) the way the read
// path does — that requires the path to exist. Instead we walk up from the
// target to the nearest directory that DOES exist, resolve that through any
// symlinks, and check containment there: if a symlink is going to redirect
// the write outside the root, it must be planted at or above that first
// existing ancestor, since everything below it is about to be created fresh
// by MkdirAll.
func writeTargetContainedInRealRoot(absRoot, abs string) (bool, error) {
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			// The art root itself doesn't exist yet (first-ever write under
			// it): there is no existing symlink anywhere to distrust.
			return true, nil
		}
		return false, err
	}

	dir := filepath.Dir(abs)
	for {
		if _, statErr := os.Lstat(dir); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return false, statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Walked to the filesystem root without finding anything that
			// exists; nothing planted, nothing to distrust.
			return true, nil
		}
		dir = parent
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(realRoot, realDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}
