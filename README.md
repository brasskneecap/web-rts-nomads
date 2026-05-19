# web-rts-nomads

Tick-based RTS with roguelike elements. Authoritative Go server + Vue 3 SPA.

## Build targets

| Target | Status | Owner |
|---|---|---|
| **Browser dev loop** | Active — `air` + `npm run dev` | always works, never breaks |
| **Packaged desktop app** | Phase 1 in progress (`standaloneApp` branch) | wraps the Go server in a Tauri shell — see [`desktop/README.md`](desktop/README.md) and the openspec change [`openspec/changes/standalone-desktop-app`](openspec/changes/standalone-desktop-app/) |

## Repository layout

```
web-rts-nomads/
├── server/             # Go server (authoritative simulation + WS hub + HTTP API)
├── client/             # Vue 3 SPA (game client; renders server state)
│   └── src/game-portal/
├── desktop/            # Tauri 2 shell — packages server + SPA into a desktop app
│   └── src-tauri/
├── openspec/changes/   # Active OpenSpec changes
├── docs/superpowers/specs/  # Playtest checklists and design docs
└── .claude/rules/      # AI assistant rules (read AI_RULES.md before editing)
```

## Dev workflows

### Browser dev (default)

```sh
# Terminal 1 — Go server with live reload
air

# Terminal 2 — Vite dev server
cd client/src/game-portal
npm run dev          # http://localhost:5173
```

### Packaged desktop dev (Tauri shell + Vite + Go child)

Prerequisites: Rust toolchain (rustup), `cargo install tauri-cli --version "^2.0" --locked`,
Visual Studio 2022 Build Tools on Windows.

```sh
cd client/src/game-portal
npm run tauri:dev    # spawns shell + Vite + Go child via tauri.conf.json beforeDevCommand
```

Stop `air` before running `tauri:dev` — both want port 8080 by default.
See [`desktop/README.md`](desktop/README.md) for `WEBRTS_PORT` workarounds.

### Building a packaged installer

End-to-end build (SPA → embedded server → Tauri bundle):

```sh
# macOS / Linux
make package

# Windows PowerShell
.\build.ps1 -Target package

# Windows cmd
build package
```

All three produce platform-appropriate installers (`.msi` / `.dmg` /
`.AppImage`) under `desktop/src-tauri/target/`. Use `make test` /
`.\build.ps1 -Target test` / `build test` to run the full Go + Rust test
matrix. Run with no target to see the full target list.

## Project context

- Server owns all simulation, AI, combat, pathing, profiles, lobbies.
  Clients send command intents; server broadcasts state.
- Simulation is tick-based and runs under a single lock (`*Locked` method
  suffix indicates "caller holds the state lock").
- Determinism is load-bearing — the Tauri shell, Steam IPC, and Steam
  Networking Sockets transport are NEVER on the tick path.

For details, read [`.claude/rules/AI_RULES.md`](.claude/rules/AI_RULES.md) —
the source of truth for project conventions and constraints.

## OpenSpec changes

Active changes are tracked under [`openspec/changes/`](openspec/changes/).
Current major change: [`standalone-desktop-app`](openspec/changes/standalone-desktop-app/)
(Phase 1 in progress — Tauri shell, embedded SPA, pluggable WS transport,
Direct connect MP, per-OS user-data dir, diagnostics logging).
