<template>
  <n-config-provider>
    <n-message-provider>
      <main class="shell">
        <section v-if="loading" class="center-panel">
          <n-spin size="large" />
        </section>

        <section v-else-if="!authenticated" class="auth-panel">
          <div class="auth-copy">
            <h1>PalPanel Lite</h1>
            <p>单机单服自用面板，优先管理 Linux 原生 SteamCMD 专服。</p>
          </div>
          <n-card :title="setupRequired ? '初始化管理员' : '登录面板'" class="auth-card">
            <n-form label-placement="top" @submit.prevent="submitAuth">
              <n-form-item label="用户名">
                <n-input v-model:value="authForm.username" autocomplete="username" />
              </n-form-item>
              <n-form-item label="密码">
                <n-input
                  v-model:value="authForm.password"
                  type="password"
                  autocomplete="current-password"
                  show-password-on="click"
                />
              </n-form-item>
              <n-button type="primary" block :loading="busy" @click="submitAuth">
                {{ setupRequired ? '创建管理员' : '登录' }}
              </n-button>
            </n-form>
          </n-card>
        </section>

        <template v-else>
          <header class="topbar">
            <div>
              <h1>PalPanel Lite</h1>
              <p>Linux SteamCMD 单服管理</p>
            </div>
            <div class="topbar-actions">
              <n-button quaternary circle title="刷新状态" @click="loadDashboard">
                <template #icon><RefreshCw :size="18" /></template>
              </n-button>
              <n-button quaternary circle title="退出登录" @click="logout">
                <template #icon><LogOut :size="18" /></template>
              </n-button>
            </div>
          </header>

          <n-tabs v-model:value="activeTab" type="segment" animated class="workspace-tabs">
            <n-tab-pane name="server" tab="服务器" display-directive="show" class="workspace-tab-pane">
              <section class="grid">
            <n-card title="服务器状态">
              <div class="status-row">
                <span>系统</span>
                <strong>{{ status?.os ?? '-' }}</strong>
              </div>
              <div class="status-row">
                <span>PalServer</span>
                <n-tag :type="status?.pal_server_exists ? 'success' : 'warning'">
                  {{ status?.pal_server_exists ? '已检测到' : '未检测到' }}
                </n-tag>
              </div>
              <div class="status-row">
                <span>SteamCMD</span>
                <n-tag :type="status?.steamcmd_available ? 'success' : 'warning'">
                  {{ status?.steamcmd_available ? '可用' : '未检测到' }}
                </n-tag>
              </div>
              <div class="status-row">
                <span>运行状态</span>
                <n-tag :type="status?.running ? (status?.external_running ? 'warning' : 'success') : 'default'">
                  {{ serverRuntimeText }}
                </n-tag>
              </div>
              <n-alert v-if="status?.external_running" type="warning" class="status-alert">
                检测到外部 PalServer，更新或恢复前请先在面板外停止。
              </n-alert>
              <n-alert v-else-if="status?.runtime_warning" type="warning" class="status-alert">
                {{ status.runtime_warning }}
              </n-alert>
              <n-divider />
              <n-space class="action-grid">
                <n-button
                  secondary
                  type="primary"
                  :loading="actionBusy === 'install'"
                  :disabled="serverActionBlocked('install')"
                  @click="runServerAction('install')"
                >
                  <template #icon><Download :size="16" /></template>
                  安装
                </n-button>
                <n-popconfirm @positive-click="runServerAction('update')">
                  <template #trigger>
                    <n-button
                      secondary
                      :loading="actionBusy === 'update'"
                      :disabled="serverActionBlocked('update')"
                    >
                      <template #icon><RefreshCw :size="16" /></template>
                      更新
                    </n-button>
                  </template>
                  更新专服前会先创建备份，并可能覆盖服务器文件。
                </n-popconfirm>
                <n-button
                  type="primary"
                  :loading="actionBusy === 'start'"
                  :disabled="serverActionBlocked('start')"
                  @click="runServerAction('start')"
                >
                  <template #icon><Play :size="16" /></template>
                  启动
                </n-button>
                <n-popconfirm @positive-click="runServerAction('restart')">
                  <template #trigger>
                    <n-button
                      :loading="actionBusy === 'restart'"
                      :disabled="serverActionBlocked('restart')"
                    >
                      <template #icon><RotateCw :size="16" /></template>
                      重启
                    </n-button>
                  </template>
                  重启会停止当前服务器进程并重新启动。
                </n-popconfirm>
                <n-popconfirm @positive-click="runServerAction('stop')">
                  <template #trigger>
                    <n-button
                      type="warning"
                      :loading="actionBusy === 'stop'"
                      :disabled="serverActionBlocked('stop')"
                    >
                      <template #icon><Square :size="16" /></template>
                      停止
                    </n-button>
                  </template>
                  停止服务器可能中断在线玩家，请确认已经保存或备份。
                </n-popconfirm>
              </n-space>
            </n-card>

            <n-card title="基础设置">
              <n-form label-placement="top">
                <n-form-item label="PalServer 安装目录">
                  <n-input v-model:value="settings.pal_server_path" placeholder="/opt/palserver" />
                </n-form-item>
                <n-form-item label="SteamCMD 路径">
                  <n-input v-model:value="settings.steamcmd_path" placeholder="留空则从 PATH 检测" />
                </n-form-item>
                <n-form-item label="REST API 地址">
                  <n-input v-model:value="settings.rest_api_url" />
                </n-form-item>
                <n-form-item label="REST API 用户名">
                  <n-input v-model:value="settings.rest_api_username" />
                </n-form-item>
                <n-form-item label="REST API 密码">
                  <n-input
                    v-model:value="settings.rest_api_password"
                    type="password"
                    placeholder="留空则保留已保存密码"
                    show-password-on="click"
                  />
                </n-form-item>
                <div class="launch-grid">
                  <n-form-item label="游戏端口">
                    <n-input-number v-model:value="launchGamePort" :min="1" :max="65535" placeholder="8211" />
                  </n-form-item>
                  <n-form-item label="启动玩家数">
                    <n-input-number v-model:value="launchPlayers" :min="1" :max="128" placeholder="32" />
                  </n-form-item>
                  <n-form-item label="Worker Threads">
                    <n-input-number v-model:value="launchWorkerThreads" :min="1" :max="256" />
                  </n-form-item>
                  <div class="launch-switch">
                    <span>公开大厅</span>
                    <n-switch v-model:value="launchPublicLobby" />
                  </div>
                  <div class="launch-switch">
                    <span>性能参数</span>
                    <n-switch v-model:value="launchPerformanceFlags" />
                  </div>
                  <div class="launch-switch">
                    <span>禁用 MOD</span>
                    <n-switch v-model:value="launchNoMods" />
                  </div>
                </div>
                <n-form-item label="启动参数">
                  <n-input v-model:value="settings.server_launch_args" type="textarea" :autosize="{ minRows: 2 }" />
                </n-form-item>
                <div class="settings-inline">
                  <n-checkbox v-model:checked="settings.auto_backup_enabled">启用自动备份</n-checkbox>
                  <span>间隔小时</span>
                  <n-input-number v-model:value="settings.auto_backup_interval_hours" :min="1" :max="168" />
                  <span>保留数量</span>
                  <n-input-number v-model:value="settings.auto_backup_retention" :min="1" :max="200" />
                </div>
                <n-alert v-if="settingsSaveBlocked" type="warning" :show-icon="false" class="config-runtime-alert">
                  {{ settingsSaveBlockReason }}
                </n-alert>
                <n-button type="primary" :disabled="settingsSaveBlocked" :loading="busy" @click="saveSettings">
                  <template #icon><Save :size="16" /></template>
                  保存设置
                </n-button>
              </n-form>
            </n-card>
              </section>
            </n-tab-pane>

            <n-tab-pane name="dashboard" tab="仪表盘" display-directive="show" class="workspace-tab-pane">
          <n-card v-if="showSetupGuide" class="wide-card setup-guide">
            <template #header>
              <div class="card-header">
                <span>初始化向导</span>
                <n-tag size="small" :type="setupGuideReady ? 'success' : 'warning'">
                  {{ setupGuideReady ? '基础就绪' : '待完成' }}
                </n-tag>
              </div>
            </template>
            <div class="setup-list">
              <div v-for="step in setupSteps" :key="step.key" class="setup-step">
                <n-tag size="small" :type="step.done ? 'success' : step.optional ? 'default' : 'warning'">
                  {{ step.done ? '完成' : step.optional ? '可选' : '待处理' }}
                </n-tag>
                <div class="setup-step-main">
                  <strong>{{ step.title }}</strong>
                  <span>{{ step.detail }}</span>
                </div>
                <n-button
                  v-if="step.action"
                  size="small"
                  secondary
                  :type="step.done ? 'default' : 'primary'"
                  :loading="setupStepBusy === step.key"
                  :disabled="step.disabled || setupStepBusy !== ''"
                  @click="runSetupStep(step)"
                >
                  {{ step.action }}
                </n-button>
              </div>
            </div>
          </n-card>

          <n-card class="wide-card">
            <template #header>
              <div class="card-header">
                <span>仪表盘</span>
                <n-tag size="small" :type="palDashboard?.available ? 'success' : 'warning'">
                  {{ palDashboard?.available ? 'REST API 已连接' : 'REST API 未连接' }}
                </n-tag>
              </div>
            </template>
            <template #header-extra>
              <n-button size="small" :loading="palBusy" @click="loadPalDashboard(true)">
                <template #icon><RefreshCw :size="15" /></template>
                刷新
              </n-button>
            </template>

            <n-alert v-if="palDashboard?.error" type="warning" :show-icon="false" class="mod-warning">
              {{ palDashboard.error }}
            </n-alert>

            <div class="metric-grid">
              <div class="metric-item">
                <span>服务器名称</span>
                <strong>{{ infoText('servername', 'server_name', 'name') }}</strong>
              </div>
              <div class="metric-item">
                <span>版本</span>
                <strong>{{ infoText('version') }}</strong>
              </div>
              <div class="metric-item">
                <span>在线人数</span>
                <strong>{{ metricText('currentplayernum') }} / {{ metricText('maxplayernum') }}</strong>
              </div>
              <div class="metric-item">
                <span>Server FPS</span>
                <strong>{{ metricText('serverfps') }}</strong>
              </div>
              <div class="metric-item">
                <span>Frame Time</span>
                <strong>{{ metricText('serverframetime') }}</strong>
              </div>
              <div class="metric-item">
                <span>运行时间</span>
                <strong>{{ uptimeText }}</strong>
              </div>
              <div class="metric-item">
                <span>游戏天数</span>
                <strong>{{ metricText('days') }}</strong>
              </div>
              <div class="metric-item">
                <span>据点数量</span>
                <strong>{{ metricText('basecampnum') }}</strong>
              </div>
              <div class="metric-item">
                <span>REST 设置</span>
                <strong>{{ runtimeSettingText('RESTAPIEnabled', 'restAPIEnabled', 'rest_api_enabled') }}</strong>
              </div>
              <div class="metric-item">
                <span>REST 端口</span>
                <strong>{{ runtimeSettingText('RESTAPIPort', 'restAPIPort', 'rest_api_port') }}</strong>
              </div>
              <div class="metric-item">
                <span>RCON 设置</span>
                <strong>{{ runtimeSettingText('RCONEnabled', 'rconEnabled', 'rcon_enabled') }}</strong>
              </div>
              <div class="metric-item">
                <span>日志格式</span>
                <strong>{{ runtimeSettingText('LogFormatType', 'logFormatType', 'log_format_type') }}</strong>
              </div>
              <div class="metric-item">
                <span>本机 CPU</span>
                <strong>{{ systemCPUText }}</strong>
              </div>
              <div class="metric-item">
                <span>本机内存</span>
                <strong>{{ systemMemoryText }}</strong>
              </div>
              <div class="metric-item">
                <span>磁盘占用</span>
                <strong>{{ systemDiskText }}</strong>
              </div>
              <div class="metric-item">
                <span>最近备份</span>
                <strong>{{ recentBackupText }}</strong>
              </div>
            </div>

            <n-divider />
              <n-space class="dashboard-actions">
                <n-input v-model:value="announceMessage" placeholder="广播消息" class="announce-input" :maxlength="500" />
              <n-button :disabled="palRestActionBlocked || !announceMessage.trim()" :loading="palActionBusy === 'announce'" @click="announceServer">
                <template #icon><Megaphone :size="15" /></template>
                广播
              </n-button>
              <n-button type="primary" secondary :disabled="palRestActionBlocked" :loading="palActionBusy === 'save'" @click="saveWorld">
                <template #icon><Save :size="15" /></template>
                保存世界
              </n-button>
              <n-input-number v-model:value="shutdownWaitSeconds" :min="1" :max="3600" placeholder="等待秒数" class="wait-input" />
              <n-input v-model:value="shutdownMessage" placeholder="关服通知" class="announce-input" :maxlength="500" />
              <n-popconfirm @positive-click="shutdownServer">
                <template #trigger>
                  <n-button type="warning" secondary :disabled="palRestActionBlocked" :loading="palActionBusy === 'shutdown'">
                    <template #icon><Power :size="15" /></template>
                    优雅关服
                  </n-button>
                </template>
                通过 Palworld REST API 发送 shutdown 请求。
              </n-popconfirm>
              <n-popconfirm @positive-click="restStopServer">
                <template #trigger>
                  <n-button type="error" secondary :disabled="palRestActionBlocked" :loading="palActionBusy === 'rest-stop'">
                    <template #icon><PowerOff :size="15" /></template>
                    REST Stop
                  </n-button>
                </template>
                通过 Palworld REST API 立即发送 stop 请求。
              </n-popconfirm>
              <n-input v-model:value="unbanUserID" placeholder="解封 UserID" class="userid-input" :maxlength="128" />
              <n-popconfirm @positive-click="unbanPlayer">
                <template #trigger>
                  <n-button :disabled="palRestActionBlocked || !unbanUserID.trim()" :loading="palActionBusy === 'unban'">
                    解封
                  </n-button>
                </template>
                确认解封该 UserID？
              </n-popconfirm>
            </n-space>
          </n-card>
            </n-tab-pane>

            <n-tab-pane name="players" tab="玩家" display-directive="show" class="workspace-tab-pane">
          <n-card class="wide-card">
            <template #header>
              <div class="card-header">
                <span>在线玩家</span>
                <n-tag size="small">{{ players.length }} 人</n-tag>
              </div>
            </template>
            <n-empty v-if="players.length === 0" description="暂无在线玩家" />
            <div v-else class="player-list">
              <div v-for="player in players" :key="playerKey(player)" class="player-row">
                <div class="player-main">
                  <div class="player-title">
                    <strong>{{ playerName(player) }}</strong>
                    <n-tag size="small">{{ playerUserID(player) || '无 UserID' }}</n-tag>
                  </div>
                  <div class="player-meta">
                    <span v-if="playerText(player, 'playerid', 'playerId')">PlayerID: {{ playerText(player, 'playerid', 'playerId') }}</span>
                    <span v-if="playerText(player, 'steamid', 'steamId')">SteamID: {{ playerText(player, 'steamid', 'steamId') }}</span>
                    <span v-if="playerText(player, 'ping')">Ping: {{ playerText(player, 'ping') }}</span>
                  </div>
                </div>
                <n-space class="backup-actions">
                  <n-popconfirm @positive-click="actOnPlayer(player, 'kick')">
                    <template #trigger>
                      <n-button size="small" :disabled="palRestActionBlocked || !playerUserID(player)" :loading="palActionBusy === `kick:${playerUserID(player)}`">
                        踢出
                      </n-button>
                    </template>
                    确认踢出该玩家？
                  </n-popconfirm>
                  <n-popconfirm @positive-click="actOnPlayer(player, 'ban')">
                    <template #trigger>
                      <n-button size="small" type="error" secondary :disabled="palRestActionBlocked || !playerUserID(player)" :loading="palActionBusy === `ban:${playerUserID(player)}`">
                        封禁
                      </n-button>
                    </template>
                    确认封禁该玩家？
                  </n-popconfirm>
                </n-space>
              </div>
            </div>
          </n-card>
            </n-tab-pane>

            <n-tab-pane name="operations" tab="运维" display-directive="show" class="workspace-tab-pane">
          <n-card class="wide-card">
            <template #header>
              <div class="card-header">
                <span>最近运维</span>
                <n-tag size="small">{{ formatDateTime(palDashboard?.system.collected_at) }}</n-tag>
              </div>
            </template>
            <div class="ops-grid">
              <div class="ops-panel">
                <h3>服务器日志</h3>
                <n-empty v-if="recentDashboardLogs.length === 0" description="暂无日志" />
                <div v-else class="ops-list">
                  <div v-for="entry in recentDashboardLogs" :key="`${entry.time}:${entry.message}`" class="ops-row">
                    <span>{{ formatDateTime(entry.time) }}</span>
                    <strong>{{ entry.message }}</strong>
                  </div>
                </div>
              </div>
              <div class="ops-panel">
                <h3>任务</h3>
                <n-empty v-if="recentDashboardTasks.length === 0" description="暂无任务" />
                <div v-else class="ops-list">
                  <div v-for="task in recentDashboardTasks" :key="task.id" class="ops-row">
                    <span>{{ formatDateTime(task.created_at) }}</span>
                    <strong>{{ task.type }} · {{ task.status }}</strong>
                  </div>
                </div>
              </div>
              <div class="ops-panel">
                <h3>备份</h3>
                <n-empty v-if="recentDashboardBackups.length === 0" description="暂无备份" />
                <div v-else class="ops-list">
                  <div v-for="backup in recentDashboardBackups" :key="backup.id" class="ops-row">
                    <span>{{ formatDateTime(backup.created_at) }} · {{ formatBytes(backup.size) }}</span>
                    <strong>{{ backup.filename }}</strong>
                  </div>
                </div>
              </div>
            </div>
          </n-card>
            </n-tab-pane>

            <n-tab-pane name="diagnostics" tab="检测" display-directive="show" class="workspace-tab-pane">
          <n-card title="检测结果" class="wide-card">
            <n-descriptions bordered :column="1" size="small">
              <n-descriptions-item label="PalServer 二进制">
                {{ status?.pal_server_binary || '未配置' }}
              </n-descriptions-item>
              <n-descriptions-item label="SteamCMD">
                {{ status?.steamcmd_path || '未检测到' }}
              </n-descriptions-item>
            </n-descriptions>
            <n-divider v-if="portChecks.length > 0" />
            <div v-if="portChecks.length > 0" class="port-check-list">
              <div v-for="check in portChecks" :key="`${check.name}:${check.protocol}`" class="port-check-row">
                <div class="port-check-main">
                  <strong>{{ check.name }} · {{ check.protocol.toUpperCase() }} {{ check.port || '-' }}</strong>
                  <span>{{ portSourceText(check.source) }}<template v-if="check.message"> · {{ check.message }}</template></span>
                </div>
                <n-tag size="small" :type="portStatusType(check.status)">
                  {{ portStatusText(check.status) }}
                </n-tag>
              </div>
            </div>
            <p v-if="status?.running && portChecks.some((check) => check.status === 'in_use')" class="hint-text">
              服务器运行中时端口占用可能是正常状态；启动前占用通常表示存在冲突。
            </p>
          </n-card>
            </n-tab-pane>

            <n-tab-pane name="config" tab="配置" display-directive="show" class="workspace-tab-pane">
          <n-card class="wide-card">
            <template #header>
              <div class="card-header">
                <span>配置文件</span>
                <n-tag v-if="config" size="small" type="info">{{ config.platform }}</n-tag>
              </div>
            </template>
            <template #header-extra>
              <n-space>
                <n-button size="small" :disabled="configBusy" :loading="configBusy" @click="loadConfig">
                  <template #icon><RefreshCw :size="15" /></template>
                  刷新
                </n-button>
                <n-button v-if="!config" size="small" type="primary" :disabled="configInitBlocked" :loading="configBusy" @click="initConfig">
                  <template #icon><Save :size="15" /></template>
                  初始化
                </n-button>
                <n-button size="small" :disabled="!config || configSaveBlocked" :loading="configBusy" @click="backupConfig">
                  <template #icon><Archive :size="15" /></template>
                  备份
                </n-button>
                <n-button size="small" type="primary" :disabled="!config || configSaveBlocked" :loading="configBusy" @click="saveConfig">
                  <template #icon><Save :size="15" /></template>
                  保存
                </n-button>
              </n-space>
            </template>

            <n-alert v-if="configError" :type="configNotInitialized ? 'info' : 'warning'" :show-icon="false">
              {{ configError }}
            </n-alert>

            <template v-else-if="config">
              <n-alert v-if="configRuntimeNotice" :type="configSaveBlocked ? 'warning' : 'info'" :show-icon="false" class="config-runtime-alert">
                {{ configRuntimeNotice }}
              </n-alert>

              <n-descriptions bordered :column="1" size="small" class="config-paths">
                <n-descriptions-item label="PalWorldSettings.ini">
                  {{ config.config_path }}
                </n-descriptions-item>
                <n-descriptions-item v-if="config.backup_path" label="最近备份">
                  {{ config.backup_path }}
                </n-descriptions-item>
              </n-descriptions>

              <n-tabs type="line" animated>
                <n-tab-pane name="basic" tab="基础">
                  <div class="config-grid">
                    <n-form-item label="服务器名称">
                      <n-input v-model:value="configForm.server_name" />
                    </n-form-item>
                    <n-form-item label="最大玩家数">
                      <n-input-number v-model:value="configForm.server_player_max_num" :min="1" :max="64" />
                    </n-form-item>
                    <n-form-item label="管理员密码">
                      <n-input v-model:value="configForm.admin_password" type="password" show-password-on="click" />
                    </n-form-item>
                    <n-form-item label="服务器密码">
                      <n-input v-model:value="configForm.server_password" type="password" show-password-on="click" />
                    </n-form-item>
                    <n-form-item label="服务器描述" class="span-2">
                      <n-input v-model:value="configForm.server_description" type="textarea" :autosize="{ minRows: 2 }" />
                    </n-form-item>
                  </div>
                </n-tab-pane>

                <n-tab-pane name="rates" tab="倍率">
                  <div class="config-grid">
                    <n-form-item label="经验倍率">
                      <n-input-number v-model:value="configForm.exp_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="捕获倍率">
                      <n-input-number v-model:value="configForm.pal_capture_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="帕鲁刷新倍率">
                      <n-input-number v-model:value="configForm.pal_spawn_num_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="敌人掉落倍率">
                      <n-input-number v-model:value="configForm.enemy_drop_item_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="采集掉落倍率">
                      <n-input-number v-model:value="configForm.collection_drop_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="采集物生命倍率">
                      <n-input-number v-model:value="configForm.collection_object_hp_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="采集物刷新倍率">
                      <n-input-number v-model:value="configForm.collection_object_respawn_speed_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                  </div>
                </n-tab-pane>

                <n-tab-pane name="world" tab="世界">
                  <div class="config-grid">
                    <n-form-item label="难度">
                      <n-select v-model:value="configForm.difficulty" :options="difficultyOptions" />
                    </n-form-item>
                    <n-form-item label="死亡惩罚">
                      <n-select v-model:value="configForm.death_penalty" :options="deathPenaltyOptions" />
                    </n-form-item>
                    <n-form-item label="白天速度">
                      <n-input-number v-model:value="configForm.day_time_speed_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="夜晚速度">
                      <n-input-number v-model:value="configForm.night_time_speed_rate" :min="0.1" :step="0.1" />
                    </n-form-item>
                    <n-form-item label="孵蛋时间">
                      <n-input-number v-model:value="configForm.egg_default_hatching_time" :min="0" :step="1" />
                    </n-form-item>
                    <n-form-item label="据点总数">
                      <n-input-number v-model:value="configForm.base_camp_max_num" :min="1" :step="1" />
                    </n-form-item>
                    <n-form-item label="公会据点上限">
                      <n-input-number v-model:value="configForm.base_camp_max_num_in_guild" :min="1" :max="10" :step="1" />
                    </n-form-item>
                    <n-form-item label="据点工作帕鲁数">
                      <n-input-number v-model:value="configForm.base_camp_worker_max_num" :min="1" :max="50" :step="1" />
                    </n-form-item>
                    <n-form-item label="公会人数">
                      <n-input-number v-model:value="configForm.guild_player_max_num" :min="1" :step="1" />
                    </n-form-item>
                    <n-form-item label="建筑数量上限">
                      <n-input-number v-model:value="configForm.max_building_limit_num" :min="0" :step="100" />
                    </n-form-item>
                    <n-form-item label="帕鲁同步距离">
                      <n-input-number v-model:value="configForm.server_replicate_pawn_cull_distance" :min="5000" :max="15000" :step="500" />
                    </n-form-item>
                    <n-form-item label="建筑退化倍率">
                      <n-input-number v-model:value="configForm.build_object_deterioration_damage_rate" :min="0" :step="0.1" />
                    </n-form-item>
                  </div>
                </n-tab-pane>

                <n-tab-pane name="network" tab="网络">
                  <div class="config-grid">
                    <n-form-item label="Public IP">
                      <n-input v-model:value="configForm.public_ip" />
                    </n-form-item>
                    <n-form-item label="Public Port">
                      <n-input-number v-model:value="configForm.public_port" :min="1" :max="65535" />
                    </n-form-item>
                    <n-form-item label="REST API Port">
                      <n-input-number v-model:value="configForm.rest_api_port" :min="1" :max="65535" />
                    </n-form-item>
                    <n-form-item label="RCON Port">
                      <n-input-number v-model:value="configForm.rcon_port" :min="1" :max="65535" />
                    </n-form-item>
                  </div>
                </n-tab-pane>

                <n-tab-pane name="advanced" tab="高级">
                  <div class="config-grid">
                    <div class="switch-row">
                      <span>REST API</span>
                      <n-switch v-model:value="configForm.rest_api_enabled" />
                    </div>
                    <div class="switch-row">
                      <span>RCON</span>
                      <n-switch v-model:value="configForm.rcon_enabled" />
                    </div>
                    <div class="switch-row">
                      <span>官方自动备份</span>
                      <n-switch v-model:value="configForm.is_use_backup_save_data" />
                    </div>
                    <div class="switch-row">
                      <span>允许客户端 MOD</span>
                      <n-switch v-model:value="configForm.allow_client_mod" />
                    </div>
                    <div class="switch-row">
                      <span>PvP</span>
                      <n-switch v-model:value="configForm.is_pvp" />
                    </div>
                    <div class="switch-row">
                      <span>玩家互伤</span>
                      <n-switch v-model:value="configForm.enable_player_to_player_damage" />
                    </div>
                    <div class="switch-row">
                      <span>据点防御其他公会玩家</span>
                      <n-switch v-model:value="configForm.enable_defense_other_guild_player" />
                    </div>
                    <n-form-item label="日志格式">
                      <n-select v-model:value="configForm.log_format_type" :options="logFormatOptions" />
                    </n-form-item>
                    <n-form-item label="跨平台连接">
                      <n-input v-model:value="configForm.crossplay_platforms" />
                    </n-form-item>
                  </div>
                </n-tab-pane>
              </n-tabs>
            </template>
          </n-card>
            </n-tab-pane>

            <n-tab-pane name="backups" tab="备份" display-directive="show" class="workspace-tab-pane">
          <n-card class="wide-card">
            <template #header>
              <div class="card-header">
                <span>备份恢复</span>
                <n-tag size="small">{{ backups.length }} 个备份</n-tag>
              </div>
            </template>
            <template #header-extra>
              <n-space>
                <n-button size="small" :loading="backupBusy" :disabled="backupBusy || backupActionBusy !== ''" @click="loadBackups">
                  <template #icon><RefreshCw :size="15" /></template>
                  刷新
                </n-button>
                <n-button size="small" type="primary" :disabled="backupCreateBlocked" :loading="backupBusy" @click="createManualBackup">
                  <template #icon><Archive :size="15" /></template>
                  手动备份
                </n-button>
              </n-space>
            </template>

            <n-alert v-if="backupRestoreBlocked" type="warning" :show-icon="false" class="config-runtime-alert">
              {{ backupRestoreBlockReason }}
            </n-alert>

            <n-empty v-if="backups.length === 0" description="暂无备份" />
            <div v-else class="backup-list">
              <div v-for="backup in backups" :key="backup.id" class="backup-row">
                <div class="backup-main">
                  <div class="backup-title">
                    <strong>{{ backup.filename }}</strong>
                    <n-tag size="small" :type="backup.type === 'manual' ? 'success' : 'info'">{{ backup.type }}</n-tag>
                  </div>
                  <div class="backup-meta">
                    <span>{{ formatBytes(backup.size) }}</span>
                    <span>{{ backup.created_at }}</span>
                  </div>
                  <div class="backup-path">{{ backup.path || '备份路径无效' }}</div>
                  <div v-if="backup.note" class="backup-note">{{ backup.note }}</div>
                </div>
                <n-space class="backup-actions">
                  <n-popconfirm @positive-click="restoreBackup(backup.id)">
                    <template #trigger>
                      <n-button size="small" :loading="backupActionBusy === `restore:${backup.id}`" :disabled="backupRestoreBlocked || !backup.path">
                        <template #icon><RotateCcw :size="15" /></template>
                        恢复
                      </n-button>
                    </template>
                    恢复会覆盖当前服务器目录，执行前会先创建保护性备份。
                  </n-popconfirm>
                  <n-popconfirm @positive-click="deleteBackup(backup.id)">
                    <template #trigger>
                      <n-button size="small" type="error" secondary :loading="backupActionBusy === `delete:${backup.id}`" :disabled="backupDeleteBlocked || !backup.path">
                        <template #icon><Trash2 :size="15" /></template>
                        删除
                      </n-button>
                    </template>
                    删除后无法从面板恢复这个备份文件。
                  </n-popconfirm>
                </n-space>
              </div>
            </div>
          </n-card>
            </n-tab-pane>

            <n-tab-pane name="mods" tab="MOD" display-directive="show" class="workspace-tab-pane">
          <n-card class="wide-card">
            <template #header>
              <div class="card-header">
                <span>MOD 管理</span>
                <n-tag size="small">{{ mods.length }} 个 MOD</n-tag>
              </div>
            </template>
            <template #header-extra>
              <n-space>
                <input ref="modFileInput" class="hidden-file" type="file" accept=".zip,.7z,.rar" @change="uploadSelectedMod" />
                <input ref="modUpdateFileInput" class="hidden-file" type="file" accept=".zip,.7z,.rar" @change="uploadSelectedModUpdate" />
                <n-button size="small" :loading="modBusy" :disabled="modBusy || modActionBusy !== ''" @click="loadMods">
                  <template #icon><RefreshCw :size="15" /></template>
                  刷新
                </n-button>
                <n-button size="small" type="primary" :loading="modBusy" :disabled="modMutationActionBlocked" @click="openModUpload">
                  <template #icon><Upload :size="15" /></template>
                  上传 MOD
                </n-button>
              </n-space>
            </template>

            <n-alert type="info" :show-icon="false" class="mod-warning">
              MOD 变更会写入服务端文件；请先停止 PalServer，完成后再启动或重启生效。
            </n-alert>
            <n-alert v-if="modMutationBlocked" type="warning" :show-icon="false" class="mod-warning">
              {{ modMutationBlockReason }}
            </n-alert>
            <n-alert v-if="modChangesNeedRestart" type="success" :show-icon="false" class="mod-warning">
              MOD 变更已保存，下一次启动或重启 PalServer 后生效。
            </n-alert>
            <n-alert v-if="status?.os !== 'windows'" type="warning" :show-icon="false" class="mod-warning">
              当前平台的服务端 MOD 管理按实验性文件管理处理；官方完整服务端 MOD 支持目前面向 Windows Dedicated Server。
            </n-alert>

            <n-empty v-if="mods.length === 0" description="暂无 MOD" />
            <div v-else class="mod-list">
              <div v-for="mod in mods" :key="mod.id" class="mod-row">
                <div class="mod-main">
                  <div class="mod-title">
                    <strong>{{ mod.name || mod.package_name }}</strong>
                    <n-tag size="small" :type="mod.enabled ? 'success' : 'default'">{{ mod.enabled ? '已启用' : '未启用' }}</n-tag>
                    <n-tag size="small" :type="mod.server_supported ? 'success' : 'warning'">
                      {{ mod.server_supported ? '服务端' : '未标记服务端' }}
                    </n-tag>
                  </div>
                  <div class="mod-meta">
                    <span>{{ mod.package_name }}</span>
                    <span v-if="mod.version">v{{ mod.version }}</span>
                    <span v-if="mod.author">{{ mod.author }}</span>
                  </div>
                  <div class="backup-path">{{ mod.install_path || mod.folder_name }}</div>
                </div>
                <n-space class="backup-actions">
                  <n-button
                    v-if="!mod.enabled"
                    size="small"
                    type="primary"
                    secondary
                    :loading="modActionBusy === `enable:${mod.id}`"
                    :disabled="modMutationActionBlocked"
                    @click="setModEnabled(mod.id, true)"
                  >
                    <template #icon><Power :size="15" /></template>
                    启用
                  </n-button>
                  <n-button
                    v-else
                    size="small"
                    :loading="modActionBusy === `disable:${mod.id}`"
                    :disabled="modMutationActionBlocked"
                    @click="setModEnabled(mod.id, false)"
                  >
                    <template #icon><PowerOff :size="15" /></template>
                    禁用
                  </n-button>
                  <n-button size="small" @click="showModInfo(mod)">
                    <template #icon><FileText :size="15" /></template>
                    Info.json
                  </n-button>
                  <n-button size="small" :loading="modActionBusy === `open:${mod.id}`" :disabled="modDirectoryActionBlocked" @click="openModDirectory(mod)">
                    <template #icon><FolderOpen :size="15" /></template>
                    目录
                  </n-button>
                  <n-popconfirm @positive-click="openModUpdate(mod)">
                    <template #trigger>
                      <n-button size="small" :loading="modActionBusy === `update:${mod.id}`" :disabled="modMutationActionBlocked">
                        <template #icon><Upload :size="15" /></template>
                        更新
                      </n-button>
                    </template>
                    更新会先创建备份，并要求上传包的 PackageName 与当前 MOD 一致。
                  </n-popconfirm>
                  <n-popconfirm @positive-click="deleteMod(mod.id)">
                    <template #trigger>
                      <n-button size="small" type="error" secondary :loading="modActionBusy === `delete:${mod.id}`" :disabled="modMutationActionBlocked">
                        <template #icon><Trash2 :size="15" /></template>
                        删除
                      </n-button>
                    </template>
                    删除前会先创建备份，并从 ActiveModList 移除该 MOD。
                  </n-popconfirm>
                </n-space>
              </div>
            </div>
          </n-card>

            </n-tab-pane>

            <n-tab-pane name="logs" tab="日志" display-directive="show" class="workspace-tab-pane">
          <section class="grid log-grid">
            <n-card>
              <template #header>
                <div class="card-header">
                  <span>任务日志</span>
                  <n-tag v-if="latestTask" size="small" :type="latestTask.status === 'running' ? 'info' : latestTask.status === 'success' ? 'success' : 'error'">
                    {{ latestTask.type }} · {{ latestTask.status }}
                  </n-tag>
                </div>
              </template>
              <pre class="log-box">{{ latestTaskLog }}</pre>
            </n-card>

            <n-card title="服务器日志">
              <pre class="log-box">{{ serverLogText }}</pre>
            </n-card>
          </section>
            </n-tab-pane>
          </n-tabs>
        </template>
      </main>
      <n-modal v-model:show="modInfoVisible">
        <n-card class="info-modal" :title="selectedMod?.package_name || 'Info.json'">
          <pre class="log-box modal-log">{{ selectedMod?.info_json }}</pre>
        </n-card>
      </n-modal>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Archive, Download, FileText, FolderOpen, LogOut, Megaphone, Play, Power, PowerOff, RefreshCw, RotateCcw, RotateCw, Save, Square, Trash2, Upload } from 'lucide-vue-next'
