package game

// ProcEffectParams is the runtime payload of a proc-fired bolt — everything
// executeProcEffectLocked needs to fire it at a target (proc_effects.go). It is
// a plain value struct constructed programmatically at the call site; the
// bespoke authored ProcEffectDef catalog (catalog/procs) that items used to
// reference was removed once every item proc became an ability cast. Today the
// sole constructor is fireAbilityChainLocked (ability_cast.go), which builds one
// from an AbilityDef's chain tuning to deliver chain_lightning's beam bounces
// through the shared proc-beam mechanic.
type ProcEffectParams struct {
	// Damage / DamageType: the typed damage instance the effect lands.
	Damage     int        `json:"damage"`
	DamageType DamageType `json:"damageType"`
	// ProjectileID names the emitter def (catalog/projectiles) that carries
	// the effect: a projectile-kind def flies a homing bolt, a beam-kind def
	// zaps instantly with deferred damage.
	ProjectileID string `json:"projectileID"`
	// ProjectileScale is a render-size multiplier for the fired bolt's sprite.
	// 0 ⇒ fall back to the firing unit's ProjectileScale.
	ProjectileScale float64 `json:"projectileScale,omitempty"`
	// Bounce / chain (beam emitters only): the effect arcs to up to BounceCount
	// further enemies, each hop leaping off the PREVIOUS victim to the nearest
	// not-yet-hit hostile within BounceRange, losing BounceDamageFalloff damage
	// per hop.
	BounceCount         int     `json:"bounceCount,omitempty"`
	BounceRange         float64 `json:"bounceRange,omitempty"`
	BounceDamageFalloff int     `json:"bounceDamageFalloff,omitempty"`
	// On-hit slow: scales the hit unit's attack + move speed by SlowMultiplier
	// for SlowDurationSeconds via the shared slow system. Zero ⇒ no slow.
	SlowMultiplier      float64 `json:"slowMultiplier,omitempty"`
	SlowDurationSeconds float64 `json:"slowDurationSeconds,omitempty"`
	// On-hit burn (fire DoT): ignites the hit unit for BurnDamagePerSecond over
	// BurnDurationSeconds via the shared burn system. Zero ⇒ no burn.
	BurnDamagePerSecond float64 `json:"burnDamagePerSecond,omitempty"`
	BurnDurationSeconds float64 `json:"burnDurationSeconds,omitempty"`
}
