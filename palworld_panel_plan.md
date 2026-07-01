# PalPanel Lite 项目计划

## 项目定位

PalPanel Lite 是一个单机单服自用型幻兽帕鲁专服管理面板。它运行在和 Palworld Dedicated Server 同一台机器上，通过本地进程、文件系统、SteamCMD 和 Palworld REST API 管理一台服务器。

一句话目标：

> 不用记命令、不用手改配置、不怕 MOD 操作前没有备份。

## 明确不做

- 多服务器实例。
- 多节点 Agent。
- Windows 运行支持。
- Windows 服务安装。
- 商业化租服。
- 复杂权限系统。
- 插件市场。
- 计费系统。
- 集群调度。
- Nexus 等第三方 MOD 站点自动下载。

## 核心要做

- 一键安装专服。
- 一键更新专服。
- 启动、停止、重启。
- 实时日志。
- 可视化编辑 `PalWorldSettings.ini`。
- 自动备份和一键恢复。
- 在线玩家管理。
- MOD 上传、Steam 创意工坊下载、识别、启用、禁用、删除。

## 平台优先级

1. Linux only：面板只面向 Linux 运行环境。
2. Linux 原生 SteamCMD 是开发、调试和早期可用路径。
3. Docker/Compose 是最终部署目标，后续应把安装、更新、备份、恢复、配置和日志流程收敛到容器化部署模型。
4. Windows 不作为支持平台；现有 Windows 兼容代码或文档只能视为历史遗留，后续应逐步删除或降级为未支持路径。

原因：
- 用户目标是自用单服，不需要多实例编排。
- Linux 更适合长期跑专服：systemd、SSH、VPS/NAS/家用小主机部署都更自然，资源占用和自动化也更稳定。
- 用户不需要 Windows 上运行，继续向 Windows 靠近会增加无意义的平台分支、测试成本和交互复杂度。
- 官方服务端 MOD 当前只支持 Windows Dedicated Server 只是外部事实，不再驱动本项目支持 Windows；Linux 上的 MOD 管理保留为文件型、实验性、自担风险能力。
- Docker/Compose 更符合最终自部署目标：依赖、目录、端口、升级和备份边界更可复制；原生 Linux 只作为先跑通能力和排障路径。

## 推荐技术栈

- 后端：Go。
- 前端：Vue 3 + TypeScript + Naive UI。
- 数据库：SQLite。
- 通信：REST + WebSocket。
- 打包：Go 单个可执行文件内嵌前端静态资源。

Go 后端负责：
- 调用 SteamCMD。
- 启停 Linux `PalServer.sh`，并为后续 Docker/Compose 编排预留清晰边界。
- 读写配置文件。
- 压缩和恢复备份。
- 解析 MOD `Info.json`。
- 代理调用 Palworld REST API。
- 推送日志和任务进度。

## 目录模型

面板自身：

```text
palpanel-lite/
  palpanel
  data/
    app.db
    backups/
    uploads/
    tasks/
```

Palworld 专服目录由用户配置：

```text
PalServer/
  DefaultPalWorldSettings.ini
  Pal/
    Saved/
      Config/
        LinuxServer/
          PalWorldSettings.ini
      SaveGames/
  Mods/
    PalModSettings.ini
    Workshop/
```

Linux 配置路径：

```text
Pal/Saved/Config/LinuxServer/PalWorldSettings.ini
```

实际实现和文档应以 Linux 路径为主；不再为了 WindowsServer 路径扩展产品范围。

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
- 检测游戏、REST API、RCON 端口是否已被占用。

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
- 读取、初始化复制、保存渲染和手动 `.bak` 复制都必须限制 `PalWorldSettings.ini` / `DefaultPalWorldSettings.ini` 大小，当前上限 4 MiB；超限时拒绝，不能产生备份或写文件副作用。

保存逻辑：
1. 读取 INI。
2. 解析 `OptionSettings=(...)`。
3. 映射到 JSON/结构体。
4. 表单修改。
5. 保存前生成 `.bak`。
6. 通过同目录临时文件和平台安全替换写回 `PalWorldSettings.ini`，避免直接截断写入导致半文件。
7. 提示需要重启。

