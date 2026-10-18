# Build & Test

- Build all 12 targets: `.\build_windows.ps1 -All -Silent`
- Build one platform/arch: `.\build_windows.ps1 -Platform windows -Arch x64 -Type cli -Type gui -Silent` (PowerShell binds a named parameter once, so repeat as `-Type cli,gui`; `-Platform`/`-Arch`/`-Type` accept `all`)
- Backend only / frontend only: use the legacy 3-tuple `-Build <system>,<mode>,<scope>`, where scope is `backend`, `frontend` or `all` — e.g. `-Build windows,gui,backend`. The scope is the THIRD element: `-Build backend` alone is invalid (`Get-NormalizedPlatform` rejects `backend`).
- Plan without building: `.\build_windows.ps1 -DryRun -All`
- Build + MSIX: `.\build_windows.ps1 -All -BuildMsix`
- Also see `.\build_windows.ps1 -Help` (self-documenting) and the 12-target matrix in build.md / build_EN.md; `build.sh` is the Linux/macOS twin and takes POSIX repeated flags (`--type cli --type gui`)
- Headless CLI (linux/WSL TUN test): `$env:GOOS='linux'; $env:CGO_ENABLED='0'; go build -tags "headless with_gvisor" -o build/bin/flowweaver-linux ./cli` (must include `with_gvisor`, otherwise TUN fails with "gVisor is not included in this build") then run in WSL2 as root: `./flowweaver start && ./flowweaver tun on`, test with `curl -k -m 15 <url>`, cleanup `./flowweaver tun off && ./flowweaver stop`; note: `stop` is async, so wait for processes to fully exit (`pgrep -x flowweaver`) before the next `start` — core RPC is token-isolated (a stale instance can no longer kill a newly started core), but a zero-gap restart can still race the TUN device teardown and leave the data plane degraded
- Frontend dev: `cd frontend && npm run dev`
- Frontend install: `cd frontend && npm install`
- Go mod tidy: `go mod tidy`
- Go build check: `go build -tags with_gvisor ./...`
- Go test: `go test -tags with_gvisor ./...`
- Go vet: `go vet -tags with_gvisor ./...`
- Frontend type check: `cd frontend && ./node_modules/.bin/tsc --noEmit` (authoritative; `vite build` does NOT run strict type checks, so it can miss TS errors)
- Tests exist and pass for `flowweaver/app`, `flowweaver/common`, `flowweaver/core` and `flowweaver/internal/rulesguard` (verified: `go test -tags with_gvisor ./...` is green; add new tests beside the package they guard)

# Project Overview

FlowWeaver is a Windows local proxy tool using ECH Injection, TLS-RF Fragmentation, QUIC Connection Rebuilding, and Lightweight Server Mode Relay. Frontend: React 19 + Wails v3 + MUI + Vite 8. Backend: Go 1.27+ with Wails v3 bindings. Its proxy engine lives in `snishaper-core` (see below); this repository holds the app, CLI, GUI and rule data.

# Architecture

- `/main.go` — entry point, Wails app + tray setup
- `/app/` — Go backend: App struct, API bindings (`app_api.go`), system proxy (`app_system.go`), autostart
- `/core/` — Mihomo core process management (`core_client.go`), TUN, admin elevation
- `/proxy` — Proxy server (MITM, SOCKS5, TLS fragmentation, TUN flow, auto-routing, rules): no longer a local directory, imported from snishaper-core as `proxy`
- `/pkg/` — Packages: local `platform` only; sysproxy, certmanager (CA), CF IP pool, DoH resolver, netiface, singtun, subscription and TLS fragmentation all live in snishaper-core
- `gfwlist` — GFWList domain list (`github.com/SnishaperTeam/snishaper-core/gfwlist`): local file/reader loading + `IsBlocked` suffix match, wired into core's `proxy/auto_routing.go`. NOTE: online update (fetch from URL, cache write) was removed; only local `gfwlist_cache.txt` (next to rules file) is loaded at startup, no network calls
- `evolution` — Rule evolution testing (tester, rule generator, result analysis) is `github.com/SnishaperTeam/snishaper-core/evolution`; there is no local `/evolution/` directory
- `/frontend/src/` — React SPA: 11 pages (Dashboard, Proxies, Rules, Routing, DNS, Evolution, Logs, Settings, Subscription, About, Welcome), all lazily loaded
- `/rules/config.json` — Site group rules (~4359 lines), MITM/ECH/SNI-fake configs
- `/config/settings.json` — App settings (port, TUN, theme, Cloudflare IPs)

Data flow: Frontend (React HashRouter) → Wails bindings (`EventsOn` + async Go calls) → App API → proxy/core subsystems. Frontend state sync via `app:state_changed` events from `emitFrontendState()`.

# snishaper-core Dependency

