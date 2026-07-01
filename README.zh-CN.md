# PalPanel Lite 中文教程

PalPanel Lite 是一个单机单服自用的 Palworld 专服管理面板。它负责在同一台 Linux 服务器上管理一个 Palworld Dedicated Server，包括安装/更新服务器、启动停止、配置编辑、备份恢复、MOD 管理、玩家管理、日志查看和 Palworld REST API 操作。

推荐部署方式是 Docker Compose，镜像从 GHCR 拉取：

```text
ghcr.io/ksamni/palpanel-lite:latest
```

## 适用范围

- 适合一台 Linux 机器管理一台 Palworld 专服。
- 当前主要目标是 Linux + Docker Compose。
- 默认只把面板绑定到 `127.0.0.1`，远程访问建议放在反向代理或 VPN 后面。
- Palworld 游戏端口默认发布 UDP `8211`。
- Palworld REST API 和 RCON 默认不对外发布，应通过面板后端访问。

## 目录说明

使用根目录的 `docker-compose.yml` 时，默认持久化目录如下：

```text
./data       面板数据、SQLite 数据库、备份、SteamCMD 运行状态
./PalServer  Palworld Dedicated Server 安装目录
```

容器内部路径如下：

```text
/data       面板数据目录
/palserver  Palworld 服务端目录
```

不要把 `pal_server_path` 配成宿主机路径，例如 `/srv/palworld/PalServer`。在容器内它应该是：

```text
/palserver
```

## 快速部署

在服务器上准备一个目录：

```bash
mkdir -p /srv/palpanel
cd /srv/palpanel
```

如果你有仓库访问权限，可以直接克隆：

```bash
git clone https://github.com/KSAMNI/pal_tool.git .
```

如果你只想部署运行，至少需要把项目里的这两个文件放到 `/srv/palpanel`：

```text
docker-compose.yml
.env.example
```

然后创建本地配置：

```bash
cp .env.example .env
mkdir -p data PalServer
```

如果拉取 GHCR 镜像时报权限错误，先登录 GHCR：

```bash
docker login ghcr.io
```

如果包没有公开，登录账号需要有读取 `ghcr.io/ksamni/palpanel-lite` 的权限。

启动：

```bash
docker compose --env-file .env pull
docker compose --env-file .env up -d
```

查看状态：

```bash
docker compose ps
docker compose logs -f palpanel
```

本机访问：

```text
http://127.0.0.1:8080
```

如果面板部署在远程服务器上，默认端口只监听 `127.0.0.1`，你需要用 SSH 隧道、VPN，或反向代理访问。

如果要让外部玩家加入服务器，防火墙和云服务器安全组需要放行 Palworld 游戏 UDP 端口，默认是：

```text
8211/udp
```

## .env 配置

常用配置：

```env
PALPANEL_IMAGE=ghcr.io/ksamni/palpanel-lite:latest
PALPANEL_PORT=8080

PALWORLD_GAME_HOST_PORT=8211
PALWORLD_GAME_PORT=8211

PALPANEL_DATA_DIR=./data
PALPANEL_SERVER_DIR=./PalServer

PALPANEL_STEAMCMD_DIR=/data/steamcmd
PALPANEL_DEFAULT_PAL_SERVER_PATH=/palserver

PALPANEL_UID=10001
PALPANEL_GID=10001
PALPANEL_FIX_OWNERSHIP=true

TZ=Asia/Hong_Kong
```

字段含义：

| 变量 | 含义 |
|---|---|
| `PALPANEL_IMAGE` | Compose 拉取的面板镜像 |
| `PALPANEL_PORT` | 面板宿主机端口，默认只绑定到 `127.0.0.1` |
| `PALWORLD_GAME_HOST_PORT` | 宿主机对外开放的 Palworld UDP 游戏端口 |
| `PALWORLD_GAME_PORT` | 容器内 Palworld 游戏端口，应和面板启动参数保持一致 |
| `PALPANEL_DATA_DIR` | 宿主机面板数据目录 |
| `PALPANEL_SERVER_DIR` | 宿主机 PalServer 目录 |
| `PALPANEL_STEAMCMD_DIR` | 容器内 SteamCMD 可写状态目录 |
| `PALPANEL_DEFAULT_PAL_SERVER_PATH` | 新数据库首次写入的 PalServer 路径，容器部署应为 `/palserver` |
| `PALPANEL_UID` / `PALPANEL_GID` | 面板进程运行用户 |
| `PALPANEL_FIX_OWNERSHIP` | 启动时修复挂载目录权限 |
| `TZ` | 容器时区 |

