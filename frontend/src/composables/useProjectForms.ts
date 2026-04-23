import { reactive, computed, ref, watch } from 'vue'
import type { ActionForm, Field, ViewMeta } from '@/types'
import { t, p, ta, sw, sel, fileField, form, resetActionFormModel } from '@/utils/form'
import { AD_ORG_UNIT_VALUES } from '@/config/ad'
import {
  PRINT_GENDER_OPTIONS_ADD,
  PRINT_GENDER_OPTIONS_MODIFY,
  PRINT_ROLE_OPTIONS,
  PRINT_SEARCH_KEY_OPTIONS,
  PRINT_SECTION_VALUES,
  PRINT_STATUS_OPTIONS,
} from '@/config/print'
import { VPN_SECTION_VALUES } from '@/config/vpn'
import { generateAdPassword } from '@/utils/password'

const adOrgUnitOptions = AD_ORG_UNIT_VALUES.map((value) => ({ label: value, value }))
const defaultAdOrgUnit = AD_ORG_UNIT_VALUES.includes('CLHD') ? 'CLHD' : (AD_ORG_UNIT_VALUES[0] ?? '')
const printSectionOptions = PRINT_SECTION_VALUES.map((value) => ({ label: value, value }))
const vpnSectionOptions = VPN_SECTION_VALUES.map((value) => ({ label: value, value }))

export const menuOptions = [
  { label: '项目凭据', key: 'config' },
  { label: 'AD管理', key: 'ad' },
  { label: '打印管理', key: 'print' },
  { label: 'VPN管理', key: 'vpn' },
  { label: '操作日志', key: 'logs' },
]

export const viewMetaMap: Record<string, ViewMeta> = {
  config: {
    kicker: 'CREDENTIAL CENTER',
    title: '项目凭据配置中心',
    desc: '为当前管理员独立维护 AD、打印、VPN 与防火墙凭据。',
    tag: '安全配置',
  },
  ad: {
    kicker: 'AD MANAGEMENT',
    title: 'AD 域账号管理',
    desc: '新增、查询、改密、解锁与批量任务统一在此执行。',
    tag: '身份目录',
  },
  print: {
    kicker: 'PRINT HUB',
    title: '打印权限管理',
    desc: '管理打印用户信息、角色权限与账户维护操作。',
    tag: '打印系统',
  },
  vpn: {
    kicker: 'VPN CONTROL',
    title: 'VPN 访问管理',
    desc: '统一处理 VPN 账户新增、查询、改密、改状态与删除。',
    tag: '远程接入',
  },
  logs: {
    kicker: 'AUDIT LOGS',
    title: '操作日志审计',
    desc: '集中查看管理员行为与项目操作轨迹，支持分页检索。',
    tag: '行为审计',
  },
}

