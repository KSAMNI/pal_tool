# Palworld Server Panel Plan

## Goal
Design a deployment and management panel for dedicated Palworld servers, starting with research and an implementation plan.

## Scope
- Research Palworld dedicated server deployment and operations.
- Identify management features, platform assumptions, and architecture options.
- Produce a practical phased implementation plan for this repository.
- Revise the plan toward a single-machine, single-server, self-use panel with MOD management.

## Phases

### Phase 1: Project Context
Status: complete

Confirm current repository state and create persistent planning files.

### Phase 2: Research
Status: complete

Collect references for Palworld dedicated server install, configuration, management, RCON/admin APIs, backup/update behavior, containerization, and security considerations.

### Phase 3: Product Requirements
Status: complete

Define target users, supported platforms, core workflows, and MVP/non-MVP boundaries.

### Phase 4: Technical Plan
Status: complete

Choose architecture, data model, service boundaries, deployment method, and implementation stack assumptions.

### Phase 5: Implementation Roadmap
Status: complete

Break the work into concrete milestones with validation steps.

### Phase 6: Single-Server Plan Revision
Status: complete

Incorporate user-provided plan correction: remove multi-instance/Docker-first assumptions, prioritize native SteamCMD deployment, backups, and MOD management.

### Phase 7: Linux-First Correction
Status: complete

Historical correction to Linux native SteamCMD first, with Windows native support second for complete server-side MOD management. This Windows-second direction is superseded by Phase 114: the current product target is Linux-only, with Docker/Compose as the intended final deployment model.

### Phase 8: v0.1 Project Skeleton
Status: complete

Create the initial Go backend, SQLite schema, authentication/setup API, settings API, server status detection, and Vue 3 + Naive UI frontend skeleton.

### Phase 9: v0.1 Server Control
Status: complete

Implement the first usable server-control loop: SteamCMD install/update tasks, native PalServer start/stop/restart, task/log APIs, and frontend controls wired to those backend APIs.

### Phase 10: v0.2 Config Editor
Status: complete

Implement the first visual configuration editor for `PalWorldSettings.ini`: platform-aware path resolution, initialization from `DefaultPalWorldSettings.ini`, `OptionSettings=(...)` parsing/writing for common typed fields, save-time `.bak` backups, API routes, tests, and frontend form controls.

### Phase 11: v0.3 Backup And Restore
Status: complete

Implement backup and restore basics: zip archives under `data/backups`, SQLite backup metadata, manual/pre-operation backup APIs, automatic retention for non-manual backups, restore with a protective `pre_restore` backup, deletion with path checks, and frontend backup management controls.

### Phase 12: v0.4 MOD Management Core
Status: complete

Implement first local MOD management loop: upload archive, extract safely, find and parse `Info.json`, detect server support, install to `Mods/Workshop/<folder>`, persist MOD metadata, enable/disable via `Mods/PalModSettings.ini`, delete installed files after backup, expose API routes, tests, and frontend controls.

### Phase 13: v0.5 REST Dashboard And Player Management
Status: complete

Implement Palworld REST API proxying for dashboard/player workflows: backend-only Basic Auth, info/metrics/player aggregation, player list, broadcast, kick, ban, unban, save-world actions, tests with a fake Palworld REST server, and frontend dashboard/player controls.

### Phase 14: v0.6 Packaging And Install Experience
Status: complete

Implement first packaging/install loop: embed the production Vue frontend into the Go binary, preserve a local development path, add runtime flags/environment defaults, improve first-run path detection for SteamCMD and PalServer, provide Linux systemd and Windows service installation guidance, document build/release commands, and verify the packaged binary with smoke checks.

### Phase 15: v0.7 Dashboard Completeness And Graceful REST Stop
Status: complete

Fill the remaining dashboard/operations gaps from the Chinese project plan: include CPU, memory, disk, recent server logs, recent task logs, and recent backups in the dashboard payload/UI; expose Palworld REST graceful shutdown/stop proxy actions through the backend and frontend; add focused tests and verification.

### Phase 16: v0.8 Automatic Backup Scheduler And Save-Before-Backup
Status: complete

Implement the remaining v0.3 backup reliability work: configurable daily/interval automatic backups, startup background scheduler, single-flight backup execution, latest-auto-backup due checks, best-effort Palworld REST `/save` before backup creation, UI settings for the schedule, and tests for retention/scheduling/save behavior.

### Phase 17: v0.9 Log And API Error Redaction
Status: complete

Harden sensitive-data handling across API errors and logs: add centralized redaction for Authorization/Basic auth strings, Palworld admin/server passwords, REST API passwords, and JSON/form password fields; apply it to `writeError`, task logs, server logs, and Palworld REST error bodies; add focused tests proving secrets do not leak through API responses or persisted logs.

### Phase 18: v1.0 Realtime Logs And Task Events
Status: complete

Close the remaining realtime operations gap from the product plan: add an authenticated WebSocket event endpoint for server logs and task changes, broadcast updates from the existing log/task write paths, wire the Vue UI to consume those events, keep REST polling as a reconnect/fallback path, and verify the stream with backend tests plus normal build/smoke checks.

### Phase 19: v1.1 Selected MOD Update Flow
Status: complete

Implement the explicit MOD update operation from the API/UI plan: add `POST /api/mods/{id}/update` for multipart archive updates, require the uploaded archive `PackageName` to match the selected MOD, reuse the backup-guarded install/update path, add backend tests for successful and mismatched updates, and expose a per-MOD update upload action in the Vue UI.

### Phase 20: v1.2 Dangerous Operation Confirmation Enforcement
Status: complete

Enforce the safety-rule "dangerous operations require second confirmation" on the backend, not only in the UI: require an explicit confirmation header for server update/stop/restart, Palworld REST shutdown/stop, backup restore/delete, MOD update/delete; update the Vue API helpers and confirm controls to send that header after user confirmation; add focused tests for rejected unconfirmed requests and accepted confirmed requests.

### Phase 21: v1.3 Backup And MOD Operation Task Records
Status: complete

Complete the task-log/final-status requirement for long-running local operations beyond SteamCMD: record manual backup, backup restore/delete, MOD upload/update/enable/disable/delete as tasks with start/completion/failure log entries, preserve the existing synchronous API responses, broadcast task updates through the realtime event channel, and add focused tests for task persistence.

### Phase 22: v1.4 MOD Install Directory Opener
Status: complete

Complete the remaining MOD page operation from the Chinese project plan: expose a safe authenticated backend action to open a selected MOD's installed `Mods/Workshop/<folder>` directory on the server machine, constrain the target path under the configured PalServer directory, report clear errors when the directory or platform opener is unavailable, wire the Vue MOD row action, and add focused backend tests.

### Phase 23: v1.5 Safe Server Update Lifecycle
Status: complete

Align the native SteamCMD update path with the product safety model and official update guidance: when `server_update` is requested while a panel-managed PalServer process is running, record that state in the task, stop the server before running SteamCMD, restart it after a successful update, leave it stopped after a failed update, and add focused tests for the update lifecycle.

### Phase 24: v1.6 Palworld REST Settings Read
Status: complete

Complete the remaining Palworld REST read endpoint from the product plan: proxy official REST `GET /settings` through the backend without exposing Basic Auth, include the runtime settings snapshot in the dashboard payload/UI, keep empty/error response shapes stable, and add focused backend tests for Basic Auth forwarding and dashboard serialization.

### Phase 25: v1.7 MOD ManagedMods Cleanup
Status: complete

Complete the remaining MOD delete cleanup detail from the product plan and official MOD behavior: when deleting a MOD, also remove the server-generated `Mods/ManagedMods/<PackageName>` directory when present, constrain cleanup under `Mods/ManagedMods`, avoid path traversal from persisted package names, and add focused tests for removal and path safety.

### Phase 26: v1.8 External PalServer Runtime Detection And Guards
Status: complete

Close the runtime-state safety gap for self-use deployments where PalServer may be started outside the panel: detect configured PalServer processes that are not panel-managed, expose managed/external running state in the status payload/UI, prevent panel start/update/restore from mutating files while an external server is running, preserve panel-managed stop/restart behavior, and add focused backend tests with injectable detection.

### Phase 27: v1.9 Structured Launch Argument Controls
Status: complete

Complete the remaining startup/network usability gap from the Chinese plan: expose common PalServer launch arguments as structured settings controls, especially the game listen port (`-port`), while preserving the raw `server_launch_args` string for advanced/unknown arguments. Cover official flags for player count, public lobby, performance flags, worker threads, and `-NoMods`; add backend launch-argument summary tests and frontend controls.

### Phase 28: v2.0 Advanced Config PvP And Client MOD Controls
Status: complete

Complete the remaining v0.2 advanced configuration coverage from the Chinese plan: add visual controls for client MOD joins and PvP-related official settings. Map `bAllowClientMod`, `bIsPvP`, `bEnablePlayerToPlayerDamage`, and `bEnableDefenseOtherGuildPlayer` through the config parser/writer, expose them in the advanced tab, preserve unknown config fields, and add focused config tests.

### Phase 29: v2.1 Product Plan Contract Sync
Status: complete

Synchronize `palworld_panel_plan.md` with the implemented backend contract after phases 18-28: update the app settings key list, remove the obsolete `admin_password_encrypted` setting, document derived launch-argument fields, add auth/dashboard/task/event/runtime-settings/shutdown/rest-stop/MOD open-dir API routes, and align dangerous-operation confirmation notes with the backend-enforced header.

### Phase 30: v2.2 Performance-Sensitive Config Controls
Status: complete

Expand the visual `PalWorldSettings.ini` editor with typed controls for official performance-sensitive settings that were still missing after v2.0: `BaseCampMaxNumInGuild`, `MaxBuildingLimitNum`, and `ServerReplicatePawnCullDistance`. Preserve unknown config fields, apply conservative validation/defaults, expose the fields in the world/performance section of the Vue UI, update docs, and add focused parser/writer tests.

### Phase 31: v2.3 Restore Lifecycle External Runtime Safety
Status: complete

