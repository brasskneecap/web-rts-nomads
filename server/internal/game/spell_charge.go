package game

import (
	"encoding/json"
	"sort"
)

// spell_charge.go implements the Arcane Charge → Arcane Missiles passive loop
// (arch-mage-spell-system). The Arch Mage is rewarded for actively spending
// mana: every mana point spent generates Arcane Charge, and once enough charge
// accrues the unit automatically fires a volley of Arcane Missiles at random
// in-range enemies, then resets its charge.
//
// The passive is entirely data-driven by a charge-fire AbilityDef (type
// "passive", ChargeRequired > 0) the unit knows — see arcane_missiles.json.
// Determinism: charge accrual is pure arithmetic; missile targeting draws from
// the seeded rngCombat stream over a sorted candidate list.
//
// ── composable migration ────────────────────────────────────────────────────
// arcane_missiles is the first non-cast, non-zone ability migrated to the
// composable (schemaVersion 2) model. Every prior migration kept its legacy
// runtime and had the executor action delegate into it (launch_projectile ->
// fireAbilityProjectileLocked, launch_vortex -> spawnArcaneOrbLocked); this
// one does the same: the charge loop, staggered pending queue, ID-based
// bookkeeping, and deterministic re-pick-at-launch targeting below are ALL
// UNCHANGED — they remain the runtime for both a legacy and a converted
// ability. What changes is WHERE the config comes from:
//
//   - chargeFireSpec is the resolved, effective config every function below
//     reads instead of a raw AbilityDef's fields. chargeFireSpecFor resolves
//     it from either a legacy def's flat fields (unchanged) or, for a
//     converted (SchemaVersion>=2) def, from the compiled Program's
//     charge_fire_volley action config — never from the converted def's own
//     (cleared, per ConvertLegacyAbility) flat fields.
//   - fireChargeFullLocked is the third RuntimeAbilityContext-building entry
//     point (alongside resolveAbilityProgramCastLocked for casts and
//     fireAbilityZoneTickLocked for zone ticks): when a converted ability
//     crosses its charge threshold, it fires the compiled on_charge_full
//     trigger through the SAME shared executor
//     (runProgramTriggersLocked) every other trigger uses, giving
//     TriggerOnChargeFull its first real producer AND consumer. The
//     trigger's one action (charge_fire_volley, registered below) does
//     nothing but enqueue the staggered volley — the hostile-in-range gate
//     that decides whether to fire at all runs in tickArcaneMissilesLocked,
//     BEFORE the trigger is fired, exactly where legacy's own gate ran
//     (queueArcaneMissileVolleyLocked's old hostile check, now hoisted one
//     level up as enqueueArcaneMissileVolleyLocked's caller's
//     responsibility). This keeps the "bank charge when no target" rule
//     intact without needing the action executor's per-action `[]int`
//     return value to carry a bool signal it was never designed to carry.
//   - The staggered, re-pick-at-launch bolt-by-bolt launch step
//     (launchDueArcaneMissilesLocked / launchArcaneMissileLocked) is
//     DELIBERATELY NOT decomposed into repeat/wait/select_targets actions:
//     wait's cross-tick semantics and "pick the target fresh at the moment
//     THIS bolt fires" would not survive being expressed that way (see the
//     composable-abilities-arcane-missiles investigation). It stays a
//     hard-coded tick-driven queue, reading its config via chargeFireSpecFor
//     exactly like the queue step does.
type chargeFireSpec struct {
	AbilityID         string
	ChargeRequired    float64
	ManaToChargeRatio float64
	MissileCount      int
	DamagePerMissile  int
	MissileDelayMs    float64
	Projectile        string
	ProjectileScale   float64
	ProjectileSpeed   float64
	DamageType        DamageType
	MinorDamage       bool
}

