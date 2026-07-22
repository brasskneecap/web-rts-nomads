---
name: build-nomads
description: Use when asked to build, package, compile, or produce a release of the Nomads desktop app — including "build the project", "build for Steam", "make the exe", or generating nomads-desktop.exe / the MSI installer.
---

# Build Nomads Desktop App

## Overview

Nomads builds through `build.ps1` (a PowerShell pipeline; `build.cmd` is a thin cmd.exe wrapper that forwards args to it). The pipeline is: Vue SPA → stage `dist/` into the Go module → `go build -tags embed_spa` sidecar → Tauri shell.

**Default to the Steam build** unless the user says otherwise — that is the primary desktop artifact for this project.

## How to run it

The command (from the repo root `C:\Users\Chad Anderson\web-rts-nomads`, in cmd.exe):

```cmd
build package -Steam
```

That's the whole task. `build.cmd` forwards to `build.ps1`. For a non-Steam MSI installer, drop the flag: `build package`.

**Running it as the agent:** invoke via the PowerShell tool so it executes reliably:

```powershell
& "C:\Users\Chad Anderson\web-rts-nomads\build.ps1" -Target package -Steam
```

Do NOT nest `cmd.exe /c "build.cmd ..."` inside the Bash/Git-Bash tool — that returns exit 0 without actually building (only prints the cmd banner). The cargo release compile is slow (~1.5 min); run with `run_in_background: true` and read the output file when notified.

## Targets (`-Target`)

| Target | Produces |
|--------|----------|
| `package` | Full pipeline (spa → stage-dist → sidecar → shell). **Use this by default.** |
| `shell` | Relink the Tauri shell only — skips SPA + sidecar rebuild. Fast iteration once SPA is built. |
| `sidecar` | Go server w/ embedded SPA into the desktop sidecar slot |
| `server` | Go server → `bin\api.exe` (no embed) |
| `spa` | `npm run build` (Vue SPA) only |
| `test` | `go test ./...` + `cargo test --lib` |
| `clean` | Remove build artifacts |

## The `-Steam` path is different on purpose

- Runs `cargo build --release --features steam` — **not** `cargo tauri build` (the Tauri CLI drops `--features steam`, silently overwriting the Steam binary with a non-Steam one).
- **Output is a raw exe, no MSI:** `desktop\src-tauri\target\release\nomads-desktop.exe`
- Auto-stages the two runtime files next to the exe: `steam_api64.dll` (from the steamworks-sys build output) and `steam_appid.txt` (auto-created with `480` = Spacewar if missing).

## Verify the build actually ran

Exit code 0 is not enough — confirm the artifact's mtime is recent (a stale binary from a prior build can look like success). Check:

```powershell
Get-Item "C:\Users\Chad Anderson\web-rts-nomads\desktop\src-tauri\target\release\nomads-desktop.exe" |
  Select-Object LastWriteTime, Length
```

Confirm it's genuinely a Steam build by checking the exe links `steam_api64` (a non-Steam build has zero references), and that the build log printed `building with Steamworks (--features steam, no bundle)`:

```powershell
$exe = "C:\Users\Chad Anderson\web-rts-nomads\desktop\src-tauri\target\release\nomads-desktop.exe"
$text = [System.Text.Encoding]::ASCII.GetString([System.IO.File]::ReadAllBytes($exe))
([regex]::Matches($text, 'steam_api64')).Count   # > 0 means steam-linked
```

`test-steam.ps1` (repo root) does this end-to-end: rebuild (`shell -Steam`) → verify the exe is Steam-linked → launch → tail the shell log at `%APPDATA%\Nomads\logs\<ts>-shell.log`. Its Step 2 checks `steam_api64` reference count (was an older `NOMADS_BUILD_VARIANT::STEAM_BUILD_V2` marker string, which was removed from the Rust source — don't reintroduce a dependency on it).

## Prerequisites

- Rust toolchain + `cargo install tauri-cli --version "^2.0" --locked`
- Go, Node/npm, VS 2022 Build Tools
- Steam client **running and signed in** at launch time for Steam features (otherwise `SteamAPI_Init` fails and it degrades to the no-Steam path — by design, not a bug). The SDK itself is bundled by `steamworks-sys 0.13`; no partner download or `STEAM_SDK_LOCATION` needed.

## Optional

- Stamp a version (else it uses the git short SHA): set `$env:NOMADS_VERSION = "v0.1.0"` before invoking.
- Full reference lives in `desktop\README.md` ("Steamworks setup", "Building with Steam enabled").