import { createDiscreteApi } from 'naive-ui'
import { ApiError, apiDelete, apiGet, apiPost, apiPut, apiUpload, type AuthState, type BackupRecord, type ModRecord, type OpenDirectoryResponse, type PalDashboard, type PalConfigPayload, type PalConfigValues, type PalPlayer, type RestoreResponse, type RuntimeEvent, type ServerLogs, type ServerStatus, type Settings, type SystemSummary, type TaskRecord } from './api'

const { message } = createDiscreteApi(['message'])

const loading = ref(true)
const busy = ref(false)
const setupRequired = ref(false)
const authenticated = ref(false)
const activeTab = ref('server')
const realtimeConnected = ref(false)
const status = ref<ServerStatus | null>(null)
const tasks = ref<TaskRecord[]>([])
const serverLogs = ref<ServerLogs['logs']>([])
const palDashboard = ref<PalDashboard | null>(null)
const palBusy = ref(false)
const palActionBusy = ref('')
const announceMessage = ref('')
const shutdownMessage = ref('')
const shutdownWaitSeconds = ref(30)
const unbanUserID = ref('')
const actionBusy = ref<ServerAction | null>(null)
const setupStepBusy = ref('')
const config = ref<PalConfigPayload | null>(null)
const configBusy = ref(false)
const configError = ref('')
const configNotInitialized = ref(false)
const backups = ref<BackupRecord[]>([])
const backupBusy = ref(false)
const backupActionBusy = ref('')
const mods = ref<ModRecord[]>([])
const modBusy = ref(false)
const modActionBusy = ref('')
const modFileInput = ref<HTMLInputElement | null>(null)
const modUpdateFileInput = ref<HTMLInputElement | null>(null)
const modUpdateTarget = ref<ModRecord | null>(null)
const modChangesNeedRestart = ref(false)
const selectedMod = ref<ModRecord | null>(null)
const modInfoVisible = ref(false)
const settings = reactive<Settings>({
  pal_server_path: '',
  steamcmd_path: '',
  rest_api_url: 'http://127.0.0.1:8212/v1/api',
  rest_api_username: 'admin',
  rest_api_password: '',
  server_launch_args: '-useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS',
  game_port: undefined,
  launch_players: undefined,
  public_lobby: false,
  no_mods: false,
  performance_flags: true,
  worker_threads: undefined,
  auto_backup_enabled: true,
  auto_backup_retention: 20,
  auto_backup_interval_hours: 24
})
const authForm = reactive({
  username: 'admin',
  password: ''
})
const configForm = reactive<PalConfigValues>(defaultConfigValues())
let pollTimer: number | undefined
let realtimeSocket: WebSocket | undefined
let reconnectTimer: number | undefined

