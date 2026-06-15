package game

import (
	"encoding/json"
	"fmt"
)

// objective_handlers.go registers the initial six objective types declared in
// the campaign-objectives-and-metrics OpenSpec change. Each handler is a
// small typed config struct + four hooks (parseConfig, validate, initialize,
// evaluate). All evaluators may assume `state.Completed` and `state.Failed`
// are both false on entry — the dispatcher (`EvaluateObjective` in
// objective_defs.go) short-circuits absorbing states before calling here.
//
// To add a seventh type:
//  1. Declare a `<type>Config` struct mirroring its JSON shape.
//  2. Add a `register<Type>Handler()` function below.
//  3. Call it from `registerAllObjectiveHandlers()`.
//
// Registration uses a package-level var (not init()) so the campaign catalog
// loader in `campaign_defs.go` can declare a dependency on the registry by
// referencing `allObjectiveHandlersRegistered`. Without that anchor, lexical
// file ordering puts `campaign_defs.go`'s package vars first and the registry
// would be empty at validation time. See the anchor comment in campaign_defs.go.
var allObjectiveHandlersRegistered = registerAllObjectiveHandlers()

func registerAllObjectiveHandlers() bool {
	registerKillCampsHandler()
	registerBuildBuildingsHandler()
	registerCollectResourceHandler()
	registerKillCampsBeforeWaveHandler()
	registerRankUnitsHandler()
	registerSurviveWavesHandler()
	registerCaptureZoneHandler()
	return true
}

// =============================================================================
// capture_zone — control the referenced map zone(s). Reads live zone ownership
// from GameState.Zones (set by the zone-control runtime) rather than a metric.
// =============================================================================

type captureZoneConfig struct {
	ZoneIDs    []string `json:"zoneIds"`
	RequireAll bool     `json:"requireAll,omitempty"`
}

func registerCaptureZoneHandler() {
	registerObjective("capture_zone", objectiveHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			var cfg captureZoneConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		validate: func(filename, levelID, objectiveID string, raw any) {
			cfg := raw.(captureZoneConfig)
			if len(cfg.ZoneIDs) == 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: capture_zone requires a non-empty zoneIds",
					filename, levelID, objectiveID))
			}
			// Cross-map zone-existence is validated when the zone runtime is
			// installed against the level's map; the campaign catalog loader
			// does not resolve the map here.
		},
		initialize: func(raw any, state *ObjectiveState) {
			cfg := raw.(captureZoneConfig)
			if cfg.RequireAll {
				state.Required = len(cfg.ZoneIDs)
			} else {
				state.Required = 1
			}
		},
		evaluate: func(s *GameState, _ *MatchMetrics, raw any, state *ObjectiveState) {
			cfg := raw.(captureZoneConfig)
			owned := 0
			for _, id := range cfg.ZoneIDs {
				if s.zoneOwnedByTeamLocked(id) {
					owned++
				}
			}
			state.Current = owned
			if cfg.RequireAll {
				if owned >= len(cfg.ZoneIDs) {
					state.Completed = true
				}
			} else if owned >= 1 {
				state.Completed = true
			}
		},
	})
}

// =============================================================================
// kill_camps — clear N neutral camps, optionally filtered by tier.
// =============================================================================

type killCampsConfig struct {
	CampTier int `json:"campTier,omitempty"` // 0 = any tier counts
	Count    int `json:"count"`
}

func registerKillCampsHandler() {
	registerObjective("kill_camps", objectiveHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			var cfg killCampsConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		validate: func(filename, levelID, objectiveID string, raw any) {
			cfg := raw.(killCampsConfig)
			if cfg.Count <= 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: kill_camps requires count > 0, got %d",
					filename, levelID, objectiveID, cfg.Count))
			}
			if cfg.CampTier < 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: kill_camps campTier must be >= 0 (0 = any tier), got %d",
					filename, levelID, objectiveID, cfg.CampTier))
			}
		},
		initialize: func(raw any, state *ObjectiveState) {
			state.Required = raw.(killCampsConfig).Count
		},
		evaluate: func(_ *GameState, metrics *MatchMetrics, raw any, state *ObjectiveState) {
			cfg := raw.(killCampsConfig)
			var current int
			if cfg.CampTier == 0 {
				current = metrics.NeutralCampsKilled
			} else {
				current = metrics.NeutralCampsKilledByTier[cfg.CampTier]
			}
			state.Current = current
			if current >= cfg.Count {
				state.Completed = true
			}
		},
	})
}

