package game

import (
	"encoding/json"
	"fmt"

	"webrts/server/pkg/protocol"
)

// allZoneCaptureHandlersRegistered wires the three shipped capture mechanics
// into the registry. Registration uses a package-level var (NOT init()) so the
// map catalog loader can declare a dependency on the registry by referencing
// this anchor — package var initialisers run before init() funcs, and
// mapCatalog = mustLoadMapCatalog() validates zone capture types at init time,
// so the registry must already be populated. See the same pattern for
// allObjectiveHandlersRegistered in objective_handlers.go.
//
// Adding a fourth mechanic is one more registerZoneCapture call — no change to
// the tick loop, the gate, the build-gate, or the snapshot.
var allZoneCaptureHandlersRegistered = registerAllZoneCaptureHandlers()

func registerAllZoneCaptureHandlers() bool {
	// control_point — own the structure on the zone's anchor. No config.
	registerZoneCapture("control_point", zoneCaptureHandler{
		parseConfig: func(raw json.RawMessage) (any, error) { return struct{}{}, nil },
		validate:    func(filename, zoneID string, cfg any) {},
		evaluate:    evaluateControlPointCapture,
	})

	// presence — sole-team occupancy timer.
	registerZoneCapture("presence", zoneCaptureHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			cfg := presenceCaptureConfig{}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &cfg); err != nil {
					return nil, err
				}
			}
			return cfg, nil
		},
		validate: func(filename, zoneID string, cfg any) {
			c := cfg.(presenceCaptureConfig)
			if c.CaptureSeconds <= 0 {
				panic(fmt.Sprintf("catalog/maps/%s: zone %s: presence captureSeconds must be > 0", filename, zoneID))
			}
		},
		evaluate: evaluatePresenceCapture,
	})

	// clear — flip to the adjacent-owning team once no hostile remains inside.
	registerZoneCapture("clear", zoneCaptureHandler{
		parseConfig: func(raw json.RawMessage) (any, error) { return struct{}{}, nil },
		validate:    func(filename, zoneID string, cfg any) {},
		evaluate:    evaluateClearCapture,
	})

	// claim — build a tower on the zone's 2x2 slot, then defend it for a
	// duration to capture the zone.
	registerZoneCapture("claim", zoneCaptureHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			cfg := claimCaptureConfig{}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &cfg); err != nil {
					return nil, err
				}
			}
			return cfg, nil
		},
		validate: func(filename, zoneID string, cfg any) {
			c := cfg.(claimCaptureConfig)
			if c.DefendSeconds <= 0 {
				panic(fmt.Sprintf("catalog/maps/%s: zone %s: claim defendSeconds must be > 0", filename, zoneID))
			}
			if c.TowerType != "" {
				if _, ok := getBuildingDef(c.TowerType); !ok {
					panic(fmt.Sprintf("catalog/maps/%s: zone %s: claim towerType %q is not a known building", filename, zoneID, c.TowerType))
				}
			}
		},
		evaluate: evaluateClaimCapture,
	})

	return true
}

type claimCaptureConfig struct {
	DefendSeconds float64 `json:"defendSeconds"`
	// TowerType, when set, is the building type that must occupy the slot to
	// count as the claim tower. Empty ⇒ any team-owned completed building counts.
	TowerType string `json:"towerType,omitempty"`
}

type presenceCaptureConfig struct {
	CaptureSeconds float64 `json:"captureSeconds"`
}

// buildingAtCellLocked returns the building whose footprint covers cell, or nil.
// Linear scan over buildings (small counts). Used by control_point capture.
func (s *GameState) buildingAtCellLocked(cell gridPoint) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if cell.X >= b.X && cell.X < b.X+b.Width && cell.Y >= b.Y && cell.Y < b.Y+b.Height {
			return b
		}
	}
	return nil
}

// evaluateControlPointCapture sets the zone owner to the owner of the live
// building sitting on the zone's anchor, subject to the adjacency gate. A
// missing / owner-less / destroyed structure leaves the owner unchanged.
func evaluateControlPointCapture(s *GameState, rt *zoneRuntime, _ float64) {
	anchor := gridPoint{X: rt.Def.Anchor.X, Y: rt.Def.Anchor.Y}
	b := s.buildingAtCellLocked(anchor)
	if b == nil || !b.Visible || b.OwnerID == nil {
		return
	}
	owner := *b.OwnerID
	if !isHumanOwner(owner) {
		return // only a human-held structure flips the zone to the team
	}
	if s.zonesAlliedLocked(rt.Owner, owner) {
		return // already held by this team
	}
	if !s.zoneCapturableByLocked(rt, owner) {
		return // structure owner lacks an adjacent foothold
	}
	rt.Owner = protocol.ZoneCaptureTeamOwner // capture is a team effort
}