Engine packages are consumed from `github.com/SnishaperTeam/snishaper-core` (pinned in `go.mod`, **no `replace` directive**) instead of being forked. This repo imports directly: `proxy`, `certmanager`, `cfpool`, `dohresolver`, `netiface`, `singtun`, `subscription`, `sysproxy`, `evolution`, `config` and `log`. Two more are reached transitively inside core's `proxy` package and are not imported here: `tlsfrag` (core `proxy/proxy.go`, `proxy/tls_fragment.go`) and `gfwlist` (core `proxy/auto_routing.go`).

`pkg/sysproxy` in this repo is only a thin facade: it holds one `core sysproxy.Manager` instance and re-exports the package-level helpers, so call sites keep working. Core's `Manager` is per-instance (no process-global proxy snapshot) and takes the sudo user through `sysproxy.WithRunAsUser`; the facade reads `SUDO_USER` for it.

- Product-specific behaviour is injected rather than forked: `config.Brand{Name: core.ProductName}` (`core.ProductName = "FlowWeaver"`), `proxy.WithProductName(core.ProductName)` and `subscription.WithProductName(core.ProductName)` (both `subscription.NewStore` call sites). Core requires `^v0.4.0`: v0.4.0 moved `sysproxy` and `evolution` into the library (each app previously carried a byte-identical copy), v0.3.1 fixed the downstream bugs listed next, and v0.3.0 and earlier hardcoded `FlowWeaver` into the subscription download User-Agent and logged every sniffed SNI at info level
- `netiface` is per-instance: construct with `netiface.NewSelector()` and refresh with `(*netiface.Selector).Invalidate()` after TUN start/stop. This replaces the old package-global `netiface.InvalidateCache()`

# Conventions

- Go: standard layout, `flowweaver` module path, no external DI framework
- Frontend: React 19 + TypeScript 7 (`typescript` ^7.0.2), `@/` path alias for `./src/`, MUI v9 + emotion for styling, `@iconify/react` (with `@iconify-icons/material-symbols` and `@iconify-icons/mdi` icon sets) for icons
- Error handling: log via `a.appendLog()`, return errors to frontend; no panic except top-level recover in `main.go`
- Naming: Go `camelCase` for unexported, `PascalCase` for exported; TS `camelCase` for functions/vars, `PascalCase` for components/types
- Imports: Go stdlib → third-party → internal; TS: React → components → API → i18n
- i18n: `I18nContext` provider, `useTranslation()` hook, lazy page loading with `Suspense`
- i18n `t(path, params)` resolves dotted paths against zh/en/ru; a **missing key returns the key itself (truthy)** — so `t(key) || 'fallback'` is dead code and must NOT be written; fallback pattern is `label === key ? fallback : label`
- i18n dynamic keys (`t(\`section.${x}\`)`) MUST have a missing-key guard, otherwise unknown values render as the raw key
- i18n placeholders: param names passed to `t('k', {...})` must match `{param}` placeholders in all three locales (shorthand props like `{ ms }` count as params); keep the key tree structurally identical across zh/en/ru
- i18n coverage: aria-label / placeholder / title / toast / button text must go through `t()`; add new UI copy to ALL of zh/en/ru JSON in the same commit
- Git: commit messages MUST be written in English (user requirement), conventional-commit style, e.g. `refactor: remove GFWList dynamic update and complete i18n`
- Wiki: any change that alters a file's purpose/nature, adds new files, or renames/removes files MUST update the GitHub Wiki in the same change (zh + EN pages; clone `https://github.com/dongle-china/FlowWeaver.wiki.git`, naming: `Page.md` = zh, `EN-Page.md` = en)

# Security & Environment

- Dev certificate: `devcert.pfx`, used to sign MSIX packages (gitignored, not present in a fresh clone; `build_windows.ps1` runs `winapp cert generate --install` when it is missing, or CI restores it from the `MSIX_PFX_BASE64` secret). Do not assume it exists.
- Cert manager lives in snishaper-core (`certmanager`); it generates the CA under the per-user config dir (`common.ConfigCertDir()` → `common.UserConfigDir()/cert`, runtime, gitignored) via `certmanager.InitCertManagerFor(config.Brand{Name: core.ProductName}, certPath)`
- Admin rights required for system proxy registry writes and TUN
- Registry access via `golang.org/x/sys/windows/registry` (not `reg.exe`)
- MSIX packaging requires PFX signing cert; CI uses self-signed

# Evidence Required

- Changes to `app_system.go`, `app_api.go` — verify proxy start/stop + system proxy toggling
- Changes to `rules/config.json` — verify correct site group, MITM/ECH modes
- Changes to frontend event handling — verify `app:state` / `app:state_changed` events
- Changes to core client — verify TUN ready; `wintun.dll` is a runtime artifact (`*.dll` is gitignored, and no `.dll` is tracked in this repo), so verify against the running build, not a tracked file
- Changes to i18n keys/text — verify `tsc --noEmit` passes and zh/en/ru key trees stay consistent (no missing keys, no placeholder mismatches)
