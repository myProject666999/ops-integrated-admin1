import type { ProjectSessionStateKey } from '@/types'

export function projectSessionStateLabel(state: ProjectSessionStateKey): string {
  if (state === 'first_login') return '首次登录'
  if (state === 'reused') return '复用会话'
  if (state === 'countdown_relogin') return '倒计时重登'
  return '未登录'
}

export function normalizeProjectSessionState(raw: any): ProjectSessionStateKey {
  const state = String(raw || '').trim()
  if (state === 'first_login' || state === 'reused' || state === 'countdown_relogin') {
    return state
  }
  return 'idle'
}

export function formatSessionEventTime(value = new Date()) {
  return value.toLocaleTimeString('zh-CN', { hour12: false })
}

export function projectSessionStateFromDidLogin(didLogin: boolean): ProjectSessionStateKey {
  return didLogin ? 'first_login' : 'reused'
}

export function formatCountdown(value: number): string {
  const safe = Math.max(0, Math.floor(value))
  const mm = String(Math.floor(safe / 60)).padStart(2, '0')
  const ss = String(safe % 60).padStart(2, '0')
  return `${mm}:${ss}`
}