// chargeFireSpecFor resolves def's charge-fire passive configuration, if any.
// For a legacy (SchemaVersion<2) def this reads the flat mechanic fields
// directly (IsChargeFirePassive() gates it, same condition as always). For a
// converted (SchemaVersion>=2, Program!=nil) def it recovers the same
// magnitudes from the compiled on_charge_full trigger's charge_fire_volley
// action config instead — see compileChargeFireProgram /
// compileChargeFireAction (ability_compile.go). Pure; no lock needed.
func chargeFireSpecFor(def AbilityDef) (chargeFireSpec, bool) {
	if def.IsPassive() && def.ChargeRequired > 0 {
		return chargeFireSpec{
			AbilityID:         def.ID,
			ChargeRequired:    def.ChargeRequired,
			ManaToChargeRatio: def.ManaToChargeRatio,
			MissileCount:      def.MissileCount,
			DamagePerMissile:  def.DamagePerMissile,
			MissileDelayMs:    def.MissileDelayMs,
			Projectile:        def.Projectile,
			ProjectileScale:   def.ProjectileScale,
			ProjectileSpeed:   def.ProjectileSpeed,
			DamageType:        def.DamageType,
			MinorDamage:       def.MinorDamage,
		}, true
	}
	if def.SchemaVersion >= 2 && def.Program != nil {
		if cfg, ok := findChargeFireVolleyConfig(def.Program); ok {
			return chargeFireSpec{
				AbilityID:         def.ID,
				ChargeRequired:    cfg.ChargeRequired,
				ManaToChargeRatio: cfg.ManaToChargeRatio,
				MissileCount:      cfg.MissileCount,
				DamagePerMissile:  cfg.DamagePerMissile,
				MissileDelayMs:    cfg.MissileDelayMs,
				Projectile:        cfg.Projectile,
				ProjectileScale:   cfg.ProjectileScale,
				ProjectileSpeed:   cfg.ProjectileSpeed,
				DamageType:        def.DamageType, // survives conversion untouched (Identity field)
				MinorDamage:       cfg.MinorDamage,
			}, true
		}
	}
	return chargeFireSpec{}, false
}

// findChargeFireVolleyConfig walks prog's top-level triggers for an
// on_charge_full trigger with a charge_fire_volley action and decodes its
// config. Also the seam ability_defs.go's IsChargeFirePassive() uses to
// recognize a converted charge-fire ability by its Program's SHAPE (never by
// a cleared flat field) — see that method's doc comment.
func findChargeFireVolleyConfig(prog *AbilityProgram) (chargeFireVolleyConfig, bool) {
	if prog == nil {
		return chargeFireVolleyConfig{}, false
	}
	for _, trig := range prog.Triggers {
		if trig.Type != TriggerOnChargeFull {
			continue
		}
		for _, act := range trig.Actions {
			if act.Type != ActionChargeFireVolley {
				continue
			}
			var cfg chargeFireVolleyConfig
			decodeActionConfig(act.Config, &cfg)
			return cfg, true
		}
	}
	return chargeFireVolleyConfig{}, false
}

// unitChargeFirePassiveLocked returns the resolved charge-fire spec for the
// first charge-fire passive ability the unit knows (in ability order), if
// any. Caller holds s.mu.
func (s *GameState) unitChargeFirePassiveLocked(unit *Unit) (chargeFireSpec, bool) {
	if unit == nil {
		return chargeFireSpec{}, false
	}
	for _, id := range unit.Abilities {
		if def, ok := getAbilityDef(id); ok {
			if spec, ok := chargeFireSpecFor(def); ok {
				return spec, true
			}
		}
	}
	return chargeFireSpec{}, false
}

// accrueArcaneChargeLocked adds charge for `manaSpent` mana points, but only for
// a unit that owns a charge-fire passive. Called from spendUnitManaLocked so
// EVERY mana spend (spell casts, channel ticks) feeds the loop. Caller holds
// s.mu.
func (s *GameState) accrueArcaneChargeLocked(unit *Unit, manaSpent int) {
	if manaSpent <= 0 {
		return
	}
	spec, ok := s.unitChargeFirePassiveLocked(unit)
	if !ok {
		return
	}
	unit.ArcaneCharge += float64(manaSpent) * spec.ManaToChargeRatio
}

