import { ref, computed } from 'vue'
import { apiRequest } from '@/api/client'
import { useAuthStore } from '@/stores/auth'

const credentialTypeTitleMap: Record<string, string> = {
  ad: 'AD',
  print: '打印',
  vpn: 'VPN',
  vpn_firewall: '防火墙',
}

export function credentialTitle(projectType: string): string {
  const key = String(projectType || '').trim()
  if (!key) return ''
  return credentialTypeTitleMap[key] || key.toUpperCase()
}

export function useCredentials() {
  const auth = useAuthStore()
  const credentials = ref<any[]>([])

  async function loadCredentials() {
    const data = await apiRequest('/api/projects/credentials')
    credentials.value = data.items || []
  }

  async function saveCredential(item: any) {
    await apiRequest(`/api/projects/credentials/${item.project_type}`, 'PUT', {
      account: item.account,
      password: item.password,
    })
    auth.resetProjectLoaded(item.project_type)
  }

  return {
    credentials,
    loadCredentials,
    saveCredential,
    credentialTitle,
  }
}
