import { ref } from 'vue'
import { apiRequest } from '@/api/client'

export function useLogs() {
  const logs = ref<any[]>([])
  const logPage = ref(1)
  const logPageSize = ref(20)
  const logTotal = ref(0)
  const logPageSizeOptions = [20, 30, 50, 100, 200]

  async function loadLogs(page = logPage.value, pageSize = logPageSize.value) {
    const data = await apiRequest(`/api/logs?page=${page}&page_size=${pageSize}`)
    logs.value = data.items || []
    logTotal.value = Number(data.total || 0)
    logPage.value = Number(data.page || page || 1)
    logPageSize.value = Number(data.page_size || pageSize || 20)
  }

  async function handleLogPageChange(page: number) {
    logPage.value = page
    await loadLogs(page, logPageSize.value)
  }

  async function handleLogPageSizeChange(size: number) {
    logPageSize.value = size
    logPage.value = 1
    await loadLogs(1, size)
  }

  return {
    logs,
    logPage,
    logPageSize,
    logTotal,
    logPageSizeOptions,
    loadLogs,
    handleLogPageChange,
    handleLogPageSizeChange,
  }
}