type ServerAction = 'install' | 'update' | 'start' | 'stop' | 'restart'
const confirmedServerActions = new Set<ServerAction>(['update', 'stop', 'restart'])
const configNotInitializedMessage = '配置文件尚未初始化。点击初始化后，面板会从默认配置创建 PalWorldSettings.ini。'
type SetupStepAction = 'save-settings' | 'install' | 'init-config' | 'create-backup' | 'refresh-rest'
interface SetupStep {
  key: string
  title: string
  detail: string
  done: boolean
  optional?: boolean
  action?: string
  actionKey?: SetupStepAction
  disabled?: boolean
}

const actionLabels: Record<ServerAction, string> = {
  install: '安装任务已启动',
  update: '更新任务已启动',
  start: '服务器已启动',
  stop: '服务器已停止',
  restart: '服务器已重启'
}
const performanceLaunchFlags = ['-useperfthreads', '-NoAsyncLoadingThread', '-UseMultithreadForDS']

const latestTask = computed(() => tasks.value[0] ?? null)
const activeTask = computed(() => Boolean(status.value?.operation_running) || tasks.value.some((task) => task.status === 'running'))
const settingsPathRetargetBlocked = computed(() => {
  if (!status.value?.running || !status.value?.pal_server_path) return false
  return !samePathDraft(settings.pal_server_path, status.value.pal_server_path)
})
const settingsSaveBlocked = computed(() => activeTask.value || settingsPathRetargetBlocked.value)
const settingsSaveBlockReason = computed(() => {
  if (activeTask.value) return '后台操作正在运行，请等待操作完成后再保存设置。'
  if (settingsPathRetargetBlocked.value) return 'PalServer 正在运行，不能切换 PalServer 安装目录。'
  return ''
})
const backupMutationBlocked = computed(() => activeTask.value)
const backupMutationBlockReason = computed(() => {
  if (activeTask.value) return '后台操作正在运行，请等待操作完成后再创建、恢复或删除备份。'
  return ''
})
const backupActionInFlight = computed(() => backupActionBusy.value !== '')
const backupCreateBlocked = computed(() => backupMutationBlocked.value || backupBusy.value || backupActionInFlight.value)
const backupCreateBlockReason = computed(() => {
  if (backupMutationBlocked.value) return backupMutationBlockReason.value
  if (backupActionInFlight.value) return '已有备份恢复或删除操作正在执行，请稍后再试。'
  if (backupBusy.value) return '备份列表正在刷新或手动备份正在执行，请稍后再试。'
  return ''
})
const backupRestoreBlocked = computed(() => backupMutationBlocked.value || backupActionInFlight.value || Boolean(status.value?.external_running))
const backupRestoreBlockReason = computed(() => {
  if (backupMutationBlocked.value) return backupMutationBlockReason.value
  if (backupActionInFlight.value) return '已有备份恢复或删除操作正在执行，请稍后再试。'
  if (status.value?.external_running) return '检测到 PalServer 由面板外部运行，请先在外部停止服务器再恢复备份。'
  return ''
})
const backupDeleteBlocked = computed(() => backupMutationBlocked.value || backupActionInFlight.value)
const backupDeleteBlockReason = computed(() => {
  if (backupMutationBlocked.value) return backupMutationBlockReason.value
  if (backupActionInFlight.value) return '已有备份恢复或删除操作正在执行，请稍后再试。'
  return ''
})
const palRestActionBlocked = computed(() => (
  activeTask.value ||
  palBusy.value ||
  palActionBusy.value !== '' ||
  !status.value?.running ||
  !palDashboard.value?.available
))
const palRestActionBlockReason = computed(() => {
  if (activeTask.value) return '后台操作正在运行，请等待操作完成后再执行 Palworld REST 操作。'
  if (palActionBusy.value !== '') return '已有 Palworld REST 操作正在执行，请稍后再试。'
  if (palBusy.value) return 'Palworld REST 状态正在刷新，请稍后再试。'
  if (!status.value?.running) return 'PalServer 未运行，无法发送 Palworld REST 操作。'
  if (!palDashboard.value?.available) return 'Palworld REST API 未连接，无法发送操作。'
  return ''
})
const latestTaskLog = computed(() => latestTask.value?.log.trim() || '暂无任务日志')
const serverRuntimeText = computed(() => {
  if (status.value?.managed_running) return '面板启动'
  if (status.value?.external_running) return '外部运行'
  if (status.value?.running) return '运行中'
  return '未运行'
})
const portChecks = computed(() => status.value?.port_checks ?? [])
const setupSteps = computed<SetupStep[]>(() => {
  const serverPathSaved = Boolean(status.value?.configured)
  const serverPathDraft = settings.pal_server_path.trim() !== ''
  const steamReady = Boolean(status.value?.steamcmd_available)
  const palServerReady = Boolean(status.value?.pal_server_exists)
  const configReady = Boolean(config.value?.exists)
  const backupReady = settings.auto_backup_enabled && settings.auto_backup_retention > 0 && settings.auto_backup_interval_hours > 0
  const restReady = Boolean(palDashboard.value?.available)
  return [
    {
      key: 'paths',
      title: '保存基础路径',
      detail: serverPathSaved
        ? `PalServer 目录已保存：${status.value?.pal_server_path || settings.pal_server_path}`
        : serverPathDraft
          ? '已填写 PalServer 目录，保存后才能安装、检测和编辑配置。'
          : '填写 PalServer 安装目录；SteamCMD 路径可留空从 PATH 检测。',
      done: serverPathSaved,
      action: serverPathSaved ? undefined : '保存设置',
      actionKey: 'save-settings',
      disabled: !serverPathDraft || busy.value || settingsSaveBlocked.value
    },
    {
      key: 'steamcmd',
      title: '确认 SteamCMD',
      detail: steamReady ? `SteamCMD 可用：${status.value?.steamcmd_path}` : '安装或填写 SteamCMD 路径，面板才能一键安装/更新专服。',
      done: steamReady,
      action: steamReady ? undefined : '刷新检测',
      actionKey: 'save-settings',
      disabled: busy.value || settingsSaveBlocked.value
    },
    {
      key: 'palserver',
      title: '安装或检测 PalServer',
      detail: palServerReady ? `已检测到：${status.value?.pal_server_binary}` : '保存路径并确认 SteamCMD 后，可以从面板安装 Palworld Dedicated Server。',
      done: palServerReady,
      action: palServerReady ? undefined : '安装专服',
      actionKey: 'install',
      disabled: !serverPathSaved || !steamReady || serverActionBlocked('install')
    },
    {
      key: 'config',
      title: '初始化配置文件',
      detail: configReady
        ? `配置文件可用：${config.value?.config_path}`
        : status.value?.external_running
          ? '检测到 PalServer 由面板外部运行，请先在外部停止服务器再初始化配置。'
          : (configNotInitialized.value ? configNotInitializedMessage : (configError.value || configNotInitializedMessage)),
      done: configReady,
      action: configReady ? undefined : '初始化配置',
      actionKey: 'init-config',
      disabled: !serverPathSaved || configBusy.value || configInitBlocked.value
    },
    {
      key: 'backup',
      title: '确认备份策略',
      detail: backupReady
        ? `自动备份已启用：每 ${settings.auto_backup_interval_hours} 小时，保留 ${settings.auto_backup_retention} 个非手动备份。`
        : '建议启用自动备份并设置保留数量，手动备份不会被自动清理。',
      done: backupReady,
      action: backupReady ? '创建手动备份' : '保存备份设置',
      actionKey: backupReady ? 'create-backup' : 'save-settings',
      disabled: backupReady ? backupCreateBlocked.value || !serverPathSaved : busy.value || settingsSaveBlocked.value
    },
    {
      key: 'rest',
      title: '连接 Palworld REST API',
      detail: restReady ? 'REST API 已连接，仪表盘和玩家管理可用。' : '可选：在配置文件启用 RESTAPIEnabled 并设置后台连接地址/账号密码。',
      done: restReady,
      optional: true,
      action: '刷新 REST',
      actionKey: 'refresh-rest',
      disabled: palBusy.value
    }
  ]
})
const setupGuideReady = computed(() => setupSteps.value.filter((step) => !step.optional).every((step) => step.done))
const showSetupGuide = computed(() => !setupGuideReady.value)
const configInitBlocked = computed(() => activeTask.value || Boolean(status.value?.external_running))
const configInitBlockReason = computed(() => {
  if (!configInitBlocked.value) return ''
  if (activeTask.value) return '后台操作正在运行，请等待操作完成后再初始化配置。'
  if (status.value?.external_running) return '检测到 PalServer 由面板外部运行，请先在外部停止服务器再初始化配置。'
  return ''
})
const configSaveBlocked = computed(() => Boolean(status.value?.external_running) || activeTask.value)
const configRuntimeNotice = computed(() => {
  if (status.value?.external_running) return '检测到 PalServer 由面板外部运行，请先在外部停止服务器再初始化、备份或保存配置。'
  if (activeTask.value) return '后台操作正在运行，请等待操作完成后再初始化、备份或保存配置。'
  if (status.value?.managed_running) return 'PalServer 正在由面板运行；保存配置后需要重启才会生效。'
  if (config.value?.needs_restart) return '配置已保存，重启后生效。'
  return ''
})
const modMutationBlocked = computed(() => activeTask.value || Boolean(status.value?.running))
const modMutationBlockReason = computed(() => {
  if (activeTask.value) return '后台操作正在运行，请等待操作完成后再修改 MOD。'
  if (status.value?.external_running) return '检测到 PalServer 由面板外部运行，请先在外部停止服务器再修改 MOD。'
  if (status.value?.managed_running) return 'PalServer 正在由面板运行，请先停止服务器再修改 MOD。'
  if (status.value?.running) return 'PalServer 正在运行，请先停止服务器再修改 MOD。'
  return ''
})
const modActionInFlight = computed(() => modActionBusy.value !== '')
const modMutationActionBlocked = computed(() => modMutationBlocked.value || modBusy.value || modActionInFlight.value)
const modMutationActionBlockReason = computed(() => {
  if (modActionInFlight.value) return '已有 MOD 操作正在执行，请稍后再试。'
  if (modBusy.value) return 'MOD 列表正在刷新或上传，请稍后再试。'
  if (modMutationBlocked.value) return modMutationBlockReason.value
  return ''
})
const modDirectoryActionBlocked = computed(() => modActionInFlight.value)
const modDirectoryActionBlockReason = computed(() => {
  if (modActionInFlight.value) return '已有 MOD 操作正在执行，请稍后再试。'
  return ''
})
const launchGamePort = computed<number | null>({
  get: () => getLaunchNumber('-port'),
  set: (value) => setLaunchNumber('-port', value)
})
const launchPlayers = computed<number | null>({
  get: () => getLaunchNumber('-players'),
  set: (value) => setLaunchNumber('-players', value)
})
const launchWorkerThreads = computed<number | null>({
  get: () => getLaunchNumber('-NumberOfWorkerThreadsServer'),
  set: (value) => setLaunchNumber('-NumberOfWorkerThreadsServer', value)
})
const launchPublicLobby = computed<boolean>({
  get: () => hasLaunchFlag('-publiclobby'),
  set: (enabled) => setLaunchFlag('-publiclobby', enabled)
})
const launchNoMods = computed<boolean>({
  get: () => hasLaunchFlag('-NoMods'),
  set: (enabled) => setLaunchFlag('-NoMods', enabled)
})
const launchPerformanceFlags = computed<boolean>({
  get: () => performanceLaunchFlags.every((flag) => hasLaunchFlag(flag)),
  set: (enabled) => {
    for (const flag of performanceLaunchFlags) {
      setLaunchFlag(flag, enabled)
    }
  }
})
const serverLogText = computed(() => {
  if (serverLogs.value.length === 0) return '暂无服务器日志'
  return serverLogs.value.map((entry) => `[${entry.time}] ${entry.message}`).join('\n')
})
const players = computed(() => palDashboard.value?.players ?? [])
const recentDashboardLogs = computed(() => palDashboard.value?.recent_logs ?? [])
const recentDashboardTasks = computed(() => palDashboard.value?.recent_tasks ?? [])
const recentDashboardBackups = computed(() => palDashboard.value?.recent_backups ?? [])
const uptimeText = computed(() => {
  const value = metricNumber('uptime')
  if (value === null) return '-'
  const hours = Math.floor(value / 3600)
  const minutes = Math.floor((value % 3600) / 60)
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
})
const systemCPUText = computed(() => {
  const cpu = palDashboard.value?.system.cpu
  if (!cpu) return '-'
  const percent = percentText(cpu.usage_percent)
  return percent !== '-' ? percent : `${cpu.cores} cores`
})
const systemMemoryText = computed(() => {
  const memory = palDashboard.value?.system.memory
  if (!memory || memory.total_bytes === 0) return '-'
  return `${percentText(memory.used_percent)} · ${formatBytes(memory.used_bytes)} / ${formatBytes(memory.total_bytes)}`
})
const systemDiskText = computed(() => {
  const disk = palDashboard.value?.system.disk
  if (!disk || disk.total_bytes === 0) return '-'
  return `${percentText(disk.used_percent)} · ${formatBytes(disk.free_bytes)} 可用`
})
const recentBackupText = computed(() => {
  const backup = recentDashboardBackups.value[0]
  if (!backup) return '-'
  return `${backup.type} · ${formatBytes(backup.size)}`
})
const difficultyOptions = [
  { label: 'None', value: 'None' },
  { label: 'Normal', value: 'Normal' },
  { label: 'Hard', value: 'Hard' }
]
const deathPenaltyOptions = [
  { label: 'None', value: 'None' },
  { label: 'Item', value: 'Item' },
  { label: 'ItemAndEquipment', value: 'ItemAndEquipment' },
  { label: 'All', value: 'All' }
]
const logFormatOptions = [
  { label: 'Text', value: 'Text' },
  { label: 'JSON', value: 'JSON' }
]