// arcaneMissileStaggerSeconds is the delay between successive bolts of one
// Arcane Missiles volley — they fire rat-a-tat rather than all at once for a
// better feel.
const arcaneMissileStaggerSeconds = 0.06

// pendingArcaneMissile is one queued bolt of a volley awaiting its staggered
// launch. The target is (re)picked at LAUNCH time so a bolt chases a live enemy
// rather than a stale one. IDs, never pointers (targeting invariant).
type pendingArcaneMissile struct {
	OwnerUnitID    int
	AbilityID      string
	DelayRemaining float64
}

// tickArcaneMissilesLocked drives the Arcane Missiles loop for the tick: it
// queues a fresh volley for any charged unit, then launches whichever queued
// bolts have finished their stagger. Charge is only consumed when there is at
// least one in-range enemy at trigger time — a full-but-idle Arch Mage banks
// its charge rather than wasting a volley. Iterates s.Units in slice order
// (deterministic). Caller holds s.mu write lock.
func (s *GameState) tickArcaneMissilesLocked(dt float64) {
	for _, unit := range s.Units {
		if unit == nil || unit.HP <= 0 {
			continue
		}
		spec, ok := s.unitChargeFirePassiveLocked(unit)
		if !ok || unit.ArcaneCharge < spec.ChargeRequired {
			continue
		}
		// Gate BEFORE firing: a full-but-idle unit with no in-range enemy banks
		// its charge instead of wasting a volley (moved here, one level above
		// enqueueArcaneMissileVolleyLocked, so the v2 on_charge_full trigger's
		// action can enqueue unconditionally — see the file doc comment).
		if len(s.hostilesInRangeSortedLocked(unit, unit.AttackRange)) == 0 {
			continue
		}
		s.fireChargeFullLocked(unit, spec)
		unit.ArcaneCharge -= spec.ChargeRequired // carry any overflow
	}
	s.launchDueArcaneMissilesLocked(dt)
}

// fireChargeFullLocked is the third RuntimeAbilityContext-building entry point
// (alongside resolveAbilityProgramCastLocked for casts and
// fireAbilityZoneTickLocked for zone ticks): a unit just crossed its charge
// threshold with a hostile in range (the caller already checked). For a
// converted (SchemaVersion>=2, Program!=nil) ability this fires the compiled
// on_charge_full trigger through the shared executor — the trigger's single
// charge_fire_volley action enqueues the staggered bolts via
// enqueueArcaneMissileVolleyLocked. For a legacy def (no Program to dispatch —
// kept alive only by a hand-built legacy fixture in tests; no shipped catalog
// ability is legacy-shaped after this migration) this enqueues directly,
// byte-for-byte the pre-migration behavior. Caller holds s.mu.
func (s *GameState) fireChargeFullLocked(unit *Unit, spec chargeFireSpec) {
	if def, ok := getAbilityDef(spec.AbilityID); ok && def.SchemaVersion >= 2 && def.Program != nil {
		ctx := &RuntimeAbilityContext{
			CasterID:    unit.ID,
			AbilityID:   def.ID,
			OwnerUnitID: unit.ID,
			program:     def.Program,
			abilityDef:  &def,
			Named:       map[string]ContextValue{},
			Trace:       s.previewTrace,
			now:         s.previewClock,
		}
		s.runProgramTriggersLocked(ctx, def.Program.Triggers, TriggerOnChargeFull)
		return
	}
	s.enqueueArcaneMissileVolleyLocked(unit, spec)
}