Steam Workshop 匿名下载失败时，可以配置 Steam 账号：

```env
PALPANEL_STEAMCMD_USERNAME=你的Steam账号
PALPANEL_STEAMCMD_PASSWORD=你的Steam密码
```

这个账号需要拥有 Palworld。密码会在面板任务日志里被打码，但它仍然是容器环境变量，宿主机管理员可以看到。

## 首次打开面板

第一次访问面板时，会要求创建管理员账号。这个账号只保存在面板自己的 SQLite 数据库里。

创建后进入主界面，建议按这个顺序操作：

1. 打开 `服务器` tab。
2. 确认 `PalServer 路径` 是 `/palserver`。
3. `SteamCMD 路径` 可以留空，容器镜像里已经带了 `/usr/local/bin/steamcmd`，面板会从 `PATH` 检测。
4. 保存设置。
5. 点击 `安装`，面板会用 SteamCMD 下载 Palworld Dedicated Server。
6. 安装完成后点击 `启动`。
7. 第一次启动会创建 `Pal/Saved/Config/LinuxServer/PalWorldSettings.ini` 等运行配置目录。
8. 如需改配置，停止服务器后进入 `配置` tab 初始化和保存配置。

## 安装 PalServer

面板内点击 `安装` 后，会执行类似下面的 SteamCMD 操作：

```bash
steamcmd +force_install_dir /palserver +login anonymous +app_update 2394010 validate +quit
```

安装成功后，容器内 `/palserver` 目录会出现：

```text
PalServer.sh
Pal/
Engine/
steamapps/
```

宿主机对应目录是：

```text
./PalServer
```

如果日志里提示 `pal_server_path is not a directory`，通常是还没安装，或者面板里的 `PalServer 路径` 填错了。Docker 部署时应填 `/palserver`。

## 启动参数

`服务器` tab 里有结构化启动参数。常用项：

| 设置 | 说明 |
|---|---|
| 游戏端口 | 默认 `8211`，要和 `.env` 的 `PALWORLD_GAME_PORT` 对齐 |
| 最大玩家数 | 对应 `-players` |
| 公开大厅 | 对应 `-publiclobby` |
| 性能参数 | 对应 `-useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS` |
| 工作线程数 | 对应 `-NumberOfWorkerThreadsServer` |
| 禁用 MOD | 对应 `-NoMods` |

如果你改了游戏端口，需要同时改：

```env
PALWORLD_GAME_HOST_PORT=8211
PALWORLD_GAME_PORT=8211
```

以及面板里的游戏端口启动参数。

## 配置 PalWorldSettings.ini

配置文件位置：

```text
/palserver/Pal/Saved/Config/LinuxServer/PalWorldSettings.ini
```

宿主机对应：

```text
./PalServer/Pal/Saved/Config/LinuxServer/PalWorldSettings.ini
```

注意不要直接改：

```text
DefaultPalWorldSettings.ini
```

这个文件只是默认样例，改它不会影响服务器运行。

推荐流程：

1. 停止 PalServer。
2. 打开 `配置` tab。
3. 如果提示未初始化，点击 `初始化`。
4. 修改配置项。
5. 点击 `保存`。
6. 启动或重启 PalServer 生效。

常见配置：

| 前端字段 | 原始配置 key | 说明 |
|---|---|---|
| 自动保存间隔 | `AutoSaveSpan` | 单位秒，`30.000000` 表示 30 秒 |
| 服务器名称 | `ServerName` | 服务器列表显示名称 |
| 服务器玩家上限 | `ServerPlayerMaxNum` | 最大可加入玩家数 |
| REST API | `RESTAPIEnabled` | 是否开启 Palworld REST API |
| REST API 端口 | `RESTAPIPort` | 默认 `8212` |
| 管理员密码 | `AdminPassword` | REST API 和游戏内管理员相关密码 |
| 官方存档备份 | `bIsUseBackupSaveData` | Palworld 官方存档备份 |
| 允许客户端 MOD | `bAllowClientMod` | 是否允许启用 MOD 的客户端加入 |
| PvP 模式 | `bIsPvP` | PvP 总开关 |