onMounted(async () => {
  await bootstrap()
})

onBeforeUnmount(() => {
  stopRealtime()
  stopPolling()
})

async function bootstrap() {
  loading.value = true
  try {
    const state = await apiGet<AuthState>('/api/auth/state')
    setupRequired.value = state.setup_required
    if (!state.setup_required) {
      await apiGet('/api/auth/me')
      authenticated.value = true
      await loadDashboard()
      startRealtime()
      startPolling()
    }
  } catch {
    authenticated.value = false
    stopRealtime()
    stopPolling()
  } finally {
    loading.value = false
  }
}

async function submitAuth() {
  busy.value = true
  try {
    const endpoint = setupRequired.value ? '/api/auth/setup' : '/api/auth/login'
    await apiPost(endpoint, authForm)
    authenticated.value = true
    setupRequired.value = false
    authForm.password = ''
    await loadDashboard()
    startRealtime()
    startPolling()
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    busy.value = false
  }
}

async function loadDashboard() {
  const [nextSettings, nextStatus, nextTasks, nextLogs] = await Promise.all([
    apiGet<Settings>('/api/settings'),
    apiGet<ServerStatus>('/api/server/status'),
    apiGet<TaskRecord[]>('/api/tasks'),
    apiGet<ServerLogs>('/api/server/logs')
  ])
  Object.assign(settings, nextSettings)
  status.value = nextStatus
  tasks.value = nextTasks
  serverLogs.value = nextLogs.logs
  await Promise.all([loadPalDashboard(false), loadConfig(), loadBackups(), loadMods()])
}

