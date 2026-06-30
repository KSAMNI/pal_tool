# PalPanel Lite 项目计划

## 项目定位

PalPanel Lite 是一个单机单服自用型幻兽帕鲁专服管理面板。它运行在和 Palworld Dedicated Server 同一台机器上，通过本地进程、文件系统、SteamCMD 和 Palworld REST API 管理一台服务器。

一句话目标：

> 不用记命令、不用手改配置、不怕 MOD 操作前没有备份。

## 明确不做

- 多服务器实例。
- 多节点 Agent。
- 商业化租服。
- 复杂权限系统。
- 插件市场。
- 计费系统。
- 集群调度。
- 一开始做 MOD 自动下载。

## 核心要做

- 一键安装专服。
- 一键更新专服。
- 启动、停止、重启。
- 实时日志。
- 可视化编辑 `PalWorldSettings.ini`。
- 自动备份和一键恢复。
- 在线玩家管理。
- MOD 上传、识别、启用、禁用、删除。

## 平台优先级

1. Linux 原生 SteamCMD 优先。
2. Windows 原生专服第二优先。
3. Docker/Compose 作为后续可选部署方式，不再作为 MVP 主线。

原因：
- 用户目标是自用单服，不需要多实例编排。
- Linux 更适合长期跑专服：systemd、SSH、VPS/NAS/家用小主机部署都更自然，资源占用和自动化也更稳定。
- 官方服务端 MOD 当前只支持 Windows Dedicated Server，因此 Windows 平台提供完整 MOD 支持；Linux 平台先提供实验性文件型 MOD 管理和清晰风险提示。
- 原生进程管理比 Docker 更符合“本机 Web 面板直接管一个服”的定位。

## 推荐技术栈

- 后端：Go。
- 前端：Vue 3 + TypeScript + Naive UI。
- 数据库：SQLite。
- 通信：REST + WebSocket。
- 打包：Go 单个可执行文件内嵌前端静态资源。

Go 后端负责：
- 调用 SteamCMD。
- 启停 `PalServer.exe` 或 `PalServer.sh`。
- 读写配置文件。
- 压缩和恢复备份。
- 解析 MOD `Info.json`。
- 代理调用 Palworld REST API。
- 推送日志和任务进度。

## 目录模型

面板自身：

```text
palpanel-lite/
  palpanel.exe
  data/
    app.db
    backups/
    uploads/
    tasks/
```

Palworld 专服目录由用户配置：

```text
PalServer/
  PalServer.exe
  DefaultPalWorldSettings.ini
  Pal/
    Saved/
      Config/
        WindowsServer/
          PalWorldSettings.ini
      SaveGames/
  Mods/
    PalModSettings.ini
    Workshop/
```

Linux 对应配置路径：

```text
Pal/Saved/Config/LinuxServer/PalWorldSettings.ini
```

实际实现中路径必须按系统分支解析，不能把 Windows 路径硬编码进核心逻辑。

## 功能规划

### v0.1：能安装、能开服、能关服

目标：不用 SSH 或远程桌面手动敲命令。

功能：
- 设置 Palworld 服务器安装目录。
- 设置 SteamCMD 路径。
- 检测 SteamCMD 是否可用。
- 一键安装专服。
- 一键更新专服。
- 启动服务器。
- 停止服务器。
- 重启服务器。
- 查看实时日志。
- 查看服务器运行状态。

SteamCMD 命令：

```text
steamcmd +login anonymous +app_update 2394010 validate +quit
```

页面：
- `/dashboard`
- `/server`
- `/logs`
- `/settings`

### v0.2：配置文件可视化

配置文件：
- 从 `DefaultPalWorldSettings.ini` 复制到系统对应的 `Pal/Saved/Config/.../PalWorldSettings.ini`。
- 后续只修改 `PalWorldSettings.ini`。
- 不修改 `DefaultPalWorldSettings.ini`。

保存逻辑：
1. 读取 INI。
2. 解析 `OptionSettings=(...)`。
3. 映射到 JSON/结构体。
4. 表单修改。
5. 保存前生成 `.bak`。
6. 写回 `PalWorldSettings.ini`。
7. 提示需要重启。

配置分组：
- 基础设置：服务器名称、描述、密码、管理员密码、最大玩家数。
- 游戏倍率：经验、捕获、掉落、采集、孵蛋、白天速度、夜晚速度。
- 世界设置：死亡惩罚、据点数量、公会人数、帕鲁刷新率、建筑退化速度。
- 网络设置：Game Port、REST API Port、RCON Port、Public IP、Public Port。
- 高级设置：REST API、RCON、PvP、客户端 MOD、跨平台连接、日志格式。