配置页已经覆盖当前默认 `OptionSettings` 样本里的所有字段。字段旁边的信息图标会显示简短说明。

## Palworld REST API

面板会通过后端代理 Palworld REST API，浏览器不会直接拿到 REST Basic Auth。

推荐启用方式：

1. 停止服务器。
2. 打开 `配置` tab。
3. 设置：

```text
RESTAPIEnabled=True
RESTAPIPort=8212
AdminPassword=一个强密码
```

4. 保存配置。
5. 打开 `服务器` tab。
6. 确认 REST API URL：

```text
http://127.0.0.1:8212/v1/api
```

7. REST 用户名一般是：

```text
admin
```

8. REST 密码填写 `AdminPassword`。
9. 启动或重启服务器。
10. 打开 `仪表盘` 或 `玩家` tab 检查是否连接成功。

不要把 `8212` 直接暴露到公网。默认 Compose 文件也不会发布这个端口。

## 备份和恢复

面板备份存放在：

```text
./data/backups
```

备份包含存档、配置和 MOD 相关文件。推荐规则：

- 更新服务器前先备份。
- 修改配置前先备份。
- 安装、更新、删除 MOD 前先备份。
- 恢复备份前确认 PalServer 已停止。

手动备份：

1. 打开 `备份` tab。
2. 点击 `手动备份`。

恢复备份：

1. 停止 PalServer。
2. 打开 `备份` tab。
3. 找到目标备份。
4. 点击 `恢复`。
5. 确认操作。

面板在恢复前会尽量创建一个恢复前保护备份。

## MOD 管理

MOD 修改必须在 PalServer 停止时进行。上传、下载、启用、禁用、更新、删除后，都需要启动或重启服务器才能生效。

### Steam Workshop 自动下载

在 `MOD` tab 输入 Steam Workshop ID 或链接，然后点击下载安装。

示例：

```text
3625364851
https://steamcommunity.com/sharedfiles/filedetails/?id=3625364851
```

如果日志出现：

```text
ERROR! Download item ... failed (Failure)
```

说明匿名 SteamCMD 可能不能下载这个条目。解决方式：

1. 在 `.env` 里配置 Steam 账号：

```env
PALPANEL_STEAMCMD_USERNAME=你的Steam账号
PALPANEL_STEAMCMD_PASSWORD=你的Steam密码
```

2. 重启面板容器：

```bash
docker compose --env-file .env up -d
```

3. 再次尝试下载。

如果仍失败，就手动从本地 Steam 客户端整理 zip 上传。

### 手动上传 MOD 压缩包

面板支持：

```text
.zip
.7z
.rar
```

压缩包里必须能找到 `Info.json`。

普通官方服务端 MOD 结构：

```text
Mods/Workshop/任意目录名/Info.json
```

Steam 客户端常见 ManagedMods/Paks 结构：

```text
Pal/Content/Paks/~WorkshopMods/PalSurgeryTableUnlocker/mEi_PalSurgeryTableUnlocker_P.pak
Mods/ManagedMods/PalSurgeryTableUnlocker/Info.json
Mods/ManagedMods/PalSurgeryTableUnlocker/InstallManifest.json
```

打 zip 时第一层建议是：

```text
Pal/
Mods/
```

不要把整个 Palworld 游戏目录压进去。

正确示例：

```text
PalSurgeryTableUnlocker.zip
  Pal/Content/Paks/~WorkshopMods/PalSurgeryTableUnlocker/mEi_PalSurgeryTableUnlocker_P.pak
  Mods/ManagedMods/PalSurgeryTableUnlocker/Info.json
  Mods/ManagedMods/PalSurgeryTableUnlocker/InstallManifest.json
```

上传流程：

1. 停止 PalServer。
2. 打开 `MOD` tab。
3. 点击 `上传 MOD`。
4. 选择压缩包。
5. 上传成功后点击 `启用`。
6. 启动或重启 PalServer。

