package game

// ProcEffectOverrides is the shared override bag every trigger that
// references a proc effect embeds (ItemOnHitProc today; perk/ability/trap
// triggers later), so all consumers share one override vocabulary and one
// precedence implementation. Non-zero fields replace the referenced def's
// value; zero means "use the def's". DamageType and ProjectileID are
// deliberately NOT here — they are the effect's identity (element, visuals,
// CC payload); a different element is a different effect def.
//
// Known limitation of zero-means-inherit: an override cannot disable a def's
// non-zero field (e.g. bounce a chaining effect down to 0 hops). Author a
// separate def for that instead of a sentinel.
type ProcEffectOverrides struct {
	Damage              int     `json:"damage,omitempty"`
	ProjectileScale     float64 `json:"projectileScale,omitempty"`
	BounceCount         int     `json:"bounceCount,omitempty"`
	BounceRange         float64 `json:"bounceRange,omitempty"`
	BounceDamageFalloff int     `json:"bounceDamageFalloff,omitempty"`
	SlowMultiplier      float64 `json:"slowMultiplier,omitempty"`
	SlowDurationSeconds float64 `json:"slowDurationSeconds,omitempty"`
	BurnDamagePerSecond float64 `json:"burnDamagePerSecond,omitempty"`
	BurnDurationSeconds float64 `json:"burnDurationSeconds,omitempty"`
}

// resolveProcEffectParams applies o's non-zero fields onto a copy of def's
// params. This is the SINGLE precedence implementation for all consumers —
// future systems (ability upgrades) call this rather than reimplementing
// override rules.
func resolveProcEffectParams(def ProcEffectDef, o ProcEffectOverrides) ProcEffectParams {
	p := def.ProcEffectParams
	if o.Damage > 0 {
		p.Damage = o.Damage
	}
	if o.ProjectileScale > 0 {
		p.ProjectileScale = o.ProjectileScale
	}
	if o.BounceCount > 0 {
		p.BounceCount = o.BounceCount
	}
	if o.BounceRange > 0 {
		p.BounceRange = o.BounceRange
	}
	if o.BounceDamageFalloff > 0 {
		p.BounceDamageFalloff = o.BounceDamageFalloff
	}
	if o.SlowMultiplier > 0 {
		p.SlowMultiplier = o.SlowMultiplier
	}
	if o.SlowDurationSeconds > 0 {
		p.SlowDurationSeconds = o.SlowDurationSeconds
	}
	if o.BurnDamagePerSecond > 0 {
		p.BurnDamagePerSecond = o.BurnDamagePerSecond
	}
	if o.BurnDurationSeconds > 0 {
		p.BurnDurationSeconds = o.BurnDurationSeconds
	}
	return p
}
