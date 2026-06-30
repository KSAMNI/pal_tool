export interface AuthState {
  setup_required: boolean
}

export interface Settings {
  pal_server_path: string
  steamcmd_path: string
  rest_api_url: string
  server_launch_args: string
  auto_backup_enabled: boolean
  auto_backup_retention: number
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
}

const headers = { 'Content-Type': 'application/json' }

export async function apiGet<T>(url: string): Promise<T> {
  const res = await fetch(url, { credentials: 'include' })
  return parseResponse<T>(res)
}

export async function apiPost<T>(url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method: 'POST',
    headers,
    credentials: 'include',
    body: body === undefined ? undefined : JSON.stringify(body)
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

async function parseResponse<T>(res: Response): Promise<T> {
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(data.error ?? `HTTP ${res.status}`)
  }
  return data as T
}
