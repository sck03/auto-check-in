import type { Schedule } from './types'

export function time(value: string | null | undefined, full = false) {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    ...(full ? { second: '2-digit' } : {}),
    hour12: false,
  }).format(new Date(value))
}
export function quota(value: number | null | undefined, unit: string, ratio: number) {
  if (value == null || !ratio) return '未提供'
  const amount = (value / ratio).toLocaleString('zh-CN', { maximumFractionDigits: 4 })
  return unit === 'USD' ? '$' + amount : amount + ' ' + unit
}
export function scheduleLabel(schedule: Schedule) {
  return schedule.window_start + '–' + schedule.window_end
}
export function today(value: string | null) {
  if (!value) return false
  const date = (v: Date) => v.toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' })
  return date(new Date(value)) === date(new Date())
}
export const statusLabels: Record<string, string> = {
  ready: '待签到',
  queued: '排队中',
  running: '执行中',
  waiting_user: '等待登录',
  succeeded: '已完成',
  already_done: '已签到',
  needs_attention: '需要处理',
  unverified: '待确认',
  failed: '未成功',
  interrupted: '已中断',
  cancelled: '已取消',
}
