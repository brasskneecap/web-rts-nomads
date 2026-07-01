package game

import (
	"encoding/json"
	"sort"
)

// ObjectiveScope tells the evaluator which metrics view to feed an objective.
// Team-scope objectives evaluate against a synthesised sum/aggregate of every
// non-AI player's MatchMetrics; player-scope objectives evaluate against the
// viewer's own metrics. See `design.md` decision "Scope = team | player".
//
// Default when missing in JSON: team. See `parseAndValidateObjectiveDef`.
type ObjectiveScope string

const (
	ObjectiveScopeTeam   ObjectiveScope = "team"
	ObjectiveScopePlayer ObjectiveScope = "player"
)

// ObjectiveDef is the static, catalog-loaded definition of one objective.
// Attached to a CampaignLevelDef.Objectives entry; loaded once at startup.
// The `parsedConfig` field is populated at load time by the registered
// handler's parseConfig hook so per-tick evaluation does not re-unmarshal.
type ObjectiveDef struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Description string          `json:"description,omitempty"`
	Scope       ObjectiveScope  `json:"scope,omitempty"`
	Required    bool            `json:"required,omitempty"`
	// RewardDominionPoints is the Dominion Point reward granted the first
	// time (ever, per player) this objective is completed. 0 / omitted = no
	// reward. Metadata only — it does not participate in evaluation.
	RewardDominionPoints int `json:"rewardDominionPoints,omitempty"`
	// RewardConquestBadges is the Conquest Badge reward granted the first time
	// (ever, per player) this objective is completed. 0 / omitted = no reward.
	RewardConquestBadges int             `json:"rewardConquestBadges,omitempty"`
	Config               json.RawMessage `json:"config"`

	// parsedConfig is the typed config struct produced by the handler's
	// parseConfig hook. Not serialized; populated once at catalog load. Each
	// handler's `evaluate` casts this to its declared config type.
	parsedConfig any
}

// ParsedConfig returns the typed config struct produced by the handler's
// parseConfig hook at load time. Returns nil if catalog load failed for
// this objective (which would have panicked anyway, but keep nil-safe).
func (d ObjectiveDef) ParsedConfig() any {
	return d.parsedConfig
}

// ObjectiveState is the per-tick mutable state for a single objective
// instance. Team-scope objectives have one ObjectiveState shared across the
// team; player-scope objectives have one per player. Once `Completed` or
// `Failed` flip true they are absorbing — evaluators short-circuit on entry.
type ObjectiveState struct {
	ObjectiveID string         `json:"objectiveId"`
	Scope       ObjectiveScope `json:"scope"`
	Current     int            `json:"current"`
	Required    int            `json:"required"`
	Completed   bool           `json:"completed"`
	Failed      bool           `json:"failed,omitempty"`
}

// objectiveHandler is the registered contract for one objective type.
// Mirrors the pattern in `profile_upgrade_defs.go`'s effect registry: a
// catalog-load validator + a per-tick evaluator, keyed by the JSON `type`
// dispatch string.
//
// Hooks:
//   - parseConfig: unmarshal the raw JSON config into the handler's typed
//     struct. Returns an error on shape mismatch (e.g. wrong types).
//   - validate: panic on semantic invariants (e.g. count < 1, unknown
//     building type). Receives filename + levelID + objectiveID so the
//     panic message points at the offending JSON entry.
//   - initialize: populate state.Required from the typed config before
//     evaluation begins. Called once per objective instance at match start.
//   - evaluate: per-tick state update. The metrics view passed in matches
//     the objective's scope (team-aggregated for team-scope, viewer's own
//     for player-scope). Sticky completion/failure invariants are enforced
//     by the evaluator entry before this is called, so handlers can assume
//     state is in-progress.
type objectiveHandler struct {
	parseConfig func(raw json.RawMessage) (any, error)
	validate    func(filename, levelID, objectiveID string, cfg any)
	initialize  func(cfg any, state *ObjectiveState)
	evaluate    func(s *GameState, metrics *MatchMetrics, cfg any, state *ObjectiveState)
}

