package game

import "testing"

func TestListTilesetDefs(t *testing.T) {
	defs := ListTilesetDefs()
	byID := map[string]TilesetDef{}
	for _, d := range defs {
		byID[d.ID] = d
	}
	g, ok := byID["grass-grass-elevation-25"]
	if !ok {
		t.Fatal("grass-grass-elevation-25 missing")
	}
	if g.Cols != 4 || g.TileWidth != 160 || g.Image != "grass-grass-elevation-25.png" {
		t.Fatalf("bad def: %+v", g)
	}
	if _, ok := byID["tileset"]; !ok {
		t.Fatal("tileset missing")
	}
}
