package game

// ═════════════════════════════════════════════════════════════════════════════
// WIRED vs. INERT PERKS (spec §7.3)
//
// "Wired" means "this perk actually does something in a match." There are
// two independent ways a perk earns that: a Go handler exists for its id
// (this file's hand-maintained half), OR it carries typed data that the
// generic engine executes with zero perk-specific Go (perkHasTypedBehavior,
// below — StatModifiers / AbilityModifiers / AbilityRiders / GrantsAbilities).
// isWiredPerk is the OR of both. A perk with neither is genuinely inert: it
// will be granted to units and show up in the HUD, but nothing in the
// simulation reacts to a unit owning it.
//
// THE GO-HANDLER HALF (wiredPerkIDs, hand-maintained):
//
// A perk's on-disk definition (catalog/perks/<path>/<id>/<id>.json) only
// carries data — display strings, tooltip tokens, config numbers. Some
// perks' actual BEHAVIOR in a match still comes from a Go handler that keys
// off the perk's id: a `case "<id>":` arm in one of the switch-based
// dispatchers (perks.go, perks_attack.go, perks_defense.go,
// perks_marksman.go, perks_trapper.go, perks_crit.go, perks_movement.go,
// perks_siphoner.go, perks_arch_mage.go, perks_cleric.go, perks_auras.go,
// perks_icons.go's HUD/buff-icon dispatch), or a direct
// `containsString(unit.PerkIDs, "<id>")` / `perkDefByID("<id>")` check
// elsewhere in the sim (e.g. trap.go's increased_deployment,
// projectile.go/state_combat.go's pierce). perks_vision.go was also
// checked; it currently has no perk-keyed behavior at all (it's a Phase-1
// stub returning a flat 1.0).
//
// Go switch labels and inline string-equality checks are not reflectable —
// there is no way to ask the compiler "which ids does this binary actually
// handle?" So this set is HAND-MAINTAINED: wiredPerkIDs enumerates every id
// that appears in a real Go handler (gameplay OR icon/HUD). It is scanned
// and rebuilt manually whenever perk-handler code changes. An id should be
// REMOVED from this set once its last Go reference is deleted — the perk
// stays "wired" (or not) purely through perkHasTypedBehavior at that point;
// leaving a stale id in wiredPerkIDs is harmless for correctness (isWiredPerk
// is an OR) but misleads the next reader auditing "does X have a Go
// handler?"
//
// THE TYPED-DATA HALF (perkHasTypedBehavior, generic engine, no hand-maintenance):
//
// A perk with a non-empty StatModifiers / AbilityModifiers / AbilityRiders /
// GrantsAbilities list is executed unconditionally by generic engine code
// (unitPerkStatModifiersLocked, abilityScalarModifiersForCasterLocked,
// assignUnitPathAbilitiesLocked's ability-grant step, and eventually the
// AbilityRider execution runtime) with no per-id Go arm at all — adding a
// new perk that only uses these fields requires zero Go changes and is
// still correctly "wired". PerkDef.Effect is deliberately NOT part of this
// list: it is a bare visual-effect descriptor (name/target/scale/duration)
// that only fires because some OTHER Go handler arm calls
// applyPerkEffectLocked(def.Effect, ...) at the point it wants a proc VFX —
// see perks_attack.go's whirlwind_core case and perks_cleric.go's
// divine_judgement handler. A perk with only Effect set and no Go case
// referencing its id would never have that effect queued at all, so
// Effect alone must not count as typed behavior.
//
// This hand-maintained set is expected to keep shrinking as more perks
// migrate onto the typed-data fields above (spec §7.4) — the long-term goal
// is for it to become empty and this file to go away, with "wired" derived
// entirely from catalog data.
var wiredPerkIDs = map[string]struct{}{
	// ─── soldier / vanguard (perks_defense.go, perks_icons.go) ───────────
	"shield_bash":       {},
	"retaliation":       {},
	"interlock":         {},
	"reinforced_armor":  {},
	"brace":             {},
	"last_stand":        {},
	"punishing_guard":   {},
	"challengers_mark":  {}, // perks_attack.go
	"guardian_aura":     {}, // perks_icons.go (owner buff-icon case arm); flat/percent armor effect itself is now data-driven via PerkDef.Auras (perk_aura_stat_cache.go) — see perkHasTypedBehavior
	"pain_share":        {}, // perks_auras.go
	"rallying_banner":   {}, // perks.go

	// ─── soldier / berserker (perks_attack.go, perks_icons.go) ───────────
	"frenzy_core":     {},
	"bloodlust":       {},
	"relentless":      {},
	"savage_strikes":  {},
	"cleaving_rage":   {},
	"momentum":        {}, // also perks.go / perks_crit.go / perks_movement.go
	"executioner":     {},
	"berserk_state":   {},
	// blood_sustain is NOT listed here — it has no remaining Go handler arm
	// (removed in the on_damage_dealt migration, perks_attack.go). It is now
	// wired purely via perkHasTypedBehavior (PerkDef.GrantsAbilities is
	// non-empty — see catalog/perks/berserker/blood_sustain).
	"blood_engine": {}, // also perks_defense.go
	"whirlwind_core":  {}, // also perks.go

	// ─── archer / marksman (perks_marksman.go, perks_crit.go) ────────────
	// hawk_spirit and vulture_spirit are NOT listed: both were fully
	// migrated to StatModifiers (task 1e) and audited to have zero
	// remaining Go references anywhere in this package, including
	// perks_icons.go's buff-icon switch — they are wired purely via
	// perkHasTypedBehavior now. See perk_wired_test.go for the regression
	// guard.
	"eagle_spirit":    {},
	"bullseye":        {},
	"split_shot":      {},
	"pierce":          {}, // projectile.go, state_combat.go
	"hunters_mark":    {},
	"double_shot":     {}, // also perks.go
	"explosive_tips":  {},

	// ─── archer / trapper (perks_trapper.go) ─────────────────────────────
	"caltrops":              {}, // also perks.go, perks_icons.go
	"fire_pit":              {}, // also perks.go, perks_icons.go
	"explosive_trap":        {}, // also perks.go, perks_icons.go
	"marker_trap":           {}, // also perks.go, perks_icons.go
	"extended_setup":        {},
	"wider_nets":            {},
	"rapid_deployment":      {},
	"amplified_effects":     {},
	"explosive_chain":       {},
	"barbed_field":          {},
	"exposed_weakness":      {},
	"lasting_flames":        {},
	"ascendant_infusion":    {},
	"overload_protocol":     {},
	"increased_deployment":  {}, // trap.go

	// ─── acolyte / cleric (perks_cleric.go, perks.go, perks_icons.go) ────
	"sanctuary":            {}, // perks_icons.go (owner buff-icon case arm); projectile-damage-reduction effect itself is now data-driven via PerkDef.Auras (perk_aura_stat_cache.go) + the src.Kind=="projectile" gate at perks_defense.go's fold site — see perkHasTypedBehavior
	"battle_prayer":        {}, // perks.go
	"bolstering_prayer":    {}, // perks.go
	"mana_conduit":         {}, // perks_icons.go (owner buff-icon case arm); mana-regen bonus itself is now data-driven via PerkDef.Auras (perk_aura_stat_cache.go) — see perkHasTypedBehavior
	"divine_aegis":         {}, // perks.go
	"divine_healer":        {}, // perks_cleric.go
	"restoration_aura":     {}, // perks.go
	"beacon_of_life":       {}, // perks_cleric.go
	"divine_intervention":  {}, // perks.go, perks_cleric.go
	"divine_judgement":     {}, // perks_cleric.go
	"zealous_march":        {}, // perks_icons.go (owner buff-icon case arm); move-speed effect itself is now data-driven via PerkDef.Auras (perk_aura_stat_cache.go) — see perkHasTypedBehavior

	// ─── acolyte / siphoner (perks_siphoner.go) ──────────────────────────
	"lingering_hex":       {}, // also perks.go
	// mark_of_weakness is NOT listed here anymore: its last Go handler arm
	// (the hand-wired debuff-icon case in activeDebuffIconsLocked,
	// perks_icons.go) was deleted once the ability's own apply_status action
	// started authoring icon:"debuff-mark-of-weakness"/iconKind:"debuff"
	// directly (generic authored-status icon emission, same file). It is now
	// wired purely via perkHasTypedBehavior (PerkDef.GrantsAbilities is
	// non-empty) — see perk_wired_test.go for the regression guard.
	"soul_leech":          {},
	"withering_beam":      {},
	"chain_siphon":        {},
	"dark_renewal":        {},
	"beam_mastery":        {},
	"ascended_corruption": {},
	"amplify_damage":      {}, // also perks.go
	"shared_suffering":    {},
	"repurposed_life":     {},

	// ─── adept / arch_mage (perks_arch_mage.go) ──────────────────────────
	"arcane_feedback":  {}, // also perks_attack.go
	"arcane_conduit":   {},
	"unstable_magic":   {},
}