Harden backup restore lifecycle around panel-managed and external runtime state: when restoring while a panel-managed server is running, stop it through the injectable lifecycle hook, re-check for externally running PalServer before extracting files, restart only after a successful restore, and add focused tests for restart-on-success plus mixed managed/external runtime rejection without mutating save files.

### Phase 32: v2.4 MOD Runtime Safety And Restart Cues
Status: complete

Close the remaining MOD runtime-safety gap: reject MOD upload/update/enable/disable/delete while the configured PalServer is running, whether panel-managed or external, prove rejected operations do not mutate MOD files/settings, and make the frontend MOD card clearly communicate stop-before-change plus restart-required behavior.

### Phase 33: v2.5 Port Diagnostics
Status: complete

Complete the remaining `system` module port-detection responsibility from the product plan: report whether the configured game UDP port, REST API TCP port, and RCON TCP port are available or already in use, expose the diagnostics in the server/settings UI, document the behavior, and add focused backend tests.

### Phase 34: v2.6 First-Run Setup Guide
Status: complete

Complete the remaining v0.6 first-run Web guided initialization requirement: after admin login, show a concise setup checklist that turns the existing status/settings/config/backup signals into actionable steps for configuring paths, detecting SteamCMD, installing PalServer, initializing config, enabling backup defaults, and optionally connecting the Palworld REST API.

### Phase 35: v2.7 Config Runtime Safety And Restart Cues
Status: complete

Close the remaining config runtime safety gap: reject `PalWorldSettings.ini` initialization and saves while the configured PalServer is externally running before creating backups or writing files, prove rejected saves do not mutate config or backup state, and make the frontend config card clearly communicate the external-stop and restart-required behavior.

### Phase 36: v2.8 SteamCMD Task Admission And Backup Observability
Status: complete

Harden the SteamCMD install/update task lifecycle: reject new SteamCMD tasks before creating `pre_update` backups when another task is already running, move the pre-operation backup into the accepted task's log-visible execution path, and prove accepted tasks create/log the backup while rejected tasks leave backup state unchanged.

### Phase 37: v2.9 Backend Structured Launch Settings Merge
Status: complete

Complete the `/api/settings` contract for structured launch controls: when clients save derived fields such as `game_port`, `launch_players`, `public_lobby`, `no_mods`, `performance_flags`, and `worker_threads`, merge them into `server_launch_args` on the backend while preserving unknown custom arguments, removing disabled structured flags, keeping password preservation behavior, and proving the persisted/reloaded response matches the merged raw launch string.

### Phase 38: v3.0 Server Start Admission And Startup Backup Observability
Status: complete

Harden the server start lifecycle so rejected starts do not create startup backup side effects: reject duplicate panel-managed starts and malformed launch arguments before `startup` backup creation, preserve external-runtime protection, keep accepted start attempts creating a `startup` backup before spawning PalServer, add focused backend tests for no-backup rejection and accepted backup creation, and update docs/progress.

### Phase 39: v3.1 Restart Preflight Before Stop
Status: complete

Harden the server restart route so it validates the configured PalServer binary, external-runtime state, and launch arguments before stopping a panel-managed running server; use the existing injectable lifecycle hooks for restart tests, prove malformed launch arguments and external-runtime conflicts do not call stop/start, preserve accepted restart behavior, and update docs/progress.

### Phase 40: v3.2 Operation Task Admission Unification
Status: complete

Unify the global running-task admission guard across SteamCMD tasks, backup/MOD operation tasks, and scheduled automatic backups so only one mutating long operation can run at a time. Rejected operation tasks must not create task records or mutation side effects, accepted operations must reserve and release the shared task slot reliably, automatic backups should skip while another task is active, and focused backend tests plus docs should prove the behavior.

### Phase 41: v3.3 Player Moderation Confirmation Enforcement
Status: complete

Extend dangerous-operation confirmation to Palworld REST player moderation actions: require `X-Palpanel-Confirm: true` for kick, ban, and unban backend routes; update the Vue player controls to ask for confirmation and send the header; add focused backend tests for rejected unconfirmed moderation requests and accepted confirmed requests; update README, product plan, and progress.

### Phase 42: v3.4 Config Mutation Task Admission
Status: complete

Extend the backend running-operation guard to config mutations: reject `GET /api/config` initialization, `PUT /api/config`, and `POST /api/config/backup` while another task is active before creating config files, backups, or `.bak` files; reserve the same operation slot during accepted config saves/backups; update the Vue config controls to respect active tasks; add focused no-mutation tests and docs.

### Phase 43: v3.5 Server Start Task Admission
Status: complete

Extend the backend running-operation guard to server start and restart startup paths: reject `POST /api/server/start` and accepted restart start phases while another task is active before creating `startup` backups or spawning PalServer; release the slot after start admission completes; add focused no-backup/no-start tests and docs.

### Phase 44: v3.6 Frontend Active Task Lock Consistency
Status: complete

Align the Vue operation controls with the backend single-running-task model: disable and locally guard server start/restart plus manual backup/restore/delete while another backend task is active, keep read-only refresh actions available, update the setup guide backup action state, and verify the frontend build plus normal backend tests.

### Phase 45: v3.7 MOD Active Task UI Lock
Status: complete

Align MOD mutation controls with the backend running-task guard: disable and locally guard MOD upload/update/enable/disable/delete while another backend task is active, preserve read-only MOD refresh/info/open-directory actions, and verify the frontend build plus normal backend tests.

### Phase 46: v3.8 Settings Save Admission And Retarget Safety
Status: complete

Harden `PUT /api/settings` so settings saves cannot race with an active backend operation and cannot retarget `pal_server_path` away from a currently running configured PalServer; add backend tests proving rejected saves do not mutate SQLite settings, update the Vue save/settings guide state, and verify normal backend/frontend checks.

### Phase 47: v3.9 Runtime Action UI Admission
Status: complete

Align runtime action controls with the current operation-safety model: disable and locally guard Palworld REST write actions when REST/runtime state is unavailable, another REST action is in flight, or a backend operation is active; disable and locally guard panel-managed server stop while another backend operation is active so accepted update/restore/restart lifecycles are not accidentally interrupted; preserve read-only dashboard/player refresh behavior and backend API compatibility.

### Phase 48: v3.10 Managed Retarget Regression Coverage
Status: complete

Add focused backend route coverage proving `PUT /api/settings` rejects `pal_server_path` retargets while the previously configured PalServer is panel-managed and running, and that the rejected save leaves existing settings unchanged.

### Phase 49: v3.11 Operation Slot Visibility
Status: complete

Expose the backend shared running-operation slot through server status so synchronous guarded operations that do not create task rows are still visible to the UI; update the frontend active-operation checks to use both persisted running tasks and the status-level operation flag; add focused backend tests and verify the frontend build.

### Phase 50: v3.12 WebSocket Same-Origin Regression Coverage
Status: complete

Make the realtime `/api/events` same-origin security contract explicit in code and tests: keep authenticated same-origin WebSocket connections working, reject authenticated cross-origin WebSocket attempts, and preserve the existing unauthenticated rejection and runtime snapshot behavior.

### Phase 51: v3.13 Settings Save Atomicity
Status: complete

Harden `PUT /api/settings` persistence so all `app_settings` key updates are committed atomically; if any SQLite write fails, the route must return an error and leave the previously persisted settings unchanged. Add a focused rollback regression test.

### Phase 52: v3.14 Realtime Operation Slot Events
Status: complete

Broadcast shared running-operation slot transitions over `/api/events` so short synchronous guarded operations immediately update UI locks without waiting for REST polling. Add backend regression coverage and update the frontend event consumer/docs.

### Phase 53: v3.15 Dashboard REST Fan-Out
Status: complete

Fetch Palworld REST dashboard endpoints concurrently so `/api/dashboard` waits for the slowest REST call rather than serially accumulating `/info`, `/metrics`, `/settings`, and `/players` latency. Preserve partial-error response behavior and add focused regression coverage.

### Phase 54: v3.16 Process Signal Shutdown
Status: complete

Handle SIGINT/SIGTERM in the production entrypoint with `http.Server.Shutdown`, stop accepting new requests, let in-flight requests finish briefly, and then close the app so background loops and WebSocket clients see the stop signal.

### Phase 55: v3.17 App Background Shutdown Wait
Status: complete

Make `App.Close` wait for app-owned background goroutines, especially the automatic backup scheduler, before closing SQLite. Add focused lifecycle coverage so shutdown cannot close the database while a background operation is still unwinding.

### Phase 56: v3.18 Unsafe REST Origin Guard
Status: complete

Reject browser-originated cross-origin unsafe REST requests (`POST`, `PUT`, `PATCH`, `DELETE`) when an `Origin` header is present, while preserving same-origin browser requests and non-browser/API clients without `Origin`. Add route-level regression coverage and document the same-origin REST write contract.

### Phase 57: v3.19 HTTPS-Aware Session Cookies
Status: complete

Mark panel session cookies as `Secure` whenever the request is HTTPS, including trusted reverse-proxy deployments that set `X-Forwarded-Proto: https`, while preserving local HTTP development behavior. Cover setup/login/logout cookie attributes with focused route tests and document the reverse-proxy cookie contract.

### Phase 58: v3.20 WebSocket Shutdown Regression Coverage
Status: complete

Pin the realtime shutdown contract with a regression test: an authenticated `/api/events` client should receive a going-away close when `App.Close` signals shutdown, and the app should unregister the event client instead of leaving a stale subscription.

### Phase 59: v3.21 Atomic First-Run Setup
Status: complete

Make single-admin first-run setup race-safe: concurrent `POST /api/auth/setup` requests must create at most one user, return conflict for losers, and leave the users table with exactly one admin. Add a focused concurrency regression test and preserve the existing setup/login cookie behavior.

### Phase 60: v3.22 Restore Preflight No Directory Side Effects
Status: complete

Harden backup restore preflight ordering so rejected restores do not create the configured `pal_server_path` directory before runtime safety checks pass. Add regression coverage proving an external-runtime rejection leaves a missing server directory uncreated and creates no protective backup or restore file side effects.

