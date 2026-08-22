// Cliente HTTP del panel: añade el token, renueva la sesión cuando caduca y
// normaliza los errores de la API.

export interface ApiError extends Error {
  status: number
  fields?: Record<string, string>
}

const ACCESS_KEY = 'gocp.access'
const REFRESH_KEY = 'gocp.refresh'

export const tokens = {
  get access() {
    return sessionStorage.getItem(ACCESS_KEY)
  },
  get refresh() {
    return sessionStorage.getItem(REFRESH_KEY)
  },
  set(access: string, refresh: string) {
    sessionStorage.setItem(ACCESS_KEY, access)
    sessionStorage.setItem(REFRESH_KEY, refresh)
  },
  clear() {
    sessionStorage.removeItem(ACCESS_KEY)
    sessionStorage.removeItem(REFRESH_KEY)
  },
}

function makeError(status: number, message: string, fields?: Record<string, string>): ApiError {
  const err = new Error(message) as ApiError
  err.status = status
  err.fields = fields
  return err
}

let refreshing: Promise<boolean> | null = null

async function tryRefresh(): Promise<boolean> {
  if (refreshing) return refreshing
  const refresh = tokens.refresh
  if (!refresh) return false

  refreshing = (async () => {
    try {
      const res = await fetch('/api/v1/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: refresh }),
      })
      if (!res.ok) {
        tokens.clear()
        return false
      }
      const data = await res.json()
      tokens.set(data.access_token, data.refresh_token)
      return true
    } catch {
      return false
    } finally {
      refreshing = null
    }
  })()
  return refreshing
}

export async function request<T>(
  path: string,
  options: RequestInit = {},
  retry = true,
): Promise<T> {
  const headers = new Headers(options.headers)
  if (!headers.has('Content-Type') && options.body) {
    headers.set('Content-Type', 'application/json')
  }
  const access = tokens.access
  if (access) headers.set('Authorization', `Bearer ${access}`)

  const res = await fetch(`/api/v1${path}`, { ...options, headers })

  if (res.status === 401 && retry) {
    if (await tryRefresh()) {
      return request<T>(path, options, false)
    }
    tokens.clear()
    window.dispatchEvent(new CustomEvent('gocp:session-expired'))
    throw makeError(401, 'La sesión ha expirado, vuelve a iniciar sesión')
  }

  if (res.status === 204) return undefined as T

  const contentType = res.headers.get('Content-Type') ?? ''
  if (!res.ok) {
    if (contentType.includes('application/json')) {
      const body = await res.json().catch(() => ({}))
      throw makeError(res.status, body.error ?? 'Error inesperado', body.fields)
    }
    throw makeError(res.status, await res.text())
  }

  if (contentType.includes('application/json')) return res.json() as Promise<T>
  return (await res.text()) as T
}

export const api = {
  get: <T,>(path: string) => request<T>(path),
  post: <T,>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined }),
  put: <T,>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body: body ? JSON.stringify(body) : undefined }),
  del: <T,>(path: string) => request<T>(path, { method: 'DELETE' }),
}

// --- Tipos que devuelve la API --------------------------------------------

export type Role = 'admin' | 'reseller' | 'user'

export interface User {
  id: string
  username: string
  email: string
  full_name: string
  role: Role
  is_active: boolean
  last_login_at?: string
  created_at: string
}

export interface Plan {
  id: string
  name: string
  description: string
  disk_quota_mb: number
  bandwidth_quota_mb: number
  max_sites: number
  max_databases: number
  max_ftp_accounts: number
  max_cron_jobs: number
  cpu_limit: number
  memory_limit_mb: number
  php_versions: string[]
  is_default: boolean
}

export interface Account {
  id: string
  owner_id: string
  plan_id: string
  system_user: string
  primary_domain: string
  status: 'active' | 'suspended' | 'terminated'
  suspend_reason: string
  disk_used_mb: number
  bandwidth_used_mb: number
  notes: string
  created_at: string
  plan?: Plan
  site_count?: number
  owner_login?: string
}

export interface Domain {
  id: string
  site_id: string
  fqdn: string
  kind: 'primary' | 'addon' | 'subdomain' | 'alias'
  redirect_to: string
  tls_mode: string
  force_https: boolean
}

export interface Site {
  id: string
  account_id: string
  name: string
  php_version: string
  document_root: string
  host_path: string
  container_name: string
  upstream_host: string
  worker_script: string
  worker_count: number
  env_vars: Record<string, string>
  status: 'provisioning' | 'running' | 'stopped' | 'error' | 'deleting'
  last_error: string
  created_at: string
  domains?: Domain[]
}

export interface SiteDatabase {
  id: string
  account_id: string
  engine: string
  db_name: string
  db_user: string
  charset: string
  size_mb: number
  created_at: string
}

export interface CronJob {
  id: string
  site_id: string
  schedule: string
  command: string
  is_active: boolean
  last_run_at?: string
  last_exit_code?: number
  last_output: string
}

export interface Overview {
  accounts: number
  active_sites: number
  total_sites: number
  domains: number
  databases: number
  users: number
  disk_used_mb: number
  suspended_accounts: number
}

export interface UsageSample {
  site_id: string
  cpu_percent: number
  memory_mb: number
  disk_mb: number
  sampled_at: string
}

export interface SiteStats {
  cpu_percent: number
  memory_mb: number
  net_rx_mb: number
  net_tx_mb: number
}

export interface AuditEntry {
  id: number
  actor_username: string
  action: string
  target_type: string
  target_id: string
  detail: Record<string, unknown>
  ip_address?: string
  created_at: string
}

export interface SystemInfo {
  hostname: string
  os: string
  arch: string
  cpu_cores: number
  load_avg: [number, number, number]
  mem_total_mb: number
  mem_used_mb: number
  disk_total_gb: number
  disk_free_gb: number
  uptime_secs: number
}
