package transportbridge

// SteamSessionStore is the joiner-side parking lot for an upstream
// MessageStream (a ws.steamTransport in practice) that's waiting for the
// joiner's SPA to reconnect with `?proxy=steam`.
//
// Compared to SessionStore (Direct connect, token-keyed):
//   - Single-slot: a Steam joiner has at most one upstream peer at a time.
//     If a second Set arrives before the first is Taken, the first is
//     closed and dropped — this matches the "host disappeared mid-handshake"
//     edge case where the user retries before the SPA picked up the first.
//   - No token: there's only one slot, so the SPA query is the literal
//     `?proxy=steam` (no per-session secret needed because there's exactly
//     one steam upstream and one local SPA on the loopback).
//
// The MessageStream type is the cycle-free byte-channel surface from
// stream.go; ws.steamTransport satisfies it structurally.

import (
	"sync"
	"time"
)

// SteamSessionTTL caps how long a parked Steam stream waits unclaimed
// before ReapStale closes and drops it. Matches SessionTTL by intent —
// 30 seconds is generous for the SPA-reconnect latency.
const SteamSessionTTL = 30 * time.Second

// SteamSessionStore holds a single upstream Steam stream. Construct with
// NewSteamSessionStore and share across the joiner-side wiring.
type SteamSessionStore struct {
	mu        sync.Mutex
	stream    MessageStream
	createdAt time.Time
}

// NewSteamSessionStore returns an empty store.
func NewSteamSessionStore() *SteamSessionStore {
	return &SteamSessionStore{}
}

// Set parks `stream` as the current Steam upstream. If a previous stream
// was already parked, it's closed before being replaced. Concurrency-safe.
func (s *SteamSessionStore) Set(stream MessageStream) {
	s.mu.Lock()
	old := s.stream
	s.stream = stream
	s.createdAt = time.Now()
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

// Take returns the parked stream and clears the slot. Returns (nil, false)
// when nothing is parked.
func (s *SteamSessionStore) Take() (MessageStream, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stream == nil {
		return nil, false
	}
	t := s.stream
	s.stream = nil
	return t, true
}

// Has reports whether a stream is currently parked. Cheap, lock-bound;
// suitable for diagnostics ("is the steam upstream ready yet?") but not
// for hot paths.
func (s *SteamSessionStore) Has() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream != nil
}

// ReapStale closes the parked stream when it has been parked for longer
// than SteamSessionTTL. Returns true when something was reaped. Intended
// to be called from a caller-owned ticker, matching the Direct-connect
// SessionStore.ReapStale pattern.
func (s *SteamSessionStore) ReapStale(now time.Time) bool {
	s.mu.Lock()
	if s.stream == nil || now.Sub(s.createdAt) < SteamSessionTTL {
		s.mu.Unlock()
		return false
	}
	stream := s.stream
	s.stream = nil
	s.mu.Unlock()
	_ = stream.Close()
	return true
}

// Close drops and closes any parked stream. Called from Hub.Close.
func (s *SteamSessionStore) Close() {
	s.mu.Lock()
	stream := s.stream
	s.stream = nil
	s.mu.Unlock()
	if stream != nil {
		_ = stream.Close()
	}
}