### Phase 61: v3.23 MOD Upload Body Limit
Status: complete

Enforce a real HTTP request-body limit for MOD upload/update multipart archives instead of relying on `ParseMultipartForm`'s memory threshold. Keep the intended 512 MiB archive limit, clean up multipart temporary files, return `413 Request Entity Too Large` for oversized bodies, and add focused parser coverage without constructing a huge test payload.

### Phase 62: v3.24 MOD Upload Admission Before Body Read
Status: complete

Make MOD upload/update reserve the backend running-operation slot before reading multipart bodies, so requests rejected because another operation is active do not read, spool, or parse uploaded archives. Preserve existing task logging for accepted uploads and add regression coverage proving active-operation rejection leaves the request body unread and creates no task.

### Phase 63: v3.25 JSON Request Body Limit
Status: complete

Enforce a real HTTP request-body limit for JSON API endpoints using the shared decoder path, so oversized setup/login/settings/config/player/server JSON requests fail with `413 Request Entity Too Large` instead of being read without bound. Add focused coverage for oversized JSON rejection while preserving normal authenticated setup/login behavior.

### Phase 64: v3.26 Strict Single JSON Bodies
Status: complete

Reject JSON API request bodies that contain more than one JSON value, so malformed payloads such as `{...}{...}` cannot be partially accepted by the shared decoder. Preserve trailing whitespace tolerance and add focused decoder coverage.

### Phase 65: v3.27 Duplicate JSON Field Rejection
Status: complete

Reject JSON API request bodies with duplicate object member names through the shared decoder, so ambiguous payloads such as `{"path":"a","path":"b"}` cannot silently use the last value. Preserve nested object/array parsing, the 1 MiB body limit, single-value enforcement, and trailing whitespace tolerance.

### Phase 66: v3.28 Unknown JSON Field Rejection
Status: complete

Reject unknown fields in structured JSON API request bodies through the shared decoder, so misspelled or unsupported settings/config/action fields return `400 Bad Request` instead of being silently ignored. Preserve existing frontend payload compatibility, duplicate-field rejection, single-value enforcement, and the 1 MiB body limit.

### Phase 67: v3.29 Backup Delete DB/File Consistency
Status: complete

Harden manual backup deletion and automatic backup retention pruning so backup archive files are not removed before SQLite accepts the matching row deletion. Roll back the database deletion when archive removal fails, preserve path checks, and add focused coverage proving a database delete failure leaves the archive file and row intact.

### Phase 68: v3.30 MOD Delete DB/File Consistency
Status: complete

Harden MOD deletion so SQLite accepts the mod row deletion before `PalModSettings.ini`, `Mods/Workshop`, or `Mods/ManagedMods` are mutated. Preserve pre-delete backups, path checks, and runtime safety, and add focused coverage proving a database delete failure leaves MOD files, managed files, settings, and the row intact.

### Phase 69: v3.31 MOD Install/Update DB/File Consistency
Status: complete

Harden MOD upload/install and selected update so final `Mods/Workshop/<folder>` files are not replaced before SQLite accepts the matching mod row insert/update. Stage extracted files under the workshop directory, preserve existing installed files when database writes fail, and add focused coverage for both new-install insert failure and existing-mod update failure.

### Phase 70: v3.32 Restore Archive Preflight
Status: complete

Validate backup ZIP archives before restore creates directories, protective backups, or writes server files. The preflight must prove every archive entry path stays under the configured server root and every file entry can be read successfully, so corrupt archives do not partially overwrite live files before failing.

### Phase 71: v3.33 Backup Symlink Boundary Safety
Status: complete

Prevent backup creation and restore from crossing the configured server-root boundary through symlinks. Backup creation should skip symlink entries instead of following them, and restore preflight/extraction should reject ZIP entries encoded as symlinks before any file side effects.

### Phase 72: v3.34 MOD Enable/Disable DB/File Consistency
Status: complete

Harden MOD enable/disable so SQLite accepts the `mods.enabled` row update before `PalModSettings.ini` is mutated, and roll the database update back if settings mutation fails. Preserve existing runtime safety, pre-MOD backups, task records, and add focused coverage proving a database update failure leaves both the settings file and row unchanged.

### Phase 73: v3.35 Atomic Config File Writes
Status: complete

Harden `PalWorldSettings.ini` initialization, saves, and `.bak` copies so config file writes go through a same-directory temporary file and platform-safe replacement instead of direct truncating writes. Preserve existing config parsing and backup behavior, add focused coverage proving failed replacement leaves the existing config file unchanged and temporary files are cleaned up, and keep Windows replacement semantics correct.

### Phase 74: v3.36 Atomic MOD Settings Writes
Status: complete

Harden `Mods/PalModSettings.ini` updates so enable/disable/install/delete paths write through the shared same-directory atomic replacement helper instead of direct truncating writes. Preserve existing MOD settings rendering and DB rollback behavior, and add focused coverage proving failed replacement leaves the existing MOD settings file unchanged and temporary files are cleaned up.

### Phase 75: v3.37 Atomic Restore File Replacement
Status: complete

Harden backup restore extraction so each restored file is written to a same-directory temporary file and platform-safe replacement is used instead of opening the target with truncate. Preserve restore preflight, path checks, and symlink rejection; stream ZIP entry contents to avoid loading large backups into memory; add focused coverage proving a failed replacement leaves the existing target file unchanged and cleans temporary files.

### Phase 76: v3.38 MOD Archive Symlink Safety
Status: complete

Harden MOD archive processing so native ZIP uploads reject symlink entries and extracted MOD metadata discovery ignores symlinked `Info.json` files. Preserve existing path checks and staging behavior, and add focused coverage for both ZIP symlink rejection and symlinked metadata not being trusted.

### Phase 77: v3.39 Extracted MOD Tree Symlink Rejection
Status: complete

Harden MOD archive processing after extraction so any symlink left in the extracted tree is rejected before metadata discovery or staging. This covers external 7z/RAR extractors as well as any future extraction path, preserves native ZIP symlink rejection, and adds focused coverage proving extracted symlinks fail the install preflight.

### Phase 78: v3.40 MOD Extraction Workspace Containment
Status: complete

Harden MOD upload/update extraction workspace handling so each operation stores the uploaded archive and extracted content inside one operation-scoped temporary directory, then rejects any extractor output outside the intended content subdirectory before metadata discovery. This improves containment for external 7z/RAR extractors, preserves existing native ZIP path checks and extracted-tree symlink rejection, and adds focused workspace-boundary coverage.

### Phase 79: v3.41 MOD Install/Update Commit-Before-Replacement
Status: complete

Harden MOD upload/install and selected update so the SQLite insert/update transaction commits before final `Mods/Workshop/<folder>` replacement occurs. Preserve backup, staging, runtime safety, and existing replacement rollback behavior; add focused coverage proving a database commit failure leaves the previously installed files unchanged.

### Phase 80: v3.42 MOD Delete Mutation Compensation
Status: complete

Harden MOD deletion so failures while disabling `PalModSettings.ini` or removing `Mods/Workshop` / `Mods/ManagedMods` after the database row deletion is accepted compensate the database row and active settings back to their previous state where possible. Preserve runtime safety, backups, path constraints, and add focused coverage for directory-removal failure.

### Phase 81: v3.43 Backup Creation DB/File Finalization
Status: complete

Harden backup creation so the final `.zip` archive path is not created before the backup metadata insert is accepted inside SQLite. Keep archive bytes in a same-directory temporary file, insert metadata in a transaction, rename to the final archive path before committing, roll the row back when finalization fails, and add focused coverage proving database insert failure leaves no final backup archive even when cleanup fails.

### Phase 82: v3.44 Backup Delete Commit-Before-Archive Removal
Status: complete

Harden manual backup deletion and retention pruning so the SQLite row deletion commits before the archive file is removed. If archive removal fails after commit, compensate by restoring the backup metadata row; if commit fails, keep the archive file in place. Preserve path checks and add focused coverage for commit failure and archive-removal failure.

### Phase 83: v3.45 MOD Enable Commit-Before-Settings Write
Status: complete

Harden MOD enable/disable so the SQLite `mods.enabled` transaction commits before `Mods/PalModSettings.ini` is written. If the commit fails, keep settings unchanged; if the settings write fails after commit, compensate by restoring the previous MOD row state. Preserve runtime safety, backups, atomic settings writes, and add focused coverage for commit failure and settings-write failure.

### Phase 84: v3.46 MOD Delete Directory Staging Compensation
Status: complete

Harden MOD delete directory removal so `Mods/ManagedMods/<PackageName>` and `Mods/Workshop/<folder>` are moved into a same-directory deletion staging area before cleanup. If staging or cleanup fails, restore any moved directories before restoring the MOD row/settings, so compensation does not leave a restored database row pointing at partially deleted files. Preserve path checks and add focused coverage for a cleanup failure after both directories are staged.

### Phase 85: v3.47 MOD Replacement Restore Error Reporting
Status: complete

Harden MOD install/update directory replacement so if replacing `Mods/Workshop/<folder>` fails after the previous directory has been moved aside, any failure to restore that previous directory is surfaced in the returned error instead of being ignored. Add focused coverage with injected rename failures.

### Phase 86: v3.48 SQLite Busy Handling Configuration
Status: complete

Harden SQLite runtime configuration for the self-use panel by limiting the app database pool to one open connection and setting a busy timeout. This should reduce transient `SQLITE_BUSY` failures between background tasks, task logs, backup metadata writes, and API requests. Add focused coverage for the connection-pool and busy-timeout settings.

### Phase 87: v3.49 Frontend Server Action Guard Consistency
Status: complete

Align the Vue server action entrypoint with the button and setup-guide disabled states. Server install/update/start/stop/restart should all use one local guard that checks active backend operations, current PalServer runtime state, external runtime conflicts, missing PalServer binaries, and in-flight server actions before sending the API request. Preserve backend enforcement and verify the frontend build plus normal backend tests.

### Phase 88: v3.50 MOD Multipart Memory Threshold
Status: complete

