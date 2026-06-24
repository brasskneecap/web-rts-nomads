package game

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
)

// computeMapContentHash returns a deterministic content hash of a catalog
// entry's authored map content.
//
// The hash is taken over the canonical rendered JSON (RenderCatalogEntryJSON)
// with the derived ContentHash and the display-only Version cleared, so:
//   - editing any real content field changes the hash;
//   - bumping only the human-readable Version string does NOT change the hash;
//   - the hash never depends on its own previous value.
//
// Determinism across machines holds because two machines running the same
// binary over the same authored map have identical embedded files + defs +
// render code, so they produce identical bytes and therefore identical hashes.
// This is what lets a host's hash (stamped into Steam lobby metadata) be
// compared against a joiner's locally-computed hash for the same map id.
func computeMapContentHash(entry MapCatalogEntry) (string, error) {
	// MapCatalogEntry is taken by value; clearing the two excluded scalar
	// fields on the local copy does not touch the caller's entry.
	basis := entry
	basis.Map.ContentHash = ""
	basis.Map.Version = ""
	raw, err := RenderCatalogEntryJSON(basis)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// attachMapContentHash computes the content hash and stores it on the entry's
// map in place. Best-effort: on a render error the hash is left empty and a
// warning is logged, so the versioning feature degrades to "unknown" rather
// than crashing catalog load (which panics on error).
func attachMapContentHash(entry *MapCatalogEntry) {
	h, err := computeMapContentHash(*entry)
	if err != nil {
		slog.Warn("map content hash: render failed", "mapId", entry.ID, "err", err)
		entry.Map.ContentHash = ""
		return
	}
	entry.Map.ContentHash = h
}