// =============================================================================
// build_buildings — finish N buildings of a specific type.
// =============================================================================

type buildBuildingsConfig struct {
	BuildingType string `json:"buildingType"`
	Count        int    `json:"count"`
}

func registerBuildBuildingsHandler() {
	registerObjective("build_buildings", objectiveHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			var cfg buildBuildingsConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		validate: func(filename, levelID, objectiveID string, raw any) {
			cfg := raw.(buildBuildingsConfig)
			if cfg.BuildingType == "" {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: build_buildings requires non-empty buildingType",
					filename, levelID, objectiveID))
			}
			if cfg.Count <= 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: build_buildings requires count > 0, got %d",
					filename, levelID, objectiveID, cfg.Count))
			}
			if _, ok := getBuildingDef(cfg.BuildingType); !ok {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: build_buildings buildingType %q is not in the building catalog",
					filename, levelID, objectiveID, cfg.BuildingType))
			}
		},
		initialize: func(raw any, state *ObjectiveState) {
			state.Required = raw.(buildBuildingsConfig).Count
		},
		evaluate: func(_ *GameState, metrics *MatchMetrics, raw any, state *ObjectiveState) {
			cfg := raw.(buildBuildingsConfig)
			current := metrics.BuildingsBuiltByType[cfg.BuildingType]
			state.Current = current
			if current >= cfg.Count {
				state.Completed = true
			}
		},
	})
}

// =============================================================================
// collect_resource — earn N gold or wood cumulatively.
// =============================================================================

type collectResourceConfig struct {
	Resource string `json:"resource"` // "gold" | "wood"
	Amount   int    `json:"amount"`
}

func registerCollectResourceHandler() {
	registerObjective("collect_resource", objectiveHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			var cfg collectResourceConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		validate: func(filename, levelID, objectiveID string, raw any) {
			cfg := raw.(collectResourceConfig)
			if cfg.Resource != "gold" && cfg.Resource != "wood" {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: collect_resource resource must be \"gold\" or \"wood\", got %q",
					filename, levelID, objectiveID, cfg.Resource))
			}
			if cfg.Amount <= 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: collect_resource requires amount > 0, got %d",
					filename, levelID, objectiveID, cfg.Amount))
			}
		},
		initialize: func(raw any, state *ObjectiveState) {
			state.Required = raw.(collectResourceConfig).Amount
		},
		evaluate: func(_ *GameState, metrics *MatchMetrics, raw any, state *ObjectiveState) {
			cfg := raw.(collectResourceConfig)
			var current int
			switch cfg.Resource {
			case "gold":
				current = metrics.TotalGoldEarned
			case "wood":
				current = metrics.TotalWoodEarned
			}
			state.Current = current
			if current >= cfg.Amount {
				state.Completed = true
			}
		},
	})
}

// =============================================================================
// kill_camps_before_wave — like kill_camps but fails if the deadline wave
// starts. The only objective type with a "failure" outcome.
// =============================================================================

type killCampsBeforeWaveConfig struct {
	CampTier   int `json:"campTier,omitempty"`
	Count      int `json:"count"`
	BeforeWave int `json:"beforeWave"`
}

