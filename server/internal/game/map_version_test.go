package game

import (
	"strings"
	"testing"
)

func TestComputeMapContentHash_Deterministic(t *testing.T) {
	e := sampleEntry()
	h1, err := computeMapContentHash(e)
	if err != nil {
		t.Fatalf("hash 1: %v", err)
	}
	h2, err := computeMapContentHash(e)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Fatalf("unexpected hash format: %q", h1)
	}
}

// The display-only Version and the derived ContentHash must be excluded from
// the hash input, so changing either one alone leaves the content hash stable.
func TestComputeMapContentHash_IgnoresVersionAndExistingHash(t *testing.T) {
	e := sampleEntry()
	base, err := computeMapContentHash(e)
	if err != nil {
		t.Fatal(err)
	}

	e.Map.Version = "v999"
	e.Map.ContentHash = "sha256:stalehashvalue"
	got, err := computeMapContentHash(e)
	if err != nil {
		t.Fatal(err)
	}
	if got != base {
		t.Fatalf("version/contentHash must not affect content hash:\n base=%q\n  got=%q", base, got)
	}
}

// Any real content change must change the hash.
func TestComputeMapContentHash_ChangesWithContent(t *testing.T) {
	e := sampleEntry()
	base, err := computeMapContentHash(e)
	if err != nil {
		t.Fatal(err)
	}

	e.Map.Width += 1
	got, err := computeMapContentHash(e)
	if err != nil {
		t.Fatal(err)
	}
	if got == base {
		t.Fatalf("content hash should change when content changes (got %q both times)", got)
	}
}

// Every embedded catalog map must carry a non-empty content hash after load,
// so the versioning feature has a key for every shipped map.
func TestEmbeddedCatalog_HasContentHash(t *testing.T) {
	for _, entry := range mapCatalog {
		if entry.Map.ContentHash == "" {
			t.Errorf("embedded map %q has empty content hash", entry.ID)
		}
		if !strings.HasPrefix(entry.Map.ContentHash, "sha256:") {
			t.Errorf("embedded map %q has malformed content hash %q", entry.ID, entry.Map.ContentHash)
		}
	}
}

// GetMapConfigByID must surface the content hash (it rides the WelcomeMessage).
func TestGetMapConfigByID_CarriesContentHash(t *testing.T) {
	cfg := GetMapConfigByID(DefaultMapID())
	if cfg.ContentHash == "" {
		t.Fatalf("GetMapConfigByID(%q) returned empty content hash", DefaultMapID())
	}
}
