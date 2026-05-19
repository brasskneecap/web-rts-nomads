# Standalone Desktop App — Playtest Checklist

Created: 2026-05-17 alongside the `standalone-desktop-app` openspec change.

This checklist covers the manual playtest steps that must run against each
packaged build before it leaves the development team. Items marked **[P1]**
are required for the Phase 1 packaged build (offline single-player + Direct
connect MP). Items marked **[P2]** are required for the Phase 2 packaged
build (Steam invite MP + achievements). Phase 3 (signing, store assets,
auto-update) is a separate change and has its own checklist.

Record the build version (`__APP_VERSION__`) and date alongside each result.

## Per-OS launch surface

### Windows
- [ ] **[P1]** `.msi` installer launches on a clean Windows 11 VM without
      WebView2 pre-installed; the Evergreen bootstrapper installs WebView2
      silently and the main menu appears within 5 s of post-install launch.
      (Tests task 5.6 + 18.2.)
- [ ] **[P1]** Windows SmartScreen "Windows protected your PC" panel appears
      on first launch (expected for unsigned builds); "More info → Run anyway"
      proceeds. Resolved properly in Phase 3 with code signing.

### macOS
- [ ] **[P1]** Universal `.dmg` mounts and the app launches on Apple Silicon.
- [ ] **[P1]** Same `.dmg` launches on an Intel Mac. Verify with `lipo -info`
      that both arches are present in BOTH the shell and Go sidecar binaries
      (task 18.2).
- [ ] **[P1]** macOS Gatekeeper shows the "Nomads.app is damaged" panel on
      first launch (expected for unsigned downloads); the workaround
      `xattr -d com.apple.quarantine /Applications/Nomads.app` clears it.
      Alternatively right-click → Open. Resolved in Phase 3 with notarisation.

### Linux
- [ ] **[P1]** `.AppImage` requires `chmod +x` before first launch on a fresh
      Ubuntu LTS install (expected); after that, double-clicking it launches.
- [ ] **[P1]** webkit2gtk version on the target system meets the minimum
      pinned in `desktop/README.md` (≥ 2.40 for the current Tauri pin).

## Steam Deck / SteamOS — renderer parity

- [ ] **[P1]** Renderer-parity smoke check (task 21.0): on a Steam Deck or
      SteamOS VM, the first Phase 1 packaged build cold-launches to the main
      menu within 10 s. At least one combat scene renders without obvious
      regressions vs. the Windows build. Capture one screenshot pair. If the
      smoke check fails in a Linux-only way severe enough to block Deck
      shipping, escalate to a design change before Phase 2 work begins.
- [ ] **[P2]** Full renderer-parity acceptance (task 21.1): SP run start to
      end on Steam Deck. Record cold-launch time, visual parity (yes/no with
      screenshots), sustained fps during mid-game combat.
- [ ] **[P2]** Steam Deck sleep / wake (task 14.9): during a Phase 2 MP
      match, suspend-to-RAM and wake. Document whether the WS transport
      survives, whether the host times out joiners during sleep, and any
      required UX.

## Single-player

- [ ] **[P1]** Cold launch → main menu → start single-player run → first wave
      clears without crash.
- [ ] **[P1]** Focus loss while in single-player pauses the simulation; an
      unobtrusive "Paused" overlay appears. Clicking back into the window
      resumes the simulation. (Tests task 15.6.)
- [ ] **[P1]** Minimising the window during single-player pauses the same
      way; restoring resumes.
- [ ] **[P1]** **Server-crash UX**: kill the Go child process externally
      (Task Manager / `kill`) while in a SP run. Confirm the shell raises the
      `server-crashed` event, the SPA shows the "Server crashed — click to
      restart" dialog (task 17.2), Retry respawns the shell. After respawn the
      SPA navigates to the main menu and shows the one-time "run could not be
      recovered" toast (task 6.6).
- [ ] **[P1]** **Window close**: profile changes (e.g., gold balance) made
      during a run persist to `<userdata>/profiles/<id>.json` after window
      close → relaunch. Settings (window size/position) persist similarly via
      `<userdata>/settings.json`.

