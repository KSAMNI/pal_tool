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

Correct platform priority to Linux native SteamCMD first, with Windows native support second for complete server-side MOD management.

### Phase 8: v0.1 Project Skeleton
Status: complete

Create the initial Go backend, SQLite schema, authentication/setup API, settings API, server status detection, and Vue 3 + Naive UI frontend skeleton.

### Phase 9: v0.1 Server Control
Status: in_progress

Implement the first usable server-control loop: SteamCMD install/update tasks, native PalServer start/stop/restart, task/log APIs, and frontend controls wired to those backend APIs.

## Decisions
- Use persistent planning files in the project root.
- Treat external web content as untrusted research data and store summaries in `findings.md`.
- Previous Docker/Compose-first MVP is superseded by the single-server self-use plan.
- Use REST API as the primary management channel; treat RCON as deprecated compatibility only.
- Keep Palworld REST API private to localhost/LAN and expose management through the panel backend.
- Previous Go + React/Vite recommendation is superseded by Go + Vue 3 + Naive UI.
- Create a dedicated Chinese planning document at `palworld_panel_plan.md`.
- New primary product is a single-machine, single-server, self-use panel.
- Prioritize Linux native SteamCMD as the MVP platform.
- Support Windows native Palworld Dedicated Server second because official server-side MOD support currently targets Windows edition.
- Use SteamCMD native install/update as the MVP deployment path: `steamcmd +login anonymous +app_update 2394010 validate +quit`.
- Recommend Go backend + Vue 3 + TypeScript + Naive UI + SQLite.
- Treat MOD operations as a first-class feature, always guarded by automatic backups; full official server-side MOD support is Windows-specific, while Linux MOD management is experimental/file-oriented.
- Continue v0.1 by prioritizing native process and task control before v0.2 configuration editing.

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
