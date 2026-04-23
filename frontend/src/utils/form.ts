import type { Field, FieldPhase, ActionForm } from '@/types'
import { PRINT_ROLE_NAME_TO_ID } from '@/config/print'

export type FieldMeta = Pick<Field, 'required' | 'readonly' | 'masked' | 'placeholder' | 'randomButton' | 'multiple' | 'phase'>

export function t(key: string, label: string, meta: FieldMeta = {}): Field {
  return { key, label, type: 'text', ...meta }
}

export function p(key: string, label: string, meta: FieldMeta = {}): Field {
  return { key, label, type: 'password', ...meta }
}

export function ta(key: string, label: string, meta: FieldMeta = {}): Field {
  return { key, label, type: 'textarea', ...meta }
}

export function sw(key: string, label: string, meta: FieldMeta = {}): Field {
  return { key, label, type: 'switch', ...meta }
}

export function sel(key: string, label: string, options: any[], meta: FieldMeta = {}): Field {
  return { key, label, type: 'select', options, ...meta }
}

export function fileField(key: string, label: string, meta: FieldMeta = {}): Field {
  return { key, label, type: 'file', ...meta }
}

export function form(title: string, action: string, fields: Field[], defaults: Record<string, any> = {}): ActionForm {
  const initial: Record<string, any> = {}
  for (const field of fields) {
    initial[field.key] = field.type === 'switch' ? false : ''
  }
  const merged = { ...initial, ...defaults }
  return {
    title,
    action,
    fields,
    model: { ...merged },
    defaults: { ...merged },
    loading: false,
    progress: 0,
    result: '',
    resultItems: [],
  }
}

export function isEmptyRequiredValue(value: any): boolean {
  if (value === null || value === undefined) return true
  if (Array.isArray(value)) return value.length === 0
  if (typeof value === 'string') return value.trim() === ''
  return false
}

export function normalizePathText(value: any): string {
  let s = String(value ?? '')
  while (s.includes('\\\\')) {
    s = s.replace(/\\\\/g, '\\')
  }
  return s
}

export function normalizePrintItems(items: any[]): any[] {
  return items.map((item) => {
    if (!item || typeof item !== 'object') return item
    return {
      ...item,
      dept: normalizePathText((item as any).dept),
      section: normalizePathText((item as any).section),
    }
  })
}

export function stringifyResult(data: any, normalizePath = false): string {
  const text = JSON.stringify(data, null, 2)
  if (!normalizePath) return text
  return text.replace(/\\\\/g, '\\')
}

export function parsePrintRoleIDsFromNames(roleNames: string): string[] {
  const ids: string[] = []
  for (const one of String(roleNames || '').split('|')) {
    const key = one.trim()
    if (!key) continue
    const id = PRINT_ROLE_NAME_TO_ID[key]
    if (id && !ids.includes(id)) {
      ids.push(id)
    }
  }
  return ids
}

export function normalizePrintRoleIDs(value: any, roleNames = ''): string[] {
  const raw: string[] = []
  if (Array.isArray(value)) {
    for (const one of value) {
      const id = String(one || '').trim()
      if (id) raw.push(id)
    }
  } else {
    const s = String(value || '').trim()
    if (s) {
      for (const one of s.split(',')) {
        const id = one.trim()
        if (id) raw.push(id)
      }
    }
  }
  if (!raw.length && roleNames) {
    raw.push(...parsePrintRoleIDsFromNames(roleNames))
  }
  return Array.from(new Set(raw))
}

export function isPrintModifyForm(form?: ActionForm): boolean {
  return !!form && form.action === 'modify_user'
}

export function isPrintModifyEditMode(form?: ActionForm): boolean {
  return isPrintModifyForm(form) && String(form?.model?.__mode || 'query') === 'edit'
}

export function visibleFields(form?: ActionForm): Field[] {
  if (!form) return []
  if (!isPrintModifyForm(form)) return form.fields
  const mode: FieldPhase = isPrintModifyEditMode(form) ? 'edit' : 'query'
  return form.fields.filter((field) => !field.phase || field.phase === 'all' || field.phase === mode)
}

export function resetPrintModifyModel(form?: ActionForm) {
  if (!form) return
  form.model.__mode = 'query'
  form.model.search_key = form.model.search_key || 'fullname'
  form.model.search_content = ''
  form.model.name = ''
  form.model.fullname = ''
  form.model.sex = ''
  form.model.status = 'enabled'
  form.model.email = ''
  form.model.section = ''
  form.model.roles = []
  form.model.__user_id = ''
  form.model.__ori_email = ''
}

export function resetActionFormModel(form?: ActionForm) {
  if (!form) return
  form.model = { ...form.defaults }
}
