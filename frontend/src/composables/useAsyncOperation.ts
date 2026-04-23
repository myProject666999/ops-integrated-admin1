import type { ActionForm, AsyncOperateJobResp } from '@/types'
import { apiRequest } from '@/api/client'

export function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

export function updateFormProgressState(f: ActionForm, job: AsyncOperateJobResp) {
  const progress = Number(job?.progress || 0)
  f.progress = Number.isFinite(progress) ? Math.max(0, Math.min(100, Math.floor(progress))) : 0

  const logLines = Array.isArray(job?.log_lines) ? job.log_lines.filter((x) => String(x || '').trim() !== '') : []
  if (logLines.length > 0) {
    f.result = logLines.join('\n')
  }
  if (Array.isArray(job?.result_items)) {
    f.resultItems = job.result_items
  }
  if (String(job?.result_text || '').trim()) {
    f.result = String(job.result_text).trim()
  }
}

export async function runAsyncProjectAction(
  f: ActionForm,
  projectType: string,
  action: string,
  params: Record<string, any>,
): Promise<AsyncOperateJobResp> {
  const start = await apiRequest('/api/projects/operate-async', 'POST', {
    project_type: projectType,
    action,
    params,
  })
  const jobID = String(start?.job_id || '').trim()
  if (!jobID) {
    throw new Error('创建异步任务失败')
  }

  const timeoutAt = Date.now() + 30 * 60 * 1000
  while (true) {
    const job = (await apiRequest(`/api/projects/operate-async/${encodeURIComponent(jobID)}`)) as AsyncOperateJobResp
    updateFormProgressState(f, job)
    if (job?.done) {
      return job
    }
    if (Date.now() >= timeoutAt) {
      throw new Error('任务执行超时，请稍后重试')
    }
    await sleep(500)
  }
}

export function useAsyncOperation() {
  return {
    sleep,
    updateFormProgressState,
    runAsyncProjectAction,
  }
}
