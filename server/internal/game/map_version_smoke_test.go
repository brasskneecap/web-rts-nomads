package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmoke_WelcomeMapRoundTripsThroughSave exercises the exact path the
// joiner's client takes to persist a host's map: take a welcome-shaped map
// (flat + hydrated — precisely what GetMapConfigByID/WelcomeMessage emit),
// serialize it the way the client POSTs (grouped, via MapCatalogEntry's
// MarshalJSON), then decode + save it the way the POST /maps handler does
// (json.Unmarshal into MapCatalogEntry -> SaveMapCatalogEntryWithOptions).
//
// This is the "does the server accept a welcome-derived map?" smoke test —
// the fragile (b) re-host persistence path. It also asserts the version
// fields behave: the host's incoming contentHash is REPLACED by a freshly
// computed one, the human Version is preserved, and contentHash is never
// written to disk.
func TestSmoke_WelcomeMapRoundTripsThroughSave(t *testing.T) {
	t.Setenv("MAP_CATALOG_DIR", t.TempDir())

	const smokeID = "smoke-welcome-map"

	// A real embedded map, in the flat+hydrated form the welcome ships.
	cfg := GetMapConfigByID(DefaultMapID())
	cfg.ID = smokeID
	cfg.Name = "Smoke Welcome Map"
	cfg.Version = "v-smoke"
	cfg.ContentHash = "sha256:FAKEHOSTHASHshouldberecomputed" // host's value; server must replace

	entry := MapCatalogEntry{
		ID:          smokeID,
		Name:        cfg.Name,
		Description: "round-trip smoke",
		Map:         cfg,
	}

	// Client-side: marshal to the grouped on-wire form it POSTs to /maps.
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal (client POST body): %v", err)
	}

	// Server-side: exactly what the POST /maps handler does.
	var decoded MapCatalogEntry
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("server decode of welcome-derived POST body failed: %v", err)
	}
	if _, _, err := SaveMapCatalogEntryWithOptions(decoded, SaveMapOptions{}); err != nil {
		t.Fatalf("server rejected welcome-derived map save: %v", err)
	}

	// Served back (the form a future host/lobby reads).
	got := GetMapConfigByID(smokeID)
	if got.ContentHash == "" || !strings.HasPrefix(got.ContentHash, "sha256:") {
		t.Fatalf("served map has no valid content hash: %q", got.ContentHash)
	}
	if got.ContentHash == cfg.ContentHash {
		t.Fatalf("server did not recompute the hash; kept the host's incoming value %q", got.ContentHash)
	}
	if got.Version != "v-smoke" {
		t.Fatalf("human version not preserved through save: got %q want %q", got.Version, "v-smoke")
	}

	// On-disk authored file must NOT carry the derived contentHash, but MUST
	// keep the authored version.
	diskPath := filepath.Join(os.Getenv("MAP_CATALOG_DIR"), smokeID+".json")
	raw, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if strings.Contains(string(raw), "contentHash") {
		t.Errorf("on-disk file leaked the derived contentHash:\n%s", raw)
	}
	if !strings.Contains(string(raw), "v-smoke") {
		t.Errorf("on-disk file dropped the authored version")
	}

	// Editing content must change the served hash (the signal the whole
	// host/joiner sync relies on).
	hashBefore := got.ContentHash
	edited := decoded
	edited.Map.Width += 128 // a real content change
	if _, _, err := SaveMapCatalogEntryWithOptions(edited, SaveMapOptions{}); err != nil {
		t.Fatalf("re-save after edit failed: %v", err)
	}
	if after := GetMapConfigByID(smokeID).ContentHash; after == hashBefore {
		t.Fatalf("content hash did not change after an edit (was %q both times)", after)
	}
}