// evaluatePresenceCapture advances a capture timer while the human team holds
// the zone's capture sub-zone uncontested. Occupancy is measured against the
// capture region (the sub-zone if authored, else the whole zone). Contested
// (a hostile unit also inside) freezes progress; capture flips the zone to the
// team sentinel. Co-op posture: "the team" vs the AI factions.
func evaluatePresenceCapture(s *GameState, rt *zoneRuntime, dt float64) {
	cfg, ok := rt.captureCfg.(presenceCaptureConfig)
	if !ok || cfg.CaptureSeconds <= 0 {
		return
	}

	humanRep := "" // smallest human owner id present (deterministic gate candidate)
	hostilePresent := false
	for _, u := range s.Units {
		if !s.unitInCaptureRegionLocked(rt, u) {
			continue
		}
		switch u.OwnerID {
		case enemyPlayerID, neutralPlayerID:
			hostilePresent = true
		default:
			if humanRep == "" || u.OwnerID < humanRep {
				humanRep = u.OwnerID
			}
		}
	}
	humanPresent := humanRep != ""

	if humanPresent && hostilePresent {
		rt.Contested = true
		return // contested — freeze progress
	}

	if humanPresent && !hostilePresent {
		if s.zonesAlliedLocked(rt.Owner, humanRep) {
			rt.Progress = 0 // already team-held
			return
		}
		if s.zoneCapturableByLocked(rt, humanRep) {
			rt.Capturing = true // progress is actively advancing this tick
			rt.Progress += dt
			if rt.Progress >= cfg.CaptureSeconds {
				rt.Owner = protocol.ZoneCaptureTeamOwner // team effort
				rt.Progress = 0
			}
			return
		}
	}

	// Empty, hostile-only, or locked — no progress is held.
	rt.Progress = 0
}

// evaluateClearCapture flips a zone to the team holding an adjacent zone once no
// hostile (enemy / neutral / opposing) unit remains inside it. Ownership is
// sticky thereafter. The capturing team is the owner of the first adjacent zone
// (authored order) held by a real team — the adjacency gate and the captor are
// resolved together.
func evaluateClearCapture(s *GameState, rt *zoneRuntime, _ float64) {
	if isHumanOwner(rt.Owner) {
		return // already team-held (sticky)
	}
	// The team must hold an adjacent zone to claim this one once it's cleared.
	hasFoothold := false
	for _, adjID := range rt.Def.Adjacent {
		if adj := s.zoneRuntimeByIDLocked(adjID); adj != nil && isHumanOwner(adj.Owner) {
			hasFoothold = true
			break
		}
	}
	if !hasFoothold {
		return // no adjacent foothold
	}
	for _, u := range s.Units {
		if !s.unitInZoneLocked(rt, u) {
			continue
		}
		if u.OwnerID == enemyPlayerID || u.OwnerID == neutralPlayerID {
			return // a hostile still occupies the zone
		}
	}
	rt.Owner = protocol.ZoneCaptureTeamOwner // team effort
}

// isClaimSlotCell reports whether cell falls in ANY claim capture-point's 2x2
// build slot — the 2x2 block whose top-left is each point cell.
func isClaimSlotCell(rt *zoneRuntime, cell gridPoint) bool {
	for _, p := range claimPointCells(rt) {
		ax, ay := p[0], p[1]
		if cell.X >= ax && cell.X <= ax+1 && cell.Y >= ay && cell.Y <= ay+1 {
			return true
		}
	}
	return false
}

// claimPointCells returns the top-left cell of each claim capture-point slot
// for rt: the authored Def.ClaimPoints, or a single anchor slot when none are
// authored (backward-compatible default). Used to size the per-point state and
// to drive all claim slot geometry.
func claimPointCells(rt *zoneRuntime) [][2]int {
	if len(rt.Def.ClaimPoints) > 0 {
		return rt.Def.ClaimPoints
	}
	return [][2]int{{rt.Def.Anchor.X, rt.Def.Anchor.Y}}
}

