package steam

// This file is the single source of truth for achievement IDs reported from
// Go to Steam. Each constant value MUST match the ACHIEVEMENT_API_NAME column
// configured in the Steam Partner dashboard (a separate operator task —
// §16 task 16.2 in the standalone-desktop-app change). A mismatch means the
// achievement silently does nothing in production.
//
// To add a new achievement:
//  1. Add the constant here.
//  2. Configure it in the Steam dashboard with the same string id.
//  3. Wire it into the relevant simulation code via SteamBridge.ReportAchievement.
//  4. If the trigger event can fire more than once per run, add a per-run dedup
//     set at the call site (the bridge itself does not dedup — see §16 task
//     16.3 and the steam-achievements spec for the rationale).
//
// Keep this file `grep`-able: one constant per line, no helpers, no maps.

const (
	// AchievementFirstWaveCleared is the Phase 2 smoke-test achievement.
	// Fires when a run successfully clears its first enemy wave. The event
	// is naturally single-fire per run (run state cannot clear its first
	// wave twice), so no dedup set is needed at the call site.
	AchievementFirstWaveCleared = "ACH_FIRST_WAVE_CLEARED"
)