配置分组：
- 基础设置：服务器名称、描述、密码、管理员密码、最大玩家数。
- 游戏倍率：经验、捕获、掉落、采集、孵蛋、白天速度、夜晚速度。
- 世界/性能设置：死亡惩罚、据点数量、公会人数、公会据点上限、建筑上限、帕鲁刷新率、帕鲁同步距离、建筑退化速度。
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
- 备份创建时跳过符号链接，不跟随链接读取服务器目录外的内容。
- 备份创建时限制源文件数量、单文件大小和总读取字节数，当前与恢复限制一致：最多 200000 个文件、单文件 8 GiB、总内容 64 GiB；超限时不能提交备份元数据或留下最终 `.zip`。

备份触发：
- 手动备份。
- 每天自动备份。
- 每次通过启动校验和后台任务准入后、真正启动前自动备份；重复启动、启动参数格式错误或已有后台任务运行这类拒绝请求不能产生备份副作用。
- 每次安装、更新、删除、恢复 MOD 前自动备份。
- 每次修改配置前自动备份。
- 每次更新专服前自动备份。

默认保留策略：
- 自动备份最多保留最近 20 个。
- 手动备份不自动删除。
- 备份创建必须先写入同目录临时压缩包，在 SQLite 元数据行可以提交或回滚的窗口内再改名为最终 `.zip`；数据库插入失败时不能留下最终备份压缩包。
- 手动备份 API 的 `type` 输入必须先归一化为安全备份类型，任务类型、任务日志、备份文件名和 SQLite 元数据必须使用同一个最终类型。
- 删除备份或自动保留清理时，不能先删压缩包再删数据库记录；必须先提交 SQLite 行删除，再删除压缩包。如果数据库提交失败，备份文件和记录都必须保留；如果压缩包删除失败，必须恢复备份元数据记录，避免 UI 留下指向缺失文件的备份记录。
- 备份列表和仪表盘最近备份这类只读响应不能序列化未通过备份目录边界校验的历史 `backups.path`；恢复、删除和保留清理仍必须在实际文件操作前做同一套失败关闭的路径校验。

恢复流程：
1. 确认危险操作。
2. 停止服务器。
3. 在停止或写文件前预检备份 ZIP：所有条目路径必须限制在服务器目录下，不能包含符号链接条目，所有文件条目必须可完整读取，并限制文件数量、单文件未压缩大小和总未压缩大小，避免损坏压缩包或 zip bomb 恢复到一半才失败。
4. 恢复前再做一次保护性备份。
5. 解压覆盖目标目录；每个文件必须先写入同目录临时文件，再用平台安全替换覆盖目标文件。
6. 启动服务器。
7. 检查状态和日志。

### v0.4：MOD 管理

这是项目差异点。当前 MOD 支持本地上传和 Steam 创意工坊 ID/链接下载，不做 Nexus 等第三方站点自动下载。

官方约束：
- 服务端 MOD 当前只支持 Windows Dedicated Server。
- 只有为服务端构建的 MOD 才能工作。
- MOD 可能导致存档损坏或崩溃。

平台策略：
- Linux 专服是唯一产品目标：提供实验性文件型 MOD 管理、Info.json 识别、备份、启用列表编辑和明确风险提示，不承诺官方服务端 MOD 可用。
- Windows 专服不支持；不要为了“官方 Windows MOD 支持”牵引本项目架构。

