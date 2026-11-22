import type { ChatRunStatus } from '@/types/workbenchChat'
import { formatBillingCredits, getCurrentBillingCurrencyView } from '@/utils/billingCurrency'
export const officeStatusLabels: Record<ChatRunStatus, string> = { queued: '排队中', running: '执行中', waiting_user: '等待用户', recovering: '恢复中', needs_confirmation: '结果待确认', completed: '已完成', partially_completed: '部分完成', failed: '失败', cancelled: '已取消' }
export function officeStatusVariant(status: ChatRunStatus): string { return status === 'completed' ? 'success' : status === 'failed' ? 'error' : ['needs_confirmation', 'partially_completed'].includes(status) ? 'warning' : 'neutral' }
export function officeBilling(credits?: number): string { return credits == null ? '—' : formatBillingCredits(credits, getCurrentBillingCurrencyView(), 4) }
export function officeTime(value?: string): string { return value ? new Date(value).toLocaleString() : '—' }
export function officeDuration(run: { created_at: string; started_at?: string; finished_at?: string }): string { if (!run.finished_at) return '—'; const ms = Date.parse(run.finished_at) - Date.parse(run.created_at); return Number.isFinite(ms) ? `${Math.max(0, ms / 1000).toFixed(1)} 秒` : '—' }
export function officeNumber(value?: number): string { return value == null ? '—' : value.toLocaleString() }
