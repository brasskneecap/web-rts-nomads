package game

import (
	"math"
	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// EFFECTS — generalized transient visual effects on units or world positions
//
// effectInstance is the server-side record. It advances each tick via
// tickEffectsLocked and is dropped when elapsed >= DurationTicks. The client
// receives a []EffectSnapshot each tick and drives its renderer from Progress.
//
// Lifecycle mirrors Explosion (explosion.go):
//   queueEffectLocked  — enqueue from a perk hook (caller holds s.mu write lock)
//   tickEffectsLocked  — advance progress, update fallback position, cull expired
//   effectSnapshotsLocked — build the per-tick wire slice for Snapshot()
//
// The tick rate is 20 Hz (loop.go: const dt = 1.0/20.0; ticker 50ms).
// All duration arithmetic uses the same 20.0 literal so the two stay in sync.
// ═════════════════════════════════════════════════════════════════════════════

// gameTicksPerSecond is the server tick rate. Matches loop.go (50ms ticker,
// dt = 1.0/20.0). Defined here rather than in loop.go because effects need it
// at queue-time; if the tick rate ever changes update loop.go AND this const.
const gameTicksPerSecond = 20.0

// effectInstance is the authoritative server-side record of one active effect.
type effectInstance struct {
	ID           int
	Name         string
	AnchorUnitID int     // 0 = world-anchored; effect lives at FallbackX/Y
	FallbackX    float64 // last known anchor position (used when anchor dies or is off-screen)
	FallbackY    float64
	StartTick    int // value of GameState.Tick at creation
	DurationTicks int
	SizeScale    float64
	Variant      string
	// Anchor is where the effect renders relative to its bound unit
	// (center/feet/head). Empty for perk-queued effects (queueEffectLocked
	// never sets it) ⇒ the client treats empty as center, preserving prior
	// behavior. playEffectOnUnitLocked sets it from the EffectDef.
	Anchor EffectAnchor
	// RequiresLiveAnchor drops the effect the moment its anchor stops being
	// alive (unitIsAliveLocked) instead of letting it play out at the death
	// position.
	//
	// The default (false) is right for a one-shot burst: an impact spark should
	// finish where the unit died. It is WRONG for a visual that represents an
	// ongoing STATE — burning is the flame of a status that no longer exists
	// once its host is gone, so a corpse kept burning for the rest of the
	// authored duration. Set by playEffectOnUnitForDurationLocked, whose only
	// caller is play_presentation's bindToStatusDuration path.
	//
	// Deliberately keyed on ALIVENESS, not on the unit being gone: once dying
	// units linger on the field as corpses, "the body is still there" must not
	// keep a state visual running.
	RequiresLiveAnchor bool
}

// queueEffectLocked spawns a transient visual effect anchored to a unit or
// a world position. Must be called under s.mu write lock.
//
//   - durationSeconds <= 0 defaults to 1.0
//   - sizeScale <= 0 defaults to 1.0
//   - anchorUnitID == 0 means no anchor — effect stays at the supplied X/Y
func (s *GameState) queueEffectLocked(name string, anchorUnitID int, fallbackX, fallbackY, sizeScale, durationSeconds float64, variant string) {
	if durationSeconds <= 0 {
		durationSeconds = 1.0
	}
	if sizeScale <= 0 {
		sizeScale = 1.0
	}
	s.nextEffectID++
	s.activeEffects = append(s.activeEffects, effectInstance{
		ID:            s.nextEffectID,
		Name:          name,
		AnchorUnitID:  anchorUnitID,
		FallbackX:     fallbackX,
		FallbackY:     fallbackY,
		StartTick:     s.Tick,
		DurationTicks: int(math.Round(durationSeconds * gameTicksPerSecond)),
		SizeScale:     sizeScale,
		Variant:       variant,
	})
}

// tickEffectsLocked advances the effect list by one tick:
//   - Refreshes FallbackX/Y from the anchor unit if it is still alive.
//   - Drops entries whose elapsed ticks have reached DurationTicks.
//
// Must be called under s.mu write lock. Mirrors tickExplosionsLocked.
func (s *GameState) tickEffectsLocked() {
	if len(s.activeEffects) == 0 {
		return
	}
	write := 0
	for read := range s.activeEffects {
		e := &s.activeEffects[read]
		elapsed := s.Tick - e.StartTick
		if elapsed >= e.DurationTicks {
			continue // expired — drop
		}
		// Keep fallback position current so the snapshot is accurate even when
		// the anchor was not in the last client frame.
		if e.AnchorUnitID != 0 {
			anchor := s.getUnitByIDLocked(e.AnchorUnitID)
			if s.unitIsAliveLocked(anchor) {
				e.FallbackX = anchor.X
				e.FallbackY = anchor.Y
			} else if e.RequiresLiveAnchor {
				continue // state-visual whose subject is gone — drop, don't play out
			}
			// Otherwise the anchor is gone but we keep the last-known
			// FallbackX/Y so the effect finishes at the position where the unit
			// died.
		}
		if write != read {
			s.activeEffects[write] = *e
		}
		write++
	}
	s.activeEffects = s.activeEffects[:write]
}

// effectSnapshotsLocked builds the wire-format slice for the per-tick snapshot.
// Returns nil when there are no active effects so the field is omitted from JSON.
// Must be called under s.mu (read or write) lock.
func (s *GameState) effectSnapshotsLocked() []protocol.EffectSnapshot {
	if len(s.activeEffects) == 0 {
		return nil
	}
	out := make([]protocol.EffectSnapshot, 0, len(s.activeEffects))
	for i := range s.activeEffects {
		e := &s.activeEffects[i]
		elapsed := s.Tick - e.StartTick
		progress := 0.0
		if e.DurationTicks > 0 {
			progress = float64(elapsed) / float64(e.DurationTicks)
			if progress < 0 {
				progress = 0
			} else if progress > 1 {
				progress = 1
			}
		}
		// Resolve current position from the anchor if available, else fallback.
		x, y := e.FallbackX, e.FallbackY
		if e.AnchorUnitID != 0 {
			if anchor := s.getUnitByIDLocked(e.AnchorUnitID); anchor != nil && anchor.HP > 0 {
				x = anchor.X
				y = anchor.Y
			}
		}
		out = append(out, protocol.EffectSnapshot{
			ID:           e.ID,
			Name:         e.Name,
			AnchorUnitID: e.AnchorUnitID,
			X:            x,
			Y:            y,
			Progress:     progress,
			SizeScale:    e.SizeScale,
			Variant:      e.Variant,
			Anchor:       string(e.Anchor),
		})
	}
	return out
}