### v0.3：备份和恢复

备份优先级高于 MOD 和更新。

备份内容：
- `Pal/Saved/`
- `Pal/Saved/SaveGames/`
- `Pal/Saved/Config/`
- `Mods/`
- `PalWorldSettings.ini`

备份触发：
- 手动备份。
- 每天自动备份。
- 每次启动前自动备份。
- 每次安装、更新、删除、恢复 MOD 前自动备份。
- 每次修改配置前自动备份。
- 每次更新专服前自动备份。

默认保留策略：
- 自动备份最多保留最近 20 个。
- 手动备份不自动删除。

恢复流程：
1. 确认危险操作。
2. 停止服务器。
3. 恢复前再做一次保护性备份。
4. 解压覆盖目标目录。
5. 启动服务器。
6. 检查状态和日志。

### v0.4：MOD 管理

这是项目差异点。第一版 MOD 不做自动从 Nexus/创意工坊下载，只做本地上传和文件管理。

官方约束：
- 服务端 MOD 当前只支持 Windows Dedicated Server。
- 只有为服务端构建的 MOD 才能工作。
- MOD 可能导致存档损坏或崩溃。

平台策略：
- Linux 专服：作为项目优先平台，先提供实验性文件型 MOD 管理、Info.json 识别、备份、启用列表编辑和明确风险提示，不承诺服务端 MOD 官方可用。
- Windows 专服：提供完整 MOD 管理，因为官方服务端 MOD 当前只支持 Windows Dedicated Server。

MOD 安装流程：
1. 上传 zip、7z 或 rar。
2. 解压到临时目录。
3. 寻找直接或间接包含的 `Info.json`。
4. 读取 `PackageName`、`Version`、作者等元数据。
5. 检查 `InstallRule` / `InstallRules` 是否包含 `"IsServer": true`。
6. 安装前自动备份。
7. 复制到 `Mods/Workshop/<folder_name>/`。
8. 写入或更新 `Mods/PalModSettings.ini`。
9. 启用时添加 `ActiveModList=<PackageName>`。
10. 提示重启服务器生效。

`PalModSettings.ini` 示例：

```ini
[PalModSettings]
bGlobalEnableMod=true
ActiveModList=PackageNameA
ActiveModList=PackageNameB
```

启用逻辑：
- 确保 `bGlobalEnableMod=true`。
- 添加 `ActiveModList=<PackageName>`。
- 不重复添加。

禁用逻辑：
- 从 `ActiveModList` 移除对应 `PackageName`。
- 不删除文件。

删除逻辑：
- 先禁用。
- 自动备份。
- 删除 `Mods/Workshop/<folder_name>/`。
- 可选清理 `Mods/ManagedMods/<PackageName>/`。
- 提示重启。

MOD 页面字段：
- MOD 名称。
- PackageName。
- 版本。
- 作者。
- 是否启用。
- 是否支持服务端。
- 安装路径。
- 最后更新时间。

操作：
- 上传。
- 启用。
- 禁用。
- 更新。
- 删除。
- 打开目录。
- 查看 `Info.json`。

### v0.5：玩家管理和仪表盘

前提：
- `RESTAPIEnabled=True`。
- Palworld REST API 只由面板后端在本机或局域网访问，不直接暴露公网。

仪表盘：
- 服务器状态。
- 服务器版本。
- 服务器名称。
- 在线人数。
- 最大人数。
- 服务器 FPS。
- 运行时间。
- 游戏内天数。
- 据点数量。
- CPU、内存、磁盘。
- 最近日志。
- 最近备份。

Palworld REST API：
- `GET /info`
- `GET /metrics`
- `GET /players`
- `GET /settings`
- `POST /announce`
- `POST /kick`
- `POST /ban`
- `POST /unban`
- `POST /save`
- `POST /shutdown`
- `POST /stop`

玩家操作：
- 广播消息。
- 踢出玩家。
- 封禁玩家。
- 解封玩家。
- 保存世界。
- 优雅关服。

### v0.6：打包和安装体验

目标：
- 一个可执行文件启动。
- 首次访问 Web 引导初始化。
- 自动检测 SteamCMD 和 PalServer 目录。
- Linux 提供 systemd service。
- 提供 Windows 服务安装。
- 写清楚备份、MOD 风险和 REST API 暴露风险。

## 页面结构

```text
/login
/dashboard
/server
/config
/mods
/backups
/logs
/settings
```

## 后端模块

