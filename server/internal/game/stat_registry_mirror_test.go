package game

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// clientStatRegistryPath is the TS mirror of this package's statRegistry.
var clientStatRegistryPath = filepath.Join(
	"..", "..", "..", "client", "src", "game-portal", "src", "game", "stats", "statRegistry.ts")

var clientStatIDPattern = regexp.MustCompile(`\{\s*id:\s*'([^']+)'`)

// TestStatRegistry_ClientMirrorIsComplete fails when a stat exists server-side
// but not in the client's hand-maintained STAT_DEFS.
//
// The drift is SILENT and it shipped: `damageTaken` was registered in Go, folded
// at the damage step, and authored by marker_trap's mark — but missing from the
// TS list. The ability editor's change_stat "stat" control is driven by
// selfStatDefs() from that list, so the dropdown offered 24 of the 25 stats and
// marker_trap's authored value matched no option. The field rendered BLANK, and
// touching it would have silently rewritten the trap's only effect.
//
// Deliberately one-directional: a client-only entry is caught by the second
// check below, but the failure that matters is a stat the server can produce
// and the editor cannot show.
func TestStatRegistry_ClientMirrorIsComplete(t *testing.T) {
	raw, err := os.ReadFile(clientStatRegistryPath)
	if err != nil {
		t.Skipf("client mirror not readable from here (%v) — this guard only runs in a full checkout", err)
	}

	inClient := map[string]bool{}
	for _, m := range clientStatIDPattern.FindAllStringSubmatch(string(raw), -1) {
		inClient[m[1]] = true
	}
	if len(inClient) == 0 {
		t.Fatalf("parsed no stat ids out of %s — the mirror's shape changed and this guard is now blind", clientStatRegistryPath)
	}

	var missing []string
	inServer := map[string]bool{}
	for _, id := range ListStatIDs() {
		inServer[id] = true
		if !inClient[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("stats registered in Go but absent from the client mirror %s: %v\n"+
			"The editor cannot offer these, so any ability authoring one renders a blank control.",
			clientStatRegistryPath, missing)
	}

	var extra []string
	for id := range inClient {
		if !inServer[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("stats in the client mirror with no server registration: %v\n"+
			"The editor would offer these and the server would reject them at save.", extra)
	}
}
