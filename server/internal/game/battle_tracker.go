package game

import (
	"math"
	"sort"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// BATTLE TRACKER — DEBUG TELEMETRY
//
// Per-match accumulator of damage dealt and kills, bucketed by (player, source
// kind, source subtype). Armed only when MapConfig.Debug.BattleTracker is true
// so production maps pay no cost.
//
// SOURCE BUCKETS
//   kind="unit"     subtype=unit type        (e.g. "archer", "worker")
//   kind="trap"     subtype=trap type        (e.g. "caltrops", "fire_pit")
//   kind="building" subtype=building type    (e.g. "tower", "barracks")
//
// NPC ENEMIES
//   Wave enemies carry OwnerID == enemyPlayerID ("__enemy__"). Their damage
//   rolls up under that same player ID so the client can render them as a
//   distinct row.
//
// CALL SITES (tag each applyUnitDamageLocked call):
//   - state_combat.go  unit-on-unit attack    → battleSourceFromUnit
//   - state_combat.go  building-on-unit       → battleSourceFromBuilding
//   - trap.go          trap DoT / blast       → battleSourceFromTrap
//   - trap.go          lasting_flames burn    → battleSourceFromBurn
//   - trap.go          reactive/expiry/final  → battleSourceFromTrap
//   - perks_attack.go  secondary hits         → battleSourceFromUnit
//
// ADD NEW SOURCES HERE when introducing a new damage kind (e.g. a projectile
// aura). The tracker silently no-ops when disabled, so new call sites can be
// added unconditionally.
// ═════════════════════════════════════════════════════════════════════════════

// BattleSource describes the attacker lane of a damage event. Construct via
// the battleSourceFromXxx helpers at the call site; pass the zero value for
// "unattributed" damage (the tracker will skip it).
type BattleSource struct {
	PlayerID string
	Kind     string // "unit" | "trap" | "building"
	Subtype  string
}

// battleBucketKey is the composite key used inside the tracker's per-player
// map. A three-tuple keeps buckets orthogonal (an "archer" unit and a
// hypothetical "archer" trap would not collide).
type battleBucketKey struct {
	Kind    string
	Subtype string
}

type battleBucketStats struct {
	DamageDealt int
	Kills       int
}

// battleTrackerPlayer holds all buckets for one player. Buckets accumulate in
// a map for O(1) updates; the snapshot builder serializes them to a sorted
// slice for deterministic wire output.
type battleTrackerPlayer struct {
	Buckets map[battleBucketKey]*battleBucketStats
	Total   battleBucketStats
}

// battleCombatEventCap bounds the forensic combat-event log so a long debug
// session can't grow the per-match tracker (and the snapshot it ships each
// tick) without limit. Only the most recent entries are retained.
const battleCombatEventCap = 2000

// BattleTracker owns the running totals for one match. Methods are
// Locked-suffixed and must be called under GameState.mu write lock.
type BattleTracker struct {
	enabled        bool
	elapsedSeconds float64
	players        map[string]*battleTrackerPlayer
	// combatEvents is a bounded, append-only forensic log of individual landed
	// hits (attacker + target positions, distance, range, lethality). Populated
	// by recordBattleCombatEventLocked at the damage choke point; capped to the
	// most recent battleCombatEventCap entries. Stored directly in wire form so
	// the snapshot builder just copies the slice.
	combatEvents []protocol.BattleCombatEvent
}

// newBattleTracker constructs a tracker with its on/off flag set from the
// map's debug config. A disabled tracker is still allocated so call sites can
// unconditionally invoke track* without a nil check.
func newBattleTracker(enabled bool) *BattleTracker {
	return &BattleTracker{
		enabled: enabled,
		players: make(map[string]*battleTrackerPlayer),
	}
}

// tickLocked advances the elapsed-time counter. Called from GameState.Update
// each tick regardless of whether the tracker is enabled; the cost is a float
// add when disabled.
func (t *BattleTracker) tickLocked(dt float64) {
	if t == nil || !t.enabled {
		return
	}
	t.elapsedSeconds += dt
}

// getOrCreatePlayerLocked returns (and lazily creates) the bucket map for
// playerID. Returns nil when the tracker is disabled so callers can cheaply
// short-circuit without a bucket allocation.
func (t *BattleTracker) getOrCreatePlayerLocked(playerID string) *battleTrackerPlayer {
	p, ok := t.players[playerID]
	if !ok {
		p = &battleTrackerPlayer{Buckets: make(map[battleBucketKey]*battleBucketStats)}
		t.players[playerID] = p
	}
	return p
}

// ─────────────────────────────────────────────────────────────────────────────
// GameState-level accessors — the public surface used by damage call sites.
// ─────────────────────────────────────────────────────────────────────────────

// trackBattleDamageLocked records `damage` dealt by `src` against `target`.
// No-op when the tracker is disabled, when damage is non-positive, or when
// src has no PlayerID (unattributed damage is silently dropped rather than
// being rolled up under a phantom bucket).
//
// Must be called under s.mu write lock.
func (s *GameState) trackBattleDamageLocked(src BattleSource, _ *Unit, damage int) {
	if s.battleTracker == nil || !s.battleTracker.enabled {
		return
	}
	if damage <= 0 || src.PlayerID == "" || src.Kind == "" {
		return
	}
	player := s.battleTracker.getOrCreatePlayerLocked(src.PlayerID)
	key := battleBucketKey{Kind: src.Kind, Subtype: src.Subtype}
	bucket, ok := player.Buckets[key]
	if !ok {
		bucket = &battleBucketStats{}
		player.Buckets[key] = bucket
	}
	bucket.DamageDealt += damage
	player.Total.DamageDealt += damage
}

// trackBattleKillLocked records a kill credited to `src` against `target`.
// Must be called after the kill is detected (typically right after the damage
// application that dropped the target to HP <= 0). Silently no-ops when the
// tracker is disabled.
//
// Must be called under s.mu write lock.
func (s *GameState) trackBattleKillLocked(src BattleSource, _ *Unit) {
	if s.battleTracker == nil || !s.battleTracker.enabled {
		return
	}
	if src.PlayerID == "" || src.Kind == "" {
		return
	}
	player := s.battleTracker.getOrCreatePlayerLocked(src.PlayerID)
	key := battleBucketKey{Kind: src.Kind, Subtype: src.Subtype}
	bucket, ok := player.Buckets[key]
	if !ok {
		bucket = &battleBucketStats{}
		player.Buckets[key] = bucket
	}
	bucket.Kills++
	player.Total.Kills++
}

// recordBattleCombatEventLocked appends a forensic combat-event record for a
// single landed hit. No-op unless the battle tracker is armed (map debug flag)
// and the damage came from a unit attacker (src.AttackerUnitID != 0) — trap /
// building / unattributed damage is skipped to keep the log focused on
// unit-vs-unit combat, which is where range/target questions arise. Captures
// both units' positions at the instant damage applied, plus the attacker's
// AttackRange and the center-to-center distance, so an out-of-range or
// wrong-target hit is visible directly in a saved log.
//
// Called from applyUnitDamageWithSourceLocked at the HP-loss point, where
// `damage` is the post-mitigation HP loss and target.HP has already been
// decremented (so target.HP <= 0 correctly marks a lethal hit).
//
// Must be called under s.mu write lock.
func (s *GameState) recordBattleCombatEventLocked(target *Unit, damage int, src DamageSource) {
	if s.battleTracker == nil || !s.battleTracker.enabled {
		return
	}
	if target == nil || damage <= 0 || src.AttackerUnitID == 0 {
		return
	}
	ev := protocol.BattleCombatEvent{
		Tick:           s.Tick,
		ElapsedSeconds: s.battleTracker.elapsedSeconds,
		AttackerID:     src.AttackerUnitID,
		TargetID:       target.ID,
		TargetType:     target.UnitType,
		TargetOwner:    target.OwnerID,
		TargetX:        target.X,
		TargetY:        target.Y,
		Damage:         damage,
		Kind:           src.Kind,
		Lethal:         target.HP <= 0,
	}
	// Resolve the attacker for its position + range. May be nil when a
	// projectile outlives its firer — the event still records target-side data
	// and a zero attacker position (Distance stays 0, AttackRange 0) rather than
	// being dropped.
	if attacker := s.getUnitByIDLocked(src.AttackerUnitID); attacker != nil {
		ev.AttackerType = attacker.UnitType
		ev.AttackerOwner = attacker.OwnerID
		ev.AttackerX = attacker.X
		ev.AttackerY = attacker.Y
		ev.AttackRange = attacker.AttackRange
		dx := target.X - attacker.X
		dy := target.Y - attacker.Y
		ev.Distance = math.Sqrt(dx*dx + dy*dy)
		// Disambiguate the label. resolveAttackHitLocked tags both melee swings
		// and landed projectiles as "melee" in the DamageSource, so refine the
		// generic "melee" using the attacker's combat profile — a ranged unit's
		// hit is a "projectile", which matters when reading "what killed them"
		// (and keeps the dist-vs-range read honest: a projectile legitimately
		// lands beyond the firer's current distance, a melee swing should not).
		if ev.Kind == "melee" && !resolveCombatProfile(attacker).Melee {
			ev.Kind = "projectile"
		}
	}
	t := s.battleTracker
	t.combatEvents = append(t.combatEvents, ev)
	if len(t.combatEvents) > battleCombatEventCap {
		// Trim to the most recent cap entries. Overlapping copy into the front
		// of the same backing array is safe here (dst index < src index for
		// every element), and reuses the allocation.
		t.combatEvents = append(t.combatEvents[:0], t.combatEvents[len(t.combatEvents)-battleCombatEventCap:]...)
	}
}

// battleTrackerSnapshotLocked serializes the tracker into the wire format for
// inclusion in MatchSnapshotMessage. Returns nil when the tracker is disabled
// so the field is omitted from the JSON entirely.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) battleTrackerSnapshotLocked() *protocol.BattleTrackerSnapshot {
	if s.battleTracker == nil || !s.battleTracker.enabled {
		return nil
	}

	players := make([]protocol.BattlePlayerStats, 0, len(s.battleTracker.players))
	for playerID, p := range s.battleTracker.players {
		buckets := make([]protocol.BattleBucket, 0, len(p.Buckets))
		for key, stats := range p.Buckets {
			buckets = append(buckets, protocol.BattleBucket{
				Kind:    key.Kind,
				Subtype: key.Subtype,
				Stats: protocol.BattleStats{
					DamageDealt: stats.DamageDealt,
					Kills:       stats.Kills,
				},
			})
		}
		// Deterministic bucket ordering: kind first, then subtype. The client
		// renders them as-delivered so this is what ends up on screen.
		sort.Slice(buckets, func(i, j int) bool {
			if buckets[i].Kind != buckets[j].Kind {
				return buckets[i].Kind < buckets[j].Kind
			}
			return buckets[i].Subtype < buckets[j].Subtype
		})
		players = append(players, protocol.BattlePlayerStats{
			PlayerID: playerID,
			Buckets:  buckets,
			Total: protocol.BattleStats{
				DamageDealt: p.Total.DamageDealt,
				Kills:       p.Total.Kills,
			},
		})
	}
	// Stable player ordering for stable UI row order.
	sort.Slice(players, func(i, j int) bool {
		return players[i].PlayerID < players[j].PlayerID
	})

	// Copy the forensic combat-event log into the wire snapshot. Copied (not
	// aliased) so a later append/trim on the live tracker can't mutate a
	// snapshot the transport layer is still serializing.
	var combatEvents []protocol.BattleCombatEvent
	if n := len(s.battleTracker.combatEvents); n > 0 {
		combatEvents = make([]protocol.BattleCombatEvent, n)
		copy(combatEvents, s.battleTracker.combatEvents)
	}

	return &protocol.BattleTrackerSnapshot{
		ElapsedSeconds: s.battleTracker.elapsedSeconds,
		Players:        players,
		CombatEvents:   combatEvents,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Source construction helpers — keep call sites terse.
// ─────────────────────────────────────────────────────────────────────────────

// battleSourceFromUnit builds the BattleSource tag for a damage event whose
// attacker is a Unit (including wave NPCs). Returns the zero value for nil
// units so the call site can still pass it through without a guard.
func battleSourceFromUnit(u *Unit) BattleSource {
	if u == nil {
		return BattleSource{}
	}
	return BattleSource{PlayerID: u.OwnerID, Kind: "unit", Subtype: u.UnitType}
}

// battleSourceFromTrap builds the BattleSource tag for damage originating
// from a trap (DoT, blast, expiry effect, etc). Uses the trap's snapshotted
// owner fields so damage is still attributed when the owner unit has died.
func battleSourceFromTrap(t *Trap) BattleSource {
	if t == nil {
		return BattleSource{}
	}
	return BattleSource{PlayerID: t.OwnerPlayerID, Kind: "trap", Subtype: t.TrapType}
}

// battleSourceFromBuilding builds the BattleSource tag for damage dealt by a
// building (e.g. defensive tower). Accepts the concrete BuildingTile value
// since that is what state_combat.go iterates over.
func battleSourceFromBuilding(b *protocol.BuildingTile) BattleSource {
	if b == nil || b.OwnerID == nil {
		return BattleSource{}
	}
	return BattleSource{PlayerID: *b.OwnerID, Kind: "building", Subtype: b.BuildingType}
}
