package game

import "testing"

func TestTilesetPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TILESET_CATALOG_DIR", dir)
	def := TilesetDef{ID: "my-set", Name: "My Set", Image: "my-set.png", Cols: 4, Rows: 4, TileWidth: 32, TileHeight: 32}
	if err := SaveTilesetDef(def); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok := GetTilesetDef("my-set")
	if !ok || got.Cols != 4 || got.Image != "my-set.png" {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	if err := SaveTilesetDef(TilesetDef{ID: "Bad Id", Name: "x", Image: "x.png", Cols: 1, Rows: 1, TileWidth: 1, TileHeight: 1}); !IsTilesetValidationError(err) {
		t.Fatalf("expected validation error for bad id, got %v", err)
	}
	existed, err := DeleteTilesetDef("my-set")
	if err != nil || !existed {
		t.Fatalf("delete: existed=%v err=%v", existed, err)
	}
	if _, ok := GetTilesetDef("my-set"); ok {
		t.Fatal("still present after delete")
	}
}