## Direct connect MP (P1)

- [ ] **[P1]** Two machines on the same LAN: host enables "Allow LAN/Internet
      connections", shares the IP, joiner connects via `host:port`. Match
      runs to completion.
- [ ] **[P1]** Toggle off "Allow LAN/Internet connections" while joiner is
      connected. Existing connection persists; a SECOND attempted join from
      another machine is refused with HTTP 403. (Tests task 13.10.)
- [ ] **[P1]** Joiner connect failure surfacing: try connecting to
      `127.0.0.1:9999` (refused), `nonexistent.example:8080` (DNS), and a
      reachable but non-responsive host (timeout). All three render
      descriptive SPA error messages, not raw error strings. (Tests task 13.5.)
- [ ] **[P1]** Host disconnects (network drop, close window, kill process)
      during MP match. Joiner sees "Match ended — host disconnected" with a
      single "Return to menu" action. No "promote to host" option appears.
      (Tests `pluggable-mp-transport` "Host disconnect ends the match".)

## Steam invite MP (P2)

- [ ] **[P2]** Steam launch via friend invite while game is closed: Steam
      launches the game, `+connect_lobby <id>` arrives via argv, SPA
      navigates to the join flow after `desktop_bridge_ready`.
- [ ] **[P2]** Friend invite from inside the game opens the Steam overlay
      invite dialog (`open_invite_overlay`).
- [ ] **[P2]** Steam mode/max-players changes via the host UI round-trip
      through the Steam lobby and reflect on the joiner UI within ~1 s.
- [ ] **[P2]** Sign out of Steam mid-session: shell continues to run; SPA
      hides Steam-mode entries; achievements no longer report.

## Achievements (P2 smoke)

- [ ] **[P2]** First-wave-cleared in a fresh single-player run reports the
      `ACH_FIRST_WAVE_CLEARED` achievement to the Steam dashboard. Confirm
      via the Steam client's achievement panel. (Task 16.1 + 16.3.)
- [ ] **[P2]** Achievement Steam-unavailable path (sign out of Steam, run a
      first-wave-cleared event): the achievement is dropped without error
      and no `pending_achievements.json` is created. (Task 17.0.)

## Logs and diagnostics

- [ ] **[P1]** On every launch, `<userdata>/logs/` contains a new
      `<timestamp>-shell.log` and `<timestamp>-server.log`. After Phase 1 SPA
      buffer is wired, `<timestamp>-spa.log` is also present.
- [ ] **[P1]** Logs directory rotation: launch the app 5 times in a row with
      large stderr noise (e.g., set `RUST_LOG=trace`). Confirm older
      run-triples are evicted but the current run's files are preserved.
- [ ] **[P1]** Force a Rust panic in the shell (insert a temporary
      `panic!("test")` after init). Confirm the final shell log line is the
      `[PANIC] at file:line: test` entry from the panic hook.
- [ ] **[P1]** From the SPA's Support / About screen (lands with §15.7),
      "Open logs folder" opens the OS file manager at `<userdata>/logs/`.
- [ ] **[P1]** **Log content audit**: open a recent `<ts>-server.log` and
      confirm it does NOT contain per-tick simulation data, raw game-state
      snapshots, or Steam auth tickets. (Tests task 22.4.)

## Capability allowlist audit

- [ ] **[P1]** `desktop/src-tauri/tauri.conf.json` capabilities section
      contains no wildcard permissions and no entries under `fs:*`,
      `http:*`, or `process:*`. (Tests task 5.5 + 5.8.)
- [ ] **[P2]** `shell:allow-open` is scoped to the resolved logs directory
      path (no globs). Verify by trying to invoke `shell.open` against a
      non-logs path from the SPA — Tauri rejects with a capability error.

## Notes for the next checklist update

- Phase 3 work (code signing, notarisation, store-page assets, anti-cheat,
  auto-update outside Steam) is a separate change and gets its own checklist.
- Single-instance lock test (task 9.4) is P2 and lands when the
  `tauri-plugin-single-instance` plugin is added.
- macOS smoke check (task 21.2) — apply alongside the P2 Steam Deck check.
