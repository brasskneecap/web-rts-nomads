package game

import (
	"math"
	"sort"

	"webrts/server/pkg/protocol"
)

// installZonesLocked builds the per-match zone runtime and the cell->zoneId
// index from MapConfig.Zones. Called from setMapConfigLocked, so zones are
// ready whenever a map is loaded (campaign or custom). Idempotent: rebuilds
// from the current MapConfig each call.
//
// Owner initialises from the authored StartingOwner (defaulted to the neutral
// sentinel by hydration). The typed capture config is parsed once here via the
// registry; a parse failure (already rejected at catalog load) leaves the
// mechanic inert for that zone rather than panicking at match start.
func (s *GameState) installZonesLocked() {
	zones := s.MapConfig.Zones
	if len(zones) == 0 {
		s.Zones = nil
		s.zoneCellIndex = nil
		return
	}

	s.Zones = make([]zoneRuntime, 0, len(zones))
	s.zoneCellIndex = make(map[gridPoint]string)
	for _, z := range zones {
		owner := z.StartingOwner
		if owner == "" {
			owner = protocol.ZoneCaptureNeutralOwner
		}
		// A zone linked to a player's start is the team's home: team-owned from
		// the first tick and never capturable.
		locked := z.LockedSpawnLabel != ""
		if locked {
			owner = protocol.ZoneCaptureTeamOwner
		}
		var cfg any
		if h, ok := zoneCaptureRegistry[z.Capture.Type]; ok {
			if parsed, err := h.parseConfig(z.Capture.Config); err == nil {
				cfg = parsed
			}
		}
		var captureCells map[gridPoint]bool
		if len(z.CaptureCells) > 0 {
			captureCells = make(map[gridPoint]bool, len(z.CaptureCells))
			for _, c := range z.CaptureCells {
				captureCells[gridPoint{X: c[0], Y: c[1]}] = true
			}
		}
		rt := zoneRuntime{
			Def:          z,
			Owner:        owner,
			captureCfg:   cfg,
			captureCells: captureCells,
			locked:       locked,
		}
		if z.Capture.Type == "claim" {
			rt.claimPoints = make([]claimPointState, len(claimPointCells(&rt)))
		}
		s.Zones = append(s.Zones, rt)
		for _, c := range z.Cells {
			s.zoneCellIndex[gridPoint{X: c[0], Y: c[1]}] = z.ID
		}
	}
}

// zoneRuntimeByIDLocked resolves a zone runtime by id, or nil. Linear scan —
// zone counts are small (single digits per map).
func (s *GameState) zoneRuntimeByIDLocked(id string) *zoneRuntime {
	for i := range s.Zones {
		if s.Zones[i].Def.ID == id {
			return &s.Zones[i]
		}
	}
	return nil
}

// zoneOwnerForCellLocked returns the id of the zone owning cell, if any. O(1).
func (s *GameState) zoneOwnerForCellLocked(cell gridPoint) (string, bool) {
	if s.zoneCellIndex == nil {
		return "", false
	}
	id, ok := s.zoneCellIndex[cell]
	return id, ok
}

// zonesAlliedLocked reports whether a zone owner string is allied with playerID.
// The neutral sentinel / empty / AI owners are allied with no one, so building
// in (or counting ownership of) an uncontrolled or hostile zone is rejected.
func (s *GameState) zonesAlliedLocked(owner, playerID string) bool {
	switch owner {
	case "", protocol.ZoneCaptureNeutralOwner, neutralPlayerID, enemyPlayerID:
		return false
	case protocol.ZoneCaptureTeamOwner:
		// Team-owned: allied with every real (non-AI) player.
		return playerID != "" && playerID != enemyPlayerID && playerID != neutralPlayerID
	}
	return s.playersAreFriendlyLocked(owner, playerID)
}

// isHumanOwnerLocked reports whether an owner id is a real (non-AI, non-neutral)
// player or the team sentinel — i.e. counts as "the players' team".
func isHumanOwner(owner string) bool {
	switch owner {
	case "", protocol.ZoneCaptureNeutralOwner, neutralPlayerID, enemyPlayerID:
		return false
	}
	return true
}

// zoneCapturableByLocked reports whether the team represented by candidateOwner
// may capture rt, per its directed prerequisite links:
//
//   - no links        → ungated, always capturable
//   - RequireAllLinks → every linked zone must be team-owned
//   - otherwise       → any one linked zone team-owned unlocks it
//
// This is the territorial gate; it applies to the presence / clear /
// control_point mechanics (claim is standalone and does not call it).
func (s *GameState) zoneCapturableByLocked(rt *zoneRuntime, candidateOwner string) bool {
	links := rt.Def.Adjacent
	if len(links) == 0 {
		return true // ungated
	}
	if rt.Def.RequireAllLinks {
		for _, adjID := range links {
			adj := s.zoneRuntimeByIDLocked(adjID)
			if adj == nil || !s.zonesAlliedLocked(adj.Owner, candidateOwner) {
				return false
			}
		}
		return true
	}
	for _, adjID := range links {
		adj := s.zoneRuntimeByIDLocked(adjID)
		if adj != nil && s.zonesAlliedLocked(adj.Owner, candidateOwner) {
			return true
		}
	}
	return false
}

