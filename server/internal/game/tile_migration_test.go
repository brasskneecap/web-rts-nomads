package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// TestTileCoordLegacyUnmarshal verifies TileCoord's UnmarshalJSON accepts
// both the legacy pixel shape (sheet/sx/sy, migrated via integer division by
// the old 32px logical tile size) and the current grid-index shape
// (tileset/col/row).
func TestTileCoordLegacyUnmarshal(t *testing.T) {
	var c protocol.TileCoord
	if err := json.Unmarshal([]byte(`{"sheet":"grass-grass-8x8","sx":96,"sy":0}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Tileset != "grass-grass-8x8" || c.Col != 3 || c.Row != 0 {
		t.Fatalf("legacy migrate wrong: %+v", c)
	}

	var c2 protocol.TileCoord
	if err := json.Unmarshal([]byte(`{"tileset":"t","col":2,"row":1}`), &c2); err != nil {
		t.Fatal(err)
	}
	if c2.Tileset != "t" || c2.Col != 2 || c2.Row != 1 {
		t.Fatalf("new shape wrong: %+v", c2)
	}
}

// TestIsWalkableGroundTile verifies the walkability re-key from pixel offsets
// to grid indices preserves the original semantics.
func TestIsWalkableGroundTile(t *testing.T) {
	if !isWalkableGroundTile(protocol.TileCoord{Tileset: "grass-grass-elevation-0", Col: 3, Row: 0}) {
		t.Fatal("flat -0 sheet tile should be walkable")
	}
	if isWalkableGroundTile(protocol.TileCoord{Tileset: "grass-grass-elevation-25", Col: 2, Row: 2}) {
		t.Fatal("-25 cliff-sheet non-interior tile should block")
	}
	if !isWalkableGroundTile(protocol.TileCoord{Tileset: "tileset", Col: 2, Row: 1}) {
		t.Fatal("pure grass slot should be walkable")
	}
	if !isWalkableGroundTile(protocol.TileCoord{Tileset: "tileset", Col: 0, Row: 3}) {
		t.Fatal("pure dirt slot should be walkable")
	}
}
