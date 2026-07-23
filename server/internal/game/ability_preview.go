package game

import (
	"fmt"
	"strings"
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
	// CasterUnitType overrides which catalog unit casts the ability. Empty uses
	// previewCasterUnitType (the adept).
	//
	// This is the seam that makes CASTER-SCALED abilities testable at all: an
	// adRatio reads the caster's attack damage and an apRatio its ability power,
	// so previewing those against one hardcoded unit tells you nothing about the
	// unit that will actually cast it. An unknown type falls back to the default
	// rather than failing the run — the editor's picker is catalog-driven, so a
	// stale value means the unit was renamed, not that the preview is invalid.
	CasterUnitType string `json:"casterUnitType,omitempty"`
	// CasterRank previews the caster at bronze/silver/gold. Empty leaves the
	// spawn rank. Rank reaches an ability only THROUGH the caster — its stat
	// multipliers, its path's per-rank base stats and per-rank ability stats.
	// The ability's own numbers are the same at every rank (an ability used to
	// be able to carry per-rank overrides of its own; that was retired so there
	// is one scaling story, not two). This is the only way to see what an
	// ability does at gold without editing a unit.
	CasterRank string `json:"casterRank,omitempty"`
	// CasterPath is the PROMOTION PATH the ranked caster is on, and it is not
	// optional detail: pathModifierFor(path, rank) is what turns a rank into
	// actual stats, and a pathless unit falls back to defaultRankCurve — a
	// GENERIC curve, not the numbers any real gold unit has. Previewing rank
	// without a path therefore shows a unit that never exists in a match.
	// Empty leaves the unit pathless (the generic curve). Unknown paths are
	// ignored, like CasterUnitType/CasterRank.
	CasterPath string `json:"casterPath,omitempty"`
	DurationSeconds float64 `json:"durationSeconds"`
	// CasterPerks are the perks the preview caster OWNS for this run.
	//
	// This replaces the old ConditionalOverrides map, which forced named
	// `conditional` actions to a fixed outcome. That was a lie-shaped testing
	// affordance: it proved the THEN branch produced some effect, but never that
	// the CONDITION was authored correctly — a `has_perk` naming a perk that
	// does not exist, or the wrong perk entirely, previewed identically to a
	// correct one. Granting the perk exercises the real evaluator, so a typo'd
	// perk id now shows up as the branch simply not being taken.
	//
	// Unknown perk ids are ignored rather than failing the run, matching
	// CasterUnitType/CasterRank: a stale editor value means the perk was
	// renamed, not that the preview is invalid.
	CasterPerks []string `json:"casterPerks,omitempty"`
	// AlliesAttack turns the allied scene units into real combatants: they keep
	// their catalog move speed, damage and attack range, and are attack-moved
	// onto the enemy group when the run starts.
	//
	// This is what makes an ability whose whole effect lands on SOMEONE ELSE'S
	// damage testable. A mark that makes its victim take +20% from all sources
	// (marker_trap) deals no damage of its own — with every scene unit frozen and
	// disarmed, the preview showed a mark being applied and nothing else, and
	// whether it actually changed a number could only be checked in a live match.
	//
	// Default false — an untouched preview still shows the ability acting alone,
	// which is the honest baseline. When ONLY one side attacks, that side hitting
	// an unresisting target is a clean measurement; ticking BOTH turns the run
	// into a skirmish whose HP deltas are harder to attribute, which is the
	// caller's explicit choice to make.
	AlliesAttack bool `json:"alliesAttack,omitempty"`
	// EnemiesAttack is the enemy-side mirror of AlliesAttack: the enemy scene
	// units keep their catalog move speed / damage / range and are attack-moved
	// onto the ally group when the run starts. This is how you preview an ability
	// whose effect lands on an ENEMY'S OUTGOING damage — a Weaken debuff
	// (exposed_weakness) makes the marked enemy deal less, which is only visible
	// once that enemy actually swings at something. Default false.
	EnemiesAttack bool `json:"enemiesAttack,omitempty"`
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
	authoredID := strings.TrimSpace(req.Ability.ID)
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

	casterType := req.CasterUnitType
	if casterType == "" {
		casterType = previewCasterUnitType
	} else if _, known := getUnitDef(casterType); !known {
		// Renamed/removed since the picker was last populated — degrade to the
		// default rather than failing the whole run.
		casterType = previewCasterUnitType
	}
	caster := s.spawnPlayerUnitLocked(casterType, previewCasterOwner, "#3498db", protocol.Vec2{X: req.CasterX, Y: req.CasterY})
	if caster == nil {
		s.mu.Unlock()
		err := fmt.Errorf("preview: failed to spawn caster unit type %q", casterType)
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
	// Rank AFTER the loadout/stat overrides above, and re-applied through the
	// real progression path: applyRankModifiersLocked scales the unit's Base*
	// stats, and an ability's own byRank overrides key off caster.Rank at
	// execute time. Setting the field alone would give the byRank half without
	// the unit-stat half, which is exactly the mismatch that makes a preview
	// lie about what gold looks like.
	// Path BEFORE rank: applyRankModifiersLocked reads ProgressionPath to pick
	// the multiplier row, so setting the path afterwards would apply the generic
	// curve and then silently keep it.
	if req.CasterPath != "" && knownPreviewPath(req.CasterPath) {
		caster.ProgressionPath = req.CasterPath
	}
	if req.CasterRank != "" {
		switch req.CasterRank {
		case unitRankBronze, unitRankSilver, unitRankGold:
			caster.Rank = req.CasterRank
			s.applyRankModifiersLocked(caster, false)
		}
		// An unrecognized rank is ignored, matching the unknown-unit-type
		// degrade above: a stale editor value must not fail the run.
	}
	// Perks, before initializeCombatUnitLocked so anything that reads PerkIDs
	// during combat init sees them. Filtered to real perks so a renamed id
	// degrades to "not owned" instead of sitting in PerkIDs matching nothing.
	if len(req.CasterPerks) > 0 {
		owned := make([]string, 0, len(req.CasterPerks))
		for _, id := range req.CasterPerks {
			if perkDefByID(id) != nil {
				owned = append(owned, id)
			}
		}
		caster.PerkIDs = owned
	}
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
		// Frozen and disarmed by default, so the only thing that moves or deals
		// damage in a preview is the ability under test. An engaging unit (an ally
		// under AlliesAttack, or an enemy under EnemiesAttack) is the exception and
		// keeps its catalog move speed / damage / range untouched.
		isEnemy := su.Team == "enemy"
		engaging := (req.AlliesAttack && !isEnemy) || (req.EnemiesAttack && isEnemy)
		if !engaging {
			u.MoveSpeed = 0
			u.Damage = 0
			u.AttackRange = 0
		}
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
	// Teach this run's state that the scratch id IS the authored ability, so a
	// perk/item row targeting the real id still matches. Without it the whole
	// ability-targeted half of perk authoring is inert in the previewer — see
	// GameState.previewAbilityAliases.
	if authoredID != "" && authoredID != pdef.ID {
		s.previewAbilityAliases = map[string]string{pdef.ID: authoredID}
	}
	// Forced conditional outcomes apply for the whole run (see
	// PreviewRequest.ConditionalOverrides). Left nil when the request names
	// none, so the executor's lookup stays the zero-value no-op it is in every
	// real match. Cleared alongside previewTrace at collection time.
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

	// ── Engage ──────────────────────────────────────────────────────────
	// Attack-move rather than a bare attack order: the allies have to CLOSE the
	// distance (they spawn on the far side of the caster from the enemy group),
	// and attack-move is the order that both walks them in and lets them acquire
	// whatever they meet — the same order a player would give. Issued after the
	// cast so an ability that places something on the ground has already placed
	// it before anyone walks through.
	if req.AlliesAttack {
		if allyIDs, dest, ok := s.previewEngagementFor(entries, false); ok {
			s.AttackMoveUnits(previewCasterOwner, allyIDs, dest)
		}
	}
	if req.EnemiesAttack {
		if enemyIDs, dest, ok := s.previewEngagementFor(entries, true); ok {
			s.AttackMoveUnits(previewEnemyOwner, enemyIDs, dest)
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
	s.mu.Unlock()

	return res, nil
}

// recordPreviewDamageTraceLocked records a LANDED hit into the ability-preview
// trace, which is what the editor derives both its floating damage numbers and
// its event-log rows from (previewDamageNumbers.ts, PreviewEventLog.vue).
//
// Nil-safe and free outside a preview: s.previewTrace is nil in every real
// match, so this is one pointer check on the game's hottest damage path.
//
// Ability damage is deliberately SKIPPED here, because the executor's
// deal_damage already traced it — and traced it with the action path that makes
// an event-log row clickable back to its flow card, which this seam has no way
// to know. Emitting from both places would double every ability hit into two
// popups. Everything else — a basic attack, a trap, an item proc, a perk hit —
// reaches the trace only through here.
//
// The amount is post-mitigation: exactly the health the victim lost, which is
// the number a match would float. That makes an amplifier like marker_trap's
// mark visible as a bigger number rather than as arithmetic the author has to
// do in their head.
func (s *GameState) recordPreviewDamageTraceLocked(target *Unit, damage int, src DamageSource) {
	if s.previewTrace == nil || target == nil || damage <= 0 {
		return
	}
	if src.Category == DamageCategoryAbility {
		return
	}
	payload := map[string]any{
		"unit":   target.ID,
		"amount": damage,
		"type":   string(src.ResolvedDamageType()),
	}
	if src.AttackerUnitID != 0 {
		payload["attacker"] = src.AttackerUnitID
	}
	if src.Category != "" {
		payload["category"] = string(src.Category)
	}
	s.previewTrace.record(s.previewClock, "damage_applied", "", payload)
}

// previewEngagementFor resolves the attack-move order for ONE engaging side of a
// preview run: every surviving scene unit whose team matches attackerIsEnemy,
// and the CENTROID of the OPPOSING scene units as their destination. Returns
// ok=false when either side is empty (nobody to send, or nowhere to send them).
// AlliesAttack passes attackerIsEnemy=false, EnemiesAttack passes true — the two
// are exact mirrors, which is why they share this helper.
//
// Acquires s.mu itself, so it must be called with the lock NOT held — same
// contract as capture() above, and for the same reason (its caller,
// AttackMoveUnits, locks too).
func (s *GameState) previewEngagementFor(entries []previewSceneEntry, attackerIsEnemy bool) ([]int, protocol.Vec2, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		attackerIDs []int
		sumX, sumY  float64
		targets     int
	)
	for _, e := range entries {
		u := s.getUnitByIDLocked(e.unitID)
		if u == nil {
			continue
		}
		if (e.team == "enemy") == attackerIsEnemy {
			attackerIDs = append(attackerIDs, e.unitID)
			continue
		}
		sumX += u.X
		sumY += u.Y
		targets++
	}
	if len(attackerIDs) == 0 || targets == 0 {
		return nil, protocol.Vec2{}, false
	}
	return attackerIDs, protocol.Vec2{X: sumX / float64(targets), Y: sumY / float64(targets)}, true
}

// knownPreviewPath reports whether a path id exists in the catalog. Guards the
// preview's CasterPath against a stale editor value: an unknown path must leave
// the caster pathless (the generic rank curve) rather than silently pretending
// to be a path that no longer exists.
func knownPreviewPath(path string) bool {
	for _, paths := range ListPathsByUnitType() {
		for _, p := range paths {
			if p == path {
				return true
			}
		}
	}
	return false
}

// authoredAbilityIDLocked maps a preview harness's scratch ability id back to
// the id the ability is authored under; every other id passes through
// unchanged. This is what lets a modifier that names an ability BY ID match
// during a preview run — see GameState.previewAbilityAliases for why the two
// ids differ in the first place.
//
// Nil-map read in every real match, i.e. one map lookup on an empty map.
//
// Caller holds s.mu (read or write).
func (s *GameState) authoredAbilityIDLocked(id string) string {
	if real, ok := s.previewAbilityAliases[id]; ok {
		return real
	}
	return id
}