async function refreshRuntime() {
  if (realtimeConnected.value) {
    status.value = await apiGet<ServerStatus>('/api/server/status')
    await loadPalDashboard(false)
    return
  }
  const [nextStatus, nextTasks, nextLogs] = await Promise.all([
    apiGet<ServerStatus>('/api/server/status'),
    apiGet<TaskRecord[]>('/api/tasks'),
    apiGet<ServerLogs>('/api/server/logs')
  ])
  status.value = nextStatus
  tasks.value = nextTasks
  serverLogs.value = nextLogs.logs
  await loadPalDashboard(false)
}

async function saveSettings() {
  if (settingsSaveBlocked.value) {
    message.warning(settingsSaveBlockReason.value || '当前不能保存设置')
    return
  }
  busy.value = true
  try {
    const saved = await apiPut<Settings>('/api/settings', settings)
    Object.assign(settings, saved)
    await loadDashboard()
    message.success('设置已保存')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    busy.value = false
  }
}

async function loadConfig() {
  configBusy.value = true
  try {
    const nextConfig = await apiGet<PalConfigPayload>('/api/config')
    config.value = nextConfig
    Object.assign(configForm, nextConfig.values)
    configError.value = ''
    configNotInitialized.value = false
  } catch (err) {
    config.value = null
    if (err instanceof ApiError && err.status === 404) {
      configNotInitialized.value = true
      configError.value = configNotInitializedMessage
    } else {
      configNotInitialized.value = false
      configError.value = (err as Error).message
    }
  } finally {
    configBusy.value = false
  }
}