export function createFormMap(): Record<string, ActionForm[]> {
  return reactive({
    ad: [
      form(
        '新增用户',
        'add_user',
        [
          t('sn', '姓'),
          t('given_name', '名'),
          t('cn', '姓名', { required: true }),
          t('username', '用户名', { required: true }),
          p('password', '密码', { required: true, masked: false, randomButton: true }),
          t('email', '邮箱', { required: true }),
          t('description', '描述'),
          sel('ou', '组织单位', adOrgUnitOptions, { required: true }),
        ],
        { ou: defaultAdOrgUnit },
      ),
      form('批量新增用户', 'batch_add_users', [fileField('excel_file', 'Excel 文件', { required: true })]),
      form('查询用户', 'search_user', [t('search_name', '搜索关键词', { required: true, placeholder: '可搜索姓名，工号或GID' })]),
      form(
        '重置密码',
        'reset_password',
        [
          t('name', '用户名', { required: true }),
          p('password', '新密码', { masked: false, randomButton: true }),
          sw('pwd_last_set', '用户下次登陆时须更改密码(U)'),
        ],
        { pwd_last_set: true },
      ),
      form('解锁用户', 'unlock_user', [t('name', '用户名', { required: true })]),
      form('修改描述', 'modify_description', [t('name', '用户名', { required: true }), t('description', '新描述')]),
      form('修改姓名', 'modify_name', [t('name', '用户名', { required: true }), t('sn', '姓'), t('given_name', '名'), t('cn', '姓名', { required: true })]),
      form('删除用户', 'delete_user', [t('name', '用户名', { required: true })]),
    ],
    print: [
      form(
        '新增用户',
        'add_user',
        [
          t('name', '用户名', { required: true }),
          t('fullname', '姓名', { required: true }),
          sel('sex', '性别', PRINT_GENDER_OPTIONS_ADD, { required: true, placeholder: '请选择性别' }),
          p('password', '密码', { required: true }),
          t('email', '邮箱', { required: true }),
          sel('section', '部门', printSectionOptions, { required: true, placeholder: '请选择部门' }),
        ],
        { name: '', fullname: '', sex: '', email: '', section: '', password: '123' },
      ),
      form(
        '查询用户',
        'search_user',
        [
          sel('search_key', '查询字段', PRINT_SEARCH_KEY_OPTIONS, { required: true }),
          t('search_content', '查询值', { required: true }),
        ],
        { search_key: 'fullname' },
      ),
      form(
        '重置密码',
        'reset_password',
        [
          sel('search_key', '查询字段', PRINT_SEARCH_KEY_OPTIONS, { required: true }),
          t('search_content', '查询值', { required: true }),
          p('password', '新密码', { required: true }),
        ],
        { search_key: 'fullname', password: '123' },
      ),
      form(
        '修改用户',
        'modify_user',
        [
          sel('search_key', '查询字段', PRINT_SEARCH_KEY_OPTIONS, { required: true, phase: 'query' }),
          t('search_content', '查询值', { required: true, phase: 'query' }),
          t('name', '用户名', { required: true, readonly: true, phase: 'edit' }),
          t('fullname', '姓名', { required: true, phase: 'edit' }),
          sel('sex', '性别', PRINT_GENDER_OPTIONS_MODIFY, { required: true, phase: 'edit' }),
          sel('status', '状态', PRINT_STATUS_OPTIONS, { required: true, phase: 'edit' }),
          t('email', '邮箱', { phase: 'edit' }),
          sel('section', '部门', printSectionOptions, { required: true, phase: 'edit', placeholder: '请选择部门' }),
          sel('roles', '角色', PRINT_ROLE_OPTIONS, { required: true, phase: 'edit', multiple: true, placeholder: '请选择角色' }),
        ],
        { search_key: 'fullname', status: 'enabled', roles: [], __mode: 'query' },
      ),
      form(
        '删除用户',
        'delete_user',
        [
          sel('search_key', '查询字段', PRINT_SEARCH_KEY_OPTIONS, { required: true }),
          t('search_content', '查询值', { required: true }),
        ],
        { search_key: 'fullname' },
      ),
    ],
    vpn: [
      form(
        '新增用户',
        'add_user',
        [
          t('vpn_user', '用户名', { required: true }),
          p('passwd', '新密码', { masked: false, randomButton: true }),
          t('description', '描述', { required: true }),
          t('mail', '邮箱', { required: true }),
          sel('section', '所属父组', vpnSectionOptions, { required: true, placeholder: '请选择所属父组' }),
          sel('status', '状态', [
            { label: '启用', value: 'enabled' },
            { label: '禁用', value: 'disabled' },
          ], { required: true }),
        ],
        { status: 'enabled', section: 'default^root' },
      ),
      form('查询用户', 'search_user', [t('search_description', '描述', { required: true })]),
      form(
        '修改密码',
        'modify_password',
        [t('modify_password_description', '描述', { required: true }), p('passwd', '新密码', { masked: false, randomButton: true })],
      ),
      form(
        '修改状态',
        'modify_status',
        [
          t('modify_status_description', '描述', { required: true }),
          sel('status', '状态', [
            { label: '启用', value: 'enabled' },
            { label: '禁用', value: 'disabled' },
          ], { required: true }),
        ],
        { status: 'enabled' },
      ),
      form(
        '删除用户',
        'delete_users',
        [
          t('vpn_user', '用户名', { required: true, placeholder: '多用户可用 , ; / 三种符号隔开' }),
          sw('remote_firewall', '同步删除防火墙上的VPN账户'),
        ],
        { remote_firewall: false },
      ),
    ],
  })
}