MOD 安装流程：
1. 确认 PalServer 当前未运行；如果正在运行，先停止服务器。
2. 上传 zip、7z 或 rar，或通过 SteamCMD 下载 Steam 创意工坊物品。
   - 后端必须对 MOD 上传/更新请求体设置硬限制，当前上限为 512 MiB，避免超大 multipart 请求落盘占满空间。
   - multipart 解析必须使用远小于硬上限的内存阈值，超过阈值但仍低于 512 MiB 硬上限的归档应落到临时文件，而不是整体缓存在内存中。
   - Steam 创意工坊下载接受纯数字物品 ID 或包含 `id=<number>` 的链接，使用 Palworld Workshop app `1623730` 执行 `steamcmd +login anonymous +workshop_download_item 1623730 <id> validate +quit`。
   - SteamCMD 下载结果从 SteamCMD 状态目录候选位置查找；下载内容如果直接包含 `Info.json` 就直接安装，如果只包含一个 zip、7z 或 rar，则按上传归档同一套解压和资源限制处理；如果下载内容是 Steam 客户端已安装布局，即 `Mods/ManagedMods/<PackageName>/Info.json` 加 `Pal/Content/Paks/~WorkshopMods/<folder>/*.pak`，则按 ManagedPak 布局安装到服务器对应目录。
   - 原生 ZIP 解压必须拒绝符号链接条目；外部解压器产出的符号链接不能作为 `Info.json` 元数据来源。
   - MOD 解压结果必须限制文件数量、单文件大小和总大小，当前最多 100000 个文件、单文件 2 GiB、总解压内容 8 GiB；原生 ZIP 解压必须在写入时按声明大小和实际复制字节计数，外部 7z/RAR 解压后也必须扫描结果目录并在元数据识别前拒绝超限内容。
   - 外部解压器执行必须有运行时间和输出大小上限，当前超时 5 分钟，错误输出最多保留 64 KiB，避免卡死或把巨大 stderr/stdout 写入 API/task 日志。
3. 解压到临时目录。
   - 上传归档和解压内容必须放在同一个操作级临时工作目录内；解压器如果在目标内容目录外产生文件，必须在识别元数据前拒绝。
   - 解压后的目录树如果包含任何符号链接，必须在识别元数据或复制到 `Mods/Workshop` / `Mods/ManagedMods` / `Pal/Content/Paks/~WorkshopMods` 前拒绝。
4. 寻找直接或间接包含的 `Info.json`。
5. 读取 `PackageName`、`Version`、作者等元数据；`Info.json` 必须在 JSON 解析、美化、SQLite 持久化或 API 返回前限制大小，当前上限 1 MiB；`PackageName` 必须先校验为单个安全的设置/路径片段，拒绝换行、路径分隔符、控制字符、Windows 非法字符、保留设备名和结尾点号，才能写入数据库、写入 `PalModSettings.ini` 或用于 `Mods/ManagedMods/<PackageName>` 路径。
6. 检查 `InstallRule` / `InstallRules` 是否包含 `"IsServer": true`。
7. 安装前自动备份。
8. 官方 Workshop 源目录布局先把解压结果流式复制到 `Mods/Workshop` 下的临时目录；Steam 客户端 ManagedPak 布局分别复制 `Mods/ManagedMods/<PackageName>` 和 `Pal/Content/Paks/~WorkshopMods/<folder>` 到各自父目录下的临时目录。单文件复制必须通过同目录临时文件和平台安全替换，不能把整个 MOD 文件一次性读入内存；最终 `folder_name` 必须是单个安全的 Workshop 或 `~WorkshopMods` 目录名，新上传从文件名、`PackageName` 或 pak 目录名派生时要经过净化，复用已有数据库行时要先校验，不能包含路径分隔符、换行、控制字符、Windows 非法/保留名称或结尾点号；SQLite 事务提交 MOD 记录插入/更新后，才能替换最终安装目录，数据库提交失败时不能替换已安装文件；如果最终替换失败，必须恢复新增或更新前的数据库记录，且旧目录移回失败时必须把该二次失败报告给调用方。
9. 通过同目录临时文件和平台安全替换写入或更新 `Mods/PalModSettings.ini`；读取旧文件以派生活跃 MOD 或保留未知行时必须限制大小，当前上限 1 MiB，超限时在创建备份或提交数据库状态前拒绝。
10. 启用时添加 `ActiveModList=<PackageName>`。
11. 提示启动或重启服务器生效。

`PalModSettings.ini` 示例：

```ini
[PalModSettings]
bGlobalEnableMod=true
ActiveModList=PackageNameA
ActiveModList=PackageNameB
```

启用逻辑：
- 要求 PalServer 未运行。
- SQLite 必须先提交启用状态更新，再修改 `PalModSettings.ini`；如果数据库提交失败，启用配置和记录都必须保持不变；如果设置写入失败，必须恢复启用前的 MOD 记录状态。
- 确保 `bGlobalEnableMod=true`。
- 添加 `ActiveModList=<PackageName>`。
- 不重复添加。

