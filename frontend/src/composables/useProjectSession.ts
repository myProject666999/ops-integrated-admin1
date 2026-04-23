import { ref, reactive, computed } from 'vue'
import type { ProjectSessionStateKey, ProjectSessionSnapshot, ProjectSessionLog } from '@/types'
import { projectSessionStateLabel, normalizeProjectSessionState, formatSessionEventTime } from '@/utils/session'

export function useProjectSession() {
  const projectSessionStates = reactive<Record<string, ProjectSessionSnapshot>>({
    ad: { state: 'idle', label: '未登录', updatedAt: '' },
    print: { state: 'idle', label: '未登录', updatedAt: '' },
    vpn: { state: 'idle', label: '未登录', updatedAt: '' },
  })
  const projectSessionLogs = ref<ProjectSessionLog[]>([])
  let projectSessionLogSeq = 0

  const currentProjectSessionState = computed<ProjectSessionSnapshot>((activeView: string) => {
    const key = activeView
    return projectSessionStates[key] || { state: 'idle', label: '未登录', updatedAt: '' }
  })

  const currentProjectSessionLogs = computed((activeView: string) =>
    projectSessionLogs.value.filter((item) => item.project === activeView).slice(0, 6),
  )

  function pushProjectSessionLog(project: string, state: ProjectSessionStateKey, text: string) {
    const key = String(project || '').trim()
    if (!key || !projectSessionStates[key]) return
    const now = new Date()
    projectSessionStates[key] = {
      state,
      label: projectSessionStateLabel(state),
      updatedAt: formatSessionEventTime(now),
    }
    projectSessionLogs.value.unshift({
      id: ++projectSessionLogSeq,
      project: key,
      state,
      text,
      time: formatSessionEventTime(now),
    })
    if (projectSessionLogs.value.length > 24) {
      projectSessionLogs.value = projectSessionLogs.value.slice(0, 24)
    }
  }

  function resetProjectSessionVisuals() {
    projectSessionStates.ad = { state: 'idle', label: '未登录', updatedAt: '' }
    projectSessionStates.print = { state: 'idle', label: '未登录', updatedAt: '' }
    projectSessionStates.vpn = { state: 'idle', label: '未登录', updatedAt: '' }
    projectSessionLogs.value = []
  }

  return {
    projectSessionStates,
    projectSessionLogs,
    currentProjectSessionState,
    currentProjectSessionLogs,
    pushProjectSessionLog,
    resetProjectSessionVisuals,
  }
}
