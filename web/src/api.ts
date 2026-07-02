export interface AuthState {
  setup_required: boolean
}

export interface Settings {
  pal_server_path: string
  steamcmd_path: string
  rest_api_url: string
  rest_api_username: string
  rest_api_password?: string
  server_launch_args: string
  game_port?: number
  launch_players?: number
  public_lobby: boolean
  no_mods: boolean
  performance_flags: boolean
  worker_threads?: number
  auto_backup_enabled: boolean
  auto_backup_retention: number
  auto_backup_interval_hours: number
}

export interface ServerStatus {
  os: string
  configured: boolean
  pal_server_path: string
  pal_server_exists: boolean
  pal_server_binary: string
  steamcmd_path: string
  steamcmd_available: boolean
  running: boolean
  managed_running: boolean
  external_running: boolean
  operation_running: boolean
  runtime_warning?: string
  port_checks?: PortCheck[]
}

export interface PortCheck {
  name: string
  protocol: string
  port?: number
  source: string
  status: string
  message?: string
}

export interface TaskRecord {
  id: number
  type: string
  status: string
  log: string
  created_at: string
  finished_at?: string
}

export interface ServerLogEntry {
  time: string
  message: string
}

export type ScheduleAction = 'start' | 'stop' | 'restart'

export interface ScheduleRecord {
  id?: number
  time: string
  action: ScheduleAction
  enabled: boolean
}

export interface SchedulesPayload {
  schedules: ScheduleRecord[]
  server_time?: string
  timezone?: string
}

export interface ServerLogs {
  running: boolean
  logs: ServerLogEntry[]
}

export type PalConfigValue = string | number | boolean | null

export interface PalConfigValues {
  [key: string]: PalConfigValue
}

export interface PalConfigFieldOption {
  label: string
  value: string
}

export interface PalConfigField {
  key: string
  raw_key: string
  label: string
  group: string
  type: string
  description?: string
  min?: number
  max?: number
  step?: number
  options?: PalConfigFieldOption[]
}

export interface PalConfigPayload {
  exists: boolean
  config_path: string
  default_path: string
  platform: string
  backup_path?: string
  needs_restart?: boolean
  values: PalConfigValues
  raw_values: Record<string, string>
  fields: PalConfigField[]
}

export interface BackupRecord {
  id: number
  filename: string
  path: string
  size: number
  type: string
  note?: string
  created_at: string
}

export interface RestoreResponse {
  restored_backup: BackupRecord
  protective_backup?: BackupRecord
}

export interface ModRecord {
  id: number
  name: string
  package_name: string
  version: string
  author: string
  folder_name: string
  enabled: boolean
  server_supported: boolean
  info_json: string
  install_path: string
  installed_at: string
  updated_at: string
}

export interface OpenDirectoryResponse {
  status: string
  path: string
}

export type PalPlayer = Record<string, unknown>

export interface SystemSummary {
  collected_at: string
  cpu: {
    cores: number
    usage_percent?: number
  }
  memory: {
    total_bytes: number
    used_bytes: number
    free_bytes: number
    used_percent?: number
  }
  disk: {
    path: string
    total_bytes: number
    used_bytes: number
    free_bytes: number
    used_percent?: number
  }
  errors?: string[]
}

export interface PalDashboard {
  available: boolean
  error?: string
  info: Record<string, unknown>
  metrics: Record<string, unknown>
  settings: Record<string, unknown>
  players: PalPlayer[]
  system: SystemSummary
  recent_logs: ServerLogEntry[]
  recent_tasks: TaskRecord[]
  recent_backups: BackupRecord[]
}

export interface RuntimeEvent {
  type: 'snapshot' | 'task' | 'server_log' | 'operation' | 'error'
  task?: TaskRecord
  tasks?: TaskRecord[]
  server_logs?: ServerLogEntry[]
  running?: boolean
  operation_running?: boolean
  error?: string
}

const confirmationHeader = 'X-Palpanel-Confirm'
const headers = { 'Content-Type': 'application/json' }

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

interface RequestOptions {
  confirm?: boolean
}

function withConfirmation(base: HeadersInit = {}, options?: RequestOptions): HeadersInit {
  if (!options?.confirm) return base
  return { ...base, [confirmationHeader]: 'true' }
}

export async function apiGet<T>(url: string): Promise<T> {
  const res = await fetch(url, { credentials: 'include' })
  return parseResponse<T>(res)
}

export async function apiPost<T>(url: string, body?: unknown, options?: RequestOptions): Promise<T> {
  const hasBody = body !== undefined
  const res = await fetch(url, {
    method: 'POST',
    headers: withConfirmation(hasBody ? headers : {}, options),
    credentials: 'include',
    body: hasBody ? JSON.stringify(body) : undefined
  })
  return parseResponse<T>(res)
}

export async function apiPut<T>(url: string, body: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'PUT',
    headers,
    credentials: 'include',
    body: JSON.stringify(body)
  })
  return parseResponse<T>(res)
}

export async function apiDelete<T>(url: string, options?: RequestOptions): Promise<T> {
  const res = await fetch(url, {
    method: 'DELETE',
    headers: withConfirmation({}, options),
    credentials: 'include'
  })
  return parseResponse<T>(res)
}

export async function apiUpload<T>(url: string, file: File, options?: RequestOptions): Promise<T> {
  const body = new FormData()
  body.append('file', file)
  const res = await fetch(url, {
    method: 'POST',
    headers: withConfirmation({}, options),
    credentials: 'include',
    body
  })
  return parseResponse<T>(res)
}

async function parseResponse<T>(res: Response): Promise<T> {
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const message = typeof data.error === 'string' ? data.error : `HTTP ${res.status}`
    throw new ApiError(res.status, message)
  }
  return data as T
}