func registerKillCampsBeforeWaveHandler() {
	registerObjective("kill_camps_before_wave", objectiveHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			var cfg killCampsBeforeWaveConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		validate: func(filename, levelID, objectiveID string, raw any) {
			cfg := raw.(killCampsBeforeWaveConfig)
			if cfg.Count <= 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: kill_camps_before_wave requires count > 0, got %d",
					filename, levelID, objectiveID, cfg.Count))
			}
			if cfg.BeforeWave <= 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: kill_camps_before_wave requires beforeWave > 0, got %d",
					filename, levelID, objectiveID, cfg.BeforeWave))
			}
			if cfg.CampTier < 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: kill_camps_before_wave campTier must be >= 0 (0 = any tier), got %d",
					filename, levelID, objectiveID, cfg.CampTier))
			}
		},
		initialize: func(raw any, state *ObjectiveState) {
			state.Required = raw.(killCampsBeforeWaveConfig).Count
		},
		evaluate: func(s *GameState, metrics *MatchMetrics, raw any, state *ObjectiveState) {
			cfg := raw.(killCampsBeforeWaveConfig)
			var current int
			if cfg.CampTier == 0 {
				current = metrics.NeutralCampsKilled
			} else {
				current = metrics.NeutralCampsKilledByTier[cfg.CampTier]
			}
			state.Current = current
			if current >= cfg.Count {
				state.Completed = true
				return
			}
			// Failure: the deadline wave has begun while we are still short.
			// The wave manager flips to "active" on transition; once it goes
			// to "upgrade" or "complete" the wave is over. We fail the moment
			// the deadline wave is observed in the active phase.
			if s != nil &&
				s.WaveManager.CurrentWave >= cfg.BeforeWave &&
				s.WaveManager.State == "active" {
				state.Failed = true
			}
		},
	})
}

// =============================================================================
// rank_units — have N units currently at the given rank or higher.
// =============================================================================

type rankUnitsConfig struct {
	Rank  string `json:"rank"` // "bronze" | "silver" | "gold"
	Count int    `json:"count"`
}

func registerRankUnitsHandler() {
	registerObjective("rank_units", objectiveHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			var cfg rankUnitsConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		validate: func(filename, levelID, objectiveID string, raw any) {
			cfg := raw.(rankUnitsConfig)
			switch cfg.Rank {
			case unitRankBronze, unitRankSilver, unitRankGold:
				// valid
			default:
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: rank_units rank must be \"bronze\", \"silver\", or \"gold\", got %q",
					filename, levelID, objectiveID, cfg.Rank))
			}
			if cfg.Count <= 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: rank_units requires count > 0, got %d",
					filename, levelID, objectiveID, cfg.Count))
			}
		},
		initialize: func(raw any, state *ObjectiveState) {
			state.Required = raw.(rankUnitsConfig).Count
		},
		// Documented semantic: "currently-at-or-above rank, not cumulative
		// rank-ups." `MatchMetrics.UnitsByRank` is recomputed at every
		// rank-up + ranked-unit death by `recomputeUnitsByRankForOwnerLocked`,
		// so this read is always fresh.
		evaluate: func(_ *GameState, metrics *MatchMetrics, raw any, state *ObjectiveState) {
			cfg := raw.(rankUnitsConfig)
			current := metrics.UnitsByRank[cfg.Rank]
			state.Current = current
			if current >= cfg.Count {
				state.Completed = true
			}
		},
	})
}

// =============================================================================
// survive_waves — clear N waves. Migration target for the legacy
// `surviveWaves` map victory condition.
// =============================================================================

type surviveWavesConfig struct {
	WavesToSurvive int `json:"wavesToSurvive"`
}

func registerSurviveWavesHandler() {
	registerObjective("survive_waves", objectiveHandler{
		parseConfig: func(raw json.RawMessage) (any, error) {
			var cfg surviveWavesConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		},
		validate: func(filename, levelID, objectiveID string, raw any) {
			cfg := raw.(surviveWavesConfig)
			if cfg.WavesToSurvive <= 0 {
				panic(fmt.Sprintf("catalog/campaigns/%s: level %s: objective %s: survive_waves requires wavesToSurvive > 0, got %d",
					filename, levelID, objectiveID, cfg.WavesToSurvive))
			}
		},
		initialize: func(raw any, state *ObjectiveState) {
			state.Required = raw.(surviveWavesConfig).WavesToSurvive
		},
		evaluate: func(_ *GameState, metrics *MatchMetrics, raw any, state *ObjectiveState) {
			cfg := raw.(surviveWavesConfig)
			current := metrics.WavesCleared
			state.Current = current
			if current >= cfg.WavesToSurvive {
				state.Completed = true
			}
		},
	})
}