## 更新

### 更新面板镜像

```bash
docker compose --env-file .env pull
docker compose --env-file .env up -d
```

确认镜像：

```bash
docker compose images
docker compose logs -f palpanel
```

如果你用 `latest`，浏览器仍然看到旧 UI，通常是容器没有重新拉取或浏览器缓存。先执行上面的 `pull` 和 `up -d`，再强制刷新浏览器。

### 更新 PalServer

在面板里点击 `更新`。如果服务器由面板启动且正在运行，面板会先停止，更新成功后再尝试启动。更新前会创建预操作备份。

如果 PalServer 是在面板外部启动的，面板会阻止更新。先在外部停止服务器，再通过面板更新。

## 远程访问建议

默认 Compose 只监听：

```text
127.0.0.1:8080
```

推荐方式：

- SSH 隧道：

```bash
ssh -L 8080:127.0.0.1:8080 user@your-server
```

然后本地打开：

```text
http://127.0.0.1:8080
```

- 或者使用 VPN。
- 或者使用带 HTTPS 的反向代理。

反向代理时，不要直接把 Palworld REST API 端口 `8212` 暴露到公网。

## 常用命令

启动：

```bash
docker compose --env-file .env up -d
```

停止：

```bash
docker compose down
```

查看日志：

```bash
docker compose logs -f palpanel
```

拉取新镜像并重启：

```bash
docker compose --env-file .env pull
docker compose --env-file .env up -d
```

查看容器内目录：

```bash
docker compose exec palpanel sh
ls -la /data
ls -la /palserver
```

## 常见问题

### `unable to open database file: out of memory (14)`

这通常不是内存真的不够，而是 SQLite 打不开 `/data/app.db`，常见原因是挂载目录权限不对。

检查：

```bash
ls -la ./data
docker compose logs -f palpanel
```

默认镜像会以 root 启动一小段时间，修复 `/data`、`/palserver`、`/data/steamcmd` 权限后再降权运行。保持：

```env
PALPANEL_FIX_OWNERSHIP=true
```

然后重启：

```bash
docker compose --env-file .env up -d
```

### 面板提示 `palserver` 未安装

确认面板里的 `PalServer 路径` 是：

```text
/palserver
```

然后点击 `安装`。

宿主机上应能看到：

```text
./PalServer/PalServer.sh
```

### 配置页提示未初始化

Palworld 的运行配置通常要在服务器至少启动过一次后才完整生成。可以：

1. 安装 PalServer。
2. 启动一次。
3. 停止。
4. 回到 `配置` tab 点击 `初始化`。

### REST API 未连接

检查：

- `RESTAPIEnabled=True`
- `RESTAPIPort=8212`
- `AdminPassword` 已设置
- 面板 REST API URL 是 `http://127.0.0.1:8212/v1/api`
- REST 用户名是 `admin`
- REST 密码是 `AdminPassword`
- 修改配置后已经重启 PalServer

### 不能保存配置或修改 MOD

面板会阻止危险状态下的文件修改。常见原因：

- PalServer 正在运行。
- PalServer 是在面板外部启动的。
- 后台还有安装、更新、备份、恢复、MOD 操作正在执行。

先停止服务器并等待后台任务完成。

### 端口冲突

如果游戏端口、REST API 端口或 RCON 端口显示被占用：

- 服务器正在运行时，游戏端口被占用可能是正常的。
- 启动前端口已占用，说明有冲突，需要换端口或停止占用进程。
- Docker 部署时游戏端口要同时对齐 `.env` 和面板启动参数。

### Workshop 下载失败

优先尝试配置 Steam 账号。如果仍失败，从 Windows Steam 客户端找到 MOD 文件，按上面的 ManagedMods/Paks 结构打成 zip 上传。

## 安全提醒

- 面板账号密码只保护面板，不要把面板裸露到公网。
- 不要直接暴露 Palworld REST API 和 RCON。
- 备份、恢复、更新、MOD 都可能影响存档，操作前先备份。
- MOD 可能导致崩溃或坏档，只安装可信来源的服务端 MOD。
- `.env` 可能包含 Steam 密码，不要提交到 Git。
