# PalPanel Lite

Single-machine, single-server Palworld dedicated server management panel.

Current implementation status:

- Go backend scaffold.
- SQLite schema for settings, users, mods, backups, and tasks; the app uses a single SQLite connection with a busy timeout to reduce transient local write contention.
- Race-safe single-admin setup/login API with `HttpOnly`, `SameSite=Lax`, and HTTPS-aware `Secure` session cookies.
- First-run setup guide after login for paths, SteamCMD detection, PalServer install, config initialization, backup defaults, and optional REST API readiness.
- Basic app settings API; settings saves are atomic, blocked while backend operations are active, and `pal_server_path` cannot be retargeted away from a running configured PalServer.
- Server status detection for PalServer and SteamCMD paths, including panel-managed versus externally started PalServer state and the backend shared-operation slot.
- Server status includes best-effort local port diagnostics for the game UDP port, REST API TCP port, and RCON TCP port.
- Structured controls for common PalServer launch arguments such as game port, player count, public lobby, performance flags, worker threads, and `-NoMods`; the backend also merges these fields into the raw advanced launch-argument string while preserving unknown custom arguments.
- SteamCMD install/update background tasks; accepted tasks log their pre-operation backup result, and update stops a panel-managed running server first and restarts it after a successful update.
- Native PalServer start/stop/restart controls for the process launched by the panel; restart validates the next start before stopping, start/restart startup admission, update, and restore are blocked before file side effects when unsafe runtime or backend task state is detected, and the UI uses the same local server-action guard for buttons, setup actions, and direct action clicks.
- Realtime task, server process log, and backend operation-lock updates through an authenticated same-origin WebSocket, with origin verification and REST polling fallback; persisted task logs keep the latest 1 MiB with a truncation marker when older output is dropped, the newest 200 finished task records are retained while running tasks are preserved, and individual server log messages are capped before storage/broadcast.
- Visual `PalWorldSettings.ini` editor for common server, rate, world, performance, network, and advanced settings, including REST/RCON, client MOD joins, and the official PvP key set.
- Config initialization from `DefaultPalWorldSettings.ini`, save-time `.bak` backups, and unknown setting preservation; config/default file reads and copies are capped at 4 MiB, config reads are GET-only, initialization uses an explicit POST action, config writes and `.bak` copies use same-directory temporary files plus platform-safe replacement, and initialization/save/manual config backups are blocked while a configured PalServer is running outside the panel or another backend operation is active.
- Zip-based backup records under `data/backups` with manual backup, restore, delete, and non-manual retention; backup creation writes a same-directory temporary archive, caps source file count/per-file/total copied bytes, and only finalizes the `.zip` while the SQLite metadata row can still commit or roll back, skips filesystem symlinks, restore preflights ZIP paths/readability/resource limits, rejects symlink entries before file side effects, read APIs omit unsafe persisted backup paths, locally blocks restore while an external PalServer is detected, replaces restored files atomically per file, and delete/retention commits the SQLite row removal before archive removal while restoring metadata if archive removal fails.
- SteamCMD, server start/restart startup admission, settings/config saves, backup, MOD, restore/delete, and scheduled automatic backup operations share one backend running-operation guard; accepted guard transitions are broadcast to the UI and rejected concurrent operations do not create task records or mutate files. Backup and MOD UI handlers also block duplicate local in-flight actions before sending another request.
- Pre-operation full backups for config saves, SteamCMD install/update, accepted server startup attempts, and restore protection; rejected duplicate starts, malformed launch arguments, and starts blocked by another backend task do not create startup backups.
- Configurable automatic backup scheduler with a 24-hour default interval.
- Best-effort Palworld REST `/save` request before backup creation when REST credentials are configured.
- Local MOD archive upload and installation to `Mods/Workshop`; upload/update requests reserve the backend operation slot before reading the archive body, request bodies are capped at 512 MiB, multipart parsing uses a smaller memory threshold so large archives spill to temporary files, extraction runs inside an operation-scoped workspace with file-count and byte limits, native ZIP uploads reject symlink entries, extracted MOD trees containing symlinks are rejected, external extractors are timeout/output bounded, staged MOD file copies stream through the atomic writer instead of reading whole files into memory, and final installed files are replaced only after SQLite commits the matching MOD row insert/update.
- MOD `Info.json` parsing with a metadata size cap, service-side support detection, PackageName validation before persistence/settings/path use, Workshop folder-name validation before path reuse, enable/disable/update through `PalModSettings.ini`, selected-package update checks, and delete with backup plus `ManagedMods` cleanup; MOD settings reads are capped at 1 MiB and oversized read APIs fail explicitly, MOD settings writes use atomic replacement, enable/disable commits the row before settings writes and restores the row if settings writes fail, install/update restores the previous database state if final replacement fails and reports previous-directory restore failures, and delete stages installed directories before cleanup so later failures can restore files plus compensate the database row and active settings.
- MOD upload/update/enable/disable/delete are blocked while PalServer is running; changes are made while stopped and take effect on the next start or restart.
- MOD rows can request the backend to open the installed `Mods/Workshop/<folder>` directory on the server machine with path constraints.
- Backend-proxied Palworld REST API dashboard for info, metrics, runtime settings, and online players; independent REST reads are fetched concurrently so dashboard refresh waits for the slowest call rather than accumulated endpoint latency, and upstream REST response bodies are capped with explicit oversized-response errors.
- Local dashboard resource summary for CPU, memory, disk, recent logs, recent tasks, and recent backups.
- Player and server actions through the panel backend: broadcast, save world, graceful shutdown, REST stop, kick, ban, and unban. The backend validates REST write payload sizes/ranges before proxying them, and the UI disables these REST write actions while REST/runtime state is unavailable, another REST action is in flight, or a backend operation is active.
- Sensitive values are redacted from API error responses, task logs, and server logs.
- Dangerous operations, including player kick/ban/unban moderation actions, require explicit UI confirmation and an API-level confirmation header.
- Production frontend assets can be embedded into the Go binary for single-file startup.
- Vue 3 + Naive UI frontend scaffold.