Harden MOD upload/update request parsing so the 512 MiB request body cap remains the hard resource limit, while `ParseMultipartForm` uses a small fixed memory threshold and spills larger archive parts to temporary files. Add focused coverage proving an archive larger than the memory threshold is still accepted under the hard cap and is backed by a temporary file, then verify normal backend/frontend checks.

### Phase 89: v3.51 Config Backup External Runtime Admission
Status: complete

Align manual config `.bak` backup with the existing config mutation runtime-safety rule. `POST /api/config/backup` should reject externally running PalServer before creating or copying config files, and the Vue config backup button/handler should use the same local config mutation guard as initialization/save. Add focused no-`.bak` backend coverage, update docs, and verify normal backend/frontend checks.

### Phase 90: v3.52 Frontend Restore External Runtime Guard
Status: complete

Align the Vue backup restore action with the backend restore runtime-safety model. Restore should be locally disabled and guarded when an external PalServer is detected, while manual backup creation and backup deletion remain available under their existing active-operation guard. Update docs/progress and verify frontend/backend checks.

### Phase 91: v3.53 Frontend Config Initialization External Runtime Guard
Status: complete

Align the Vue config refresh/initialization action with the backend config initialization runtime-safety model. When the config file is not already loaded/known to exist, the UI should locally block refresh under an externally running PalServer because the backend `GET /api/config` may initialize `PalWorldSettings.ini`. Existing loaded config reads should remain available, while save and manual `.bak` backup keep their existing config mutation guard. Update docs/progress and verify frontend/backend checks.

### Phase 92: v3.54 Frontend Backup And MOD In-Flight Guard Consistency
Status: complete

Align backup and MOD direct action handlers with their disabled button states. Manual backup, restore, delete, MOD upload, update, enable, disable, delete, and open-directory actions should locally reject duplicate or overlapping in-flight UI actions before sending API requests, while preserving the backend shared operation guard as the authoritative safety layer. Update docs/progress and verify frontend/backend checks.

### Phase 93: v3.55 No-Body Action Request Shape Enforcement
Status: complete

Reject unexpected non-empty request bodies on authenticated action endpoints that have no request-body contract, such as logout, server lifecycle no-body actions, REST save/stop, config backup, backup restore/delete, and MOD enable/disable/open-dir/delete. Preserve routes with explicit JSON or multipart payloads, add focused backend coverage, update docs/progress, and verify normal backend/frontend checks.

### Phase 94: v3.56 Frontend No-Body POST Header Hygiene
Status: complete

Align the Vue API helper with backend no-body action contracts. `apiPost` should omit the JSON `Content-Type` header when no request body is provided, while still sending the confirmation header when required and preserving JSON headers for real payloads. Update docs/progress and verify frontend/backend checks.

### Phase 95: v3.57 Explicit Config Initialization Route
Status: complete

Remove the write side effect from `GET /api/config` by making it read-only. Add an explicit authenticated `POST /api/config/init` action for creating `PalWorldSettings.ini` from defaults/minimal config, with no-body request-shape enforcement, active-operation admission, external-runtime protection, frontend setup/config flow updates, docs, and focused tests.

### Phase 96: v3.58 Config Not-Initialized Frontend State
Status: complete

Treat the expected first-run `GET /api/config` 404 as a typed frontend API state instead of a generic displayed backend error: preserve read-only GET semantics, keep the initialization CTA, show localized setup/config guidance, and verify the frontend build plus normal backend checks.

### Phase 97: v3.59 Player Unban No-Body Contract
Status: complete

Close the remaining no-body action request-shape gap for `POST /api/players/{userid}/unban`: keep kick/ban message payload support, require confirmed unban requests to have an empty body, proxy only the route `userid`, add focused route coverage, update docs/progress, and verify normal backend/frontend checks.

### Phase 98: v3.60 Optional JSON Empty Body Handling
Status: complete

Harden routes with optional JSON request bodies so an actually empty request body is accepted as "no options" while malformed non-empty JSON is still rejected through the shared strict decoder. Apply this to manual backup creation, Palworld REST shutdown defaults, and kick/ban optional message payloads; add focused empty-body and malformed-body coverage; verify normal backend/frontend checks; update docs/progress.

### Phase 99: v3.61 Backup Type API Normalization
Status: complete

Normalize manual backup API `type` input before task creation so task type, start log, backup filename, and persisted backup metadata use the same sanitized/defaulted value. Preserve existing internal backup callers, add focused route coverage for unsafe/blank type input, update docs/progress, and verify normal backend/frontend checks.

### Phase 100: v3.62 MOD PackageName Validation
Status: complete

Validate MOD `Info.json` `PackageName` before persisting, writing `PalModSettings.ini`, or deriving `ManagedMods` paths. Reject line-breaking, path-like, control-character, or Windows-invalid package names that could corrupt settings files or escape directory semantics; preserve normal package names, add focused upload/settings/path tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 101: v3.63 MOD FolderName Validation
Status: complete

Validate persisted MOD `folder_name` values before reusing them for `Mods/Workshop` path derivation, upload/update folder reuse, enable-time `Info.json` checks, delete/open-dir targets, or serialized install paths. Reject path-like, line-breaking, control-character, or Windows-invalid folder names before backups, database changes, settings writes, or filesystem mutation; preserve normal sanitized folder names, add focused legacy-row tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 102: v3.64 MOD Info.json Size Limit
Status: complete

Limit uploaded MOD `Info.json` metadata reads before JSON parsing, pretty-printing, SQLite persistence, or API exposure. Reject oversized metadata files with a focused error, preserve normal MOD metadata, add parser/upload tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 103: v3.65 Palworld REST Response Size Enforcement
Status: complete

Detect Palworld REST upstream response bodies that exceed the backend client limit instead of silently truncating them before JSON parsing or error serialization. Preserve normal dashboard/player/action proxy behavior, add focused fake-upstream tests for oversized success/error bodies and normal responses, update docs/progress, and verify normal backend/frontend checks.

### Phase 104: v3.66 Backup Read Path Filtering
Status: complete

Prevent unsafe persisted backup paths from being serialized through backup list and dashboard recent-backup read APIs. Preserve valid backup paths and existing restore/delete fail-closed path checks, omit or clearly mark unsafe read-only paths from dirty SQLite rows, add focused route/dashboard tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 105: v3.67 Backup Restore ZIP Resource Limits
Status: complete

Limit backup ZIP restore preflight and extraction by file count, per-entry uncompressed size, and total uncompressed size so malformed or hostile archives cannot consume unbounded CPU/disk during validation or restore. Preserve existing path, symlink, corrupt-archive, and atomic restore behavior; add focused resource-limit tests; update docs/progress; and verify normal backend/frontend checks.

### Phase 106: v3.68 Palworld REST Write Payload Validation
Status: complete

Validate backend-proxied Palworld REST write payloads before forwarding them upstream: bound operator messages, player user IDs, and shutdown wait seconds so script/API clients cannot bypass frontend limits or send oversized control payloads. Preserve optional empty-body defaults, add focused route tests, align frontend input limits, update docs/progress, and verify normal backend/frontend checks.

### Phase 107: v3.69 Task Log Size Limit
Status: complete

Cap persisted task logs so long-running SteamCMD, backup, restore, or MOD operations cannot grow SQLite rows and realtime/API payloads without bound. Preserve latest log output, add a clear truncation marker when older content is dropped, keep redaction and normalization behavior, add focused task-log tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 108: v3.70 Server Log Entry Size Limit
Status: complete

Cap individual in-memory server log entries before they are stored or broadcast so a single oversized PalServer stdout/stderr line cannot create oversized `/api/server/logs`, dashboard, or realtime WebSocket payloads. Preserve the existing 400-entry rolling window, redaction, and multiline splitting behavior; add focused server-log tests; update docs/progress; and verify normal backend/frontend checks.

### Phase 109: v3.71 Finished Task Retention
Status: complete

Cap retained finished task records so bounded per-task logs cannot still accumulate as unbounded SQLite rows over long-running panel use. Preserve running tasks, keep the newest finished task history for API/WebSocket snapshots, prune old finished rows on startup and after task completion, add focused retention tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 110: v3.72 Backup Creation Resource Limits
Status: complete

Cap backup archive creation so walking and compressing the configured server directory cannot consume unbounded CPU, disk, or API/task time from unexpectedly huge source trees. Reuse the restore-side limits for source file count, per-file size, and total source bytes; count actual copied bytes as well as file metadata; reject before metadata commit/final archive rename leaves a finished backup; add focused tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 111: v3.73 MOD Extraction Resource Limits
Status: complete

Cap MOD archive extraction so a compressed upload cannot expand into unbounded files or bytes before metadata discovery and installation. Add native ZIP file-count/per-entry/total-byte checks with actual copied-byte counting, make extracted-tree validation enforce the same limits for external 7z/RAR extractors, bound external extractor runtime/output, add focused tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 112: v3.74 Config File Resource Limits
Status: complete

Cap `PalWorldSettings.ini` and `DefaultPalWorldSettings.ini` reads/copies so config GET/init/save/manual `.bak` flows cannot load or copy unbounded files into memory or API responses. Preserve normal config parsing, initialization, atomic writes, and mutation admission; add focused oversized-config tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 113: v3.75 Streaming File Copy
Status: complete

Make the shared file-copy helper stream from source to the atomic writer instead of reading entire files into memory, so MOD install/update directory copies can handle resource-limited but large files without large heap spikes. Preserve atomic replacement behavior and permissions, keep config-specific size caps separate, add focused copy/MOD route coverage, update docs/progress, and verify normal backend/frontend checks.

### Phase 114: Linux-Only Docker Deployment Direction Correction
Status: complete

Correct the product direction away from Windows support: document Linux as the only supported runtime target, keep native Linux SteamCMD as the development/early-operation path, promote Docker/Compose to the intended final deployment model, and mark Windows runtime/service support as out of scope. Preserve official research notes as external facts while making the product plan and README unambiguous.

### Phase 115: v3.76 MOD Settings File Size Limit
Status: complete