// perkHasTypedBehavior reports whether def carries data the generic engine
// executes with no perk-specific Go — the data-driven half of "wired" (see
// this file's top-of-file doc comment). True when any of StatModifiers /
// AbilityModifiers / AbilityRiders / GrantsAbilities / Auras is non-empty.
// Auras counts here even though zealous_march (the only shipped perk using
// it today) also keeps a hand-maintained wiredPerkIDs entry for its
// buff-icon case arm — a FUTURE aura-only perk with no icon/Go case would
// need this to avoid a false "inert" badge, the same reasoning that put
// StatModifiers here for hold_the_line.
// PerkDef.Effect deliberately does NOT count — see the doc comment above
// for why a bare visual-effect descriptor isn't independently executable.
func perkHasTypedBehavior(def PerkDef) bool {
	auras := false
	for _, a := range def.Auras {
		if len(a.StatModifiers) > 0 {
			auras = true
			break
		}
	}
	return len(def.StatModifiers) > 0 ||
		len(def.AbilityModifiers) > 0 ||
		len(def.AbilityRiders) > 0 ||
		len(def.GrantsAbilities) > 0 ||
		auras
}

// isWiredPerk reports whether def actually does something in a match: a Go
// handler exists for its id (wiredPerkIDs) OR it carries typed data the
// generic engine executes (perkHasTypedBehavior). See this file's top-of-file
// doc comment for exactly what counts on each side.
func isWiredPerk(def PerkDef) bool {
	_, hasGoHandler := wiredPerkIDs[def.ID]
	return hasGoHandler || perkHasTypedBehavior(def)
}