Primary platform: Linux only. Native Linux SteamCMD remains the current development and early-operation path; Docker/Compose is the intended final deployment model. Windows runtime support is not a project goal.

## Development

```powershell
go run ./cmd/palpanel -addr 127.0.0.1:8080 -data-dir data
```

The backend listens on `127.0.0.1:8080` by default and stores SQLite data in `data/app.db`. Flags can also be set with `PALPANEL_ADDR` and `PALPANEL_DATA_DIR`.

Frontend development:

```powershell
cd web
npm install
npm run dev
```

Production frontend build:

```powershell
cd web
npm run build
```

This writes the Vite production assets into `internal/frontend/dist` so Go can embed them.

Build verification used by this project:

```powershell
go test ./...
cd web
npm run build
```

## Production Build

Docker/Compose is the preferred deployment path:

```bash
cp .env.example .env
mkdir -p data PalServer
sudo chown -R 10001:10001 data PalServer
docker compose --env-file .env -f deploy/compose.yaml up -d --build
```

The GitHub Actions workflow publishes the runtime image to GHCR on pushes to
`main`, version tags matching `v*`, and manual dispatches. Use the published
image by setting `PALPANEL_IMAGE` and running Compose without `--build`:

```bash
PALPANEL_IMAGE=ghcr.io/<owner>/palpanel-lite:latest
docker compose --env-file .env -f deploy/compose.yaml up -d
```

Default Compose behavior:

- Binds the panel to `127.0.0.1:8080`.
- Publishes the Palworld game UDP port as `8211:8211/udp`.
- Stores panel data in `./data`.
- Mounts the Palworld server directory at `./PalServer`.
- Seeds a fresh panel database with `pal_server_path=/palserver`; existing settings are not overwritten on container restarts.
- Includes SteamCMD through `/usr/local/bin/steamcmd`; inside the container, leave `steamcmd_path` empty so the panel detects it from `PATH`.
- Stores SteamCMD's mutable runtime files under `/data/steamcmd` by default, so they persist in the panel data volume and remain writable when `PALPANEL_UID`/`PALPANEL_GID` are customized.
- Sets the container home directory to `/data` so SteamCMD and Steam runtime user files such as `~/.steam` stay on the writable data volume.
- Runs with Compose `init: true` and a 60-second stop grace period so the panel can stop a panel-managed PalServer cleanly and child processes are reaped.
- Runs the container as UID/GID `10001` by default.

The Docker runtime image is currently intended for Linux amd64 hosts because SteamCMD and Palworld Dedicated Server are x86_64 targets.
Palworld REST/RCON ports are not published by the default Compose file; access those operations through the panel backend instead of exposing them directly.

`.env` is ignored by Git and is the intended place for local deployment overrides. The checked-in `.env.example` includes:

- `PALPANEL_VERSION` for the image tag and embedded build version.
- `PALPANEL_IMAGE` for the full Compose image reference, including GHCR images.
- `PALPANEL_PORT` for the localhost panel port.
- `PALWORLD_GAME_HOST_PORT` and `PALWORLD_GAME_PORT` for the UDP game port mapping.
- `PALPANEL_DATA_DIR` and `PALPANEL_SERVER_DIR` for bind mounts.
- `PALPANEL_STEAMCMD_DIR` for SteamCMD's in-container writable state directory.
- `PALPANEL_DEFAULT_PAL_SERVER_PATH` for the initial persisted `pal_server_path` in a fresh panel database.
- `PALPANEL_UID` and `PALPANEL_GID` for the container user.
- `TZ` for container timezone.

The example uses `../data` and `../PalServer` because relative bind paths are resolved from `deploy/compose.yaml`; use absolute paths for production directories:

```bash
PALPANEL_PORT=18080
PALWORLD_GAME_HOST_PORT=8211
PALWORLD_GAME_PORT=8211
PALPANEL_DATA_DIR=/srv/palpanel/data
PALPANEL_SERVER_DIR=/srv/palworld/PalServer
PALPANEL_STEAMCMD_DIR=/data/steamcmd
PALPANEL_DEFAULT_PAL_SERVER_PATH=/palserver
PALPANEL_UID=10001
PALPANEL_GID=10001
TZ=Asia/Hong_Kong
```

Build the web UI first, then build the Linux Go binary:

```bash
cd web
npm ci
npm run build
cd ..
GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=2.0.0" -o palpanel ./cmd/palpanel
```

Run it:

```bash
./palpanel -addr 127.0.0.1:8080 -data-dir ./data
```

Native Linux execution remains useful for development, diagnostics, and early self-hosting.

`pal_server_path` is still the authoritative configured server directory. On a fresh database, `PALPANEL_DEFAULT_PAL_SERVER_PATH` can seed that setting once. If it remains empty, the panel tries to detect an already installed PalServer from `PALPANEL_PAL_SERVER_PATH`, `PALWORLD_SERVER_PATH`, `PAL_SERVER_PATH`, the current/executable directory, and a few common SteamCMD install paths.

The process handles `SIGINT`/`SIGTERM` by gracefully shutting down the HTTP server, stopping any PalServer process launched by the panel, waiting for app-owned background tasks such as the automatic backup scheduler to exit, and then closing app resources.

## Service Install

Linux systemd template for native deployment:

```bash
sudo install -d -o palpanel -g palpanel /opt/palpanel-lite
sudo install -m 0755 palpanel /opt/palpanel-lite/palpanel
sudo install -m 0644 deploy/palpanel-lite.service /etc/systemd/system/palpanel-lite.service
sudo systemctl daemon-reload
sudo systemctl enable --now palpanel-lite
```

Adjust `User`, `Group`, `WorkingDirectory`, `ExecStart`, and data paths in `deploy/palpanel-lite.service` before installing on a real server. Windows service installation is intentionally out of scope.

Keep the panel bound to `127.0.0.1` unless you have a trusted reverse proxy or VPN in front of it. If a reverse proxy terminates HTTPS, it must set `X-Forwarded-Proto: https` so panel session cookies are marked `Secure`. Do not expose the Palworld REST API directly to the Internet. JSON API request bodies are capped at 1 MiB, must contain exactly one JSON value, and reject duplicate or unknown object field names; trailing whitespace is allowed. Authenticated action routes with no request-body contract reject unexpected non-empty bodies. `PalWorldSettings.ini` and `DefaultPalWorldSettings.ini` reads/copies are capped at 4 MiB for config GET/init/save/manual `.bak` flows, and oversized saves are rejected before backup or write side effects. Backend-proxied Palworld REST write actions cap operator messages at 500 characters, player user IDs at 128 characters, and graceful-shutdown wait time at 3600 seconds. Persisted task logs retain the latest 1 MiB per task and add a truncation marker when older output is dropped; only the newest 200 finished task records are retained, while running tasks are not pruned; individual server log messages are capped at 8 KiB before API/WebSocket exposure. Backup creation and restore both reject more than 200000 files, any single file/entry above 8 GiB, or total source/expanded content above 64 GiB. MOD archive upload/update bodies are capped at 512 MiB; extracted MOD content is capped at 100000 files, 2 GiB per file, and 8 GiB total; `Mods/PalModSettings.ini` reads are capped at 1 MiB before active-list derivation or settings rewrites; staged MOD file copies stream through atomic writes; external extractor runtime is capped at 5 minutes and extractor output at 64 KiB. API errors and logs redact known password/auth fields, but you should still avoid pasting secrets into free-form names or messages. Browser-originated unsafe REST requests with a cross-origin `Origin` header are rejected; local scripts and API clients without `Origin` remain supported. Config initialization is an explicit POST action; `GET /api/config` does not create files. Dangerous routes such as server update/stop/restart, backup restore/delete, MOD update/delete, and player kick/ban/unban require `X-Palpanel-Confirm: true` after the UI confirmation. If PalServer was started outside the panel, stop it outside the panel before config initialization/save, update, restore, or MOD mutation operations. Wait for running backend operations to finish before changing settings, starting/stopping/restarting the server, using runtime REST actions from the UI, or initializing, backing up, or saving config. The UI locally blocks invalid server actions for active backend operations, external PalServer state, missing PalServer binaries, duplicate starts, non-panel-managed stops, and in-flight server actions; it also locally blocks config initialization and backup restore when an external PalServer would make those requests unsafe. Do not change `pal_server_path` away from a running configured server. Stop PalServer before MOD changes, then start or restart it for the changes to apply. Back up before updates, restores, and MOD changes; MODs can corrupt saves or crash the server.