Cap `Mods/PalModSettings.ini` reads before active-mod derivation or enable/disable/delete/install-update settings rewrites, and preflight the file before backup/database side effects when a MOD operation will update it. Preserve missing-file behavior, atomic settings writes, compensation paths, and normal MOD routes; add focused oversized-settings tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 116: v3.77 MOD Settings Read Error Propagation
Status: complete

Propagate bounded `Mods/PalModSettings.ini` read errors from MOD list/detail APIs instead of silently treating oversized or unreadable settings files as an empty active-mod set. Preserve missing-file behavior and normal MOD list/detail responses; add focused route tests, update docs/progress, and verify normal backend/frontend checks.

### Phase 117: v3.78 Linux Docker Compose Deployment Scaffolding
Status: complete

Add the first Linux-only Docker deployment path for the panel: a multi-stage Dockerfile that builds the Vue frontend and embedded Go binary, a Compose file with persistent data and PalServer mounts, and a Docker build context ignore file. Remove the obsolete Windows NSSM service helper, keep native Linux systemd as a secondary path, update README/product-plan/progress, and verify normal backend/frontend checks.

### Phase 118: v3.79 Compose Environment Example
Status: complete

Add a checked-in `.env.example` for the Compose deployment variables, ignore local `.env` overrides, document the copy/edit workflow in the README, and keep the sample aligned with `deploy/compose.yaml` so users can customize ports, bind mounts, UID/GID, image version, and timezone without editing the Compose file. Preserve the Linux-only deployment direction and verify the sample by static variable coverage plus normal backend/frontend checks.

### Phase 119: v3.80 Docker SteamCMD Runtime Tooling
Status: complete

Make the Linux Docker runtime image include SteamCMD on `PATH` so the existing backend install/update flow can work in the Compose deployment without requiring users to inject a separate SteamCMD binary. Install only the runtime dependencies needed for SteamCMD, keep `/data` and `/palserver` owned by the non-root panel user, document the container SteamCMD path and Linux amd64 assumption, and verify with static Dockerfile checks plus normal backend/frontend checks because Docker CLI remains unavailable in this environment.

### Phase 120: v3.81 Compose Palworld Game Port Exposure
Status: complete

Expose the default Palworld game UDP port in the Linux Compose deployment so a PalServer process started by the panel inside the container can accept player traffic. Add environment variables for host/container game port mapping, document that REST/RCON remain unexposed by default and should be accessed through the panel, keep `.env.example` aligned with `deploy/compose.yaml`, and verify with static Compose variable coverage plus normal backend/frontend checks.

### Phase 121: v3.82 Docker SteamCMD Writable State
Status: complete

Move SteamCMD's mutable runtime state out of the immutable image layer and into the writable panel data volume so the Compose deployment still works when `PALPANEL_UID`/`PALPANEL_GID` are customized. Keep a read-only SteamCMD installer archive in the image, make `/usr/local/bin/steamcmd` extract/use `${PALPANEL_STEAMCMD_DIR:-/data/steamcmd}`, document the setting, keep `.env.example` and Compose aligned, and verify with static Dockerfile/Compose checks plus normal backend/frontend checks.

### Phase 122: v3.83 Docker Default PalServer Path
Status: complete

Improve first-run Docker usability by seeding the initial persisted `pal_server_path` from an explicit `PALPANEL_DEFAULT_PAL_SERVER_PATH` environment variable, with Compose and the image defaulting it to `/palserver`. Preserve the existing detected-but-not-configured status semantics for `PALPANEL_PAL_SERVER_PATH`, do not overwrite already persisted settings on later restarts, update docs/env samples/plans, add focused backend tests, and verify normal backend/frontend checks.

### Phase 123: v3.84 Docker Writable Home For SteamCMD
Status: complete

Make the Docker runtime's passwd home and `HOME` environment point at the writable `/data` volume so SteamCMD and Steam runtime user files such as `~/.steam` do not target an unwritable image path. Keep SteamCMD's explicit mutable directory at `/data/steamcmd`, keep custom UID/GID Compose deployments supported, update docs/plans, and verify with static Docker/Compose checks plus normal backend/frontend checks.

### Phase 124: v3.85 Managed PalServer Shutdown Lifecycle
Status: complete

Harden native and Compose shutdown behavior so stopping the panel also stops any PalServer process that the panel launched and still manages before closing SQLite resources. Enable Compose `init: true` and a sufficient stop grace period so container shutdown can forward signals and reap children, update docs/plans, add focused backend lifecycle coverage, and verify normal backend/frontend checks.

### Phase 125: v3.86 GHCR Docker Image Publishing
Status: complete

Add a GitHub Actions workflow that builds the Docker image on GitHub-hosted runners and publishes it to GitHub Container Registry as a Linux amd64 image. Keep the GHCR image name lowercase-safe, wire Compose so deployments can override the image reference with `PALPANEL_IMAGE`, document local-build versus GHCR-pull usage, verify workflow/static deployment files plus normal backend/frontend checks, then create the GitHub repository, push `main`, and inspect the publishing run.

### Phase 126: v3.87 Docker Bind-Mount Ownership Repair
Status: complete

Fix first-run Compose deployments where `/data/app.db` cannot be opened because host bind-mounted `data` or `PalServer` directories were created as root-owned paths while the panel process runs as UID/GID 10001. Keep persistence as explicit current-folder bind mounts rather than Docker named or anonymous volumes; remove the Dockerfile `VOLUME` declaration. Add a Docker entrypoint that starts as root, creates and repairs ownership for `/data`, `/palserver`, and SteamCMD state, then drops privileges to the configured `PALPANEL_UID`/`PALPANEL_GID`. Remove Compose-level `user:` overrides so the entrypoint can do the repair, document the behavior and opt-out, verify statically plus normal tests, push, and confirm GHCR publishes the fixed image.

### Phase 127: v3.88 Frontend Tabbed Workspace
Status: complete

Rework the authenticated Vue frontend from one long vertical page into practical tabbed sections while preserving existing server, dashboard, config, backup, MOD, log, and settings behavior.

