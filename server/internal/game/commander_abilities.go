package game

import (
	"sort"

	"webrts/server/pkg/protocol"
)

// Commander Abilities are player-level (NOT unit-level) abilities the player
// can cast from the action bar at any world position. They are simple,
// AoE-only, instant-cast, and cooldown-gated per player. They do not consume
// mana and do not require a unit selected.
//
// First pass: two abilities — Smite (AoE damage to enemies) and Blessing
// (AoE heal to allies). Cooldown of 60s each.
//
// Failure reason strings — mirror the castFail* pattern from ability_cast.go
// so the WS layer can pass them straight into NotificationMessage.Message.
const (
	commanderFailUnknownAbility = "Unknown commander ability."
	commanderFailOnCooldown     = "Ability is on cooldown."
)

// CommanderAbilityID is the canonical id of one of the player-level abilities.
type CommanderAbilityID string

const (
	CommanderAbilitySmite    CommanderAbilityID = "commander_smite"
	CommanderAbilityBlessing CommanderAbilityID = "commander_blessing"
)

// CommanderAbilityDef is the static definition of one commander ability.
// Damage / Heal are mutually exclusive in practice but both fields exist so
// future abilities (e.g. "smite that also heals allies in the same zone")
// drop in without a schema change.
type CommanderAbilityDef struct {
	ID          CommanderAbilityID
	DisplayName string
	// Icon id resolved by the client's action icon catalog. Kept separate
	// from ID so we can reuse existing artwork without renaming the ability.
	Icon string
	// Radius is the world-pixel AoE radius for the cast.
	Radius float64
	// CooldownSeconds is the wall-clock cooldown gating subsequent casts.
	CooldownSeconds float64
	// Damage > 0 ⇒ deal that much damage to every hostile unit in radius.
	Damage int
	// Heal > 0 ⇒ heal every friendly unit in radius by that amount.
	Heal int
	// EffectName is the transient VFX queued at the cast point (world-anchored,
	// not unit-anchored). Empty = no effect.
	EffectName string
	// EffectDurationSeconds is the lifetime of the world VFX. <=0 defaults to
	// 1.0 via queueEffectLocked.
	EffectDurationSeconds float64
}

var commanderAbilityRegistry = map[CommanderAbilityID]CommanderAbilityDef{
	CommanderAbilitySmite: {
		ID:                    CommanderAbilitySmite,
		DisplayName:           "Smite",
		Icon:                  "attack",
		Radius:                160,
		CooldownSeconds:       60,
		Damage:                150,
		EffectName:            "explosion",
		EffectDurationSeconds: 0.6,
	},
	CommanderAbilityBlessing: {
		ID:                    CommanderAbilityBlessing,
		DisplayName:           "Blessing",
		Icon:                  "heal",
		Radius:                160,
		CooldownSeconds:       60,
		Heal:                  150,
		EffectName:            "healing_glow",
		EffectDurationSeconds: 0.6,
	},
}