async function initConfig() {
  if (configInitBlocked.value) {
    message.warning(configInitBlockReason.value || '当前不能初始化配置')
    return
  }
  configBusy.value = true
  try {
    const nextConfig = await apiPost<PalConfigPayload>('/api/config/init')
    config.value = nextConfig
    Object.assign(configForm, nextConfig.values)
    configError.value = ''
    configNotInitialized.value = false
    message.success('配置文件已初始化')
  } catch (err) {
    config.value = null
    configNotInitialized.value = false
    configError.value = (err as Error).message
    message.error((err as Error).message)
  } finally {
    configBusy.value = false
  }
}

async function loadPalDashboard(showMessage: boolean) {
  palBusy.value = true
  try {
    palDashboard.value = await apiGet<PalDashboard>('/api/dashboard')
    if (showMessage && palDashboard.value.available) {
      message.success('REST API 状态已刷新')
    }
    if (showMessage && palDashboard.value.error) {
      message.warning(palDashboard.value.error)
    }
  } catch (err) {
    palDashboard.value = {
      available: false,
      error: (err as Error).message,
      info: {},
      metrics: {},
      settings: {},
      players: [],
      system: defaultSystemSummary(),
      recent_logs: [],
      recent_tasks: [],
      recent_backups: []
    }
    if (showMessage) message.error((err as Error).message)
  } finally {
    palBusy.value = false
  }
}

async function announceServer() {
  const text = announceMessage.value.trim()
  if (!text) {
    message.warning('请输入广播消息')
    return
  }
  if (!ensurePalRestActionAllowed()) return
  palActionBusy.value = 'announce'
  try {
    await apiPost('/api/server/announce', { message: text })
    announceMessage.value = ''
    message.success('广播已发送')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    palActionBusy.value = ''
  }
}

async function saveWorld() {
  if (!ensurePalRestActionAllowed()) return
  palActionBusy.value = 'save'
  try {
    await apiPost('/api/server/save')
    message.success('保存世界请求已发送')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    palActionBusy.value = ''
  }
}

async function shutdownServer() {
  if (!ensurePalRestActionAllowed()) return
  palActionBusy.value = 'shutdown'
  try {
    await apiPost('/api/server/shutdown', {
      waittime: shutdownWaitSeconds.value,
      message: shutdownMessage.value.trim()
    }, { confirm: true })
    shutdownMessage.value = ''
    message.success('优雅关服请求已发送')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    palActionBusy.value = ''
  }
}

async function restStopServer() {
  if (!ensurePalRestActionAllowed()) return
  palActionBusy.value = 'rest-stop'
  try {
    await apiPost('/api/server/rest-stop', undefined, { confirm: true })
    message.success('REST Stop 请求已发送')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    palActionBusy.value = ''
  }
}

async function actOnPlayer(player: PalPlayer, action: 'kick' | 'ban') {
  const userID = playerUserID(player)
  if (!userID) return
  if (!ensurePalRestActionAllowed()) return
  palActionBusy.value = `${action}:${userID}`
  try {
    await apiPost(`/api/players/${encodeURIComponent(userID)}/${action}`, {
      message: action === 'kick' ? 'Kicked by PalPanel Lite' : 'Banned by PalPanel Lite'
    }, { confirm: true })
    await loadPalDashboard(false)
    message.success(action === 'kick' ? '已发送踢出请求' : '已发送封禁请求')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    palActionBusy.value = ''
  }
}

async function unbanPlayer() {
  const userID = unbanUserID.value.trim()
  if (!userID) {
    message.warning('请输入 UserID')
    return
  }
  if (!ensurePalRestActionAllowed()) return
  palActionBusy.value = 'unban'
  try {
    await apiPost(`/api/players/${encodeURIComponent(userID)}/unban`, undefined, { confirm: true })
    unbanUserID.value = ''
    message.success('已发送解封请求')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    palActionBusy.value = ''
  }
}

async function saveConfig() {
  if (!config.value) return
  if (configSaveBlocked.value) {
    if (status.value?.external_running) {
      message.warning('检测到 PalServer 由面板外部运行，请先在外部停止服务器再保存配置')
    } else {
      message.warning('后台操作正在运行，请等待操作完成后再保存配置')
    }
    return
  }
  configBusy.value = true
  try {
    const saved = await apiPut<PalConfigPayload>('/api/config', { values: configForm })
    config.value = saved
    Object.assign(configForm, saved.values)
    await loadBackups()
    message.success(saved.needs_restart ? '配置已保存，重启后生效' : '配置已保存')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    configBusy.value = false
  }
}

