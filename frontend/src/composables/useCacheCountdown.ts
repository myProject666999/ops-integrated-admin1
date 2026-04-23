import { ref, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore, getProjectCacheDeadlineKey, getProjectCacheReloginLockKey } from '@/stores/auth'
import { apiRequest } from '@/api/client'
import { formatCountdown, normalizeProjectSessionState, projectSessionStateLabel } from '@/utils/session'

export function useCacheCountdown() {
  const router = useRouter()
  const auth = useAuthStore()

  const cacheTTLSeconds = ref(600)
  const cacheCountdownSeconds = ref(600)
  let cacheCountdownTimer: number | null = null
  let reloginInProgress = false
  const cacheReloginLockMs = 15 * 1000
  const projectFieldFocused = ref(false)

  function cacheDeadlineStorageKey() {
    return getProjectCacheDeadlineKey(auth.token)
  }

  function cacheReloginLockStorageKey() {
    return getProjectCacheReloginLockKey(auth.token)
  }

  function readSharedCacheDeadline(): number {
    const key = cacheDeadlineStorageKey()
    if (!key) return 0
    const raw = Number(localStorage.getItem(key) || 0)
    if (!Number.isFinite(raw) || raw <= 0) {
      return 0
    }
    return raw
  }

  function syncCacheCountdownFromDeadline() {
    const deadline = readSharedCacheDeadline()
    if (deadline <= 0) {
      cacheCountdownSeconds.value = cacheTTLSeconds.value
      return
    }
    cacheCountdownSeconds.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
  }

  function setSharedCacheDeadline(deadlineMs: number) {
    const key = cacheDeadlineStorageKey()
    if (!key) return
    localStorage.setItem(key, String(deadlineMs))
    syncCacheCountdownFromDeadline()
  }

  function ensureSharedCacheDeadline() {
    if (!auth.token) return
    const deadline = readSharedCacheDeadline()
    if (deadline > 0) {
      syncCacheCountdownFromDeadline()
      return
    }
    setSharedCacheDeadline(Date.now() + cacheTTLSeconds.value * 1000)
  }

  function resetCacheCountdown() {
    if (!auth.token) {
      cacheCountdownSeconds.value = cacheTTLSeconds.value
      return
    }
    setSharedCacheDeadline(Date.now() + cacheTTLSeconds.value * 1000)
  }

  function acquireCacheReloginLock(): boolean {
    const key = cacheReloginLockStorageKey()
    if (!key) return false
    const now = Date.now()
    const current = Number(localStorage.getItem(key) || 0)
    if (Number.isFinite(current) && current > now) {
      return false
    }
    const next = now + cacheReloginLockMs
    localStorage.setItem(key, String(next))
    return Number(localStorage.getItem(key) || 0) === next
  }

  function releaseCacheReloginLock() {
    const key = cacheReloginLockStorageKey()
    if (!key) return
    localStorage.removeItem(key)
  }

  function handleStorageSync(event: StorageEvent) {
    if (!auth.token) return
    const deadlineKey = cacheDeadlineStorageKey()
    const reloginLockKey = cacheReloginLockStorageKey()
    if (event.key === deadlineKey || event.key === reloginLockKey) {
      syncCacheCountdownFromDeadline()
    }
  }

  async function loadAuthMeta() {
    try {
      const data = await apiRequest('/api/auth/me')
      const ttl = Number(data?.project_cache_ttl_seconds || 0)
      if (Number.isFinite(ttl) && ttl > 0) {
        cacheTTLSeconds.value = ttl
        ensureSharedCacheDeadline()
      }
      const idleTTL = Number(data?.session_idle_ttl_seconds || 0)
      if (Number.isFinite(idleTTL) && idleTTL > 0) {
        auth.setIdleTTL(idleTTL)
      }
    } catch (e) {
      // keep default TTL
    }
  }

  async function reloginProjectsSilently() {
    if (reloginInProgress) return
    if (!acquireCacheReloginLock()) {
      syncCacheCountdownFromDeadline()
      return
    }
    reloginInProgress = true
    try {
      const data = await apiRequest('/api/projects/relogin', 'POST', {})
      auth.clearLoadedProjects()
      const items = Array.isArray(data?.items) ? data.items : []
      for (const item of items) {
        const key = String(item?.project_type || '').trim()
        if (!key) continue
        if (item?.ok) {
          auth.markProjectLoaded(key)
          const state = normalizeProjectSessionState(item?.session_state) || 'countdown_relogin'
          // 这里需要外部传入 pushProjectSessionLog
        }
      }
    } catch (e) {
      // silent refresh
    } finally {
      reloginInProgress = false
      resetCacheCountdown()
      releaseCacheReloginLock()
    }
  }

  function isAnyProjectActionRunning(formMap: Record<string, any[]>): boolean {
    return ['ad', 'print', 'vpn'].some((projectKey) => {
      const forms = formMap[projectKey] || []
      return forms.some((form: any) => form.loading)
    })
  }

  function startCacheCountdownTimer(formMap: Record<string, any[]>) {
    if (cacheCountdownTimer !== null) {
      window.clearInterval(cacheCountdownTimer)
    }
    cacheCountdownTimer = window.setInterval(async () => {
      if (!auth.token) return
      syncCacheCountdownFromDeadline()
      if (projectFieldFocused.value) return
      if (isAnyProjectActionRunning(formMap)) return
      if (cacheCountdownSeconds.value > 0) {
        return
      }
      await reloginProjectsSilently()
    }, 1000)
  }

  function onProjectFieldFocus() {
    projectFieldFocused.value = true
  }

  function onProjectFieldBlur() {
    window.setTimeout(() => {
      projectFieldFocused.value = false
    }, 0)
  }

  onMounted(() => {
    window.addEventListener('storage', handleStorageSync)
  })

  onBeforeUnmount(() => {
    window.removeEventListener('storage', handleStorageSync)
    if (cacheCountdownTimer !== null) {
      window.clearInterval(cacheCountdownTimer)
      cacheCountdownTimer = null
    }
  })

  return {
    cacheTTLSeconds,
    cacheCountdownSeconds,
    projectFieldFocused,
    formatCountdown,
    loadAuthMeta,
    reloginProjectsSilently,
    ensureSharedCacheDeadline,
    resetCacheCountdown,
    startCacheCountdownTimer,
    onProjectFieldFocus,
    onProjectFieldBlur,
    acquireCacheReloginLock,
    releaseCacheReloginLock,
    syncCacheCountdownFromDeadline,
    readSharedCacheDeadline,
  }
}