// enqueueArcaneMissileVolleyLocked appends MissileCount staggered pending
// bolts (0, stagger, 2*stagger, ...) unconditionally — callers are
// responsible for gating on hostile-in-range presence BEFORE calling this
// (tickArcaneMissilesLocked, for both the legacy direct call and the v2
// charge_fire_volley action's Execute below). Caller holds s.mu write lock.
func (s *GameState) enqueueArcaneMissileVolleyLocked(unit *Unit, spec chargeFireSpec) {
	if unit == nil {
		return
	}
	count := spec.MissileCount
	if count < 1 {
		count = 1
	}
	// Per-missile stagger: authored in ms on the ability, default when unset.
	stagger := arcaneMissileStaggerSeconds
	if spec.MissileDelayMs > 0 {
		stagger = spec.MissileDelayMs / 1000.0
	}
	for i := 0; i < count; i++ {
		s.pendingArcaneMissiles = append(s.pendingArcaneMissiles, pendingArcaneMissile{
			OwnerUnitID:    unit.ID,
			AbilityID:      spec.AbilityID,
			DelayRemaining: float64(i) * stagger,
		})
	}
}

// launchDueArcaneMissilesLocked decrements every queued bolt's stagger by dt and
// launches those that have come due (FIFO for determinism). Caller holds s.mu.
func (s *GameState) launchDueArcaneMissilesLocked(dt float64) {
	if len(s.pendingArcaneMissiles) == 0 {
		return
	}
	kept := s.pendingArcaneMissiles[:0]
	for _, pm := range s.pendingArcaneMissiles {
		pm.DelayRemaining -= dt
		if pm.DelayRemaining > 0 {
			kept = append(kept, pm)
			continue
		}
		s.launchArcaneMissileLocked(pm.OwnerUnitID, pm.AbilityID)
	}
	s.pendingArcaneMissiles = kept
}

// launchArcaneMissileLocked fires a single bolt at a random in-range enemy
// (duplicates allowed), reusing the ability-projectile spawn path so
// mitigation / death / threat / attribution / minor-damage rendering are all
// inherited. A dead owner, an unresolvable spec, or an empty target list
// simply fizzles the bolt. The shim def/eff below are built purely from the
// resolved spec (never a raw, possibly-cleared AbilityDef's own fields) — same
// discipline as launch_vortex's Execute. Caller holds s.mu write lock.
func (s *GameState) launchArcaneMissileLocked(ownerID int, abilityID string) {
	owner := s.getUnitByIDLocked(ownerID)
	if owner == nil || owner.HP <= 0 {
		return
	}
	def, ok := getAbilityDef(abilityID)
	if !ok {
		return
	}
	spec, ok := chargeFireSpecFor(def)
	if !ok {
		return
	}
	candidates := s.hostilesInRangeSortedLocked(owner, owner.AttackRange)
	if len(candidates) == 0 {
		return
	}
	target := candidates[s.rngCombat.Intn(len(candidates))]
	shimDef := AbilityDef{
		ID:              spec.AbilityID,
		Projectile:      spec.Projectile,
		ProjectileScale: spec.ProjectileScale,
		DamageType:      spec.DamageType,
		MinorDamage:     spec.MinorDamage,
	}
	// Carry the ability's projectileSpeed so missiles can be slowed via the
	// ability file (0 ⇒ the projectile def's own speed).
	s.fireAbilityProjectileLocked(owner, target, shimDef, EffectiveSpell{
		Damage:          spec.DamagePerMissile,
		ProjectileSpeed: spec.ProjectileSpeed,
	})
}

