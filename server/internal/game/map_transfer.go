package game

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"

	"webrts/server/pkg/protocol"
)

// gzipMapConfig marshals a MapConfig to JSON, gzip-compresses it, and base64-
// encodes the result for embedding in a JSON message field. Coordinate-dense
// maps compress ~8-10x, keeping the on-wire map well under transport size caps.
// Callers must hold s.mu (the MapConfig's Building/Obstacle Metadata maps alias
// tick-loop state) — same locking contract as MarshalWelcomeMessage.
func gzipMapConfig(cfg protocol.MapConfig) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(raw); err != nil {
		return "", err
	}
	if err := gw.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// containsHash reports whether h is in list. Used for the welcome cache-hit
// decision (is the match map's contentHash among the client's cached hashes?).
func containsHash(list []string, h string) bool {
	for _, v := range list {
		if v == h {
			return true
		}
	}
	return false
}

// MarshalMapContentMessage builds the out-of-band map_content reply for a
// RequestMapMessage: the current match map, gzip+base64 compressed. Runs under
// s.mu RLock for the same Metadata-aliasing reason as MarshalWelcomeMessage.
func (s *GameState) MarshalMapContentMessage() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	gz, err := gzipMapConfig(s.MapConfig)
	if err != nil {
		return nil, err
	}
	return json.Marshal(protocol.MapContentMessage{
		Type:        "map_content",
		MapID:       s.MapConfig.ID,
		ContentHash: s.MapConfig.ContentHash,
		MapGz:       gz,
	})
}