export function useProjectForms(formMap: Record<string, ActionForm[]>) {
  const activeView = ref('config')
  const selectedAction = reactive<Record<string, string>>({
    ad: '',
    print: '',
    vpn: '',
  })

  const currentViewMeta = computed(() => viewMetaMap[activeView.value] || viewMetaMap.config)
  const isProjectView = computed(() => ['ad', 'print', 'vpn'].includes(activeView.value))

  const currentProjectForms = computed(() => {
    if (!isProjectView.value) return []
    return formMap[activeView.value] || []
  })

  const currentProjectAction = computed(() => {
    if (!isProjectView.value) return ''
    const key = activeView.value
    const saved = selectedAction[key]
    if (saved && currentProjectForms.value.some((f) => f.action === saved)) {
      return saved
    }
    return currentProjectForms.value[0]?.action || ''
  })

  const projectFunctionMenuOptions = computed(() => currentProjectForms.value.map((f) => ({ label: f.title, key: f.action })))
  const projectFunctionSelectOptions = computed(() =>
    currentProjectForms.value.map((f) => ({
      label: f.title,
      value: f.action,
    })),
  )

  const currentProjectForm = computed(() => {
    return currentProjectForms.value.find((f) => f.action === currentProjectAction.value) || currentProjectForms.value[0]
  })

  const adAddUserForm = computed(() => formMap.ad?.find((x: ActionForm) => x.action === 'add_user'))
  const adResetPasswordForm = computed(() => formMap.ad?.find((x: ActionForm) => x.action === 'reset_password'))
  const adModifyNameForm = computed(() => formMap.ad?.find((x: ActionForm) => x.action === 'modify_name'))
  const vpnAddUserForm = computed(() => formMap.vpn?.find((x: ActionForm) => x.action === 'add_user'))
  const vpnModifyPasswordForm = computed(() => formMap.vpn?.find((x: ActionForm) => x.action === 'modify_password'))
  const printModifyUserForm = computed(() => formMap.print?.find((x: ActionForm) => x.action === 'modify_user'))

  function setProjectDefaultAction(project: string) {
    const forms = formMap[project] || []
    if (!forms.length) return
    selectedAction[project] = forms[0].action
  }

  function syncAdFullName() {
    const f = adAddUserForm.value
    if (!f) return
    const sn = String(f.model.sn || '').trim()
    const givenName = String(f.model.given_name || '').trim()
    f.model.cn = `${sn}${givenName}`
  }

  function syncAdModifyNameFullName() {
    const f = adModifyNameForm.value
    if (!f) return
    const sn = String(f.model.sn || '').trim()
    const givenName = String(f.model.given_name || '').trim()
    if (sn || givenName) {
      f.model.cn = `${sn}${givenName}`
    }
  }

  function initAdAddUserDefaults() {
    const f = adAddUserForm.value
    if (!f) return
    f.model.password = generateAdPassword()
    if (!f.model.ou) {
      f.model.ou = defaultAdOrgUnit
    }
    syncAdFullName()
  }

  function initAdResetPasswordDefaults() {
    const f = adResetPasswordForm.value
    if (!f) return
    f.model.password = generateAdPassword()
  }

  function initVpnAddUserDefaults() {
    const f = vpnAddUserForm.value
    if (!f) return
    f.model.passwd = generateAdPassword()
  }

  function initVpnModifyPasswordDefaults() {
    const f = vpnModifyPasswordForm.value
    if (!f) return
    f.model.passwd = generateAdPassword()
  }

  watch(
    () => [adAddUserForm.value?.model.sn, adAddUserForm.value?.model.given_name],
    () => syncAdFullName(),
    { immediate: true },
  )

  watch(
    () => [adModifyNameForm.value?.model.sn, adModifyNameForm.value?.model.given_name],
    () => syncAdModifyNameFullName(),
    { immediate: true },
  )

  return {
    activeView,
    selectedAction,
    currentViewMeta,
    isProjectView,
    currentProjectForms,
    currentProjectAction,
    projectFunctionMenuOptions,
    projectFunctionSelectOptions,
    currentProjectForm,
    adAddUserForm,
    adResetPasswordForm,
    adModifyNameForm,
    vpnAddUserForm,
    vpnModifyPasswordForm,
    printModifyUserForm,
    setProjectDefaultAction,
    initAdAddUserDefaults,
    initAdResetPasswordDefaults,
    initVpnAddUserDefaults,
    initVpnModifyPasswordDefaults,
    syncAdFullName,
    syncAdModifyNameFullName,
    adOrgUnitOptions,
    defaultAdOrgUnit,
    printSectionOptions,
    vpnSectionOptions,
    generateAdPassword,
  }
}