禁用逻辑：
- 要求 PalServer 未运行。
- SQLite 必须先提交禁用状态更新，再修改 `PalModSettings.ini`；如果数据库提交失败，启用配置和记录都必须保持不变；如果设置写入失败，必须恢复禁用前的 MOD 记录状态。
- 从 `ActiveModList` 移除对应 `PackageName`。
- 不删除文件。

删除逻辑：
- 要求 PalServer 未运行。
- 先禁用。
- 自动备份。
- SQLite 必须先提交 MOD 记录删除，再修改 `PalModSettings.ini` 或删除 `Mods/Workshop` / `Mods/ManagedMods` / `Pal/Content/Paks/~WorkshopMods` 文件；如果数据库删除失败，文件、启用配置和记录都必须保留；目录删除必须先移动到服务器根目录下的删除暂存区，后续清理失败时必须先恢复已移动目录，再尽量恢复删除前的数据库记录和启用配置。
- 删除 `Mods/Workshop/<folder_name>/`。
- 清理 `Mods/ManagedMods/<PackageName>/`。
- 清理 `Pal/Content/Paks/~WorkshopMods/<folder_name>/`。
- 提示启动或重启。

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
- 游戏端口、REST API 端口、RCON 端口本机占用状态。
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

仪表盘聚合这些相互独立的 REST 读接口时应并发请求，保留部分成功/部分失败的响应形状，避免 REST 慢或不可用时串行叠加多个超时。后端读取 Palworld REST 上游响应体必须有大小上限，当前为 2 MiB，超限时返回明确的上游响应过大错误，不能把截断数据继续交给 JSON 解析或错误序列化。

玩家操作：
- 广播消息。
- 踢出玩家，需要二次确认。
- 封禁玩家，需要二次确认。
- 解封玩家，需要二次确认。
- 保存世界。
- 优雅关服。
- 后端转发这些 Palworld REST 写操作前必须校验控制载荷：广播/踢封/关服消息最多 500 个字符，玩家 UserID 最多 128 个字符，shutdown 等待时间默认 30 秒且最大 3600 秒。

### v0.6：打包和安装体验

目标：
- 一个可执行文件启动。
- 提供 Dockerfile / Compose 部署路径，并把它作为最终推荐部署方式：面板镜像内构建 Vue 前端和 Go 单文件，运行时通过当前目录 bind mount 挂载 `./data` 到 `/data` 保存面板状态，挂载 `./PalServer` 到 `/palserver` 作为 Palworld 服务端目录；不依赖 Docker named volume 或匿名 volume 保存核心数据。
- Docker 运行镜像必须内置 SteamCMD 并把 `steamcmd` 放入 `PATH`，这样容器部署下 `steamcmd_path` 可留空，面板仍能执行安装和更新；SteamCMD 自更新和缓存这类可变状态必须放在 bind-mounted `/data/steamcmd` 目录下，容器 `HOME` 也必须指向可写的 `/data`，避免 SteamCMD/Steam 运行时用户状态写到不可写目录；该镜像当前按 Linux amd64 目标处理。
- GitHub Actions 负责在 GitHub 托管 runner 上构建并发布 Linux amd64 镜像到 GHCR，镜像名使用 `ghcr.io/<lowercase-owner>/palpanel-lite`，Compose 通过 `PALPANEL_IMAGE` 覆盖完整镜像引用。
- Compose 默认发布 Palworld 游戏 UDP 端口，变量必须能覆盖宿主机端口和容器内游戏端口；Palworld REST/RCON 端口默认不发布到宿主机，相关操作通过面板后端代理。
- Docker/Compose 首次初始化空面板数据库时应通过 `PALPANEL_DEFAULT_PAL_SERVER_PATH=/palserver` 预填 `pal_server_path`，让容器内安装专服不需要用户手动输入挂载路径；已有 SQLite 设置不能被该环境变量覆盖。
- 提供 `.env.example` 作为 Compose 部署变量样例，覆盖端口、数据目录、PalServer 目录、运行 UID/GID、镜像版本和时区；本地 `.env` 不纳入版本管理，部署定制应优先改 `.env`，不要直接改 `deploy/compose.yaml`。
- 首次访问 Web 引导初始化：管理员登录后用状态驱动清单提示保存路径、确认 SteamCMD、安装 PalServer、初始化配置、确认备份策略和可选连接 REST API。
- 自动检测 SteamCMD 和 PalServer 目录。
- Linux 提供 systemd service。
- 进程收到 SIGINT/SIGTERM 时优雅停止 HTTP 服务，先停止由面板启动并托管的 PalServer，等待自动备份调度器等面板后台任务退出，再关闭 SQLite 和面板资源；Compose 应启用 `init: true` 并提供足够停止宽限期，避免容器停止时子进程无法回收或被直接强杀。
- 写清楚备份、MOD 风险和 REST API 暴露风险。
- Windows NSSM/服务安装脚本不再随部署产物提供。

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
- `rest_api_username`
- `rest_api_password`
- `auto_backup_enabled`
- `auto_backup_retention`
- `auto_backup_interval_hours`
- `server_launch_args`

