import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import type { ActionForm } from '@/types'
import { useAuthStore } from '@/stores/auth'
import { apiRequest, isAuthExpiredError } from '@/api/client'
import { runAsyncProjectAction } from './useAsyncOperation'
import {
  isEmptyRequiredValue,
  normalizePrintItems,
  normalizePrintRoleIDs,
  stringifyResult,
  isPrintModifyForm,
  isPrintModifyEditMode,
  visibleFields,
  resetPrintModifyModel,
  resetActionFormModel,
} from '@/utils/form'
import { normalizeProjectSessionState, formatSessionEventTime } from '@/utils/session'

export function useProjectActions() {
  const router = useRouter()
  const message = useMessage()
  const auth = useAuthStore()

  const loadingProject = ref(false)
  const oldPwd = ref('')
  const newPwd = ref('')
  const showPwd = ref(false)
  const windowCloseHandled = ref(false)

  function handleRequestError(err: any, fallback = '请求失败') {
    if (isAuthExpiredError(err)) {
      return
    }
    const msg = String(err?.message || '').trim()
    message.error(msg || fallback)
  }

  async function downloadAdBatchTemplate() {
    try {
      const headers: Record<string, string> = {}
      if (auth.token) {
        headers.Authorization = `Bearer ${auth.token}`
      }
      const res = await fetch(`${auth.apiBase}/api/projects/ad/batch-template`, {
        method: 'GET',
        headers,
      })
      if (res.status === 401) {
        auth.clearSession()
        await router.replace('/login')
        return
      }
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || '下载模板失败')
      }
      const blob = await res.blob()
      let filename = '创建AD用户模板.xlsx'
      const disposition = res.headers.get('content-disposition') || ''
      const m = disposition.match(/filename\*=UTF-8''([^;]+)/i)
      if (m && m[1]) {
        filename = decodeURIComponent(m[1])
      }
      const link = document.createElement('a')
      const url = URL.createObjectURL(blob)
      link.href = url
      link.download = filename
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      URL.revokeObjectURL(url)
    } catch (e: any) {
      handleRequestError(e, '下载模板失败')
    }
  }

  async function handleAdBatchFileUpload(options: any, form: ActionForm, field: any) {
    try {
      const rawFile: File | undefined =
        options?.file?.file ||
        options?.file?.blobFile ||
        options?.file
      if (!rawFile) {
        throw new Error('未获取到上传文件')
      }
      const formData = new FormData()
      formData.append('file', rawFile)
      formData.append('old_file', String(form.model[field.key] || ''))

      const headers: Record<string, string> = {}
      if (auth.token) {
        headers.Authorization = `Bearer ${auth.token}`
      }
      const res = await fetch(`${auth.apiBase}/api/projects/ad/batch-upload`, {
        method: 'POST',
        headers,
        body: formData,
      })
      if (res.status === 401) {
        auth.clearSession()
        await router.replace('/login')
        return
      }
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        throw new Error(data.error || '上传失败')
      }
      form.model[field.key] = String(data.name || '')
      form.resultItems = []
      form.result = ''
      options?.onFinish?.()
      message.success(`上传成功：${form.model[field.key]}`)
    } catch (e: any) {
      options?.onError?.()
      handleRequestError(e, '上传失败')
    }
  }

  async function confirmPrintModify(form: ActionForm) {
    form.loading = true
    try {
      const searchKey = String(form.model.search_key || 'fullname').trim() || 'fullname'
      const searchContent = String(form.model.search_content || '').trim()
      if (!searchContent) {
        throw new Error('查询值不能为空')
      }
      const res = await apiRequest('/api/projects/print/operate', 'POST', {
        action: 'get_user',
        params: {
          search_key: searchKey,
          search_content: searchContent,
        },
      })
      const item = res?.data?.item || {}
      form.model.name = String(item.name || '')
      form.model.fullname = String(item.fullname || '')
      form.model.sex = String(item.sex || '')
      form.model.status = String(item.status || 'enabled')
      form.model.email = String(item.email || '')
      form.model.section = String(item.section || '')
      form.model.roles = normalizePrintRoleIDs(item.role_ids, String(item.role_names || ''))
      form.model.__user_id = String(item.id || '')
      form.model.__ori_email = String(item.email || '')
      form.model.__mode = 'edit'
      form.resultItems = []
      form.result = stringifyResult(res, true)
      message.success('user info loaded')
    } catch (e: any) {
      form.resultItems = []
      form.result = '错误：' + e.message
      handleRequestError(e)
    } finally {
      form.loading = false
    }
  }

  function backPrintModify(form: ActionForm) {
    resetPrintModifyModel(form)
  }

  async function ensureLoaded(project: string, pushProjectSessionLog: (project: string, state: any, text: string) => void) {
    if (auth.loadedProjects[project]) return
    loadingProject.value = true
    try {
      const res = await apiRequest(`/api/projects/${project}/load`, 'POST', {})
      auth.markProjectLoaded(project)
      const state = normalizeProjectSessionState(res?.session_state)
      pushProjectSessionLog(
        project,
        state,
        state === 'first_login'
          ? `项目首次登录完成`
          : `项目复用已有会话`,
      )
    } finally {
      loadingProject.value = false
    }
  }

  async function submitAction(
    f: ActionForm,
    activeView: string,
    pushProjectSessionLog: (project: string, state: any, text: string) => void,
  ) {
    f.loading = true
    f.progress = 1
    f.resultItems = []
    f.result = '任务已提交，正在执行...'
    try {
      const fields = visibleFields(f)
      const missing = fields
        .filter((field) => field.required && isEmptyRequiredValue(f.model[field.key]))
        .map((field) => field.label)
      if (missing.length) {
        throw new Error('必填项不能为空：' + missing.join('、'))
      }

      const params = { ...f.model }
      delete params.__mode
      delete params.__user_id
      delete params.__ori_email

      if (f.action === 'batch_add_users') {
        params.excel_file = String(params.excel_file || '').trim()
      }
      if (f.action === 'delete_users') {
        params.vpn_users = String(params.vpn_user || '')
          .split(/[,/;]+/)
          .map((x: string) => x.trim())
          .filter(Boolean)
        delete params.vpn_user
      }

      const emailRegex = /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/
      const strongPasswordRegex = /^(?=.*[a-z])(?=.*[A-Z])(?=.*\d).{8,}$/

      if (activeView === 'ad' && f.action === 'add_user') {
        params.password = String(params.password || '').trim()
        const email = String(params.email || '').trim()
        if (!strongPasswordRegex.test(params.password)) {
          throw new Error('密码需至少8位，且包含大小写字母和数字')
        }
        if (!emailRegex.test(email)) {
          throw new Error('邮箱格式不正确')
        }
      }
      if (activeView === 'ad' && f.action === 'reset_password') {
        params.password = String(params.password || '').trim()
        if (params.password && !strongPasswordRegex.test(params.password)) {
          throw new Error('密码需至少8位，且包含大小写字母和数字')
        }
      }

      if (activeView === 'print' && f.action === 'add_user') {
        params.name = String(params.name || '').trim()
        params.fullname = String(params.fullname || '').trim()
        params.sex = String(params.sex || '').trim()
        params.password = String(params.password || '').trim()
        params.email = String(params.email || '').trim()
        params.section = String(params.section || '').trim()
        if (!emailRegex.test(params.email)) {
          throw new Error('邮箱格式不正确')
        }
      }

      if (activeView === 'print' && (f.action === 'search_user' || f.action === 'reset_password' || f.action === 'delete_user')) {
        params.search_content = String(params.search_content || '').trim()
        if (!params.search_content) {
          throw new Error('查询值不能为空')
        }
      }

      if (activeView === 'print' && f.action === 'modify_user') {
        if (!isPrintModifyEditMode(f)) {
          throw new Error('请先点击确认查询')
        }
        params.name = String(params.name || '').trim()
        params.fullname = String(params.fullname || '').trim()
        params.sex = String(params.sex || '').trim()
        params.status = String(params.status || '').trim()
        params.email = String(params.email || '').trim()
        params.section = String(params.section || '').trim()
        params.user_id = String(f.model.__user_id || '').trim()
        params.ori_email = String(f.model.__ori_email || '').trim()
        const roleIDs = normalizePrintRoleIDs(params.roles)
        if (!roleIDs.length) {
          throw new Error('角色不能为空')
        }
        params.roles = roleIDs.join(',')
        if (params.email && !emailRegex.test(params.email)) {
          throw new Error('邮箱格式不正确')
        }
      }
      if (activeView === 'vpn' && f.action === 'add_user') {
        params.vpn_user = String(params.vpn_user || '').trim()
        params.passwd = String(params.passwd || '').trim()
        params.description = String(params.description || '').trim()
        params.mail = String(params.mail || '').trim()
        params.section = String(params.section || '').trim()
        params.status = String(params.status || '').trim()
        if (params.passwd && !strongPasswordRegex.test(params.passwd)) {
          throw new Error('密码需至少8位，且包含大小写字母和数字')
        }
        if (!emailRegex.test(params.mail)) {
          throw new Error('邮箱格式不正确')
        }
      }
      if (activeView === 'vpn' && f.action === 'search_user') {
        params.description = String(params.search_description || '').trim()
        if (!params.description) {
          throw new Error('描述不能为空')
        }
        delete params.search_description
      }
      if (activeView === 'vpn' && f.action === 'modify_password') {
        params.description = String(params.modify_password_description || '').trim()
        params.passwd = String(params.passwd || '').trim()
        if (!params.description) {
          throw new Error('描述不能为空')
        }
        if (params.passwd && !strongPasswordRegex.test(params.passwd)) {
          throw new Error('密码需至少8位，且包含大小写字母和数字')
        }
        delete params.modify_password_description
      }
      if (activeView === 'vpn' && f.action === 'modify_status') {
        params.description = String(params.modify_status_description || '').trim()
        params.status = String(params.status || '').trim()
        if (!params.description) {
          throw new Error('描述不能为空')
        }
        delete params.modify_status_description
      }
      if (activeView === 'vpn' && f.action === 'delete_users') {
        if (!Array.isArray(params.vpn_users) || params.vpn_users.length === 0) {
          throw new Error('用户名不能为空')
        }
      }

      const job = await runAsyncProjectAction(f, activeView, f.action, params)
      let resultItems = Array.isArray(job?.result_items) ? job.result_items : []
      if (activeView === 'print') {
        resultItems = normalizePrintItems(resultItems)
      }
      f.resultItems = resultItems
      const resultText = String(job?.result_text || '').trim()
      if (resultText) {
        f.result = resultText
      }
      if (!job?.ok) {
        throw new Error(String(job?.error || job?.message || '执行失败'))
      }

      f.progress = 100
      if (activeView === 'print' && f.action === 'add_user') {
        resetActionFormModel(f)
      }
      if (activeView === 'ad' && f.action === 'add_user') {
        resetActionFormModel(f)
      }
      if (activeView === 'vpn') {
        resetActionFormModel(f)
      }
      if (activeView === 'print' && f.action === 'modify_user') {
        backPrintModify(f)
      }
      if (activeView === 'ad' && f.action !== 'add_user') {
        resetActionFormModel(f)
      }
      if (activeView === 'print' && f.action !== 'add_user' && f.action !== 'modify_user') {
        resetActionFormModel(f)
      }
      message.success(job?.message || 'success')
    } catch (e: any) {
      const errMsg = String(e?.message || '执行失败')
      if (!String(f.result || '').trim()) {
        f.result = '错误：' + errMsg
      } else if (!String(f.result).includes(errMsg)) {
        f.result = `${f.result}\n错误：${errMsg}`
      }
      handleRequestError(e)
    } finally {
      f.loading = false
    }
  }

  async function changePassword() {
    try {
      await apiRequest('/api/auth/change-password', 'POST', {
        old_password: oldPwd.value,
        new_password: newPwd.value,
      })
      message.success('密码修改成功')
      oldPwd.value = ''
      newPwd.value = ''
      showPwd.value = false
    } catch (e: any) {
      handleRequestError(e)
    }
  }

  function logout() {
    const token = auth.token
    if (token) {
      fetch(`${auth.apiBase}/api/auth/logout`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}` },
        keepalive: true,
      }).catch(() => undefined)
    }
    auth.clearSession()
    router.push('/login')
  }

  function markWindowClosed() {
    return auth.markWindowClosed()
  }

  function ensureSessionAliveBeforeAction(): boolean {
    if (!auth.token) {
      return false
    }
    if (auth.ensureSession()) {
      return true
    }
    auth.clearSession()
    void router.replace('/login')
    return false
  }

  function handleVisibilityOrFocus() {
    if (document.visibilityState === 'hidden') {
      return
    }
    ensureSessionAliveBeforeAction()
  }

  function handleWindowClose() {
    if (windowCloseHandled.value) {
      return
    }
    const meta = auth.markWindowClosed()
    if (!meta || !auth.token) {
      return
    }
    windowCloseHandled.value = true
    fetch(`${auth.apiBase}/api/auth/window-close-start`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${auth.token}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(meta),
      keepalive: true,
    }).catch(() => undefined)
  }

  return {
    loadingProject,
    oldPwd,
    newPwd,
    showPwd,
    windowCloseHandled,
    handleRequestError,
    downloadAdBatchTemplate,
    handleAdBatchFileUpload,
    confirmPrintModify,
    backPrintModify,
    ensureLoaded,
    submitAction,
    changePassword,
    logout,
    markWindowClosed,
    ensureSessionAliveBeforeAction,
    handleVisibilityOrFocus,
    handleWindowClose,
  }
}
