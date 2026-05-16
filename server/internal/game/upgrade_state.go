package game

// PlayerUpgradeState tracks wave upgrade progression for a single player.
// Run-persistent fields (UpgradeStacks, MaxRerolls, MaxUpgradeStacks) survive
// until the match ends. Wave-transient fields (CurrentOffers, OfferDeadlineMs,
// RerollsRemaining, Resolved) are reset by enterWaveUpgradePhaseLocked each wave.
type PlayerUpgradeState struct {
	// Run-persistent
	UpgradeStacks    map[string]int // upgrade group → number of times taken this run
	MaxRerolls       int            // rerolls per wave; default 1, legend-incrementable
	MaxUpgradeStacks int            // stack cap override; default 3, legend-incrementable

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