派生字段：
- `game_port`
- `launch_players`
- `public_lobby`
- `no_mods`
- `performance_flags`
- `worker_threads`

这些派生字段由 `server_launch_args` 解析/回写，不作为单独的 SQLite 键保存。管理员账号密码哈希保存在 `users` 表，不放入 `app_settings`。

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

运行中的任务记录必须保留；已完成任务只保留最新 200 条，启动时和任务完成后都要清理更旧的完成记录，避免面板长期运行后 SQLite 任务表无限增长。

## 后端 API 草案

除健康检查、认证状态、初始化和登录外，API 默认要求登录。

### 健康和认证

```http
GET  /api/health
GET  /api/auth/state
POST /api/auth/setup
POST /api/auth/login
POST /api/auth/logout
GET  /api/auth/me
```

### 仪表盘、任务和实时事件

```http
GET /api/dashboard
GET /api/tasks
GET /api/events
```

`/api/events` 是同源认证 WebSocket，用于推送任务、服务器日志、运行状态快照和后端运行中操作标记变化；REST 轮询保留为断线兜底。

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
GET  /api/server/rest-settings
POST /api/server/shutdown
POST /api/server/rest-stop
```

`GET /api/server/status` 返回 PalServer/SteamCMD 检测结果、面板托管/外部运行状态、后端共享运行中操作标记 `operation_running`，以及 `port_checks`。`port_checks` 使用本机 bind 探测判断游戏 UDP 端口、REST API TCP 端口、RCON TCP 端口是否可用、已占用、禁用或未知。服务器运行中时端口已占用可能是正常状态；启动前占用通常表示冲突。

### 配置

```http
GET  /api/config
POST /api/config/init
PUT  /api/config
POST /api/config/backup
```

### MOD

```http
GET    /api/mods
POST   /api/mods/upload
POST   /api/mods/workshop/download
POST   /api/mods/:id/enable
POST   /api/mods/:id/disable
POST   /api/mods/:id/update
POST   /api/mods/:id/open-dir
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

设置响应包含基础落库字段，也包含从 `server_launch_args` 派生出的常用启动参数字段。保存时这些结构化字段会合并回原始启动参数，未知自定义参数必须保留。

## 安全和稳定性规则

