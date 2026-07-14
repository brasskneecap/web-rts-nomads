package game

// ═════════════════════════════════════════════════════════════════════════════
// WIRED vs. INERT PERKS (spec §7.3)
//
// A perk's on-disk definition (catalog/units/<faction>/<unit>/paths/<path>/
// perks/<rank>.json) only carries data — display strings, tooltip tokens,
// config numbers. Its actual BEHAVIOR in a match comes from a Go handler
// that keys off the perk's id: a `case "<id>":` arm in one of the
// switch-based dispatchers (perks.go, perks_attack.go, perks_defense.go,
// perks_marksman.go, perks_trapper.go, perks_crit.go, perks_movement.go,
// perks_siphoner.go, perks_arch_mage.go, perks_cleric.go, perks_auras.go),
// or a direct `containsString(unit.PerkIDs, "<id>")` / `perkDefByID("<id>")`
// check in those same files or elsewhere in the sim (e.g. trap.go's
// increased_deployment, projectile.go/state_combat.go's pierce). perks_vision.go
// and perks_icons.go were also checked; perks_vision.go currently has no
// perk-keyed behavior at all (it's a Phase-1 stub returning a flat 1.0),
// and perks_icons.go's ids are the HUD/icon-selection dispatch (still a Go
// handler, still "wired" per the spec's broad definition below).
//
// Go switch labels and inline string-equality checks are not reflectable —
// there is no way to ask the compiler "which ids does this binary actually
// handle?" So this set is HAND-MAINTAINED: wiredPerkIDs enumerates every id
// that appears in a real Go handler (gameplay OR icon/HUD; the spec's
// definition of "wired" is "a Go handler exists for its id", not
// specifically a gameplay effect). It is scanned and rebuilt manually
// whenever perk-handler code changes.
//
// This hand-maintained set is a stopgap until the perk redesign (spec §7.4)
// makes perk behavior itself data-driven (e.g. an effect-descriptor system
// instead of a Go switch per id) — at that point "wired" becomes derivable
// from the catalog data directly and this file goes away.
//
// A newly authored perk (via the future editor's SaveEditorPerkPool) whose
// id is NOT in this set ships with Wired=false (see ListPerkDefs in
// perk_defs.go) — the UI is expected to label such a perk "inert": it will
// be granted to units and show up in the HUD, but nothing in the simulation
// currently reacts to a unit owning it.
var wiredPerkIDs = map[string]struct{}{
	// ─── soldier / vanguard (perks_defense.go, perks_icons.go) ───────────
	"hold_the_line":    {},
	"shield_bash":       {},
	"retaliation":       {},
	"interlock":         {},
	"reinforced_armor":  {},
	"brace":             {},
	"last_stand":        {},
	"punishing_guard":   {},
	"challengers_mark":  {}, // perks_attack.go
	"guardian_aura":     {}, // perks_auras.go
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
	"blood_sustain":   {},
	"blood_engine":    {}, // also perks_defense.go
	"whirlwind_core":  {}, // also perks.go

	// ─── archer / marksman (perks_marksman.go, perks_crit.go) ────────────
	"eagle_spirit":    {},
	"hawk_spirit":     {}, // perks_attack.go
	"vulture_spirit":  {},
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

	// ─── acolyte / cleric (perks_cleric.go, perks.go, perks_auras.go) ────
	"sanctuary":            {}, // perks_auras.go
	"battle_prayer":        {}, // perks.go
	"bolstering_prayer":    {}, // perks.go
	"mana_conduit":         {}, // perks_cleric.go
	"divine_aegis":         {}, // perks.go
	"divine_healer":        {}, // perks_cleric.go
	"restoration_aura":     {}, // perks.go
	"beacon_of_life":       {}, // perks_cleric.go
	"divine_intervention":  {}, // perks.go, perks_cleric.go
	"divine_judgement":     {}, // perks_cleric.go
	"zealous_march":        {}, // perks_cleric.go

	// ─── acolyte / siphoner (perks_siphoner.go) ──────────────────────────
	"lingering_hex":       {}, // also perks.go
	"mark_of_weakness":    {}, // also perks.go
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

// isWiredPerk reports whether a Go handler exists for the given perk id —
// see wiredPerkIDs' doc comment above for exactly what counts.
func isWiredPerk(id string) bool {
	_, ok := wiredPerkIDs[id]
	return ok
}
