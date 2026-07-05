package ws

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"webrts/server/internal/game"
	"webrts/server/pkg/protocol"
)

// updateBaseline regenerates the committed baseline file. Run with:
//
//	go test -run TestSPBaseline -update ./internal/ws/...
//
// Intentional protocol changes regenerate the baseline as part of the same PR
// (the PR description SHALL explain why).
var updateBaseline = flag.Bool("update", false, "regenerate the SP baseline file")

// TestSPBaseline_StructuralShape is the §10 SP regression guard required by
// the pluggable-mp-transport spec. It runs a scripted single-player scenario
// (join a match, receive welcome + initial snapshot) and asserts that the
// normalized application-protocol payload bytes match the committed baseline
// in testdata/sp_baseline_outbound.json.
//
// "Normalized" means matchID, serverNow timestamps, RNG-seeded internal ids,
// and any other run-varying fields are replaced with stable tokens before
// comparison. This catches structural protocol changes (added/removed fields,
// type changes) while tolerating per-run randomness in seeds and timestamps.
//
// True byte-identity baseline (per the spec's "byte-identical" wording) would
// require injecting a fixed seed into game.Match construction; that's a
// separate game-package change tracked outside this section. The §10 refactor
// itself preserves bytes by construction — Client.WriteJSON marshals once and
// hands the bytes to Transport.WriteMessage unchanged (proven structurally by
// TestClient_WriteJSON_BytesEqualJSONMarshal). This baseline guards against
// future protocol changes slipping in unannounced.
func TestSPBaseline_StructuralShape(t *testing.T) {
	// Force the StartWaveBonus testing toggle off for this scenario. It is a
	// debug switch the user flips on/off; when on, the match opens with an
	// RNG-seeded start-of-match upgrade offer whose card set varies every run,
	// which would leak per-run randomness into this golden snapshot. Pinning it
	// off keeps the transport/protocol shape guard deterministic regardless of
	// the committed toggle value.
	t.Cleanup(game.SetStartWaveBonusForTest(false))

	mm := game.NewMatchManager()
	lm := game.NewLobbyManager()
	hub := NewHub(mm, lm)
	defer hub.Close()

	fake := NewFakeTransport("baseline-client", 8)
	client := hub.RegisterTransport(fake)
	if client == nil {
		t.Fatal("RegisterTransport returned nil")
	}

	// Scripted scenario: a single SP join.
	join := protocol.JoinMatchMessage{
		Type:     "join_match",
		PlayerID: "baseline-player",
		MapID:    "",
	}
	rawJoin, err := json.Marshal(join)
	if err != nil {
		t.Fatalf("marshal join: %v", err)
	}
	fake.Push(rawJoin)

	// Wait for welcome + snapshot.
	if !waitForOutgoing(fake, 2, 2*time.Second) {
		t.Fatalf("hub did not produce 2 outgoing messages; got %d", len(fake.Outgoing()))
	}
	out := fake.Outgoing()

	normalized := make([]map[string]any, 0, len(out))
	for i, raw := range out {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("unmarshal msg %d: %v\nraw: %s", i, err, raw)
		}
		normalize(obj)
		normalized = append(normalized, obj)
	}

	got, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		t.Fatalf("marshal normalized: %v", err)
	}

	baselinePath := filepath.Join("testdata", "sp_baseline_outbound.json")
	if *updateBaseline {
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(baselinePath, append(got, '\n'), 0o644); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		t.Logf("baseline regenerated at %s", baselinePath)
		return
	}

	want, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read baseline at %s: %v (regenerate with -update)", baselinePath, err)
	}
	// Trim trailing newline written by -update path.
	want = []byte(strings.TrimRight(string(want), "\n"))

	if !reflect.DeepEqual(string(want), string(got)) {
		t.Errorf("SP outbound shape drift detected. To accept the change intentionally, regenerate with:\n  go test -run TestSPBaseline_StructuralShape -update ./internal/ws/...\n\n--- expected (committed baseline) ---\n%s\n\n--- actual (this run) ---\n%s", want, got)
	}
}

// normalize walks a parsed JSON object tree and replaces run-varying values
// with stable tokens so the result is comparable across runs. Conservative —
// any key that obviously varies per-run is normalized; structural fields stay.
func normalize(obj map[string]any) {
	for k, v := range obj {
		switch vv := v.(type) {
		case map[string]any:
			normalize(vv)
		case []any:
			for _, item := range vv {
				if m, ok := item.(map[string]any); ok {
					normalize(m)
				}
			}
		}
		// Keys whose values vary per-run regardless of input.
		if isVaryingKey(k) {
			switch v.(type) {
			case string:
				obj[k] = "NORM-STR"
			case float64:
				obj[k] = float64(0)
			case bool:
				obj[k] = false
			default:
				obj[k] = "NORM"
			}
		}
	}
}

func isVaryingKey(k string) bool {
	switch k {
	case "matchId", "serverNow", "instanceId", "id", "createdAt", "updatedAt", "createdAtUnix", "updatedAtUnix", "seed", "lobbyId", "ts", "tickMs", "currentTickMs",
		// Per-player cosmetic randomness (rngCosmetic) — colour, etc.
		"color",
		// Resource/config amounts are balance tunables owned by player.json and
		// spawn config; a structural-shape baseline must not pin their values,
		// only that the field is present and numeric.
		"amount":
		return true
	}
	// Keys whose lower-cased name ends in these suffixes are treated as varying
	// timestamps / ids regardless of casing.
	low := strings.ToLower(k)
	if strings.HasSuffix(low, "deadlinems") || strings.HasSuffix(low, "atunix") || strings.HasSuffix(low, "atms") {
		return true
	}
	return false
}

// ensure sort import isn't dropped if normalize gains a sort step later.
var _ = sort.Strings
