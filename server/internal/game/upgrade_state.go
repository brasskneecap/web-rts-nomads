package game

// WaveStatBuff records a single stat-multiplier wave upgrade application.
// One entry is appended per upgrade chosen; they are replayed in order at
// spawn so newly-trained units receive identical sequential rounding to units
// that were alive when each upgrade was chosen.
type WaveStatBuff struct {
	Scope     string  // upgradeScopeArmy / upgradeScopeArchetype / upgradeScopeUnitType
	Archetype string  // only used when Scope == upgradeScopeArchetype
	UnitType  string  // only used when Scope == upgradeScopeUnitType
	Stat      string  // upgradeEffectStat* constant
	Multiplier float64 // single multiplier from this one upgrade application
}

// PlayerUpgradeState tracks wave upgrade progression for a single player.
// Run-persistent fields (UpgradeStacks, MaxRerolls, MaxUpgradeStacks,
// WaveStatBuffs) survive until the match ends. Wave-transient fields
// (CurrentOffers, OfferDeadlineMs, RerollsRemaining, Resolved) are reset by
// enterWaveUpgradePhaseLocked each wave.
type PlayerUpgradeState struct {
	// Run-persistent
	UpgradeStacks    map[string]int // upgrade group → number of times taken this run
	MaxRerolls       int            // rerolls per wave; default 1, legend-incrementable
	MaxUpgradeStacks int            // stack cap override; default 3, legend-incrementable
	// WaveStatBuffs accumulates stat multipliers from wave upgrade choices so
	// that units spawned after the upgrade choice receive the same bonuses.
	WaveStatBuffs []WaveStatBuff

	// Wave-transient (reset each wave by enterWaveUpgradePhaseLocked)
	RerollsRemaining int
	CurrentOffers    []UpgradeDef
	OfferDeadlineMs  int64 // unix ms auto-pick fires
	Resolved         bool
}

func newPlayerUpgradeState(maxRerolls, maxStacks int) PlayerUpgradeState {
	if maxRerolls <= 0 {
		maxRerolls = 1
	}
	if maxStacks <= 0 {
		maxStacks = 3
	}
	return PlayerUpgradeState{
		UpgradeStacks:    make(map[string]int),
		MaxRerolls:       maxRerolls,
		MaxUpgradeStacks: maxStacks,
	}
}