// zoneOwnedByTeamLocked reports whether the named zone is controlled by the
// players' team. Under the current single-shared-team co-op posture, any real
// (non-AI, non-neutral) player owner means "our team holds it"; the build-gate
// and capture mechanics use the finer-grained alliance check where a candidate
// id is available. Used by the capture_zone objective.
func (s *GameState) zoneOwnedByTeamLocked(zoneID string) bool {
	rt := s.zoneRuntimeByIDLocked(zoneID)
	if rt == nil {
		return false
	}
	switch rt.Owner {
	case "", protocol.ZoneCaptureNeutralOwner, neutralPlayerID, enemyPlayerID:
		return false
	}
	return true
}

// unitInZoneLocked reports whether a live unit currently stands in rt's cells.
func (s *GameState) unitInZoneLocked(rt *zoneRuntime, unit *Unit) bool {
	if unit == nil || !unit.Visible || unit.HP <= 0 {
		return false
	}
	cell := s.worldToGrid(unit.X, unit.Y)
	id, ok := s.zoneOwnerForCellLocked(cell)
	return ok && id == rt.Def.ID
}

// unitInCaptureRegionLocked reports whether a live unit stands in rt's capture
// sub-zone. When the zone has no capture sub-zone authored, the whole zone is
// the capture region (so presence keeps working on legacy zones).
func (s *GameState) unitInCaptureRegionLocked(rt *zoneRuntime, unit *Unit) bool {
	if unit == nil || !unit.Visible || unit.HP <= 0 {
		return false
	}
	cell := s.worldToGrid(unit.X, unit.Y)
	if len(rt.captureCells) > 0 {
		return rt.captureCells[cell]
	}
	id, ok := s.zoneOwnerForCellLocked(cell)
	return ok && id == rt.Def.ID
}

// nearestCapturingUnitPosLocked returns the position of the live capturing-team
// (human) unit standing in rt's capture region nearest to (fromX, fromY), or nil
// when none is present. Presence zones have no structure to attack, so their
// capture defenders are pointed at the units holding the zone: arriving contests
// the zone (freezing progress) and killing them resets it — i.e. it stops the
// capture, mirroring how claim defenders destroy the tower.
func (s *GameState) nearestCapturingUnitPosLocked(rt *zoneRuntime, fromX, fromY float64) *protocol.Vec2 {
	if rt == nil {
		return nil
	}
	var best *Unit
	bestD := math.MaxFloat64
	for _, u := range s.Units {
		if u == nil || !isHumanOwner(u.OwnerID) {
			continue
		}
		if !s.unitInCaptureRegionLocked(rt, u) {
			continue
		}
		if d := distanceSquared(u.X, u.Y, fromX, fromY); d < bestD {
			bestD = d
			best = u
		}
	}
	if best == nil {
		return nil
	}
	return &protocol.Vec2{X: best.X, Y: best.Y}
}

// zoneCapturingLocked reports whether the named zone is currently being
// captured — i.e. a capture mechanic advanced its Progress on the most recent
// tickZonesLocked. Drives enemy-spawnpoint `triggerCaptureZoneId` activation
// (the "While Zone Being Captured" spawn-timing mode). Returns false for an
// unknown zone, and for zones whose mechanic has no timed capture
// (clear / control_point never set the flag).
//
// Tick ordering: spawnpoints tick before zones, so this reads the flag computed
// on the previous tick — a deterministic ≤1-tick lag. See the design doc.
func (s *GameState) zoneCapturingLocked(zoneID string) bool {
	rt := s.zoneRuntimeByIDLocked(zoneID)
	return rt != nil && rt.Capturing
}

// tickZonesLocked runs one tick of zone capture evaluation. No-op when the map
// has no zones. Iterates zones in stable authored order; each zone's registered
// mechanic decides — using the adjacency gate — whether and how ownership
// changes. Caller holds s.mu. Deterministic: no wall-clock, no unseeded RNG,
// stable iteration order.
func (s *GameState) tickZonesLocked(dt float64) {
	if len(s.Zones) == 0 {
		return
	}
	for i := range s.Zones {
		rt := &s.Zones[i]
		rt.Contested = false
		rt.Capturing = false
		if rt.locked {
			continue // home zones linked to a start are never capturable
		}
		h, ok := zoneCaptureRegistry[rt.Def.Capture.Type]
		if !ok {
			continue
		}
		h.evaluate(s, rt, dt)
	}
}

