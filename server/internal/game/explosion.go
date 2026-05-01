package game

import (
	"fmt"
	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// EXPLOSIONS — transient AoE VFX
//
// Live for explosionDurationSeconds, then drop. Server queues an entry every
// time a perk fires an explosion (Marksman explosive_tips today, future
// perks tomorrow); the client renders an expanding orange-red circle that
// fades over its lifetime. No gameplay damage lives here — that's handled
// in the perk that queued the explosion. This is purely visual state.
// ═════════════════════════════════════════════════════════════════════════════

// explosionDurationSeconds is the on-screen lifetime of an explosion VFX.
// Tuning point — short enough to avoid cluttering the screen, long enough to
// register as a "boom" (~0.35s feels punchy in playtest).
const explosionDurationSeconds = 0.35

// Explosion is the server-side record. Tick decay lives in tickExplosionsLocked;
// once RemainingSeconds <= 0 the entry is dropped from s.Explosions.
type Explosion struct {
	ID               string
	OwnerUnitID      int
	OwnerPlayerID    string
	X, Y             float64
	Radius           float64
	Variant          string // "explosive_tips" today; future perks set their own
	TotalSeconds     float64
	RemainingSeconds float64
}

// queueExplosionLocked appends a one-shot explosion VFX. Damage from the
// caused-by perk is applied separately (e.g. fireExplosiveTipsLocked) — this
// helper is purely visual queueing.
func (s *GameState) queueExplosionLocked(attacker *Unit, x, y, radius float64, variant string) {
	if radius <= 0 {
		return
	}
	id := fmt.Sprintf("expl_%d", s.nextExplosionID)
	s.nextExplosionID++
	expl := &Explosion{
		ID:               id,
		X:                x,
		Y:                y,
		Radius:           radius,
		Variant:          variant,
		TotalSeconds:     explosionDurationSeconds,
		RemainingSeconds: explosionDurationSeconds,
	}
	if attacker != nil {
		expl.OwnerUnitID = attacker.ID
		expl.OwnerPlayerID = attacker.OwnerID
	}
	s.Explosions = append(s.Explosions, expl)
}

// tickExplosionsLocked decays the lifetime of every queued explosion and
// drops expired ones. Mirrors tickBannersLocked / tickTrapsLocked.
func (s *GameState) tickExplosionsLocked(dt float64) {
	if len(s.Explosions) == 0 {
		return
	}
	kept := s.Explosions[:0]
	for _, expl := range s.Explosions {
		expl.RemainingSeconds -= dt
		if expl.RemainingSeconds > 0 {
			kept = append(kept, expl)
		}
	}
	s.Explosions = kept
}

// snapshotExplosionsLocked builds the wire-format slice for the per-tick
// snapshot. Returns nil when there are no active explosions so the field is
// omitted from JSON entirely.
func (s *GameState) snapshotExplosionsLocked() []protocol.ExplosionSnapshot {
	if len(s.Explosions) == 0 {
		return nil
	}
	out := make([]protocol.ExplosionSnapshot, 0, len(s.Explosions))
	for _, expl := range s.Explosions {
		progress := 0.0
		if expl.TotalSeconds > 0 {
			progress = 1.0 - (expl.RemainingSeconds / expl.TotalSeconds)
			if progress < 0 {
				progress = 0
			} else if progress > 1 {
				progress = 1
			}
		}
		out = append(out, protocol.ExplosionSnapshot{
			ID:          expl.ID,
			OwnerUnitID: expl.OwnerUnitID,
			OwnerID:     expl.OwnerPlayerID,
			X:           expl.X,
			Y:           expl.Y,
			Radius:      expl.Radius,
			Variant:     expl.Variant,
			Progress:    progress,
		})
	}
	return out
}