// hostilesInRangeSortedLocked returns the visible, living units hostile to
// `unit` within `radius` world-px of it, sorted by unit ID for deterministic
// random selection. Caller holds s.mu.
func (s *GameState) hostilesInRangeSortedLocked(unit *Unit, radius float64) []*Unit {
	if unit == nil || radius <= 0 {
		return nil
	}
	radSq := radius * radius
	out := make([]*Unit, 0, 8)
	for _, u := range s.Units {
		if u == nil || u.ID == unit.ID || u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(u.OwnerID, unit.OwnerID) {
			continue
		}
		dx := u.X - unit.X
		dy := u.Y - unit.Y
		if dx*dx+dy*dy > radSq {
			continue
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// charge_fire_volley action
// ─────────────────────────────────────────────────────────────────────────────

// chargeFireVolleyConfig is the compiled config for charge_fire_volley
// (arcane_missiles' auto-fire volley). Its Execute enqueues the staggered
// volley via enqueueArcaneMissileVolleyLocked using ONLY this config's
// fields — never a raw (post-conversion, cleared) AbilityDef's ChargeRequired/
// MissileCount/etc, mirroring launch_projectile/launch_vortex's "Config is
// the sole authority" discipline. Targeting/AllowDuplicateTargets are decoded
// for round-trip/schema completeness but not read by Execute, mirroring the
// legacy AbilityDef fields of the same name (decoded, never read — see
// ability_defs.go): targeting is hard-coded random-among-in-range and
// duplicate targets are always allowed, same as before migration.
type chargeFireVolleyConfig struct {
	ChargeRequired        float64    `json:"chargeRequired"`
	ManaToChargeRatio     float64    `json:"manaToChargeRatio,omitempty"`
	MissileCount          int        `json:"missileCount"`
	DamagePerMissile      int        `json:"damagePerMissile"`
	MissileDelayMs        float64    `json:"missileDelayMs,omitempty"`
	Projectile            string     `json:"projectile,omitempty"`
	ProjectileScale       float64    `json:"projectileScale,omitempty"`
	ProjectileSpeed       float64    `json:"projectileSpeed,omitempty"`
	Type                  DamageType `json:"type,omitempty"`
	MinorDamage           bool       `json:"minorDamage,omitempty"`
	Targeting             string     `json:"targeting,omitempty"`
	AllowDuplicateTargets bool       `json:"allowDuplicateTargets,omitempty"`
}

func (chargeFireVolleyConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionChargeFireVolley,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c chargeFireVolleyConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(chargeFireVolleyConfig)
			var out []ValidationIssue
			if c.ChargeRequired <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "charge_fire_volley requires chargeRequired > 0", Severity: "error"})
			}
			if c.MissileCount <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "charge_fire_volley requires missileCount > 0", Severity: "error"})
			}
			if c.Type != "" && !IsValidDamageType(c.Type) {
				out = append(out, ValidationIssue{Code: "invalid_damage_type", Message: "unknown damage type " + string(c.Type), Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "chargeRequired", Label: "Charge Required", Control: "number", Section: "Properties"},
			{Key: "manaToChargeRatio", Label: "Mana To Charge Ratio", Control: "number", Section: "Properties"},
			{Key: "missileCount", Label: "Missile Count", Control: "number", Section: "Properties"},
			{Key: "damagePerMissile", Label: "Damage Per Missile", Control: "number", Section: "Properties"},
			{Key: "missileDelayMs", Label: "Missile Delay (ms)", Control: "number", Section: "Timing"},
			{Key: "projectile", Label: "Projectile", Control: "asset", Section: "Presentation"},
			{Key: "projectileScale", Label: "Projectile Scale", Control: "number", Section: "Presentation"},
			{Key: "projectileSpeed", Label: "Travel Speed", Control: "number", Section: "Properties"},
			{Key: "type", Label: "Damage Type", Control: "enum", Section: "Properties"},
			{Key: "minorDamage", Label: "Minor Damage", Control: "boolean", Section: "Presentation"},
			{Key: "targeting", Label: "Targeting", Control: "text", Section: "Targeting"},
			{Key: "allowDuplicateTargets", Label: "Allow Duplicate Targets", Control: "boolean", Section: "Targeting"},
		}},
		// Execute only enqueues — the hostile-in-range gate already ran in
		// tickArcaneMissilesLocked before this trigger was fired (see that
		// function's doc comment), so this is unconditional.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(chargeFireVolleyConfig)
			unit := s.getUnitByIDLocked(ctx.CasterID)
			if unit == nil {
				return nil
			}
			spec := chargeFireSpec{
				AbilityID:      ctx.AbilityID,
				MissileCount:   c.MissileCount,
				MissileDelayMs: c.MissileDelayMs,
			}
			s.enqueueArcaneMissileVolleyLocked(unit, spec)
			ctx.trace("charge_volley_queued", ctx.currentActionPath, map[string]any{"missileCount": c.MissileCount})
			return nil
		},
	})
}
