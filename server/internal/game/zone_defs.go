package game

import (
	"encoding/json"
	"sort"

	"webrts/server/pkg/protocol"
)

// zoneRuntime is the per-match mutable shell for one authored protocol.Zone.
// One instance lives in GameState.Zones per loaded zone for the full match.
// Mirrors objectiveRuntime: the immutable Def plus the mutable control state.
//
//   - Owner      controlling team/player id, or protocol.ZoneCaptureNeutralOwner.
//   - Progress   capture-timer accumulator (seconds) for the presence mechanic.
//   - Contested  true when more than one team occupies a presence zone.
//   - captureCfg  the typed config produced by the mechanic's parseConfig hook
//                 at install time; the mechanic's evaluate casts it back.
//
// Zones reference other entities (the anchor structure, occupying units) by id
// and resolve them each tick — no *Unit / *BuildingTile is persisted here.
type zoneRuntime struct {
	Def       protocol.Zone
	Owner     string
	Progress  float64
	Contested bool
	// Capturing is true for the tick(s) on which a capture mechanic actively
	// advanced this zone's Progress toward a flip — i.e. the zone is currently
	// "being captured". Reset to false each tick (like Contested) and set by the
	// presence/claim handlers when they add to Progress. Read by
	// zoneCapturingLocked to gate capture-triggered enemy spawns. Mechanics
	// without a timed capture (clear/control_point) never set it.
	Capturing bool
	captureCfg any
	// captureCells is the membership set for the presence "capture sub-zone"
	// (protocol.Zone.CaptureCells). Empty ⇒ the whole zone is the capture region.
	// Built once at install; read by unitInCaptureRegionLocked.
	captureCells map[gridPoint]bool
	// locked is true when the zone is linked to a player's starting point
	// (protocol.Zone.LockedSpawnLabel). Locked zones start team-owned and are
	// skipped by the capture tick — they can never be captured or lost.
	locked bool
}

// zoneCaptureHandler is the registered contract for one capture mechanic.
// Isomorphic to objectiveHandler:
//
//   - parseConfig turns the raw capture JSON config into a typed struct.
//   - validate panics on semantic invariants at catalog load, naming the
//     offending map file + zone id.
//   - evaluate runs one tick for a capturable zone, mutating its control state.
//     The capturability (adjacency) gate is enforced by the handler via
//     s.zoneCapturableByLocked before any ownership flip — see zone_handlers.go.
type zoneCaptureHandler struct {
	parseConfig func(raw json.RawMessage) (any, error)
	validate    func(filename, zoneID string, cfg any)
	evaluate    func(s *GameState, rt *zoneRuntime, dt float64)
}

// zoneCaptureRegistry is the capture-type -> handler dispatch table, populated
// at package init by zone_handlers.go. Never mutated after init.
var zoneCaptureRegistry = map[string]zoneCaptureHandler{}

// registerZoneCapture adds a handler under typeKey. Panics on duplicate or a
// missing hook so a misconfigured mechanic fails fast at startup. Mirrors
// registerObjective.
func registerZoneCapture(typeKey string, h zoneCaptureHandler) {
	if _, dup := zoneCaptureRegistry[typeKey]; dup {
		panic("zone_defs: duplicate capture mechanic registration for type " + typeKey)
	}
	if h.parseConfig == nil || h.validate == nil || h.evaluate == nil {
		panic("zone_defs: capture mechanic for type " + typeKey + " is missing a required hook")
	}
	zoneCaptureRegistry[typeKey] = h
}

// GetZoneCaptureHandler returns the handler registered for typeKey.
func GetZoneCaptureHandler(typeKey string) (zoneCaptureHandler, bool) {
	h, ok := zoneCaptureRegistry[typeKey]
	return h, ok
}

// ListZoneCaptureTypes returns all registered capture-type keys sorted
// alphabetically. Stable across runs. Exposed for client-side editor schema
// discovery (the zone popup's capture-type selector).
func ListZoneCaptureTypes() []string {
	keys := make([]string, 0, len(zoneCaptureRegistry))
	for k := range zoneCaptureRegistry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseAndValidateZoneCapture applies the registry to one zone's capture config
// at catalog load. Panics (catalogs are static — a bad entry is a build error)
// naming the map file + zone id on: unknown type, unparseable config, or a
// failed handler validation. Returns the typed config for runtime install.
func parseAndValidateZoneCapture(filename, zoneID string, capture protocol.ZoneCapture) any {
	if capture.Type == "" {
		panic("catalog/maps/" + filename + ": zone " + zoneID + ": capture.type is required")
	}
	handler, ok := zoneCaptureRegistry[capture.Type]
	if !ok {
		panic("catalog/maps/" + filename + ": zone " + zoneID +
			": unknown capture type " + capture.Type +
			" (register a mechanic in zone_handlers.go init())")
	}
	cfg, err := handler.parseConfig(capture.Config)
	if err != nil {
		panic("catalog/maps/" + filename + ": zone " + zoneID +
			": invalid capture config: " + err.Error())
	}
	handler.validate(filename, zoneID, cfg)
	return cfg
}
