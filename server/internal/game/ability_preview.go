package game

import (
	"fmt"
	"sync/atomic"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// RunAbilityPreview (Phase 6a, Task 2)
//
// RunAbilityPreview is a deterministic, server-only harness for the (Task 3)
// ability-editor preview endpoint: it runs a candidate ability's composable
// program end-to-end inside a fresh, isolated GameState with the executor's
// trace hook (GameState.previewTrace / previewClock, Phase 6a Task 1) turned
// on, steps the sim for the requested duration, and reports the resulting
// execution trace plus a compact per-scene-unit HP-before/after summary.
//
// It never touches a live match's GameState and never persists anything —
// the ability is registered into the runtimeAbilities overlay only for the
// duration of the call, under a per-call UNIQUE id (nextPreviewAbilityID;
// this matters for concurrent callers — see its doc comment), and removed
// via defer before returning.
// ═════════════════════════════════════════════════════════════════════════════

// previewIDCounter is a process-wide monotonic counter used to mint a
// UNIQUE runtimeAbilities registration id for every RunAbilityPreview call
// — see nextPreviewAbilityID. Accessed only via atomic.AddInt64.
var previewIDCounter int64

// nextPreviewAbilityID mints a registration id for one RunAbilityPreview
// call, distinct from req.Ability.ID and from every other call's id
// (including a concurrent one). This is required, not just a nicety: the
// runtimeAbilities overlay is a single package-level map shared by every
// concurrent caller (the eventual HTTP preview endpoint serves requests
// concurrently), while each call's GameState is otherwise fully isolated.
// A FIXED registration id would let two overlapping previews collide on
// that one shared map entry — call B's register/defer-delete could
// overwrite or remove call A's program mid-run, so A's caster (which
// re-resolves its ability by id via getAbilityDef on every cast/zone-tick)
// could end up executing B's program, or find its ability gone entirely
// once B returns. A per-call unique id makes the two runs share nothing
// after this point, so no ordering between concurrent calls matters.
func nextPreviewAbilityID() string {
	return fmt.Sprintf("__ability_preview_%d__", atomic.AddInt64(&previewIDCounter, 1))
}

// previewTickDT is the fixed simulation step RunAbilityPreview drives
// GameState.Update by. A fixed, small dt (matching the package's own
// tick-driven test helpers) keeps the harness deterministic and its
// resolution independent of req.DurationSeconds.
const previewTickDT = 0.05

// previewMaxTicks bounds a single preview run's simulated duration (400
// ticks * 0.05s = 20s of simulated time) so a large/malformed
// DurationSeconds in a request can't hang the eventual HTTP handler. It also
// bounds PreviewResult.Frames memory/response size: one wire snapshot is
// captured per tick (plus the initial frame), ~8KB each for a small scene, so
// a full run is ~(previewMaxTicks+1) frames ⇒ a few MB — the dominant
// contributor to the preview response. Decimate/gzip at the handler if that
// grows problematic.
const previewMaxTicks = 400

// previewCasterOwner / previewEnemyOwner are the two synthetic player ids
// RunAbilityPreview spawns into its isolated GameState: the caster's side
// (team 0) and the hostile side (team 1) requested scene units land on.
const (
	previewCasterOwner = "preview_caster"
	previewEnemyOwner  = "preview_enemy"
)

// previewCasterUnitType, previewEnemySceneUnitType, and
// previewAllySceneUnitType are the catalog unit types the preview harness
// spawns its scene units as. They are split by role — rather than one
// fixed type for everything — purely so the ability-editor's preview
// canvas renders the caster, its allies, and the hostile side as visually
// distinct silhouettes ("Adept" vs "Raider"); RunAbilityPreview overwrites
// every spawned unit's combat stats immediately after spawn (HP/MaxHP from
// the request, MoveSpeed/Damage zeroed, etc. — see the spawn loop below),
// so none of these three catalog defs' own numbers leak into a preview
// result. All three are plain units with no perks/abilities/passives of
// their own to interfere with the ability under test.
const (
	previewCasterUnitType     = "adept"
	previewEnemySceneUnitType = "raider"
	previewAllySceneUnitType  = "soldier"
)

// PreviewSceneUnit is one scene-placed unit (ally or enemy, relative to the
// caster) a preview request spawns before casting the ability under test.
type PreviewSceneUnit struct {
	Team  string  `json:"team"` // "ally" | "enemy"
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	HP    int     `json:"hp"`
	MaxHP int     `json:"maxHp"`
}

// PreviewRequest describes one ability-preview run: the ability under test,
// a deterministic seed, the caster's position, the scene it casts into, how
// the cast is aimed (a scene-unit index for a unit-target ability, or a
// world point for a point-target one), and how long to step the sim
// afterward.
type PreviewRequest struct {
	Ability AbilityDef         `json:"ability"`
	Seed    int64              `json:"seed"`
	CasterX float64            `json:"casterX"`
	CasterY float64            `json:"casterY"`
	Units   []PreviewSceneUnit `json:"units"`
	Target  int                `json:"target"` // index into Units for a unit-target cast; -1 = none
	CastX   float64            `json:"castX"`
	CastY   float64            `json:"castY"`
	// CasterCharge seeds the caster's Arcane Charge before the sim steps, so a
	// charge-fire passive (arcane_missiles: a positive charge threshold, no
	// castable action) can be previewed — set it at or above the ability's
	// chargeRequired and the passive auto-fires a volley on the first tick that
	// finds a hostile in range. Ignored for any ability that isn't a charge-fire
	// passive (the charge is simply never read). Default 0.
	CasterCharge    float64 `json:"casterCharge"`
	DurationSeconds float64 `json:"durationSeconds"`
	// ConditionalOverrides forces named `conditional` actions to a fixed
	// outcome, keyed by the conditional's authored action id: true takes THEN,
	// false takes ELSE, and an id that isn't present evaluates normally. Any
	// number of conditionals can be overridden independently in one run.
	//
	// This is a TESTING affordance, not a simulation input: the synthetic
	// preview caster owns no perks, items or advancements, so a has_perk branch
	// would otherwise always resolve false and its THEN side could never be
	// previewed. An id that matches no conditional is silently ignored — the
	// author may have renamed or deleted the node since the checkbox was set.
	ConditionalOverrides map[string]bool `json:"conditionalOverrides,omitempty"`
}

// PreviewUnitResult is one scene unit's HP before/after the preview run,
// indexed identically to PreviewRequest.Units.
type PreviewUnitResult struct {
	Index    int    `json:"index"`
	Team     string `json:"team"`
	HPBefore int    `json:"hpBefore"`
	HPAfter  int    `json:"hpAfter"`
}

// PreviewFrame is one captured tick of RunAbilityPreview's simulation: the
// full unfiltered wire snapshot (GameState.snapshotUnfilteredLocked — the
// exact shape the live client renders, including Units, Projectiles, Beams,
// Effects, Zones, ...) alongside the tick index and simulated time it was
// taken at. Frames let the ability editor scrub/replay a preview run instead
// of only seeing the before/after HP summary in PreviewUnitResult.
type PreviewFrame struct {
	Tick     int                           `json:"tick"`
	Time     float64                       `json:"t"`
	Snapshot protocol.MatchSnapshotMessage `json:"snapshot"`
}

// PreviewResult is RunAbilityPreview's report: the full, execution-ordered
// trace, per-scene-unit HP deltas, mana spent by the caster, whether the
// compiled program is fully executor-runnable, honest degradation
// warnings (never nil), a per-tick snapshot timeline (Frames), and — if the
// cast itself could not be initiated (bad target, insufficient mana, out of
// range, ...) — the human-readable failure reason.
type PreviewResult struct {
	Trace           []AbilityExecutionTraceEvent `json:"trace"`
	Units           []PreviewUnitResult          `json:"units"`
	CasterManaSpent int                          `json:"casterManaSpent"`
	Runnable        bool                         `json:"runnable"`
	Warnings        []string                     `json:"warnings"`
	Frames          []PreviewFrame               `json:"frames"`
	Error           string                       `json:"error,omitempty"`
}

// previewSceneEntry tracks a spawned scene unit's id (never a *Unit — see
// AI_RULES.md's target-by-ID invariant; this struct outlives the spawn call
// and is resolved fresh at collection time) alongside the bookkeeping
// needed to build its PreviewUnitResult.
type previewSceneEntry struct {
	unitID   int
	team     string
	hpBefore int
}

// RunAbilityPreview runs req.Ability's composable program end-to-end inside
// a fresh, isolated GameState: it spawns a caster and req.Units, casts the
// ability once, steps Update for req.DurationSeconds of simulated time with
// tracing on, and reports the resulting execution trace plus a compact
// per-unit HP-before/after summary.
//
// Deterministic: the isolated GameState is seeded from req.Seed and is
// driven only by this function's own fixed-dt Update loop — no wall clock,
// no unseeded RNG, no shared state with any other GameState.
//
// A legacy (SchemaVersion<2 or Program==nil) ability is compiled via
// compileLegacyAbility first, exactly as the editor's "Convert to
// Composable Ability" flow does, so previewing an unconverted catalog
// ability shows the SAME composable behavior conversion would produce.
//
// Range is NOT meaningfully validated: the caster's AttackRange is set to
// a very large constant so a castRange:"match_attack_range" ability always
// reaches every placed scene unit regardless of distance, and no other
// cast-range gate is exercised beyond what beginAbilityCastLocked itself
// enforces against that inflated range. A preview cannot be used to check
// "is this target/point within the ability's real cast range" — only what
// the ability DOES once it resolves.
func RunAbilityPreview(req PreviewRequest) (PreviewResult, error) {
	pdef := req.Ability
	if pdef.SchemaVersion < 2 || pdef.Program == nil {
		pdef.Program = compileLegacyAbility(req.Ability)
		pdef.SchemaVersion = 2
	}
	// Collision-safe registration id — see nextPreviewAbilityID's doc
	// comment. Overrides whatever id the caller supplied on req.Ability.
	pdef.ID = nextPreviewAbilityID()

	if err := validateAbilityDef(&pdef); err != nil {
		return PreviewResult{Error: err.Error()}, err
	}

	runtimeAbilitiesMu.Lock()
	runtimeAbilities[pdef.ID] = pdef
	runtimeAbilitiesMu.Unlock()
	defer func() {
		runtimeAbilitiesMu.Lock()
		delete(runtimeAbilities, pdef.ID)
		runtimeAbilitiesMu.Unlock()
	}()

	res := PreviewResult{
		Runnable: AbilityProgramRunnable(pdef.Program),
		Warnings: degradationWarnings(pdef.Program),
	}

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), req.Seed)

	// ── Setup: spawn caster + scene units, arm the trace hook. All under
	// one s.mu critical section; RequestAbilityCast/Update below acquire
	// s.mu themselves, so this section must fully release the lock before
	// either is called (self-deadlock otherwise — sync.Mutex isn't
	// reentrant).
	s.mu.Lock()
	// The damage multipliers MUST be seeded to 1.0. applyPlayerUpgradesAtSpawnLocked
	// runs for any owner that is not the enemy/neutral faction and scales a
	// spawned unit's damage by its player's PhysicalDamageMultiplier — so a bare
	// Player{} (zero value) multiplies the caster's damage by ZERO. Real players
	// get 1.0 at construction (state.go); these synthetic ones need it too.
	//
	// This was invisible until an ability's damage could scale off the caster
	// (dealDamageConfig.ADRatio): the preview caster silently had 0 damage, so an
	// authored adRatio contributed nothing and the field looked broken.
	s.Players[previewCasterOwner] = &Player{
		ID: previewCasterOwner, TeamID: 0,
		PhysicalDamageMultiplier: 1.0, MagicDamageMultiplier: 1.0,
	}
	s.Players[previewEnemyOwner] = &Player{
		ID: previewEnemyOwner, TeamID: 1,
		PhysicalDamageMultiplier: 1.0, MagicDamageMultiplier: 1.0,
	}

	caster := s.spawnPlayerUnitLocked(previewCasterUnitType, previewCasterOwner, "#3498db", protocol.Vec2{X: req.CasterX, Y: req.CasterY})
	if caster == nil {
		s.mu.Unlock()
		err := fmt.Errorf("preview: failed to spawn caster unit type %q", previewCasterUnitType)
		res.Error = err.Error()
		return res, err
	}
	caster.Visible = true
	caster.MoveSpeed = 0 // preview units never move; positions are exactly what the request specifies
	// The caster keeps its REAL catalog Damage. This used to be zeroed for the
	// "no autonomous combat noise" contract, which was fine until an ability's
	// damage could SCALE off the caster (dealDamageConfig.ADRatio): with Damage
	// at 0 an authored adRatio contributed exactly nothing, so damage-ratio
	// scaling was silently untestable in the one place abilities are tested.
	//
	// Suppress the AUTO-ATTACK instead, which is what the contract actually
	// needs. canAutoAttack (combat_ai.go) requires BOTH Damage > 0 and the
	// "attack" capability, and NonCombat stops the AI auto-acquiring a target at
	// all — so removing the capability and marking it non-combat is strictly
	// more targeted than neutering a stat the ability under test reads.
	caster.NonCombat = true
	caps := caster.Capabilities[:0]
	for _, c := range caster.Capabilities {
		if c != "attack" {
			caps = append(caps, c)
		}
	}
	caster.Capabilities = caps
	// Large enough that a castRange:"match_attack_range" ability (e.g.
	// greater_heal) reaches every scene unit a caller places, independent
	// of previewCasterUnitType's catalog AttackRange.
	caster.AttackRange = 1_000_000
	caster.MaxMana = 999_999
	caster.CurrentMana = 999_999
	// REPLACE the loadout, never append to it. spawnPlayerUnitLocked gives the
	// caster its full CATALOG loadout (the adept carries arcane_bolt), and
	// spawnUnitFromDefLocked's seedDefaultAutoCastLocked has already switched
	// auto-cast ON for anything flagged defaultAutoCast. Appending left those
	// abilities live, so the Update loop below auto-fired them at scene units
	// for the whole preview — with CurrentMana at 999,999 there was nothing to
	// stop it. The preview's contract is that ONLY the ability under test acts.
	//
	// Clearing AutoCastEnabled after that seeding pass also means pdef.ID is
	// never seeded into it, so an ability that is itself defaultAutoCast (heal,
	// meteor, ...) can only fire via the explicit RequestAbilityCast below —
	// exactly one cast, never a second unrequested one.
	caster.Abilities = []string{pdef.ID}
	caster.AutoCastEnabled = nil
	s.initializeCombatUnitLocked(caster)
	// Seed Arcane Charge so a charge-fire passive (arcane_missiles) can be
	// previewed: with the ability under test as the caster's only ability and
	// nothing spending mana, the charge loop would otherwise never cross its
	// threshold. Set after initializeCombatUnitLocked so it isn't reset. Gated
	// on IsChargeFirePassive so a stale/erroneous CasterCharge from the client
	// can never leak into a non-charge ability's preview — the field only makes
	// sense for a charge-fire passive in the first place.
	if pdef.IsChargeFirePassive() {
		caster.ArcaneCharge = req.CasterCharge
	}
	manaBefore := caster.CurrentMana
	casterID := caster.ID

	entries := make([]previewSceneEntry, len(req.Units))
	for i, su := range req.Units {
		owner := previewCasterOwner
		sceneUnitType := previewAllySceneUnitType
		if su.Team == "enemy" {
			owner = previewEnemyOwner
			sceneUnitType = previewEnemySceneUnitType
		}
		u := s.spawnPlayerUnitLocked(sceneUnitType, owner, "#e74c3c", protocol.Vec2{X: su.X, Y: su.Y})
		if u == nil {
			// Unknown catalog type: unlike the caster (single fixed type,
			// checked above), a scene unit now spawns as one of TWO types
			// depending on su.Team, so this is honestly reachable if either
			// previewAllySceneUnitType or previewEnemySceneUnitType is ever
			// renamed/removed from the catalog without updating this file.
			// Degrade by dropping the unit (entries[i] stays its zero value,
			// which getUnitByIDLocked(0) safely resolves to nil at collection
			// time) rather than aborting the whole preview run.
			continue
		}
		u.Visible = true // required for a hostile candidate to pass targeting (applyTargetFiltersLocked)
		u.MoveSpeed = 0
		u.Damage = 0
		u.AttackRange = 0
		if su.MaxHP > 0 {
			u.MaxHP = su.MaxHP
		}
		u.HP = su.HP
		if u.HP > u.MaxHP {
			u.HP = u.MaxHP
		}
		s.initializeCombatUnitLocked(u)
		entries[i] = previewSceneEntry{unitID: u.ID, team: su.Team, hpBefore: u.HP}
	}

	// Trace on for the whole run. previewClock/previewTrace are read ONLY
	// inside Update/RequestAbilityCast's own s.mu critical sections (the
	// executor builds RuntimeAbilityContext from them under the lock), and
	// this harness is single-goroutine, so writing previewClock between
	// Update calls below needs no lock for correctness — it is still
	// wrapped in one (see the tick loop) purely so every s.previewClock
	// write goes through s.mu like every other GameState field, keeping
	// this function race-detector-clean by construction rather than by
	// argument.
	tr := &AbilityExecutionTrace{}
	s.previewTrace = tr
	s.previewClock = 0
	// Forced conditional outcomes apply for the whole run (see
	// PreviewRequest.ConditionalOverrides). Left nil when the request names
	// none, so the executor's lookup stays the zero-value no-op it is in every
	// real match. Cleared alongside previewTrace at collection time.
	s.previewConditionalOverrides = req.ConditionalOverrides
	s.mu.Unlock()

	// capture appends one unfiltered wire frame (the same shape
	// snapshotUnfilteredLocked builds for a live client) into res.Frames.
	// MUST be called with s.mu NOT held by the caller — it acquires the
	// lock itself and sync.Mutex is not reentrant, so calling this from
	// inside an already-locked section would deadlock. Every call site
	// below sits at a point in this function where the lock has just been
	// released (setup, immediately after Update returns) or never taken.
	capture := func() {
		s.mu.Lock()
		snap := s.snapshotUnfilteredLocked()
		t := s.previewClock
		s.mu.Unlock()
		res.Frames = append(res.Frames, PreviewFrame{Tick: len(res.Frames), Time: t, Snapshot: snap})
	}

	// Frame 0: the initial scene, before the cast is even requested — lets
	// the editor show "what the scene looked like at t=0" as the first
	// scrub position.
	capture()

	// ── Cast ────────────────────────────────────────────────────────────
	// hasExplicitTarget is false for BOTH "no target requested" (Target<0)
	// AND an out-of-range index (Target>=len(entries)) — the two invalid
	// shapes are explicitly unified here rather than one accidentally
	// falling out of the bounds check below unexamined.
	hasExplicitTarget := req.Target >= 0 && req.Target < len(entries)
	var targetUnitID int
	switch {
	case hasExplicitTarget:
		targetUnitID = entries[req.Target].unitID
	case !pdef.TargetsPoint:
		// No usable scene-unit target and this ability aims at a unit (not
		// a point): pick a sane anchor so the cast has something to
		// resolve against. Prefer self (covers self/ally heals and
		// buffs); else the first enemy scene unit (covers enemy-only
		// abilities); else the first scene unit of any kind.
		switch {
		case pdef.CanTargetSelf:
			targetUnitID = casterID
		default:
			for _, e := range entries {
				if e.team == "enemy" {
					targetUnitID = e.unitID
					break
				}
			}
			if targetUnitID == 0 && len(entries) > 0 {
				targetUnitID = entries[0].unitID
			}
		}
	}
	// A passive (arcane_missiles) has no castable action — attempting a cast
	// would just fail and surface a spurious error. Its behavior is driven
	// entirely by the sim loop below (the seeded Arcane Charge crossing its
	// threshold), so skip the cast for passives and let the tick loop fire it.
	if !pdef.IsPassive() {
		// For a point-target ability targetUnitID is ignored by
		// RequestAbilityCast (it routes on def.TargetsPoint); req.CastX/CastY
		// are always passed and are likewise ignored on the unit-target path.
		ok, reason := s.RequestAbilityCast(previewCasterOwner, casterID, pdef.ID, targetUnitID, req.CastX, req.CastY)
		if !ok {
			res.Error = reason
		}
	}

	// ── Step ────────────────────────────────────────────────────────────
	ticks := int(req.DurationSeconds / previewTickDT)
	if req.DurationSeconds > 0 && ticks == 0 {
		ticks = 1
	}
	if ticks > previewMaxTicks {
		ticks = previewMaxTicks
	}
	for i := 0; i < ticks; i++ {
		s.mu.Lock()
		s.previewClock += previewTickDT
		s.mu.Unlock()
		s.Update(previewTickDT) // locks/unlocks s.mu internally; safe to capture() right after
		capture()
	}

	// ── Collect ─────────────────────────────────────────────────────────
	s.mu.Lock()
	res.Trace = tr.Events // execution order preserved — Events is append-only
	res.Units = make([]PreviewUnitResult, len(entries))
	for i, e := range entries {
		hpAfter := 0
		if u := s.getUnitByIDLocked(e.unitID); u != nil {
			hpAfter = u.HP
		}
		res.Units[i] = PreviewUnitResult{Index: i, Team: e.team, HPBefore: e.hpBefore, HPAfter: hpAfter}
	}
	if u := s.getUnitByIDLocked(casterID); u != nil {
		res.CasterManaSpent = manaBefore - u.CurrentMana
	}
	s.previewTrace = nil
	s.previewConditionalOverrides = nil
	s.mu.Unlock()

	return res, nil
}