// claimTowerAtPointLocked returns the team-owned, fully-built tower occupying
// the 2x2 slot whose top-left is point (matching towerType if set), or nil.
// Under-construction buildings do not count.
func (s *GameState) claimTowerAtPointLocked(point [2]int, towerType string) *protocol.BuildingTile {
	ax, ay := point[0], point[1]
	for dy := 0; dy < 2; dy++ {
		for dx := 0; dx < 2; dx++ {
			b := s.buildingAtCellLocked(gridPoint{X: ax + dx, Y: ay + dy})
			if b == nil || !b.Visible || b.OwnerID == nil {
				continue
			}
			if !isHumanOwner(*b.OwnerID) {
				continue
			}
			if getMetadataBool(b.Metadata, "underConstruction") {
				continue
			}
			if towerType != "" && b.BuildingType != towerType {
				continue
			}
			return b
		}
	}
	return nil
}

// claimZoneTowerLocked returns the first completed team tower standing on an
// UNCAPTURED claim point of the named zone (authored order), or nil. Used to
// point capture-trigger enemy spawns at a structure the team is actively using
// to capture the zone. Deterministic (authored order, no RNG).
func (s *GameState) claimZoneTowerLocked(zoneID string) *protocol.BuildingTile {
	rt := s.zoneRuntimeByIDLocked(zoneID)
	if rt == nil {
		return nil
	}
	cfg, ok := rt.captureCfg.(claimCaptureConfig)
	if !ok {
		return nil
	}
	points := claimPointCells(rt)
	for i, p := range points {
		if i < len(rt.claimPoints) && rt.claimPoints[i].Captured {
			continue // captured points need no defending
		}
		if tower := s.claimTowerAtPointLocked(p, cfg.TowerType); tower != nil {
			return tower
		}
	}
	return nil
}

// claimSlotBuildableLocked reports whether buildingType may be built on cell as
// the claim-tower exception to the build-gate: the cell is in a claim zone's
// 2x2 slot and (if a towerType is configured) the building being placed is that
// tower. Claim is a standalone build-and-defend capture, so — unlike the other
// mechanics — it does NOT require an adjacent foothold to start: the open slot
// is how the team breaks into the zone in the first place.
func (s *GameState) claimSlotBuildableLocked(rt *zoneRuntime, cell gridPoint, buildingType string) bool {
	if rt == nil || rt.Def.Capture.Type != "claim" {
		return false
	}
	if !isClaimSlotCell(rt, cell) {
		return false
	}
	if cfg, ok := rt.captureCfg.(claimCaptureConfig); ok && cfg.TowerType != "" && buildingType != cfg.TowerType {
		return false
	}
	return true
}

// evaluateClaimCapture advances a per-point defend timer for every uncaptured
// claim capture point that has a completed team tower standing on its 2x2 slot.
// Each point captures independently once its timer reaches the shared
// defendSeconds and stays captured (sticky). The zone flips to the team only
// once EVERY point is captured. A point with no/destroyed tower resets its own
// timer — the team must keep each tower alive for the full duration.
//
// rt.Progress is set to the max in-flight point fraction so the existing
// top-of-screen "Defending" bar shows the most-progressed point; per-point
// progress travels in the snapshot. rt.Capturing is set if any point advanced.
func evaluateClaimCapture(s *GameState, rt *zoneRuntime, dt float64) {
	if isHumanOwner(rt.Owner) {
		return // already claimed (sticky)
	}
	cfg, ok := rt.captureCfg.(claimCaptureConfig)
	if !ok || cfg.DefendSeconds <= 0 {
		return
	}
	rt.Progress = 0 // recomputed below as the max in-flight point fraction (0..1)
	points := claimPointCells(rt)
	allCaptured := true
	for i, p := range points {
		if i >= len(rt.claimPoints) {
			break // defensive: should not happen (sized at install)
		}
		ps := &rt.claimPoints[i]
		if ps.Captured {
			continue
		}
		tower := s.claimTowerAtPointLocked(p, cfg.TowerType)
		if tower == nil {
			ps.Progress = 0 // no tower → restart this point's defend timer
			allCaptured = false
			continue
		}
		rt.Capturing = true // this point's defend timer is advancing this tick
		ps.Progress += dt
		if frac := ps.Progress / cfg.DefendSeconds; frac > rt.Progress {
			rt.Progress = frac
		}
		if ps.Progress >= cfg.DefendSeconds {
			ps.Captured = true
			ps.Progress = 0
		} else {
			allCaptured = false
		}
	}
	if rt.Progress > 1 {
		rt.Progress = 1
	}
	if allCaptured {
		rt.Owner = protocol.ZoneCaptureTeamOwner
		rt.Progress = 0
		rt.Capturing = false
	}
}