// orderedCommanderAbilities returns the ability defs in a stable order so the
// snapshot / action bar render the slots in the same positions every tick.
func orderedCommanderAbilities() []CommanderAbilityDef {
	ids := make([]string, 0, len(commanderAbilityRegistry))
	for id := range commanderAbilityRegistry {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	out := make([]CommanderAbilityDef, 0, len(ids))
	for _, id := range ids {
		out = append(out, commanderAbilityRegistry[CommanderAbilityID(id)])
	}
	return out
}

// getCommanderAbilityDef looks up an ability by id; ok==false on unknown.
func getCommanderAbilityDef(id string) (CommanderAbilityDef, bool) {
	def, ok := commanderAbilityRegistry[CommanderAbilityID(id)]
	return def, ok
}

// tickCommanderCooldownsLocked decays every player's commander-ability
// cooldown timers by dt seconds, removing entries that reach zero. Caller
// holds s.mu.
func (s *GameState) tickCommanderCooldownsLocked(dt float64) {
	for _, p := range s.Players {
		if p == nil || len(p.CommanderAbilityCooldowns) == 0 {
			continue
		}
		for id, remaining := range p.CommanderAbilityCooldowns {
			remaining -= dt
			if remaining <= 0 {
				delete(p.CommanderAbilityCooldowns, id)
				continue
			}
			p.CommanderAbilityCooldowns[id] = remaining
		}
	}
}

// commanderAbilitySnapshotsLocked builds the per-player wire snapshot of
// every commander ability with its live cooldown. Stable order (see
// orderedCommanderAbilities). Returns nil when the registry is empty.
func (s *GameState) commanderAbilitySnapshotsLocked(p *Player) []protocol.CommanderAbilitySnapshot {
	defs := orderedCommanderAbilities()
	if len(defs) == 0 {
		return nil
	}
	out := make([]protocol.CommanderAbilitySnapshot, 0, len(defs))
	for _, def := range defs {
		var remaining float64
		if p != nil && p.CommanderAbilityCooldowns != nil {
			remaining = p.CommanderAbilityCooldowns[string(def.ID)]
		}
		out = append(out, protocol.CommanderAbilitySnapshot{
			ID:                string(def.ID),
			DisplayName:       def.DisplayName,
			Icon:              def.Icon,
			Radius:            def.Radius,
			CooldownTotal:     def.CooldownSeconds,
			CooldownRemaining: remaining,
			Damage:            def.Damage,
			Heal:              def.Heal,
		})
	}
	return out
}

// RequestCastCommanderAbility is the public entry point for the WS layer.
// Validates the player + ability + cooldown, applies the AoE effect, and
// arms the cooldown. Returns (true,"") on success, (false,reason) on
// validation failure (reason maps directly into NotificationMessage).
func (s *GameState) RequestCastCommanderAbility(playerID, abilityID string, x, y float64) (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	def, ok := getCommanderAbilityDef(abilityID)
	if !ok {
		return false, commanderFailUnknownAbility
	}
	player, ok := s.Players[playerID]
	if !ok || player == nil {
		return false, commanderFailUnknownAbility
	}
	if player.CommanderAbilityCooldowns == nil {
		player.CommanderAbilityCooldowns = make(map[string]float64, 2)
	}
	if remaining, gated := player.CommanderAbilityCooldowns[string(def.ID)]; gated && remaining > 0 {
		return false, commanderFailOnCooldown
	}

	s.applyCommanderAbilityLocked(player, def, x, y)
	player.CommanderAbilityCooldowns[string(def.ID)] = def.CooldownSeconds
	return true, ""
}

// applyCommanderAbilityLocked is the shared effect application path. Caller
// must hold s.mu and have already validated the cooldown.
func (s *GameState) applyCommanderAbilityLocked(caster *Player, def CommanderAbilityDef, x, y float64) {
	radiusSq := def.Radius * def.Radius

	for _, u := range s.Units {
		if u == nil || u.HP <= 0 {
			continue
		}
		dx := u.X - x
		dy := u.Y - y
		if dx*dx+dy*dy > radiusSq {
			continue
		}

		switch {
		case def.Damage > 0 && s.playersAreHostileLocked(caster.ID, u.OwnerID):
			s.applyUnitDamageWithSourceLocked(u, def.Damage, DamageSource{
				Kind:       "commander_ability",
				Category:   DamageCategoryAbility,
				DamageType: DamagePhysical,
			})
		case def.Heal > 0 && s.playersAreFriendlyLocked(caster.ID, u.OwnerID):
			before := u.HP
			u.HP += def.Heal
			if u.HP > u.MaxHP {
				u.HP = u.MaxHP
			}
			gained := u.HP - before
			if gained > 0 {
				s.recordHealEventLocked(u, gained)
			}
		}
	}

	if def.EffectName != "" {
		s.queueEffectLocked(def.EffectName, 0, x, y, 1.0, def.EffectDurationSeconds, "")
	}

	// Drain any deaths the AoE produced so the post-cast snapshot reflects the
	// final state. Mirrors how ability_cast.go relies on the central death
	// queue: damage routes through applyUnitDamageWithSourceLocked which
	// enqueues, and we drain here since we're outside the tick's central
	// drainPendingDeathsLocked call.
	s.drainPendingDeathsLocked()
}
