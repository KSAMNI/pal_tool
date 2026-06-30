# PalPanel Lite

Single-machine, single-server Palworld dedicated server management panel.

Current implementation status:

- Go backend scaffold.
- SQLite schema for settings, users, mods, backups, and tasks.
- Single-admin setup/login API.
- Basic app settings API.
- Basic server status detection for PalServer and SteamCMD paths.
- Vue 3 + Naive UI frontend scaffold.

Primary MVP platform: Linux native SteamCMD. Windows native server support remains important for official server-side MOD support.

## Development

```powershell
go run ./cmd/palpanel
```

The backend listens on `127.0.0.1:8080` by default and stores SQLite data in `data/app.db`.

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

Build verification used by this project:

```powershell
go test ./...
cd web
npm run build
```