async function backupConfig() {
  if (configSaveBlocked.value) {
    if (status.value?.external_running) {
      message.warning('检测到 PalServer 由面板外部运行，请先在外部停止服务器再备份配置')
    } else {
      message.warning('后台操作正在运行，请等待操作完成后再备份配置')
    }
    return
  }
  if (!config.value) return
  configBusy.value = true
  try {
    const result = await apiPost<{ backup_path: string }>('/api/config/backup')
    config.value = { ...config.value, backup_path: result.backup_path }
    message.success('配置备份已创建')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    configBusy.value = false
  }
}

async function loadBackups() {
  backupBusy.value = true
  try {
    backups.value = await apiGet<BackupRecord[]>('/api/backups')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    backupBusy.value = false
  }
}

async function createManualBackup() {
  if (!ensureBackupCreateAllowed()) return
  backupBusy.value = true
  try {
    await apiPost<BackupRecord>('/api/backups', { type: 'manual', note: 'Manual backup from web UI' })
    await loadBackups()
    message.success('手动备份已创建')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    backupBusy.value = false
  }
}

async function restoreBackup(id: number) {
  if (!ensureBackupRestoreAllowed()) return
  if (!ensureBackupPathAvailable(id)) return
  backupActionBusy.value = `restore:${id}`
  try {
    const result = await apiPost<RestoreResponse>(`/api/backups/${id}/restore`, undefined, { confirm: true })
    await Promise.all([loadDashboard(), loadBackups()])
    message.success(result.protective_backup ? '备份已恢复，并已创建恢复前保护备份' : '备份已恢复')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    backupActionBusy.value = ''
  }
}

async function deleteBackup(id: number) {
  if (!ensureBackupDeleteAllowed()) return
  if (!ensureBackupPathAvailable(id)) return
  backupActionBusy.value = `delete:${id}`
  try {
    await apiDelete<{ status: string }>(`/api/backups/${id}`, { confirm: true })
    await loadBackups()
    message.success('备份已删除')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    backupActionBusy.value = ''
  }
}

async function loadMods() {
  modBusy.value = true
  try {
    mods.value = await apiGet<ModRecord[]>('/api/mods')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    modBusy.value = false
  }
}

function openModUpload() {
  if (!ensureModMutationAllowed()) return
  modFileInput.value?.click()
}

async function uploadSelectedMod(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  if (!ensureModMutationAllowed()) {
    input.value = ''
    return
  }
  modBusy.value = true
  try {
    await apiUpload<ModRecord>('/api/mods/upload', file)
    await Promise.all([loadMods(), loadBackups()])
    modChangesNeedRestart.value = true
    message.success('MOD 已上传并安装')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    modBusy.value = false
    input.value = ''
  }
}

function openModUpdate(mod: ModRecord) {
  if (!ensureModMutationAllowed()) return
  modUpdateTarget.value = mod
  modUpdateFileInput.value?.click()
}

async function uploadSelectedModUpdate(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const target = modUpdateTarget.value
  if (!file || !target) {
    input.value = ''
    modUpdateTarget.value = null
    return
  }
  if (!ensureModMutationAllowed()) {
    input.value = ''
    modUpdateTarget.value = null
    return
  }
  modActionBusy.value = `update:${target.id}`
  try {
    await apiUpload<ModRecord>(`/api/mods/${target.id}/update`, file, { confirm: true })
    await Promise.all([loadMods(), loadBackups()])
    modChangesNeedRestart.value = true
    message.success('MOD 已更新，重启后生效')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    modActionBusy.value = ''
    modUpdateTarget.value = null
    input.value = ''
  }
}

async function setModEnabled(id: number, enabled: boolean) {
  if (!ensureModMutationAllowed()) return
  modActionBusy.value = `${enabled ? 'enable' : 'disable'}:${id}`
  try {
    await apiPost<ModRecord>(`/api/mods/${id}/${enabled ? 'enable' : 'disable'}`)
    await Promise.all([loadMods(), loadBackups()])
    modChangesNeedRestart.value = true
    message.success(enabled ? 'MOD 已启用，重启后生效' : 'MOD 已禁用，重启后生效')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    modActionBusy.value = ''
  }
}

async function openModDirectory(mod: ModRecord) {
  if (!ensureModDirectoryActionAllowed()) return
  modActionBusy.value = `open:${mod.id}`
  try {
    const result = await apiPost<OpenDirectoryResponse>(`/api/mods/${mod.id}/open-dir`)
    message.success(`已请求打开目录：${result.path}`)
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    modActionBusy.value = ''
  }
}

async function deleteMod(id: number) {
  if (!ensureModMutationAllowed()) return
  modActionBusy.value = `delete:${id}`
  try {
    await apiDelete<{ status: string }>(`/api/mods/${id}`, { confirm: true })
    await Promise.all([loadMods(), loadBackups()])
    modChangesNeedRestart.value = true
    message.success('MOD 已删除，重启后生效')
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    modActionBusy.value = ''
  }
}

function showModInfo(mod: ModRecord) {
  selectedMod.value = mod
  modInfoVisible.value = true
}

function serverActionBlocked(action: ServerAction): boolean {
  return serverActionBlockReason(action) !== ''
}

function serverActionBlockReason(action: ServerAction): string {
  if (actionBusy.value !== null) return '已有服务器操作正在执行，请稍后再试。'
  if (activeTask.value) return '后台操作正在运行，请等待操作完成后再执行服务器操作。'

  if ((action === 'install' || action === 'update' || action === 'restart') && status.value?.external_running) {
    return '检测到 PalServer 由面板外部运行，请先在外部停止后再执行服务器操作。'
  }
  if ((action === 'start' || action === 'restart') && !status.value?.pal_server_exists) {
    return '未检测到 PalServer，请先安装或配置正确的 PalServer 目录。'
  }
  if (action === 'start' && status.value?.running) {
    return 'PalServer 已在运行。'
  }
  if (action === 'stop' && !status.value?.managed_running) {
    return '没有由面板托管运行的 PalServer 可停止。'
  }
  return ''
}

async function runServerAction(action: ServerAction) {
  const blockReason = serverActionBlockReason(action)
  if (blockReason) {
    message.warning(blockReason)
    return
  }
  actionBusy.value = action
  try {
    await apiPost(`/api/server/${action}`, undefined, { confirm: confirmedServerActions.has(action) })
    await loadDashboard()
    if (action === 'start' || action === 'restart') {
      modChangesNeedRestart.value = false
    }
    message.success(actionLabels[action])
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    actionBusy.value = null
  }
}

async function runSetupStep(step: SetupStep) {
  if (!step.actionKey) return
  setupStepBusy.value = step.key
  try {
    if (step.actionKey === 'save-settings') {
      await saveSettings()
    } else if (step.actionKey === 'install') {
      await runServerAction('install')
    } else if (step.actionKey === 'init-config') {
      await initConfig()
      if (config.value) {
        message.success('配置文件已就绪')
      } else if (configError.value) {
        message.warning(configError.value)
      }
    } else if (step.actionKey === 'create-backup') {
      await createManualBackup()
    } else if (step.actionKey === 'refresh-rest') {
      await loadPalDashboard(true)
    }
  } finally {
    setupStepBusy.value = ''
  }
}

function ensureModMutationAllowed(): boolean {
  if (!modMutationActionBlocked.value) return true
  message.warning(modMutationActionBlockReason.value || '请先停止 PalServer 再修改 MOD')
  return false
}

function ensureModDirectoryActionAllowed(): boolean {
  if (!modDirectoryActionBlocked.value) return true
  message.warning(modDirectoryActionBlockReason.value || '当前不能打开 MOD 目录')
  return false
}

function ensureBackupCreateAllowed(): boolean {
  if (!backupCreateBlocked.value) return true
  message.warning(backupCreateBlockReason.value || '当前不能创建备份')
  return false
}

function ensureBackupRestoreAllowed(): boolean {
  if (!backupRestoreBlocked.value) return true
  message.warning(backupRestoreBlockReason.value || '当前不能恢复备份')
  return false
}

function ensureBackupDeleteAllowed(): boolean {
  if (!backupDeleteBlocked.value) return true
  message.warning(backupDeleteBlockReason.value || '当前不能删除备份')
  return false
}

function ensureBackupPathAvailable(id: number): boolean {
  const backup = backups.value.find((item) => item.id === id)
  if (backup?.path) return true
  message.warning('备份路径无效，不能恢复或删除。')
  return false
}

function ensurePalRestActionAllowed(): boolean {
  if (!palRestActionBlocked.value) return true
  message.warning(palRestActionBlockReason.value || '当前无法执行 Palworld REST 操作')
  return false
}

function samePathDraft(a: string, b: string): boolean {
  const normalize = (value: string) => value.trim().replace(/[\\/]+$/, '')
  const left = normalize(a)
  const right = normalize(b)
  if (!left || !right) return left === right
  if (status.value?.os === 'windows') {
    return left.toLowerCase() === right.toLowerCase()
  }
  return left === right
}

type TagType = 'default' | 'success' | 'warning' | 'error' | 'info'

function portStatusType(status: string): TagType {
  if (status === 'available') return 'success'
  if (status === 'in_use') return 'warning'
  if (status === 'disabled') return 'default'
  if (status === 'invalid') return 'error'
  return 'info'
}

function portStatusText(status: string): string {
  const labels: Record<string, string> = {
    available: '可用',
    in_use: '已占用',
    disabled: '未启用',
    invalid: '无效',
    unknown: '未知'
  }
  return labels[status] ?? status
}

function portSourceText(source: string): string {
  const labels: Record<string, string> = {
    server_launch_args: '启动参数',
    'PalWorldSettings.ini': '配置文件',
    rest_api_url: 'REST API 地址',
    default: '默认值'
  }
  return labels[source] ?? source
}

function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  if (size < 1024 * 1024 * 1024) return `${(size / 1024 / 1024).toFixed(1)} MB`
  return `${(size / 1024 / 1024 / 1024).toFixed(1)} GB`
}

