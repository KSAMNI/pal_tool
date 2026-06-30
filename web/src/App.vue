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
                <n-tag :type="status?.running ? 'success' : 'default'">
                  {{ status?.running ? '运行中' : '未运行' }}
                </n-tag>
              </div>
              <n-divider />
              <n-space>
                <n-button disabled title="下一阶段实现">
                  <template #icon><Play :size="16" /></template>
                  启动
                </n-button>
                <n-button disabled title="下一阶段实现">
                  <template #icon><Square :size="16" /></template>
                  停止
                </n-button>
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
                <n-form-item label="启动参数">
                  <n-input v-model:value="settings.server_launch_args" type="textarea" :autosize="{ minRows: 2 }" />
                </n-form-item>
                <div class="settings-inline">
                  <n-checkbox v-model:checked="settings.auto_backup_enabled">启用自动备份</n-checkbox>
                  <n-input-number v-model:value="settings.auto_backup_retention" :min="1" :max="200" />
                </div>
                <n-button type="primary" :loading="busy" @click="saveSettings">
                  <template #icon><Save :size="16" /></template>
                  保存设置
                </n-button>
              </n-form>
            </n-card>
          </section>

          <n-card title="检测结果" class="wide-card">
            <n-descriptions bordered :column="1" size="small">
              <n-descriptions-item label="PalServer 二进制">
                {{ status?.pal_server_binary || '未配置' }}
              </n-descriptions-item>
              <n-descriptions-item label="SteamCMD">
                {{ status?.steamcmd_path || '未检测到' }}
              </n-descriptions-item>
            </n-descriptions>
          </n-card>
        </template>
      </main>
    </n-message-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { LogOut, Play, RefreshCw, Save, Square } from 'lucide-vue-next'
import { createDiscreteApi } from 'naive-ui'
import { apiGet, apiPost, apiPut, type AuthState, type ServerStatus, type Settings } from './api'

const { message } = createDiscreteApi(['message'])

const loading = ref(true)
const busy = ref(false)
const setupRequired = ref(false)
const authenticated = ref(false)
const status = ref<ServerStatus | null>(null)
const settings = reactive<Settings>({
  pal_server_path: '',
  steamcmd_path: '',
  rest_api_url: 'http://127.0.0.1:8212/v1/api',
  server_launch_args: '-useperfthreads -NoAsyncLoadingThread -UseMultithreadForDS',
  auto_backup_enabled: true,
  auto_backup_retention: 20
})
const authForm = reactive({
  username: 'admin',
  password: ''
})

onMounted(async () => {
  await bootstrap()
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
    }
  } catch {
    authenticated.value = false
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
  } catch (err) {
    message.error((err as Error).message)
  } finally {
    busy.value = false
  }
}

async function loadDashboard() {
  const [nextSettings, nextStatus] = await Promise.all([
    apiGet<Settings>('/api/settings'),
    apiGet<ServerStatus>('/api/server/status')
  ])
  Object.assign(settings, nextSettings)
  status.value = nextStatus
}

async function saveSettings() {
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

async function logout() {
  await apiPost('/api/auth/logout')
  authenticated.value = false
}
</script>
