package game

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestReadUnitArtFile_ServesPNGAndJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	writeArtFixture(t, dir, "human/archer/sprites.json", `{"key":"archer"}`)
	writeArtFixture(t, dir, "human/archer/packed/walking.png", "\x89PNG\r\n\x1a\n fake")

	if _, ct, ok := ReadUnitArtFile("human/archer/sprites.json"); !ok || ct != "application/json" {
		t.Fatalf("sprites.json: ok=%v ct=%q", ok, ct)
	}
	data, ct, ok := ReadUnitArtFile("human/archer/packed/walking.png")
	if !ok || ct != "image/png" {
		t.Fatalf("walking.png: ok=%v ct=%q", ok, ct)
	}
	if len(data) == 0 {
		t.Fatal("walking.png served zero bytes")
	}
}

// THE security test. Every one of these must be REFUSED. If any is served,
// this handler is a filesystem read primitive for anyone who can reach the port.
func TestReadUnitArtFile_RejectsTraversalAndOtherTypes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	writeArtFixture(t, dir, "human/archer/sprites.json", `{"key":"archer"}`)

	// A secret sitting just outside the art root — the classic traversal target.
	// Note it ends in .json, so an extension-only check would happily serve it.
	secret := filepath.Join(dir, "..", "secret.json")
	if err := os.WriteFile(secret, []byte(`{"password":"hunter2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	for _, bad := range []string{
		"../secret.json",
		"../../secret.json",
		"human/../../secret.json",
		"human/archer/../../../secret.json",
		"./../secret.json",
		"human/archer/notes.txt",  // wrong extension
		"human/archer/run.exe",    // wrong extension
		"human/archer",            // a directory
		"human",                   // a directory
		"",                        // empty
	} {
		if _, _, ok := ReadUnitArtFile(bad); ok {
			t.Fatalf("ReadUnitArtFile(%q) was SERVED — it must be refused", bad)
		}
	}
}

// Windows-style separators must not be a bypass either.
func TestReadUnitArtFile_RejectsBackslashTraversal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)
	secret := filepath.Join(dir, "..", "secret2.json")
	if err := os.WriteFile(secret, []byte(`{"password":"hunter2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	for _, bad := range []string{
		`..\secret2.json`,
		`human\..\..\secret2.json`,
	} {
		if _, _, ok := ReadUnitArtFile(bad); ok {
			t.Fatalf("ReadUnitArtFile(%q) was SERVED — backslash traversal must be refused", bad)
		}
	}
}

// A symlink planted inside the art root that points outside it must not be
// followed. The lexical "../" check alone does not catch this — Join/Abs
// never touch the filesystem — so this proves the EvalSymlinks re-check.
// Creating a symlink requires elevated privilege on Windows without Developer
// Mode enabled, so this test skips (not fails) when os.Symlink is refused —
// the environment simply can't exercise this path, rather than the guard
// being broken.
func TestReadUnitArtFile_RejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)

	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret3.json")
	if err := os.WriteFile(secret, []byte(`{"password":"hunter2"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(dir, "human", "escape")
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlink creation not permitted in this environment: %v", err)
	}

	if _, _, ok := ReadUnitArtFile("human/escape/secret3.json"); ok {
		t.Fatal("ReadUnitArtFile served a file reached through a symlink escaping the art root")
	}
}

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

// THE security test. Every one of these must be REFUSED and write nothing
// outside the intended tree.
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
		"slug traversal":    {Faction: "human", Unit: "u", Files: []UnitArtFile{{Name: "packed/../evil.png", ContentBase64: b64("x")}}},
		"no files":          {Faction: "human", Unit: "u", Files: nil},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if err := SaveUnitArt(req); err == nil {
				t.Fatalf("%s: expected rejection, got nil", name)
			}
		})
	}
	if b, _ := os.ReadFile(secret); string(b) != "orig" {
		t.Fatal("a rejected write escaped the art root")
	}
}

// A genuine disk-write failure must not leak the server's absolute art-root
// path into the returned error, since the HTTP handler echoes SaveUnitArt's
// error message straight to the client.
func TestSaveUnitArt_WriteFailureDoesNotLeakAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)

	// Pre-create the target file's path AS A DIRECTORY so os.WriteFile fails
	// with a genuine OS error (not a validation rejection).
	badPath := filepath.Join(dir, "human", "moon_dancer", "sprites.json")
	if err := os.MkdirAll(badPath, 0o755); err != nil {
		t.Fatal(err)
	}

	req := UnitArtSaveRequest{
		Faction: "human", Unit: "moon_dancer",
		Files: []UnitArtFile{{Name: "sprites.json", ContentBase64: b64(`{"key":"moon_dancer"}`)}},
	}
	err := SaveUnitArt(req)
	if err == nil {
		t.Fatal("expected a write failure, got nil")
	}
	if strings.Contains(err.Error(), dir) {
		t.Fatalf("write-failure error leaked the absolute art root path: %v", err)
	}
}

// Symlink-escape coverage mirroring TestReadUnitArtFile_RejectsSymlinkEscape,
// but for the write path: a symlinked ancestor planted inside the art root
// must not redirect a save outside it. Creating a symlink requires elevated
// privilege on Windows without Developer Mode enabled, so this test skips
// (not fails) when os.Symlink is refused.
func TestSaveUnitArt_RejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UNIT_ASSETS_DIR", dir)

	outsideDir := t.TempDir()

	linkPath := filepath.Join(dir, "human")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlink creation not permitted in this environment: %v", err)
	}

	req := UnitArtSaveRequest{
		Faction: "human", Unit: "evil",
		Files: []UnitArtFile{{Name: "sprites.json", ContentBase64: b64(`{"key":"evil"}`)}},
	}
	if err := SaveUnitArt(req); err == nil {
		t.Fatal("SaveUnitArt wrote through a symlink escaping the art root")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "evil", "sprites.json")); err == nil {
		t.Fatal("file was written outside the art root via a symlinked ancestor")
	}
}
