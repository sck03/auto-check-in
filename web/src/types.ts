export interface Schedule {
  timezone: string
  window_start: string
  window_end: string
  retry_until: string
}
export interface Selectors {
  login_entry: string
  username: string
  password: string
  submit: string
  github: string
  linuxdo: string
  checkin: string
  success: string
  already: string
}
export interface Site {
  id: string
  name: string
  base_url: string
  template_id: string
  adapter: 'newapi' | 'anyrouter' | 'agentrouter' | 'browser' | 'x666'
  transport: 'http' | 'browser'
  login_path: string
  login_api_path: string
  checkin_path: string
  user_info_path: string
  user_header: string
  quota_per_unit: number
  unit: string
  selectors: Selectors
  version: number
}
export type AuthMethod = 'cookie' | 'password' | 'github' | 'linuxdo'
export interface AuthSummary {
  method: AuthMethod
  username: string
  api_user: string
  has_password: boolean
  has_session: boolean
  has_token: boolean
}
export interface Account {
  id: string
  name: string
  site_id: string
  enabled: boolean
  schedule: Schedule
  auth: AuthSummary
  version: number
  created_at: string
  updated_at: string
  next_run_at: string | null
  retry_not_before?: string | null
  last_run_at: string | null
  last_success_at: string | null
  last_status: string
  last_message: string
  active_run_id: string
  active_status: string
}
export interface CredentialPatch {
  method?: AuthMethod
  username?: string
  password?: string
  cookie_header?: string
  api_user?: string
  token?: string
}
export interface AccountInput {
  name: string
  site_id: string
  enabled: boolean
  schedule: Schedule
  version: number
  credentials?: CredentialPatch
}
export interface Result {
  status: string
  message: string
  evidence: string
  stage?: string
  http_status?: number
  advice?: string
  reward: number | null
  balance_before: number | null
  balance_after: number | null
  unit: string
  quota_per_unit: number
  retryable: boolean
}
export interface Run {
  id: string
  account_id: string
  account_name: string
  site_name: string
  action: string
  trigger: string
  status: string
  attempt: number
  created_at: string
  started_at: string | null
  finished_at: string | null
  result: Result
}
export interface RunPage {
  items: Run[]
  total: number
}
export interface Settings {
  scheduler_enabled: boolean
  concurrency: number
  max_retries: number
  retry_base_seconds: number
  task_timeout_seconds: number
  site_gap_seconds: number
  retention_days: number
  version: number
}
export interface Template {
  id: string
  description: string
  site: Site
  schedule: Schedule
}
export interface Meta {
  version: string
  data_dir: string
  go_version: string
  desktop: boolean
  server_time: string
  browser_available: boolean
  allow_http: boolean
}
export interface LoginInfo {
  id: string
  account_id: string
  expires_at: string
  width: number
  height: number
  native_window?: boolean
}
export interface EnqueueResult {
  account_id: string
  accepted: boolean
  message: string
  run_id?: string
}
export interface DesktopPreferences {
  autostart: boolean
  minimize_to_tray: boolean
  close_to_tray: boolean
  version: number
}
export interface DesktopStatus {
  preferences: DesktopPreferences
  tray_ready: boolean
  window_open: boolean
  last_error: string
  menu: { action: string; label: string; checked: boolean }[]
}
