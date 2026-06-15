package game

import (
	"encoding/json"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
)

// expectZonePanic runs validateZones and asserts it panics with a message
// containing want.
func expectZonePanic(t *testing.T, zones []protocol.Zone, want string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("validateZones did not panic; expected message containing %q", want)
		}
		if msg, ok := r.(string); ok && !strings.Contains(msg, want) {
			t.Fatalf("panic = %q; want substring %q", msg, want)
		}
	}()
	validateZones("test.json", normalizeZones(zones))
}

func presenceCfg() protocol.ZoneCapture {
	return protocol.ZoneCapture{Type: "presence", Config: json.RawMessage(`{"captureSeconds":3}`)}
}

func TestValidateZones_DuplicateID(t *testing.T) {
	expectZonePanic(t, []protocol.Zone{
		{ID: "a", Cells: [][2]int{{0, 0}}, Capture: presenceCfg()},
		{ID: "a", Cells: [][2]int{{1, 0}}, Capture: presenceCfg()},
	}, "duplicate zone id a")
}

func TestValidateZones_OverlappingCells(t *testing.T) {
	expectZonePanic(t, []protocol.Zone{
		{ID: "a", Cells: [][2]int{{5, 5}}, Capture: presenceCfg()},
		{ID: "b", Cells: [][2]int{{5, 5}}, Capture: presenceCfg()},
	}, "cell [5,5]")
}

func TestValidateZones_DanglingAdjacency(t *testing.T) {
	expectZonePanic(t, []protocol.Zone{
		{ID: "a", Cells: [][2]int{{0, 0}}, Capture: presenceCfg(), Adjacent: []string{"ghost"}},
	}, "unknown zone \"ghost\"")
}

func TestValidateZones_UnknownCaptureType(t *testing.T) {
	expectZonePanic(t, []protocol.Zone{
		{ID: "a", Cells: [][2]int{{0, 0}}, Capture: protocol.ZoneCapture{Type: "teleport"}},
	}, "unknown capture type teleport")
}

func TestValidateZones_PresenceCaptureSecondsMustBePositive(t *testing.T) {
	expectZonePanic(t, []protocol.Zone{
		{ID: "a", Cells: [][2]int{{0, 0}}, Capture: protocol.ZoneCapture{Type: "presence", Config: json.RawMessage(`{"captureSeconds":0}`)}},
	}, "captureSeconds must be > 0")
}

func TestNormalizeZones_DirectedLinksAndDefaults(t *testing.T) {
	zones := normalizeZones([]protocol.Zone{
		{ID: "a", Cells: [][2]int{{0, 0}}, Capture: presenceCfg(), Adjacent: []string{"b"}},
		{ID: "b", Cells: [][2]int{{1, 0}}, Capture: presenceCfg()},
	})
	// Links are DIRECTED now — a -> b does NOT add b -> a.
	if containsString(zones[1].Adjacent, "a") {
		t.Fatalf("adjacency must not be symmetrised: b.adjacent = %v", zones[1].Adjacent)
	}
	if !containsString(zones[0].Adjacent, "b") {
		t.Fatalf("a should keep its authored link to b, got %v", zones[0].Adjacent)
	}
	// Defaults: empty StartingOwner -> neutral; empty Name -> id.
	if zones[0].StartingOwner != protocol.ZoneCaptureNeutralOwner {
		t.Fatalf("StartingOwner default = %q; want neutral", zones[0].StartingOwner)
	}
	if zones[0].Name != "a" {
		t.Fatalf("Name default = %q; want id 'a'", zones[0].Name)
	}
}