// objectiveRegistry is the type-string -> handler dispatch table. Populated
// at package init time by `init()` blocks in `objective_handlers.go`. Never
// mutated after init; catalog loaders read from it under no lock.
var objectiveRegistry = map[string]objectiveHandler{}

// registerObjective adds a handler to the registry under typeKey. Panics on
// duplicate registration so an accidentally-shadowed handler does not
// silently win.
func registerObjective(typeKey string, h objectiveHandler) {
	if _, dup := objectiveRegistry[typeKey]; dup {
		panic("objective_defs: duplicate handler registration for type " + typeKey)
	}
	if h.parseConfig == nil || h.validate == nil || h.initialize == nil || h.evaluate == nil {
		panic("objective_defs: handler for type " + typeKey + " is missing a required hook")
	}
	objectiveRegistry[typeKey] = h
}

// GetObjectiveHandler returns the handler registered for typeKey and whether
// it was found. Exported for the campaign catalog loader.
func GetObjectiveHandler(typeKey string) (objectiveHandler, bool) {
	h, ok := objectiveRegistry[typeKey]
	return h, ok
}

// ListObjectiveTypes returns all registered objective type keys sorted
// alphabetically. Stable across runs (no map iteration order leaks into
// outputs). Used by client-side schema discovery in the future editor
// change; safe to call any time after package init.
func ListObjectiveTypes() []string {
	keys := make([]string, 0, len(objectiveRegistry))
	for k := range objectiveRegistry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseAndValidateObjectiveDef applies the registry to one freshly-loaded
// ObjectiveDef. Sets scope/required defaults, dispatches to the handler's
// parseConfig + validate, and stores the typed config back on the def.
// Returns the populated def.
//
// Panics on bad data — catalogs are static; a bad entry means the binary
// is misconfigured and we want to fail fast at startup, naming the file +
// level + objective so the operator can fix it.
func parseAndValidateObjectiveDef(filename, levelID string, raw ObjectiveDef) ObjectiveDef {
	if raw.ID == "" {
		panic("catalog/campaigns/" + filename + ": level " + levelID + ": objective missing id")
	}
	if raw.Type == "" {
		panic("catalog/campaigns/" + filename + ": level " + levelID +
			": objective " + raw.ID + " missing type")
	}

	// Default scope = team. Reject any explicit value that is not one of the
	// two allowed literals — silent coercion would hide typos like "Team".
	switch raw.Scope {
	case "":
		raw.Scope = ObjectiveScopeTeam
	case ObjectiveScopeTeam, ObjectiveScopePlayer:
		// valid; leave as-is
	default:
		panic("catalog/campaigns/" + filename + ": level " + levelID +
			": objective " + raw.ID + ": invalid scope " + string(raw.Scope) +
			` (must be "team" or "player")`)
	}

	// Default required = false. JSON unmarshal already gives us false when
	// the field is absent, so nothing to do here — kept as a comment so the
	// behaviour is documented in code.
	// raw.Required is left at its JSON-decoded value.

	if raw.RewardDominionPoints < 0 {
		panic("catalog/campaigns/" + filename + ": level " + levelID +
			": objective " + raw.ID + ": rewardDominionPoints must be >= 0")
	}
	if raw.RewardConquestBadges < 0 {
		panic("catalog\\campaigns\\" + filename + ": level " + levelID +
			": objective " + raw.ID + ": rewardConquestBadges must be >= 0")
	}

	handler, ok := objectiveRegistry[raw.Type]
	if !ok {
		panic("catalog/campaigns/" + filename + ": level " + levelID +
			": objective " + raw.ID + ": unknown type " + raw.Type +
			" (register a handler in objective_handlers.go init())")
	}

	cfg, err := handler.parseConfig(raw.Config)
	if err != nil {
		panic("catalog/campaigns/" + filename + ": level " + levelID +
			": objective " + raw.ID + ": invalid config: " + err.Error())
	}
	handler.validate(filename, levelID, raw.ID, cfg)
	raw.parsedConfig = cfg
	return raw
}

// NewObjectiveState constructs a ready-to-evaluate ObjectiveState from a
// validated ObjectiveDef. Calls the handler's initialize hook to populate
// `state.Required` from the parsed config (typically the `count` or
// `amount` field). The returned state's Completed/Failed start false.
func NewObjectiveState(def ObjectiveDef) ObjectiveState {
	state := ObjectiveState{
		ObjectiveID: def.ID,
		Scope:       def.Scope,
	}
	if handler, ok := objectiveRegistry[def.Type]; ok && def.parsedConfig != nil {
		handler.initialize(def.parsedConfig, &state)
	}
	return state
}

// EvaluateObjective dispatches one tick of evaluation against the metrics
// view chosen by the caller (the evaluator computes the right view based
// on the def's scope before invoking this). Enforces sticky completion +
// failure here so individual handlers can omit the guard.
func EvaluateObjective(s *GameState, def ObjectiveDef, metrics *MatchMetrics, state *ObjectiveState) {
	if state.Completed || state.Failed {
		return
	}
	handler, ok := objectiveRegistry[def.Type]
	if !ok || def.parsedConfig == nil {
		return
	}
	handler.evaluate(s, metrics, def.parsedConfig, state)
	// Belt-and-braces: a handler that sets Completed and Failed in the same
	// tick should not be ambiguous. Completion wins.
	if state.Completed {
		state.Failed = false
	}
}

// objectiveRuntime is the per-match mutable shell for a single ObjectiveDef.
// One instance lives in `GameState.Objectives` per loaded objective for the
// full match duration. The shell carries:
//
//   - Def         the immutable catalog definition (id, type, scope, etc).
//   - TeamState   used when Def.Scope == ObjectiveScopeTeam. Single shared
//                 state evaluated against the team-aggregated MatchMetrics.
//   - PlayerStates used when Def.Scope == ObjectiveScopePlayer. Keyed by
//                 playerID; lazy-init on first encounter (the evaluator
//                 creates an entry when a player is seen) so late joins get
//                 the correct initial Required value.
//
// Unexported so external packages cannot fabricate runtimes from outside
// the engine — Section 10's snapshot serialiser will read these via the
// package-private projection.
type objectiveRuntime struct {
	Def          ObjectiveDef
	TeamState    ObjectiveState
	PlayerStates map[string]ObjectiveState
}

// newObjectiveRuntime constructs a runtime shell with the team state
// pre-initialised via the registered handler. Player states stay nil — the
// evaluator lazy-inits per player on first encounter.
func newObjectiveRuntime(def ObjectiveDef) objectiveRuntime {
	return objectiveRuntime{
		Def:          def,
		TeamState:    NewObjectiveState(def),
		PlayerStates: nil,
	}
}

// ensurePlayerState returns a pointer to the per-player ObjectiveState for
// the given playerID, lazy-allocating both the outer map and the per-player
// entry as needed. Each entry is freshly initialised via the handler so
// `Required` is populated.
func (r *objectiveRuntime) ensurePlayerState(playerID string) *ObjectiveState {
	if r.PlayerStates == nil {
		r.PlayerStates = map[string]ObjectiveState{}
	}
	if _, ok := r.PlayerStates[playerID]; !ok {
		r.PlayerStates[playerID] = NewObjectiveState(r.Def)
	}
	s := r.PlayerStates[playerID]
	return &s
}

// storePlayerState commits the per-player state back into the map after the
// evaluator has mutated the pointer returned by ensurePlayerState.
func (r *objectiveRuntime) storePlayerState(playerID string, state ObjectiveState) {
	if r.PlayerStates == nil {
		r.PlayerStates = map[string]ObjectiveState{}
	}
	r.PlayerStates[playerID] = state
}