- 面板必须登录。
- 第一版只做单管理员；首次设置必须在后端临界区内再次检查并创建管理员，避免并发 setup 请求创建多个账号。
- Session Cookie 必须使用 `HttpOnly` 和 `SameSite=Lax`；当面板请求是 HTTPS，或可信反向代理通过 `X-Forwarded-Proto: https` 表示外部 HTTPS 时，Session Cookie 必须带 `Secure`。
- 所有通过后端共享 JSON 解码器处理的 JSON API 请求体必须设置硬限制，当前上限为 1 MiB；超限请求返回 `413 Request Entity Too Large`。请求体必须只包含一个 JSON 值，允许尾随空白，但不能接受 `{...}{...}` 这类拼接值，也不能接受同一对象内重复或未知的字段名。带可选 JSON 参数的动作接口可以把真正空的请求体视为未提供参数，但非空请求体仍必须使用同一套严格 JSON 规则。
- 没有请求体合同的认证动作接口必须拒绝非空请求体，例如 logout、无 body 的服务器动作、REST 保存/停止、玩家解封、配置备份、备份恢复/删除、MOD 启用/禁用/打开目录/删除；有明确 JSON 或 multipart 合同的接口按各自解析规则处理。
- 浏览器发起的 `POST`、`PUT`、`PATCH`、`DELETE` 请求如果带有跨源 `Origin` 必须拒绝；无 `Origin` 的本地脚本/API 客户端请求保持可用。
- 不把 Palworld REST API 暴露公网。
- SQLite 运行时应限制为单连接并设置 busy timeout，避免后台自动备份、任务日志、备份元数据和 API 请求之间因多连接写入竞争产生瞬时 `SQLITE_BUSY`。
- 后端调用 REST API 时隐藏 Basic Auth。
- 后端读取 Palworld REST 上游响应体必须限制大小，当前上限为 2 MiB；超过上限必须明确报错，不能静默截断后继续解析 JSON 或拼接错误响应。
- 后端代理 Palworld REST 写操作时必须限制操作载荷：消息最多 500 个字符，玩家 UserID 最多 128 个字符，shutdown 等待时间最大 3600 秒；这些限制不能只依赖前端控件。
- 日志脱敏管理员密码、服务器密码、Authorization header。
- 服务器实时日志每条消息必须有大小上限，当前为 8 KiB；超长单行在进入内存滚动窗口、仪表盘/API 或 WebSocket 事件前截断并加标记。
- 修改配置、更新服务器、MOD 操作、恢复备份前都自动备份。
- 更新专服、停止/重启专服、REST 优雅关服/停止、踢出/封禁/解封玩家、恢复/删除备份、更新/删除 MOD 必须先经过 UI 二次确认，并在请求中携带 `X-Palpanel-Confirm: true`。
- UI 必须在 REST/API 状态不可用、已有 REST 操作执行中或已有后端任务运行时禁用 Palworld REST 写操作；服务器安装、更新、启动、停止、重启动作必须由按钮、首次设置清单和实际执行入口共享同一套本地阻断原因判断，覆盖后端任务运行、外部 PalServer、缺少 PalServer、重复启动、非面板托管停止和已有服务器动作执行中等状态。备份和 MOD 操作的按钮状态与实际执行入口也必须共享本地 in-flight 阻断，避免重复点击或重叠 UI 动作先发到后端。
- 如果检测到 PalServer 由面板外部启动，面板必须拒绝配置初始化/保存、手动配置 `.bak` 备份、启动、安装/更新、恢复备份等会修改运行中文件的操作，提示用户先从外部停止服务器；配置初始化必须使用显式 `POST /api/config/init`，`GET /api/config` 只能读取已存在配置，不能创建文件；恢复备份必须在创建服务器目录、保护性备份或解压文件前完成该检查。前端也必须在外部运行态下本地禁用配置初始化按钮和恢复按钮；手动创建备份和删除备份不直接写服务器目录，仍按已有后台任务锁控制。
- 恢复备份写入单个文件时必须使用同目录临时文件和平台安全替换，避免写入失败时截断现有目标文件。
- 删除备份和自动保留清理必须先提交 SQLite 行删除，再移除备份压缩包；提交失败时不得删除压缩包，压缩包删除失败时必须恢复备份元数据。
- 备份只读 API 返回记录前必须过滤未通过备份目录边界校验的持久化路径，避免脏数据行把服务器外路径暴露给 UI；真正恢复/删除/清理仍按现有路径校验失败关闭。
- 备份创建必须限制服务器源文件资源，当前最多 200000 个文件、单文件 8 GiB、总读取内容 64 GiB；限制既要根据文件元数据预判，也要按实际复制字节计数，避免源文件在备份期间增长导致压缩任务无界运行。
- 备份恢复必须限制 ZIP 展开资源，当前上限为 200000 个文件、单个条目 8 GiB、总未压缩内容 64 GiB；这些限制必须在恢复预检和实际解压时都执行，避免校验后文件被替换或异常归档消耗无界磁盘/CPU。
- 保存面板设置必须通过后端运行中任务锁，并以一个 SQLite 事务原子写入；已有后台任务运行时必须拒绝。已配置的 PalServer 正在运行时，不能把 `pal_server_path` 改到其他目录，避免后续运行态检测和文件保护失效。
- 配置初始化、配置保存和手动 `.bak` 备份会写入服务器配置目录；检测到外部 PalServer 或已有后端任务运行时必须拒绝，不能与更新、恢复、MOD 或备份任务并发写文件。
- 配置初始化、保存和 `.bak` 复制必须使用同目录临时文件和平台安全替换；Windows 下不能依赖普通 `os.Rename` 覆盖已有配置文件；读取/复制配置文件和保存后的渲染结果必须限制在 4 MiB 内，超限时在创建备份或写入前拒绝。
- 启动和重启的启动阶段必须先通过后端运行中任务锁；已有后台任务运行时必须拒绝，不能创建 `startup` 备份或启动进程。重启还必须先校验下一次启动所需的路径、二进制文件和启动参数，再停止面板托管的 PalServer，避免无效重启请求把运行中的服务器停掉。
- MOD 上传、更新、启用、禁用、删除会修改 `Mods/Workshop`、`Mods/ManagedMods`、`Pal/Content/Paks/~WorkshopMods` 或 `PalModSettings.ini`，必须在 PalServer 停止时执行，完成后再启动或重启生效。
- MOD Steam 创意工坊下载和本地上传使用同一套后端运行锁、停服检查、`pre_mod` 备份、`Info.json` 校验、资源限制、SQLite 提交和最终目录替换流程；支持官方 `Mods/Workshop/<folder>/Info.json` 源布局，也支持 Steam 客户端 `Mods/ManagedMods/<PackageName>/Info.json` 加 `Pal/Content/Paks/~WorkshopMods/<folder>/*.pak` 的已安装布局；缺少目标父目录时必须自动创建。
- MOD `Info.json` 元数据读取必须有独立大小上限，当前为 1 MiB；512 MiB 上传请求体上限不能替代元数据解析和数据库/API 暴露前的专门限制。
- MOD `Info.json` 的 `PackageName` 必须是单个安全的设置/路径片段，拒绝换行、路径分隔符、控制字符、Windows 非法字符、保留设备名和结尾点号；上传/更新在持久化前校验，已有脏数据行在启用/禁用状态变更、写 `PalModSettings.ini` 或派生 `ManagedMods` 路径前必须拒绝。
- MOD `mods.folder_name` 必须是单个安全的 `Mods/Workshop` 或 `~WorkshopMods` 子目录名；上传/更新复用已有行、启用时检查已安装的 `Info.json` 或 managed pak 文件、删除/打开目录目标、以及只读响应里的 `install_path` 派生前都要校验，已有脏数据行不能导致备份、数据库状态变更、设置写入或文件系统修改。
- MOD 启用/禁用必须先提交 SQLite 中的 `mods.enabled` 更新，再修改 `PalModSettings.ini`；提交失败时不得修改设置文件，设置写入失败时必须补偿恢复原 MOD 行状态。
- `Mods/PalModSettings.ini` 读取必须限制在 1 MiB 内，覆盖活跃列表派生和启用/禁用/删除/更新时的设置重写；只读 MOD API 遇到超限或不可读设置文件时必须明确返回错误，不能静默当作空启用列表；写入必须使用同目录临时文件和平台安全替换，不能直接截断写入。会改写该文件的 MOD 操作必须在创建备份或提交数据库状态前先完成读取大小预检。
- MOD 上传和更新必须先通过后端运行中任务锁，再读取 multipart 请求体；创意工坊下载也必须先通过同一运行中任务锁，再解析请求并执行 SteamCMD；已有后台操作运行时必须直接拒绝，不能先读取或落盘上传归档，也不能启动 SteamCMD。multipart 请求体必须仍受 512 MiB 硬上限约束，但解析内存阈值必须较小，避免接近上限的 MOD 归档整体占用面板内存。
- MOD 上传和更新必须先提交 SQLite 中的 MOD 记录插入/更新，再替换最终 `Mods/Workshop/<folder>` 或 ManagedPak 的 `Mods/ManagedMods/<PackageName>` 与 `Pal/Content/Paks/~WorkshopMods/<folder>` 目录；数据库提交失败时不得替换最终目录，最终替换失败时必须补偿恢复数据库记录，旧目录恢复失败不能被静默忽略。
- MOD 安装/更新复制暂存文件时必须流式写入同目录临时文件并通过平台安全替换提交，避免资源限制内但较大的 MOD 文件被一次性读入内存。
- MOD 删除必须先提交 SQLite 中的 MOD 记录删除，再禁用 `PalModSettings.ini` 和删除最终目录；最终目录删除必须先进入同目录暂存区，后续设置或目录清理失败时必须补偿恢复原 MOD 行、原先启用时恢复 `ActiveModList`，并恢复已暂存的目录。
- MOD ZIP 上传必须拒绝符号链接条目；解压后的 `Info.json` 扫描必须跳过符号链接，不能信任链接到临时目录外的元数据。
- MOD 解压后的完整目录树必须拒绝符号链接，覆盖外部 7z/RAR 解压器留下链接的情况。
- MOD 解压必须在操作级临时工作目录中进行；外部解压器如果把内容写到目标内容目录之外，必须拒绝该上传。
- MOD 解压必须限制资源占用：最多 100000 个文件、单文件 2 GiB、总内容 8 GiB；原生 ZIP 路径在解压前按条目声明大小检查并在复制时继续计数，外部 7z/RAR 路径在解压后扫描完整目录树，且外部解压器最多运行 5 分钟、最多记录 64 KiB 输出。
- 每个长任务都有任务日志和最终状态；持久化任务日志必须有大小上限，当前每个任务保留最新 1 MiB，并在丢弃旧输出时写入截断标记；已完成任务记录只保留最新 200 条且不清理运行中任务，避免长时间 SteamCMD/服务端输出或长期运行让 SQLite 行和 API/WebSocket 负载无界增长。
- SteamCMD、服务器启动/重启的启动阶段、备份、恢复、MOD 修改和自动备份共享同一个后端运行中任务锁；已有任务运行时，新提交的变更任务必须拒绝，不能先创建任务记录或产生文件副作用。
- 前端判断是否有后台操作运行时，必须同时参考任务列表中的 running 任务和 `/api/server/status`/`/api/events` 的 `operation_running`，因为设置、配置保存等短同步操作会占用同一后端锁但不一定创建任务记录；后端运行锁获取和释放都必须实时广播该标记变化。服务端操作的本地守卫也必须使用这个合并后的 active-task 状态，而不是只依赖按钮禁用。
- 所有文件路径必须做目录逃逸检查，尤其是解压 MOD 时防止 zip slip。

## 开发优先级

1. 本地面板骨架：Go、Vue 3、SQLite、登录、基础布局。
2. 服务器控制：SteamCMD 检测、安装、更新、启动、停止、重启、实时日志。
3. 配置编辑：解析 `PalWorldSettings.ini`、表单编辑、保存前备份、重启提示。
4. 备份恢复：手动备份、自动备份、一键恢复、恢复前停服和保护备份。
5. MOD 管理：上传、解析 `Info.json`、安装到 `Mods/Workshop`、启用/禁用 `ActiveModList`、删除。
6. 玩家管理：REST API 指标、玩家列表、广播、踢人、封禁、保存世界。
7. 打包部署：Linux 单文件、systemd、Dockerfile、Docker Compose、安装文档。

## 当前最终结论

项目应定为：

> 一个单文件启动的自用幻兽帕鲁单服管理面板。

技术路线：

```text
Go 后端 + Vue 3 + Naive UI + SQLite + REST/WebSocket
```

优先目标：

```text
Linux only + SteamCMD/native first for development + Docker/Compose as final deployment target
```

Windows 不作为目标平台。Docker 不再是“可选以后再说”的边缘项，而是最终部署形态；多实例仍不作为第一版目标。