```text
auth       登录和单管理员账号
server     安装、更新、启停、进程状态
steamcmd   SteamCMD 路径检测和命令封装
config     PalWorldSettings.ini 解析和保存
palapi     Palworld REST API client
mods       MOD 上传、解析、启用、禁用、删除
backup     备份、恢复、保留策略
logs       实时日志和历史日志
tasks      安装、更新、备份、恢复等长任务
settings   面板自身设置
system     CPU、内存、磁盘、端口检测
```

## SQLite 表

### app_settings

```sql
key TEXT PRIMARY KEY
value TEXT NOT NULL
```

常用键：
- `pal_server_path`
- `steamcmd_path`
- `rest_api_url`
- `admin_password_encrypted`
- `auto_backup_enabled`
- `auto_backup_retention`
- `server_launch_args`

### users

```sql
id INTEGER PRIMARY KEY
username TEXT NOT NULL UNIQUE
password_hash TEXT NOT NULL
created_at DATETIME NOT NULL
```

自用场景第一版只允许一个管理员。

### mods

```sql
id INTEGER PRIMARY KEY
name TEXT
package_name TEXT NOT NULL UNIQUE
version TEXT
author TEXT
folder_name TEXT NOT NULL
enabled BOOLEAN NOT NULL
server_supported BOOLEAN NOT NULL
info_json TEXT NOT NULL
installed_at DATETIME NOT NULL
updated_at DATETIME NOT NULL
```

### backups

```sql
id INTEGER PRIMARY KEY
filename TEXT NOT NULL
path TEXT NOT NULL
size INTEGER NOT NULL
type TEXT NOT NULL
note TEXT
created_at DATETIME NOT NULL
```

`type` 可取：
- `manual`
- `auto`
- `pre_config`
- `pre_mod`
- `pre_update`
- `pre_restore`
- `startup`

### tasks

```sql
id INTEGER PRIMARY KEY
type TEXT NOT NULL
status TEXT NOT NULL
log TEXT
created_at DATETIME NOT NULL
finished_at DATETIME
```

## 后端 API 草案

### 服务器

```http
GET  /api/server/status
POST /api/server/install
POST /api/server/update
POST /api/server/start
POST /api/server/stop
POST /api/server/restart
GET  /api/server/logs
POST /api/server/announce
POST /api/server/save
```

### 配置

```http
GET  /api/config
PUT  /api/config
POST /api/config/backup
```

### MOD

```http
GET    /api/mods
POST   /api/mods/upload
POST   /api/mods/:id/enable
POST   /api/mods/:id/disable
POST   /api/mods/:id/update
DELETE /api/mods/:id
GET    /api/mods/:id/info
```

### 备份

```http
GET    /api/backups
POST   /api/backups
POST   /api/backups/:id/restore
DELETE /api/backups/:id
```

### 玩家

```http
GET  /api/players
POST /api/players/:userid/kick
POST /api/players/:userid/ban
POST /api/players/:userid/unban
```

### 设置

```http
GET /api/settings
PUT /api/settings
```

## 安全和稳定性规则

- 面板必须登录。
- 第一版只做单管理员。
- 不把 Palworld REST API 暴露公网。
- 后端调用 REST API 时隐藏 Basic Auth。
- 日志脱敏管理员密码、服务器密码、Authorization header。
- 修改配置、更新服务器、MOD 操作、恢复备份前都自动备份。
- 强制停止、恢复备份、删除备份、删除 MOD、更新专服需要二次确认。
- 每个长任务都有任务日志和最终状态。
- 所有文件路径必须做目录逃逸检查，尤其是解压 MOD 时防止 zip slip。

## 开发优先级

1. 本地面板骨架：Go、Vue 3、SQLite、登录、基础布局。
2. 服务器控制：SteamCMD 检测、安装、更新、启动、停止、重启、实时日志。
3. 配置编辑：解析 `PalWorldSettings.ini`、表单编辑、保存前备份、重启提示。
4. 备份恢复：手动备份、自动备份、一键恢复、恢复前停服和保护备份。
5. MOD 管理：上传、解析 `Info.json`、安装到 `Mods/Workshop`、启用/禁用 `ActiveModList`、删除。
6. 玩家管理：REST API 指标、玩家列表、广播、踢人、封禁、保存世界。
7. 打包部署：单文件、systemd、Windows 服务、安装文档。

## 当前最终结论

项目应定为：

> 一个单文件启动的自用幻兽帕鲁单服管理面板。

技术路线：

```text
Go 后端 + Vue 3 + Naive UI + SQLite + REST/WebSocket
```

优先目标：

```text
Linux 原生专服 + SteamCMD + 配置/备份/日志/玩家管理优先，Windows 提供完整 MOD 管理
```

Docker 和多实例不作为第一版目标。