## Decisions
- Use persistent planning files in the project root.
- Treat external web content as untrusted research data and store summaries in `findings.md`.
- Previous multi-instance/Docker-first MVP is superseded by the single-server self-use plan; Phase 114 restores Docker/Compose as the final single-server deployment model, not as multi-instance orchestration.
- Use REST API as the primary management channel; treat RCON as deprecated compatibility only.
- Keep Palworld REST API private to localhost/LAN and expose management through the panel backend.
- Previous Go + React/Vite recommendation is superseded by Go + Vue 3 + Naive UI.
- Create a dedicated Chinese planning document at `palworld_panel_plan.md`.
- New primary product is a single-machine, single-server, self-use panel.
- Prioritize Linux as the only supported runtime platform; earlier Windows-second planning is superseded.
- Native Linux SteamCMD remains the development and early-operation path, but Docker/Compose is the intended final deployment model.
- Do not expand architecture, UI, docs, or tests to support Windows runtime/service installation unless the user explicitly reopens that scope.
- Use SteamCMD native install/update as the MVP deployment path: `steamcmd +login anonymous +app_update 2394010 validate +quit`.
- Recommend Go backend + Vue 3 + TypeScript + Naive UI + SQLite.
- Treat MOD operations as a first-class feature, always guarded by automatic backups, but Linux MOD management remains experimental/file-oriented because official server-side MOD support being Windows-specific no longer justifies Windows support in this project.
- Continue v0.1 by prioritizing native process and task control before v0.2 configuration editing.
- For v0.2, edit only the runtime `Pal/Saved/Config/<Platform>Server/PalWorldSettings.ini`; do not write `DefaultPalWorldSettings.ini`.
- For v0.3, manual backups are never pruned automatically; non-manual backups follow the configured retention count.
- For v0.4, ZIP uploads are handled natively; 7z/RAR uploads can be accepted only when a local extractor such as `7z`/`7zz`/`7za`/`unar`/`bsdtar` is available.
- For v0.5, the browser talks only to the panel backend; Palworld REST API Basic Auth is applied by the backend client.
- For v0.6, production builds embed frontend assets in the Go binary; local Vite development remains available through the existing dev server proxy.
- For v0.7, dashboard resource metrics come from local OS APIs and Palworld shutdown/stop actions remain backend-proxied REST calls.
- For v0.8, automatic backup cadence is configurable in hours with a 24-hour default; backup creation tries Palworld REST `/save` first when credentials are configured, but does not fail the backup if the REST API is unavailable.
- For v0.9, all API error responses and persisted panel/server/task logs pass through centralized sensitive-data redaction before storage or response serialization.
- For v1.0 realtime operations, the browser uses an authenticated same-origin WebSocket for server-log and task updates while preserving REST polling as a fallback when the socket is disconnected.
- For v1.1 MOD updates, uploading a replacement archive for an existing MOD must verify `Info.json` `PackageName` matches the selected MOD before replacing files.
- For v1.2 dangerous operations, backend routes enforce an explicit `X-Palpanel-Confirm: true` header so UI confirmation is backed by an API-level guard.
- For v1.3 operation observability, synchronous backup and MOD mutation APIs still return their existing payloads, but they also create task records with final status and logs for dashboard/realtime visibility.
- For v1.4 MOD directory opening, the browser asks the backend to open the directory on the panel/server machine; the backend must constrain the resolved path under the configured PalServer root and invoke only a platform-specific opener without shell interpolation.
- For v1.5 server updates, a panel-managed running PalServer process is stopped inside the update task before SteamCMD mutates files and restarted only if SteamCMD succeeds.
- For v1.6 Palworld REST settings, the browser still talks only to the panel backend; runtime `/settings` data is read by the backend with Basic Auth and surfaced as a read-only dashboard/settings snapshot.
- For v1.7 MOD delete cleanup, removing a MOD also removes its server-generated `Mods/ManagedMods/<PackageName>` directory only after constraining the resolved path under `Mods/ManagedMods`.
- For v1.8 runtime state, `running` means either a panel-managed process or a detected external PalServer process, while server start/update/restore must refuse to proceed if an external process is detected because the panel cannot stop and restart it safely.
- For v1.9 launch arguments, the raw `server_launch_args` string remains the stored source of truth; structured controls update that string for common official flags without discarding unknown custom arguments.
- For v2.0 advanced config, PvP is represented by the official three-key set (`bIsPvP`, `bEnablePlayerToPlayerDamage`, `bEnableDefenseOtherGuildPlayer`) rather than a single hidden aggregate switch.
- For v2.1 product-plan sync, `palworld_panel_plan.md`'s API draft should track the implemented `internal/app/app.go` route surface, and structured launch controls remain API/UI fields derived from the stored `server_launch_args` value rather than independent SQLite settings.
- For v2.2 performance config controls, official performance-sensitive config keys are modeled as typed fields with conservative limits: `BaseCampMaxNumInGuild` caps at 10, `MaxBuildingLimitNum` accepts 0 as unlimited, and `ServerReplicatePawnCullDistance` is constrained to 5000-15000 cm.
- For v2.3 restore lifecycle safety, restore may stop and restart only panel-managed PalServer processes; it must re-check for externally running PalServer after any managed stop and before extracting backup files.
- For v2.4 MOD runtime safety, MOD mutations must refuse to proceed while PalServer is running because they change `Mods/Workshop`, `Mods/ManagedMods`, or `PalModSettings.ini`; successful MOD changes still require starting/restarting PalServer before they take effect.
- For v2.5 port diagnostics, port checks are best-effort local bind probes intended for pre-start conflict detection; a port reported as in use while PalServer is running may be expected.
- For v2.6 first-run setup, the guide is a derived UI checklist over existing backend state rather than a separate persisted wizard state, so it cannot drift from actual server readiness.
- For v2.7 config runtime safety, `GET /api/config` may read existing config and `PUT /api/config` may save while PalServer is panel-managed and report `needs_restart`, but initialization and saves must reject externally running PalServer before backups or file writes because the panel cannot safely coordinate that process.
- For v2.8 SteamCMD task admission, the single-flight task guard must run before any pre-operation backup side effects, while accepted install/update tasks should log their own `pre_update` backup result before running SteamCMD.
- For v2.9 structured launch settings, the backend API must not rely on the Vue client mutating `server_launch_args`; `PUT /api/settings` owns the same merge semantics so script/API clients get the documented behavior.
- For v3.0 server start admission, startup backup creation must happen only after all cheap rejection checks pass, including malformed launch arguments and panel-managed duplicate starts; an accepted start attempt may still create the protective `startup` backup before process spawn.
- For v3.1 restart lifecycle safety, restart must preflight the next start before stopping a panel-managed server so invalid settings or external-runtime conflicts do not turn a rejected restart into an unnecessary stop.
- For v3.2 operation task admission, the panel's "one running task" model applies to SteamCMD, backup, MOD, and scheduled auto-backup tasks; backend admission must match the UI active-task lock rather than relying on frontend disabling alone.
- For v3.3 player moderation safety, kick/ban/unban are operator-affecting Palworld REST actions and must use the same backend confirmation-header pattern as other dangerous operations.
- For v3.4 config mutation safety, config initialization/save/manual `.bak` backup are file mutations and must not run concurrently with the backend running-operation slot even though they do not create long-running task records.
- For v3.8 settings safety, `PUT /api/settings` is part of operation admission because it can retarget server paths used by later file mutations; it must not run during another backend task or move `pal_server_path` away from a running configured PalServer.
- For v3.9 runtime action UI admission, dashboard REST write actions and panel-managed stop controls should not be clickable from the UI while the runtime/API state makes them invalid or another panel operation is active; the backend proxy routes remain available for explicit API callers and confirmation-protected emergency/operator workflows.
- For v3.11 operation-slot visibility, UI locks must consider the backend shared running-operation flag in addition to persisted `tasks.status=running`, because some guarded synchronous operations reserve the slot without creating task records.
- For v3.12 realtime security coverage, `/api/events` relies on the WebSocket library's default origin verification, where the request host is authorized and cross-origin attempts are rejected; tests should pin both accepted same-origin and rejected cross-origin behavior.
- For v3.13 settings atomicity, multi-key app settings saves must use one SQLite transaction so partial persistence cannot retarget paths or credentials after a mid-save database error.
- For v3.14 realtime operation visibility, the shared backend running-operation slot must broadcast true/false WebSocket events whenever it is reserved or released, including short synchronous operations that do not create task records.
- For v3.15 dashboard REST aggregation, independent Palworld REST reads should fan out concurrently and preserve the existing partial-data/partial-error response contract.
- For v3.16 process lifecycle, production startup should use an explicit `http.Server` and graceful signal shutdown so app cleanup runs under systemd/service stops.
- For v3.17 app lifecycle, closing the app should signal background goroutines, wait for them to exit, and only then close SQLite to avoid shutdown-time races with scheduler work.
- For v3.18 browser safety, unsafe REST methods should reject cross-origin `Origin` headers server-side; absent `Origin` remains allowed for scripts and local API clients.
- For v3.19 session transport safety, session cookies remain `HttpOnly` and `SameSite=Lax`, and should add `Secure` when the panel request scheme is HTTPS or a trusted reverse proxy reports HTTPS through `X-Forwarded-Proto`.
- For v3.21 auth setup safety, the "single admin" rule must be enforced inside the backend critical section/insert path, not only by a pre-insert `hasUsers` check.
- For v3.22 restore safety, restore preflight must run external-runtime checks before creating the configured PalServer directory, protective backups, or restored files.
- For v3.23 upload safety, MOD archive upload/update requests must be wrapped with `http.MaxBytesReader`; multipart memory thresholds are not a substitute for request-size limits.
- For v3.24 upload admission, MOD upload/update must reserve the shared operation slot before reading multipart request bodies so active-operation rejection has no upload parsing or temp-file side effects.
- For v3.25 API resource safety, all JSON request bodies decoded through the shared backend helper must be wrapped with `http.MaxBytesReader`; small operator payloads do not need unbounded body reads.
- For v3.26 API parsing correctness, JSON request bodies must contain exactly one JSON value; trailing whitespace is allowed, but extra JSON values are rejected.
- For v3.27 API parsing correctness, JSON object member names must be unique within each object decoded by the shared backend helper; exact duplicate names are rejected before the payload is applied.
- For v3.28 API contract correctness, structured JSON API request bodies should reject unknown fields rather than silently ignoring typoed or unsupported operator input.
- For v3.29 backup consistency, manual delete and retention pruning should delete the SQLite row inside a transaction before removing the archive file, rolling the row delete back if archive removal fails.
- For v3.30 MOD delete consistency, the mod row deletion should be accepted inside a transaction before disabling the mod or removing installed files, rolling back the row delete if filesystem mutation fails.
- For v3.31 MOD install/update consistency, archive extraction may use temporary staging, but final `Mods/Workshop/<folder>` replacement must happen only after the corresponding SQLite insert/update has been accepted.
- For v3.32 restore safety, backup ZIP files should be path-checked and fully readable before restore creates the server directory, creates a protective backup, or extracts any file.
- For v3.33 backup boundary safety, filesystem symlinks under the server root are not backup content; backup creation skips them, and restore rejects symlink entries in ZIP archives.
- For v3.34 MOD enable/disable consistency, the `mods.enabled` update should be accepted inside a SQLite transaction before `PalModSettings.ini` changes, rolling the row update back if the settings write fails.
- For v3.35 config write safety, `PalWorldSettings.ini` writes and config `.bak` copies should use same-directory temporary files plus platform-safe replacement instead of direct truncating writes.
- For v3.36 MOD settings write safety, `Mods/PalModSettings.ini` updates should use the same atomic replacement helper as config writes.
- For v3.37 restore write safety, backup restore should stream each restored file through a same-directory temporary file and platform-safe replacement rather than truncating existing target files directly.
- For v3.38 MOD archive safety, uploaded native ZIP archives should reject symlink entries and extracted metadata scanning should not trust symlinked `Info.json` files.
- For v3.39 MOD extraction safety, the full extracted MOD tree should be rejected if it contains any symlink before metadata discovery or staging.
- For v3.40 MOD extraction containment, uploaded archives and extracted content should live in one operation-scoped workspace, and extractor output outside the intended content directory should be rejected before metadata is trusted.
- For v3.41 MOD install/update consistency, the SQLite insert/update transaction must commit before final `Mods/Workshop/<folder>` replacement; if final replacement fails after commit, the backend should compensate by restoring the previous database row state.
- For v3.42 MOD delete consistency, the SQLite row deletion should commit before settings and directory mutation; if later settings or directory removal fails, the backend should compensate by restoring the previous MOD row and active settings where possible.
- For v3.43 backup creation consistency, backup ZIP bytes should remain in a same-directory temporary archive until the SQLite metadata row is accepted in a transaction; final archive rename failure should roll the row back, and metadata insertion failure should not leave a final `.zip` archive.
- For v3.44 backup delete consistency, row deletion commits before archive removal; archive removal failure restores the backup metadata row.
- For v3.45 MOD enable/disable consistency, `mods.enabled` commits before `PalModSettings.ini` changes; settings write failure restores the previous row state.
- For v3.46 MOD delete consistency, installed MOD directories move into a same-`Mods` deletion staging area before cleanup; cleanup failure restores staged directories before row/settings compensation.
- For v3.47 MOD install/update consistency, replacement failures must report any failure to restore the previous installed directory rather than silently returning only the initial replacement error.
- For v3.48 SQLite runtime stability, the panel uses one SQLite connection plus a busy timeout so concurrent panel goroutines wait instead of racing multiple writer connections into `SQLITE_BUSY`.
- For v3.49 frontend admission, server operation buttons, setup-guide actions, and `runServerAction` must share the same local block-reason logic so disabled UI state and actual click behavior cannot diverge.
- For v3.50 MOD upload resource safety, request-size limits and multipart in-memory caching are separate controls: MOD upload/update keeps the 512 MiB hard body cap, but multipart parsing should use a much smaller memory threshold so large archives spill to temporary files.
- For v3.51 config mutation safety, manual config `.bak` backup is a config-directory write and must share the same external-runtime and active-operation admission model as config initialization and config save.
- For v3.52 frontend restore admission, backup restore is a server-directory mutation and the UI should locally block it when an external PalServer is detected, but backup creation and backup deletion are not server-directory mutations and should keep their existing active-operation guard.
- For v3.53 frontend config initialization admission (superseded by v3.57), the UI locally blocked config refresh under external runtime because `GET /api/config` could initialize files at that time.
- For v3.54 frontend in-flight admission, backup and MOD buttons plus direct handlers should share local busy-state checks so duplicate or overlapping UI actions are rejected before they reach the backend operation guard.
- For v3.55 request-shape enforcement, authenticated actions with no request-body contract should reject unexpected non-empty bodies, while JSON and multipart routes keep their explicit parsers and limits.
- For v3.56 frontend request-shape hygiene, no-body POST calls from the Vue API helper should not send a JSON `Content-Type`; confirmation-only requests should carry only the confirmation header.
- For v3.57 config initialization semantics, `GET /api/config` is strictly read-only and missing config returns a not-initialized error; creating `PalWorldSettings.ini` is an explicit no-body `POST /api/config/init` action protected by normal unsafe-method origin checks and operation/runtime admission.
- For v3.58 frontend first-run config UX, the expected `GET /api/config` 404 should be represented as a typed "not initialized" UI state with localized guidance and the explicit initialization action, while other config load failures remain warnings.
- For v3.59 player moderation request shape, kick/ban keep their optional JSON message payloads, but unban has no request-body contract because the route path already supplies `userid` and the Palworld REST proxy should send only that field.
- For v3.60 optional JSON request shape, routes with optional JSON payloads should accept an actually empty body as absent options while preserving the same size, single-value, duplicate-key, and unknown-field rejection rules for non-empty JSON bodies.
- For v3.61 backup API normalization, manual backup route input should be sanitized/defaulted before task creation so task type, task log, backup filename, and backup metadata agree on the effective backup type.
- For v3.62 MOD PackageName safety, uploaded `Info.json` PackageName values and legacy MOD rows must be valid single settings/path tokens before persistence, `PalModSettings.ini` writes, enable-state changes, or `ManagedMods` path derivation.
- For v3.63 MOD folder-name safety, persisted `mods.folder_name` values must be valid single Workshop directory names before they are reused for upload/update installs, enable checks, delete/open-dir targets, or `install_path` serialization; unsafe legacy rows are rejected before mutation and omitted from derived read-only paths.
- For v3.64 MOD metadata resource safety, extracted `Info.json` is capped at 1 MiB before JSON parsing, pretty-printing, SQLite persistence, or API exposure; the archive upload cap is not a substitute for a metadata-specific limit.
- For v3.65 Palworld REST response safety, upstream REST response bodies are capped at 2 MiB and over-limit bodies must produce an explicit upstream response-size error instead of passing truncated bytes into JSON parsing or API error serialization.
- For v3.66 backup read safety, backup list and dashboard read APIs must not serialize persisted backup paths that fail the backup-directory containment check; restore/delete/prune keep using fail-closed mutation-time path validation.
- For v3.67 backup restore resource safety, restore ZIP preflight and extraction both cap archive expansion at 200000 files, 8 GiB per entry, and 64 GiB total uncompressed bytes before stopping PalServer, creating protective backups, or writing restored files.
- For v3.68 Palworld REST write payload safety, backend-proxied broadcast/kick/ban/shutdown/unban inputs cap messages at 500 characters, route user IDs at 128 characters, and shutdown wait time at 3600 seconds before any upstream REST call.
- For v3.69 task log resource safety, each persisted task log keeps at most the latest 1 MiB plus a truncation marker so `/api/tasks` and realtime task snapshots cannot grow without bound from long-running command output.
- For v3.70 server log resource safety, each in-memory server log message is capped at 8 KiB with a truncation suffix before storage, dashboard/API serialization, or realtime WebSocket broadcast.
- For v3.71 task retention safety, finished task rows are pruned to the newest 200 records on startup and after task completion, while running tasks are preserved so active operations remain visible.
- For v3.72 backup creation resource safety, source archive creation uses the same 200000-file, 8 GiB per-file, and 64 GiB total-byte limits as restore, with both metadata checks and actual copied-byte counting before backup metadata/final archives are committed.
- For v3.73 MOD extraction resource safety, native ZIP and external extractor outputs are capped at 100000 files, 2 GiB per file, and 8 GiB total extracted content; external extractors also have a 5-minute timeout and 64 KiB captured-output limit.
- For v3.74 config file resource safety, `PalWorldSettings.ini` and `DefaultPalWorldSettings.ini` reads/copies plus rendered save output are capped at 4 MiB, and oversized GET/init/save/manual `.bak` flows reject before backup or write side effects.
- For v3.75 streaming file-copy safety, the shared helper used by MOD staging copies streams source bytes through the atomic writer instead of loading whole files into memory, while config-specific copies keep their separate 4 MiB cap.
- For v3.76 MOD settings resource safety, `Mods/PalModSettings.ini` reads are capped at 1 MiB before active-list derivation or settings rewrites, and MOD operations that will rewrite it preflight the file before backup/database side effects.
- For v3.77 MOD settings read signaling, MOD list/detail APIs propagate bounded settings-read errors such as oversized `PalModSettings.ini` instead of silently treating the active-mod set as empty.
- For v3.78 deployment direction, Docker/Compose is the primary Linux deployment path: the image builds embedded frontend assets and the Go binary, Compose mounts `/data` and `/palserver`, and obsolete Windows NSSM deployment helpers are removed.
- For v3.79 Compose deployment configuration, `deploy/compose.yaml` stays as the stable Linux deployment template while local port, bind mount, UID/GID, version, and timezone overrides live in an ignored `.env` copied from `.env.example`.
- For v3.80 Docker runtime tooling, the Linux amd64 panel image includes SteamCMD at `/usr/local/bin/steamcmd` and keeps it writable by the non-root `palpanel` user so install/update tasks can use the existing PATH-based resolver inside Compose deployments.
- For v3.81 Compose networking, the default deployment publishes the Palworld game UDP port with env-configurable host/container values while keeping Palworld REST/RCON ports unpublished by default because those operations should go through the panel backend.
- For v3.82 Docker SteamCMD state, the image stores only the SteamCMD installer archive read-only and the `/usr/local/bin/steamcmd` wrapper extracts/runs SteamCMD from `${PALPANEL_STEAMCMD_DIR:-/data/steamcmd}` so self-update/cache writes happen in the writable data volume even when Compose uses a custom runtime UID/GID.
- For v3.83 Docker first-run defaults, `PALPANEL_DEFAULT_PAL_SERVER_PATH` seeds `pal_server_path` only when the SQLite setting is first inserted; existing databases keep their saved value, while `PALPANEL_PAL_SERVER_PATH` remains a detected-but-not-configured path hint for already installed PalServer binaries.
- For v3.84 Docker runtime state, the container passwd home and runtime `HOME` are `/data` so SteamCMD/Steam user-state paths such as `~/.steam` land on the writable data volume instead of an unwritable image home.
- For v3.85 panel shutdown, `App.Close()` stops only panel-managed PalServer child processes before closing background workers and SQLite; Compose uses `init: true` plus a 60-second stop grace period so container shutdown has time to signal, wait, and reap child processes.
- For v3.86 GHCR publishing, GitHub Actions owns remote Docker builds and publishes `ghcr.io/<lowercase-owner>/palpanel-lite` for `main`, `v*` tags, and manual dispatch; Compose keeps source-build support but can pull any full image reference through `PALPANEL_IMAGE`.
- For v3.87 Docker ownership repair, persistence stays as explicit bind mounts such as `./data:/data` and `./PalServer:/palserver`, not Docker named/anonymous volumes; the image entrypoint runs briefly as root only to normalize mounted directory ownership, then executes the panel as the configured non-root `palpanel` UID/GID; Compose must not set `user:` because that would block the repair step.
- For v3.88 frontend workspace layout, the authenticated Vue UI should use Naive UI tabs to separate server/settings, dashboard, config, backups, MODs, and logs while keeping existing API behavior and local operation guards unchanged.

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| Official Palworld guide direct fetch returned limited/invalid content | Fetch `https://tech.palworldgame.com/` with PowerShell | Use Jina Reader for readable Markdown |
| Valve SteamCMD wiki returned anti-bot challenge | Fetch `https://developer.valvesoftware.com/wiki/SteamCMD` | Use other reliable SteamCMD references or known command syntax and cross-check with project docs |
| Steam app list endpoint returned 404 | Fetch `https://api.steampowered.com/ISteamApps/GetAppList/v2/` | Use SteamCMD/app IDs from official Palworld docs and mature Docker project references |
| Official Docker compose root path returned 404 | Fetch `.../main/compose.yaml` | Located sample at `compose/compose.yaml` via GitHub contents API |
| GitHub latest release API returned 404 for official Docker repo | Fetch latest release endpoint | Use image tags/README guidance instead; do not assume GitHub releases exist |
| GitHub Packages versions API returned 404 unauthenticated/path | Fetch package versions endpoint | Defer automatic tag discovery; require user-selected/known image tag in MVP |
| `git diff --stat` failed because directory is not a Git repository | Final change check | Use file listing and direct status checks instead |
| TypeScript build failed with TS6310 because referenced `tsconfig.node.json` disabled emit | `npm run build` after adding `noEmit` to node tsconfig | Changed build script to `vue-tsc --noEmit -p tsconfig.json && vite build` and removed the main tsconfig project reference |
| PowerShell homepage smoke check failed because `$home` conflicts with read-only `$HOME` | Invoke health and homepage check with `$home` variable | Re-ran the homepage check using `$page` |
| Go embed rejected frontend placeholder directory because it contained only `.keep` | `go test ./...` after adding embedded frontend package | Added `internal/frontend/dist/placeholder.txt` because Go embed ignores dotfiles |
| Dashboard recent operation fields encoded as null when empty | `go test ./...` after adding Phase 15 dashboard extras | Normalized recent logs, tasks, backups, and players to empty arrays in dashboard payload |
| Backup scheduler tests failed to compile because `strings` import was missing | `go test ./...` after adding Phase 16 tests | Added the missing `strings` import to `internal/app/backup_test.go` |
| PowerShell `rg` with `internal/app/*_test.go` path failed with invalid path syntax | Test helper search during Phase 17 code inspection | Use `rg ... internal/app -g '*_test.go'` or direct file reads instead of shell glob path syntax |
| PowerShell `Select-Object -Index 680..920` failed because the range was parsed as a string | Phase 18 code inspection | Use `Select-Object -Skip <n> -First <n>` or parenthesized ranges for future snippets |
| `go doc` rejected multiple symbols in one invocation | Phase 18 WebSocket library API lookup | Query the package or a single type/symbol at a time, e.g. `go doc github.com/coder/websocket.Conn` |
| `rg` regex failed due malformed quote escaping around `type=\"file\"` | Phase 19 frontend control search | Use simpler fixed terms or separate searches for quoted template attributes |
| PowerShell parser rejected a complex `rg` pattern with nested quotes/backslashes | Phase 20 final line-number check | Use simple fixed-term `rg -F` searches or split patterns into separate commands |
| Repeated PowerShell `rg internal/app/*_test.go` glob-path failure | Phase 20 final test coverage check | Use `rg ... internal/app -g '*_test.go'`; avoid shell-style glob paths in PowerShell |
| `go test ./...` failed vet with non-constant format string in `logTaskf` | Phase 21 first verification | Changed dynamic task messages to `logTaskf(taskID, "%s", message)` |
| `go test ./...` failed with transient `database is locked (SQLITE_BUSY)` in async task polling tests | Phase 23 first verification | Changed the test wait helper to retry temporary `getTask` errors until its deadline |
| PowerShell `rg internal/app/*.go` glob-path search failed with invalid path syntax | Phase 26 runtime-state audit | Use `rg ... internal/app -g '*.go'` or `rg --files internal/app` instead of shell-style glob paths |
| `go test ./internal/app` left an async task in `running` after logging completion, likely from transient SQLite busy during `finishTask` | Phase 27 first backend verification | Added a short retry loop around task final-status updates when SQLite reports temporary busy/locked errors |
| Malformed Jina Reader URL nested `r.jina.ai` hosts and returned 451 circular-host error | Phase 28 official config lookup | Re-ran fetches with normal `https://r.jina.ai/http://tech.palworldgame.com/...` URLs |
| `go test ./...` failed because a server-update failure task reached final status without the expected SteamCMD failure log line, consistent with transient SQLite busy during task log append | Phase 32 first full backend verification | Added the same short retry loop to task log appends that final task status updates already use |
| PowerShell homepage smoke check failed because inline `if` was used where an expression value was expected | Phase 35 smoke checks | Re-ran the homepage check with an explicit `$detail` variable |
| `go test ./internal/app` failed with `database is locked (SQLITE_BUSY)` while a SteamCMD task created its pre-operation backup immediately after task creation | Phase 36 first backend verification | Added the existing short SQLite-busy retry pattern to backup metadata inserts |
| PowerShell parsed an unquoted `go test -run Launch|SettingsRoute` pattern as a pipeline | Phase 37 focused backend verification | Re-ran the test with the regex quoted as `'Launch|SettingsRoute'` |
| Phase 100 patch context missed the local validation snippet shape | Applied a combined patch using stale `if name := ...` context | Re-applied the patch against the actual `name := strings.TrimSpace(...)` code and reran focused MOD tests |
| Phase 102 test patch context missed the local extraction-workspace test name | Applied the oversized `Info.json` tests before a stale `TestValidateExtractionWorkspaceRejectsEscapedOutput` anchor | Re-read the nearby test block and inserted the tests after `TestInspectExtractedModIgnoresSymlinkedInfoJSON` |
| `rg` regex/search quoting failed while auditing startup backup code | Phase 38 code inspection | Switched to simpler fixed-term searches and direct snippets instead of complex escaped patterns |
| `rg` regex parse error from a malformed grouped pattern while auditing dashboard REST calls | Phase 53 audit | Switched to simpler fixed-term searches and direct file reads |
| PowerShell `rg` with `internal/app/server*` glob-path search failed with invalid path syntax | Phase 59 lifecycle audit | Use `rg ... internal/app -g 'server*.go'` or direct file reads instead of shell-style glob paths in PowerShell |
| `go test ./internal/app -run 'Mod|ParseModArchiveUpload'` failed to compile because `handleUploadMod` reused `err` after replacing the parser with a helper | Phase 61 focused verification | Declared the operation-task error locally with `err :=` in `handleUploadMod` |
| Partial Phase 64 edit made `requireConfirmation` return true after writing `412 confirmation required` | Phase 64 resume audit | Restored fail-closed `return false` and ran dangerous-route regression coverage |
| PowerShell process lookup attempted to assign `$pid`, which is a read-only automatic variable | Phase 64 backend restart check | Re-ran the lookup with `$ownerPidValue` and confirmed the port owner was `palpanel.exe` |
| `rg` rejected an over-escaped search pattern as an unrecognized flag | Phase 66 JSON request-shape audit | Re-ran the audit with simpler fixed-string searches |
| Unknown-field decoder test expected raw quotes inside a JSON-encoded error string | Phase 66 focused verification | Matched stable substrings (`unknown field` and the field name) instead of unescaped quotes |
| Phase 68 MOD delete consistency test passed a string to `assertFileEqual`, which expects `[]byte` | Phase 68 focused verification | Converted the string fixture with `[]byte(settingsBefore)` and reran the MOD suite |
| Phase 71 backup symlink test used `io.ReadAll` without importing `io` | Phase 71 focused verification | Added the missing `io` import and reran the focused backup/restore suite |
| Phase 78 focused MOD extraction test failed to compile because `sameFilesystemPath` was redeclared | First Phase 78 focused verification | Removed the duplicate helper from `mods.go` and reused the existing shared helper in `server_runtime.go` |
| PowerShell `rg` with `internal/app/*_test.go` glob-path search failed with invalid path syntax | Phase 79 commit-before-replacement audit | Use `rg ... internal/app -g '*_test.go'` or direct file reads instead of shell-style glob paths in PowerShell |
| Phase 79 commit-failure test left the mod row at version `2.0.0` when using a deferred SQLite foreign-key violation | First Phase 79 focused verification | Replaced the deferred-FK test setup with an injected commit function that rolls back and returns a stable `commit blocked` error |
| `go test ./...` hit transient `database is locked (SQLITE_BUSY)` in `TestSteamCMDTaskCreatesAndLogsPreUpdateBackup` while creating a pre-update backup | Phase 84 first full verification | Re-ran the focused test successfully, then re-ran `go test ./...` successfully |
| PowerShell `rg` with `internal/app/*_test.go` glob-path search failed with invalid path syntax | Phase 86 task-path audit | Use `rg ... internal/app -g '*_test.go'` instead of shell-style glob paths in PowerShell |
| PowerShell `Get-Content web\package.json` failed from inside the `web` working directory | Phase 87 frontend audit | Re-read `package.json` relative to the correct working directory without duplicating the `web` path |
| Repeated PowerShell `rg` with `internal/app/*_test.go` glob-path search failed with invalid path syntax | Phase 105 backup restore resource-limit audit | Switched back to direct file reads and `rg ... internal/app -g '*.go'` style filters; avoid shell glob paths in PowerShell |
| `rg` regex/quoted fixed-string searches failed while locating Phase 106 line references | Phase 106 final line-reference check | Re-ran with simpler fixed terms such as `maxPalRESTMessageRunes`, `v3.68`, and `maxlength` |
| Phase 107 UTF-8 truncation test did not trigger truncation because the fixture was shorter than the temporary limit | First Phase 107 focused verification | Added a longer ASCII prefix before the multibyte suffix and reran the focused task-log tests successfully |
| Phase 108 focused redaction verification used non-existent test names and matched no tests | First Phase 108 focused verification | Read `redact_test.go` for the actual `TestTaskAndServerLogsRedactSensitiveData` name and reran the matching test successfully |
| Phase 109 completion-path retention test retained old finished tasks | First Phase 109 focused verification | The prune call had been inserted in `appendTaskLog` instead of `finishTask`; moved pruning to final-status updates and reran focused tests |
| Phase 115 oversized MOD settings test still created a `pre_mod` backup | First Phase 115 focused verification | Moved `readPalModSettings` preflight before `createBackupIfPossible` in `setModEnabled` and reran focused tests |
| Docker CLI unavailable in current environment | Phase 117 Compose validation, Phase 123 Docker availability check, Phase 124 Compose lifecycle validation, and Phase 125 local Docker availability check | Recorded the environment limitation; validated files by inspection and continued with normal Go/frontend verification; Phase 125 relies on GitHub Actions for the real Docker build/push |
| PowerShell glob path failed again with `internal/app/*_test.go` | Phase 124 lifecycle audit search | Continued with explicit file reads or `rg ... -g '*_test.go'` filters, matching the established PowerShell caveat |
| Global `dist/` ignore hid frontend embed placeholders | Phase 125 pre-commit file audit | Changed the root build-output ignore rule to `/dist/` so `internal/frontend/dist/.keep` and `placeholder.txt` can be tracked while generated assets remain ignored |
| GHCR package API returned 403 without `read:packages` scope | Phase 125 package verification | Used the successful GitHub Actions run and build logs to confirm pushed tags and digest; publishing itself used the workflow `GITHUB_TOKEN` with `packages: write` |
| SQLite reported `unable to open database file: out of memory (14)` after Compose deploy | Phase 126 user deployment report | Treated it as a bind-mount ownership/open failure for `/data/app.db`, changed Compose to explicit current-folder bind mounts without Dockerfile anonymous volumes, and added an entrypoint ownership repair before dropping privileges |
| PowerShell broke a complex `rg` template-boundary pattern | Phase 127 first App.vue boundary search | Re-ran with fixed-string `rg -F -e` patterns and line-number snippets before editing the Vue template |