// claimPointSnapshotsLocked projects a claim zone's per-point control state for
// the snapshot, in authored order. Returns nil for non-claim zones (no point
// state). Progress is a 0..1 fraction of the shared defendSeconds.
func claimPointSnapshotsLocked(rt *zoneRuntime) []protocol.ZoneClaimPointSnapshot {
	if len(rt.claimPoints) == 0 {
		return nil
	}
	cfg, ok := rt.captureCfg.(claimCaptureConfig)
	if !ok || cfg.DefendSeconds <= 0 {
		return nil
	}
	out := make([]protocol.ZoneClaimPointSnapshot, len(rt.claimPoints))
	for i := range rt.claimPoints {
		frac := rt.claimPoints[i].Progress / cfg.DefendSeconds
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		// A captured point reads as fully filled regardless of its reset timer.
		if rt.claimPoints[i].Captured {
			frac = 1
		}
		out[i] = protocol.ZoneClaimPointSnapshot{Progress: frac, Captured: rt.claimPoints[i].Captured}
	}
	return out
}

// zoneSnapshotsLocked projects the mutable zone control state for the per-tick
// match snapshot. Static geometry travels once in the welcome MapConfig.
//
// Progress is sent as a normalised 0..1 FRACTION (capture/defend timer ÷ its
// configured duration), NOT raw seconds — the client renders it as a fill over
// the zone's cell count, so a value > 1 would index past the cell array.
func (s *GameState) zoneSnapshotsLocked() []protocol.ZoneSnapshot {
	if len(s.Zones) == 0 {
		return nil
	}
	out := make([]protocol.ZoneSnapshot, 0, len(s.Zones))
	for i := range s.Zones {
		rt := &s.Zones[i]
		out = append(out, protocol.ZoneSnapshot{
			ID:          rt.Def.ID,
			Owner:       rt.Owner,
			Contested:   rt.Contested,
			Progress:    zoneProgressFraction(rt),
			OwnerColor:  s.zoneOwnerColorLocked(rt.Owner),
			ClaimPoints: claimPointSnapshotsLocked(rt),
		})
	}
	return out
}

// zoneOwnerColorLocked resolves a zone owner string to the display color used
// for its perimeter tint. Neutral / unowned / AI ⇒ "" (client renders grey).
// The team sentinel ⇒ the lowest-slot joined player's color. A specific player
// id or label ⇒ that player's color.
func (s *GameState) zoneOwnerColorLocked(owner string) string {
	switch owner {
	case "", protocol.ZoneCaptureNeutralOwner, neutralPlayerID, enemyPlayerID:
		return ""
	case protocol.ZoneCaptureTeamOwner:
		return s.lowestPlayerColorLocked()
	}
	if p := s.Players[owner]; p != nil {
		return p.Color
	}
	if id := s.findPlayerIDByLabelLocked(owner); id != "" {
		if p := s.Players[id]; p != nil {
			return p.Color
		}
	}
	return ""
}

// lowestPlayerColorLocked returns the color of the lowest-slot joined player —
// the spawn point with the smallest fillOrder (then lexically smallest label)
// whose slot a real player occupies. Used to colour team-owned zones. Falls
// back to the smallest player id's color if no slot resolves.
func (s *GameState) lowestPlayerColorLocked() string {
	type slot struct {
		label string
		order int
	}
	var slots []slot
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if b.BuildingType != "spawn-point" {
			continue
		}
		label, ok := getMetadataString(b.Metadata, "playerLabel")
		if !ok || label == "" {
			continue
		}
		order := 0
		if v, ok := getMetadataFloat(b.Metadata, "fillOrder"); ok {
			order = int(v)
		}
		slots = append(slots, slot{label: label, order: order})
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].order != slots[j].order {
			return slots[i].order < slots[j].order
		}
		return slots[i].label < slots[j].label
	})
	for _, sl := range slots {
		if id := s.findPlayerIDByLabelLocked(sl.label); id != "" {
			if p := s.Players[id]; p != nil {
				return p.Color
			}
		}
	}
	// Fallback: smallest real player id (deterministic).
	bestID, bestColor := "", ""
	for id, p := range s.Players {
		if id == enemyPlayerID || id == neutralPlayerID {
			continue
		}
		if bestID == "" || id < bestID {
			bestID, bestColor = id, p.Color
		}
	}
	return bestColor
}

// zoneProgressFraction normalises a zone's raw capture/defend accumulator
// to a 0..1 fraction for the snapshot.
// Presence zones: divides by CaptureSeconds (raw seconds accumulator).
// Claim zones: rt.Progress is already a normalised max-point fraction (0..1).
// Returns 0 when the mechanic has no timed duration.
func zoneProgressFraction(rt *zoneRuntime) float64 {
	switch cfg := rt.captureCfg.(type) {
	case presenceCaptureConfig:
		if cfg.CaptureSeconds <= 0 {
			return 0
		}
		return clamp01(rt.Progress / cfg.CaptureSeconds)
	case claimCaptureConfig:
		// Claim sets rt.Progress to an already-normalised max-point fraction.
		return clamp01(rt.Progress)
	}
	return 0
}
