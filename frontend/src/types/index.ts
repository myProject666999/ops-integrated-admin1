export type FieldPhase = 'query' | 'edit' | 'all'

export type Field = {
  key: string
  label: string
  type: 'text' | 'password' | 'select' | 'switch' | 'textarea' | 'file'
  options?: { label: string; value: any }[]
  required?: boolean
  readonly?: boolean
  masked?: boolean
  placeholder?: string
  randomButton?: boolean
  multiple?: boolean
  phase?: FieldPhase
}

export type ActionForm = {
  title: string
  action: string
  fields: Field[]
  model: Record<string, any>
  defaults: Record<string, any>
  loading: boolean
  progress: number
  result: string
  resultItems: any[]
}

export type ProjectSessionStateKey = 'idle' | 'first_login' | 'reused' | 'countdown_relogin'

export type ProjectSessionSnapshot = {
  state: ProjectSessionStateKey
  label: string
  updatedAt: string
}

export type ProjectSessionLog = {
  id: number
  project: string
  state: ProjectSessionStateKey
  text: string
  time: string
}

export type AsyncOperateJobResp = {
  job_id: string
  status: string
  ok: boolean
  done: boolean
  message: string
  error: string
  progress: number
  processed: number
  total: number
  log_lines: string[]
  result_text: string
  result_items: any[]
}

export type ViewMeta = {
  kicker: string
  title: string
  desc: string
  tag: string
}