function percentText(value: number | undefined | null): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return `${value.toFixed(1)}%`
}

function formatDateTime(value: string | undefined): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function getLaunchNumber(flag: string): number | null {
  const value = getLaunchArgValue(flag)
  if (!value) return null
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null
}

function setLaunchNumber(flag: string, value: number | null) {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    setLaunchArgValue(flag, `${Math.trunc(value)}`)
    return
  }
  setLaunchArgValue(flag, '')
}

function getLaunchArgValue(flag: string): string {
  const args = parseLaunchArgs(settings.server_launch_args)
  const flagLower = flag.toLowerCase()
  const prefix = `${flagLower}=`
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index]
    const argLower = arg.toLowerCase()
    if (argLower === flagLower) {
      return args[index + 1] ?? ''
    }
    if (argLower.startsWith(prefix)) {
      return arg.slice(flag.length + 1)
    }
  }
  return ''
}

function setLaunchArgValue(flag: string, value: string) {
  const args = parseLaunchArgs(settings.server_launch_args)
  const flagLower = flag.toLowerCase()
  const prefix = `${flagLower}=`
  const next: string[] = []
  let replaced = false
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index]
    const argLower = arg.toLowerCase()
    if (argLower === flagLower) {
      if (value && !replaced) {
        next.push(`${flag}=${value}`)
        replaced = true
      }
      if (index + 1 < args.length && !args[index + 1].startsWith('-')) {
        index += 1
      }
      continue
    }
    if (argLower.startsWith(prefix)) {
      if (value && !replaced) {
        next.push(`${flag}=${value}`)
        replaced = true
      }
      continue
    }
    next.push(arg)
  }
  if (value && !replaced) {
    next.push(`${flag}=${value}`)
  }
  commitLaunchArgs(next)
}

function hasLaunchFlag(flag: string): boolean {
  const flagLower = flag.toLowerCase()
  return parseLaunchArgs(settings.server_launch_args).some((arg) => arg.toLowerCase() === flagLower)
}

function setLaunchFlag(flag: string, enabled: boolean) {
  const flagLower = flag.toLowerCase()
  const next = parseLaunchArgs(settings.server_launch_args).filter((arg) => arg.toLowerCase() !== flagLower)
  if (enabled) {
    next.push(flag)
  }
  commitLaunchArgs(next)
}

function commitLaunchArgs(args: string[]) {
  settings.server_launch_args = args.filter((arg) => arg.trim() !== '').map(formatLaunchArg).join(' ')
}

function parseLaunchArgs(input: string): string[] {
  const args: string[] = []
  let current = ''
  let quote = ''
  const flush = () => {
    if (current !== '') {
      args.push(current)
      current = ''
    }
  }
  for (const char of input.trim()) {
    if (quote) {
      if (char === quote) {
        quote = ''
      } else {
        current += char
      }
      continue
    }
    if (char === '"' || char === "'") {
      quote = char
      continue
    }
    if (/\s/.test(char)) {
      flush()
      continue
    }
    current += char
  }
  flush()
  return args
}

function formatLaunchArg(arg: string): string {
  if (!/\s/.test(arg)) return arg
  if (!arg.includes('"')) return `"${arg}"`
  if (!arg.includes("'")) return `'${arg}'`
  return arg
}

function infoText(...keys: string[]): string {
  return mapText(palDashboard.value?.info ?? {}, keys)
}

function metricText(...keys: string[]): string {
  return mapText(palDashboard.value?.metrics ?? {}, keys)
}

function runtimeSettingText(...keys: string[]): string {
  return mapText(palDashboard.value?.settings ?? {}, keys)
}

function metricNumber(key: string): number | null {
  const value = palDashboard.value?.metrics?.[key]
  if (typeof value === 'number') return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : null
  }
  return null
}

function playerName(player: PalPlayer): string {
  return playerText(player, 'name', 'playername', 'playerName') || 'Unknown Player'
}

function playerUserID(player: PalPlayer): string {
  return playerText(player, 'userid', 'user_id', 'userId', 'uid')
}

function playerKey(player: PalPlayer): string {
  return playerUserID(player) || playerName(player)
}

function playerText(player: PalPlayer, ...keys: string[]): string {
  const text = mapText(player, keys)
  return text === '-' ? '' : text
}

function mapText(source: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value = source[key]
    if (value === undefined || value === null || value === '') continue
    if (typeof value === 'number') return Number.isInteger(value) ? `${value}` : value.toFixed(1)
    if (typeof value === 'string') return value
    if (typeof value === 'boolean') return value ? 'true' : 'false'
    return JSON.stringify(value)
  }
  return '-'
}

function startRealtime() {
  stopRealtime()
  connectRealtime()
}

function connectRealtime() {
  if (!authenticated.value) return
  const socket = new WebSocket(runtimeWebSocketURL())
  realtimeSocket = socket

  socket.onopen = () => {
    if (realtimeSocket !== socket) return
    realtimeConnected.value = true
  }

  socket.onmessage = (event) => {
    if (realtimeSocket !== socket) return
    try {
      applyRuntimeEvent(JSON.parse(event.data) as RuntimeEvent)
    } catch {
      // Ignore malformed runtime messages; the REST fallback will refresh state.
    }
  }

  socket.onerror = () => {
    socket.close()
  }

  socket.onclose = () => {
    if (realtimeSocket !== socket) return
    realtimeSocket = undefined
    realtimeConnected.value = false
    scheduleRealtimeReconnect()
  }
}

function stopRealtime() {
  if (reconnectTimer !== undefined) {
    window.clearTimeout(reconnectTimer)
    reconnectTimer = undefined
  }
  const socket = realtimeSocket
  realtimeSocket = undefined
  realtimeConnected.value = false
  if (socket) {
    socket.onclose = null
    socket.onerror = null
    socket.onmessage = null
    socket.close()
  }
}

function scheduleRealtimeReconnect() {
  if (!authenticated.value || reconnectTimer !== undefined) return
  reconnectTimer = window.setTimeout(() => {
    reconnectTimer = undefined
    connectRealtime()
  }, 3000)
}

function runtimeWebSocketURL(): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}/api/events`
}

function applyRuntimeEvent(event: RuntimeEvent) {
  if (typeof event.operation_running === 'boolean') {
    applyOperationState(event.operation_running)
  }
  if (event.type === 'error') {
    if (event.error) message.warning(event.error)
    return
  }
  if (event.type === 'operation') {
    return
  }
  if (event.type === 'snapshot') {
    if (event.tasks) tasks.value = event.tasks
    if (event.server_logs) serverLogs.value = event.server_logs
    applyRunningState(event.running)
    return
  }
  if (event.type === 'task' && event.task) {
    upsertTask(event.task)
    return
  }
  if (event.type === 'server_log' && event.server_logs) {
    appendServerLogs(event.server_logs)
    applyRunningState(event.running)
  }
}

function upsertTask(task: TaskRecord) {
  const next = tasks.value.filter((item) => item.id !== task.id)
  next.push(task)
  next.sort((a, b) => b.id - a.id)
  tasks.value = next.slice(0, 10)
  if (palDashboard.value) {
    const recent = palDashboard.value.recent_tasks.filter((item) => item.id !== task.id)
    recent.push(task)
    recent.sort((a, b) => b.id - a.id)
    palDashboard.value.recent_tasks = recent.slice(0, 5)
  }
}

function appendServerLogs(entries: ServerLogs['logs']) {
  if (entries.length === 0) return
  serverLogs.value = [...serverLogs.value, ...entries].slice(-400)
  if (palDashboard.value) {
    palDashboard.value.recent_logs = [...palDashboard.value.recent_logs, ...entries].slice(-8)
  }
}

function applyRunningState(running: boolean | undefined) {
  if (typeof running !== 'boolean' || !status.value) return
  status.value.running = running
  if (!running) {
    status.value.managed_running = false
    status.value.external_running = false
  }
}

function applyOperationState(operationRunning: boolean | undefined) {
  if (typeof operationRunning !== 'boolean' || !status.value) return
  status.value.operation_running = operationRunning
}

function defaultSystemSummary(): SystemSummary {
  return {
    collected_at: '',
    cpu: { cores: 0 },
    memory: {
      total_bytes: 0,
      used_bytes: 0,
      free_bytes: 0
    },
    disk: {
      path: '',
      total_bytes: 0,
      used_bytes: 0,
      free_bytes: 0
    }
  }
}

async function logout() {
  await apiPost('/api/auth/logout')
  authenticated.value = false
  stopRealtime()
  stopPolling()
}

function startPolling() {
  stopPolling()
  pollTimer = window.setInterval(() => {
    if (authenticated.value) {
      refreshRuntime().catch(() => undefined)
    }
  }, 3000)
}

function stopPolling() {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer)
    pollTimer = undefined
  }
}

function defaultConfigValues(): PalConfigValues {
  return {
    server_name: '',
    server_description: '',
    admin_password: '',
    server_password: '',
    server_player_max_num: 32,
    difficulty: 'None',
    day_time_speed_rate: 1,
    night_time_speed_rate: 1,
    exp_rate: 1,
    pal_capture_rate: 1,
    pal_spawn_num_rate: 1,
    enemy_drop_item_rate: 1,
    collection_drop_rate: 1,
    collection_object_hp_rate: 1,
    collection_object_respawn_speed_rate: 1,
    egg_default_hatching_time: 72,
    death_penalty: 'All',
    base_camp_max_num: 128,
    base_camp_max_num_in_guild: 4,
    base_camp_worker_max_num: 15,
    guild_player_max_num: 20,
    build_object_deterioration_damage_rate: 1,
    max_building_limit_num: 0,
    server_replicate_pawn_cull_distance: 15000,
    public_port: 8211,
    public_ip: '',
    rcon_enabled: false,
    rcon_port: 25575,
    rest_api_enabled: false,
    rest_api_port: 8212,
    log_format_type: 'Text',
    crossplay_platforms: '',
    allow_client_mod: false,
    is_pvp: false,
    enable_player_to_player_damage: false,
    enable_defense_other_guild_player: false,
    is_use_backup_save_data: true
  }
}
</script>
