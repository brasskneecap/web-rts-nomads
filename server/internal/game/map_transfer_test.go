package game

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"

	"webrts/server/pkg/protocol"
)

func gunzipB64(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return out
}

// Cache miss (client sent no matching hash) → welcome embeds the gzipped map,
// and it round-trips to the same map.
func TestWelcome_Miss_EmbedsCompressedMap(t *testing.T) {
	s := NewGameState(GetMapConfigByID(DefaultMapID()))
	b, err := s.MarshalWelcomeMessage("p1", "m1", nil)
	if err != nil {
		t.Fatal(err)
	}
	var w protocol.WelcomeMessage
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if w.MapGz == "" {
		t.Fatal("miss must embed mapGz")
	}
	if w.ContentHash == "" || w.MapID == "" {
		t.Fatalf("welcome must carry mapId+contentHash: %+v", w)
	}
	var cfg protocol.MapConfig
	if err := json.Unmarshal(gunzipB64(t, w.MapGz), &cfg); err != nil {
		t.Fatalf("decompressed map is not valid MapConfig: %v", err)
	}
	if cfg.ID != w.MapID || cfg.ContentHash != w.ContentHash {
		t.Fatalf("round-trip mismatch: cfg(%s,%s) vs welcome(%s,%s)",
			cfg.ID, cfg.ContentHash, w.MapID, w.ContentHash)
	}
}

// Cache hit (client already holds the map's hash) → welcome omits the map but
// still identifies it by hash.
func TestWelcome_Hit_OmitsMap(t *testing.T) {
	s := NewGameState(GetMapConfigByID(DefaultMapID()))
	hash := s.MapConfig.ContentHash
	if hash == "" {
		t.Fatal("expected the default map to carry a content hash")
	}
	b, err := s.MarshalWelcomeMessage("p1", "m1", []string{"sha256:unrelated", hash})
	if err != nil {
		t.Fatal(err)
	}
	var w protocol.WelcomeMessage
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	if w.MapGz != "" {
		t.Fatal("hit must omit mapGz (no bytes on the wire)")
	}
	if w.ContentHash != hash {
		t.Fatalf("hit must still carry the contentHash: %q", w.ContentHash)
	}
}

// The request_map fallback reply carries the full map, compressed.
func TestMapContentMessage_RoundTrips(t *testing.T) {
	s := NewGameState(GetMapConfigByID(DefaultMapID()))
	b, err := s.MarshalMapContentMessage()
	if err != nil {
		t.Fatal(err)
	}
	var m protocol.MapContentMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.MapGz == "" {
		t.Fatal("map_content must carry mapGz")
	}
	var cfg protocol.MapConfig
	if err := json.Unmarshal(gunzipB64(t, m.MapGz), &cfg); err != nil {
		t.Fatalf("invalid compressed map: %v", err)
	}
	if cfg.ID != m.MapID {
		t.Fatalf("map id mismatch: %q vs %q", cfg.ID, m.MapID)
	}
}
